// Package platformfs contains descriptor and ownership primitives shared by the
// workspace kernel. No method receives renderer input or resolves permissions.
package platformfs

import (
	"errors"
	"os"
)

var ErrLocked = errors.New("file already has an exclusive owner")

// OpenRegular pins a regular file without following a final symlink/reparse point.
func OpenRegular(path string) (*os.File, error) {
	f, err := openRegular(path)
	if err != nil {
		return nil, err
	}
	st, err := f.Stat()
	if err != nil || !st.Mode().IsRegular() {
		f.Close()
		if err != nil {
			return nil, err
		}
		return nil, errors.New("file is not regular")
	}
	return f, nil
}

// AcquireLock retains its descriptor until ReleaseLock. Closing it after process
// death also releases OS ownership; the lock pathname itself is never removed.
func AcquireLock(path string) (*os.File, error) {
	f, err := openLock(path)
	if err != nil {
		return nil, err
	}
	if err = lockFile(f); err != nil {
		f.Close()
		return nil, err
	}
	return f, nil
}
func ReleaseLock(f *os.File) error {
	if f == nil {
		return nil
	}
	return errors.Join(unlockFile(f), f.Close())
}

func RootIdentity(root *os.Root) (string, error) {
	f, err := root.Open(".")
	if err != nil {
		return "", err
	}
	defer f.Close()
	return FileIdentity(f)
}
