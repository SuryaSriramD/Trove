package workspace

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"trove.local/phase0/schemas"
)

func legacyMigrationFixture(t *testing.T) (string, string, map[string][]byte) {
	t.Helper()
	k, e := testKernel(t)
	page := createPage(t, k, e, "Original retained page")
	// An unreferenced/history-only asset must survive, even though current-content
	// exports intentionally omit it. Snapshot inventory is independent of file refs.
	assetPath := "files/ab/unreferenced-history-original.bin"
	if err := os.MkdirAll(filepath.Join(k.path, "files/ab"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(k.path, assetPath), []byte("retained historical attachment"), 0600); err != nil {
		t.Fatal(err)
	}
	draft, f, err := k.BeginRecording(context.Background(), e, "Synthetic interrupted stream")
	if err != nil {
		t.Fatal(err)
	}
	audio := bytes.Repeat([]byte{0, 0, 128, 62}, 257)
	if _, err = f.Write(audio); err != nil {
		t.Fatal(err)
	}
	if err = f.Sync(); err != nil {
		t.Fatal(err)
	}
	f.Close()
	originals := map[string][]byte{assetPath: []byte("retained historical attachment"), "recordings/" + draft.ID + ".f32le": audio}
	p := "recordings/" + draft.ID + ".json"
	originals[p], err = os.ReadFile(filepath.Join(k.path, p))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = k.db.Exec("DELETE FROM kernel_meta WHERE key IN ('storageGeneration','storageMigrationId','storageMigrationManifestSHA256'); PRAGMA user_version=2"); err != nil {
		t.Fatal(err)
	}
	path := k.path
	k.Close()
	return path, page, originals
}
func assertMigrationBytes(t *testing.T, root string, objects map[string][]byte) {
	t.Helper()
	for path, want := range objects {
		got, err := os.ReadFile(filepath.Join(root, path))
		if err != nil || !bytes.Equal(got, want) {
			t.Fatalf("%s bytes differ (%v)", path, err)
		}
	}
}
func migrationFault(stage string) func(string) error {
	return func(current string) error {
		if stage == current {
			return errors.New("injected migration interruption: " + stage)
		}
		return nil
	}
}
func requireMigrationOpenFailure(t *testing.T, path string) {
	t.Helper()
	if k, err := Open(path); err == nil {
		k.Close()
		t.Fatal("incomplete migration reopened")
	}
}

func TestMigrationSnapshotPreservesLegacyDatabaseAssetsAndRawRecording(t *testing.T) {
	ctx := context.Background()
	path, page, originals := legacyMigrationFixture(t)
	k, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	d, err := k.DescribeCompatibility(ctx)
	if err != nil || d != CurrentCompatibility() {
		t.Fatal(d, err)
	}
	if title := readPage(t, k, k.OwnerContext(), page)["page"].(map[string]any)["title"]; title != "Original retained page" {
		t.Fatal(title)
	}
	k.Close()
	status, err := ReadMigrationStatus(ctx, path)
	if err != nil || status == nil || status.State != "complete" {
		t.Fatal(status, err)
	}
	manifest, err := VerifyMigrationSnapshot(ctx, status.SnapshotPath)
	if err != nil {
		t.Fatal(err)
	}
	if manifest.Before != (CompatibilityDescriptor{2, 0}) || manifest.Target != CurrentCompatibility() || len(manifest.Objects) != len(originals) {
		t.Fatal(manifest)
	}
	assertMigrationBytes(t, status.SnapshotPath, originals)
	snapshotDB, err := openMigrationDatabase(status.SnapshotPath)
	if err != nil {
		t.Fatal(err)
	}
	var title string
	if err = snapshotDB.QueryRow("SELECT title FROM pages WHERE id=?", page).Scan(&title); err != nil || title != "Original retained page" {
		t.Fatal(title, err)
	}
	snapshotDB.Close()
	destination := filepath.Join(t.TempDir(), "restored")
	if _, err = RestoreMigrationSnapshot(ctx, status.SnapshotPath, destination); err != nil {
		t.Fatal(err)
	}
	assertMigrationBytes(t, destination, originals)
	restoredFormat, err := ReadWorkspaceCompatibility(ctx, destination)
	if err != nil || restoredFormat != manifest.Before {
		t.Fatal(restoredFormat, err)
	}
	if _, err = RestoreMigrationSnapshot(ctx, status.SnapshotPath, destination); err == nil {
		t.Fatal("restored over an existing workspace")
	}
	if restored, err := Open(destination); err != nil {
		t.Fatal(err)
	} else {
		restored.Close()
	}
	k, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	k.Close()
	entries, err := os.ReadDir(filepath.Join(path, migrationSnapshotRoot))
	if err != nil || len(entries) != 1 {
		t.Fatal("ordinary reopen created another snapshot", len(entries), err)
	}
}

func TestMigrationV1FullSnapshotPrecedesKernelAndPackageSchema(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "workspace")
	if err := os.Mkdir(path, 0700); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite3", filepath.Join(path, "workspace.db"))
	if err != nil {
		t.Fatal(err)
	}
	base, err := schemas.Files.ReadFile("workspace.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(string(base)); err != nil {
		t.Fatal(err)
	}
	id := NewID()
	if _, err = db.Exec("INSERT INTO workspaces(id,format_version) VALUES(?,1)", id); err != nil {
		t.Fatal(err)
	}
	db.Close()
	k, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	k.Close()
	status, err := ReadMigrationStatus(ctx, path)
	if err != nil || status.Before != (CompatibilityDescriptor{1, 0}) {
		t.Fatal(status, err)
	}
	snap, err := openMigrationDatabase(status.SnapshotPath)
	if err != nil {
		t.Fatal(err)
	}
	defer snap.Close()
	var count int
	if err = snap.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name IN ('kernel_meta','personal_review_generations')").Scan(&count); err != nil || count != 0 {
		t.Fatal("snapshot contains upgraded schema", count, err)
	}
}

