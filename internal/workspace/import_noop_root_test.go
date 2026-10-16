package workspace

import (
	"archive/zip"
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func importCanonicalRows(t *testing.T, k *Kernel) map[string][]string {
	t.Helper()
	out := map[string][]string{}
	for name, query := range map[string]string{
		"pages":  "SELECT json_object('id',id,'parent',parent_page_id,'title',title,'revision',revision,'deleted',deleted_at) FROM pages ORDER BY id",
		"blocks": "SELECT json_object('id',id,'page',page_id,'parent',parent_block_id,'type',type,'data',data_json,'sort',sort_key,'revision',revision) FROM blocks ORDER BY id",
	} {
		rows, err := k.db.Query(query)
		if err != nil {
			t.Fatal(err)
		}
		for rows.Next() {
			var raw string
			if err = rows.Scan(&raw); err != nil {
				t.Fatal(err)
			}
			out[name] = append(out[name], raw)
		}
		if err = rows.Err(); err != nil {
			t.Fatal(err)
		}
		rows.Close()
	}
	return out
}
func assertImportRoot(t *testing.T, k *Kernel, p ImportPlan, exists bool) {
	t.Helper()
	var count int
	if err := k.db.QueryRow("SELECT count(*) FROM pages WHERE id=?", p.RootID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if (count == 1) != exists {
		t.Fatalf("import root present=%v, want %v: %s", count == 1, exists, p.RootID)
	}
	if !exists {
		var state map[string]any
		if err := json.Unmarshal(marshal(p), &state); err != nil {
			t.Fatal(err)
		}
		if state["rootSkipped"] != true {
			t.Fatal("root omission missing from durable report", state["rootSkipped"])
		}
	}
}
func TestImportNoopRootCancellationResumeAndFreshness(t *testing.T) {
	k, e := testKernel(t)
	source := map[string]string{"folder/nested/A.html": `<p id="one">Unchanged knowledge.</p>`}
	first := completeImportFixture(t, k, e, importFixture(t, k, e, source))
	assertImportRoot(t, k, first, true)
	item := identityItem(t, first, "folder/nested/A.html")
	testOK(t, dbBatch(t, k, e, []any{map[string]any{"kind": "page.review", "pageId": item.PageID, "contentIdentity": testFreshness(t, k, item.PageID).ContentIdentity}}, "page", item.PageID))
	reviewed := testFreshness(t, k, item.PageID)
	before := importCanonicalRows(t, k)
	preview := importFixture(t, k, e, source)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := commitPortableImport(ctx, k, e, map[string]any{"planId": preview.ID, "planHash": preview.Hash}); err == nil {
		t.Fatal("cancelled commit succeeded")
	}
	if !reflect.DeepEqual(before, importCanonicalRows(t, k)) {
		t.Fatal("cancelled import changed canonical content")
	}
	out, err := commitPortableImport(context.Background(), k, e, map[string]any{"planId": preview.ID, "planHash": preview.Hash})
	if err != nil {
		t.Fatal(err)
	}
	paused := out.(ImportPlan)
	assertImportRoot(t, k, paused, false)
	if !paused.Initialized || paused.Cursor != 0 || paused.State != "running" {
		t.Fatal("no-root initialization skipped durable item work", paused.Cursor, paused.State)
	}
	path := k.Path()
	if err := k.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	finished := completeImportFixture(t, reopened, reopened.OwnerContext(), paused)
	assertImportRoot(t, reopened, finished, false)
	if !reflect.DeepEqual(before, importCanonicalRows(t, reopened)) {
		t.Fatal("no-op import changed canonical pages/blocks, including folders")
	}
	if !reflect.DeepEqual(reviewed, testFreshness(t, reopened, item.PageID)) {
		t.Fatal("retained import evidence invalidated semantic review")
	}
	repeated := completeImportFixture(t, reopened, reopened.OwnerContext(), finished)
	if !reflect.DeepEqual(finished, repeated) || !reflect.DeepEqual(before, importCanonicalRows(t, reopened)) {
		t.Fatal("same-plan retry changed result")
	}
	var observations int
	if err := reopened.db.QueryRow("SELECT count(*) FROM import_observations WHERE mapping_id=? AND import_run_id=?", item.MappingID, finished.ID).Scan(&observations); err != nil {
		t.Fatal(err)
	}
	if observations != 1 {
		t.Fatal("no-op import lost per-item observation", observations)
	}
}

func TestImportNoopRetainsFolderEvidenceForFreshCaptureAfterReopen(t *testing.T) {
	k, e := testKernel(t)
	source := map[string]string{"folder/nested/A.html": `<p id="one">Retained source knowledge.</p>`}
	first := completeImportFixture(t, k, e, importFixture(t, k, e, source))
	original := identityItem(t, first, "folder/nested/A.html")
	localParent := createPage(t, k, e, "Local destination")
	testOK(t, dbBatch(t, k, e, []any{map[string]any{"kind": "page.move", "pageId": original.PageID, "parentPageId": localParent}}, "page", original.PageID, "page", original.ParentID, "page", localParent))
	before := importCanonicalRows(t, k)
	folders := map[string]ImportItem{}
	for _, item := range first.Items {
		if item.Kind == "folder" {
			if err := k.db.QueryRow("SELECT revision FROM pages WHERE id=?", item.PageID).Scan(&item.ExpectedRevision); err != nil {
				t.Fatal(err)
			}
			folders[item.Locator] = item
		}
	}
	if len(folders) != 2 {
		t.Fatal("fixture must contain two canonical directory pages", folders)
	}
	noop := completeImportFixture(t, k, e, importFixture(t, k, e, source))
	assertImportRoot(t, k, noop, false)
	path := k.Path()
	if err := k.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	retained, err := loadImportPlan(context.Background(), reopened.db, noop.ID)
	if err != nil {
		t.Fatal(err)
	}
	for locator, expected := range folders {
		item := identityItem(t, retained, locator)
		if item.PageID != expected.PageID || item.ExpectedRevision != expected.ExpectedRevision || item.Choice != "keep_local" {
			t.Fatalf("completed no-op lost directory identity: locator=%q expectedId=%q expectedRevision=%q item=%s", locator, expected.PageID, expected.ExpectedRevision, marshal(item))
		}
	}
	// This is a new retained capture, not a replay of the completed plan.
	fresh := importFixture(t, reopened, reopened.OwnerContext(), source)
	if fresh.ID == noop.ID {
		t.Fatal("fixture reused a completed plan")
	}
	item := identityItem(t, fresh, original.Locator)
	if item.PageID != original.PageID || item.ParentID != original.ParentID || item.Choice != "keep_local" {
		t.Fatalf("fresh capture lost identity or fabricated a parent change: %s", marshal(item))
	}
	if item.Merge == nil {
		t.Fatal("fresh capture omitted the three-way review")
	}
	parentFields := 0
	for _, field := range item.Merge.Fields {
		if field.Path == "/parent" {
			parentFields++
			if field.Outcome != "keep_local" || !equalImportPresence(field.Base, field.Incoming) {
				t.Fatalf("unchanged source parent conflicts with the local move: %s", marshal(field))
			}
		}
	}
	if parentFields != 1 {
		t.Fatal("fresh capture must contain exactly one parent-field decision", parentFields)
	}
	done := completeImportFixture(t, reopened, reopened.OwnerContext(), fresh)
	assertImportRoot(t, reopened, done, false)
	if !reflect.DeepEqual(before, importCanonicalRows(t, reopened)) {
		t.Fatal("fresh no-op changed canonical page/block identities, revisions or local parent")
	}
	for locator, expected := range folders {
		item := identityItem(t, done, locator)
		if item.PageID != expected.PageID || item.ExpectedRevision != expected.ExpectedRevision {
			t.Fatal("second no-op lost retained directory evidence", locator, item)
		}
		var attached int
		if err := reopened.db.QueryRow("SELECT count(*) FROM file_references WHERE page_id=?", expected.PageID).Scan(&attached); err != nil || attached != 0 {
			t.Fatal("retaining folder identity attached raw capture evidence to it", locator, attached, err)
		}
	}
}

func TestImportNoopStillSuppressesUnneededNewFolders(t *testing.T) {
	k, e := testKernel(t)
	first := completeImportFixture(t, k, e, importFixture(t, k, e, map[string]string{"Old.html": `<p id="one">Keep this local page.</p>`}))
	old := identityItem(t, first, "Old.html")
	before := importCanonicalRows(t, k)
	p := importFixture(t, k, e, map[string]string{"new/nested/New.html": `<p id="one">Keep this local page.</p>`})
	p = resolveIdentity(t, k, p, "new/nested/New.html", "link_existing", old.PageID)
	out, err := configurePortableImport(context.Background(), k, e, map[string]any{"planId": p.ID, "planHash": p.Hash, "choices": []any{map[string]any{"sourceLocator": "new/nested/New.html", "choice": "keep_local"}}})
	if err != nil {
		t.Fatal(err)
	}
	done := completeImportFixture(t, k, e, out.(ImportPlan))
	assertImportRoot(t, k, done, false)
	folders := 0
	for _, item := range done.Items {
		if item.Kind == "folder" {
			folders++
			if item.PageID != "" || item.ParentID != "" || item.ExpectedRevision != "" {
				t.Fatal("unused new directory became canonical evidence", item)
			}
		}
	}
	if folders != 2 || !reflect.DeepEqual(before, importCanonicalRows(t, k)) {
		t.Fatal("keeping a relocated source added a root, folder or canonical revision", folders)
	}
}

func TestImportNoopMovedIdentityRetainsEvidenceBackupAndPortableBoundary(t *testing.T) {
	k, e := testKernel(t)
	first := completeImportFixture(t, k, e, importFixture(t, k, e, map[string]string{"Old.html": `<p id="body">Current local knowledge.</p>`}))
	old := identityItem(t, first, "Old.html")
	testOK(t, dbBatch(t, k, e, []any{map[string]any{"kind": "page.review", "pageId": old.PageID, "contentIdentity": testFreshness(t, k, old.PageID).ContentIdentity}}, "page", old.PageID))
	reviewed, before := testFreshness(t, k, old.PageID), importCanonicalRows(t, k)
	incoming := `<p id="body">Retained incoming secret marker.</p>`
	p := importFixture(t, k, e, map[string]string{"Moved.html": incoming})
	p = resolveIdentity(t, k, p, "Moved.html", "link_existing", old.PageID)
	out, err := configurePortableImport(context.Background(), k, e, map[string]any{"planId": p.ID, "planHash": p.Hash, "choices": []any{map[string]any{"sourceLocator": "Moved.html", "choice": "keep_local"}}})
	if err != nil {
		t.Fatal(err)
	}
	p = out.(ImportPlan)
	sourceBytes, err := k.readImportBytes(context.Background(), p.SourceFileID)
	if err != nil {
		t.Fatal(err)
	}
	p = completeImportFixture(t, k, e, p)
	assertImportRoot(t, k, p, false)
	if !reflect.DeepEqual(before, importCanonicalRows(t, k)) || !reflect.DeepEqual(reviewed, testFreshness(t, k, old.PageID)) {
		t.Fatal("keep_local altered canonical content or review")
	}
	item := identityItem(t, p, "Moved.html")
	base, _, known, err := loadImportBaseline(context.Background(), k.db, item.MappingID)
	if err != nil || !known || base.SourceRevision != item.Hash {
		t.Fatal("new alias lost incoming baseline", known, err)
	}
	backup := filepath.Join(t.TempDir(), "backup")
	if _, err = k.CreateBackup(context.Background(), e, backup); err != nil {
		t.Fatal(err)
	}
	destination := filepath.Join(t.TempDir(), "restore")
	if _, err = RestoreBackup(context.Background(), backup, destination); err != nil {
		t.Fatal(err)
	}
	restored, err := Open(destination)
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()
	saved, err := loadImportPlan(context.Background(), restored.db, p.ID)
	if err != nil {
		t.Fatal(err)
	}
	assertImportRoot(t, restored, saved, false)
	archiveBytes, err := restored.readImportBytes(context.Background(), saved.SourceFileID)
	if err != nil || !reflect.DeepEqual(archiveBytes, sourceBytes) {
		t.Fatal("backup lost exact source archive", err)
	}
	raw, err := restored.readImportBytes(context.Background(), item.RawFileID)
	if err != nil || string(raw) != incoming {
		t.Fatal("backup lost retained incoming item", err)
	}
	retry := importFixture(t, restored, restored.OwnerContext(), map[string]string{"Moved.html": incoming})
	again := identityItem(t, retry, "Moved.html")
	if again.IdentityReview == nil || again.IdentityReview.Choice != "link_existing" || again.PageID != old.PageID || again.MappingID != old.MappingID || again.Choice != "keep_local" {
		t.Fatal("no-root commit lost reviewed alias", again)
	}
	portable := filepath.Join(t.TempDir(), "portable.zip")
	if _, err = restored.ExportPortable(context.Background(), restored.OwnerContext(), portable); err != nil {
		t.Fatal(err)
	}
	zr, err := zip.OpenReader(portable)
	if err != nil {
		t.Fatal(err)
	}
	defer zr.Close()
	for _, file := range zr.File {
		r, err := file.Open()
		if err != nil {
			t.Fatal(err)
		}
		b, err := io.ReadAll(r)
		r.Close()
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(b), "Retained incoming secret marker.") {
			t.Fatal("archival evidence became portable current content", file.Name)
		}
	}
	if !reflect.DeepEqual(before, importCanonicalRows(t, restored)) || !reflect.DeepEqual(reviewed, testFreshness(t, restored, old.PageID)) {
		t.Fatal("restore/export altered keep_local content or human review")
	}
}
func TestImportMixedKeepLocalAndChangedStillCreatesPlacedRoot(t *testing.T) {
	k, e := testKernel(t)
	first := completeImportFixture(t, k, e, importFixture(t, k, e, map[string]string{"A.html": `<p id="a">Keep A.</p>`, "B.html": `<p id="b">Before B.</p>`}))
	assertImportRoot(t, k, first, true)
	a, b := identityItem(t, first, "A.html"), identityItem(t, first, "B.html")
	destination := createPage(t, k, e, "Selected import destination")
	p := importFixture(t, k, e, map[string]string{"A.html": `<p id="a">Keep A.</p>`, "B.html": `<p id="b">Changed B.</p>`})
	out, err := configurePortableImport(context.Background(), k, e, map[string]any{"planId": p.ID, "planHash": p.Hash, "resolutions": []any{map[string]any{"sourceLocator": "B.html", "fields": []any{}}}})
	if err != nil {
		t.Fatal(err)
	}
	p = out.(ImportPlan)
	p.RootParentID = destination
	if err = k.db.QueryRow("SELECT revision FROM pages WHERE id=?", destination).Scan(&p.ExpectedParentRevision); err != nil {
		t.Fatal(err)
	}
	p.Hash = importPlanHash(p)
	if err = k.write(context.Background(), func(tx *sql.Tx) error { return saveImportPlan(context.Background(), tx, p) }); err != nil {
		t.Fatal(err)
	}
	p = completeImportFixture(t, k, e, p)
	assertImportRoot(t, k, p, true)
	var parent, body string
	if err = k.db.QueryRow("SELECT parent_page_id FROM pages WHERE id=?", p.RootID).Scan(&parent); err != nil || parent != destination {
		t.Fatal("new root lost explicit parent placement", parent, err)
	}
	if err = k.db.QueryRow("SELECT data_json FROM blocks WHERE id=?", b.Blocks[0].ID).Scan(&body); err != nil || !strings.Contains(body, "Changed B.") {
		t.Fatal("mixed import failed changed item", body, err)
	}
	if err = k.db.QueryRow("SELECT data_json FROM blocks WHERE id=?", a.Blocks[0].ID).Scan(&body); err != nil || !strings.Contains(body, "Keep A.") {
		t.Fatal("mixed import changed keep_local item", body, err)
	}
}

func assertImportFinalBaseline(t *testing.T, k *Kernel, p ImportPlan, item ImportItem) {
	t.Helper()
	var run, source string
	if err := k.db.QueryRow("SELECT import_run_id,source_revision FROM import_baselines WHERE mapping_id=?", item.MappingID).Scan(&run, &source); err != nil || run != p.ID || source != item.Hash {
		t.Fatal("finalization lost retained baseline", run, source, err)
	}
	var count int
	if err := k.db.QueryRow("SELECT count(*) FROM import_observations WHERE mapping_id=? AND import_run_id=?", item.MappingID, p.ID).Scan(&count); err != nil || count != 1 {
		t.Fatal("finalization lost observation", count, err)
	}
}
func TestImportNoopRootStillFinalizesKeptSourceDeletion(t *testing.T) {
	source, se := testKernel(t)
	kept := createPage(t, source, se, "Still present")
	removed := createPage(t, source, se, "Keep local after source removal")
	target, te := testKernel(t)
	first := completeImportFixture(t, target, te, portableDatabasePreview(t, source, target))
	localKept, localRemoved := sourcePageLocal(t, first, kept), sourcePageLocal(t, first, removed)
	testOK(t, dbBatch(t, source, se, []any{map[string]any{"kind": "page.trash", "pageId": removed, "includeDescendants": false}}, "page", removed))
	p := portableDatabasePreview(t, source, target)
	choices := []any{}
	for _, item := range p.Items {
		if item.Kind == "source_deleted_page" {
			choices = append(choices, map[string]any{"sourceLocator": item.Locator, "choice": "keep_local"})
		}
	}
	if len(choices) != 1 {
		t.Fatal("missing explicit source deletion", p.Items)
	}
	value, err := configurePortableImport(context.Background(), target, te, map[string]any{"planId": p.ID, "planHash": p.Hash, "choices": choices})
	if err != nil {
		t.Fatal(err)
	}
	before := importCanonicalRows(t, target)
	done := completeImportFixture(t, target, te, value.(ImportPlan))
	assertImportRoot(t, target, done, false)
	if pageDeleted(t, target, localKept) || pageDeleted(t, target, localRemoved) || !reflect.DeepEqual(before, importCanonicalRows(t, target)) {
		t.Fatal("kept deletion changed canonical state")
	}
	for _, item := range done.Items {
		if item.Kind == "source_deleted_page" {
			assertImportFinalBaseline(t, target, done, item)
		}
	}
}
func TestImportNoopRootStillFinalizesTypedDatabase(t *testing.T) {
	source, se := testKernel(t)
	f := newDatabase(t, source, se)
	prop := addImportTextProperty(t, source, se, f, "Retained typed field")
	record, page := newRecord(t, source, se, f, "Typed row")
	testOK(t, setValue(t, source, se, f, record, page, prop, "text", richImport("Original typed value")))
	target, te := testKernel(t)
	completeImportFixture(t, target, te, portableDatabasePreview(t, source, target))
	localRecord, localProp := importedEntity(t, target, "record", record), importedEntity(t, target, "property", prop)
	p := portableDatabasePreview(t, source, target)
	if !p.TypedDatabaseMerge {
		t.Fatal("missing typed database finalization path")
	}
	before := importCanonicalRows(t, target)
	done := completeImportFixture(t, target, te, p)
	assertImportRoot(t, target, done, false)
	if !reflect.DeepEqual(before, importCanonicalRows(t, target)) {
		t.Fatal("no-op database changed canonical pages/blocks")
	}
	assertSchemaValue(t, target, localRecord, localProp, "text", richImport("Original typed value"))
	found := false
	for _, item := range done.Items {
		if item.Kind == "portable_collection" {
			found = true
			assertImportFinalBaseline(t, target, done, item)
		}
	}
	if !found {
		t.Fatal("missing typed collection evidence")
	}
	for _, item := range done.Items {
		if item.Kind == "portable_record" {
			if item.Choice != "keep_local" {
				t.Fatal("kept page/collection projection changed", item)
			}
			var count int
			if err := target.db.QueryRow("SELECT count(*) FROM file_references WHERE page_id=? AND file_id=?", item.PageID, item.RawFileID).Scan(&count); err != nil || count != 1 {
				t.Fatal("unmapped row projection lost source evidence", count, err)
			}
		}
	}
}
func TestImportNoopRootRetainsIgnoredAssetEvidenceWithoutDanglingPages(t *testing.T) {
	k, e := testKernel(t)
	entries := map[string]string{"nested/A.html": "<p id=\"one\">Current knowledge</p>", "nested/assets/blob.png": "retained opaque asset bytes", "empty/": ""}
	completeImportFixture(t, k, e, importFixture(t, k, e, entries))
	before := importCanonicalRows(t, k)
	done := completeImportFixture(t, k, e, importFixture(t, k, e, entries))
	assertImportRoot(t, k, done, false)
	if !reflect.DeepEqual(before, importCanonicalRows(t, k)) {
		t.Fatal("ignored asset created pages")
	}
	assets, dirs := 0, 0
	for _, item := range done.Items {
		if item.Kind == "asset" {
			assets++
			b, err := k.readImportBytes(context.Background(), item.RawFileID)
			if err != nil || string(b) != entries[item.Locator] {
				t.Fatal("lost asset evidence", item, err)
			}
		}
		if item.Kind == "directory" {
			dirs++
		}
		if item.PageID != "" {
			var count int
			if err := k.db.QueryRow("SELECT count(*) FROM pages WHERE id=?", item.PageID).Scan(&count); err != nil || count != 1 {
				t.Fatal("dangling page reference", item, err)
			}
		}
	}
	if assets != 1 || dirs != 1 {
		t.Fatal("asset/directory missing from report", assets, dirs)
	}
	var dangling int
	if err := k.db.QueryRow("SELECT count(*) FROM file_references r LEFT JOIN pages p ON p.id=r.page_id WHERE p.id IS NULL").Scan(&dangling); err != nil || dangling != 0 {
		t.Fatal("dangling evidence", dangling, err)
	}
}

func TestImportRootStillPreservesFolderAssetAndUnsupportedOnlySources(t *testing.T) {
	for name, entries := range map[string]map[string]string{
		"folder_only":      {"empty/": ""},
		"asset_only":       {"image.png": "opaque retained image fixture"},
		"unsupported_only": {"retained.bin": "opaque unsupported fixture"},
	} {
		t.Run(name, func(t *testing.T) {
			k, e := testKernel(t)
			done := completeImportFixture(t, k, e, importFixture(t, k, e, entries))
			assertImportRoot(t, k, done, true)
			if done.RootSkipped {
				t.Fatal("new source without mapped kept content lost its root")
			}
			var count int
			if err := k.db.QueryRow("SELECT count(*) FROM file_references WHERE page_id=? AND file_id=?", done.RootID, done.SourceFileID).Scan(&count); err != nil || count != 1 {
				t.Fatal("source-only root lost evidence", count, err)
			}
		})
	}
}

func TestImportTypedProjectionDoesNotSuppressValueBodyOrNewRecord(t *testing.T) {
	for _, change := range []string{"value", "body", "new_record"} {
		t.Run(change, func(t *testing.T) {
			source, se := testKernel(t)
			f := newDatabase(t, source, se)
			prop := addImportTextProperty(t, source, se, f, "Independent value")
			record, page := newRecord(t, source, se, f, "Existing record")
			testOK(t, setValue(t, source, se, f, record, page, prop, "text", richImport("Base value")))
			block := NewID()
			testOK(t, dbBatch(t, source, se, []any{map[string]any{"kind": "block.insert", "block": map[string]any{"blockId": block, "pageId": page, "type": "paragraph", "schemaVersion": 1, "data": map[string]any{"richText": richImport("Base body")}}}}, "page", page, "block", block, "record", record, "collection", f.collection))
			target, te := testKernel(t)
			completeImportFixture(t, target, te, portableDatabasePreview(t, source, target))
			localRecord, localProp := importedEntity(t, target, "record", record), importedEntity(t, target, "property", prop)
			localPage := importedRecordPage(t, target, localRecord)
			newRecordID := ""
			switch change {
			case "value":
				testOK(t, setValue(t, source, se, f, record, page, prop, "text", richImport("Incoming value")))
			case "body":
				testOK(t, dbBatch(t, source, se, []any{map[string]any{"kind": "block.update", "blockId": block, "type": "paragraph", "schemaVersion": 1, "data": map[string]any{"richText": richImport("Incoming body")}}}, "page", page, "block", block, "record", record, "collection", f.collection))
			case "new_record":
				newRecordID, _ = newRecord(t, source, se, f, "New record")
			}
			preview := portableDatabasePreview(t, source, target)
			done := completeImportFixture(t, target, te, reviewDatabasePreview(t, target, preview, nil))
			assertImportRoot(t, target, done, true)
			if importedEntity(t, target, "record", record) != localRecord || importedRecordPage(t, target, localRecord) != localPage {
				t.Fatal("changed canonical record identity")
			}
			expected := "Base value"
			if change == "value" {
				expected = "Incoming value"
			}
			assertSchemaValue(t, target, localRecord, localProp, "text", richImport(expected))
			if change == "body" {
				var count int
				if err := target.db.QueryRow("SELECT count(*) FROM blocks WHERE page_id=? AND data_json LIKE '%Incoming body%'", localPage).Scan(&count); err != nil || count != 1 {
					t.Fatal("body change suppressed", count, err)
				}
			}
			if change == "new_record" {
				id := importedEntity(t, target, "record", newRecordID)
				if id == localRecord || importedRecordPage(t, target, id) == localPage {
					t.Fatal("new record collapsed")
				}
			}
		})
	}
}
