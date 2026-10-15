package workspace

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/mail"
	"net/url"
	"strings"
	"time"
	"trove.local/phase0/contracts"
)

// The UI advances one durable unit per request. Closing the app or cancelling
// between requests leaves a resumable cursor and never deletes staged evidence.
func commitPortableImport(ctx context.Context, k *Kernel, e contracts.ServiceExecutionContext, r map[string]any) (any, error) {
	if err := requireOwnerScope(k, e); err != nil {
		return nil, err
	}
	p, err := loadImportPlan(ctx, k.db, str(r["planId"]))
	if err != nil {
		return nil, err
	}
	if p.Hash != str(r["planHash"]) {
		return nil, Fail("CONFLICT", "Import preview changed; reload it")
	}
	if p.State == "capturing" {
		return nil, Fail("CONFLICT", "Finish the capture and review its complete or partial inventory before importing")
	}
	if p.State == "complete" {
		return p, nil
	}
	request := map[string]any{"contractVersion": 1, "requestId": NewID(), "workspaceId": k.id, "mutation": map[string]any{"scopeGrantId": e.ScopeGrantId, "idempotencyKey": p.ID + fmt.Sprintf("-%t-%d", p.Initialized, p.Cursor), "expectedRevisions": []any{}}, "commands": []any{map[string]any{"kind": "import.applyPlan", "planId": p.ID, "planHash": p.Hash, "resolutions": []any{}}}}
	response := k.batch(ctx, e, marshal(request))
	var out struct {
		OK    bool `json:"ok"`
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err = json.Unmarshal(response, &out); err != nil {
		return nil, err
	}
	if !out.OK {
		return nil, Fail(out.Error.Code, out.Error.Message)
	}
	return loadImportPlan(ctx, k.db, p.ID)
}

// guardImport obtains guards only for IDs in the frozen, owner-approved plan.
// Existing target pages are independently compared with their preview revision.
func guardImport(m *Mutation, kind, id string) error {
	if id == "" {
		return nil
	}
	key := guardKey(kind, id)
	if _, ok := m.guards[key]; ok {
		return nil
	}
	table, ok := entityTables[kind]
	if !ok {
		return Fail("INVALID_REQUEST", "Unknown import entity")
	}
	var rev string
	err := m.Tx.QueryRowContext(m.Context, "SELECT revision FROM "+table+" WHERE id=?", id).Scan(&rev)
	if errors.Is(err, sql.ErrNoRows) {
		rev = "0"
	} else if err != nil {
		return err
	}
	m.guards[key] = rev
	return nil
}
func importGuardPage(m *Mutation, id string) error {
	if err := guardImport(m, "page", id); err != nil {
		return err
	}
	var rid string
	err := m.Tx.QueryRowContext(m.Context, "SELECT id FROM records WHERE page_id=?", id).Scan(&rid)
	if err == nil {
		return guardImport(m, "record", rid)
	}
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	return err
}
func importAttach(m *Mutation, page, file string) error {
	if file == "" {
		return nil
	}
	_, err := m.Exec("INSERT OR IGNORE INTO file_references VALUES(?,'import_evidence',?,?)", page, file, file)
	return err
}
func applyPortableImport(m *Mutation, c map[string]any) error {
	if unit, ok := m.Context.Value(jobCommitKey{}).(jobCommit); ok {
		if m.Execution.Actor.Kind != "job" || string(m.Execution.Actor.ActorId) != unit.JobID {
			return Fail("SCOPE_DENIED", "Import job principal mismatch")
		}
		if scope, ok := m.Execution.Scope.Value.(contracts.DomainScopeVariant1); !ok || string(scope.WorkspaceId) != m.Kernel.id {
			return Fail("SCOPE_DENIED", "Import requires workspace scope")
		}
	} else if err := requireOwnerScope(m.Kernel, m.Execution); err != nil {
		return err
	}
	p, err := loadImportPlan(m.Context, m.Tx, str(c["planId"]))
	if err != nil {
		return err
	}
	if p.Hash != str(c["planHash"]) {
		return Fail("CONFLICT", "Import plan hash changed")
	}
	if p.State == "capturing" {
		return Fail("CONFLICT", "Finish the capture before committing its import plan")
	}
	if p.State == "complete" {
		return nil
	}
	if p.Initialized {
		for i := range p.Items {
			if err := checkImportMergeGuards(m, &p.Items[i]); err != nil {
				return err
			}
		}
	}
	if p.RootParentID != "" {
		if err = m.AuthorizePage(p.RootParentID); err != nil {
			return err
		}
		var revision string
		if err = m.Tx.QueryRowContext(m.Context, "SELECT revision FROM pages WHERE id=? AND deleted_at IS NULL", p.RootParentID).Scan(&revision); err != nil {
			return Fail("NOT_FOUND", "Capture destination is unavailable")
		}
		if !p.Initialized && revision != p.ExpectedParentRevision {
			return Fail("CONFLICT", "Capture destination changed after preview; create a new reviewed capture")
		}
		if err = guardImport(m, "page", p.RootParentID); err != nil {
			return err
		}
	}
	if !p.Initialized {
		if err := prepareNotionGraphChoice(&p); err != nil {
			return err
		}
		if err := validateNotionIdentityPreview(m, &p); err != nil {
			return err
		}
		if resolutions, ok := c["resolutions"].([]any); ok {
			if err := applyImportFieldResolutions(&p, resolutions); err != nil {
				return err
			}
		}
		remap := map[string]string{}
		for i := range p.Items {
			item := &p.Items[i]
			if err := checkImportMergeGuards(m, item); err != nil {
				return err
			}
			if item.Conflict != "" && item.Choice == "" {
				return Fail("CONFLICT", "Resolve re-import conflict for "+item.Locator)
			}
			if item.MappingID != "" {
				var revision string
				var deleted sql.NullString
				err := m.Tx.QueryRowContext(m.Context, "SELECT revision,deleted_at FROM pages WHERE id=?", item.PageID).Scan(&revision, &deleted)
				if err != nil {
					return err
				}
				if revision != item.ExpectedRevision {
					return Fail("CONFLICT", "Page changed after import preview: "+item.Locator)
				}
				if deleted.Valid && item.Choice == "replace" {
					return Fail("CONFLICT", "Restore the trashed page first, or import a copy")
				}
			}
			if item.Choice == "replace" && item.Incoming != nil {
				var parent *string
				if err := json.Unmarshal(item.Incoming.Fields["/parent"], &parent); err != nil {
					return err
				}
				item.ParentID = ""
				if parent != nil {
					item.ParentID = *parent
				}
			}
			if err := materializeImportMerge(item); err != nil {
				return err
			}
			if item.Choice == "duplicate" {
				blockIDs := map[string]string{}
				for _, block := range item.Blocks {
					blockIDs[block.ID] = NewID()
				}
				remapImportBlockIDs(item, blockIDs)
				old := item.PageID
				if remap[old] == "" {
					remap[old] = NewID()
				}
				item.PageID = remap[old]
				item.MappingID = ""
				item.ExpectedRevision = ""
				item.IdentityKind = "derived_locator"
				item.ExternalID = item.Locator + "#copy=" + p.ID
				item.Merge = nil
				snapshot := snapshotImportItem(item)
				item.Incoming = &snapshot
			}
		}
		for i := range p.Items {
			item := &p.Items[i]
			if id := remap[item.ParentID]; id != "" {
				item.ParentID = id
			}
			if id := remap[item.PageID]; id != "" && (item.Kind == "row" || item.Kind == "portable_record") {
				item.PageID = id
			}
			for b := range item.Blocks {
				rewriteImportBlock(item.Blocks[b].Data, remap)
			}
		}
		remapPortableDatabasePages(&p, remap)
		if p.NotionCopyGraph {
			for i := range p.Items {
				item := &p.Items[i]
				if item.PageID != "" {
					snapshot := snapshotImportItem(item)
					if c := importCollectionFor(&p, item); c != nil {
						addIncomingDatabaseFields(&snapshot, c)
					}
					item.Incoming = &snapshot
					item.ResolvedFields = nil
					item.MergeGuards = nil
				}
			}
		}
		// Keeping an existing CSV also keeps all its records. A source CSV never
		// silently becomes authority for deleting absent or locally created rows.
		for i := range p.Items {
			item := &p.Items[i]
			if item.Kind == "row" || item.Kind == "portable_record" {
				for _, parent := range p.Items {
					if parent.Locator == item.CSVSource && parent.Choice == "keep_local" && !p.TypedDatabaseMerge {
						item.Choice = "keep_local"
						item.PageID = ""
						item.Reason += "; retained incoming row while keeping the local database"
					}
				}
			}
		}
		if _, err := reviewedPageDeletionActions(m.Context, m.Tx, &p); err != nil {
			return err
		}
		if err := validateReviewedImportDatabase(m.Context, m.Tx, &p); err != nil {
			return err
		}
		if err = guardImport(m, "page", p.RootID); err != nil {
			return err
		}
		if err = pageCommand(m, map[string]any{"kind": "page.create", "pageId": p.RootID, "title": "Import · " + p.SourceName, "parentPageId": nullable(p.RootParentID)}); err != nil {
			return err
		}
		if err = importAttach(m, p.RootID, p.SourceFileID); err != nil {
			return err
		}
		pending := []int{}
		for i, item := range p.Items {
			if item.RawFileID != "" {
				if err = importAttach(m, p.RootID, item.RawFileID); err != nil {
					return err
				}
			}
			if item.PageID != "" && item.Choice != "keep_local" {
				pending = append(pending, i)
			}
		}
		for len(pending) > 0 {
			remaining := []int{}
			made := false
			for _, i := range pending {
				item := &p.Items[i]
				if _, created := m.touched[guardKey("page", item.PageID)]; created {
					continue
				}
				if err = importGuardPage(m, item.PageID); err != nil {
					return err
				}
				if m.GuardRevision("page", item.PageID) != "0" {
					continue
				}
				var present int
				if err = m.Tx.QueryRowContext(m.Context, "SELECT count(*) FROM pages WHERE id=?", item.ParentID).Scan(&present); err != nil {
					return err
				}
				if present == 0 {
					remaining = append(remaining, i)
					continue
				}
				if err = guardImport(m, "page", item.ParentID); err != nil {
					return err
				}
				if err = pageCommand(m, map[string]any{"kind": "page.create", "pageId": item.PageID, "parentPageId": item.ParentID, "title": item.Title}); err != nil {
					return err
				}
				made = true
			}
			if !made && len(remaining) > 0 {
				return Fail("INVALID_REQUEST", "Import hierarchy has a cycle or missing parent")
			}
			pending = remaining
		}
		p.Initialized = true
		p.State = "running"
	} else if p.Cursor < len(p.Items) {
		item := &p.Items[p.Cursor]
		if err = commitImportItem(m, &p, item); err != nil {
			return err
		}
		item.Committed = true
		p.Cursor++
	}
	// Every source remains accounted for even when an individual format is
	// unsupported. State completion means processing finished, not full fidelity.
	if p.Cursor == len(p.Items) {
		if err = commitPortableDatabase(m, &p); err != nil {
			return err
		}
		if err = commitSourcePageDeletions(m, &p); err != nil {
			return err
		}
		p.State = "complete"
	}
	// Record projections touch their collection during commit validation. Do
	// that now so the durable next-unit guards include this import's own bump.
	for _, change := range m.touched {
		if change.Target.EntityType == "record" {
			var collection string
			if err := m.Tx.QueryRowContext(m.Context, "SELECT collection_id FROM records WHERE id=?", change.Target.EntityId).Scan(&collection); err != nil {
				return err
			}
			if err := guardImport(m, "collection", collection); err != nil {
				return err
			}
			if err := m.Touch("collection", collection); err != nil {
				return err
			}
		}
	}
	for i := range p.Items {
		item := &p.Items[i]
		for key := range item.MergeGuards {
			if change, ok := m.touched[key]; ok {
				item.MergeGuards[key] = string(change.After)
			}
		}
		if v, ok := m.touched[guardKey("page", item.PageID)]; ok {
			item.ExpectedRevision = string(v.After)
		}
	}
	p.CommittedBatchIDs = append(p.CommittedBatchIDs, string(m.Request.RequestId))
	return saveImportPlan(m.Context, m.Tx, p)
}
func rewriteImportBlock(v any, ids map[string]string) {
	switch x := v.(type) {
	case map[string]any:
		for key, child := range x {
			if key == "targetPageId" {
				if id := ids[str(child)]; id != "" {
					x[key] = id
				}
			}
			if key == "link" {
				if page, block, ok := parseCanonicalPageLink(str(child)); ok {
					if id := ids[page]; id != "" {
						x[key] = canonicalPageLink(id, block)
					}
				}
			}
			rewriteImportBlock(child, ids)
		}
	case []any:
		for _, child := range x {
			rewriteImportBlock(child, ids)
		}
	}
}

func commitImportItem(m *Mutation, p *ImportPlan, item *ImportItem) error {
	if isSourceDeletion(item.Kind) {
		return nil
	}
	if item.Kind == "directory" || item.Kind == "asset" || item.Kind == "rejected" {
		return nil
	}
	if item.Choice == "keep_local" {
		if item.MappingID != "" && item.PageID != "" {
			var revision string
			if err := m.Tx.QueryRowContext(m.Context, "SELECT revision FROM pages WHERE id=?", item.PageID).Scan(&revision); err != nil {
				return err
			}
			if revision != item.ExpectedRevision {
				return Fail("CONFLICT", "Page changed after import review")
			}
			if item.Kind != "portable_collection" {
				if err := saveImportBaseline(m, p, item); err != nil {
					return err
				}
			}
			if _, err := m.Exec("INSERT OR REPLACE INTO import_observations VALUES(?,?,?,?,?)", item.MappingID, p.ID, item.Hash, item.RawFileID, item.Outcome); err != nil {
				return err
			}
		}
		item.Reason += "; incoming bytes retained without changing local content"
		if item.Conflict != "" {
			item.Outcome = "partial"
		}
		return nil
	}
	if err := importGuardPage(m, item.PageID); err != nil {
		return err
	}
	if item.ExpectedRevision != "" && m.GuardRevision("page", item.PageID) != item.ExpectedRevision {
		return Fail("CONFLICT", "Imported page changed while processing; local edits were preserved: "+item.Locator)
	}
	if err := m.TouchPage(item.PageID); err != nil {
		return err
	}
	if item.Choice == "replace" {
		rows, err := m.Tx.QueryContext(m.Context, "SELECT id FROM blocks WHERE page_id=?", item.PageID)
		if err != nil {
			return err
		}
		ids := []string{}
		for rows.Next() {
			var id string
			if err = rows.Scan(&id); err != nil {
				rows.Close()
				return err
			}
			ids = append(ids, id)
		}
		rows.Close()
		wanted := map[string]bool{}
		for _, block := range item.Blocks {
			wanted[block.ID] = true
		}
		var parkingBase int64
		if err = m.Tx.QueryRowContext(m.Context, "SELECT COALESCE(MAX(CAST(sort_key AS INTEGER)),0) FROM blocks WHERE page_id=?", item.PageID).Scan(&parkingBase); err != nil {
			return err
		}
		for index, id := range ids {
			if err = guardImport(m, "block", id); err != nil {
				return err
			}
			// Park old ownership/order until the reviewed source graph is rebuilt.
			// Explicit source identities survive replacement, including linked IDs.
			if _, err = m.Exec("UPDATE blocks SET parent_block_id=NULL,sort_key=? WHERE id=?", fmt.Sprintf("%012d", parkingBase+int64(index)+1), id); err != nil {
				return err
			}
			if wanted[id] {
				continue
			}
			if err = m.Deleted("block", id); err != nil {
				return err
			}
			if _, err = m.Exec("DELETE FROM page_links WHERE source_block_id=?", id); err != nil {
				return err
			}
			if _, err = m.Exec("DELETE FROM todo_references WHERE block_id=?", id); err != nil {
				return err
			}
			if _, err = m.Exec("UPDATE attachments SET block_id=NULL WHERE block_id=?", id); err != nil {
				return err
			}
		}
		// A page containing transcript-owned blocks requires meeting-specific review.
		var transcripts int
		if err = m.Tx.QueryRowContext(m.Context, "SELECT count(*) FROM transcript_segments WHERE block_id IN(SELECT id FROM blocks WHERE page_id=?)", item.PageID).Scan(&transcripts); err != nil {
			return err
		}
		if transcripts > 0 {
			return Fail("CONFLICT", "Meeting content cannot be replaced by an import; import a separate copy")
		}
		for _, id := range ids {
			if !wanted[id] {
				if _, err = m.Exec("DELETE FROM blocks WHERE id=?", id); err != nil {
					return err
				}
			}
		}
		if item.Merge != nil {
			var previous sql.NullString
			if err := m.Tx.QueryRowContext(m.Context, "SELECT parent_page_id FROM pages WHERE id=?", item.PageID).Scan(&previous); err != nil {
				return err
			}
			if previous.String != item.ParentID {
				if err := guardImport(m, "page", previous.String); err != nil {
					return err
				}
				if err := guardImport(m, "page", item.ParentID); err != nil {
					return err
				}
				if err := pageCommand(m, map[string]any{"kind": "page.move", "pageId": item.PageID, "parentPageId": nullable(item.ParentID)}); err != nil {
					return err
				}
			}
		}
		if _, err = m.Exec("UPDATE pages SET title=? WHERE id=?", item.Title, item.PageID); err != nil {
			return err
		}
	}
	// Re-import review covers source content. Local presentation is preserved;
	// display metadata is applied only when creating a new page or explicit copy.
	if item.DisplayPreferences != nil && item.Merge == nil {
		if _, err := m.Exec("INSERT INTO p2_page_display VALUES(?,?,?) ON CONFLICT(page_id) DO UPDATE SET full_width=excluded.full_width,presentation=excluded.presentation", item.PageID, item.DisplayPreferences.FullWidth, item.DisplayPreferences.Presentation); err != nil {
			return err
		}
	}
	provenance := map[string]any{"sourceSystem": importItemSourceSystem(p, item), "raw": map[string]any{"sourceNamespace": p.Namespace, "sourceLocator": item.Locator, "externalId": item.ExternalID, "rawFileId": item.RawFileID, "importRunId": p.ID, "supplied": item.Provenance, "sourceBlockIdentities": item.BlockSources}}
	if _, err := m.Exec("UPDATE pages SET provenance_json=? WHERE id=?", string(marshal(provenance)), item.PageID); err != nil {
		return err
	}
	if err := importAttach(m, item.PageID, item.RawFileID); err != nil {
		return err
	}
	for _, block := range item.Blocks {
		if err := guardImport(m, "block", block.ID); err != nil {
			return err
		}
		if item.Choice == "replace" && m.GuardRevision("block", block.ID) != "0" {
			if err := pageCommand(m, map[string]any{"kind": "block.update", "blockId": block.ID, "type": block.Type, "schemaVersion": float64(1), "data": block.Data}); err != nil {
				return err
			}
			parent := item.BlockParents[block.ID]
			if parent != "" {
				if err := m.Require("block", parent); err != nil {
					return err
				}
			}
			key, err := appendKey(m.Context, m.Tx, item.PageID, parent)
			if err != nil {
				return err
			}
			if _, err = m.Exec("UPDATE blocks SET parent_block_id=?,sort_key=? WHERE id=?", nullable(parent), key, block.ID); err != nil {
				return err
			}
			continue
		}
		if err := pageCommand(m, map[string]any{"kind": "block.insert", "block": map[string]any{"blockId": block.ID, "pageId": item.PageID, "parentBlockId": nullable(item.BlockParents[block.ID]), "type": block.Type, "schemaVersion": float64(1), "data": block.Data}}); err != nil {
			return err
		}
	}
	if err := applyImportAttachments(m, item); err != nil {
		return err
	}
	if item.Kind == "csv" {
		if err := guardImport(m, "collection", item.CollectionID); err != nil {
			return err
		}
		if err := guardImport(m, "property", item.Columns[0].PropertyID); err != nil {
			return err
		}
		if err := databaseCommand(m, map[string]any{"kind": "collection.create", "collectionId": item.CollectionID, "owningPageId": item.PageID, "title": item.Title, "titlePropertyId": item.Columns[0].PropertyID}); err != nil {
			return err
		}
		for _, column := range item.Columns[1:] {
			if err := guardImport(m, "property", column.PropertyID); err != nil {
				return err
			}
			config := map[string]any{}
			if column.Type == "number" {
				config["format"] = "number"
			}
			if err := databaseCommand(m, map[string]any{"kind": "collection.addProperty", "collectionId": item.CollectionID, "propertyId": column.PropertyID, "name": column.Name, "type": column.Type, "schemaVersion": float64(1), "config": config}); err != nil {
				return err
			}
		}
	}
	if item.Kind == "row" {
		var source *ImportItem
		for i := range p.Items {
			if p.Items[i].Locator == item.CSVSource {
				source = &p.Items[i]
				break
			}
		}
		if source == nil {
			return Fail("NOT_FOUND", "CSV source disappeared")
		}
		if err := guardImport(m, "collection", item.CollectionID); err != nil {
			return err
		}
		if err := guardImport(m, "record", item.RecordID); err != nil {
			return err
		}
		if err := m.Touch("collection", item.CollectionID); err != nil {
			return err
		}
		if err := m.Touch("record", item.RecordID); err != nil {
			return err
		}
		var n int64
		if err := m.Tx.QueryRowContext(m.Context, "SELECT COALESCE(MAX(CAST(sort_key AS INTEGER)),0)+1 FROM records WHERE collection_id=?", item.CollectionID).Scan(&n); err != nil {
			return err
		}
		var pageRevision string
		if err := m.Tx.QueryRowContext(m.Context, "SELECT revision FROM pages WHERE id=?", item.PageID).Scan(&pageRevision); err != nil {
			return err
		}
		if _, err := m.Exec("INSERT INTO records VALUES(?,?,?,?,?)", item.RecordID, item.CollectionID, item.PageID, fmt.Sprintf("%012d", n), pageRevision); err != nil {
			return err
		}
		recordChange := m.touched[guardKey("record", item.RecordID)]
		recordChange.After = m.touched[guardKey("page", item.PageID)].After
		m.touched[guardKey("record", item.RecordID)] = recordChange
		for index, column := range source.Columns {
			if index == 0 || index >= len(item.Cells) || item.Cells[index] == "" {
				continue
			}
			if err := guardImport(m, "property", column.PropertyID); err != nil {
				return err
			}
			value, err := importCellValue(column.Type, item.Cells[index])
			if err != nil {
				item.Outcome = "partial"
				item.Reason += "; " + column.Name + " could not convert to " + column.Type + "; original value retained"
				fallback := fallbackImportBlock(item, column.Name+": "+item.Cells[index])
				if err = guardImport(m, "block", fallback.ID); err != nil {
					return err
				}
				if err = pageCommand(m, map[string]any{"kind": "block.insert", "block": map[string]any{"blockId": fallback.ID, "pageId": item.PageID, "type": fallback.Type, "schemaVersion": float64(1), "data": fallback.Data}}); err != nil {
					return err
				}
				continue
			}
			if err = setRecordValue(m, item.RecordID, column.PropertyID, map[string]any{"type": column.Type, "schemaVersion": float64(1), "value": value}); err != nil {
				return err
			}
		}
	}
	if item.RawFileID != "" {
		mapping := item.MappingID
		if mapping == "" {
			mapping = NewID()
			item.MappingID = mapping
		}
		_, err := m.Exec(`INSERT INTO import_mappings VALUES(?,?,?,?,? ,?,? ,'page',?,?,?,?,?) ON CONFLICT(workspace_id,source_system,source_namespace,external_kind,identity_kind,identity_value) DO UPDATE SET base_source_revision=excluded.base_source_revision,base_content_hash=excluded.base_content_hash,base_raw_file_id=excluded.base_raw_file_id,last_import_run_id=excluded.last_import_run_id`, mapping, m.Kernel.id, importItemSourceSystem(p, item), p.Namespace, "page", item.IdentityKind, item.ExternalID, item.PageID, item.Hash, item.Hash, item.RawFileID, p.ID)
		if err != nil {
			return err
		}
		// Resolve the actual mapping ID when an existing identity was updated.
		if err = m.Tx.QueryRowContext(m.Context, "SELECT id FROM import_mappings WHERE workspace_id=? AND source_system=? AND source_namespace=? AND external_kind='page' AND identity_kind=? AND identity_value=?", m.Kernel.id, importItemSourceSystem(p, item), p.Namespace, item.IdentityKind, item.ExternalID).Scan(&mapping); err != nil {
			return err
		}
		item.MappingID = mapping
		if item.Kind != "portable_collection" {
			if err = saveImportBaseline(m, p, item); err != nil {
				return err
			}
		}
		if _, err = m.Exec("INSERT OR REPLACE INTO import_observations VALUES(?,?,?,?,?)", mapping, p.ID, item.Hash, item.RawFileID, item.Outcome); err != nil {
			return err
		}
		revision := m.touched[guardKey("page", item.PageID)].After
		if _, err = m.Exec("INSERT OR REPLACE INTO import_mapping_state VALUES(?,?)", mapping, revision); err != nil {
			return err
		}
	}
	return nil
}
func importCellValue(kind, value string) (any, error) {
	switch kind {
	case "text":
		return richImport(value), nil
	case "number":
		if !decimalImport.MatchString(value) {
			return nil, fmt.Errorf("not a decimal")
		}
		return map[string]any{"decimal": value, "format": "number"}, nil
	case "checkbox":
		if strings.EqualFold(value, "true") {
			return true, nil
		}
		if strings.EqualFold(value, "false") {
			return false, nil
		}
		return nil, fmt.Errorf("not a boolean")
	case "date":
		if _, err := time.Parse("2006-01-02", value); err != nil {
			return nil, err
		}
		return map[string]any{"start": value, "end": nil, "timezone": "UTC", "hasTime": false}, nil
	case "url":
		u, err := url.Parse(value)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https" && u.Scheme != "mailto" && u.Scheme != "tel") {
			return nil, fmt.Errorf("invalid URL")
		}
		return value, nil
	case "email":
		a, err := mail.ParseAddress(value)
		if err != nil || a.Address != value {
			return nil, fmt.Errorf("invalid email")
		}
		return value, nil
	}
	return nil, fmt.Errorf("unsupported property type")
}
