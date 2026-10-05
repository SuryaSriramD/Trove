package platformfs

import "errors"

var ErrDirectoryDurability = errors.New("UNSUPPORTED_DURABILITY: this platform has no qualified directory flush/recovery publication protocol")
var ErrPrivateStorage = errors.New("PRIVATE_STORAGE_REQUIRED: existing storage ownership or access permissions are unsupported; no permissions were changed")

// EnsurePrivateDirectory creates missing directories restrictively. Existing
// permissions are audited, never repaired or recursively replaced.
func EnsurePrivateDirectory(path string) error { return ensurePrivateDirectory(path) }

// AuditWorkspace inspects existing Windows ownership/access without changing it.
// POSIX retains its existing workspace permission behavior.
func AuditWorkspace(path string) error { return auditWorkspace(path) }

// PublishFile flushes a regular staged file then publishes it on the same volume.
// replace=false is an OS-enforced no-replace operation. Windows uses documented
// MOVEFILE_WRITE_THROUGH; this is not a claim of POSIX directory fsync or power-loss
// qualification. The caller must own the workspace and serialize publications.
func PublishFile(staged, target string, replace bool) error {
	return publishFile(staged, target, replace)
}

// SyncDirectory must never hide an unsupported Windows directory flush.
func SyncDirectory(path string) error  { return syncDirectory(path) }
func RequireRecoveryDurability() error { return requireRecoveryDurability() }
