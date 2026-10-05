package main

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"trove.local/phase0/internal/platformfs"
)

func TestWorkspaceSelectionWriterProcess(t *testing.T) {
	path := os.Getenv("TROVE_TEST_SELECTION_PATH")
	if path == "" {
		return
	}
	fmt.Println("attempting selection")
	if err := saveWorkspaceSelection(path, os.Getenv("TROVE_TEST_SELECTION_DESTINATION")); err != nil {
		t.Fatal(err)
	}
}

func TestWorkspaceSelectionLockSerializesASeparateProcess(t *testing.T) {
	preference := filepath.Join(t.TempDir(), "selection.json")
	old, _ := workspaceSelectionBytes(filepath.Join(t.TempDir(), "old"))
	if err := os.WriteFile(preference, old, 0600); err != nil {
		t.Fatal(err)
	}
	lock, err := platformfs.AcquireLock(preference + ".lock")
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if lock != nil {
			_ = platformfs.ReleaseLock(lock)
		}
	}()
	destination := filepath.Join(t.TempDir(), "new")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestWorkspaceSelectionWriterProcess$")
	cmd.Env = append(os.Environ(), "TROVE_TEST_SELECTION_PATH="+preference, "TROVE_TEST_SELECTION_DESTINATION="+destination)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err = cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = cmd.Process.Kill(); _ = cmd.Wait() }()
	line, err := bufio.NewReader(stdout).ReadString('\n')
	if err != nil || line != "attempting selection\n" {
		t.Fatalf("writer readiness: %q %v", line, err)
	}
	// The separate process has entered the writer. It cannot replace the selected
	// path while another descriptor owns the lock, including during a CAS read.
	blocked, stop := context.WithTimeout(context.Background(), 80*time.Millisecond)
	defer stop()
	if err = saveWorkspaceSelectionContext(blocked, preference, destination); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("lock deadline: %v", err)
	}
	raw, err := os.ReadFile(preference)
	if err != nil || !bytes.Equal(raw, old) {
		t.Fatalf("writer bypassed lock: %q %v", raw, err)
	}
	if err = platformfs.ReleaseLock(lock); err != nil {
		t.Fatal(err)
	}
	lock = nil
	if err = cmd.Wait(); err != nil {
		t.Fatalf("writer failed: %v %s", err, stderr.String())
	}
	expected, _ := workspaceSelectionBytes(destination)
	raw, err = os.ReadFile(preference)
	if err != nil || !bytes.Equal(raw, expected) {
		t.Fatalf("writer did not resume: %q %v", raw, err)
	}
	if _, err = os.Stat(preference + ".lock"); err != nil {
		t.Fatal("persistent lock pathname removed", err)
	}
}

func TestRollbackPreferenceCaptureRestoreAndChangedSelection(t *testing.T) {
	for _, existed := range []bool{false, true} {
		t.Run(fmt.Sprint(existed), func(t *testing.T) {
			preference := filepath.Join(t.TempDir(), "settings", "selection.json")
			original := filepath.Join(t.TempDir(), "original")
			if existed {
				if err := saveWorkspaceSelection(preference, original); err != nil {
					t.Fatal(err)
				}
			}
			selected, previous, present, err := selectWorkspaceForRollback(context.Background(), preference, filepath.Join(t.TempDir(), "restored"))
			if err != nil || present != existed {
				t.Fatalf("capture: %v %v", present, err)
			}
			if existed {
				want, _ := workspaceSelectionBytes(original)
				if !bytes.Equal(previous, want) {
					t.Fatal("incorrect prior selection captured")
				}
			}
			if err = restoreRollbackPreference(preference, selected, previous, present); err != nil {
				t.Fatal(err)
			}
			got, nowPresent, err := readRollbackPreference(preference)
			if err != nil || nowPresent != existed || !bytes.Equal(got, previous) {
				t.Fatalf("restore: %q %v %v", got, nowPresent, err)
			}
			// Restoration is idempotent, including when there never was a preference.
			if err = restoreRollbackPreference(preference, selected, previous, present); err != nil {
				t.Fatal(err)
			}
			selected, previous, present, err = selectWorkspaceForRollback(context.Background(), preference, filepath.Join(t.TempDir(), "restored-again"))
			if err != nil {
				t.Fatal(err)
			}
			newer := filepath.Join(t.TempDir(), "newer")
			if err = saveWorkspaceSelection(preference, newer); err != nil {
				t.Fatal(err)
			}
			if err = restoreRollbackPreference(preference, selected, previous, present); err == nil {
				t.Fatal("changed selection was not reported")
			}
			want, _ := workspaceSelectionBytes(newer)
			got, err = os.ReadFile(preference)
			if err != nil || !bytes.Equal(got, want) {
				t.Fatal("newer selection lost", err)
			}
		})
	}
}

func TestRollbackPreferenceCancellationCannotReadOrReplaceUnderAnotherWriter(t *testing.T) {
	preference := filepath.Join(t.TempDir(), "selection.json")
	selected, previous, present, err := selectWorkspaceForRollback(context.Background(), preference, filepath.Join(t.TempDir(), "restored"))
	if err != nil {
		t.Fatal(err)
	}
	lock, err := platformfs.AcquireLock(preference + ".lock")
	if err != nil {
		t.Fatal(err)
	}
	defer platformfs.ReleaseLock(lock)
	for _, operation := range []string{"capture", "restore"} {
		t.Run(operation, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
			defer cancel()
			if operation == "capture" {
				_, _, _, err = selectWorkspaceForRollback(ctx, preference, filepath.Join(t.TempDir(), "next"))
			} else {
				err = restoreRollbackPreferenceContext(ctx, preference, selected, previous, present)
			}
			if !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("bounded lock cancellation: %v", err)
			}
			got, readErr := os.ReadFile(preference)
			if readErr != nil || !bytes.Equal(got, selected) {
				t.Fatal("cancelled operation changed selection", readErr)
			}
		})
	}
}

func TestRollbackPreferenceConcurrentNewSelectionAlwaysWins(t *testing.T) {
	for i := 0; i < 8; i++ {
		preference := filepath.Join(t.TempDir(), "selection.json")
		if err := saveWorkspaceSelection(preference, filepath.Join(t.TempDir(), "old")); err != nil {
			t.Fatal(err)
		}
		selected, previous, present, err := selectWorkspaceForRollback(context.Background(), preference, filepath.Join(t.TempDir(), "restored"))
		if err != nil {
			t.Fatal(err)
		}
		newer := filepath.Join(t.TempDir(), "newer")
		start := make(chan struct{})
		saved := make(chan error, 1)
		restored := make(chan error, 1)
		go func() { <-start; saved <- saveWorkspaceSelection(preference, newer) }()
		go func() { <-start; restored <- restoreRollbackPreference(preference, selected, previous, present) }()
		close(start)
		if err = <-saved; err != nil {
			t.Fatal(err)
		}
		<-restored // Either restored before the new write, or preserved its newer value.
		got, err := os.ReadFile(preference)
		want, _ := workspaceSelectionBytes(newer)
		if err != nil || !bytes.Equal(got, want) {
			t.Fatalf("concurrent new selection lost: %q %v", got, err)
		}
	}
}
