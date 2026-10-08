package workspace

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestAICitedStreamHidesProtocolAndRejectsUnmatchedQuotes(t *testing.T) {
	prepared, err := prepareAIContext(context.Background(), AIRequest{Mode: "answer", Prompt: "When?"}, []AIEvidence{{Text: "The meeting is Friday."}}, nil, "", wordBudget())
	if err != nil {
		t.Fatal(err)
	}
	var streamed strings.Builder
	result, err := GeneratePreparedAI(context.Background(), prepared, func(_ context.Context, system, prompt string, _ int, grammar string, stream func(string)) (string, error) {
		if grammar == "" || !strings.Contains(grammar, fmt.Sprintf("%q", `"The meeting is Friday."`)) {
			t.Fatal("missing cited contract")
		}
		result := `{"title":"","claims":[{"text":"Friday","quote":"The meeting is Friday."}]}`
		for _, r := range result {
			stream(string(r))
		}
		return result, nil
	}, func(token string) { streamed.WriteString(token) })
	if err != nil || result.Text != "Friday — “The meeting is Friday.” [1]" || streamed.String() != "Friday" {
		t.Fatal(result, streamed.String(), err)
	}
	_, err = GeneratePreparedAI(context.Background(), prepared, func(context.Context, string, string, int, string, func(string)) (string, error) {
		return `{"title":"","claims":[{"text":"Monday","quote":"The meeting is Monday."}]}`, nil
	}, nil)
	if err == nil {
		t.Fatal("invented supporting quote was accepted")
	}
	if texts := partialAIClaimTexts(`{"title":"","claims":[{"text":"Unfinished \u`); len(texts) != 0 {
		t.Fatal("partial escape exposed", texts)
	}
}

func TestAICitedDraftResolvesOnlyExactSuppliedQuotes(t *testing.T) {
	sources := []AIEvidence{{Number: 1, Text: "The current budget is 12000 USD."}, {Number: 2, Text: "The preferred heading is Status for review."}}
	text, err := renderAICitedClaims(`{"title":"","claims":[{"text":"Status for review","quote":"The preferred heading is Status for review."},{"text":"Budget: 12000 USD","quote":"The current budget is 12000 USD."}]}`, sources)
	if err != nil || text != "Status for review — “The preferred heading is Status for review.” [2]\nBudget: 12000 USD — “The current budget is 12000 USD.” [1]" {
		t.Fatal(text, err)
	}
	for _, raw := range []string{`{"title":"","claims":[]}`, `{"title":"","claims":[{"text":"Unsupported","quote":"An invented supporting quote"}]}`, `{"title":"","claims":[{"text":"Unattributed","quote":""}]}`, `{"title":"","claims":[{"text":"Budget","quote":"The current budget is 12000 USD.","execute":true}]}`} {
		if _, err := renderAICitedClaims(raw, sources); err == nil {
			t.Fatal("malformed or unsupplied citation was accepted", raw)
		}
	}
}

func TestAICitedDraftRejectsRepeatedClaimsWithoutRepair(t *testing.T) {
	for _, repeated := range []string{
		"The meeting is Tuesday.",
		"  The\tmeeting is\nTuesday.  ",
		"The\u00a0meeting\u2003is Tuesday.",
	} {
		raw := string(marshal(map[string]any{"title": "Schedule", "claims": []any{
			map[string]string{"text": "The meeting is Tuesday.", "quote": "The meeting is Tuesday in Hall A."},
			map[string]string{"text": repeated, "quote": "The meeting is Tuesday in Hall A."},
		}}))
		sources := []AIEvidence{{Number: 1, Text: "The meeting is Tuesday in Hall A."}}
		if text, err := renderAICitedClaims(raw, sources); err == nil || text != "" {
			t.Fatal("repeated claim was published or silently repaired", text, err)
		}
	}
	for _, mode := range []string{"answer", "summarize", "explain"} {
		t.Run(mode, func(t *testing.T) {
			prepared, err := prepareAIContext(context.Background(), AIRequest{Mode: mode, Prompt: "Describe the meeting."}, []AIEvidence{{Text: "The meeting is Tuesday in Hall A."}}, nil, "", wordBudget())
			if err != nil {
				t.Fatal(err)
			}
			calls := 0
			_, err = GeneratePreparedAI(context.Background(), prepared, func(context.Context, string, string, int, string, func(string)) (string, error) {
				calls++
				return `{"title":"","claims":[{"text":"Tuesday","quote":"The meeting is Tuesday in Hall A."},{"text":"Tuesday","quote":"The meeting is Tuesday in Hall A."}]}`, nil
			}, nil)
			if err == nil || calls != 1 {
				t.Fatal("duplicate was accepted or triggered a hidden regeneration", err, calls)
			}
		})
	}
}

