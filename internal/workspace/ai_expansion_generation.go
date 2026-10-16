package workspace

import (
	"context"
	"encoding/json"
	"io"
	"strconv"
	"strings"
	"unicode/utf8"
)

const aiStructuredExpansionVersion = "local-structured-expansion-v1"

// A transport experiment, not an entailment or permission checker. The original
// request and independent all-clause assessment remain authoritative.
const aiStructuredExpansionContract = `
EXPANSION OUTPUT CONTRACT
Return one JSON object with title and paragraphs. Use an empty title unless the user requests a heading. Each paragraph is an ordered array of items. Each item has kind, text and quote, in that order.
FACT ITEMS: Set kind to "fact". Text is one expanded factual statement. Preserve the source's entity, scope, certainty, negation, amounts and timing. Every clause and modifier requires support. Put an exact supporting span copied from one evidence text field in quote. A quoted span or the fact label does not establish that the statement follows from it. Do not add unstated purposes, benefits, explanations, plans or commitments. Missing evidence is not a negative fact. Expand wording and organization while preserving the supplied meaning; do not use repetition to meet length.
SUGGESTION ITEMS: Set kind to "suggestion" and quote to "". Include these only when the user's request permits additional ideas; a no-suggestions request requires none. Text must be visibly optional or conditional and may propose an idea, but must not assert an unrecorded fact, effect or guarantee. The application prefixes this text with "Suggestion: "; do not include that prefix yourself. A suggestion label never makes a factual assertion supported.
LAYOUT: Follow the user's paragraph, sentence, order and length requests. Items become text in their paragraph, separated by a space; paragraphs are separated by a blank line. Keep any requested suggestion in its requested position. Do not invent a heading from source display metadata. Return only JSON, with at most eight paragraphs and twelve items per paragraph. Preserve all requested facts. Do not add an insufficient-evidence statement or an unnecessary suggestion merely to fill the format.
`

func aiExpansionQuoteSpans(sources []AIEvidence) ([]string, error) {
	spans, err := aiEvidenceQuoteSpans(sources)
	if err != nil {
		return nil, err
	}
	// Very short retained records are still valid sources. The shared cited
	// grammar's minimum-length heuristic must not make them unexpandable.
	seen := map[string]bool{}
	for _, span := range spans {
		seen[span] = true
	}
	for _, source := range sources {
		text := strings.TrimSpace(source.Text)
		if text != "" && len(text) < 8 && !seen[text] {
			spans = append(spans, text)
			seen[text] = true
		}
	}
	if len(spans) == 0 || len(spans) > 256 {
		return nil, Fail("RESOURCE_EXHAUSTED", "Exact expansion quote choices exceed the supported context")
	}
	return spans, nil
}

func aiExpansionGrammarForSources(sources []AIEvidence) (string, error) {
	spans, err := aiExpansionQuoteSpans(sources)
	if err != nil {
		return "", err
	}
	choices := make([]string, 0, len(spans))
	for _, span := range spans {
		choices = append(choices, strconv.Quote(string(marshal(span))))
	}
	grammar := `root ::= "{" ws "\"title\"" ws ":" ws heading ws "," ws "\"paragraphs\"" ws ":" ws "[" ws paragraph (ws "," ws paragraph){0,7} ws "]" ws "}" ws
paragraph ::= "[" ws item (ws "," ws item){0,11} ws "]"
item ::= fact | suggestion
fact ::= "{" ws "\"kind\"" ws ":" ws "\"fact\"" ws "," ws "\"text\"" ws ":" ws text ws "," ws "\"quote\"" ws ":" ws evidence-quote ws "}"
suggestion ::= "{" ws "\"kind\"" ws ":" ws "\"suggestion\"" ws "," ws "\"text\"" ws ":" ws text ws "," ws "\"quote\"" ws ":" ws "\"\"" ws "}"
heading ::= "\"" char{0,128} "\""
text ::= "\"" char{1,2000} "\""
char ::= [^"\\\x7F\x00-\x1F] | "\\" (["\\/bfnrt] | "u" [0-9a-fA-F]{4})
ws ::= [ \t\n\r]{0,8}
` + "evidence-quote ::= " + strings.Join(choices, " | ") + "\n"
	if len(grammar) > 64<<10 {
		return "", Fail("RESOURCE_EXHAUSTED", "Expansion quote choices exceed 64 KiB; narrow the source context")
	}
	return grammar, nil
}

