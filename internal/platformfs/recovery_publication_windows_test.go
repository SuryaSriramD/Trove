//go:build windows

package platformfs

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestWindowsRecoveryPublicGatesDoNotReserveOrModify(t *testing.T) {
	parent := publicationDirectory(t)
	dest := filepath.Join(parent, "new-workspace")
	payload := []byte("verified native staging fixture")
	plan := RecoveryPlan{Version: 1, OperationID: "0123456789abcdef0123456789abcdef", Kind: "restore", SourceID: "native-test-source", SourceSHA256: recoveryHash(payload), Objects: []RecoveryObject{{Path: "workspace.db", SHA256: recoveryHash(payload), SizeBytes: int64(len(payload))}}}
	if _, err := BeginRecoveryPublication(context.Background(), dest, plan); !errors.Is(err, ErrDirectoryDurability) {
		t.Fatal(err)
	}
	if _, err := os.Stat(dest); !os.IsNotExist(err) {
		t.Fatal("disabled recovery reserved destination", err)
	}
	if err := RecoverPublication(context.Background(), dest, plan.OperationID); !errors.Is(err, ErrDirectoryDurability) {
		t.Fatal(err)
	}
	if err := RelocateRecoveryPublication(context.Background(), dest, dest+"-other", plan.OperationID); !errors.Is(err, ErrDirectoryDurability) {
		t.Fatal(err)
	}
	if err := ActivateRecoveryAdmission(dest, plan.OperationID, nil); !errors.Is(err, ErrDirectoryDurability) {
		t.Fatal(err)
	}
}

// Opt-in target execution exercises ACL-backed staging and exact bytes on local
// NTFS. It deliberately expects directory commit to fail: this is foundation
// evidence, not the required power-loss/crash-order qualification.
func TestWindowsRecoveryNativePrivateStagingRemainsUnqualified(t *testing.T) {
	if os.Getenv("TROVE_WINDOWS_RECOVERY_NATIVE") != "1" {
		t.Skip("requires Windows 11 x64 fixed local NTFS target; set TROVE_WINDOWS_RECOVERY_NATIVE=1")
	}
	parent := publicationDirectory(t)
	dest := filepath.Join(parent, "native-stage")
	payload := []byte("NTFS staged bytes\x00\xff")
	plan := RecoveryPlan{Version: 1, OperationID: "0123456789abcdef0123456789abcdef", Kind: "restore", SourceID: "native-test-source", SourceSHA256: recoveryHash(payload), Objects: []RecoveryObject{{Path: "workspace.db", SHA256: recoveryHash(payload), SizeBytes: int64(len(payload))}}}
	p, err := beginRecoveryPublication(context.Background(), dest, plan, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	if err = p.Stage(context.Background(), "workspace.db", bytes.NewReader(payload)); err != nil {
		t.Fatal(err)
	}
	if err = p.Verify(context.Background()); err != nil {
		t.Fatal(err)
	}
	if other, err := openRecoveryPublication(dest); err == nil {
		other.Close()
		t.Fatal("second owner admitted")
	}
	if err = p.commit(context.Background()); !errors.Is(err, ErrDirectoryDurability) {
		t.Fatal("unqualified directory ordering became success", err)
	}
	if _, err = os.Stat(filepath.Join(dest, recoveryComplete)); !os.IsNotExist(err) {
		t.Fatal("unqualified commit wrote completion", err)
	}
	if _, err = InspectRecoveryAdmission(dest, plan.OperationID); !errors.Is(err, ErrRecoveryIncomplete) {
		t.Fatal("incomplete native publication admitted", err)
	}
}
