package workspace

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"trove.local/phase0/contracts"
)

func schemaValuesAction(collection, property string, definition any, values ...map[string]any) map[string]any {
	action := schemaAction(collection, property, definition)
	list := []any{}
	for _, value := range values {
		list = append(list, value)
	}
	action["values"] = list
	action["valueInventoryVersion"] = 1
	return action
}
func schemaValue(record string, value any) map[string]any {
	return map[string]any{"recordId": record, "value": value}
}
func assertSchemaValue(t *testing.T, k *Kernel, record, property, kind string, value any) {
	t.Helper()
	var gotKind, got string
	if err := k.db.QueryRow("SELECT property_type,value_json FROM property_values WHERE record_id=? AND property_id=?", record, property).Scan(&gotKind, &got); err != nil {
		t.Fatal(err)
	}
	if gotKind != kind || got != string(marshal(value)) {
		t.Fatalf("value mismatch %s %s, want %s %s", gotKind, got, kind, marshal(value))
	}
}
func TestPopulatedSchemaScalarMigrationAndUndoPreservesUnrelatedEdits(t *testing.T) {
	for _, tc := range []struct {
		kind  string
		value any
	}{
		{"text", richImport("new")}, {"number", map[string]any{"decimal": "42.25", "format": "decimal"}}, {"checkbox", true},
		{"date", map[string]any{"start": "2026-10-21", "end": nil, "timezone": "Asia/Kolkata", "hasTime": false}},
		{"url", "https://example.com/path"}, {"email", "person@example.com"},
	} {
		t.Run(tc.kind, func(t *testing.T) {
			k, e := testKernel(t)
			f := newDatabase(t, k, e)
			property := addImportTextProperty(t, k, e, f, "Text")
			other := addImportTextProperty(t, k, e, f, "Other")
			record, page := newRecord(t, k, e, f, "Record")
			empty, _ := newRecord(t, k, e, f, "Absent value")
			testOK(t, setValue(t, k, e, f, record, page, property, "text", richImport("original")))
			config := map[string]any{}
			if tc.kind == "number" {
				config["format"] = "decimal"
			}
			action := schemaValuesAction(f.collection, property, schemaDefinition("Migrated", tc.kind, config), schemaValue(record, tc.value))
			op := NewID()
			testOK(t, dbBatch(t, k, e, []any{map[string]any{"kind": "operation.apply", "operationId": op, "label": "Reviewed values", "actions": []any{action}}}, "page", f.page, "collection", f.collection, "property", property, "record", record, "page", page))
			assertSchemaValue(t, k, record, property, tc.kind, tc.value)
			var count int
			k.db.QueryRow("SELECT count(*) FROM property_values WHERE record_id=? AND property_id=?", empty, property).Scan(&count)
			if count != 0 {
				t.Fatal("missing value fabricated")
			}
			testOK(t, setValue(t, k, e, f, record, page, other, "text", richImport("later unrelated value")))
			testOK(t, dbBatch(t, k, e, []any{map[string]any{"kind": "page.update", "pageId": page, "title": "Later title"}}, "page", page, "record", record, "collection", f.collection))
			schemaUndo(t, k, e, op)
			assertSchemaValue(t, k, record, property, "text", richImport("original"))
			assertSchemaValue(t, k, record, other, "text", richImport("later unrelated value"))
			if readPage(t, k, e, page)["page"].(map[string]any)["title"] != "Later title" {
				t.Fatal("undo rewound unrelated title")
			}
		})
	}
}
func TestPopulatedSchemaDeletionBackupUndoAndPostconditionGuard(t *testing.T) {
	k, e := testKernel(t)
	f := newDatabase(t, k, e)
	property := addImportTextProperty(t, k, e, f, "Text")
	record, page := newRecord(t, k, e, f, "Record")
	testOK(t, setValue(t, k, e, f, record, page, property, "text", richImport("recover me")))
	action := schemaValuesAction(f.collection, property, nil)
	op := NewID()
	testOK(t, dbBatch(t, k, e, []any{map[string]any{"kind": "operation.apply", "operationId": op, "label": "Remove with values", "actions": []any{action}}}, "page", f.page, "collection", f.collection, "property", property, "page", page, "record", record))
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
	schemaUndo(t, restored, restored.OwnerContext(), op)
	assertSchemaValue(t, restored, record, property, "text", richImport("recover me"))
	// A later populated value blocks undo of a conversion, including added rows.
	action = schemaValuesAction(f.collection, property, schemaDefinition("N", "number", map[string]any{"format": "decimal"}), schemaValue(record, map[string]any{"decimal": "5", "format": "decimal"}))
	op = NewID()
	testOK(t, dbBatch(t, restored, restored.OwnerContext(), []any{map[string]any{"kind": "operation.apply", "operationId": op, "label": "Convert", "actions": []any{action}}}, "page", f.page, "collection", f.collection, "property", property, "page", page, "record", record))
	testOK(t, setValue(t, restored, restored.OwnerContext(), f, record, page, property, "number", map[string]any{"decimal": "6", "format": "decimal"}))
	testCode(t, testDispatch(t, restored, restored.OwnerContext(), "operations.previewUndo", map[string]any{"contractVersion": 1, "requestId": NewID(), "workspaceId": restored.ID(), "operationId": op}), "CONFLICT")
}
func TestPopulatedSchemaInvalidValuesBlockWholeOperationWithPerRowErrors(t *testing.T) {
	k, e := testKernel(t)
	f := newDatabase(t, k, e)
	property := addImportTextProperty(t, k, e, f, "Text")
	a, ap := newRecord(t, k, e, f, "A")
	b, bp := newRecord(t, k, e, f, "B")
	for _, pair := range [][2]string{{a, ap}, {b, bp}} {
		testOK(t, setValue(t, k, e, f, pair[0], pair[1], property, "text", richImport("old")))
	}
	action := schemaValuesAction(f.collection, property, schemaDefinition("URL", "url", map[string]any{}), schemaValue(a, "javascript:bad"), schemaValue(b, "relative/path"))
	err := validateReviewedPropertyRestore(context.Background(), k.db, action)
	if err == nil || !strings.Contains(err.Error(), a) || !strings.Contains(err.Error(), b) {
		t.Fatal("missing per-row validation", err)
	}
	op := NewID()
	testCode(t, dbBatch(t, k, e, []any{map[string]any{"kind": "operation.apply", "operationId": op, "label": "Invalid", "actions": []any{map[string]any{"kind": "page.update", "pageId": f.page, "title": "Must rollback"}, action}}}, "page", f.page, "collection", f.collection, "property", property, "page", ap, "record", a, "page", bp, "record", b), "CONFLICT")
	assertSchemaValue(t, k, a, property, "text", richImport("old"))
	assertSchemaValue(t, k, b, property, "text", richImport("old"))
	if readPage(t, k, e, f.page)["page"].(map[string]any)["title"] == "Must rollback" {
		t.Fatal("partial operation committed")
	}
}
func TestPopulatedSchemaRequiresEveryAffectedGuardAndRejectsUnsupportedTypes(t *testing.T) {
	k, e := testKernel(t)
	f := newDatabase(t, k, e)
	property := addImportTextProperty(t, k, e, f, "Text")
	record, page := newRecord(t, k, e, f, "Record")
	testOK(t, setValue(t, k, e, f, record, page, property, "text", richImport("old")))
	action := schemaValuesAction(f.collection, property, nil)
	testCode(t, dbBatch(t, k, e, []any{action}, "page", f.page, "collection", f.collection, "property", property), "INVALID_REQUEST")
	assertSchemaValue(t, k, record, property, "text", richImport("old"))
	testOK(t, setValue(t, k, e, f, record, page, f.status, "status", f.open))
	testCode(t, dbBatch(t, k, e, []any{schemaValuesAction(f.collection, f.status, nil)}, "page", f.page, "collection", f.collection, "property", f.status, "page", page, "record", record), "CONFLICT")
}
func TestSchemaInverseClearsIntroducedValuesAndPreservesHistoricalEmptyInventory(t *testing.T) {
	k, e := testKernel(t)
	f := newDatabase(t, k, e)
	property := addImportTextProperty(t, k, e, f, "Empty")
	record, page := newRecord(t, k, e, f, "Record")
	op := NewID()
	action := schemaValuesAction(f.collection, property, schemaDefinition("Number", "number", map[string]any{"format": "decimal"}), schemaValue(record, map[string]any{"decimal": "1", "format": "decimal"}))
	testOK(t, dbBatch(t, k, e, []any{map[string]any{"kind": "operation.apply", "operationId": op, "label": "Populate", "actions": []any{action}}}, "page", f.page, "collection", f.collection, "property", property, "page", page, "record", record))
	schemaUndo(t, k, e, op)
	var count int
	k.db.QueryRow("SELECT count(*) FROM property_values WHERE property_id=?", property).Scan(&count)
	if count != 0 {
		t.Fatal("inverse kept introduced value")
	}
}
func TestImportPopulatedSchemaDeletionAndConversionReviewedTogether(t *testing.T) {
	for _, which := range []string{"delete", "convert"} {
		t.Run(which, func(t *testing.T) {
			source, se := testKernel(t)
			f := newDatabase(t, source, se)
			property := addImportTextProperty(t, source, se, f, "Text")
			record, page := newRecord(t, source, se, f, "Row")
			testOK(t, setValue(t, source, se, f, record, page, property, "text", richImport("base")))
			target, te := testKernel(t)
			completeImportFixture(t, target, te, portableDatabasePreview(t, source, target))
			localProperty := importedEntity(t, target, "property", property)
			localRecord := importedEntity(t, target, "record", record)
			var localPage string
			if err := target.db.QueryRow("SELECT page_id FROM records WHERE id=?", localRecord).Scan(&localPage); err != nil {
				t.Fatal(err)
			}
			localCollection := importedEntity(t, target, "collection", f.collection)
			// A local edit must appear as an explicit value conflict when source deletes it.
			testOK(t, setValue(t, target, te, databaseFixture{collection: localCollection}, localRecord, localPage, localProperty, "text", richImport("local edit")))
			var definition any
			values := []map[string]any{}
			if which == "convert" {
				definition = schemaDefinition("Number", "number", map[string]any{"format": "decimal"})
				values = append(values, schemaValue(record, map[string]any{"decimal": "7", "format": "decimal"}))
			}
			testOK(t, dbBatch(t, source, se, []any{schemaValuesAction(f.collection, property, definition, values...)}, "page", f.page, "collection", f.collection, "property", property, "page", page, "record", record))
			// New rows in the incoming graph must exist before the atomic conversion.
			if which == "convert" {
				r, p := newRecord(t, source, se, f, "New row")
				testOK(t, setValue(t, source, se, f, r, p, property, "number", map[string]any{"decimal": "8", "format": "decimal"}))
			}
			preview := portableDatabasePreview(t, source, target)
			schemaPath := "/schema/" + localProperty
			valuePath := "/values/" + localRecord + "/" + localProperty
			choices := map[string]string{schemaPath: "take_incoming", valuePath: "take_incoming"}
			preview = reviewDatabasePreview(t, target, preview, choices)
			completeImportFixture(t, target, te, preview)
			if which == "convert" {
				assertSchemaValue(t, target, localRecord, localProperty, "number", map[string]any{"decimal": "7", "format": "decimal"})
			}
			schemaUndo(t, target, te, preview.ID)
			assertSchemaValue(t, target, localRecord, localProperty, "text", richImport("local edit"))
		})
	}
}
func TestImportSchemaConflictingTypedValueFailsBeforeAnyPageWrites(t *testing.T) {
	source, se := testKernel(t)
	f := newDatabase(t, source, se)
	property := addImportTextProperty(t, source, se, f, "Text")
	record, page := newRecord(t, source, se, f, "Row")
	testOK(t, setValue(t, source, se, f, record, page, property, "text", richImport("base")))
	target, te := testKernel(t)
	completeImportFixture(t, target, te, portableDatabasePreview(t, source, target))
	localProperty := importedEntity(t, target, "property", property)
	localRecord := importedEntity(t, target, "record", record)
	testOK(t, dbBatch(t, source, se, []any{schemaValuesAction(f.collection, property, schemaDefinition("Number", "number", map[string]any{"format": "decimal"}), schemaValue(record, map[string]any{"decimal": "7", "format": "decimal"}))}, "page", f.page, "collection", f.collection, "property", property, "page", page, "record", record))
	preview := portableDatabasePreview(t, source, target)
	resolutions := []any{}
	for _, item := range preview.Items {
		if item.Merge != nil {
			fields := []any{}
			for _, field := range item.Merge.Fields {
				choice := ""
				if field.Path == "/schema/"+localProperty {
					choice = "take_incoming"
				}
				if field.Path == "/values/"+localRecord+"/"+localProperty {
					choice = "keep_local"
				}
				if choice != "" {
					fields = append(fields, map[string]any{"path": field.Path, "choice": choice})
				}
			}
			resolutions = append(resolutions, map[string]any{"sourceLocator": item.Locator, "fields": fields})
		}
	}
	configured, err := configurePortableImport(context.Background(), target, te, map[string]any{"planId": preview.ID, "planHash": preview.Hash, "resolutions": resolutions})
	if err != nil {
		t.Fatal(err)
	}
	preview = configured.(ImportPlan)
	_, err = commitPortableImport(context.Background(), target, te, map[string]any{"planId": preview.ID, "planHash": preview.Hash})
	if err == nil || !strings.Contains(err.Error(), localRecord) {
		t.Fatal("invalid kept value must block reviewed type change", err)
	}
	var definition contracts.DomainPropertyDefinition
	definition, err = propertyDefinition(context.Background(), target.db, localProperty)
	if err != nil || definition.Type != "text" {
		t.Fatal(definition, err)
	}
	assertSchemaValue(t, target, localRecord, localProperty, "text", richImport("base"))
}
func TestSchemaNullIsNotAnAbsentScalarValue(t *testing.T) {
	k, _ := testKernel(t)
	f := newDatabase(t, k, k.OwnerContext())
	property := addImportTextProperty(t, k, k.OwnerContext(), f, "Text")
	record, _ := newRecord(t, k, k.OwnerContext(), f, "Row")
	for _, value := range []any{nil, json.RawMessage(`null`)} {
		action := schemaValuesAction(f.collection, property, schemaDefinition("Text", "text", map[string]any{}), schemaValue(record, value))
		if validateReviewedPropertyRestore(context.Background(), k.db, action) == nil {
			t.Fatal("null silently accepted as missing text")
		}
	}
}

