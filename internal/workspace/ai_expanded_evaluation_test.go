package workspace

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"

	"trove.local/phase0/contracts"
	"trove.local/phase0/internal/native"
)

// This corpus is synthetic and independently frozen before execution. Its strict
// answer grammar evaluates these bounded tasks, not arbitrary semantic entailment.
type expandedFact struct {
	ClaimPattern string `json:"claimPattern"`
	QuotePattern string `json:"quotePattern"`
	ValidClaim   string `json:"validClaim"`
	ValidQuote   string `json:"validQuote"`
}
type expandedMemory struct {
	Kind    string `json:"kind"`
	Content string `json:"content"`
}
type expandedQACase struct {
	ID        string           `json:"id"`
	Family    string           `json:"family"`
	Title     string           `json:"title"`
	Source    string           `json:"source"`
	Prompt    string           `json:"prompt"`
	Heading   string           `json:"heading"`
	UseMemory bool             `json:"useMemory"`
	Memories  []expandedMemory `json:"memories"`
	Facts     []expandedFact   `json:"facts"`
	Abstain   bool             `json:"abstain"`
}
type expandedDraft struct {
	Title  string `json:"title"`
	Claims []struct {
		Text  string `json:"text"`
		Quote string `json:"quote"`
	} `json:"claims"`
}