func TestMigrationInterruptedBoundariesFenceAndRestoreOriginal(t *testing.T) {
	for _, stage := range []string{"after_journal", "before_initializers", "after_initializers"} {
		t.Run(stage, func(t *testing.T) {
			ctx := context.Background()
			path, _, originals := legacyMigrationFixture(t)
			if k, err := openMigrationObserved(path, false, migrationFault(stage)); err == nil {
				k.Close()
				t.Fatal("fault not injected")
			}
			requireMigrationOpenFailure(t, path)
			status, err := ReadMigrationStatus(ctx, path)
			if err != nil || status == nil || status.State != "pending" {
				t.Fatal(status, err)
			}
			destination := filepath.Join(t.TempDir(), "recovered")
			if _, err = RestoreMigrationSnapshot(ctx, status.SnapshotPath, destination); err != nil {
				t.Fatal(err)
			}
			assertMigrationBytes(t, destination, originals)
			requireMigrationOpenFailure(t, path)
		})
	}
}

func TestMigrationCompletionCrashFinishesOnlyVerifiedIdentity(t *testing.T) {
	for _, stage := range []string{"after_stamp", "after_completion"} {
		t.Run(stage, func(t *testing.T) {
			path, _, _ := legacyMigrationFixture(t)
			if k, err := openMigrationObserved(path, false, migrationFault(stage)); err == nil {
				k.Close()
				t.Fatal("fault not injected")
			}
			if exists, err := markerExists(path); err != nil || !exists {
				t.Fatal("missing incomplete marker", err)
			}
			k, err := Open(path)
			if err != nil {
				t.Fatal(err)
			}
			k.Close()
			if exists, err := markerExists(path); err != nil || exists {
				t.Fatal("completion marker was not cleared", err)
			}
			status, err := ReadMigrationStatus(context.Background(), path)
			if err != nil || status.State != "complete" {
				t.Fatal(status, err)
			}
		})
	}
}

