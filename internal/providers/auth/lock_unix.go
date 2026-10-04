//go:build darwin || linux

package auth

import (
	"os"
	"path/filepath"
	"syscall"
)

func ownedByCurrentUser(st os.FileInfo) bool {
	s, ok := st.Sys().(*syscall.Stat_t)
	return ok && int(s.Uid) == os.Getuid()
}
func lockRoot(root string) (*os.File, error) {
	// Reject symlink ancestors: a selected workspace cannot redirect host storage.
	for p := root; p != filepath.Dir(p); p = filepath.Dir(p) {
		st, err := os.Lstat(p)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil || st.Mode()&os.ModeSymlink != 0 {
			return nil, fail("STORAGE_FAILED", "Account storage path is unsafe")
		}
	}
	if err := os.MkdirAll(root, 0700); err != nil {
		return nil, fail("STORAGE_FAILED", "Account storage is unavailable")
	}
	st, err := os.Lstat(root)
	if err != nil || !st.IsDir() || !ownedByCurrentUser(st) || st.Mode().Perm()&0077 != 0 {
		return nil, fail("STORAGE_FAILED", "Account storage must be an owner-only directory")
	}
	fd, err := syscall.Open(filepath.Join(root, "owner.lock"), syscall.O_CREAT|syscall.O_RDWR|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0600)
	if err != nil {
		return nil, fail("STORAGE_FAILED", "Account storage lock is unavailable")
	}
	f := os.NewFile(uintptr(fd), "account storage lock")
	if st, err := f.Stat(); err != nil || !st.Mode().IsRegular() || !ownedByCurrentUser(st) || st.Mode().Perm()&0077 != 0 {
		f.Close()
		return nil, fail("STORAGE_FAILED", "Account storage lock is not private")
	}
	if syscall.Flock(fd, syscall.LOCK_EX|syscall.LOCK_NB) != nil {
		f.Close()
		return nil, fail("AUTH_BUSY", "Another Trove process owns these account registrations")
	}
	return f, nil
}
