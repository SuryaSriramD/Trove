package workspace

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

func pageDeleted(t *testing.T, k *Kernel, id string) bool {
	t.Helper()
	var deleted bool
	if err := k.db.QueryRow("SELECT deleted_at IS NOT NULL FROM pages WHERE id=?", id).Scan(&deleted); err != nil {
		t.Fatal(err)
	}
	return deleted
}
func sourcePageLocal(t *testing.T, p ImportPlan, source string) string {
	t.Helper()
	for _, item := range p.Items {
		if item.Kind == "markdown" && strings.Contains(strings.ReplaceAll(item.Locator, "-", ""), strings.ReplaceAll(source, "-", "")) {
			return item.PageID
		}
	}
	t.Fatal("missing imported source page")
	return ""
}
func approveSourcePageDeletions(t *testing.T, k *Kernel, p ImportPlan) ImportPlan {
	t.Helper()
	return reviewDatabasePreview(t, k, p, map[string]string{"/exists": "take_incoming"})
}
func TestImportWholePageDeletionGraphReviewedUndoAndBackup(t *testing.T) {
	source, se := testKernel(t)
	parent := createPage(t, source, se, "Parent")
	child := NewID()
	testOK(t, dbBatch(t, source, se, []any{map[string]any{"kind": "page.create", "pageId": child, "parentPageId": parent, "title": "Child"}}, "page", parent, "page", child))
	target, te := testKernel(t)
	first := completeImportFixture(t, target, te, portableDatabasePreview(t, source, target))
	lp, lc := sourcePageLocal(t, first, parent), sourcePageLocal(t, first, child)
	testOK(t, dbBatch(t, source, se, []any{map[string]any{"kind": "page.trash", "pageId": parent, "includeDescendants": true}}, "page", parent, "page", child))
	preview := portableDatabasePreview(t, source, target)
	count := 0
	for _, item := range preview.Items {
		if item.Kind == "source_deleted_page" {
			count++
			if item.Merge == nil || len(item.Merge.Fields) != 1 || item.Merge.Fields[0].Outcome != "conflict" {
				t.Fatal("absence lacks explicit review", item)
			}
		}
	}
	if count != 2 {
		t.Fatal("missing whole-page deletion review", count)
	}
	if _, err := commitPortableImport(context.Background(), target, te, map[string]any{"planId": preview.ID, "planHash": preview.Hash}); err == nil {
		t.Fatal("unreviewed page deletion applied")
	}
	done := completeImportFixture(t, target, te, approveSourcePageDeletions(t, target, preview))
	if !pageDeleted(t, target, lp) || !pageDeleted(t, target, lc) {
		t.Fatal("selected graph not trashed")
	}
	testOK(t, dbBatch(t, target, te, []any{map[string]any{"kind": "page.update", "pageId": lc, "title": "Later local title"}}, "page", lc))
	backup := filepath.Join(t.TempDir(), "backup")
	manifest, err := target.CreateBackup(context.Background(), te, backup)
	if err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(t.TempDir(), "restored")
	if _, err = RestoreBackupReviewed(context.Background(), backup, dest, manifest); err != nil {
		t.Fatal(err)
	}
	restored, err := Open(dest)
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()
	schemaUndo(t, restored, restored.OwnerContext(), done.PageDeletionOperationID)
	if pageDeleted(t, restored, lp) || pageDeleted(t, restored, lc) {
		t.Fatal("undo failed")
	}
	var title string
	restored.db.QueryRow("SELECT title FROM pages WHERE id=?", lc).Scan(&title)
	if title != "Later local title" {
		t.Fatal("metadata inverse replaced unrelated edit", title)
	}
	again := portableDatabasePreview(t, source, target)
	for _, item := range again.Items {
		if item.Kind == "source_deleted_page" {
			t.Fatal("accepted absence reproposed")
		}
	}
}
func TestImportWholePageDeletionRejectsUnreviewedChildrenAndStaleGraph(t *testing.T) {
	for _, stale := range []bool{false, true} {
		t.Run(map[bool]string{false: "existing_child", true: "new_child_after_preview"}[stale], func(t *testing.T) {
			source, se := testKernel(t)
			page := createPage(t, source, se, "Source")
			target, te := testKernel(t)
			first := completeImportFixture(t, target, te, portableDatabasePreview(t, source, target))
			local := sourcePageLocal(t, first, page)
			add := func() {
				child := NewID()
				testOK(t, dbBatch(t, target, te, []any{map[string]any{"kind": "page.create", "pageId": child, "parentPageId": local, "title": "Unreviewed local child"}}, "page", child, "page", local))
			}
			if !stale {
				add()
			}
			testOK(t, dbBatch(t, source, se, []any{map[string]any{"kind": "page.trash", "pageId": page, "includeDescendants": false}}, "page", page))
			p := approveSourcePageDeletions(t, target, portableDatabasePreview(t, source, target))
			if stale {
				add()
			}
			if _, err := commitPortableImport(context.Background(), target, te, map[string]any{"planId": p.ID, "planHash": p.Hash}); err == nil {
				t.Fatal("unreviewed child deletion accepted")
			}
			if pageDeleted(t, target, local) {
				t.Fatal("failure partially deleted page")
			}
		})
	}
}
func TestImportWholePageOmissionRequiresVersionedOwnerInventory(t *testing.T) {
	source, se := testKernel(t)
	page := createPage(t, source, se, "Keep without evidence")
	target, te := testKernel(t)
	first := completeImportFixture(t, target, te, portableDatabasePreview(t, source, target))
	local := sourcePageLocal(t, first, page)
	testOK(t, dbBatch(t, source, se, []any{map[string]any{"kind": "page.trash", "pageId": page, "includeDescendants": false}}, "page", page))
	entries := exportPrivacyEntries(t, source, se, false)
	var manifest map[string]any
	json.Unmarshal([]byte(entries["trove-manifest.json"]), &manifest)
	delete(manifest, "pageEnumerationVersion")
	entries["trove-manifest.json"] = string(marshal(manifest))
	p := orderedPortablePreview(t, target, entries, true)
	for _, item := range p.Items {
		if item.Kind == "source_deleted_page" {
			t.Fatal("legacy omission inferred deletion")
		}
	}
	completeImportFixture(t, target, te, p)
	if pageDeleted(t, target, local) {
		t.Fatal("legacy omission deleted page")
	}
}
func TestImportCollectionOwnerDeletionRetainsTypedRecords(t *testing.T) {
	source, se := testKernel(t)
	f := newDatabase(t, source, se)
	property := addImportTextProperty(t, source, se, f, "Keep value")
	record, page := newRecord(t, source, se, f, "Row")
	testOK(t, setValue(t, source, se, f, record, page, property, "text", richImport("recover me")))
	target, te := testKernel(t)
	first := completeImportFixture(t, target, te, portableDatabasePreview(t, source, target))
	localRecord := importedEntity(t, target, "record", record)
	localProperty := importedEntity(t, target, "property", property)
	localPage := sourcePageLocal(t, first, f.page)
	testOK(t, dbBatch(t, source, se, []any{map[string]any{"kind": "page.trash", "pageId": f.page, "includeDescendants": true}}, "page", f.page, "page", page, "record", record, "collection", f.collection))
	p := approveSourcePageDeletions(t, target, portableDatabasePreview(t, source, target))
	done := completeImportFixture(t, target, te, p)
	if !pageDeleted(t, target, localPage) || !recordTrashed(t, target, localRecord) {
		t.Fatal("owner graph deletion incomplete")
	}
	assertSchemaValue(t, target, localRecord, localProperty, "text", richImport("recover me"))
	schemaUndo(t, target, te, done.PageDeletionOperationID)
	if pageDeleted(t, target, localPage) || recordTrashed(t, target, localRecord) {
		t.Fatal("typed graph undo failed")
	}
}

