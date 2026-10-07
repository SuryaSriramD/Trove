//go:build windows

package platformfs

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/windows"
)

func TestWindowsPathRejectsDevicesNetworksAndStreams(t *testing.T) {
	for _, path := range []string{`\\server\share\file`, `\\?\C:\file`, `\\.\PhysicalDrive0`, `C:\file:secret`, `C:\file::$DATA`} {
		if _, err := windowsPath(path); err == nil {
			t.Fatalf("unsafe path accepted: %q", path)
		}
	}
	path := filepath.Join(t.TempDir(), strings.Repeat("a", 100), strings.Repeat("b", 100), strings.Repeat("c", 100))
	utf16, err := windowsPath(path)
	if err != nil {
		t.Fatal(err)
	}
	if result := windows.UTF16PtrToString(utf16); !strings.HasPrefix(result, `\\?\`) || len(result) < 260 {
		t.Fatalf("long local path not preserved: %q", result)
	}
}

func TestWindowsRootLockRejectsReplacementWhileOwned(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "private")
	if err := EnsurePrivateDirectory(dir); err != nil {
		t.Fatal(err)
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	lock, err := AcquireRootLock(root, "owner.lock")
	if err != nil {
		t.Fatal(err)
	}
	defer ReleaseLock(lock)
	if err := root.Rename("owner.lock", "moved.lock"); err == nil {
		t.Fatal("renamed a live root owner lock")
	}
	if err := root.Remove("owner.lock"); err == nil {
		t.Fatal("deleted a live root owner lock")
	}
	if second, err := AcquireRootLock(root, "owner.lock"); !errors.Is(err, ErrLocked) {
		ReleaseLock(second)
		t.Fatal("second owner admitted", err)
	}
}
