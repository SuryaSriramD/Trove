package workspace

import (
	"context"
	"strings"
	"testing"

	"trove.local/phase0/contracts"
)

func aiSemanticFixture(t *testing.T) (*Kernel, contracts.ServiceExecutionContext, string, string, string) {
	t.Helper()
	k, e := testKernel(t)
	page := createPage(t, k, e, "Canine care requires daily exercise")
	private := createPage(t, k, e, "Private veterinary observations")
	model := NewID()
	k.SetEmbeddingProvider(func(_ context.Context, got string, texts []string, _ []contracts.DomainLocation) ([][]float32, error) {
		if got != model {
			t.Errorf("retrieval used model %s instead of configured embedding model %s", got, model)
		}
		vectors := make([][]float32, len(texts))
		for i := range vectors {
			vectors[i] = []float32{1, 0}
		}
		return vectors, nil
	})
	t.Cleanup(func() { k.SetEmbeddingProvider(nil) })
	testOK(t, testDispatch(t, k, e, "search.configureSemantic", recoveryRequest(k, map[string]any{"enabled": true, "modelPackId": model, "dimension": 2})))
	enqueueTestJob(t, k, e, "GENERATE_EMBEDDINGS", map[string]any{"kind": "embedding", "locations": []any{map[string]any{"pageId": page, "sourceRevision": "1"}, map[string]any{"pageId": private, "sourceRevision": "1"}}, "modelPackId": model, "dimension": 2})
	j, err := k.ClaimJob(context.Background(), []string{"GENERATE_EMBEDDINGS"})
	if err != nil {
		t.Fatal(err)
	}
	if err = registeredJobs["GENERATE_EMBEDDINGS"](context.Background(), k, j); err != nil {
		t.Fatal(err)
	}
	return k, e, page, private, model
}

func TestAIWorkspaceHybridRetrievesConceptualSourcesWithinScope(t *testing.T) {
	k, owner, page, private, _ := aiSemanticFixture(t)
	e := k.Grant(owner.Actor, "user", contracts.DomainScope{Value: contracts.DomainScopeVariant3{Kind: "pages", WorkspaceId: contracts.DomainId(k.ID()), PageIds: []contracts.DomainId{contracts.DomainId(page)}}})
	defer k.RevokeGrant(string(e.ScopeGrantId))
	p, err := k.PrepareAI(context.Background(), e, AIRequest{Mode: "answer", Prompt: "How should I keep my dog healthy?", ModelPackID: NewID()})
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Run.Sources) == 0 || !strings.Contains(p.Prompt, "daily exercise") {
		t.Fatal("conceptual source missing", p.Run)
	}
	for _, s := range p.Run.Sources {
		if string(s.Location.PageId) != page || string(s.Location.PageId) == private || s.Location.SourceRevision != "1" {
			t.Fatal("unscoped or stale evidence", s)
		}
	}
	if p.Run.Warning != "" {
		t.Fatal(p.Run.Warning)
	}
	finished, err := k.FinishAI(context.Background(), e, p.Run.ID, "Daily exercise is needed [1].", "completed", "")
	if err != nil || len(finished.Citations) != 1 || string(finished.Citations[0].Location.PageId) != page {
		t.Fatal(finished, err)
	}
}

func TestAIWorkspaceHybridReportsKeywordFallback(t *testing.T) {
	k, e, _, _, _ := aiSemanticFixture(t)
	k.SetEmbeddingProvider(nil)
	p, err := k.PrepareAI(context.Background(), e, AIRequest{Mode: "answer", Prompt: "Canine care", ModelPackID: NewID()})
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Run.Sources) == 0 || !strings.Contains(p.Run.Warning, "keyword") {
		t.Fatal("fallback was not disclosed", p.Run)
	}
	saved, err := k.AIRun(context.Background(), e, p.Run.ID)
	if err != nil || saved.Warning != p.Run.Warning {
		t.Fatal("retrieval warning was not persisted", saved, err)
	}
}

func TestAIWorkspaceDisabledSemanticDoesNotUseRetainedVectors(t *testing.T) {
	k, e, _, _, _ := aiSemanticFixture(t)
	testOK(t, testDispatch(t, k, e, "search.configureSemantic", recoveryRequest(k, map[string]any{"enabled": false})))
	k.SetEmbeddingProvider(func(context.Context, string, []string, []contracts.DomainLocation) ([][]float32, error) {
		t.Error("disabled embedding model was invoked")
		return [][]float32{{1, 0}}, nil
	})
	p, err := k.PrepareAI(context.Background(), e, AIRequest{Mode: "answer", Prompt: "Canine care", ModelPackID: NewID()})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(p.Run.Warning, "keyword") {
		t.Fatal("disabled semantic fallback was not disclosed", p.Run)
	}
}

func TestAIWorkspaceHybridRejectsChangedEvidenceAtCompletion(t *testing.T) {
	k, e, page, _, _ := aiSemanticFixture(t)
	p, err := k.PrepareAI(context.Background(), e, AIRequest{Mode: "answer", Prompt: "dog wellbeing", ModelPackID: NewID()})
	if err != nil {
		t.Fatal(err)
	}
	testOK(t, dbBatch(t, k, e, []any{map[string]any{"kind": "page.update", "pageId": page, "title": "Superseded advice"}}, "page", page))
	finished, err := k.FinishAI(context.Background(), e, p.Run.ID, "Daily exercise [1].", "completed", "")
	if err != nil || finished.State != "failed" || finished.Text != "" {
		t.Fatal("changed evidence was published", finished, err)
	}
}
