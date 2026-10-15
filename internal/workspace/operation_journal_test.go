package workspace

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
	"trove.local/phase0/contracts"
)

func TestGroupedUndoPreservesUnrelatedEditsAndRejectsConflicts(t *testing.T) {
	k, e := testKernel(t)
	f := newDatabase(t, k, e)
	record, page := newRecord(t, k, e, f, "Before")
	operation := NewID()
	value := map[string]any{"type": "date", "schemaVersion": 1, "value": map[string]any{"start": "2026-10-20", "end": nil, "timezone": "UTC", "hasTime": false}}
	actions := []any{map[string]any{"kind": "record.setValue", "recordId": record, "propertyId": f.date, "value": value}}
	testOK(t, dbBatch(t, k, e, []any{map[string]any{"kind": "operation.apply", "operationId": operation, "label": "Move due date", "actions": actions}}, "page", page, "record", record, "collection", f.collection, "property", f.date))
	// A later title edit must survive reversing the date field.
	testOK(t, dbBatch(t, k, e, []any{map[string]any{"kind": "page.update", "pageId": page, "title": "Keep this later title"}}, "page", page, "record", record, "collection", f.collection))
	preview := testOK(t, testDispatch(t, k, e, "operations.previewUndo", map[string]any{"contractVersion": 1, "requestId": NewID(), "workspaceId": k.ID(), "operationId": operation}))
	if err := contracts.Validate("service", "OperationPreviewUndoResult", marshal(preview)); err != nil {
		t.Fatal(err)
	}
	var guards []contracts.ServiceExpectedRevision
	json.Unmarshal(marshal(preview["expectedRevisions"]), &guards)
	req := testRequest(k, e, []any{preview["command"]}, guards...)
	testOK(t, testDispatch(t, k, e, "batch", req))
	testOK(t, testDispatch(t, k, e, "batch", req))
	if readPage(t, k, e, page)["page"].(map[string]any)["title"] != "Keep this later title" {
		t.Fatal("unrelated title rewound")
	}
	var count int
	k.db.QueryRow("SELECT count(*) FROM property_values WHERE record_id=? AND property_id=?", record, f.date).Scan(&count)
	if count != 0 {
		t.Fatal("missing was not restored")
	}
	testCode(t, testDispatch(t, k, e, "operations.previewUndo", map[string]any{"contractVersion": 1, "requestId": NewID(), "workspaceId": k.ID(), "operationId": operation}), "CONFLICT")
	operation = NewID()
	testOK(t, dbBatch(t, k, e, []any{map[string]any{"kind": "operation.apply", "operationId": operation, "label": "Move again", "actions": actions}}, "page", page, "record", record, "collection", f.collection, "property", f.date))
	testOK(t, setValue(t, k, e, f, record, page, f.date, "date", map[string]any{"start": "2026-10-25", "end": nil, "timezone": "UTC", "hasTime": false}))
	testCode(t, dbBatch(t, k, e, []any{map[string]any{"kind": "operation.undo", "operationId": operation}}, "page", page, "record", record, "collection", f.collection, "property", f.date), "CONFLICT")
}
func TestGroupedUndoAtomicFailureAndNativePersistence(t *testing.T) {
	k, e := testKernel(t)
	a := createPage(t, k, e, "A")
	b := createPage(t, k, e, "B")
	operation := NewID()
	apply := map[string]any{"kind": "operation.apply", "operationId": operation, "label": "Rename pair", "actions": []any{map[string]any{"kind": "page.update", "pageId": a, "title": "A2"}, map[string]any{"kind": "page.update", "pageId": b, "title": "B2"}}}
	testOK(t, dbBatch(t, k, e, []any{apply}, "page", a, "page", b))
	testOK(t, dbBatch(t, k, e, []any{map[string]any{"kind": "page.update", "pageId": b, "title": "B3"}}, "page", b))
	testCode(t, dbBatch(t, k, e, []any{map[string]any{"kind": "operation.undo", "operationId": operation}}, "page", a, "page", b), "CONFLICT")
	if readPage(t, k, e, a)["page"].(map[string]any)["title"] != "A2" {
		t.Fatal("partial undo")
	}
	backup := filepath.Join(t.TempDir(), "backup")
	if _, err := k.CreateBackup(context.Background(), e, backup); err != nil {
		t.Fatal(err)
	}
	if _, err := VerifyBackup(context.Background(), backup); err != nil {
		t.Fatal(err)
	}
	path := k.Path()
	k.Close()
	reopened, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	result := testOK(t, testDispatch(t, reopened, reopened.OwnerContext(), "operations.list", map[string]any{"contractVersion": 1, "requestId": NewID(), "workspaceId": reopened.ID(), "pageId": a}))
	if len(result["operations"].([]any)) != 1 {
		t.Fatal(result)
	}
}

func TestGroupedUndoDisplayPreservesUnchangedMetadata(t *testing.T) {
	k, e := testKernel(t)
	page := createPage(t, k, e, "Layout")
	id := NewID()
	testOK(t, dbBatch(t, k, e, []any{map[string]any{"kind": "operation.apply", "operationId": id, "label": "Widen page", "actions": []any{map[string]any{"kind": "page.display.set", "pageId": page, "fullWidth": true, "presentation": false}}}}, "page", page))
	testOK(t, dbBatch(t, k, e, []any{map[string]any{"kind": "page.display.set", "pageId": page, "fullWidth": true, "presentation": true}}, "page", page))
	testOK(t, dbBatch(t, k, e, []any{map[string]any{"kind": "operation.undo", "operationId": id}}, "page", page))
	display, err := readPageDisplay(context.Background(), k.db, page)
	if err != nil || display.FullWidth || !display.Presentation {
		t.Fatal("unrelated display change lost", display, err)
	}
}

func TestGroupedUndoRejectsUnjournaledRelationEffects(t *testing.T) {
	k, e := testKernel(t)
	f := newDatabase(t, k, e)
	record, page := newRecord(t, k, e, f, "Source")
	relation := NewID()
	testOK(t, dbBatch(t, k, e, []any{map[string]any{"kind": "collection.addProperty", "collectionId": f.collection, "propertyId": relation, "name": "Relation", "type": "relation", "schemaVersion": 1, "config": map[string]any{"targetCollectionId": f.collection, "bidirectionalPropertyId": nil}}}, "collection", f.collection, "property", relation))
	for _, action := range []map[string]any{{"kind": "record.setValue", "recordId": record, "propertyId": relation, "value": map[string]any{"type": "relation", "schemaVersion": 1, "value": []any{}}}, {"kind": "record.clearValue", "recordId": record, "propertyId": relation}} {
		testCode(t, dbBatch(t, k, e, []any{map[string]any{"kind": "operation.apply", "operationId": NewID(), "label": "Unsupported relation", "actions": []any{action}}}, "page", page, "record", record, "collection", f.collection, "property", relation), "INVALID_REQUEST")
	}
	var count int
	k.db.QueryRow("SELECT count(*) FROM operation_journal").Scan(&count)
	if count != 0 {
		t.Fatal("unsupported effects journaled")
	}
}
