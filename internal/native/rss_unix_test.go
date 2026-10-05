//go:build (darwin && cgo) || linux

package native

import (
	"os"
	"os/exec"
	"runtime"
	"testing"
)

func TestWorkerRSSUsesOwnedProcessWithoutPS(t *testing.T) {
	process, err := os.FindProcess(os.Getpid())
	if err != nil {
		t.Fatal(err)
	}
	p := &workerProcess{cmd: &exec.Cmd{Process: process}}
	// Touch an independent allocation so the expected resident lower bound comes
	// from data that this fixture actually owns, not a guessed process baseline.
	data := make([]byte, 8<<20)
	for i := 0; i < len(data); i += os.Getpagesize() {
		data[i] = byte(i + 1)
	}
	n, err := p.rss()
	runtime.KeepAlive(data)
	if err != nil || n < 8<<20 {
		t.Fatalf("owned resident allocation not measurable: %d %v", n, err)
	}
	for _, pid := range []int{0, -1} {
		if _, err = processRSS(pid); err == nil {
			t.Fatal("invalid PID accepted", pid)
		}
	}
	if _, err = (&workerProcess{cmd: &exec.Cmd{}}).rss(); err != os.ErrProcessDone {
		t.Fatal(err)
	}
}
