package workspace

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

func TestOnlineBackupAllowsEditsAndCaptureWithConsistentRecovery(t *testing.T) {
	ctx := context.Background()
	k, e := testKernel(t)
	page := createPage(t, k, e, "Snapshot title")
	originalFile, _ := attachTestFile(t, k, e, page, stageTestFile(t, k, e, "SNAPSHOT_ASSET"), true)
	// Ensure several bounded backup steps without adding megabytes of test text.
	if _, err := k.db.Exec("CREATE TABLE backup_test_payload(body BLOB); INSERT INTO backup_test_payload VALUES(zeroblob(4194304))"); err != nil {
		t.Fatal(err)
	}
	draft, audio, err := k.BeginRecording(ctx, e, "Concurrent recording")
	if err != nil {
		t.Fatal(err)
	}
	defer audio.Close()
	if _, err = audio.Write(make([]byte, 640)); err != nil {
		t.Fatal(err)
	}
	if err = audio.Sync(); err != nil {
		t.Fatal(err)
	}
	if _, err = k.CheckpointRecording(ctx, e, draft.ID, false, ""); err != nil {
		t.Fatal(err)
	}
	// Unacknowledged active-capture tail must not enter the backup checkpoint.
	if _, err = audio.Write(make([]byte, 64)); err != nil {
		t.Fatal(err)
	}
	ready, resume := make(chan struct{}), make(chan struct{})
	var signal sync.Once
	var release sync.Once
	unblock := func() { release.Do(func() { close(resume) }) }
	defer unblock()
	type result struct {
		manifest BackupManifest
		err      error
	}
	done := make(chan result, 1)
	backup := filepath.Join(t.TempDir(), "online")
	steps := 0
	go func() {
		m, err := k.createBackup(ctx, e, backup, func() error { steps++; signal.Do(func() { close(ready) }); <-resume; return nil })
		done <- result{m, err}
	}()
	select {
	case <-ready:
	case r := <-done:
		t.Fatal("backup failed before copy", r.err)
	case <-time.After(5 * time.Second):
		t.Fatal("backup never reached online copy")
	}
	var revision string
	if err = k.db.QueryRow("SELECT revision FROM pages WHERE id=?", page).Scan(&revision); err != nil {
		t.Fatal(err)
	}
	editDone := make(chan json.RawMessage, 1)
	request := testRequest(k, e, []any{map[string]any{"kind": "page.update", "pageId": page, "title": "Edited during backup"}}, required("page", page, revision))
	started := time.Now()
	go func() { editDone <- k.Dispatch(ctx, e, "batch", marshal(request)) }()
	select {
	case raw := <-editDone:
		var result map[string]any
		if err = json.Unmarshal(raw, &result); err != nil {
			t.Fatal(err)
		}
		testOK(t, result)
	case <-time.After(2 * time.Second):
		unblock()
		t.Fatal("editor remained blocked by database copying")
	}
	t.Logf("concurrent edit while SQLite copy is paused: %s", time.Since(started))
	newFile, _ := attachTestFile(t, k, e, page, stageTestFile(t, k, e, "POST_SNAPSHOT_ASSET"), true)
	if _, err = audio.Write(make([]byte, 64)); err != nil {
		t.Fatal(err)
	}
	if err = audio.Sync(); err != nil {
		t.Fatal(err)
	}
	captureDone := make(chan error, 1)
	started = time.Now()
	go func() {
		_, err := k.CheckpointRecording(ctx, e, draft.ID, true, "")
		if err == nil {
			_, err = k.FinalizeRecording(ctx, e, draft.ID)
		}
		captureDone <- err
	}()
	select {
	case err = <-captureDone:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		unblock()
		t.Fatal("capture/finalization remained blocked by database copying")
	}
	t.Logf("checkpoint and finalization while SQLite copy is paused: %s", time.Since(started))
	unblock()
	r := <-done
	if r.err != nil {
		t.Fatal(r.err)
	}
	if steps < 2 {
		t.Fatal("fixture did not exercise incremental backup", steps)
	}
	if len(r.manifest.Assets) != 1 || r.manifest.Assets[0].FileID != originalFile {
		t.Fatal("asset set escaped snapshot boundary", r.manifest.Assets, newFile)
	}
	if _, err = VerifyBackup(ctx, backup); err != nil {
		t.Fatal(err)
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
	if readPage(t, restored, restored.OwnerContext(), page)["page"].(map[string]any)["title"] != "Snapshot title" {
		t.Fatal("database includes post-boundary edit")
	}
	saved, err := restored.FinalizeRecording(ctx, restored.OwnerContext(), draft.ID)
	if err != nil || saved.Bytes != 640 {
		t.Fatal("backup recording checkpoint is inconsistent", saved, err)
	}
	live, err := k.readRecording(e, draft.ID)
	if err != nil || live.State != "saved" || live.Bytes != 768 {
		t.Fatal("backup rewound source recording", live, err)
	}
	if _, err = VerifyBackup(ctx, backup); err != nil {
		t.Fatal("staged restoration modified backup", err)
	}
	var pins int
	if err = k.db.QueryRow("SELECT count(*) FROM backup_pins").Scan(&pins); err != nil || pins != 0 {
		t.Fatal("completed backup leaked pins", pins, err)
	}
}

func TestOnlineBackupCancellationAndDiskFailureKeepSourceAndReleasePins(t *testing.T) {
	for _, failure := range []string{"cancel", "disk full"} {
		t.Run(failure, func(t *testing.T) {
			k, e := testKernel(t)
			page := createPage(t, k, e, "Source remains usable")
			attachTestFile(t, k, e, page, stageTestFile(t, k, e, "SOURCE_BYTES"), true)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			destination := filepath.Join(t.TempDir(), "failed")
			m, err := k.createBackup(ctx, e, destination, func() error {
				if failure == "cancel" {
					cancel()
					return ctx.Err()
				}
				return &os.PathError{Op: "write", Path: filepath.Join(destination, "workspace.db"), Err: syscall.ENOSPC}
			})
			if failure == "cancel" && !errors.Is(err, context.Canceled) {
				t.Fatal("cancellation lost", err)
			}
			if failure == "disk full" && !errors.Is(err, syscall.ENOSPC) {
				t.Fatal("disk-full error lost", err)
			}
			if _, err = os.Stat(filepath.Join(destination, "INCOMPLETE")); err != nil {
				t.Fatal("partial output looks complete", err)
			}
			if _, err = VerifyBackup(context.Background(), destination); err == nil {
				t.Fatal("failed output accepted")
			}
			var pins int
			var state string
			if err = k.db.QueryRow("SELECT count(*) FROM backup_pins WHERE backup_id=?", m.BackupID).Scan(&pins); err != nil || pins != 0 {
				t.Fatal("failure leaked asset pins", pins, err)
			}
			if err = k.db.QueryRow("SELECT state FROM backups WHERE id=?", m.BackupID).Scan(&state); err != nil || state != "failed" {
				t.Fatal("failure state missing", state, err)
			}
			if !strings.Contains(string(marshal(readPage(t, k, e, page))), "Source remains usable") {
				t.Fatal("failure changed source")
			}
			testOK(t, dbBatch(t, k, e, []any{map[string]any{"kind": "page.update", "pageId": page, "title": "Still editable"}}, "page", page))
		})
	}
}
