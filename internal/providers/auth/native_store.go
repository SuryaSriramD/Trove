package auth

import "path/filepath"

// NewNativeSecretStore shares the protected operating-system backend with
// other trusted native subsystems. Callers supply a separate absolute host
// root; no renderer-provided path or workspace directory is permitted.
func NewNativeSecretStore(hostRoot string) (SecretStore, error) {
	if !filepath.IsAbs(hostRoot) || filepath.Clean(hostRoot) != hostRoot {
		return nil, fail("STORAGE_FAILED", "Protected credential storage path is invalid")
	}
	return nativeSecretStore(hostRoot)
}
