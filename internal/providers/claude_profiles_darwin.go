//go:build darwin

package providers

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"syscall"
)

func validatePrivateClaudeFile(path string, directory bool, limit int64) error {
	st, err := os.Lstat(path)
	if err != nil {
		return errors.New("Private Claude storage is unavailable")
	}
	owner, ok := st.Sys().(*syscall.Stat_t)
	if !ok || int(owner.Uid) != os.Getuid() || st.Mode().Perm()&0077 != 0 || st.Mode()&os.ModeSymlink != 0 || st.IsDir() != directory || (!directory && (!st.Mode().IsRegular() || st.Size() > limit)) {
		return errors.New("Claude storage must be an owner-only regular file or directory")
	}
	return nil
}
func privateClaudeDirectory(path string) error {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return errors.New("Private Claude storage path is invalid")
	}
	for p := path; p != filepath.Dir(p); p = filepath.Dir(p) {
		st, err := os.Lstat(p)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil || !st.IsDir() || st.Mode()&os.ModeSymlink != 0 {
			return errors.New("Private Claude storage cannot use symlink ancestors")
		}
	}
	if err := os.MkdirAll(path, 0700); err != nil {
		return errors.New("Private Claude storage cannot be created")
	}
	return validatePrivateClaudeFile(path, true, 0)
}
func lockClaudeProfileRoot(root string) (*os.File, error) {
	if err := privateClaudeDirectory(root); err != nil {
		return nil, err
	}
	path := filepath.Join(root, "owner.lock")
	fd, err := syscall.Open(path, syscall.O_CREAT|syscall.O_RDWR|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0600)
	if err != nil {
		return nil, errors.New("Private Claude profile lock is unavailable")
	}
	f := os.NewFile(uintptr(fd), path)
	if err = validatePrivateClaudeFile(path, false, 0); err != nil {
		f.Close()
		return nil, err
	}
	if syscall.Flock(fd, syscall.LOCK_EX|syscall.LOCK_NB) != nil {
		f.Close()
		return nil, errors.New("Another Trove process owns these Claude profiles")
	}
	return f, nil
}
func launchClaudeTerminal(ctx context.Context, script string) error {
	// The user-visible script contains only an exactly quoted app/control path.
	// Credentials are neither passed in arguments nor inherited by the vendor.
	cmd := exec.CommandContext(ctx, "/usr/bin/open", "-a", "Terminal", script)
	cmd.Env = []string{"PATH=/usr/bin:/bin", "LANG=en_US.UTF-8"}
	return cmd.Run()
}

func claudeTerminalContext() (context.Context, context.CancelFunc) {
	return signal.NotifyContext(context.Background(), syscall.SIGHUP, syscall.SIGTERM, syscall.SIGINT)
}
