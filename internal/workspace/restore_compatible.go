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

// RestoreBackupCompatible is exclusively for a paired application rollback. It
// retains the reviewed storage format, and never opens a kernel, runs a schema
// initializer, rebuilds an index or starts a job. Extend the frozen cleanup
// contract explicitly before supporting another storage generation here.
func RestoreBackupCompatible(ctx context.Context, source, destination string, expected BackupManifest, expectedFormat CompatibilityDescriptor) (result RestoreResult, err error) {
	if expectedFormat != (CompatibilityDescriptor{3, 1}) {
		return result, Fail("UNSUPPORTED_SCHEMA", "Compatible rollback cleanup is unavailable for this storage format")
	}
	current, err := VerifyBackup(ctx, source)
	if err != nil {
		return result, err
	}
	if string(marshal(current)) != string(marshal(expected)) {
		return result, Fail("CONFLICT", "Backup changed since its review")
	}
	actual, err := ReadWorkspaceCompatibility(ctx, source)
	if err != nil {
		return result, err
	}
	if actual != expectedFormat {
		return result, Fail("CONFLICT", "Backup storage format does not match the paired application")
	}
	if err = platformfs.RequireRecoveryDurability(); err != nil {
		return result, err
	}
	destination, err = filepath.Abs(destination)
	if err != nil {
		return result, err
	}
	if _, err = os.Lstat(destination); err == nil {
		return result, os.ErrExist
	} else if !errors.Is(err, os.ErrNotExist) {
		return result, err
	}
	if err = validateRestoreSeparation(source, destination); err != nil {
		return result, err
	}
	stage, err := os.MkdirTemp(filepath.Dir(destination), ".trove-compatible-restore-")
	if err != nil {
		return result, err
	}
	defer os.RemoveAll(stage)
	marker := []byte("Compatible rollback restore is incomplete.\n")
	if err = createMigrationMarker(stage, marker); err != nil {
		return result, err
	}
	objects := []BackupObject{current.Database}
	for _, a := range current.Assets {
		objects = append(objects, BackupObject{a.Path, a.ContentHash, a.SizeBytes})
	}
	objects = append(objects, current.Recordings...)
	seen := map[string]bool{}
	for _, object := range objects {
		// Several file identities can share the same immutable content-addressed file.
		if seen[object.Path] {
			continue
		}
		seen[object.Path] = true
		if err = copyMigrationObject(ctx, source, stage, object); err != nil {
			return result, err
		}
		if err = verifyMigrationObject(ctx, stage, object); err != nil {
			return result, err
		}
	}
	if err = checkMigrationDatabase(ctx, stage, expectedFormat, current.WorkspaceID); err != nil {
		return result, err
	}
	if err = clearCopiedRecoveryBinding(ctx, stage, current.Database); err != nil {
		return result, err
	}
	if err = revokeCompatibleRestorePermissions(ctx, stage); err != nil {
		return result, err
	}
	if err = checkMigrationDatabase(ctx, stage, expectedFormat, current.WorkspaceID); err != nil {
		return result, err
	}
	if err = ctx.Err(); err != nil {
		return result, err
	}
	if err = syncRecoveryTree(stage); err != nil {
		return result, err
	}
	// Reserve a new destination without ever replacing an existing workspace.
	if err = os.Mkdir(destination, 0700); err != nil {
		return result, err
	}
	if err = createMigrationMarker(destination, marker); err != nil {
		return result, err
	}
	if err = syncRecoveryDirectory(filepath.Dir(destination)); err != nil {
		return result, err
	}
	entries, err := os.ReadDir(stage)
	if err != nil {
		return result, err
	}
	for _, entry := range entries {
		if entry.Name() == "INCOMPLETE" {
			continue
		}
		if err = os.Rename(filepath.Join(stage, entry.Name()), filepath.Join(destination, entry.Name())); err != nil {
			return result, err
		}
	}
	receipt := map[string]any{"backupId": current.BackupID, "workspaceId": current.WorkspaceID, "restoredAt": now(), "sourceManifest": current, "compatibility": expectedFormat, "permissionsRevoked": true, "indexRebuilt": false}
	if err = durableRecoveryFile(filepath.Join(destination, "restore-receipt.json"), marshal(receipt)); err != nil {
		return result, err
	}
	if err = checkMigrationDatabase(ctx, destination, expectedFormat, current.WorkspaceID); err != nil {
		return result, err
	}
	if err = syncRecoveryTree(destination); err != nil {
		return result, err
	}
	if err = removeMigrationMarker(destination, marker); err != nil {
		return result, err
	}
	if err = syncRecoveryDirectory(filepath.Dir(destination)); err != nil {
		return result, err
	}
	return RestoreResult{WorkspaceID: current.WorkspaceID, Destination: destination, BackupID: current.BackupID, Assets: len(current.Assets), RecordingObjects: len(current.Recordings), IndexRebuilt: false}, nil
}

