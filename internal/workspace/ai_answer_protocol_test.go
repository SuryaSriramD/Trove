package workspace

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

// Protocol tests never establish answerability or semantic entailment. Native
// comparisons use the unchanged independently declared source/question oracles.
func TestAIAnswerEvidenceFirstProtocolIsScopedAndMeasured(t *testing.T) {
	const fact = "The exhibition opens on Monday."
	const question = "When does the exhibition open?"
	for _, mode := range []string{"answer", "summarize", "explain"} {
		t.Run(mode, func(t *testing.T) {
			budget := wordBudget()
			counts := map[string]int{}
			count := budget.CountTokens
			budget.CountTokens = func(ctx context.Context, prompts []AIPrompt) ([]int, error) {
				values, err := count(ctx, prompts)
				for i, p := range prompts {
					counts[p.System+p.User] = values[i]
				}
				return values, err
			}
			prepared, err := prepareAIContext(context.Background(), AIRequest{Mode: mode, Prompt: question}, []AIEvidence{{Text: fact}}, nil, "", budget)
			if err != nil {
				t.Fatal(err)
			}
			if prepared.PromptTokens != counts[prepared.System+prepared.Prompt] || prepared.PromptTokens+prepared.OutputTokens+16 > budget.ContextTokens || !strings.HasSuffix(prepared.Prompt, "\nUSER REQUEST\n"+question) || len(prepared.Run.Sources) != 1 || prepared.Run.Sources[0].Text != fact {
				t.Fatal("protocol bypassed measured budget or changed selected source/request")
			}
			calls := 0
			result, err := GeneratePreparedAI(context.Background(), prepared, func(_ context.Context, system, prompt string, cap int, grammar string, stream func(string)) (string, error) {
				calls++
				if system != prepared.System || prompt != prepared.Prompt || cap != prepared.OutputTokens {
					t.Fatal("unmeasured generation controls")
				}
				var claimRule string
				for _, line := range strings.Split(grammar, "\n") {
					if strings.HasPrefix(line, "claim ::=") {
						claimRule = line
					}
				}
				quote, text := strings.Index(claimRule, `\"quote\"`), strings.Index(claimRule, `\"text\"`)
				if quote < 0 || text < 0 {
					t.Fatal("claim rule missing its fields", claimRule)
				}
				if mode == "answer" {
					if quote > text || !strings.Contains(grammar, "evidence-first-v2") || !strings.Contains(system, "evidence-first-v2") {
						t.Fatal("answer must choose evidence before composing claim", grammar)
					}
				} else {
					legacy, _ := aiCitedGrammarForSources(prepared.Run.Sources)
					if grammar != legacy || quote < text || strings.Contains(system, "evidence-first-v2") {
						t.Fatal("legacy cited mode changed")
					}
				}
				if !strings.Contains(grammar, fmt.Sprintf("%q", `"`+fact+`"`)) || !strings.Contains(grammar, `evidence-quote ::= "\"\""`) {
					t.Fatal("evidence or empty-quote option removed")
				}
				return `{"title":"","claims":[{"quote":"The exhibition opens on Monday.","text":"The exhibition opens on Monday."}]}`, nil
			}, nil)
			if err != nil || calls != 1 || result.Text != fact+" [1]" {
				t.Fatal("claim changed or generation retried", result, calls, err)
			}
		})
	}
}

func TestAIAnswerEvidenceFirstStreamKeepsQuoteAndProtocolPrivate(t *testing.T) {
	const raw = `{"title":"","claims":[{"quote":"The source records a private supporting span.","text":"A quoted \"answer\" uses café."}]}`
	const want = `A quoted "answer" uses café.`
	for i := 0; i <= len(raw); i++ {
		got := partialAIClaimTexts(raw[:i])
		if len(got) > 1 || (len(got) == 1 && got[0] != want) {
			t.Fatalf("partial claim or private quote leaked at byte%d: %q", i, got)
		}
		if i == len(raw) && (len(got) != 1 || got[0] != want) {
			t.Fatal("complete evidence-first claim did not stream", got)
		}
	}
	for _, raw := range []string{
		`{"title":"","claims":[{"quote":"unterminated`,
		`{"title":"","claims":[{"quote":42,"text":"wrong quote type"}]}`,
		`{"title":"","claims":[{"quote":"source","text":"incomplete \u`,
	} {
		if got := partialAIClaimTexts(raw); len(got) != 0 {
			t.Fatal("invalid or partial evidence-first value streamed", got)
		}
	}
	legacy := `{"title":"","claims":[{"text":"Legacy text.","quote":"A captured supporting sentence."}]}`
	if got := partialAIClaimTexts(legacy); len(got) != 1 || got[0] != "Legacy text." {
		t.Fatal("legacy stream compatibility lost", got)
	}
}

