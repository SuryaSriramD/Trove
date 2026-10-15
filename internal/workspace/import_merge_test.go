package workspace

import (
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestImportMergePartialAPIKeepsIdentityEvidenceForLaterFullCapture(t *testing.T) {
	k, e := testKernel(t)
	capture := func(text string) ImportPlan {
		name := filepath.Join(t.TempDir(), "A.html")
		if err := os.WriteFile(name, []byte(text), 0600); err != nil {
			t.Fatal(err)
		}
		handle, err := k.RegisterSelection(name)
		if err != nil {
			t.Fatal(err)
		}
		result, err := previewPortableImport(context.Background(), k, e, map[string]any{"sourceHandle": handle, "sourceNamespace": "partial-identity", "sourceSystem": "notion_api"})
		if err != nil {
			t.Fatal(err)
		}
		return result.(ImportPlan)
	}
	first := completeImportFixture(t, k, e, capture(`<p id="one">one</p><p id="two">two</p>`))
	secondID := first.Items[0].Blocks[1].ID
	partial := capture(`<p id="one">one changed</p>`)
	if partial.Items[0].Choice != "keep_local" {
		partial = configureMergeFixture(t, k, partial, nil)
	}
	completeImportFixture(t, k, e, partial)
	base, _, known, err := loadImportBaseline(context.Background(), k.db, first.Items[0].MappingID)
	if err != nil || !known || base.BlockSources[secondID] != "html-id:two" {
		t.Fatal("omitted block lost its source identity", err)
	}
	full := capture(`<p id="one">one changed</p><p id="two">two changed</p>`)
	full = configureMergeFixture(t, k, full, nil)
	completeImportFixture(t, k, e, full)
	var raw string
	if err = k.db.QueryRow("SELECT data_json FROM blocks WHERE id=?", secondID).Scan(&raw); err != nil || !strings.Contains(raw, "two changed") {
		t.Fatal("full capture recreated the omitted source block", err)
	}
	var count int
	k.db.QueryRow("SELECT count(*) FROM blocks WHERE page_id=?", first.Items[0].PageID).Scan(&count)
	if count != 2 {
		t.Fatal("partial/full capture duplicated blocks", count)
	}
}

func TestImportMergeRetainsSyntheticFolderAndLocalPageMove(t *testing.T) {
	k, e := testKernel(t)
	first := completeImportFixture(t, k, e, importFixture(t, k, e, map[string]string{"folder/nested/A.html": `<p id="one">one</p>`}))
	var original ImportItem
	for _, item := range first.Items {
		if item.Kind == "html" {
			original = item
		}
	}
	localParent := createPage(t, k, e, "Local destination")
	var rev, parentRev, localRev string
	k.db.QueryRow("SELECT revision FROM pages WHERE id=?", original.PageID).Scan(&rev)
	k.db.QueryRow("SELECT revision FROM pages WHERE id=?", original.ParentID).Scan(&parentRev)
	k.db.QueryRow("SELECT revision FROM pages WHERE id=?", localParent).Scan(&localRev)
	testOK(t, testDispatch(t, k, e, "batch", testRequest(k, e, []any{map[string]any{"kind": "page.move", "pageId": original.PageID, "parentPageId": localParent}}, required("page", original.PageID, rev), required("page", original.ParentID, parentRev), required("page", localParent, localRev))))
	for iteration := 0; iteration < 2; iteration++ {
		preview := importFixture(t, k, e, map[string]string{"folder/nested/A.html": `<p id="one">one</p>`})
		for _, item := range preview.Items {
			if item.Kind == "html" && (item.Choice != "keep_local" || item.ParentID != original.ParentID) {
				t.Fatal("unchanged source fabricated a directory move", item.Choice, item.ParentID)
			}
		}
		completeImportFixture(t, k, e, preview)
	}
	var parent string
	k.db.QueryRow("SELECT parent_page_id FROM pages WHERE id=?", original.PageID).Scan(&parent)
	if parent != localParent {
		t.Fatal("unchanged import undid the local page move")
	}
	var count int
	k.db.QueryRow("SELECT count(*) FROM pages WHERE title IN ('folder','nested')").Scan(&count)
	if count != 2 {
		t.Fatal("reimport duplicated synthetic folders", count)
	}
}

func TestImportMergePreservesLocalDisplayPreferences(t *testing.T) {
	k, e := testKernel(t)
	first := completeImportFixture(t, k, e, importFixture(t, k, e, map[string]string{"A.html": `<p id="one">one</p>`}))
	page := first.Items[0].PageID
	var rev string
	k.db.QueryRow("SELECT revision FROM pages WHERE id=?", page).Scan(&rev)
	testOK(t, testDispatch(t, k, e, "batch", testRequest(k, e, []any{map[string]any{"kind": "page.display.set", "pageId": page, "fullWidth": true, "presentation": true}}, required("page", page, rev))))
	preview := importFixture(t, k, e, map[string]string{"A.html": `<p id="one">incoming</p>`})
	preview.Items[0].DisplayPreferences = &PageDisplayPreferences{FullWidth: false, Presentation: false}
	preview.Hash = importPlanHash(preview)
	if err := k.write(context.Background(), func(tx *sql.Tx) error { return saveImportPlan(context.Background(), tx, preview) }); err != nil {
		t.Fatal(err)
	}
	preview = configureMergeFixture(t, k, preview, nil)
	completeImportFixture(t, k, e, preview)
	var wide, presentation bool
	if err := k.db.QueryRow("SELECT full_width,presentation FROM p2_page_display WHERE page_id=?", page).Scan(&wide, &presentation); err != nil || !wide || !presentation {
		t.Fatal("content merge rewrote unreviewed display settings", err)
	}
}

func TestImportMergeRejectsConflictingSiblingOrderInsteadOfUUIDTieBreak(t *testing.T) {
	a, b, c := NewID(), NewID(), NewID()
	makeFields := func(order []string) map[string]json.RawMessage {
		item := ImportItem{Title: "Order", BlockParents: map[string]string{}}
		for _, id := range order {
			item.Blocks = append(item.Blocks, ImportBlock{ID: id, Type: "paragraph", Data: map[string]any{"richText": richImport(id)}})
		}
		return snapshotImportItem(&item).Fields
	}
	item := ImportItem{Choice: "merge", Merge: &ImportMergeReview{Fields: mergeImportFields(makeFields([]string{a, b, c}), makeFields([]string{b, a, c}), makeFields([]string{a, c, b}), true), Resolutions: []ImportFieldChoice{{Path: "/blocks/" + b + "/order", Choice: "keep_local"}}}}
	if err := materializeImportMerge(&item); err == nil || !strings.Contains(err.Error(), "sibling order") {
		t.Fatal("incompatible independent reorders were resolved by UUID ordering", err)
	}
}

func TestImportMergeFieldOraclePreservesMissingNullAndIndependentChanges(t *testing.T) {
	fields := func(v map[string]any) map[string]json.RawMessage {
		out := map[string]json.RawMessage{}
		for k, v := range v {
			out[k] = marshal(v)
		}
		return out
	}
	base := fields(map[string]any{"/unchanged": "same", "/remote": "before", "/local": "before", "/both": "before", "/conflict": "before", "/null": nil, "/removed": "before"})
	local := fields(map[string]any{"/unchanged": "same", "/remote": "before", "/local": "local", "/both": "same edit", "/conflict": "local", "/null": nil, "/removed": "before", "/localAdded": nil})
	incoming := fields(map[string]any{"/unchanged": "same", "/remote": "remote", "/local": "before", "/both": "same edit", "/conflict": "remote"})
	want := map[string]string{"/unchanged": "unchanged", "/remote": "apply_incoming", "/local": "keep_local", "/both": "converged", "/conflict": "conflict", "/null": "apply_incoming", "/removed": "apply_incoming", "/localAdded": "keep_local"}
	for _, f := range mergeImportFields(base, local, incoming, true) {
		if f.Outcome != want[f.Path] {
			t.Fatalf("%s: %s != %s", f.Path, f.Outcome, want[f.Path])
		}
		if f.Path == "/null" && (!f.Base.Present || string(f.Base.Value) != "null" || f.Incoming.Present) {
			t.Fatal("null collapsed into absence")
		}
	}
	for _, f := range mergeImportFields(base, local, incoming, false) {
		if f.Outcome != "conflict" {
			t.Fatal("unknown historical baseline silently accepted")
		}
	}
}

func configureMergeFixture(t *testing.T, k *Kernel, p ImportPlan, fields []any) ImportPlan {
	t.Helper()
	out, err := configurePortableImport(context.Background(), k, k.OwnerContext(), map[string]any{"planId": p.ID, "planHash": p.Hash, "resolutions": []any{map[string]any{"sourceLocator": p.Items[0].Locator, "fields": fields}}})
	if err != nil {
		t.Fatal(err)
	}
	return out.(ImportPlan)
}
func updateImportFixtureBlock(t *testing.T, k *Kernel, id, text string) {
	t.Helper()
	e := k.OwnerContext()
	var page, revision, pageRevision string
	if err := k.db.QueryRow("SELECT b.page_id,b.revision,p.revision FROM blocks b JOIN pages p ON p.id=b.page_id WHERE b.id=?", id).Scan(&page, &revision, &pageRevision); err != nil {
		t.Fatal(err)
	}
	testOK(t, testDispatch(t, k, e, "batch", testRequest(k, e, []any{map[string]any{"kind": "block.update", "blockId": id, "type": "paragraph", "schemaVersion": 1, "data": map[string]any{"richText": richImport(text)}}}, required("page", page, pageRevision), required("block", id, revision))))
}

func TestImportThreeWayRetainsLocalEditAndAppliesIndependentIncomingAfterRestart(t *testing.T) {
	k, e := testKernel(t)
	first := completeImportFixture(t, k, e, importFixture(t, k, e, map[string]string{"A.html": `<p id="one">base one</p><p id="two">base two</p>`}))
	item := first.Items[0]
	updateImportFixtureBlock(t, k, item.Blocks[0].ID, "local one")
	preview := importFixture(t, k, e, map[string]string{"A.html": `<p id="one">base one</p><p id="two">incoming two</p>`})
	if preview.Items[0].Merge == nil || preview.Items[0].Merge.BaselineStatus != "retained" {
		t.Fatal("real retained baseline missing")
	}
	for _, f := range preview.Items[0].Merge.Fields {
		if f.Outcome == "conflict" {
			t.Fatalf("independent edit conflicted: %s", f.Path)
		}
	}
	preview = configureMergeFixture(t, k, preview, nil)
	workspace := k.Path()
	k.Close()
	reopened, err := Open(workspace)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	preview, err = loadImportPlan(context.Background(), reopened.db, preview.ID)
	if err != nil {
		t.Fatal(err)
	}
	completeImportFixture(t, reopened, reopened.OwnerContext(), preview)
	got := string(marshal(readPage(t, reopened, reopened.OwnerContext(), item.PageID)))
	if !strings.Contains(got, "local one") || !strings.Contains(got, "incoming two") {
		t.Fatal("three-way merge lost an independent edit", got)
	}
	next := importFixture(t, reopened, reopened.OwnerContext(), map[string]string{"A.html": `<p id="one">base one</p><p id="two">incoming two</p>`})
	if next.Items[0].Choice != "keep_local" {
		t.Fatal("resolved source did not advance its baseline")
	}
}

func TestImportThreeWayConflictPersistsResolvesAndRejectsStaleReview(t *testing.T) {
	k, e := testKernel(t)
	first := completeImportFixture(t, k, e, importFixture(t, k, e, map[string]string{"A.html": `<p id="one">base</p>`}))
	item := first.Items[0]
	updateImportFixtureBlock(t, k, item.Blocks[0].ID, "local")
	preview := importFixture(t, k, e, map[string]string{"A.html": `<p id="one">incoming</p>`})
	path := "/blocks/" + item.Blocks[0].ID + "/content"
	if _, err := commitPortableImport(context.Background(), k, e, map[string]any{"planId": preview.ID, "planHash": preview.Hash}); err == nil {
		t.Fatal("unresolved conflict committed")
	}
	preview = configureMergeFixture(t, k, preview, []any{map[string]any{"path": path, "choice": "merged", "value": map[string]any{"present": true, "value": map[string]any{"type": "paragraph", "data": map[string]any{"richText": richImport("reviewed synthesis")}}}}})
	updateImportFixtureBlock(t, k, item.Blocks[0].ID, "later local")
	if _, err := commitPortableImport(context.Background(), k, e, map[string]any{"planId": preview.ID, "planHash": preview.Hash}); err == nil {
		t.Fatal("stale review overwrote a later edit")
	}
	fresh := importFixture(t, k, e, map[string]string{"A.html": `<p id="one">incoming</p>`})
	fresh = configureMergeFixture(t, k, fresh, []any{map[string]any{"path": path, "choice": "keep_local"}})
	completeImportFixture(t, k, e, fresh)
	if !strings.Contains(string(marshal(readPage(t, k, e, item.PageID))), "later local") {
		t.Fatal("chosen local field lost")
	}
	var base string
	if err := k.db.QueryRow("SELECT snapshot_json FROM import_baselines WHERE mapping_id=?", item.MappingID).Scan(&base); err != nil || !strings.Contains(base, "incoming") {
		t.Fatal("resolved incoming baseline was not retained", err)
	}
}

func TestImportBaselineUnavailableRequiresExplicitReviewAndDoesNotGuessFromLocal(t *testing.T) {
	k, e := testKernel(t)
	first := completeImportFixture(t, k, e, importFixture(t, k, e, map[string]string{"A.html": `<p id="one">base</p>`}))
	if err := k.write(context.Background(), func(tx *sql.Tx) error { _, err := tx.Exec("DELETE FROM import_baselines"); return err }); err != nil {
		t.Fatal(err)
	}
	preview := importFixture(t, k, e, map[string]string{"A.html": `<p id="one">base</p>`})
	if preview.Items[0].Merge.BaselineStatus != "unavailable" || preview.Items[0].Choice != "" {
		t.Fatal("legacy evidence falsely treated as normalized baseline")
	}
	for _, f := range preview.Items[0].Merge.Fields {
		if f.Outcome != "conflict" || f.Base.Present {
			t.Fatal("baseline was fabricated from local state")
		}
	}
	out, err := configurePortableImport(context.Background(), k, e, map[string]any{"planId": preview.ID, "planHash": preview.Hash, "choices": []any{map[string]any{"sourceLocator": "A.html", "choice": "keep_local"}}})
	if err != nil {
		t.Fatal(err)
	}
	completeImportFixture(t, k, e, out.(ImportPlan))
	var count int
	if err = k.db.QueryRow("SELECT count(*) FROM import_baselines WHERE mapping_id=?", first.Items[0].MappingID).Scan(&count); err != nil || count != 1 {
		t.Fatal("explicit review did not establish retained baseline", err)
	}
}

func TestImportMergePreservesIdentityAcrossSourceRenameAndBlockMove(t *testing.T) {
	k, e := testKernel(t)
	oldName := "Before 0123456789abcdef0123456789abcdef.html"
	newName := "After 0123456789abcdef0123456789abcdef.html"
	first := completeImportFixture(t, k, e, importFixture(t, k, e, map[string]string{oldName: `<details id="parent"><summary>Parent</summary><p id="one">one</p></details><p id="two">two</p>`}))
	preview := importFixture(t, k, e, map[string]string{newName: `<details id="parent"><summary>Parent</summary><p id="two">two</p><p id="one">one</p></details>`})
	if preview.Items[0].PageID != first.Items[0].PageID {
		t.Fatal("source filename changed page identity")
	}
	preview = configureMergeFixture(t, k, preview, nil)
	completeImportFixture(t, k, e, preview)
	var title string
	if err := k.db.QueryRow("SELECT title FROM pages WHERE id=?", first.Items[0].PageID).Scan(&title); err != nil || title != "After" {
		t.Fatal("title did not merge", err)
	}
	var parent sql.NullString
	if err := k.db.QueryRow("SELECT parent_block_id FROM blocks WHERE id=?", first.Items[0].Blocks[2].ID).Scan(&parent); err != nil || parent.String != first.Items[0].Blocks[0].ID {
		t.Fatal("stable block identity did not move", err)
	}
}
