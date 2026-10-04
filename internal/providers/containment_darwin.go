//go:build darwin

package providers

import (
	"context"
	"errors"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

const ContainmentPolicy = "trove-provider-macos-v1"

// containedCommand permits only the selected executable and its private state.
// No workspace path, user home, shell, Apple Events, or Keychain service is
// exposed to a turn. A runtime's own tool configuration is defense in depth;
// it cannot relax this outer process policy through an approval request.
func containedCommand(ctx context.Context, binary, state string, args, env []string, network bool) (*exec.Cmd, error) {
	for _, p := range []string{binary, state} {
		if !filepath.IsAbs(p) || strings.ContainsAny(p, "\x00\n\r") {
			return nil, errors.New("invalid containment path")
		}
	}
	q := strconv.Quote
	profile := `(version 1)
(deny default)
(allow process-exec (literal ` + q(binary) + `))
(allow process-info* (target self))
(allow signal (target self))
(allow sysctl-read)
(allow file-read-metadata)
(allow file-read* (literal "/"))
(allow file-map-executable (subpath "/System/Library") (subpath "/System/Volumes/Preboot/Cryptexes") (subpath "/System/Cryptexes") (subpath "/usr/lib") (subpath "/Library/Apple/System") (literal ` + q(binary) + `))
(allow file-read* (subpath "/System/Library") (subpath "/System/Volumes/Preboot/Cryptexes") (subpath "/System/Cryptexes") (subpath "/usr/lib") (subpath "/Library/Apple/System") (subpath "/private/var/db/dyld") (literal "/dev/null") (literal "/dev/random") (literal "/dev/urandom") (literal ` + q(binary) + `) (subpath ` + q(state) + `))
(allow file-write* (subpath ` + q(state) + `) (literal "/dev/null"))
(allow mach-lookup (global-name "com.apple.system.logger") (global-name "com.apple.logd") (global-name "com.apple.trustd.agent") (global-name "com.apple.trustd") (global-name "com.apple.networkd") (global-name "com.apple.SystemConfiguration.configd") (global-name "com.apple.system.opendirectoryd"))
`
	if network {
		profile += `(allow network-outbound (remote tcp))
(allow network-outbound (remote udp "*:53"))
(allow file-read* (literal "/private/etc/hosts") (literal "/private/etc/resolv.conf") (literal "/private/etc/protocols") (literal "/private/etc/services") (subpath "/private/var/run/resolv.conf"))
`
	}
	cmd := exec.CommandContext(ctx, "/usr/bin/sandbox-exec", append([]string{"-p", profile, binary}, args...)...)
	cmd.Dir = state
	cmd.Env = env
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	cmd.WaitDelay = 2 * time.Second
	return cmd, nil
}

func containmentAvailable() bool { _, err := exec.LookPath("/usr/bin/sandbox-exec"); return err == nil }
