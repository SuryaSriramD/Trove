package providers

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"time"
)

type Runtime struct {
	Provider  string `json:"provider"`
	Version   string `json:"version"`
	SHA256    string `json:"sha256"`
	Path      string `json:"-"`
	Available bool   `json:"available"`
	Reason    string `json:"reason,omitempty"`
}

// Discover executes only the official version command in a fresh, networkless
// containment. It neither opens vendor configuration nor inspects credentials.
func Discover(ctx context.Context, provider string) Runtime {
	r := Runtime{Provider: provider}
	name := "codex"
	if provider == "claude_code" {
		name = "claude"
	} else if provider != "codex" {
		r.Reason = "Unknown provider"
		return r
	}
	if runtime.GOOS != "darwin" || !containmentAvailable() {
		r.Reason = "Managed runtime containment is unavailable on this platform"
		return r
	}
	path, err := exec.LookPath(name)
	if err != nil {
		home, _ := os.UserHomeDir()
		for _, candidate := range []string{filepath.Join(home, ".local/bin", name), filepath.Join("/opt/homebrew/bin", name), filepath.Join("/usr/local/bin", name)} {
			if info, e := os.Stat(candidate); e == nil && info.Mode().IsRegular() {
				path = candidate
				break
			}
		}
	}
	if path == "" {
		r.Reason = "Install the official " + name + " runtime, then discover again"
		return r
	}
	r.Path, err = filepath.EvalSymlinks(path)
	if err != nil {
		r.Reason = "Runtime path cannot be resolved"
		return r
	}
	r.SHA256, err = runtimeDigest(r.Path)
	if err != nil {
		r.Reason = err.Error()
		return r
	}
	dir, err := os.MkdirTemp("", "trove-provider-probe-")
	if err != nil {
		r.Reason = "Could not create private runtime probe"
		return r
	}
	defer os.RemoveAll(dir)
	dir, _ = filepath.EvalSymlinks(dir)
	bounded, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	cmd, err := containedCommand(bounded, r.Path, dir, []string{"--version"}, isolatedEnv(dir), false)
	if err != nil {
		r.Reason = err.Error()
		return r
	}
	var out limitedBuffer
	cmd.Stdout = &out
	cmd.Stderr = io.Discard
	if err = cmd.Run(); err != nil {
		r.Reason = "Runtime could not start inside Trove's containment"
		return r
	}
	pattern := regexp.MustCompile(`^codex-cli (0\.153\.[0-9]+)\s*$`)
	if provider == "claude_code" {
		pattern = regexp.MustCompile(`^(2\.1\.[0-9]+) \(Claude Code\)\s*$`)
	}
	match := pattern.FindStringSubmatch(out.String())
	if len(match) != 2 {
		r.Reason = "Runtime version has no compatible Trove adapter"
		return r
	}
	r.Version, r.Available = match[1], true
	return r
}

func runtimeDigest(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", errors.New("Runtime is unavailable")
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Mode()&0111 == 0 || info.Size() < 4 || info.Size() > 512<<20 {
		return "", errors.New("Runtime must be an executable file of at most 512 MiB")
	}
	var magic [4]byte
	if _, err = io.ReadFull(f, magic[:]); err != nil || (magic != [4]byte{0xcf, 0xfa, 0xed, 0xfe} && magic != [4]byte{0xca, 0xfe, 0xba, 0xbe} && magic != [4]byte{0xca, 0xfe, 0xba, 0xbf}) {
		return "", errors.New("Managed mode requires the official native macOS executable")
	}
	if _, err = f.Seek(0, 0); err != nil {
		return "", err
	}
	h := sha256.New()
	if _, err = io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func isolatedEnv(dir string) []string {
	return []string{"HOME=" + dir, "TMPDIR=" + dir, "PATH=/usr/bin:/bin", "LANG=en_US.UTF-8", "NO_COLOR=1"}
}

type limitedBuffer struct {
	mu    sync.Mutex
	bytes []byte
}

func (b *limitedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if len(b.bytes)+len(p) > 64<<10 {
		return 0, errors.New("runtime diagnostic exceeds limit")
	}
	b.bytes = append(b.bytes, p...)
	return len(p), nil
}
func (b *limitedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return strings.TrimSpace(string(b.bytes))
}
