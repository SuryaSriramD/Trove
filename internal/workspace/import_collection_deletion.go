package workspace

import (
	"context"
	"encoding/json"
	"sort"
	"strings"
	"trove.local/phase0/contracts"
)

func isSourceDeletion(kind string) bool {
	return kind == "source_deleted_page" || kind == "source_deleted_collection"
}

func preparePortableCollectionInventory(p *ImportPlan, manifest portableHierarchy) error {
	if manifest.CollectionEnumerationVersion == 0 {
		return nil
	}
	if manifest.CollectionEnumerationVersion != 1 || p.PageEnumerationScope == "" {
		return Fail("INTEGRITY_ERROR", "Collection enumeration requires a supported complete owner page inventory")
	}
	scope := "trove-owner-live-collections-v1:" + manifest.WorkspaceID
	seen := map[string]bool{}
	for _, c := range manifest.Collections {
		if seen[c.ID] || contracts.Validate("domain", "Id", marshal(c.ID)) != nil {
			return Fail("INTEGRITY_ERROR", "Invalid or duplicate owner collection identity")
		}
		seen[c.ID] = true
		found := false
		for i := range p.Items {
			item := &p.Items[i]
			if item.Locator == c.Path && item.Kind == "portable_collection" && item.PageSourceScope != "" {
				item.CollectionSourceScope = scope
				found = true
			}
		}
		if !found {
			return Fail("INTEGRITY_ERROR", "Owner collection enumeration has a missing typed source")
		}
	}
	p.CollectionEnumerationScope = scope
	return nil
}

