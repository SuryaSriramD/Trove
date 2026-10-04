package workspace

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"trove.local/phase0/contracts"
)

type BackupCleanupItem struct {
	Name     string         `json:"name"`
	Manifest BackupManifest `json:"manifest"`
}
type BackupCleanupPlan struct {
	WorkspaceID string              `json:"workspaceId"`
	KeepCount   int                 `json:"keepCount"`
	Remove      []BackupCleanupItem `json:"remove"`
	Warnings    []string            `json:"warnings"`
	parent      string
}

// Cleanup only considers this workspace's verified generated backup directories.
// Unknown files or backups with failed integrity checks are never removed.
func (k *Kernel) PreviewBackupCleanup(ctx context.Context, e contracts.ServiceExecutionContext, parent string) (BackupCleanupPlan, error) {
	plan := BackupCleanupPlan{WorkspaceID: k.ID(), Remove: []BackupCleanupItem{}, Warnings: []string{}, parent: parent}
	settings, err := k.RecoverySettings(ctx, e)
	if err != nil {
		return plan, err
	}
	plan.KeepCount = settings.BackupKeepCount
	if settings.BackupKeepCount == 0 {
		return plan, nil
	}
	entries, err := os.ReadDir(parent)
	if err != nil {
		return plan, err
	}
	candidates := []BackupCleanupItem{}
	for _, entry := range entries {
		if !strings.HasPrefix(entry.Name(), "Trove-backup-") {
			continue
		}
		if !entry.IsDir() || entry.Type()&os.ModeSymlink != 0 {
			continue
		}
		path := filepath.Join(parent, entry.Name())
		manifest, err := VerifyBackup(ctx, path)
		if err != nil {
			plan.Warnings = append(plan.Warnings, entry.Name()+": verification failed; retained")
			continue
		}
		if manifest.WorkspaceID != k.ID() {
			continue
		}
		if _, err = backupCleanupInventory(path, manifest); err != nil {
			plan.Warnings = append(plan.Warnings, entry.Name()+": extra files present; retained")
			continue
		}
		candidates = append(candidates, BackupCleanupItem{Name: entry.Name(), Manifest: manifest})
	}
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].Manifest.CreatedAt == candidates[j].Manifest.CreatedAt {
			return candidates[i].Name > candidates[j].Name
		}
		return candidates[i].Manifest.CreatedAt > candidates[j].Manifest.CreatedAt
	})
	if len(candidates) > plan.KeepCount {
		plan.Remove = candidates[plan.KeepCount:]
	}
	return plan, nil
}
func backupCleanupInventory(root string, m BackupManifest) ([]string, error) {
	allowed := map[string]bool{"manifest.json": true, "workspace.db": true}
	for _, a := range m.Assets {
		allowed[a.Path] = true
	}
	for _, o := range m.Recordings {
		allowed[o.Path] = true
	}
	paths := []string{}
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if path == root {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("unsafe link")
		}
		if entry.IsDir() {
			prefix := rel + "/"
			valid := false
			for p := range allowed {
				if strings.HasPrefix(p, prefix) {
					valid = true
					break
				}
			}
			if !valid {
				return fmt.Errorf("unexpected directory")
			}
		} else if !allowed[rel] {
			return fmt.Errorf("unexpected file")
		}
		paths = append(paths, path)
		return nil
	})
	sort.Slice(paths, func(i, j int) bool { return len(paths[i]) > len(paths[j]) })
	return paths, err
}
func (k *Kernel) ApplyBackupCleanup(ctx context.Context, e contracts.ServiceExecutionContext, plan BackupCleanupPlan) (int, error) {
	if err := requireOwnerScope(k, e); err != nil {
		return 0, err
	}
	if plan.WorkspaceID != k.ID() || plan.parent == "" {
		return 0, Fail("CONFLICT", "Backup cleanup belongs to another workspace")
	}
	removed := 0
	// Validate the entire reviewed set again before deleting the first file.
	inventories := map[string][]string{}
	for _, item := range plan.Remove {
		if filepath.Base(item.Name) != item.Name || !strings.HasPrefix(item.Name, "Trove-backup-") {
			return 0, Fail("INVALID_REQUEST", "Unsafe backup cleanup name")
		}
		root := filepath.Join(plan.parent, item.Name)
		current, err := VerifyBackup(ctx, root)
		if err != nil {
			return 0, err
		}
		if string(marshal(current)) != string(marshal(item.Manifest)) {
			return 0, Fail("CONFLICT", "Reviewed backup changed")
		}
		paths, err := backupCleanupInventory(root, current)
		if err != nil {
			return 0, err
		}
		inventories[root] = paths
	}
	for _, item := range plan.Remove {
		if err := ctx.Err(); err != nil {
			return removed, err
		}
		root := filepath.Join(plan.parent, item.Name)
		for _, path := range inventories[root] {
			if err := os.Remove(path); err != nil {
				return removed, err
			}
		}
		if err := os.Remove(root); err != nil {
			return removed, err
		}
		removed++
	}
	return removed, nil
}
