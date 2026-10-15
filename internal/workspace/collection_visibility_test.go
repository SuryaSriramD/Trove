package workspace

import (
	"context"
	"database/sql"
	"encoding/json"
	"path/filepath"
	"testing"
)

func collectionHidden(t *testing.T, k *Kernel, id string) bool {
	t.Helper()
	var n int
	if err := k.db.QueryRow("SELECT count(*) FROM collection_visibility WHERE collection_id=?", id).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n > 0
}
func TestCollectionVisibilityAtomicUndoQueriesAndBackup(t *testing.T) {
	k, e := testKernel(t)
	f := newDatabase(t, k, e)
	property := addImportTextProperty(t, k, e, f, "Retained")
	record, page := newRecord(t, k, e, f, "Task row")
	testOK(t, setValue(t, k, e, f, record, page, property, "text", richImport("preserved")))
	testOK(t, dbBatch(t, k, e, []any{map[string]any{"kind": "task.promote", "recordId": record, "statusPropertyId": f.status}}, "page", page, "record", record, "collection", f.collection, "property", f.status))
	if out := dbBatch(t, k, e, []any{map[string]any{"kind": "collection.trash.set", "collectionId": f.collection, "trashed": true}}, "page", f.page, "collection", f.collection); out["ok"] == true {
		t.Fatal("collection with live records hidden")
	}
	linked := createPage(t, k, e, "Linked view")
	view := NewID()
	testOK(t, dbBatch(t, k, e, []any{map[string]any{"kind": "view.save", "viewId": view, "collectionId": f.collection, "owningPageId": linked, "name": "Retained view", "type": "table", "schemaVersion": 1, "config": map[string]any{"columnWidths": []any{}}, "query": emptyQuery(f.title)}}, "view", view, "collection", f.collection, "page", linked))
	operation := NewID()
	actions := []any{map[string]any{"kind": "record.trash.set", "recordId": record, "trashed": true}, map[string]any{"kind": "collection.trash.set", "collectionId": f.collection, "trashed": true}}
	testOK(t, dbBatch(t, k, e, []any{map[string]any{"kind": "operation.apply", "operationId": operation, "label": "Reviewed collection removal", "actions": actions}}, "page", f.page, "page", page, "record", record, "collection", f.collection))
	if !collectionHidden(t, k, f.collection) || !recordTrashed(t, k, record) || pageDeleted(t, k, f.page) {
		t.Fatal("visibility changed owner or lost record tombstone")
	}
	assertSchemaValue(t, k, record, property, "text", richImport("preserved"))
	if out := queryDB(t, k, e, f.collection, "", 10, ""); out["ok"] == true {
		t.Fatal("hidden collection queried as available")
	}
	tx, err := k.db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	visible, err := databaseSnapshot(context.Background(), k, tx, e, "collections")
	if err != nil {
		t.Fatal(err)
	}
	if len(visible.([]any)) != 0 {
		t.Fatal("hidden collection in snapshot")
	}
	facets, err := taskSnapshot(context.Background(), k, tx, e)
	if err != nil {
		t.Fatal(err)
	}
	if string(marshal(facets)) != "[]" {
		t.Fatal("hidden task facet exposed", string(marshal(facets)))
	}
	views, err := databaseSnapshot(context.Background(), k, tx, e, "views")
	if err != nil {
		t.Fatal(err)
	}
	if len(views.([]any)) == 0 {
		t.Fatal("unavailable linked view reference silently disappeared")
	}
	tx.Rollback()
	if out := dbBatch(t, k, e, []any{map[string]any{"kind": "record.trash.set", "recordId": record, "trashed": false}}, "page", page, "page", f.page, "record", record, "collection", f.collection); out["ok"] == true {
		t.Fatal("record restored into hidden collection")
	}
	backup := filepath.Join(t.TempDir(), "backup")
	manifest, err := k.CreateBackup(context.Background(), e, backup)
	if err != nil {
		t.Fatal(err)
	}
	destination := filepath.Join(t.TempDir(), "restored")
	if _, err = RestoreBackupReviewed(context.Background(), backup, destination, manifest); err != nil {
		t.Fatal(err)
	}
	restored, err := Open(destination)
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()
	schemaUndo(t, restored, restored.OwnerContext(), operation)
	if collectionHidden(t, restored, f.collection) || recordTrashed(t, restored, record) {
		t.Fatal("visibility inverse did not restore graph")
	}
	assertSchemaValue(t, restored, record, property, "text", richImport("preserved"))
	testOK(t, queryDB(t, restored, restored.OwnerContext(), f.collection, "", 10, ""))
}
func TestCollectionVisibilityEmptyCollectionRecoveryMetadataAndABA(t *testing.T) {
	k, e := testKernel(t)
	f := newDatabase(t, k, e)
	operation := NewID()
	testOK(t, dbBatch(t, k, e, []any{map[string]any{"kind": "operation.apply", "operationId": operation, "label": "Empty collection removal", "actions": []any{map[string]any{"kind": "collection.trash.set", "collectionId": f.collection, "trashed": true}}}}, "page", f.page, "collection", f.collection))
	read, err := readPageTx(context.Background(), k.db, f.page)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := captureRecoverySnapshot(context.Background(), k.db, f.page, read)
	if err != nil {
		t.Fatal(err)
	}
	var snap recoverySnapshot
	if err = json.Unmarshal(raw, &snap); err != nil {
		t.Fatal(err)
	}
	if snap.MetadataVersion != 8 || snap.CollectionVisibility == nil || len(*snap.CollectionVisibility) != 1 || (*snap.CollectionVisibility)[0].DeletedAt == nil {
		t.Fatal("visibility absent from history")
	}
	testOK(t, dbBatch(t, k, e, []any{map[string]any{"kind": "collection.trash.set", "collectionId": f.collection, "trashed": false}}, "page", f.page, "collection", f.collection))
	testOK(t, dbBatch(t, k, e, []any{map[string]any{"kind": "collection.trash.set", "collectionId": f.collection, "trashed": true}}, "page", f.page, "collection", f.collection))
	if out := testDispatch(t, k, e, "operations.previewUndo", map[string]any{"contractVersion": 1, "requestId": NewID(), "workspaceId": k.ID(), "operationId": operation}); out["ok"] == true {
		t.Fatal("old inverse ignored later collection tombstone")
	}
}
func TestCollectionVisibilityRestoreRejectsMalformedMetadata(t *testing.T) {
	for _, scenario := range []string{"shape", "trigger", "stamp", "live_records", "missing_optional"} {
		t.Run(scenario, func(t *testing.T) {
			k, e := testKernel(t)
			f := newDatabase(t, k, e)
			record, _ := newRecord(t, k, e, f, "Retained")
			_ = record
			err := k.write(context.Background(), func(tx *sql.Tx) error {
				var err error
				switch scenario {
				case "shape":
					_, err = tx.Exec("DROP TABLE collection_visibility; CREATE TABLE collection_visibility(collection_id TEXT)")
				case "trigger":
					_, err = tx.Exec("CREATE TRIGGER visibility_trigger AFTER INSERT ON collection_visibility BEGIN DELETE FROM properties; END")
				case "stamp":
					_, err = tx.Exec("INSERT INTO collection_visibility VALUES(?,1,'bad')", f.collection)
				case "live_records":
					_, err = tx.Exec("INSERT INTO collection_visibility VALUES(?,1,?)", f.collection, now())
				case "missing_optional":
					_, err = tx.Exec("DROP TABLE collection_visibility")
				}
				if err != nil {
					return err
				}
				return validateCollectionVisibilityRestore(context.Background(), tx)
			})
			if scenario == "missing_optional" {
				if err != nil {
					t.Fatal(err)
				}
			} else if err == nil {
				t.Fatal("invalid visibility metadata accepted")
			}
		})
	}
}

