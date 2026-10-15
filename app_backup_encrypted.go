package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/wailsapp/wails/v2/pkg/runtime"
	"trove.local/phase0/internal/workspace"
)

type backupOperationState struct {
	mu       sync.Mutex
	cancel   context.CancelFunc
	progress workspace.BackupProgress
}

var backupOperationStates sync.Map

func (a *App) backupOperationState() *backupOperationState {
	s, _ := backupOperationStates.LoadOrStore(a, &backupOperationState{progress: workspace.BackupProgress{Version: 1, Phase: "idle", Bytes: "0", TotalBytes: "0"}})
	return s.(*backupOperationState)
}
func (a *App) beginBackupOperation() (context.Context, func(), error) {
	if err := a.requireRollbackCommitted(); err != nil {
		return nil, nil, err
	}
	select {
	case <-a.shutdownDone:
		return nil, nil, errors.New("application is shutting down")
	default:
	}
	a.mu.RLock()
	base := a.ctx
	a.mu.RUnlock()
	if base == nil {
		base = context.Background()
	}
	s := a.backupOperationState()
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cancel != nil {
		return nil, nil, errors.New("another backup operation is running")
	}
	ctx, cancel := context.WithCancel(base)
	s.cancel = cancel
	s.progress = workspace.BackupProgress{Version: 1, Phase: "choosing", Bytes: "0", TotalBytes: "0"}
	return ctx, func() { cancel(); s.mu.Lock(); s.cancel = nil; s.progress.Phase = "idle"; s.mu.Unlock() }, nil
}
func (a *App) backupProgress(p workspace.BackupProgress) {
	s := a.backupOperationState()
	s.mu.Lock()
	s.progress = p
	s.mu.Unlock()
}
func (a *App) NativeBackupProgress() (string, error) {
	if err := a.requireRollbackCommitted(); err != nil {
		return "", err
	}
	s := a.backupOperationState()
	s.mu.Lock()
	defer s.mu.Unlock()
	b, err := json.Marshal(s.progress)
	return string(b), err
}
func (a *App) CancelBackupOperation() {
	if err := a.requireRollbackCommitted(); err != nil {
		return
	}
	a.cancelBackupOperation()
}
func (a *App) cancelBackupOperation() {
	s := a.backupOperationState()
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cancel != nil {
		s.cancel()
	}
}
func backupStagingDirectory() (string, error) {
	base, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "Trove", "portable-backup-staging"), nil
}
func (a *App) cleanupAbandonedBackupStages() {
	if path, err := backupStagingDirectory(); err == nil {
		_, _ = workspace.CleanupAbandonedBackupStages(path)
	}
}

// Dedicated native methods keep passphrases out of Dispatch, job parameters,
// command/history logs, telemetry and model context. They are never retained in
// operation or preview state. The renderer clears its password input on submit.
func (a *App) CreateEncryptedBackup(passphrase string) (string, error) {
	if err := a.requireRollbackCommitted(); err != nil {
		return "", err
	}
	ctx, done, err := a.beginBackupOperation()
	if err != nil {
		return "", err
	}
	defer done()
	if passphrase == "" {
		return "", errors.New("enter a backup passphrase")
	}
	parent, err := runtime.OpenDirectoryDialog(a.ctx, runtime.OpenDialogOptions{Title: "Save an encrypted Trove backup", CanCreateDirectories: true})
	if err != nil || parent == "" {
		return "", err
	}
	stage, err := backupStagingDirectory()
	if err != nil {
		return "", err
	}
	a.mu.RLock()
	defer a.mu.RUnlock()
	if a.kernel == nil {
		return "", fmt.Errorf("workspace is not open")
	}
	destination := filepath.Join(parent, "Trove-backup-"+workspace.NewID()+".trove.age")
	manifest, err := a.kernel.CreateEncryptedBackup(ctx, a.execution, destination, passphrase, stage, a.backupProgress)
	passphrase = ""
	if err != nil {
		return "", err
	}
	raw, err := json.Marshal(map[string]any{"version": 1, "manifest": manifest, "summary": workspace.PortableBackupSummary(), "destination": destination})
	return string(raw), err
}
func (a *App) PreviewEncryptedBackup(passphrase string) (string, error) {
	if err := a.requireRollbackCommitted(); err != nil {
		return "", err
	}
	ctx, done, err := a.beginBackupOperation()
	if err != nil {
		return "", err
	}
	defer done()
	if passphrase == "" {
		return "", errors.New("enter the backup passphrase")
	}
	source, err := runtime.OpenFileDialog(a.ctx, runtime.OpenDialogOptions{Title: "Choose an encrypted Trove backup", Filters: []runtime.FileFilter{{DisplayName: "Encrypted Trove backup", Pattern: "*.age"}}})
	if err != nil || source == "" {
		return "", err
	}
	stage, err := backupStagingDirectory()
	if err != nil {
		return "", err
	}
	verified, err := workspace.OpenEncryptedBackup(ctx, source, passphrase, stage, a.backupProgress)
	passphrase = ""
	if err != nil {
		return "", err
	}
	if err = ctx.Err(); err != nil {
		verified.Close()
		return "", err
	}
	id := a.saveRestorePreview(nativeRestorePreview{manifest: verified.Manifest, encrypted: verified})
	if id == "" {
		return "", errors.New("application is shutting down")
	}
	raw, err := json.Marshal(map[string]any{"version": 1, "previewId": id, "sourceName": filepath.Base(source), "manifest": verified.Manifest, "summary": workspace.PortableBackupSummary(), "warnings": []string{"The complete archive was authenticated before its contents were read.", "Restores the reviewed snapshot into a new folder; your open workspace remains unchanged.", "Pending approvals and local file access are revoked. Search is rebuilt.", restoredMemoryWarning, "This private preview expires after ten minutes; cancel it to remove staged plaintext immediately."}})
	return string(raw), err
}
func (a *App) CloseNativeRestorePreview(id string) {
	if err := a.requireRollbackCommitted(); err != nil {
		return
	}
	s := a.restoreState()
	s.mu.Lock()
	p, ok := s.previews[id]
	if ok {
		delete(s.previews, id)
	}
	s.mu.Unlock()
	if ok {
		p.encrypted.Close()
	}
}
func (a *App) closeBackupPreviews() {
	s := a.restoreState()
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closed = true
	for id, p := range s.previews {
		p.encrypted.Close()
		delete(s.previews, id)
	}
}
