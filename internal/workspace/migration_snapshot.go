package workspace

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"trove.local/phase0/internal/platformfs"
)

const migrationJournalName = "migration-journal.json"
const migrationManifestName = "migration-manifest.json"
const migrationSnapshotRoot = ".migration-snapshots"
const maxMigrationObjects = 100000
const maxMigrationObjectBytes int64 = 64 << 30
const maxMigrationTotalBytes int64 = 256 << 30
const maxMigrationManifestBytes int64 = 32 << 20

var migrationIDPattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

type MigrationSnapshotManifest struct {
	FormatVersion int                     `json:"formatVersion"`
	SnapshotID    string                  `json:"snapshotId"`
	WorkspaceID   string                  `json:"workspaceId"`
	CreatedAt     string                  `json:"createdAt"`
	Before        CompatibilityDescriptor `json:"before"`
	Target        CompatibilityDescriptor `json:"target"`
	Database      BackupObject            `json:"database"`
	Objects       []BackupObject          `json:"objects"`
}

type MigrationStatus struct {
	FormatVersion  int                     `json:"formatVersion"`
	State          string                  `json:"state"` // pending or complete
	SnapshotID     string                  `json:"snapshotId"`
	SnapshotPath   string                  `json:"snapshotPath"` // computed locally, never trusted from disk
	ManifestSHA256 string                  `json:"manifestSha256"`
	Before         CompatibilityDescriptor `json:"before"`
	Target         CompatibilityDescriptor `json:"target"`
	CompletedAt    string                  `json:"completedAt,omitempty"`
	Reason         string                  `json:"reason,omitempty"`
}

type migrationJournal struct {
	FormatVersion  int                     `json:"formatVersion"`
	State          string                  `json:"state"`
	SnapshotID     string                  `json:"snapshotId"`
	ManifestSHA256 string                  `json:"manifestSha256"`
	Before         CompatibilityDescriptor `json:"before"`
	Target         CompatibilityDescriptor `json:"target"`
	CompletedAt    string                  `json:"completedAt,omitempty"`
}

