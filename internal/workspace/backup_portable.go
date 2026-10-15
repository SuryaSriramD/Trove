package workspace

// Portable native backups use age's passphrase format around an exact native
// snapshot. A decrypted stream is never passed to a parser: its authenticated
// EOF must first be reached in private staging.
import (
	"archive/tar"
	"bytes"
	"context"
	"database/sql"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"filippo.io/age"
	"trove.local/phase0/contracts"
	"trove.local/phase0/internal/platformfs"
)

const portableStagePrefix = ".trove-portable-v1-"
const maxPortableBytes int64 = 1 << 40
const maxPortableObjects = 100000

// BackupProgress contains no content, filesystem paths, or passphrase. Counts
// are decimal strings so native and JavaScript callers retain large values.
type BackupProgress = contracts.ServiceNativeBackupProgress

type BackupProgressFunc func(BackupProgress)

func requirePortableSpace(path string, required int64) error {
	available, err := platformfs.AvailableBytes(path)
	if err != nil {
		return err
	}
	if required < 0 || uint64(required)+(64<<20) > available {
		return Fail("RESOURCE_EXHAUSTED", "Not enough free space for private backup staging; free space or choose another destination and retry")
	}
	return nil
}

func (k *Kernel) portableSnapshotEstimate(ctx context.Context) (int64, error) {
	var size int64
	if err := k.db.QueryRowContext(ctx, "SELECT COALESCE(SUM(size_bytes),0) FROM files").Scan(&size); err != nil {
		return 0, err
	}
	for _, name := range []string{"workspace.db", "workspace.db-wal"} {
		if info, err := os.Stat(filepath.Join(k.path, name)); err == nil {
			size += info.Size()
		} else if !errors.Is(err, os.ErrNotExist) {
			return 0, err
		}
	}
	entries, err := os.ReadDir(filepath.Join(k.path, "recordings"))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return 0, err
	}
	for _, entry := range entries {
		if entry.Type().IsRegular() {
			info, e := entry.Info()
			if e != nil {
				return 0, e
			}
			size += info.Size()
		}
	}
	return size, nil
}

func backupProgress(fn BackupProgressFunc, phase string, done, total int64) {
	if fn != nil {
		fn(BackupProgress{1, phase, strconv.FormatInt(done, 10), strconv.FormatInt(total, 10)})
	}
}

type portableStage struct {
	path string
	root *os.Root
	lock *os.File
}

func newPortableStage(parent string) (*portableStage, error) {
	if err := platformfs.EnsurePrivateDirectory(parent); err != nil {
		return nil, err
	}
	path, err := os.MkdirTemp(parent, portableStagePrefix)
	if err != nil {
		return nil, err
	}
	root, err := os.OpenRoot(path)
	if err != nil {
		os.RemoveAll(path)
		return nil, err
	}
	lock, err := platformfs.AcquireRootLock(root, ".owner.lock")
	if err != nil {
		root.Close()
		os.RemoveAll(path)
		return nil, err
	}
	s := &portableStage{path, root, lock}
	if err = os.WriteFile(filepath.Join(path, "stage-v1"), []byte("Trove private backup staging v1\n"), 0600); err != nil {
		s.close()
		return nil, err
	}
	return s, nil
}
func (s *portableStage) close() {
	if s == nil {
		return
	}
	if s.root != nil {
		s.root.Close()
		s.root = nil
	}
	// Keep the lock while removing plaintext. Active stages cannot be reclaimed.
	if s.path != "" {
		os.RemoveAll(s.path)
		s.path = ""
	}
	if s.lock != nil {
		platformfs.ReleaseLock(s.lock)
		s.lock = nil
	}
}

// CleanupAbandonedBackupStages only removes our marked, private, unlocked
// stages. A process crash releases the OS lock; another running process keeps it.
func CleanupAbandonedBackupStages(parent string) (int, error) {
	if err := platformfs.EnsurePrivateDirectory(parent); err != nil {
		return 0, err
	}
	entries, err := os.ReadDir(parent)
	if err != nil {
		return 0, err
	}
	n := 0
	for _, entry := range entries {
		if !entry.IsDir() || entry.Type()&os.ModeSymlink != 0 || !strings.HasPrefix(entry.Name(), portableStagePrefix) {
			continue
		}
		path := filepath.Join(parent, entry.Name())
		root, e := os.OpenRoot(path)
		if e != nil {
			continue
		}
		dir, e := root.Open(".")
		if e == nil {
			e = platformfs.AuditPrivateHandle(dir, true)
			dir.Close()
		}
		if e != nil {
			root.Close()
			continue
		}
		marker, e := root.ReadFile("stage-v1")
		if e != nil || string(marker) != "Trove private backup staging v1\n" {
			root.Close()
			continue
		}
		lock, e := platformfs.AcquireRootLock(root, ".owner.lock")
		if e != nil {
			root.Close()
			continue
		}
		root.Close()
		e = os.RemoveAll(path)
		platformfs.ReleaseLock(lock)
		if e != nil {
			return n, e
		}
		n++
	}
	return n, nil
}

