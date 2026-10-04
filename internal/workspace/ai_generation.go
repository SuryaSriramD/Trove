package workspace

import (
	"context"
	"encoding/json"
	"regexp"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

type AIGenerator func(context.Context, string, string, int, string, func(string)) (string, error)
type AICompletion struct{ Text string }

const aiCitedInstructions = ` Return only JSON: {"title":"A short title or the requested heading","claims":[{"text":"One factual line of the answer","quote":"A supporting quote copied from EVIDENCE"}]}. Put headings only in title, not in claims. Include actual facts in claim text, not just labels. Do not put numbered citation labels in title or claim text; the application adds them. Copy supporting quotes without their numbered labels. Do not invent a quote. The application displays quotes with their canonical sources. Preserve approved formatting preferences. If the evidence cannot answer, use exactly {"title":"","claims":[{"text":"The supplied evidence does not answer this question.","quote":""}]}.`

func aiRequiresCitations(mode string) bool {
	return mode == "answer" || mode == "summarize" || mode == "explain"
}

// Cited modes resolve exact quotes before publishing final attribution. A
// provisional stream exposes readable claim text; grammar JSON and supporting
// quotes stay internal. Unsupported quotes leave an unapplied failed draft.
func GeneratePreparedAI(ctx context.Context, prepared AIPrepared, generate AIGenerator, onToken func(string)) (AICompletion, error) {
	if len(prepared.Run.Sources) == 0 || !aiRequiresCitations(prepared.Run.Request.Mode) {
		text, err := generate(ctx, prepared.System, prepared.Prompt, prepared.OutputTokens, "", onToken)
		return AICompletion{Text: text}, err
	}
	var raw strings.Builder
	emitted := 0
	var partial strings.Builder
	stream := func(token string) {
		raw.WriteString(token)
		texts := partialAIClaimTexts(raw.String())
		for emitted < len(texts) {
			piece := texts[emitted]
			if emitted > 0 {
				piece = "\n" + piece
			}
			partial.WriteString(piece)
			if onToken != nil {
				onToken(piece)
			}
			emitted++
		}
	}
	result, err := generate(ctx, prepared.System, prepared.Prompt, prepared.OutputTokens, aiCitationGrammar(), stream)
	out := AICompletion{Text: partial.String()}
	if err != nil {
		return out, err
	}
	out.Text, err = renderAICitedClaims(result, prepared.Run.Sources)
	if err != nil {
		out.Text = partial.String()
		return out, err
	}
	return out, nil
}

// The worker grammar fixes field order. Parse only completed JSON text values;
// never expose a partial escape, supporting quote, or internal object syntax.
func partialAIClaimTexts(raw string) []string {
	d := json.NewDecoder(strings.NewReader(raw))
	texts := []string{}
	expect := func(want any) bool { got, err := d.Token(); return err == nil && got == want }
	if !expect(json.Delim('{')) || !expect("title") {
		return texts
	}
	title, err := d.Token()
	if err != nil {
		return texts
	}
	if heading, ok := title.(string); ok && heading != "" {
		texts = append(texts, heading)
	}
	if !expect("claims") || !expect(json.Delim('[')) {
		return texts
	}
	for d.More() {
		if !expect(json.Delim('{')) || !expect("text") {
			return texts
		}
		value, err := d.Token()
		if err != nil {
			return texts
		}
		text, ok := value.(string)
		if !ok {
			return texts
		}
		texts = append(texts, text)
		if !expect("quote") {
			return texts
		}
		if _, err = d.Token(); err != nil {
			return texts
		}
		if !expect(json.Delim('}')) {
			return texts
		}
	}
	return texts
}
func aiCitationGrammar() string {
	return `root ::= "{" ws "\"title\"" ws ":" ws quote ws "," ws "\"claims\"" ws ":" ws "[" ws claim (ws "," ws claim){0,31} ws "]" ws "}"
claim ::= "{" ws "\"text\"" ws ":" ws string ws "," ws "\"quote\"" ws ":" ws quote ws "}"
string ::= "\"" char{1,2000} "\""
quote ::= "\"" char{0,2000} "\""
char ::= [^"\\\x7F\x00-\x1F] | "\\" (["\\/bfnrt] | "u" [0-9a-fA-F]{4})
ws ::= [ \t\n\r]*
`
}
func renderAICitedClaims(raw string, sources []AIEvidence) (string, error) {
	var result struct {
		Title  string `json:"title"`
		Claims []struct {
			Text  string `json:"text"`
			Quote string `json:"quote"`
		} `json:"claims"`
	}
	if err := decodeAIDraft(raw, &result); err != nil {
		return "", err
	}
	if len(result.Claims) == 0 || len(result.Claims) > 32 {
		return "", Fail("WORKER_FAILED", "Invalid cited answer claim count")
	}
	lines := []string{}
	if len(result.Title) > 512 {
		return "", Fail("WORKER_FAILED", "Cited answer title is too long")
	}
	if result.Title != "" {
		title := result.Title
		for _, source := range sources {
			if len(title) >= 8 && canonicalAIQuote(source.Text, title) != "" {
				title += " [" + strconv.Itoa(source.Number) + "]"
				break
			}
		}
		lines = append(lines, title)
	}
	for _, claim := range result.Claims {
		quote := strings.TrimSpace(claim.Quote)
		if quote == "" && claim.Text == "The supplied evidence does not answer this question." {
			lines = append(lines, claim.Text)
			continue
		}
		if strings.TrimSpace(claim.Text) == "" || len(claim.Text) > 8000 || len(quote) < 8 || len(quote) > 8000 {
			return "", Fail("WORKER_FAILED", "Invalid cited answer claim or supporting quote")
		}
		labels := []string{}
		canonicalQuote := ""
		// Case changes and whitespace wrapping do not alter the words, numbers or
		// punctuation. Display the original matched bytes, not a normalized quote.
		for _, source := range sources {
			if matched := canonicalAIQuote(source.Text, quote); matched != "" {
				labels = append(labels, strconv.Itoa(source.Number))
				if canonicalQuote == "" {
					canonicalQuote = matched
				}
			}
		}
		if len(labels) == 0 {
			return "", Fail("WORKER_FAILED", "The answer's supporting quote is absent from supplied evidence")
		}
		// The model supplies readable evidence, never authoritative source identities.
		// A repeated exact quote can cite every supplied occurrence, up to three.
		if len(labels) > 3 {
			return "", Fail("WORKER_FAILED", "The supporting quote is too ambiguous; use a longer quote")
		}
		line := strings.TrimSpace(claim.Text)
		if !strings.Contains(strings.ToLower(line), strings.ToLower(canonicalQuote)) {
			line += " — “" + canonicalQuote + "”"
		}
		for _, label := range labels {
			line += " [" + label + "]"
		}
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n"), nil
}

// A quote may normalize case and wrapping but must not slice a word or number
// ("2000 USD" is not evidence for that amount inside "12000 USD").
func canonicalAIQuote(source, quote string) string {
	words := strings.Fields(quote)
	if len(words) == 0 {
		return ""
	}
	escaped := make([]string, len(words))
	for i, word := range words {
		escaped[i] = regexp.QuoteMeta(word)
	}
	pattern := regexp.MustCompile("(?i)" + strings.Join(escaped, `\s+`))
	wordRune := func(r rune) bool { return unicode.IsLetter(r) || unicode.IsNumber(r) || unicode.IsMark(r) || r == '_' }
	for _, loc := range pattern.FindAllStringIndex(source, -1) {
		first, _ := utf8.DecodeRuneInString(source[loc[0]:loc[1]])
		last, _ := utf8.DecodeLastRuneInString(source[loc[0]:loc[1]])
		before, _ := utf8.DecodeLastRuneInString(source[:loc[0]])
		after, _ := utf8.DecodeRuneInString(source[loc[1]:])
		if wordRune(first) && wordRune(before) || wordRune(last) && wordRune(after) {
			continue
		}
		return source[loc[0]:loc[1]]
	}
	return ""
}
