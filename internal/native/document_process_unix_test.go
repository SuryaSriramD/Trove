//go:build darwin || linux

package native

import (
	"context"
	"errors"
	"fmt"
	"math"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"testing"
	"time"
)

func init() {
	if len(os.Args) != 3 || os.Args[1] != "trove-document-memory-fixture" {
		return
	}
	switch os.Args[2] {
	case "hang":
		for {
			time.Sleep(time.Hour)
		}
	case "flood":
		block := []byte(strings.Repeat("x", 64<<10))
		for i := 0; i < 128; i++ {
			if _, err := os.Stdout.Write(block); err != nil {
				syscall.Exit(0)
			}
		}
	case "valid":
		fmt.Print("41 41 12\n42 41 3\n99 99 9999\n")
	default:
		syscall.Exit(9)
	}
	syscall.Exit(0)
}
func TestDocumentMemorySnapshotRequiresCompleteBoundedLeaderInventory(t *testing.T) {
	valid := []byte("41 41 12\n42 41 3\n99 99 9999\n")
	got, err := parseDocumentProcessSnapshot(valid, 41)
	if err != nil || got != 15*1024 {
		t.Fatalf("wrong scoped sum: %d %v", got, err)
	}
	for name, raw := range map[string]string{
		"empty": "", "other group only": "99 99 12\n", "child without leader": "42 41 12\n", "moved leader": "41 99 12\n", "malformed": "41 41\n", "negative memory": "41 41 -1\n", "repeated pid": "41 41 1\n41 41 2\n", "invalid foreign row": "41 41 1\n99 99 unknown\n", "byte overflow": fmt.Sprintf("41 41 %d\n", int64(math.MaxInt64)/1024+1), "sum overflow": fmt.Sprintf("41 41 %d\n42 41 %d\n", int64(math.MaxInt64)/1024, int64(math.MaxInt64)/1024),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := parseDocumentProcessSnapshot([]byte(raw), 41); err == nil {
				t.Fatal("invalid memory inventory accepted")
			}
		})
	}
	if _, err := parseDocumentProcessSnapshot(make([]byte, documentSnapshotLimit+1), 41); err == nil {
		t.Fatal("oversize snapshot accepted")
	}
}
func TestDocumentMemoryProbeDeadlineAndOutputLimit(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	command := func(mode string) *exec.Cmd { return exec.Command(executable, "trove-document-memory-fixture", mode) }
	got, err := readDocumentProcessSnapshot(context.Background(), command("valid"), 41)
	if err != nil || got != 15*1024 {
		t.Fatalf("valid bounded probe: %d %v", got, err)
	}
	started := time.Now()
	if _, err = readDocumentProcessSnapshot(context.Background(), command("hang"), 41); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("hung probe lacked deadline error: %v", err)
	}
	if time.Since(started) > 2*time.Second {
		t.Fatal("hung memory probe exceeded bounded cleanup interval")
	}
	if _, err = readDocumentProcessSnapshot(context.Background(), command("flood"), 41); err == nil {
		t.Fatal("probe accepted more than four MiB")
	}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err = readDocumentProcessSnapshot(cancelled, command("valid"), 41); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled memory probe ran: %v", err)
	}
}
