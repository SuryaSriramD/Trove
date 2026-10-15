package workspace

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"trove.local/phase0/contracts"
)

func importedRecordPage(t *testing.T, k *Kernel, id string) string {
	t.Helper()
	var page string
	if err := k.db.QueryRow("SELECT page_id FROM records WHERE id=?", id).Scan(&page); err != nil {
		t.Fatal(err)
	}
	return page
}
func recordTrashed(t *testing.T, k *Kernel, id string) bool {
	t.Helper()
	var deleted bool
	if err := k.db.QueryRow("SELECT p.deleted_at IS NOT NULL FROM records r JOIN pages p ON p.id=r.page_id WHERE r.id=?", id).Scan(&deleted); err != nil {
		t.Fatal(err)
	}
	return deleted
}
func deleteRecordAtSource(t *testing.T, k *Kernel, e contracts.ServiceExecutionContext, f databaseFixture, record, page string) {
	t.Helper()
	testOK(t, dbBatch(t, k, e, []any{map[string]any{"kind": "page.trash", "pageId": page, "includeDescendants": false}}, "record", record, "page", page, "collection", f.collection))
}
func TestImportSourceRecordDeletionReviewUndoBackupAndIdempotentRetry(t *testing.T) {
	source, se := testKernel(t)
	f := newDatabase(t, source, se)
	property := addImportTextProperty(t, source, se, f, "Notes")
	record, page := newRecord(t, source, se, f, "Source row")
	testOK(t, setValue(t, source, se, f, record, page, property, "text", richImport("base")))
	target, te := testKernel(t)
	completeImportFixture(t, target, te, portableDatabasePreview(t, source, target))
	local := importedEntity(t, target, "record", record)
	localPage := importedRecordPage(t, target, local)
	localProperty := importedEntity(t, target, "property", property)
	localCollection := importedEntity(t, target, "collection", f.collection)
	testOK(t, setValue(t, target, te, databaseFixture{collection: localCollection}, local, localPage, localProperty, "text", richImport("local edits survive in trash")))
	prepared := stageTestFile(t, target, te, "local retained attachment")
	file := str(prepared["fileId"])
	testOK(t, dbBatch(t, target, te, []any{map[string]any{"kind": "file.attach", "pageId": localPage, "fileId": file, "ingestHandle": prepared["ingestHandle"]}}, "page", localPage, "record", local, "collection", localCollection, "file", file))
	deleteRecordAtSource(t, source, se, f, record, page)
	preview := portableDatabasePreview(t, source, target)
	path := "/records/" + local
	found := false
	for _, item := range preview.Items {
		if item.Merge != nil {
			for _, field := range item.Merge.Fields {
				if field.Path == path {
					found = true
					if field.Outcome != "conflict" || field.Incoming.Present {
						t.Fatal("deletion not explicit", field)
					}
				}
			}
		}
	}
	if !found {
		t.Fatal("missing deleted source record review")
	}
	if _, err := commitPortableImport(context.Background(), target, te, map[string]any{"planId": preview.ID, "planHash": preview.Hash}); err == nil {
		t.Fatal("unreviewed deletion applied")
	}
	preview = reviewDatabasePreview(t, target, preview, map[string]string{path: "take_incoming"})
	done := completeImportFixture(t, target, te, preview)
	if !recordTrashed(t, target, local) {
		t.Fatal("record not trashed")
	}
	assertSchemaValue(t, target, local, localProperty, "text", richImport("local edits survive in trash"))
	// Resume/replay the accepted plan cannot duplicate its journal or tombstone.
	if _, err := commitPortableImport(context.Background(), target, te, map[string]any{"planId": done.ID, "planHash": done.Hash}); err != nil {
		t.Fatal(err)
	}
	var collectionPage string
	target.db.QueryRow("SELECT owning_page_id FROM collections WHERE id=?", localCollection).Scan(&collectionPage)
	operations := testOK(t, testDispatch(t, target, te, "operations.list", map[string]any{"contractVersion": 1, "requestId": NewID(), "workspaceId": target.ID(), "pageId": collectionPage}))
	if len(operations["operations"].([]any)) != 1 {
		t.Fatal("collection does not expose deletion undo", operations)
	}
	// Full subsequent enumeration does not repropose an already accepted tombstone.
	again := portableDatabasePreview(t, source, target)
	for _, item := range again.Items {
		if item.Merge != nil {
			for _, field := range item.Merge.Fields {
				if field.Path == path && field.Outcome == "conflict" {
					t.Fatal("accepted source tombstone repeated")
				}
			}
		}
	}
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
	schemaUndo(t, restored, restored.OwnerContext(), preview.ID)
	if recordTrashed(t, restored, local) {
		t.Fatal("undo failed")
	}
	assertSchemaValue(t, restored, local, localProperty, "text", richImport("local edits survive in trash"))
	var references int
	if err := restored.db.QueryRow("SELECT count(*) FROM file_references WHERE page_id=? AND file_id=?", localPage, file).Scan(&references); err != nil || references == 0 {
		t.Fatal("record recovery lost attachment relationship", err)
	}
}
func TestImportRecordOmissionWithoutEnumerationProofPreservesRows(t *testing.T) {
	source, se := testKernel(t)
	f := newDatabase(t, source, se)
	record, page := newRecord(t, source, se, f, "Retain")
	target, te := testKernel(t)
	completeImportFixture(t, target, te, portableDatabasePreview(t, source, target))
	local := importedEntity(t, target, "record", record)
	deleteRecordAtSource(t, source, se, f, record, page)
	archive := filepath.Join(t.TempDir(), "source.zip")
	if _, err := source.ExportPortable(context.Background(), se, archive); err != nil {
		t.Fatal(err)
	}
	entries := portableEntries(t, archive)
	var manifest map[string]any
	if err := json.Unmarshal([]byte(entries["trove-manifest.json"]), &manifest); err != nil {
		t.Fatal(err)
	}
	for _, raw := range manifest["collections"].([]any) {
		delete(raw.(map[string]any), "recordEnumerationVersion")
	}
	entries["trove-manifest.json"] = string(marshal(manifest))
	preview, err := previewRecordDeletionEntries(t, target, te, entries)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range preview.PortableCollections {
		if c.RecordEnumerationVersion != 0 {
			t.Fatal("unknown enumeration upgraded")
		}
	}
	choices := map[string]string{}
	for _, item := range preview.Items {
		if item.Merge != nil {
			for _, field := range item.Merge.Fields {
				if field.Outcome == "conflict" {
					if strings.HasPrefix(field.Path, "/records/") {
						t.Fatal("legacy record omission became conflict", field)
					}
					choices[field.Path] = "keep_local"
				}
			}
		}
	}
	preview = reviewDatabasePreview(t, target, preview, choices)
	completeImportFixture(t, target, te, preview)
	// A direct falsified deletion review is refused even with canonical IDs.
	item := ImportItem{Kind: "portable_collection", CollectionID: importedEntity(t, target, "collection", f.collection), Merge: &ImportMergeReview{Fields: []ImportFieldDecision{{Path: "/records/" + local, Base: ImportPresence{Present: true, Value: marshal(importedRecordPage(t, target, local))}, Local: ImportPresence{Present: true, Value: marshal(importedRecordPage(t, target, local))}}}, Resolutions: []ImportFieldChoice{{Path: "/records/" + local, Choice: "take_incoming"}}}, ResolvedFields: map[string]json.RawMessage{}}
	p := ImportPlan{PortableCollections: []PortableCollectionImport{{CollectionID: item.CollectionID}}}
	if _, err := importRecordDeletions(context.Background(), target.db, &p, &item); err == nil {
		t.Fatal("incomplete enumeration deleted record")
	}
	if recordTrashed(t, target, local) {
		t.Fatal("legacy omission deleted record")
	}
}
func TestImportRecordDeletionRejectsStaleReviewAndLiveChildren(t *testing.T) {
	for _, which := range []string{"stale", "child"} {
		t.Run(which, func(t *testing.T) {
			source, se := testKernel(t)
			f := newDatabase(t, source, se)
			record, page := newRecord(t, source, se, f, "Record")
			target, te := testKernel(t)
			completeImportFixture(t, target, te, portableDatabasePreview(t, source, target))
			local := importedEntity(t, target, "record", record)
			localPage := importedRecordPage(t, target, local)
			collection := importedEntity(t, target, "collection", f.collection)
			if which == "child" {
				child := NewID()
				testOK(t, dbBatch(t, target, te, []any{map[string]any{"kind": "page.create", "pageId": child, "parentPageId": localPage, "title": "Local child"}}, "page", child, "page", localPage))
			}
			deleteRecordAtSource(t, source, se, f, record, page)
			preview := portableDatabasePreview(t, source, target)
			preview = reviewDatabasePreview(t, target, preview, map[string]string{"/records/" + local: "take_incoming"})
			if which == "stale" {
				testOK(t, dbBatch(t, target, te, []any{map[string]any{"kind": "page.update", "pageId": localPage, "title": "Newer local edit"}}, "page", localPage, "record", local, "collection", collection))
			}
			_, err := commitPortableImport(context.Background(), target, te, map[string]any{"planId": preview.ID, "planHash": preview.Hash})
			if err == nil {
				t.Fatal("unsafe deletion accepted")
			}
			if which == "child" && !strings.Contains(err.Error(), "child pages") {
				t.Fatal(err)
			}
			if recordTrashed(t, target, local) {
				t.Fatal("rejected deletion changed record")
			}
		})
	}
}
func TestRecordTrashUndoRejectsLaterTombstoneAndRetainsLaterUnrelatedValues(t *testing.T) {
	k, e := testKernel(t)
	f := newDatabase(t, k, e)
	record, page := newRecord(t, k, e, f, "Record")
	op := NewID()
	action := map[string]any{"kind": "record.trash.set", "recordId": record, "trashed": true}
	testOK(t, dbBatch(t, k, e, []any{map[string]any{"kind": "operation.apply", "operationId": op, "label": "Delete", "actions": []any{action}}}, "page", page, "record", record, "collection", f.collection))
	testOK(t, dbBatch(t, k, e, []any{map[string]any{"kind": "record.trash.set", "recordId": record, "trashed": false}}, "page", page, "record", record, "collection", f.collection, "page", f.page))
	testOK(t, dbBatch(t, k, e, []any{action}, "page", page, "record", record, "collection", f.collection))
	testCode(t, testDispatch(t, k, e, "operations.previewUndo", map[string]any{"contractVersion": 1, "requestId": NewID(), "workspaceId": k.ID(), "operationId": op}), "CONFLICT")
}