type progressWriter struct {
	destination  io.Writer
	fn           BackupProgressFunc
	phase        string
	count, total int64
}

func (w *progressWriter) Write(b []byte) (int, error) {
	n, err := w.destination.Write(b)
	w.count += int64(n)
	backupProgress(w.fn, w.phase, w.count, w.total)
	return n, err
}

func portableInventory(m BackupManifest) []string {
	set := map[string]bool{"manifest.json": true, "workspace.db": true}
	for _, a := range m.Assets {
		set[a.Path] = true
	}
	for _, r := range m.Recordings {
		set[r.Path] = true
	}
	names := make([]string, 0, len(set))
	for name := range set {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// CreateEncryptedBackup never persists the passphrase or accepts it through the
// service/job dispatcher. The native adapter supplies a fresh destination only.
func (k *Kernel) CreateEncryptedBackup(ctx context.Context, e contracts.ServiceExecutionContext, destination, passphrase, stagingParent string, progress BackupProgressFunc) (manifest BackupManifest, err error) {
	defer func() {
		if err != nil && manifest.BackupID != "" {
			_ = k.write(context.WithoutCancel(ctx), func(tx *sql.Tx) error {
				_, e := tx.Exec("UPDATE backups SET state='failed',updated_at=? WHERE id=?", now(), manifest.BackupID)
				return e
			})
		}
	}()
	if err = requireOwnerScope(k, e); err != nil {
		return manifest, err
	}
	if passphrase == "" || len(passphrase) > 4096 {
		return manifest, Fail("INVALID_REQUEST", "A non-empty passphrase of at most 4096 bytes is required")
	}
	if _, err = os.Lstat(destination); err == nil {
		return manifest, os.ErrExist
	} else if !errors.Is(err, os.ErrNotExist) {
		return manifest, err
	}
	stage, err := newPortableStage(stagingParent)
	if err != nil {
		return manifest, err
	}
	defer stage.close()
	estimate, err := k.portableSnapshotEstimate(ctx)
	if err != nil {
		return manifest, err
	}
	if err = requirePortableSpace(stage.path, estimate); err != nil {
		return manifest, err
	}
	if err = requirePortableSpace(filepath.Dir(destination), estimate*2); err != nil {
		return manifest, err
	}
	backupProgress(progress, "snapshot", 0, 0)
	snapshot := filepath.Join(stage.path, "snapshot")
	manifest, err = k.createBackup(ctx, e, snapshot, func() error { backupProgress(progress, "snapshot", 0, 0); return ctx.Err() })
	if err != nil {
		return manifest, err
	}
	if _, err = VerifyBackup(ctx, snapshot); err != nil {
		return manifest, err
	}
	if err = ctx.Err(); err != nil {
		return manifest, err
	}
	recipient, err := age.NewScryptRecipient(passphrase)
	passphrase = ""
	if err != nil {
		return manifest, Fail("INVALID_REQUEST", "Invalid backup passphrase")
	}
	// Reserve a sibling partial file exclusively; publication never overwrites a
	// file selected since the native dialog. Partial ciphertext has no plaintext.
	partial, err := os.CreateTemp(filepath.Dir(destination), ".trove-encrypted-*.partial")
	if err != nil {
		return manifest, err
	}
	defer func() { partial.Close(); os.Remove(partial.Name()) }()
	encrypted, err := age.Encrypt(partial, recipient)
	if err != nil {
		return manifest, err
	}
	if err = ctx.Err(); err != nil {
		return manifest, err
	}
	names := portableInventory(manifest)
	var total int64 = 1024
	for _, name := range names {
		info, e := os.Stat(filepath.Join(snapshot, filepath.FromSlash(name)))
		if e != nil {
			return manifest, e
		}
		total += ((info.Size()+511)/512 + 1) * 512
	}
	if total > maxPortableBytes {
		return manifest, Fail("RESOURCE_EXHAUSTED", "Portable backup exceeds 1 TiB")
	}
	if err = requirePortableSpace(filepath.Dir(destination), total+(total/4096)); err != nil {
		return manifest, err
	}
	w := &progressWriter{destination: encrypted, fn: progress, phase: "encrypting", total: total}
	archive := tar.NewWriter(w)
	for _, name := range names {
		if err = ctx.Err(); err != nil {
			return manifest, err
		}
		path, e := backupRegular(snapshot, name)
		if e != nil {
			return manifest, e
		}
		input, e := openAsset(path)
		if e != nil {
			return manifest, e
		}
		info, e := input.Stat()
		if e == nil {
			e = archive.WriteHeader(&tar.Header{Name: name, Mode: 0600, Size: info.Size(), Typeflag: tar.TypeReg, Format: tar.FormatUSTAR})
		}
		if e == nil {
			_, e = copyBounded(ctx, archive, input, info.Size())
		}
		input.Close()
		if e != nil {
			return manifest, e
		}
	}
	if err = archive.Close(); err == nil {
		err = encrypted.Close()
	}
	if err == nil {
		err = partial.Sync()
	}
	if err == nil {
		err = partial.Close()
	}
	if err != nil {
		return manifest, err
	}
	if err = ctx.Err(); err != nil {
		return manifest, err
	}
	// Link is an atomic no-replace publication on the same filesystem.
	if err = os.Link(partial.Name(), destination); err != nil {
		return manifest, err
	}
	if err = os.Remove(partial.Name()); err != nil {
		return manifest, err
	}
	if err = platformfs.SyncDirectory(filepath.Dir(destination)); err != nil {
		return manifest, err
	}
	backupProgress(progress, "complete", total, total)
	return manifest, nil
}

// VerifiedEncryptedBackup owns authenticated, verified private data, never a
// passphrase. Native preview dismissal/expiry/shutdown must call Close.
type VerifiedEncryptedBackup struct {
	Manifest BackupManifest
	stage    *portableStage
}

func (v *VerifiedEncryptedBackup) Close() {
	if v != nil {
		v.stage.close()
		v.stage = nil
	}
}
func (v *VerifiedEncryptedBackup) Restore(ctx context.Context, destination string, progress BackupProgressFunc) (RestoreResult, error) {
	if v == nil || v.stage == nil {
		return RestoreResult{}, Fail("CONFLICT", "Encrypted backup review is closed")
	}
	var size int64 = parseSize(v.Manifest.Database.SizeBytes)
	for _, a := range v.Manifest.Assets {
		size += parseSize(a.SizeBytes)
	}
	for _, r := range v.Manifest.Recordings {
		size += parseSize(r.SizeBytes)
	}
	if err := requirePortableSpace(filepath.Dir(destination), size*2); err != nil {
		return RestoreResult{}, err
	}
	backupProgress(progress, "restoring", 0, 0)
	return RestoreBackupReviewed(ctx, filepath.Join(v.stage.path, "snapshot"), destination, v.Manifest)
}

func OpenEncryptedBackup(ctx context.Context, source, passphrase, stagingParent string, progress BackupProgressFunc) (verified *VerifiedEncryptedBackup, err error) {
	if passphrase == "" || len(passphrase) > 4096 {
		return nil, Fail("INVALID_REQUEST", "A non-empty passphrase of at most 4096 bytes is required")
	}
	stage, err := newPortableStage(stagingParent)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err != nil {
			stage.close()
		}
	}()
	input, err := openAsset(source)
	if err != nil {
		return nil, err
	}
	defer input.Close()
	info, err := input.Stat()
	if err != nil {
		return nil, err
	}
	if info.Size() > maxPortableBytes+(512<<20) {
		return nil, Fail("RESOURCE_EXHAUSTED", "Encrypted backup exceeds 1 TiB")
	}
	if err = requirePortableSpace(stage.path, info.Size()*2); err != nil {
		return nil, err
	}
	identity, err := age.NewScryptIdentity(passphrase)
	passphrase = ""
	if err != nil {
		return nil, Fail("INVALID_REQUEST", "Invalid backup passphrase")
	}
	// age's default ceiling permits a hostile header to request far more work.
	// Trove writes the library default (18); cap accepted work at 20 (~1 GiB).
	identity.SetMaxWorkFactor(20)
	backupProgress(progress, "authenticating", 0, info.Size())
	plaintext, err := age.Decrypt(input, identity)
	if err != nil {
		return nil, Fail("INTEGRITY_ERROR", "Unable to authenticate backup: incorrect passphrase or damaged archive")
	}
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	spool, err := os.OpenFile(filepath.Join(stage.path, "authenticated.tar"), os.O_CREATE|os.O_EXCL|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	defer spool.Close()
	_, err = copyBounded(ctx, &progressWriter{destination: spool, fn: progress, phase: "authenticating", total: info.Size()}, plaintext, maxPortableBytes+(64<<20))
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, errors.Join(Fail("INTEGRITY_ERROR", "Encrypted backup did not authenticate completely"), err)
	}
	if err = spool.Sync(); err != nil {
		return nil, err
	}
	if _, err = spool.Seek(0, io.SeekStart); err != nil {
		return nil, err
	}
	// No tar header, manifest JSON, database, or preview is read above this line.
	backupProgress(progress, "verifying", 0, 0)
	snapshot := filepath.Join(stage.path, "snapshot")
	if err = os.Mkdir(snapshot, 0700); err != nil {
		return nil, err
	}
	if err = extractPortableArchive(ctx, spool, snapshot); err != nil {
		return nil, err
	}
	manifest, err := VerifyBackup(ctx, snapshot)
	if err != nil {
		return nil, err
	}
	if _, err = backupCleanupInventory(snapshot, manifest); err != nil {
		return nil, Fail("INTEGRITY_ERROR", "Portable backup contains undeclared objects")
	}
	spool.Close()
	if err = os.Remove(filepath.Join(stage.path, "authenticated.tar")); err != nil {
		return nil, err
	}
	backupProgress(progress, "review", 0, 0)
	return &VerifiedEncryptedBackup{Manifest: manifest, stage: stage}, nil
}

func extractPortableArchive(ctx context.Context, input io.Reader, destination string) error {
	archive := tar.NewReader(input)
	seen := map[string]bool{}
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		h, err := archive.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		if h.Typeflag != tar.TypeReg || h.Name == "" || filepath.IsAbs(h.Name) || strings.Contains(h.Name, "\\") || filepath.ToSlash(filepath.Clean(h.Name)) != h.Name || strings.HasPrefix(h.Name, "../") || seen[h.Name] || len(seen) >= maxPortableObjects || h.Size < 0 || h.Size > maxPortableBytes {
			return Fail("INTEGRITY_ERROR", "Unsafe portable backup archive entry")
		}
		if h.Name != "manifest.json" && h.Name != "workspace.db" && !strings.HasPrefix(h.Name, "files/") && !strings.HasPrefix(h.Name, "recordings/") {
			return Fail("INTEGRITY_ERROR", "Unexpected portable backup archive entry")
		}
		seen[h.Name] = true
		path := filepath.Join(destination, filepath.FromSlash(h.Name))
		if err = safeDirectory(filepath.Dir(path)); err != nil {
			return err
		}
		output, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if err != nil {
			return err
		}
		n, err := copyBounded(ctx, output, archive, h.Size)
		if err == nil && n != h.Size {
			err = io.ErrUnexpectedEOF
		}
		if err == nil {
			err = output.Sync()
		}
		closeErr := output.Close()
		if err == nil {
			err = closeErr
		}
		if err != nil {
			return err
		}
	}
	// Do not accept concatenated archives or non-zero bytes after tar EOF.
	trailing, err := io.ReadAll(io.LimitReader(input, 1025))
	if err != nil {
		return err
	}
	if len(trailing) > 1024 || !bytes.Equal(trailing, make([]byte, len(trailing))) {
		return Fail("INTEGRITY_ERROR", "Trailing portable backup data")
	}
	if !seen["manifest.json"] || !seen["workspace.db"] {
		return Fail("INTEGRITY_ERROR", "Portable backup inventory is incomplete")
	}
	return nil
}

// PortableBackupSummary is explicit about recovery coverage and exclusions.
func PortableBackupSummary() contracts.ServiceNativeBackupSummary {
	return contracts.ServiceNativeBackupSummary{
		Version: 1, Format: "age-passphrase/native-tar-v1",
		Included:    []string{"Complete workspace database, including canonical content, retained history, import evidence, derived search tables and saved approval/job state", "Managed files, transcripts and retained recordings", "Approved memory and workspace settings"},
		Excluded:    []string{"Secrets held in the native credential store and this backup's passphrase", "Downloaded model binaries", "Original external files not copied into managed storage (indexed excerpts can remain in the database snapshot)"},
		Restoration: []string{"Search indexes are rebuilt; copied external-folder index chunks are removed", "Pending and approved action approvals are revoked; prior restore and file-ingestion handles are invalidated", "External folder roots require fresh authorization; native credential-store secrets are not supplied by this archive"},
	}
}
