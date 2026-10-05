package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"trove.local/phase0/internal/workspace"
)

func supportTestApp(t *testing.T) *App {
	t.Helper()
	k, err := workspace.Create(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	a := NewApp()
	a.ctx = context.Background()
	a.kernel = k
	a.execution = k.OwnerContext()
	t.Cleanup(func() { _ = a.CancelSupportBundle(); supportStates.Delete(a); k.Close() })
	return a
}
func supportTestPreview(t *testing.T, a *App) string {
	t.Helper()
	raw, _ := json.Marshal(map[string]any{"workspaceId": a.kernel.ID(), "selection": map[string]any{"titles": false, "text": false, "pageIds": []string{}}})
	result, err := a.PreviewSupportBundle(string(raw))
	if err != nil {
		t.Fatal(err)
	}
	var response struct {
		PreviewID string `json:"previewId"`
	}
	if err = json.Unmarshal([]byte(result), &response); err != nil {
		t.Fatal(err)
	}
	return response.PreviewID
}
func TestDesktopSupportNativeDestinationOpaqueSingleUseAndCurrentWorkspace(t *testing.T) {
	a := supportTestApp(t)
	original := supportSaveDialog
	t.Cleanup(func() { supportSaveDialog = original })
	calls := 0
	path := filepath.Join(t.TempDir(), "support.zip")
	supportSaveDialog = func(context.Context) (string, error) { calls++; return path, nil }
	id := supportTestPreview(t, a)
	if result, err := a.ExportSupportBundle(id); err != nil || !strings.Contains(result, "Nothing was uploaded") {
		t.Fatal(result, err)
	}
	if _, err := a.ExportSupportBundle(id); err == nil {
		t.Fatal("replayed consumed review")
	}
	if calls != 1 {
		t.Fatal("replayed dialog")
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal(err)
	}
	id = supportTestPreview(t, a)
	other, err := workspace.Create(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	old := a.kernel
	a.kernel = other
	a.execution = other.OwnerContext()
	if _, err = a.ExportSupportBundle(id); err == nil {
		t.Fatal("workspace switch accepted")
	}
	if calls != 1 {
		t.Fatal("dialog before workspace check")
	}
	a.kernel = old
	a.execution = old.OwnerContext()
	for _, raw := range []string{`{"destination":"/untrusted/path"}`, `{"workspaceId":"other","selection":{}}`, `{} {}`, strings.Repeat("x", 4097)} {
		if _, err = a.PreviewSupportBundle(raw); err == nil {
			t.Fatal("forged selection accepted")
		}
	}
}
func TestDesktopSupportCancellationDuringNativeDialogPreventsPublication(t *testing.T) {
	a := supportTestApp(t)
	original := supportSaveDialog
	t.Cleanup(func() { supportSaveDialog = original })
	path := filepath.Join(t.TempDir(), "cancel.zip")
	entered, release := make(chan struct{}), make(chan struct{})
	supportSaveDialog = func(context.Context) (string, error) { close(entered); <-release; return path, nil }
	id := supportTestPreview(t, a)
	done := make(chan error, 1)
	go func() { _, err := a.ExportSupportBundle(id); done <- err }()
	<-entered
	if err := a.CancelSupportBundle(); err != nil {
		t.Fatal(err)
	}
	close(release)
	if err := <-done; err == nil {
		t.Fatal("cancel accepted save")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("cancel published", err)
	}
	if _, err := a.ExportSupportBundle(id); err == nil {
		t.Fatal("cancelled preview reused")
	}
}

func TestDesktopSupportRollbackPendingRejectsBeforeStateOrDialog(t *testing.T) {
	a := NewApp()
	a.rollback = &rollbackChildState{}
	a.rollback.pending.Store(true)
	if _, err := a.PreviewSupportBundle(`{}`); err == nil {
		t.Fatal("preview bypassed rollback")
	}
	if _, err := a.ExportSupportBundle("untrusted"); err == nil {
		t.Fatal("export bypassed rollback")
	}
	if err := a.CancelSupportBundle(); err == nil {
		t.Fatal("cancel bypassed rollback")
	}
	if _, ok := supportStates.Load(a); ok {
		t.Fatal("pending child consumed support state")
	}
}
