package workspace

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"testing"
	"trove.local/phase0/contracts"
)

type databaseFixture struct{ page, collection, title, status, date, open, done string }

func dbGuards(t *testing.T, k *Kernel, pairs ...string) []contracts.ServiceExpectedRevision {
	t.Helper()
	out := []contracts.ServiceExpectedRevision{}
	seen := map[string]bool{}
	for i := 0; i < len(pairs); i += 2 {
		kind, id := pairs[i], pairs[i+1]
		if seen[kind+id] {
			continue
		}
		seen[kind+id] = true
		rev := "0"
		_ = k.db.QueryRow("SELECT revision FROM "+entityTables[kind]+" WHERE id=?", id).Scan(&rev)
		out = append(out, required(kind, id, rev))
	}
	return out
}
func dbBatch(t *testing.T, k *Kernel, e contracts.ServiceExecutionContext, commands []any, pairs ...string) map[string]any {
	return testDispatch(t, k, e, "batch", testRequest(k, e, commands, dbGuards(t, k, pairs...)...))
}
func newDatabase(t *testing.T, k *Kernel, e contracts.ServiceExecutionContext) databaseFixture {
	t.Helper()
	f := databaseFixture{NewID(), NewID(), NewID(), NewID(), NewID(), NewID(), NewID()}
	testOK(t, dbBatch(t, k, e, []any{map[string]any{"kind": "page.create", "pageId": f.page, "title": "Project"}, map[string]any{"kind": "collection.create", "collectionId": f.collection, "owningPageId": f.page, "titlePropertyId": f.title, "title": "Work"}, map[string]any{"kind": "collection.addProperty", "collectionId": f.collection, "propertyId": f.status, "name": "Status", "type": "status", "schemaVersion": 1, "config": map[string]any{"options": []any{map[string]any{"id": f.open, "label": "Open", "group": "not_started"}, map[string]any{"id": f.done, "label": "Done", "group": "done"}}, "defaultOptionId": f.open, "doneOptionId": f.done, "reopenOptionId": f.open}}, map[string]any{"kind": "collection.addProperty", "collectionId": f.collection, "propertyId": f.date, "name": "When", "type": "date", "schemaVersion": 1, "config": map[string]any{}}}, "page", f.page, "collection", f.collection, "property", f.title, "property", f.status, "property", f.date))
	return f
}
func newRecord(t *testing.T, k *Kernel, e contracts.ServiceExecutionContext, f databaseFixture, title string) (string, string) {
	t.Helper()
	r, p := NewID(), NewID()
	testOK(t, dbBatch(t, k, e, []any{map[string]any{"kind": "record.create", "recordId": r, "pageId": p, "collectionId": f.collection, "title": title}}, "record", r, "page", p, "collection", f.collection))
	return r, p
}
func setValue(t *testing.T, k *Kernel, e contracts.ServiceExecutionContext, f databaseFixture, r, p, property, kind string, value any) map[string]any {
	return dbBatch(t, k, e, []any{map[string]any{"kind": "record.setValue", "recordId": r, "propertyId": property, "value": map[string]any{"type": kind, "schemaVersion": 1, "value": value}}}, "record", r, "page", p, "collection", f.collection, "property", property)
}
func emptyQuery(properties ...string) map[string]any {
	return map[string]any{"filter": nil, "sorts": []any{}, "visiblePropertyIds": properties, "groupByPropertyId": nil, "subgroupByPropertyId": nil}
}
func queryDB(t *testing.T, k *Kernel, e contracts.ServiceExecutionContext, collection, view string, limit int, cursor string) map[string]any {
	t.Helper()
	r := map[string]any{"contractVersion": 1, "requestId": NewID(), "workspaceId": k.ID(), "collectionId": collection, "limit": limit}
	if view != "" {
		r["viewId"] = view
	}
	if cursor != "" {
		r["cursor"] = cursor
	}
	return testDispatch(t, k, e, "queryRecords", r)
}
func TestDatabaseFourViewsShareRecordsAndRestart(t *testing.T) {
	k, e := testKernel(t)
	f := newDatabase(t, k, e)
	r, p := newRecord(t, k, e, f, "First")
	r2, _ := newRecord(t, k, e, f, "Second")
	testOK(t, setValue(t, k, e, f, r, p, f.status, "status", f.open))
	testOK(t, setValue(t, k, e, f, r, p, f.date, "date", map[string]any{"start": "2026-09-30", "end": "2026-10-02", "hasTime": false, "timezone": "Asia/Kolkata"}))
	views := []string{}
	for _, kind := range []string{"table", "list", "board", "calendar"} {
		id := NewID()
		views = append(views, id)
		config := map[string]any{"table": map[string]any{"columnWidths": []any{}}, "list": map[string]any{"showPageIcon": true}, "board": map[string]any{"groupPropertyId": f.status, "hideEmptyGroups": false}, "calendar": map[string]any{"datePropertyId": f.date, "weekStartsOn": 1}}[kind]
		testOK(t, dbBatch(t, k, e, []any{map[string]any{"kind": "view.save", "viewId": id, "collectionId": f.collection, "owningPageId": f.page, "name": kind, "type": kind, "schemaVersion": 1, "config": config, "query": emptyQuery(f.title, f.status, f.date)}}, "view", id, "collection", f.collection, "page", f.page))
		result := testOK(t, queryDB(t, k, e, f.collection, id, 200, ""))
		ids := result["recordIds"].([]any)
		if len(ids) != 2 || ids[0] != r || ids[1] != r2 {
			t.Fatal(result)
		}
	}
	// Board drop and calendar reschedule both commit through record.setValue.
	testOK(t, setValue(t, k, e, f, r, p, f.status, "status", f.done))
	testOK(t, setValue(t, k, e, f, r, p, f.date, "date", map[string]any{"start": "2026-10-04", "end": "2026-10-06", "hasTime": false, "timezone": "Asia/Kolkata"}))
	destination := createPage(t, k, e, "Another place")
	testOK(t, dbBatch(t, k, e, []any{map[string]any{"kind": "view.save", "viewId": views[0], "collectionId": f.collection, "owningPageId": destination, "name": "Renamed and moved", "type": "table", "schemaVersion": 1, "config": map[string]any{"columnWidths": []any{}}, "query": emptyQuery(f.title)}}, "view", views[0], "collection", f.collection, "page", f.page, "page", destination))
	path := k.Path()
	k.Close()
	k, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer k.Close()
	e = k.OwnerContext()
	for _, v := range views {
		result := testOK(t, queryDB(t, k, e, f.collection, v, 200, ""))
		if len(result["records"].([]any)) != 2 {
			t.Fatal(result)
		}
	}
	var count int
	k.db.QueryRow("SELECT count(*) FROM records").Scan(&count)
	if count != 2 {
		t.Fatal("View move duplicated records")
	}
	var pr, rr string
	k.db.QueryRow("SELECT revision FROM pages WHERE id=?", p).Scan(&pr)
	k.db.QueryRow("SELECT revision FROM records WHERE id=?", r).Scan(&rr)
	if pr != rr {
		t.Fatal(pr, rr)
	}
	snap := testOK(t, testDispatch(t, k, e, "workspace.snapshot", map[string]any{"contractVersion": 1, "requestId": NewID(), "workspaceId": k.ID()}))
	if err = contracts.Validate("domain", "DomainSnapshot", marshal(map[string]any{"contractVersion": snap["contractVersion"], "workspaceId": snap["workspaceId"], "pages": snap["pages"], "blocks": snap["blocks"], "collections": snap["collections"], "properties": snap["properties"], "records": snap["records"], "views": snap["views"], "containers": snap["containers"], "containerCollections": snap["containerCollections"], "taskFacets": snap["taskFacets"], "files": snap["files"], "meetings": snap["meetings"], "memories": snap["memories"], "projectScopes": snap["projectScopes"], "transcriptSegments": snap["transcriptSegments"], "importMappings": snap["importMappings"]})); err != nil {
		t.Fatal(err)
	}
}
func TestDatabaseTypedPropertiesPreserveImportMetadata(t *testing.T) {
	k, e := testKernel(t)
	f := newDatabase(t, k, e)
	r, p := newRecord(t, k, e, f, "Row body")
	external := map[string]any{"sourceSystem": "notion", "externalId": "person-1", "displayName": "Asha", "externalKind": "person", "raw": map[string]any{"preserved": "source identity"}}
	cases := []struct {
		kind          string
		config, value any
	}{{"text", map[string]any{}, []any{map[string]any{"text": "नमस्ते 世界 مرحبا 👩🏽‍💻", "marks": []any{"bold"}}}}, {"number", map[string]any{"format": "decimal"}, map[string]any{"decimal": "9007199254740993.000001", "format": "decimal"}}, {"select", map[string]any{"options": []any{map[string]any{"id": NewID(), "label": "One"}}}, nil}, {"multi_select", map[string]any{"options": []any{}}, []any{}}, {"checkbox", map[string]any{}, true}, {"url", map[string]any{}, "https://example.com/test"}, {"email", map[string]any{}, "asha@example.com"}, {"phone", map[string]any{}, "+91 555 0100"}, {"files", map[string]any{}, []any{}}, {"identity", map[string]any{}, []any{external}}, {"author", map[string]any{}, external}, {"created_time", map[string]any{}, "2026-09-30T10:15:00Z"}, {"last_edited_time", map[string]any{}, "2026-09-30T11:15:00Z"}, {"place", map[string]any{}, map[string]any{"label": "Hyderabad", "latitude": 17.385, "longitude": 78.4867, "raw": map[string]any{"source": "preserved"}}}}
	for _, c := range cases {
		id := NewID()
		testOK(t, dbBatch(t, k, e, []any{map[string]any{"kind": "collection.addProperty", "collectionId": f.collection, "propertyId": id, "name": c.kind, "type": c.kind, "schemaVersion": 1, "config": c.config}}, "collection", f.collection, "property", id))
		testOK(t, setValue(t, k, e, f, r, p, id, c.kind, c.value))
		var stored string
		if err := k.db.QueryRow("SELECT value_json FROM property_values WHERE record_id=? AND property_id=?", r, id).Scan(&stored); err != nil {
			t.Fatal(err)
		}
		var actual any
		json.Unmarshal([]byte(stored), &actual)
		if string(marshal(actual)) != string(marshal(c.value)) {
			t.Fatalf("%s changed: %s", c.kind, stored)
		}
	}
	testCode(t, setValue(t, k, e, f, r, p, f.title, "title", map[string]any{"pageId": p}), "INVALID_REQUEST")
	testCode(t, setValue(t, k, e, f, r, p, f.date, "date", map[string]any{"start": "2026-10-01", "end": "2026-09-01", "hasTime": false, "timezone": "UTC"}), "INVALID_REQUEST")
	testCode(t, setValue(t, k, e, f, r, p, f.status, "status", NewID()), "INVALID_REQUEST")
	var titleValues int
	k.db.QueryRow("SELECT count(*) FROM property_values WHERE property_type='title'").Scan(&titleValues)
	if titleValues != 0 {
		t.Fatal("Duplicate title")
	}
}
func TestDatabaseFiltersDecimalSortCursorAndAtomicity(t *testing.T) {
	k, e := testKernel(t)
	f := newDatabase(t, k, e)
	number := NewID()
	testOK(t, dbBatch(t, k, e, []any{map[string]any{"kind": "collection.addProperty", "collectionId": f.collection, "propertyId": number, "name": "Cost", "type": "number", "schemaVersion": 1, "config": map[string]any{"format": "decimal"}}}, "collection", f.collection, "property", number))
	a, ap := newRecord(t, k, e, f, "A")
	b, bp := newRecord(t, k, e, f, "B")
	testOK(t, setValue(t, k, e, f, a, ap, number, "number", map[string]any{"decimal": "10000000000000000.001", "format": "decimal"}))
	testOK(t, setValue(t, k, e, f, b, bp, number, "number", map[string]any{"decimal": "10000000000000000.0001", "format": "decimal"}))
	v := NewID()
	q := emptyQuery(f.title, number)
	q["filter"] = map[string]any{"op": "and", "terms": []any{map[string]any{"op": "or", "terms": []any{map[string]any{"op": "eq", "propertyId": f.title, "value": "A"}, map[string]any{"op": "eq", "propertyId": f.title, "value": "B"}}}}}
	q["sorts"] = []any{map[string]any{"propertyId": number, "direction": "asc"}}
	q["groupByPropertyId"] = f.status
	q["subgroupByPropertyId"] = f.date
	testOK(t, dbBatch(t, k, e, []any{map[string]any{"kind": "view.save", "viewId": v, "collectionId": f.collection, "owningPageId": f.page, "name": "Sorted", "type": "table", "schemaVersion": 1, "config": map[string]any{"columnWidths": []any{}}, "query": q}}, "view", v, "collection", f.collection, "page", f.page))
	first := testOK(t, queryDB(t, k, e, f.collection, v, 1, ""))
	if first["recordIds"].([]any)[0] != b {
		t.Fatal("Decimal precision lost", first)
	}
	cursor := first["nextCursor"].(string)
	second := testOK(t, queryDB(t, k, e, f.collection, v, 1, cursor))
	if second["recordIds"].([]any)[0] != a {
		t.Fatal(second)
	}
	testOK(t, dbBatch(t, k, e, []any{map[string]any{"kind": "page.update", "pageId": ap, "title": "Changed"}}, "page", ap, "record", a, "collection", f.collection))
	testCode(t, queryDB(t, k, e, f.collection, v, 1, cursor), "CONFLICT")
	guards := dbGuards(t, k, "page", ap, "record", a, "collection", f.collection, "property", number)
	request := testRequest(k, e, []any{map[string]any{"kind": "page.update", "pageId": ap, "title": "Must roll back"}, map[string]any{"kind": "record.setValue", "recordId": a, "propertyId": number, "value": map[string]any{"type": "checkbox", "schemaVersion": 1, "value": true}}}, guards...)
	testCode(t, testDispatch(t, k, e, "batch", request), "INVALID_REQUEST")
	if readPage(t, k, e, ap)["page"].(map[string]any)["title"] != "Changed" {
		t.Fatal("Partial transaction committed")
	}
}
func TestDatabaseCrossCollectionAndScopeDenied(t *testing.T) {
	k, e := testKernel(t)
	f := newDatabase(t, k, e)
	other := newDatabase(t, k, e)
	r, p := newRecord(t, k, e, f, "private")
	testCode(t, setValue(t, k, e, f, r, p, other.status, "status", other.open), "INVALID_REQUEST")
	scoped := k.Grant(e.Actor, "user", contracts.DomainScope{Value: contracts.DomainScopeVariant3{Kind: "pages", WorkspaceId: contracts.DomainId(k.ID()), PageIds: []contracts.DomainId{contracts.DomainId(f.page)}}})
	result := testOK(t, queryDB(t, k, scoped, f.collection, "", 200, ""))
	if len(result["records"].([]any)) != 0 {
		t.Fatal("Leaked record body")
	}
	testCode(t, queryDB(t, k, scoped, other.collection, "", 200, ""), "SCOPE_DENIED")
}
func TestDatabaseContainersDoNotOwnRows(t *testing.T) {
	k, e := testKernel(t)
	f := newDatabase(t, k, e)
	newRecord(t, k, e, f, "shared")
	container := NewID()
	testOK(t, dbBatch(t, k, e, []any{map[string]any{"kind": "container.create", "containerId": container, "owningPageId": f.page, "containerKind": "database", "label": "Presentation", "parentProjectId": nil}, map[string]any{"kind": "container.linkCollection", "containerId": container, "collectionId": f.collection}}, "container", container, "page", f.page, "collection", f.collection))
	var count int
	k.db.QueryRow("SELECT count(*) FROM records").Scan(&count)
	if count != 1 {
		t.Fatal(count)
	}
}
func TestDatabaseDiskRestartIsolation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "workspace")
	k, err := Create(path)
	if err != nil {
		t.Fatal(err)
	}
	e := k.OwnerContext()
	f := newDatabase(t, k, e)
	r, p := newRecord(t, k, e, f, "body")
	block := NewID()
	testOK(t, dbBatch(t, k, e, []any{map[string]any{"kind": "block.insert", "block": map[string]any{"blockId": block, "pageId": p, "type": "paragraph", "schemaVersion": 1, "data": map[string]any{"richText": []any{map[string]any{"text": "persistent body"}}}}}}, "page", p, "record", r, "collection", f.collection, "block", block))
	k.Close()
	k, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer k.Close()
	body, err := readPageTx(context.Background(), k.db, p)
	if err != nil || len(body.Blocks) != 1 {
		t.Fatal(body, err)
	}
}

