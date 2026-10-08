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

func TestAICitedContextPreservesCanonicalMemoryRoles(t *testing.T) {
	sources := []AIEvidence{
		{Title: "Approved preference", Text: "The current review date is Tuesday.", MemoryKind: "preference"},
		{Title: "Historical decision", Text: "The preferred heading is Review notes.", MemoryID: NewID(), MemoryKind: "preference"},
		{Title: "Approved preference", Text: "The reviewed decision is to retain the current scope.", MemoryID: NewID(), MemoryKind: "decision"},
		{Title: "Approved decision", Text: "The project context includes the design team.", MemoryID: NewID(), MemoryKind: "project_context"},
		{Title: "Approved preference", Text: "Legacy approved context without a captured kind.", MemoryID: NewID()},
	}
	for _, mode := range []string{"answer", "summarize", "explain"} {
		t.Run(mode, func(t *testing.T) {
			prepared, err := prepareAIContext(context.Background(), AIRequest{Mode: mode, Prompt: "What heading do I prefer?", UseMemory: true}, sources, nil, "", wordBudget())
			if err != nil {
				t.Fatal(err)
			}
			contextBody, _, _ := strings.Cut(strings.TrimPrefix(prepared.Prompt, "EVIDENCE\n"), "\nUSER REQUEST")
			body, preferences, hasPreferenceSection := strings.Cut(contextBody, "\nPRESENTATION PREFERENCES\n")
			if !hasPreferenceSection {
				t.Fatal("canonical preferences were not separated from factual evidence", contextBody)
			}
			for _, section := range []struct {
				text    string
				numbers []int
			}{{body, []int{1, 3, 4, 5}}, {preferences, []int{2}}} {
				lines := strings.Split(strings.TrimSpace(section.text), "\n")
				if len(lines) != len(section.numbers) {
					t.Fatal("source omitted, repeated or assigned to the wrong role", section.text)
				}
				for j, line := range lines {
					i := section.numbers[j] - 1
					var row map[string]any
					if err := json.Unmarshal([]byte(line), &row); err != nil {
						t.Fatal(err)
					}
					wantKind, wantMemoryKind := "approved_memory", sources[i].MemoryKind
					if sources[i].MemoryID == "" {
						wantKind, wantMemoryKind = "current_content", ""
					}
					if row["kind"] != wantKind || row["source"] != float64(i+1) || row["text"] != sources[i].Text || prepared.Run.Sources[i].Number != i+1 {
						t.Fatal("source authority or identity changed", row)
					}
					if wantMemoryKind == "" {
						if _, present := row["memoryKind"]; present {
							t.Fatal("memory role inferred without canonical identity/kind", row)
						}
					} else if row["memoryKind"] != wantMemoryKind {
						t.Fatal("canonical memory kind lost or inferred from title", row)
					}
				}
			}
			spans, err := aiEvidenceQuoteSpans(prepared.Run.Sources)
			if err != nil {
				t.Fatal(err)
			}
			found := false
			for _, span := range spans {
				found = found || span == sources[1].Text
			}
			if !found {
				t.Fatal("direct questions about preferences lost their supporting quote")
			}
		})
	}
}

func TestAICitedPreferenceSectionUsesTheMeasuredContextBudget(t *testing.T) {
	sources := []AIEvidence{
		{Title: "Current facts", Text: strings.Repeat("The current task remains pending. ", 80)},
		{Title: "Formatting", Text: "The preferred heading is Delivery notes.", MemoryID: NewID(), MemoryKind: "preference"},
	}
	budget := wordBudget()
	budget.ContextTokens = 1024
	budget.OutputTokens = 256
	count := budget.CountTokens
	measured := map[string]int{}
	budget.CountTokens = func(ctx context.Context, prompts []AIPrompt) ([]int, error) {
		counts, err := count(ctx, prompts)
		for i, prompt := range prompts {
			measured[prompt.System+prompt.User] = counts[i]
		}
		return counts, err
	}
	prepared, err := prepareAIContext(context.Background(), AIRequest{Mode: "answer", Prompt: "Summarize the current task using my preferred heading.", UseMemory: true}, sources, nil, "", budget)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(prepared.Prompt, "\nPRESENTATION PREFERENCES\n") != 1 || !strings.Contains(prepared.Prompt, sources[1].Text) {
		t.Fatal("preference missing or section repeated", prepared.Prompt)
	}
	if measured[prepared.System+prepared.Prompt] != prepared.PromptTokens || prepared.PromptTokens+prepared.OutputTokens+16 > budget.ContextTokens {
		t.Fatal("presentation section was added after tokenization or exceeded the budget")
	}
}

func TestAICitedResponseContractIsMeasuredWithoutInterpretingTheEvidence(t *testing.T) {
	for _, mode := range []string{"answer", "summarize", "explain"} {
		t.Run(mode, func(t *testing.T) {
			request := AIRequest{Mode: mode, Prompt: "Return two complete sentences in the requested order.", Selection: "Selected source remains unchanged."}
			sources := []AIEvidence{{Text: "The room has not been selected. The meeting day is Monday."}}
			budget := wordBudget()
			budget.OutputTokens = 512
			count := budget.CountTokens
			measured := map[string]int{}
			budget.CountTokens = func(ctx context.Context, prompts []AIPrompt) ([]int, error) {
				counts, err := count(ctx, prompts)
				for i, prompt := range prompts {
					if !strings.HasSuffix(prompt.User, aiCitedResponseContract) {
						t.Fatal("response contract was not part of the measured prompt")
					}
					measured[prompt.System+prompt.User] = counts[i]
				}
				return counts, err
			}
			prepared, err := prepareAIContext(context.Background(), request, sources, nil, "", budget)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(prepared.Prompt, "\nUSER REQUEST\n"+request.Prompt) || !strings.Contains(prepared.Prompt, "\nSELECTED TEXT\n"+request.Selection) || len(prepared.Run.Sources) != 1 || prepared.Run.Sources[0].Text != sources[0].Text {
				t.Fatal("source or request was interpreted or rewritten", prepared)
			}
			if measured[prepared.System+prepared.Prompt] != prepared.PromptTokens || prepared.OutputTokens != 512 || prepared.PromptTokens+prepared.OutputTokens+16 > budget.ContextTokens {
				t.Fatal("contract bypassed the token budget", prepared)
			}
			// The same policy applies when a different field is absent, known,
			// or not mentioned. No source keyword can decide answerability here.
			for _, source := range []string{"The meeting is Monday.", "The meeting location has not been selected.", "No budget was approved; the meeting day is unknown."} {
				sources[0].Text = source
				next, err := prepareAIContext(context.Background(), request, sources, nil, "", budget)
				if err != nil || !strings.HasSuffix(next.Prompt, aiCitedResponseContract) || next.Run.Sources[0].Text != source {
					t.Fatal("response policy depends on an extracted answer", next, err)
				}
			}
		})
	}
	for _, tc := range []struct{ mode, system string }{{"rewrite", ""}, {"answer", "Private review contract."}} {
		prepared, err := prepareAIContextWithSystem(context.Background(), AIRequest{Mode: tc.mode, Prompt: "Review this source."}, []AIEvidence{{Text: "The meeting is Monday."}}, nil, "", wordBudget(), tc.system)
		if err != nil || strings.Contains(prepared.Prompt, aiCitedResponseContract) {
			t.Fatal("cited contract leaked into a different output protocol", prepared, err)
		}
	}
}
