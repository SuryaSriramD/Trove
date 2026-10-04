package workspace

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
)

func TestPortableCanonicalDatabaseRoundTripRelationsFormulasAndLinkedViews(t *testing.T) {
	k, e := testKernel(t)
	f := newDatabase(t, k, e)
	a, ap := newRecord(t, k, e, f, "A")
	b, bp := newRecord(t, k, e, f, "B")
	number, formula, relation, rollup, text := NewID(), NewID(), NewID(), NewID(), NewID()
	defs := []struct {
		id, kind string
		cfg      any
	}{{number, "number", map[string]any{"format": "decimal"}}, {formula, "formula", derivedConfig("formula", map[string]any{"originalExpression": `prop("Price") * 2`, "translatedExpression": fmt.Sprintf("prop(%q) * 2", number), "importedCachedValue": "captured 99"})}, {relation, "relation", map[string]any{"targetCollectionId": f.collection, "bidirectionalPropertyId": nil}}, {rollup, "rollup", derivedConfig("rollup", map[string]any{"relationPropertyId": relation, "targetPropertyId": formula, "aggregation": "sum"})}, {text, "text", map[string]any{}}}
	for _, d := range defs {
		testOK(t, dbBatch(t, k, e, []any{map[string]any{"kind": "collection.addProperty", "collectionId": f.collection, "propertyId": d.id, "name": d.kind, "type": d.kind, "schemaVersion": 1, "config": d.cfg}}, "collection", f.collection, "property", d.id))
	}
	testOK(t, setValue(t, k, e, f, b, bp, number, "number", map[string]any{"decimal": "12.5", "format": "decimal"}))
	testOK(t, setValue(t, k, e, f, a, ap, text, "text", []any{map[string]any{"text": b}}))
	testOK(t, setValue(t, k, e, f, a, ap, f.status, "status", f.done))
	testOK(t, dbBatch(t, k, e, []any{map[string]any{"kind": "record.setValue", "recordId": a, "propertyId": relation, "value": map[string]any{"type": "relation", "schemaVersion": 1, "value": []string{b}}}}, "page", ap, "record", a, "record", b, "collection", f.collection, "property", relation))
	linked := createPage(t, k, e, "Linked dashboard")
	view := NewID()
	testOK(t, dbBatch(t, k, e, []any{map[string]any{"kind": "view.save", "viewId": view, "collectionId": f.collection, "owningPageId": linked, "name": "Related gallery", "type": "gallery", "schemaVersion": 1, "config": map[string]any{"coverPropertyId": nil, "cardSize": "medium"}, "query": emptyQuery(f.title, formula, rollup)}}, "view", view, "collection", f.collection, "page", linked))
	zip := filepath.Join(t.TempDir(), "database.zip")
	if _, err := k.ExportPortable(context.Background(), e, zip); err != nil {
		t.Fatal(err)
	}
	target, owner := testKernel(t)
	handle, err := target.RegisterSelection(zip)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := previewPortableImport(context.Background(), target, owner, map[string]any{"sourceHandle": handle, "sourceNamespace": "native-database-roundtrip"})
	if err != nil {
		t.Fatal(err)
	}
	plan := raw.(ImportPlan)
	if len(plan.PortableCollections) != 1 {
		t.Fatalf("canonical definitions not selected: %s", marshal(plan.SourceOmissions))
	}
	plan = completeImportFixture(t, target, owner, plan)
	c := plan.PortableCollections[0]
	if c.Title != "Work" {
		t.Fatal("collection title lost", c.Title)
	}
	var prop, record string
	target.db.QueryRow("SELECT id FROM properties WHERE collection_id=? AND type='rollup'", c.CollectionID).Scan(&prop)
	target.db.QueryRow("SELECT r.id FROM records r JOIN pages p ON p.id=r.page_id WHERE r.collection_id=? AND p.title='A'", c.CollectionID).Scan(&record)
	if prop == "" || record == "" {
		t.Fatal("reconstructed schema/record missing")
	}
	result := testOK(t, testDispatch(t, target, owner, "databaseDerived", recoveryRequest(target, map[string]any{"collectionId": c.CollectionID})))
	matched := false
	for _, raw := range result["values"].(map[string]any)[record].([]any) {
		cell := raw.(map[string]any)
		if cell["propertyId"] == prop {
			matched = true
			if textValue(cell["value"]) != "25" {
				t.Fatal("live rollup changed", cell)
			}
		}
	}
	if !matched {
		t.Fatal(result)
	}
	var config string
	target.db.QueryRow("SELECT config_json FROM properties WHERE collection_id=? AND type='formula'", c.CollectionID).Scan(&config)
	var cfg map[string]any
	json.Unmarshal([]byte(config), &cfg)
	evidence := cfg["raw"].(map[string]any)
	if evidence["originalExpression"] != `prop("Price") * 2` || evidence["importedCachedValue"] != "captured 99" || strings.Contains(str(evidence["translatedExpression"]), number) {
		t.Fatal("formula evidence/remapping changed", config)
	}
	var literal string
	target.db.QueryRow("SELECT v.value_json FROM property_values v JOIN properties p ON p.id=v.property_id WHERE v.record_id=? AND p.type='text'", record).Scan(&literal)
	if !strings.Contains(literal, b) {
		t.Fatal("opaque UUID text was rewritten", literal)
	}
	var ownerTitle string
	target.db.QueryRow("SELECT p.title FROM views v JOIN pages p ON p.id=v.owning_page_id WHERE v.name='Related gallery'").Scan(&ownerTitle)
	if ownerTitle != "Linked dashboard" {
		t.Fatal("linked view placement lost", ownerTitle)
	}
	var rules int
	target.db.QueryRow("SELECT count(*) FROM automation_rules").Scan(&rules)
	if rules != 0 {
		t.Fatal("import activated rules")
	}
}
