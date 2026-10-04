package workspace

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"trove.local/phase0/contracts"
)

func retainedTable(t *testing.T, k *Kernel, table string) string {
	t.Helper()
	rows, err := k.db.Query("SELECT * FROM " + table)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	columns, err := rows.Columns()
	if err != nil {
		t.Fatal(err)
	}
	out := []string{}
	for rows.Next() {
		values := make([]any, len(columns))
		dest := make([]any, len(columns))
		for i := range values {
			dest[i] = &values[i]
		}
		if err = rows.Scan(dest...); err != nil {
			t.Fatal(err)
		}
		out = append(out, string(marshal(values)))
	}
	if err = rows.Err(); err != nil {
		t.Fatal(err)
	}
	sort.Strings(out)
	return strings.Join(out, "\n")
}

func TestRestoreSecondInstallationPreservesCanonicalHistoryAndEvidence(t *testing.T) {
	ctx := context.Background()
	k, e := testKernel(t)
	plan := completeImportFixture(t, k, e, importFixture(t, k, e, map[string]string{"Notebook.md": "# RecoverableResearch\n\nSee [child](Notebook/Child.md)", "Notebook/Child.md": "# Child\n\n![asset](../assets/diagram.bin)", "Tasks.csv": "Name,Priority,Done\nRestore validation,3,true\n", "assets/diagram.bin": "retained opaque asset", "unknown.widget": "uninterpreted evidence"}))
	f := newDatabase(t, k, e)
	record, page := newRecord(t, k, e, f, "Equivalent record")
	testOK(t, setValue(t, k, e, f, record, page, f.status, "status", f.done))
	view := NewID()
	testOK(t, dbBatch(t, k, e, []any{map[string]any{"kind": "view.save", "viewId": view, "collectionId": f.collection, "owningPageId": f.page, "name": "Recovery board", "type": "board", "schemaVersion": 1, "config": map[string]any{"groupPropertyId": f.status, "hideEmptyGroups": false}, "query": emptyQuery(f.title, f.status)}}, "view", view, "collection", f.collection, "page", f.page))

	meetingPage, meetingID, _ := meetingFixture(t, k, e)
	transcriptBlock, transcriptID := NewID(), NewID()
	testOK(t, dbBatch(t, k, e, []any{map[string]any{"kind": "meeting.appendSegments", "meetingId": meetingID, "segments": []any{map[string]any{"segmentId": transcriptID, "blockId": transcriptBlock, "startMs": 0, "endMs": 1000, "text": "Preserved meeting evidence"}}}, map[string]any{"kind": "meeting.finalize", "meetingId": meetingID, "durationMs": 1200}}, "page", meetingPage, "meeting", meetingID, "block", transcriptBlock))
	source := createPage(t, k, e, "Decision source")
	memory, memoryPage := memoryFixture(t, k, e, source)
	testOK(t, dbBatch(t, k, e, []any{map[string]any{"kind": "memory.forget", "memoryId": memory}}, "memory", memory, "page", memoryPage))
	draft, audio, err := k.BeginRecording(ctx, e, "Interrupted recording")
	if err != nil {
		t.Fatal(err)
	}
	audio.Write(make([]byte, 640))
	audio.Sync()
	if _, err = k.CheckpointRecording(ctx, e, draft.ID, true, "Capture interrupted"); err != nil {
		t.Fatal(err)
	}
	audio.Close()
	testOK(t, testDispatch(t, k, e, "history.recoveryPoint", recoveryRequest(k, map[string]any{"label": "Complete native restore", "idempotencyKey": NewID()})))
	approval := NewID()
	if _, err = k.db.Exec("INSERT INTO approvals VALUES(?,?,?,?,?,?,?,?,'approved')", approval, e.Actor.ActorId, e.ScopeGrantId, 1, strings.Repeat("a", 64), "[]", timeAfterMinutes(60), e.Actor.ActorId); err != nil {
		t.Fatal(err)
	}
	tables := []string{"pages", "blocks", "collections", "properties", "property_options", "records", "property_values", "views", "page_links", "files", "attachments", "page_snapshots", "mutation_history", "committed_events", "recovery_points", "recovery_point_pages", "import_mappings", "import_runs", "import_mapping_state", "memory_entries", "memory_sources", "meetings", "transcript_segments"}
	expected := map[string]string{}
	for _, table := range tables {
		expected[table] = retainedTable(t, k, table)
	}
	backup := filepath.Join(t.TempDir(), "backup")
	manifest, err := k.CreateBackup(ctx, e, backup)
	if err != nil {
		t.Fatal(err)
	}
	if manifest.FormatVersion != 2 || len(manifest.Recordings) != 2 {
		t.Fatalf("recordings absent: %+v", manifest)
	}
	// The source remains owned and writable while an unrelated clean installation
	// receives the staged restore. The backup itself is not opened or migrated.
	destination := filepath.Join(t.TempDir(), "second-installation")
	result, err := RestoreBackupReviewed(ctx, backup, destination, manifest)
	if err != nil {
		t.Fatal(err)
	}
	if !result.IndexRebuilt {
		t.Fatal(result)
	}
	restored, err := Open(destination)
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()
	for _, table := range tables {
		if actual := retainedTable(t, restored, table); actual != expected[table] {
			t.Fatalf("%s changed across clean-install restore\nwant %s\ngot %s", table, expected[table], actual)
		}
	}
	var state string
	if err = restored.db.QueryRow("SELECT state FROM approvals WHERE approval_id=?", approval).Scan(&state); err != nil || state != "revoked" {
		t.Fatal("approval capability survived", state, err)
	}
	loaded, err := loadImportPlan(ctx, restored.db, plan.ID)
	if err != nil || loaded.State != "complete" {
		t.Fatal(loaded, err)
	}
	if len(querySearch(t, restored, restored.OwnerContext(), "RecoverableResearch", nil)["hits"].([]any)) == 0 {
		t.Fatal("rebuilt search did not find imported content")
	}
	drafts, err := restored.ListRecordings(ctx, restored.OwnerContext())
	if err != nil || len(drafts) != 1 || drafts[0].ID != draft.ID {
		t.Fatal(drafts, err)
	}
	if _, err = restored.FinalizeRecording(ctx, restored.OwnerContext(), draft.ID); err != nil {
		t.Fatal("restored recording cannot resume", err)
	}
	if _, err = VerifyBackup(ctx, backup); err != nil {
		t.Fatal("restore modified source backup", err)
	}
	if _, err = RestoreBackup(ctx, backup, destination); !os.IsExist(err) {
		t.Fatal("restore overwrote existing installation", err)
	}
}

