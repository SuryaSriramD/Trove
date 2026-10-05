//go:build windows

package platformfs

import (
	"errors"
	"fmt"
	"golang.org/x/sys/windows"
	"os"
	"path/filepath"
	"strings"
	"unsafe"
)

func requireRecoveryDurability() error { return ErrDirectoryDurability }
func syncDirectory(string) error       { return ErrDirectoryDurability }

// Extended Win32 paths preserve names that ordinary Go/Win32 calls normalize.
// Reject ambiguous components so both APIs refer to the same managed objects.
func validateStoragePath(path string) error {
	if _, err := windowsPath(path); err != nil {
		return err
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	for _, part := range strings.Split(absolute[3:], string(filepath.Separator)) {
		if part == "" {
			continue
		}
		if strings.TrimRight(part, ". ") != part {
			return ErrPrivateStorage
		}
		for _, r := range part {
			if r < 32 || strings.ContainsRune(`<>"|?*`, r) {
				return ErrPrivateStorage
			}
		}
		stem := strings.ToUpper(strings.SplitN(part, ".", 2)[0])
		if stem == "CON" || stem == "PRN" || stem == "AUX" || stem == "NUL" {
			return ErrPrivateStorage
		}
		for _, prefix := range []string{"COM", "LPT"} {
			if strings.HasPrefix(stem, prefix) && strings.Contains("123456789¹²³", strings.TrimPrefix(stem, prefix)) && len([]rune(strings.TrimPrefix(stem, prefix))) == 1 {
				return ErrPrivateStorage
			}
		}
	}
	return nil
}

func currentStorageSID() (*windows.SID, error) {
	u, e := windows.GetCurrentProcessToken().GetTokenUser()
	if e != nil {
		return nil, e
	}
	return u.User.Sid, nil
}
func privateStorageAttributes() (*windows.SecurityAttributes, error) {
	sid, e := currentStorageSID()
	if e != nil {
		return nil, e
	}
	sd, e := windows.SecurityDescriptorFromString("O:" + sid.String() + "D:P(A;OICI;FA;;;" + sid.String() + ")(A;OICI;FA;;;SY)")
	if e != nil {
		return nil, e
	}
	return &windows.SecurityAttributes{Length: uint32(unsafe.Sizeof(windows.SecurityAttributes{})), SecurityDescriptor: sd}, nil
}
func auditStorageACL(h windows.Handle) (string, error) {
	sd, e := windows.GetSecurityInfo(h, windows.SE_FILE_OBJECT, windows.OWNER_SECURITY_INFORMATION|windows.DACL_SECURITY_INFORMATION)
	if e != nil {
		return "", e
	}
	sid, e := currentStorageSID()
	if e != nil {
		return "", e
	}
	owner, _, e := sd.Owner()
	if e != nil || owner == nil || !owner.Equals(sid) {
		return "", ErrPrivateStorage
	}
	acl, _, e := sd.DACL()
	if e != nil || acl == nil || acl.AceCount == 0 || acl.AceCount > 1024 {
		return "", ErrPrivateStorage
	}
	ownerAllowed := false
	for i := uint32(0); i < uint32(acl.AceCount); i++ {
		var ace *windows.ACCESS_ALLOWED_ACE
		if e = windows.GetAce(acl, i, &ace); e != nil {
			return "", e
		}
		// Complex/callback/object ACLs need an explicit access model; do not rewrite them.
		if ace.Header.AceType == windows.ACCESS_DENIED_ACE_TYPE {
			continue
		}
		if ace.Header.AceType != windows.ACCESS_ALLOWED_ACE_TYPE {
			return "", ErrPrivateStorage
		}
		principal := (*windows.SID)(unsafe.Pointer(&ace.SidStart))
		if !principal.Equals(sid) && !principal.IsWellKnown(windows.WinLocalSystemSid) && !principal.IsWellKnown(windows.WinBuiltinAdministratorsSid) && ace.Mask != 0 {
			return "", ErrPrivateStorage
		}
		if principal.Equals(sid) && ace.Header.AceFlags&windows.INHERIT_ONLY_ACE == 0 && ace.Mask != 0 {
			ownerAllowed = true
		}
	}
	if !ownerAllowed {
		return "", ErrPrivateStorage
	}
	return sd.String(), nil
}
func storageInfo(h windows.Handle, directory bool) (windows.ByHandleFileInformation, error) {
	var info windows.ByHandleFileInformation
	e := windows.GetFileInformationByHandle(h, &info)
	if e != nil {
		return info, e
	}
	if info.FileAttributes&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 || (info.FileAttributes&windows.FILE_ATTRIBUTE_DIRECTORY != 0) != directory || (!directory && info.NumberOfLinks != 1) {
		return info, ErrPrivateStorage
	}
	return info, nil
}
func requireStorageVolume(path string, h windows.Handle) error {
	var fs [64]uint16
	var flags uint32
	if e := windows.GetVolumeInformationByHandle(h, nil, 0, nil, nil, &flags, &fs[0], uint32(len(fs))); e != nil {
		return e
	}
	if windows.UTF16ToString(fs[:]) != "NTFS" || flags&windows.FILE_PERSISTENT_ACLS == 0 {
		return errors.New("UNSUPPORTED_FILESYSTEM: Windows managed storage requires local NTFS with persistent ACLs")
	}
	p, e := windowsPath(path)
	if e != nil {
		return e
	}
	var root [32768]uint16
	if e = windows.GetVolumePathName(p, &root[0], uint32(len(root))); e != nil {
		return e
	}
	if windows.GetDriveType(&root[0]) != windows.DRIVE_FIXED {
		return errors.New("UNSUPPORTED_FILESYSTEM: Windows managed storage requires a fixed local volume")
	}
	return nil
}
func openStorageDirectory(path string) (windows.Handle, error) {
	p, e := windowsPath(path)
	if e != nil {
		return 0, e
	}
	h, e := windows.CreateFile(p, windows.FILE_READ_ATTRIBUTES|windows.READ_CONTROL, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE, nil, windows.OPEN_EXISTING, windows.FILE_FLAG_OPEN_REPARSE_POINT|windows.FILE_FLAG_BACKUP_SEMANTICS, 0)
	if e != nil {
		return 0, e
	}
	if _, e = storageInfo(h, true); e != nil {
		windows.CloseHandle(h)
		return 0, e
	}
	return h, nil
}

// Keep every ancestor open without delete sharing until the operation ends.
// Broad ancestor ACLs are not modified; the managed leaf itself must be private.
func lockStorageDirectories(path string, create bool) ([]windows.Handle, error) {
	absolute, e := filepath.Abs(path)
	if e != nil {
		return nil, e
	}
	if e = validateStoragePath(absolute); e != nil {
		return nil, e
	}
	parts := []string{}
	for p := absolute; ; p = filepath.Dir(p) {
		parts = append(parts, p)
		if p == filepath.Dir(p) {
			break
		}
	}
	attrs, e := privateStorageAttributes()
	if e != nil {
		return nil, e
	}
	handles := []windows.Handle{}
	fail := func(e error) ([]windows.Handle, error) { closeStorageDirectories(handles); return nil, e }
	for i := len(parts) - 1; i >= 0; i-- {
		h, err := openStorageDirectory(parts[i])
		created := false
		if errors.Is(err, windows.ERROR_FILE_NOT_FOUND) || errors.Is(err, windows.ERROR_PATH_NOT_FOUND) {
			if !create {
				return fail(err)
			}
			if len(handles) == 0 {
				return fail(ErrPrivateStorage)
			}
			if e = requireStorageVolume(parts[i+1], handles[len(handles)-1]); e != nil {
				return fail(e)
			}
			p, e := windowsPath(parts[i])
			if e != nil {
				return fail(e)
			}
			if e = windows.CreateDirectory(p, attrs); e != nil && e != windows.ERROR_ALREADY_EXISTS {
				return fail(e)
			}
			created = e == nil
			h, err = openStorageDirectory(parts[i])
		}
		if err != nil {
			return fail(err)
		}
		handles = append(handles, h)
		if created || i == 0 {
			if _, e = auditStorageACL(h); e != nil {
				return fail(e)
			}
			if e = requireStorageVolume(parts[i], h); e != nil {
				return fail(e)
			}
		}
	}
	return handles, nil
}
func closeStorageDirectories(handles []windows.Handle) {
	for i := len(handles) - 1; i >= 0; i-- {
		windows.CloseHandle(handles[i])
	}
}
func ensurePrivateDirectory(path string) error {
	hs, e := lockStorageDirectories(path, true)
	defer closeStorageDirectories(hs)
	return e
}
func auditWorkspace(path string) error {
	hs, e := lockStorageDirectories(path, false)
	if e != nil {
		return e
	}
	defer closeStorageDirectories(hs)
	entries := 0
	return filepath.WalkDir(path, func(name string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		entries++
		if entries > 100000 {
			return errors.New("RESOURCE_EXHAUSTED: workspace ACL audit exceeds 100000 entries")
		}
		var h windows.Handle
		if entry.IsDir() {
			h, err = openStorageDirectory(name)
		} else {
			p, e := windowsPath(name)
			if e != nil {
				return e
			}
			h, err = windows.CreateFile(p, windows.FILE_READ_ATTRIBUTES|windows.READ_CONTROL, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE, nil, windows.OPEN_EXISTING, windows.FILE_FLAG_OPEN_REPARSE_POINT, 0)
		}
		if err != nil {
			return err
		}
		defer windows.CloseHandle(h)
		if _, err = storageInfo(h, entry.IsDir()); err != nil {
			return fmt.Errorf("workspace privacy audit %s: %w", name, err)
		}
		if _, err = auditStorageACL(h); err != nil {
			return fmt.Errorf("workspace privacy audit %s: %w", name, err)
		}
		return nil
	})
}
func publishFile(staged, target string, replace bool) error {
	from, e := filepath.Abs(staged)
	if e != nil {
		return e
	}
	to, e := filepath.Abs(target)
	if e != nil {
		return e
	}
	if e = validateStoragePath(from); e != nil {
		return e
	}
	if e = validateStoragePath(to); e != nil {
		return e
	}
	if strings.EqualFold(from, to) {
		return errors.New("publication source equals target")
	}
	sourceDirs, e := lockStorageDirectories(filepath.Dir(from), false)
	if e != nil {
		return e
	}
	defer closeStorageDirectories(sourceDirs)
	targetDirs, e := lockStorageDirectories(filepath.Dir(to), false)
	if e != nil {
		return e
	}
	defer closeStorageDirectories(targetDirs)
	source, e := openWindows(from, windows.GENERIC_READ|windows.GENERIC_WRITE|windows.READ_CONTROL, windows.OPEN_EXISTING, windows.FILE_SHARE_READ|windows.FILE_SHARE_DELETE, true)
	if e != nil {
		return e
	}
	defer source.Close()
	sh := windows.Handle(source.Fd())
	sourceInfo, e := storageInfo(sh, false)
	if e != nil {
		return e
	}
	sourceACL, e := auditStorageACL(sh)
	if e != nil {
		return e
	}
	targetInfo, e := storageInfo(targetDirs[len(targetDirs)-1], true)
	if e != nil {
		return e
	}
	if sourceInfo.VolumeSerialNumber != targetInfo.VolumeSerialNumber {
		return errors.New("publication requires the same volume")
	}
	if dst, e := openWindows(to, windows.FILE_READ_ATTRIBUTES|windows.READ_CONTROL, windows.OPEN_EXISTING, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE, true); e == nil {
		acl, err := auditStorageACL(windows.Handle(dst.Fd()))
		dst.Close()
		if err != nil {
			return err
		}
		if !replace {
			return os.ErrExist
		}
		// Preserve an existing record's ACL exactly. Different policies require an
		// explicit migration; do not silently transplant or broaden/narrow access.
		if acl != sourceACL {
			return ErrPrivateStorage
		}
	} else if !errors.Is(e, windows.ERROR_FILE_NOT_FOUND) {
		return e
	}
	if e = source.Sync(); e != nil {
		return e
	}
	a, e := windowsPath(from)
	if e != nil {
		return e
	}
	b, e := windowsPath(to)
	if e != nil {
		return e
	}
	flags := uint32(windows.MOVEFILE_WRITE_THROUGH)
	if replace {
		flags |= windows.MOVEFILE_REPLACE_EXISTING
	}
	// No COPY_ALLOWED flag: cross-volume moves must fail, never copy/delete.
	return windows.MoveFileEx(a, b, flags)
}
