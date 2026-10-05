//go:build darwin || linux

package platformfs

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

func TestOpenRegularRejectsFIFOWithoutWaitingForWriter(t *testing.T) {
	path := filepath.Join(t.TempDir(), "fifo")
	if err := syscall.Mkfifo(path, 0600); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		f, err := OpenRegular(path)
		if f != nil {
			f.Close()
		}
		done <- err
	}()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("FIFO accepted as regular file")
		}
	case <-time.After(time.Second):
		t.Fatal("file-type rejection blocked on a FIFO")
	}
}

func TestUnixIdentityPreservesPersistedFormat(t *testing.T) {
	path := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(path, nil, 0600); err != nil {
		t.Fatal(err)
	}
	f, err := OpenRegular(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		t.Fatal(err)
	}
	stat := info.Sys().(*syscall.Stat_t)
	actual, err := FileIdentity(f)
	if err != nil {
		t.Fatal(err)
	}
	expected := fmt.Sprintf("%d:%d", stat.Dev, stat.Ino)
	if actual != expected {
		t.Fatalf("persisted external-root identity changed: %q != %q", actual, expected)
	}
}