func writeBackupManifest(t *testing.T, root string, m BackupManifest) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(root, "manifest.json"), marshal(m), 0600); err != nil {
		t.Fatal(err)
	}
}
func TestRestoreRejectsCorruptUnsafeAndChangedBackups(t *testing.T) {
	ctx := context.Background()
	k, e := testKernel(t)
	page := createPage(t, k, e, "secure backup")
	attachTestFile(t, k, e, page, stageTestFile(t, k, e, "retained bytes"), false)
	source := filepath.Join(t.TempDir(), "backup")
	original, err := k.CreateBackup(ctx, e, source)
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name   string
		mutate func(*BackupManifest)
	}{{"traversal", func(m *BackupManifest) { m.Assets[0].Path = "../outside" }}, {"short digest", func(m *BackupManifest) { m.Assets[0].ContentHash = "a" }}, {"negative size", func(m *BackupManifest) { m.Assets[0].SizeBytes = "-1" }}, {"omitted asset", func(m *BackupManifest) { m.Assets = nil }}, {"wrong cursor", func(m *BackupManifest) { m.EventCursor = "999999" }}, {"wrong workspace", func(m *BackupManifest) { m.WorkspaceID = NewID() }}}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var m BackupManifest
			json.Unmarshal(marshal(original), &m)
			tc.mutate(&m)
			writeBackupManifest(t, source, m)
			dest := filepath.Join(t.TempDir(), "rejected")
			if _, err = RestoreBackup(ctx, source, dest); err == nil {
				t.Fatal("unsafe backup restored")
			}
			if _, err = os.Lstat(dest); !os.IsNotExist(err) {
				t.Fatal("failed verification published destination", err)
			}
		})
	}
	writeBackupManifest(t, source, original)
	changed := original
	changed.BackupID = NewID()
	writeBackupManifest(t, source, changed)
	if _, err = RestoreBackupReviewed(ctx, source, filepath.Join(t.TempDir(), "changed"), original); err == nil {
		t.Fatal("changed review restored")
	}
	writeBackupManifest(t, source, original)
	asset := filepath.Join(source, filepath.FromSlash(original.Assets[0].Path))
	os.Chmod(asset, 0600)
	if err = os.WriteFile(asset, []byte("tamper"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = VerifyBackup(ctx, source); err == nil {
		t.Fatal("corrupt asset accepted")
	}
	if err = os.Remove(asset); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "outside")
	os.WriteFile(outside, []byte("retained bytes"), 0600)
	os.Symlink(outside, asset)
	if _, err = VerifyBackup(ctx, source); err == nil {
		t.Fatal("symlink asset accepted")
	}
}

