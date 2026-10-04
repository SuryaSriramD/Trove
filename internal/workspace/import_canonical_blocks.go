package workspace

import (
	"encoding/json"
	"strings"
	"time"

	"trove.local/phase0/contracts"
)

// Only validated inert canonical blocks are reconstructed from manifests.
// Action/database/task definitions require separate review and stay evidence.
func (k *Kernel) restorePortableBlocks(p *ImportPlan, manifest portableHierarchy) {
	pages, files, blocks, blockPages := map[string]string{}, map[string]string{}, map[string]string{}, map[string]string{}
	blockCounts := map[string]int{}
	items := map[string]int{}
	for i, item := range p.Items {
		items[item.Locator] = i
	}
	for _, page := range manifest.Pages {
		if i, ok := items[page.Path]; ok && p.Items[i].PageID != "" {
			pages[string(page.CanonicalPage.Id)] = p.Items[i].PageID
			for _, b := range page.CanonicalBlocks {
				blockCounts[string(b.Id)]++
				blocks[string(b.Id)] = NewID()
				blockPages[string(b.Id)] = string(b.PageId)
			}
		}
	}
	for _, file := range manifest.Attachments {
		if i, ok := items[file.Path]; ok && p.Items[i].Kind == "asset" {
			files[file.ID] = p.Items[i].RawFileID
		}
	}
	safe := map[string]bool{}
	for _, kind := range []string{"paragraph", "heading_1", "heading_2", "heading_3", "heading_4", "quote", "bulleted_list_item", "numbered_list_item", "callout", "toggle", "toggle_heading", "divider", "code", "mermaid", "equation", "table", "image", "audio", "video", "pdf", "file", "bookmark", "embed", "columns", "synced_block", "breadcrumb", "table_of_contents", "page_mention", "date_mention", "tab_container", "unsupported_external"} {
		safe[kind] = true
	}
	for _, page := range manifest.Pages {
		index, ok := items[page.Path]
		if !ok || len(page.CanonicalBlocks) == 0 {
			continue
		}
		item := &p.Items[index]
		if item.Kind != "markdown" && item.Kind != "html" {
			continue
		}
		restored := []ImportBlock{}
		parents := map[string]string{}
		seen := map[string]bool{}
		valid := true
		for _, b := range page.CanonicalBlocks {
			if contracts.Validate("domain", "Block", marshal(b)) != nil || string(b.PageId) != string(page.CanonicalPage.Id) || seen[string(b.Id)] || blockCounts[string(b.Id)] != 1 {
				valid = false
				break
			}
			seen[string(b.Id)] = true
		}
		if valid && !validPortableGraph(page.CanonicalBlocks) {
			valid = false
		}
		if !valid {
			item.Outcome = "partial"
			item.Reason += "; invalid canonical manifest block evidence retained without applying it"
			continue
		}
		pending := append([]contracts.DomainBlock{}, page.CanonicalBlocks...)
		for len(pending) > 0 {
			next := []contracts.DomainBlock{}
			made := false
			for _, b := range pending {
				parent := ""
				if b.ParentBlockId != nil {
					parent = string(*b.ParentBlockId)
					if !seen[parent] {
						valid = false
						break
					}
					if _, inserted := parents[blocks[parent]]; !inserted {
						next = append(next, b)
						continue
					}
				}
				var data map[string]any
				if json.Unmarshal(b.Data, &data) != nil {
					valid = false
					break
				}
				kind := b.Type
				unsupported := !safe[kind] || b.SchemaVersion != 1
				if kind == "synced_block" && blockPages[str(data["contentSourceId"])] != string(b.PageId) {
					unsupported = true
				}
				if !rewritePortableData(data, pages, files, blocks) {
					unsupported = true
				}
				if unsupported {
					fallback := fallbackImportBlock(item, "Preserved canonical "+b.Type+" (requires separate reference/action review):\n"+string(b.Data))
					fallback.ID = blocks[string(b.Id)]
					restored = append(restored, fallback)
					item.Outcome = "partial"
					item.Reason += "; " + b.Type + " definition retained as inert evidence"
				} else {
					restored = append(restored, ImportBlock{blocks[string(b.Id)], kind, data})
				}
				parents[blocks[string(b.Id)]] = blocks[parent]
				made = true
			}
			if !valid || !made {
				valid = false
				break
			}
			pending = next
		}
		if !valid {
			item.Outcome = "partial"
			item.Reason += "; cyclic/missing canonical parents retained without applying"
			continue
		}
		item.Blocks = restored
		item.BlockParents = parents
		item.BlockSources = map[string]string{}
		for _, b := range page.CanonicalBlocks {
			item.BlockSources[blocks[string(b.Id)]] = "portable-block:" + string(b.Id)
		}
		item.Reason += "; validated canonical blocks restored with workspace ID remapping"
	}
}
func rewritePortableData(data map[string]any, pages, files, blocks map[string]string) bool {
	valid := true
	var walk func(any)
	walk = func(value any) {
		switch v := value.(type) {
		case map[string]any:
			for key, child := range v {
				var table map[string]string
				switch key {
				case "fileId", "rawFileId":
					table = files
				case "contentSourceId":
					table = blocks
				case "targetPageId":
					table = pages
				}
				if table != nil {
					mapped := table[str(child)]
					if mapped == "" {
						valid = false
					} else {
						v[key] = mapped
					}
					continue
				}
				if key == "link" && strings.HasPrefix(str(child), "trove://page/") {
					pageID, blockID, ok := parseCanonicalPageLink(str(child))
					mapped := pages[pageID]
					if !ok || mapped == "" {
						valid = false
					} else {
						if blockID != "" {
							if next := blocks[blockID]; next != "" {
								blockID = next
							}
						}
						v[key] = canonicalPageLink(mapped, blockID)
					}
					continue
				}
				if key == "childBlockIds" {
					list, _ := child.([]any)
					for i, id := range list {
						mapped := blocks[str(id)]
						if mapped == "" {
							valid = false
						} else {
							list[i] = mapped
						}
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
	walk(data)
	return valid
}

func validPortableGraph(source []contracts.DomainBlock) bool {
	if len(source) > 10000 {
		return false
	}
	byID := map[string]contracts.DomainBlock{}
	edges := map[string][]string{}
	for _, b := range source {
		byID[string(b.Id)] = b
		if b.ParentBlockId != nil {
			edges[string(*b.ParentBlockId)] = append(edges[string(*b.ParentBlockId)], string(b.Id))
		}
	}
	for _, b := range source {
		var d map[string]any
		if json.Unmarshal(b.Data, &d) != nil {
			return false
		}
		if b.ParentBlockId != nil {
			if _, ok := byID[string(*b.ParentBlockId)]; !ok {
				return false
			}
		}
		if b.Type == "table" && validateAdvancedTable(d) != nil {
			return false
		}
		if b.Type == "date_mention" {
			if _, err := time.LoadLocation(str(d["timezone"])); err != nil {
				return false
			}
		}
		if b.Type == "synced_block" {
			if _, ok := byID[str(d["contentSourceId"])]; ok {
				edges[string(b.Id)] = append(edges[string(b.Id)], str(d["contentSourceId"]))
			}
		}
		for _, key := range []string{"tabs", "columns"} {
			groups, _ := d[key].([]any)
			seen := map[string]bool{}
			for _, raw := range groups {
				group, _ := raw.(map[string]any)
				children, _ := group["childBlockIds"].([]any)
				for _, rawID := range children {
					id := str(rawID)
					target, ok := byID[id]
					if !ok || seen[id] || target.ParentBlockId == nil || string(*target.ParentBlockId) != string(b.Id) {
						return false
					}
					seen[id] = true
					edges[string(b.Id)] = append(edges[string(b.Id)], id)
				}
			}
		}
	}
	colors := map[string]int{}
	var visit func(string, int) bool
	visit = func(id string, depth int) bool {
		if depth > 256 || colors[id] == 1 {
			return false
		}
		if colors[id] == 2 {
			return true
		}
		colors[id] = 1
		for _, next := range edges[id] {
			if !visit(next, depth+1) {
				return false
			}
		}
		colors[id] = 2
		return true
	}
	for id := range byID {
		if !visit(id, 0) {
			return false
		}
	}
	return true
}
