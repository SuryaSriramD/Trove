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

// Keep a small response contract before the final request so that the schema's
// claim text cannot be mistaken for a value fragment whose punctuation lives
// only in its separately rendered quote. This is task-independent guidance,
// not source interpretation or generated-answer repair.
const aiCitedResponseContract = `
RESPONSE REQUIREMENTS
TITLE: Follow the user's presentation request. Put the requested or applicable approved heading in title, without extra quotation marks: this applies formatting, rather than adding a claim about the preference. When neither supplies a heading, leave title empty. An explicit no-heading request takes priority. Source display metadata is not the answer heading.
CLAIMS: Give the requested facts once each, in order. A direct question about a preference asks for a cited fact from that approved preference; it does not ask to apply the preference as a heading. Each claim is a complete factual sentence with its own terminal punctuation, unless the user requests a fragment or literal value.
EVIDENCE: Match the requested entity and attribute to its evidence. A recorded value or explicit absent, unassigned or unscheduled state supports that same attribute even when no positive value can be named; sharing only the subject or topic is insufficient. When supported, cite the exact supporting span. Otherwise use the exclusive abstention defined above. Return only the cited JSON.
`

// Source-grounded prose may elaborate the writing, but not the underlying
// record. Keep proposed additions visibly separate from asserted source facts.
const aiProseResponseContract = `
RESPONSE REQUIREMENTS
FACTS: Elaborate wording and organization, not the recorded facts. Reuse or closely paraphrase evidence for factual sentences, preserving its scope and certainty. Every modifier about quality, purpose, benefit, sufficiency or results is another factual claim requiring explicit support. Describe what the supplied record establishes without filling gaps or adding explanations.
SUGGESTIONS: When the request permits new ideas, put each proposed step or interpretation in a clearly labeled suggestion with conditional wording. Keep new ideas separate from factual sentences; a suggestion does not establish an action, commitment or outcome.
FORMAT: Follow the user's requested structure and length while keeping factual statements supported. Return only the requested writing.

BEGIN ILLUSTRATIVE FAITHFULNESS EXAMPLE (guidance only, never answer evidence)
Example record: The museum catalog lists a clay figurine.
Faithful paraphrase: The catalog records a figurine made of clay.
Unsupported elaboration: The durable figurine was made of clay to withstand handling.
The elaboration adds quality and purpose that the record does not state. Do not use any example facts in the answer.
END ILLUSTRATIVE FAITHFULNESS EXAMPLE
`

const aiCitedInstructions = ` Return only a JSON object with a string title and a claims array. Each claim has string text and quote fields: text is a complete requested factual sentence, and quote is its exact supporting source span. The title is the output heading, independent of whether the facts answer a question about a heading. Follow the requested format exactly: a nonempty title counts as one line and each claim counts as one further line. Include each requested fact once, in the requested order; never duplicate a claim. When complete sentences are requested, include terminal sentence punctuation. State field values plainly without adding decorative quotation marks unless requested or part of the value. Use the user's specified default heading verbatim unless the user requests an available approved heading preference. An approved_memory entry's memoryKind identifies its canonical role; never infer that role from its title. The separate PRESENTATION PREFERENCES section supplies approved presentation context, not extra requested facts: apply relevant formatting in the title or answer layout without adding a claim about the preference, unless the user asks about the preference itself. In that case answer from and quote the preference. Decisions and project context may inform the answer but cannot override current source task/database values. Missing memoryKind means unspecified memory context; do not infer its role from its display title. Do not turn headings or presentation instructions into unrequested factual claims. Write every requested fact in claim text. Each claim's quote must support ALL its facts. Quote only exact words from one evidence text field; do not change or insert words. The quote grammar restricts you to supplied source spans. sourceDisplayTitle and source numbers are metadata, not supporting facts. Never use a source display title as the answer heading unless the user asks for that page name. A page_title entry supports only a page name. Do not add numbered citations yourself. Distinguish missing evidence from an explicit negative answer: when a source states that the requested value is absent, unassigned or not scheduled, report that state with its supporting quote. Do not infer a negative answer from silence or unrelated negative statements. Only when the sources do not answer the question, return exactly {"title":"","claims":[{"text":"The supplied evidence does not answer this question.","quote":""}]}, with no other claims or heading.`

