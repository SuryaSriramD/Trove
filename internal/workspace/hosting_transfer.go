package workspace

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"trove.local/phase0/internal/platformfs"
)

// TransferWorkspace requires the old owner to be stopped. It holds the source
// kernel lock while verifying a full snapshot, fences the old path durably, then
// publishes the destination. No merge or synchronization takes place.
func TransferWorkspace(ctx context.Context, source, destination string) (RestoreResult, error) {
	return transferWorkspaceWithFence(ctx, source, destination, durableRecoveryFile)
}
func transferWorkspaceWithFence(ctx context.Context, source, destination string, writeFence func(string, []byte) error) (result RestoreResult, err error) {
	if err = platformfs.RequireRecoveryDurability(); err != nil {
		return result, err
	}
	source, err = filepath.EvalSymlinks(source)
	if err != nil {
		return result, err
	}
	source, err = filepath.Abs(source)
	if err != nil {
		return result, err
	}
	destination, err = filepath.Abs(destination)
	if err != nil {
		return result, err
	}
	parent, err := filepath.EvalSymlinks(filepath.Dir(destination))
	if err != nil {
		return result, err
	}
	destination = filepath.Join(parent, filepath.Base(destination))
	if source == destination {
		return result, Fail("INVALID_REQUEST", "Transfer needs a new workspace directory")
	}
	if rel, e := filepath.Rel(source, destination); e != nil || rel == "." || !(rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator))) {
		return result, Fail("INVALID_REQUEST", "Destination must be outside the source workspace")
	}
	if _, e := os.Lstat(destination); !os.IsNotExist(e) {
		return result, Fail("CONFLICT", "Transfer destination must not exist")
	}
	k, err := Open(source)
	if err != nil {
		return result, err
	}
	defer k.Close()
	stage, err := os.MkdirTemp(filepath.Dir(destination), ".trove-transfer-")
	if err != nil {
		return result, err
	}
	fenced := false
	defer func() {
		if !fenced || err == nil {
			os.RemoveAll(stage)
		}
	}()
	backup := filepath.Join(stage, "snapshot")
	manifest, err := k.CreateBackup(ctx, k.OwnerContext(), backup)
	if err != nil {
		return result, err
	}
	if _, err = VerifyBackup(ctx, backup); err != nil {
		return result, err
	}
	restored := filepath.Join(stage, "workspace")
	receipt := map[string]any{"schemaVersion": 1, "workspaceId": k.ID(), "backupId": manifest.BackupID, "databaseSHA256": manifest.Database.SHA256, "eventCursor": manifest.EventCursor, "destination": destination, "recoveryStage": stage, "transferredAt": now()}
	result, err = restoreBackupVerifiedExtra(ctx, backup, restored, manifest, marshal(receipt))
	if err != nil {
		return result, err
	}
	if err = ctx.Err(); err != nil {
		return result, err
	}
	if err = writeFence(filepath.Join(source, "OWNER_TRANSFERRED"), marshal(receipt)); err != nil {
		// Creation may have succeeded before a write/fsync/close failure. Any
		// possibly published fence must retain the verified recovery snapshot.
		_, statErr := os.Lstat(filepath.Join(source, "OWNER_TRANSFERRED"))
		fenced = !os.IsNotExist(statErr)
		if fenced {
			return result, fmt.Errorf("ownership fence may be present; recover verified stage %s: %w", stage, err)
		}
		return result, err
	}
	fenced = true
	if err = syncRecoveryDirectory(source); err != nil {
		return result, err
	}
	// Source can no longer open even after process death. Publish only after that
	// fence is durable; failure retains the verified staging directory for recovery.
	operationID, err := readRecoveryOperation(ctx, restored)
	if err != nil {
		return result, err
	}
	if err = platformfs.RelocateRecoveryPublication(ctx, restored, destination, operationID); err != nil {
		return result, fmt.Errorf("source fenced; recover verified stage %s: %w", stage, err)
	}
	result.Destination = destination
	return result, nil
}