func TestRestoreSubtreeIsAtomicAndPreservesUnrelatedValues(t *testing.T) {
	k, e := testKernel(t)
	root := createPage(t, k, e, "Root")
	child := NewID()
	testOK(t, dbBatch(t, k, e, []any{map[string]any{"kind": "page.create", "pageId": child, "parentPageId": root, "title": "Child"}}, "page", root, "page", child))
	testOK(t, dbBatch(t, k, e, []any{map[string]any{"kind": "page.trash", "pageId": root, "includeDescendants": true}}, "page", root, "page", child))
	value := testOK(t, testDispatch(t, k, e, "history.previewSubtree", recoveryRequest(k, map[string]any{"pageId": root})))
	var guards []contracts.ServiceExpectedRevision
	json.Unmarshal(marshal(value["expectedRevisions"]), &guards)
	commands := value["commands"].([]any)
	testOK(t, dbBatch(t, k, e, []any{map[string]any{"kind": "page.update", "pageId": child, "title": "Edited after review"}}, "page", child))
	testCode(t, testDispatch(t, k, e, "batch", testRequest(k, e, commands, guards...)), "CONFLICT")
	var deleted int
	k.db.QueryRow("SELECT count(*) FROM pages WHERE id IN(?,?) AND deleted_at IS NOT NULL", root, child).Scan(&deleted)
	if deleted != 2 {
		t.Fatal("partial subtree restored")
	}
	value = testOK(t, testDispatch(t, k, e, "history.previewSubtree", recoveryRequest(k, map[string]any{"pageId": root})))
	json.Unmarshal(marshal(value["expectedRevisions"]), &guards)
	testOK(t, testDispatch(t, k, e, "batch", testRequest(k, e, value["commands"].([]any), guards...)))
	k.db.QueryRow("SELECT count(*) FROM pages WHERE id IN(?,?) AND deleted_at IS NOT NULL", root, child).Scan(&deleted)
	if deleted != 0 {
		t.Fatal("subtree not restored")
	}
	f := newDatabase(t, k, e)
	r1, p1 := newRecord(t, k, e, f, "Restore me")
	r2, p2 := newRecord(t, k, e, f, "Keep me current")
	testOK(t, setValue(t, k, e, f, r1, p1, f.status, "status", f.open))
	testOK(t, setValue(t, k, e, f, r1, p1, f.status, "status", f.done))
	testOK(t, setValue(t, k, e, f, r2, p2, f.status, "status", f.done))
	beforeProperties := retainedTable(t, k, "properties")
	beforeOptions := retainedTable(t, k, "property_options")
	testOK(t, restoreTest(t, k, e, p1, "2"))
	if retainedTable(t, k, "properties") != beforeProperties || retainedTable(t, k, "property_options") != beforeOptions {
		t.Fatal("restore rewound shared schemas")
	}
	var actual string
	k.db.QueryRow("SELECT value_json FROM property_values WHERE record_id=? AND property_id=?", r2, f.status).Scan(&actual)
	if actual != "\""+f.done+"\"" {
		t.Fatal("unrelated record was rewound", actual)
	}
	// A schema revision conflict is explicit; old evidence stays in snapshots.
	if _, err := k.db.Exec("UPDATE properties SET schema_version=2 WHERE id=?", f.status); err != nil {
		t.Fatal(err)
	}
	testCode(t, testDispatch(t, k, e, "history.previewRestore", recoveryRequest(k, map[string]any{"pageId": p1, "restoreRevision": "2"})), "CONFLICT")
}

