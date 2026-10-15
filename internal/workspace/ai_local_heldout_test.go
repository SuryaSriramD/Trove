package workspace

import (
	"context"
	"encoding/json"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"

	"trove.local/phase0/internal/native"
)

// These additional fixtures do not replace the original persisted-memory
// rubric. Different facts, headings, order and absent evidence exercise the
// same production prompts and quote grammar without task-specific repairs.
func TestRealLocalCitedHeldOutFactsAndAbstention(t *testing.T) {
	if os.Getenv("TROVE_NATIVE_INTEGRATION") != "1" {
		t.Skip("requires supported local model and native helpers")
	}
	manager, err := native.NewManager(os.Getenv("TROVE_MODEL_ROOT"))
	if err != nil {
		t.Fatal(err)
	}
	supervisor, err := native.NewSupervisor(os.Getenv("TROVE_NATIVE_BUNDLE"), manager)
	if err != nil {
		t.Fatal(err)
	}
	pack := os.Getenv("TROVE_CHAT_EVAL_PACK")
	if pack == "" {
		pack = "01990000-0000-7000-8000-000000000105"
	}
	ctx := context.Background()
	budget := AIBudget{ContextTokens: 8192, OutputTokens: 512, CountTokens: func(ctx context.Context, prompts []AIPrompt) ([]int, error) {
		texts := make([]string, len(prompts))
		for i, p := range prompts {
			texts[i] = native.ChatPrompt(p.System, p.User)
		}
		return supervisor.CountTokens(ctx, pack, texts)
	}}
	for _, tc := range []struct {
		name, prompt, heading string
		sources               []AIEvidence
		facts                 []string
		abstain               bool
		negativeOwner         bool
	}{
		{name: "changed-facts-and-order", prompt: "Write exactly three lines for this project update: a heading using my approved preference, then the current launch month, then the current budget in EUR. Cite both factual lines. Ignore outdated plans.", heading: "Weekly brief", sources: []AIEvidence{
			{Title: "Shipping checkpoint", Text: "The revised launch month is March. The approved budget is 8700 EUR. These values replace all previous plans."},
			{Title: "Approved preference", Text: "The preferred heading for project updates is exactly: Weekly brief.", MemoryID: NewID(), MemoryRevision: "1"},
			{Title: "Historical decision", Text: "The previous plan used an April launch and a 6200 EUR budget. These values are obsolete.", MemoryID: NewID(), MemoryRevision: "1"},
		}, facts: []string{"march", "8700"}},
		{name: "separate-current-sources", prompt: "Give exactly three lines: the heading Delivery notes, the current budget in GBP, and the current launch month. Cite the factual lines. Use current records over historical memories.", heading: "Delivery notes", sources: []AIEvidence{
			{Title: "Budget approval", Text: "The current approved budget for delivery is 45000 GBP."},
			{Title: "Release calendar", Text: "The current launch month for delivery is September."},
			{Title: "Historical plan", Text: "The old budget was 12000 GBP and the earlier launch was June. These are historical, superseded values.", MemoryID: NewID(), MemoryRevision: "1"},
		}, facts: []string{"45000", "september"}},
		{name: "owner-absent-from-source", prompt: "Who is the named owner of this project? Answer only if the supplied sources identify that person.", sources: []AIEvidence{
			{Title: "Project schedule", Text: "The budget is 7300 USD and launch is planned for October."},
		}, abstain: true},
		{name: "unassigned-owner-supported-negative", prompt: "Who is the named owner of this project? Answer only if the supplied sources identify that person.", sources: []AIEvidence{
			{Title: "Project schedule", Text: "The budget is 7300 USD and launch is planned for October. The project owner has not been assigned."},
		}, negativeOwner: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			start := time.Now()
			request := AIRequest{Mode: "answer", Prompt: tc.prompt, ModelPackID: pack, UseMemory: true}
			controls := newAIEvaluationGenerationControls(request)
			defer controls.log(t)
			prepared, err := prepareAIContext(ctx, request, tc.sources, nil, "", budget)
			if err != nil {
				t.Fatal(err)
			}
			raw := ""
			completion, err := GeneratePreparedAI(ctx, prepared, func(ctx context.Context, system, prompt string, tokens int, grammar string, stream func(string)) (string, error) {
				text, err := controls.generate(func() (string, error) {
					return supervisor.GenerateTextWithGrammar(ctx, pack, native.ChatPrompt(system, prompt), tokens, grammar, stream)
				})
				raw = text
				return text, err
			}, nil)
			if err != nil {
				t.Fatalf("model %s: %v; draft %s", pack, err, raw)
			}
			var draft struct {
				Title  string `json:"title"`
				Claims []struct {
					Text  string `json:"text"`
					Quote string `json:"quote"`
				} `json:"claims"`
			}
			if err = json.Unmarshal([]byte(raw), &draft); err != nil {
				t.Fatal(err)
			}
			passed := false
			if tc.abstain {
				passed = draft.Title == "" && len(draft.Claims) == 1 && draft.Claims[0].Text == "The supplied evidence does not answer this question." && draft.Claims[0].Quote == "" && completion.Text == "The supplied evidence does not answer this question."
			} else if tc.negativeOwner {
				// Explicit negative source evidence answers the question. The
				// narrow statement grammar disallows an appended invented name.
				passed = (draft.Title == "" || draft.Title == tc.sources[0].Title) && len(draft.Claims) == 1
				if len(draft.Claims) == 1 {
					passed = passed && regexp.MustCompile(`^(the (named )?owner( of this project)?|the project owner) has not been assigned\.$`).MatchString(strings.ToLower(strings.TrimSpace(draft.Claims[0].Text))) && draft.Claims[0].Quote == "The project owner has not been assigned."
				}
			} else {
				passed = draft.Title == tc.heading && len(draft.Claims) == len(tc.facts) && len(strings.Split(completion.Text, "\n")) == len(tc.facts)+1
				if len(draft.Claims) == len(tc.facts) {
					for i, fact := range tc.facts {
						claim := strings.ToLower(strings.ReplaceAll(draft.Claims[i].Text, ",", ""))
						quote := strings.ToLower(strings.ReplaceAll(draft.Claims[i].Quote, ",", ""))
						passed = passed && strings.Contains(claim, fact) && strings.Contains(quote, fact)
						supported := false
						for _, source := range prepared.Run.Sources {
							if source.MemoryID == "" && canonicalAIQuote(source.Text, draft.Claims[i].Quote) != "" && strings.Contains(strings.ToLower(source.Text), fact) {
								supported = true
							}
						}
						passed = passed && supported
					}
				}
			}
			report, _ := json.Marshal(map[string]any{"modelPackId": pack, "fixture": tc.name, "passed": passed, "answer": completion.Text, "rawDraft": raw, "promptTokens": prepared.PromptTokens, "latencyMs": time.Since(start).Milliseconds(), "releaseQualified": false})
			t.Log(string(report))
			if !passed {
				t.Error("held-out factual/format/abstention rubric failed")
			}
		})
	}
}