func appendMissingSourceCollections(ctx context.Context, q querier, p *ImportPlan) error {
	if p.CollectionEnumerationScope == "" {
		return nil
	}
	present := map[string]bool{}
	livePages := map[string]bool{}
	for _, item := range p.Items {
		present[item.MappingID] = true
		if item.PageSourceScope != "" && item.Kind != "portable_collection" {
			livePages[item.PageID] = true
		}
	}
	rows, err := q.QueryContext(ctx, `SELECT m.id,m.local_id,m.identity_kind,m.identity_value,m.source_system,p.revision,p.title FROM import_mappings m JOIN pages p ON p.id=m.local_id WHERE m.source_namespace=? AND m.source_system=? AND m.external_kind='page' ORDER BY m.id`, p.Namespace, p.SourceSystem)
	if err != nil {
		return err
	}
	candidates := []ImportItem{}
	for rows.Next() {
		var item ImportItem
		if err = rows.Scan(&item.MappingID, &item.PageID, &item.IdentityKind, &item.ExternalID, &item.SourceSystem, &item.ExpectedRevision, &item.Title); err != nil {
			rows.Close()
			return err
		}
		if !present[item.MappingID] && livePages[item.PageID] {
			candidates = append(candidates, item)
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
		if !known || base.CollectionSourceScope == "" {
			continue
		}
		if base.CollectionSourceScope != p.CollectionEnumerationScope {
			return Fail("CONFLICT", "Source collection enumeration changed; use a separate source namespace")
		}
		if base.Fields["/collectionExists"] == nil {
			continue
		}
		// The retained collection inventory binds the source UUID, never its title.
		const prefix = "trove-owner-collection-v1:"
		source := strings.TrimPrefix(base.SourceScope, prefix)
		parts := strings.Split(source, ":")
		if !strings.HasPrefix(base.SourceScope, prefix) || len(parts) != 2 {
			return Fail("CONFLICT", "Collection baseline lacks a verified source identity")
		}
		if err = q.QueryRowContext(ctx, "SELECT local_id FROM import_entity_ids WHERE source_system=? AND source_namespace=? AND entity_kind='collection' AND source_identity=?", p.SourceSystem, p.Namespace, parts[1]).Scan(&item.CollectionID); err != nil {
			return err
		}
		var owner, revision string
		var hidden int
		if err = q.QueryRowContext(ctx, "SELECT owning_page_id,revision,(SELECT count(*) FROM collection_visibility WHERE collection_id=collections.id) FROM collections WHERE id=?", item.CollectionID).Scan(&owner, &revision, &hidden); err != nil {
			return err
		}
		if owner != item.PageID {
			return Fail("CONFLICT", "Collection ownership changed since retained source capture")
		}
		item.Kind = "source_deleted_collection"
		item.PortableIdentity = base.PortableIdentity
		item.Locator = "source-collection-absence:" + item.MappingID
		item.RawFileID = p.SourceFileID
		item.CollectionSourceScope = p.CollectionEnumerationScope
		item.PageSourceScope = base.PageSourceScope
		item.Outcome = "partial"
		item.Reason = "Absent from the complete owner collection inventory; removal retains its schema, values and identities and requires every live record to be reviewed"
		item.Conflict = "Review collection removal and all affected records explicitly; live ordinary source pages require a separate membership migration"
		item.Hash = importDigest(marshal(map[string]any{"scope": p.CollectionEnumerationScope, "mapping": item.MappingID, "absent": true}))
		incoming := base
		incoming.Fields = map[string]json.RawMessage{}
		for key, value := range base.Fields {
			if key != "/collectionExists" {
				incoming.Fields[key] = value
			}
		}
		incoming.SourceRevision = item.Hash
		item.Incoming = &incoming
		local := ImportPresence{Present: hidden == 0}
		if local.Present {
			local.Value = marshal(true)
		}
		item.Merge = &ImportMergeReview{ID: NewID(), Version: 1, BaselineStatus: "retained", Fields: []ImportFieldDecision{{Path: "/collectionExists", Outcome: "conflict", Base: importPresence(base.Fields, "/collectionExists"), Local: local, Incoming: ImportPresence{}}}, Resolutions: []ImportFieldChoice{}}
		item.MergeGuards = map[string]string{guardKey("page", owner): item.ExpectedRevision, guardKey("collection", item.CollectionID): revision}
		records, err := q.QueryContext(ctx, "SELECT r.id,r.revision,p.id,p.revision,p.title FROM records r JOIN pages p ON p.id=r.page_id WHERE r.collection_id=? AND p.deleted_at IS NULL ORDER BY r.id", item.CollectionID)
		if err != nil {
			return err
		}
		for records.Next() {
			var id, rev, page, prev, title string
			if err = records.Scan(&id, &rev, &page, &prev, &title); err != nil {
				records.Close()
				return err
			}
			item.MergeGuards[guardKey("record", id)] = rev
			item.MergeGuards[guardKey("page", page)] = prev
			item.Merge.Fields = append(item.Merge.Fields, ImportFieldDecision{Path: "/collectionRecords/" + id, Outcome: "conflict", Local: ImportPresence{Present: true, Value: marshal(map[string]any{"pageId": page, "title": title})}, Incoming: ImportPresence{}})
		}
		if err = records.Err(); err != nil {
			records.Close()
			return err
		}
		records.Close()
		if hidden != 0 {
			item.Choice = "keep_local"
			item.Conflict = ""
		}
		if p.PageDeletionOperationID == "" {
			p.PageDeletionOperationID = NewID()
		}
		p.Items = append(p.Items, item)
	}
	return nil
}

func reviewedCollectionDeletions(ctx context.Context, q querier, p *ImportPlan, selected map[string]bool) ([]map[string]any, error) {
	actions := []map[string]any{}
	liveSource := map[string]bool{}
	for _, item := range p.Items {
		if !isSourceDeletion(item.Kind) && item.Kind != "portable_collection" && item.PageSourceScope != "" {
			liveSource[item.PageID] = true
		}
	}
	for _, item := range p.Items {
		if item.Kind != "source_deleted_collection" || item.Choice == "keep_local" {
			continue
		}
		if item.Choice != "merge" || item.Merge == nil || item.CollectionSourceScope == "" || item.CollectionSourceScope != p.CollectionEnumerationScope {
			return nil, Fail("CONFLICT", "Review every source collection deletion explicitly")
		}
		choices := map[string]string{}
		for _, choice := range item.Merge.Resolutions {
			if choice.Choice != "keep_local" && choice.Choice != "take_incoming" {
				return nil, Fail("INVALID_REQUEST", "Collection deletion accepts only keep local or accept deletion")
			}
			choices[choice.Path] = choice.Choice
		}
		take := choices["/collectionExists"] == "take_incoming"
		for _, field := range item.Merge.Fields {
			if !strings.HasPrefix(field.Path, "/collectionRecords/") {
				continue
			}
			if take != (choices[field.Path] == "take_incoming") {
				return nil, Fail("CONFLICT", "Collection removal and all affected records must be reviewed together")
			}
			if !take {
				continue
			}
			var value struct {
				PageID string `json:"pageId"`
			}
			if json.Unmarshal(field.Local.Value, &value) != nil || value.PageID == "" {
				return nil, Fail("INTEGRITY_ERROR", "Invalid retained collection record review")
			}
			var page string
			if err := q.QueryRowContext(ctx, "SELECT page_id FROM records WHERE id=? AND collection_id=?", strings.TrimPrefix(field.Path, "/collectionRecords/"), item.CollectionID).Scan(&page); err != nil {
				return nil, err
			}
			if page != value.PageID {
				return nil, Fail("CONFLICT", "Collection membership changed after review")
			}
			if liveSource[page] {
				return nil, Fail("CONFLICT", "A collection record remains a live ordinary source page; preserve it until an explicit record-to-page membership migration is supported")
			}
			selected[page] = true
		}
		if take {
			actions = append(actions, map[string]any{"kind": "collection.trash.set", "collectionId": item.CollectionID, "trashed": true})
		}
	}
	sort.Slice(actions, func(i, j int) bool { return str(actions[i]["collectionId"]) < str(actions[j]["collectionId"]) })
	for _, action := range actions {
		if err := validateCollectionTrashSelected(ctx, q, action, selected); err != nil {
			return nil, err
		}
	}
	return actions, nil
}

// A source reappearance must never reactivate local visibility implicitly.
func keepHiddenImportCollection(ctx context.Context, q querier, item *ImportItem) (bool, error) {
	var n int
	if err := q.QueryRowContext(ctx, "SELECT count(*) FROM collection_visibility WHERE collection_id=?", item.CollectionID).Scan(&n); err != nil {
		return false, err
	}
	if n == 0 {
		return false, nil
	}
	if item.Choice != "keep_local" {
		return false, Fail("CONFLICT", "Restore the locally hidden collection before merging its source; keep local preserves its retained graph")
	}
	return true, nil
}

func hiddenImportCollections(ctx context.Context, q querier, p *ImportPlan) (map[string]bool, error) {
	hidden := map[string]bool{}
	for i := range p.Items {
		item := &p.Items[i]
		if item.Kind != "portable_collection" {
			continue
		}
		keep, err := keepHiddenImportCollection(ctx, q, item)
		if err != nil {
			return nil, err
		}
		if keep {
			hidden[item.CollectionID] = true
		}
	}
	for _, c := range p.PortableCollections {
		if !hidden[c.CollectionID] {
			continue
		}
		pages := map[string]bool{}
		for _, record := range c.Records {
			pages[record.PageID] = true
		}
		for i := range p.Items {
			item := &p.Items[i]
			if !pages[item.PageID] {
				continue
			}
			// The CSV row is retained representation of the separately reviewed
			// canonical page, not a second independent page effect.
			if item.Kind == "portable_record" && item.Merge == nil {
				var count int
				if err := q.QueryRowContext(ctx, "SELECT count(*) FROM records WHERE id=? AND page_id=? AND collection_id=?", item.RecordID, item.PageID, c.CollectionID).Scan(&count); err != nil {
					return nil, err
				}
				if count != 1 {
					return nil, Fail("CONFLICT", "Restore the hidden collection before adding a new source record")
				}
				item.Choice = "keep_local"
			}
			if item.Choice != "keep_local" {
				return nil, Fail("CONFLICT", "Keep every source record local or restore the collection before importing its graph")
			}
		}
	}
	return hidden, nil
}