func TestDatabaseFilterKeepsMultiSelectBoundaries(t *testing.T) {
	if equalValue([]any{"ab", "c"}, []any{"a", "bc"}) {
		t.Fatal("Distinct options collapsed")
	}
	if !equalValue([]any{"one", "two"}, []any{"two", "one"}) {
		t.Fatal("Selection order changed set membership")
	}
	if matchesFilter(map[string]any{"op": "contains", "propertyId": "p", "value": "partial"}, map[string]any{"p": []any{"partial-id"}}) {
		t.Fatal("Partial option ID matched")
	}
	if !matchesFilter(map[string]any{"op": "contains", "propertyId": "p", "value": "world"}, map[string]any{"p": []any{map[string]any{"text": "Hello world"}}}) {
		t.Fatal("Rich text contains failed")
	}
}

func TestDatabaseClearValuePreservesTaskAndTitleInvariants(t *testing.T) {
	k, e := testKernel(t)
	f := newDatabase(t, k, e)
	r, p := newRecord(t, k, e, f, "Clear values")
	testOK(t, setValue(t, k, e, f, r, p, f.date, "date", map[string]any{"start": "2026-09-30", "end": nil, "timezone": "UTC", "hasTime": false}))
	clear := func(property string) map[string]any {
		return dbBatch(t, k, e, []any{map[string]any{"kind": "record.clearValue", "recordId": r, "propertyId": property}}, "record", r, "page", p, "collection", f.collection, "property", property)
	}
	testOK(t, clear(f.date))
	var count int
	k.db.QueryRow("SELECT count(*) FROM property_values WHERE record_id=? AND property_id=?", r, f.date).Scan(&count)
	if count != 0 {
		t.Fatal("Value was not cleared")
	}
	testCode(t, clear(f.title), "INVALID_REQUEST")
	testOK(t, dbBatch(t, k, e, []any{map[string]any{"kind": "task.promote", "recordId": r, "statusPropertyId": f.status}}, "record", r, "page", p, "collection", f.collection, "property", f.status))
	testCode(t, clear(f.status), "INVALID_REQUEST")
}

