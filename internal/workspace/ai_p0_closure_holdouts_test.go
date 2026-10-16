package workspace

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"trove.local/phase0/contracts"
	"trove.local/phase0/internal/native"
)

// These expectations are declared independently of production context roles,
// duplicate detection and citation rendering. They never rewrite model output.
type p0ClosureCase struct {
	expandedQACase
	ExpectedSupports []string `json:"expectedSupports"`
	MemoryTitles     []string `json:"memoryTitles"`
}

func p0ClosureCorpus(t *testing.T) ([]p0ClosureCase, string) {
	t.Helper()
	data, err := os.ReadFile("testdata/expanded-evaluation/p0-closure-holdouts.json")
	if err != nil {
		t.Fatal(err)
	}
	var corpus struct {
		SchemaVersion int             `json:"schemaVersion"`
		Cases         []p0ClosureCase `json:"cases"`
	}
	if err = json.Unmarshal(data, &corpus); err != nil {
		t.Fatal(err)
	}
	if corpus.SchemaVersion != 1 || len(corpus.Cases) != 12 {
		t.Fatal("unexpected locked P0 holdout inventory")
	}
	sum := sha256.Sum256(data)
	return corpus.Cases, hex.EncodeToString(sum[:])
}

func p0ClosureSupport(tc p0ClosureCase, i int) (string, error) {
	if len(tc.ExpectedSupports) == 0 {
		return tc.Source, nil
	}
	if len(tc.ExpectedSupports) != len(tc.Facts) {
		return "", fmt.Errorf("support inventory differs from expected claims")
	}
	label := tc.ExpectedSupports[i]
	if label == "source" {
		return tc.Source, nil
	}
	if !strings.HasPrefix(label, "memory:") {
		return "", fmt.Errorf("unknown independent support: %s", label)
	}
	n, err := strconv.Atoi(strings.TrimPrefix(label, "memory:"))
	if err != nil || n < 0 || n >= len(tc.Memories) {
		return "", fmt.Errorf("unknown memory support: %s", label)
	}
	return tc.Memories[n].Content, nil
}

func p0ClosureOracle(tc p0ClosureCase, raw string) error {
	var draft expandedDraft
	if err := json.Unmarshal([]byte(raw), &draft); err != nil {
		return err
	}
	if draft.Title != tc.Heading {
		return fmt.Errorf("heading %q, want %q", draft.Title, tc.Heading)
	}
	if tc.Abstain {
		if len(draft.Claims) != 1 || draft.Claims[0].Text != "The supplied evidence does not answer this question." || draft.Claims[0].Quote != "" {
			return fmt.Errorf("missing exclusive evidence-absent abstention")
		}
		return nil
	}
	if len(draft.Claims) != len(tc.Facts) {
		return fmt.Errorf("claim count %d, want %d", len(draft.Claims), len(tc.Facts))
	}
	for i, fact := range tc.Facts {
		claim := draft.Claims[i]
		if !regexp.MustCompile(fact.ClaimPattern).MatchString(claim.Text) {
			return fmt.Errorf("claim %d fails independent field/value rubric: %q", i, claim.Text)
		}
		support, err := p0ClosureSupport(tc, i)
		if err != nil {
			return err
		}
		if claim.Quote == "" || !strings.Contains(support, claim.Quote) || !regexp.MustCompile(fact.QuotePattern).MatchString(claim.Quote) {
			return fmt.Errorf("claim %d lacks its independently selected exact supporting quote", i)
		}
	}
	return nil
}

func seedP0Closure(t *testing.T, k *Kernel, e contracts.ServiceExecutionContext, tc p0ClosureCase) string {
	t.Helper()
	page := seedExpandedQA(t, k, e, tc.expandedQACase)
	if len(tc.MemoryTitles) != 0 && len(tc.MemoryTitles) != len(tc.Memories) {
		t.Fatal("memory title inventory mismatch")
	}
	for i, title := range tc.MemoryTitles {
		var memoryPage string
		if err := k.db.QueryRow("SELECT page_id FROM memory_entries WHERE content=?", tc.Memories[i].Content).Scan(&memoryPage); err != nil {
			t.Fatal(err)
		}
		testOK(t, dbBatch(t, k, e, []any{map[string]any{"kind": "page.update", "pageId": memoryPage, "title": title}}, "page", memoryPage))
	}
	return page
}

