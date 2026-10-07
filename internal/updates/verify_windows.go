package updates

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
)

var trustDLL = windows.NewLazySystemDLL("wintrust.dll")
var acquireCatalog = trustDLL.NewProc("CryptCATAdminAcquireContext2")
var releaseCatalog = trustDLL.NewProc("CryptCATAdminReleaseContext")
var calculateCatalogHash = trustDLL.NewProc("CryptCATAdminCalcHashFromFileHandle2")
var providerData = trustDLL.NewProc("WTHelperProvDataFromStateData")
var providerSigner = trustDLL.NewProc("WTHelperGetProvSignerFromChain")
var providerCertificate = trustDLL.NewProc("WTHelperGetProvCertFromChain")

type trustCatalogInfo struct {
	Size               uint32
	Version            uint32
	CatalogPath        *uint16
	MemberTag          *uint16
	MemberPath         *uint16
	MemberFile         windows.Handle
	CalculatedHash     *byte
	CalculatedHashSize uint32
	CatalogContext     uintptr
	CatalogAdmin       windows.Handle
}
type providerCertificatePrefix struct {
	Size        uint32
	Certificate *windows.CertContext
}

func (v WindowsVerifier) Inspect(ctx context.Context, path string) (Application, error) {
	pins, err := v.Policy.windowsPins()
	if err != nil {
		return Application{}, err
	}
	if runtime.GOARCH != "amd64" {
		return Application{}, errors.New("Windows publisher verification currently requires the x64 target")
	}
	path, err = filepath.Abs(path)
	if err != nil {
		return Application{}, err
	}
	return inspectPortable(ctx, path, "windows", func(root *os.Root, raw []byte, id Identity, authenticated map[string]string) error {
		if !pins[id.Publisher.ID] {
			return errors.New("Windows publisher certificate is not pinned by this installation")
		}
		catalog, err := readReleaseFile(root, WindowsCatalogName, 16<<20)
		if err != nil {
			return err
		}
		hash := sha256.Sum256(catalog)
		authenticated[WindowsCatalogName] = hex.EncodeToString(hash[:])
		// The native handle protects the same manifest bytes which the strict parser
		// accepted. Catalog membership and its verified signer are checked together.
		leaf, err := verifyCatalogMember(ctx, path, raw, catalog)
		if err != nil {
			return err
		}
		if leaf != id.Publisher.ID || !pins[leaf] {
			return errors.New("verified Windows signer differs from the declared/pinned publisher")
		}
		return nil
	})
}
func openTrustFile(path string) (*os.File, error) {
	name, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return nil, err
	}
	// Deny concurrent writes/deletes while Windows evaluates these exact files.
	handle, err := windows.CreateFile(name, windows.GENERIC_READ, windows.FILE_SHARE_READ, nil, windows.OPEN_EXISTING, windows.FILE_ATTRIBUTE_NORMAL|windows.FILE_FLAG_OPEN_REPARSE_POINT, 0)
	if err != nil {
		return nil, err
	}
	var info windows.ByHandleFileInformation
	if err = windows.GetFileInformationByHandle(handle, &info); err != nil || info.FileAttributes&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 || info.FileAttributes&windows.FILE_ATTRIBUTE_DIRECTORY != 0 {
		windows.CloseHandle(handle)
		return nil, errors.New("signature input is not a regular native file")
	}
	return os.NewFile(uintptr(handle), path), nil
}
func verifyCatalogMember(ctx context.Context, directory string, expectedManifest, expectedCatalog []byte) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	for _, proc := range []*windows.LazyProc{acquireCatalog, releaseCatalog, calculateCatalogHash, providerData, providerSigner, providerCertificate} {
		if err := proc.Find(); err != nil {
			return "", errors.New("required Windows trust API unavailable; no fallback")
		}
	}
	manifestPath := filepath.Join(directory, ReleaseManifestName)
	catalogPath := filepath.Join(directory, WindowsCatalogName)
	manifest, err := openTrustFile(manifestPath)
	if err != nil {
		return "", err
	}
	defer manifest.Close()
	catalog, err := openTrustFile(catalogPath)
	if err != nil {
		return "", err
	}
	defer catalog.Close()
	for _, item := range []struct {
		file     *os.File
		expected []byte
	}{{manifest, expectedManifest}, {catalog, expectedCatalog}} {
		actual, err := osReadBoundedTrust(item.file, int64(len(item.expected)))
		if err != nil || !bytes.Equal(actual, item.expected) {
			return "", errors.New("native signature input changed")
		}
		if _, err = item.file.Seek(0, 0); err != nil {
			return "", err
		}
	}
	algorithm, _ := windows.UTF16PtrFromString("SHA256")
	var admin windows.Handle
	ok, _, nativeErr := acquireCatalog.Call(uintptr(unsafe.Pointer(&admin)), 0, uintptr(unsafe.Pointer(algorithm)), 0, 0)
	if ok == 0 || admin == 0 {
		return "", fmt.Errorf("Windows SHA256 catalog context unavailable: %v", nativeErr)
	}
	defer releaseCatalog.Call(uintptr(admin), 0)
	hash := make([]byte, sha256.Size)
	size := uint32(len(hash))
	ok, _, nativeErr = calculateCatalogHash.Call(uintptr(admin), manifest.Fd(), uintptr(unsafe.Pointer(&size)), uintptr(unsafe.Pointer(&hash[0])), 0)
	if ok == 0 || size != sha256.Size {
		return "", fmt.Errorf("Windows catalog member hashing failed: %v", nativeErr)
	}
	// For this fixed JSON member the SHA256 catalog hash must cover all bytes.
	expectedHash := sha256.Sum256(expectedManifest)
	if !bytes.Equal(hash, expectedHash[:]) {
		return "", errors.New("catalog hash does not cover the exact manifest bytes")
	}
	tag, _ := windows.UTF16PtrFromString(strings.ToUpper(hex.EncodeToString(hash)))
	memberName, _ := windows.UTF16PtrFromString(manifestPath)
	catalogName, _ := windows.UTF16PtrFromString(catalogPath)
	info := trustCatalogInfo{CatalogPath: catalogName, MemberTag: tag, MemberPath: memberName, MemberFile: windows.Handle(manifest.Fd()), CalculatedHash: &hash[0], CalculatedHashSize: size, CatalogAdmin: admin}
	info.Size = uint32(unsafe.Sizeof(info))
	data := windows.WinTrustData{UIChoice: windows.WTD_UI_NONE, RevocationChecks: windows.WTD_REVOKE_WHOLECHAIN, UnionChoice: windows.WTD_CHOICE_CATALOG, FileOrCatalogOrBlobOrSgnrOrCert: unsafe.Pointer(&info), StateAction: windows.WTD_STATEACTION_VERIFY, ProvFlags: windows.WTD_REVOCATION_CHECK_CHAIN_EXCLUDE_ROOT | windows.WTD_CACHE_ONLY_URL_RETRIEVAL | windows.WTD_DISABLE_MD2_MD4 | windows.WTD_MOTW, UIContext: windows.WTD_UICONTEXT_INSTALL}
	data.Size = uint32(unsafe.Sizeof(data))
	oid := []byte("1.3.6.1.4.1.311.72.1.1\x00")
	strong := windows.CertStrongSignPara{InfoChoice: 2, InfoOrSerializedInfoOrOID: unsafe.Pointer(&oid[0])}
	strong.Size = uint32(unsafe.Sizeof(strong))
	settings := windows.WinTrustSignatureSettings{Flags: 1, Index: 0, CryptoPolicy: &strong}
	settings.Size = uint32(unsafe.Sizeof(settings))
	data.SignatureSettings = &settings
	// Cache-only revocation failure remains a failure; never silently disable
	// revocation or contact an account/network service as an implicit fallback.
	err = windows.WinVerifyTrustEx(windows.HWND(windows.InvalidHandle), &windows.WINTRUST_ACTION_GENERIC_VERIFY_V2, &data)
	defer func() {
		data.StateAction = windows.WTD_STATEACTION_CLOSE
		_ = windows.WinVerifyTrustEx(windows.HWND(windows.InvalidHandle), &windows.WINTRUST_ACTION_GENERIC_VERIFY_V2, &data)
	}()
	if err != nil || settings.VerifiedSigIndex != 0 {
		return "", errors.New("Windows catalog/member Authenticode policy failed; trusted chain and cached revocation evidence are required")
	}
	if err = ctx.Err(); err != nil {
		return "", err
	}
	native, _, _ := providerData.Call(uintptr(data.StateData))
	if native == 0 {
		return "", errors.New("verified Windows trust state unavailable")
	}
	signer, _, _ := providerSigner.Call(native, 0, 0, 0)
	if signer == 0 {
		return "", errors.New("verified Windows signer unavailable")
	}
	cert, _, _ := providerCertificate.Call(signer, 0)
	if cert == 0 {
		return "", errors.New("verified Windows leaf certificate unavailable")
	}
	prefix := (*providerCertificatePrefix)(unsafe.Pointer(cert))
	if prefix.Size < uint32(unsafe.Sizeof(*prefix)) || prefix.Certificate == nil || prefix.Certificate.EncodedCert == nil || prefix.Certificate.Length == 0 || prefix.Certificate.Length > 1<<20 {
		return "", errors.New("invalid verified Windows certificate bounds")
	}
	leaf := sha256.Sum256(unsafe.Slice(prefix.Certificate.EncodedCert, int(prefix.Certificate.Length)))
	runtime.KeepAlive(oid)
	runtime.KeepAlive(strong)
	runtime.KeepAlive(settings)
	runtime.KeepAlive(info)
	runtime.KeepAlive(hash)
	runtime.KeepAlive(manifest)
	runtime.KeepAlive(catalog)
	return hex.EncodeToString(leaf[:]), nil
}

func osReadBoundedTrust(file *os.File, limit int64) ([]byte, error) {
	if limit < 1 || limit > 16<<20 {
		return nil, errors.New("native trust input exceeds bounds")
	}
	body, err := io.ReadAll(io.LimitReader(file, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(body)) != limit {
		return nil, errors.New("native trust input length changed")
	}
	return body, nil
}
