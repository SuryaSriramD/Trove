package workspace

import (
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf8"
)

// The grammar fixes required fields, not property semantics or workspace
// authority. Configs remain generic JSON; canonical schemas validate proposals.
// Output-token limits additionally bound the total emitted document.
func aiDatabaseDraftGrammar() string {
	grammar := `root ::= "{" ws "\"title\"" ws ":" ws title ws "," ws "\"properties\"" ws ":" ws "[" ws (property (ws "," ws property){0,31})? ws "]" ws "," ws "\"view\"" ws ":" ws ("\"table\"" | "\"list\"" | "\"gallery\"") ws "}" ws
property ::= "{" ws "\"name\"" ws ":" ws name ws "," ws "\"type\"" ws ":" ws type ws "," ws "\"config\"" ws ":" ws config-object-0 ws "}"
title ::= "\"" char{1,512} "\""
name ::= "\"" char{1,128} "\""
type ::= "\"" char{1,64} "\""
key ::= "\"" char{0,128} "\""
text ::= "\"" char{0,4000} "\""
scalar ::= text | number | "true" | "false" | "null"
number ::= "-"? ("0" | [1-9] [0-9]{0,18}) ("." [0-9]{1,18})? ([eE] [+-]? [0-9]{1,3})?
char ::= [^"\\\x7F\x00-\x1F] | "\\" (["\\/bfnrt] | "u" [0-9a-fA-F]{4})
ws ::= [ \t\n\r]{0,8}
`
	// Six nested config containers, each with at most 32 members/elements.
	for depth := 0; depth < 6; depth++ {
		grammar += fmt.Sprintf("config-object-%d ::= \"{\" ws (key ws \":\" ws config-value-%d (ws \",\" ws key ws \":\" ws config-value-%d){0,31})? ws \"}\"\n", depth, depth, depth)
		grammar += fmt.Sprintf("config-array-%d ::= \"[\" ws (config-value-%d (ws \",\" ws config-value-%d){0,31})? ws \"]\"\n", depth, depth, depth)
		grammar += fmt.Sprintf("config-value-%d ::= scalar", depth)
		if depth < 5 {
			grammar += fmt.Sprintf(" | config-object-%d | config-array-%d", depth+1, depth+1)
		}
		grammar += "\n"
	}
	return grammar
}

// Validate the generated shape independently of the adapter. The grammar should
// enforce it, but alternate/failing generators must not report a partial shape
// as completed. Actual type/config compatibility is still a kernel proposal gate.
func validateAIDatabaseDraftShape(text string) error {
	invalid := func() error {
		return Fail("WORKER_FAILED", "Database draft must include title, properties, view and complete bounded property definitions")
	}
	if len(text) > 64000 {
		return invalid()
	}
	var draft struct {
		Title      *string `json:"title"`
		Properties *[]struct {
			Name   *string        `json:"name"`
			Type   *string        `json:"type"`
			Config map[string]any `json:"config"`
		} `json:"properties"`
		View *string `json:"view"`
	}
	if err := decodeAIDraft(text, &draft); err != nil {
		return invalid()
	}
	bounded := func(s *string, max int) bool {
		return s != nil && strings.TrimSpace(*s) != "" && utf8.RuneCountInString(*s) <= max
	}
	if !bounded(draft.Title, 512) || draft.Properties == nil || len(*draft.Properties) > 32 || draft.View == nil {
		return invalid()
	}
	if *draft.View != "table" && *draft.View != "list" && *draft.View != "gallery" {
		return invalid()
	}
	var configValue func(any, int) bool
	configValue = func(value any, depth int) bool {
		switch v := value.(type) {
		case map[string]any:
			if depth >= 6 || len(v) > 32 {
				return false
			}
			for key, child := range v {
				if utf8.RuneCountInString(key) > 128 || !configValue(child, depth+1) {
					return false
				}
			}
		case []any:
			if depth >= 6 || len(v) > 32 {
				return false
			}
			for _, child := range v {
				if !configValue(child, depth+1) {
					return false
				}
			}
		case string:
			return utf8.RuneCountInString(v) <= 4000
		case float64, bool, nil, json.Number:
		default:
			return false
		}
		return true
	}
	for _, property := range *draft.Properties {
		if !bounded(property.Name, 128) || !bounded(property.Type, 64) || property.Config == nil || !configValue(property.Config, 0) {
			return invalid()
		}
	}
	return nil
}
