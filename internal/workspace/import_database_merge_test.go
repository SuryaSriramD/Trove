package workspace

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"trove.local/phase0/contracts"
)

func portableDatabasePreview(t *testing.T, source, target *Kernel) ImportPlan {
	t.Helper()
	path := filepath.Join(t.TempDir(), "source.zip")
	if _, err := source.ExportPortable(context.Background(), source.OwnerContext(), path); err != nil {
		t.Fatal(err)
	}
	handle, err := target.RegisterSelection(path)
	if err != nil {
		t.Fatal(err)
	}
	preview, err := previewPortableImport(context.Background(), target, target.OwnerContext(), map[string]any{"sourceHandle": handle, "sourceNamespace": "typed-merge"})
	if err != nil {
		t.Fatal(err)
	}
	return preview.(ImportPlan)
}

func reviewDatabasePreview(t *testing.T, k *Kernel, p ImportPlan, choices map[string]string) ImportPlan {
	t.Helper()
	resolutions := []any{}
	for _, item := range p.Items {
		if item.Merge == nil || item.Choice == "keep_local" {
			continue
		}
		fields := []any{}
		for _, field := range item.Merge.Fields {
			if choice := choices[field.Path]; choice != "" {
				fields = append(fields, map[string]any{"path": field.Path, "choice": choice})
			} else if field.Outcome == "conflict" {
				t.Fatalf("unexpected conflict %s: %s", item.Locator, marshal(field))
			}
		}
		resolutions = append(resolutions, map[string]any{"sourceLocator": item.Locator, "fields": fields})
	}
	out, err := configurePortableImport(context.Background(), k, k.OwnerContext(), map[string]any{"planId": p.ID, "planHash": p.Hash, "resolutions": resolutions})
	if err != nil {
		t.Fatal(err)
	}
	return out.(ImportPlan)
}