func TestDatabaseFileValuesGrantScopedAccessAfterRestart(t *testing.T) {
	k, e := testKernel(t)
	f := newDatabase(t, k, e)
	r, p := newRecord(t, k, e, f, "Attachment row")
	property := NewID()
	testOK(t, dbBatch(t, k, e, []any{map[string]any{"kind": "collection.addProperty", "collectionId": f.collection, "propertyId": property, "name": "Documents", "type": "files", "schemaVersion": 1, "config": map[string]any{}}}, "collection", f.collection, "property", property))
	source := filepath.Join(t.TempDir(), "source.txt")
	if err := os.WriteFile(source, []byte("canonical attachment bytes"), 0600); err != nil {
		t.Fatal(err)
	}
	selection, err := k.RegisterSelection(source)
	if err != nil {
		t.Fatal(err)
	}
	prepared := testOK(t, testDispatch(t, k, e, "prepareFile", map[string]any{"contractVersion": 1, "requestId": NewID(), "workspaceId": k.ID(), "selectionHandle": selection}))
	file := str(prepared["fileId"])
	testOK(t, dbBatch(t, k, e, []any{map[string]any{"kind": "file.attach", "pageId": f.page, "fileId": file, "ingestHandle": prepared["ingestHandle"]}}, "page", f.page, "file", file))
	testOK(t, setValue(t, k, e, f, r, p, property, "files", []any{map[string]any{"fileId": file, "displayName": "Source document"}}))
	path := k.Path()
	k.Close()
	k, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer k.Close()
	e = k.OwnerContext()
	scoped := k.Grant(e.Actor, "user", contracts.DomainScope{Value: contracts.DomainScopeVariant3{Kind: "pages", WorkspaceId: contracts.DomainId(k.ID()), PageIds: []contracts.DomainId{contracts.DomainId(p)}}})
	reader, _, err := k.ReadFile(context.Background(), scoped, file)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	bytes, err := io.ReadAll(reader)
	if err != nil || string(bytes) != "canonical attachment bytes" {
		t.Fatal(string(bytes), err)
	}
}
