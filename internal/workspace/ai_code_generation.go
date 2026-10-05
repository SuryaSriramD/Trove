package workspace

import (
	"context"
	"encoding/json"
	"io"
	"strconv"
	"strings"
	"unicode/utf8"
)

const aiCodeDraftInstructions = `Return only JSON {"source":"the complete replacement source"}. Put literal source in the source string, with JSON escaping. Do not add presentation fences or explanatory prose to source. Retain any fences, quotes, whitespace and escapes that belong to the source itself. Maximum 16000 source characters and 64000 UTF-8 bytes. Evidence text fields contain source; titles, kind, source numbers and EVIDENCE/USER REQUEST headings are metadata, never replacement source. If a selection is supplied, replace only that selection. Never execute code.`

// Transport syntax is separated from source. No trimming, fence stripping or
// newline normalization is performed on the decoded string.
func aiCodeDraftGrammar() string {
	return `root ::= "{" ws "\"source\"" ws ":" ws "\"" char{1,16000} "\"" ws "}" ws
char ::= [^"\\\x7F\x00-\x1F] | "\\" (["\\/bfnrt] | "u" [0-9a-fA-F]{4})
ws ::= [ \t\n\r]{0,8}
`
}

func generateAICodeDraft(ctx context.Context, prepared AIPrepared, generate AIGenerator, onToken func(string)) (AICompletion, error) {
	// A partial JSON string is not source; publish only after complete validation.
	raw, err := generate(ctx, prepared.System, prepared.Prompt, prepared.OutputTokens, aiCodeDraftGrammar(), nil)
	if err != nil {
		return AICompletion{}, err
	}
	if err = ctx.Err(); err != nil {
		return AICompletion{}, err
	}
	source, err := decodeAICodeDraft(raw)
	if err != nil {
		return AICompletion{}, err
	}
	if err = ctx.Err(); err != nil {
		return AICompletion{}, err
	}
	if onToken != nil {
		onToken(source)
	}
	return AICompletion{Text: source}, nil
}

func decodeAICodeDraft(raw string) (string, error) {
	invalid := func() (string, error) {
		return "", Fail("WORKER_FAILED", "Code draft must contain one complete source string; incomplete or malformed transport was not published")
	}
	if len(raw) > 384<<10 || !utf8.ValidString(raw) || !aiValidJSONSurrogates(raw) {
		return invalid()
	}
	d := json.NewDecoder(strings.NewReader(raw))
	expect := func(want any) bool { v, e := d.Token(); return e == nil && v == want }
	if !expect(json.Delim('{')) || !expect("source") {
		return invalid()
	}
	v, err := d.Token()
	if err != nil {
		return invalid()
	}
	source, ok := v.(string)
	if !ok || len(source) == 0 || len(source) > 64000 || utf8.RuneCountInString(source) > 16000 || strings.ContainsRune(source, 0) || !expect(json.Delim('}')) {
		return invalid()
	}
	if _, err = d.Token(); err != io.EOF {
		return invalid()
	}
	return source, nil
}

// encoding/json replaces lone UTF-16 surrogates with U+FFFD. Reject them before
// decoding so malformed model output cannot silently change source characters.
func aiValidJSONSurrogates(raw string) bool {
	for i := 0; i < len(raw); i++ {
		if raw[i] != '\\' {
			continue
		}
		i++
		if i >= len(raw) {
			return false
		}
		if raw[i] != 'u' {
			continue
		}
		if i+4 >= len(raw) {
			return false
		}
		n, e := strconv.ParseUint(raw[i+1:i+5], 16, 16)
		if e != nil {
			return false
		}
		i += 4
		if n >= 0xdc00 && n <= 0xdfff {
			return false
		}
		if n >= 0xd800 && n <= 0xdbff {
			if i+6 >= len(raw) || raw[i+1:i+3] != `\u` {
				return false
			}
			low, e := strconv.ParseUint(raw[i+3:i+7], 16, 16)
			if e != nil || low < 0xdc00 || low > 0xdfff {
				return false
			}
			i += 6
		}
	}
	return true
}