func importedEntity(t *testing.T, k *Kernel, kind, source string) string {
	t.Helper()
	var id string
	if err := k.db.QueryRow("SELECT local_id FROM import_entity_ids WHERE source_namespace='typed-merge' AND entity_kind=? AND source_identity=?", kind, source).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

func addImportTextProperty(t *testing.T, k *Kernel, e contracts.ServiceExecutionContext, f databaseFixture, name string) string {
	t.Helper()
	id := NewID()
	testOK(t, dbBatch(t, k, e, []any{map[string]any{"kind": "collection.addProperty", "collectionId": f.collection, "propertyId": id, "name": name, "type": "text", "schemaVersion": 1, "config": map[string]any{}}}, "collection", f.collection, "property", id))
	return id
}

func TestImportDatabaseMergeIndependentCellsStableIdentitiesAndRestart(t *testing.T) {
	source, se := testKernel(t)
	f := newDatabase(t, source, se)
	a, ap := newRecord(t, source, se, f, "A")
	b, bp := newRecord(t, source, se, f, "B")
	text := addImportTextProperty(t, source, se, f, "Notes")
	for _, record := range [][2]string{{a, ap}, {b, bp}} {
		testOK(t, setValue(t, source, se, f, record[0], record[1], text, "text", richImport("base")))
	}
	target, te := testKernel(t)
	first := completeImportFixture(t, target, te, portableDatabasePreview(t, source, target))
	c := first.PortableCollections[0]
	localProperty := importedEntity(t, target, "property", text)
	localRecord := importedEntity(t, target, "record", a)
	var localPage string
	target.db.QueryRow("SELECT page_id FROM records WHERE id=?", localRecord).Scan(&localPage)
	localFixture := databaseFixture{collection: c.CollectionID}
	testOK(t, setValue(t, target, te, localFixture, localRecord, localPage, localProperty, "text", richImport("local A")))
	testOK(t, setValue(t, source, se, f, b, bp, text, "text", richImport("incoming B")))
	preview := portableDatabasePreview(t, source, target)
	if preview.PortableCollections[0].CollectionID != c.CollectionID {
		t.Fatal("collection identity changed")
	}
	preview = reviewDatabasePreview(t, target, preview, nil)
	// Every merge decision survives closing and reopening before application.
	path := target.Path()
	target.Close()
	target, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer target.Close()
	preview, err = loadImportPlan(context.Background(), target.db, preview.ID)
	if err != nil {
		t.Fatal(err)
	}
	completeImportFixture(t, target, target.OwnerContext(), preview)
	for _, want := range [][2]string{{a, "local A"}, {b, "incoming B"}} {
		id := importedEntity(t, target, "record", want[0])
		var raw string
		if err = target.db.QueryRow("SELECT value_json FROM property_values WHERE record_id=? AND property_id=?", id, localProperty).Scan(&raw); err != nil || !strings.Contains(raw, want[1]) {
			t.Fatal("independent typed edit lost", raw, err)
		}
	}
	var count int
	target.db.QueryRow("SELECT count(*) FROM records").Scan(&count)
	if count != 2 {
		t.Fatal("reimport duplicated records", count)
	}
	unchanged := portableDatabasePreview(t, source, target)
	for _, item := range unchanged.Items {
		if item.Merge != nil && item.Choice != "keep_local" {
			t.Fatalf("accepted incoming baseline not retained: %s %s", item.Locator, marshal(item.Merge))
		}
	}
}

func TestImportDatabaseMergeConflictAndStaleReviewWriteNothing(t *testing.T) {
	source, se := testKernel(t)
	f := newDatabase(t, source, se)
	r, page := newRecord(t, source, se, f, "A")
	prop := addImportTextProperty(t, source, se, f, "Notes")
	testOK(t, setValue(t, source, se, f, r, page, prop, "text", richImport("base")))
	target, te := testKernel(t)
	completeImportFixture(t, target, te, portableDatabasePreview(t, source, target))
	lr, lp, lc := importedEntity(t, target, "record", r), importedEntity(t, target, "property", prop), importedEntity(t, target, "collection", f.collection)
	var rp string
	target.db.QueryRow("SELECT page_id FROM records WHERE id=?", lr).Scan(&rp)
	lf := databaseFixture{collection: lc}
	testOK(t, setValue(t, target, te, lf, lr, rp, lp, "text", richImport("local")))
	testOK(t, setValue(t, source, se, f, r, page, prop, "text", richImport("remote")))
	preview := portableDatabasePreview(t, source, target)
	path := "/values/" + lr + "/" + lp
	conflicts := 0
	for _, item := range preview.Items {
		if item.Merge != nil {
			for _, field := range item.Merge.Fields {
				if field.Path == path && field.Outcome == "conflict" {
					conflicts++
				}
			}
		}
	}
	if conflicts != 1 {
		t.Fatal("typed value did not get one explicit conflict", conflicts)
	}
	if _, err := commitPortableImport(context.Background(), target, te, map[string]any{"planId": preview.ID, "planHash": preview.Hash}); err == nil {
		t.Fatal("unresolved typed conflict committed")
	}
	preview = reviewDatabasePreview(t, target, preview, map[string]string{path: "take_incoming"})
	testOK(t, setValue(t, target, te, lf, lr, rp, lp, "text", richImport("later local")))
	if _, err := commitPortableImport(context.Background(), target, te, map[string]any{"planId": preview.ID, "planHash": preview.Hash}); err == nil {
		t.Fatal("stale reviewed value committed")
	}
	var raw string
	target.db.QueryRow("SELECT value_json FROM property_values WHERE record_id=? AND property_id=?", lr, lp).Scan(&raw)
	if !strings.Contains(raw, "later local") {
		t.Fatal("stale plan changed value", raw)
	}
}

func TestImportDatabaseMergeExplicitNullAndMissingRemainDistinct(t *testing.T) {
	source, se := testKernel(t)
	f := newDatabase(t, source, se)
	r, page := newRecord(t, source, se, f, "A")
	prop := NewID()
	testOK(t, dbBatch(t, source, se, []any{map[string]any{"kind": "collection.addProperty", "collectionId": f.collection, "propertyId": prop, "name": "Optional", "type": "select", "schemaVersion": 1, "config": map[string]any{"options": []any{}}}}, "collection", f.collection, "property", prop))
	testOK(t, setValue(t, source, se, f, r, page, prop, "select", nil))
	target, te := testKernel(t)
	completeImportFixture(t, target, te, portableDatabasePreview(t, source, target))
	lr, lp := importedEntity(t, target, "record", r), importedEntity(t, target, "property", prop)
	testOK(t, dbBatch(t, source, se, []any{map[string]any{"kind": "record.clearValue", "recordId": r, "propertyId": prop}}, "record", r, "page", page, "collection", f.collection, "property", prop))
	preview := portableDatabasePreview(t, source, target)
	matched := false
	for _, item := range preview.Items {
		if item.Merge != nil {
			for _, field := range item.Merge.Fields {
				if field.Path == "/values/"+lr+"/"+lp {
					matched = field.Base.Present && string(field.Base.Value) == "null" && !field.Incoming.Present && field.Outcome == "apply_incoming"
				}
			}
		}
	}
	if !matched {
		t.Fatal("explicit null collapsed into missing")
	}
	completeImportFixture(t, target, te, reviewDatabasePreview(t, target, preview, nil))
	var count int
	target.db.QueryRow("SELECT count(*) FROM property_values WHERE record_id=? AND property_id=?", lr, lp).Scan(&count)
	if count != 0 {
		t.Fatal("reviewed missing field remained an explicit null")
	}
	var baseline string
	target.db.QueryRow("SELECT snapshot_json FROM import_baselines b JOIN import_mappings m ON m.id=b.mapping_id WHERE m.local_id=(SELECT owning_page_id FROM collections LIMIT 1)").Scan(&baseline)
	var snapshot ImportSnapshot
	if err := json.Unmarshal([]byte(baseline), &snapshot); err != nil {
		t.Fatal(err)
	}
}

func TestImportDatabaseMergeAddsSourcePropertyAndPreservesLocalSchema(t *testing.T) {
	source, se := testKernel(t)
	f := newDatabase(t, source, se)
	r, page := newRecord(t, source, se, f, "A")
	target, te := testKernel(t)
	completeImportFixture(t, target, te, portableDatabasePreview(t, source, target))
	lc, lr := importedEntity(t, target, "collection", f.collection), importedEntity(t, target, "record", r)
	var lp string
	target.db.QueryRow("SELECT page_id FROM records WHERE id=?", lr).Scan(&lp)
	localProp := addImportTextProperty(t, target, te, databaseFixture{collection: lc}, "Local notes")
	testOK(t, setValue(t, target, te, databaseFixture{collection: lc}, lr, lp, localProp, "text", richImport("keep my notes")))
	newProp := addImportTextProperty(t, source, se, f, "Source notes")
	testOK(t, setValue(t, source, se, f, r, page, newProp, "text", richImport("source notes")))
	preview := reviewDatabasePreview(t, target, portableDatabasePreview(t, source, target), nil)
	completeImportFixture(t, target, te, preview)
	for _, value := range [][2]string{{localProp, "keep my notes"}, {importedEntity(t, target, "property", newProp), "source notes"}} {
		var raw string
		if err := target.db.QueryRow("SELECT value_json FROM property_values WHERE record_id=? AND property_id=?", lr, value[0]).Scan(&raw); err != nil || !strings.Contains(raw, value[1]) {
			t.Fatal("schema addition lost an independent field", raw, err)
		}
	}
	for _, item := range portableDatabasePreview(t, source, target).Items {
		if item.Merge != nil && item.Choice != "keep_local" {
			t.Fatalf("new property identity was not retained: %s", marshal(item.Merge))
		}
	}
}

func TestImportDatabaseSeparateCopyKeepsOriginalGraphAndPages(t *testing.T) {
	source, se := testKernel(t)
	f := newDatabase(t, source, se)
	r, page := newRecord(t, source, se, f, "A")
	prop := addImportTextProperty(t, source, se, f, "Notes")
	testOK(t, setValue(t, source, se, f, r, page, prop, "text", richImport("original")))
	target, te := testKernel(t)
	first := completeImportFixture(t, target, te, portableDatabasePreview(t, source, target))
	c := first.PortableCollections[0]
	testOK(t, setValue(t, source, se, f, r, page, prop, "text", richImport("copy only")))
	preview := portableDatabasePreview(t, source, target)
	choices := []any{}
	for _, item := range preview.Items {
		if notionGraphPage(item) {
			choices = append(choices, map[string]any{"sourceLocator": item.Locator, "choice": "duplicate"})
		}
	}
	configured, err := configurePortableImport(context.Background(), target, te, map[string]any{"planId": preview.ID, "planHash": preview.Hash, "choices": choices})
	if err != nil {
		t.Fatal(err)
	}
	copy := completeImportFixture(t, target, te, configured.(ImportPlan))
	if copy.PortableCollections[0].CollectionID == c.CollectionID || copy.PortableCollections[0].PageID == c.PageID {
		t.Fatal("copy reused collection identity or owning page")
	}
	var raw string
	lr, lp := importedEntity(t, target, "record", r), importedEntity(t, target, "property", prop)
	target.db.QueryRow("SELECT value_json FROM property_values WHERE record_id=? AND property_id=?", lr, lp).Scan(&raw)
	if !strings.Contains(raw, "original") {
		t.Fatal("copy mutated the original value", raw)
	}
	var records, collections int
	target.db.QueryRow("SELECT count(*) FROM records").Scan(&records)
	target.db.QueryRow("SELECT count(*) FROM collections").Scan(&collections)
	if records != 2 || collections != 2 {
		t.Fatal("copy did not produce a distinct complete graph", records, collections)
	}
}

func TestImportDatabaseMergeResumeRejectsInterveningCollectionEdit(t *testing.T) {
	source, se := testKernel(t)
	f := newDatabase(t, source, se)
	r, page := newRecord(t, source, se, f, "A")
	prop := addImportTextProperty(t, source, se, f, "Notes")
	testOK(t, setValue(t, source, se, f, r, page, prop, "text", richImport("original")))
	target, te := testKernel(t)
	completeImportFixture(t, target, te, portableDatabasePreview(t, source, target))
	testOK(t, setValue(t, source, se, f, r, page, prop, "text", richImport("remote")))
	preview := reviewDatabasePreview(t, target, portableDatabasePreview(t, source, target), nil)
	started, err := commitPortableImport(context.Background(), target, te, map[string]any{"planId": preview.ID, "planHash": preview.Hash})
	if err != nil || !started.(ImportPlan).Initialized {
		t.Fatal("could not initialize import", err)
	}
	lc := importedEntity(t, target, "collection", f.collection)
	addImportTextProperty(t, target, te, databaseFixture{collection: lc}, "An intervening property")
	if _, err = commitPortableImport(context.Background(), target, te, map[string]any{"planId": preview.ID, "planHash": preview.Hash}); err == nil {
		t.Fatal("resumed import ignored a changed schema")
	}
	stored, err := loadImportPlan(context.Background(), target.db, preview.ID)
	if err != nil || stored.Cursor != 0 {
		t.Fatal("stale resume advanced its durable cursor", stored.Cursor, err)
	}
}

func TestImportDatabaseBackupRetainsSourceBaselineAndPendingReview(t *testing.T) {
	source, se := testKernel(t)
	f := newDatabase(t, source, se)
	r, page := newRecord(t, source, se, f, "A")
	prop := addImportTextProperty(t, source, se, f, "Notes")
	testOK(t, setValue(t, source, se, f, r, page, prop, "text", richImport("base")))
	target, te := testKernel(t)
	completeImportFixture(t, target, te, portableDatabasePreview(t, source, target))
	testOK(t, setValue(t, source, se, f, r, page, prop, "text", richImport("incoming")))
	preview := reviewDatabasePreview(t, target, portableDatabasePreview(t, source, target), nil)
	var mapping, base string
	for _, item := range preview.Items {
		if item.Kind == "portable_collection" {
			mapping = item.MappingID
		}
	}
	if err := target.db.QueryRow("SELECT snapshot_json FROM import_baselines WHERE mapping_id=?", mapping).Scan(&base); err != nil {
		t.Fatal(err)
	}
	backup := filepath.Join(t.TempDir(), "backup")
	if _, err := target.CreateBackup(context.Background(), te, backup); err != nil {
		t.Fatal(err)
	}
	destination := filepath.Join(t.TempDir(), "restored")
	if _, err := RestoreBackup(context.Background(), backup, destination); err != nil {
		t.Fatal(err)
	}
	restored, err := Open(destination)
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()
	var retained string
	if err = restored.db.QueryRow("SELECT snapshot_json FROM import_baselines WHERE mapping_id=?", mapping).Scan(&retained); err != nil || retained != base {
		t.Fatal("backup changed the accepted baseline", err)
	}
	pending, err := loadImportPlan(context.Background(), restored.db, preview.ID)
	if err != nil || string(marshal(pending)) != string(marshal(preview)) {
		t.Fatal("backup lost pending reviewed fields", err)
	}
	completeImportFixture(t, restored, restored.OwnerContext(), pending)
	lr, lp := importedEntity(t, restored, "record", r), importedEntity(t, restored, "property", prop)
	var value string
	if err = restored.db.QueryRow("SELECT value_json FROM property_values WHERE record_id=? AND property_id=?", lr, lp).Scan(&value); err != nil || !strings.Contains(value, "incoming") {
		t.Fatal("restored reviewed import did not preserve its canonical target", value, err)
	}
}
