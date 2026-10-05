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
		{"code_comment", "```go\n// Retain this comment.\nfunc F() int { return 7 }\n```", "```go\n// Retain this comment.\nfunc F() int { return 7 }\n```"},
		{"database", "```json\n{\"title\":\"A\",\"properties\":[],\"view\":\"table\"}\n```", "{\"title\":\"A\",\"properties\":[],\"view\":\"table\"}\n"},
		{"mermaid", "```mermaid\r\nflowchart LR\r\n A-->B\r\n```\r\n", "flowchart LR\r\n A-->B\r\n"},
		{"code_refactor", "func F() int { return 7 }\n", "func F() int { return 7 }\n"},
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
	}{{"rewrite", nil}, {"code_explain", nil}, {"code_comment", context.Canceled}} {
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
				if grammar != "" {
					t.Fatal("code source was forced into a structured grammar")
				}
				for _, piece := range []string{replacement[:4], replacement[4:]} {
					stream(piece)
				}
				return replacement, nil
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
