package workspace

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"time"

	"trove.local/phase0/contracts"
)

type recoverySnapshot struct {
	AttachmentRelationships *[]recoveryAttachmentReference `json:"attachmentRelationships,omitempty"`
	RecordLayouts           *recoveryLayouts               `json:"recordLayouts,omitempty"`
	DisplayPreferences      *PageDisplayPreferences        `json:"displayPreferences,omitempty"`
	contracts.ServiceReadPageResult
	Record          *contracts.DomainRecord              `json:"record,omitempty"`
	Task            *contracts.DomainTaskFacet           `json:"task,omitempty"`
	Attachments     []contracts.DomainId                 `json:"attachmentFileIds,omitempty"`
	RecordStructure *recoveryRecordStructure             `json:"recordStructure,omitempty"`
	MetadataVersion int                                  `json:"recoveryMetadataVersion,omitempty"`
	Properties      []contracts.DomainPropertyDefinition `json:"propertyDefinitions,omitempty"`
	Meeting         *contracts.DomainMeeting             `json:"meeting,omitempty"`
	Segments        []contracts.DomainTranscriptSegment  `json:"transcriptSegments,omitempty"`
}

func captureRecoverySnapshot(ctx context.Context, q querier, page string, result contracts.ServiceReadPageResult) (json.RawMessage, error) {
	out := recoverySnapshot{ServiceReadPageResult: result, MetadataVersion: 6}
	display, displayErr := readPageDisplay(ctx, q, page)
	if displayErr != nil {
		return nil, displayErr
	}
	out.DisplayPreferences = &display
	var layoutErr error
	out.RecordLayouts, layoutErr = captureRecoveryLayouts(ctx, q, page)
	if layoutErr != nil {
		return nil, layoutErr
	}
	var collection string
	err := q.QueryRowContext(ctx, "SELECT collection_id FROM records WHERE page_id=?", page).Scan(&collection)
	if err == nil {
		record, err := captureOwnedRecord(ctx, q, page)
		if err != nil {
			return nil, err
		}
		out.Record = &record
		out.RecordStructure, err = captureRecordStructure(ctx, q, string(record.Id))
		if err != nil {
			return nil, err
		}

		if out.Record != nil {
			for _, value := range out.Record.Values {
				def, err := propertyDefinition(ctx, q, string(value.PropertyId))
				if err != nil {
					return nil, err
				}
				out.Properties = append(out.Properties, def)
			}
			var f contracts.DomainTaskFacet
			var due sql.NullString
			err = q.QueryRowContext(ctx, "SELECT record_id,status_property_id,due_property_id FROM task_facets WHERE record_id=?", out.Record.Id).Scan(&f.RecordId, &f.StatusPropertyId, &due)
			if err == nil {
				if due.Valid {
					id := contracts.DomainId(due.String)
					f.DuePropertyId = &id
				}
				out.Task = &f
			} else if !errors.Is(err, sql.ErrNoRows) {
				return nil, err
			}
		}
	} else if !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	rows, err := q.QueryContext(ctx, "SELECT file_id FROM attachments WHERE page_id=? ORDER BY file_id", page)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id contracts.DomainId
		if err = rows.Scan(&id); err != nil {
			return nil, err
		}
		out.Attachments = append(out.Attachments, id)
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	rows.Close()
	relationships, err := captureRecoveryAttachments(ctx, q, page)
	if err != nil {
		return nil, err
	}
	out.AttachmentRelationships = &relationships
	if err = captureRecoveryMeeting(ctx, q, page, &out); err != nil {
		return nil, err
	}
	return marshal(out), nil
}
func init() {
	RegisterQuery("history.list", listHistory)
	RegisterQuery("history.previewRestore", previewRestore)
	RegisterQuery("history.trash", listTrash)
	RegisterQuery("history.recoveryPoint", createRecoveryPoint)
	RegisterCommand("history.restore", restoreHistory)
}
func listHistory(ctx context.Context, k *Kernel, e contracts.ServiceExecutionContext, r map[string]any) (any, error) {
	page := str(r["pageId"])
	if err := k.authorizePage(ctx, k.db, e, page); err != nil {
		return nil, err
	}
	tx, err := k.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	rows, err := tx.QueryContext(ctx, "SELECT revision,created_at,snapshot_json FROM page_snapshots WHERE page_id=? ORDER BY length(revision) DESC,revision DESC LIMIT 500", page)
	if err != nil {
		return nil, err
	}
	revisions := []any{}
	for rows.Next() {
		var revision, created, raw string
		if err = rows.Scan(&revision, &created, &raw); err != nil {
			rows.Close()
			return nil, err
		}
		var p recoverySnapshot
		if err = json.Unmarshal([]byte(raw), &p); err != nil {
			rows.Close()
			return nil, err
		}
		revisions = append(revisions, map[string]any{"revision": revision, "createdAt": created, "deleted": p.Page.DeletedAt != nil, "title": p.Page.Title})
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	var cursor string
	if err = tx.QueryRowContext(ctx, "SELECT CAST(COALESCE(MAX(cursor),0) AS TEXT) FROM committed_events").Scan(&cursor); err != nil {
		return nil, err
	}
	return map[string]any{"revisions": revisions, "snapshotCursor": cursor}, nil
}
func loadRecoverySnapshot(ctx context.Context, q querier, page, revision string) (recoverySnapshot, string, error) {
	var out recoverySnapshot
	var raw string
	err := q.QueryRowContext(ctx, "SELECT snapshot_json FROM page_snapshots WHERE page_id=? AND revision=?", page, revision).Scan(&raw)
	if err == nil {
		err = json.Unmarshal([]byte(raw), &out)
	}
	return out, raw, err
}
func snapshotDigest(raw string) string {
	v := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(v[:])
}
func previewRestore(ctx context.Context, k *Kernel, e contracts.ServiceExecutionContext, r map[string]any) (any, error) {
	page, revision := str(r["pageId"]), str(r["restoreRevision"])
	if err := k.authorizePage(ctx, k.db, e, page); err != nil {
		return nil, err
	}
	var result any
	err := k.write(ctx, func(tx *sql.Tx) error {
		if err := k.authorizePage(ctx, tx, e, page); err != nil {
			return err
		}
		snap, raw, err := loadRecoverySnapshot(ctx, tx, page, revision)
		if err != nil {
			return err
		}
		var decisions []historicalValueDecision
		if err = json.Unmarshal(marshal(r["propertyDecisions"]), &decisions); err != nil {
			return Fail("INVALID_REQUEST", "Invalid historical property decisions")
		}
		snap, issues, targets, ready, err := resolveHistoricalValues(ctx, tx, snap, decisions)
		if err != nil {
			return err
		}
		guards, files, err := restoreGuards(ctx, k, tx, e, page, snap)
		if err != nil {
			return err
		}
		if err = validateRestoreValues(ctx, k, tx, e, snap, guards); err != nil {
			return err
		}
		for _, file := range files {
			meta, err := readFileMetadata(ctx, tx, file)
			if err != nil {
				return err
			}
			if err = verifyAsset(k.path+"/"+meta.StorageKey, string(meta.ContentHash), parseSize(meta.SizeBytes)); err != nil {
				return err
			}
		}
		id, expires := NewID(), timeAfterMinutes(10)
		if _, err = tx.ExecContext(ctx, "INSERT INTO restore_previews VALUES(?,?,?,?,?,?,?,?,0)", id, page, revision, e.Actor.ActorId, e.ScopeGrantId, string(marshal(guards)), snapshotDigest(raw), expires); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, "INSERT INTO restore_preview_decisions VALUES(?,?,?,?)", id, string(marshal(decisions)), snapshotDigest(string(marshal(snap))), !ready); err != nil {
			return err
		}
		warnings := recoveryPreviewWarnings(snap)
		if !ready {
			warnings = append(warnings, "Review every incompatible historical property: map an explicit current typed value or retain the old value as inert evidence before restoring.")
		}
		for _, decision := range decisions {
			if decision.Action == "retain" {
				warnings = append(warnings, "Retain historical property "+decision.SourcePropertyID+" as inert evidence; keep its current typed value when present.")
			}
		}
		result = map[string]any{"previewId": id, "pageId": page, "restoreRevision": revision, "expiresAt": expires, "expectedRevisions": guards, "affectedFileIds": files, "snapshot": snap.ServiceReadPageResult, "warnings": warnings, "incompatibleValues": issues, "targetProperties": targets, "canRestore": ready}
		return nil
	})
	return result, err
}
func restoreGuards(ctx context.Context, k *Kernel, q querier, e contracts.ServiceExecutionContext, page string, snap recoverySnapshot) ([]contracts.ServiceExpectedRevision, []string, error) {
	targets := map[string]string{}
	add := func(kind, id string) error {
		if id == "" {
			return nil
		}
		table, ok := entityTables[kind]
		if !ok {
			return Fail("INVALID_REQUEST", "Unknown restore target")
		}
		var revision string
		err := q.QueryRowContext(ctx, "SELECT revision FROM "+table+" WHERE id=?", id).Scan(&revision)
		if errors.Is(err, sql.ErrNoRows) {
			revision = "0"
		} else if err != nil {
			return err
		}
		if revision != "0" {
			if err = k.authorizeEntity(ctx, q, e, kind, id); err != nil {
				return err
			}
		}
		targets[guardKey(kind, id)] = revision
		return nil
	}
	if err := add("page", page); err != nil {
		return nil, nil, err
	}
	if snap.Page.ParentPageId != nil {
		if err := add("page", string(*snap.Page.ParentPageId)); err != nil {
			return nil, nil, err
		}
	}
	current, err := readPageTx(ctx, q, page)
	if err != nil {
		return nil, nil, err
	}
	if current.Page.ParentPageId != nil {
		if err = add("page", string(*current.Page.ParentPageId)); err != nil {
			return nil, nil, err
		}
	}
	for _, b := range append(append([]contracts.DomainBlock{}, current.Blocks...), snap.Blocks...) {
		if err = add("block", string(b.Id)); err != nil {
			return nil, nil, err
		}
	}
	var record, collection string
	err = q.QueryRowContext(ctx, "SELECT id,collection_id FROM records WHERE page_id=?", page).Scan(&record, &collection)
	if err == nil {
		if snap.Record == nil {
			return nil, nil, Fail("CONFLICT", "Historical snapshot predates this page's record identity")
		}
		if string(snap.Record.Id) != record || string(snap.Record.CollectionId) != collection {
			return nil, nil, Fail("CONFLICT", "Record collection changed since this snapshot")
		}
		for _, target := range [][2]string{{"record", record}, {"collection", collection}} {
			if err = add(target[0], target[1]); err != nil {
				return nil, nil, err
			}
		}
		if err = checkHistoricalProperties(ctx, q, snap); err != nil {
			return nil, nil, err
		}
		for _, value := range snap.Record.Values {
			var valueData any
			json.Unmarshal(value.Value, &valueData)
			for _, def := range snap.Properties {
				if def.Id == value.PropertyId && def.Type == "relation" {
					if refs, ok := valueData.([]any); ok {
						for _, ref := range refs {
							if err = add("record", str(ref)); err != nil {
								return nil, nil, err
							}
						}
					}
				}
			}
			if err = add("property", string(value.PropertyId)); err != nil {
				return nil, nil, err
			}
		}
	} else if !errors.Is(err, sql.ErrNoRows) {
		return nil, nil, err
	} else if snap.Record != nil {
		return nil, nil, Fail("CONFLICT", "Historical record identity is no longer available")
	}
	if snap.RecordLayouts != nil {
		for _, entry := range snap.RecordLayouts.Collections {
			if err = add("collection", entry.CollectionID); err != nil {
				return nil, nil, err
			}
		}
	}
	if err = addRecoveryDatabaseGuards(ctx, q, snap, add); err != nil {
		return nil, nil, err
	}
	if err = addRecoveryMeetingGuards(ctx, q, page, snap, add); err != nil {
		return nil, nil, err
	}
	if err = validateRecoveryReferences(ctx, q, page, snap, add); err != nil {
		return nil, nil, err
	}
	ids := map[string]bool{}
	var v any
	json.Unmarshal(marshal(snap.ServiceReadPageResult), &v)
	// An arbitrary old payload may contain keys such as fileId. Retained value
	// evidence is readable provenance, not a new attachment or capability.
	if result, ok := v.(map[string]any); ok {
		if page, ok := result["page"].(map[string]any); ok {
			if provenance, ok := page["provenance"].(map[string]any); ok {
				if raw, ok := provenance["raw"].(map[string]any); ok {
					delete(raw, "retainedHistoricalValues")
				}
			}
		}
	}
	collectFileIDs(v, ids)
	for _, id := range snap.Attachments {
		ids[string(id)] = true
	}
	if snap.AttachmentRelationships != nil {
		blocks := map[string]bool{}
		for _, block := range snap.Blocks {
			blocks[string(block.Id)] = true
		}
		for _, relationship := range *snap.AttachmentRelationships {
			if _, err := recoveryAttachmentKind(relationship); err != nil {
				return nil, nil, err
			}
			if _, err := decodeAttachment(marshal(relationship)); err != nil {
				return nil, nil, err
			}
			if relationship.BlockID != "" && !blocks[relationship.BlockID] {
				return nil, nil, Fail("CONFLICT", "Historical attachment references an absent block")
			}
			ids[relationship.FileID] = true
		}
	}
	if snap.Record != nil {
		json.Unmarshal(marshal(snap.Record), &v)
		collectFileIDs(v, ids)
	}
	if snap.Meeting != nil && snap.Meeting.RecordingFileId != nil {
		ids[string(*snap.Meeting.RecordingFileId)] = true
	}
	files := []string{}
	for id := range ids {
		if err = add("file", id); err != nil {
			return nil, nil, err
		}
		files = append(files, id)
	}
	sort.Strings(files)
	keys := []string{}
	for key := range targets {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	guards := []contracts.ServiceExpectedRevision{}
	for _, key := range keys {
		for kind := range entityTables {
			prefix := kind + ":"
			if len(key) > len(prefix) && key[:len(prefix)] == prefix {
				guards = append(guards, required(kind, key[len(prefix):], targets[key]))
				break
			}
		}
	}
	if len(guards) > 1000 {
		return nil, nil, Fail("RESOURCE_EXHAUSTED", "Page recovery exceeds 1000 revision guards; retained content can be exported for recovery")
	}
	return guards, files, nil
}
func restoreHistory(m *Mutation, c map[string]any) error {
	page, revision := str(c["pageId"]), str(c["restoreRevision"])
	var storedPage, storedRevision, actor, grant, guardsRaw, digest, expires string
	var consumed bool
	err := m.Tx.QueryRowContext(m.Context, "SELECT page_id,restore_revision,actor_id,grant_id,guards_json,snapshot_hash,expires_at,consumed FROM restore_previews WHERE id=?", c["previewId"]).Scan(&storedPage, &storedRevision, &actor, &grant, &guardsRaw, &digest, &expires, &consumed)
	if errors.Is(err, sql.ErrNoRows) {
		return Fail("NOT_FOUND", "Restoration preview is absent")
	}
	if err != nil {
		return err
	}
	expiry, _ := time.Parse(time.RFC3339Nano, expires)
	if consumed || !expiry.After(time.Now()) || actor != string(m.Execution.Actor.ActorId) || grant != string(m.Execution.ScopeGrantId) || storedPage != page || storedRevision != revision {
		return Fail("CONFLICT", "Restoration preview expired or changed")
	}
	var guards []contracts.ServiceExpectedRevision
	if err = json.Unmarshal([]byte(guardsRaw), &guards); err != nil {
		return err
	}
	for _, guard := range guards {
		if m.GuardRevision(guard.Target.EntityType, string(guard.Target.EntityId)) != guard.Revision {
			return Fail("CONFLICT", "Restoration requires all preview revision guards")
		}
	}
	snap, raw, err := loadRecoverySnapshot(m.Context, m.Tx, page, revision)
	if err != nil {
		return err
	}
	if snapshotDigest(raw) != digest {
		return Fail("CONFLICT", "Historical snapshot changed")
	}
	var decisionsRaw, resolvedHash string
	var requiresDecisions bool
	err = m.Tx.QueryRowContext(m.Context, "SELECT decisions_json,resolved_hash,requires_decisions FROM restore_preview_decisions WHERE preview_id=?", c["previewId"]).Scan(&decisionsRaw, &resolvedHash, &requiresDecisions)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if err == nil {
		if requiresDecisions {
			return Fail("CONFLICT", "Review incompatible historical values before restoring")
		}
		var decisions []historicalValueDecision
		if err = json.Unmarshal([]byte(decisionsRaw), &decisions); err != nil {
			return err
		}
		var ready bool
		snap, _, _, ready, err = resolveHistoricalValues(m.Context, m.Tx, snap, decisions)
		if err != nil {
			return err
		}
		if !ready || snapshotDigest(string(marshal(snap))) != resolvedHash {
			return Fail("CONFLICT", "Historical value mapping or current schema changed; preview again")
		}
	}
	_, files, err := restoreGuards(m.Context, m.Kernel, m.Tx, m.Execution, page, snap)
	if err != nil {
		return err
	}
	for _, file := range files {
		meta, err := readFileMetadata(m.Context, m.Tx, file)
		if err != nil {
			return err
		}
		if err = verifyAsset(m.Kernel.path+"/"+meta.StorageKey, string(meta.ContentHash), parseSize(meta.SizeBytes)); err != nil {
			return err
		}
	}
	if err = m.TouchPage(page); err != nil {
		return err
	}
	current, err := readPageTx(m.Context, m.Tx, page)
	if err != nil {
		return err
	}
	if snap.Page.ParentPageId != nil {
		var deleted sql.NullString
		if err = m.Tx.QueryRowContext(m.Context, "SELECT deleted_at FROM pages WHERE id=?", *snap.Page.ParentPageId).Scan(&deleted); err != nil {
			return err
		}
		if deleted.Valid {
			return Fail("CONFLICT", "Restore the parent page first")
		}
	}
	wanted := map[string]contracts.DomainBlock{}
	for _, b := range snap.Blocks {
		wanted[string(b.Id)] = b
		var currentPage string
		if err = m.Tx.QueryRowContext(m.Context, "SELECT page_id FROM blocks WHERE id=?", b.Id).Scan(&currentPage); err == nil && currentPage != page {
			return Fail("CONFLICT", "Historical block moved to another page")
		} else if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
	}
	if err = prepareRecoverySegments(m, page, snap); err != nil {
		return err
	}
	// Park all existing order keys, preserving IDs and foreign references to blocks
	// which survive. Removing referenced transcript/source blocks fails atomically.
	for i, b := range current.Blocks {
		if _, err = m.Exec("UPDATE blocks SET sort_key=? WHERE id=?", fmt.Sprintf("%012d", 900000000000+i), b.Id); err != nil {
			return err
		}
	}
	for _, b := range current.Blocks {
		if _, exists := wanted[string(b.Id)]; exists {
			continue
		}
		if err = m.Deleted("block", string(b.Id)); err != nil {
			return err
		}
		for _, query := range []string{"DELETE FROM page_links WHERE source_block_id=?", "DELETE FROM todo_references WHERE block_id=?", "UPDATE attachments SET block_id=NULL WHERE block_id=?"} {
			if _, err = m.Exec(query, b.Id); err != nil {
				return err
			}
		}
		if _, err = m.Exec("DELETE FROM blocks WHERE id=?", b.Id); err != nil {
			return err
		}
	}
	for _, b := range snap.Blocks {
		id := string(b.Id)
		if err = m.Touch("block", id); err != nil {
			return err
		}
		if m.GuardRevision("block", id) == "0" {
			var tombstone string
			if e := m.Tx.QueryRowContext(m.Context, "SELECT revision FROM entity_tombstones WHERE entity_type='block' AND entity_id=?", id).Scan(&tombstone); e == nil {
				next, e := revNext(tombstone)
				if e != nil {
					return e
				}
				change := m.touched[guardKey("block", id)]
				change.After = contracts.DomainRevision(next)
				m.touched[guardKey("block", id)] = change
			} else if !errors.Is(e, sql.ErrNoRows) {
				return e
			}
		}
		if _, err = m.Exec("INSERT INTO blocks(id,page_id,parent_block_id,sort_key,type,schema_version,data_json,revision) VALUES(?,?,?,?,?,?,?,1) ON CONFLICT(id) DO UPDATE SET parent_block_id=excluded.parent_block_id,sort_key=excluded.sort_key,type=excluded.type,schema_version=excluded.schema_version,data_json=excluded.data_json", b.Id, page, b.ParentBlockId, b.SortKey, b.Type, b.SchemaVersion, string(b.Data)); err != nil {
			return err
		}
		var data any
		json.Unmarshal(b.Data, &data)
		if err = updateLinks(m, id, page, data); err != nil {
			return err
		}
		if _, err = m.Exec("DELETE FROM entity_tombstones WHERE entity_type='block' AND entity_id=?", id); err != nil {
			return err
		}
	}
	if _, err = m.Exec("UPDATE pages SET title=?,parent_page_id=?,deleted_at=?,provenance_json=? WHERE id=?", snap.Page.Title, snap.Page.ParentPageId, snap.Page.DeletedAt, nullableJSON(snap.Page.Provenance), page); err != nil {
		return err
	}
	if _, err = m.Exec("INSERT INTO page_metadata(page_id,archived,icon,cover_file_id,archived_at) VALUES(?,?,?,?,?) ON CONFLICT(page_id) DO UPDATE SET archived=excluded.archived,icon=excluded.icon,cover_file_id=excluded.cover_file_id,archived_at=excluded.archived_at", page, snap.Page.ArchivedAt != nil, snap.Page.Icon, snap.Page.CoverFileId, snap.Page.ArchivedAt); err != nil {
		return err
	}
	if err = restoreRecoveryLayouts(m, snap); err != nil {
		return err
	}
	if snap.DisplayPreferences != nil {
		if _, err = m.Exec("INSERT INTO p2_page_display VALUES(?,?,?) ON CONFLICT(page_id) DO UPDATE SET full_width=excluded.full_width,presentation=excluded.presentation", page, snap.DisplayPreferences.FullWidth, snap.DisplayPreferences.Presentation); err != nil {
			return err
		}
	}
	if err = restoreRecoveryMeeting(m, page, snap); err != nil {
		return err
	}
	if snap.Record != nil {
		if err = checkHistoricalProperties(m.Context, m.Tx, snap); err != nil {
			return err
		}
		if err = restoreRecoveryDatabase(m, snap); err != nil {
			return err
		}
		if _, err = m.Exec("DELETE FROM property_values WHERE record_id=?", snap.Record.Id); err != nil {
			return err
		}
		for _, value := range snap.Record.Values {
			p, err := propertyDefinition(m.Context, m.Tx, string(value.PropertyId))
			if err != nil {
				return err
			}
			if p.Type == "title" {
				continue
			}
			if p.CollectionId != snap.Record.CollectionId || p.SchemaVersion != value.SchemaVersion {
				return Fail("CONFLICT", "Property schema changed since the historical snapshot")
			}
			var v any
			json.Unmarshal(value.Value, &v)
			if err = validatePropertyValue(m, p, v); err != nil {
				return err
			}
			if _, err = m.Exec("INSERT INTO property_values(record_id,collection_id,property_id,property_type,schema_version,value_json) VALUES(?,?,?,?,?,?)", snap.Record.Id, snap.Record.CollectionId, p.Id, p.Type, value.SchemaVersion, string(value.Value)); err != nil {
				return err
			}
		}
		// Removing a promoted task would invalidate occurrences on other pages. Such
		// a cross-page change needs its own reviewed operation, not a page restore.
		var n int
		if err = m.Tx.QueryRowContext(m.Context, "SELECT count(*) FROM task_facets WHERE record_id=?", snap.Record.Id).Scan(&n); err != nil {
			return err
		}
		if snap.Task == nil && n > 0 {
			return Fail("CONFLICT", "Restoring before task promotion requires an explicit task operation")
		}
		if snap.Task != nil {
			if _, err = m.Exec("INSERT INTO task_facets(record_id,collection_id,status_property_id,due_property_id) VALUES(?,?,?,?) ON CONFLICT(record_id) DO UPDATE SET status_property_id=excluded.status_property_id,due_property_id=excluded.due_property_id", snap.Record.Id, snap.Record.CollectionId, snap.Task.StatusPropertyId, snap.Task.DuePropertyId); err != nil {
				return err
			}
		}
	}
	if _, err = m.Exec("DELETE FROM file_references WHERE page_id=?", page); err != nil {
		return err
	}
	if snap.AttachmentRelationships != nil {
		for _, relationship := range *snap.AttachmentRelationships {
			kind, kindErr := recoveryAttachmentKind(relationship)
			if kindErr != nil {
				return kindErr
			}
			_, source := attachmentReference(relationship.ImportAttachmentRelationship)
			if _, err = m.Exec("INSERT OR IGNORE INTO file_references VALUES(?,?,?,?)", page, kind, source, relationship.FileID); err != nil {
				return err
			}
		}
	} else {
		// Older snapshots retained only an aggregate inventory. Preserve their
		// documented recovery behavior without inventing block ownership.
		for _, id := range snap.Attachments {
			if _, err = m.Exec("INSERT INTO file_references VALUES(?,'explicit',?,?)", page, id, id); err != nil {
				return err
			}
		}
	}
	_, err = m.Exec("UPDATE restore_previews SET consumed=1 WHERE id=?", c["previewId"])
	return err
}
func nullableJSON(v any) any {
	if v == nil {
		return nil
	}
	return string(marshal(v))
}
func listTrash(ctx context.Context, k *Kernel, e contracts.ServiceExecutionContext, r map[string]any) (any, error) {
	rows, err := k.db.QueryContext(ctx, "SELECT id FROM pages WHERE deleted_at IS NOT NULL ORDER BY deleted_at DESC LIMIT 1000")
	if err != nil {
		return nil, err
	}
	ids := []string{}
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return nil, err
		}
		ids = append(ids, id)
	}
	rows.Close()
	pages := []contracts.DomainPage{}
	for _, id := range ids {
		if k.authorizePage(ctx, k.db, e, id) != nil {
			continue
		}
		p, err := readPageTx(ctx, k.db, id)
		if err != nil {
			return nil, err
		}
		pages = append(pages, p.Page)
	}
	return map[string]any{"pages": pages}, nil
}
func createRecoveryPoint(ctx context.Context, k *Kernel, e contracts.ServiceExecutionContext, r map[string]any) (any, error) {
	if err := requireOwnerScope(k, e); err != nil {
		return nil, err
	}
	var result any
	err := k.write(ctx, func(tx *sql.Tx) error {
		key := "recoveryPoint:" + string(e.Actor.ActorId) + ":" + str(r["idempotencyKey"])
		var previous string
		err := tx.QueryRowContext(ctx, "SELECT value FROM kernel_meta WHERE key=?", key).Scan(&previous)
		if err == nil {
			var saved map[string]any
			if err = json.Unmarshal([]byte(previous), &saved); err != nil {
				return err
			}
			if saved["label"] != r["label"] {
				return Fail("IDEMPOTENCY_CONFLICT", "Recovery point key was used with another label")
			}
			result = saved["result"]
			return nil
		} else if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		id, created := NewID(), now()
		var cursor string
		if err = tx.QueryRowContext(ctx, "SELECT CAST(COALESCE(MAX(cursor),0) AS TEXT) FROM committed_events").Scan(&cursor); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, "INSERT INTO recovery_points VALUES(?,?,?,?)", id, str(r["label"]), created, cursor); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, "INSERT INTO recovery_point_pages SELECT ?,id,revision FROM pages", id); err != nil {
			return err
		}
		result = map[string]any{"recoveryPointId": id, "createdAt": created, "eventCursor": cursor}
		_, err = tx.ExecContext(ctx, "INSERT INTO kernel_meta VALUES(?,?)", key, string(marshal(map[string]any{"label": r["label"], "result": result})))
		return err
	})
	return result, err
}