func TestMigrationCorruptionCannotBypassRecoveryFence(t *testing.T) {
	for _, scenario := range []string{"missing_snapshot", "damaged_asset", "missing_journal", "invalid_journal", "wrong_identity", "wrong_digest", "unrelated_marker"} {
		t.Run(scenario, func(t *testing.T) {
			path, _, originals := legacyMigrationFixture(t)
			if k, err := openMigrationObserved(path, false, migrationFault("after_stamp")); err == nil {
				k.Close()
				t.Fatal("fault not injected")
			}
			j, err := readMigrationJournal(path)
			if err != nil {
				t.Fatal(err)
			}
			snapshot := filepath.Join(path, migrationSnapshotRoot, j.SnapshotID)
			switch scenario {
			case "missing_snapshot":
				err = os.Rename(snapshot, snapshot+"-unavailable")
			case "damaged_asset":
				for p := range originals {
					err = os.WriteFile(filepath.Join(snapshot, p), []byte("changed"), 0600)
					break
				}
			case "missing_journal":
				err = os.Remove(filepath.Join(path, migrationJournalName))
			case "invalid_journal":
				err = os.WriteFile(filepath.Join(path, migrationJournalName), []byte("{}"), 0600)
			case "wrong_identity", "wrong_digest":
				db, e := sql.Open("sqlite3", filepath.Join(path, "workspace.db"))
				if e != nil {
					t.Fatal(e)
				}
				key := "storageMigrationId"
				if scenario == "wrong_digest" {
					key = "storageMigrationManifestSHA256"
				}
				_, err = db.Exec("UPDATE kernel_meta SET value='incorrect' WHERE key=?", key)
				db.Close()
			case "unrelated_marker":
				err = os.WriteFile(filepath.Join(path, "INCOMPLETE"), []byte("Unrelated restore in progress\n"), 0600)
			}
			if err != nil {
				t.Fatal(err)
			}
			requireMigrationOpenFailure(t, path)
			if exists, err := markerExists(path); err != nil || !exists {
				t.Fatal("failure removed the recovery fence", err)
			}
		})
	}
}

func TestMigrationSnapshotFailureLeavesCanonicalContentUntouched(t *testing.T) {
	path, _, originals := legacyMigrationFixture(t)
	outside := filepath.Join(t.TempDir(), "unrelated")
	if err := os.WriteFile(outside, []byte("must not capture"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(path, "files", "hostile-link")); err != nil {
		t.Fatal(err)
	}
	if k, err := Open(path); err == nil {
		k.Close()
		t.Fatal("accepted linked source")
	}
	assertMigrationBytes(t, path, originals)
	d, err := ReadWorkspaceCompatibility(context.Background(), path)
	if err != nil || d != (CompatibilityDescriptor{2, 0}) {
		t.Fatal(d, err)
	}
	if j, err := readMigrationJournal(path); err != nil || j != nil {
		t.Fatal("published an unverified snapshot journal", j, err)
	}
	if exists, err := markerExists(path); err != nil || exists {
		t.Fatal("began migration before snapshot passed", err)
	}
	entries, err := os.ReadDir(filepath.Join(path, migrationSnapshotRoot))
	if err != nil || len(entries) != 1 {
		t.Fatal(entries, err)
	}
	if _, err = VerifyMigrationSnapshot(context.Background(), filepath.Join(path, migrationSnapshotRoot, entries[0].Name())); err == nil {
		t.Fatal("partial snapshot verified")
	}
}

func TestMigrationFutureFormatRefusedBeforeStartupMutations(t *testing.T) {
	for _, future := range []string{"version", "generation"} {
		t.Run(future, func(t *testing.T) {
			path, _, originals := legacyMigrationFixture(t)
			db, err := sql.Open("sqlite3", filepath.Join(path, "workspace.db"))
			if err != nil {
				t.Fatal(err)
			}
			if future == "version" {
				_, err = db.Exec("PRAGMA user_version=4")
			} else {
				_, err = db.Exec("PRAGMA user_version=3; INSERT INTO kernel_meta(key,value) VALUES('storageGeneration',?)", CurrentStorageGeneration+1)
			}
			if err != nil {
				t.Fatal(err)
			}
			db.Close()
			hash, _, err := hashFile(context.Background(), filepath.Join(path, "workspace.db"))
			if err != nil {
				t.Fatal(err)
			}
			if k, err := Open(path); err == nil {
				k.Close()
				t.Fatal("future format opened")
			} else if !strings.Contains(err.Error(), "newer") {
				t.Fatal(err)
			}
			after, _, err := hashFile(context.Background(), filepath.Join(path, "workspace.db"))
			if err != nil || hash != after {
				t.Fatal("future database changed", err)
			}
			assertMigrationBytes(t, path, originals)
			if _, err = os.Lstat(filepath.Join(path, migrationSnapshotRoot)); !os.IsNotExist(err) {
				t.Fatal("future format ran snapshot/migration", err)
			}
		})
	}
}

func TestMigrationInspectorAndFreshWorkspace(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "absent")
	if _, err := ReadWorkspaceCompatibility(context.Background(), missing); err == nil {
		t.Fatal("missing database inspected")
	}
	if _, err := os.Lstat(missing); !os.IsNotExist(err) {
		t.Fatal("inspector created directory", err)
	}
	k, _ := testKernel(t)
	path := k.path
	k.Close()
	d, err := ReadWorkspaceCompatibility(context.Background(), path)
	if err != nil || d != CurrentCompatibility() {
		t.Fatal(d, err)
	}
	if status, err := ReadMigrationStatus(context.Background(), path); err != nil || status != nil {
		t.Fatal(status, err)
	}
	if _, err = os.Stat(filepath.Join(path, migrationSnapshotRoot)); !os.IsNotExist(err) {
		t.Fatal("new workspace unnecessarily snapshotted", err)
	}
	if err = os.WriteFile(filepath.Join(path, "INCOMPLETE"), []byte("Restore incomplete"), 0600); err != nil {
		t.Fatal(err)
	}
	requireMigrationOpenFailure(t, path)
}

