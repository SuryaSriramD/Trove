//go:build windows

package platformfs

import (
	"errors"
	"golang.org/x/sys/windows"
	"os"
	"path/filepath"
	"testing"
)

func storageSecurity(t *testing.T, path string) string {
	t.Helper()
	p, e := windowsPath(path)
	if e != nil {
		t.Fatal(e)
	}
	h, e := windows.CreateFile(p, windows.READ_CONTROL, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE, nil, windows.OPEN_EXISTING, windows.FILE_FLAG_BACKUP_SEMANTICS|windows.FILE_FLAG_OPEN_REPARSE_POINT, 0)
	if e != nil {
		t.Fatal(e)
	}
	defer windows.CloseHandle(h)
	sd, e := windows.GetSecurityInfo(h, windows.SE_FILE_OBJECT, windows.OWNER_SECURITY_INFORMATION|windows.DACL_SECURITY_INFORMATION)
	if e != nil {
		t.Fatal(e)
	}
	return sd.String()
}
func grantTestWorldAccess(t *testing.T, path string) {
	t.Helper()
	sid, e := currentStorageSID()
	if e != nil {
		t.Fatal(e)
	}
	sd, e := windows.SecurityDescriptorFromString("O:" + sid.String() + "D:P(A;OICI;FA;;;" + sid.String() + ")(A;OICI;FR;;;WD)")
	if e != nil {
		t.Fatal(e)
	}
	acl, _, e := sd.DACL()
	if e != nil {
		t.Fatal(e)
	}
	if e = windows.SetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION, nil, nil, acl, nil); e != nil {
		t.Fatal(e)
	}
}
func TestWindowsManagedDirectoryStartsPrivateAndDoesNotRewriteExistingACL(t *testing.T) {
	parent := publicationDirectory(t)
	grantTestWorldAccess(t, parent)
	before := storageSecurity(t, parent)
	child := filepath.Join(parent, "new-private")
	if e := EnsurePrivateDirectory(child); e != nil {
		t.Fatal(e)
	}
	if e := AuditWorkspace(child); e != nil {
		t.Fatal(e)
	}
	if e := EnsurePrivateDirectory(parent); !errors.Is(e, ErrPrivateStorage) {
		t.Fatal("broad existing directory accepted", e)
	}
	if got := storageSecurity(t, parent); got != before {
		t.Fatal("existing ACL changed")
	}
}
func TestWindowsWorkspaceAuditFindsBroadChildWithoutRepair(t *testing.T) {
	dir := publicationDirectory(t)
	file := filepath.Join(dir, "workspace.db")
	if e := os.WriteFile(file, []byte("unchanged"), 0600); e != nil {
		t.Fatal(e)
	}
	grantTestWorldAccess(t, file)
	before := storageSecurity(t, file)
	if e := AuditWorkspace(dir); !errors.Is(e, ErrPrivateStorage) {
		t.Fatal("broad file access accepted", e)
	}
	if storageSecurity(t, file) != before {
		t.Fatal("file ACL changed")
	}
	if b, e := os.ReadFile(file); e != nil || string(b) != "unchanged" {
		t.Fatal("file bytes changed", e)
	}
}
func TestWindowsPublicationRefusesACLChange(t *testing.T) {
	dir := publicationDirectory(t)
	source, target := filepath.Join(dir, "source"), filepath.Join(dir, "target")
	os.WriteFile(source, []byte("new"), 0600)
	os.WriteFile(target, []byte("old"), 0600)
	grantTestWorldAccess(t, target)
	before := storageSecurity(t, target)
	if e := PublishFile(source, target, true); !errors.Is(e, ErrPrivateStorage) {
		t.Fatal("broad target accepted", e)
	}
	if storageSecurity(t, target) != before {
		t.Fatal("target policy changed")
	}
	if b, e := os.ReadFile(target); e != nil || string(b) != "old" {
		t.Fatal("target replaced", e)
	}
}

func TestWindowsStorageRejectsAmbiguousWin32Names(t *testing.T) {
	for _, path := range []string{`C:\workspace.`, `C:\workspace `, `C:\AUX.txt`, `C:\nested\COM1`, `C:\nested\LPT².log`, `C:\file:stream`, `\\server\share\file`, `C:\nested\bad?name`} {
		if err := validateStoragePath(path); err == nil {
			t.Fatalf("ambiguous managed path accepted: %q", path)
		}
	}
	if err := validateStoragePath(`C:\Users\Owner\résumé 🦉\workspace`); err != nil {
		t.Fatal(err)
	}
}