func TestAICitedDistinctClaimsMayShareAQuoteAndPreserveText(t *testing.T) {
	sources := []AIEvidence{{Number: 1, Text: "The meeting is Tuesday in Hall A."}, {Number: 2, Text: "Tags include Work and work."}}
	raw := `{"title":"","claims":[{"text":"The meeting is Tuesday.","quote":"The meeting is Tuesday in Hall A."},{"text":"The meeting is in Hall A.","quote":"The meeting is Tuesday in Hall A."},{"text":"The tag is Work.","quote":"Tags include Work and work."},{"text":"The tag is work.","quote":"Tags include Work and work."}]}`
	text, err := renderAICitedClaims(raw, sources)
	if err != nil || len(strings.Split(text, "\n")) != 4 || !strings.Contains(text, "The tag is Work.") || !strings.Contains(text, "The tag is work.") {
		t.Fatal("distinct claim text was rejected or rewritten", text, err)
	}
}

func TestAICitedAbstentionIsExclusiveAndNegativeEvidenceIsAnswerable(t *testing.T) {
	sources := []AIEvidence{{Number: 1, Text: "The meeting location has not been selected."}, {Number: 2, Text: "The preferred heading is Team notes.", MemoryID: NewID(), MemoryKind: "preference"}}
	for _, raw := range []string{
		`{"title":"Update","claims":[{"text":"The supplied evidence does not answer this question.","quote":""}]}`,
		`{"title":"","claims":[{"text":"The supplied evidence does not answer this question.","quote":"The meeting location has not been selected."}]}`,
		`{"title":"","claims":[{"text":"The supplied evidence does not answer this question.","quote":" "}]}`,
		`{"title":"","claims":[{"text":"The supplied evidence does not answer this question.","quote":""},{"text":"The meeting location has not been selected.","quote":"The meeting location has not been selected."}]}`,
		`{"title":"","claims":[{"text":"The meeting location has not been selected.","quote":"The meeting location has not been selected."},{"text":"The supplied evidence does not answer this question.","quote":""}]}`,
	} {
		if text, err := renderAICitedClaims(raw, sources); err == nil || text != "" {
			t.Fatal("nonexclusive abstention was published", text, err)
		}
	}
	for _, tc := range []struct{ raw, want string }{
		{`{"title":"","claims":[{"text":"The supplied evidence does not answer this question.","quote":""}]}`, "The supplied evidence does not answer this question."},
		{`{"title":"","claims":[{"text":"The meeting location has not been selected.","quote":"The meeting location has not been selected."}]}`, "The meeting location has not been selected. [1]"},
		{`{"title":"","claims":[{"text":"The preferred heading is Team notes.","quote":"The preferred heading is Team notes."}]}`, "The preferred heading is Team notes. [2]"},
	} {
		if text, err := renderAICitedClaims(tc.raw, sources); err != nil || text != tc.want {
			t.Fatal("valid abstention, explicit negative or direct preference answer rejected", text, err)
		}
	}
}

func TestAICitedDirectPreferenceQuestionRetainsCapturedQuoteAndNumber(t *testing.T) {
	for _, mode := range []string{"answer", "summarize", "explain"} {
		t.Run(mode, func(t *testing.T) {
			prepared, err := prepareAIContext(context.Background(), AIRequest{Mode: mode, Prompt: "What heading do I prefer? Omit a heading.", UseMemory: true}, []AIEvidence{
				{Title: "Current schedule", Text: "The review is Tuesday."},
				{Title: "My formatting", Text: "The preferred heading is Team notes.", MemoryID: NewID(), MemoryKind: "preference"},
			}, nil, "", wordBudget())
			if err != nil {
				t.Fatal(err)
			}
			calls := 0
			completion, err := GeneratePreparedAI(context.Background(), prepared, func(_ context.Context, _, prompt string, _ int, grammar string, _ func(string)) (string, error) {
				calls++
				if !strings.Contains(prompt, "\nPRESENTATION PREFERENCES\n") || !strings.Contains(grammar, fmt.Sprintf("%q", `"The preferred heading is Team notes."`)) {
					t.Fatal("preference separated by removing its source or quote choice")
				}
				return `{"title":"","claims":[{"text":"The preferred heading is Team notes.","quote":"The preferred heading is Team notes."}]}`, nil
			}, nil)
			if err != nil || calls != 1 || completion.Text != "The preferred heading is Team notes. [2]" {
				t.Fatal("direct preference answer identity changed or regeneration occurred", completion, calls, err)
			}
		})
	}
}

