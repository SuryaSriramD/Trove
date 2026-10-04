package workspace

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"
	"trove.local/phase0/contracts"
)

func TestFormulaV2IndependentCompatibilityFixtures(t *testing.T) {
	at, _ := time.Parse(time.RFC3339, "2024-03-10T07:30:00Z")
	clock := FormulaClock{at, "America/New_York"}
	fixtures := []struct{ expression, want string }{
		{`format(dateBetween(date("2024-01-01T00:00:00.5Z"),date("2024-01-01T00:00:00Z"),"minutes"))`, `0.008333333333`},
		{`format(dateAdd(date("2024-11-02T05:30:00Z"),1,"days"))`, `2024-11-03T01:30:00-04:00`},
		{`format(9007199254740993 + 0.000001)`, `9007199254740993.000001`}, {`format(-8 % 3)`, `-2`}, {`format(toNumber("0.1") + 0.2)`, `0.3`},
		{`join(list(1, 2, 3), "|")`, `1|2|3`}, {`first(list("a","b"))`, `a`}, {`last(list("a","b"))`, `b`}, {`at(list("a","b"),1)`, `b`}, {`format(at(list(1),9))`, ``},
		{`join(slice(list("a","b","c"),1,5),"-")`, `b-c`}, {`join(unique(list("b","a","b")),"-")`, `b-a`}, {`join(sort(list(12,2,1)),"-")`, `1-2-12`}, {`join(reverse(list("a","b")),"-")`, `b-a`}, {`format(length(list(true,false)))`, `2`}, {`contains(list(0.1+0.2),0.3)`, `true`},
		{`regexMatch("Notes 42", "[0-9]+")`, `true`}, {`regexReplace("a12b34", "[0-9]+", "$1")`, `a$1b$1`},
		{`format(date("2024-02-29"))`, `2024-02-29`}, {`format(dateStart(date("2024-03-10T07:30:00Z")))`, `2024-03-10T03:30:00-04:00`}, {`format(dateEnd(date("2024-02-29")))`, `2024-02-29`},
		{`format(dateAdd(date("2024-01-31"),1,"months"))`, `2024-02-29`}, {`format(dateAdd(date("2024-02-29"),1,"years"))`, `2025-02-28`}, {`format(dateAdd(date("2024-03-09T17:00:00Z"),1,"days"))`, `2024-03-10T12:00:00-04:00`},
		{`format(dateBetween(date("2024-03-10T16:00:00Z"),date("2024-03-09T17:00:00Z"),"hours"))`, `23`}, {`format(dateBetween(date("2024-03-10T16:00:00Z"),date("2024-03-09T17:00:00Z"),"days"))`, `1`}, {`formatDate(date("2024-03-10T07:30:00Z"),"YYYY/MM/DD HH:mm:ss Z")`, `2024/03/10 03:30:00 -04:00`},
		{`name(prop("reference"))`, `A page`}, {`id(prop("reference"))`, `page-id`},
		{`ifElse(true,"chosen",1/0)`, `chosen`}, {`today()`, `2024-03-10`}, {`now()`, `2024-03-10T03:30:00-04:00`}, {`empty(null)`, `true`}, {`empty(0)`, `false`}, {`lower("TROVE")`, `trove`}, {`upper("Trove")`, `TROVE`}, {`format(length("తెలుగు"))`, `6`}, {`contains("Trove","rov")`, `true`}, {`concat("a","b")`, `ab`}, {`format(abs(-2.5))`, `2.5`}, {`format(round(-2.5))`, `-3`}, {`format(min(3,1,2))`, `1`}, {`format(max(3,1,2))`, `3`},
		{`format((12+3)*4/2-1)`, `29`}, {`1 == 1 && 2 != 3 && 1 < 2 && 2 <= 2 && 3 > 2 && 3 >= 3 && !false`, `true`}, {`false || true`, `true`},
	}
	tested := map[string]bool{}
	for _, fixture := range fixtures {
		t.Run(fixture.expression, func(t *testing.T) {
			for name := range formulaArity {
				if strings.Contains(fixture.expression, name+"(") {
					tested[name] = true
				}
			}
			value, err := EvaluateFormulaVersion(fixture.expression, 2, clock, func(id string) (any, error) {
				return map[string]any{"kind": "page", "id": "page-id", "name": "A page"}, nil
			})
			if err != nil || textValue(value) != fixture.want {
				t.Fatalf("got %v / %v; want %q", value, err, fixture.want)
			}
		})
	}
	for name := range formulaArity {
		if !tested[name] {
			t.Errorf("published function lacks independent fixture: %s", name)
		}
	}
	for _, expression := range []string{`list(1,"2")`, `list(list(1))`, `sort(list(true,false))`, `sort(list(true))`, `toNumber("1,000")`, `1.5 % 2`, `1 % 0`, `regexMatch("aa", "(?=a)")`, `regexReplace("a", "a", 1)`, `formatDate(date("2024-01-01"),"MMMM")`, `date("2024-02-30")`, `dateAdd(date("2024-03-09T07:30:00Z"),1,"days")`, `dateAdd(date("2024-01-01"),1,"hours")`, `dateBetween(date("2024-01-01"),date("2024-01-01"),"months")`, `name(1)`, `prop("x").name`, `unknown(1)`, `ifElse(true,1,unknown(1))`, `now(1)`, `0xff`, `1_000`} {
		if _, err := EvaluateFormulaVersion(expression, 2, clock, nil); err == nil {
			t.Errorf("accepted unsupported expression %s", expression)
		}
	}
	if _, err := EvaluateFormula(`list(1)`, clock, nil); err == nil {
		t.Fatal("v1 silently accepted v2")
	}
	if _, err := EvaluateFormulaVersion("1", 3, clock, nil); err == nil {
		t.Fatal("future version accepted")
	}
	for _, expr := range []string{`regexReplace(prop("large"),"a","aa")`, `regexMatch("a","` + strings.Repeat("a", 1025) + `")`, `join(list(prop("large"),prop("large")),"")`} {
		if _, err := EvaluateFormulaVersion(expr, 2, clock, func(string) (any, error) { return strings.Repeat("a", 65536), nil }); err == nil {
			t.Fatal("limit not enforced", expr)
		}
	}
}
func TestFormulaV2MemoAndListWorkLimit(t *testing.T) {
	clock := FormulaClock{time.Now(), "UTC"}
	calls := 0
	value, err := EvaluateFormulaVersion(`concat(first(prop("text")),last(prop("text")))`, 2, clock, func(string) (any, error) { calls++; return []any{"a", "b"}, nil })
	if err != nil || value != "ab" || calls != 1 {
		t.Fatal(value, err, calls)
	}
	values := []string{}
	for i := 0; i < 256; i++ {
		values = append(values, fmt.Sprint(i))
	}
	if _, err := EvaluateFormulaVersion("unique(list("+strings.Join(values, ",")+"))", 2, clock, nil); err == nil || !strings.Contains(err.Error(), "work limit") {
		t.Fatal("list work limit missing", err)
	}
	if _, _, err := formulaCompileV2("0xff"); err == nil {
		t.Fatal("unsupported numeric literal compiled")
	}
}

