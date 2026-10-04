package workspace

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"
	"trove.local/phase0/contracts"
)

func init() {
	RegisterInitializer(func(ctx context.Context, k *Kernel) error {
		return k.write(ctx, func(tx *sql.Tx) error {
			_, err := tx.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS advanced_page_locks(page_id TEXT PRIMARY KEY REFERENCES pages(id),locked INTEGER NOT NULL,revision INTEGER NOT NULL);
 CREATE TABLE IF NOT EXISTS advanced_templates(id TEXT PRIMARY KEY,source_page_id TEXT NOT NULL REFERENCES pages(id),name TEXT NOT NULL,snapshot_json TEXT NOT NULL,created_at TEXT NOT NULL);
 CREATE TABLE IF NOT EXISTS advanced_button_actions(id TEXT PRIMARY KEY,page_id TEXT NOT NULL REFERENCES pages(id),label TEXT NOT NULL,commands_json TEXT NOT NULL);`)
			return err
		})
	})
	RegisterCommitValidator(validateAdvancedEditor)
	RegisterQuery("editor.lock", editorLock)
	RegisterQuery("editor.setLock", editorSetLock)
	RegisterQuery("editor.references", editorReferences)
	RegisterQuery("editor.templates", editorTemplates)
	RegisterQuery("editor.saveTemplate", editorSaveTemplate)
	RegisterQuery("editor.template", editorTemplate)
	RegisterQuery("editor.saveButton", editorSaveButton)
	RegisterQuery("editor.button", editorButton)
}
func editorLock(ctx context.Context, k *Kernel, e contracts.ServiceExecutionContext, r map[string]any) (any, error) {
	page := str(r["pageId"])
	if err := k.authorizePage(ctx, k.db, e, page); err != nil {
		return nil, err
	}
	var locked bool
	var revision int64
	err := k.db.QueryRowContext(ctx, "SELECT locked,revision FROM advanced_page_locks WHERE page_id=?", page).Scan(&locked, &revision)
	if err != nil && err != sql.ErrNoRows {
		return nil, err
	}
	return map[string]any{"locked": locked, "revision": fmt.Sprint(revision)}, nil
}
func editorSetLock(ctx context.Context, k *Kernel, e contracts.ServiceExecutionContext, r map[string]any) (any, error) {
	if e.Actor.Kind != "user" || e.Origin != "user" {
		return nil, Fail("SCOPE_DENIED", "Only the direct user can change page locks")
	}
	page := str(r["pageId"])
	locked, ok := r["locked"].(bool)
	if !ok {
		return nil, Fail("INVALID_REQUEST", "locked must be boolean")
	}
	err := k.write(ctx, func(tx *sql.Tx) error {
		if err := k.authorizePage(ctx, tx, e, page); err != nil {
			return err
		}
		var pageRev string
		if err := tx.QueryRowContext(ctx, "SELECT revision FROM pages WHERE id=? AND deleted_at IS NULL", page).Scan(&pageRev); err != nil {
			return err
		}
		if pageRev != str(r["expectedPageRevision"]) {
			return Fail("CONFLICT", "Page changed before lock operation")
		}
		var rev int64
		err := tx.QueryRowContext(ctx, "SELECT revision FROM advanced_page_locks WHERE page_id=?", page).Scan(&rev)
		if err != nil && err != sql.ErrNoRows {
			return err
		}
		if fmt.Sprint(rev) != str(r["expectedRevision"]) {
			return Fail("CONFLICT", "Page lock changed elsewhere")
		}
		_, err = tx.ExecContext(ctx, "INSERT INTO advanced_page_locks VALUES(?,?,?) ON CONFLICT(page_id) DO UPDATE SET locked=excluded.locked,revision=excluded.revision", page, locked, rev+1)
		return err
	})
	if err != nil {
		return nil, err
	}
	return editorLock(ctx, k, e, r)
}

// Validate after every canonical batch, including imports and recovery. Edges from
// ownership, layout membership and reusable content share a single cycle check.
func validateAdvancedEditor(m *Mutation) error {
	// A reviewed history restore may reinstate an existing historical synced
	// reference whose external source was subsequently deleted. This evidence
	// does not authorize an ordinary mutation to introduce a missing source.
	restoredSynced := map[string]string{}
	onlyRestores := len(m.Request.Commands) > 0
	for _, command := range m.Request.Commands {
		if _, ok := command.Value.(contracts.ServiceRestoreRevision); !ok {
			onlyRestores = false
			break
		}
	}
	if onlyRestores {
		for _, command := range m.Request.Commands {
			restore := command.Value.(contracts.ServiceRestoreRevision)
			snapshot, _, err := loadRecoverySnapshot(m.Context, m.Tx, string(restore.PageId), string(restore.RestoreRevision))
			if err != nil {
				return err
			}
			for _, block := range snapshot.Blocks {
				if block.Type == "synced_block" {
					var data map[string]any
					if err = json.Unmarshal(block.Data, &data); err != nil {
						return err
					}
					restoredSynced[string(block.Id)] = str(data["contentSourceId"])
				}
			}
		}
	}
	for _, change := range m.touched {
		if change.Target.EntityType != "page" {
			continue
		}
		var locked bool
		err := m.Tx.QueryRowContext(m.Context, "SELECT locked FROM advanced_page_locks WHERE page_id=?", change.Target.EntityId).Scan(&locked)
		if err != nil && err != sql.ErrNoRows {
			return err
		}
		if locked {
			return Fail("CONFLICT", "Page is locked; unlock it before making changes")
		}
	}
	if editorBatchCannotAddGraphEdges(m.Request.Commands) {
		return nil
	}
	rows, err := m.Tx.QueryContext(m.Context, "SELECT id,page_id,COALESCE(parent_block_id,''),type,data_json FROM blocks")
	if err != nil {
		return err
	}
	type entry struct {
		page, parent, kind string
		data               map[string]any
	}
	all := map[string]entry{}
	for rows.Next() {
		var id, raw string
		var b entry
		if err = rows.Scan(&id, &b.page, &b.parent, &b.kind, &raw); err != nil {
			rows.Close()
			return err
		}
		if err = json.Unmarshal([]byte(raw), &b.data); err != nil {
			rows.Close()
			return err
		}
		all[id] = b
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	edges := map[string][]string{}
	for id, b := range all {
		if b.parent != "" {
			edges[b.parent] = append(edges[b.parent], id)
		}
		_, changed := m.touched[guardKey("block", id)]
		targets := []string{}
		if b.kind == "synced_block" {
			targets = append(targets, str(b.data["contentSourceId"]))
		}
		for _, key := range []string{"tabs", "columns"} {
			groups, _ := b.data[key].([]any)
			seen := map[string]bool{}
			for _, raw := range groups {
				g, _ := raw.(map[string]any)
				children, _ := g["childBlockIds"].([]any)
				for _, v := range children {
					child := str(v)
					if changed && seen[child] {
						return Fail("INVALID_REQUEST", "A layout child belongs to only one group")
					}
					seen[child] = true
					targets = append(targets, child)
					if target, ok := all[child]; ok && changed && (target.page != b.page || target.parent != id) {
						return Fail("INVALID_REQUEST", "Layout children must be owned by their container on the same page")
					}
				}
			}
		}
		for _, target := range targets {
			other, ok := all[target]
			if !ok {
				if changed && m.Execution.Origin == "user" {
					if b.kind == "synced_block" && restoredSynced[id] == target {
						continue
					}
					return Fail("INVALID_REQUEST", "Referenced block is unavailable")
				}
				continue
			}
			if changed {
				if err = m.AuthorizePage(other.page); err != nil {
					return err
				}
			}
			edges[id] = append(edges[id], target)
		}
		if changed && b.kind == "date_mention" {
			if _, err = time.LoadLocation(str(b.data["timezone"])); err != nil {
				return Fail("INVALID_REQUEST", "Date mention timezone is unknown")
			}
		}
		if changed && b.kind == "table" {
			if err = validateAdvancedTable(b.data); err != nil {
				return err
			}
		}
	}
	colors := map[string]int{}
	var visit func(string) error
	visit = func(id string) error {
		if colors[id] == 1 {
			return Fail("INVALID_REQUEST", "Reusable blocks and layouts cannot form a cycle")
		}
		if colors[id] == 2 {
			return nil
		}
		colors[id] = 1
		for _, to := range edges[id] {
			if err := visit(to); err != nil {
				return err
			}
		}
		colors[id] = 2
		return nil
	}
	for id := range all {
		if err = visit(id); err != nil {
			return err
		}
	}
	return nil
}

// The ordinary P0 editing path must not decode every unrelated workspace block.
// These commands are schema-validated before commit validation: paragraphs have
// only richText, so a root insertion has no outgoing graph edges and a paragraph
// update can only retain ownership or remove a previous reference/layout edge.
// A mixed batch containing any structural or advanced command takes the complete
// graph-validation path. Page locks above apply to both paths.
func editorBatchCannotAddGraphEdges(commands []contracts.ServiceCommand) bool {
	if len(commands) == 0 {
		return false
	}
	for _, command := range commands {
		switch c := command.Value.(type) {
		case contracts.ServiceCreatePage, contracts.ServiceUpdatePage:
		case contracts.ServiceInsertBlock:
			if c.Block.Type != "paragraph" || c.Block.SchemaVersion != 1 || c.Block.ParentBlockId != nil {
				return false
			}
		case contracts.ServiceUpdateBlock:
			if c.Type != "paragraph" || c.SchemaVersion != 1 {
				return false
			}
		default:
			return false
		}
	}
	return true
}

func validateAdvancedTable(d map[string]any) error {
	rows, cols := number(d["rowCount"]), number(d["columnCount"])
	if rows*cols > 100000 {
		return Fail("RESOURCE_EXHAUSTED", "Table exceeds 100000 cells")
	}
	used := map[[2]int64]bool{}
	ids := map[string]bool{}
	cells, _ := d["cells"].([]any)
	for _, raw := range cells {
		c, _ := raw.(map[string]any)
		id := str(c["id"])
		if ids[id] {
			return Fail("INVALID_REQUEST", "Table cell IDs must be unique")
		}
		ids[id] = true
		r, col, rs, cs := number(c["row"]), number(c["column"]), number(c["rowSpan"]), number(c["colSpan"])
		if r+rs > rows || col+cs > cols {
			return Fail("INVALID_REQUEST", "Merged cell exceeds table bounds")
		}
		for y := r; y < r+rs; y++ {
			for x := col; x < col+cs; x++ {
				key := [2]int64{y, x}
				if used[key] {
					return Fail("INVALID_REQUEST", "Table cells overlap")
				}
				used[key] = true
			}
		}
	}
	return nil
}
func editorReferences(ctx context.Context, k *Kernel, e contracts.ServiceExecutionContext, r map[string]any) (any, error) {
	page := str(r["pageId"])
	if err := k.authorizePage(ctx, k.db, e, page); err != nil {
		return nil, err
	}
	var title string
	if err := k.db.QueryRowContext(ctx, "SELECT title FROM pages WHERE id=?", page).Scan(&title); err != nil {
		return nil, err
	}
	// Derived text and explicit links are read at request time, so no stale cache can
	// leak a page after a scope change. Similarity is an explained lexical heuristic.
	rows, err := k.db.QueryContext(ctx, "SELECT id,title FROM pages WHERE id<>? AND deleted_at IS NULL ORDER BY title LIMIT 2000", page)
	if err != nil {
		return nil, err
	}
	type candidate struct{ id, title string }
	candidates := []candidate{}
	for rows.Next() {
		var p candidate
		if err = rows.Scan(&p.id, &p.title); err != nil {
			rows.Close()
			return nil, err
		}
		candidates = append(candidates, p)
	}
	rows.Close()
	out := []any{}
	for _, p := range candidates {
		if k.authorizePage(ctx, k.db, e, p.id) != nil {
			continue
		}
		result, err := readPageTx(ctx, k.db, p.id)
		if err != nil {
			return nil, err
		}
		var text strings.Builder
		linked := false
		for _, b := range result.Blocks {
			var d any
			json.Unmarshal(b.Data, &d)
			text.WriteString(advancedVisibleText(d))
			if strings.Contains(string(b.Data), page) {
				linked = true
			}
		}
		if title != "" && !linked && strings.Contains(strings.ToLower(text.String()), strings.ToLower(title)) {
			out = append(out, map[string]any{"pageId": p.id, "title": p.title, "kind": "unlinked", "reason": "Page title occurs in visible text"})
			continue
		}
		for _, word := range strings.Fields(strings.ToLower(title)) {
			if len(word) >= 4 && strings.Contains(strings.ToLower(p.title+" "+text.String()), word) {
				out = append(out, map[string]any{"pageId": p.id, "title": p.title, "kind": "related", "reason": "Shared term: " + word})
				break
			}
		}
		if len(out) >= 100 {
			break
		}
	}
	return map[string]any{"references": out}, nil
}
func advancedVisibleText(v any) string {
	switch x := v.(type) {
	case []any:
		parts := []string{}
		for _, v := range x {
			parts = append(parts, advancedVisibleText(v))
		}
		return strings.Join(parts, " ")
	case map[string]any:
		parts := []string{}
		for _, key := range []string{"richText", "cells", "tabs", "title", "text", "source", "latex", "offlineText", "readableText", "description", "label", "displaySnapshot"} {
			if v, ok := x[key]; ok {
				parts = append(parts, advancedVisibleText(v))
			}
		}
		return strings.Join(parts, " ")
	case string:
		return x
	}
	return ""
}
func editorTemplates(ctx context.Context, k *Kernel, e contracts.ServiceExecutionContext, _ map[string]any) (any, error) {
	rows, err := k.db.QueryContext(ctx, "SELECT id,source_page_id,name,created_at FROM advanced_templates ORDER BY name")
	if err != nil {
		return nil, err
	}
	type row struct{ id, page, name, created string }
	entries := []row{}
	for rows.Next() {
		var x row
		if err = rows.Scan(&x.id, &x.page, &x.name, &x.created); err != nil {
			rows.Close()
			return nil, err
		}
		entries = append(entries, x)
	}
	rows.Close()
	out := []any{}
	for _, x := range entries {
		if k.authorizePage(ctx, k.db, e, x.page) == nil {
			out = append(out, map[string]any{"id": x.id, "sourcePageId": x.page, "name": x.name, "createdAt": x.created})
		}
	}
	return map[string]any{"templates": out}, nil
}
func editorSaveTemplate(ctx context.Context, k *Kernel, e contracts.ServiceExecutionContext, r map[string]any) (any, error) {
	if err := contracts.Validate("service", "TemplateCaptureRequest", marshal(r)); err != nil {
		return nil, Fail("INVALID_REQUEST", err.Error())
	}
	if e.Actor.Kind != "user" || e.Origin != "user" {
		return nil, Fail("SCOPE_DENIED", "Only the user can save a template")
	}
	page, name := str(r["pageId"]), strings.TrimSpace(str(r["name"]))
	if name == "" || len(name) > 200 {
		return nil, Fail("INVALID_REQUEST", "Template requires a name of at most 200 bytes")
	}
	id := NewID()
	err := k.write(ctx, func(tx *sql.Tx) error {
		if err := k.authorizePage(ctx, tx, e, page); err != nil {
			return err
		}
		p, err := readPageTx(ctx, tx, page)
		if err != nil {
			return err
		}
		if string(p.Page.Revision) != str(r["expectedPageRevision"]) {
			return Fail("CONFLICT", "Page changed before template capture")
		}
		snapshot, err := captureUnifiedTemplate(ctx, k, tx, e, p)
		if err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, "INSERT INTO advanced_templates VALUES(?,?,?,?,?)", id, page, name, string(marshal(snapshot)), now())
		return err
	})
	return map[string]any{"id": id}, err
}
func editorTemplate(ctx context.Context, k *Kernel, e contracts.ServiceExecutionContext, r map[string]any) (any, error) {
	var page, raw string
	if err := k.db.QueryRowContext(ctx, "SELECT source_page_id,snapshot_json FROM advanced_templates WHERE id=?", r["templateId"]).Scan(&page, &raw); err != nil {
		return nil, err
	}
	if err := k.authorizePage(ctx, k.db, e, page); err != nil {
		return nil, err
	}
	// Compatibility reads expose the original page-body shape. Captured owned
	// tasks/defaults are resolved only through the scoped unified preview.
	var out contracts.ServiceReadPageResult
	err := json.Unmarshal([]byte(raw), &out)
	return out, err
}
func editorSaveButton(ctx context.Context, k *Kernel, e contracts.ServiceExecutionContext, r map[string]any) (any, error) {
	if e.Actor.Kind != "user" || e.Origin != "user" {
		return nil, Fail("SCOPE_DENIED", "Only the user can define a page button")
	}
	page := str(r["pageId"])
	if err := k.authorizePage(ctx, k.db, e, page); err != nil {
		return nil, err
	}
	commands, ok := r["commands"].([]any)
	if !ok || len(commands) < 1 || len(commands) > 50 {
		return nil, Fail("INVALID_REQUEST", "Button requires 1 to 50 canonical commands")
	}
	for _, c := range commands {
		if err := contracts.Validate("service", "Command", marshal(c)); err != nil {
			return nil, Fail("INVALID_REQUEST", "Invalid button command")
		}
	}
	id := NewID()
	err := k.write(ctx, func(tx *sql.Tx) error {
		if err := k.authorizePage(ctx, tx, e, page); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, "INSERT INTO advanced_button_actions VALUES(?,?,?,?)", id, page, str(r["label"]), string(marshal(commands)))
		return err
	})
	return map[string]any{"actionDefinitionId": id}, err
}

// Returning a proposal never executes it. The caller previews the exact commands
// and submits them to Batch with current guards and the caller's own grant.
func editorButton(ctx context.Context, k *Kernel, e contracts.ServiceExecutionContext, r map[string]any) (any, error) {
	var page, label, raw string
	if err := k.db.QueryRowContext(ctx, "SELECT page_id,label,commands_json FROM advanced_button_actions WHERE id=?", r["actionDefinitionId"]).Scan(&page, &label, &raw); err != nil {
		return nil, err
	}
	if err := k.authorizePage(ctx, k.db, e, page); err != nil {
		return nil, err
	}
	var commands any
	err := json.Unmarshal([]byte(raw), &commands)
	return map[string]any{"label": label, "commands": commands}, err
}
