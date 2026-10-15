package workspace

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"

	"trove.local/phase0/contracts"
	"trove.local/phase0/internal/native"
)

// Independently authored transfer inputs: these do not replace any earlier
// fixture, rubric, or manual outcome. The hash is locked before prompt repair.
const aiTransferCorpusSHA256 = "1eb8cb07b0047fde8e4f349be53b17613c33200d6fa44bfdbfe8d4d488f886bd"

type aiTransferFact struct {
	expandedFact
	Support string `json:"support"`
}
type aiTransferCase struct {
	ID           string           `json:"id"`
	Mode         string           `json:"mode"`
	Title        string           `json:"title"`
	Source       string           `json:"source"`
	Prompt       string           `json:"prompt"`
	Selection    string           `json:"selection"`
	UseMemory    bool             `json:"useMemory"`
	Memories     []expandedMemory `json:"memories"`
	MemoryTitles []string         `json:"memoryTitles"`
	OutsidePages []struct {
		Title  string `json:"title"`
		Source string `json:"source"`
	} `json:"outsidePages"`
	Expected struct {
		Required                     []string `json:"required"`
		Forbidden                    []string `json:"forbidden"`
		AcceptableExample            string   `json:"acceptableExample"`
		RejectedExample              string   `json:"rejectedExample"`
		ManualSemanticReviewRequired bool     `json:"manualSemanticReviewRequired"`
		AllClausesMustPass           bool     `json:"allClausesMustPass"`
	} `json:"expected"`
	AnswerOracle *struct {
		Heading string           `json:"heading"`
		Abstain bool             `json:"abstain"`
		Facts   []aiTransferFact `json:"facts"`
	} `json:"answerOracle,omitempty"`
}

func aiTransferCorpus(t *testing.T) ([]aiTransferCase, string) {
	t.Helper()
	data, err := os.ReadFile("testdata/expanded-evaluation/transfer-20261009.json")
	if err != nil {
		t.Fatal(err)
	}
	hash := fmt.Sprintf("%x", sha256.Sum256(data))
	if hash != aiTransferCorpusSHA256 {
		t.Fatal("locked transfer corpus bytes changed")
	}
	var c struct {
		SchemaVersion int              `json:"schemaVersion"`
		Cases         []aiTransferCase `json:"cases"`
	}
	if err := json.Unmarshal(data, &c); err != nil || c.SchemaVersion != 1 || len(c.Cases) != 12 {
		t.Fatal("invalid locked transfer inventory", err)
	}
	return c.Cases, hash
}

