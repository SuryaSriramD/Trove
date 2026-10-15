package main

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"slices"
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

// Exercise the exact native bridge response builders used after verification.
// This does not exercise OS dialogs; encrypted authentication is tested separately.
func TestNativeRestorePreviewsDiscloseOlderBackupMemory(t *testing.T) {
	const warning = "Restoring an older backup can recover memory state from before a later Forget. Review restored memory before using AI."
	for _, tc := range []struct {
		name    string
		version int
		build   func(string, string, workspace.BackupManifest) (string, error)
	}{
		{"legacy directory", 1, nativeRestorePreviewResponse},
		{"native directory", 2, nativeRestorePreviewResponse},
		{"encrypted archive", 2, encryptedRestorePreviewResponse},
	} {
		t.Run(tc.name, func(t *testing.T) {
			manifest := workspace.BackupManifest{FormatVersion: tc.version, BackupID: "reviewed-backup"}
			source := filepath.Join(t.TempDir(), "reviewed-backup")
			raw, err := tc.build("review-token", source, manifest)
			if err != nil {
				t.Fatal(err)
			}
			var preview struct {
				PreviewID  string                   `json:"previewId"`
				SourceName string                   `json:"sourceName"`
				Manifest   workspace.BackupManifest `json:"manifest"`
				Warnings   []string                 `json:"warnings"`
			}
			if err := json.Unmarshal([]byte(raw), &preview); err != nil {
				t.Fatal(err)
			}
			if !slices.Contains(preview.Warnings, warning) {
				t.Fatalf("memory recovery warning missing from %s response: %+v", tc.name, preview.Warnings)
			}
			if preview.PreviewID != "review-token" || preview.SourceName != "reviewed-backup" || preview.Manifest.BackupID != manifest.BackupID || preview.Manifest.FormatVersion != tc.version {
				t.Fatalf("preview identity changed: %+v", preview)
			}
		})
	}
}
