//go:build darwin || linux

package platformfs

import (
	"errors"
	"golang.org/x/sys/unix"
	"os"
	"path/filepath"
)

func ensurePrivateDirectory(path string) error {
	if err := os.MkdirAll(path, 0700); err != nil {
		return err
	}
	st, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !st.IsDir() || st.Mode()&os.ModeSymlink != 0 {
		return ErrPrivateStorage
	}
	return nil
}
func auditWorkspace(string) error      { return nil }
func requireRecoveryDurability() error { return nil }
func syncDirectory(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return f.Sync()
}
func publishFile(staged, target string, replace bool) error {
	if filepath.Clean(staged) == filepath.Clean(target) {
		return errors.New("publication source equals target")
	}
	fd, err := unix.Open(staged, unix.O_RDWR|unix.O_NOFOLLOW|unix.O_CLOEXEC|unix.O_NONBLOCK, 0)
	if err != nil {
		return err
	}
	f := os.NewFile(uintptr(fd), staged)
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return err
	}
	if !st.Mode().IsRegular() {
		return errors.New("publication source must be regular")
	}
	if dst, err := os.Lstat(target); err == nil {
		if !dst.Mode().IsRegular() {
			return errors.New("publication target must be regular")
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	if err = f.Sync(); err != nil {
		return err
	}
	if err = renamePublication(staged, target, replace); err != nil {
		return err
	}
	if err = syncDirectory(filepath.Dir(target)); err != nil {
		return err
	}
	if filepath.Dir(staged) != filepath.Dir(target) {
		return syncDirectory(filepath.Dir(staged))
	}
	return nil
}
