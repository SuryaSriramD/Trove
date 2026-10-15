package workspace

import (
	"archive/tar"
	"bytes"
	"context"
	"database/sql"
	"errors"
	"math"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"filippo.io/age"
	"trove.local/phase0/contracts"
	"trove.local/phase0/internal/platformfs"
)

func assertPortableStagesEmpty(t *testing.T, parent string) {
	t.Helper()
	entries, err := os.ReadDir(parent)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("private staging remains: %v", entries)
	}
}
func TestEncryptedBackupRoundTripPreservesRetainedState(t *testing.T) {
	ctx := context.Background()
	k, e := testKernel(t)
	page := createPage(t, k, e, "Private portable snapshot title")
	attachTestFile(t, k, e, page, stageTestFile(t, k, e, "private portable asset bytes"), false)
	if err := k.RebuildSearch(ctx); err != nil {
		t.Fatal(err)
	}
	approval := NewID()
	if _, err := k.db.Exec("INSERT INTO approvals VALUES(?,?,?,?,?,?,?,?,'approved')", approval, e.Actor.ActorId, e.ScopeGrantId, 1, strings.Repeat("a", 64), "[]", timeAfterMinutes(60), e.Actor.ActorId); err != nil {
		t.Fatal(err)
	}
	draft, audio, err := k.BeginRecording(ctx, e, "Retained interrupted recording")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = audio.Write(make([]byte, 640)); err != nil {
		t.Fatal(err)
	}
	audio.Sync()
	if _, err = k.CheckpointRecording(ctx, e, draft.ID, true, "Interrupted"); err != nil {
		t.Fatal(err)
	}
	audio.Close()
	tables := []string{"pages", "blocks", "files", "attachments", "mutation_history", "page_snapshots", "committed_events"}
	expected := map[string]string{}
	for _, table := range tables {
		expected[table] = retainedTable(t, k, table)
	}
	destination := filepath.Join(t.TempDir(), "backup.trove.age")
	staging := t.TempDir()
	manifest, err := k.CreateEncryptedBackup(ctx, e, destination, "portable test passphrase", staging, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(manifest.Assets) != 1 || len(manifest.Recordings) != 2 {
		t.Fatalf("retained inventory missing: %+v", manifest)
	}
	assertPortableStagesEmpty(t, staging)
	raw, err := os.ReadFile(destination)
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"Private portable snapshot title", "private portable asset bytes", "portable test passphrase", "manifest.json", k.ID()} {
		if bytes.Contains(raw, []byte(secret)) {
			t.Fatalf("plaintext exposed: %q", secret)
		}
	}
	if !bytes.HasPrefix(raw, []byte("age-encryption.org/v1\n")) {
		t.Fatal("not standard age format")
	}
	verified, err := OpenEncryptedBackup(ctx, destination, "portable test passphrase", staging, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer verified.Close()
	if string(marshal(verified.Manifest)) != string(marshal(manifest)) {
		t.Fatal("manifest changed")
	}
	// The encrypted payload is a full SQLite snapshot. Search and approval
	// bytes are included even though restore rebuilds/revokes their live state.
	snapshotDB, err := sql.Open("sqlite3", (&url.URL{Scheme: "file", Path: filepath.Join(verified.stage.path, "snapshot", "workspace.db")}).String()+"?mode=ro&immutable=1")
	if err != nil {
		t.Fatal(err)
	}
	var indexed int
	var approvalState string
	if err = snapshotDB.QueryRow("SELECT count(*) FROM search_chunks").Scan(&indexed); err != nil || indexed == 0 {
		t.Fatal("snapshot lost included search tables", indexed, err)
	}
	if err = snapshotDB.QueryRow("SELECT state FROM approvals WHERE approval_id=?", approval).Scan(&approvalState); err != nil || approvalState != "approved" {
		t.Fatal("snapshot did not preserve approval evidence", approvalState, err)
	}
	snapshotDB.Close()
	if info, err := os.Stat(verified.stage.path); err != nil || info.Mode().Perm() != 0700 {
		t.Fatal("non-private staging", info, err)
	}
	if removed, err := CleanupAbandonedBackupStages(staging); err != nil || removed != 0 {
		t.Fatal("removed live review", removed, err)
	}
	restoredPath := filepath.Join(t.TempDir(), "restored")
	result, err := verified.Restore(ctx, restoredPath, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !result.IndexRebuilt {
		t.Fatal("search not rebuilt")
	}
	restored, err := Open(restoredPath)
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()
	if err = restored.db.QueryRow("SELECT state FROM approvals WHERE approval_id=?", approval).Scan(&approvalState); err != nil || approvalState != "revoked" {
		t.Fatal("restored approval remains active", approvalState, err)
	}
	for _, table := range tables {
		if got := retainedTable(t, restored, table); got != expected[table] {
			t.Fatalf("%s changed", table)
		}
	}
	if _, err = verified.Restore(ctx, restoredPath, nil); !errors.Is(err, os.ErrExist) {
		t.Fatal("overwrote destination", err)
	}
	// A preview pins the authenticated snapshot but does not bypass verification
	// at execution: even private staged content changed after review is rejected.
	asset := filepath.Join(verified.stage.path, "snapshot", filepath.FromSlash(manifest.Assets[0].Path))
	if err = os.WriteFile(asset, []byte("changed after review"), 0600); err != nil {
		t.Fatal(err)
	}
	changedDestination := filepath.Join(t.TempDir(), "changed-review")
	if _, err = verified.Restore(ctx, changedDestination, nil); err == nil {
		t.Fatal("changed reviewed bytes restored")
	}
	if _, err = os.Stat(changedDestination); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("changed review published a destination", err)
	}
	verified.Close()
	assertPortableStagesEmpty(t, staging)
	if _, err = verified.Restore(ctx, filepath.Join(t.TempDir(), "closed"), nil); err == nil {
		t.Fatal("closed preview reused")
	}
}

func TestPortableBackupSummarySeparatesPayloadFromRestoration(t *testing.T) {
	summary := PortableBackupSummary()
	excluded := strings.ToLower(strings.Join(summary.Excluded, " "))
	for _, includedTerm := range []string{"search", "approval", "pending execution"} {
		if strings.Contains(excluded, includedTerm) {
			t.Fatalf("summary falsely excludes database state: %s", includedTerm)
		}
	}
	if !strings.Contains(strings.Join(summary.Included, " "), "derived search tables") || !strings.Contains(strings.Join(summary.Included, " "), "approval/job state") {
		t.Fatal("summary omits included SQLite state")
	}
	if !strings.Contains(strings.Join(summary.Restoration, " "), "rebuilt") || !strings.Contains(strings.Join(summary.Restoration, " "), "revoked") {
		t.Fatal("restore transformations are not disclosed separately")
	}
}

func encryptedTestPayload(t *testing.T, raw []byte) []byte {
	t.Helper()
	recipient, err := age.NewScryptRecipient("test-only-passphrase")
	if err != nil {
		t.Fatal(err)
	}
	// Test fixtures keep cryptographic framing/authentication real but use a
	// smaller work factor. The production writer always keeps age's default.
	recipient.SetWorkFactor(10)
	var output bytes.Buffer
	w, err := age.Encrypt(&output, recipient)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = w.Write(raw); err != nil {
		t.Fatal(err)
	}
	if err = w.Close(); err != nil {
		t.Fatal(err)
	}
	return output.Bytes()
}
func TestEncryptedBackupAuthenticatesBeforeParsing(t *testing.T) {
	// Invalid tar is intentional: with any damage, the authentication failure
	// must occur before parsing can observe the invalid plaintext.
	valid := encryptedTestPayload(t, bytes.Repeat([]byte("not a tar archive"), 10000))
	corrupt := bytes.Clone(valid)
	corrupt[len(corrupt)-1] ^= 1
	cases := []struct {
		name       string
		data       []byte
		passphrase string
	}{
		{"wrong passphrase", valid, "wrong"}, {"truncated", valid[:len(valid)-1], "test-only-passphrase"}, {"corrupt last chunk", corrupt, "test-only-passphrase"}, {"invalid header", []byte("not age"), "test-only-passphrase"},
		{"trailing encrypted bytes", append(bytes.Clone(valid), []byte("extra")...), "test-only-passphrase"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "test.age")
			os.WriteFile(path, tc.data, 0600)
			staging := t.TempDir()
			parsed := false
			_, err := OpenEncryptedBackup(context.Background(), path, tc.passphrase, staging, func(p BackupProgress) {
				if p.Phase == "verifying" || p.Phase == "review" {
					parsed = true
				}
			})
			if err == nil || parsed || !strings.Contains(err.Error(), "authenticat") {
				t.Fatalf("authentication boundary crossed: parsed=%v error=%v", parsed, err)
			}
			assertPortableStagesEmpty(t, staging)
		})
	}
}

