package workspace

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
)

// Synthetic directory pages belong to the versioned import projection. Reuse
// them only from retained plans referenced by this capture's matched objects;
// titles and unrelated historical imports are not identity evidence.
func reconcileImportFolderIDs(ctx context.Context, q querier, p *ImportPlan) error {
	priorRuns := map[string]bool{}
	for _, item := range p.Items {
		if item.MappingID == "" {
			continue
		}
		var run string
		err := q.QueryRowContext(ctx, "SELECT import_run_id FROM import_baselines WHERE mapping_id=? AND normalizer_version=?", item.MappingID, importNormalizerVersion).Scan(&run)
		if errors.Is(err, sql.ErrNoRows) {
			continue
		}
		if err != nil {
			return err
		}
		priorRuns[run] = true
	}
	byLocator := map[string]map[string]string{}
	for run := range priorRuns {
		prior, err := loadImportPlan(ctx, q, run)
		if err != nil {
			return err
		}
		if prior.NormalizerVersion != importNormalizerVersion || prior.Namespace != p.Namespace || prior.SourceSystem != p.SourceSystem {
			continue
		}
		for _, item := range prior.Items {
			if item.Kind != "folder" {
				continue
			}
			var revision string
			err = q.QueryRowContext(ctx, "SELECT revision FROM pages WHERE id=? AND deleted_at IS NULL", item.PageID).Scan(&revision)
			if errors.Is(err, sql.ErrNoRows) {
				continue
			}
			if err != nil {
				return err
			}
			if byLocator[item.Locator] == nil {
				byLocator[item.Locator] = map[string]string{}
			}
			byLocator[item.Locator][item.PageID] = revision
		}
	}
	ids := map[string]string{}
	for i := range p.Items {
		item := &p.Items[i]
		if item.Kind != "folder" || len(byLocator[item.Locator]) != 1 {
			continue
		}
		for id, revision := range byLocator[item.Locator] {
			ids[item.PageID] = id
			item.PageID, item.ExpectedRevision, item.Choice = id, revision, "keep_local"
			item.Reason += "; existing directory identity retained from the accepted normalized source"
		}
	}
	for i := range p.Items {
		item := &p.Items[i]
		if id := ids[item.ParentID]; id != "" {
			item.ParentID = id
		}
		for j := range item.Blocks {
			rewriteImportBlock(item.Blocks[j].Data, ids)
		}
	}
	return nil
}

// Only explicit source IDs are reusable after source edits. Positional Markdown
// lines/anonymous DOM paths are evidence, not invented durable source identity.
func reconcileImportBlockIDs(ctx context.Context, q querier, item *ImportItem) error {
	if item.MappingID == "" || len(item.BlockSources) == 0 {
		return nil
	}
	var raw string
	if err := q.QueryRowContext(ctx, "SELECT COALESCE(provenance_json,'{}') FROM pages WHERE id=?", item.PageID).Scan(&raw); err != nil {
		return err
	}
	var provenance map[string]any
	if err := json.Unmarshal([]byte(raw), &provenance); err != nil {
		return err
	}
	evidence, _ := provenance["raw"].(map[string]any)
	prior, _ := evidence["sourceBlockIdentities"].(map[string]any)
	bySource := map[string]string{}
	for local, source := range prior {
		bySource[str(source)] = local
	}
	ids := map[string]string{}
	for local, source := range item.BlockSources {
		if !strings.HasPrefix(source, "notion-block:") && !strings.HasPrefix(source, "html-id:") && !strings.HasPrefix(source, "portable-block:") {
			continue
		}
		old := bySource[source]
		if old == "" {
			continue
		}
		var n int
		if err := q.QueryRowContext(ctx, "SELECT count(*) FROM blocks WHERE id=? AND page_id=?", old, item.PageID).Scan(&n); err != nil {
			return err
		}
		if n == 1 {
			ids[local] = old
		}
	}
	remapImportBlockIDs(item, ids)
	return nil
}

func remapImportBlockIDs(item *ImportItem, ids map[string]string) {
	if len(ids) == 0 {
		return
	}
	remap := func(id string) string {
		if replacement := ids[id]; replacement != "" {
			return replacement
		}
		return id
	}
	var walk func(any)
	walk = func(value any) {
		switch v := value.(type) {
		case map[string]any:
			for key, child := range v {
				if key == "contentSourceId" {
					v[key] = remap(str(child))
					continue
				}
				if key == "childBlockIds" {
					switch values := child.(type) {
					case []any:
						for i, id := range values {
							values[i] = remap(str(id))
						}
					case []string:
						for i, id := range values {
							values[i] = remap(id)
						}
					}
					continue
				}
				if key == "link" {
					if page, block, ok := parseCanonicalPageLink(str(child)); ok && block != "" {
						v[key] = canonicalPageLink(page, remap(block))
					}
					continue
				}
				walk(child)
			}
		case []any:
			for _, child := range v {
				walk(child)
			}
		}
	}
	parents, sources := map[string]string{}, map[string]string{}
	for _, block := range item.Blocks {
		parents[remap(block.ID)] = remap(item.BlockParents[block.ID])
		if source := item.BlockSources[block.ID]; source != "" {
			sources[remap(block.ID)] = source
		}
	}
	for i := range item.Blocks {
		item.Blocks[i].ID = remap(item.Blocks[i].ID)
		walk(item.Blocks[i].Data)
	}
	item.BlockParents, item.BlockSources = parents, sources
	for path, relationship := range item.AttachmentRelationships {
		relationship.BlockID = remap(relationship.BlockID)
		item.AttachmentRelationships[path] = relationship
	}
}