func TestAIAnswerEvidenceFirstPreservesValidationWithoutRepairOrRetry(t *testing.T) {
	const fact = "The exhibition has not been scheduled. The recorded fee is 75 EUR."
	prepared, err := prepareAIContext(context.Background(), AIRequest{Mode: "answer", Prompt: "State what the evidence supports."}, []AIEvidence{{Text: fact}}, nil, "", wordBudget())
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name, raw, want string
		reject          bool
	}{
		{"explicit-negative", `{"title":"","claims":[{"quote":"The exhibition has not been scheduled.","text":"The exhibition has not been scheduled."}]}`, "The exhibition has not been scheduled. [1]", false},
		{"exclusive-abstention", `{"title":"","claims":[{"quote":"","text":"The supplied evidence does not answer this question."}]}`, "The supplied evidence does not answer this question.", false},
		{"unsupported-quote", `{"title":"","claims":[{"quote":"The exhibition opens on Tuesday.","text":"The exhibition opens on Tuesday."}]}`, "", true},
		{"unsupported-number", `{"title":"","claims":[{"quote":"The recorded fee is 75 EUR.","text":"The recorded fee is 76 EUR."}]}`, "", true},
		{"abstention-with-quote", `{"title":"","claims":[{"quote":"The exhibition has not been scheduled.","text":"The supplied evidence does not answer this question."}]}`, "", true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			result, err := GeneratePreparedAI(context.Background(), prepared, func(context.Context, string, string, int, string, func(string)) (string, error) {
				calls++
				return tc.raw, nil
			}, nil)
			if (err != nil) != tc.reject || calls != 1 || (!tc.reject && result.Text != tc.want) {
				t.Fatal("validator altered answer or retried", result, calls, err)
			}
		})
	}
}

func TestAIAnswerEvidenceFirstRetainsExactQuoteChoicesAndGrammarLimit(t *testing.T) {
	sources := []AIEvidence{{Text: "The exhibition opens on Monday. The fee is 75 EUR."}, {Text: "A separate recorded fact remains available."}}
	legacy, err := aiCitedGrammarForSources(sources)
	if err != nil {
		t.Fatal(err)
	}
	current, err := aiCitedGrammarForMode("answer", sources)
	if err != nil {
		t.Fatal(err)
	}
	const marker = "evidence-quote ::= "
	if strings.SplitN(current, marker, 2)[1] != strings.SplitN(legacy, marker, 2)[1] {
		t.Fatal("protocol filtered or manufactured quote choices")
	}
	// Choose a bounded source inventory whose v1 grammar fits but leaves less
	// room than the v2 version marker. A new protocol cannot exceed the cap.
	nearLimit := make([]AIEvidence, 128)
	for i := range nearLimit {
		nearLimit[i].Text = fmt.Sprintf("%03d ", i) + strings.Repeat("x", 400)
	}
	base, err := aiCitedGrammarForSources(nearLimit)
	if err != nil {
		t.Fatal(err)
	}
	// All strings use ASCII without extra escaping or sentence boundaries, so
	// each added byte increases only its one exact quote choice by one byte.
	versionBytes := len(current) - len(legacy)
	target := 64*1024 - versionBytes + 1
	extra := target - len(base)
	if versionBytes <= 0 || extra < 0 || 400+(extra+127)/128 > 508 {
		t.Fatal("invalid near-limit fixture geometry", len(base), versionBytes, extra)
	}
	for i := range nearLimit {
		n := 400 + extra/128
		if i < extra%128 {
			n++
		}
		nearLimit[i].Text = fmt.Sprintf("%03d ", i) + strings.Repeat("x", n)
	}
	old, err := aiCitedGrammarForSources(nearLimit)
	if err != nil || len(old) != target {
		t.Fatal("test did not exercise the exact pre-existing grammar limit", len(old), target, err)
	}
	if got, err := aiCitedGrammarForMode("answer", nearLimit); err == nil || got != "" {
		t.Fatal("version marker escaped fixed grammar byte budget", len(old), len(got), err)
	}

}
