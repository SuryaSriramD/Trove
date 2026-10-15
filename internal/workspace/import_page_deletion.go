package workspace

import (
	"context"
	"database/sql"
	"encoding/json"
	"sort"
	"trove.local/phase0/contracts"
)

// This inventory enumerates all live, non-inventory pages from an owner export.
// Source completeness for other portable data remains independently partial.
func preparePortablePageInventory(ctx context.Context, q querier, p *ImportPlan, manifest portableHierarchy) error {
	if manifest.PageEnumerationVersion == 0 {
		return nil
	}
	if manifest.Format != "trove-portable" || manifest.PageEnumerationVersion != 1 || contracts.Validate("domain", "Id", marshal(manifest.WorkspaceID)) != nil {
		return Fail("INTEGRITY_ERROR", "Unsupported or invalid owner page enumeration")
	}
	scope := "trove-owner-live-content-pages-v1:" + manifest.WorkspaceID
	pages := map[string]bool{}
	ids := map[string]bool{}
	for _, entry := range manifest.Pages {
		id := string(entry.CanonicalPage.Id)
		if entry.Path == "" || pages[entry.Path] || ids[id] || contracts.Validate("domain", "Id", marshal(id)) != nil {
			return Fail("INTEGRITY_ERROR", "Owner page enumeration has invalid or duplicate identities")
		}
		pages[entry.Path], ids[id] = true, true
		found := false
		for _, item := range p.Items {
			if item.Locator == entry.Path && item.PageID != "" && item.Kind != "rejected" {
				found = true
			}
		}
		if !found {
			return Fail("INTEGRITY_ERROR", "Owner page enumeration has a missing canonical page source")
		}
	}
	selected := map[string]bool{}
	for i := range p.Items {
		if pages[p.Items[i].Locator] {
			selected[p.Items[i].PageID] = true
		}
	}
	for i := range p.Items {
		if selected[p.Items[i].PageID] {
			p.Items[i].PageSourceScope = scope
		}
	}
	p.PageEnumerationScope = scope
	return preparePortableCollectionInventory(p, manifest)
}

