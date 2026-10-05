//go:build darwin || linux

package auth

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"syscall"
)

func readPrivateFile(path string, limit int64) ([]byte, error) {
	fd, err := syscall.Open(path, syscall.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}
	f := os.NewFile(uintptr(fd), path)
	defer f.Close()
	st, err := f.Stat()
	if err != nil || !st.Mode().IsRegular() || !ownedByCurrentUser(st) || st.Mode().Perm()&0077 != 0 || st.Size() > limit {
		return nil, fmt.Errorf("private file required")
	}
	data, err := io.ReadAll(io.LimitReader(f, limit+1))
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("private file exceeds limit")
	}
	return data, err
}
func writePrivateFile(root, name string, data []byte) error {
	f, err := os.CreateTemp(root, ".account-*")
	if err != nil {
		return err
	}
	path := f.Name()
	defer os.Remove(path)
	err = f.Chmod(0600)
	if err == nil {
		_, err = f.Write(data)
	}
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err == nil {
		err = closeErr
	}
	if err == nil {
		err = os.Rename(path, filepath.Join(root, name))
	}
	if err != nil {
		return err
	}
	dir, err := os.Open(root)
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}
