package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"trove.local/phase0/internal/platformfs"
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

// The persistent lock pathname is never removed: every writer locks the same
// inode across atomic preference replacements. OS ownership is released on exit.
const workspaceSelectionLockTimeout = 5 * time.Second

func withWorkspaceSelectionLock(ctx context.Context, preference string, write func() error) (err error) {
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithTimeout(ctx, workspaceSelectionLockTimeout)
	defer cancel()
	if err = ctx.Err(); err != nil {
		return err
	}
	if err = os.MkdirAll(filepath.Dir(preference), 0700); err != nil {
		return err
	}
	for {
		if err = ctx.Err(); err != nil {
			return fmt.Errorf("workspace selection is busy: %w", err)
		}
		lock, lockErr := platformfs.AcquireLock(preference + ".lock")
		if lockErr == nil {
			defer func() { err = errors.Join(err, platformfs.ReleaseLock(lock)) }()
			if err = ctx.Err(); err != nil {
				return err
			}
			return write()
		}
		if !errors.Is(lockErr, platformfs.ErrLocked) {
			return fmt.Errorf("could not lock workspace selection: %w", lockErr)
		}
		timer := time.NewTimer(20 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return fmt.Errorf("workspace selection is busy: %w", ctx.Err())
		case <-timer.C:
		}
	}
}

func workspaceSelectionBytes(path string) ([]byte, error) {
	path, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	return json.Marshal(workspaceSelection{Path: path})
}

func saveWorkspaceSelection(preference, path string) error {
	return saveWorkspaceSelectionContext(context.Background(), preference, path)
}

func saveWorkspaceSelectionContext(ctx context.Context, preference, path string) error {
	raw, err := workspaceSelectionBytes(path)
	if err != nil {
		return err
	}
	return withWorkspaceSelectionLock(ctx, preference, func() error {
		return writeWorkspaceSelectionLocked(preference, raw)
	})
}

// Caller holds the preference lock across any preceding comparison/read.
func writeWorkspaceSelectionLocked(preference string, raw []byte) error {
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
	return syncWorkspaceSelectionDirectory(preference)
}

func syncWorkspaceSelectionDirectory(preference string) error {
	dir, err := os.Open(filepath.Dir(preference))
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}