func TestImportLocalRecordTombstoneNeverResurrectsFromUnchangedSource(t *testing.T) {
	source, se := testKernel(t)
	f := newDatabase(t, source, se)
	record, page := newRecord(t, source, se, f, "Record")
	property := addImportTextProperty(t, source, se, f, "Notes")
	testOK(t, setValue(t, source, se, f, record, page, property, "text", richImport("same source")))
	target, te := testKernel(t)
	completeImportFixture(t, target, te, portableDatabasePreview(t, source, target))
	local := importedEntity(t, target, "record", record)
	localPage := importedRecordPage(t, target, local)
	collection := importedEntity(t, target, "collection", f.collection)
	deleteRecordAtSource(t, target, te, databaseFixture{collection: collection}, local, localPage)
	preview := portableDatabasePreview(t, source, target)
	choices := []any{}
	for _, item := range preview.Items {
		if item.Merge != nil && item.Merge.LocalDeleted {
			choices = append(choices, map[string]any{"sourceLocator": item.Locator, "choice": "keep_local"})
		}
	}
	if len(choices) != 1 {
		t.Fatal("tombstone page missing from review")
	}
	configured, err := configurePortableImport(context.Background(), target, te, map[string]any{"planId": preview.ID, "planHash": preview.Hash, "choices": choices})
	if err != nil {
		t.Fatal(err)
	}
	preview = reviewDatabasePreview(t, target, configured.(ImportPlan), map[string]string{})
	completeImportFixture(t, target, te, preview)
	if !recordTrashed(t, target, local) {
		t.Fatal("unchanged source resurrected record")
	}
}
func TestImportCombinedSchemaAndRecordDeletionUndoKeepsValues(t *testing.T) {
	source, se := testKernel(t)
	f := newDatabase(t, source, se)
	record, page := newRecord(t, source, se, f, "Record")
	property := addImportTextProperty(t, source, se, f, "Notes")
	testOK(t, setValue(t, source, se, f, record, page, property, "text", richImport("recover exact value")))
	target, te := testKernel(t)
	completeImportFixture(t, target, te, portableDatabasePreview(t, source, target))
	local := importedEntity(t, target, "record", record)
	localProperty := importedEntity(t, target, "property", property)
	testOK(t, dbBatch(t, source, se, []any{schemaValuesAction(f.collection, property, nil)}, "page", f.page, "collection", f.collection, "property", property, "page", page, "record", record))
	deleteRecordAtSource(t, source, se, f, record, page)
	preview := portableDatabasePreview(t, source, target)
	preview = reviewDatabasePreview(t, target, preview, map[string]string{"/records/" + local: "take_incoming", "/schema/" + localProperty: "take_incoming", "/values/" + local + "/" + localProperty: "take_incoming"})
	completeImportFixture(t, target, te, preview)
	if !recordTrashed(t, target, local) {
		t.Fatal("record not deleted")
	}
	schemaUndo(t, target, te, preview.ID)
	if recordTrashed(t, target, local) {
		t.Fatal("record undo failed")
	}
	assertSchemaValue(t, target, local, localProperty, "text", richImport("recover exact value"))
}

