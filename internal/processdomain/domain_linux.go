package processdomain

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"syscall"

	"golang.org/x/sys/unix"
)

type cgroup struct {
	root, dir int
	name      string
	identity  unix.Stat_t
	mu        sync.Mutex
	released  bool
}

// New requires an existing, explicitly delegated cgroup-v2 domain with cpu,
// memory and pids already enabled. It never edits ancestor policy or silently
// falls back to process groups. This is accounting for trusted fixtures, not a
// sandbox: same-UID migration and host filesystem access remain possible.
func New(config Config) (ProcessDomain, error) {
	if err := config.validate(); err != nil {
		return nil, err
	}
	root, err := unix.Openat2(unix.AT_FDCWD, config.DelegationRoot, &unix.OpenHow{Flags: unix.O_RDONLY | unix.O_DIRECTORY | unix.O_CLOEXEC, Resolve: unix.RESOLVE_NO_SYMLINKS | unix.RESOLVE_NO_MAGICLINKS})
	if err != nil {
		return nil, fmt.Errorf("%w: open explicit delegation: %v", ErrUnavailable, err)
	}
	b := &cgroup{root: root, dir: -1}
	good := false
	defer func() {
		if !good {
			if b.dir >= 0 {
				unix.Close(b.dir)
				unix.Unlinkat(root, b.name, unix.AT_REMOVEDIR)
			}
			unix.Close(root)
		}
	}()
	var fs unix.Statfs_t
	if err = unix.Fstatfs(root, &fs); err != nil || uint64(fs.Type) != unix.CGROUP2_SUPER_MAGIC {
		return nil, fmt.Errorf("%w: delegation is not cgroup v2", ErrUnavailable)
	}
	var owner unix.Stat_t
	if err = unix.Fstat(root, &owner); err != nil || owner.Uid != uint32(os.Geteuid()) || owner.Mode&0022 != 0 {
		return nil, fmt.Errorf("%w: delegation must be owned by this supervisor and not group/world writable", ErrUnavailable)
	}
	kind, err := readControl(root, "cgroup.type")
	if err != nil || kind != "domain" {
		return nil, fmt.Errorf("%w: delegation is not a normal domain", ErrUnavailable)
	}
	controllers, err := readControl(root, "cgroup.subtree_control")
	if err != nil {
		return nil, fmt.Errorf("%w: delegated controllers unavailable", ErrUnavailable)
	}
	for _, name := range []string{"cpu", "memory", "pids"} {
		if !hasWord(controllers, name) {
			return nil, fmt.Errorf("%w: delegated %s controller is not enabled", ErrUnavailable, name)
		}
	}
	nonce := make([]byte, 16)
	if _, err = rand.Read(nonce); err != nil {
		return nil, err
	}
	b.name = "trove-domain-" + hex.EncodeToString(nonce)
	if err = unix.Mkdirat(root, b.name, 0700); err != nil {
		return nil, fmt.Errorf("%w: create owned domain: %v", ErrUnavailable, err)
	}
	b.dir, err = unix.Openat(root, b.name, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		unix.Unlinkat(root, b.name, unix.AT_REMOVEDIR)
		return nil, err
	}
	if err = unix.Fstat(b.dir, &b.identity); err != nil {
		return nil, err
	}
	// No process can start until every required control is configured/read back.
	controls := []struct{ name, value string }{
		{"cgroup.max.depth", "0"}, {"cgroup.max.descendants", "0"},
		{"memory.max", strconv.FormatInt(config.Limits.MemoryBytes, 10)},
		{"memory.swap.max", "0"}, {"memory.oom.group", "1"},
		{"pids.max", strconv.FormatInt(config.Limits.Processes, 10)},
		{"cpu.max", fmt.Sprintf("%d %d", config.Limits.CPUQuotaMicros, config.Limits.CPUPeriodMicros)},
	}
	for _, control := range controls {
		if err = writeControl(b.dir, control.name, control.value); err != nil {
			return nil, fmt.Errorf("%w: cannot enforce %s: %v", ErrUnavailable, control.name, err)
		}
		actual, e := readControl(b.dir, control.name)
		if e != nil || actual != control.value {
			return nil, fmt.Errorf("%w: %s limit read-back mismatch", ErrUnavailable, control.name)
		}
	}
	kind, err = readControl(b.dir, "cgroup.type")
	if err != nil || kind != "domain" {
		return nil, fmt.Errorf("%w: child cgroup is not a normal domain", ErrUnavailable)
	}
	killFD, err := openControl(b.dir, "cgroup.kill", unix.O_WRONLY)
	if err != nil {
		return nil, fmt.Errorf("%w: recursive cgroup.kill is required", ErrUnavailable)
	}
	unix.Close(killFD)
	populated, err := b.populated()
	if err != nil || populated {
		return nil, fmt.Errorf("%w: new domain must be verifiably empty", ErrUnavailable)
	}
	if _, err = b.usage(); err != nil {
		return nil, fmt.Errorf("%w: aggregate accounting unavailable: %v", ErrUnavailable, err)
	}
	good = true
	return newDomain(b, config.Limits), nil
}

