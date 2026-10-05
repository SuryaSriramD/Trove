//go:build darwin

package updates

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

func TestRollbackProcessChild(t *testing.T) {
	mode := os.Getenv("TROVE_TEST_ROLLBACK_CHILD")
	if mode == "" {
		return
	}
	if mode == "never-ready" {
		time.Sleep(time.Minute)
		os.Exit(9)
	}
	child, err := ReceiveRollbackChild()
	if err != nil || child == nil {
		os.Exit(10)
	}
	if mode == "exit" {
		os.Exit(11)
	}
	target := child.Target()
	if mode == "wrong-target" {
		target.WorkspaceID += "-wrong"
	}
	err = child.ConfirmReady(target, func() error {
		if mode == "init-failure" {
			return errors.New("fixture initialization failure")
		}
		if marker := os.Getenv("TROVE_TEST_ROLLBACK_MARKER"); marker != "" {
			if e := os.WriteFile(marker, []byte("parent committed"), 0600); e != nil {
				return e
			}
		}
		if mode == "late-cancel" {
			time.Sleep(250 * time.Millisecond)
		}
		return nil
	})
	if err != nil {
		os.Exit(12)
	}
	time.Sleep(time.Minute)
	os.Exit(0)
}
func TestRollbackProcessWaitsForInitializedChildAndReapsFailures(t *testing.T) {
	for _, mode := range []string{"success", "exit", "never-ready", "wrong-target", "init-failure", "late-cancel"} {
		t.Run(mode, func(t *testing.T) {
			exe, err := os.Executable()
			if err != nil {
				t.Fatal(err)
			}
			marker := filepath.Join(t.TempDir(), "committed")
			cmd := exec.Command(exe, "-test.run=^TestRollbackProcessChild$")
			cmd.Env = append(os.Environ(), "TROVE_TEST_ROLLBACK_CHILD="+mode, "TROVE_TEST_ROLLBACK_MARKER="+marker)
			timeout := 5 * time.Second
			if mode == "never-ready" {
				timeout = 250 * time.Millisecond
			}
			ctx, cancel := context.WithTimeout(context.Background(), timeout)
			defer cancel()
			if mode == "late-cancel" {
				go func() {
					for ctx.Err() == nil {
						if _, e := os.Stat(marker); e == nil {
							cancel()
							return
						}
						time.Sleep(time.Millisecond)
					}
				}()
			}
			target := RollbackTarget{WorkspacePath: t.TempDir(), WorkspaceID: "reviewed", UserVersion: 3, StorageGeneration: 1}
			err = launchRollbackProcess(ctx, cmd, target)
			success := mode == "success" || mode == "late-cancel"
			if success {
				if err != nil {
					t.Fatal(err)
				}
				if _, e := os.Stat(marker); e != nil {
					t.Fatal("returned before committed initialization", e)
				}
				if e := cmd.Process.Signal(syscall.Signal(0)); e != nil {
					t.Fatal("accepted exited child", e)
				}
				_ = cmd.Process.Kill()
			} else {
				if err == nil {
					_ = cmd.Process.Kill()
					t.Fatal("accepted failed child")
				}
				if cmd.ProcessState == nil {
					t.Fatal("failed child was not reaped")
				}
				if _, e := os.Stat(marker); !os.IsNotExist(e) {
					t.Fatal("unready child entered runtime initialization")
				}
			}
		})
	}
}
func TestRollbackLaunchCancellationBeforeSpawn(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	cmd := exec.Command("must-not-run")
	if launchRollbackProcess(ctx, cmd, RollbackTarget{WorkspacePath: t.TempDir(), WorkspaceID: "review", UserVersion: 3, StorageGeneration: 1}) == nil || cmd.Process != nil {
		t.Fatal("spawned after cancellation")
	}
}