func previewRecordDeletionEntries(t *testing.T, k *Kernel, e contracts.ServiceExecutionContext, entries map[string]string) (ImportPlan, error) {
	t.Helper()
	var archiveBytes bytes.Buffer
	zw := zip.NewWriter(&archiveBytes)
	for name, body := range entries {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = w.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "modified.zip")
	if err := os.WriteFile(path, archiveBytes.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	handle, err := k.RegisterSelection(path)
	if err != nil {
		return ImportPlan{}, err
	}
	result, err := previewPortableImport(context.Background(), k, e, map[string]any{"sourceHandle": handle, "sourceNamespace": "typed-merge"})
	if err != nil {
		return ImportPlan{}, err
	}
	return result.(ImportPlan), nil
}
func TestImportRecordDeletionEnumerationRejectsChangedSourceWorkspace(t *testing.T) {
	source, se := testKernel(t)
	f := newDatabase(t, source, se)
	record, page := newRecord(t, source, se, f, "Keep scoped identity")
	target, te := testKernel(t)
	completeImportFixture(t, target, te, portableDatabasePreview(t, source, target))
	local := importedEntity(t, target, "record", record)
	deleteRecordAtSource(t, source, se, f, record, page)
	entries := exportPrivacyEntries(t, source, se, false)
	var manifest map[string]any
	if err := json.Unmarshal([]byte(entries["trove-manifest.json"]), &manifest); err != nil {
		t.Fatal(err)
	}
	for _, raw := range manifest["collections"].([]any) {
		raw.(map[string]any)["sourceWorkspaceId"] = NewID()
	}
	entries["trove-manifest.json"] = string(marshal(manifest))
	if _, err := previewRecordDeletionEntries(t, target, te, entries); err == nil || !strings.Contains(err.Error(), "source workspace or collection changed") {
		t.Fatal("changed source namespace impersonated prior enumeration", err)
	}
	if recordTrashed(t, target, local) {
		t.Fatal("scope mismatch deleted record")
	}
}
