package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
)

// Capture and replace under the same cross-process lock, so a failed handoff
// restores precisely the selection that it replaced, never an earlier read.
func selectWorkspaceForRollback(ctx context.Context, path, destination string) (selected, previous []byte, existed bool, err error) {
	wanted, err := workspaceSelectionBytes(destination)
	if err != nil {
		return
	}
	err = withWorkspaceSelectionLock(ctx, path, func() error {
		var readErr error
		previous, existed, readErr = readRollbackPreference(path)
		if readErr != nil {
			return readErr
		}
		selected = wanted
		return writeWorkspaceSelectionLocked(path, selected)
	})
	return
}

func restoreRollbackPreference(path string, selected, previous []byte, existed bool) error {
	return restoreRollbackPreferenceContext(context.Background(), path, selected, previous, existed)
}

func restoreRollbackPreferenceContext(ctx context.Context, path string, selected, previous []byte, existed bool) error {
	return withWorkspaceSelectionLock(ctx, path, func() error {
		current, present, err := readRollbackPreference(path)
		if err != nil {
			return fmt.Errorf("could not restore previous workspace selection: %w", err)
		}
		if present == existed && bytes.Equal(current, previous) {
			return nil
		}
		if !present || !bytes.Equal(current, selected) {
			return errors.New("workspace selection changed during failed launch; its newer selection was preserved")
		}
		if existed {
			return writeWorkspaceSelectionLocked(path, previous)
		}
		if err = os.Remove(path); err != nil {
			return err
		}
		return syncWorkspaceSelectionDirectory(path)
	})
}