func readMigrationJSON(root, name string, maximum int64, out any) ([]byte, error) {
	path, err := backupRegular(root, name)
	if err != nil {
		return nil, err
	}
	f, err := openAsset(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if st.Size() > maximum {
		return nil, Fail("INTEGRITY_ERROR", "Migration metadata is too large")
	}
	b, err := io.ReadAll(io.LimitReader(f, maximum+1))
	if err != nil {
		return nil, err
	}
	if int64(len(b)) > maximum {
		return nil, Fail("INTEGRITY_ERROR", "Migration metadata is too large")
	}
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	if err = dec.Decode(out); err != nil {
		return nil, Fail("INTEGRITY_ERROR", "Migration metadata is invalid")
	}
	if err = dec.Decode(new(any)); err != io.EOF {
		return nil, Fail("INTEGRITY_ERROR", "Migration metadata has trailing content")
	}
	return b, nil
}

func migrationDigest(b []byte) string { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }
func migrationMarker(id string) []byte {
	return []byte("Trove migration " + id + " is incomplete. Preserve this workspace and its recovery snapshot.\n")
}

func writeMigrationJSON(root, name string, value any) error {
	b, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	return atomicMigrationFile(root, name, append(b, '\n'))
}
func atomicMigrationFile(root, name string, b []byte) (err error) {
	if st, e := os.Lstat(filepath.Join(root, name)); e == nil && !st.Mode().IsRegular() {
		return Fail("INTEGRITY_ERROR", "Migration metadata must be a regular file")
	} else if e != nil && !os.IsNotExist(e) {
		return e
	}
	f, err := os.CreateTemp(root, ".migration-write-")
	if err != nil {
		return err
	}
	temp := f.Name()
	defer func() { f.Close(); os.Remove(temp) }()
	if _, err = f.Write(b); err != nil {
		return err
	}
	if err = f.Sync(); err != nil {
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	if err = os.Rename(temp, filepath.Join(root, name)); err != nil {
		return err
	}
	return syncRecoveryDirectory(root)
}
func createMigrationMarker(root string, b []byte) error {
	f, err := os.OpenFile(filepath.Join(root, "INCOMPLETE"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	_, err = f.Write(b)
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err == nil {
		err = closeErr
	}
	if err == nil {
		err = syncRecoveryDirectory(root)
	}
	return err
}
func markerExists(root string) (bool, error) {
	_, err := os.Lstat(filepath.Join(root, "INCOMPLETE"))
	if os.IsNotExist(err) {
		return false, nil
	}
	return err == nil, err
}
func verifyMigrationMarker(root string, expected []byte) error {
	p, err := backupRegular(root, "INCOMPLETE")
	if err != nil {
		return err
	}
	f, err := openAsset(p)
	if err != nil {
		return err
	}
	b, err := io.ReadAll(io.LimitReader(f, 4096))
	f.Close()
	if err != nil {
		return err
	}
	if !bytes.Equal(b, expected) {
		return Fail("INTEGRITY_ERROR", "Workspace incomplete marker does not belong to this migration")
	}
	return nil
}
func removeMigrationMarker(root string, expected []byte) error {
	if err := verifyMigrationMarker(root, expected); err != nil {
		return err
	}
	if err := os.Remove(filepath.Join(root, "INCOMPLETE")); err != nil {
		return err
	}
	return syncRecoveryDirectory(root)
}
func readMigrationJournal(root string) (*migrationJournal, error) {
	var j migrationJournal
	_, err := readMigrationJSON(root, migrationJournalName, 16384, &j)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if j.FormatVersion != 1 || !migrationIDPattern.MatchString(j.SnapshotID) || !validDigest(j.ManifestSHA256) || (j.State != "pending" && j.State != "complete") || !validMigrationTransition(j.Before, j.Target) || (j.State == "complete" && j.CompletedAt == "") {
		return nil, Fail("INTEGRITY_ERROR", "Migration journal is invalid")
	}
	return &j, nil
}
func validMigrationTransition(before, target CompatibilityDescriptor) bool {
	valid := func(d CompatibilityDescriptor) bool {
		return (d.UserVersion == 1 || d.UserVersion == 2) && d.StorageGeneration == 0 || d.UserVersion >= 3 && d.StorageGeneration >= 1
	}
	return valid(before) && valid(target) && target.UserVersion >= before.UserVersion && target.StorageGeneration >= before.StorageGeneration && target != before
}
func journalStatus(root string, j *migrationJournal) *MigrationStatus {
	reason := "Upgrade completed; recovery snapshot retained"
	if j.State == "pending" {
		reason = "Upgrade was interrupted; restore the verified snapshot into a new workspace"
	}
	return &MigrationStatus{j.FormatVersion, j.State, j.SnapshotID, filepath.Join(root, migrationSnapshotRoot, j.SnapshotID), j.ManifestSHA256, j.Before, j.Target, j.CompletedAt, reason}
}

// ReadMigrationStatus verifies the saved recovery snapshot, and reports no
// status for a workspace that has never undergone a format migration.
func ReadMigrationStatus(ctx context.Context, path string) (*MigrationStatus, error) {
	root, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	j, err := readMigrationJournal(root)
	if err != nil {
		return nil, err
	}
	if j == nil {
		if exists, e := markerExists(root); e != nil {
			return nil, e
		} else if exists {
			return nil, Fail("INTEGRITY_ERROR", "Workspace has an incomplete restore or creation marker")
		}
		return nil, nil
	}
	status := journalStatus(root, j)
	_, err = verifyJournalSnapshot(ctx, root, j)
	return status, err
}

// migrationInventory visits only canonical assets and raw retained recording
// files. It never follows links, snapshots snapshot directories, or reads siblings.
func migrationInventory(ctx context.Context, root string) ([]BackupObject, error) {
	objects := []BackupObject{}
	treeEntries := 0
	var total, inventoryBytes int64
	for _, name := range []string{"files", "recordings"} {
		start := filepath.Join(root, name)
		if _, err := os.Lstat(start); os.IsNotExist(err) {
			continue
		} else if err != nil {
			return nil, err
		}
		err := filepath.WalkDir(start, func(path string, entry fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if err := ctx.Err(); err != nil {
				return err
			}
			treeEntries++
			if treeEntries > maxMigrationObjects*4 {
				return Fail("INTEGRITY_ERROR", "Migration source tree exceeds the supported limit")
			}
			rel, err := filepath.Rel(root, path)
			if err != nil {
				return err
			}
			rel = filepath.ToSlash(rel)
			if strings.Count(rel, "/") > 32 {
				return Fail("INTEGRITY_ERROR", "Migration object nesting exceeds the supported limit")
			}
			if entry.Type()&os.ModeSymlink != 0 {
				return Fail("INTEGRITY_ERROR", "Migration sources cannot contain symbolic links")
			}
			if entry.IsDir() {
				return nil
			}
			st, err := entry.Info()
			if err != nil {
				return err
			}
			if !st.Mode().IsRegular() || st.Size() > maxMigrationObjectBytes {
				return Fail("INTEGRITY_ERROR", "Migration source is not a supported regular file")
			}
			if len(objects) >= maxMigrationObjects || total > maxMigrationTotalBytes-st.Size() {
				return Fail("INTEGRITY_ERROR", "Migration snapshot exceeds the supported limit")
			}
			hash, size, err := hashMigrationFile(ctx, path, st.Size())
			if err != nil {
				return err
			}
			total += size
			// Leave room for JSON escaping, indentation and the fixed manifest header.
			inventoryBytes += int64(len(rel)*6 + 256)
			if inventoryBytes > maxMigrationManifestBytes-4096 {
				return Fail("INTEGRITY_ERROR", "Migration inventory exceeds the manifest limit")
			}
			objects = append(objects, BackupObject{rel, hash, strconv.FormatInt(size, 10)})
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	sort.Slice(objects, func(i, j int) bool { return objects[i].Path < objects[j].Path })
	return objects, nil
}
func hashMigrationFile(ctx context.Context, path string, limit int64) (string, int64, error) {
	if limit < 0 || limit > maxMigrationObjectBytes {
		return "", 0, Fail("INTEGRITY_ERROR", "Invalid migration object size")
	}
	f, err := openAsset(path)
	if err != nil {
		return "", 0, err
	}
	defer f.Close()
	h := sha256.New()
	n, err := copyBounded(ctx, h, f, limit)
	return hex.EncodeToString(h.Sum(nil)), n, err
}
func verifyMigrationObject(ctx context.Context, root string, o BackupObject) error {
	size, err := backupSize(o.SizeBytes)
	if err != nil {
		return err
	}
	if !validDigest(o.SHA256) {
		return Fail("INTEGRITY_ERROR", "Invalid migration checksum")
	}
	path, err := backupRegular(root, o.Path)
	if err != nil {
		return err
	}
	hash, n, err := hashMigrationFile(ctx, path, size)
	if err != nil {
		return err
	}
	if hash != o.SHA256 || n != size {
		return Fail("INTEGRITY_ERROR", "Migration snapshot checksum or size does not match")
	}
	return nil
}

// Hash the bounded stream while copying; unlike the normal backup helper, this
// path keeps cancellation active throughout both copying and verification.
func copyMigrationObject(ctx context.Context, source, destination string, object BackupObject) error {
	size, err := backupSize(object.SizeBytes)
	if err != nil {
		return err
	}
	if size > maxMigrationObjectBytes || !validDigest(object.SHA256) {
		return Fail("INTEGRITY_ERROR", "Invalid migration object")
	}
	path, err := backupRegular(source, object.Path)
	if err != nil {
		return err
	}
	f, err := openAsset(path)
	if err != nil {
		return err
	}
	defer f.Close()
	out := filepath.Join(destination, filepath.FromSlash(object.Path))
	if err = safeDirectory(filepath.Dir(out)); err != nil {
		return err
	}
	dst, err := os.OpenFile(out, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	defer dst.Close()
	hash := sha256.New()
	n, err := copyBounded(ctx, io.MultiWriter(dst, hash), f, size)
	if err != nil {
		return err
	}
	if n != size || hex.EncodeToString(hash.Sum(nil)) != object.SHA256 {
		return Fail("INTEGRITY_ERROR", "Migration source changed during copying")
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	return dst.Sync()
}

func (k *Kernel) createMigrationSnapshot(ctx context.Context, before CompatibilityDescriptor) (*migrationJournal, error) {
	if err := platformfs.RequireRecoveryDurability(); err != nil {
		return nil, err
	}
	parent := filepath.Join(k.path, migrationSnapshotRoot)
	if err := safeDirectory(parent); err != nil {
		return nil, err
	}
	id := NewID()
	root := filepath.Join(parent, id)
	if err := os.Mkdir(root, 0700); err != nil {
		return nil, err
	}
	marker := []byte("Migration snapshot has not been verified.\n")
	if err := createMigrationMarker(root, marker); err != nil {
		return nil, err
	}
	m := MigrationSnapshotManifest{FormatVersion: 1, SnapshotID: id, CreatedAt: now(), Before: before, Target: CurrentCompatibility(), Objects: []BackupObject{}}
	// The kernel owns the process lock, and no mutator, recording helper or job has
	// started. Pin a read transaction for SQLite's incremental online backup API.
	source := &sqliteBackupSource{}
	var err error
	source.conn, err = k.db.Conn(ctx)
	if err != nil {
		return nil, err
	}
	defer source.Close()
	source.tx, err = source.conn.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	if err = source.tx.QueryRowContext(ctx, "SELECT id FROM workspaces LIMIT 1").Scan(&m.WorkspaceID); err != nil {
		return nil, err
	}
	var pageCount, pageSize int64
	if err = source.tx.QueryRowContext(ctx, "PRAGMA page_count").Scan(&pageCount); err != nil {
		return nil, err
	}
	if err = source.tx.QueryRowContext(ctx, "PRAGMA page_size").Scan(&pageSize); err != nil {
		return nil, err
	}
	if pageCount < 0 || pageSize < 1 || pageCount > maxMigrationObjectBytes/pageSize {
		return nil, Fail("INTEGRITY_ERROR", "Workspace database exceeds migration snapshot limit")
	}
	if err = copySQLiteBackup(ctx, source.conn, filepath.Join(root, "workspace.db"), nil); err != nil {
		return nil, err
	}
	source.Close()
	dbInfo, err := os.Stat(filepath.Join(root, "workspace.db"))
	if err != nil {
		return nil, err
	}
	hash, size, err := hashMigrationFile(ctx, filepath.Join(root, "workspace.db"), dbInfo.Size())
	if err != nil {
		return nil, err
	}
	if size > maxMigrationObjectBytes {
		return nil, Fail("INTEGRITY_ERROR", "Workspace database exceeds migration snapshot limit")
	}
	m.Database = BackupObject{"workspace.db", hash, strconv.FormatInt(size, 10)}
	m.Objects, err = migrationInventory(ctx, k.path)
	if err != nil {
		return nil, err
	}
	totalBytes := size
	for _, object := range m.Objects {
		objectBytes, e := backupSize(object.SizeBytes)
		if e != nil {
			return nil, e
		}
		if totalBytes > maxMigrationTotalBytes-objectBytes {
			return nil, Fail("INTEGRITY_ERROR", "Migration snapshot exceeds the total size limit")
		}
		totalBytes += objectBytes
	}
	for _, object := range m.Objects {
		if err = copyMigrationObject(ctx, k.path, root, object); err != nil {
			return nil, err
		}
	}
	if err = writeMigrationJSON(root, migrationManifestName, m); err != nil {
		return nil, err
	}
	if _, err = verifyMigrationSnapshot(ctx, root, true); err != nil {
		return nil, err
	}
	if err = syncRecoveryTree(root); err != nil {
		return nil, err
	}
	if err = removeMigrationMarker(root, marker); err != nil {
		return nil, err
	}
	if err = syncRecoveryDirectory(parent); err != nil {
		return nil, err
	}
	if err = syncRecoveryDirectory(k.path); err != nil {
		return nil, err
	}
	var stored MigrationSnapshotManifest
	raw, err := readMigrationJSON(root, migrationManifestName, maxMigrationManifestBytes, &stored)
	if err != nil {
		return nil, err
	}
	if string(marshal(stored)) != string(marshal(m)) {
		return nil, Fail("INTEGRITY_ERROR", "Migration snapshot changed before journal publication")
	}
	return &migrationJournal{FormatVersion: 1, State: "pending", SnapshotID: id, ManifestSHA256: migrationDigest(raw), Before: before, Target: m.Target}, nil
}

func VerifyMigrationSnapshot(ctx context.Context, snapshotDir string) (MigrationSnapshotManifest, error) {
	return verifyMigrationSnapshot(ctx, snapshotDir, false)
}
func verifyMigrationSnapshot(ctx context.Context, root string, allowIncomplete bool) (m MigrationSnapshotManifest, err error) {
	if exists, e := markerExists(root); e != nil {
		return m, e
	} else if exists && !allowIncomplete {
		return m, Fail("INTEGRITY_ERROR", "Migration snapshot is incomplete")
	}
	_, err = readMigrationJSON(root, migrationManifestName, maxMigrationManifestBytes, &m)
	if err != nil {
		return
	}
	if m.FormatVersion != 1 || !migrationIDPattern.MatchString(m.SnapshotID) || !migrationIDPattern.MatchString(m.WorkspaceID) || !validMigrationTransition(m.Before, m.Target) || m.Database.Path != "workspace.db" || len(m.Objects) > maxMigrationObjects {
		return m, Fail("INTEGRITY_ERROR", "Migration manifest is invalid")
	}
	seen := map[string]bool{"workspace.db": true, migrationManifestName: true}
	if allowIncomplete {
		seen["INCOMPLETE"] = true
	}
	var total int64
	for index, o := range append([]BackupObject{m.Database}, m.Objects...) {
		if index != 0 {
			if !(strings.HasPrefix(o.Path, "files/") || strings.HasPrefix(o.Path, "recordings/")) || seen[o.Path] || strings.Count(o.Path, "/") > 32 {
				return m, Fail("INTEGRITY_ERROR", "Migration manifest has unsafe or duplicate object paths")
			}
			seen[o.Path] = true
		}
		size, e := backupSize(o.SizeBytes)
		if e != nil {
			return m, e
		}
		if size > maxMigrationObjectBytes || total > maxMigrationTotalBytes-size {
			return m, Fail("INTEGRITY_ERROR", "Migration snapshot exceeds supported limits")
		}
		total += size
		if err = verifyMigrationObject(ctx, root, o); err != nil {
			return
		}
	}
	// Reject extra files: a restored raw recording must be exactly the inventoried
	// stream and journal, not a mixture with later or unrelated data.
	count := 0
	err = filepath.WalkDir(root, func(path string, entry fs.DirEntry, e error) error {
		if e != nil {
			return e
		}
		if e = ctx.Err(); e != nil {
			return e
		}
		count++
		if count > maxMigrationObjects*4 {
			return Fail("INTEGRITY_ERROR", "Migration snapshot tree exceeds supported limits")
		}
		rel, e := filepath.Rel(root, path)
		if e != nil {
			return e
		}
		rel = filepath.ToSlash(rel)
		if entry.Type()&os.ModeSymlink != 0 {
			return Fail("INTEGRITY_ERROR", "Migration snapshot contains a symbolic link")
		}
		if entry.IsDir() {
			if rel != "." && rel != "files" && rel != "recordings" && !strings.HasPrefix(rel, "files/") && !strings.HasPrefix(rel, "recordings/") {
				return Fail("INTEGRITY_ERROR", "Migration snapshot contains an unexpected directory")
			}
			return nil
		}
		if !seen[rel] {
			return Fail("INTEGRITY_ERROR", "Migration snapshot contains an unlisted file")
		}
		return nil
	})
	if err != nil {
		return
	}
	err = checkMigrationDatabase(ctx, root, m.Before, m.WorkspaceID)
	return
}
func verifyJournalSnapshot(ctx context.Context, root string, j *migrationJournal) (MigrationSnapshotManifest, error) {
	snapshot := filepath.Join(root, migrationSnapshotRoot, j.SnapshotID)
	// Check the parent itself, not only the final snapshot directory.
	st, err := os.Lstat(filepath.Join(root, migrationSnapshotRoot))
	if err != nil {
		return MigrationSnapshotManifest{}, err
	}
	if !st.IsDir() || st.Mode()&os.ModeSymlink != 0 {
		return MigrationSnapshotManifest{}, Fail("INTEGRITY_ERROR", "Migration snapshot parent is not a regular directory")
	}
	var expected MigrationSnapshotManifest
	raw, err := readMigrationJSON(snapshot, migrationManifestName, maxMigrationManifestBytes, &expected)
	if err != nil {
		return expected, err
	}
	if migrationDigest(raw) != j.ManifestSHA256 || expected.SnapshotID != j.SnapshotID || expected.Before != j.Before || expected.Target != j.Target {
		return expected, Fail("INTEGRITY_ERROR", "Migration journal does not match its recovery snapshot")
	}
	m, err := VerifyMigrationSnapshot(ctx, snapshot)
	if err != nil {
		return m, err
	}
	if string(marshal(m)) != string(marshal(expected)) {
		return m, Fail("INTEGRITY_ERROR", "Migration snapshot changed during journal verification")
	}
	return m, nil
}

// RestoreMigrationSnapshot preserves original bytes, including unfinished audio
// journals. Only a NEW directory is accepted; its normal open handles recovery.
func RestoreMigrationSnapshot(ctx context.Context, snapshotDir, newDestination string) (m MigrationSnapshotManifest, err error) {
	m, err = VerifyMigrationSnapshot(ctx, snapshotDir)
	if err != nil {
		return
	}
	return restoreMigrationSnapshotVerified(ctx, snapshotDir, newDestination, m)
}

// RestoreMigrationSnapshotReviewed binds approval to the exact verified manifest
// and passes that parsed value directly to copying, without a second read.
func RestoreMigrationSnapshotReviewed(ctx context.Context, snapshotDir, newDestination string, expected MigrationSnapshotManifest) (MigrationSnapshotManifest, error) {
	current, err := VerifyMigrationSnapshot(ctx, snapshotDir)
	if err != nil {
		return current, err
	}
	if string(marshal(current)) != string(marshal(expected)) {
		return current, Fail("CONFLICT", "Migration snapshot changed since its review")
	}
	return restoreMigrationSnapshotVerified(ctx, snapshotDir, newDestination, current)
}

func restoreMigrationSnapshotVerified(ctx context.Context, snapshotDir, newDestination string, m MigrationSnapshotManifest) (MigrationSnapshotManifest, error) {
	if err := platformfs.RequireRecoveryDurability(); err != nil {
		return m, err
	}
	destination, err := filepath.Abs(newDestination)
	if err != nil {
		return m, err
	}
	if err = validateRestoreSeparation(snapshotDir, destination); err != nil {
		return m, err
	}
	if err = os.Mkdir(destination, 0700); err != nil {
		return m, err
	}
	marker := []byte("Restoring a pre-migration snapshot. Do not open this directory.\n")
	if err = createMigrationMarker(destination, marker); err != nil {
		return m, err
	}
	for _, object := range append([]BackupObject{m.Database}, m.Objects...) {
		if err = copyMigrationObject(ctx, snapshotDir, destination, object); err != nil {
			return m, err
		}
		if err = verifyMigrationObject(ctx, destination, object); err != nil {
			return m, err
		}
	}
	if err = checkMigrationDatabase(ctx, destination, m.Before, m.WorkspaceID); err != nil {
		return m, err
	}
	if err = syncRecoveryTree(destination); err != nil {
		return m, err
	}
	if err = syncRecoveryDirectory(filepath.Dir(destination)); err != nil {
		return m, err
	}
	if err = removeMigrationMarker(destination, marker); err != nil {
		return m, err
	}
	return m, syncRecoveryDirectory(filepath.Dir(destination))
}

func (k *Kernel) finishMigration(ctx context.Context, j *migrationJournal) error {
	m, err := verifyJournalSnapshot(ctx, k.path, j)
	if err != nil {
		return err
	}
	if err = checkMigrationDatabase(ctx, k.path, j.Target, m.WorkspaceID); err != nil {
		return err
	}
	var id, digest string
	if err = k.db.QueryRowContext(ctx, "SELECT value FROM kernel_meta WHERE key='storageMigrationId'").Scan(&id); err != nil {
		return err
	}
	if err = k.db.QueryRowContext(ctx, "SELECT value FROM kernel_meta WHERE key='storageMigrationManifestSHA256'").Scan(&digest); err != nil {
		return err
	}
	if id != j.SnapshotID || digest != j.ManifestSHA256 {
		return Fail("INTEGRITY_ERROR", "Migration completion identity does not match its snapshot")
	}
	j.State = "complete"
	j.CompletedAt = now()
	return writeMigrationJSON(k.path, migrationJournalName, j)
}
func (k *Kernel) resumeMigrationCompletion(ctx context.Context, j *migrationJournal, incomplete bool) error {
	if j.Target.UserVersion > CurrentUserVersion || j.Target.StorageGeneration > CurrentStorageGeneration {
		return Fail("UNSUPPORTED_SCHEMA", "Interrupted upgrade requires a newer application")
	}
	if j.State == "complete" && !incomplete {
		return nil
	}
	if incomplete {
		if err := verifyMigrationMarker(k.path, migrationMarker(j.SnapshotID)); err != nil {
			return err
		}
	}
	if err := k.finishMigration(ctx, j); err != nil {
		return errors.Join(Fail("INTEGRITY_ERROR", "Workspace upgrade is incomplete; restore its verified snapshot into a new workspace"), err)
	}
	if incomplete {
		return removeMigrationMarker(k.path, migrationMarker(j.SnapshotID))
	}
	return nil
}

// A destination inside the backup would modify the supposedly immutable source.
// Resolve existing ancestors so /var aliases and symlinked parents cannot hide it.
func validateRestoreSeparation(source, destination string) error {
	sourceRoot, err := filepath.EvalSymlinks(source)
	if err != nil {
		return err
	}
	sourceRoot, err = filepath.Abs(sourceRoot)
	if err != nil {
		return err
	}
	parent, err := filepath.EvalSymlinks(filepath.Dir(destination))
	if err != nil {
		return err
	}
	target, err := filepath.Abs(filepath.Join(parent, filepath.Base(destination)))
	if err != nil {
		return err
	}
	relative, err := filepath.Rel(sourceRoot, target)
	if err != nil {
		return err
	}
	if relative == "." || (relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))) {
		return Fail("INVALID_REQUEST", "Restore destination must be outside its source backup")
	}
	return nil
}