// Only answer-mode cases have a bounded automated claim grammar. For all other
// modes, collection success never asserts the independent manual prose oracle.
func aiTransferAnswerOracle(tc aiTransferCase, raw string) error {
	if tc.AnswerOracle == nil {
		return nil
	}
	var d expandedDraft
	dec := json.NewDecoder(strings.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&d); err != nil {
		return err
	}
	if err := dec.Decode(new(any)); err != io.EOF {
		return fmt.Errorf("additional JSON value")
	}
	o := tc.AnswerOracle
	if d.Title != o.Heading {
		return fmt.Errorf("unexpected output heading")
	}
	if o.Abstain {
		if len(d.Claims) != 1 || d.Claims[0].Text != "The supplied evidence does not answer this question." || d.Claims[0].Quote != "" {
			return fmt.Errorf("missing exclusive evidence-absent abstention")
		}
		return nil
	}
	if len(d.Claims) != len(o.Facts) {
		return fmt.Errorf("wrong claim count")
	}
	for i, f := range o.Facts {
		support := tc.Source
		if f.Support != "source" {
			if f.Support != "memory:0" || len(tc.Memories) != 1 {
				return fmt.Errorf("unknown independently declared support")
			}
			support = tc.Memories[0].Content
		}
		claim := d.Claims[i]
		if !regexp.MustCompile(f.ClaimPattern).MatchString(claim.Text) || claim.Quote == "" || !strings.Contains(support, claim.Quote) || !regexp.MustCompile(f.QuotePattern).MatchString(claim.Quote) {
			return fmt.Errorf("claim or exact supporting quotation differs from independent expectation")
		}
	}
	return nil
}
func aiTransferGolden(tc aiTransferCase) string {
	if tc.AnswerOracle == nil {
		return tc.Expected.AcceptableExample
	}
	o := tc.AnswerOracle
	d := expandedDraft{Title: o.Heading}
	for _, f := range o.Facts {
		d.Claims = append(d.Claims, struct {
			Text  string `json:"text"`
			Quote string `json:"quote"`
		}{f.ValidClaim, f.ValidQuote})
	}
	if o.Abstain {
		d.Claims = append(d.Claims, struct {
			Text  string `json:"text"`
			Quote string `json:"quote"`
		}{"The supplied evidence does not answer this question.", ""})
	}
	b, _ := json.Marshal(d)
	return string(b)
}
func seedAITransfer(t *testing.T, k *Kernel, e contracts.ServiceExecutionContext, tc aiTransferCase) (string, string) {
	t.Helper()
	page := seedP0Closure(t, k, e, p0ClosureCase{expandedQACase: expandedQACase{Title: tc.Title, Source: tc.Source, Memories: tc.Memories}, MemoryTitles: tc.MemoryTitles})
	var block string
	if err := k.db.QueryRow("SELECT id FROM blocks WHERE page_id=?", page).Scan(&block); err != nil {
		t.Fatal(err)
	}
	for _, p := range tc.OutsidePages {
		seedExpandedQA(t, k, e, expandedQACase{Title: p.Title, Source: p.Source})
	}
	return page, block
}
func aiTransferSourceScope(tc aiTransferCase, page, block string, sources []AIEvidence) error {
	sourceFound := false
	memories := map[string]bool{}
	for _, s := range sources {
		if s.MemoryID != "" {
			matched := false
			for _, m := range tc.Memories {
				if tc.UseMemory && s.MemoryKind == m.Kind && s.Text == m.Content {
					matched = true
				}
			}
			if !matched || memories[s.MemoryID] {
				return fmt.Errorf("unexpected or duplicated approved memory source")
			}
			memories[s.MemoryID] = true
			continue
		}
		if string(s.Location.PageId) != page || s.Title != tc.Title {
			return fmt.Errorf("foreign page or display metadata source")
		}
		if s.Location.BlockId == nil {
			if s.Text != tc.Title {
				return fmt.Errorf("unexpected page title source body")
			}
		} else {
			if string(*s.Location.BlockId) != block || s.Text != tc.Source || sourceFound {
				return fmt.Errorf("unexpected or duplicate primary block source")
			}
			sourceFound = true
		}
	}
	if !sourceFound || len(memories) != len(tc.Memories) {
		return fmt.Errorf("missing exact primary source or approved memory")
	}
	return nil
}
func TestAITransferIndependentCorpusAndOracle(t *testing.T) {
	cases, _ := aiTransferCorpus(t)
	seen := map[string]bool{}
	for _, tc := range cases {
		t.Run(tc.ID, func(t *testing.T) {
			if tc.ID == "" || seen[tc.ID] || tc.Source == "" || tc.Prompt == "" || tc.UseMemory != (len(tc.Memories) > 0) || len(tc.MemoryTitles) != len(tc.Memories) || len(tc.Expected.Required) < 2 || len(tc.Expected.Forbidden) < 1 || !tc.Expected.AllClausesMustPass || !tc.Expected.ManualSemanticReviewRequired || tc.Expected.AcceptableExample == tc.Expected.RejectedExample {
				t.Fatal("incomplete independent fixture")
			}
			seen[tc.ID] = true
			if tc.Selection != "" && !strings.Contains(tc.Source, tc.Selection) {
				t.Fatal("selection not in source")
			}
			if (tc.Mode == "answer") != (tc.AnswerOracle != nil) {
				t.Fatal("answer grammar inventory mismatch")
			}
			if tc.AnswerOracle == nil {
				return
			}
			for _, f := range tc.AnswerOracle.Facts {
				if _, err := regexp.Compile(f.ClaimPattern); err != nil {
					t.Fatal(err)
				}
				if _, err := regexp.Compile(f.QuotePattern); err != nil {
					t.Fatal(err)
				}
			}
			golden := aiTransferGolden(tc)
			if err := aiTransferAnswerOracle(tc, golden); err != nil {
				t.Fatal(err)
			}
			for _, kind := range []string{"claim", "quote", "extra", "missing", "heading", "trailing"} {
				var d expandedDraft
				json.Unmarshal([]byte(golden), &d)
				switch kind {
				case "claim":
					d.Claims[0].Text = "An unsupported factual claim."
				case "quote":
					d.Claims[0].Quote = "Invented quotation."
				case "extra":
					d.Claims = append(d.Claims, d.Claims[0])
				case "missing":
					d.Claims = nil
				case "heading":
					d.Title = "Metadata heading"
				}
				b, _ := json.Marshal(d)
				poison := string(b)
				if kind == "trailing" {
					poison += "{}"
				}
				if aiTransferAnswerOracle(tc, poison) == nil {
					t.Fatal("oracle accepted poison", kind)
				}
			}
		})
	}
}
func TestAITransferCanonicalSeedAndScope(t *testing.T) {
	cases, _ := aiTransferCorpus(t)
	for _, tc := range cases {
		t.Run(tc.ID, func(t *testing.T) {
			k, e := testKernel(t)
			page, block := seedAITransfer(t, k, e, tc)
			audit := beginAICanonicalObservation(t, k)
			p, err := k.PrepareAIWithBudget(context.Background(), e, AIRequest{PageID: page, BlockID: block, Mode: tc.Mode, Prompt: tc.Prompt, Selection: tc.Selection, UseMemory: tc.UseMemory, ModelPackID: NewID()}, wordBudget())
			if err != nil {
				t.Fatal(err)
			}
			if err = aiTransferSourceScope(tc, page, block, p.Run.Sources); err != nil {
				t.Fatal(err)
			}
			poisoned := append([]AIEvidence{}, p.Run.Sources...)
			poisoned = append(poisoned, AIEvidence{Title: "Foreign", Text: "Unrelated", Location: contracts.DomainLocation{PageId: contracts.DomainId(NewID())}})
			if aiTransferSourceScope(tc, page, block, poisoned) == nil {
				t.Fatal("source scope accepts foreign evidence")
			}
			run, err := k.FinishAI(context.Background(), e, p.Run.ID, "", "cancelled", "")
			if err != nil || run.State != "cancelled" {
				t.Fatal(err, run.State)
			}
			if !audit.finish(t, k).Unchanged {
				t.Fatal("prepare/cancel changed protected canonical content")
			}
		})
	}
}
func TestRealLocalAITransferCases(t *testing.T) {
	runRealLocalAITransferCases(t, aiTransferCorpus)
}

