package workspace

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
)

func derivedConfig(kind string, raw map[string]any) map[string]any {
	return map[string]any{"sourceType": "fixture." + kind, "sourceVersion": 1, "readableText": kind, "reason": "unsupported_behavior", "raw": raw}
}
func TestAdvancedFormulaEvidenceRollupAndInvalidation(t *testing.T) {
	k, e := testKernel(t)
	f := newDatabase(t, k, e)
	a, ap := newRecord(t, k, e, f, "A")
	b, bp := newRecord(t, k, e, f, "B")
	num, formula, relation, rollup, bad := NewID(), NewID(), NewID(), NewID(), NewID()
	configs := []struct {
		id, kind string
		config   any
	}{{num, "number", map[string]any{"format": "decimal"}}, {formula, "formula", derivedConfig("formula", map[string]any{"originalExpression": "source calculation", "translatedExpression": fmt.Sprintf("prop(%q) * 2", num), "importedCachedValue": "old captured 99"})}, {relation, "relation", map[string]any{"targetCollectionId": f.collection, "bidirectionalPropertyId": nil}}, {rollup, "rollup", derivedConfig("rollup", map[string]any{"relationPropertyId": relation, "targetPropertyId": formula, "aggregation": "sum"})}, {bad, "formula", derivedConfig("formula", map[string]any{"originalExpression": "proprietary(x)", "translatedExpression": "proprietary(1)", "importedCachedValue": "captured 123"})}}
	for _, p := range configs {
		testOK(t, dbBatch(t, k, e, []any{map[string]any{"kind": "collection.addProperty", "collectionId": f.collection, "propertyId": p.id, "name": p.kind, "type": p.kind, "schemaVersion": 1, "config": p.config}}, "collection", f.collection, "property", p.id))
	}
	testOK(t, setValue(t, k, e, f, b, bp, num, "number", map[string]any{"decimal": "12.5", "format": "decimal"}))
	testOK(t, dbBatch(t, k, e, []any{map[string]any{"kind": "record.setValue", "recordId": a, "propertyId": relation, "value": map[string]any{"type": "relation", "schemaVersion": 1, "value": []string{b}}}}, "page", ap, "record", a, "collection", f.collection, "property", relation, "record", b))
	get := func() map[string]any {
		return testOK(t, testDispatch(t, k, e, "databaseDerived", map[string]any{"contractVersion": 1, "requestId": NewID(), "workspaceId": k.ID(), "collectionId": f.collection, "at": "2026-09-30T20:00:00Z", "timezone": "Asia/Kolkata"}))
	}
	find := func(result map[string]any, record, property string) map[string]any {
		for _, raw := range result["values"].(map[string]any)[record].([]any) {
			v := raw.(map[string]any)
			if v["propertyId"] == property {
				return v
			}
		}
		t.Fatal("missing derived cell")
		return nil
	}
	result := get()
	if textValue(find(result, a, rollup)["value"]) != "25" {
		t.Fatal(result)
	}
	captured := find(result, a, bad)
	if captured["status"] != "captured" || captured["value"] != nil || captured["computedAt"] != nil || captured["importedCachedValue"] != "captured 123" {
		t.Fatal(captured)
	}
	testOK(t, setValue(t, k, e, f, b, bp, num, "number", map[string]any{"decimal": "20", "format": "decimal"}))
	if textValue(find(get(), a, rollup)["value"]) != "40" {
		t.Fatal("stale rollup")
	}
	var count int
	k.db.QueryRow("SELECT count(*) FROM relation_edges WHERE record_id=?", a).Scan(&count)
	if count != 1 {
		t.Fatal("missing indexed relation", count)
	}
	// Formula dependencies are checked as a whole before any definition commits.
	x, y := NewID(), NewID()
	cmds := []any{}
	for id, dep := range map[string]string{x: y, y: x} {
		cmds = append(cmds, map[string]any{"kind": "collection.addProperty", "collectionId": f.collection, "propertyId": id, "name": "cycle", "type": "formula", "schemaVersion": 1, "config": derivedConfig("formula", map[string]any{"translatedExpression": fmt.Sprintf("prop(%q)", dep)})})
	}
	testCode(t, dbBatch(t, k, e, cmds, "collection", f.collection, "property", x, "property", y), "INVALID_REQUEST")
	// A linked gallery and timeline retain canonical IDs.
	for _, kind := range []string{"gallery", "timeline"} {
		view := NewID()
		cfg := any(map[string]any{"coverPropertyId": nil, "cardSize": "medium"})
		if kind == "timeline" {
			cfg = map[string]any{"datePropertyId": f.date, "scale": "month"}
		}
		testOK(t, dbBatch(t, k, e, []any{map[string]any{"kind": "view.save", "viewId": view, "collectionId": f.collection, "owningPageId": f.page, "name": kind, "type": kind, "schemaVersion": 1, "config": cfg, "query": emptyQuery(f.title, formula, rollup)}}, "view", view, "collection", f.collection, "page", f.page))
		testOK(t, queryDB(t, k, e, f.collection, view, 200, ""))
	}
	// Keep captured definitions byte-for-byte in the canonical property envelope.
	p, err := propertyDefinition(context.Background(), k.db, bad)
	if err != nil {
		t.Fatal(err)
	}
	var cfg map[string]any
	json.Unmarshal(p.Config, &cfg)
	if cfg["raw"].(map[string]any)["originalExpression"] != "proprietary(x)" {
		t.Fatal(cfg)
	}
}
func TestAdvancedLocksStructureAndCycle(t *testing.T) {
	k, e := testKernel(t)
	f := newDatabase(t, k, e)
	a, ap := newRecord(t, k, e, f, "A")
	b, bp := newRecord(t, k, e, f, "B")
	configure := func(id, page, parent string, deps []string) map[string]any {
		var p any
		if parent != "" {
			p = parent
		}
		return dbBatch(t, k, e, []any{map[string]any{"kind": "record.configure", "recordId": id, "parentRecordId": p, "dependencyRecordIds": deps}}, "record", a, "record", b, "page", page, "collection", f.collection)
	}
	testOK(t, configure(a, ap, b, []string{b}))
	testCode(t, configure(b, bp, a, []string{a}), "INVALID_REQUEST")
	testOK(t, dbBatch(t, k, e, []any{map[string]any{"kind": "collection.configure", "collectionId": f.collection, "locked": true}}, "collection", f.collection))
	testCode(t, setValue(t, k, e, f, a, ap, f.status, "status", f.done), "SCOPE_DENIED")
	testOK(t, dbBatch(t, k, e, []any{map[string]any{"kind": "collection.configure", "collectionId": f.collection, "locked": false}}, "collection", f.collection))
	testOK(t, setValue(t, k, e, f, a, ap, f.status, "status", f.done))
}
func TestReciprocalRelationsAndCardinalityAreAtomic(t *testing.T) {
	k, e := testKernel(t)
	f := newDatabase(t, k, e)
	a, ap := newRecord(t, k, e, f, "A")
	b, bp := newRecord(t, k, e, f, "B")
	p, q := NewID(), NewID()
	cmds := []any{}
	for id, back := range map[string]string{p: q, q: p} {
		cmds = append(cmds, map[string]any{"kind": "collection.addProperty", "collectionId": f.collection, "propertyId": id, "name": "Related", "type": "relation", "schemaVersion": 1, "config": map[string]any{"targetCollectionId": f.collection, "bidirectionalPropertyId": back, "cardinality": "one"}})
	}
	testOK(t, dbBatch(t, k, e, cmds, "collection", f.collection, "property", p, "property", q))
	command := map[string]any{"kind": "record.setValue", "recordId": a, "propertyId": p, "value": map[string]any{"type": "relation", "schemaVersion": 1, "value": []string{b}}}
	testCode(t, dbBatch(t, k, e, []any{command}, "record", a, "record", b, "page", ap, "collection", f.collection, "property", p), "INVALID_REQUEST")
	var count int
	k.db.QueryRow("SELECT count(*) FROM property_values WHERE property_id IN (?,?)", p, q).Scan(&count)
	if count != 0 {
		t.Fatal("partial reciprocal write")
	}
	testOK(t, dbBatch(t, k, e, []any{command}, "record", a, "record", b, "page", ap, "page", bp, "collection", f.collection, "property", p, "property", q))
	var raw string
	k.db.QueryRow("SELECT value_json FROM property_values WHERE record_id=? AND property_id=?", b, q).Scan(&raw)
	if raw != string(marshal([]string{a})) {
		t.Fatal(raw)
	}
	testOK(t, dbBatch(t, k, e, []any{map[string]any{"kind": "record.clearValue", "recordId": a, "propertyId": p}}, "record", a, "record", b, "page", ap, "page", bp, "collection", f.collection, "property", p, "property", q))
	k.db.QueryRow("SELECT value_json FROM property_values WHERE record_id=? AND property_id=?", b, q).Scan(&raw)
	if raw != "[]" {
		t.Fatal("clear left reverse relation", raw)
	}
}
func TestAdvancedRecordStructureRestoresWithHistory(t *testing.T) {
	k, e := testKernel(t)
	f := newDatabase(t, k, e)
	a, ap := newRecord(t, k, e, f, "A")
	b, _ := newRecord(t, k, e, f, "B")
	c, _ := newRecord(t, k, e, f, "C")
	set := func(parent string) {
		testOK(t, dbBatch(t, k, e, []any{map[string]any{"kind": "record.configure", "recordId": a, "parentRecordId": parent, "dependencyRecordIds": []string{parent}}}, "record", a, "record", parent, "page", ap, "collection", f.collection))
	}
	set(b)
	var revision string
	k.db.QueryRow("SELECT revision FROM pages WHERE id=?", ap).Scan(&revision)
	set(c)
	testOK(t, restoreTest(t, k, e, ap, revision))
	state, err := captureRecordStructure(context.Background(), k.db, a)
	if err != nil || state.ParentRecordID == nil || *state.ParentRecordID != b || len(state.DependencyRecordIDs) != 1 || state.DependencyRecordIDs[0] != b {
		t.Fatal(state, err)
	}
	// Older snapshots cannot silently retain or discard an active hierarchy.
	var raw string
	k.db.QueryRow("SELECT snapshot_json FROM page_snapshots WHERE page_id=? AND revision=?", ap, revision).Scan(&raw)
	var old map[string]any
	json.Unmarshal([]byte(raw), &old)
	delete(old, "recordStructure")
	old["recoveryMetadataVersion"] = 2
	k.db.Exec("UPDATE page_snapshots SET snapshot_json=? WHERE page_id=? AND revision=?", string(marshal(old)), ap, revision)
	testCode(t, testDispatch(t, k, e, "history.previewRestore", recoveryRequest(k, map[string]any{"pageId": ap, "restoreRevision": revision})), "CONFLICT")
}
func TestReciprocalRelationsRecoverBothGuardedEndpoints(t *testing.T) {
	k, e := testKernel(t)
	f := newDatabase(t, k, e)
	a, ap := newRecord(t, k, e, f, "A")
	b, bp := newRecord(t, k, e, f, "B")
	p, q := NewID(), NewID()
	cmds := []any{}
	for id, back := range map[string]string{p: q, q: p} {
		cmds = append(cmds, map[string]any{"kind": "collection.addProperty", "collectionId": f.collection, "propertyId": id, "name": "Related", "type": "relation", "schemaVersion": 1, "config": map[string]any{"targetCollectionId": f.collection, "bidirectionalPropertyId": back}})
	}
	testOK(t, dbBatch(t, k, e, cmds, "collection", f.collection, "property", p, "property", q))
	set := func(value []string) {
		testOK(t, dbBatch(t, k, e, []any{map[string]any{"kind": "record.setValue", "recordId": a, "propertyId": p, "value": map[string]any{"type": "relation", "schemaVersion": 1, "value": value}}}, "record", a, "record", b, "page", ap, "page", bp, "collection", f.collection, "property", p, "property", q))
	}
	set([]string{b})
	var revision string
	k.db.QueryRow("SELECT revision FROM pages WHERE id=?", ap).Scan(&revision)
	set([]string{})
	testOK(t, restoreTest(t, k, e, ap, revision))
	var raw string
	k.db.QueryRow("SELECT value_json FROM property_values WHERE record_id=? AND property_id=?", b, q).Scan(&raw)
	if raw != string(marshal([]string{a})) {
		t.Fatal("reverse link not restored", raw)
	}
}
