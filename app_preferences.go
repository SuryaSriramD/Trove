package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

type workspaceSelection struct {
	Path string `json:"path"`
}

// Only a successful native workspace selection is persisted, never a renderer
// path. An unavailable selected workspace is surfaced instead of recreated.
func selectedWorkspace(preference string) (string, error) {
	raw, err := os.ReadFile(preference)
	if os.IsNotExist(err) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	var selected workspaceSelection
	if json.Unmarshal(raw, &selected) != nil || !filepath.IsAbs(selected.Path) {
		return "", fmt.Errorf("saved workspace preference is invalid; choose a workspace to continue")
	}
	if _, err := os.Stat(filepath.Join(selected.Path, "workspace.db")); err != nil {
		return "", fmt.Errorf("your selected workspace is unavailable; reconnect its drive or choose another workspace: %w", err)
	}
	return selected.Path, nil
}

func saveWorkspaceSelection(preference, path string) error {
	path, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(preference), 0700); err != nil {
		return err
	}
	raw, err := json.Marshal(workspaceSelection{Path: path})
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(preference), ".selection-")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err = f.Write(raw); err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	if err = os.Rename(f.Name(), preference); err != nil {
		return err
	}
	dir, err := os.Open(filepath.Dir(preference))
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}
