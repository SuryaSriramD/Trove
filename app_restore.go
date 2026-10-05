package main

import (
	"encoding/json"
	"fmt"
	"github.com/wailsapp/wails/v2/pkg/runtime"
	"path/filepath"
	"sync"
	"time"
	"trove.local/phase0/internal/workspace"
)

type nativeRestorePreview struct {
	source   string
	manifest workspace.BackupManifest
	cleanup  *workspace.BackupCleanupPlan
	expires  time.Time
}
type nativeRestoreState struct {
	mu       sync.Mutex
	previews map[string]nativeRestorePreview
}

var nativeRestoreStates sync.Map

func (a *App) restoreState() *nativeRestoreState {
	state, _ := nativeRestoreStates.LoadOrStore(a, &nativeRestoreState{previews: map[string]nativeRestorePreview{}})
	return state.(*nativeRestoreState)
}
func (a *App) saveRestorePreview(p nativeRestorePreview) string {
	s := a.restoreState()
	s.mu.Lock()
	defer s.mu.Unlock()
	for id, old := range s.previews {
		if time.Now().After(old.expires) {
			delete(s.previews, id)
		}
	}
	id := workspace.NewID()
	p.expires = time.Now().Add(10 * time.Minute)
	s.previews[id] = p
	return id
}
func (a *App) takeRestorePreview(id string) (nativeRestorePreview, error) {
	s := a.restoreState()
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.previews[id]
	if !ok || !p.expires.After(time.Now()) {
		return p, fmt.Errorf("review expired; choose the backup again")
	}
	delete(s.previews, id)
	return p, nil
}

// Only native dialogs supply filesystem locations; the renderer receives a
// short-lived, one-use review token and cannot request arbitrary restore paths.
func (a *App) PreviewNativeRestore() (string, error) {
	if err := a.requireRollbackCommitted(); err != nil {
		return "", err
	}

	source, err := runtime.OpenDirectoryDialog(a.ctx, runtime.OpenDialogOptions{Title: "Choose a completed Trove backup"})
	if err != nil || source == "" {
		return "", err
	}
	manifest, err := workspace.VerifyBackup(a.ctx, source)
	if err != nil {
		return "", err
	}
	id := a.saveRestorePreview(nativeRestorePreview{source: source, manifest: manifest})
	warnings := []string{"Restores into a new folder. Your open workspace remains unchanged.", "All retained history, import evidence, managed assets and declared recording journals are included. Search is rebuilt.", "Pending approvals and old file/restore handles are revoked. Approved memory states are preserved."}
	if manifest.FormatVersion == 1 {
		warnings = append(warnings, "This legacy backup did not inventory recording drafts. Only its declared data can be restored.")
	}
	raw, err := json.Marshal(map[string]any{"previewId": id, "manifest": manifest, "sourceName": filepath.Base(source), "warnings": warnings})
	return string(raw), err
}
func (a *App) RestoreNativeBackup(previewID string) (string, error) {
	if err := a.requireRollbackCommitted(); err != nil {
		return "", err
	}

	p, err := a.takeRestorePreview(previewID)
	if err != nil {
		return "", err
	}
	if p.cleanup != nil {
		return "", fmt.Errorf("invalid restore review")
	}
	parent, err := runtime.OpenDirectoryDialog(a.ctx, runtime.OpenDialogOptions{Title: "Choose a destination for the restored workspace", CanCreateDirectories: true})
	if err != nil || parent == "" {
		return "", err
	}
	destination := filepath.Join(parent, "Trove-restored-"+workspace.NewID())
	result, err := workspace.RestoreBackupReviewed(a.ctx, p.source, destination, p.manifest)
	if err != nil {
		return "", err
	}
	raw, err := json.Marshal(result)
	return string(raw), err
}
func (a *App) PreviewBackupCleanup() (string, error) {
	if err := a.requireRollbackCommitted(); err != nil {
		return "", err
	}

	parent, err := runtime.OpenDirectoryDialog(a.ctx, runtime.OpenDialogOptions{Title: "Choose your external backup folder"})
	if err != nil || parent == "" {
		return "", err
	}
	a.mu.RLock()
	defer a.mu.RUnlock()
	if a.kernel == nil {
		return "", fmt.Errorf("workspace is not open")
	}
	plan, err := a.kernel.PreviewBackupCleanup(a.ctx, a.execution, parent)
	if err != nil {
		return "", err
	}
	id := a.saveRestorePreview(nativeRestorePreview{cleanup: &plan})
	raw, err := json.Marshal(map[string]any{"previewId": id, "plan": plan})
	return string(raw), err
}
func (a *App) ApplyBackupCleanup(previewID string) (int, error) {
	if err := a.requireRollbackCommitted(); err != nil {
		return 0, err
	}

	p, err := a.takeRestorePreview(previewID)
	if err != nil {
		return 0, err
	}
	if p.cleanup == nil {
		return 0, fmt.Errorf("invalid cleanup review")
	}
	a.mu.RLock()
	defer a.mu.RUnlock()
	if a.kernel == nil {
		return 0, fmt.Errorf("workspace is not open")
	}
	return a.kernel.ApplyBackupCleanup(a.ctx, a.execution, *p.cleanup)
}