func appendMissingSourcePages(ctx context.Context, q querier, p *ImportPlan) error {
	if p.PageEnumerationScope == "" {
		return nil
	}
	present := map[string]bool{}
	for _, item := range p.Items {
		if item.PageID != "" {
			present[item.PageID] = true
		}
	}
	rows, err := q.QueryContext(ctx, `SELECT m.id,m.local_id,m.identity_kind,m.identity_value,m.source_system,p.revision,p.title,p.deleted_at FROM import_mappings m JOIN pages p ON p.id=m.local_id WHERE m.source_namespace=? AND m.source_system=? AND m.external_kind='page' ORDER BY m.id`, p.Namespace, p.SourceSystem)
	if err != nil {
		return err
	}
	candidates := []ImportItem{}
	deleted := map[string]bool{}
	for rows.Next() {
		var item ImportItem
		var tombstone sql.NullString
		if err = rows.Scan(&item.MappingID, &item.PageID, &item.IdentityKind, &item.ExternalID, &item.SourceSystem, &item.ExpectedRevision, &item.Title, &tombstone); err != nil {
			rows.Close()
			return err
		}
		if !present[item.PageID] {
			candidates = append(candidates, item)
			deleted[item.PageID] = tombstone.Valid
		}
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()
	for _, item := range candidates {
		base, _, known, err := loadImportBaseline(ctx, q, item.MappingID)
		if err != nil {
			return err
		}
		if !known || base.PageSourceScope == "" {
			continue
		}
		if base.PageSourceScope != p.PageEnumerationScope {
			return Fail("CONFLICT", "Source workspace enumeration changed; use a separate source namespace")
		}
		if base.Fields["/exists"] == nil || present[item.PageID] {
			continue
		}
		// A still-present collection owns its missing-record review. Whole collection
		// absence instead reviews each member page alongside the owning page.
		var collection string
		_ = q.QueryRowContext(ctx, "SELECT collection_id FROM records WHERE page_id=?", item.PageID).Scan(&collection)
		delegated := false
		for _, c := range p.PortableCollections {
			if c.CollectionID == collection {
				delegated = true
			}
		}
		for _, c := range p.Items {
			if c.Kind == "source_deleted_collection" && c.CollectionID == collection {
				delegated = true
			}
		}
		if delegated {
			continue
		}
		item.Kind = "source_deleted_page"
		item.PortableIdentity = base.PortableIdentity
		item.Locator = "source-absence:" + item.MappingID
		item.RawFileID = p.SourceFileID
		item.PageSourceScope = p.PageEnumerationScope
		item.Outcome = "partial"
		item.Reason = "Absent from complete owner page enumeration; reviewed deletion retains content, values, files and identity in Trash"
		item.Conflict = "Source page is absent from its bound owner inventory; explicitly keep local or accept deletion"
		item.Hash = importDigest(marshal(map[string]any{"scope": p.PageEnumerationScope, "mapping": item.MappingID, "absent": true}))
		incoming := base
		incoming.Fields = map[string]json.RawMessage{}
		for key, value := range base.Fields {
			if key != "/exists" {
				incoming.Fields[key] = value
			}
		}
		incoming.SourceRevision = item.Hash
		item.Incoming = &incoming
		local := ImportPresence{Present: !deleted[item.PageID]}
		if local.Present {
			local.Value = marshal(true)
		}
		item.Merge = &ImportMergeReview{ID: NewID(), Version: 1, BaselineStatus: "retained", LocalDeleted: deleted[item.PageID], Fields: []ImportFieldDecision{{Path: "/exists", Outcome: "conflict", Base: importPresence(base.Fields, "/exists"), Local: local, Incoming: ImportPresence{}}}, Resolutions: []ImportFieldChoice{}}
		if deleted[item.PageID] {
			item.Choice = "keep_local"
			item.Conflict = ""
		}
		item.MergeGuards = map[string]string{guardKey("page", item.PageID): item.ExpectedRevision}
		for _, query := range []string{"SELECT 'collection',id,revision FROM collections WHERE owning_page_id=?", "SELECT 'record',id,revision FROM records WHERE page_id=?", "SELECT 'collection',c.id,c.revision FROM records r JOIN collections c ON c.id=r.collection_id WHERE r.page_id=?"} {
			guards, err := q.QueryContext(ctx, query, item.PageID)
			if err != nil {
				return err
			}
			for guards.Next() {
				var kind, id, revision string
				if err = guards.Scan(&kind, &id, &revision); err != nil {
					guards.Close()
					return err
				}
				item.MergeGuards[guardKey(kind, id)] = revision
			}
			if err = guards.Err(); err != nil {
				guards.Close()
				return err
			}
			guards.Close()
		}
		if p.PageDeletionOperationID == "" {
			p.PageDeletionOperationID = NewID()
		}
		p.Items = append(p.Items, item)
		present[item.PageID] = true
	}
	return nil
}

func init() { RegisterCommand("page.trash.set", setPageTrash) }
func pageTrashCurrent(ctx context.Context, q querier, action map[string]any) (journalEntry, error) {
	entry := journalEntry{PageID: str(action["pageId"]), Target: "page-trash:" + str(action["pageId"])}
	var deleted sql.NullString
	if err := q.QueryRowContext(ctx, "SELECT deleted_at FROM pages WHERE id=?", entry.PageID).Scan(&deleted); err != nil {
		return entry, err
	}
	entry.Tombstone = deleted.String
	entry.Before = map[string]any{"kind": "page.trash.set", "pageId": entry.PageID, "trashed": deleted.Valid}
	return entry, nil
}

// selected is the set of pages that the same atomic group has explicitly reviewed
// for deletion/restoration. The command itself receives no such bypass.
func validatePageTrash(ctx context.Context, q querier, action map[string]any, selected map[string]bool) error {
	page := str(action["pageId"])
	var parent sql.NullString
	if err := q.QueryRowContext(ctx, "SELECT parent_page_id FROM pages WHERE id=?", page).Scan(&parent); err != nil {
		return err
	}
	if action["trashed"] == true {
		rows, err := q.QueryContext(ctx, `SELECT id FROM pages WHERE parent_page_id=? AND deleted_at IS NULL UNION SELECT p.id FROM collections c JOIN records r ON r.collection_id=c.id JOIN pages p ON p.id=r.page_id WHERE c.owning_page_id=? AND p.deleted_at IS NULL`, page, page)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var id string
			if err = rows.Scan(&id); err != nil {
				return err
			}
			if !selected[id] {
				return Fail("CONFLICT", "Page has live children or collection records outside the reviewed deletion; review or move them first")
			}
		}
		return rows.Err()
	}
	if parent.Valid && !selected[parent.String] {
		var live int
		if err := q.QueryRowContext(ctx, "SELECT count(*) FROM pages WHERE id=? AND deleted_at IS NULL", parent.String).Scan(&live); err != nil {
			return err
		}
		if live != 1 {
			return Fail("CONFLICT", "Restore the page parent before restoring this page")
		}
	}
	return nil
}
func setPageTrash(m *Mutation, action map[string]any) error {
	if err := validatePageTrash(m.Context, m.Tx, action, nil); err != nil {
		return err
	}
	page := str(action["pageId"])
	if err := m.TouchPage(page); err != nil {
		return err
	}
	if action["trashed"] == true {
		if err := m.Deleted("page", page); err != nil {
			return err
		}
		_, err := m.Exec("UPDATE pages SET deleted_at=COALESCE(deleted_at,?) WHERE id=?", now(), page)
		return err
	}
	var parent sql.NullString
	if err := m.Tx.QueryRowContext(m.Context, "SELECT parent_page_id FROM pages WHERE id=?", page).Scan(&parent); err != nil {
		return err
	}
	if parent.Valid {
		if err := m.Require("page", parent.String); err != nil {
			return err
		}
	}
	_, err := m.Exec("UPDATE pages SET deleted_at=NULL WHERE id=?", page)
	return err
}
func reviewedPageDeletionActions(ctx context.Context, q querier, p *ImportPlan) ([]map[string]any, error) {
	selected := map[string]bool{}
	for _, item := range p.Items {
		if item.Kind != "source_deleted_page" {
			continue
		}
		if item.Choice == "keep_local" {
			continue
		}
		if item.Choice != "merge" || item.Merge == nil || item.PageSourceScope == "" || item.PageSourceScope != p.PageEnumerationScope {
			return nil, Fail("CONFLICT", "Review every source page deletion explicitly")
		}
		take := false
		for _, choice := range item.Merge.Resolutions {
			if choice.Path == "/exists" {
				if choice.Choice == "take_incoming" {
					take = true
				} else if choice.Choice != "keep_local" {
					return nil, Fail("INVALID_REQUEST", "Source page existence accepts only keep local or accept source deletion")
				}
			}
		}
		if take {
			selected[item.PageID] = true
		}
	}
	collectionActions, err := reviewedCollectionDeletions(ctx, q, p, selected)
	if err != nil {
		return nil, err
	}
	if len(selected)+len(collectionActions) > 128 {
		return nil, Fail("RESOURCE_EXHAUSTED", "At most 128 source page deletions may be reviewed together")
	}
	keys := []string{}
	for id := range selected {
		keys = append(keys, id)
	}
	sort.Strings(keys)
	actions := []map[string]any{}
	state := map[string]int{}
	var visit func(string) error
	visit = func(id string) error {
		if state[id] == 2 {
			return nil
		}
		if state[id] == 1 {
			return Fail("CONFLICT", "Page deletion graph has a cycle")
		}
		state[id] = 1
		action := map[string]any{"kind": "page.trash.set", "pageId": id, "trashed": true}
		if err := validatePageTrash(ctx, q, action, selected); err != nil {
			return err
		}
		rows, err := q.QueryContext(ctx, `SELECT id FROM pages WHERE parent_page_id=? AND deleted_at IS NULL UNION SELECT p.id FROM collections c JOIN records r ON r.collection_id=c.id JOIN pages p ON p.id=r.page_id WHERE c.owning_page_id=? AND p.deleted_at IS NULL`, id, id)
		if err != nil {
			return err
		}
		children := []string{}
		for rows.Next() {
			var child string
			if err = rows.Scan(&child); err != nil {
				rows.Close()
				return err
			}
			children = append(children, child)
		}
		if err = rows.Err(); err != nil {
			rows.Close()
			return err
		}
		rows.Close()
		sort.Strings(children)
		for _, child := range children {
			if err = visit(child); err != nil {
				return err
			}
		}
		state[id] = 2
		actions = append(actions, action)
		return nil
	}
	for _, id := range keys {
		if err := visit(id); err != nil {
			return nil, err
		}
	}
	return append(actions, collectionActions...), nil
}
func commitSourcePageDeletions(m *Mutation, p *ImportPlan) error {
	actions, err := reviewedPageDeletionActions(m.Context, m.Tx, p)
	if err != nil {
		return err
	}
	for i := range p.Items {
		item := &p.Items[i]
		if !isSourceDeletion(item.Kind) {
			continue
		}
		if err := checkImportMergeGuards(m, item); err != nil {
			return err
		}
	}
	for _, action := range actions {
		page := str(action["pageId"])
		if action["kind"] == "collection.trash.set" {
			if err := m.Tx.QueryRowContext(m.Context, "SELECT owning_page_id FROM collections WHERE id=?", action["collectionId"]).Scan(&page); err != nil {
				return err
			}
			if err := guardImport(m, "collection", str(action["collectionId"])); err != nil {
				return err
			}
		}
		if err := importGuardPage(m, page); err != nil {
			return err
		}
		var record, collection string
		if err := m.Tx.QueryRowContext(m.Context, "SELECT id,collection_id FROM records WHERE page_id=?", page).Scan(&record, &collection); err == nil {
			if err := guardImport(m, "record", record); err != nil {
				return err
			}
			if err := guardImport(m, "collection", collection); err != nil {
				return err
			}
		} else if err != sql.ErrNoRows {
			return err
		}
	}
	if len(actions) > 0 {
		if err := applyJournal(m, p.PageDeletionOperationID, "Reviewed source page and collection deletions", actions); err != nil {
			return err
		}
	}
	for i := range p.Items {
		item := &p.Items[i]
		if !isSourceDeletion(item.Kind) {
			continue
		}
		if err := saveImportBaseline(m, p, item); err != nil {
			return err
		}
		if _, err := m.Exec("INSERT OR REPLACE INTO import_observations VALUES(?,?,?,?,?)", item.MappingID, p.ID, item.Hash, item.RawFileID, item.Outcome); err != nil {
			return err
		}
	}
	return nil
}
