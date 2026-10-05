package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"
	"trove.local/phase0/internal/updates"
	"trove.local/phase0/internal/workspace"
)

type updateReview struct {
	kind, source, currentPath, workspaceID string
	current, next                          updates.Application
	kit                                    updates.Kit
	backup                                 workspace.BackupManifest
	migration                              workspace.MigrationSnapshotManifest
	expires                                time.Time
}
type updateState struct {
	mu      sync.Mutex
	cancel  context.CancelFunc
	reviews map[string]updateReview
}

var updateStates sync.Map

func (a *App) updatesState() *updateState {
	value, _ := updateStates.LoadOrStore(a, &updateState{reviews: map[string]updateReview{}})
	return value.(*updateState)
}
func (a *App) beginUpdateOperation() (context.Context, func(), error) {
	if err := a.requireRollbackCommitted(); err != nil {
		return nil, nil, err
	}

	a.mu.RLock()
	base := a.ctx
	a.mu.RUnlock()
	if base == nil {
		base = context.Background()
	}
	s := a.updatesState()
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cancel != nil {
		return nil, nil, errors.New("another recovery operation is running")
	}
	ctx, cancel := context.WithTimeout(base, 30*time.Minute)
	s.cancel = cancel
	return ctx, func() { cancel(); s.mu.Lock(); s.cancel = nil; s.mu.Unlock() }, nil
}
func (a *App) CancelUpdateOperation() {
	if err := a.requireRollbackCommitted(); err != nil {
		return
	}

	s := a.updatesState()
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cancel != nil {
		s.cancel()
	}
}
func (a *App) saveUpdateReview(p updateReview) string {
	s := a.updatesState()
	s.mu.Lock()
	defer s.mu.Unlock()
	for id, old := range s.reviews {
		if !old.expires.After(time.Now()) {
			delete(s.reviews, id)
		}
	}
	if len(s.reviews) >= 16 {
		s.reviews = map[string]updateReview{}
	}
	id := workspace.NewID()
	p.expires = time.Now().Add(10 * time.Minute)
	s.reviews[id] = p
	return id
}
func (a *App) takeUpdateReview(id, kind string) (updateReview, error) {
	s := a.updatesState()
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.reviews[id]
	delete(s.reviews, id)
	if !ok || p.kind != kind || !p.expires.After(time.Now()) {
		return updateReview{}, errors.New("review expired; select and review the source again")
	}
	return p, nil
}
func runningBundle() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	exe, err = filepath.EvalSymlinks(exe)
	if err != nil {
		return "", err
	}
	bundle := filepath.Dir(filepath.Dir(filepath.Dir(exe)))
	if filepath.Base(filepath.Dir(exe)) != "MacOS" || filepath.Base(filepath.Dir(filepath.Dir(exe))) != "Contents" || filepath.Ext(bundle) != ".app" {
		return "", errors.New("verified updates require a packaged macOS application")
	}
	return bundle, nil
}
func jsonResult(value any) (string, error) { raw, err := json.Marshal(value); return string(raw), err }

