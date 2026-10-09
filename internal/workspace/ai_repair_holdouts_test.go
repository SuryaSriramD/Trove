package workspace

import (
	"context"
	"crypto/sha256"
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

// These twelve holdouts are independent additional inputs. They do not replace
// the original77 cases or their oracles. Corpus bytes were authored before the
// next prompt revision; bounded guards are not complete semantic acceptance.
const aiRepairHoldoutsCorpusSHA256 = "b5e2b877ad83acbc25316f847bb2f08204e0302c9d8438bf4bad138a12ab3121"

var aiRepairHoldoutIDs = []string{
	"repair-missing-boarding-platform",
	"repair-explicit-unscheduled-calibration",
	"repair-current-heading-overrides-memory",
	"repair-literal-approved-instruction",
	"repair-misleading-permit-title",
	"repair-selected-route-paraphrase",
	"repair-current-lamp-voltage",
	"repair-shorten-without-opening-days",
	"repair-generate-without-inspection-purpose",
	"repair-expand-without-approval-dependency",
	"repair-rewrite-supported-purpose",
	"repair-explicit-conditional-suggestions",
}

func aiRepairHoldoutsCorpus(t *testing.T) ([]aiTransferCase, string) {
	t.Helper()
	data, err := os.ReadFile("testdata/expanded-evaluation/repair-holdouts-20261009.json")
	if err != nil {
		t.Fatal(err)
	}
	hash := fmt.Sprintf("%x", sha256.Sum256(data))
	if hash != aiRepairHoldoutsCorpusSHA256 {
		t.Fatal("locked repair holdout bytes changed")
	}
	var corpus struct {
		SchemaVersion int              `json:"schemaVersion"`
		Cases         []aiTransferCase `json:"cases"`
	}
	if err := json.Unmarshal(data, &corpus); err != nil || corpus.SchemaVersion != 1 || len(corpus.Cases) != len(aiRepairHoldoutIDs) {
		t.Fatal("invalid locked repair holdout inventory", err)
	}
	for i, tc := range corpus.Cases {
		if tc.ID != aiRepairHoldoutIDs[i] {
			t.Fatal("repair holdout identity/order changed")
		}
	}
	return corpus.Cases, hash
}

func TestAIRepairHoldoutsIndependentCorpusAndOracle(t *testing.T) {
	cases, _ := aiRepairHoldoutsCorpus(t)
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
func TestAIRepairHoldoutsCanonicalSeedAndScope(t *testing.T) {
	cases, _ := aiRepairHoldoutsCorpus(t)
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
func TestRealLocalAIRepairHoldouts(t *testing.T) {
	if os.Getenv("TROVE_NATIVE_INTEGRATION") != "1" {
		t.Skip("requires explicit native integration")
	}
	if os.Getenv("TROVE_AI_CANONICAL_EVIDENCE_DIR") == "" {
		t.Fatal("retained canonical evidence destination required")
	}
	pack := os.Getenv("TROVE_CHAT_EVAL_PACK")
	if pack != "01990000-0000-7000-8000-000000000106" {
		t.Fatal("fixed pack106 required")
	}
	cases, hash := aiRepairHoldoutsCorpus(t)
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
			report, _ := json.Marshal(aiEvaluationRequestReport(request, attempts, map[string]any{"fixture": tc.ID, "mode": request.Mode, "useMemory": request.UseMemory, "corpusSHA256": hash, "request": request, "sourceTitle": tc.Title, "source": tc.Source, "selection": tc.Selection, "prompt": tc.Prompt, "modelPackId": pack, "contextTokens": 8192, "outputCap": p.OutputTokens, "promptTokens": p.PromptTokens, "generatorSystem": system, "generatorPrompt": prompt, "generatorOutputCap": tokens, "generatorGrammarSHA256": fmt.Sprintf("%x", sha256.Sum256([]byte(grammar))), "preparedSystem": p.System, "preparedPrompt": p.Prompt, "sources": p.Run.Sources, "rawDraft": raw, "decodedBody": decoded, "displayed": run.Text, "citations": run.Citations, "generationAttempts": attempts, "prepareError": fmt.Sprint(prepareErr), "sourceScopeError": fmt.Sprint(sourceErr), "generationError": fmt.Sprint(generationErr), "finishError": fmt.Sprint(finishErr), "rubricError": fmt.Sprint(oracleErr), "boundedAnswerOracleApplied": tc.AnswerOracle != nil, "latencyMs": latency, "canonicalInventory": canonical, "canonicalUnchanged": canonical.Unchanged, "mutationHistoryCountUnchanged": canonical.MutationHistoryCountUnchanged, "executionPassed": executionPassed, "passed": automatedPassed, "manualSemanticReviewPending": true, "releaseQualified": false}))
			t.Log(string(report))
			if !automatedPassed {
				t.Error("repair holdout execution, source scope, bounded answer or canonical preservation failed; every case still needs independent manual review")
			}
		})
	}
}
