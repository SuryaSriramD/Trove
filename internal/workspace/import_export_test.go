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

func importFixture(t *testing.T, k *Kernel, e contracts.ServiceExecutionContext, entries map[string]string) ImportPlan {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, text := range entries {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = w.Write([]byte(text)); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	name := filepath.Join(t.TempDir(), "fixture.zip")
	if err := os.WriteFile(name, buf.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	handle, err := k.RegisterSelection(name)
	if err != nil {
		t.Fatal(err)
	}
	out, err := previewPortableImport(context.Background(), k, e, map[string]any{"sourceHandle": handle, "sourceNamespace": "fixture"})
	if err != nil {
		t.Fatal(err)
	}
	return out.(ImportPlan)
}
func completeImportFixture(t *testing.T, k *Kernel, e contracts.ServiceExecutionContext, p ImportPlan) ImportPlan {
	t.Helper()
	for n := 0; n < len(p.Items)+3 && p.State != "complete"; n++ {
		out, err := commitPortableImport(context.Background(), k, e, map[string]any{"planId": p.ID, "planHash": p.Hash})
		if err != nil {
			t.Fatalf("cursor=%d initialized=%v item=%s error=%v", p.Cursor, p.Initialized, p.Items[p.Cursor].Locator, err)
		}
		p = out.(ImportPlan)
	}
	if p.State != "complete" {
		t.Fatalf("did not complete: %s", marshal(p))
	}
	return p
}
func TestPortableImportPreservesInventoryHierarchyDataAndLinks(t *testing.T) {
	k, e := testKernel(t)
	p := importFixture(t, k, e, map[string]string{"Parent 0123456789abcdef0123456789abcdef.md": "# Parent\nHello [child](Parent%200123456789abcdef0123456789abcdef/Child.md)\n![diagram](asset.png)", "Parent 0123456789abcdef0123456789abcdef/Child.md": "# Child\nReadable nested content\n```go\nfmt.Println(42)\n```", "asset.png": "PNG fixture", "Tasks.csv": "Name,Amount,Done,When,Formula\nShip,12345678901234567890.50,true,2026-09-30,=1+2\nReview,0,false,,\n", "Unsupported.html": "<h1>Readable unknown content</h1>"})
	if p.SourceCompleteness != "unknown" || len(p.UnknownCompleteness) == 0 {
		t.Fatal("completeness invented")
	}
	p = completeImportFixture(t, k, e, p)
	var parent, child, csvItem ImportItem
	for _, item := range p.Items {
		switch item.Title {
		case "Parent":
			parent = item
		case "Child":
			child = item
		case "Tasks":
			csvItem = item
		}
		if !item.Committed {
			t.Fatalf("unaccounted %s", item.Locator)
		}
	}
	if child.ParentID != parent.PageID {
		t.Fatalf("hierarchy lost: %s %s", child.ParentID, parent.PageID)
	}
	var links int
	if err := k.db.QueryRow("SELECT count(*) FROM page_links WHERE source_page_id=? AND target_page_id=?", parent.PageID, child.PageID).Scan(&links); err != nil || links != 1 {
		t.Fatal("link not reconstructed", links, err)
	}
	var records int
	if err := k.db.QueryRow("SELECT count(*) FROM records WHERE collection_id=?", csvItem.CollectionID).Scan(&records); err != nil || records != 2 {
		t.Fatal("CSV rows missing", records, err)
	}
	var number string
	if err := k.db.QueryRow("SELECT json_extract(v.value_json,'$.decimal') FROM property_values v JOIN properties p ON p.id=v.property_id WHERE p.name='Amount' AND json_extract(v.value_json,'$.decimal')<>'0'").Scan(&number); err != nil || number != "12345678901234567890.50" {
		t.Fatal("decimal precision lost", number, err)
	}
	var formula string
	if err := k.db.QueryRow("SELECT type FROM properties WHERE name='Formula'").Scan(&formula); err != nil || formula != "text" {
		t.Fatal("formula invented", formula, err)
	}
	report := portableImportReport(p)
	if report["discoveredCount"] != report["accountedCount"] {
		t.Fatal("silent loss")
	}
	var n int
	if err := k.db.QueryRow("PRAGMA foreign_key_check").Scan(&n); err != nil && err.Error() != "sql: no rows in result set" {
		t.Fatal(err)
	}
}
func TestPortableImportRejectsUnsafeMembersAndRetainsContainer(t *testing.T) {
	k, e := testKernel(t)
	p := importFixture(t, k, e, map[string]string{"../../escape.md": "escape", "C:/evil.md": "drive", "safe.md": "Safe", "SAFE.md": "Collision"})
	failed := 0
	for _, item := range p.Items {
		if item.Outcome == "failed" {
			failed++
			if item.RawFileID != p.SourceFileID {
				t.Fatal("rejected bytes not retained in container")
			}
		}
	}
	if failed != 3 {
		t.Fatal("unsafe members not rejected", failed)
	}
	p = completeImportFixture(t, k, e, p)
	if portableImportReport(p)["state"] != "partial" {
		t.Fatal("false success")
	}
}
func TestPortableImportResumesAndRequiresConflictReview(t *testing.T) {
	k, e := testKernel(t)
	p := importFixture(t, k, e, map[string]string{"A.md": "original"})
	out, err := commitPortableImport(context.Background(), k, e, map[string]any{"planId": p.ID, "planHash": p.Hash})
	if err != nil {
		t.Fatal(err)
	}
	p = out.(ImportPlan)
	workspacePath := k.Path()
	k.Close()
	reopened, err := Open(workspacePath)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	owner := reopened.OwnerContext()
	p = completeImportFixture(t, reopened, owner, p)
	var page string
	for _, item := range p.Items {
		if item.Locator == "A.md" {
			page = item.PageID
		}
	}
	before, _ := loadImportPlan(context.Background(), reopened.db, p.ID)
	again := completeImportFixture(t, reopened, owner, p)
	if before.Cursor != again.Cursor {
		t.Fatal("resume replay advanced")
	}
	local := readPage(t, reopened, owner, page)["page"].(map[string]any)
	req := testRequest(reopened, owner, []any{map[string]any{"kind": "page.update", "pageId": page, "title": "Local edit"}}, required("page", page, str(local["revision"])))
	testOK(t, testDispatch(t, reopened, owner, "batch", req))
	next := importFixture(t, reopened, owner, map[string]string{"A.md": "incoming source"})
	if next.Items[0].Conflict == "" {
		t.Fatal("missing explicit conflict")
	}
	if _, err = commitPortableImport(context.Background(), reopened, owner, map[string]any{"planId": next.ID, "planHash": next.Hash}); err == nil {
		t.Fatal("unreviewed overwrite succeeded")
	}
	configured, err := configurePortableImport(context.Background(), reopened, owner, map[string]any{"planId": next.ID, "planHash": next.Hash, "choices": []any{map[string]any{"sourceLocator": "A.md", "choice": "keep_local"}}})
	if err != nil {
		t.Fatal(err)
	}
	completeImportFixture(t, reopened, owner, configured.(ImportPlan))
	result := readPage(t, reopened, owner, page)
	if result["page"].(map[string]any)["title"] != "Local edit" || !strings.Contains(string(marshal(result)), "original") {
		t.Fatal("local changes overwritten")
	}
}
func TestPortableCSVEditableInferenceAndConversionFallback(t *testing.T) {
	k, e := testKernel(t)
	p := importFixture(t, k, e, map[string]string{"Rows.csv": "Name,Value\nFirst,12\nSecond,nope\n"})
	configured, err := configurePortableImport(context.Background(), k, e, map[string]any{"planId": p.ID, "planHash": p.Hash, "columns": []any{map[string]any{"sourceLocator": "Rows.csv", "index": float64(1), "type": "number"}}})
	if err != nil {
		t.Fatal(err)
	}
	p = completeImportFixture(t, k, e, configured.(ImportPlan))
	found := false
	for _, item := range p.Items {
		if item.Kind == "row" && item.Title == "Second" {
			found = item.Outcome == "partial"
			body := readPage(t, k, e, item.PageID)
			if !strings.Contains(string(marshal(body)), "nope") {
				t.Fatal("failed conversion silently dropped value")
			}
		}
	}
	if !found {
		t.Fatal("conversion not reported")
	}
}
func TestPortableExportRoundTripIncludesAssetsAndManifest(t *testing.T) {
	k, e := testKernel(t)
	p := completeImportFixture(t, k, e, importFixture(t, k, e, map[string]string{"Parent.md": "Parent [child](Parent/Child.md)\n![asset](asset.png)", "Parent/Child.md": "roundtrip content", "Rows.csv": "Name,Value\nRecord,42\n", "asset.png": "PNG bytes"}))
	_ = p
	destination := filepath.Join(t.TempDir(), "portable.zip")
	result, err := k.ExportPortable(context.Background(), e, destination)
	if err != nil {
		t.Fatal(err)
	}
	if result.Assets < 4 || result.Collections != 1 {
		t.Fatal(result)
	}
	zr, err := zip.OpenReader(destination)
	if err != nil {
		t.Fatal(err)
	}
	defer zr.Close()
	found := false
	for _, f := range zr.File {
		if f.Name == "trove-manifest.json" {
			found = true
			r, _ := f.Open()
			var manifest map[string]any
			if err = json.NewDecoder(r).Decode(&manifest); err != nil {
				t.Fatal(err)
			}
			r.Close()
			if manifest["format"] != "trove-portable" || len(manifest["attachments"].([]any)) != result.Assets {
				t.Fatal("manifest mismatch")
			}
		}
	}
	if !found {
		t.Fatal("manifest absent")
	}
	k2, e2 := testKernel(t)
	handle, err := k2.RegisterSelection(destination)
	if err != nil {
		t.Fatal(err)
	}
	preview, err := previewPortableImport(context.Background(), k2, e2, map[string]any{"sourceHandle": handle, "sourceNamespace": "portable"})
	if err != nil {
		t.Fatal(err)
	}
	restored := completeImportFixture(t, k2, e2, preview.(ImportPlan))
	for _, item := range restored.Items {
		if item.Outcome == "failed" {
			t.Fatalf("roundtrip failed %s: %s", item.Locator, item.Reason)
		}
	}
	var count int
	if err = k2.db.QueryRow("SELECT count(*) FROM blocks WHERE data_json LIKE '%roundtrip content%'").Scan(&count); err != nil || count == 0 {
		t.Fatal("roundtrip body missing", err)
	}
}

func TestPortableQueuedImportIsBoundToReviewedPlan(t *testing.T) {
	k, e := testKernel(t)
	p := importFixture(t, k, e, map[string]string{"Queued.md": "Queued searchable content"})
	other := importFixture(t, k, e, map[string]string{"Other.md": "Cannot be committed by this job"})
	result, err := enqueuePortableImport(context.Background(), k, e, map[string]any{"planId": p.ID, "planHash": p.Hash})
	if err != nil {
		t.Fatal(err)
	}
	job := result.(contracts.JobsJob)
	claimed, err := k.ClaimJob(context.Background(), []string{"IMPORT_NOTION"})
	if err != nil {
		t.Fatal(err)
	}
	if claimed.JobId != job.JobId {
		t.Fatal("wrong job claimed")
	}
	execution, err := k.JobExecution(context.Background(), string(job.JobId))
	if err != nil {
		t.Fatal(err)
	}
	defer k.RevokeGrant(string(execution.ScopeGrantId))
	raw := testRequest(k, execution, []any{map[string]any{"kind": "import.applyPlan", "planId": other.ID, "planHash": other.Hash, "resolutions": []any{}}})
	raw["mutation"].(map[string]any)["expectedRevisions"] = []any{}
	var request contracts.ServiceBatchRequest
	if err = json.Unmarshal(marshal(raw), &request); err != nil {
		t.Fatal(err)
	}
	response := k.CommitJobBatch(context.Background(), execution, string(job.JobId), string(*claimed.LeaseToken), "wrong-plan", request)
	failure, ok := response.Value.(contracts.ServiceBatchResponseVariant2)
	if !ok || failure.Error.Code != "SCOPE_DENIED" {
		t.Fatalf("job escaped reviewed plan: %s", marshal(response))
	}
	if err = runPortableImportJob(context.Background(), k, claimed); err != nil {
		t.Fatal(err)
	}
	finished, err := loadImportPlan(context.Background(), k.db, p.ID)
	if err != nil || finished.State != "complete" {
		t.Fatal("queued import incomplete", err)
	}
	untouched, err := loadImportPlan(context.Background(), k.db, other.ID)
	if err != nil || untouched.Initialized {
		t.Fatal("other plan mutated")
	}
	report := portableImportReport(finished)
	if err = contracts.Validate("service", "ImportReport", marshal(report)); err != nil {
		t.Fatal("report violates canonical contract", err)
	}
}