func generateAIStructuredExpansion(ctx context.Context, p AIPrepared, generate AIGenerator, onToken func(string)) (AICompletion, error) {
	// Publish only a fully validated draft; incomplete JSON never appears as prose.
	if err := ctx.Err(); err != nil {
		return AICompletion{}, err
	}
	raw, err := generate(ctx, p.System, p.Prompt, p.OutputTokens, p.Grammar, nil)
	if err != nil {
		return AICompletion{}, err
	}
	if err = ctx.Err(); err != nil {
		return AICompletion{}, err
	}
	text, err := renderAIStructuredExpansion(raw, p.Run.Sources)
	if err != nil {
		return AICompletion{}, err
	}
	if err = ctx.Err(); err != nil {
		return AICompletion{}, err
	}
	if onToken != nil {
		onToken(text)
	}
	return AICompletion{Text: text}, nil
}

func renderAIStructuredExpansion(raw string, sources []AIEvidence) (string, error) {
	invalid := func() (string, error) {
		return "", Fail("WORKER_FAILED", "Expansion must be one complete structured draft with exact source quotations")
	}
	if len(raw) > 384<<10 || !utf8.ValidString(raw) || !aiValidJSONSurrogates(raw) {
		return invalid()
	}
	spans, err := aiExpansionQuoteSpans(sources)
	if err != nil {
		return invalid()
	}
	allowed := map[string]bool{}
	for _, span := range spans {
		allowed[span] = true
	}
	d := json.NewDecoder(strings.NewReader(raw))
	expect := func(want any) bool { got, err := d.Token(); return err == nil && got == want }
	read := func() (string, bool) { v, err := d.Token(); s, ok := v.(string); return s, err == nil && ok }
	if !expect(json.Delim('{')) || !expect("title") {
		return invalid()
	}
	title, ok := read()
	if !ok || len(title) > 512 || utf8.RuneCountInString(title) > 128 || strings.ContainsAny(title, "\r\n\x00") || !expect("paragraphs") || !expect(json.Delim('[')) {
		return invalid()
	}
	paragraphs := []string{}
	for d.More() {
		if len(paragraphs) >= 8 || !expect(json.Delim('[')) {
			return invalid()
		}
		items := []string{}
		for d.More() {
			if len(items) >= 12 || !expect(json.Delim('{')) || !expect("kind") {
				return invalid()
			}
			kind, ok := read()
			if !ok || (kind != "fact" && kind != "suggestion") || !expect("text") {
				return invalid()
			}
			text, ok := read()
			if !ok || strings.TrimSpace(text) == "" || len(text) > 8000 || utf8.RuneCountInString(text) > 2000 || strings.ContainsAny(text, "\r\n\x00") || !expect("quote") {
				return invalid()
			}
			quote, ok := read()
			if !ok || !expect(json.Delim('}')) {
				return invalid()
			}
			if kind == "fact" {
				if !allowed[quote] || !aiQuoteSupportsNumbers(text, quote) {
					return invalid()
				}
			} else {
				if quote != "" {
					return invalid()
				}
				text = "Suggestion: " + text
			}
			items = append(items, text)
		}
		if len(items) == 0 || !expect(json.Delim(']')) {
			return invalid()
		}
		paragraphs = append(paragraphs, strings.Join(items, " "))
	}
	if len(paragraphs) == 0 || !expect(json.Delim(']')) || !expect(json.Delim('}')) {
		return invalid()
	}
	if _, err := d.Token(); err != io.EOF {
		return invalid()
	}
	if title != "" {
		paragraphs = append([]string{title}, paragraphs...)
	}
	return strings.Join(paragraphs, "\n\n"), nil
}