func TestEncryptedBackupCancellationCleansStagingAndDestination(t *testing.T) {
	k, e := testKernel(t)
	createPage(t, k, e, "Cancellation source")
	for _, phase := range []string{"snapshot", "encrypting"} {
		t.Run(phase, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			staging := t.TempDir()
			destination := filepath.Join(t.TempDir(), "cancelled.age")
			_, err := k.CreateEncryptedBackup(ctx, e, destination, "cancellation passphrase", staging, func(p BackupProgress) {
				if p.Phase == phase {
					cancel()
				}
			})
			if !errors.Is(err, context.Canceled) {
				t.Fatal(err)
			}
			if _, err = os.Lstat(destination); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("cancelled destination visible", err)
			}
			assertPortableStagesEmpty(t, staging)
			entries, _ := os.ReadDir(filepath.Dir(destination))
			if len(entries) != 0 {
				t.Fatal("partial ciphertext remains", entries)
			}
			var pins int
			if err = k.db.QueryRow("SELECT count(*) FROM backup_pins").Scan(&pins); err != nil || pins != 0 {
				t.Fatal("asset pins leaked", pins, err)
			}
		})
	}
	t.Run("authentication", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "test.age")
		os.WriteFile(path, encryptedTestPayload(t, bytes.Repeat([]byte("x"), 200000)), 0600)
		staging := t.TempDir()
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		_, err := OpenEncryptedBackup(ctx, path, "test-only-passphrase", staging, func(p BackupProgress) {
			if p.Phase == "authenticating" && p.Bytes != "0" {
				cancel()
			}
		})
		if !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
		assertPortableStagesEmpty(t, staging)
	})
}