func expandedCorpus(t *testing.T) ([]expandedQACase, string) {
	t.Helper()
	data, err := os.ReadFile("testdata/expanded-evaluation/p0-qa-memory.json")
	if err != nil {
		t.Fatal(err)
	}
	var corpus struct {
		SchemaVersion int              `json:"schemaVersion"`
		Cases         []expandedQACase `json:"cases"`
	}
	if err = json.Unmarshal(data, &corpus); err != nil {
		t.Fatal(err)
	}
	if corpus.SchemaVersion != 1 || len(corpus.Cases) != 24 {
		t.Fatal("unexpected locked P0 corpus shape")
	}
	sum := sha256.Sum256(data)
	return corpus.Cases, hex.EncodeToString(sum[:])
}
func expandedOracle(tc expandedQACase, raw string) error {
	var draft expandedDraft
	if err := json.Unmarshal([]byte(raw), &draft); err != nil {
		return err
	}
	if draft.Title != tc.Heading {
		return fmt.Errorf("heading %q, want %q", draft.Title, tc.Heading)
	}
	if tc.Abstain {
		if len(draft.Claims) != 1 || draft.Claims[0].Text != "The supplied evidence does not answer this question." || draft.Claims[0].Quote != "" {
			return fmt.Errorf("missing exact evidence-absent abstention")
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
		if claim.Quote == "" || !strings.Contains(tc.Source, claim.Quote) || !regexp.MustCompile(fact.QuotePattern).MatchString(claim.Quote) {
			return fmt.Errorf("claim %d lacks its exact current-source supporting quote", i)
		}
	}
	return nil
}
func expandedGolden(tc expandedQACase) string {
	d := expandedDraft{Title: tc.Heading}
	if tc.Abstain {
		d.Claims = append(d.Claims, struct {
			Text  string `json:"text"`
			Quote string `json:"quote"`
		}{"The supplied evidence does not answer this question.", ""})
	}
	for _, f := range tc.Facts {
		d.Claims = append(d.Claims, struct {
			Text  string `json:"text"`
			Quote string `json:"quote"`
		}{f.ValidClaim, f.ValidQuote})
	}
	out, _ := json.Marshal(d)
	return string(out)
}
func TestExpandedQACorpusAndIndependentOracle(t *testing.T) {
	cases, _ := expandedCorpus(t)
	seen := map[string]bool{}
	for _, tc := range cases {
		t.Run(tc.ID, func(t *testing.T) {
			if seen[tc.ID] || tc.ID == "" || tc.Source == "" || tc.Prompt == "" || tc.UseMemory != (len(tc.Memories) > 0) {
				t.Fatal("invalid independent fixture")
			}
			seen[tc.ID] = true
			for _, f := range tc.Facts {
				if _, err := regexp.Compile(f.ClaimPattern); err != nil {
					t.Fatal(err)
				}
				if _, err := regexp.Compile(f.QuotePattern); err != nil {
					t.Fatal(err)
				}
			}
			golden := expandedGolden(tc)
			if err := expandedOracle(tc, golden); err != nil {
				t.Fatal("golden oracle", err)
			}
			var d expandedDraft
			json.Unmarshal([]byte(golden), &d)
			encode := func() string { b, _ := json.Marshal(d); return string(b) }
			d.Claims[0].Text = "The current answer is invented."
			if expandedOracle(tc, encode()) == nil {
				t.Fatal("oracle accepts an invented answer")
			}
			json.Unmarshal([]byte(golden), &d)
			d.Claims[0].Quote = "Fabricated source wording."
			if expandedOracle(tc, encode()) == nil {
				t.Fatal("oracle accepts invented support")
			}
			json.Unmarshal([]byte(golden), &d)
			d.Claims = append(d.Claims, d.Claims[0])
			if expandedOracle(tc, encode()) == nil {
				t.Fatal("oracle accepts an extra claim")
			}
			json.Unmarshal([]byte(golden), &d)
			d.Claims = d.Claims[:0]
			if expandedOracle(tc, encode()) == nil {
				t.Fatal("oracle accepts missing facts")
			}
			if !tc.Abstain && len(tc.Facts) > 1 {
				json.Unmarshal([]byte(golden), &d)
				d.Claims[0].Quote = d.Claims[1].Quote
				if expandedOracle(tc, encode()) == nil {
					t.Fatal("oracle accepts another fact's quote")
				}
			}
		})
	}
}
func seedExpandedQA(t *testing.T, k *Kernel, e contracts.ServiceExecutionContext, tc expandedQACase) string {
	t.Helper()
	paragraph := func(title, body string) string {
		page := createPage(t, k, e, title)
		block := NewID()
		testOK(t, dbBatch(t, k, e, []any{map[string]any{"kind": "block.insert", "block": map[string]any{"blockId": block, "pageId": page, "type": "paragraph", "schemaVersion": 1, "data": map[string]any{"richText": []any{map[string]any{"text": body}}}}}}, "page", page, "block", block))
		return page
	}
	page := paragraph(tc.Title, tc.Source)
	for _, m := range tc.Memories {
		source := paragraph("Previously reviewed context", m.Content)
		var sourceRevision string
		if err := k.db.QueryRow("SELECT revision FROM pages WHERE id=?", source).Scan(&sourceRevision); err != nil {
			t.Fatal(err)
		}
		memoryPage := createPage(t, k, e, "Approved "+m.Kind)
		id := NewID()
		testOK(t, dbBatch(t, k, e, []any{map[string]any{"kind": "memory.propose", "memoryId": id, "pageId": memoryPage, "scope": map[string]any{"kind": "workspace", "workspaceId": k.ID()}, "memoryKind": m.Kind, "content": m.Content, "sources": []any{map[string]any{"pageId": source, "sourceRevision": sourceRevision}}}}, "page", memoryPage, "memory", id))
		testOK(t, dbBatch(t, k, e, []any{map[string]any{"kind": "memory.approve", "memoryId": id}}, "page", memoryPage, "memory", id))
	}
	return page
}
func TestExpandedQACanonicalSeedAndPublication(t *testing.T) {
	cases, _ := expandedCorpus(t)
	for _, tc := range cases {
		t.Run(tc.ID, func(t *testing.T) {
			k, e := testKernel(t)
			page := seedExpandedQA(t, k, e, tc)
			p, err := k.PrepareAIWithBudget(context.Background(), e, AIRequest{PageID: page, Mode: "answer", Prompt: tc.Prompt, UseMemory: tc.UseMemory, ModelPackID: NewID()}, wordBudget())
			if err != nil {
				t.Fatal(err)
			}
			var before int
			if err = k.db.QueryRow("SELECT count(*) FROM mutation_history").Scan(&before); err != nil {
				t.Fatal(err)
			}
			c, err := GeneratePreparedAI(context.Background(), p, func(context.Context, string, string, int, string, func(string)) (string, error) {
				return expandedGolden(tc), nil
			}, nil)
			if err != nil {
				t.Fatal(err)
			}
			r, err := k.FinishAI(context.Background(), e, p.Run.ID, c.Text, "completed", "")
			if err != nil || r.State != "completed" {
				t.Fatalf("publish: %v %s", err, r.State)
			}
			var after int
			if err = k.db.QueryRow("SELECT count(*) FROM mutation_history").Scan(&after); err != nil {
				t.Fatal(err)
			}
			if before != after {
				t.Fatal("draft modified canonical content")
			}
			if !tc.Abstain && len(r.Citations) == 0 {
				t.Fatal("supported answer lost canonical citations")
			}
		})
	}
}
func TestRealLocalExpandedP0QAMemory(t *testing.T) {
	if os.Getenv("TROVE_NATIVE_INTEGRATION") != "1" {
		t.Skip("requires explicit verified native bundle and installed model")
	}
	pack := os.Getenv("TROVE_CHAT_EVAL_PACK")
	if pack == "" {
		t.Fatal("select the model explicitly; no fallback pack")
	}
	cases, corpusHash := expandedCorpus(t)
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
			page := seedExpandedQA(t, k, e, tc)
			var before int
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
			p, prepareErr := k.PrepareAIWithBudget(ctx, e, AIRequest{PageID: page, Mode: "answer", Prompt: tc.Prompt, UseMemory: tc.UseMemory, ModelPackID: pack}, budget)
			raw := ""
			metrics := &native.RunMetrics{}
			var generationErr, finishErr, oracleErr error
			var run AIRun
			if prepareErr == nil {
				completion, err := GeneratePreparedAI(ctx, p, func(ctx context.Context, system, prompt string, tokens int, grammar string, stream func(string)) (string, error) {
					out, err := supervisor.GenerateTextWithGrammarMetrics(ctx, pack, native.ChatPrompt(system, prompt), tokens, grammar, metrics, stream)
					raw = out
					return out, err
				}, nil)
				generationErr = err
				state, failure := "completed", ""
				if err != nil {
					state, failure = "failed", err.Error()
				}
				run, finishErr = k.FinishAI(context.Background(), e, p.Run.ID, completion.Text, state, failure)
				oracleErr = expandedOracle(tc, raw)
			}
			var after int
			if err := k.db.QueryRow("SELECT count(*) FROM mutation_history").Scan(&after); err != nil {
				t.Fatal(err)
			}
			passed := prepareErr == nil && generationErr == nil && finishErr == nil && oracleErr == nil && run.State == "completed" && before == after
			if !tc.Abstain && len(run.Citations) == 0 {
				passed = false
			}
			var rss any
			if n := metrics.PeakRSSBytes.Load(); n > 0 {
				rss = n
			}
			report, _ := json.Marshal(map[string]any{"fixture": tc.ID, "corpusSHA256": corpusHash, "modelPackId": pack, "passed": passed, "canonicalUnchanged": before == after, "latencyMs": time.Since(started).Milliseconds(), "promptTokens": p.PromptTokens, "outputCap": p.OutputTokens, "warning": p.Run.Warning, "peakWorkerRSSBytes": rss, "prepareError": fmt.Sprint(prepareErr), "generationError": fmt.Sprint(generationErr), "finishError": fmt.Sprint(finishErr), "rubricError": fmt.Sprint(oracleErr), "rawDraft": raw, "displayed": run.Text, "releaseQualified": false})
			t.Log(string(report))
			if !passed {
				t.Error("expanded P0 task or canonical-write boundary failed")
			}
		})
	}
}
