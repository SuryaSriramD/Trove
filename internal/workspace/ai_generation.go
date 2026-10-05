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

const aiCitedInstructions = ` Return only JSON {"title":"the requested heading","claims":[{"text":"one requested factual line","quote":"its exact supporting source span"}]}. Follow the requested format exactly: title counts as one line and each claim counts as one further line. Never duplicate a claim. Use the user's specified default heading verbatim unless the user requests an available approved heading preference. Write every requested fact in claim text. Each claim's quote must support ALL its facts. Quote only exact words from one evidence text field; do not change or insert words. The quote grammar restricts you to supplied source spans. Evidence title and source numbers are metadata, not supporting facts. A page_title entry supports only a page name. Current content overrides historical memories for current facts. Do not add numbered citations yourself. If evidence is insufficient, return {"title":"","claims":[{"text":"The supplied evidence does not answer this question.","quote":""}]}.`

func aiRequiresCitations(mode string) bool {
	return mode == "answer" || mode == "summarize" || mode == "explain"
}

// Cited modes resolve exact quotes before publishing final attribution. A
// provisional stream exposes readable claim text; grammar JSON and supporting
// quotes stay internal. Unsupported quotes leave an unapplied failed draft.
func GeneratePreparedAI(ctx context.Context, prepared AIPrepared, generate AIGenerator, onToken func(string)) (AICompletion, error) {
	if prepared.Run.Request.Mode == "workspace_agent" {
		text, err := generate(ctx, prepared.System, prepared.Prompt, prepared.OutputTokens, actionPlanGrammar(), nil)
		return AICompletion{Text: text}, err
	}
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
	grammar, err := aiCitedGrammarForSources(prepared.Run.Sources)
	if err != nil {
		return AICompletion{}, err
	}
	result, err := generate(ctx, prepared.System, prepared.Prompt, prepared.OutputTokens, grammar, stream)
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
claim ::= "{" ws "\"text\"" ws ":" ws string ws "," ws "\"quote\"" ws ":" ws evidence-quote ws "}"
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
		if !aiQuoteSupportsNumbers(claim.Text, canonicalQuote) {
			return "", Fail("WORKER_FAILED", "A numeric claim is not supported by its displayed quote")
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

// The model chooses from bounded exact spans; it cannot paraphrase a quoted
// number or invent a prefix. Rendering still independently resolves each span
// to captured sources and the task rubric still checks claim support.
func aiCitedGrammarForSources(sources []AIEvidence) (string, error) {
	spans, err := aiEvidenceQuoteSpans(sources)
	if err != nil {
		return "", err
	}
	alternatives := []string{strconv.Quote(string(marshal("")))}
	for _, span := range spans {
		alternatives = append(alternatives, strconv.Quote(string(marshal(span))))
	}
	grammar := aiCitationGrammar() + "evidence-quote ::= " + strings.Join(alternatives, " | ") + "\n"
	if len(grammar) > 64<<10 {
		return "", Fail("RESOURCE_EXHAUSTED", "Exact evidence quote choices exceed 64 KiB; narrow the source context")
	}
	return grammar, nil
}
func aiEvidenceQuoteSpans(sources []AIEvidence) ([]string, error) {
	spans := []string{}
	seen := map[string]bool{}
	currentSource := ""
	add := func(text string) error {
		text = strings.TrimSpace(text)
		// A truncation marker is presentation, not copied source evidence.
		text = strings.TrimSuffix(text, "…")
		if len(text) < 8 || utf8.RuneCountInString(text) > 512 || seen[text] || canonicalAIQuote(currentSource, text) == "" {
			return nil
		}
		if len(spans) >= 256 {
			return Fail("RESOURCE_EXHAUSTED", "More than 256 exact quote choices; narrow the source context")
		}
		seen[text] = true
		spans = append(spans, text)
		return nil
	}
	for _, source := range sources {
		currentSource = source.Text
		if err := add(source.Text); err != nil {
			return nil, err
		}
		runes := []rune(source.Text)
		start := 0
		for i, r := range runes {
			boundary := r == '\n' || (strings.ContainsRune(".!?:;", r) && (i+1 == len(runes) || unicode.IsSpace(runes[i+1])))
			if !boundary && i-start+1 < 512 && i+1 < len(runes) {
				continue
			}
			end := i + 1
			if !boundary && end < len(runes) {
				for end > start && !unicode.IsSpace(runes[end-1]) {
					end--
				}
				if end == start {
					end = i + 1
				}
			}
			if err := add(string(runes[start:end])); err != nil {
				return nil, err
			}
			start = end
		}
		// Include a short final tail together with preceding context instead of
		// silently making the final words unavailable as a quote choice.
		if len(runes) > 512 {
			tail := max(0, len(runes)-512)
			for tail < len(runes) && tail > 0 && !unicode.IsSpace(runes[tail-1]) {
				tail++
			}
			if err := add(string(runes[tail:])); err != nil {
				return nil, err
			}
		}
	}
	return spans, nil
}

var aiClaimNumberPattern = regexp.MustCompile(`[0-9]+(?:[.,][0-9]+)*`)

// This narrow invariant prevents an observed misleading citation: a budget
// amount attached to a quote about a date. It is not semantic entailment or
// support for inferred arithmetic; numeric literals retain their source form.
func aiQuoteSupportsNumbers(claim, quote string) bool {
	word := func(r rune) bool { return unicode.IsLetter(r) || unicode.IsNumber(r) || unicode.IsMark(r) || r == '_' }
	for _, loc := range aiClaimNumberPattern.FindAllStringIndex(claim, -1) {
		before, _ := utf8.DecodeLastRuneInString(claim[:loc[0]])
		after, _ := utf8.DecodeRuneInString(claim[loc[1]:])
		if word(before) || word(after) {
			continue
		}
		if canonicalAIQuote(quote, claim[loc[0]:loc[1]]) == "" {
			return false
		}
	}
	return true
}
