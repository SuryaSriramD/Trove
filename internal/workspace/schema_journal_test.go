package workspace

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"testing"

	"trove.local/phase0/contracts"
)

func schemaAction(collection, property string, definition any) map[string]any {
	return map[string]any{"kind": "collection.property.restore", "collectionId": collection, "propertyId": property, "definition": definition}
}
func schemaDefinition(name, kind string, config any) map[string]any {
	return map[string]any{"name": name, "type": kind, "schemaVersion": 1, "config": config}
}
func schemaUndo(t *testing.T, k *Kernel, e contracts.ServiceExecutionContext, operation string) {
	t.Helper()
	preview := testOK(t, testDispatch(t, k, e, "operations.previewUndo", map[string]any{"contractVersion": 1, "requestId": NewID(), "workspaceId": k.ID(), "operationId": operation}))
	if err := contracts.Validate("service", "OperationPreviewUndoResult", marshal(preview)); err != nil {
		t.Fatal(err)
	}
	var guards []contracts.ServiceExpectedRevision
	if err := json.Unmarshal(marshal(preview["expectedRevisions"]), &guards); err != nil {
		t.Fatal(err)
	}
	testOK(t, testDispatch(t, k, e, "batch", testRequest(k, e, []any{preview["command"]}, guards...)))
}
func TestUnusedSchemaDeletionUndoPreservesUnrelatedChangesAndIdentity(t *testing.T) {
	k, e := testKernel(t)
	f := newDatabase(t, k, e)
	property := addImportTextProperty(t, k, e, f, "Unused notes")
	operation := NewID()
	testOK(t, dbBatch(t, k, e, []any{map[string]any{"kind": "operation.apply", "operationId": operation, "label": "Remove unused property", "actions": []any{schemaAction(f.collection, property, nil)}}}, "page", f.page, "collection", f.collection, "property", property))
	var count int
	k.db.QueryRow("SELECT count(*) FROM properties WHERE id=?", property).Scan(&count)
	if count != 0 {
		t.Fatal("property not removed")
	}
	testOK(t, dbBatch(t, k, e, []any{map[string]any{"kind": "page.update", "pageId": f.page, "title": "Keep later page title"}}, "page", f.page))
	other := addImportTextProperty(t, k, e, f, "Later property")
	var tombstone string
	if err := k.db.QueryRow("SELECT revision FROM entity_tombstones WHERE entity_type='property' AND entity_id=?", property).Scan(&tombstone); err != nil {
		t.Fatal(err)
	}
	schemaUndo(t, k, e, operation)
	restored, err := propertyDefinition(context.Background(), k.db, property)
	if err != nil || restored.Type != "text" || string(restored.Revision) <= tombstone {
		t.Fatal(restored, err)
	}
	if _, err = propertyDefinition(context.Background(), k.db, other); err != nil {
		t.Fatal("unrelated property erased", err)
	}
	if readPage(t, k, e, f.page)["page"].(map[string]any)["title"] != "Keep later page title" {
		t.Fatal("unrelated title rewound")
	}
}
func TestUnusedSchemaConversionSurvivesBackupAndRestoresOptionDefinitions(t *testing.T) {
	k, e := testKernel(t)
	f := newDatabase(t, k, e)
	operation := NewID()
	// Status is unused and unbound in this fixture; all option IDs must return.
	before, err := propertyDefinition(context.Background(), k.db, f.status)
	if err != nil {
		t.Fatal(err)
	}
	action := schemaAction(f.collection, f.status, schemaDefinition("Changed to text", "text", map[string]any{}))
	testOK(t, dbBatch(t, k, e, []any{map[string]any{"kind": "operation.apply", "operationId": operation, "label": "Convert unused schema", "actions": []any{action}}}, "page", f.page, "collection", f.collection, "property", f.status))
	backup := filepath.Join(t.TempDir(), "backup")
	manifest, err := k.CreateBackup(context.Background(), e, backup)
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
	schemaUndo(t, restored, restored.OwnerContext(), operation)
	after, err := propertyDefinition(context.Background(), restored.db, f.status)
	if err != nil || string(importPropertyField(before)) != string(importPropertyField(after)) {
		t.Fatal(before, after, err)
	}
	var count int
	restored.db.QueryRow("SELECT count(*) FROM property_options WHERE property_id=?", f.status).Scan(&count)
	if count != 2 {
		t.Fatal("option identity recovery lost", count)
	}
}
func TestSchemaUndoRejectsSubsequentValueAndDefinitionChanges(t *testing.T) {
	for _, which := range []string{"value", "definition"} {
		t.Run(which, func(t *testing.T) {
			k, e := testKernel(t)
			f := newDatabase(t, k, e)
			property := addImportTextProperty(t, k, e, f, "Unused")
			operation := NewID()
			action := schemaAction(f.collection, property, schemaDefinition("Number", "number", map[string]any{"format": "decimal"}))
			testOK(t, dbBatch(t, k, e, []any{map[string]any{"kind": "operation.apply", "operationId": operation, "label": "Convert", "actions": []any{action}}}, "page", f.page, "collection", f.collection, "property", property))
			if which == "value" {
				record, page := newRecord(t, k, e, f, "Later row")
				testOK(t, setValue(t, k, e, f, record, page, property, "number", map[string]any{"decimal": "123", "format": "decimal"}))
			} else {
				testOK(t, dbBatch(t, k, e, []any{schemaAction(f.collection, property, schemaDefinition("Later name", "number", map[string]any{"format": "decimal"}))}, "page", f.page, "collection", f.collection, "property", property))
			}
			testCode(t, testDispatch(t, k, e, "operations.previewUndo", map[string]any{"contractVersion": 1, "requestId": NewID(), "workspaceId": k.ID(), "operationId": operation}), "CONFLICT")
		})
	}
}
func TestSchemaDeletionDependenciesAndAtomicBatchFailure(t *testing.T) {
	k, e := testKernel(t)
	f := newDatabase(t, k, e)
	property := addImportTextProperty(t, k, e, f, "Unused")
	record, page := newRecord(t, k, e, f, "Populated")
	testOK(t, setValue(t, k, e, f, record, page, f.date, "date", map[string]any{"start": "2026-10-21", "end": nil, "timezone": "UTC", "hasTime": false}))
	actions := []any{schemaAction(f.collection, property, nil), schemaAction(f.collection, f.date, nil)}
	testCode(t, dbBatch(t, k, e, []any{map[string]any{"kind": "operation.apply", "operationId": NewID(), "label": "Must roll back", "actions": actions}}, "page", f.page, "collection", f.collection, "property", property, "property", f.date), "CONFLICT")
	if _, err := propertyDefinition(context.Background(), k.db, property); err != nil {
		t.Fatal("earlier schema deletion committed", err)
	}
	// A stored view's exact property binding prevents a silent broken interface.
	view := NewID()
	testOK(t, dbBatch(t, k, e, []any{map[string]any{"kind": "view.save", "viewId": view, "collectionId": f.collection, "owningPageId": f.page, "name": "Uses notes", "type": "table", "schemaVersion": 1, "config": map[string]any{"columnWidths": []any{}}, "query": emptyQuery(f.title, property)}}, "view", view, "collection", f.collection, "page", f.page))
	testCode(t, dbBatch(t, k, e, []any{schemaAction(f.collection, property, nil)}, "page", f.page, "collection", f.collection, "property", property), "CONFLICT")
}