func TestPopulatedSchemaUndoRequiresEveryRecordScopeAndRejectsTrashedRows(t *testing.T) {
	k, e := testKernel(t)
	f := newDatabase(t, k, e)
	property := addImportTextProperty(t, k, e, f, "Text")
	record, page := newRecord(t, k, e, f, "Restricted row")
	testOK(t, setValue(t, k, e, f, record, page, property, "text", richImport("private source value")))
	action := schemaValuesAction(f.collection, property, nil)
	op := NewID()
	testOK(t, dbBatch(t, k, e, []any{map[string]any{"kind": "operation.apply", "operationId": op, "label": "Deleted", "actions": []any{action}}}, "page", f.page, "collection", f.collection, "property", property, "page", page, "record", record))
	scoped := k.Grant(e.Actor, "user", contracts.DomainScope{Value: contracts.DomainScopeVariant3{Kind: "pages", WorkspaceId: contracts.DomainId(k.ID()), PageIds: []contracts.DomainId{contracts.DomainId(f.page)}}})
	request := map[string]any{"contractVersion": 1, "requestId": NewID(), "workspaceId": k.ID(), "operationId": op}
	result := testDispatch(t, k, scoped, "operations.previewUndo", request)
	testCode(t, result, "SCOPE_DENIED")
	if strings.Contains(string(marshal(result)), "private source value") {
		t.Fatal("denied preview leaked values")
	}
	testOK(t, dbBatch(t, k, e, []any{map[string]any{"kind": "page.trash", "pageId": page, "includeDescendants": false}}, "page", page, "record", record, "collection", f.collection))
	testCode(t, testDispatch(t, k, e, "operations.previewUndo", request), "CONFLICT")
}