func TestCollectionVisibilityHistoryRestoresEmptyCollection(t *testing.T) {
	k, e := testKernel(t)
	f := newDatabase(t, k, e)
	// Capture a visible state before hiding, then a hidden state before restoring.
	testOK(t, dbBatch(t, k, e, []any{map[string]any{"kind": "page.update", "pageId": f.page, "title": "Visible checkpoint"}}, "page", f.page))
	var visible string
	k.db.QueryRow("SELECT revision FROM pages WHERE id=?", f.page).Scan(&visible)
	testOK(t, dbBatch(t, k, e, []any{map[string]any{"kind": "collection.trash.set", "collectionId": f.collection, "trashed": true}}, "page", f.page, "collection", f.collection))
	var hidden string
	k.db.QueryRow("SELECT revision FROM pages WHERE id=?", f.page).Scan(&hidden)
	testOK(t, restoreTest(t, k, e, f.page, visible))
	if collectionHidden(t, k, f.collection) {
		t.Fatal("visible history failed to restore empty collection")
	}
	testOK(t, restoreTest(t, k, e, f.page, hidden))
	if !collectionHidden(t, k, f.collection) {
		t.Fatal("hidden history failed to retain collection removal")
	}
}
func TestCollectionVisibilityRejectsExternalDependencies(t *testing.T) {
	for _, kind := range []string{"relation", "formula_v1", "formula_v2"} {
		t.Run(kind, func(t *testing.T) {
			k, e := testKernel(t)
			f := newDatabase(t, k, e)
			other := newDatabase(t, k, e)
			property := addImportTextProperty(t, k, e, f, "Referenced")
			typ := "relation"
			config := map[string]any{"targetCollectionId": f.collection, "bidirectionalPropertyId": nil}
			if kind != "relation" {
				typ = "formula"
				raw := map[string]any{"originalExpression": "Original", "translatedExpression": "prop(\"" + property + "\")"}
				if kind == "formula_v2" {
					raw["evaluatorVersion"] = 2
				}
				config = derivedConfig("formula", raw)
			}
			savedConfig := config
			if typ == "formula" {
				own := addImportTextProperty(t, k, e, other, "Local formula input")
				config = derivedConfig("formula", map[string]any{"originalExpression": "Local", "translatedExpression": "prop(\"" + own + "\")"})
			}
			id := NewID()
			testOK(t, dbBatch(t, k, e, []any{map[string]any{"kind": "collection.addProperty", "collectionId": other.collection, "propertyId": id, "name": "Reference", "type": typ, "schemaVersion": 1, "config": config}}, "collection", other.collection, "property", id))
			if typ == "formula" {
				// Imported legacy configurations can retain an unavailable reference;
				// removal still fails closed even when modern authoring rejects it.
				if _, err := k.db.Exec("UPDATE properties SET config_json=? WHERE id=?", string(marshal(savedConfig)), id); err != nil {
					t.Fatal(err)
				}
			}
			if out := dbBatch(t, k, e, []any{map[string]any{"kind": "collection.trash.set", "collectionId": f.collection, "trashed": true}}, "page", f.page, "collection", f.collection); out["ok"] == true {
				t.Fatal("external dependency silently hidden")
			}
			if collectionHidden(t, k, f.collection) {
				t.Fatal("failed dependency review mutated collection")
			}
		})
	}
}
