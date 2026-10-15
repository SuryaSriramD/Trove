package workspace

import (
	"context"
	"database/sql"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"trove.local/phase0/contracts"
)

func detachPreview(t *testing.T, k *Kernel, e contracts.ServiceExecutionContext, record string, choices []taskDetachChoice) taskDetachPlan {
	t.Helper()
	if choices == nil {
		choices = []taskDetachChoice{}
	}
	raw := testOK(t, testDispatch(t, k, e, "tasks.detachPreview", map[string]any{"contractVersion": 1, "requestId": NewID(), "workspaceId": k.ID(), "recordId": record, "choices": choices}))
	if err := contracts.Validate("service", "TaskDetachPlan", marshal(raw)); err != nil {
		t.Fatal(err)
	}
	var plan taskDetachPlan
	if err := json.Unmarshal(marshal(raw), &plan); err != nil {
		t.Fatal(err)
	}
	return plan
}
func detachRequest(k *Kernel, e contracts.ServiceExecutionContext, p taskDetachPlan) map[string]any {
	return testRequest(k, e, []any{map[string]any{"kind": "task.detach.apply", "plan": p}}, p.RequiredRevisions...)
}
func undoDetach(t *testing.T, k *Kernel, e contracts.ServiceExecutionContext, id string) {
	t.Helper()
	preview := testOK(t, testDispatch(t, k, e, "operations.previewUndo", map[string]any{"contractVersion": 1, "requestId": NewID(), "workspaceId": k.ID(), "operationId": id}))
	if err := contracts.Validate("service", "OperationPreviewUndoResult", marshal(preview)); err != nil {
		t.Fatal(err)
	}
	var guards []contracts.ServiceExpectedRevision
	if err := json.Unmarshal(marshal(preview["expectedRevisions"]), &guards); err != nil {
		t.Fatal(err)
	}
	testOK(t, testDispatch(t, k, e, "batch", testRequest(k, e, []any{preview["command"]}, guards...)))
}
func taskForDetach(t *testing.T, k *Kernel, e contracts.ServiceExecutionContext) (databaseFixture, string, string) {
	t.Helper()
	f := ensureTestTasks(t, k, e)
	record, page := NewID(), NewID()
	testOK(t, dbBatch(t, k, e, []any{map[string]any{"kind": "task.create", "recordId": record, "pageId": page, "title": "Publish Friday"}}, "record", record, "page", page, "collection", f.collection, "property", f.status))
	return f, record, page
}
func insertDetachTodo(t *testing.T, k *Kernel, e contracts.ServiceExecutionContext, page, record, parent string) string {
	t.Helper()
	id := NewID()
	b := map[string]any{"blockId": id, "pageId": page, "type": "todo", "schemaVersion": 1, "data": map[string]any{"taskRecordId": record}}
	pairs := []string{"page", page, "block", id}
	if parent != "" {
		b["parentBlockId"] = parent
		pairs = append(pairs, "block", parent)
	}
	testOK(t, dbBatch(t, k, e, []any{map[string]any{"kind": "block.insert", "block": b}}, pairs...))
	return id
}
func TestTaskDetachWithoutOccurrencesPreservesValuesAcrossUndoAndRestart(t *testing.T) {
	k, e := testKernel(t)
	f, record, page := taskForDetach(t, k, e)
	plan := detachPreview(t, k, e, record, nil)
	if len(plan.Occurrences) != 0 {
		t.Fatal(plan)
	}
	testOK(t, testDispatch(t, k, e, "batch", detachRequest(k, e, plan)))
	testOK(t, dbBatch(t, k, e, []any{map[string]any{"kind": "page.update", "pageId": page, "title": "Later title"}, map[string]any{"kind": "record.setValue", "recordId": record, "propertyId": f.status, "value": map[string]any{"type": "status", "schemaVersion": 1, "value": f.done}}, map[string]any{"kind": "record.setValue", "recordId": record, "propertyId": f.date, "value": map[string]any{"type": "date", "schemaVersion": 1, "value": map[string]any{"start": "2026-11-11", "end": nil, "timezone": "UTC", "hasTime": false}}}}, "record", record, "page", page, "collection", f.collection, "property", f.status, "property", f.date))
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
	k = reopened
	e = k.OwnerContext()
	undoDetach(t, k, e, plan.ID)
	var actualPage, title, status, due string
	if err = k.db.QueryRow(`SELECT r.page_id,p.title,s.value_json,d.value_json FROM records r JOIN pages p ON p.id=r.page_id JOIN property_values s ON s.record_id=r.id AND s.property_id=? JOIN property_values d ON d.record_id=r.id AND d.property_id=? JOIN task_facets f ON f.record_id=r.id WHERE r.id=?`, f.status, f.date, record).Scan(&actualPage, &title, &status, &due); err != nil {
		t.Fatal(err)
	}
	if actualPage != page || title != "Later title" || !strings.Contains(status, f.done) || !strings.Contains(due, "2026-11-11") {
		t.Fatal("undo rewound unrelated canonical values", actualPage, title, status, due)
	}
}
func TestTaskDetachReviewedOccurrencesPreserveChildrenAndRestoreExactIDs(t *testing.T) {
	k, e := testKernel(t)
	_, record, taskPage := taskForDetach(t, k, e)
	note := createPage(t, k, e, "Meeting note")
	leaf := insertDetachTodo(t, k, e, note, record, "")
	nested := insertDetachTodo(t, k, e, note, record, "")
	child := NewID()
	testOK(t, dbBatch(t, k, e, []any{map[string]any{"kind": "block.insert", "block": map[string]any{"blockId": child, "pageId": note, "parentBlockId": nested, "type": "paragraph", "schemaVersion": 1, "data": map[string]any{"richText": []any{map[string]any{"text": "Keep nested evidence"}}}}}}, "page", note, "block", child, "block", nested))
	plan := detachPreview(t, k, e, record, []taskDetachChoice{{leaf, "remove"}})
	var count int
	if err := k.db.QueryRow("SELECT count(*) FROM operation_journal").Scan(&count); err != nil || count != 0 {
		t.Fatal("preview wrote", count, err)
	}
	req := detachRequest(k, e, plan)
	testOK(t, testDispatch(t, k, e, "batch", req))
	testOK(t, testDispatch(t, k, e, "batch", req))
	if err := k.db.QueryRow("SELECT count(*) FROM task_facets WHERE record_id=?", record).Scan(&count); err != nil || count != 0 {
		t.Fatal(count, err)
	}
	var typ, raw, parent string
	if err := k.db.QueryRow("SELECT type,data_json FROM blocks WHERE id=?", nested).Scan(&typ, &raw); err != nil || typ != "paragraph" || !strings.Contains(raw, "Publish Friday") || !strings.Contains(raw, taskPage) {
		t.Fatal(typ, raw, err)
	}
	if err := k.db.QueryRow("SELECT parent_block_id FROM blocks WHERE id=?", child).Scan(&parent); err != nil || parent != nested {
		t.Fatal("child lost", parent, err)
	}
	if err := k.db.QueryRow("SELECT count(*) FROM blocks WHERE id=?", leaf).Scan(&count); err != nil || count != 0 {
		t.Fatal("leaf retained", count, err)
	}
	unrelated := NewID()
	testOK(t, dbBatch(t, k, e, []any{map[string]any{"kind": "block.insert", "block": map[string]any{"blockId": unrelated, "pageId": note, "type": "paragraph", "schemaVersion": 1, "data": map[string]any{"richText": []any{map[string]any{"text": "Later unrelated paragraph"}}}}}}, "page", note, "block", unrelated))
	undoDetach(t, k, e, plan.ID)
	if err := k.db.QueryRow("SELECT count(*) FROM todo_references WHERE task_record_id=?", record).Scan(&count); err != nil || count != 2 {
		t.Fatal(count, err)
	}
	if err := k.db.QueryRow("SELECT count(*) FROM blocks WHERE id IN(?,?,?)", leaf, child, unrelated).Scan(&count); err != nil || count != 3 {
		t.Fatal("undo lost IDs/unrelated content", count, err)
	}
	var revision string
	if err := k.db.QueryRow("SELECT revision FROM blocks WHERE id=?", leaf).Scan(&revision); err != nil || revision != "3" {
		t.Fatal("restoration rewound block revision", revision, err)
	}
}
func TestTaskDetachRejectsStaleClosureHiddenOccurrencesAndDirectBypass(t *testing.T) {
	k, e := testKernel(t)
	f, record, page := taskForDetach(t, k, e)
	note := createPage(t, k, e, "Private occurrence")
	plan := detachPreview(t, k, e, record, nil)
	insertDetachTodo(t, k, e, note, record, "")
	testCode(t, testDispatch(t, k, e, "batch", detachRequest(k, e, plan)), "CONFLICT")
	scope := contracts.DomainScope{Value: contracts.DomainScopeVariant3{Kind: "pages", WorkspaceId: contracts.DomainId(k.ID()), PageIds: []contracts.DomainId{contracts.DomainId(page), contracts.DomainId(f.page)}}}
	scoped := k.Grant(e.Actor, "user", scope)
	testCode(t, testDispatch(t, k, scoped, "tasks.detachPreview", map[string]any{"contractVersion": 1, "requestId": NewID(), "workspaceId": k.ID(), "recordId": record, "choices": []any{}}), "SCOPE_DENIED")
	testCode(t, dbBatch(t, k, e, []any{map[string]any{"kind": "task.facet.set", "recordId": record, "binding": taskBinding{f.status, &f.date}, "attached": false}}, "page", page, "record", record, "collection", f.collection, "property", f.status, "property", f.date), "APPROVAL_REQUIRED")
	var count int
	if err := k.db.QueryRow("SELECT count(*) FROM task_facets WHERE record_id=?", record).Scan(&count); err != nil || count != 1 {
		t.Fatal("rejected plan partially detached", count, err)
	}
}
func TestTaskDetachUndoRejectsAffectedOccurrenceEditsAtomically(t *testing.T) {
	k, e := testKernel(t)
	_, record, _ := taskForDetach(t, k, e)
	note := createPage(t, k, e, "Note")
	occurrence := insertDetachTodo(t, k, e, note, record, "")
	plan := detachPreview(t, k, e, record, nil)
	testOK(t, testDispatch(t, k, e, "batch", detachRequest(k, e, plan)))
	testOK(t, dbBatch(t, k, e, []any{map[string]any{"kind": "block.update", "blockId": occurrence, "type": "paragraph", "schemaVersion": 1, "data": map[string]any{"richText": []any{map[string]any{"text": "A later independent edit"}}}}}, "page", note, "block", occurrence))
	testCode(t, testDispatch(t, k, e, "operations.previewUndo", map[string]any{"contractVersion": 1, "requestId": NewID(), "workspaceId": k.ID(), "operationId": plan.ID}), "CONFLICT")
	var count int
	if err := k.db.QueryRow("SELECT count(*) FROM task_facets WHERE record_id=?", record).Scan(&count); err != nil || count != 0 {
		t.Fatal("failed undo partially attached", count, err)
	}
}

