package workspace

import (
	"context"
	"strings"
	"testing"
)

func TestAICitedStreamHidesProtocolAndRejectsUnmatchedQuotes(t *testing.T) {
	prepared, err := prepareAIContext(context.Background(), AIRequest{Mode: "answer", Prompt: "When?"}, []AIEvidence{{Text: "The meeting is Friday."}}, nil, "", wordBudget())
	if err != nil {
		t.Fatal(err)
	}
	var streamed strings.Builder
	result, err := GeneratePreparedAI(context.Background(), prepared, func(_ context.Context, system, prompt string, _ int, grammar string, stream func(string)) (string, error) {
		if grammar == "" || !strings.Contains(system, "supporting quote") {
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
