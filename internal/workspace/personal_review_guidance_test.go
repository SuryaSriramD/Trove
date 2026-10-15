package workspace

import (
	"context"
	"reflect"
	"strings"
	"testing"
)

// This measures context accounting and protocol isolation with a deterministic
// byte-counting stand-in. It establishes no tokenizer or model-quality result.
func TestPersonalReviewGuidanceBudgetAndProtocolIsolation(t *testing.T) {
	k, e := testKernel(t)
	r, _, _ := generativeReviewFixture(t, k, e)
	ctx := context.Background()
	count := func(p AIPrompt) int { return len(p.System) + len(p.User) + 32 }
	budget := AIBudget{ContextTokens: 8192, OutputTokens: 1024, CountTokens: func(_ context.Context, prompts []AIPrompt) ([]int, error) {
		counts := make([]int, len(prompts))
		for i, p := range prompts {
			counts[i] = count(p)
		}
		return counts, nil
	}}
	for _, focus := range []string{
		"Return exactly two summary claims covering the selected source facts.",
		"Name the selected source pages in summary claims.",
	} {
		t.Run(focus, func(t *testing.T) {
			request := AIRequest{Mode: "workspace_agent", Prompt: focus, ModelPackID: r.Generative.ModelPackID}
			prepared, err := prepareAIContextWithSystem(ctx, request, r.Evidence, nil, r.Warning, budget, personalReviewSystem(*r.Generative))
			if err != nil {
				t.Fatal(err)
			}
			measured := count(AIPrompt{System: prepared.System, User: prepared.Prompt})
			if measured != prepared.PromptTokens || measured+prepared.OutputTokens+16 > budget.ContextTokens || prepared.OutputTokens != 1024 {
				t.Fatalf("complete review prompt or reserved output missing from budget: measured=%d recorded=%d output=%d", measured, prepared.PromptTokens, prepared.OutputTokens)
			}
			if !reflect.DeepEqual(prepared.Run.Sources, r.Evidence) || !strings.Contains(prepared.Prompt, focus) {
				t.Fatal("review guidance displaced captured evidence or exact request")
			}
			if !strings.Contains(prepared.System, "Use source titles for explicitly requested page names, never in place of body facts.") {
				t.Fatal("missing explicit page-name exception to title guidance")
			}
			if strings.Contains(prepared.System, "Return only a JSON object with a string title and a claims array") || strings.Contains(prepared.Prompt, aiCitedResponseContract) {
				t.Fatal("ordinary cited-answer protocol leaked into personal review")
			}
			ordinary := request
			ordinary.Mode = "answer"
			answer, err := prepareAIContext(ctx, ordinary, r.Evidence, nil, r.Warning, budget)
			if err != nil || strings.Contains(answer.System, "The example claim illustrates the schema") || strings.Contains(answer.System, "Allowed claim categories:") {
				t.Fatal("review-specific guidance changed ordinary answers", err)
			}
			if !strings.Contains(answer.System, "Return only a JSON object with a string title and a claims array") {
				t.Fatal("ordinary answer lost its original protocol")
			}
			low := budget
			low.ContextTokens = 2048
			if _, err := prepareAIContextWithSystem(ctx, request, r.Evidence, nil, r.Warning, low, personalReviewSystem(*r.Generative)); err == nil {
				t.Fatal("over-budget system instructions bypassed context admission")
			}
			t.Logf("stand-in complete prompt units=%d, output=%d, reserve=16, evidence=%d", measured, prepared.OutputTokens, len(prepared.Run.Sources))
		})
	}
}
