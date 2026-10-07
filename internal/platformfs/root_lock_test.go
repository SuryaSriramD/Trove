//go:build darwin || linux

package platformfs

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestRootLockPinnedOwnershipAndRefusedLinks(t *testing.T) {
	dir := t.TempDir()
	r, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	first, err := AcquireRootLock(r, "owner.lock")
	if err != nil {
		t.Fatal(err)
	}
	defer ReleaseLock(first)
	if second, err := AcquireRootLock(r, "owner.lock"); !errors.Is(err, ErrLocked) {
		ReleaseLock(second)
		t.Fatal("second owner admitted", err)
	}
	outside := filepath.Join(t.TempDir(), "untouched")
	if err := os.WriteFile(outside, []byte("unchanged"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(dir, "linked")); err != nil {
		t.Fatal(err)
	}
	if lock, err := AcquireRootLock(r, "linked"); err == nil {
		ReleaseLock(lock)
		t.Fatal("linked lock admitted")
	}
	if err := os.Link(outside, filepath.Join(dir, "hard-linked")); err != nil {
		t.Fatal(err)
	}
	if lock, err := AcquireRootLock(r, "hard-linked"); err == nil {
		ReleaseLock(lock)
		t.Fatal("multiply linked lock admitted")
	}
	if raw, err := os.ReadFile(outside); err != nil || string(raw) != "unchanged" {
		t.Fatal("outside changed", err)
	}
}
