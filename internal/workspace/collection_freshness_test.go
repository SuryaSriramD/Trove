package workspace

import (
	"context"
	"testing"
	"trove.local/phase0/contracts"
)

func TestCollectionFreshnessHideUndoAndPresentation(t *testing.T) {
	k, e := testKernel(t)
	f := newDatabase(t, k, e)
	identity := testFreshness(t, k, f.page).ContentIdentity
	testOK(t, dbBatch(t, k, e, []any{map[string]any{"kind": "page.review", "pageId": f.page, "contentIdentity": identity}}, "page", f.page))
	before := testFreshness(t, k, f.page).Review
	testOK(t, dbBatch(t, k, e, []any{map[string]any{"kind": "collection.layout.set", "collectionId": f.collection, "layout": layoutFixture(f.status)}}, "collection", f.collection, "page", f.page))
	if got := testFreshness(t, k, f.page); got.State != "reviewed" {
		t.Fatal("presentation invalidated owner review")
	}
	op := NewID()
	testOK(t, dbBatch(t, k, e, []any{map[string]any{"kind": "operation.apply", "operationId": op, "label": "Hide empty collection", "actions": []any{map[string]any{"kind": "collection.trash.set", "collectionId": f.collection, "trashed": true}}}}, "page", f.page, "collection", f.collection))
	if got := testFreshness(t, k, f.page); got.State != "stale" || got.Review.Id != before.Id {
		t.Fatal("collection visibility missing from semantic knowledge", got)
	}
	schemaUndo(t, k, e, op)
	got := testFreshness(t, k, f.page)
	if got.State != "reviewed" || got.Review.Id != before.Id || got.Review.ReviewedAt != before.ReviewedAt {
		t.Fatal("undo fabricated or lost matching historical review", got)
	}
	var count int
	k.db.QueryRow("SELECT count(*) FROM page_review_events WHERE page_id=?", f.page).Scan(&count)
	if count != 1 {
		t.Fatal("undo created a review")
	}
}
func TestCollectionFreshnessOwnerRowsAndScope(t *testing.T) {
	k, e := testKernel(t)
	f := newDatabase(t, k, e)
	record, page := newRecord(t, k, e, f, "Row")
	identity := testFreshness(t, k, f.page).ContentIdentity
	scoped := k.Grant(e.Actor, "user", contracts.DomainScope{Value: contracts.DomainScopeVariant3{Kind: "pages", WorkspaceId: contracts.DomainId(k.ID()), PageIds: []contracts.DomainId{contracts.DomainId(f.page)}}})
	testCode(t, dbBatch(t, k, scoped, []any{map[string]any{"kind": "page.review", "pageId": f.page, "contentIdentity": identity}}, "page", f.page), "SCOPE_DENIED")
	testOK(t, dbBatch(t, k, e, []any{map[string]any{"kind": "page.review", "pageId": f.page, "contentIdentity": identity}}, "page", f.page))
	testOK(t, setValue(t, k, e, f, record, page, f.date, "date", scheduleTestDate("2026-10-21", "", false, "UTC")))
	if got := testFreshness(t, k, f.page); got.State != "stale" {
		t.Fatal("owner collection review ignored changed row knowledge", got)
	}
}
func TestCollectionHiddenSearchAndAIEvidence(t *testing.T) {
	k, e := testKernel(t)
	f := newDatabase(t, k, e)
	record, page := newRecord(t, k, e, f, "Quasar evidence")
	before, _, err := k.aiSources(context.Background(), e, AIRequest{Mode: "answer", PageID: page, Prompt: "Quasar"})
	if err != nil || len(before) == 0 {
		t.Fatal("fixture has no initial evidence", err)
	}
	hideSourceCollection(t, k, e, f, record, page)
	after, _, err := k.aiSources(context.Background(), e, AIRequest{Mode: "answer", PageID: page, Prompt: "Quasar"})
	if err != nil {
		t.Fatal(err)
	}
	if len(after) != 0 {
		t.Fatal("hidden record still AI evidence", after)
	}
	result := querySearch(t, k, e, "Quasar", nil)
	if len(result["hits"].([]any)) != 0 {
		t.Fatal("hidden record still searchable", result)
	}
}
func TestCollectionVisibilityInitializerAcceptsHistoricalAbsence(t *testing.T) {
	k, _ := testKernel(t)
	if _, err := k.db.Exec("DROP TABLE collection_visibility"); err != nil {
		t.Fatal(err)
	}
	path := k.Path()
	k.Close()
	reopened, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	var count int
	if err = reopened.db.QueryRow("SELECT count(*) FROM collection_visibility").Scan(&count); err != nil || count != 0 {
		t.Fatal("legacy optional visibility was not initialized", err)
	}
}

func TestCollectionFreshnessHiddenReviewDoesNotRenewAfterUndo(t *testing.T) {
	k, e := testKernel(t)
	f := newDatabase(t, k, e)
	op := NewID()
	testOK(t, dbBatch(t, k, e, []any{map[string]any{"kind": "operation.apply", "operationId": op, "label": "Hide", "actions": []any{map[string]any{"kind": "collection.trash.set", "collectionId": f.collection, "trashed": true}}}}, "page", f.page, "collection", f.collection))
	identity := testFreshness(t, k, f.page).ContentIdentity
	testOK(t, dbBatch(t, k, e, []any{map[string]any{"kind": "page.review", "pageId": f.page, "contentIdentity": identity}}, "page", f.page))
	review := testFreshness(t, k, f.page).Review
	schemaUndo(t, k, e, op)
	got := testFreshness(t, k, f.page)
	if got.State != "stale" || got.Review.Id != review.Id || got.Review.ReviewedAt != review.ReviewedAt {
		t.Fatal("undo fabricated review of restored collection", got)
	}
}
func TestCollectionFreshnessLinkedViewsShareKnowledgeNotPresentation(t *testing.T) {
	k, e := testKernel(t)
	f := newDatabase(t, k, e)
	record, row := newRecord(t, k, e, f, "Row")
	linked := createPage(t, k, e, "Linked dashboard")
	view := NewID()
	save := map[string]any{"kind": "view.save", "viewId": view, "collectionId": f.collection, "owningPageId": linked, "name": "View", "type": "table", "schemaVersion": 1, "config": map[string]any{"columnWidths": []any{}}, "query": emptyQuery(f.title)}
	testOK(t, dbBatch(t, k, e, []any{save}, "view", view, "collection", f.collection, "page", linked))
	identity := testFreshness(t, k, linked).ContentIdentity
	testOK(t, dbBatch(t, k, e, []any{map[string]any{"kind": "page.review", "pageId": linked, "contentIdentity": identity}}, "page", linked))
	save["name"] = "Renamed presentation"
	save["query"] = emptyQuery(f.title, f.date)
	testOK(t, dbBatch(t, k, e, []any{save}, "view", view, "collection", f.collection, "page", linked))
	if got := testFreshness(t, k, linked); got.State != "reviewed" {
		t.Fatal("view presentation invalidated knowledge")
	}
	testOK(t, setValue(t, k, e, f, record, row, f.date, "date", scheduleTestDate("2026-10-23", "", false, "UTC")))
	if got := testFreshness(t, k, linked); got.State != "stale" {
		t.Fatal("linked collection ignored row knowledge")
	}
}
