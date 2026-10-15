package workspace

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"

	"trove.local/phase0/contracts"
)

func layoutFixture(properties ...string) *recordLayout {
	return &recordLayout{Version: 1, Sections: []recordLayoutSection{{ID: NewID(), Title: "Details", Placement: "sidebar", PropertyIDs: properties}}, HiddenPropertyIDs: []string{}}
}
func TestRecordLayoutDefaultsOverridesUndoHistoryAndNativeRecovery(t *testing.T) {
	k, e := testKernel(t)
	f := newDatabase(t, k, e)
	a, ap := newRecord(t, k, e, f, "A")
	b, _ := newRecord(t, k, e, f, "B")
	testOK(t, setValue(t, k, e, f, a, ap, f.date, "date", scheduleTestDate("2026-10-15", "", false, "UTC")))
	original, err := semanticContentIdentity(context.Background(), k.db, ap)
	if err != nil {
		t.Fatal(err)
	}
	defaults := layoutFixture(f.date, f.status)
	var expectedDefaults any
	json.Unmarshal(marshal(defaults), &expectedDefaults)
	testOK(t, dbBatch(t, k, e, []any{map[string]any{"kind": "collection.layout.set", "collectionId": f.collection, "layout": defaults}}, "collection", f.collection, "page", f.page))
	get := func(record string) map[string]any {
		return testOK(t, testDispatch(t, k, e, "recordLayouts.get", map[string]any{"contractVersion": 1, "requestId": NewID(), "workspaceId": k.ID(), "collectionId": f.collection, "recordId": record}))
	}
	for _, record := range []string{a, b} {
		r := get(record)
		if err := contracts.Validate("service", "RecordLayoutResult", marshal(r)); err != nil {
			t.Fatal(err)
		}
		if r["overrideLayout"] != nil || string(marshal(r["effectiveLayout"])) != string(marshal(expectedDefaults)) {
			t.Fatal(r)
		}
	}
	var collectionRevision string
	k.db.QueryRow("SELECT revision FROM pages WHERE id=?", f.page).Scan(&collectionRevision)
	override := layoutFixture(f.status)
	override.HiddenPropertyIDs = []string{f.date}
	operation := NewID()
	testOK(t, dbBatch(t, k, e, []any{map[string]any{"kind": "operation.apply", "operationId": operation, "label": "Customize record", "actions": []any{map[string]any{"kind": "record.layout.set", "recordId": a, "layout": override}}}}, "collection", f.collection, "record", a, "page", ap))
	var recordRevision string
	k.db.QueryRow("SELECT revision FROM pages WHERE id=?", ap).Scan(&recordRevision)
	if get(a)["overrideLayout"] == nil || get(b)["overrideLayout"] != nil {
		t.Fatal("override leaked")
	}
	current, err := semanticContentIdentity(context.Background(), k.db, ap)
	if err != nil || current != original {
		t.Fatal("layout changed knowledge identity", err)
	}
	// Values changed later survive layout undo.
	testOK(t, setValue(t, k, e, f, a, ap, f.date, "date", scheduleTestDate("2026-10-20", "", false, "UTC")))
	undo := testOK(t, testDispatch(t, k, e, "operations.previewUndo", map[string]any{"contractVersion": 1, "requestId": NewID(), "workspaceId": k.ID(), "operationId": operation}))
	var guards []contracts.ServiceExpectedRevision
	json.Unmarshal(marshal(undo["expectedRevisions"]), &guards)
	testOK(t, testDispatch(t, k, e, "batch", testRequest(k, e, []any{undo["command"]}, guards...)))
	if get(a)["overrideLayout"] != nil {
		t.Fatal("undo did not restore inheritance")
	}
	var raw string
	k.db.QueryRow("SELECT value_json FROM property_values WHERE record_id=? AND property_id=?", a, f.date).Scan(&raw)
	var value scheduleDate
	json.Unmarshal([]byte(raw), &value)
	if value.Start != "2026-10-20" {
		t.Fatal("layout undo rewound date")
	}
	testOK(t, restoreTest(t, k, e, ap, recordRevision))
	if get(a)["overrideLayout"] == nil {
		t.Fatal("record history omitted override")
	}
	testOK(t, dbBatch(t, k, e, []any{map[string]any{"kind": "collection.layout.set", "collectionId": f.collection, "layout": nil}}, "collection", f.collection, "page", f.page))
	testOK(t, restoreTest(t, k, e, f.page, collectionRevision))
	if get(b)["defaultLayout"] == nil {
		t.Fatal("collection history omitted default")
	}
	backup := filepath.Join(t.TempDir(), "backup")
	if _, err := k.CreateBackup(context.Background(), e, backup); err != nil {
		t.Fatal(err)
	}
	if _, err := VerifyBackup(context.Background(), backup); err != nil {
		t.Fatal(err)
	}
	restoredPath := filepath.Join(t.TempDir(), "restored")
	if _, err := RestoreBackup(context.Background(), backup, restoredPath); err != nil {
		t.Fatal(err)
	}
	restoredKernel, err := Open(restoredPath)
	if err != nil {
		t.Fatal(err)
	}
	restoredLayout, err := readRecordLayout(context.Background(), restoredKernel.db, "record", a)
	restoredKernel.Close()
	if err != nil || restoredLayout == nil {
		t.Fatal("native restore lost layout", err)
	}
	path := k.Path()
	k.Close()
	reopened, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	k = reopened
	e = k.OwnerContext()
	if get(a)["overrideLayout"] == nil || get(b)["defaultLayout"] == nil {
		t.Fatal("restart lost layout")
	}
}
func TestRecordLayoutInvalidReferencesAndUndoConflict(t *testing.T) {
	k, e := testKernel(t)
	f := newDatabase(t, k, e)
	other := newDatabase(t, k, e)
	a, ap := newRecord(t, k, e, f, "A")
	testCode(t, dbBatch(t, k, e, []any{map[string]any{"kind": "record.layout.set", "recordId": a, "layout": layoutFixture(other.date)}}, "collection", f.collection, "record", a, "page", ap), "INVALID_REQUEST")
	duplicate := layoutFixture(f.date, f.date)
	testCode(t, dbBatch(t, k, e, []any{map[string]any{"kind": "record.layout.set", "recordId": a, "layout": duplicate}}, "collection", f.collection, "record", a, "page", ap), "INVALID_REQUEST")
	missing := NewID()
	layout := layoutFixture(f.date, missing)
	operation := NewID()
	testOK(t, dbBatch(t, k, e, []any{map[string]any{"kind": "operation.apply", "operationId": operation, "label": "Retained layout", "actions": []any{map[string]any{"kind": "record.layout.set", "recordId": a, "layout": layout}}}}, "collection", f.collection, "record", a, "page", ap))
	result := testOK(t, testDispatch(t, k, e, "recordLayouts.get", map[string]any{"contractVersion": 1, "requestId": NewID(), "workspaceId": k.ID(), "collectionId": f.collection, "recordId": a}))
	if len(result["missingPropertyIds"].([]any)) != 1 {
		t.Fatal(result)
	}
	testOK(t, dbBatch(t, k, e, []any{map[string]any{"kind": "record.layout.set", "recordId": a, "layout": layoutFixture(f.status)}}, "collection", f.collection, "record", a, "page", ap))
	testCode(t, dbBatch(t, k, e, []any{map[string]any{"kind": "operation.undo", "operationId": operation}}, "collection", f.collection, "record", a, "page", ap), "CONFLICT")
}