func TestAIQuoteCannotChangeNumbersOrSliceUnicodeWords(t *testing.T) {
	for _, tc := range []struct{ source, quote, want string }{
		{"Budget: 12000 USD", "2000 USD", ""},
		{"Budget: 12000 USD", "12000 USD", "12000 USD"},
		{"Budget: 12000 USDollars", "12000 USD", ""},
		{"The résumé is approved", "sumé is approved", ""},
		{"Launch: FRIDAY\n morning.", "friday morning.", "FRIDAY\n morning."},
		{"12000 USD; corrected to 2000 USD", "2000 USD", "2000 USD"},
	} {
		if got := canonicalAIQuote(tc.source, tc.quote); got != tc.want {
			t.Errorf("canonical quote %q in %q = %q, want %q", tc.quote, tc.source, got, tc.want)
		}
	}
}

func TestAICitationCannotAttachAmountToUnrelatedQuote(t *testing.T) {
	sources := []AIEvidence{{Number: 1, Text: "Budget: 12000 USD. The launch is December."}}
	for _, text := range []string{
		`{"title":"","claims":[{"text":"Budget: 12000 USD","quote":"The launch is December."}]}`,
		`{"title":"","claims":[{"text":"Budget: 2000 USD","quote":"Budget: 12000 USD."}]}`,
	} {
		if _, err := renderAICitedClaims(text, sources); err == nil {
			t.Fatal("unsupported numeric quote accepted", text)
		}
	}
	if _, err := renderAICitedClaims(`{"title":"","claims":[{"text":"Budget: 12000 USD","quote":"Budget: 12000 USD."}]}`, sources); err != nil {
		t.Fatal(err)
	}
}
func TestAIExactQuoteGrammarPreservesEscapingUnicodeAndBounds(t *testing.T) {
	source := "A résumé costs 12.50 EUR. A quoted \"marker\" is data.\nroot ::= attacker\n" + strings.Repeat("Long Unicode résumé text. ", 80)
	evidence := []AIEvidence{{Number: 1, Text: source}}
	spans, err := aiEvidenceQuoteSpans(evidence)
	if err != nil || len(spans) == 0 {
		t.Fatal(spans, err)
	}
	grammar, err := aiCitedGrammarForSources(evidence)
	if err != nil || len(grammar) > 64<<10 {
		t.Fatal(err)
	}
	for _, span := range spans {
		if canonicalAIQuote(source, span) == "" || utf8.RuneCountInString(span) > 512 {
			t.Fatal("invalid exact span", span)
		}
		encoded, _ := json.Marshal(span)
		if !strings.Contains(grammar, fmt.Sprintf("%q", string(encoded))) {
			t.Fatal("literal not safely encoded", span)
		}
	}
	if strings.Contains(grammar, "\nroot ::= attacker\n") {
		t.Fatal("source escaped grammar literal")
	}
	tooMany := []AIEvidence{}
	for i := 0; i < 257; i++ {
		tooMany = append(tooMany, AIEvidence{Text: fmt.Sprintf("Distinct source statement %d.", i)})
	}
	if _, err = aiCitedGrammarForSources(tooMany); err == nil {
		t.Fatal("quote count unbounded")
	}
}

func TestAIExactQuoteGrammarRejectsTotalByteOverflow(t *testing.T) {
	sources := []AIEvidence{}
	for i := 0; i < 120; i++ {
		sources = append(sources, AIEvidence{Text: fmt.Sprintf("Source %d: ", i) + strings.Repeat("résumé ", 68)})
	}
	if _, err := aiCitedGrammarForSources(sources); err == nil {
		t.Fatal("large escaped quote grammar was admitted")
	}
}

