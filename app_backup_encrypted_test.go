package main

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"trove.local/phase0/contracts"
	"trove.local/phase0/internal/workspace"
)

func TestNativeBackupCancellationAndProgress(t *testing.T) {
	a := NewApp()
	defer backupOperationStates.Delete(a)
	ctx, done, err := a.beginBackupOperation()
	if err != nil {
		t.Fatal(err)
	}
	defer done()
	if _, _, err = a.beginBackupOperation(); err == nil {
		t.Fatal("concurrent operation accepted")
	}
	a.backupProgress(workspace.BackupProgress{Version: 1, Phase: "authenticating", Bytes: "42", TotalBytes: "100"})
	raw, err := a.NativeBackupProgress()
	if err != nil {
		t.Fatal(err)
	}
	if err = contracts.Validate("service", "NativeBackupProgress", []byte(raw)); err != nil {
		t.Fatal(err)
	}
	var p workspace.BackupProgress
	json.Unmarshal([]byte(raw), &p)
	if p.Phase != "authenticating" || p.Bytes != "42" {
		t.Fatal(p)
	}
	a.CancelBackupOperation()
	if !errors.Is(ctx.Err(), context.Canceled) {
		t.Fatal("operation not cancelled")
	}
}

func TestNativeBackupReviewBoundedExpiryAndDisposal(t *testing.T) {
	a := NewApp()
	defer nativeRestoreStates.Delete(a)
	id := a.saveRestorePreview(nativeRestorePreview{source: "native-only"})
	a.CloseNativeRestorePreview(id)
	if _, err := a.takeRestorePreview(id); err == nil {
		t.Fatal("dismissed review remained live")
	}
	s := a.restoreState()
	s.mu.Lock()
	s.previews["expired"] = nativeRestorePreview{expires: time.Now().Add(-time.Minute)}
	s.mu.Unlock()
	if _, err := a.takeRestorePreview("expired"); err == nil {
		t.Fatal("expired review accepted")
	}
	for i := 0; i < 50; i++ {
		a.saveRestorePreview(nativeRestorePreview{source: "native-only"})
	}
	s.mu.Lock()
	count := len(s.previews)
	s.mu.Unlock()
	if count > 16 {
		t.Fatal("unbounded private previews", count)
	}
	a.closeBackupPreviews()
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.previews) != 0 {
		t.Fatal("shutdown retains reviews")
	}
}

func TestNativeBackupShutdownRejectsNewOperations(t *testing.T) {
	a := NewApp()
	defer nativeRestoreStates.Delete(a)
	defer backupOperationStates.Delete(a)
	a.shutdownOnce.Do(func() { close(a.shutdownDone) })
	if _, _, err := a.beginBackupOperation(); err == nil {
		t.Fatal("operation started after shutdown")
	}
	a.closeBackupPreviews()
	if id := a.saveRestorePreview(nativeRestorePreview{source: "late result"}); id != "" {
		t.Fatal("late preview retained after shutdown")
	}
}