// Native selectors supply all paths. Reviews are one-use, expire, and retain the
// exact trusted app and workspace identities; renderer JSON cannot choose them.
func (a *App) PreviewApplicationUpdate() (string, error) {
	ctx, done, err := a.beginUpdateOperation()
	if err != nil {
		return "", err
	}
	defer done()
	currentPath, err := runningBundle()
	if err != nil {
		return "", err
	}
	v := updates.MacVerifier{}
	current, err := v.Inspect(ctx, currentPath)
	if err != nil {
		return "", err
	}
	source, err := runtime.OpenFileDialog(a.ctx, runtime.OpenDialogOptions{Title: "Choose a signed Trove update", Filters: []runtime.FileFilter{{DisplayName: "Trove application", Pattern: "*.app"}}})
	if err != nil || source == "" {
		return "", err
	}
	if err = ctx.Err(); err != nil {
		return "", err
	}
	next, err := v.Inspect(ctx, source)
	if err != nil {
		return "", err
	}
	if err = updates.VerifyUpgrade(current, next); err != nil {
		return "", err
	}
	a.mu.RLock()
	defer a.mu.RUnlock()
	if a.kernel == nil {
		return "", errors.New("open a workspace before preparing an update")
	}
	descriptor, err := a.kernel.DescribeCompatibility(ctx)
	if err != nil {
		return "", err
	}
	if descriptor.UserVersion != current.Identity.UserVersion || descriptor.StorageGeneration != current.Identity.StorageGeneration {
		return "", errors.New("open workspace is not compatible with the installed application")
	}
	id := a.saveUpdateReview(updateReview{kind: "update", source: source, currentPath: currentPath, workspaceID: a.kernel.ID(), current: current, next: next})
	return jsonResult(map[string]any{"previewId": id, "current": current.Identity, "update": next.Identity, "workspaceId": a.kernel.ID(), "bytes": current.Inventory.Bytes + next.Inventory.Bytes, "warnings": []string{"Save pending edits before continuing. A verified online backup of this workspace and copies of both apps will be saved together.", "Installation is manual: quit Trove, then open the verified update from the completed recovery folder. Keep that folder for rollback.", "For rollback, restore this backup into a new workspace and open it with the saved previous app. Changes made after this backup remain only in the newer workspace."}})
}
func (a *App) PrepareApplicationUpdate(previewID string) (string, error) {
	ctx, done, err := a.beginUpdateOperation()
	if err != nil {
		return "", err
	}
	defer done()
	p, err := a.takeUpdateReview(previewID, "update")
	if err != nil {
		return "", err
	}
	parent, err := runtime.OpenDirectoryDialog(a.ctx, runtime.OpenDialogOptions{Title: "Choose an external recovery folder", CanCreateDirectories: true})
	if err != nil || parent == "" {
		return "", err
	}
	if err = ctx.Err(); err != nil {
		return "", err
	}
	a.lifecycle.RLock()
	defer a.lifecycle.RUnlock()
	a.mu.RLock()
	k, e := a.kernel, a.execution
	a.mu.RUnlock()
	if k == nil || k.ID() != p.workspaceID {
		return "", errors.New("workspace changed; review the update again")
	}
	destination := filepath.Join(parent, "Trove-update-"+workspace.NewID())
	if updates.PathsOverlap(k.Path(), destination) {
		return "", errors.New("save the update recovery folder outside the workspace")
	}
	backup := func(ctx context.Context, path string) (updates.Snapshot, error) {
		if _, err := k.CreateBackup(ctx, e, path); err != nil {
			return updates.Snapshot{}, err
		}
		return describeUpdateSnapshot(ctx, path)
	}
	kit, err := updates.PrepareKit(ctx, updates.MacVerifier{}, p.currentPath, p.source, destination, p.current, p.next, backup, verifyUpdateSnapshot)
	if err != nil {
		return "", err
	}
	id := a.saveUpdateReview(updateReview{kind: "kit", source: destination, kit: kit, current: p.current})
	return jsonResult(map[string]any{"previewId": id, "destination": destination, "kit": kit, "message": "Recovery folder verified. Save edits, quit Trove, then open update/Trove.app in this folder. Keep previous/Trove.app and workspace together for rollback."})
}
func describeUpdateSnapshot(ctx context.Context, path string) (updates.Snapshot, error) {
	result, _, err := readReviewedUpdateSnapshot(ctx, path)
	return result, err
}