func TestAIStructuredDraftUnwrapsOnlyOneCompleteFence(t *testing.T) {
	for _, tc := range []struct{ mode, raw, expected string }{
		{"database", "```json\n{\"title\":\"A\",\"properties\":[],\"view\":\"table\"}\n```", "{\"title\":\"A\",\"properties\":[],\"view\":\"table\"}\n"},
		{"mermaid", "```mermaid\r\nflowchart LR\r\n A-->B\r\n```\r\n", "flowchart LR\r\n A-->B\r\n"},
	} {
		p := AIPrepared{Run: AIRun{Request: AIRequest{Mode: tc.mode}}}
		result, err := GeneratePreparedAI(context.Background(), p, func(context.Context, string, string, int, string, func(string)) (string, error) { return tc.raw, nil }, nil)
		if err != nil || result.Text != tc.expected {
			t.Fatalf("%s body changed: %q, %v", tc.mode, result.Text, err)
		}
	}
	for _, raw := range []string{"Before\n```go\nreturn 7\n```", "```go\nreturn 7\n```\nAfter", "```go\nreturn 7", "```go\nreturn 7\n```\n```go\nreturn 8\n```", "```go\n\n```", "````go\nreturn 7\n````"} {
		if result := unwrapAIWholeCodeFence(raw); result != raw {
			t.Fatalf("ambiguous content was discarded: %q => %q", raw, result)
		}
	}
	raw := "```go\nreturn 7\n```"
	for _, tc := range []struct {
		mode    string
		failure error
	}{{"rewrite", nil}, {"code_explain", nil}, {"rewrite", context.Canceled}} {
		p := AIPrepared{Run: AIRun{Request: AIRequest{Mode: tc.mode}}}
		result, err := GeneratePreparedAI(context.Background(), p, func(context.Context, string, string, int, string, func(string)) (string, error) {
			return raw, tc.failure
		}, nil)
		if result.Text != raw || err != tc.failure {
			t.Fatalf("nonliteral or failed draft was changed: %q, %v", result.Text, err)
		}
	}
}

func TestAICodeDraftPreservesActualMarkdownFenceThroughReview(t *testing.T) {
	for _, mode := range []string{"code_refactor", "code_comment"} {
		t.Run(mode, func(t *testing.T) {
			k, e := testKernel(t)
			ctx := context.Background()
			page, block := createPage(t, k, e, "Markdown source"), NewID()
			selected := "```go\nfunc F() int { return 7 }\n```"
			replacement := "```go\n// F returns the configured constant.\nfunc F() int { return 7 }\n```"
			original := "Before selection\n" + selected + "\nAfter selection\n"
			testOK(t, dbBatch(t, k, e, []any{map[string]any{"kind": "block.insert", "block": map[string]any{"blockId": block, "pageId": page, "type": "code", "schemaVersion": 1, "data": map[string]any{"source": original, "language": "markdown"}}}}, "page", page, "block", block))
			prepared, err := k.PrepareAIWithBudget(ctx, e, AIRequest{Mode: mode, Prompt: "Retain the Markdown snippet and add a useful function comment.", PageID: page, BlockID: block, Selection: selected}, wordBudget())
			if err != nil {
				t.Fatal(err)
			}
			var streamed strings.Builder
			completion, err := GeneratePreparedAI(ctx, prepared, func(_ context.Context, _, _ string, _ int, grammar string, stream func(string)) (string, error) {
				if grammar == "" || stream != nil {
					t.Fatal("missing code transport or raw protocol streaming enabled")
				}
				raw, _ := json.Marshal(map[string]string{"source": replacement})
				return string(raw), nil
			}, func(piece string) { streamed.WriteString(piece) })
			if err != nil || completion.Text != replacement || streamed.String() != replacement {
				t.Fatalf("literal streamed/final bytes changed: %q / %q, %v", streamed.String(), completion.Text, err)
			}
			run, err := k.FinishAI(ctx, e, prepared.Run.ID, completion.Text, "completed", "")
			if err != nil || run.Text != replacement {
				t.Fatal(run, err)
			}
			proposal, err := prepareAIAction(ctx, k, e, map[string]any{"runId": run.ID, "kind": "replace_code"})
			if err != nil {
				t.Fatal(err)
			}
			if got := proposal.(AIProposal).After; got != "Before selection\n"+replacement+"\nAfter selection\n" {
				t.Fatalf("review discarded source fence or unselected bytes: %q", got)
			}
		})
	}
}

