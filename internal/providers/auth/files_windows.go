//go:build windows

package auth

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"unsafe"

	"github.com/google/uuid"
	"golang.org/x/sys/windows"
)

func currentSID() (*windows.SID, error) {
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return nil, err
	}
	return user.User.Sid, nil
}
func privateAttributes(directory bool) (*windows.SecurityAttributes, error) {
	sid, err := currentSID()
	if err != nil {
		return nil, err
	}
	flags := ""
	if directory {
		flags = "OICI"
	}
	sd, err := windows.SecurityDescriptorFromString("O:" + sid.String() + "D:P(A;" + flags + ";FA;;;" + sid.String() + ")(A;" + flags + ";FA;;;SY)")
	if err != nil {
		return nil, err
	}
	return &windows.SecurityAttributes{Length: uint32(unsafe.Sizeof(windows.SecurityAttributes{})), SecurityDescriptor: sd}, nil
}
func verifyPrivateHandle(h windows.Handle, directory bool) error {
	var info windows.ByHandleFileInformation
	if err := windows.GetFileInformationByHandle(h, &info); err != nil {
		return err
	}
	if info.FileAttributes&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 || (info.FileAttributes&windows.FILE_ATTRIBUTE_DIRECTORY != 0) != directory || (!directory && info.NumberOfLinks != 1) {
		return fmt.Errorf("unsafe private file type")
	}
	sd, err := windows.GetSecurityInfo(h, windows.SE_FILE_OBJECT, windows.OWNER_SECURITY_INFORMATION|windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		return err
	}
	sid, err := currentSID()
	if err != nil {
		return err
	}
	owner, _, err := sd.Owner()
	if err != nil || owner == nil || !owner.Equals(sid) {
		return fmt.Errorf("private file owner mismatch")
	}
	control, _, err := sd.Control()
	if err != nil || control&windows.SE_DACL_PROTECTED == 0 {
		return fmt.Errorf("unprotected private file ACL")
	}
	acl, _, err := sd.DACL()
	if err != nil || acl == nil || acl.AceCount < 1 || acl.AceCount > 8 {
		return fmt.Errorf("invalid private file ACL")
	}
	hasOwner := false
	for i := uint32(0); i < uint32(acl.AceCount); i++ {
		var ace *windows.ACCESS_ALLOWED_ACE
		if err := windows.GetAce(acl, i, &ace); err != nil {
			return err
		}
		if ace.Header.AceType != windows.ACCESS_ALLOWED_ACE_TYPE || ace.Header.AceFlags&windows.INHERIT_ONLY_ACE != 0 {
			return fmt.Errorf("unsupported private file ACE")
		}
		trustee := (*windows.SID)(unsafe.Pointer(&ace.SidStart))
		if !trustee.IsValid() {
			return fmt.Errorf("invalid private file trustee")
		}
		if trustee.Equals(sid) {
			hasOwner = true
		} else if trustee.String() != "S-1-5-18" {
			return fmt.Errorf("private file grants another account access")
		}
	}
	if !hasOwner {
		return fmt.Errorf("private file lacks owner access")
	}
	return nil
}
func openPrivate(path string, access, disposition uint32, directory bool) (windows.Handle, error) {
	name, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return windows.InvalidHandle, err
	}
	attrs, err := privateAttributes(directory)
	if err != nil {
		return windows.InvalidHandle, err
	}
	flags := uint32(windows.FILE_FLAG_OPEN_REPARSE_POINT)
	if directory {
		flags |= windows.FILE_FLAG_BACKUP_SEMANTICS
	}
	h, err := windows.CreateFile(name, access|windows.READ_CONTROL, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE, attrs, disposition, flags, 0)
	if err != nil {
		return windows.InvalidHandle, err
	}
	if err = verifyPrivateHandle(h, directory); err != nil {
		windows.CloseHandle(h)
		return windows.InvalidHandle, err
	}
	return h, nil
}
func ensurePrivateRoot(root string) error {
	// Only local drive paths are supported; a network share cannot satisfy the
	// declared machine-local credential and filesystem qualification boundary.
	if !filepath.IsAbs(root) || strings.HasPrefix(root, `\\`) || filepath.Clean(root) != root {
		return fmt.Errorf("local absolute storage required")
	}
	var paths []string
	for p := root; p != filepath.Dir(p); p = filepath.Dir(p) {
		paths = append(paths, p)
	}
	attrs, err := privateAttributes(true)
	if err != nil {
		return err
	}
	for i := len(paths) - 1; i >= 0; i-- {
		name, err := windows.UTF16PtrFromString(paths[i])
		if err != nil {
			return err
		}
		err = windows.CreateDirectory(name, attrs)
		if err != nil && err != windows.ERROR_ALREADY_EXISTS {
			return err
		}
		h, err := windows.CreateFile(name, windows.FILE_READ_ATTRIBUTES, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE, nil, windows.OPEN_EXISTING, windows.FILE_FLAG_OPEN_REPARSE_POINT|windows.FILE_FLAG_BACKUP_SEMANTICS, 0)
		if err != nil {
			return err
		}
		var info windows.ByHandleFileInformation
		err = windows.GetFileInformationByHandle(h, &info)
		windows.CloseHandle(h)
		if err != nil {
			return err
		}
		if info.FileAttributes&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 || info.FileAttributes&windows.FILE_ATTRIBUTE_DIRECTORY == 0 {
			return fmt.Errorf("storage ancestor is a reparse point or file")
		}
	}
	h, err := openPrivate(root, windows.FILE_READ_ATTRIBUTES, windows.OPEN_EXISTING, true)
	if err == nil {
		windows.CloseHandle(h)
	}
	return err
}
func lockRoot(root string) (*os.File, error) {
	if err := ensurePrivateRoot(root); err != nil {
		return nil, fail("STORAGE_FAILED", "Account storage must be a private local directory without reparse points")
	}
	h, err := openPrivate(filepath.Join(root, "owner.lock"), windows.GENERIC_READ|windows.GENERIC_WRITE, windows.OPEN_ALWAYS, false)
	if err != nil {
		return nil, fail("STORAGE_FAILED", "Account storage lock is not private")
	}
	if err = windows.LockFileEx(h, windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY, 0, 1, 0, &windows.Overlapped{}); err != nil {
		windows.CloseHandle(h)
		return nil, fail("AUTH_BUSY", "Another Trove process owns these account registrations")
	}
	return os.NewFile(uintptr(h), "account storage lock"), nil
}
func readPrivateFile(path string, limit int64) ([]byte, error) {
	h, err := openPrivate(path, windows.GENERIC_READ, windows.OPEN_EXISTING, false)
	if err != nil {
		return nil, err
	}
	f := os.NewFile(uintptr(h), path)
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if st.Size() > limit {
		return nil, fmt.Errorf("private file exceeds limit")
	}
	data, err := io.ReadAll(io.LimitReader(f, limit+1))
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("private file exceeds limit")
	}
	return data, err
}
func writePrivateFile(root, name string, data []byte) error {
	path := filepath.Join(root, ".account-"+uuid.NewString())
	h, err := openPrivate(path, windows.GENERIC_WRITE, windows.CREATE_NEW, false)
	if err != nil {
		return err
	}
	f := os.NewFile(uintptr(h), path)
	defer os.Remove(path)
	_, err = f.Write(data)
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	from, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return err
	}
	to, err := windows.UTF16PtrFromString(filepath.Join(root, name))
	if err != nil {
		return err
	}
	return windows.MoveFileEx(from, to, windows.MOVEFILE_REPLACE_EXISTING|windows.MOVEFILE_WRITE_THROUGH)
}
