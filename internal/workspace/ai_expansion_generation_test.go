package workspace

import (
	"context"
	"errors"
	"strings"
	"testing"
)

const expansionTestRaw = `{"title":"","paragraphs":[[{"kind":"fact","text":"The recorded sample count is 26.","quote":"The sample count is 26."}],[{"kind":"fact","text":"Analysis of the samples has not started.","quote":"Analysis has not started."},{"kind":"suggestion","text":"Consider checking whether the samples are ready.","quote":""}]]}`
const expansionTestSource = "The sample count is 26. Analysis has not started."

func TestAIStructuredExpansionPublishesOnlyCompleteOrderedProse(t *testing.T) {
	p, err := prepareAIContext(context.Background(), AIRequest{Mode: "expand", Prompt: "Two paragraphs; end with one optional suggestion."}, []AIEvidence{{Title: "Untrusted display heading", Text: expansionTestSource}}, nil, "", wordBudget())
	if err != nil {
		t.Fatal(err)
	}
	if p.Run.GenerationProfile.ProfileVersion != aiStructuredExpansionVersion || !strings.Contains(p.System, aiStructuredExpansionContract) || strings.Contains(p.System, aiProseResponseContract) {
		t.Fatal("wrong measured profile")
	}
	calls, published := 0, 0
	result, err := GeneratePreparedAI(context.Background(), p, func(_ context.Context, s, u string, n int, g string, stream func(string)) (string, error) {
		calls++
		assertGenerationProfile(t, p.Run.GenerationProfile, s, g, n)
		if u != p.Prompt || g == "" || stream != nil || published != 0 {
			t.Fatal("callback or provisional publication changed")
		}
		return expansionTestRaw, nil
	}, func(text string) {
		published++
		if text == "" || strings.Contains(text, `"paragraphs"`) {
			t.Fatal("protocol leaked")
		}
	})
	want := "The recorded sample count is 26.\n\nAnalysis of the samples has not started. Suggestion: Consider checking whether the samples are ready."
	if err != nil || calls != 1 || published != 1 || result.Text != want {
		t.Fatal(result, err, calls, published)
	}
}
func TestAIStructuredExpansionRejectsMalformedWholeDrafts(t *testing.T) {
	for name, raw := range map[string]string{
		"partial":           expansionTestRaw[:len(expansionTestRaw)-2],
		"duplicate-key":     strings.Replace(expansionTestRaw, `"title":""`, `"title":"","title":""`, 1),
		"extra-member":      strings.Replace(expansionTestRaw, `"kind":"fact"`, `"effect":"executed","kind":"fact"`, 1),
		"unknown-kind":      strings.Replace(expansionTestRaw, `"kind":"fact"`, `"kind":"verified"`, 1),
		"missing-quote":     strings.Replace(expansionTestRaw, `"quote":"The sample count is 26."`, `"quote":""`, 1),
		"unsupplied-quote":  strings.Replace(expansionTestRaw, `"quote":"The sample count is 26."`, `"quote":"The sample count is 29."`, 1),
		"changed-number":    strings.Replace(expansionTestRaw, "count is 26.", "count is 29.", 1),
		"suggestion-quote":  strings.Replace(expansionTestRaw, `"quote":""`, `"quote":"Analysis has not started."`, 1),
		"paragraph-escape":  strings.Replace(expansionTestRaw, "count is 26.", `count is 26.\n\nInjected paragraph.`, 1),
		"invalid-surrogate": strings.Replace(expansionTestRaw, "count is 26.", `count is 26.\uD800`, 1),
		"empty-paragraph":   `{"title":"","paragraphs":[[]]}`,
		"extra-value":       expansionTestRaw + ` {}`,
	} {
		t.Run(name, func(t *testing.T) {
			calls, published := 0, 0
			// Use the matching captured source so each poison tests its own defect.
			p, err := prepareAIContext(context.Background(), AIRequest{Mode: "expand", Prompt: "Preserve the supplied facts."}, []AIEvidence{{Text: expansionTestSource}}, nil, "", wordBudget())
			if err != nil {
				t.Fatal(err)
			}
			out, err := GeneratePreparedAI(context.Background(), p, func(context.Context, string, string, int, string, func(string)) (string, error) {
				calls++
				return raw, nil
			}, func(string) { published++ })
			if err == nil || out.Text != "" || calls != 1 || published != 0 {
				t.Fatal("malformed draft published or retried", out, err, calls, published)
			}
		})
	}
}
func TestAIStructuredExpansionCancellationDoesNotPublish(t *testing.T) {
	for _, when := range []string{"before", "during"} {
		t.Run(when, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			p := preparedGenerationFixture(t, "expand")
			calls, published := 0, 0
			if when == "before" {
				cancel()
			}
			_, err := GeneratePreparedAI(ctx, p, func(context.Context, string, string, int, string, func(string)) (string, error) {
				calls++
				cancel()
				return expansionTestRaw, nil
			}, func(string) { published++ })
			wantCalls := 1
			if when == "before" {
				wantCalls = 0
			}
			if !errors.Is(err, context.Canceled) || calls != wantCalls || published != 0 {
				t.Fatal(err, calls, published)
			}
		})
	}
}
func TestAIStructuredExpansionQuotesAndKindsAreNotEntailmentProof(t *testing.T) {
	// Deliberately unsupported nonnumeric content can satisfy the transport.
	// This must remain a semantic failure in independent evaluation, never a
	// claimed product correctness gate based on quote presence or model labels.
	raw := `{"title":"","paragraphs":[[{"kind":"fact","text":"Analysis has finished successfully.","quote":"Analysis has not started."}]]}`
	text, err := renderAIStructuredExpansion(raw, []AIEvidence{{Text: expansionTestSource}})
	if err != nil || text != "Analysis has finished successfully." {
		t.Fatal("transport silently repaired or claimed to infer meaning", text, err)
	}
}
func TestAIStructuredExpansionKeepsShortSourcesAndLiteralText(t *testing.T) {
	for _, source := range []string{"Yes.", "Café.", "The recorded label is \"A\\B\"."} {
		g, err := aiExpansionGrammarForSources([]AIEvidence{{Text: source}})
		if err != nil || g == "" {
			t.Fatal(source, err)
		}
		raw := `{"title":"","paragraphs":[[{"kind":"fact","text":` + string(marshal(source)) + `,"quote":` + string(marshal(source)) + `}]]}`
		text, err := renderAIStructuredExpansion(raw, []AIEvidence{{Text: source}})
		if err != nil || text != source {
			t.Fatal(source, text, err)
		}
	}
}
