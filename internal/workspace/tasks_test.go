package workspace

import (
	"testing"
	"trove.local/phase0/contracts"
)

func ensureTestTasks(t *testing.T, k *Kernel, e contracts.ServiceExecutionContext) databaseFixture {
	f := databaseFixture{NewID(), NewID(), NewID(), NewID(), NewID(), NewID(), NewID()}
	testOK(t, dbBatch(t, k, e, []any{map[string]any{"kind": "tasks.ensure", "pageId": f.page, "collectionId": f.collection, "titlePropertyId": f.title, "statusPropertyId": f.status, "duePropertyId": f.date, "defaultOptionId": f.open, "doneOptionId": f.done}}, "page", f.page, "collection", f.collection, "property", f.title, "property", f.status, "property", f.date))
	return f
}
func TestTasksCanonicalStatusOccurrencesPromotionAndRestart(t *testing.T) {
	k, e := testKernel(t)
	f := ensureTestTasks(t, k, e)
	ensureTestTasks(t, k, e)
	var count int
	k.db.QueryRow("SELECT count(*) FROM collections WHERE system_role='tasks'").Scan(&count)
	if count != 1 {
		t.Fatal("Duplicate tasks collection")
	}
	r, p := NewID(), NewID()
	testOK(t, dbBatch(t, k, e, []any{map[string]any{"kind": "task.create", "recordId": r, "pageId": p, "title": "Canonical task"}}, "record", r, "page", p, "collection", f.collection, "property", f.status))
	note := createPage(t, k, e, "Note")
	one, two := NewID(), NewID()
	testOK(t, dbBatch(t, k, e, []any{map[string]any{"kind": "block.insert", "block": map[string]any{"blockId": one, "pageId": note, "type": "todo", "schemaVersion": 1, "data": map[string]any{"taskRecordId": r}}}, map[string]any{"kind": "block.insert", "block": map[string]any{"blockId": two, "pageId": note, "type": "todo", "schemaVersion": 1, "data": map[string]any{"taskRecordId": r}}}}, "page", note, "block", one, "block", two))
	testOK(t, dbBatch(t, k, e, []any{map[string]any{"kind": "task.setStatus", "recordId": r, "statusOptionId": f.done}}, "page", p, "record", r, "collection", f.collection, "property", f.status))
	var completed bool
	k.db.QueryRow("SELECT completed FROM task_state WHERE record_id=?", r).Scan(&completed)
	if !completed {
		t.Fatal("Task completion failed")
	}
	testOK(t, dbBatch(t, k, e, []any{map[string]any{"kind": "block.delete", "blockId": one, "includeDescendants": false}}, "page", note, "block", one))
	k.db.QueryRow("SELECT count(*) FROM task_facets WHERE record_id=?", r).Scan(&count)
	if count != 1 {
		t.Fatal("Deleting occurrence deleted task")
	}
	custom := newDatabase(t, k, e)
	existing, body := newRecord(t, k, e, custom, "Promote me")
	testOK(t, dbBatch(t, k, e, []any{map[string]any{"kind": "task.promote", "recordId": existing, "statusPropertyId": custom.status}}, "record", existing, "page", body, "collection", custom.collection, "property", custom.status))
	var actual string
	k.db.QueryRow("SELECT collection_id FROM records WHERE id=?", existing).Scan(&actual)
	if actual != custom.collection {
		t.Fatal("Promotion copied/moved task")
	}
	path := k.Path()
	k.Close()
	k, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer k.Close()
	k.db.QueryRow("SELECT completed FROM task_state WHERE record_id=?", r).Scan(&completed)
	if !completed {
		t.Fatal("Status lost on restart")
	}
	k.db.QueryRow("SELECT count(*) FROM todo_references WHERE task_record_id=?", r).Scan(&count)
	if count != 1 {
		t.Fatal(count)
	}
	testOK(t, dbBatch(t, k, k.OwnerContext(), []any{map[string]any{"kind": "task.setStatus", "recordId": r, "statusOptionId": f.open}}, "record", r, "page", p, "collection", f.collection, "property", f.status))
}
func TestTaskStaleMutationAndMissingMappings(t *testing.T) {
	k, e := testKernel(t)
	f := newDatabase(t, k, e)
	r, p := newRecord(t, k, e, f, "row")
	testOK(t, dbBatch(t, k, e, []any{map[string]any{"kind": "task.promote", "recordId": r, "statusPropertyId": f.status}}, "record", r, "page", p, "collection", f.collection, "property", f.status))
	request := testRequest(k, e, []any{map[string]any{"kind": "task.setStatus", "recordId": r, "statusOptionId": f.done}}, dbGuards(t, k, "record", r, "page", p, "collection", f.collection, "property", f.status)...)
	testOK(t, testDispatch(t, k, e, "batch", request))
	request["requestId"] = NewID()
	request["mutation"].(map[string]any)["idempotencyKey"] = NewID()
	testCode(t, testDispatch(t, k, e, "batch", request), "CONFLICT")
}

func TestTasksRejectPromotionWithoutCompletionMappings(t *testing.T) {
	k, e := testKernel(t)
	f := newDatabase(t, k, e)
	r, p := newRecord(t, k, e, f, "Needs mapping")
	property, option := NewID(), NewID()
	testOK(t, dbBatch(t, k, e, []any{map[string]any{"kind": "collection.addProperty", "collectionId": f.collection, "propertyId": property, "name": "Imported status", "type": "status", "schemaVersion": 1, "config": map[string]any{"options": []any{map[string]any{"id": option, "label": "Unknown", "group": "not_started"}}}}}, "collection", f.collection, "property", property))
	testCode(t, dbBatch(t, k, e, []any{map[string]any{"kind": "task.promote", "recordId": r, "statusPropertyId": property}}, "record", r, "page", p, "collection", f.collection, "property", property), "INVALID_REQUEST")
	var count int
	k.db.QueryRow("SELECT count(*) FROM task_facets WHERE record_id=?", r).Scan(&count)
	if count != 0 {
		t.Fatal("Invalid task facet committed")
	}
}
