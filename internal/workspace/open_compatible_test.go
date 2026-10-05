package workspace

import (
	"bytes"
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"
)

func TestCompatibleReviewedOpenPreservesPendingJobUntilCommit(t *testing.T) {
	k, e := testKernel(t)
	path, id := k.Path(), k.ID()
	job := enqueueTestJob(t, k, e, "BACKUP_WORKSPACE", map[string]any{"kind": "backup", "destinationHandle": NewID()})
	if _, err := k.ClaimJob(context.Background(), []string{"BACKUP_WORKSPACE"}); err != nil {
		t.Fatal(err)
	}
	if err := k.Close(); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(filepath.Join(path, "workspace.db"))
	if err != nil {
		t.Fatal(err)
	}
	reopened, err := OpenCompatibleReviewed(context.Background(), path, id, CurrentCompatibility())
	if err != nil {
		t.Fatal(err)
	}
	actual, err := reopened.ReadJob(context.Background(), string(job.JobId))
	if err != nil || actual.State != "running" {
		t.Fatal("pending startup recovered job", actual, err)
	}
	testOK(t, testDispatch(t, reopened, reopened.OwnerContext(), "workspace.snapshot", recoveryRequest(reopened, map[string]any{})))
	if err = reopened.Close(); err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(filepath.Join(path, "workspace.db"))
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("pending reviewed startup changed database bytes", err)
	}
	reopened, err = OpenCompatibleReviewed(context.Background(), path, id, CurrentCompatibility())
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	if err = reopened.CompleteCompatibleStartup(context.Background()); err != nil {
		t.Fatal(err)
	}
	actual, err = reopened.ReadJob(context.Background(), string(job.JobId))
	if err != nil || actual.State != "retry_wait" {
		t.Fatal("committed startup did not recover job", actual, err)
	}
	if err = reopened.CompleteCompatibleStartup(context.Background()); err == nil {
		t.Fatal("accepted repeated runtime initialization")
	}
}
func TestCompatibleReviewedOpenRejectsWithoutMigrationOrCreation(t *testing.T) {
	for _, kind := range []string{"identity", "old-format", "future-format", "incomplete", "cancelled", "locked", "missing"} {
		t.Run(kind, func(t *testing.T) {
			k, _ := testKernel(t)
			path, id := k.Path(), k.ID()
			if kind == "old-format" {
				if _, e := k.db.Exec("PRAGMA user_version=2"); e != nil {
					t.Fatal(e)
				}
			}
			if kind != "locked" {
				if e := k.Close(); e != nil {
					t.Fatal(e)
				}
			}
			if kind == "identity" {
				id += "-wrong"
			}
			format := CurrentCompatibility()
			if kind == "future-format" {
				format.StorageGeneration++
			}
			if kind == "incomplete" {
				if e := os.WriteFile(filepath.Join(path, "INCOMPLETE"), []byte("preserve"), 0600); e != nil {
					t.Fatal(e)
				}
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if kind == "cancelled" {
				cancel()
			}
			before, _ := os.ReadFile(filepath.Join(path, "workspace.db"))
			if kind == "missing" {
				path = filepath.Join(t.TempDir(), "missing")
			}
			opened, err := OpenCompatibleReviewed(ctx, path, id, format)
			if err == nil {
				opened.Close()
				t.Fatal("accepted invalid reviewed workspace")
			}
			after, _ := os.ReadFile(filepath.Join(path, "workspace.db"))
			if kind != "missing" && !bytes.Equal(before, after) {
				t.Fatal("rejection changed existing database")
			}
			if kind == "missing" {
				if _, e := os.Stat(path); !os.IsNotExist(e) {
					t.Fatal("created missing workspace")
				}
			}
			if _, e := os.Stat(filepath.Join(path, ".migration-snapshots")); !os.IsNotExist(e) {
				t.Fatal("attempted migration")
			}
		})
	}
}

func TestCompatibleReviewedDeleteJournalIsStrictlyReadOnly(t *testing.T) {
	k, _ := testKernel(t)
	path, id := k.Path(), k.ID()
	if err := k.Close(); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite3", filepath.Join(path, "workspace.db"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec("PRAGMA journal_mode=DELETE"); err != nil {
		t.Fatal(err)
	}
	db.Close()
	before, err := os.ReadFile(filepath.Join(path, "workspace.db"))
	if err != nil {
		t.Fatal(err)
	}
	k, err = OpenCompatibleReviewed(context.Background(), path, id, CurrentCompatibility())
	if err != nil {
		t.Fatal(err)
	}
	if _, err = k.db.Exec("CREATE TABLE forbidden_pending_change(value TEXT)"); err == nil {
		t.Fatal("pending read pool permitted schema write")
	}
	if _, err = k.writeDB.Exec("UPDATE workspaces SET format_version=99"); err == nil {
		t.Fatal("pending write pool permitted canonical write")
	}
	var mode string
	if err = k.db.QueryRow("PRAGMA journal_mode").Scan(&mode); err != nil || mode != "delete" {
		t.Fatal("pending open switched journal mode", mode, err)
	}
	if err = k.Close(); err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(filepath.Join(path, "workspace.db"))
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("pending open changed DELETE-journal bytes", err)
	}
	for _, suffix := range []string{"-wal", "-shm", "-journal"} {
		if _, err = os.Stat(filepath.Join(path, "workspace.db") + suffix); !os.IsNotExist(err) {
			t.Fatal("pending open created writable journal", suffix, err)
		}
	}
	k, err = OpenCompatibleReviewed(context.Background(), path, id, CurrentCompatibility())
	if err != nil {
		t.Fatal(err)
	}
	defer k.Close()
	if err = k.CompleteCompatibleStartup(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err = k.writeDB.Exec("CREATE TABLE committed_test(value TEXT)"); err != nil {
		t.Fatal("committed writer stayed read-only", err)
	}
}
