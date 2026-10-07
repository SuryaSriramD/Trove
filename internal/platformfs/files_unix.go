//go:build darwin || linux

package platformfs

import (
	"errors"
	"fmt"
	"os"
	"syscall"
)

func openRegular(path string) (*os.File, error) {
	fd, err := syscall.Open(path, syscall.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_CLOEXEC|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, err
	}
	return os.NewFile(uintptr(fd), path), nil
}
func openLock(path string) (*os.File, error) {
	fd, err := syscall.Open(path, syscall.O_RDWR|syscall.O_CREAT|syscall.O_NOFOLLOW|syscall.O_CLOEXEC|syscall.O_NONBLOCK, 0600)
	if err != nil {
		return nil, err
	}
	f := os.NewFile(uintptr(fd), path)
	st, err := f.Stat()
	if err != nil || !st.Mode().IsRegular() {
		f.Close()
		return nil, errors.New("owner lock is not a regular file")
	}
	if native, ok := st.Sys().(*syscall.Stat_t); !ok || native.Nlink != 1 {
		f.Close()
		return nil, errors.New("owner lock has multiple links")
	}
	return f, nil
}
func lockFile(f *os.File) error {
	err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
	if errors.Is(err, syscall.EWOULDBLOCK) || errors.Is(err, syscall.EAGAIN) {
		return ErrLocked
	}
	return err
}
func unlockFile(f *os.File) error { return syscall.Flock(int(f.Fd()), syscall.LOCK_UN) }
func auditPrivateHandle(f *os.File, directory bool) error {
	st, err := f.Stat()
	if err != nil {
		return err
	}
	native, ok := st.Sys().(*syscall.Stat_t)
	if !ok || native.Uid != uint32(os.Geteuid()) || st.Mode().Perm()&0077 != 0 || st.IsDir() != directory || (!directory && (!st.Mode().IsRegular() || native.Nlink != 1)) {
		return ErrPrivateStorage
	}
	return nil
}
func FileIdentity(f *os.File) (string, error) {
	st, err := f.Stat()
	if err != nil {
		return "", err
	}
	native, ok := st.Sys().(*syscall.Stat_t)
	if !ok {
		return "", errors.New("stable filesystem identity unavailable")
	}
	return fmt.Sprintf("%d:%d", native.Dev, native.Ino), nil
}
