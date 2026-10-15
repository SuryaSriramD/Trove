package workspace

import (
	"context"
	"strings"
	"testing"
)

func keyedCSVFixture(t *testing.T, k *Kernel, contents string) ImportPlan {
	t.Helper()
	p := importFixture(t, k, k.OwnerContext(), map[string]string{"Tasks.csv": contents})
	for _, item := range p.Items {
		if item.Kind == "csv" {
			out, err := configurePortableImport(context.Background(), k, k.OwnerContext(), map[string]any{"planId": p.ID, "planHash": p.Hash, "csvKeys": []any{map[string]any{"sourceLocator": "Tasks.csv", "index": float64(1)}}})
			if err != nil {
				t.Fatal(err)
			}
			return out.(ImportPlan)
		}
	}
	return p
}

func TestImportCSVKeySurvivesReorderRenameAndIndependentLocalValue(t *testing.T) {
	k, e := testKernel(t)
	first := completeImportFixture(t, k, e, keyedCSVFixture(t, k, "Name,ID,Notes\nSame title,a,base A\nSame title,b,base B\n"))
	var ar, ap, br, bp, notes, collection string
	for _, item := range first.Items {
		if item.Kind == "portable_collection" {
			collection = item.CollectionID
			notes = item.Columns[2].PropertyID
		}
		if item.Kind == "portable_record" && item.Cells[1] == "a" {
			ar, ap = item.RecordID, item.PageID
		}
		if item.Kind == "portable_record" && item.Cells[1] == "b" {
			br, bp = item.RecordID, item.PageID
		}
	}
	if ar == "" || br == "" || ar == br || ap == bp {
		t.Fatal("equal titles collapsed distinct source keys")
	}
	testOK(t, setValue(t, k, e, databaseFixture{collection: collection}, ar, ap, notes, "text", richImport("local A")))
	preview := keyedCSVFixture(t, k, "Name,ID,Notes\nRenamed B,b,incoming B\nRenamed A,a,base A\n")
	for _, item := range preview.Items {
		if item.Kind == "portable_record" && item.Cells[1] == "a" && (item.RecordID != ar || item.PageID != ap) {
			t.Fatal("row reorder changed canonical identity")
		}
		if item.Kind == "portable_record" && item.Cells[1] == "b" && (item.RecordID != br || item.PageID != bp) {
			t.Fatal("title rename changed canonical identity")
		}
	}
	completeImportFixture(t, k, e, reviewDatabasePreview(t, k, preview, nil))
	for _, want := range [][3]string{{ar, "Renamed A", "local A"}, {br, "Renamed B", "incoming B"}} {
		var title, value string
		if err := k.db.QueryRow("SELECT p.title,v.value_json FROM records r JOIN pages p ON p.id=r.page_id JOIN property_values v ON v.record_id=r.id AND v.property_id=? WHERE r.id=?", notes, want[0]).Scan(&title, &value); err != nil || title != want[1] || !strings.Contains(value, want[2]) {
			t.Fatal("keyed merge lost an independent field", title, value, err)
		}
	}
	var count int
	k.db.QueryRow("SELECT count(*) FROM records").Scan(&count)
	if count != 2 {
		t.Fatal("CSV reimport duplicated records", count)
	}
	for _, item := range keyedCSVFixture(t, k, "Name,ID,Notes\nRenamed B,b,incoming B\nRenamed A,a,base A\n").Items {
		if item.Merge != nil && item.Choice != "keep_local" {
			t.Fatalf("keyed baseline did not converge: %s", marshal(item.Merge))
		}
	}
}

