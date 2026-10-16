package workspace

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestAIDatabaseModesRequireGrammarAndCompleteShape(t *testing.T) {
	valid := `{"title":"Roadmap","properties":[{"name":"Phase","type":"select","config":{"options":[{"id":"3b88f280-e606-7d20-8dfb-01faab99acb4","label":"Planned"}]}}],"view":"gallery"}`
	for _, mode := range []string{"database", "schema"} {
		p := preparedGenerationFixture(t, mode)
		for _, raw := range []string{valid,
			`{"properties":[],"view":"table"}`,
			`{"title":"Missing view","properties":[]}`,
			`{"title":"Missing properties","view":"table"}`,
			`{"title":"Incomplete property","properties":[{"name":"Cost","type":"number"}],"view":"table"}`,
			`{"title":"Wrong config shape","properties":[{"name":"Cost","type":"number","config":[]}],"view":"table"}`,
			`{"title":"Unknown field","properties":[],"view":"table","execute":true}`,
		} {
			var stream strings.Builder
			completion, err := GeneratePreparedAI(context.Background(), p, func(_ context.Context, _, _ string, _ int, grammar string, onToken func(string)) (string, error) {
				if grammar == "" || len(grammar) > 64<<10 {
					t.Fatal("missing or unbounded database grammar")
				}
				for _, field := range []string{`\"title\"`, `\"properties\"`, `\"view\"`, `\"name\"`, `\"type\"`, `\"config\"`} {
					if !strings.Contains(grammar, field) {
						t.Fatalf("required field %s absent", field)
					}
				}
				onToken(raw)
				return raw, nil
			}, func(piece string) { stream.WriteString(piece) })
			if (err == nil) != (raw == valid) {
				t.Fatalf("%s malformed shape result %q: %v", mode, raw, err)
			}
			if stream.String() != raw || completion.Text != raw {
				t.Fatal("raw stream or failed draft was silently discarded")
			}
		}
	}
}

func TestAIDatabaseGrammarAllowsAdvancedConfigButKernelRejectsInvalidSemantics(t *testing.T) {
	k, e := testKernel(t)
	ctx := context.Background()
	for _, valid := range []bool{true, false} {
		prepared, err := k.PrepareAI(ctx, e, AIRequest{Mode: "schema", Prompt: "Draft a select property"})
		if err != nil {
			t.Fatal(err)
		}
		config := `{"options":[{"id":"3b88f280-e606-7d20-8dfb-01faab99acb4","label":"Planned"}]}`
		if !valid {
			config = `{"options":"not an array"}`
		}
		raw := `{"title":"Roadmap","properties":[{"name":"Phase","type":"select","config":` + config + `}],"view":"table"}`
		if err = validateAIDatabaseDraftShape(raw); err != nil {
			t.Fatal("generic shape excluded advanced property config", err)
		}
		run, err := k.FinishAI(ctx, e, prepared.Run.ID, raw, "completed", "")
		if err != nil {
			t.Fatal(err)
		}
		_, err = prepareAIAction(ctx, k, e, map[string]any{"runId": run.ID, "kind": "create_database"})
		if (err == nil) != valid {
			t.Fatalf("canonical select config gate valid=%v: %v", valid, err)
		}
		var count int
		k.db.QueryRow("SELECT count(*) FROM collections").Scan(&count)
		if count != 0 {
			t.Fatal("draft or review mutated canonical data")
		}
	}
}

func TestAIDatabaseGeneratedShapeBounds(t *testing.T) {
	property := map[string]any{"name": "Cost", "type": "number", "config": map[string]any{"format": "decimal"}}
	check := func(properties []any, valid bool) {
		raw, _ := json.Marshal(map[string]any{"title": "Limits", "properties": properties, "view": "table"})
		if err := validateAIDatabaseDraftShape(string(raw)); (err == nil) != valid {
			t.Fatalf("shape bound: %v", err)
		}
	}
	properties := []any{}
	for i := 0; i < 32; i++ {
		properties = append(properties, property)
	}
	check(properties, true)
	check(append(properties, property), false)
	config := map[string]any{}
	for i := 0; i < 6; i++ {
		config = map[string]any{"nested": config}
	}
	check([]any{map[string]any{"name": "Deep", "type": "formula", "config": config}}, false)
	check([]any{map[string]any{"name": "Long", "type": "formula", "config": map[string]any{"expression": strings.Repeat("x", 4001)}}}, false)
}