// These declarations are frozen for format 3/generation 1. Table shape checks
// reject missing or altered constraints, generated columns, views and virtual
// tables before cleanup; no guessed fallback cleanup is allowed.
var compatiblePermissionTables = map[string]string{
	"approvals": `CREATE TABLE approvals (
 approval_id TEXT PRIMARY KEY, actor_id TEXT NOT NULL, scope_grant_id TEXT NOT NULL,
 policy_revision INTEGER NOT NULL CHECK(typeof(policy_revision)='integer' AND policy_revision BETWEEN 1 AND 9223372036854775807), command_hash TEXT NOT NULL CHECK(length(command_hash)=64),
 expected_revisions_json TEXT NOT NULL CHECK(json_valid(expected_revisions_json)),
 expires_at TEXT NOT NULL, approved_by TEXT NOT NULL,
 state TEXT NOT NULL CHECK(state IN ('pending','approved','consumed','revoked','expired'))
)`,
	"restore_previews":    `CREATE TABLE restore_previews(id TEXT PRIMARY KEY,page_id TEXT NOT NULL,restore_revision TEXT NOT NULL,actor_id TEXT NOT NULL,grant_id TEXT NOT NULL,guards_json TEXT NOT NULL,snapshot_hash TEXT NOT NULL,expires_at TEXT NOT NULL,consumed INTEGER NOT NULL DEFAULT 0)`,
	"file_ingests":        `CREATE TABLE file_ingests(handle TEXT PRIMARY KEY,file_id TEXT NOT NULL,content_hash TEXT NOT NULL,original_name TEXT NOT NULL,media_type TEXT NOT NULL,size_bytes INTEGER NOT NULL,actor_id TEXT NOT NULL,scope_grant_id TEXT NOT NULL,expires_at TEXT NOT NULL)`,
	"external_roots":      `CREATE TABLE external_roots(id TEXT PRIMARY KEY,path TEXT NOT NULL UNIQUE,identity TEXT NOT NULL,repository INTEGER NOT NULL,approved INTEGER NOT NULL DEFAULT 1,last_scan TEXT NOT NULL DEFAULT '')`,
	"external_chunks":     `CREATE TABLE external_chunks(source_id TEXT NOT NULL REFERENCES external_sources(id),ordinal INTEGER NOT NULL,start_line INTEGER NOT NULL,end_line INTEGER NOT NULL,symbol TEXT NOT NULL,text TEXT NOT NULL,PRIMARY KEY(source_id,ordinal))`,
	"job_authorizations":  `CREATE TABLE job_authorizations(job_id TEXT PRIMARY KEY REFERENCES jobs(job_id),actor_id TEXT NOT NULL,grant_id TEXT NOT NULL,policy_revision TEXT NOT NULL,state TEXT NOT NULL CHECK(state IN('approved','revoked')))`,
	"backup_destinations": `CREATE TABLE backup_destinations(id TEXT PRIMARY KEY,actor_id TEXT NOT NULL,grant_id TEXT NOT NULL,path TEXT NOT NULL,expires_at TEXT NOT NULL,job_id TEXT REFERENCES jobs(job_id))`,
	"template_previews":   `CREATE TABLE template_previews(id TEXT PRIMARY KEY,actor_id TEXT NOT NULL,scope_grant_id TEXT NOT NULL,policy_revision TEXT NOT NULL,plan_json TEXT NOT NULL,expires_at TEXT NOT NULL,consumed INTEGER NOT NULL DEFAULT 0)`,
}