func TestImportCSVKeyRejectsAmbiguityAndHistoricalUnkeyedIdentity(t *testing.T) {
	for name, contents := range map[string]string{"duplicate": "Name,ID\nOne,x\nTwo,x\n", "empty": "Name,ID\nOne,\n", "empty_header": "Name,\nOne,x\n", "duplicate_headers": "Name,ID,ID\nOne,x,y\n", "missing_cell": "Name,ID,Notes\nOne,x\n"} {
		t.Run(name, func(t *testing.T) {
			k, e := testKernel(t)
			p := importFixture(t, k, e, map[string]string{"Tasks.csv": contents})
			if _, err := configurePortableImport(context.Background(), k, e, map[string]any{"planId": p.ID, "planHash": p.Hash, "csvKeys": []any{map[string]any{"sourceLocator": "Tasks.csv", "index": float64(1)}}}); err == nil {
				t.Fatal("ambiguous key accepted")
			}
			stored, err := loadImportPlan(context.Background(), k.db, p.ID)
			if err != nil || stored.Hash != p.Hash {
				t.Fatal("invalid key changed durable preview", err)
			}
		})
	}
	k, e := testKernel(t)
	completeImportFixture(t, k, e, importFixture(t, k, e, map[string]string{"Tasks.csv": "Name,ID\nOld,x\n"}))
	p := importFixture(t, k, e, map[string]string{"Tasks.csv": "Name,ID\nNew,x\n"})
	if _, err := configurePortableImport(context.Background(), k, e, map[string]any{"planId": p.ID, "planHash": p.Hash, "csvKeys": []any{map[string]any{"sourceLocator": "Tasks.csv", "index": float64(1)}}}); err == nil || !strings.Contains(err.Error(), "historical CSV") {
		t.Fatal("legacy rows silently gained an identity from current position", err)
	}
}

func TestImportCSVDoesNotUsePageTitleAsRecordBodyIdentity(t *testing.T) {
	k, e := testKernel(t)
	p := importFixture(t, k, e, map[string]string{"Tasks.csv": "Name,ID\nSame,x\n", "Tasks/Same.html": "<p>Independent page with an equal title</p>"})
	var page, row string
	for _, item := range p.Items {
		if item.Kind == "html" {
			page = item.PageID
		}
		if item.Kind == "row" {
			row = item.PageID
		}
	}
	if page == "" || row == "" || page == row {
		t.Fatal("title equality established source identity")
	}
}

func TestImportCSVBindingSurvivesRestartAndColumnOrderWithoutDeletingAbsentRows(t *testing.T) {
	k, e := testKernel(t)
	first := completeImportFixture(t, k, e, keyedCSVFixture(t, k, "Name,ID,Notes\nA,a,before\nB,b,keep absent B\n"))
	var a, b, notes string
	for _, item := range first.Items {
		if item.Kind == "portable_collection" {
			notes = item.Columns[2].PropertyID
		}
		if item.Kind == "portable_record" && item.Cells[1] == "a" {
			a = item.RecordID
		}
		if item.Kind == "portable_record" && item.Cells[1] == "b" {
			b = item.RecordID
		}
	}
	path := k.Path()
	k.Close()
	k, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer k.Close()
	// The title header can be renamed, and non-title headers can move. Exact
	// retained header bindings identify fields; absent source rows are not deletes.
	p := keyedCSVFixture(t, k, "Task,Notes,ID\nRenamed A,after,a\nC,new row,c\n")
	completeImportFixture(t, k, k.OwnerContext(), reviewDatabasePreview(t, k, p, nil))
	var value string
	if err = k.db.QueryRow("SELECT value_json FROM property_values WHERE record_id=? AND property_id=?", a, notes).Scan(&value); err != nil || !strings.Contains(value, "after") {
		t.Fatal("column reorder lost the field binding", value, err)
	}
	if err = k.db.QueryRow("SELECT value_json FROM property_values WHERE record_id=? AND property_id=?", b, notes).Scan(&value); err != nil || !strings.Contains(value, "keep absent B") {
		t.Fatal("absent row became a deletion", value, err)
	}
	var count int
	k.db.QueryRow("SELECT count(*) FROM records").Scan(&count)
	if count != 3 {
		t.Fatal("new row did not preserve existing identities", count)
	}
}

func TestImportCSVSourceColumnNamedTitleHasIndependentIdentity(t *testing.T) {
	k, e := testKernel(t)
	p := completeImportFixture(t, k, e, keyedCSVFixture(t, k, "Name,ID,title\nA,a,ordinary value\n"))
	if len(p.PortableCollections) != 1 {
		t.Fatal("keyed CSV collection missing")
	}
	var count int
	k.db.QueryRow("SELECT count(*) FROM properties WHERE collection_id=?", p.PortableCollections[0].CollectionID).Scan(&count)
	if count != 3 {
		t.Fatal("source column name collided with canonical title identity", count)
	}
}