func TestExpandedP0ClosureIndependentOracle(t *testing.T) {
	cases, _ := p0ClosureCorpus(t)
	seen := map[string]bool{}
	for _, tc := range cases {
		t.Run(tc.ID, func(t *testing.T) {
			if tc.ID == "" || seen[tc.ID] || tc.Source == "" || tc.Prompt == "" || tc.UseMemory != (len(tc.Memories) > 0) {
				t.Fatal("invalid holdout")
			}
			seen[tc.ID] = true
			for _, fact := range tc.Facts {
				if _, err := regexp.Compile(fact.ClaimPattern); err != nil {
					t.Fatal(err)
				}
				if _, err := regexp.Compile(fact.QuotePattern); err != nil {
					t.Fatal(err)
				}
			}
			golden := expandedGolden(tc.expandedQACase)
			if err := p0ClosureOracle(tc, golden); err != nil {
				t.Fatal(err)
			}
			for _, poison := range []string{"invented", "quote", "extra", "missing", "heading"} {
				var d expandedDraft
				if err := json.Unmarshal([]byte(golden), &d); err != nil {
					t.Fatal(err)
				}
				switch poison {
				case "invented":
					d.Claims[0].Text = "A fabricated person owns the project."
				case "quote":
					d.Claims[0].Quote = "A fabricated supporting quotation."
				case "extra":
					d.Claims = append(d.Claims, d.Claims[0])
				case "missing":
					d.Claims = nil
				case "heading":
					d.Title = "Invented heading"
				}
				b, _ := json.Marshal(d)
				if p0ClosureOracle(tc, string(b)) == nil {
					t.Fatalf("oracle accepts %s", poison)
				}
			}
		})
	}
}

func TestExpandedP0ClosureCanonicalSeedAndPublication(t *testing.T) {
	cases, _ := p0ClosureCorpus(t)
	for _, tc := range cases {
		t.Run(tc.ID, func(t *testing.T) {
			k, e := testKernel(t)
			page := seedP0Closure(t, k, e, tc)
			canonicalAudit := beginAICanonicalObservation(t, k)
			p, err := k.PrepareAIWithBudget(context.Background(), e, AIRequest{PageID: page, Mode: "answer", Prompt: tc.Prompt, UseMemory: tc.UseMemory, ModelPackID: NewID()}, wordBudget())
			if err != nil {
				t.Fatal(err)
			}
			var before, after int
			if err := k.db.QueryRow("SELECT count(*) FROM mutation_history").Scan(&before); err != nil {
				t.Fatal(err)
			}
			c, err := GeneratePreparedAI(context.Background(), p, func(context.Context, string, string, int, string, func(string)) (string, error) {
				return expandedGolden(tc.expandedQACase), nil
			}, nil)
			if err != nil {
				t.Fatal(err)
			}
			r, err := k.FinishAI(context.Background(), e, p.Run.ID, c.Text, "completed", "")
			if err != nil || r.State != "completed" {
				t.Fatalf("publication: %v %s", err, r.State)
			}
			if err := k.db.QueryRow("SELECT count(*) FROM mutation_history").Scan(&after); err != nil {
				t.Fatal(err)
			}
			canonicalAudit.finish(t, k)
			if before != after || (!tc.Abstain && len(r.Citations) == 0) {
				t.Fatal("canonical preservation or attribution failed")
			}
		})
	}
}

