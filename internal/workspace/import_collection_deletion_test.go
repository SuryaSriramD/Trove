package workspace

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"trove.local/phase0/contracts"
)

func hideSourceCollection(t *testing.T, k *Kernel, e contracts.ServiceExecutionContext, f databaseFixture, record, page string) {
	t.Helper()
	commands := []any{}
	guards := []string{"page", f.page, "collection", f.collection}
	if record != "" {
		commands = append(commands, map[string]any{"kind": "record.trash.set", "recordId": record, "trashed": true})
		guards = append(guards, "record", record, "page", page)
	}
	commands = append(commands, map[string]any{"kind": "collection.trash.set", "collectionId": f.collection, "trashed": true})
	testOK(t, dbBatch(t, k, e, commands, guards...))
}
func approveCollectionRemoval(t *testing.T, k *Kernel, p ImportPlan) ImportPlan {
	t.Helper()
	choices := map[string]string{}
	for _, item := range p.Items {
		if item.Kind == "source_deleted_collection" {
			for _, field := range item.Merge.Fields {
				choices[field.Path] = "take_incoming"
			}
		}
	}
	return reviewDatabasePreview(t, k, p, choices)
}
func TestImportIndependentCollectionDeletionReviewUndoBackup(t *testing.T) {
	source, se := testKernel(t)
	f := newDatabase(t, source, se)
	property := addImportTextProperty(t, source, se, f, "Retained")
	record, page := newRecord(t, source, se, f, "Row")
	testOK(t, setValue(t, source, se, f, record, page, property, "text", richImport("base")))
	target, te := testKernel(t)
	first := completeImportFixture(t, target, te, portableDatabasePreview(t, source, target))
	local := importedEntity(t, target, "collection", f.collection)
	lr := importedEntity(t, target, "record", record)
	lp := importedEntity(t, target, "property", property)
	owner := sourcePageLocal(t, first, f.page)
	// Local-only records also belong in the exact deletion review.
	extra, extraPage := newRecord(t, target, te, databaseFixture{collection: local}, "Local row")
	hideSourceCollection(t, source, se, f, record, page)
	preview := portableDatabasePreview(t, source, target)
	found := false
	for _, item := range preview.Items {
		if item.Kind == "source_deleted_collection" {
			found = true
			if len(item.Merge.Fields) != 3 {
				t.Fatal("missing local record review", string(marshal(item.Merge)))
			}
		}
		if item.Kind == "source_deleted_page" {
			t.Fatal("collection member independently proposed twice")
		}
	}
	if !found {
		t.Fatal("missing collection deletion review", string(marshal(preview.Items)))
	}
	if _, err := commitPortableImport(context.Background(), target, te, map[string]any{"planId": preview.ID, "planHash": preview.Hash}); err == nil {
		t.Fatal("unreviewed deletion applied")
	}
	done := completeImportFixture(t, target, te, approveCollectionRemoval(t, target, preview))
	if !collectionHidden(t, target, local) || !recordTrashed(t, target, lr) || !recordTrashed(t, target, extra) || pageDeleted(t, target, owner) {
		t.Fatal("collection removal changed wrong graph")
	}
	assertSchemaValue(t, target, lr, lp, "text", richImport("base"))
	testOK(t, dbBatch(t, target, te, []any{map[string]any{"kind": "page.update", "pageId": owner, "title": "Later local title"}}, "page", owner))
	backup := filepath.Join(t.TempDir(), "backup")
	manifest, err := target.CreateBackup(context.Background(), te, backup)
	if err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(t.TempDir(), "restore")
	if _, err = RestoreBackupReviewed(context.Background(), backup, dest, manifest); err != nil {
		t.Fatal(err)
	}
	restored, err := Open(dest)
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()
	schemaUndo(t, restored, restored.OwnerContext(), done.PageDeletionOperationID)
	if collectionHidden(t, restored, local) || recordTrashed(t, restored, lr) || pageDeleted(t, restored, extraPage) {
		t.Fatal("undo did not restore collection before records")
	}
	assertSchemaValue(t, restored, lr, lp, "text", richImport("base"))
	var title string
	restored.db.QueryRow("SELECT title FROM pages WHERE id=?", owner).Scan(&title)
	if title != "Later local title" {
		t.Fatal("undo replaced unrelated edit")
	}
	for _, item := range portableDatabasePreview(t, source, target).Items {
		if item.Kind == "source_deleted_collection" {
			t.Fatal("accepted source absence reproposed")
		}
	}
}
func TestImportIndependentCollectionDeletionEmptyAndIncompleteInventory(t *testing.T) {
	for _, legacy := range []bool{false, true} {
		t.Run(map[bool]string{false: "empty", true: "legacy"}[legacy], func(t *testing.T) {
			source, se := testKernel(t)
			f := newDatabase(t, source, se)
			target, te := testKernel(t)
			completeImportFixture(t, target, te, portableDatabasePreview(t, source, target))
			local := importedEntity(t, target, "collection", f.collection)
			hideSourceCollection(t, source, se, f, "", "")
			entries := exportPrivacyEntries(t, source, se, false)
			if legacy {
				var m map[string]any
				json.Unmarshal([]byte(entries["trove-manifest.json"]), &m)
				delete(m, "collectionEnumerationVersion")
				entries["trove-manifest.json"] = string(marshal(m))
			}
			raw, err := previewRecordDeletionEntries(t, target, te, entries)
			if err != nil {
				t.Fatal(err)
			}
			p := raw
			for _, item := range p.Items {
				if legacy && item.Kind == "source_deleted_collection" {
					t.Fatal("legacy inferred deletion")
				}
			}
			p = completeImportFixture(t, target, te, approveCollectionRemoval(t, target, p))
			if collectionHidden(t, target, local) == legacy {
				t.Fatal("incorrect collection visibility")
			}
			if !legacy {
				schemaUndo(t, target, te, p.PageDeletionOperationID)
				if collectionHidden(t, target, local) {
					t.Fatal("empty collection undo failed")
				}
			}
		})
	}
}
func TestImportIndependentCollectionDeletionRejectsPartialReviewChildrenAndLateRecord(t *testing.T) {
	for _, scenario := range []string{"partial_review", "child", "late_record"} {
		t.Run(scenario, func(t *testing.T) {
			source, se := testKernel(t)
			f := newDatabase(t, source, se)
			record, page := newRecord(t, source, se, f, "Row")
			target, te := testKernel(t)
			completeImportFixture(t, target, te, portableDatabasePreview(t, source, target))
			local := importedEntity(t, target, "collection", f.collection)
			lr := importedEntity(t, target, "record", record)
			lpage := importedRecordPage(t, target, lr)
			if scenario == "child" {
				child := NewID()
				testOK(t, dbBatch(t, target, te, []any{map[string]any{"kind": "page.create", "pageId": child, "parentPageId": lpage, "title": "Unreviewed child"}}, "page", child, "page", lpage))
			}
			hideSourceCollection(t, source, se, f, record, page)
			p := portableDatabasePreview(t, source, target)
			if scenario == "partial_review" {
				p = reviewDatabasePreview(t, target, p, map[string]string{"/collectionExists": "take_incoming", "/collectionRecords/" + lr: "keep_local"})
			} else {
				p = approveCollectionRemoval(t, target, p)
			}
			if scenario == "late_record" {
				value, err := commitPortableImport(context.Background(), target, te, map[string]any{"planId": p.ID, "planHash": p.Hash})
				if err != nil {
					t.Fatal(err)
				}
				p = value.(ImportPlan)
				newRecord(t, target, te, databaseFixture{collection: local}, "Late row")
			}
			rejected := false
			for n := 0; n < len(p.Items)+3 && p.State != "complete"; n++ {
				value, err := commitPortableImport(context.Background(), target, te, map[string]any{"planId": p.ID, "planHash": p.Hash})
				if err != nil {
					rejected = true
					break
				}
				p = value.(ImportPlan)
			}
			if !rejected || collectionHidden(t, target, local) || recordTrashed(t, target, lr) {
				t.Fatal("unsafe graph deletion applied")
			}
		})
	}
}
func TestImportIndependentCollectionDeletionRejectsImplicitDemotion(t *testing.T) {
	source, se := testKernel(t)
	f := newDatabase(t, source, se)
	record, _ := newRecord(t, source, se, f, "Live ordinary source page")
	target, te := testKernel(t)
	completeImportFixture(t, target, te, portableDatabasePreview(t, source, target))
	local := importedEntity(t, target, "collection", f.collection)
	lr := importedEntity(t, target, "record", record)
	entries := exportPrivacyEntries(t, source, se, false)
	var manifest map[string]any
	json.Unmarshal([]byte(entries["trove-manifest.json"]), &manifest)
	for _, c := range manifest["collections"].([]any) {
		delete(entries, str(c.(map[string]any)["path"]))
	}
	manifest["collections"] = []any{}
	entries["trove-manifest.json"] = string(marshal(manifest))
	raw, err := previewRecordDeletionEntries(t, target, te, entries)
	if err != nil {
		t.Fatal(err)
	}
	p := approveCollectionRemoval(t, target, raw)
	_, err = commitPortableImport(context.Background(), target, te, map[string]any{"planId": p.ID, "planHash": p.Hash})
	if err == nil || !strings.Contains(err.Error(), "membership migration") {
		t.Fatal("implicit demotion accepted", err)
	}
	if collectionHidden(t, target, local) || recordTrashed(t, target, lr) {
		t.Fatal("live source content lost")
	}
}