// The digest and copy manifest come from the same bounded read. VerifyBackup
// authenticates its declared objects, and its parsed result must equal that read.
func readReviewedUpdateSnapshot(ctx context.Context, path string) (updates.Snapshot, workspace.BackupManifest, error) {
	var result updates.Snapshot
	var m workspace.BackupManifest
	d, err := workspace.ReadWorkspaceCompatibility(ctx, path)
	if err != nil {
		return result, m, err
	}
	root, err := os.OpenRoot(path)
	if err != nil {
		return result, m, err
	}
	defer root.Close()
	st, err := root.Lstat("manifest.json")
	if err != nil || !st.Mode().IsRegular() {
		return result, m, errors.New("invalid backup manifest")
	}
	f, err := root.Open("manifest.json")
	if err != nil {
		return result, m, err
	}
	defer f.Close()
	opened, err := f.Stat()
	if err != nil || !os.SameFile(st, opened) {
		return result, m, errors.New("backup manifest changed while opening")
	}
	raw, err := io.ReadAll(io.LimitReader(f, (16<<20)+1))
	if err != nil || len(raw) > 16<<20 {
		return result, m, errors.New("backup manifest is too large")
	}
	if err = json.Unmarshal(raw, &m); err != nil {
		return result, m, err
	}
	verified, err := workspace.VerifyBackup(ctx, path)
	if err != nil {
		return result, m, err
	}
	if !reflect.DeepEqual(verified, m) {
		return result, m, errors.New("backup changed during verification")
	}
	digest := sha256.Sum256(raw)
	return updates.Snapshot{WorkspaceID: m.WorkspaceID, BackupID: m.BackupID, ManifestSHA256: hex.EncodeToString(digest[:]), UserVersion: d.UserVersion, StorageGeneration: d.StorageGeneration}, m, nil
}
func verifyUpdateSnapshot(ctx context.Context, path string, expected updates.Snapshot) error {
	actual, err := describeUpdateSnapshot(ctx, path)
	if err != nil {
		return err
	}
	if actual != expected {
		return errors.New("reviewed workspace backup changed")
	}
	return nil
}
func (a *App) PreviewApplicationRollback() (string, error) {
	ctx, done, err := a.beginUpdateOperation()
	if err != nil {
		return "", err
	}
	defer done()
	currentPath, err := runningBundle()
	if err != nil {
		return "", err
	}
	current, err := (updates.MacVerifier{}).Inspect(ctx, currentPath)
	if err != nil {
		return "", err
	}
	source, err := runtime.OpenDirectoryDialog(a.ctx, runtime.OpenDialogOptions{Title: "Choose a completed Trove update recovery folder"})
	if err != nil || source == "" {
		return "", err
	}
	if err = ctx.Err(); err != nil {
		return "", err
	}
	kit, err := updates.InspectKit(ctx, updates.MacVerifier{}, source, verifyUpdateSnapshot)
	if err != nil {
		return "", err
	}
	if err = compatibleRollbackPublisher(current, kit); err != nil {
		return "", err
	}
	descriptor, manifest, err := readReviewedUpdateSnapshot(ctx, filepath.Join(source, "workspace"))
	if err != nil {
		return "", err
	}
	if descriptor != kit.Snapshot {
		return "", errors.New("recovery backup changed; review it again")
	}
	id := a.saveUpdateReview(updateReview{kind: "rollback", source: source, current: current, kit: kit, backup: manifest})
	return jsonResult(map[string]any{"previewId": id, "previous": kit.Previous.Identity, "snapshot": kit.Snapshot, "createdAt": kit.CreatedAt, "warnings": []string{"Restore creates a new workspace. Keep your newer workspace to retain changes made after this backup.", "After restoring, use the reviewed launch button to open the restored copy with the saved previous app. This selects the compatible copy before that app opens any workspace."}})
}
func compatibleRollbackPublisher(current updates.Application, kit updates.Kit) error {
	if current.Identity.BundleID != kit.Previous.Identity.BundleID || current.Identity.TeamID != kit.Previous.Identity.TeamID || current.Identity.Architecture != kit.Previous.Identity.Architecture || current.Identity.Build < kit.Previous.Identity.Build {
		return errors.New("recovery folder is not compatible with this installed publisher or architecture")
	}
	return nil
}
func (a *App) RestoreApplicationRollback(previewID string) (string, error) {
	ctx, done, err := a.beginUpdateOperation()
	if err != nil {
		return "", err
	}
	defer done()
	p, err := a.takeUpdateReview(previewID, "rollback")
	if err != nil {
		return "", err
	}
	kit, err := updates.InspectKit(ctx, updates.MacVerifier{}, p.source, verifyUpdateSnapshot)
	if err != nil {
		return "", err
	}
	if !reflect.DeepEqual(kit, p.kit) {
		return "", errors.New("recovery folder changed; review it again")
	}
	if err = compatibleRollbackPublisher(p.current, kit); err != nil {
		return "", err
	}
	parent, err := runtime.OpenDirectoryDialog(a.ctx, runtime.OpenDialogOptions{Title: "Choose a folder for the compatible rollback workspace", CanCreateDirectories: true})
	if err != nil || parent == "" {
		return "", err
	}
	if err = ctx.Err(); err != nil {
		return "", err
	}
	destination := filepath.Join(parent, "Trove-rollback-"+workspace.NewID())
	if updates.PathsOverlap(p.source, destination) {
		return "", errors.New("restore outside the recovery folder")
	}
	a.mu.RLock()
	if a.kernel != nil && updates.PathsOverlap(a.kernel.Path(), destination) {
		a.mu.RUnlock()
		return "", errors.New("restore outside the open workspace")
	}
	a.mu.RUnlock()
	source := filepath.Join(p.source, "workspace")
	if err = verifyUpdateSnapshot(ctx, source, kit.Snapshot); err != nil {
		return "", err
	}
	if _, err = workspace.RestoreBackupCompatible(ctx, source, destination, p.backup, workspace.CompatibilityDescriptor{UserVersion: kit.Snapshot.UserVersion, StorageGeneration: kit.Snapshot.StorageGeneration}); err != nil {
		return "", err
	}
	d, err := workspace.ReadWorkspaceCompatibility(ctx, destination)
	if err != nil {
		return "", err
	}
	if d.UserVersion != kit.Previous.Identity.UserVersion || d.StorageGeneration != kit.Previous.Identity.StorageGeneration {
		return "", errors.New("restored format is not compatible with the saved previous application")
	}
	application := filepath.Join(p.source, "previous", "Trove.app")
	launchID, launchErr := a.saveRollbackLaunchReviewContext(ctx, application, kit.Previous, destination)
	message := "Compatible workspace restored. Use the launch button to select this restored copy before the previous app starts. Your newer workspace remains preserved."
	if launchErr != nil {
		message = "Compatible workspace restored, but a safe launch review could not be prepared: " + launchErr.Error() + ". Keep both folders; do not open the previous app against your newer workspace."
	}
	return jsonResult(map[string]any{"destination": destination, "application": application, "launchReviewId": launchID, "message": message})
}
func (a *App) RevealApplicationUpdate(previewID string) error {
	ctx, done, err := a.beginUpdateOperation()
	if err != nil {
		return err
	}
	defer done()
	p, err := a.takeUpdateReview(previewID, "kit")
	if err != nil {
		return err
	}
	kit, err := updates.InspectKit(ctx, updates.MacVerifier{}, p.source, verifyUpdateSnapshot)
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(kit, p.kit) {
		return errors.New("recovery folder changed; review it again")
	}
	return updates.Reveal(ctx, filepath.Join(p.source, "update", "Trove.app"))
}

