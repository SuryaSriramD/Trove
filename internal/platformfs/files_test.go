package platformfs

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestOwnerLockProcessHelper(t *testing.T) {
	path := os.Getenv("TROVE_PLATFORMFS_TEST_LOCK")
	if path == "" {
		return
	}
	lock, err := AcquireLock(path)
	if err != nil {
		t.Fatal(err)
	}
	defer ReleaseLock(lock)
	fmt.Println("locked")
	_, _ = io.Copy(io.Discard, os.Stdin)
}

func TestOwnerLockMutualExclusionAndProcessDeath(t *testing.T) {
	path := filepath.Join(t.TempDir(), "owner.lock")
	command := exec.Command(os.Args[0], "-test.run=^TestOwnerLockProcessHelper$")
	command.Env = append(os.Environ(), "TROVE_PLATFORMFS_TEST_LOCK="+path)
	input, err := command.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	defer input.Close()
	output, err := command.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	command.Stderr = os.Stderr
	if err = command.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = command.Process.Kill(); _ = command.Wait() }()
	ready := make(chan error, 1)
	go func() {
		line, e := bufio.NewReader(output).ReadString('\n')
		if e == nil && line != "locked\n" {
			e = fmt.Errorf("unexpected readiness: %q", line)
		}
		ready <- e
	}()
	select {
	case err = <-ready:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("owner process did not acquire lock")
	}
	if other, err := AcquireLock(path); !errors.Is(err, ErrLocked) {
		ReleaseLock(other)
		t.Fatalf("second process acquired ownership: %v", err)
	}
	if err = command.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	_ = command.Wait()
	deadline := time.Now().Add(3 * time.Second)
	for {
		replacement, e := AcquireLock(path)
		if e == nil {
			if e = ReleaseLock(replacement); e != nil {
				t.Fatal(e)
			}
			break
		}
		if !errors.Is(e, ErrLocked) || time.Now().After(deadline) {
			t.Fatalf("ownership was not released after process death: %v", e)
		}
		time.Sleep(10 * time.Millisecond)
	}
	if _, err = os.Stat(path); err != nil {
		t.Fatalf("lock pathname must remain stable: %v", err)
	}
}

func TestOwnerLockRejectsSecondDescriptorAndHardLinks(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "owner.lock")
	first, err := AcquireLock(path)
	if err != nil {
		t.Fatal(err)
	}
	if second, err := AcquireLock(path); !errors.Is(err, ErrLocked) {
		ReleaseLock(second)
		t.Fatalf("second descriptor acquired ownership: %v", err)
	}
	if err = ReleaseLock(first); err != nil {
		t.Fatal(err)
	}
	again, err := AcquireLock(path)
	if err != nil {
		t.Fatal(err)
	}
	if err = ReleaseLock(again); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(dir, "alias.lock")
	if err = os.Link(path, alias); err != nil {
		t.Skipf("filesystem cannot create hard links: %v", err)
	}
	for _, candidate := range []string{path, alias} {
		if file, err := AcquireLock(candidate); err == nil {
			ReleaseLock(file)
			t.Fatal("hardlinked ownership file accepted")
		}
	}
}

func TestRegularFileAndLockRejectSymlinksAndDirectories(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "target")
	if err := os.WriteFile(target, []byte("retained"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, candidate := range []string{dir} {
		if f, err := OpenRegular(candidate); err == nil {
			f.Close()
			t.Fatal("directory accepted as regular file")
		}
		if f, err := AcquireLock(candidate); err == nil {
			ReleaseLock(f)
			t.Fatal("directory accepted as ownership file")
		}
	}
	link := filepath.Join(dir, "link")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlink creation unavailable: %v", err)
	}
	if f, err := OpenRegular(link); err == nil {
		f.Close()
		t.Fatal("selected symlink followed")
	}
	if f, err := AcquireLock(link); err == nil {
		ReleaseLock(f)
		t.Fatal("owner symlink followed")
	}
	if got, err := os.ReadFile(target); err != nil || string(got) != "retained" {
		t.Fatalf("target modified: %q, %v", got, err)
	}
}

func TestDescriptorIdentitySurvivesPathReplacement(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "selected.txt")
	if err := os.WriteFile(path, []byte("original"), 0600); err != nil {
		t.Fatal(err)
	}
	selected, err := OpenRegular(path)
	if err != nil {
		t.Fatal(err)
	}
	defer selected.Close()
	identity, err := FileIdentity(selected)
	if err != nil || identity == "" {
		t.Fatalf("missing identity: %v", err)
	}
	if err = os.Rename(path, filepath.Join(dir, "moved.txt")); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(path, []byte("replacement"), 0600); err != nil {
		t.Fatal(err)
	}
	replacement, err := OpenRegular(path)
	if err != nil {
		t.Fatal(err)
	}
	defer replacement.Close()
	newID, err := FileIdentity(replacement)
	if err != nil || newID == identity {
		t.Fatalf("replacement retained identity: %q %v", newID, err)
	}
	retainedID, err := FileIdentity(selected)
	if err != nil || retainedID != identity {
		t.Fatalf("descriptor identity changed: %q %v", retainedID, err)
	}
	bytes, err := io.ReadAll(selected)
	if err != nil || string(bytes) != "original" {
		t.Fatalf("descriptor retargeted: %q %v", bytes, err)
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	rootID, err := RootIdentity(root)
	if err != nil || rootID == "" || rootID == identity {
		t.Fatalf("invalid root identity: %q %v", rootID, err)
	}
	reopened, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	repeatedID, err := RootIdentity(reopened)
	if err != nil || repeatedID != rootID {
		t.Fatalf("root identity unstable: %q %v", repeatedID, err)
	}
}
