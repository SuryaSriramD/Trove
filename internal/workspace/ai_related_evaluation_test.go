package workspace

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"trove.local/phase0/contracts"
	"trove.local/phase0/internal/native"
)

// The declared corpus contains short excerpts, below the production adapter's
// 384-byte split boundary. Use the actual native protocol and identity rather
// than invented vectors for the native runner. Deterministic mode is labelled
// separately and only checks service authority/ranking plumbing.
func workflowShortEmbedding(ctx context.Context, s *native.Supervisor, model string, texts []string, locations []contracts.DomainLocation) ([][]float32, error) {
	if len(texts) != len(locations) {
		return nil, fmt.Errorf("embedding input location count differs")
	}
	for _, text := range texts {
		if text == "" || len(text) > 384 {
			return nil, fmt.Errorf("declared short-text corpus exceeds native adapter boundary")
		}
	}
	f, err := os.CreateTemp("", "trove-related-evaluation-")
	if err != nil {
		return nil, err
	}
	defer os.Remove(f.Name())
	defer f.Close()
	if err = json.NewEncoder(f).Encode(native.InputMetadata{SchemaVersion: 1, Texts: texts, Locations: locations, ContextTokens: 512}); err != nil {
		return nil, err
	}
	input, err := native.OpenInput(f, "text", NewID())
	if err != nil {
		return nil, err
	}
	wire := contracts.JobsWorkerRequest{ProtocolVersion: 1, RequestId: contracts.DomainId(NewID()), JobId: contracts.DomainId(NewID()), LeaseToken: contracts.DomainId(NewID()), CancellationId: contracts.DomainId(NewID()), Operation: "embed", ModelPackId: contracts.DomainId(model), Limits: contracts.JobsResourceLimits{Threads: 2, MemoryBytes: s.MemoryBudget(), TimeoutMs: 180000, MaxOutputBytes: 16 << 20, MaxTokens: 512}}
	raw, err := s.Run(ctx, native.Request{Wire: wire, Inputs: []native.Input{input}, Priority: native.PriorityBackground}, nil)
	if err != nil {
		return nil, err
	}
	var result contracts.JobsWorkerResult
	if err = json.Unmarshal(raw, &result); err != nil {
		return nil, err
	}
	body, ok := result.Value.(contracts.JobsWorkerResultVariant2)
	if !ok || len(body.Vectors) != len(texts) {
		return nil, fmt.Errorf("invalid native embedding result")
	}
	out := make([][]float32, len(body.Vectors))
	for i, v := range body.Vectors {
		out[i] = make([]float32, len(v))
		for j, x := range v {
			out[i][j] = float32(x)
		}
	}
	return out, nil
}
func TestWorkflowRelatedBoundaries(t *testing.T) {
	runWorkflowRelated(t, nil, native.Pack{ID: NewID(), Dimension: 2})
}
func TestRealLocalWorkflowRelated(t *testing.T) {
	s, p := workflowNative(t, "embedding")
	runWorkflowRelated(t, s, p)
}
func runWorkflowRelated(t *testing.T, s *native.Supervisor, pack native.Pack) {
	cases, hash := workflowCorpus(t, "related", 12)
	for _, tc := range cases {
		t.Run(tc.ID, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
			defer cancel()
			k, owner := testKernel(t)
			source, _ := workflowParagraph(t, k, owner, "Current notes", tc.Query)
			relevant, _ := workflowParagraph(t, k, owner, "Related material", tc.RelevantSource)
			irrelevant, _ := workflowParagraph(t, k, owner, "Other material", tc.IrrelevantSource)
			private, _ := workflowParagraph(t, k, owner, "Restricted material", tc.InaccessibleSource)
			scoped := relatedScope(k, owner, source, relevant, irrelevant)
			defer k.RevokeGrant(string(scoped.ScopeGrantId))
			calls := 0
			k.SetEmbeddingProvider(func(ctx context.Context, model string, texts []string, locations []contracts.DomainLocation) ([][]float32, error) {
				calls++
				if model != pack.ID {
					return nil, fmt.Errorf("unexpected model")
				}
				if s != nil {
					return workflowShortEmbedding(ctx, s, model, texts, locations)
				}
				vectors := make([][]float32, len(texts))
				for i, text := range texts {
					vectors[i] = []float32{0, 1}
					if strings.Contains(text, tc.Query) || strings.Contains(text, tc.RelevantSource) {
						vectors[i] = []float32{1, 0}
					}
				}
				return vectors, nil
			})
			if s != nil {
				k.SetEmbeddingIdentityProvider(s.EmbeddingIdentity)
			}
			testOK(t, testDispatch(t, k, owner, "search.configureSemantic", recoveryRequest(k, map[string]any{"enabled": true, "modelPackId": pack.ID, "dimension": pack.Dimension})))
			locations := []any{}
			for _, page := range []string{source, relevant, irrelevant, private} {
				locations = append(locations, map[string]any{"pageId": page, "sourceRevision": "2"})
			}
			enqueueTestJob(t, k, owner, "GENERATE_EMBEDDINGS", map[string]any{"kind": "embedding", "locations": locations, "modelPackId": pack.ID, "dimension": pack.Dimension})
			job, err := k.ClaimJob(ctx, []string{"GENERATE_EMBEDDINGS"})
			if err != nil {
				t.Fatal(err)
			}
			if err = registeredJobs["GENERATE_EMBEDDINGS"](ctx, k, job); err != nil {
				t.Fatal(err)
			}
			if err = k.FinishJob(ctx, string(job.JobId), string(*job.LeaseToken), nil, false); err != nil {
				t.Fatal(err)
			}
			identity, err := k.embeddingIdentity(ctx, pack.ID)
			if err != nil {
				t.Fatal(err)
			}
			audit := beginAICanonicalObservation(t, k)
			checks := map[string]bool{}
			report := map[string]any{"fixture": tc.ID, "family": "related", "corpusSHA256": hash, "modelPackId": pack.ID, "embeddingIdentity": identity, "engineMode": "deterministic", "releaseQualified": false, "assertions": checks, "sourceBindings": map[string]string{"current": source, "relevant": relevant, "irrelevant": irrelevant, "restricted": private}}
			if s != nil {
				report["engineMode"] = "native"
			}
			defer func() { report["passed"] = !t.Failed(); t.Log(string(marshal(report))) }()
			check := func(name string, ok bool) {
				checks[name] = ok
				if !ok {
					t.Errorf("related invariant failed: %s", name)
				}
			}
			raw, err := aiSuggestions(ctx, k, scoped, map[string]any{"pageId": source, "semantic": true})
			if err != nil {
				t.Fatal(err)
			}
			result := raw.(map[string]any)
			semantic := result["semanticBacklinks"].(SearchResults)
			check("semanticDisclosed", semantic.Mode == "semantic" && semantic.Warning == "")
			seen := map[string]bool{}
			rank, other := 0, 0
			scopedHits, currentRevisions := true, true
			for i, hit := range semantic.Hits {
				page := string(hit.Location.PageId)
				if page == relevant {
					rank = i + 1
				}
				if page == irrelevant {
					other = i + 1
				}
				scopedHits = scopedHits && page != source && page != private && !seen[page]
				currentRevisions = currentRevisions && hit.Location.SourceRevision == "2" && hit.Excerpt != ""
				seen[page] = true
			}
			check("relevantRetrieved", rank > 0)
			check("relevanceAboveDistractor", rank > 0 && (other == 0 || rank < other))
			check("scopeAndSelfFiltered", scopedHits)
			check("currentRevisions", currentRevisions)
			keyword, err := configuredRelatedSearch(ctx, k, scoped, tc.Query, source, 12)
			if err != nil {
				t.Fatal(err)
			}
			report["semanticResults"], report["selectedQueryResults"], report["embeddingCalls"] = semantic, keyword, calls
			inv := audit.finish(t, k)
			report["canonicalInventory"], report["canonicalUnchanged"], report["mutationHistoryCountUnchanged"] = inv, inv.Unchanged, inv.MutationHistoryCountUnchanged
			check("canonicalPreserved", inv.Unchanged)
			// A settings change cannot silently reuse those real vectors as a semantic
			// answer. This explicit owner change is outside the no-write observation.
			testOK(t, testDispatch(t, k, owner, "search.configureSemantic", recoveryRequest(k, map[string]any{"enabled": false})))
			before := calls
			fallback, err := configuredRelatedSearch(ctx, k, scoped, tc.Query, source, 12)
			if err != nil {
				t.Fatal(err)
			}
			check("keywordFallbackDisclosed", fallback.Mode == "keyword" && fallback.Warning != "" && calls == before)
			report["keywordFallback"] = fallback
		})
	}
}