// This entry point also works on the welcome screen after workspace.Open fails.
func (a *App) PreviewMigrationRecovery() (string, error) {
	ctx, done, err := a.beginUpdateOperation()
	if err != nil {
		return "", err
	}
	defer done()
	source, err := runtime.OpenDirectoryDialog(a.ctx, runtime.OpenDialogOptions{Title: "Choose the workspace whose upgrade needs recovery"})
	if err != nil || source == "" {
		return "", err
	}
	if err = ctx.Err(); err != nil {
		return "", err
	}
	status, err := workspace.ReadMigrationStatus(ctx, source)
	if err != nil {
		return "", fmt.Errorf("migration snapshot cannot be verified: %w", err)
	}
	if status == nil {
		return "", errors.New("this workspace has no recorded pre-migration snapshot")
	}
	m, err := workspace.VerifyMigrationSnapshot(ctx, status.SnapshotPath)
	if err != nil {
		return "", err
	}
	id := a.saveUpdateReview(updateReview{kind: "migration", source: status.SnapshotPath, migration: m})
	return jsonResult(map[string]any{"previewId": id, "status": status, "snapshot": m, "warnings": []string{"Restore copies the pre-upgrade database, managed files, and original recording bytes into a new folder.", "Use a compatible application to open the restored folder. Opening it with this version will attempt the upgrade again."}})
}
func (a *App) RestoreMigrationRecovery(previewID string) (string, error) {
	ctx, done, err := a.beginUpdateOperation()
	if err != nil {
		return "", err
	}
	defer done()
	p, err := a.takeUpdateReview(previewID, "migration")
	if err != nil {
		return "", err
	}
	m, err := workspace.VerifyMigrationSnapshot(ctx, p.source)
	if err != nil {
		return "", err
	}
	if !reflect.DeepEqual(m, p.migration) {
		return "", errors.New("migration snapshot changed; review it again")
	}
	parent, err := runtime.OpenDirectoryDialog(a.ctx, runtime.OpenDialogOptions{Title: "Choose a folder for the recovered workspace", CanCreateDirectories: true})
	if err != nil || parent == "" {
		return "", err
	}
	if err = ctx.Err(); err != nil {
		return "", err
	}
	destination := filepath.Join(parent, "Trove-migration-recovery-"+workspace.NewID())
	if updates.PathsOverlap(filepath.Dir(filepath.Dir(p.source)), destination) {
		return "", errors.New("restore outside the original workspace")
	}
	if _, err = workspace.RestoreMigrationSnapshotReviewed(ctx, p.source, destination, p.migration); err != nil {
		return "", err
	}
	return jsonResult(map[string]any{"destination": destination, "format": m.Before, "message": "Pre-upgrade workspace restored. Use a compatible application to open this new folder."})
}