func normalizeCompatibleDDL(s string) string { return strings.Join(strings.Fields(s), " ") }
func revokeCompatibleRestorePermissions(ctx context.Context, root string) (err error) {
	db, err := sql.Open("sqlite3", (&url.URL{Scheme: "file", Path: filepath.Join(root, "workspace.db")}).String()+"?mode=rw&_journal_mode=DELETE&_synchronous=FULL&_foreign_keys=on&_busy_timeout=5000")
	if err != nil {
		return err
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	if _, err = db.ExecContext(ctx, "PRAGMA trusted_schema=OFF"); err != nil {
		return err
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var total int64
	for name, expected := range compatiblePermissionTables {
		var kind, definition string
		if err = tx.QueryRowContext(ctx, "SELECT type,sql FROM sqlite_schema WHERE name=?", name).Scan(&kind, &definition); err != nil {
			return Fail("UNSUPPORTED_SCHEMA", "Rollback permission tables are unavailable")
		}
		if kind != "table" || normalizeCompatibleDDL(definition) != normalizeCompatibleDDL(expected) {
			return Fail("UNSUPPORTED_SCHEMA", "Rollback permission schema is not supported")
		}
		var triggers int
		if err = tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM sqlite_schema WHERE type='trigger' AND tbl_name=? COLLATE NOCASE", name).Scan(&triggers); err != nil {
			return err
		}
		if triggers != 0 {
			return Fail("INTEGRITY_ERROR", "Rollback permission cleanup cannot run schema triggers")
		}
		// name is selected only from the fixed map, never from the backup's SQL.
		var count int64
		if err = tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+name).Scan(&count); err != nil {
			return err
		}
		total += count
		if total > 1000000 {
			return Fail("RESOURCE_EXHAUSTED", "Rollback permission cleanup exceeds the supported row limit")
		}
	}
	// Referential NO ACTION/RESTRICT can fail cleanup but cannot rewrite canonical
	// data. Reject cascades or SET actions targeting any permission table.
	rows, err := tx.QueryContext(ctx, `SELECT f."table",f.on_update,f.on_delete FROM sqlite_schema s JOIN pragma_foreign_key_list(s.name) f WHERE s.type='table' LIMIT 10001`)
	if err != nil {
		return err
	}
	count := 0
	for rows.Next() {
		var table, update, del string
		if err = rows.Scan(&table, &update, &del); err != nil {
			rows.Close()
			return err
		}
		count++
		if _, cleanup := compatiblePermissionTables[strings.ToLower(table)]; cleanup && ((update != "NO ACTION" && update != "RESTRICT") || (del != "NO ACTION" && del != "RESTRICT")) {
			rows.Close()
			return Fail("INTEGRITY_ERROR", "Rollback cleanup has an unsafe foreign-key dependency")
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	if count > 10000 {
		return Fail("RESOURCE_EXHAUSTED", "Rollback schema exceeds the supported relationship limit")
	}
	if _, err = tx.ExecContext(ctx, `UPDATE approvals SET state='revoked' WHERE state IN('pending','approved');
 UPDATE restore_previews SET consumed=1;
 DELETE FROM file_ingests;
 UPDATE external_roots SET approved=0;
 DELETE FROM external_chunks;
 UPDATE job_authorizations SET state='revoked';
 DELETE FROM backup_destinations;
 UPDATE template_previews SET consumed=1;`); err != nil {
		return err
	}
	if err := validateExternalContextRestore(ctx, tx); err != nil {
		return err
	}
	if err = resetConnectorRestore(ctx, tx); err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	if err = db.Close(); err != nil {
		return err
	}
	file, err := openAsset(filepath.Join(root, "workspace.db"))
	if err != nil {
		return err
	}
	defer file.Close()
	if err = file.Sync(); err != nil {
		return err
	}
	return syncRecoveryDirectory(root)
}
