package workspace

import (
	"context"
	"path/filepath"
	"testing"
	"time"
	"trove.local/phase0/contracts"
)

func testFreshness(t *testing.T, k *Kernel, page string) contracts.DomainPageFreshness {
	t.Helper()
	f, err := pageFreshness(context.Background(), k.db, page, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	return f
}
func TestPageFreshnessContentPresentationAndHistory(t *testing.T) {
	k, e := testKernel(t)
	page := createPage(t, k, e, "Reviewed knowledge")
	block := NewID()
	testOK(t, dbBatch(t, k, e, []any{map[string]any{"kind": "block.insert", "block": map[string]any{"blockId": block, "pageId": page, "type": "paragraph", "schemaVersion": 1, "data": map[string]any{"richText": []any{map[string]any{"text": "stable words"}}}}}}, "page", page, "block", block))
	first := testFreshness(t, k, page)
	if first.State != "never_reviewed" {
		t.Fatal(first)
	}
	command := map[string]any{"kind": "page.review", "pageId": page, "contentIdentity": first.ContentIdentity}
	req := testRequest(k, e, []any{command}, dbGuards(t, k, "page", page)...)
	testOK(t, testDispatch(t, k, e, "batch", req))
	testOK(t, testDispatch(t, k, e, "batch", req))
	reviewed := testFreshness(t, k, page)
	if reviewed.State != "reviewed" || reviewed.Review == nil {
		t.Fatal(reviewed)
	}
	var count int
	k.db.QueryRow("SELECT count(*) FROM page_review_events WHERE page_id=?", page).Scan(&count)
	if count != 1 {
		t.Fatal("replay created reviews", count)
	}
	testOK(t, dbBatch(t, k, e, []any{map[string]any{"kind": "page.display.set", "pageId": page, "fullWidth": true, "presentation": true}, map[string]any{"kind": "block.update", "blockId": block, "type": "paragraph", "schemaVersion": 1, "data": map[string]any{"richText": []any{map[string]any{"text": "stable ", "marks": []string{"bold"}}, map[string]any{"text": "words", "marks": []string{"italic"}}}}}}, "page", page, "block", block))
	if got := testFreshness(t, k, page); got.State != "reviewed" || got.ContentIdentity != first.ContentIdentity {
		t.Fatal("presentation invalidated review", got)
	}
	testOK(t, dbBatch(t, k, e, []any{map[string]any{"kind": "page.update", "pageId": page, "title": "Changed knowledge"}}, "page", page))
	if got := testFreshness(t, k, page); got.State != "stale" {
		t.Fatal(got)
	}
	testCode(t, dbBatch(t, k, e, []any{command}, "page", page), "CONFLICT")
	// Returning to the original content reuses its attributable review; never invent a new date.
	testOK(t, dbBatch(t, k, e, []any{map[string]any{"kind": "page.update", "pageId": page, "title": "Reviewed knowledge"}}, "page", page))
	got := testFreshness(t, k, page)
	if got.State != "reviewed" || got.Review.Id != reviewed.Review.Id || got.Review.ReviewedAt != reviewed.Review.ReviewedAt {
		t.Fatal(got)
	}
	future := time.Now().Add(time.Hour).UTC().Format(time.RFC3339Nano)
	command["expiresAt"] = future
	testOK(t, dbBatch(t, k, e, []any{command}, "page", page))
	expired, err := pageFreshness(context.Background(), k.db, page, time.Now().Add(2*time.Hour))
	if err != nil || expired.State != "expired" {
		t.Fatal(expired, err)
	}
	path := k.Path()
	k.Close()
	reopened, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	if got := testFreshness(t, reopened, page); got.State != "reviewed" || got.Review.ExpiresAt == nil {
		t.Fatal(got)
	}
}
func TestPageFreshnessRecordValuesQueueAndAuthority(t *testing.T) {
	k, e := testKernel(t)
	f := newDatabase(t, k, e)
	record, page := newRecord(t, k, e, f, "Task")
	first := testFreshness(t, k, page)
	testOK(t, dbBatch(t, k, e, []any{map[string]any{"kind": "page.review", "pageId": page, "contentIdentity": first.ContentIdentity}}, "page", page, "record", record, "collection", f.collection))
	testOK(t, setValue(t, k, e, f, record, page, f.date, "date", map[string]any{"start": "2026-10-16", "end": nil, "timezone": "Asia/Kolkata", "hasTime": false}))
	if got := testFreshness(t, k, page); got.State != "stale" {
		t.Fatal(got)
	}
	result := testOK(t, testDispatch(t, k, e, "page.reviewQueue", map[string]any{"contractVersion": 1, "requestId": NewID(), "workspaceId": k.ID(), "limit": 1}))
	if len(result["items"].([]any)) != 1 || result["nextPageId"] == nil {
		t.Fatal(result)
	}
	if err := contracts.Validate("service", "ReviewQueueResult", marshal(result)); err != nil {
		t.Fatal(err)
	}
	result = testOK(t, testDispatch(t, k, e, "page.freshness", map[string]any{"contractVersion": 1, "requestId": NewID(), "workspaceId": k.ID(), "pageId": page}))
	if err := contracts.Validate("service", "PageFreshnessResult", marshal(result)); err != nil {
		t.Fatal(err)
	}
	e.Actor.Kind = "local_ai"
	e.Origin = "ai"
	testCode(t, dbBatch(t, k, e, []any{map[string]any{"kind": "page.review", "pageId": page, "contentIdentity": testFreshness(t, k, page).ContentIdentity}}, "page", page, "record", record, "collection", f.collection), "SCOPE_DENIED")
}

func TestPageFreshnessSearchCitationRestoreAndScope(t *testing.T) {
	k, e := testKernel(t)
	page := createPage(t, k, e, "Verifiable garden")
	identity := testFreshness(t, k, page).ContentIdentity
	testOK(t, dbBatch(t, k, e, []any{map[string]any{"kind": "page.review", "pageId": page, "contentIdentity": identity}}, "page", page))
	reviewed := testFreshness(t, k, page).Review
	location := map[string]any{"pageId": page, "sourceRevision": "2"}
	citation := testOK(t, testDispatch(t, k, e, "resolveCitation", advancedRequest(k, map[string]any{"location": location})))
	if citation["freshness"].(map[string]any)["state"] != "reviewed" {
		t.Fatal(citation)
	}
	search := testOK(t, testDispatch(t, k, e, "search.query", advancedRequest(k, map[string]any{"query": "garden", "mode": "keyword", "limit": 10})))
	hits := search["hits"].([]any)
	if len(hits) == 0 || hits[0].(map[string]any)["freshness"].(map[string]any)["state"] != "reviewed" {
		t.Fatal(search)
	}
	testOK(t, dbBatch(t, k, e, []any{map[string]any{"kind": "page.update", "pageId": page, "title": "Changed"}}, "page", page))
	testOK(t, restoreTest(t, k, e, page, "2"))
	if got := testFreshness(t, k, page); got.State != "reviewed" || got.Review.Id != reviewed.Id || got.Review.ReviewedAt != reviewed.ReviewedAt {
		t.Fatal("restore fabricated a review", got)
	}
	outsider := k.Grant(contracts.ServiceActor{Kind: "user", ActorId: contracts.DomainId(NewID())}, "user", contracts.DomainScope{Value: contracts.DomainScopeVariant3{Kind: "pages", WorkspaceId: contracts.DomainId(k.ID()), PageIds: []contracts.DomainId{contracts.DomainId(NewID())}}})
	testCode(t, testDispatch(t, k, outsider, "page.freshness", advancedRequest(k, map[string]any{"pageId": page})), "SCOPE_DENIED")
	queue := testOK(t, testDispatch(t, k, outsider, "page.reviewQueue", advancedRequest(k, map[string]any{"limit": 10})))
	if len(queue["items"].([]any)) != 0 {
		t.Fatal("queue leaked inaccessible page")
	}
	backup := filepath.Join(t.TempDir(), "review-backup")
	if _, err := k.CreateBackup(context.Background(), e, backup); err != nil {
		t.Fatal(err)
	}
	restored, err := Open(backup)
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()
	if got := testFreshness(t, restored, page); got.Review.Id != reviewed.Id || got.Review.ReviewedAt != reviewed.ReviewedAt {
		t.Fatal("native backup lost review attribution", got)
	}
}

func TestPageFreshnessReusableSourceAndFormattedRecordValues(t *testing.T) {
	k, e := testKernel(t)
	sourcePage := createPage(t, k, e, "Source")
	page := createPage(t, k, e, "Reader")
	source, reference := NewID(), NewID()
	testOK(t, dbBatch(t, k, e, []any{map[string]any{"kind": "block.insert", "block": map[string]any{"blockId": source, "pageId": sourcePage, "type": "paragraph", "schemaVersion": 1, "data": map[string]any{"richText": richImport("Original")}}}, map[string]any{"kind": "block.insert", "block": map[string]any{"blockId": reference, "pageId": page, "type": "synced_block", "schemaVersion": 1, "data": map[string]any{"contentSourceId": source}}}}, "page", sourcePage, "page", page, "block", source, "block", reference))
	identity := testFreshness(t, k, page).ContentIdentity
	testOK(t, dbBatch(t, k, e, []any{map[string]any{"kind": "page.review", "pageId": page, "contentIdentity": identity}}, "page", page))
	testOK(t, dbBatch(t, k, e, []any{map[string]any{"kind": "block.update", "blockId": source, "type": "paragraph", "schemaVersion": 1, "data": map[string]any{"richText": richImport("Changed")}}}, "page", sourcePage, "block", source))
	if got := testFreshness(t, k, page); got.State != "stale" {
		t.Fatal("reusable source change hidden", got)
	}
	f := newDatabase(t, k, e)
	record, recordPage := newRecord(t, k, e, f, "Record")
	text, formula := NewID(), NewID()
	testOK(t, dbBatch(t, k, e, []any{map[string]any{"kind": "collection.addProperty", "collectionId": f.collection, "propertyId": text, "name": "Text", "type": "text", "schemaVersion": 1, "config": map[string]any{}}, map[string]any{"kind": "collection.addProperty", "collectionId": f.collection, "propertyId": formula, "name": "Calculation", "type": "formula", "schemaVersion": 1, "config": derivedConfig("formula", map[string]any{"translatedExpression": "1", "evaluatorVersion": 2})}}, "collection", f.collection, "property", text, "property", formula))
	testOK(t, setValue(t, k, e, f, record, recordPage, text, "text", richImport("Same words")))
	identity = testFreshness(t, k, recordPage).ContentIdentity
	testOK(t, dbBatch(t, k, e, []any{map[string]any{"kind": "page.review", "pageId": recordPage, "contentIdentity": identity}}, "page", recordPage, "record", record, "collection", f.collection))
	testOK(t, setValue(t, k, e, f, record, recordPage, text, "text", []any{map[string]any{"text": "Same ", "marks": []string{"bold"}}, map[string]any{"text": "words", "marks": []string{"italic"}}}))
	if got := testFreshness(t, k, recordPage); got.State != "reviewed" {
		t.Fatal("record formatting invalidated review", got)
	}
	testOK(t, dbBatch(t, k, e, []any{map[string]any{"kind": "property.setFormula", "propertyId": formula, "expression": "2", "evaluatorVersion": 2}}, "property", formula, "collection", f.collection, "page", f.page))
	if got := testFreshness(t, k, recordPage); got.State != "stale" {
		t.Fatal("formula definition change hidden", got)
	}
}

func TestPageFreshnessReusableTodoTracksDueBinding(t *testing.T) {
	k, e := testKernel(t)
	f := newDatabase(t, k, e)
	record, taskPage := newRecord(t, k, e, f, "Task")
	secondDue := NewID()
	testOK(t, dbBatch(t, k, e, []any{map[string]any{"kind": "collection.addProperty", "collectionId": f.collection, "propertyId": secondDue, "name": "Alternate due", "type": "date", "schemaVersion": 1, "config": map[string]any{}}}, "collection", f.collection, "property", secondDue))
	promote := func(due string) {
		testOK(t, dbBatch(t, k, e, []any{map[string]any{"kind": "task.promote", "recordId": record, "statusPropertyId": f.status, "duePropertyId": due}}, "page", taskPage, "record", record, "collection", f.collection, "property", f.status, "property", due))
	}
	promote(f.date)
	sourcePage := createPage(t, k, e, "Source task reference")
	page := createPage(t, k, e, "Shared task reference")
	todo, synced := NewID(), NewID()
	testOK(t, dbBatch(t, k, e, []any{map[string]any{"kind": "block.insert", "block": map[string]any{"blockId": todo, "pageId": sourcePage, "type": "todo", "schemaVersion": 1, "data": map[string]any{"taskRecordId": record}}}, map[string]any{"kind": "block.insert", "block": map[string]any{"blockId": synced, "pageId": page, "type": "synced_block", "schemaVersion": 1, "data": map[string]any{"contentSourceId": todo}}}}, "page", sourcePage, "page", page, "block", todo, "block", synced))
	identity := testFreshness(t, k, page).ContentIdentity
	testOK(t, dbBatch(t, k, e, []any{map[string]any{"kind": "page.review", "pageId": page, "contentIdentity": identity}}, "page", page))
	promote(secondDue)
	if got := testFreshness(t, k, page); got.State != "stale" {
		t.Fatal("reused todo binding change hidden", got)
	}
}