func TestFormulaV2PreviewSaveProvenanceScopeAndReopen(t *testing.T) {
	k, e := testKernel(t)
	f := newDatabase(t, k, e)
	a, ap := newRecord(t, k, e, f, "Alpha")
	b, bp := newRecord(t, k, e, f, "Beta")
	formula, relation, text, people := NewID(), NewID(), NewID(), NewID()
	defs := []struct {
		id, kind string
		config   any
	}{{formula, "formula", derivedConfig("formula", map[string]any{"originalExpression": "source(original)", "translatedExpression": "1", "importedCachedValue": "old 99"})}, {relation, "relation", map[string]any{"targetCollectionId": f.collection, "bidirectionalPropertyId": nil}}, {text, "text", map[string]any{}}, {people, "identity", map[string]any{}}}
	for _, p := range defs {
		testOK(t, dbBatch(t, k, e, []any{map[string]any{"kind": "collection.addProperty", "collectionId": f.collection, "propertyId": p.id, "name": p.kind, "type": p.kind, "schemaVersion": 1, "config": p.config}}, "collection", f.collection, "property", p.id))
	}
	testOK(t, setValue(t, k, e, f, a, ap, text, "text", []any{map[string]any{"text": "Hello "}, map[string]any{"text": "world"}}))
	testOK(t, dbBatch(t, k, e, []any{map[string]any{"kind": "record.setValue", "recordId": a, "propertyId": relation, "value": map[string]any{"type": "relation", "schemaVersion": 1, "value": []string{b}}}}, "page", ap, "record", a, "collection", f.collection, "property", relation, "record", b))
	testOK(t, setValue(t, k, e, f, a, ap, people, "identity", []any{map[string]any{"sourceSystem": "fixture", "externalKind": "person", "externalId": "ada", "displayName": "Ada", "raw": map[string]any{}}}))
	request := func(expression string) map[string]any {
		return map[string]any{"contractVersion": 1, "requestId": NewID(), "workspaceId": k.ID(), "propertyId": formula, "recordId": a, "expression": expression, "evaluatorVersion": 2, "at": "2024-03-10T07:30:00Z", "timezone": "UTC"}
	}
	expression := fmt.Sprintf(`concat(prop(%q), ": ",name(first(prop(%q)))," / ",name(first(prop(%q))))`, text, relation, people)
	preview := testOK(t, testDispatch(t, k, e, "databaseFormulaPreview", request(expression)))
	if preview["status"] != "computed" || preview["value"] != "Hello world: Beta / Ada" {
		t.Fatal(preview)
	}
	scoped := k.Grant(e.Actor, "user", contracts.DomainScope{Value: contracts.DomainScopeVariant3{Kind: "pages", WorkspaceId: contracts.DomainId(k.ID()), PageIds: []contracts.DomainId{contracts.DomainId(f.page), contracts.DomainId(ap)}}})
	denied := testOK(t, testDispatch(t, k, scoped, "databaseFormulaPreview", request(expression)))
	if denied["status"] != "error" || !strings.Contains(str(denied["diagnostic"]), "scope") {
		t.Fatal("relation scope leaked", denied, bp)
	}
	command := map[string]any{"kind": "property.setFormula", "propertyId": formula, "expression": expression, "evaluatorVersion": 2}
	testCode(t, dbBatch(t, k, e, []any{command}, "property", formula, "collection", f.collection, "page", f.page), "INVALID_REQUEST")
	stale := testRequest(k, e, []any{command}, dbGuards(t, k, "property", formula, "collection", f.collection, "page", f.page, "property", text, "property", relation, "property", people)...)
	testOK(t, dbBatch(t, k, e, []any{command}, "property", formula, "collection", f.collection, "page", f.page, "property", text, "property", relation, "property", people))
	testCode(t, testDispatch(t, k, e, "batch", stale), "CONFLICT")
	command["expression"] = fmt.Sprintf("prop(%q)", formula)
	testCode(t, dbBatch(t, k, e, []any{command}, "property", formula, "collection", f.collection, "page", f.page), "INVALID_REQUEST")
	command["expression"] = "unsupported(1)"
	testCode(t, dbBatch(t, k, e, []any{command}, "property", formula, "collection", f.collection, "page", f.page), "INVALID_REQUEST")
	path := k.Path()
	k.Close()
	k, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer k.Close()
	p, err := propertyDefinition(context.Background(), k.db, formula)
	if err != nil {
		t.Fatal(err)
	}
	var cfg map[string]any
	json.Unmarshal(p.Config, &cfg)
	raw := cfg["raw"].(map[string]any)
	if raw["originalExpression"] != "source(original)" || raw["importedCachedValue"] != "old 99" || raw["translatedExpression"] != expression || number(raw["evaluatorVersion"]) != 2 || raw["translatedAST"] == nil || len(str(raw["expressionHash"])) != 64 {
		t.Fatal("source/evaluation evidence conflated", raw)
	}
	result := testOK(t, queryDB(t, k, k.OwnerContext(), f.collection, "", 200, ""))
	records := result["records"].([]any)
	found := false
	for _, record := range records {
		if record.(map[string]any)["id"] != a {
			continue
		}
		for _, v := range record.(map[string]any)["values"].([]any) {
			cell := v.(map[string]any)
			if cell["propertyId"] == formula {
				r := cell["value"].(map[string]any)["raw"].(map[string]any)
				if r["value"] != "Hello world: Beta / Ada" || number(r["evaluatorVersion"]) != 2 {
					t.Fatal(r)
				}
				found = true
			}
		}
	}
	if !found {
		t.Fatal("derived v2 value missing after reopen")
	}
}
