package workspace

// These executable cases preserve the earlier scenario documents. Deterministic
// tests exercise their service boundaries; only explicitly selected native runs
// can supply tokenizer/model evidence. Reports never equate the two.
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

type workflowCase struct {
	ID                 string `json:"id"`
	Scenario           string `json:"scenario"`
	Expected           string `json:"expected"`
	ModelRequired      bool   `json:"modelRequired"`
	Query              string `json:"query"`
	RelevantSource     string `json:"relevantSource"`
	IrrelevantSource   string `json:"irrelevantSource"`
	InaccessibleSource string `json:"inaccessibleNearerSource"`
}

func workflowCorpus(t *testing.T, family string, count int) ([]workflowCase, string) {
	t.Helper()
	raw, err := os.ReadFile("testdata/expanded-evaluation/" + family + ".json")
	if err != nil {
		t.Fatal(err)
	}
	var corpus struct {
		SchemaVersion int            `json:"schemaVersion"`
		Cases         []workflowCase `json:"cases"`
	}
	if err = json.Unmarshal(raw, &corpus); err != nil {
		t.Fatal(err)
	}
	if corpus.SchemaVersion != 1 || len(corpus.Cases) != count {
		t.Fatal("scenario inventory changed without a new declared runner")
	}
	seen := map[string]bool{}
	for _, c := range corpus.Cases {
		if c.ID == "" || seen[c.ID] {
			t.Fatal("missing/duplicate scenario")
		}
		seen[c.ID] = true
	}
	return corpus.Cases, fmt.Sprintf("%x", sha256.Sum256(raw))
}
func workflowNative(t *testing.T, kind string) (*native.Supervisor, native.Pack) {
	t.Helper()
	if os.Getenv("TROVE_NATIVE_INTEGRATION") != "1" {
		t.Skip("explicit native bundle and verified model required; deterministic checks do not qualify this case")
	}
	packID := os.Getenv("TROVE_CHAT_EVAL_PACK")
	if kind == "embedding" {
		packID = os.Getenv("TROVE_EMBED_EVAL_PACK")
	}
	if packID == "" || os.Getenv("TROVE_MODEL_ROOT") == "" || os.Getenv("TROVE_NATIVE_BUNDLE") == "" {
		t.Fatal("select exact native bundle, model directory and pack; no fallback")
	}
	manager, err := native.NewManager(os.Getenv("TROVE_MODEL_ROOT"))
	if err != nil {
		t.Fatal(err)
	}
	supervisor, err := native.NewSupervisor(os.Getenv("TROVE_NATIVE_BUNDLE"), manager)
	if err != nil {
		t.Fatal(err)
	}
	packs, err := manager.List()
	if err != nil {
		t.Fatal(err)
	}
	for _, status := range packs {
		if status.Pack.ID == packID && status.State == "ready" && status.Pack.Kind == kind && !status.Pack.QualificationOnly {
			return supervisor, status.Pack
		}
	}
	t.Fatal("selected verified compatible model is unavailable")
	return nil, native.Pack{}
}
func workflowBudget(supervisor *native.Supervisor, pack string) AIBudget {
	budget := wordBudget()
	budget.OutputTokens = 512
	if supervisor != nil {
		budget.CountTokens = func(ctx context.Context, prompts []AIPrompt) ([]int, error) {
			framed := make([]string, len(prompts))
			for i, p := range prompts {
				framed[i] = native.ChatPrompt(p.System, p.User)
			}
			return supervisor.CountTokens(ctx, pack, framed)
		}
	}
	return budget
}
func workflowParagraph(t *testing.T, k *Kernel, e contracts.ServiceExecutionContext, title, source string) (string, string) {
	t.Helper()
	page, block := createPage(t, k, e, title), NewID()
	testOK(t, dbBatch(t, k, e, []any{map[string]any{"kind": "block.insert", "block": map[string]any{"blockId": block, "pageId": page, "type": "paragraph", "schemaVersion": 1, "data": map[string]any{"richText": []any{map[string]any{"text": source}}}}}}, "page", page, "block", block))
	return page, block
}

const contextFact = "Project Marigold launches on 14 April 2031."