func TestRestoreTranscriptTimingAndForgottenMemory(t *testing.T) {
	k, e := testKernel(t)
	page, meeting, _ := meetingFixture(t, k, e)
	block, segment := NewID(), NewID()
	testOK(t, dbBatch(t, k, e, []any{map[string]any{"kind": "meeting.appendSegments", "meetingId": meeting, "segments": []any{map[string]any{"segmentId": segment, "blockId": block, "startMs": 1200, "endMs": 3400, "text": "TimestampRestore evidence"}}}, map[string]any{"kind": "meeting.finalize", "meetingId": meeting, "durationMs": 4000}}, "page", page, "meeting", meeting, "block", block))
	var revision string
	k.db.QueryRow("SELECT revision FROM pages WHERE id=?", page).Scan(&revision)
	source := createPage(t, k, e, "Memory evidence")
	memory, memoryPage := memoryFixture(t, k, e, source)
	testOK(t, dbBatch(t, k, e, []any{map[string]any{"kind": "memory.forget", "memoryId": memory}}, "memory", memory, "page", memoryPage))
	testOK(t, restoreTest(t, k, e, memoryPage, "3"))
	var state string
	k.db.QueryRow("SELECT status FROM memory_entries WHERE id=?", memory).Scan(&state)
	if state != "forgotten" {
		t.Fatal("history revived forgotten memory", state)
	}
	testOK(t, dbBatch(t, k, e, []any{map[string]any{"kind": "block.delete", "blockId": block, "includeDescendants": false}}, "page", page, "block", block))
	restored := testOK(t, restoreTest(t, k, e, page, revision))
	var storedRevision string
	if err := k.db.QueryRow("SELECT revision FROM transcript_segments WHERE id=?", segment).Scan(&storedRevision); err != nil {
		t.Fatal(err)
	}
	matched := false
	for _, raw := range restored["revisions"].([]any) {
		change := raw.(map[string]any)
		target := change["target"].(map[string]any)
		if target["entityType"] == "transcript_segment" && target["entityId"] == segment {
			matched = true
			if change["after"] != storedRevision {
				t.Fatal("segment event differs from canonical row", change, storedRevision)
			}
		}
	}
	if !matched {
		t.Fatal("missing segment revision change")
	}
	var restoredBlock string
	var start, end int64
	if err := k.db.QueryRow("SELECT block_id,start_ms,end_ms FROM transcript_segments WHERE id=? AND meeting_id=?", segment, meeting).Scan(&restoredBlock, &start, &end); err != nil {
		t.Fatal(err)
	}
	if restoredBlock != block || start != 1200 || end != 3400 {
		t.Fatal("transcript mapping changed")
	}
	hits := querySearch(t, k, e, "TimestampRestore", nil)["hits"].([]any)
	if len(hits) == 0 {
		t.Fatal("restored transcript unsearchable")
	}
	found := false
	for _, raw := range hits {
		loc := raw.(map[string]any)["location"].(map[string]any)
		if loc["transcript"] != nil {
			found = true
		}
	}
	if !found {
		t.Fatal("restored search lost timestamp navigation")
	}
}