func TestImportUnusedSchemaDeletionAndConversionHaveGuardedUndo(t *testing.T) {
	for _, which := range []string{"delete", "convert"} {
		t.Run(which, func(t *testing.T) {
			source, se := testKernel(t)
			f := newDatabase(t, source, se)
			property := addImportTextProperty(t, source, se, f, "Unused source property")
			target, te := testKernel(t)
			completeImportFixture(t, target, te, portableDatabasePreview(t, source, target))
			local := importedEntity(t, target, "property", property)
			if which == "delete" {
				testOK(t, dbBatch(t, source, se, []any{schemaAction(f.collection, property, nil)}, "page", f.page, "collection", f.collection, "property", property))
			} else {
				testOK(t, dbBatch(t, source, se, []any{schemaAction(f.collection, property, schemaDefinition("Changed source type", "number", map[string]any{"format": "decimal"}))}, "page", f.page, "collection", f.collection, "property", property))
			}
			preview := portableDatabasePreview(t, source, target)
			path := "/schema/" + local
			found := false
			for _, item := range preview.Items {
				if item.Merge != nil {
					for _, field := range item.Merge.Fields {
						if field.Path == path {
							found = true
							if field.Outcome != "conflict" {
								t.Fatal("destructive schema decision was automatic", field)
							}
						}
					}
				}
			}
			if !found {
				t.Fatal("missing schema review")
			}
			preview = reviewDatabasePreview(t, target, preview, map[string]string{path: "take_incoming"})
			completeImportFixture(t, target, te, preview)
			var count int
			target.db.QueryRow("SELECT count(*) FROM operation_journal WHERE id=? AND state='applied'", preview.ID).Scan(&count)
			if count != 1 {
				t.Fatal("missing schema inverse journal")
			}
			if which == "delete" {
				target.db.QueryRow("SELECT count(*) FROM properties WHERE id=?", local).Scan(&count)
				if count != 0 {
					t.Fatal("reviewed property deletion not applied")
				}
			} else {
				p, err := propertyDefinition(context.Background(), target.db, local)
				if err != nil || p.Type != "number" {
					t.Fatal(p, err)
				}
			}
			schemaUndo(t, target, te, preview.ID)
			p, err := propertyDefinition(context.Background(), target.db, local)
			if err != nil || p.Type != "text" {
				t.Fatal("schema inverse failed", p, err)
			}
		})
	}
}
func TestSchemaDeleteRetryAndLaterValueConflict(t *testing.T) {
	k, e := testKernel(t)
	f := newDatabase(t, k, e)
	property := addImportTextProperty(t, k, e, f, "Unused")
	req := testRequest(k, e, []any{schemaAction(f.collection, property, nil)}, dbGuards(t, k, "page", f.page, "collection", f.collection, "property", property)...)
	testOK(t, testDispatch(t, k, e, "batch", req))
	testOK(t, testDispatch(t, k, e, "batch", req))
}

