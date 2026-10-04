package workspace

import (
	"context"
	"database/sql"
	"encoding/json"
	"sort"
	"trove.local/phase0/contracts"
)

func init() {
	RegisterQuery("history.previewSubtree", previewSubtreeRecovery)
	RegisterQuery("recovery.settings", recoverySettings)
	RegisterQuery("recovery.setRetention", setRecoveryRetention)
}

// Each child uses its last retained live revision, then one normal batch restores
// the complete reviewed set. The ordinary mutation guards make racing edits fail
// atomically; parents precede children and shared schemas never roll back.
func previewSubtreeRecovery(ctx context.Context, k *Kernel, e contracts.ServiceExecutionContext, r map[string]any) (any, error) {
	root := str(r["pageId"])
	if root == "" {
		return nil, Fail("INVALID_REQUEST", "A subtree root is required")
	}
	if err := k.authorizePage(ctx, k.db, e, root); err != nil {
		return nil, err
	}
	ids, err := descendantPages(ctx, k.db, root)
	if err != nil {
		return nil, err
	}
	if len(ids) > 200 {
		return nil, Fail("RESOURCE_EXHAUSTED", "Recover this subtree in groups of at most 200 pages")
	}
	var deleted sql.NullString
	if err = k.db.QueryRowContext(ctx, "SELECT deleted_at FROM pages WHERE id=?", root).Scan(&deleted); err != nil {
		return nil, err
	}
	if !deleted.Valid {
		return nil, Fail("INVALID_REQUEST", "Choose a page in trash")
	}
	commands := []any{}
	previews := []any{}
	guards := map[string]contracts.ServiceExpectedRevision{}
	for _, id := range ids {
		if err = k.authorizePage(ctx, k.db, e, id); err != nil {
			return nil, err
		}
		var rev string
		err = k.db.QueryRowContext(ctx, `SELECT revision FROM page_snapshots WHERE page_id=? AND json_extract(snapshot_json,'$.page.deletedAt') IS NULL ORDER BY length(revision) DESC,revision DESC LIMIT 1`, id).Scan(&rev)
		if err != nil {
			return nil, Fail("CONFLICT", "A subtree page has no retained live revision")
		}
		value, err := previewRestore(ctx, k, e, map[string]any{"pageId": id, "restoreRevision": rev})
		if err != nil {
			return nil, err
		}
		preview := value.(map[string]any)
		if preview["canRestore"] == false {
			return nil, Fail("CONFLICT", "A subtree page needs historical property decisions; review and restore that page first")
		}
		for _, g := range preview["expectedRevisions"].([]contracts.ServiceExpectedRevision) {
			key := guardKey(g.Target.EntityType, string(g.Target.EntityId))
			if old, ok := guards[key]; ok && old.Revision != g.Revision {
				return nil, Fail("CONFLICT", "The subtree changed while preparing review; preview again")
			}
			guards[key] = g
		}
		commands = append(commands, map[string]any{"kind": "history.restore", "pageId": id, "restoreRevision": rev, "previewId": preview["previewId"]})
		previews = append(previews, preview)
	}

	// Historical parent edges can differ from the current trash tree. Order the
	// reviewed snapshots by those edges and reject missing external parents now.
	byPage := map[string]int{}
	for i, id := range ids {
		byPage[id] = i
	}
	visiting := map[string]bool{}
	visited := map[string]bool{}
	orderedCommands := []any{}
	orderedPreviews := []any{}
	var visit func(string) error
	visit = func(id string) error {
		if visited[id] {
			return nil
		}
		if visiting[id] {
			return Fail("CONFLICT", "Historical subtree parent references form a cycle")
		}
		visiting[id] = true
		index := byPage[id]
		preview := previews[index].(map[string]any)
		snapshot := preview["snapshot"].(contracts.ServiceReadPageResult)
		if snapshot.Page.ParentPageId != nil {
			parent := string(*snapshot.Page.ParentPageId)
			if _, included := byPage[parent]; included {
				if err := visit(parent); err != nil {
					return err
				}
			} else {
				var deleted sql.NullString
				if err := k.db.QueryRowContext(ctx, "SELECT deleted_at FROM pages WHERE id=?", parent).Scan(&deleted); err != nil {
					return Fail("CONFLICT", "Historical parent is unavailable")
				}
				if deleted.Valid {
					return Fail("CONFLICT", "Restore the historical parent outside this subtree first")
				}
			}
		}
		visiting[id] = false
		visited[id] = true
		orderedCommands = append(orderedCommands, commands[index])
		orderedPreviews = append(orderedPreviews, previews[index])
		return nil
	}
	for _, id := range ids {
		if err := visit(id); err != nil {
			return nil, err
		}
	}
	commands = orderedCommands
	previews = orderedPreviews
	if len(guards) > 1000 {
		return nil, Fail("RESOURCE_EXHAUSTED", "This subtree exceeds 1000 revision guards; restore ancestor pages individually, then smaller descendant subtrees")
	}
	keys := []string{}
	for key := range guards {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	all := []contracts.ServiceExpectedRevision{}
	for _, key := range keys {
		all = append(all, guards[key])
	}
	return map[string]any{"rootPageId": root, "pages": previews, "commands": commands, "expectedRevisions": all, "warnings": []string{"Restores the last retained live revision of each page. Existing task/database values on other pages and shared schemas are preserved.", "Memory approval and forgotten states are preserved; restored sources receive new revisions and require renewed evidence review."}}, nil
}

type RecoverySettings struct {
	HistoryPolicy     string `json:"historyPolicy"`
	TrashPolicy       string `json:"trashPolicy"`
	RecordingPolicy   string `json:"recordingPolicy"`
	BackupKeepCount   int    `json:"backupKeepCount"`
	BackupPolicy      string `json:"backupPolicy"`
	RetainedRevisions int    `json:"retainedRevisions"`
	Backups           int    `json:"backups"`
}

func (k *Kernel) RecoverySettings(ctx context.Context, e contracts.ServiceExecutionContext) (RecoverySettings, error) {
	out := RecoverySettings{HistoryPolicy: "All retained page revisions and recovery points are kept indefinitely.", TrashPolicy: "Trashed pages and managed assets referenced by history are retained.", RecordingPolicy: "Interrupted audio and capture journals are retained until recovered.", BackupPolicy: "External backups are kept until explicitly reviewed and deleted. Keep count is a cleanup target, never automatic deletion."}
	if err := requireOwnerScope(k, e); err != nil {
		return out, err
	}
	var raw string
	if err := k.db.QueryRowContext(ctx, "SELECT value FROM kernel_meta WHERE key='backupKeepCount'").Scan(&raw); err == nil {
		json.Unmarshal([]byte(raw), &out.BackupKeepCount)
	} else if err != sql.ErrNoRows {
		return out, err
	}
	if err := k.db.QueryRowContext(ctx, "SELECT count(*) FROM page_snapshots").Scan(&out.RetainedRevisions); err != nil {
		return out, err
	}
	err := k.db.QueryRowContext(ctx, "SELECT count(*) FROM backups WHERE state='complete'").Scan(&out.Backups)
	return out, err
}
func recoverySettings(ctx context.Context, k *Kernel, e contracts.ServiceExecutionContext, r map[string]any) (any, error) {
	return k.RecoverySettings(ctx, e)
}
func setRecoveryRetention(ctx context.Context, k *Kernel, e contracts.ServiceExecutionContext, r map[string]any) (any, error) {
	if err := requireOwnerScope(k, e); err != nil {
		return nil, err
	}
	count, ok := r["backupKeepCount"].(float64)
	if !ok || count < 0 || count > 1000 || count != float64(int(count)) {
		return nil, Fail("INVALID_REQUEST", "Keep count must be a whole number from 0 (all) through 1000")
	}
	err := k.write(ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, "INSERT INTO kernel_meta(key,value) VALUES('backupKeepCount',?) ON CONFLICT(key) DO UPDATE SET value=excluded.value", string(marshal(int(count))))
		return err
	})
	if err != nil {
		return nil, err
	}
	return k.RecoverySettings(ctx, e)
}