func TestPortableArchiveRejectsUnsafeEntries(t *testing.T) {
	for _, tc := range []struct {
		name      string
		header    tar.Header
		duplicate bool
	}{
		{"traversal", tar.Header{Name: "../outside", Typeflag: tar.TypeReg}, false},
		{"absolute", tar.Header{Name: "/outside", Typeflag: tar.TypeReg}, false},
		{"symlink", tar.Header{Name: "workspace.db", Typeflag: tar.TypeSymlink, Linkname: "/outside"}, false},
		{"undeclared", tar.Header{Name: "credentials.json", Typeflag: tar.TypeReg}, false},
		{"duplicate", tar.Header{Name: "manifest.json", Typeflag: tar.TypeReg}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var raw bytes.Buffer
			w := tar.NewWriter(&raw)
			if err := w.WriteHeader(&tc.header); err != nil {
				t.Fatal(err)
			}
			if tc.duplicate {
				w.WriteHeader(&tc.header)
			}
			w.Close()
			path := filepath.Join(t.TempDir(), "test.age")
			os.WriteFile(path, encryptedTestPayload(t, raw.Bytes()), 0600)
			staging := t.TempDir()
			if _, err := OpenEncryptedBackup(context.Background(), path, "test-only-passphrase", staging, nil); err == nil {
				t.Fatal("unsafe authenticated archive accepted")
			}
			assertPortableStagesEmpty(t, staging)
		})
	}
}

func TestPortableBackupStageRecoveryAndSpace(t *testing.T) {
	parent := t.TempDir()
	live, err := newPortableStage(parent)
	if err != nil {
		t.Fatal(err)
	}
	defer live.close()
	abandoned, err := newPortableStage(parent)
	if err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(abandoned.path, "authenticated.tar"), []byte("sensitive abandoned data"), 0600)
	abandoned.root.Close()
	abandoned.root = nil
	platformfs.ReleaseLock(abandoned.lock)
	abandoned.lock = nil
	unknown := filepath.Join(parent, portableStagePrefix+"unknown")
	os.Mkdir(unknown, 0700)
	os.WriteFile(filepath.Join(unknown, "keep"), []byte("unrelated"), 0600)
	outside := t.TempDir()
	os.Symlink(outside, filepath.Join(parent, portableStagePrefix+"link"))
	n, err := CleanupAbandonedBackupStages(parent)
	if err != nil || n != 1 {
		t.Fatal(n, err)
	}
	if _, err = os.Stat(abandoned.path); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("abandoned plaintext survives", err)
	}
	if _, err = os.Stat(live.path); err != nil {
		t.Fatal("active stage removed", err)
	}
	if _, err = os.Stat(filepath.Join(unknown, "keep")); err != nil {
		t.Fatal("unmarked directory removed", err)
	}
	if err = requirePortableSpace(parent, math.MaxInt64); err == nil || !strings.Contains(err.Error(), "Not enough free space") {
		t.Fatal("low-space preflight absent", err)
	}
	for _, value := range []struct {
		def  string
		data any
	}{{"NativeBackupProgress", BackupProgress{Version: 1, Phase: "authenticating", Bytes: "1048576", TotalBytes: "2097152"}}, {"NativeBackupSummary", PortableBackupSummary()}} {
		if err = contracts.Validate("service", value.def, marshal(value.data)); err != nil {
			t.Fatal(err)
		}
	}
}
