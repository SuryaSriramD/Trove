//go:build windows

package platformfs

import (
	"errors"
	"fmt"
	"golang.org/x/sys/windows"
	"os"
	"path/filepath"
	"strings"
)

func windowsPath(path string) (*uint16, error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	// Workspace ownership is supported on local volumes only. Device paths and
	// named streams cannot represent a stable canonical asset or owner lock.
	if strings.HasPrefix(absolute, `\\`) || len(absolute) < 3 || absolute[1] != ':' || strings.Contains(absolute[2:], ":") {
		return nil, errors.New("local filesystem path required")
	}
	return windows.UTF16PtrFromString(`\\?\` + absolute)
}
func openWindows(path string, access, disposition, share uint32, singleLink bool) (*os.File, error) {
	name, err := windowsPath(path)
	if err != nil {
		return nil, err
	}
	h, err := windows.CreateFile(name, access, share, nil, disposition, windows.FILE_FLAG_OPEN_REPARSE_POINT, 0)
	if err != nil {
		return nil, err
	}
	if kind, err := windows.GetFileType(h); err != nil || kind != windows.FILE_TYPE_DISK {
		windows.CloseHandle(h)
		return nil, errors.New("regular disk file required")
	}
	var info windows.ByHandleFileInformation
	if err = windows.GetFileInformationByHandle(h, &info); err != nil {
		windows.CloseHandle(h)
		return nil, err
	}
	if info.FileAttributes&(windows.FILE_ATTRIBUTE_REPARSE_POINT|windows.FILE_ATTRIBUTE_DIRECTORY) != 0 || singleLink && info.NumberOfLinks != 1 {
		windows.CloseHandle(h)
		return nil, errors.New("unsafe file type or multiple owner-lock links")
	}
	return os.NewFile(uintptr(h), path), nil
}
func openRegular(path string) (*os.File, error) {
	return openWindows(path, windows.GENERIC_READ, windows.OPEN_EXISTING, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE, false)
}

// Denying FILE_SHARE_DELETE prevents renaming/replacing a live owner lock.
func openLock(path string) (*os.File, error) {
	return openWindows(path, windows.GENERIC_READ|windows.GENERIC_WRITE, windows.OPEN_ALWAYS, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE, true)
}
func lockFile(f *os.File) error {
	err := windows.LockFileEx(windows.Handle(f.Fd()), windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY, 0, 1, 0, &windows.Overlapped{})
	if errors.Is(err, windows.ERROR_LOCK_VIOLATION) || errors.Is(err, windows.ERROR_IO_PENDING) {
		return ErrLocked
	}
	return err
}
func unlockFile(f *os.File) error {
	return windows.UnlockFileEx(windows.Handle(f.Fd()), 0, 1, 0, &windows.Overlapped{})
}
func auditPrivateHandle(f *os.File, directory bool) error {
	h := windows.Handle(f.Fd())
	if _, err := storageInfo(h, directory); err != nil {
		return err
	}
	_, err := auditStorageACL(h)
	return err
}
func FileIdentity(f *os.File) (string, error) {
	h := windows.Handle(f.Fd())
	kind, err := windows.GetFileType(h)
	if err != nil || kind != windows.FILE_TYPE_DISK {
		return "", errors.New("stable disk identity unavailable")
	}
	var info windows.ByHandleFileInformation
	if err = windows.GetFileInformationByHandle(h, &info); err != nil {
		return "", err
	}
	if info.FileAttributes&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 {
		return "", errors.New("reparse identity is not allowed")
	}
	if info.FileIndexHigh == 0 && info.FileIndexLow == 0 {
		return "", errors.New("filesystem did not supply stable identity")
	}
	return fmt.Sprintf("win:%08x:%08x%08x", info.VolumeSerialNumber, info.FileIndexHigh, info.FileIndexLow), nil
}
