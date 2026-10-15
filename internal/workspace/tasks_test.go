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

func TestTaskPromotionExplicitDueBindingAndSharedEdits(t *testing.T) {
	k, e := testKernel(t)
	f := newDatabase(t, k, e)
	record, page := newRecord(t, k, e, f, "Original task")
	otherDate := NewID()
	testOK(t, dbBatch(t, k, e, []any{map[string]any{"kind": "collection.addProperty", "collectionId": f.collection, "propertyId": otherDate, "name": "Reviewed due", "type": "date", "schemaVersion": 1, "config": map[string]any{}}}, "collection", f.collection, "property", otherDate))
	due := map[string]any{"start": "2026-10-20", "end": nil, "timezone": "Asia/Kolkata", "hasTime": false}
	testOK(t, dbBatch(t, k, e, []any{map[string]any{"kind": "record.setValue", "recordId": record, "propertyId": otherDate, "value": map[string]any{"type": "date", "schemaVersion": 1, "value": due}}}, "record", record, "page", page, "collection", f.collection, "property", otherDate))
	command := map[string]any{"kind": "task.promote", "recordId": record, "statusPropertyId": f.status, "duePropertyId": otherDate}
	testCode(t, dbBatch(t, k, e, []any{command}, "record", record, "page", page, "collection", f.collection, "property", f.status), "INVALID_REQUEST")
	testOK(t, dbBatch(t, k, e, []any{command}, "record", record, "page", page, "collection", f.collection, "property", f.status, "property", otherDate))
	var binding, recordPage string
	k.db.QueryRow("SELECT due_property_id FROM task_facets WHERE record_id=?", record).Scan(&binding)
	k.db.QueryRow("SELECT page_id FROM records WHERE id=?", record).Scan(&recordPage)
	if binding != otherDate || recordPage != page {
		t.Fatal("Promotion changed identity or selected the wrong date property")
	}
	note := createPage(t, k, e, "Occurrences")
	one, two := NewID(), NewID()
	testOK(t, dbBatch(t, k, e, []any{map[string]any{"kind": "block.insert", "block": map[string]any{"blockId": one, "pageId": note, "type": "todo", "schemaVersion": 1, "data": map[string]any{"taskRecordId": record}}}, map[string]any{"kind": "block.insert", "block": map[string]any{"blockId": two, "pageId": note, "type": "todo", "schemaVersion": 1, "data": map[string]any{"taskRecordId": record}}}}, "page", note, "block", one, "block", two))
	testOK(t, dbBatch(t, k, e, []any{map[string]any{"kind": "page.update", "pageId": page, "title": "Edited everywhere"}, map[string]any{"kind": "task.setStatus", "recordId": record, "statusOptionId": f.done}, map[string]any{"kind": "record.clearValue", "recordId": record, "propertyId": otherDate}}, "record", record, "page", page, "collection", f.collection, "property", f.status, "property", otherDate))
	var title string
	var complete bool
	var occurrences, values int
	k.db.QueryRow("SELECT p.title,t.completed FROM task_state t JOIN records r ON r.id=t.record_id JOIN pages p ON p.id=r.page_id WHERE r.id=?", record).Scan(&title, &complete)
	k.db.QueryRow("SELECT count(*) FROM todo_references WHERE task_record_id=?", record).Scan(&occurrences)
	k.db.QueryRow("SELECT count(*) FROM property_values WHERE record_id=? AND property_id=?", record, otherDate).Scan(&values)
	if title != "Edited everywhere" || !complete || occurrences != 2 || values != 0 {
		t.Fatalf("Canonical task edit diverged: %q %v %d %d", title, complete, occurrences, values)
	}
}
func TestTaskPromotionRejectsForeignOrNonDateBinding(t *testing.T) {
	k, e := testKernel(t)
	f := newDatabase(t, k, e)
	foreign := newDatabase(t, k, e)
	r, p := newRecord(t, k, e, f, "Task")
	for _, due := range []string{f.status, foreign.date} {
		testCode(t, dbBatch(t, k, e, []any{map[string]any{"kind": "task.promote", "recordId": r, "statusPropertyId": f.status, "duePropertyId": due}}, "record", r, "page", p, "collection", f.collection, "property", f.status, "property", due), "INVALID_REQUEST")
	}
	var count int
	k.db.QueryRow("SELECT count(*) FROM task_facets WHERE record_id=?", r).Scan(&count)
	if count != 0 {
		t.Fatal("Invalid binding partially promoted the record")
	}
}
