package workspace

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func compatibleBackupFixture(t *testing.T) (*Kernel, string, BackupManifest) {
	t.Helper()
	ctx := context.Background()
	k, e := testKernel(t)
	page := createPage(t, k, e, "Rollback canonical source")
	attachTestFile(t, k, e, page, stageTestFile(t, k, e, "rollback attachment bytes"), false)
	draft, audio, err := k.BeginRecording(ctx, e, "Rollback retained recording")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = audio.Write(make([]byte, 640)); err != nil {
		t.Fatal(err)
	}
	audio.Sync()
	if _, err = k.CheckpointRecording(ctx, e, draft.ID, true, "Synthetic capture ended"); err != nil {
		t.Fatal(err)
	}
	audio.Close()
	stageTestFile(t, k, e, "uncommitted file permission")
	job := enqueueTestJob(t, k, e, "BACKUP_WORKSPACE", map[string]any{"kind": "backup", "destinationHandle": NewID()})
	statements := []struct {
		sql  string
		args []any
	}{
		{"INSERT INTO approvals VALUES(?,?,?,?,?,?,?,?,'approved')", []any{NewID(), e.Actor.ActorId, e.ScopeGrantId, 1, strings.Repeat("a", 64), "[]", timeAfterMinutes(60), e.Actor.ActorId}},
		{"INSERT INTO restore_previews VALUES(?,?,?,?,?,?,?,?,0)", []any{NewID(), page, "1", e.Actor.ActorId, e.ScopeGrantId, "[]", strings.Repeat("b", 64), timeAfterMinutes(60)}},
		{"INSERT INTO template_previews VALUES(?,?,?,?,?,?,0)", []any{NewID(), e.Actor.ActorId, e.ScopeGrantId, "1", "{}", timeAfterMinutes(60)}},
		{"INSERT INTO backup_destinations VALUES(?,?,?,?,?,?)", []any{NewID(), e.Actor.ActorId, e.ScopeGrantId, "/synthetic/never-accessed", timeAfterMinutes(60), job.JobId}},
		{"INSERT INTO external_roots VALUES('fixture-root','/synthetic/never-accessed','fixture-identity',0,1,'')", nil},
		{"INSERT INTO external_sources VALUES('fixture-source','fixture-root','fixture.txt','fixture-source-identity','hash','current','text','','1')", nil},
		{"INSERT INTO external_chunks VALUES('fixture-source',0,1,1,'','old filesystem context')", nil},
	}
	for _, s := range statements {
		if _, err = k.db.Exec(s.sql, s.args...); err != nil {
			t.Fatal(err)
		}
	}
	backup := filepath.Join(t.TempDir(), "backup")
	manifest, err := k.CreateBackup(ctx, e, backup)
	if err != nil {
		t.Fatal(err)
	}
	return k, backup, manifest
}

