package workspace

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func wordBudget() AIBudget {
	return AIBudget{ContextTokens: 8192, OutputTokens: 2048, CountTokens: func(_ context.Context, prompts []AIPrompt) ([]int, error) {
		counts := make([]int, len(prompts))
		for i, p := range prompts {
			counts[i] = len(strings.Fields(p.System+" "+p.User)) + 32
		}
		return counts, nil
	}}
}
func TestAITokenBudgetKeepsLongSelectionAndUnselectedCode(t *testing.T) {
	k, e := testKernel(t)
	ctx := context.Background()
	page := createPage(t, k, e, "Large code")
	block := NewID()
	var lines strings.Builder
	for i := 0; i < 150; i++ {
		fmt.Fprintf(&lines, "value_%d = %d\n", i, i)
	}
	selected := lines.String()
	original := "# before\n" + selected + "# after\n"
	testOK(t, dbBatch(t, k, e, []any{map[string]any{"kind": "block.insert", "block": map[string]any{"blockId": block, "pageId": page, "type": "code", "schemaVersion": 1, "data": map[string]any{"source": original, "language": "python"}}}}, "page", page, "block", block))
	prepared, err := k.PrepareAIWithBudget(ctx, e, AIRequest{Mode: "code_refactor", Prompt: "Replace selected assignments only", Selection: selected, PageID: page, BlockID: block, ModelPackID: NewID()}, wordBudget())
	if err != nil {
		t.Fatal(err)
	}
	_, encodedSelection, present := strings.Cut(prepared.Prompt, "SELECTED SOURCE (JSON string; replace only this source)\n")
	var decodedSelection string
	if !present || json.Unmarshal([]byte(encodedSelection), &decodedSelection) != nil || decodedSelection != selected || prepared.OutputTokens != 2048 || prepared.PromptTokens+prepared.OutputTokens > 8192 {
		t.Fatal("long selection was omitted or budget lost")
	}
	run, err := k.FinishAI(ctx, e, prepared.Run.ID, "replacement = 1", "completed", "")
	if err != nil {
		t.Fatal(err)
	}
	raw, err := prepareAIAction(ctx, k, e, map[string]any{"runId": run.ID, "kind": "replace_code"})
	if err != nil {
		t.Fatal(err)
	}
	proposal := raw.(AIProposal)
	if proposal.After != "# before\nreplacement = 1# after\n" {
		t.Fatalf("unselected source changed: %q", proposal.After)
	}
}
func TestAITokenBudgetReportsOmissionsAndRejectsOversizedSelection(t *testing.T) {
	budget := wordBudget()
	budget.ContextTokens = 1024
	budget.OutputTokens = 256
	sources := []AIEvidence{}
	for i := 0; i < 25; i++ {
		sources = append(sources, AIEvidence{Title: fmt.Sprintf("Part %d", i), Text: strings.Repeat("independent fact ", 100)})
	}
	prepared, err := prepareAIContext(context.Background(), AIRequest{Mode: "summarize"}, sources, nil, "", budget)
	if err != nil {
		t.Fatal(err)
	}
	if prepared.PromptTokens+prepared.OutputTokens+16 > 1024 || !strings.Contains(prepared.Run.Warning, "omitted") || len(prepared.Run.Sources) >= 25 {
		t.Fatal("missing omission or budget evidence", prepared)
	}
	_, err = prepareAIContext(context.Background(), AIRequest{Mode: "rewrite", Selection: strings.Repeat("exact selection ", 1024)}, sources, nil, "", budget)
	if err == nil || !strings.Contains(err.Error(), "no selected text has been omitted") {
		t.Fatal("oversized selection silently shortened", err)
	}
}
func TestAITokenBudgetRechecksEvidenceAfterTokenization(t *testing.T) {
	k, e := testKernel(t)
	page := createPage(t, k, e, "Original")
	budget := wordBudget()
	counter := budget.CountTokens
	budget.CountTokens = func(ctx context.Context, prompts []AIPrompt) ([]int, error) {
		testOK(t, dbBatch(t, k, e, []any{map[string]any{"kind": "page.update", "pageId": page, "title": "New authoritative title"}}, "page", page))
		return counter(ctx, prompts)
	}
	if _, err := k.PrepareAIWithBudget(context.Background(), e, AIRequest{PageID: page, Mode: "summarize"}, budget); err == nil {
		t.Fatal("stale source was persisted after tokenization")
	}
	var n int
	k.db.QueryRow("SELECT count(*) FROM ai_runs").Scan(&n)
	if n != 0 {
		t.Fatal("stale run persisted")
	}
}
func TestAITruncatedGenerationCannotProduceMutationProposal(t *testing.T) {
	k, e := testKernel(t)
	ctx := context.Background()
	prepared, err := k.PrepareAI(ctx, e, AIRequest{Mode: "generate", Prompt: "Draft a page"})
	if err != nil {
		t.Fatal(err)
	}
	run, err := k.FinishAI(ctx, e, prepared.Run.ID, "unfinished answer", "failed", "OUTPUT_LIMIT")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = prepareAIAction(ctx, k, e, map[string]any{"runId": run.ID, "kind": "append"}); err == nil {
		t.Fatal("partial generation became an approved write")
	}
}

func TestAICitedContextDelimitsMetadataAndPrioritizesContent(t *testing.T) {
	sources := []AIEvidence{{Title: "Misleading title", Text: "Misleading title"}, {Title: "Misleading title", Text: "The delivery is Thursday."}, {Title: "Approved preference", Text: "Use the heading Weekly review.", MemoryID: NewID()}}
	for _, mode := range []string{"answer", "summarize", "explain"} {
		prepared, err := prepareAIContext(context.Background(), AIRequest{Mode: mode, Prompt: "Review the delivery"}, sources, nil, "", wordBudget())
		if err != nil {
			t.Fatal(err)
		}
		if prepared.Run.Sources[0].Text != "The delivery is Thursday." || sources[0].Text != "Misleading title" {
			t.Fatal("source order or caller mutated")
		}
		body, _, _ := strings.Cut(strings.TrimPrefix(prepared.Prompt, "EVIDENCE\n"), "\nUSER REQUEST")
		lines := strings.Split(strings.TrimSpace(body), "\n")
		if len(lines) != 3 {
			t.Fatal(body)
		}
		for i, line := range lines {
			var row map[string]any
			if json.Unmarshal([]byte(line), &row) != nil || row["text"] != prepared.Run.Sources[i].Text {
				t.Fatal("evidence text mixed with metadata", line)
			}
		}
		if prepared.PromptTokens+prepared.OutputTokens+16 > wordBudget().ContextTokens {
			t.Fatal("context budget")
		}
	}
}