// Keep the original transfer execution path identical for separately locked corpora.
func runRealLocalAITransferCases(t *testing.T, load func(*testing.T) ([]aiTransferCase, string)) {
	t.Helper()
	if os.Getenv("TROVE_NATIVE_INTEGRATION") != "1" {
		t.Skip("requires explicit native integration")
	}
	pack := os.Getenv("TROVE_CHAT_EVAL_PACK")
	if pack != "01990000-0000-7000-8000-000000000106" {
		t.Fatal("fixed pack106 required")
	}
	cases, hash := load(t)
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
			page, block := seedAITransfer(t, k, e, tc)
			audit := beginAICanonicalObservation(t, k)
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
			defer cancel()
			request := AIRequest{Mode: tc.Mode, Prompt: tc.Prompt, PageID: page, BlockID: block, Selection: tc.Selection, ModelPackID: pack, UseMemory: tc.UseMemory}
			budget := AIBudget{ContextTokens: 8192, OutputTokens: 512, CountTokens: func(ctx context.Context, prompts []AIPrompt) ([]int, error) {
				texts := make([]string, len(prompts))
				for i, p := range prompts {
					texts[i] = native.ChatPrompt(p.System, p.User)
				}
				return supervisor.CountTokens(ctx, pack, texts)
			}}
			started := time.Now()
			p, prepareErr := k.PrepareAIWithBudget(ctx, e, request, budget)
			raw, system, prompt, grammar, decoded := "", "", "", "", ""
			tokens, attempts := 0, 0
			var generationErr, finishErr, oracleErr, sourceErr error
			var run AIRun
			if prepareErr == nil {
				sourceErr = aiTransferSourceScope(tc, page, block, p.Run.Sources)
				if sourceErr == nil {
					c, err := GeneratePreparedAI(ctx, p, func(ctx context.Context, s, u string, n int, g string, stream func(string)) (string, error) {
						attempts++
						if attempts != 1 {
							return "", fmt.Errorf("single attempt required")
						}
						system, prompt, grammar, tokens = s, u, g, n
						out, err := supervisor.GenerateTextWithGrammarMetrics(ctx, pack, native.ChatPrompt(s, u), n, g, &native.RunMetrics{}, stream)
						raw = out
						return out, err
					}, nil)
					decoded, generationErr = c.Text, err
					state, failure := "completed", ""
					if err != nil {
						state, failure = "failed", err.Error()
					}
					run, finishErr = k.FinishAI(context.Background(), e, p.Run.ID, c.Text, state, failure)
					oracleErr = aiTransferAnswerOracle(tc, raw)
				}
			}
			latency := time.Since(started).Milliseconds()
			canonical := audit.finish(t, k)
			executionPassed := prepareErr == nil && sourceErr == nil && generationErr == nil && finishErr == nil && attempts == 1 && run.State == "completed" && canonical.Unchanged
			automatedPassed := executionPassed && oracleErr == nil
			report, _ := json.Marshal(map[string]any{"fixture": tc.ID, "mode": request.Mode, "useMemory": request.UseMemory, "corpusSHA256": hash, "request": request, "sourceTitle": tc.Title, "source": tc.Source, "selection": tc.Selection, "prompt": tc.Prompt, "modelPackId": pack, "contextTokens": 8192, "outputCap": p.OutputTokens, "promptTokens": p.PromptTokens, "generatorSystem": system, "generatorPrompt": prompt, "generatorOutputCap": tokens, "generatorGrammarSHA256": fmt.Sprintf("%x", sha256.Sum256([]byte(grammar))), "preparedSystem": p.System, "preparedPrompt": p.Prompt, "sources": p.Run.Sources, "rawDraft": raw, "decodedBody": decoded, "displayed": run.Text, "citations": run.Citations, "generationAttempts": attempts, "prepareError": fmt.Sprint(prepareErr), "sourceScopeError": fmt.Sprint(sourceErr), "generationError": fmt.Sprint(generationErr), "finishError": fmt.Sprint(finishErr), "rubricError": fmt.Sprint(oracleErr), "boundedAnswerOracleApplied": tc.AnswerOracle != nil, "latencyMs": latency, "canonicalInventory": canonical, "canonicalUnchanged": canonical.Unchanged, "mutationHistoryCountUnchanged": canonical.MutationHistoryCountUnchanged, "executionPassed": executionPassed, "passed": automatedPassed, "manualSemanticReviewPending": true, "releaseQualified": false})
			t.Log(string(report))
			if !automatedPassed {
				t.Error("transfer execution, source scope, bounded answer or canonical preservation failed; every case still needs independent manual review")
			}
		})
	}
}
