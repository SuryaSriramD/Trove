package workspace

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"trove.local/phase0/contracts"
)

func recoveryRequest(k *Kernel, extra map[string]any) map[string]any {
	r := map[string]any{"contractVersion": 1, "requestId": NewID(), "workspaceId": k.ID()}
	for key, v := range extra {
		r[key] = v
	}
	return r
}
func stageTestFile(t *testing.T, k *Kernel, e contracts.ServiceExecutionContext, body string) map[string]any {
	t.Helper()
	path := filepath.Join(t.TempDir(), "fixture.txt")
	if err := os.WriteFile(path, []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
	handle, err := k.RegisterSelection(path)
	if err != nil {
		t.Fatal(err)
	}
	return testOK(t, testDispatch(t, k, e, "prepareFile", recoveryRequest(k, map[string]any{"selectionHandle": handle})))
}
func attachTestFile(t *testing.T, k *Kernel, e contracts.ServiceExecutionContext, page string, prepared map[string]any, block bool) (string, string) {
	t.Helper()
	id := str(prepared["fileId"])
	var pageRevision, fileRevision string
	if err := k.db.QueryRow("SELECT revision FROM pages WHERE id=?", page).Scan(&pageRevision); err != nil {
		t.Fatal(err)
	}
	if err := k.db.QueryRow("SELECT revision FROM files WHERE id=?", id).Scan(&fileRevision); err != nil {
		fileRevision = "0"
	}
	command := map[string]any{"kind": "file.attach", "pageId": page, "fileId": id, "ingestHandle": prepared["ingestHandle"]}
	commands := []any{command}
	guards := []contracts.ServiceExpectedRevision{required("page", page, pageRevision), required("file", id, fileRevision)}
	bid := ""
	if block {
		bid = NewID()
		command["blockId"] = bid
		commands = append(commands, map[string]any{"kind": "block.insert", "block": map[string]any{"blockId": bid, "pageId": page, "type": "file", "schemaVersion": 1, "data": map[string]any{"fileId": id, "caption": []any{}, "displayName": "fixture.txt"}}})
		guards = append(guards, required("block", bid, "0"))
	}
	testOK(t, testDispatch(t, k, e, "batch", testRequest(k, e, commands, guards...)))
	return id, bid
}
func TestAssetDeduplicationScopedReadsAndRestart(t *testing.T) {
	k, e := testKernel(t)
	page := createPage(t, k, e, "attachments")
	first := stageTestFile(t, k, e, "immutable తెలుగు 中文 🌱")
	id, _ := attachTestFile(t, k, e, page, first, true)
	second := stageTestFile(t, k, e, "immutable తెలుగు 中文 🌱")
	if first["fileId"] != second["fileId"] || first["contentHash"] != second["contentHash"] {
		t.Fatal("identical bytes duplicated identity")
	}
	otherPage := createPage(t, k, e, "second relationship")
	attachTestFile(t, k, e, otherPage, second, false)
	deniedPage := createPage(t, k, e, "unrelated")
	scoped := k.Grant(e.Actor, "user", contracts.DomainScope{Value: contracts.DomainScopeVariant3{Kind: "pages", WorkspaceId: contracts.DomainId(k.ID()), PageIds: []contracts.DomainId{contracts.DomainId(deniedPage)}}})
	if f, _, err := k.ReadFile(context.Background(), scoped, id); err == nil {
		f.Close()
		t.Fatal("cross-scope file read")
	}
	path := k.Path()
	k.Close()
	opened, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer opened.Close()
	f, meta, err := opened.ReadFile(context.Background(), opened.OwnerContext(), id)
	if err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(f)
	f.Close()
	if err != nil || string(body) != "immutable తెలుగు 中文 🌱" || string(meta.ContentHash) != first["contentHash"] {
		t.Fatal(meta, err, string(body))
	}
	var count int
	opened.db.QueryRow("SELECT count(*) FROM files").Scan(&count)
	if count != 1 {
		t.Fatal("expected one canonical file", count)
	}
	opened.db.QueryRow("SELECT count(*) FROM attachments WHERE file_id=?", id).Scan(&count)
	if count != 2 {
		t.Fatal("attachment relationships collapsed", count)
	}
}
func TestFileSelectionsRejectSymlinksAndInvalidHandles(t *testing.T) {
	k, e := testKernel(t)
	path := filepath.Join(t.TempDir(), "source")
	os.WriteFile(path, []byte("bytes"), 0600)
	link := path + "-link"
	os.Symlink(path, link)
	if _, err := k.RegisterSelection(link); err == nil {
		t.Fatal("symlink accepted")
	}
	if _, err := k.RegisterSelection(filepath.Dir(path)); err == nil {
		t.Fatal("directory accepted")
	}
	testCode(t, testDispatch(t, k, e, "prepareFile", recoveryRequest(k, map[string]any{"selectionHandle": NewID()})), "NOT_FOUND")
	prepared := stageTestFile(t, k, e, "content")
	page := createPage(t, k, e, "page")
	other := k.Grant(e.Actor, "user", e.Scope)
	bad := testRequest(k, other, []any{map[string]any{"kind": "file.attach", "pageId": page, "fileId": prepared["fileId"], "ingestHandle": prepared["ingestHandle"]}}, required("page", page, "1"), required("file", str(prepared["fileId"]), "0"))
	testCode(t, testDispatch(t, k, other, "batch", bad), "SCOPE_DENIED")
}
func restoreTest(t *testing.T, k *Kernel, e contracts.ServiceExecutionContext, page, revision string) map[string]any {
	t.Helper()
	preview := testOK(t, testDispatch(t, k, e, "history.previewRestore", recoveryRequest(k, map[string]any{"pageId": page, "restoreRevision": revision})))
	var guards []contracts.ServiceExpectedRevision
	json.Unmarshal(marshal(preview["expectedRevisions"]), &guards)
	return testDispatch(t, k, e, "batch", testRequest(k, e, []any{map[string]any{"kind": "history.restore", "pageId": page, "restoreRevision": revision, "previewId": preview["previewId"]}}, guards...))
}
func TestTrashHistoryRestoreAndStalePreview(t *testing.T) {
	k, e := testKernel(t)
	page := createPage(t, k, e, "original")
	prepared := stageTestFile(t, k, e, "history attachment")
	file, block := attachTestFile(t, k, e, page, prepared, true)
	testOK(t, testDispatch(t, k, e, "batch", testRequest(k, e, []any{map[string]any{"kind": "page.update", "pageId": page, "title": "edited"}}, required("page", page, "2"))))
	preview := testOK(t, testDispatch(t, k, e, "history.previewRestore", recoveryRequest(k, map[string]any{"pageId": page, "restoreRevision": "2"})))
	testOK(t, testDispatch(t, k, e, "batch", testRequest(k, e, []any{map[string]any{"kind": "page.trash", "pageId": page, "includeDescendants": false}}, required("page", page, "3"))))
	var guards []contracts.ServiceExpectedRevision
	json.Unmarshal(marshal(preview["expectedRevisions"]), &guards)
	testCode(t, testDispatch(t, k, e, "batch", testRequest(k, e, []any{map[string]any{"kind": "history.restore", "pageId": page, "restoreRevision": "2", "previewId": preview["previewId"]}}, guards...)), "CONFLICT")
	trash := testOK(t, testDispatch(t, k, e, "history.trash", recoveryRequest(k, nil)))
	if len(trash["pages"].([]any)) != 1 {
		t.Fatal(trash)
	}
	testOK(t, restoreTest(t, k, e, page, "2"))
	restored := readPage(t, k, e, page)
	p := restored["page"].(map[string]any)
	if p["title"] != "original" || p["revision"] != "5" || p["deletedAt"] != nil || len(restored["blocks"].([]any)) != 1 {
		t.Fatal(restored)
	}
	if restored["blocks"].([]any)[0].(map[string]any)["id"] != block {
		t.Fatal("stable block identity lost")
	}
	f, _, err := k.ReadFile(context.Background(), e, file)
	if err != nil {
		t.Fatal(err)
	}
	f.Close()
	history := testOK(t, testDispatch(t, k, e, "history.list", recoveryRequest(k, map[string]any{"pageId": page})))
	if len(history["revisions"].([]any)) != 5 {
		t.Fatal(history)
	}
	// Historical reads expose exactly the public ReadPageResult, not private
	// record/attachment restoration metadata.
	response := k.Dispatch(context.Background(), e, "readPage", marshal(recoveryRequest(k, map[string]any{"pageId": page, "atRevision": "2"})))
	if err = contracts.Validate("service", "ReadPageResponse", response); err != nil {
		t.Fatal(err, string(response))
	}
}
func TestRestoreDeletedBlockKeepsMonotonicRevision(t *testing.T) {
	k, e := testKernel(t)
	page := createPage(t, k, e, "fixture")
	_, block := attachTestFile(t, k, e, page, stageTestFile(t, k, e, "persist"), true)
	testOK(t, testDispatch(t, k, e, "batch", testRequest(k, e, []any{map[string]any{"kind": "block.delete", "blockId": block, "includeDescendants": false}}, required("page", page, "2"), required("block", block, "1"))))
	testOK(t, restoreTest(t, k, e, page, "2"))
	p := readPage(t, k, e, page)
	if p["blocks"].([]any)[0].(map[string]any)["revision"] != "3" {
		t.Fatal("restored block revision rewound", p)
	}
}
func TestBackupSnapshotRetentionAndRecoveryPoint(t *testing.T) {
	k, e := testKernel(t)
	page := createPage(t, k, e, "backup source")
	file, block := attachTestFile(t, k, e, page, stageTestFile(t, k, e, "backup bytes"), true)
	request := recoveryRequest(k, map[string]any{"label": "Before cleanup", "idempotencyKey": NewID()})
	point := testOK(t, testDispatch(t, k, e, "history.recoveryPoint", request))
	request["requestId"] = NewID()
	again := testOK(t, testDispatch(t, k, e, "history.recoveryPoint", request))
	if point["recoveryPointId"] != again["recoveryPointId"] {
		t.Fatal("recovery point replay duplicated")
	}
	// Remove the live block: asset remains needed by snapshots and recovery point.
	testOK(t, testDispatch(t, k, e, "batch", testRequest(k, e, []any{map[string]any{"kind": "block.delete", "blockId": block, "includeDescendants": false}}, required("page", page, "2"), required("block", block, "1"))))
	removed, err := k.CollectAssets(context.Background(), e)
	if err != nil || len(removed) != 0 {
		t.Fatal(removed, err)
	}
	backup := filepath.Join(t.TempDir(), "backup")
	manifest, err := k.CreateBackup(context.Background(), e, backup)
	if err != nil {
		t.Fatal(err)
	}
	if len(manifest.Assets) != 1 {
		t.Fatal(manifest)
	}
	if _, err = VerifyBackup(context.Background(), backup); err != nil {
		t.Fatal(err)
	}
	if _, err = k.CreateBackup(context.Background(), e, backup); !os.IsExist(err) {
		t.Fatal("existing destination overwritten", err)
	}
	restored, err := Open(backup)
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()
	owner := restored.OwnerContext()
	if readPage(t, restored, owner, page)["page"].(map[string]any)["title"] != "backup source" {
		t.Fatal("page missing from backup")
	}
	f, _, err := restored.ReadFile(context.Background(), owner, file)
	if err != nil {
		t.Fatal(err)
	}
	bytes, _ := io.ReadAll(f)
	f.Close()
	if string(bytes) != "backup bytes" {
		t.Fatal(string(bytes))
	}
	testOK(t, restoreTest(t, restored, owner, page, "2"))
	var active int
	k.db.QueryRow("SELECT count(*) FROM backup_pins").Scan(&active)
	if active != 0 {
		t.Fatal("completed backup leaked pins")
	}
}
func TestAbandonedAssetsBackupPinsAndTamperDetection(t *testing.T) {
	k, e := testKernel(t)
	hash := strings.Repeat("a", 64)
	dir := filepath.Join(k.path, "files", "aa")
	os.MkdirAll(dir, 0700)
	path := filepath.Join(dir, hash)
	os.WriteFile(path, []byte("orphan"), 0600)
	old := time.Now().Add(-48 * time.Hour)
	os.Chtimes(path, old, old)
	backup := NewID()
	k.db.Exec("INSERT INTO backups VALUES(?,'active',?,?)", backup, now(), now())
	k.db.Exec("INSERT INTO backup_pins VALUES(?,?)", backup, hash)
	removed, err := k.CollectAssets(context.Background(), e)
	if err != nil || len(removed) != 0 {
		t.Fatal(removed, err)
	}
	k.db.Exec("DELETE FROM backup_pins WHERE backup_id=?", backup)
	removed, err = k.CollectAssets(context.Background(), e)
	if err != nil || len(removed) != 1 {
		t.Fatal(removed, err)
	}
	page := createPage(t, k, e, "tamper")
	id, _ := attachTestFile(t, k, e, page, stageTestFile(t, k, e, "immutable"), false)
	meta, err := readFileMetadata(context.Background(), k.db, id)
	if err != nil {
		t.Fatal(err)
	}
	asset := filepath.Join(k.path, meta.StorageKey)
	os.Chmod(asset, 0600)
	os.WriteFile(asset, []byte("corrupted"), 0600)
	if f, _, err := k.ReadFile(context.Background(), e, id); err == nil {
		f.Close()
		t.Fatal("corrupt asset returned")
	}
	backupDir := filepath.Join(t.TempDir(), "failed")
	if _, err = k.CreateBackup(context.Background(), e, backupDir); err == nil {
		t.Fatal("corrupt backup succeeded")
	}
	if _, err = VerifyBackup(context.Background(), backupDir); err == nil {
		t.Fatal("partial backup accepted")
	}
}

func TestRecordTaskHistoryRestoresValuesWithoutCopyingIdentity(t *testing.T) {
	k, e := testKernel(t)
	f := ensureTestTasks(t, k, e)
	record, page := NewID(), NewID()
	testOK(t, dbBatch(t, k, e, []any{map[string]any{"kind": "task.create", "recordId": record, "pageId": page, "title": "Original task"}}, "record", record, "page", page, "collection", f.collection, "property", f.status))
	testOK(t, dbBatch(t, k, e, []any{map[string]any{"kind": "task.setStatus", "recordId": record, "statusOptionId": f.done}, map[string]any{"kind": "page.update", "pageId": page, "title": "Completed renamed task"}}, "record", record, "page", page, "collection", f.collection, "property", f.status))
	testOK(t, restoreTest(t, k, e, page, "1"))
	var title, status, rr, pr string
	if err := k.db.QueryRow("SELECT p.title,v.value_json,r.revision,p.revision FROM records r JOIN pages p ON p.id=r.page_id JOIN property_values v ON v.record_id=r.id WHERE r.id=? AND v.property_id=?", record, f.status).Scan(&title, &status, &rr, &pr); err != nil {
		t.Fatal(err)
	}
	if title != "Original task" || status != "\""+f.open+"\"" || rr != "3" || pr != rr {
		t.Fatal(title, status, rr, pr)
	}
	var count int
	k.db.QueryRow("SELECT count(*) FROM task_facets WHERE record_id=?", record).Scan(&count)
	if count != 1 {
		t.Fatal("task identity lost")
	}
	var originalCollection string
	k.db.QueryRow("SELECT collection_id FROM records WHERE id=?", record).Scan(&originalCollection)
	if originalCollection != f.collection {
		t.Fatal("task was copied to another collection")
	}
}

func TestAttachmentReferencesFollowCoverPropertiesAndBlockChanges(t *testing.T) {
	k, e := testKernel(t)
	source := createPage(t, k, e, "ingested")
	id, _ := attachTestFile(t, k, e, source, stageTestFile(t, k, e, "retained in property"), false)
	f := newDatabase(t, k, e)
	record, page := newRecord(t, k, e, f, "record attachments")
	property := NewID()
	testOK(t, dbBatch(t, k, e, []any{map[string]any{"kind": "collection.addProperty", "collectionId": f.collection, "propertyId": property, "name": "Files", "type": "files", "schemaVersion": 1, "config": map[string]any{}}}, "collection", f.collection, "property", property))
	testOK(t, setValue(t, k, e, f, record, page, property, "files", []any{map[string]any{"fileId": id, "displayName": "linked.txt"}}))
	var count int
	k.db.QueryRow("SELECT count(*) FROM attachments WHERE page_id=? AND file_id=?", page, id).Scan(&count)
	if count != 1 {
		t.Fatal("files property missing attachment projection")
	}
	coverPage := createPage(t, k, e, "cover")
	testOK(t, dbBatch(t, k, e, []any{map[string]any{"kind": "page.update", "pageId": coverPage, "coverFileId": id}}, "page", coverPage))
	scoped := k.Grant(e.Actor, "user", contracts.DomainScope{Value: contracts.DomainScopeVariant3{Kind: "pages", WorkspaceId: contracts.DomainId(k.ID()), PageIds: []contracts.DomainId{contracts.DomainId(coverPage)}}})
	file, _, err := k.ReadFile(context.Background(), scoped, id)
	if err != nil {
		t.Fatal(err)
	}
	file.Close()
	testOK(t, dbBatch(t, k, e, []any{map[string]any{"kind": "page.update", "pageId": coverPage, "coverFileId": nil}}, "page", coverPage))
	if file, _, err = k.ReadFile(context.Background(), scoped, id); err == nil {
		file.Close()
		t.Fatal("removed cover retained live scoped access")
	}
	// The historical cover remains safely restorable by the workspace owner.
	testOK(t, restoreTest(t, k, e, coverPage, "2"))
}

func TestCancelledBackupAndRestartRecoverAbandonedPins(t *testing.T) {
	k, e := testKernel(t)
	page := createPage(t, k, e, "backup")
	attachTestFile(t, k, e, page, stageTestFile(t, k, e, "retained"), false)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	destination := filepath.Join(t.TempDir(), "cancelled")
	if _, err := k.CreateBackup(ctx, e, destination); err == nil {
		t.Fatal("cancelled backup succeeded")
	}
	if _, err := VerifyBackup(context.Background(), destination); err == nil {
		t.Fatal("cancelled backup accepted")
	}
	id := NewID()
	k.db.Exec("INSERT INTO backups VALUES(?,'active',?,?)", id, now(), now())
	k.db.Exec("INSERT INTO backup_pins SELECT ?,content_hash FROM files", id)
	path := k.Path()
	k.Close()
	opened, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer opened.Close()
	var state string
	if err = opened.db.QueryRow("SELECT state FROM backups WHERE id=?", id).Scan(&state); err != nil || state != "interrupted" {
		t.Fatal(state, err)
	}
	var pins int
	opened.db.QueryRow("SELECT count(*) FROM backup_pins WHERE backup_id=?", id).Scan(&pins)
	if pins != 0 {
		t.Fatal("abandoned lease was not recovered")
	}
}