func TestRealLocalP0ClosureHoldouts(t *testing.T) {
	if os.Getenv("TROVE_NATIVE_INTEGRATION") != "1" {
		t.Skip("requires explicit verified native bundle and installed model")
	}
	pack := os.Getenv("TROVE_CHAT_EVAL_PACK")
	if pack == "" {
		t.Fatal("select the model explicitly; no fallback pack")
	}
	cases, corpusHash := p0ClosureCorpus(t)
	manager, err := native.NewManager(os.Getenv("TROVE_MODEL_ROOT"))
	if err != nil {
		t.Fatal(err)
	}
	supervisor, err := native.NewSupervisor(os.Getenv("TROVE_NATIVE_BUNDLE"), manager)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range cases {
		t.Run(tc.ID, func(t *testing.T) {
			k, e := testKernel(t)
			page := seedP0Closure(t, k, e, tc)
			canonicalAudit := beginAICanonicalObservation(t, k)
			var before, after int
			if err := k.db.QueryRow("SELECT count(*) FROM mutation_history").Scan(&before); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
			defer cancel()
			started := time.Now()
			budget := AIBudget{ContextTokens: 8192, OutputTokens: 512, CountTokens: func(ctx context.Context, prompts []AIPrompt) ([]int, error) {
				texts := make([]string, len(prompts))
				for i, p := range prompts {
					texts[i] = native.ChatPrompt(p.System, p.User)
				}
				return supervisor.CountTokens(ctx, pack, texts)
			}}
			request := AIRequest{PageID: page, Mode: "answer", Prompt: tc.Prompt, UseMemory: tc.UseMemory, ModelPackID: pack}
			p, prepareErr := k.PrepareAIWithBudget(ctx, e, request, budget)
			raw, generatedSystem, generatedPrompt, generatedGrammar := "", "", "", ""
			generatedCap := 0
			nativeCompleted := false
			var nativeCallbackErr error
			attempts := 0
			metrics := &native.RunMetrics{}
			var generationErr, finishErr, oracleErr error
			var run AIRun
			if prepareErr == nil {
				completion, err := GeneratePreparedAI(ctx, p, func(ctx context.Context, system, prompt string, tokens int, grammar string, stream func(string)) (string, error) {
					attempts++
					if attempts != 1 {
						return "", fmt.Errorf("holdout requires a single generation attempt")
					}
					generatedSystem, generatedPrompt, generatedGrammar, generatedCap = system, prompt, grammar, tokens
					out, err := supervisor.GenerateTextWithGrammarMetrics(ctx, pack, native.ChatPrompt(system, prompt), tokens, grammar, metrics, stream)
					raw, nativeCallbackErr, nativeCompleted = out, err, err == nil
					return out, err
				}, nil)
				generationErr = err
				state, failure := "completed", ""
				if err != nil {
					state, failure = "failed", err.Error()
				}
				run, finishErr = k.FinishAI(context.Background(), e, p.Run.ID, completion.Text, state, failure)
				oracleErr = p0ClosureOracle(tc, raw)
			}
			if err := k.db.QueryRow("SELECT count(*) FROM mutation_history").Scan(&after); err != nil {
				t.Fatal(err)
			}
			latencyMS := time.Since(started).Milliseconds() // exclude canonical evidence capture/fsync
			canonicalInventory := canonicalAudit.finish(t, k)
			passed := prepareErr == nil && generationErr == nil && finishErr == nil && oracleErr == nil && attempts == 1 && run.State == "completed" && before == after && canonicalInventory.Unchanged
			if !tc.Abstain && len(run.Citations) == 0 {
				passed = false
			}
			var rss any
			if n := metrics.PeakRSSBytes.Load(); n > 0 {
				rss = n
			}
			report, _ := json.Marshal(aiEvaluationRequestReport(request, attempts, map[string]any{"fixture": tc.ID, "corpusSHA256": corpusHash, "passed": passed, "canonicalUnchanged": before == after, "mutationHistoryCountUnchanged": before == after, "canonicalInventory": canonicalInventory, "latencyMs": latencyMS, "promptTokens": p.PromptTokens, "outputCap": p.OutputTokens, "warning": p.Run.Warning, "peakWorkerRSSBytes": rss, "prepareError": fmt.Sprint(prepareErr), "generationError": fmt.Sprint(generationErr), "finishError": fmt.Sprint(finishErr), "rubricError": fmt.Sprint(oracleErr), "rawDraft": raw, "sources": p.Run.Sources, "preparedSystem": p.System, "preparedPrompt": p.Prompt, "generatorSystem": generatedSystem, "generatorPrompt": generatedPrompt, "generatorGrammar": generatedGrammar, "generatorGrammarSHA256": fmt.Sprintf("%x", sha256.Sum256([]byte(generatedGrammar))), "generatorOutputCap": generatedCap, "nativeCallbackCompleted": nativeCompleted, "nativeCallbackError": fmt.Sprint(nativeCallbackErr), "nativeCompletionContract": "GenerateTextWithGrammarMetrics: success validates generation kind, nonempty text and eos", "citations": run.Citations, "displayed": run.Text, "releaseQualified": false}))
			t.Log(string(report))
			if !passed {
				t.Error("P0 closure holdout or canonical-write boundary failed")
			}
		})
	}
}
