//go:build windows

package auth

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/uuid"
	"golang.org/x/sys/windows"
)

func windowsAuthRoot(t *testing.T) string {
	t.Helper()
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(base, "accounts")
	if err = ensurePrivateRoot(root); err != nil {
		t.Fatal(err)
	}
	return root
}
func TestWindowsDPAPIIdentityBindingAndTamper(t *testing.T) {
	root := windowsAuthRoot(t)
	s := &dpapiStore{root: root}
	ctx := context.Background()
	id := uuid.NewString()
	data := bytes.Repeat([]byte("synthetic OAuth record "), 200)
	if err := s.Put(ctx, id, data); err != nil {
		t.Fatal(err)
	}
	got, err := s.Get(ctx, id)
	if err != nil || !bytes.Equal(got, data) {
		t.Fatal("DPAPI round trip", err)
	}
	path, _ := s.path(id)
	encrypted, err := readPrivateFile(path, 262144)
	if err != nil || bytes.Contains(encrypted, []byte("synthetic OAuth record")) {
		t.Fatal("plaintext persisted", err)
	}
	other := uuid.NewString()
	otherPath, _ := s.path(other)
	if err = writePrivateFile(root, filepath.Base(otherPath), encrypted); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Get(ctx, other); err == nil {
		t.Fatal("another registration decrypted copied record")
	}
	encrypted[len(encrypted)/2] ^= 1
	if err = writePrivateFile(root, filepath.Base(path), encrypted); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Get(ctx, id); err == nil {
		t.Fatal("tampered DPAPI record accepted")
	}
	if err = s.Put(ctx, id, []byte("rotated synthetic record")); err != nil {
		t.Fatal(err)
	}
	if err = s.Delete(ctx, id); err != nil {
		t.Fatal(err)
	}
	if err = s.Delete(ctx, id); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Get(ctx, id); err != ErrSecretMissing {
		t.Fatal("deleted record returned", err)
	}
}
func TestWindowsAuthRootRejectsPublicACLAndConcurrentOwner(t *testing.T) {
	root := windowsAuthRoot(t)
	owner, err := lockRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	if second, err := lockRoot(root); err == nil {
		second.Close()
		t.Fatal("second process lock accepted")
	}
	owner.Close()
	owner, err = lockRoot(root)
	if err != nil {
		t.Fatal("lock not released", err)
	}
	owner.Close()
	descriptor, err := windows.SecurityDescriptorFromString("D:P(A;OICI;FA;;;WD)")
	if err != nil {
		t.Fatal(err)
	}
	acl, _, err := descriptor.DACL()
	if err != nil {
		t.Fatal(err)
	}
	if err = windows.SetNamedSecurityInfo(root, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION, nil, nil, acl, nil); err != nil {
		t.Fatal(err)
	}
	if owner, err := lockRoot(root); err == nil {
		owner.Close()
		t.Fatal("public ACL accepted")
	}
}
func TestWindowsPrivateMetadataAndReparseRefusal(t *testing.T) {
	root := windowsAuthRoot(t)
	if err := writePrivateFile(root, "registrations.json", []byte("first")); err != nil {
		t.Fatal(err)
	}
	if err := writePrivateFile(root, "registrations.json", []byte("second")); err != nil {
		t.Fatal(err)
	}
	data, err := readPrivateFile(filepath.Join(root, "registrations.json"), 100)
	if err != nil || string(data) != "second" {
		t.Fatal("atomic replace", err)
	}
	link := filepath.Join(filepath.Dir(root), "linked")
	if err = os.Symlink(root, link); err != nil {
		t.Skip("Windows symlink privilege unavailable; reparse qualification remains required")
	}
	if owner, err := lockRoot(link); err == nil {
		owner.Close()
		t.Fatal("reparse root accepted")
	}
}
