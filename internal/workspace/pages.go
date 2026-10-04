package workspace

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"trove.local/phase0/contracts"
)

func init() {
	for _, kind := range []string{"page.create", "page.update", "page.move", "page.trash", "block.insert", "block.update", "block.delete", "block.reorder", "block.move"} {
		RegisterCommand(kind, pageCommand)
	}
}
func pageCommand(m *Mutation, c map[string]any) error {
	ctx, tx := m.Context, m.Tx
	kind := str(c["kind"])
	switch kind {
	case "page.create":
		id := str(c["pageId"])
		if err := m.Touch("page", id); err != nil {
			return err
		}
		if m.GuardRevision("page", id) != "0" {
			return Fail("CONFLICT", "Page already exists")
		}
		if parent := str(c["parentPageId"]); parent != "" {
			if err := m.Require("page", parent); err != nil {
				return err
			}
			if err := m.AuthorizePage(parent); err != nil {
				return err
			}
		} else {
			if _, ok := m.Execution.Scope.Value.(contracts.DomainScopeVariant1); !ok {
				return Fail("SCOPE_DENIED", "Root creation requires workspace scope")
			}
		}
		m.before[id] = json.RawMessage(`null`)
		_, err := m.Exec("INSERT INTO pages(id,workspace_id,parent_page_id,title,revision,created_at,updated_at) VALUES(?,?,?,?,1,?,?)", id, m.Kernel.id, nullable(c["parentPageId"]), str(c["title"]), now(), now())
		return err
	case "page.update", "page.move", "page.trash":
		id := str(c["pageId"])
		if err := m.TouchPage(id); err != nil {
			return err
		}
		if kind == "page.update" {
			if len(c) < 3 {
				return Fail("INVALID_REQUEST", "No page changes supplied")
			}
			if title, ok := c["title"]; ok {
				if _, err := m.Exec("UPDATE pages SET title=? WHERE id=?", title, id); err != nil {
					return err
				}
			}
			if _, err := m.Exec("INSERT OR IGNORE INTO page_metadata(page_id) VALUES(?)", id); err != nil {
				return err
			}
			if value, ok := c["archived"]; ok {
				if _, err := m.Exec("UPDATE page_metadata SET archived=?,archived_at=CASE WHEN ? THEN COALESCE(archived_at,?) ELSE NULL END WHERE page_id=?", value, value, now(), id); err != nil {
					return err
				}
			}
			if value, ok := c["icon"]; ok {
				if _, err := m.Exec("UPDATE page_metadata SET icon=? WHERE page_id=?", value, id); err != nil {
					return err
				}
			}
			if value, ok := c["coverFileId"]; ok {
				if value != nil {
					if err := m.Kernel.authorizeEntity(ctx, tx, m.Execution, "file", str(value)); err != nil {
						return err
					}
				}
				if _, err := m.Exec("UPDATE page_metadata SET cover_file_id=? WHERE page_id=?", value, id); err != nil {
					return err
				}
			}
			return nil
		}
		if kind == "page.move" {
			var previous sql.NullString
			if err := tx.QueryRowContext(ctx, "SELECT parent_page_id FROM pages WHERE id=?", id).Scan(&previous); err != nil {
				return err
			}
			if previous.Valid {
				if err := m.Require("page", previous.String); err != nil {
					return err
				}
			}
			if parent := str(c["parentPageId"]); parent != "" {
				if err := m.Require("page", parent); err != nil {
					return err
				}
			}
			_, err := m.Exec("UPDATE pages SET parent_page_id=? WHERE id=?", nullable(c["parentPageId"]), id)
			return err
		}
		ids, err := descendantPages(ctx, tx, id)
		if err != nil {
			return err
		}
		if len(ids) > 1 && c["includeDescendants"] != true {
			return Fail("INVALID_REQUEST", "Page has descendants")
		}
		for _, page := range ids {
			if err = m.TouchPage(page); err != nil {
				return err
			}
			if err = m.Deleted("page", page); err != nil {
				return err
			}
			if _, err = m.Exec("UPDATE pages SET deleted_at=? WHERE id=?", now(), page); err != nil {
				return err
			}
		}
		return nil
	case "block.insert":
		b := c["block"].(map[string]any)
		id, page := str(b["blockId"]), str(b["pageId"])
		if err := m.TouchPage(page); err != nil {
			return err
		}
		if err := m.Touch("block", id); err != nil {
			return err
		}
		if m.GuardRevision("block", id) != "0" {
			return Fail("CONFLICT", "Block already exists")
		}
		if err := validateBlock(str(b["type"]), number(b["schemaVersion"]), b["data"]); err != nil {
			return err
		}
		parent := str(b["parentBlockId"])
		if parent != "" {
			if err := m.Require("block", parent); err != nil {
				return err
			}
		}
		key, err := appendKey(ctx, tx, page, parent)
		if err != nil {
			return err
		}
		if _, err = m.Exec("INSERT INTO blocks(id,page_id,parent_block_id,sort_key,type,schema_version,data_json,revision) VALUES(?,?,?,?,?,?,?,1)", id, page, nullable(b["parentBlockId"]), key, b["type"], number(b["schemaVersion"]), string(marshal(b["data"]))); err != nil {
			return err
		}
		if err = updateLinks(m, id, page, b["data"]); err != nil {
			return err
		}
		if after := str(b["afterBlockId"]); after != "" {
			return reposition(m, id, page, parent, after)
		}
		return nil
	case "block.update", "block.delete", "block.move":
		id := str(c["blockId"])
		var page, parent string
		if err := tx.QueryRowContext(ctx, "SELECT page_id,COALESCE(parent_block_id,'') FROM blocks WHERE id=?", id).Scan(&page, &parent); err != nil {
			return err
		}
		if err := m.TouchPage(page); err != nil {
			return err
		}
		if err := m.Touch("block", id); err != nil {
			return err
		}
		if kind == "block.update" {
			if err := validateBlock(str(c["type"]), number(c["schemaVersion"]), c["data"]); err != nil {
				return err
			}
			if _, err := m.Exec("UPDATE blocks SET type=?,schema_version=?,data_json=? WHERE id=?", c["type"], number(c["schemaVersion"]), string(marshal(c["data"])), id); err != nil {
				return err
			}
			return updateLinks(m, id, page, c["data"])
		}
		ids, err := descendantBlocks(ctx, tx, id)
		if err != nil {
			return err
		}
		if kind == "block.move" {
			dest := str(c["pageId"])
			destParent := str(c["parentBlockId"])
			if dest != page {
				for _, bid := range ids {
					var segments int
					if err = tx.QueryRowContext(ctx, "SELECT count(*) FROM transcript_segments WHERE block_id=?", bid).Scan(&segments); err != nil {
						return err
					}
					if segments != 0 {
						return Fail("CONFLICT", "Transcript blocks must remain on their meeting page; copy their text to another page")
					}
				}
				if err = m.TouchPage(dest); err != nil {
					return err
				}
			}
			if destParent != "" {
				if err = m.Require("block", destParent); err != nil {
					return err
				}
				for _, bid := range ids {
					if bid == destParent {
						return Fail("INVALID_REQUEST", "A block cannot contain itself")
					}
				}
			}
			key, err := appendKey(ctx, tx, dest, destParent)
			if err != nil {
				return err
			}
			for _, bid := range ids {
				if err = m.Touch("block", bid); err != nil {
					return err
				}
				if _, err = m.Exec("UPDATE blocks SET page_id=? WHERE id=?", dest, bid); err != nil {
					return err
				}
				if _, err = m.Exec("UPDATE page_links SET source_page_id=? WHERE source_block_id=?", dest, bid); err != nil {
					return err
				}
			}
			if _, err = m.Exec("UPDATE blocks SET parent_block_id=?,sort_key=? WHERE id=?", nullable(c["parentBlockId"]), key, id); err != nil {
				return err
			}
			return reposition(m, id, dest, destParent, str(c["afterBlockId"]))
		}
		if len(ids) > 1 && c["includeDescendants"] != true {
			return Fail("INVALID_REQUEST", "Block has descendants")
		}
		for i := len(ids) - 1; i >= 0; i-- {
			bid := ids[i]
			if err = m.Deleted("block", bid); err != nil {
				return err
			}
			for _, q := range []string{"DELETE FROM page_links WHERE source_block_id=?", "DELETE FROM todo_references WHERE block_id=?", "UPDATE attachments SET block_id=NULL WHERE block_id=?", "DELETE FROM transcript_segments WHERE block_id=?"} {
				if _, err = m.Exec(q, bid); err != nil {
					return err
				}
			}
			if _, err = m.Exec("DELETE FROM blocks WHERE id=?", bid); err != nil {
				return err
			}
		}
		return nil
	case "block.reorder":
		page, parent := str(c["pageId"]), str(c["parentBlockId"])
		if err := m.TouchPage(page); err != nil {
			return err
		}
		existing, err := siblingIDs(ctx, tx, page, parent)
		if err != nil {
			return err
		}
		raw := c["orderedBlockIds"].([]any)
		if len(raw) != len(existing) {
			return Fail("INVALID_REQUEST", "Reorder must contain every sibling")
		}
		ids := make([]string, len(raw))
		set := map[string]bool{}
		for _, id := range existing {
			set[id] = true
		}
		for i, v := range raw {
			id := str(v)
			if !set[id] {
				return Fail("INVALID_REQUEST", "Reorder contains a duplicate or foreign block")
			}
			delete(set, id)
			ids[i] = id
		}
		return reorder(m, ids)
	}
	return Fail("UNSUPPORTED_SCHEMA", kind)
}
func validateBlock(kind string, version int64, data any) error {
	if version != 1 {
		return Fail("UNSUPPORTED_SCHEMA", "Unsupported block schema version")
	}
	name := "block_" + kind + "_v1"
	if err := contracts.Validate("domain", name, marshal(data)); err != nil {
		return Fail("INVALID_REQUEST", "Block data failed registry validation: "+err.Error())
	}
	return nil
}
func descendantPages(ctx context.Context, q querier, id string) ([]string, error) {
	rows, err := q.QueryContext(ctx, "WITH RECURSIVE tree(id,depth) AS(SELECT id,0 FROM pages WHERE id=? UNION ALL SELECT p.id,t.depth+1 FROM pages p JOIN tree t ON p.parent_page_id=t.id) SELECT id FROM tree ORDER BY depth", id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	ids := []string{}
	for rows.Next() {
		var id string
		rows.Scan(&id)
		ids = append(ids, id)
	}
	return ids, rows.Err()
}
func descendantBlocks(ctx context.Context, q querier, id string) ([]string, error) {
	rows, err := q.QueryContext(ctx, "WITH RECURSIVE tree(id,depth) AS(SELECT id,0 FROM blocks WHERE id=? UNION ALL SELECT b.id,t.depth+1 FROM blocks b JOIN tree t ON b.parent_block_id=t.id) SELECT id FROM tree ORDER BY depth", id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	ids := []string{}
	for rows.Next() {
		var id string
		rows.Scan(&id)
		ids = append(ids, id)
	}
	return ids, rows.Err()
}
func siblingIDs(ctx context.Context, q querier, page, parent string) ([]string, error) {
	rows, err := q.QueryContext(ctx, "SELECT id FROM blocks WHERE page_id=? AND COALESCE(parent_block_id,'')=? ORDER BY sort_key", page, parent)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	ids := []string{}
	for rows.Next() {
		var id string
		rows.Scan(&id)
		ids = append(ids, id)
	}
	return ids, rows.Err()
}
func appendKey(ctx context.Context, q querier, page, parent string) (string, error) {
	var max int64
	err := q.QueryRowContext(ctx, "SELECT COALESCE(MAX(CAST(sort_key AS INTEGER)),0) FROM blocks WHERE page_id=? AND COALESCE(parent_block_id,'')=?", page, parent).Scan(&max)
	if err != nil {
		return "", err
	}
	if max >= 499999999999 {
		return "", Fail("RESOURCE_EXHAUSTED", "Block order limit reached")
	}
	return fmt.Sprintf("%012d", max+1), nil
}
func reposition(m *Mutation, id, page, parent, after string) error {
	ids, err := siblingIDs(m.Context, m.Tx, page, parent)
	if err != nil {
		return err
	}
	ordered := []string{}
	found := after == ""
	if after == "" {
		ordered = append(ordered, id)
	}
	for _, bid := range ids {
		if bid == id {
			continue
		}
		ordered = append(ordered, bid)
		if bid == after {
			ordered = append(ordered, id)
			found = true
		}
	}
	if !found {
		return Fail("INVALID_REQUEST", "afterBlockId is not a sibling")
	}
	return reorder(m, ordered)
}
func reorder(m *Mutation, ids []string) error {
	for i, id := range ids {
		if err := m.Touch("block", id); err != nil {
			return err
		}
		if _, err := m.Exec("UPDATE blocks SET sort_key=? WHERE id=?", fmt.Sprintf("%012d", 500000000000+int64(i)), id); err != nil {
			return err
		}
	}
	for i, id := range ids {
		if _, err := m.Exec("UPDATE blocks SET sort_key=? WHERE id=?", fmt.Sprintf("%012d", i+1), id); err != nil {
			return err
		}
	}
	return nil
}
func updateLinks(m *Mutation, id, page string, data any) error {
	if _, err := m.Exec("DELETE FROM page_links WHERE source_block_id=?", id); err != nil {
		return err
	}
	targets := map[string]bool{}
	invalidCanonicalLink := false
	var walk func(any)
	walk = func(v any) {
		switch x := v.(type) {
		case map[string]any:
			if t := str(x["targetPageId"]); t != "" {
				targets[t] = true
			}
			if t := str(x["link"]); strings.HasPrefix(t, "trove://page/") {
				target, _, ok := parseCanonicalPageLink(t)
				if !ok {
					invalidCanonicalLink = true
				} else {
					targets[target] = true
				}
			}
			for _, v := range x {
				walk(v)
			}
		case []any:
			for _, v := range x {
				walk(v)
			}
		}
	}
	walk(data)
	if invalidCanonicalLink {
		return Fail("INVALID_REQUEST", "Invalid canonical page or block link")
	}
	for target := range targets {
		if err := m.AuthorizePage(target); err != nil {
			return err
		}
		if _, err := m.Exec("INSERT INTO page_links VALUES(?,?,?)", page, id, target); err != nil {
			return err
		}
	}
	return nil
}
func readPageTx(ctx context.Context, q querier, id string) (contracts.ServiceReadPageResult, error) {
	var r contracts.ServiceReadPageResult
	r.Blocks = []contracts.DomainBlock{}
	var parent, deleted, provenance sql.NullString
	err := q.QueryRowContext(ctx, "SELECT id,workspace_id,parent_page_id,title,revision,created_at,updated_at,deleted_at,provenance_json FROM pages WHERE id=?", id).Scan(&r.Page.Id, &r.Page.WorkspaceId, &parent, &r.Page.Title, &r.Page.Revision, &r.Page.CreatedAt, &r.Page.UpdatedAt, &deleted, &provenance)
	if err != nil {
		return r, err
	}
	if parent.Valid {
		v := contracts.DomainId(parent.String)
		r.Page.ParentPageId = &v
	}
	if deleted.Valid {
		v := contracts.DomainTimestamp(deleted.String)
		r.Page.DeletedAt = &v
	}
	if provenance.Valid {
		json.Unmarshal([]byte(provenance.String), &r.Page.Provenance)
	}
	var archived bool
	var icon, cover, archivedAt sql.NullString
	if err = q.QueryRowContext(ctx, "SELECT archived,icon,cover_file_id,archived_at FROM page_metadata WHERE page_id=?", id).Scan(&archived, &icon, &cover, &archivedAt); err != nil && !errors.Is(err, sql.ErrNoRows) {
		return r, err
	}
	if archived {
		v := contracts.DomainTimestamp(archivedAt.String)
		r.Page.ArchivedAt = &v
	}
	if icon.Valid {
		r.Page.Icon = &icon.String
	}
	if cover.Valid {
		v := contracts.DomainId(cover.String)
		r.Page.CoverFileId = &v
	}
	rows, err := q.QueryContext(ctx, "SELECT id,page_id,parent_block_id,sort_key,type,schema_version,data_json,revision FROM blocks WHERE page_id=? ORDER BY sort_key,id", id)
	if err != nil {
		return r, err
	}
	defer rows.Close()
	for rows.Next() {
		var b contracts.DomainBlock
		var parent sql.NullString
		var data string
		if err = rows.Scan(&b.Id, &b.PageId, &parent, &b.SortKey, &b.Type, &b.SchemaVersion, &data, &b.Revision); err != nil {
			return r, err
		}
		b.Data = json.RawMessage(data)
		if parent.Valid {
			v := contracts.DomainId(parent.String)
			b.ParentBlockId = &v
		}
		r.Blocks = append(r.Blocks, b)
	}
	if err = rows.Err(); err != nil {
		return r, err
	}
	rows.Close()
	err = q.QueryRowContext(ctx, "SELECT CAST(COALESCE(MAX(cursor),0) AS TEXT) FROM committed_events").Scan(&r.SnapshotCursor)
	return r, err
}