func TestRestoreBackupCompatiblePreservesFormatContentAndRevokesPermissions(t *testing.T) {
	ctx := context.Background()
	k, source, manifest := compatibleBackupFixture(t)
	tables := []string{"pages", "blocks", "files", "attachments", "committed_events", "page_snapshots", "mutation_history", "jobs", "search_chunks", "kernel_meta"}
	expected := map[string]string{}
	for _, table := range tables {
		expected[table] = retainedTable(t, k, table)
	}
	previous := kernelInitializers
	called := false
	kernelInitializers = append(append([]func(context.Context, *Kernel) error{}, previous...), func(context.Context, *Kernel) error {
		called = true
		return errors.New("compatible rollback must not initialize storage")
	})
	destination := filepath.Join(t.TempDir(), "rollback")
	result, err := RestoreBackupCompatible(ctx, source, destination, manifest, CurrentCompatibility())
	kernelInitializers = previous
	if err != nil {
		t.Fatal(err)
	}
	if called || result.IndexRebuilt {
		t.Fatal("rollback invoked initialization or rebuilt indexes", called, result)
	}
	d, err := ReadWorkspaceCompatibility(ctx, destination)
	if err != nil || d != CurrentCompatibility() {
		t.Fatal(d, err)
	}
	db, err := openMigrationDatabase(destination)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for _, table := range tables {
		if got := retainedTable(t, &Kernel{db: db}, table); got != expected[table] {
			t.Fatalf("canonical/derived table changed: %s", table)
		}
	}
	for _, query := range []string{
		"SELECT COUNT(*) FROM approvals WHERE state IN('pending','approved')",
		"SELECT COUNT(*) FROM restore_previews WHERE consumed=0",
		"SELECT COUNT(*) FROM template_previews WHERE consumed=0",
		"SELECT COUNT(*) FROM file_ingests",
		"SELECT COUNT(*) FROM external_roots WHERE approved!=0",
		"SELECT COUNT(*) FROM external_chunks",
		"SELECT COUNT(*) FROM job_authorizations WHERE state!='revoked'",
		"SELECT COUNT(*) FROM backup_destinations",
	} {
		var count int
		if err = db.QueryRow(query).Scan(&count); err != nil || count != 0 {
			t.Fatal("permission survived", query, count, err)
		}
	}
	for _, a := range manifest.Assets {
		if err = verifyMigrationObject(ctx, destination, BackupObject{a.Path, a.ContentHash, a.SizeBytes}); err != nil {
			t.Fatal(err)
		}
	}
	for _, r := range manifest.Recordings {
		if err = verifyMigrationObject(ctx, destination, r); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = os.Lstat(filepath.Join(destination, migrationSnapshotRoot)); !os.IsNotExist(err) {
		t.Fatal("rollback created a migration snapshot", err)
	}
	if _, err = VerifyBackup(ctx, source); err != nil {
		t.Fatal("rollback changed backup", err)
	}
	if _, err = RestoreBackupCompatible(ctx, source, destination, manifest, CurrentCompatibility()); !os.IsExist(err) {
		t.Fatal("rollback replaced existing destination", err)
	}
}

func rewriteCompatibleBackupDatabase(t *testing.T, source string, m BackupManifest, statement string) BackupManifest {
	t.Helper()
	db, err := sql.Open("sqlite3", filepath.Join(source, "workspace.db"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(statement); err != nil {
		t.Fatal(err)
	}
	db.Close()
	hash, size, err := hashFile(context.Background(), filepath.Join(source, "workspace.db"))
	if err != nil {
		t.Fatal(err)
	}
	m.Database.SHA256 = hash
	m.Database.SizeBytes = strconv.FormatInt(size, 10)
	writeBackupManifest(t, source, m)
	return m
}

func TestRestoreBackupCompatibleRejectsUnreviewedAndUnsupportedFormats(t *testing.T) {
	for _, scenario := range []string{"manifest", "pair_format", "legacy_source", "future_version", "future_generation"} {
		t.Run(scenario, func(t *testing.T) {
			_, source, manifest := compatibleBackupFixture(t)
			format := CurrentCompatibility()
			switch scenario {
			case "manifest":
				manifest.BackupID = NewID()
			case "pair_format":
				format.StorageGeneration++
			case "legacy_source":
				manifest = rewriteCompatibleBackupDatabase(t, source, manifest, "PRAGMA user_version=2")
			case "future_version":
				manifest = rewriteCompatibleBackupDatabase(t, source, manifest, "PRAGMA user_version=4")
			case "future_generation":
				manifest = rewriteCompatibleBackupDatabase(t, source, manifest, "UPDATE kernel_meta SET value='2' WHERE key='storageGeneration'")
			}
			destination := filepath.Join(t.TempDir(), "refused")
			if _, err := RestoreBackupCompatible(context.Background(), source, destination, manifest, format); err == nil {
				t.Fatal("unsupported pairing restored")
			}
			if _, err := os.Lstat(destination); !os.IsNotExist(err) {
				t.Fatal("invalid pairing published destination", err)
			}
		})
	}
}

func TestRestoreBackupCompatibleRefusesUnsafeCleanupSchema(t *testing.T) {
	for _, scenario := range []string{"missing_table", "changed_table", "trigger", "cascade", "cascade_case"} {
		t.Run(scenario, func(t *testing.T) {
			_, source, manifest := compatibleBackupFixture(t)
			statement := map[string]string{
				"missing_table": "DROP TABLE template_previews",
				"changed_table": "ALTER TABLE file_ingests ADD COLUMN unexpected TEXT",
				"trigger":       "CREATE TRIGGER hostile_approval_cleanup AFTER UPDATE ON approvals BEGIN UPDATE pages SET title='hostile cleanup'; END",
				"cascade":       "CREATE TABLE hostile_dependency(id TEXT REFERENCES external_roots(id) ON DELETE CASCADE)",
				"cascade_case":  "CREATE TABLE hostile_dependency(id TEXT REFERENCES BACKUP_DESTINATIONS(id) ON DELETE CASCADE)",
			}[scenario]
			manifest = rewriteCompatibleBackupDatabase(t, source, manifest, statement)
			destination := filepath.Join(t.TempDir(), "refused")
			if _, err := RestoreBackupCompatible(context.Background(), source, destination, manifest, CurrentCompatibility()); err == nil {
				t.Fatal("unsafe cleanup schema accepted")
			}
			if _, err := os.Lstat(destination); !os.IsNotExist(err) {
				t.Fatal("unsafe cleanup published destination", err)
			}
			if _, err := VerifyBackup(context.Background(), source); err != nil {
				t.Fatal("failure changed source", err)
			}
		})
	}
}

func TestRestoreBackupCompatibleCannotPublishInsideSource(t *testing.T) {
	_, source, manifest := compatibleBackupFixture(t)
	alias := filepath.Join(t.TempDir(), "alias")
	if err := os.Symlink(source, alias); err != nil {
		t.Fatal(err)
	}
	for _, root := range []string{source, alias} {
		if _, err := RestoreBackupCompatible(context.Background(), source, filepath.Join(root, "nested"), manifest, CurrentCompatibility()); err == nil {
			t.Fatal("published inside backup")
		}
	}
	if _, err := os.Lstat(filepath.Join(source, "nested")); !os.IsNotExist(err) {
		t.Fatal("source was modified", err)
	}
	if _, err := VerifyBackup(context.Background(), source); err != nil {
		t.Fatal(err)
	}
}
