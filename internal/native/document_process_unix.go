//go:build darwin || linux

package native

import (
	"errors"
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
func documentProcessRSS(p *workerProcess) (int64, error) {
	raw, err := exec.Command("/bin/ps", "-e", "-o", "pgid=,rss=").Output()
	if err != nil {
		return 0, err
	}
	var total int64
	for _, line := range strings.Split(string(raw), "\n") {
		fields := strings.Fields(line)
		if len(fields) != 2 {
			continue
		}
		group, e := strconv.Atoi(fields[0])
		if e != nil || group != p.cmd.Process.Pid {
			continue
		}
		rss, e := strconv.ParseInt(fields[1], 10, 64)
		if e != nil {
			return 0, e
		}
		total += rss * 1024
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