func aiRequiresCitations(mode string) bool {
	return mode == "answer" || mode == "summarize" || mode == "explain"
}

// Cited modes resolve exact quotes before publishing final attribution. A
// provisional stream exposes readable claim text; grammar JSON and supporting
// quotes stay internal. Unsupported quotes leave an unapplied failed draft.
func GeneratePreparedAI(ctx context.Context, prepared AIPrepared, generate AIGenerator, onToken func(string)) (AICompletion, error) {
	if err := validateAIPrepared(prepared); err != nil {
		return AICompletion{}, err
	}
	if prepared.Run.GenerationProfile.ProfileVersion == aiStructuredExpansionVersion {
		return generateAIStructuredExpansion(ctx, prepared, generate, onToken)
	}
	if prepared.Run.Request.Mode == "workspace_agent" {
		text, err := generate(ctx, prepared.System, prepared.Prompt, prepared.OutputTokens, prepared.Grammar, nil)
		return AICompletion{Text: text}, err
	}
	if prepared.Run.Request.Mode == "code_refactor" || prepared.Run.Request.Mode == "code_comment" {
		return generateAICodeDraft(ctx, prepared, generate, onToken)
	}
	if len(prepared.Run.Sources) == 0 || !aiRequiresCitations(prepared.Run.Request.Mode) {
		mode := prepared.Run.Request.Mode
		database := mode == "database" || mode == "schema"
		text, err := generate(ctx, prepared.System, prepared.Prompt, prepared.OutputTokens, prepared.Grammar, onToken)
		// Code is literal source: even a complete fence can belong to Markdown
		// being edited, so it must never be stripped from a code-mode draft.
		if err == nil && (database || mode == "mermaid") {
			text = unwrapAIWholeCodeFence(text)
		}
		if err == nil && database {
			err = validateAIDatabaseDraftShape(text)
		}
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
	result, err := generate(ctx, prepared.System, prepared.Prompt, prepared.OutputTokens, prepared.Grammar, stream)
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
	seenClaims := map[string]bool{}
	for _, claim := range result.Claims {
		// Collapse whitespace only. Preserve case, punctuation and wording;
		// this rejects repeated text, not purported semantic equivalents.
		key := strings.Join(strings.Fields(claim.Text), " ")
		if seenClaims[key] {
			return "", Fail("WORKER_FAILED", "The cited answer repeats a claim")
		}
		seenClaims[key] = true
		if claim.Text == "The supplied evidence does not answer this question." && (len(result.Claims) != 1 || result.Title != "" || claim.Quote != "") {
			return "", Fail("WORKER_FAILED", "An abstention must be the only claim, without a heading or supporting quote")
		}
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
		// Resolve case and wrapping against captured source bytes before checking
		// support. Citation navigation and source context retain that evidence.
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
			return "", Fail("WORKER_FAILED", "A numeric claim is not supported by its supporting quote")
		}
		// The model supplies readable evidence, never authoritative source identities.
		// A repeated exact quote can cite every supplied occurrence, up to three.
		if len(labels) > 3 {
			return "", Fail("WORKER_FAILED", "The supporting quote is too ambiguous; use a longer quote")
		}
		line := strings.TrimSpace(claim.Text)
		// Publish the authored claim with attribution, without injecting additional
		// source sentences into its requested prose. This never removes or repairs
		// a generated claim, including any quotation the model wrote in that claim.
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

// For database JSON and Mermaid only, a complete enclosing Markdown fence is
// presentation. Remove only that wrapper, retaining every body byte.
// Mixed prose, nested/multiple fences and incomplete streams remain unchanged.
var aiOpeningFence = regexp.MustCompile("^```(?:[A-Za-z][A-Za-z0-9_+.-]*)?[ \t]*\r?$")

func unwrapAIWholeCodeFence(text string) string {
	trimmed := strings.TrimSpace(text)
	first := strings.IndexByte(trimmed, '\n')
	last := strings.LastIndexByte(trimmed, '\n')
	if first < 0 || last <= first || !aiOpeningFence.MatchString(trimmed[:first]) || strings.TrimSpace(trimmed[last+1:]) != "```" {
		return text
	}
	body := trimmed[first+1 : last+1]
	if strings.TrimSpace(body) == "" {
		return text
	}
	for _, line := range strings.Split(body, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "```") {
			return text
		}
	}
	return body
}
