//go:build darwin || linux

package native

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"time"
)

func prepareDocumentProcess(cmd *exec.Cmd) (*workerProcess, error) {
	return prepareWorker(cmd, nil, 2<<30)
}
func documentEnvironment(private string) []string {
	env := documentBaseEnvironment(private)
	// Native display/session endpoints are required by GTK. No other user
	// settings, proxy, runtime loader overrides, or app credentials cross over.
	for _, key := range []string{"DISPLAY", "WAYLAND_DISPLAY", "XDG_RUNTIME_DIR", "XAUTHORITY", "DBUS_SESSION_BUS_ADDRESS"} {
		if value, ok := os.LookupEnv(key); ok {
			env = append(env, key+"="+value)
		}
	}
	return append(env, "GTK_PRINT_BACKENDS=file", "LC_ALL=C")
}

const documentSnapshotLimit = 4 << 20
const documentSnapshotTimeout = 250 * time.Millisecond

func documentProcessRSS(p *workerProcess) (int64, error) {
	if p == nil || p.cmd == nil || p.cmd.Process == nil {
		return 0, errors.New("document process identity unavailable")
	}
	// This is a bounded diagnostic for the local group; it is not an enforceable
	// browser-domain memory limit. Linux document capability is disabled above.
	return readDocumentProcessSnapshot(context.Background(), exec.Command("/bin/ps", "-e", "-o", "pid=,pgid=,rss="), p.cmd.Process.Pid)
}

func readDocumentProcessSnapshot(ctx context.Context, cmd *exec.Cmd, leader int) (int64, error) {
	if leader <= 0 {
		return 0, errors.New("invalid document process identity")
	}
	ctx, cancel := context.WithTimeout(ctx, documentSnapshotTimeout)
	defer cancel()
	// CommandContext owns cancellation; copy only native-created argv. The helper
	// does not select this command and no ambient credentials/loader options pass.
	bounded := exec.CommandContext(ctx, cmd.Path, cmd.Args[1:]...)
	bounded.Env = []string{"PATH=/usr/bin:/bin", "LC_ALL=C"}
	bounded.WaitDelay = 100 * time.Millisecond
	var out bytes.Buffer
	bounded.Stdout = &documentOutput{w: &out, remaining: documentSnapshotLimit}
	bounded.Stderr = io.Discard
	if err := bounded.Run(); err != nil {
		return 0, fmt.Errorf("document memory snapshot unavailable: %w", errors.Join(err, ctx.Err()))
	}
	return parseDocumentProcessSnapshot(out.Bytes(), leader)
}

func parseDocumentProcessSnapshot(raw []byte, leader int) (int64, error) {
	if leader <= 0 || len(raw) > documentSnapshotLimit {
		return 0, errors.New("invalid document memory snapshot")
	}
	var total int64
	seen := map[int64]bool{}
	foundLeader := false
	for _, line := range strings.Split(string(raw), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) != 3 {
			return 0, errors.New("malformed document memory snapshot")
		}
		pid, err := strconv.ParseInt(fields[0], 10, 64)
		if err != nil || pid <= 0 || seen[pid] {
			return 0, errors.New("invalid or repeated process identity")
		}
		seen[pid] = true
		group, err := strconv.ParseInt(fields[1], 10, 64)
		if err != nil || group < 0 {
			return 0, errors.New("invalid process group")
		}
		rss, err := strconv.ParseInt(fields[2], 10, 64)
		if err != nil || rss < 0 {
			return 0, errors.New("invalid process memory")
		}
		if group != int64(leader) {
			continue
		}
		if rss > math.MaxInt64/1024 || total > math.MaxInt64-rss*1024 {
			return 0, errors.New("process memory overflow")
		}
		total += rss * 1024
		if pid == int64(leader) {
			foundLeader = true
		}
	}
	if !foundLeader {
		return 0, errors.New("document process leader missing from memory snapshot")
	}
	return total, nil
}
func documentMkdir(path string) error { return os.Mkdir(path, 0700) }

func drainDocumentProcess(p *workerProcess) error {
	if err := p.stop(true); err != nil && !errors.Is(err, syscall.ESRCH) {
		return errors.New("document process group termination failed")
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		if err := syscall.Kill(-p.cmd.Process.Pid, 0); errors.Is(err, syscall.ESRCH) {
			return nil
		}
		if time.Now().After(deadline) {
			return errors.New("document process group cleanup exceeded deadline")
		}
		time.Sleep(25 * time.Millisecond)
	}
}