func TestSchemaGroupRejectsOwnPopulatedValueWithoutMigration(t *testing.T) {
	k, e := testKernel(t)
	f := newDatabase(t, k, e)
	property := addImportTextProperty(t, k, e, f, "Unused")
	record, page := newRecord(t, k, e, f, "Row")
	actions := []any{schemaAction(f.collection, property, schemaDefinition("Number", "number", map[string]any{"format": "decimal"})), map[string]any{"kind": "record.setValue", "recordId": record, "propertyId": property, "value": map[string]any{"type": "number", "schemaVersion": 1, "value": map[string]any{"decimal": "7", "format": "decimal"}}}}
	testCode(t, dbBatch(t, k, e, []any{map[string]any{"kind": "operation.apply", "operationId": NewID(), "label": "Needs value migration", "actions": actions}}, "page", f.page, "page", page, "record", record, "collection", f.collection, "property", property), "CONFLICT")
	definition, err := propertyDefinition(context.Background(), k.db, property)
	if err != nil || definition.Type != "text" {
		t.Fatal("rejected group changed schema", definition, err)
	}
}

func TestSchemaMutationRejectsFormulaExpressionDependencies(t *testing.T) {
	for _, version := range []int{1, 2} {
		t.Run(fmt.Sprint(version), func(t *testing.T) {
			k, e := testKernel(t)
			f := newDatabase(t, k, e)
			property := addImportTextProperty(t, k, e, f, "Unused but referenced")
			formula := NewID()
			cfg := derivedConfig("formula", map[string]any{"translatedExpression": fmt.Sprintf("prop(%q)", property), "evaluatorVersion": version})
			testOK(t, dbBatch(t, k, e, []any{map[string]any{"kind": "collection.addProperty", "collectionId": f.collection, "propertyId": formula, "name": "Dependent", "type": "formula", "schemaVersion": 1, "config": cfg}}, "page", f.page, "collection", f.collection, "property", formula))
			for _, definition := range []any{nil, schemaDefinition("Changed", "number", map[string]any{"format": "decimal"})} {
				testCode(t, dbBatch(t, k, e, []any{schemaAction(f.collection, property, definition)}, "page", f.page, "collection", f.collection, "property", property), "CONFLICT")
			}
		})
	}
}