func TestTaskDetachRejectsNestedRemovalStaleChoicesAndMissingGuards(t *testing.T) {
	k, e := testKernel(t)
	_, record, _ := taskForDetach(t, k, e)
	note := createPage(t, k, e, "Nested")
	parent := insertDetachTodo(t, k, e, note, record, "")
	child := insertDetachTodo(t, k, e, note, record, parent)
	preview := map[string]any{"contractVersion": 1, "requestId": NewID(), "workspaceId": k.ID(), "recordId": record, "choices": []taskDetachChoice{{parent, "remove"}}}
	testCode(t, testDispatch(t, k, e, "tasks.detachPreview", preview), "CONFLICT")
	plan := detachPreview(t, k, e, record, nil)
	// Omission changes the reviewed complete inventory even if the client keeps the hash.
	tampered := plan
	tampered.Occurrences = append([]taskDetachOccurrence{}, plan.Occurrences[:1]...)
	testCode(t, testDispatch(t, k, e, "batch", detachRequest(k, e, tampered)), "CONFLICT")
	request := detachRequest(k, e, plan)
	request["mutation"].(map[string]any)["expectedRevisions"] = []contracts.ServiceExpectedRevision{}
	testCode(t, testDispatch(t, k, e, "batch", request), "INVALID_REQUEST")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var response map[string]any
	if err := json.Unmarshal(k.Dispatch(ctx, e, "batch", marshal(detachRequest(k, e, plan))), &response); err != nil {
		t.Fatal(err)
	}
	testCode(t, response, "CANCELLED")
	var count int
	if err := k.db.QueryRow("SELECT count(*) FROM todo_references WHERE block_id IN(?,?)", parent, child).Scan(&count); err != nil || count != 2 {
		t.Fatal("rejected plan changed occurrences", count, err)
	}
}
func TestTaskDetachUndoRejectsReattachmentAndMissingStatusValue(t *testing.T) {
	for _, mode := range []string{"rebound", "status_cleared"} {
		t.Run(mode, func(t *testing.T) {
			k, e := testKernel(t)
			f, record, page := taskForDetach(t, k, e)
			plan := detachPreview(t, k, e, record, nil)
			testOK(t, testDispatch(t, k, e, "batch", detachRequest(k, e, plan)))
			if mode == "rebound" {
				testOK(t, dbBatch(t, k, e, []any{map[string]any{"kind": "task.promote", "recordId": record, "statusPropertyId": f.status}}, "page", page, "record", record, "collection", f.collection, "property", f.status))
			} else {
				testOK(t, dbBatch(t, k, e, []any{map[string]any{"kind": "record.clearValue", "recordId": record, "propertyId": f.status}}, "page", page, "record", record, "collection", f.collection, "property", f.status))
			}
			testCode(t, testDispatch(t, k, e, "operations.previewUndo", map[string]any{"contractVersion": 1, "requestId": NewID(), "workspaceId": k.ID(), "operationId": plan.ID}), "CONFLICT")
		})
	}
}
func TestTaskDetachIncludesRetainedTrashedOccurrencesAndRestrictedSourcePolicy(t *testing.T) {
	k, e := testKernel(t)
	_, record, page := taskForDetach(t, k, e)
	note := createPage(t, k, e, "Trashed notes")
	occurrence := insertDetachTodo(t, k, e, note, record, "")
	testOK(t, dbBatch(t, k, e, []any{map[string]any{"kind": "page.trash", "pageId": note, "includeDescendants": false}}, "page", note))
	if err := k.write(context.Background(), func(tx *sql.Tx) error {
		return retainPageExternalContextPolicy(context.Background(), tx, page, gmailExternalContextPolicy)
	}); err != nil {
		t.Fatal(err)
	}
	plan := detachPreview(t, k, e, record, nil)
	if len(plan.Occurrences) != 1 || !plan.Occurrences[0].Trashed {
		t.Fatal("retained reference omitted", plan)
	}
	testOK(t, testDispatch(t, k, e, "batch", detachRequest(k, e, plan)))
	restricted, err := pageExternalContextRestricted(context.Background(), k.db, note)
	if err != nil || !restricted {
		t.Fatal("converted source policy lost", restricted, err)
	}
	var deleted sql.NullString
	if err = k.db.QueryRow("SELECT deleted_at FROM pages WHERE id=?", note).Scan(&deleted); err != nil || !deleted.Valid {
		t.Fatal("detachment restored trashed page", err)
	}
	undoDetach(t, k, e, plan.ID)
	var count int
	if err = k.db.QueryRow("SELECT count(*) FROM todo_references WHERE block_id=?", occurrence).Scan(&count); err != nil || count != 1 {
		t.Fatal(count, err)
	}
}

