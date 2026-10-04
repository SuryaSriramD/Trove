package workspace

import (
	"context"
	"database/sql"
	"encoding/json"

	"trove.local/phase0/contracts"
)

func init() { RegisterQuery("readBlock", readShellBlock); RegisterQuery("readRecord", readShellRecord) }

func readShellRecord(ctx context.Context, k *Kernel, e contracts.ServiceExecutionContext, request map[string]any) (any, error) {
	if err := contracts.Validate("service", "ReadRecordRequest", marshal(request)); err != nil {
		return nil, Fail("INVALID_REQUEST", err.Error())
	}
	tx, err := k.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	id := str(request["recordId"])
	if err = k.authorizeEntity(ctx, tx, e, "record", id); err != nil {
		return nil, err
	}
	var deleted sql.NullString
	if err = tx.QueryRowContext(ctx, "SELECT p.deleted_at FROM records r JOIN pages p ON p.id=r.page_id WHERE r.id=?", id).Scan(&deleted); err != nil {
		return nil, err
	}
	if deleted.Valid {
		return nil, Fail("NOT_FOUND", "Record page is deleted")
	}
	records, _, err := hydrateRecordPage(ctx, tx, []string{id})
	if err != nil {
		return nil, err
	}
	if err = k.authorize(e); err != nil {
		return nil, err
	}
	return map[string]any{"record": records[0]}, nil
}

func readShellBlock(ctx context.Context, k *Kernel, e contracts.ServiceExecutionContext, request map[string]any) (any, error) {
	if err := contracts.Validate("service", "ReadBlockRequest", marshal(request)); err != nil {
		return nil, Fail("INVALID_REQUEST", err.Error())
	}
	tx, err := k.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	id := str(request["blockId"])
	if err = k.authorizeEntity(ctx, tx, e, "block", id); err != nil {
		return nil, err
	}
	var b contracts.DomainBlock
	var parent sql.NullString
	var raw string
	err = tx.QueryRowContext(ctx, "SELECT b.id,b.page_id,b.parent_block_id,b.sort_key,b.type,b.schema_version,b.data_json,b.revision FROM blocks b JOIN pages p ON p.id=b.page_id WHERE b.id=? AND p.deleted_at IS NULL", id).Scan(&b.Id, &b.PageId, &parent, &b.SortKey, &b.Type, &b.SchemaVersion, &raw, &b.Revision)
	if err != nil {
		return nil, err
	}
	if parent.Valid {
		v := contracts.DomainId(parent.String)
		b.ParentBlockId = &v
	}
	b.Data = json.RawMessage(raw)
	if err = k.authorize(e); err != nil {
		return nil, err
	}
	return map[string]any{"block": b}, nil
}

// The shell keeps lightweight navigation/guard identities, not every page body
// and cell payload. Metadata remains O(pages+records); it is not an RSS bound.
// Task status/due values remain available so task operations retain their guards.
func (k *Kernel) shellSnapshot(ctx context.Context, e contracts.ServiceExecutionContext) (any, error) {
	tx, err := k.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if err = k.authorize(e); err != nil {
		return nil, err
	}
	out := map[string]any{"contractVersion": "1.0.0", "workspaceId": k.id, "scopeGrantId": e.ScopeGrantId, "snapshotCursor": "0", "contentMode": "shell", "blocks": []any{}}
	plan := &recordSQL{}
	scope := plan.scope(e)
	rows, err := tx.QueryContext(ctx, "SELECT p.id,p.workspace_id,p.parent_page_id,p.title,p.revision,p.created_at,p.updated_at,p.deleted_at,p.provenance_json,m.archived_at,m.icon,m.cover_file_id FROM pages p LEFT JOIN page_metadata m ON m.page_id=p.id WHERE "+scope+" ORDER BY p.created_at,p.id", plan.args...)
	if err != nil {
		return nil, err
	}
	pages := []contracts.DomainPage{}
	for rows.Next() {
		var p contracts.DomainPage
		var parent, deleted, provenance, archived, icon, cover sql.NullString
		if err = rows.Scan(&p.Id, &p.WorkspaceId, &parent, &p.Title, &p.Revision, &p.CreatedAt, &p.UpdatedAt, &deleted, &provenance, &archived, &icon, &cover); err != nil {
			rows.Close()
			return nil, err
		}
		if parent.Valid {
			v := contracts.DomainId(parent.String)
			p.ParentPageId = &v
		}
		if deleted.Valid {
			v := contracts.DomainTimestamp(deleted.String)
			p.DeletedAt = &v
		}
		if archived.Valid {
			v := contracts.DomainTimestamp(archived.String)
			p.ArchivedAt = &v
		}
		if icon.Valid {
			p.Icon = &icon.String
		}
		if cover.Valid {
			v := contracts.DomainId(cover.String)
			p.CoverFileId = &v
		}
		if provenance.Valid {
			_ = json.Unmarshal([]byte(provenance.String), &p.Provenance)
		}
		pages = append(pages, p)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	out["pages"] = pages
	rows, err = tx.QueryContext(ctx, "SELECT r.id,r.collection_id,r.page_id,r.revision,r.sort_key,c.title_property_id FROM records r JOIN pages p ON p.id=r.page_id JOIN collections c ON c.id=r.collection_id WHERE "+scope+" ORDER BY r.sort_key,r.id", plan.args...)
	if err != nil {
		return nil, err
	}
	records := []contracts.DomainRecord{}
	recordIndex := map[string]int{}
	for rows.Next() {
		var r contracts.DomainRecord
		var title contracts.DomainId
		if err = rows.Scan(&r.Id, &r.CollectionId, &r.PageId, &r.Revision, &r.SortKey, &title); err != nil {
			rows.Close()
			return nil, err
		}
		r.Values = []contracts.DomainPropertyValue{{PropertyId: title, SchemaVersion: 1, Value: marshal(map[string]any{"pageId": r.PageId})}}
		recordIndex[string(r.Id)] = len(records)
		records = append(records, r)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	rows, err = tx.QueryContext(ctx, "SELECT v.record_id,v.property_id,v.schema_version,v.value_json FROM property_values v JOIN task_facets t ON t.record_id=v.record_id JOIN records r ON r.id=v.record_id JOIN pages p ON p.id=r.page_id WHERE ("+scope+") AND (v.property_id=t.status_property_id OR v.property_id=t.due_property_id)", plan.args...)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var id, raw string
		var value contracts.DomainPropertyValue
		if err = rows.Scan(&id, &value.PropertyId, &value.SchemaVersion, &raw); err != nil {
			rows.Close()
			return nil, err
		}
		value.Value = json.RawMessage(raw)
		if index, ok := recordIndex[id]; ok {
			records[index].Values = append(records[index].Values, value)
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	out["records"] = records
	for _, name := range []string{"collections", "properties", "views", "containers", "containerCollections", "taskFacets", "files", "meetings", "memories", "projectScopes", "transcriptSegments", "importMappings"} {
		out[name] = []any{}
		if reader := snapshotReaders[name]; reader != nil {
			value, err := reader(ctx, k, tx, e)
			if err != nil {
				return nil, err
			}
			out[name] = value
		}
	}
	var cursor string
	if err = tx.QueryRowContext(ctx, "SELECT CAST(COALESCE(MAX(cursor),0) AS TEXT) FROM committed_events").Scan(&cursor); err != nil {
		return nil, err
	}
	out["snapshotCursor"] = cursor
	if err = k.authorize(e); err != nil {
		return nil, err
	}
	return out, nil
}
