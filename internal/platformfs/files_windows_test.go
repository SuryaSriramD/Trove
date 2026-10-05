//go:build windows

package platformfs

import (
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