func TestBackupRetentionIsExplicitVerifiedAndWorkspaceBound(t *testing.T) {
	ctx := context.Background()
	k, e := testKernel(t)
	createPage(t, k, e, "Retention")
	parent := t.TempDir()
	for n := 0; n < 3; n++ {
		if _, err := k.CreateBackup(ctx, e, filepath.Join(parent, "Trove-backup-"+NewID())); err != nil {
			t.Fatal(err)
		}
	}
	testOK(t, testDispatch(t, k, e, "recovery.setRetention", recoveryRequest(k, map[string]any{"backupKeepCount": 1})))
	plan, err := k.PreviewBackupCleanup(ctx, e, parent)
	if err != nil || len(plan.Remove) != 2 {
		t.Fatal(plan, err)
	}
	entries, _ := os.ReadDir(parent)
	if len(entries) != 3 {
		t.Fatal("preview deleted backups")
	}
	removed, err := k.ApplyBackupCleanup(ctx, e, plan)
	if err != nil || removed != 2 {
		t.Fatal(removed, err)
	}
	entries, _ = os.ReadDir(parent)
	if len(entries) != 1 {
		t.Fatal("retention removed wrong count")
	}
	if _, err = VerifyBackup(ctx, filepath.Join(parent, entries[0].Name())); err != nil {
		t.Fatal(err)
	}
	// A foreign workspace's valid backup is never a candidate.
	other, owner := testKernel(t)
	if _, err = other.CreateBackup(ctx, owner, filepath.Join(parent, "Trove-backup-"+NewID())); err != nil {
		t.Fatal(err)
	}
	plan, err = k.PreviewBackupCleanup(ctx, e, parent)
	if err != nil || len(plan.Remove) != 0 {
		t.Fatal(plan, err)
	}
}

func TestBackupInterruptedRecordingRetainsUnjournaledTailAndRejectsActiveCapture(t *testing.T) {
	ctx := context.Background()
	k, e := testKernel(t)
	draft, audio, err := k.BeginRecording(ctx, e, "Crash audio")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = audio.Write(make([]byte, 640)); err != nil {
		t.Fatal(err)
	}
	audio.Sync()
	if _, err = k.CheckpointRecording(ctx, e, draft.ID, false, ""); err != nil {
		t.Fatal(err)
	}
	if _, err = k.CreateBackup(ctx, e, filepath.Join(t.TempDir(), "active")); err == nil {
		t.Fatal("active capture was backed up without a stable boundary")
	}
	if _, err = audio.Write(make([]byte, 128)); err != nil {
		t.Fatal(err)
	}
	audio.Sync()
	audio.Close()
	path := k.Path()
	k.Close()
	k, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer k.Close()
	e = k.OwnerContext()
	backup := filepath.Join(t.TempDir(), "crashed")
	manifest, err := k.CreateBackup(ctx, e, backup)
	if err != nil {
		t.Fatal(err)
	}
	if len(manifest.Recordings) != 2 {
		t.Fatal(manifest)
	}
	var found bool
	for _, object := range manifest.Recordings {
		if strings.HasSuffix(object.Path, ".f32le") {
			found = true
			if object.SizeBytes != "768" {
				t.Fatal("unjournaled tail disappeared", object)
			}
		}
	}
	if !found {
		t.Fatal("raw audio absent")
	}
	restoredPath := filepath.Join(t.TempDir(), "restored")
	if _, err = RestoreBackup(ctx, backup, restoredPath); err != nil {
		t.Fatal(err)
	}
	restored, err := Open(restoredPath)
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()
	saved, err := restored.FinalizeRecording(ctx, restored.OwnerContext(), draft.ID)
	if err != nil {
		t.Fatal(err)
	}
	if saved.Bytes != 768 {
		t.Fatal("resume lost tail", saved)
	}
	// Hash verification protects both the full audio and acknowledged segments.
	rawPath := filepath.Join(backup, "recordings", draft.ID+".f32le")
	if err = os.WriteFile(rawPath, make([]byte, 4), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = VerifyBackup(ctx, backup); err == nil {
		t.Fatal("corrupt recording accepted")
	}
}