// This is a prompt/control-flow regression, not a simulated model-quality pass.
// Native holdouts must still prove that the model obeys the requested format.
func TestAICitedRequestContractsPreserveEvidenceBudgetAndRawTitle(t *testing.T) {
	for _, mode := range []string{"answer", "summarize", "explain"} {
		t.Run(mode, func(t *testing.T) {
			for _, request := range []string{
				"What heading do I prefer? Give one factual sentence without a heading.",
				"What is the current region? Give one factual sentence without a heading.",
				"Use my approved preferred heading above one factual sentence about the region.",
				"What is the inspection frequency? Answer only if the evidence supplies that information.",
			} {
				sources := []AIEvidence{
					{Title: "A record title", Text: "The current region is North."},
					{Title: "Stored formatting", Text: "The preferred heading is Progress report.", MemoryID: NewID(), MemoryKind: "preference"},
				}
				budget := wordBudget()
				budget.OutputTokens = 512
				counter := budget.CountTokens
				measured := map[string]int{}
				budget.CountTokens = func(ctx context.Context, prompts []AIPrompt) ([]int, error) {
					counts, err := counter(ctx, prompts)
					for i, prompt := range prompts {
						if strings.Count(prompt.User, aiCitedResponseContract) != 1 || strings.Count(prompt.User, "\nUSER REQUEST\n"+request) != 1 || !strings.HasSuffix(prompt.User, "\nUSER REQUEST\n"+request) || strings.Count(prompt.User, "\nTITLE:") != 1 || strings.Count(prompt.User, "\nCLAIMS:") != 1 || strings.Count(prompt.User, "\nEVIDENCE:") != 1 {
							t.Fatal("structured title, claim and evidence rules must be included in the measured response tail")
						}
						measured[prompt.System+prompt.User] = counts[i]
					}
					return counts, err
				}
				prepared, err := prepareAIContext(context.Background(), AIRequest{Mode: mode, Prompt: request, UseMemory: true}, sources, nil, "", budget)
				if err != nil {
					t.Fatal(err)
				}
				if !strings.Contains(prepared.Prompt, "\nUSER REQUEST\n"+request) || strings.Count(prepared.Prompt, aiCitedResponseContract) != 1 || !strings.HasSuffix(prepared.Prompt, "\nUSER REQUEST\n"+request) || !strings.Contains(prepared.Prompt, "\nPRESENTATION PREFERENCES\n") || len(prepared.Run.Sources) != 2 {
					t.Fatal("format request or canonical preference context was lost", prepared)
				}
				for i, source := range sources {
					if prepared.Run.Sources[i].Text != source.Text || prepared.Run.Sources[i].Number != i+1 {
						t.Fatal("heading policy interpreted or rewrote the captured source")
					}
				}
				if measured[prepared.System+prepared.Prompt] != prepared.PromptTokens || prepared.OutputTokens != 512 || prepared.PromptTokens+prepared.OutputTokens+16 > budget.ContextTokens {
					t.Fatal("heading contract bypassed the fixed output/context budget")
				}
				// No request parsing, hidden retry or renderer rewriting may turn
				// an unwanted model title into a passing no-heading answer.
				for _, title := range []string{"", "A record title"} {
					calls := 0
					raw := string(marshal(map[string]any{"title": title, "claims": []any{map[string]string{"text": "The current region is North.", "quote": "The current region is North."}}}))
					completion, err := GeneratePreparedAI(context.Background(), prepared, func(_ context.Context, system, prompt string, tokens int, grammar string, _ func(string)) (string, error) {
						calls++
						if system != prepared.System || prompt != prepared.Prompt || tokens != 512 || !strings.Contains(grammar, fmt.Sprintf("%q", `"The preferred heading is Progress report."`)) {
							t.Fatal("generation lost the measured prompt, budget or preference quote choice")
						}
						return raw, nil
					}, nil)
					want := "The current region is North. [1]"
					if title != "" {
						want = title + "\n" + want
					}
					if err != nil || calls != 1 || completion.Text != want {
						t.Fatal("model title was rewritten or generation retried", completion, calls, err)
					}
				}
			}
		})
	}
}
