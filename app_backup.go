package main

import (
	"encoding/json"
	"fmt"
	"github.com/wailsapp/wails/v2/pkg/runtime"
	"path/filepath"
	"trove.local/phase0/internal/workspace"
)

// CreateBackup accepts only a native-selected directory and always creates a
// new destination. Renderer input cannot select arbitrary filesystem paths.
func (a *App) CreateBackup() (string, error) {
	if err := a.requireRollbackCommitted(); err != nil {
		return "", err
	}

	parent, err := runtime.OpenDirectoryDialog(a.ctx, runtime.OpenDialogOptions{Title: "Save a Trove backup", CanCreateDirectories: true})
	if err != nil || parent == "" {
		return "", err
	}
	a.mu.RLock()
	defer a.mu.RUnlock()
	if a.kernel == nil {
		return "", fmt.Errorf("workspace is not open")
	}
	destination := filepath.Join(parent, "Trove-backup-"+workspace.NewID())
	manifest, err := a.kernel.CreateBackup(a.ctx, a.execution, destination)
	if err != nil {
		return "", err
	}
	raw, err := json.Marshal(manifest)
	return string(raw), err
}