func TestImportIndependentCollectionReappearancePreservesLocalVisibility(t *testing.T) {
	source, se := testKernel(t)
	f := newDatabase(t, source, se)
	record, page := newRecord(t, source, se, f, "Row")
	target, te := testKernel(t)
	completeImportFixture(t, target, te, portableDatabasePreview(t, source, target))
	local := importedEntity(t, target, "collection", f.collection)
	lr := importedEntity(t, target, "record", record)
	hideSourceCollection(t, source, se, f, record, page)
	completeImportFixture(t, target, te, approveCollectionRemoval(t, target, portableDatabasePreview(t, source, target)))
	testOK(t, dbBatch(t, source, se, []any{map[string]any{"kind": "collection.trash.set", "collectionId": f.collection, "trashed": false}, map[string]any{"kind": "record.trash.set", "recordId": record, "trashed": false}}, "page", f.page, "collection", f.collection, "page", page, "record", record))
	p := portableDatabasePreview(t, source, target)
	choices := []any{}
	for _, item := range p.Items {
		if item.Merge != nil {
			choices = append(choices, map[string]any{"sourceLocator": item.Locator, "choice": "keep_local"})
		}
	}
	configured, err := configurePortableImport(context.Background(), target, te, map[string]any{"planId": p.ID, "planHash": p.Hash, "choices": choices})
	if err != nil {
		t.Fatal(err)
	}
	p = completeImportFixture(t, target, te, configured.(ImportPlan))
	if !collectionHidden(t, target, local) || !recordTrashed(t, target, lr) {
		t.Fatal("source reappearance reactivated local graph")
	}
	if got := importedEntity(t, target, "collection", f.collection); got != local {
		t.Fatal("source reappearance changed collection identity")
	}
}
func TestImportIndependentCollectionEnumerationScopeChangeRejected(t *testing.T) {
	source, se := testKernel(t)
	f := newDatabase(t, source, se)
	target, te := testKernel(t)
	completeImportFixture(t, target, te, portableDatabasePreview(t, source, target))
	hideSourceCollection(t, source, se, f, "", "")
	entries := exportPrivacyEntries(t, source, se, false)
	var manifest map[string]any
	json.Unmarshal([]byte(entries["trove-manifest.json"]), &manifest)
	manifest["workspaceId"] = NewID()
	entries["trove-manifest.json"] = string(marshal(manifest))
	if _, err := previewRecordDeletionEntries(t, target, te, entries); err == nil {
		t.Fatal("changed owner scope impersonated prior collection inventory")
	}
}