func openControl(dir int, name string, flags int) (int, error) {
	fd, err := unix.Openat(dir, name, flags|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return -1, err
	}
	var st unix.Stat_t
	if err = unix.Fstat(fd, &st); err != nil || st.Mode&unix.S_IFMT != unix.S_IFREG {
		unix.Close(fd)
		return -1, errors.New("cgroup control is not a regular kernel interface")
	}
	return fd, nil
}
func readControl(dir int, name string) (string, error) {
	fd, err := openControl(dir, name, unix.O_RDONLY)
	if err != nil {
		return "", err
	}
	file := os.NewFile(uintptr(fd), name)
	defer file.Close()
	body, err := io.ReadAll(io.LimitReader(file, (16<<10)+1))
	if err != nil {
		return "", err
	}
	if len(body) > 16<<10 {
		return "", errors.New("cgroup interface exceeds bound")
	}
	return strings.TrimSpace(string(body)), nil
}
func writeControl(dir int, name, value string) error {
	fd, err := openControl(dir, name, unix.O_WRONLY)
	if err != nil {
		return err
	}
	defer unix.Close(fd)
	body := []byte(value)
	count, err := unix.Write(fd, body)
	if err == nil && count != len(body) {
		return io.ErrShortWrite
	}
	return err
}
func hasWord(raw, want string) bool {
	for _, word := range strings.Fields(raw) {
		if word == want {
			return true
		}
	}
	return false
}
func (b *cgroup) start(command Command) (child, error) {
	cmd := exec.Command(command.Executable, command.Args...)
	cmd.Dir = command.Directory
	cmd.Env = append([]string{}, command.Environment...) // Empty means empty, never inherited.
	if command.Stdin != nil {
		cmd.Stdin = command.Stdin
	}
	if command.Stdout != nil {
		cmd.Stdout = command.Stdout
	}
	if command.Stderr != nil {
		cmd.Stderr = command.Stderr
	}
	// clone3 creates the first process inside the cgroup. An unsupported kernel,
	// seccomp restriction or missing permission fails Cmd.Start before user code;
	// never start then migrate a PID through cgroup.procs.
	cmd.SysProcAttr = &syscall.SysProcAttr{UseCgroupFD: true, CgroupFD: b.dir}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	return cmd, nil
}
func (b *cgroup) kill() error { return writeControl(b.dir, "cgroup.kill", "1") }
func (b *cgroup) populated() (bool, error) {
	raw, err := readControl(b.dir, "cgroup.events")
	if err != nil {
		return false, err
	}
	fields, err := parseCounters(raw)
	if err != nil {
		return false, err
	}
	value, ok := fields["populated"]
	if !ok || value > 1 {
		return false, errors.New("invalid cgroup populated state")
	}
	return value == 1, nil
}
func (b *cgroup) usage() (Usage, error) {
	var result Usage
	for _, item := range []struct {
		name  string
		value *uint64
	}{
		{"memory.current", &result.MemoryCurrentBytes}, {"memory.peak", &result.MemoryPeakBytes}, {"pids.current", &result.ProcessesCurrent},
	} {
		raw, err := readControl(b.dir, item.name)
		if err != nil {
			return result, err
		}
		value, err := parseUnsigned(raw)
		if err != nil {
			return result, err
		}
		*item.value = value
	}
	for _, item := range []struct {
		name, key string
		value     *uint64
	}{
		{"cpu.stat", "usage_usec", &result.CPUUsageMicros}, {"cpu.stat", "nr_throttled", &result.CPUThrottledPeriods}, {"memory.events", "oom_kill", &result.OOMKills}, {"pids.events", "max", &result.PIDLimitEvents},
	} {
		raw, err := readControl(b.dir, item.name)
		if err != nil {
			return result, err
		}
		values, err := parseCounters(raw)
		if err != nil {
			return result, err
		}
		value, ok := values[item.key]
		if !ok {
			return result, errors.New("required aggregate counter is missing")
		}
		*item.value = value
	}
	return result, nil
}
func (b *cgroup) release() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.released {
		return nil
	}
	// Removing a different inode after a rename/replacement would violate domain
	// ownership. Keep the original FD and refuse that cleanup instead.
	var current unix.Stat_t
	if err := unix.Fstatat(b.root, b.name, &current, unix.AT_SYMLINK_NOFOLLOW); err != nil {
		return err
	}
	if current.Ino != b.identity.Ino || current.Dev != b.identity.Dev {
		return errors.New("owned cgroup path was replaced")
	}
	populated, err := b.populated()
	if err != nil {
		return err
	}
	if populated {
		return ErrDrain
	}
	if err = unix.Unlinkat(b.root, b.name, unix.AT_REMOVEDIR); err != nil {
		return err
	}
	b.released = true
	return errors.Join(unix.Close(b.dir), unix.Close(b.root))
}
