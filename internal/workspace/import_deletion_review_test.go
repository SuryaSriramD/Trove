package workspace

import (
	"context"
	"strings"
	"testing"
)

func TestImportSourceBlockDeletionRequiresExplicitReviewedFields(t *testing.T) {
	k, e := testKernel(t)
	first := completeImportFixture(t, k, e, importFixture(t, k, e, map[string]string{"A.html": `<p id="one">Retained block</p><p id="two">Source deleted block</p>`}))
	item := first.Items[0]
	retained, removed := item.Blocks[0].ID, item.Blocks[1].ID
	updateImportFixtureBlock(t, k, retained, "Independent local correction")
	preview := importFixture(t, k, e, map[string]string{"A.html": `<p id="one">Retained block</p>`})
	fields := []any{}
	for _, field := range preview.Items[0].Merge.Fields {
		if strings.HasPrefix(field.Path, "/blocks/"+removed+"/") {
			if field.Outcome != "conflict" || field.Incoming.Present {
				t.Fatal("source deletion was not explicit review", field)
			}
			fields = append(fields, map[string]any{"path": field.Path, "choice": "take_incoming"})
		}
	}
	if len(fields) != 2 {
		t.Fatal("incomplete block deletion review", fields)
	}
	if _, err := commitPortableImport(context.Background(), k, e, map[string]any{"planId": preview.ID, "planHash": preview.Hash}); err == nil {
		t.Fatal("source deletion applied without a decision")
	}
	if unchanged, err := loadImportPlan(context.Background(), k.db, preview.ID); err != nil || unchanged.Initialized {
		t.Fatal("unreviewed deletion initialized resumable writes", err)
	}
	preview = configureMergeFixture(t, k, preview, fields)
	completeImportFixture(t, k, e, preview)
	var count int
	if err := k.db.QueryRow("SELECT count(*) FROM blocks WHERE id=?", removed).Scan(&count); err != nil || count != 0 {
		t.Fatal("reviewed source deletion was not applied", err)
	}
	var raw string
	if err := k.db.QueryRow("SELECT data_json FROM blocks WHERE id=?", retained).Scan(&raw); err != nil || !strings.Contains(raw, "Independent local correction") {
		t.Fatal("source deletion overwrote unrelated local content", err)
	}
	if _, err := k.readImportBytes(context.Background(), item.RawFileID); err != nil {
		t.Fatal("source deletion discarded prior evidence", err)
	}
}

func TestImportSchemaTypeChangeRequiresReviewAndInvalidValueChoiceWritesNothing(t *testing.T) {
	source, se := testKernel(t)
	f := newDatabase(t, source, se)
	property := addImportTextProperty(t, source, se, f, "Changing type")
	r, page := newRecord(t, source, se, f, "Record")
	testOK(t, setValue(t, source, se, f, r, page, property, "text", richImport("Original text")))
	target, te := testKernel(t)
	completeImportFixture(t, target, te, portableDatabasePreview(t, source, target))
	localProperty := importedEntity(t, target, "property", property)
	// Model a valid changed upstream typed schema/value. The import must not
	// reinterpret local text as a number merely because the source did so.
	if _, err := source.db.Exec("DELETE FROM property_values WHERE property_id=?", property); err != nil {
		t.Fatal(err)
	}
	if _, err := source.db.Exec(`UPDATE properties SET type='number',config_json='{"format":"decimal"}' WHERE id=?`, property); err != nil {
		t.Fatal(err)
	}
	testOK(t, setValue(t, source, se, f, r, page, property, "number", map[string]any{"decimal": "42", "format": "decimal"}))
	preview := portableDatabasePreview(t, source, target)
	path := "/schema/" + localProperty
	found := false
	for _, item := range preview.Items {
		if item.Merge == nil {
			continue
		}
		for _, field := range item.Merge.Fields {
			if field.Path == path {
				found = true
				if field.Outcome != "conflict" {
					t.Fatal("type change auto-approved", field)
				}
			}
		}
	}
	if !found {
		t.Fatal("schema type change absent from review")
	}
	localRecord := importedEntity(t, target, "record", r)
	preview = reviewDatabasePreview(t, target, preview, map[string]string{path: "take_incoming", "/values/" + localRecord + "/" + localProperty: "keep_local"})
	if _, err := commitPortableImport(context.Background(), target, te, map[string]any{"planId": preview.ID, "planHash": preview.Hash}); err == nil || !strings.Contains(err.Error(), "incompatible reviewed values") {
		t.Fatal("incompatible reviewed type conversion was not rejected explicitly", err)
	}
	if unchanged, err := loadImportPlan(context.Background(), target.db, preview.ID); err != nil || unchanged.Initialized {
		t.Fatal("incompatible conversion partially initialized", err)
	}
	definition, err := propertyDefinition(context.Background(), target.db, localProperty)
	if err != nil || definition.Type != "text" {
		t.Fatal("incompatible conversion changed schema", err)
	}
}
