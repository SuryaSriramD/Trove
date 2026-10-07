package workspace

import (
	"context"
	"database/sql"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"trove.local/phase0/internal/platformfs"
)

const recoveryOperationKey = "recoveryPublicationOperationId"

// The binding is checkpointed in the baseline database before publication and
// never changes during normal editing. immutable reads deliberately ignore WAL
// so admission cannot create sidecars or consume unverified recovery bytes.
func readRecoveryOperation(ctx context.Context, path string) (string, error) {
	db, err := sql.Open("sqlite3", (&url.URL{Scheme: "file", Path: filepath.Join(path, "workspace.db")}).String()+"?mode=ro&immutable=1&_query_only=true")
	if err != nil {
		return "", err
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	var exists int
	if err = db.QueryRowContext(ctx, "SELECT COUNT(*) FROM sqlite_schema WHERE type='table' AND name='kernel_meta'").Scan(&exists); err != nil {
		return "", err
	}
	if exists == 0 {
		return "", nil
	}
	var id string
	err = db.QueryRowContext(ctx, "SELECT value FROM kernel_meta WHERE key=?", recoveryOperationKey).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return id, err
}

func inspectRecoveryPublication(path string, existing bool, reviewed bool) (string, platformfs.RecoveryStatus, error) {
	id := ""
	var err error
	if existing {
		id, err = readRecoveryOperation(context.Background(), path)
		if err != nil {
			return id, platformfs.RecoveryStatus{}, err
		}
	}
	status, err := platformfs.InspectRecoveryAdmission(path, id)
	if err != nil {
		return id, status, err
	}
	if status.State == "complete" && reviewed {
		return id, status, errors.Join(platformfs.ErrRecoveryIncomplete, errors.New("recovery baseline requires ordinary startup activation before reviewed rollback"))
	}
	return id, status, nil
}

func readImmutableRecoveryCompatibility(ctx context.Context, path string) (CompatibilityDescriptor, error) {
	db, err := sql.Open("sqlite3", (&url.URL{Scheme: "file", Path: filepath.Join(path, "workspace.db")}).String()+"?mode=ro&immutable=1&_query_only=true")
	if err != nil {
		return CompatibilityDescriptor{}, err
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	return compatibility(ctx, db)
}

// Only verified private copies use this helper. Backup and migration snapshot
// bytes are immutable source evidence; the copy needs a fresh publication, not
// authority inherited from the original directory. Recheck the exact manifest
// hash under its owner lock before the only permitted metadata edit. Canonical
// rows, workspace identity, schema version and historical migration IDs remain.
func clearCopiedRecoveryBinding(ctx context.Context, directory string, object BackupObject) (err error) {
	root, err := os.OpenRoot(directory)
	if err != nil {
		return err
	}
	defer root.Close()
	owner, err := platformfs.AcquireRootLock(root, ".owner.lock")
	if err != nil {
		return err
	}
	defer platformfs.ReleaseLock(owner)
	if err = verifyBackupObject(ctx, directory, object); err != nil {
		return err
	}
	id, err := readRecoveryOperation(ctx, directory)
	if err != nil || id == "" {
		return err
	}
	db, err := sql.Open("sqlite3", (&url.URL{Scheme: "file", Path: filepath.Join(directory, "workspace.db")}).String()+"?mode=rw&_journal_mode=DELETE&_synchronous=FULL&_foreign_keys=on")
	if err != nil {
		return err
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	if _, err = db.ExecContext(ctx, "PRAGMA trusted_schema=OFF"); err != nil {
		return err
	}
	var definition string
	if err = db.QueryRowContext(ctx, "SELECT sql FROM sqlite_schema WHERE type='table' AND name='kernel_meta'").Scan(&definition); err != nil {
		return err
	}
	if normalizeCompatibleDDL(definition) != normalizeCompatibleDDL("CREATE TABLE kernel_meta(key TEXT PRIMARY KEY,value TEXT NOT NULL)") {
		return Fail("INTEGRITY_ERROR", "Recovery metadata schema is not supported")
	}
	var unsafe int
	if err = db.QueryRowContext(ctx, "SELECT COUNT(*) FROM sqlite_schema WHERE type='trigger' AND tbl_name='kernel_meta' COLLATE NOCASE").Scan(&unsafe); err != nil {
		return err
	}
	if unsafe != 0 {
		return Fail("INTEGRITY_ERROR", "Recovery metadata cleanup cannot execute triggers")
	}
	if err = db.QueryRowContext(ctx, `SELECT COUNT(*) FROM sqlite_schema s JOIN pragma_foreign_key_list(s.name) f WHERE s.type='table' AND f."table"='kernel_meta' COLLATE NOCASE`).Scan(&unsafe); err != nil {
		return err
	}
	if unsafe != 0 {
		return Fail("INTEGRITY_ERROR", "Recovery metadata has an unsupported foreign-key dependency")
	}
	if _, err = db.ExecContext(ctx, "DELETE FROM kernel_meta WHERE key=?", recoveryOperationKey); err != nil {
		return err
	}
	return db.Close()
}

func newRecoveryOperation() string { return strings.ReplaceAll(NewID(), "-", "") }
