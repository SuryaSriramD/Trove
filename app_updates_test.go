package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
	"trove.local/phase0/internal/updates"
	"trove.local/phase0/internal/workspace"
)

func TestUpdateReviewOneUseKindAndExpiry(t *testing.T) {
	a := NewApp()
	defer updateStates.Delete(a)
	id := a.saveUpdateReview(updateReview{kind: "update", source: "native-only", workspaceID: "original"})
	if _, err := a.takeUpdateReview(id, "rollback"); err == nil {
		t.Fatal("accepted wrong review kind")
	}
	if _, err := a.takeUpdateReview(id, "update"); err == nil {
		t.Fatal("reused rejected token")
	}
	id = a.saveUpdateReview(updateReview{kind: "update", source: "native-only", workspaceID: "original"})
	p, err := a.takeUpdateReview(id, "update")
	if err != nil || p.source != "native-only" || p.workspaceID != "original" {
		t.Fatal("review binding changed", err)
	}
	if _, err = a.takeUpdateReview(id, "update"); err == nil {
		t.Fatal("reused consumed review")
	}
	s := a.updatesState()
	s.mu.Lock()
	s.reviews["expired"] = updateReview{kind: "update", expires: time.Now().Add(-time.Second)}
	s.mu.Unlock()
	if _, err = a.takeUpdateReview("expired", "update"); err == nil {
		t.Fatal("expired review accepted")
	}
	for i := 0; i < 100; i++ {
		a.saveUpdateReview(updateReview{kind: "update"})
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.reviews) > 16 {
		t.Fatal("unbounded pending reviews")
	}
}

func TestReviewedUpdateSnapshotBindsManifestBytesAndRestoreInventory(t *testing.T) {
	ctx := context.Background()
	base := t.TempDir()
	k, err := workspace.Create(filepath.Join(base, "workspace"))
	if err != nil {
		t.Fatal(err)
	}
	defer k.Close()
	path := filepath.Join(base, "backup")
	m, err := k.CreateBackup(ctx, k.OwnerContext(), path)
	if err != nil {
		t.Fatal(err)
	}
	descriptor, reviewed, err := readReviewedUpdateSnapshot(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	if reviewed.BackupID != m.BackupID || descriptor.WorkspaceID != k.ID() {
		t.Fatal("reviewed manifest binding incorrect")
	}
	raw, err := os.ReadFile(filepath.Join(path, "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(raw)
	if descriptor.ManifestSHA256 != hex.EncodeToString(sum[:]) {
		t.Fatal("digest did not describe exact reviewed bytes")
	}
	changed := reviewed
	changed.BackupID = workspace.NewID()
	raw, err = json.Marshal(changed)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(path, "manifest.json"), raw, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = workspace.VerifyBackup(ctx, path); err != nil {
		t.Fatalf("replacement fixture must remain self-consistent: %v", err)
	}
	if err = verifyUpdateSnapshot(ctx, path, descriptor); err == nil {
		t.Fatal("accepted different backup after review")
	}
	destination := filepath.Join(base, "restored")
	if _, err = workspace.RestoreBackupCompatible(ctx, path, destination, reviewed, workspace.CurrentCompatibility()); err == nil {
		t.Fatal("restored replacement manifest")
	}
	if _, err = os.Lstat(destination); !os.IsNotExist(err) {
		t.Fatal("unreviewed backup published")
	}
}
func TestUpdateCancellationAndSingleOperation(t *testing.T) {
	a := NewApp()
	a.ctx = context.Background()
	defer updateStates.Delete(a)
	ctx, done, err := a.beginUpdateOperation()
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err = a.beginUpdateOperation(); err == nil {
		t.Fatal("parallel recovery admitted")
	}
	a.CancelUpdateOperation()
	select {
	case <-ctx.Done():
	case <-time.After(time.Second):
		t.Fatal("cancellation did not reach operation")
	}
	done()
	ctx, done, err = a.beginUpdateOperation()
	if err != nil {
		t.Fatal(err)
	}
	defer done()
	if ctx.Err() != nil {
		t.Fatal("new operation inherited cancellation")
	}
}
func TestRollbackPublisherAndArchitecture(t *testing.T) {
	current := updates.Application{Identity: updates.Identity{BundleID: updates.BundleID, TeamID: "ABCDEFGHIJ", Build: 4, Architecture: "arm64"}}
	kit := updates.Kit{Previous: current}
	kit.Previous.Identity.Build = 2
	if err := compatibleRollbackPublisher(current, kit); err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"publisher", "app", "architecture", "future"} {
		t.Run(kind, func(t *testing.T) {
			changed := kit
			switch kind {
			case "publisher":
				changed.Previous.Identity.TeamID = "ZZZZZZZZZZ"
			case "app":
				changed.Previous.Identity.BundleID = "other"
			case "architecture":
				changed.Previous.Identity.Architecture = "amd64"
			case "future":
				changed.Previous.Identity.Build = 5
			}
			if compatibleRollbackPublisher(current, changed) == nil {
				t.Fatal("untrusted paired rollback accepted")
			}
		})
	}
}