func TestTaskDetachReviewsReminderReactivationAndRejectsChangedReminder(t *testing.T) {
	k, e := testKernel(t)
	f, record, page := taskForDetach(t, k, e)
	reminder := NewID()
	command := map[string]any{"kind": "reminder.put", "reminderId": reminder, "pageId": page, "expectedRevision": "0", "startDate": "2026-10-16", "localTime": "09:00", "timezone": "UTC", "frequency": "daily", "enabled": true, "private": true}
	testOK(t, dbBatch(t, k, e, []any{command, map[string]any{"kind": "task.setStatus", "recordId": record, "statusOptionId": f.done}}, "page", page, "record", record, "collection", f.collection, "property", f.status))
	plan := detachPreview(t, k, e, record, nil)
	if len(plan.Reminders) != 1 || plan.Reminders[0].EligibleBefore || !plan.Reminders[0].EligibleAfter || plan.Reminders[0].ID != reminder {
		t.Fatal("reactivation not explicit", plan.Reminders)
	}
	command["expectedRevision"] = "1"
	command["enabled"] = false
	testOK(t, dbBatch(t, k, e, []any{command}, "page", page, "record", record, "collection", f.collection))
	testCode(t, testDispatch(t, k, e, "batch", detachRequest(k, e, plan)), "CONFLICT")
	plan = detachPreview(t, k, e, record, nil)
	testOK(t, testDispatch(t, k, e, "batch", detachRequest(k, e, plan)))
	rules, err := readReminderRules(context.Background(), k.db)
	if err != nil || len(rules) != 1 || rules[0].Enabled {
		t.Fatal("configuration changed", rules, err)
	}
}
