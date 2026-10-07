package workspace

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"trove.local/phase0/internal/platformfs"
)

func restoredAdmissionFixture(t *testing.T) (string, string, string, BackupManifest) {
	t.Helper()
	ctx := context.Background()
	k, e := testKernel(t)
	page := createPage(t, k, e, "Independent preserved recovery content")
	backup := filepath.Join(t.TempDir(), "backup")
	m, err := k.CreateBackup(ctx, e, backup)
	if err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(t.TempDir(), "restored")
	if _, err = RestoreBackupReviewed(ctx, backup, dest, m); err != nil {
		t.Fatal(err)
	}
	return dest, page, backup, m
}
func TestRecoveryAdmissionRejectsMissingReceiptsBeforeSQLiteMutation(t *testing.T) {
	for _, kind := range []string{"completion", "all", "changed-binding", "unverified-wal"} {
		t.Run(kind, func(t *testing.T) {
			dir, _, _, _ := restoredAdmissionFixture(t)
			switch kind {
			case "completion":
				os.Remove(filepath.Join(dir, ".trove-recovery-v1.complete.json"))
			case "all":
				entries, _ := os.ReadDir(dir)
				for _, entry := range entries {
					if strings.HasPrefix(entry.Name(), ".trove-recovery-v1") {
						os.RemoveAll(filepath.Join(dir, entry.Name()))
					}
				}
			case "changed-binding":
				raw, e := os.ReadFile(filepath.Join(dir, ".trove-recovery-v1.intent.json"))
				if e != nil {
					t.Fatal(e)
				}
				raw = []byte(strings.Replace(string(raw), `"kind":"restore"`, `"kind":"transfer"`, 1))
				os.WriteFile(filepath.Join(dir, ".trove-recovery-v1.intent.json"), raw, 0600)
			case "unverified-wal":
				os.WriteFile(filepath.Join(dir, "workspace.db-wal"), []byte("not part of the verified baseline"), 0600)
			}
			before, _, err := hashFile(context.Background(), filepath.Join(dir, "workspace.db"))
			if err != nil {
				t.Fatal(err)
			}
			for _, open := range []func(string) (*Kernel, error){Open, Create} {
				k, e := open(dir)
				if k != nil {
					k.Close()
				}
				if !errors.Is(e, platformfs.ErrRecoveryIncomplete) {
					t.Fatalf("unsafe startup accepted: %v", e)
				}
			}
			after, _, err := hashFile(context.Background(), filepath.Join(dir, "workspace.db"))
			if err != nil || after != before {
				t.Fatalf("database mutated before admission: %s %s %v", before, after, err)
			}
			if _, e := os.Stat(filepath.Join(dir, ".trove-recovery-v1.active.json")); !os.IsNotExist(e) {
				t.Fatal("invalid baseline activated", e)
			}
		})
	}
}
func TestRecoveryAdmissionActivationPreservesLaterEditsAndPortableBackups(t *testing.T) {
	ctx := context.Background()
	dir, page, source, m := restoredAdmissionFixture(t)
	firstID, err := readRecoveryOperation(ctx, dir)
	if err != nil || firstID == "" {
		t.Fatal("binding absent", err)
	}
	k, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if readPage(t, k, k.OwnerContext(), page)["page"].(map[string]any)["title"] != "Independent preserved recovery content" {
		t.Fatal("canonical source content changed")
	}
	added := createPage(t, k, k.OwnerContext(), "Ordinary edit after activation")
	backup := filepath.Join(t.TempDir(), "second-backup")
	nextManifest, err := k.CreateBackup(ctx, k.OwnerContext(), backup)
	if err != nil {
		t.Fatal(err)
	}
	k.Close()
	k, err = Open(dir)
	if err != nil {
		t.Fatal("ordinary reopen failed", err)
	}
	k.Close()
	next := filepath.Join(t.TempDir(), "second-restored")
	if _, err = RestoreBackupReviewed(ctx, backup, next, nextManifest); err != nil {
		t.Fatal("backup of restored workspace failed", err)
	}
	nextID, err := readRecoveryOperation(ctx, next)
	if err != nil || nextID == "" || firstID == nextID {
		t.Fatal("copy inherited original admission authority", err)
	}
	nextKernel, err := Open(next)
	if err != nil {
		t.Fatal(err)
	}
	defer nextKernel.Close()
	if readPage(t, nextKernel, nextKernel.OwnerContext(), added)["page"].(map[string]any)["title"] != "Ordinary edit after activation" {
		t.Fatal("later edit lost")
	}
	if _, err = VerifyBackup(ctx, source); err != nil {
		t.Fatal("first source backup was modified", err)
	}
	actual, err := VerifyBackup(ctx, backup)
	if err != nil || string(marshal(actual)) != string(marshal(nextManifest)) {
		t.Fatal("second source backup changed", err)
	}
	if m.WorkspaceID != nextKernel.ID() {
		t.Fatal("workspace identity changed")
	}
	compatible := filepath.Join(t.TempDir(), "compatible-restored")
	if _, err = RestoreBackupCompatible(ctx, backup, compatible, nextManifest, CurrentCompatibility()); err != nil {
		t.Fatal("compatible restore of a published workspace failed", err)
	}
	compatibleKernel, err := OpenCompatibleReviewed(ctx, compatible, m.WorkspaceID, CurrentCompatibility())
	if err != nil {
		t.Fatal(err)
	}
	compatibleKernel.Close()
}
func TestRecoveryAdmissionReviewedPreviewCannotActivate(t *testing.T) {
	dir, _, _, m := restoredAdmissionFixture(t)
	before, _, err := hashFile(context.Background(), filepath.Join(dir, "workspace.db"))
	if err != nil {
		t.Fatal(err)
	}
	k, err := OpenCompatibleReviewed(context.Background(), dir, m.WorkspaceID, CurrentCompatibility())
	if k != nil {
		k.Close()
	}
	if !errors.Is(err, platformfs.ErrRecoveryIncomplete) {
		t.Fatal("read-only preview activated baseline", err)
	}
	if _, err = os.Stat(filepath.Join(dir, ".trove-recovery-v1.active.json")); !os.IsNotExist(err) {
		t.Fatal("read-only preview created activation", err)
	}
	after, _, err := hashFile(context.Background(), filepath.Join(dir, "workspace.db"))
	if err != nil || before != after {
		t.Fatal("read-only preview changed baseline", err)
	}
}
func TestRecoveryCopyMetadataRequiresExactBackupBytes(t *testing.T) {
	dir, _, _, m := restoredAdmissionFixture(t)
	// This database has restore cleanup and a new binding, so the original source
	// digest is no longer an authority to edit its metadata.
	before, _, err := hashFile(context.Background(), filepath.Join(dir, "workspace.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err = clearCopiedRecoveryBinding(context.Background(), dir, m.Database); err == nil {
		t.Fatal("mismatched manifest authorized metadata mutation")
	}
	after, _, err := hashFile(context.Background(), filepath.Join(dir, "workspace.db"))
	if err != nil || before != after {
		t.Fatal("rejected metadata reset changed bytes", err)
	}
}

func TestRecoveryRestoreCannotWriteIntoSourceBackup(t *testing.T) {
	ctx := context.Background()
	_, _, source, m := restoredAdmissionFixture(t)
	original, err := os.ReadDir(source)
	if err != nil {
		t.Fatal(err)
	}
	for _, alias := range []bool{false, true} {
		t.Run(fmt.Sprint(alias), func(t *testing.T) {
			parent := source
			if alias {
				parent = filepath.Join(t.TempDir(), "backup-alias")
				if err = os.Symlink(source, parent); err != nil {
					t.Fatal(err)
				}
			}
			if _, err = RestoreBackupReviewed(ctx, source, filepath.Join(parent, "nested"), m); err == nil {
				t.Fatal("restore wrote inside immutable source")
			}
			current, e := os.ReadDir(source)
			if e != nil || len(current) != len(original) {
				t.Fatal("source directory inventory changed", e)
			}
			actual, e := VerifyBackup(ctx, source)
			if e != nil || string(marshal(actual)) != string(marshal(m)) {
				t.Fatal("source backup changed", e)
			}
		})
	}
}
func TestRecoveryTransferProvenanceMustMatchBeforeFirstOpen(t *testing.T) {
	for _, kind := range []string{"missing", "replaced", "undeclared"} {
		t.Run(kind, func(t *testing.T) {
			ctx := context.Background()
			k, e := testKernel(t)
			createPage(t, k, e, "Transfer receipt oracle")
			source := k.path
			k.Close()
			dest := filepath.Join(t.TempDir(), "transferred")
			if _, err := TransferWorkspace(ctx, source, dest); err != nil {
				t.Fatal(err)
			}
			switch kind {
			case "missing":
				os.Remove(filepath.Join(dest, "owner-transfer.json"))
			case "replaced":
				os.WriteFile(filepath.Join(dest, "owner-transfer.json"), []byte(`{"destination":"unreviewed"}`), 0600)
			case "undeclared":
				os.WriteFile(filepath.Join(dest, "unbound-transfer.json"), []byte("not inventoried"), 0600)
			}
			opened, err := Open(dest)
			if opened != nil {
				opened.Close()
			}
			if !errors.Is(err, platformfs.ErrRecoveryIncomplete) {
				t.Fatal("tampered provenance admitted", err)
			}
		})
	}
}
func TestRecoveryLegacyCompatibleWorkspaceRetainsOrdinaryEdits(t *testing.T) {
	k, e := testKernel(t)
	page := createPage(t, k, e, "Legacy compatible workspace")
	path := k.path
	k.Close()
	if id, err := readRecoveryOperation(context.Background(), path); err != nil || id != "" {
		t.Fatal("unexpected protocol binding", err)
	}
	// POSIX legacy permissions keep their pre-protocol policy; only new reserved
	// recovery roots require the stricter private-handle audit.
	if runtime.GOOS != "windows" {
		if err := os.Chmod(path, 0755); err != nil {
			t.Fatal(err)
		}
	}
	reopened, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	if readPage(t, reopened, reopened.OwnerContext(), page)["page"].(map[string]any)["title"] != "Legacy compatible workspace" {
		t.Fatal("legacy data lost")
	}
	createPage(t, reopened, reopened.OwnerContext(), "ordinary edit remains supported")
}

func TestRecoveryUnsupportedBaselineDoesNotActivateOrCreateSQLiteSidecars(t *testing.T) {
	ctx := context.Background()
	k, e := testKernel(t)
	createPage(t, k, e, "Future version content")
	source := k.path
	k.Close()
	operation := newRecoveryOperation()
	db, err := sql.Open("sqlite3", filepath.Join(source, "workspace.db"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec("INSERT OR REPLACE INTO kernel_meta(key,value) VALUES(?,?)", recoveryOperationKey, operation); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(fmt.Sprintf("PRAGMA user_version=%d; PRAGMA wal_checkpoint(TRUNCATE)", CurrentUserVersion+1)); err != nil {
		t.Fatal(err)
	}
	if err = db.Close(); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(source, "workspace.db"))
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(raw)
	plan := platformfs.RecoveryPlan{Version: 1, OperationID: operation, Kind: "restore", SourceID: "unsupported-baseline-oracle", SourceSHA256: hex.EncodeToString(digest[:]), Objects: []platformfs.RecoveryObject{{Path: "workspace.db", SHA256: hex.EncodeToString(digest[:]), SizeBytes: int64(len(raw))}}}
	destination := filepath.Join(t.TempDir(), "future-workspace")
	publication, err := platformfs.BeginRecoveryPublication(ctx, destination, plan)
	if err != nil {
		t.Fatal(err)
	}
	if err = publication.Stage(ctx, "workspace.db", bytes.NewReader(raw)); err != nil {
		t.Fatal(err)
	}
	if err = publication.Verify(ctx); err != nil {
		t.Fatal(err)
	}
	if err = publication.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	publication.Close()
	inventory := func() map[string]string {
		result := map[string]string{}
		err := filepath.WalkDir(destination, func(name string, entry os.DirEntry, e error) error {
			if e != nil {
				return e
			}
			if entry.IsDir() {
				return nil
			}
			b, e := os.ReadFile(name)
			if e != nil {
				return e
			}
			relative, _ := filepath.Rel(destination, name)
			result[relative] = string(b)
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
		return result
	}
	before := inventory()
	opened, err := Open(destination)
	if opened != nil {
		opened.Close()
	}
	if err == nil || !strings.Contains(err.Error(), "UNSUPPORTED_SCHEMA") {
		t.Fatal("future version admitted", err)
	}
	if !reflect.DeepEqual(before, inventory()) {
		t.Fatal("rejected startup changed database, receipt or SQLite sidecars")
	}
	status, err := platformfs.InspectRecoveryAdmission(destination, operation)
	if err != nil || status.State != "complete" {
		t.Fatal("unsupported baseline activated or damaged", status, err)
	}
}
