package workspace

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func capturedExternalScan(t *testing.T, k *Kernel, id string, content string) (ExternalRoot, []externalScanSource) {
	t.Helper()
	var root ExternalRoot
	err := k.db.QueryRow("SELECT r.id,r.path,r.identity,r.repository,r.approved,r.last_scan,a.epoch FROM external_roots r JOIN external_root_approvals a ON a.root_id=r.id WHERE r.id=?", id).Scan(&root.ID, &root.Path, &root.Identity, &root.Repository, &root.Approved, &root.LastScan, &root.ApprovalEpoch)
	if err != nil {
		t.Fatal(err)
	}
	result, err := listExternalSources(context.Background(), k, k.OwnerContext(), map[string]any{"rootId": id})
	if err != nil {
		t.Fatal(err)
	}
	sources := result.([]ExternalSource)
	if len(sources) != 1 {
		t.Fatal(sources)
	}
	_, chunks := languageChunks(sources[0].Path, content)
	return root, []externalScanSource{{sources[0], chunks}}
}
func TestIndexReapprovalImmediatelyInvalidatesPriorDirectoryText(t *testing.T) {
	k, e := testKernel(t)
	ctx := context.Background()
	folder := filepath.Join(t.TempDir(), "approved")
	if err := os.Mkdir(folder, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(folder, "note.txt"), []byte("PRIOR_DIRECTORY_TEXT"), 0600); err != nil {
		t.Fatal(err)
	}
	root, err := k.ApproveIndexRoot(ctx, e, folder, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = scanExternalRoot(ctx, k, e, map[string]any{"rootId": root.ID}); err != nil {
		t.Fatal(err)
	}
	if err = os.Rename(folder, folder+"-old"); err != nil {
		t.Fatal(err)
	}
	if err = os.Mkdir(folder, 0700); err != nil {
		t.Fatal(err)
	}
	if _, err = k.ApproveIndexRoot(ctx, e, folder, false); err != nil {
		t.Fatal(err)
	}
	hits, err := searchExternalSources(ctx, k, e, map[string]any{"query": "PRIOR_DIRECTORY_TEXT"})
	if err != nil || len(hits.([]any)) != 0 {
		t.Fatal("old directory remains searchable after new approval", hits, err)
	}
}
func TestIndexOlderScanCannotOverwriteCompletedNewerScan(t *testing.T) {
	k, e := testKernel(t)
	ctx := context.Background()
	folder := t.TempDir()
	path := filepath.Join(folder, "note.txt")
	if err := os.WriteFile(path, []byte("OLDER_SNAPSHOT"), 0600); err != nil {
		t.Fatal(err)
	}
	root, err := k.ApproveIndexRoot(ctx, e, folder, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = scanExternalRoot(ctx, k, e, map[string]any{"rootId": root.ID}); err != nil {
		t.Fatal(err)
	}
	selected, sources := capturedExternalScan(t, k, root.ID, "OLDER_SNAPSHOT")
	if err = os.WriteFile(path, []byte("NEWER_SNAPSHOT"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = scanExternalRoot(ctx, k, e, map[string]any{"rootId": root.ID}); err != nil {
		t.Fatal(err)
	}
	if _, err = publishExternalScan(ctx, k, e, selected, sources, now()); err == nil {
		t.Fatal("older enumeration overwrote newer completed scan")
	}
	hits, err := searchExternalSources(ctx, k, e, map[string]any{"query": "NEWER_SNAPSHOT"})
	if err != nil || len(hits.([]any)) != 1 {
		t.Fatal("newer indexed text was lost", hits, err)
	}
}

func TestIndexOldSelectionCannotPublishOrInvalidateReapprovedDirectory(t *testing.T) {
	k, e := testKernel(t)
	ctx := context.Background()
	folder := filepath.Join(t.TempDir(), "approved")
	if err := os.Mkdir(folder, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(folder, "note.txt"), []byte("OLD_SELECTION"), 0600); err != nil {
		t.Fatal(err)
	}
	root, err := k.ApproveIndexRoot(ctx, e, folder, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = scanExternalRoot(ctx, k, e, map[string]any{"rootId": root.ID}); err != nil {
		t.Fatal(err)
	}
	selected, sources := capturedExternalScan(t, k, root.ID, "OLD_SELECTION")
	if err = os.Rename(folder, folder+"-old"); err != nil {
		t.Fatal(err)
	}
	if err = os.Mkdir(folder, 0700); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(folder, "note.txt"), []byte("NEW_SELECTION"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = k.ApproveIndexRoot(ctx, e, folder, false); err != nil {
		t.Fatal(err)
	}
	if _, err = scanExternalRoot(ctx, k, e, map[string]any{"rootId": root.ID}); err != nil {
		t.Fatal(err)
	}
	if _, err = publishExternalScan(ctx, k, e, selected, sources, now()); err == nil {
		t.Fatal("old selection published after new native approval")
	}
	if err = invalidateExternalScan(ctx, k, e, selected); err == nil {
		t.Fatal("old failed scan invalidated new native approval")
	}
	hits, err := searchExternalSources(ctx, k, e, map[string]any{"query": "NEW_SELECTION"})
	if err != nil || len(hits.([]any)) != 1 {
		t.Fatal("new selection text was lost", hits, err)
	}
	current, currentSources := capturedExternalScan(t, k, root.ID, "NEW_SELECTION")
	if _, err = revokeExternalRoot(ctx, k, e, map[string]any{"rootId": root.ID}); err != nil {
		t.Fatal(err)
	}
	if _, err = publishExternalScan(ctx, k, e, current, currentSources, now()); err == nil {
		t.Fatal("revoked scan republished text")
	}
}

func TestIndexEveryApprovalFencesAnUnfinishedFirstScan(t *testing.T) {
	for _, revoke := range []bool{false, true} {
		t.Run(map[bool]string{false: "reapprove", true: "revoke_reapprove"}[revoke], func(t *testing.T) {
			k, e := testKernel(t)
			ctx := context.Background()
			folder := t.TempDir()
			selected, err := k.ApproveIndexRoot(ctx, e, folder, false)
			if err != nil {
				t.Fatal(err)
			}
			if selected.LastScan != "" {
				t.Fatal("fixture unexpectedly completed a scan")
			}
			if revoke {
				if _, err = revokeExternalRoot(ctx, k, e, map[string]any{"rootId": selected.ID}); err != nil {
					t.Fatal(err)
				}
			}
			if _, err = k.ApproveIndexRoot(ctx, e, folder, false); err != nil {
				t.Fatal(err)
			}
			if _, err = publishExternalScan(ctx, k, e, selected, nil, now()); err == nil {
				t.Fatal("old first scan published under a later approval of the same folder")
			}
			if err = invalidateExternalScan(ctx, k, e, selected); err == nil {
				t.Fatal("old first scan invalidated a later approval")
			}
		})
	}
}

func TestIndexApprovalEpochPersistsAndMigratesLegacyRoots(t *testing.T) {
	k, e := testKernel(t)
	ctx := context.Background()
	root, err := k.ApproveIndexRoot(ctx, e, t.TempDir(), false)
	if err != nil {
		t.Fatal(err)
	}
	workspacePath := k.Path()
	if err = k.Close(); err != nil {
		t.Fatal(err)
	}
	k, err = Open(workspacePath)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = k.Close() }()
	var epoch string
	if err = k.db.QueryRow("SELECT epoch FROM external_root_approvals WHERE root_id=?", root.ID).Scan(&epoch); err != nil || epoch != root.ApprovalEpoch || epoch == "" {
		t.Fatal("approval did not persist", epoch, err)
	}
	// A retained workspace from before this addition has no approval table.
	if _, err = k.db.Exec("DROP TABLE external_root_approvals"); err != nil {
		t.Fatal(err)
	}
	if err = k.Close(); err != nil {
		t.Fatal(err)
	}
	k, err = Open(workspacePath)
	if err != nil {
		t.Fatal(err)
	}
	if err = k.db.QueryRow("SELECT epoch FROM external_root_approvals WHERE root_id=?", root.ID).Scan(&epoch); err != nil || epoch == "" || epoch == root.ApprovalEpoch {
		t.Fatal("legacy root was not assigned a fresh persisted epoch", epoch, err)
	}
	if _, err = publishExternalScan(ctx, k, k.OwnerContext(), root, nil, now()); err == nil {
		t.Fatal("old epoch survived legacy migration")
	}
	if _, err = scanExternalRoot(ctx, k, k.OwnerContext(), map[string]any{"rootId": root.ID}); err != nil {
		t.Fatal("migrated root could not refresh", err)
	}
}

func TestIndexApprovalEpochBackupRestoreRequiresNewApproval(t *testing.T) {
	for _, legacy := range []bool{false, true} {
		t.Run(map[bool]string{false: "current", true: "legacy_without_epochs"}[legacy], func(t *testing.T) {
			k, e := testKernel(t)
			ctx := context.Background()
			folder := t.TempDir()
			prior, err := k.ApproveIndexRoot(ctx, e, folder, false)
			if err != nil {
				t.Fatal(err)
			}
			if legacy {
				if _, err = k.db.Exec("DROP TABLE external_root_approvals"); err != nil {
					t.Fatal(err)
				}
			}
			backup := filepath.Join(t.TempDir(), "backup")
			if _, err = k.CreateBackup(ctx, e, backup); err != nil {
				t.Fatal(err)
			}
			destination := filepath.Join(t.TempDir(), "restored")
			if _, err = RestoreBackup(ctx, backup, destination); err != nil {
				t.Fatal(err)
			}
			restored, err := Open(destination)
			if err != nil {
				t.Fatal(err)
			}
			defer restored.Close()
			owner := restored.OwnerContext()
			if _, err = scanExternalRoot(ctx, restored, owner, map[string]any{"rootId": prior.ID}); err == nil {
				t.Fatal("restored folder approval reactivated")
			}
			current, err := restored.ApproveIndexRoot(ctx, owner, folder, false)
			if err != nil || current.ApprovalEpoch == prior.ApprovalEpoch || current.ApprovalEpoch == "" {
				t.Fatal("restored selection did not get a new epoch", current, err)
			}
			if _, err = publishExternalScan(ctx, restored, owner, prior, nil, now()); err == nil {
				t.Fatal("old selection published after restoration")
			}
			if _, err = scanExternalRoot(ctx, restored, owner, map[string]any{"rootId": current.ID}); err != nil {
				t.Fatal(err)
			}
		})
	}
}