func TestMigrationSnapshotVerifierRejectsUnlistedObjectsAndCancelledRestore(t *testing.T) {
	path, _, _ := legacyMigrationFixture(t)
	k, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	k.Close()
	status, err := ReadMigrationStatus(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	destination := filepath.Join(t.TempDir(), "cancelled")
	if _, err = RestoreMigrationSnapshot(ctx, status.SnapshotPath, destination); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if _, err = os.Lstat(destination); !os.IsNotExist(err) {
		t.Fatal("cancelled verifier created destination", err)
	}
	if err = os.WriteFile(filepath.Join(status.SnapshotPath, "recordings", "unlisted"), []byte("extra"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = VerifyMigrationSnapshot(context.Background(), status.SnapshotPath); err == nil {
		t.Fatal("unlisted object verified")
	}
}

func TestMigrationAdditiveInitializerFailureRetainsIndependentRestorePoint(t *testing.T) {
	path, page, originals := legacyMigrationFixture(t)
	previous := kernelInitializers
	// Simulate an additive package migration that commits SQL before its later
	// step fails. Its external writes are deliberately not rolled back by SQLite.
	kernelInitializers = append(append([]func(context.Context, *Kernel) error{}, previous...), func(ctx context.Context, k *Kernel) error {
		if _, err := k.db.ExecContext(ctx, "CREATE TABLE injected_partial_migration(value TEXT); UPDATE pages SET title='partial migration changed content' WHERE id=?", page); err != nil {
			return err
		}
		return errors.New("injected additive migration failure")
	})
	k, err := Open(path)
	kernelInitializers = previous
	if err == nil {
		k.Close()
		t.Fatal("partial initializer did not fail")
	}
	requireMigrationOpenFailure(t, path)
	status, err := ReadMigrationStatus(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	destination := filepath.Join(t.TempDir(), "intact")
	if _, err = RestoreMigrationSnapshot(context.Background(), status.SnapshotPath, destination); err != nil {
		t.Fatal(err)
	}
	assertMigrationBytes(t, destination, originals)
	db, err := openMigrationDatabase(destination)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var count int
	var title string
	if err = db.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE name='injected_partial_migration'").Scan(&count); err != nil || count != 0 {
		t.Fatal("partial schema survived restore", count, err)
	}
	if err = db.QueryRow("SELECT title FROM pages WHERE id=?", page).Scan(&title); err != nil || title != "Original retained page" {
		t.Fatal("partial value survived restore", title, err)
	}
}

func TestMigrationSnapshotRejectsDatabaseRepeatedAsContent(t *testing.T) {
	path, _, _ := legacyMigrationFixture(t)
	k, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	k.Close()
	status, err := ReadMigrationStatus(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := VerifyMigrationSnapshot(context.Background(), status.SnapshotPath)
	if err != nil {
		t.Fatal(err)
	}
	manifest.Objects = append(manifest.Objects, manifest.Database)
	if err = writeMigrationJSON(status.SnapshotPath, migrationManifestName, manifest); err != nil {
		t.Fatal(err)
	}
	if _, err = VerifyMigrationSnapshot(context.Background(), status.SnapshotPath); err == nil {
		t.Fatal("database allowed in content inventory")
	}
}

func TestMigrationSnapshotRefusesOversizedSparseSourceBeforeMigration(t *testing.T) {
	path, _, originals := legacyMigrationFixture(t)
	p := filepath.Join(path, "files", "oversized")
	f, err := os.OpenFile(p, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	err = f.Truncate(maxMigrationObjectBytes + 1)
	f.Close()
	if err != nil {
		t.Fatal(err)
	}
	if k, err := Open(path); err == nil {
		k.Close()
		t.Fatal("oversized source accepted")
	}
	assertMigrationBytes(t, path, originals)
	if j, err := readMigrationJournal(path); err != nil || j != nil {
		t.Fatal("oversized source began migration", j, err)
	}
}

func TestMigrationRestoreCannotModifySourceByNestedDestination(t *testing.T) {
	path, _, _ := legacyMigrationFixture(t)
	k, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	k.Close()
	status, err := ReadMigrationStatus(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(t.TempDir(), "alias")
	if err = os.Symlink(status.SnapshotPath, alias); err != nil {
		t.Fatal(err)
	}
	for _, root := range []string{status.SnapshotPath, alias} {
		if _, err = RestoreMigrationSnapshot(context.Background(), status.SnapshotPath, filepath.Join(root, "nested")); err == nil {
			t.Fatal("nested restore changed source")
		}
	}
	if _, err = VerifyMigrationSnapshot(context.Background(), status.SnapshotPath); err != nil {
		t.Fatal("nested attempt changed source", err)
	}
}

func TestMigrationReviewedRestoreRejectsSelfConsistentSnapshotReplacement(t *testing.T) {
	ctx := context.Background()
	path, _, _ := legacyMigrationFixture(t)
	k, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	k.Close()
	status, err := ReadMigrationStatus(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	reviewed, err := VerifyMigrationSnapshot(ctx, status.SnapshotPath)
	if err != nil {
		t.Fatal(err)
	}
	current := reviewed
	current.Objects = append([]BackupObject{}, reviewed.Objects...)
	for i, object := range current.Objects {
		if !strings.HasPrefix(object.Path, "files/") {
			continue
		}
		p := filepath.Join(status.SnapshotPath, object.Path)
		replacement := []byte("a self-consistent newer attachment that was not reviewed")
		if err = os.WriteFile(p, replacement, 0600); err != nil {
			t.Fatal(err)
		}
		current.Objects[i].SHA256 = migrationDigest(replacement)
		current.Objects[i].SizeBytes = strconv.Itoa(len(replacement))
		break
	}
	if err = writeMigrationJSON(status.SnapshotPath, migrationManifestName, current); err != nil {
		t.Fatal(err)
	}
	if _, err = VerifyMigrationSnapshot(ctx, status.SnapshotPath); err != nil {
		t.Fatal("replacement fixture is not self-consistent", err)
	}
	destination := filepath.Join(t.TempDir(), "rejected")
	if _, err = RestoreMigrationSnapshotReviewed(ctx, status.SnapshotPath, destination, reviewed); err == nil {
		t.Fatal("restored content changed after review")
	}
	if _, err = os.Lstat(destination); !os.IsNotExist(err) {
		t.Fatal("unreviewed content was published", err)
	}
	// A new explicit review can restore that exact verified inventory.
	if _, err = RestoreMigrationSnapshotReviewed(ctx, status.SnapshotPath, destination, current); err != nil {
		t.Fatal(err)
	}
}