func contextCaseSource(id string) string {
	var inventory strings.Builder
	for i := 0; i < 720; i++ {
		fmt.Fprintf(&inventory, "Archived inventory entry %d describes ordinary folders and unrelated filing order. ", i)
	}
	filler := inventory.String()
	switch id {
	case "near-budget":
		return contextFact + " " + strings.Repeat("q ", 7600)
	case "overflow", "beginning-fact":
		return contextFact + " " + filler + filler
	case "middle-fact":
		return filler + contextFact + " " + filler
	case "ending-fact":
		return filler + filler + contextFact
	default:
		return contextFact
	}
}
func contextAnswerOracle(raw string, retained bool) error {
	var result expandedDraft
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		return err
	}
	if result.Title != "" || len(result.Claims) != 1 {
		return fmt.Errorf("one answer without a heading required")
	}
	claim := result.Claims[0]
	if !retained {
		if claim.Text != "The supplied evidence does not answer this question." || claim.Quote != "" {
			return fmt.Errorf("omitted fact requires abstention")
		}
	} else if !regexp.MustCompile(`^(?:Project Marigold (?:launches|will launch|is scheduled to launch) on 14 April 2031|The launch date for Project Marigold is 14 April 2031)\.$`).MatchString(claim.Text) || claim.Quote != contextFact {
		return fmt.Errorf("launch answer and exact independent supporting fact required")
	}
	return nil
}
func TestWorkflowContextOracleRejectsCounterexamples(t *testing.T) {
	for _, retained := range []bool{false, true} {
		valid := `{"title":"","claims":[{"text":"The supplied evidence does not answer this question.","quote":""}]}`
		if retained {
			valid = string(marshal(map[string]any{"title": "", "claims": []any{map[string]string{"text": contextFact, "quote": contextFact}}}))
		}
		if err := contextAnswerOracle(valid, retained); err != nil {
			t.Fatal(err)
		}
		for _, bad := range []string{strings.Replace(valid, "2031", "2032", -1), strings.Replace(valid, "Marigold", "Private source", 1), strings.Replace(valid, contextFact, contextFact+" The launch is guaranteed to succeed.", 1), `{"title":"","claims":[]}`} {
			if bad != valid && contextAnswerOracle(bad, retained) == nil {
				t.Fatal("oracle accepted counterexample", bad)
			}
		}
	}
}
func TestWorkflowContextBoundaries(t *testing.T) { runWorkflowContext(t, nil, NewID()) }
func TestRealLocalWorkflowContext(t *testing.T) {
	s, p := workflowNative(t, "chat")
	runWorkflowContext(t, s, p.ID)
}
func runWorkflowContext(t *testing.T, supervisor *native.Supervisor, pack string) {
	cases, hash := workflowCorpus(t, "context", 12)
	for _, tc := range cases {
		t.Run(tc.ID, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
			defer cancel()
			k, e := testKernel(t)
			page, block := workflowParagraph(t, k, e, "Current launch record", contextCaseSource(tc.ID))
			request := AIRequest{Mode: "answer", PageID: page, Prompt: "When does Project Marigold launch? Give one complete sentence and no heading. If the supplied excerpts do not contain its launch date, abstain.", ModelPackID: pack}
			checks := map[string]bool{}
			report := map[string]any{"fixture": tc.ID, "family": "context", "corpusSHA256": hash, "modelPackId": pack, "engineMode": "deterministic", "releaseQualified": false, "assertions": checks}
			if supervisor != nil {
				report["engineMode"] = "native"
			}
			defer func() { report["passed"] = !t.Failed(); t.Log(string(marshal(report))) }()
			check := func(name string, ok bool) {
				checks[name] = ok
				if !ok {
					t.Errorf("context invariant failed: %s", name)
				}
			}
			budget := workflowBudget(supervisor, pack)
			if tc.ID == "exact-code" || tc.ID == "oversized-code" {
				selected := "value = 'తెలుగు 世界'\r\nreturn value\r\n"
				if tc.ID == "oversized-code" {
					selected = strings.Repeat("x = 1\r\n", 1500)
					budget.ContextTokens = 1024
				}
				original := "# unchanged prefix\r\n" + selected + "# unchanged suffix\r\n"
				testOK(t, dbBatch(t, k, e, []any{map[string]any{"kind": "block.update", "blockId": block, "type": "code", "schemaVersion": 1, "data": map[string]any{"source": original, "language": "python"}}}, "page", page, "block", block))
				request.Mode, request.BlockID, request.Selection, request.Prompt = "code_refactor", block, selected, "Replace only the exact selected code."
			}
			// Framing is measured with the actual helper. The raw request narrowly fits
			// this explicit context limit while its framed form must be rejected.
			if tc.ID == "framing-cost" {
				request.Mode, request.Selection = "generate", strings.Repeat("x = 1\n", 180)
				request.BlockID = block
				testOK(t, dbBatch(t, k, e, []any{map[string]any{"kind": "block.update", "blockId": block, "type": "code", "schemaVersion": 1, "data": map[string]any{"source": request.Selection, "language": "python"}}}, "page", page, "block", block))
				baseline, err := prepareAIContext(ctx, request, nil, nil, "", budget)
				if err != nil {
					t.Fatal(err)
				}
				rawCount := baseline.PromptTokens - 12
				if supervisor != nil {
					counts, err := supervisor.CountTokens(ctx, pack, []string{baseline.System + baseline.Prompt})
					if err != nil {
						t.Fatal(err)
					}
					rawCount = counts[0]
				}
				budget.ContextTokens = rawCount + budget.OutputTokens + 16
				check("framingAddsTokens", baseline.PromptTokens > rawCount)
				report["unframedTokens"], report["framedTokens"], report["contextTokens"] = rawCount, baseline.PromptTokens, budget.ContextTokens
				audit := beginAICanonicalObservation(t, k)
				_, err = prepareAIContext(ctx, request, nil, nil, "", budget)
				check("framingRejected", err != nil)
				inv := audit.finish(t, k)
				report["canonicalInventory"], report["canonicalUnchanged"], report["mutationHistoryCountUnchanged"] = inv, inv.Unchanged, inv.MutationHistoryCountUnchanged
				report["generationAttempts"] = 0
				return
			}
			if tc.ID == "near-budget" {
				request.BlockID = block
				// Calibrate neutral exact selection before inference. The answer
				// fact/oracle never changes; framing plus the 512-token output
				// reservation must place the final prompt near 8192 positions.
				sources, _, err := k.aiSources(ctx, e, request)
				if err != nil {
					t.Fatal(err)
				}
				low, high := 0, 7600
				for low < high {
					mid := (low + high + 1) / 2
					candidate := request
					candidate.Selection = contextFact + " " + strings.Repeat("q ", mid)
					if _, err := prepareAIContext(ctx, candidate, sources, nil, "", budget); err == nil {
						low = mid
					} else {
						high = mid - 1
					}
				}
				request.Selection = contextFact + " " + strings.Repeat("q ", low)
				report["calibratedSelectionBytes"] = len(request.Selection)
			}

			var audit *aiCanonicalObservation
			if tc.ID != "during-tokenization" && tc.ID != "during-generation" {
				a := beginAICanonicalObservation(t, k)
				audit = a
			}
			if tc.ID == "during-tokenization" {
				counter := budget.CountTokens
				changed := false
				budget.CountTokens = func(ctx context.Context, p []AIPrompt) ([]int, error) {
					if !changed {
						changed = true
						testOK(t, dbBatch(t, k, e, []any{map[string]any{"kind": "page.update", "pageId": page, "title": "Author deliberately changed source during tokenization"}}, "page", page))
						a := beginAICanonicalObservation(t, k)
						audit = a
					}
					return counter(ctx, p)
				}
			}
			prepared, err := k.PrepareAIWithBudget(ctx, e, request, budget)
			report["prepareError"], report["request"], report["contextTokens"], report["outputCap"], report["generationAttempts"] = fmt.Sprint(err), request, budget.ContextTokens, budget.OutputTokens, 0
			if tc.ID == "oversized-code" || tc.ID == "during-tokenization" {
				check("preparationRejected", err != nil)
				var n int
				k.db.QueryRow("SELECT count(*) FROM ai_runs").Scan(&n)
				check("noRunnableDraft", n == 0)
			} else {
				if err != nil {
					t.Fatal(err)
				}
				report["sources"], report["promptTokens"], report["warning"] = prepared.Run.Sources, prepared.PromptTokens, prepared.Run.Warning
				check("reservedBudget", prepared.PromptTokens+prepared.OutputTokens+16 <= budget.ContextTokens)
				if tc.ID == "near-budget" {
					check("nearCapacityMeasured", prepared.PromptTokens >= (budget.ContextTokens-budget.OutputTokens-16)*9/10)
				}
				if tc.ID == "short-source" {
					check("noOmission", prepared.Run.Warning == "")
				}
				if tc.ID == "overflow" || tc.ID == "middle-fact" || tc.ID == "ending-fact" {
					check("omissionDisclosed", strings.Contains(prepared.Run.Warning, "shortened") || strings.Contains(prepared.Run.Warning, "omitted"))
				}
				if tc.ID == "exact-code" {
					_, encoded, found := strings.Cut(prepared.Prompt, "SELECTED SOURCE (JSON string; replace only this source)\n")
					var exact string
					check("selectionPreserved", found && json.Unmarshal([]byte(encoded), &exact) == nil && exact == request.Selection)
					run, err := k.FinishAI(ctx, e, prepared.Run.ID, "replacement = 'unchanged'\r\n", "completed", "")
					if err != nil {
						t.Fatal(err)
					}
					raw, err := prepareAIAction(ctx, k, e, map[string]any{"runId": run.ID, "kind": "replace_code"})
					if err != nil {
						t.Fatal(err)
					}
					check("unselectedCodePreserved", raw.(AIProposal).After == "# unchanged prefix\r\nreplacement = 'unchanged'\r\n# unchanged suffix\r\n")
				} else if tc.ID == "during-generation" || tc.ID == "truncated-output" {
					state, failure := "completed", ""
					if tc.ID == "during-generation" {
						testOK(t, dbBatch(t, k, e, []any{map[string]any{"kind": "page.update", "pageId": page, "title": "Author deliberately changed source during generation"}}, "page", page))
						a := beginAICanonicalObservation(t, k)
						audit = a
					} else {
						state, failure = "failed", "OUTPUT_LIMIT"
					}
					run, err := k.FinishAI(ctx, e, prepared.Run.ID, "partial or superseded draft", state, failure)
					if err != nil {
						t.Fatal(err)
					}
					check("draftInvalidated", run.State == "failed")
					_, err = prepareAIAction(ctx, k, e, map[string]any{"runId": run.ID, "kind": "append"})
					check("proposalRejected", err != nil)
				} else {
					retained := false
					for _, s := range prepared.Run.Sources {
						retained = retained || strings.Contains(s.Text, contextFact)
					}
					if tc.ID == "beginning-fact" || tc.ID == "short-source" || tc.ID == "near-budget" {
						check("requiredFactRetained", retained)
					}
					attempts := 0
					raw := ""
					completion, generationErr := GeneratePreparedAI(ctx, prepared, func(ctx context.Context, system, prompt string, n int, grammar string, stream func(string)) (string, error) {
						attempts++
						if attempts != 1 {
							return "", fmt.Errorf("one generation only")
						}
						if supervisor != nil {
							out, err := supervisor.GenerateTextWithGrammar(ctx, pack, native.ChatPrompt(system, prompt), n, grammar, stream)
							raw = out
							return out, err
						}
						raw = `{"title":"","claims":[{"text":"The supplied evidence does not answer this question.","quote":""}]}`
						if retained {
							raw = string(marshal(map[string]any{"title": "", "claims": []any{map[string]string{"text": contextFact, "quote": contextFact}}}))
						}
						return raw, nil
					}, nil)
					state, failure := "completed", ""
					if generationErr != nil {
						state, failure = "failed", generationErr.Error()
					}
					run, finishErr := k.FinishAI(ctx, e, prepared.Run.ID, completion.Text, state, failure)
					check("oneGeneration", attempts == 1)
					check("generatedSuccessfully", generationErr == nil && finishErr == nil && run.State == "completed")
					check("independentAnswerOracle", contextAnswerOracle(raw, retained) == nil)
					if retained {
						check("currentCitation", len(run.Citations) == 1 && string(run.Citations[0].Location.PageId) == page && run.Citations[0].State == "current")
					}
					report["rawDraft"], report["generationAttempts"], report["generationError"], report["citations"], report["displayed"], report["retainedFact"] = raw, attempts, fmt.Sprint(generationErr), run.Citations, run.Text, retained
				}
			}
			if audit == nil {
				t.Fatal("canonical observation missing")
			}
			inv := audit.finish(t, k)
			report["canonicalInventory"], report["canonicalUnchanged"], report["mutationHistoryCountUnchanged"] = inv, inv.Unchanged, inv.MutationHistoryCountUnchanged
			check("canonicalPreserved", inv.Unchanged)
		})
	}
}

func TestAIContextRetainsDistinctChunksOfOneBlock(t *testing.T) {
	k, e := testKernel(t)
	text := strings.Repeat("Inventory reference is retained. ", 100) + "The unique final approval code is ORCHID-73."
	page, block := workflowParagraph(t, k, e, "Long evidence", text)
	p, err := k.PrepareAIWithBudget(context.Background(), e, AIRequest{Mode: "answer", PageID: page, Prompt: "What is the final approval code?", ModelPackID: NewID()}, wordBudget())
	if err != nil {
		t.Fatal(err)
	}
	found := false
	count := 0
	for _, s := range p.Run.Sources {
		if s.Location.BlockId != nil && string(*s.Location.BlockId) == block {
			count++
			found = found || strings.Contains(s.Text, "ORCHID-73")
		}
	}
	if !found || count < 2 {
		t.Fatal("distinct later chunk lost before budget selection", p.Run.Sources)
	}
}
