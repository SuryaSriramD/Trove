//go:build windows

package auth

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"unsafe"

	"github.com/google/uuid"
	"golang.org/x/sys/windows"
)

type dpapiStore struct{ root string }

func nativeSecretStore(root string) (SecretStore, error) { return &dpapiStore{root: root}, nil }
func (s *dpapiStore) path(id string) (string, error) {
	parsed, err := uuid.Parse(id)
	if err != nil || parsed.String() != id {
		return "", fail("CREDENTIAL_STORE_FAILED", "Credential identity is invalid")
	}
	return filepath.Join(s.root, ".credential-"+id+".dpapi"), nil
}
func (s *dpapiStore) entropy(id string) []byte {
	sum := sha256.Sum256([]byte("org.trove.siwc.dpapi.v1\x00" + s.root + "\x00" + id))
	return sum[:]
}
func dataBlob(data []byte) *windows.DataBlob {
	return &windows.DataBlob{Size: uint32(len(data)), Data: &data[0]}
}
func dpapi(data, entropy []byte, encrypt bool) ([]byte, error) {
	if len(data) == 0 || len(data) > 262144 {
		return nil, fmt.Errorf("invalid protected record length")
	}
	var output windows.DataBlob
	var err error
	if encrypt {
		err = windows.CryptProtectData(dataBlob(data), nil, dataBlob(entropy), 0, nil, windows.CRYPTPROTECT_UI_FORBIDDEN, &output)
	} else {
		err = windows.CryptUnprotectData(dataBlob(data), nil, dataBlob(entropy), 0, nil, windows.CRYPTPROTECT_UI_FORBIDDEN, &output)
	}
	runtime.KeepAlive(data)
	runtime.KeepAlive(entropy)
	if err != nil {
		return nil, err
	}
	if output.Data == nil {
		return nil, fmt.Errorf("missing protected record")
	}
	defer windows.LocalFree(windows.Handle(uintptr(unsafe.Pointer(output.Data))))
	if output.Size > 262144 {
		return nil, fmt.Errorf("protected record exceeds limit")
	}
	memory := unsafe.Slice(output.Data, int(output.Size))
	defer clear(memory)
	return append([]byte(nil), memory...), nil
}
func (s *dpapiStore) Get(ctx context.Context, id string) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	path, err := s.path(id)
	if err != nil {
		return nil, err
	}
	encrypted, err := readPrivateFile(path, 262144)
	if os.IsNotExist(err) {
		return nil, ErrSecretMissing
	}
	if err != nil {
		return nil, fail("CREDENTIAL_STORE_FAILED", "Windows protected account storage could not read this account")
	}
	data, err := dpapi(encrypted, s.entropy(id), false)
	if err != nil || len(data) == 0 || len(data) > 131072 {
		clear(data)
		return nil, fail("CREDENTIAL_STORE_FAILED", "Windows DPAPI could not unlock this account for the current user")
	}
	if err := ctx.Err(); err != nil {
		clear(data)
		return nil, err
	}
	return data, nil
}
func (s *dpapiStore) Put(ctx context.Context, id string, data []byte) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	path, err := s.path(id)
	if err != nil {
		return err
	}
	if len(data) == 0 || len(data) > 131072 {
		return fail("CREDENTIAL_STORE_FAILED", "Credential record is invalid")
	}
	encrypted, err := dpapi(data, s.entropy(id), true)
	if err != nil {
		return fail("CREDENTIAL_STORE_FAILED", "Windows DPAPI could not protect this account")
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	if err = writePrivateFile(s.root, filepath.Base(path), encrypted); err != nil {
		return fail("CREDENTIAL_STORE_FAILED", "Windows protected account storage could not save this account")
	}
	return nil
}
func (s *dpapiStore) Delete(ctx context.Context, id string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	path, err := s.path(id)
	if err != nil {
		return err
	}
	// Validate the current object before removing only this registration's file.
	h, err := openPrivate(path, windows.GENERIC_READ, windows.OPEN_EXISTING, false)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fail("CREDENTIAL_STORE_FAILED", "Windows protected account storage could not clear this account")
	}
	windows.CloseHandle(h)
	if err = os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fail("CREDENTIAL_STORE_FAILED", "Windows protected account storage could not clear this account")
	}
	return nil
}
