package workspace

import (
	"context"
	"database/sql"
	"errors"
	"net/url"
	"path/filepath"
	"strconv"
)

// Canonical schema changes, including additive package initializers, MUST raise
// CurrentStorageGeneration. New binaries snapshot before advancing either value.
// Version 3 fences pre-framework applications, whose maximum version was 2.
const CurrentUserVersion = 3
const CurrentStorageGeneration = 2

type CompatibilityDescriptor struct {
	UserVersion       int `json:"userVersion"`
	StorageGeneration int `json:"storageGeneration"`
}

func CurrentCompatibility() CompatibilityDescriptor {
	return CompatibilityDescriptor{CurrentUserVersion, CurrentStorageGeneration}
}

func compatibility(ctx context.Context, q interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}) (d CompatibilityDescriptor, err error) {
	if err = q.QueryRowContext(ctx, "PRAGMA user_version").Scan(&d.UserVersion); err != nil {
		return
	}
	// Check the format before querying tables a future version might remove.
	if d.UserVersion > CurrentUserVersion {
		return d, Fail("UNSUPPORTED_SCHEMA", "Workspace requires a newer application")
	}
	if d.UserVersion < 1 {
		return d, Fail("INVALID_REQUEST", "Not a Trove workspace")
	}
	if d.UserVersion < 3 {
		return d, nil
	}
	var value string
	if err = q.QueryRowContext(ctx, "SELECT value FROM kernel_meta WHERE key='storageGeneration'").Scan(&value); err != nil {
		return d, Fail("INTEGRITY_ERROR", "Workspace storage generation is missing")
	}
	d.StorageGeneration, err = strconv.Atoi(value)
	if err != nil || d.StorageGeneration < 1 || strconv.Itoa(d.StorageGeneration) != value {
		return d, Fail("INTEGRITY_ERROR", "Workspace storage generation is invalid")
	}
	if d.StorageGeneration > CurrentStorageGeneration {
		return d, Fail("UNSUPPORTED_SCHEMA", "Workspace requires a newer storage generation")
	}
	return d, nil
}

func (k *Kernel) DescribeCompatibility(ctx context.Context) (CompatibilityDescriptor, error) {
	return compatibility(ctx, k.db)
}

// ReadWorkspaceCompatibility neither creates a database nor runs initializers.
// It describes storage even when a migration marker fences normal opening.
func ReadWorkspaceCompatibility(ctx context.Context, path string) (CompatibilityDescriptor, error) {
	db, err := openMigrationDatabase(path)
	if err != nil {
		return CompatibilityDescriptor{}, err
	}
	defer db.Close()
	return compatibility(ctx, db)
}

func openMigrationDatabase(path string) (*sql.DB, error) {
	if _, err := backupRegular(path, "workspace.db"); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite3", (&url.URL{Scheme: "file", Path: filepath.Join(path, "workspace.db")}).String()+"?mode=ro&_foreign_keys=on&_busy_timeout=5000")
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	return db, nil
}

func checkMigrationDatabase(ctx context.Context, path string, expected CompatibilityDescriptor, workspaceID string) error {
	db, err := openMigrationDatabase(path)
	if err != nil {
		return err
	}
	defer db.Close()
	d, err := compatibility(ctx, db)
	if err != nil {
		return err
	}
	if d != expected {
		return Fail("INTEGRITY_ERROR", "Migration database format does not match its manifest")
	}
	var id, integrity string
	if err = db.QueryRowContext(ctx, "SELECT id FROM workspaces LIMIT 1").Scan(&id); err != nil {
		return err
	}
	if id != workspaceID {
		return Fail("INTEGRITY_ERROR", "Migration database workspace does not match its manifest")
	}
	if err = db.QueryRowContext(ctx, "PRAGMA integrity_check").Scan(&integrity); err != nil {
		return err
	}
	if integrity != "ok" {
		return Fail("INTEGRITY_ERROR", "Migration database failed integrity verification")
	}
	rows, err := db.QueryContext(ctx, "PRAGMA foreign_key_check")
	if err != nil {
		return err
	}
	defer rows.Close()
	if rows.Next() {
		return Fail("INTEGRITY_ERROR", "Migration database has broken references")
	}
	return rows.Err()
}

func (k *Kernel) stampCompatibility(ctx context.Context, migrationID, manifestDigest string) error {
	tx, err := k.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for key, value := range map[string]string{"storageGeneration": strconv.Itoa(CurrentStorageGeneration), "storageMigrationId": migrationID, "storageMigrationManifestSHA256": manifestDigest} {
		if _, err = tx.ExecContext(ctx, "INSERT INTO kernel_meta(key,value) VALUES(?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value", key, value); err != nil {
			return err
		}
	}
	if _, err = tx.ExecContext(ctx, "PRAGMA user_version="+strconv.Itoa(CurrentUserVersion)); err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	// Every connection uses synchronous=FULL; checkpoint and sync the durable DB
	// before a marker can be removed. No service jobs have started at this point.
	var busy, log, checkpointed int
	if err = k.db.QueryRowContext(ctx, "PRAGMA wal_checkpoint(TRUNCATE)").Scan(&busy, &log, &checkpointed); err != nil {
		return err
	}
	if busy != 0 {
		return errors.New("migration checkpoint is busy")
	}
	f, err := openAsset(filepath.Join(k.path, "workspace.db"))
	if err != nil {
		return err
	}
	defer f.Close()
	if err = f.Sync(); err != nil {
		return err
	}
	return syncRecoveryDirectory(k.path)
}
