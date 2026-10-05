//go:build darwin

package updates

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
	"time"
	"trove.local/phase0/internal/workspace"
)

// This explicit development qualification harness is never linked into Trove.
// Production MacVerifier continues rejecting ad-hoc application signatures.
func TestRollbackNativeDevelopmentChild(t *testing.T) {
	bundle := os.Getenv("TROVE_TEST_NATIVE_ROLLBACK_APP")
	if bundle == "" {
		t.Skip("requires an explicitly built development app; no production signature bypass")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	if _, err := (MacVerifier{}).Inspect(ctx, bundle); err == nil {
		t.Fatal("development fixture unexpectedly passed production verification")
	}
	home := t.TempDir()
	path, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	path = filepath.Join(path, "restored")
	k, err := workspace.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	id := k.ID()
	if err = k.Close(); err != nil {
		t.Fatal(err)
	}
	format := workspace.CurrentCompatibility()
	target := RollbackTarget{WorkspacePath: path, WorkspaceID: id, UserVersion: format.UserVersion, StorageGeneration: format.StorageGeneration}
	cmd := exec.Command(filepath.Join(bundle, "Contents", "MacOS", "Trove"))
	cmd.Dir = "/"
	cmd.Env = []string{"PATH=/usr/bin:/bin:/usr/sbin:/sbin", "HOME=" + home, "TMPDIR=" + home, "LANG=en_US.UTF-8", "TROVE_WORKSPACE=" + path}
	started := time.Now()
	if err = launchRollbackProcess(ctx, cmd, target); err != nil {
		t.Fatal(err)
	}
	defer cmd.Process.Kill()
	t.Logf("Actual Wails child PID %d acknowledged rendered workspace and committed initialization after %s", cmd.Process.Pid, time.Since(started))
	if err = cmd.Process.Signal(syscall.Signal(0)); err != nil {
		t.Fatal("child exited after committed acknowledgement", err)
	}
	if reopened, openErr := workspace.OpenCompatibleReviewed(ctx, path, id, format); openErr == nil {
		reopened.Close()
		t.Fatal("ready child did not own reviewed workspace")
	}
	_ = cmd.Process.Kill()
	// Retain no running development application or fixture workspace. Wait uses
	// the launcher's sole Wait goroutine; ownership acquisition proves release.
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		k, err = workspace.OpenCompatibleReviewed(ctx, path, id, format)
		if err == nil {
			k.Close()
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("native child workspace owner was not released after test cleanup")
}
