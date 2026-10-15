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
	err := k.db.QueryRow("SELECT id,path,identity,repository,approved,last_scan FROM external_roots WHERE id=?", id).Scan(&root.ID, &root.Path, &root.Identity, &root.Repository, &root.Approved, &root.LastScan)
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
