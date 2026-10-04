package workspace

import (
	"strings"
)

func notionSourceID(v any) string { return strings.ToLower(strings.ReplaceAll(str(v), "-", "")) }

// A children response already supplies its parent via the requested endpoint.
// Keep that relationship independently of pagination order and local UUIDs.
func reconstructNotionStructure(p *ImportPlan, pages map[string]int) {
	for pageID, at := range pages {
		page := &p.Items[at]
		page.BlockParents, page.BlockSources = map[string]string{}, map[string]string{}
		bySource := map[string]*ImportItem{}
		ordered := []*ImportItem{}
		for i := range p.Items {
			item := &p.Items[i]
			if item.Kind == "notion_block" && item.PageID == pageID {
				if item.ExternalID == "" {
					// Captures persisted by the older adapter have parent evidence in
					// the response locator, but no explicit per-block source metadata.
					item.ExternalID = strings.Split(strings.TrimPrefix(item.Locator, "notion/blocks/"), "#")[0]
					prior, _ := item.Provenance.(map[string]any)
					item.Provenance = map[string]any{"response": prior, "sourceParentId": prior["sourceId"], "sourceBlockId": item.ExternalID, "sourceType": "legacy"}
					item.Outcome = "partial"
					item.Reason += "; legacy capture ownership recovered from persisted response identity; unavailable type metadata remains in raw evidence"
				}
				bySource[item.ExternalID] = item
				ordered = append(ordered, item)
			}
		}
		meta := func(item *ImportItem) map[string]any { v, _ := item.Provenance.(map[string]any); return v }
		kind := func(item *ImportItem) string { return str(meta(item)["sourceType"]) }
		children := map[string][]*ImportItem{}
		for _, item := range ordered {
			children[str(meta(item)["sourceParentId"])] = append(children[str(meta(item)["sourceParentId"])], item)
		}
		// Hash source identities, relationships and exact supplied block bytes, not
		// generated UUIDs, download file IDs or capture timestamps.
		hashes := []any{}
		for _, item := range ordered {
			hashes = append(hashes, map[string]any{"id": item.ExternalID, "parent": meta(item)["sourceParentId"], "hash": item.Hash})
		}
		page.Hash = importDigest(marshal(map[string]any{"sourceHash": page.Hash, "blocks": hashes}))
		for _, item := range ordered {
			if kind(item) == "column_list" {
				groups := []any{}
				for _, column := range children[item.ExternalID] {
					if kind(column) != "column" {
						item.Outcome = "partial"
						item.Reason += "; non-column child remains readable outside the group"
						continue
					}
					data, _ := meta(column)["sourceData"].(map[string]any)
					width := number(data["width_ratio"])
					if width <= 0 {
						width = 1
					}
					ids := []any{}
					for _, child := range children[column.ExternalID] {
						if len(child.Blocks) > 0 {
							ids = append(ids, child.Blocks[0].ID)
						}
					}
					groups = append(groups, map[string]any{"id": str(meta(column)["groupId"]), "widthRatio": width, "childBlockIds": ids})
				}
				if len(groups) == 0 {
					id := item.Blocks[0].ID
					item.Blocks = []ImportBlock{fallbackImportBlock(item, "Column children unavailable; layout source retained")}
					item.Blocks[0].ID = id
					item.Outcome = "partial"
					item.Reason += "; column groups unavailable"
				} else {
					item.Blocks[0].Data["columns"] = groups
				}
			}
		}
		// Reusable references are safe only when their original source is present
		// on this page and the reference cannot introduce an ownership cycle.
		for _, item := range ordered {
			if kind(item) != "synced_block" {
				continue
			}
			data, _ := meta(item)["sourceData"].(map[string]any)
			from, _ := data["synced_from"].(map[string]any)
			if from == nil {
				continue
			}
			source := bySource[notionSourceID(from["block_id"])]
			valid := source != nil && source != item && kind(source) == "synced_block" && len(source.Blocks) > 0
			if valid {
				sourceData, _ := meta(source)["sourceData"].(map[string]any)
				valid = sourceData["synced_from"] == nil
			}
			ancestors := map[string]bool{}
			for ancestor := str(meta(item)["sourceParentId"]); valid && bySource[ancestor] != nil; ancestor = str(meta(bySource[ancestor])["sourceParentId"]) {
				if bySource[ancestor] == source || ancestors[ancestor] || len(ancestors) > 64 {
					valid = false
				}
				ancestors[ancestor] = true
			}
			if valid {
				item.Blocks[0].Type = "synced_block"
				item.Blocks[0].Data = map[string]any{"contentSourceId": source.Blocks[0].ID}
			} else {
				item.Outcome = "partial"
				item.Reason += "; reusable source unavailable or cyclic; supplied reference retained without activation"
			}
		}
		for _, item := range ordered {
			if kind(item) == "column" {
				continue
			}
			parent := bySource[str(meta(item)["sourceParentId"])]
			if parent != nil && kind(parent) == "column" {
				parent = bySource[str(meta(parent)["sourceParentId"])]
			}
			parentID := ""
			if parent != nil && len(parent.Blocks) > 0 {
				parentID = parent.Blocks[0].ID
			}
			for _, block := range item.Blocks {
				page.Blocks = append(page.Blocks, block)
				page.BlockParents[block.ID] = parentID
				page.BlockSources[block.ID] = "notion-block:" + item.ExternalID
			}
			if item.Outcome != "adapted" && item.Outcome != "preserved" {
				page.Outcome = "partial"
			}
		}
		// Parent-first order permits ordinary guarded block insertion. Source
		// sibling order remains that of the paginated children responses.
		orderedBlocks := []ImportBlock{}
		pending := append([]ImportBlock{}, page.Blocks...)
		seen := map[string]bool{"": true}
		for len(pending) > 0 {
			next := []ImportBlock{}
			for _, block := range pending {
				if seen[page.BlockParents[block.ID]] {
					orderedBlocks = append(orderedBlocks, block)
					seen[block.ID] = true
				} else {
					next = append(next, block)
				}
			}
			if len(next) == len(pending) {
				page.Outcome = "partial"
				page.Reason += "; invalid source ownership retained without activation"
				break
			}
			pending = next
		}
		page.Blocks = orderedBlocks
		for _, item := range ordered {
			item.Blocks = nil
			item.Kind = "asset"
			item.PageID = ""
			item.ParentID = ""
		}
	}
}