func TestImportWholePageDeletionRejectsNewChildAfterInitialization(t *testing.T) {
	source, se := testKernel(t)
	page := createPage(t, source, se, "Source")
	target, te := testKernel(t)
	first := completeImportFixture(t, target, te, portableDatabasePreview(t, source, target))
	local := sourcePageLocal(t, first, page)
	testOK(t, dbBatch(t, source, se, []any{map[string]any{"kind": "page.trash", "pageId": page, "includeDescendants": false}}, "page", page))
	p := approveSourcePageDeletions(t, target, portableDatabasePreview(t, source, target))
	value, err := commitPortableImport(context.Background(), target, te, map[string]any{"planId": p.ID, "planHash": p.Hash})
	if err != nil {
		t.Fatal(err)
	}
	p = value.(ImportPlan)
	child := NewID()
	testOK(t, dbBatch(t, target, te, []any{map[string]any{"kind": "page.create", "pageId": child, "parentPageId": local, "title": "Late child"}}, "page", child, "page", local))
	rejected := false
	for n := 0; n < len(p.Items)+2 && p.State != "complete"; n++ {
		value, err = commitPortableImport(context.Background(), target, te, map[string]any{"planId": p.ID, "planHash": p.Hash})
		if err != nil {
			rejected = true
			break
		}
		p = value.(ImportPlan)
	}
	if !rejected || pageDeleted(t, target, local) || pageDeleted(t, target, child) {
		t.Fatal("late unreviewed graph change was not rejected atomically")
	}
}
func TestImportWholePageDeletionScopeAndReappearance(t *testing.T) {
	source, se := testKernel(t)
	page := createPage(t, source, se, "Reappearing source")
	target, te := testKernel(t)
	first := completeImportFixture(t, target, te, portableDatabasePreview(t, source, target))
	local := sourcePageLocal(t, first, page)
	testOK(t, dbBatch(t, source, se, []any{map[string]any{"kind": "page.trash", "pageId": page, "includeDescendants": false}}, "page", page))
	entries := exportPrivacyEntries(t, source, se, false)
	var manifest map[string]any
	json.Unmarshal([]byte(entries["trove-manifest.json"]), &manifest)
	manifest["workspaceId"] = NewID()
	entries["trove-manifest.json"] = string(marshal(manifest))
	if _, err := previewRecordDeletionEntries(t, target, te, entries); err == nil || !strings.Contains(err.Error(), "Source workspace enumeration changed") {
		t.Fatal("different owner inventory reused namespace", err)
	}
	p := completeImportFixture(t, target, te, approveSourcePageDeletions(t, target, portableDatabasePreview(t, source, target)))
	testOK(t, dbBatch(t, source, se, []any{map[string]any{"kind": "page.trash.set", "pageId": page, "trashed": false}}, "page", page))
	incoming := portableDatabasePreview(t, source, target)
	if sourcePageLocal(t, incoming, page) != local {
		t.Fatal("source reappearance changed canonical page identity")
	}
	found := false
	for _, item := range incoming.Items {
		if item.PageID == local && item.Merge != nil {
			found = true
			if !item.Merge.LocalDeleted {
				t.Fatal("source reappearance bypassed local tombstone")
			}
		}
	}
	if !found {
		t.Fatal("source reappearance lost reviewed mapping")
	}
	// A later restore/trash cycle is a new metadata decision even if the final
	// boolean matches the older operation; exact tombstone identity blocks undo.
	var parent string
	target.db.QueryRow("SELECT COALESCE(parent_page_id,'') FROM pages WHERE id=?", local).Scan(&parent)
	testOK(t, dbBatch(t, target, te, []any{map[string]any{"kind": "page.trash.set", "pageId": local, "trashed": false}}, "page", local, "page", parent))
	testOK(t, dbBatch(t, target, te, []any{map[string]any{"kind": "page.trash.set", "pageId": local, "trashed": true}}, "page", local))
	if out := testDispatch(t, target, te, "operations.previewUndo", map[string]any{"contractVersion": 1, "requestId": NewID(), "workspaceId": target.ID(), "operationId": p.PageDeletionOperationID}); out["ok"] == true {
		t.Fatal("old page inverse ignored newer tombstone")
	}
}
func TestImportOwnerScopeSurvivesHistoricalBaselineReconstruction(t *testing.T) {
	source, se := testKernel(t)
	f := newDatabase(t, source, se)
	_, _ = newRecord(t, source, se, f, "Known source")
	target, te := testKernel(t)
	completeImportFixture(t, target, te, portableDatabasePreview(t, source, target))
	if _, err := target.db.Exec("DELETE FROM import_baselines"); err != nil {
		t.Fatal(err)
	}
	p := portableDatabasePreview(t, source, target)
	found := false
	for _, item := range p.Items {
		if item.Kind == "portable_collection" {
			found = true
			if item.Merge == nil || item.Merge.BaselineStatus != "retained" || item.Incoming.SourceScope == "" || item.Incoming.PageSourceScope == "" {
				t.Fatal("known versioned scope snapshot could not reconstruct", item.Merge)
			}
		}
	}
	if !found {
		t.Fatal("no typed collection")
	}
}
