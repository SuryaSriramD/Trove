package workspace

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"path"
	"sort"
	"strings"
)

const maxConversationNodes = 2000

type conversationNode struct {
	DerivedIdentity bool     `json:"derivedIdentity,omitempty"`
	ID              string   `json:"id"`
	Parent          string   `json:"parentId,omitempty"`
	Children        []string `json:"childrenIds"`
	MessageID       string   `json:"messageId,omitempty"`
	Role            string   `json:"role"`
	CreatedAt       any      `json:"createdAt,omitempty"`
	BlockID         string   `json:"blockId"`
	Text            string   `json:"-"`
}

// ParseConversationExport is a pure parser: it produces canonical import objects
// but never writes workspace state. Exact source bytes are retained by inventory.
// The deliberately bounded formats are ChatGPT mapping JSON and Claude
// chat_messages JSON (either one conversation or an array of conversations).
func ParseConversationExport(raw []byte, source ImportItem) ([]ImportItem, bool, error) {
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return nil, false, nil
	}
	entries, ok := value.([]any)
	if !ok {
		entries = []any{value}
	}
	if len(entries) == 0 {
		return nil, false, nil
	}
	first, _ := entries[0].(map[string]any)
	_, chatgpt := first["mapping"].(map[string]any)
	_, claude := first["chat_messages"].([]any)
	if !chatgpt && !claude {
		return nil, false, nil
	}
	if len(entries) > 256 {
		return nil, true, fmt.Errorf("conversation export exceeds 256 conversations; split the source")
	}
	items := []ImportItem{}
	seen := map[string]bool{}
	for index, entry := range entries {
		c, ok := entry.(map[string]any)
		if !ok {
			return nil, true, fmt.Errorf("conversation %d is not an object", index+1)
		}
		provider, id, title := "chatgpt", str(c["id"]), str(c["title"])
		nodes := []conversationNode{}
		if mapping, ok := c["mapping"].(map[string]any); ok {
			if id == "" {
				id = str(c["conversation_id"])
			}
			keys := make([]string, 0, len(mapping))
			for key := range mapping {
				keys = append(keys, key)
			}
			sort.Strings(keys)
			for _, key := range keys {
				v, ok := mapping[key].(map[string]any)
				if !ok {
					return nil, true, fmt.Errorf("invalid mapping node %s", key)
				}
				m, _ := v["message"].(map[string]any)
				author, _ := m["author"].(map[string]any)
				n := conversationNode{ID: key, Parent: str(v["parent"]), Role: str(author["role"]), MessageID: str(m["id"]), CreatedAt: m["create_time"], Children: []string{}}
				children, _ := v["children"].([]any)
				for _, child := range children {
					if s, ok := child.(string); ok {
						n.Children = append(n.Children, s)
					} else {
						return nil, true, fmt.Errorf("invalid child of %s", key)
					}
				}
				n.Text = conversationContent(m["content"])
				if n.Role == "" {
					n.Role = "structure"
				}
				nodes = append(nodes, n)
			}
		} else if messages, ok := c["chat_messages"].([]any); ok {
			provider, id, title = "claude", str(c["uuid"]), str(c["name"])
			previous := ""
			for i, rawMessage := range messages {
				m, ok := rawMessage.(map[string]any)
				if !ok {
					return nil, true, fmt.Errorf("invalid Claude message %d", i+1)
				}
				mid := str(m["uuid"])
				derivedIdentity := mid == ""
				if mid == "" {
					mid = fmt.Sprintf("message-%d", i+1)
				}
				parent := previous
				if val, exists := m["parent_message_uuid"]; exists {
					parent = str(val)
				}
				text := str(m["text"])
				if text == "" {
					text = conversationContent(m["content"])
				}
				nodes = append(nodes, conversationNode{ID: mid, Parent: parent, MessageID: mid, Role: str(m["sender"]), CreatedAt: m["created_at"], Children: []string{}, Text: text, DerivedIdentity: derivedIdentity})
				previous = mid
			}
		} else {
			return nil, true, fmt.Errorf("mixed or unsupported conversation format at item %d", index+1)
		}
		if len(nodes) > maxConversationNodes {
			return nil, true, fmt.Errorf("conversation exceeds %d nodes; source retained for splitting", maxConversationNodes)
		}
		identity := "external_id"
		if id == "" {
			id = fmt.Sprintf("%s#conversation=%d", source.Locator, index+1)
			identity = "derived_locator"
		}
		key := provider + ":" + id
		if seen[key] {
			return nil, true, fmt.Errorf("duplicate conversation identity %s", key)
		}
		seen[key] = true
		if title == "" {
			title = fmt.Sprintf("Conversation %d", index+1)
		}
		item := ImportItem{SourceSystem: provider, Locator: source.Locator + "#conversation=" + url.QueryEscape(key), Kind: "conversation", Outcome: "adapted", Reason: "Conversation roles, source IDs, parent/child links, timestamps and all branches preserved; original JSON retained", RawFileID: source.RawFileID, Hash: importDigest(marshal(c)), Size: int64(len(marshal(c))), PageID: NewID(), Title: title, IdentityKind: identity, ExternalID: key, BlockParents: map[string]string{}}
		byID := map[string]int{}
		for i := range nodes {
			if nodes[i].ID == "" {
				return nil, true, fmt.Errorf("empty conversation node ID")
			}
			if _, exists := byID[nodes[i].ID]; exists {
				return nil, true, fmt.Errorf("duplicate message identity %s", nodes[i].ID)
			}
			byID[nodes[i].ID] = i
			nodes[i].BlockID = NewID()
		}
		// Fill inferred linear Claude edges; ChatGPT child order stays explicit.
		if provider == "claude" {
			for _, n := range nodes {
				if i, exists := byID[n.Parent]; exists {
					nodes[i].Children = append(nodes[i].Children, n.ID)
				}
			}
		}
		order := []int{}
		visited := map[string]bool{}
		visiting := map[string]bool{}
		var visit func(int) error
		visit = func(i int) error {
			n := nodes[i]
			if visiting[n.ID] {
				return fmt.Errorf("cyclic conversation graph at %s", n.ID)
			}
			if visited[n.ID] {
				return nil
			}
			visiting[n.ID] = true
			visited[n.ID] = true
			order = append(order, i)
			for _, child := range n.Children {
				j, exists := byID[child]
				if !exists {
					return fmt.Errorf("missing child %s", child)
				}
				if nodes[j].Parent != n.ID {
					return fmt.Errorf("inconsistent conversation parent for %s", child)
				}
				if err := visit(j); err != nil {
					return err
				}
			}
			visiting[n.ID] = false
			return nil
		}
		for i, n := range nodes {
			if n.Parent == "" {
				if err := visit(i); err != nil {
					return nil, true, err
				}
			} else if _, exists := byID[n.Parent]; !exists {
				return nil, true, fmt.Errorf("missing parent %s", n.Parent)
			}
		}
		for i := range nodes {
			if err := visit(i); err != nil {
				return nil, true, err
			}
		}
		// Detect cycles even when a producer omitted child arrays.
		for _, n := range nodes {
			ancestry := map[string]bool{}
			for id := n.ID; id != ""; id = nodes[byID[id]].Parent {
				if ancestry[id] {
					return nil, true, fmt.Errorf("cyclic parent chain at %s", id)
				}
				ancestry[id] = true
			}
		}
		item.BlockSources = map[string]string{}
		item.Blocks = append(item.Blocks, ImportBlock{ID: NewID(), Type: "paragraph", Data: map[string]any{"richText": richImport(fmt.Sprintf("Imported %s conversation · source ID %s · %d nodes. Every branch is retained below; parent and reply links preserve navigation. Non-text payloads remain in the original JSON.", provider, id, len(nodes)))}})
		item.BlockSources[item.Blocks[len(item.Blocks)-1].ID] = "conversation-node:summary"
		for _, i := range order {
			n := nodes[i]
			identity := "conversation-node:" + n.ID
			if n.DerivedIdentity {
				// An anonymous array position only matches identical retained raw
				// evidence. Edited exports must not assign it an external identity.
				identity = "normalizer-position:" + item.Hash + ":" + identity
			}
			label := n.Role + " · " + n.ID
			if n.CreatedAt != nil {
				label += " · " + fmt.Sprint(n.CreatedAt)
			}
			if str(c["current_node"]) == n.ID {
				label += " · selected source branch"
			}
			item.Blocks = append(item.Blocks, ImportBlock{ID: n.BlockID, Type: "heading_3", Data: map[string]any{"richText": richImport(label)}})
			item.BlockSources[n.BlockID] = identity + ":heading"
			links := []any{}
			if p, exists := byID[n.Parent]; exists {
				links = append(links, map[string]any{"text": "Parent: " + n.Parent, "link": canonicalPageLink(item.PageID, nodes[p].BlockID)})
			}
			for _, child := range n.Children {
				if len(links) > 0 {
					links = append(links, map[string]any{"text": " · "})
				}
				links = append(links, map[string]any{"text": "Reply: " + child, "link": canonicalPageLink(item.PageID, nodes[byID[child]].BlockID)})
			}
			if len(links) > 0 {
				item.Blocks = append(item.Blocks, ImportBlock{ID: NewID(), Type: "paragraph", Data: map[string]any{"richText": links}})
				item.BlockSources[item.Blocks[len(item.Blocks)-1].ID] = identity + ":links"
			}
			text := n.Text
			if text == "" {
				text = "[No readable text in this node; exact source payload retained.]"
			}
			item.Blocks = append(item.Blocks, ImportBlock{ID: NewID(), Type: "paragraph", Data: map[string]any{"richText": richImport(text)}})
			item.BlockSources[item.Blocks[len(item.Blocks)-1].ID] = identity + ":text"
		}
		item.Provenance = map[string]any{"format": "conversation-export-v1", "provider": provider, "conversationId": id, "createdAt": c["create_time"], "updatedAt": c["update_time"], "currentNode": c["current_node"], "nodes": nodes, "sourceLocator": source.Locator, "rawFileId": source.RawFileID, "nonTextPolicy": "Exact original JSON retained; only explicit text is projected"}
		if provider == "claude" {
			item.Provenance.(map[string]any)["createdAt"] = c["created_at"]
			item.Provenance.(map[string]any)["updatedAt"] = c["updated_at"]
		}
		items = append(items, item)
	}
	return items, true, nil
}

// Only known text-bearing keys enter search/AI; arbitrary metadata is evidence.
func conversationContent(value any) string {
	switch v := value.(type) {
	case string:
		return v
	case []any:
		parts := []string{}
		for _, part := range v {
			if text := conversationContent(part); text != "" {
				parts = append(parts, text)
			}
		}
		return strings.Join(parts, "\n")
	case map[string]any:
		if text, ok := v["text"].(string); ok {
			return text
		}
		if parts, ok := v["parts"]; ok {
			return conversationContent(parts)
		}
	}
	return ""
}

func (k *Kernel) expandConversationImports(ctx context.Context, p *ImportPlan) error {
	items := []ImportItem{}
	for _, source := range p.Items {
		if source.Kind != "unsupported" || !strings.EqualFold(path.Ext(source.Locator), ".json") {
			items = append(items, source)
			continue
		}
		b, err := k.readImportBytes(ctx, source.RawFileID)
		if err != nil {
			return err
		}
		conversations, recognized, err := ParseConversationExport(b, source)
		if !recognized {
			items = append(items, source)
			continue
		}
		if err != nil {
			source.Outcome = "unsupported_but_retained"
			source.Reason = "Conversation export was not committed: " + err.Error() + ". Original bytes retained."
			items = append(items, source)
			continue
		}
		source.Kind = "asset"
		source.PageID = ""
		source.Outcome = "preserved"
		source.Reason = "Original conversation export retained as provenance evidence"
		items = append(items, source)
		items = append(items, conversations...)
		if len(items) > maxImportEntries {
			return Fail("RESOURCE_EXHAUSTED", "Expanded conversation import exceeds 10,000 items")
		}
		p.UnknownCompleteness = append(p.UnknownCompleteness, "Conversation export "+source.Locator+": only supplied nodes and text are projected. Missing attachments, provider settings and unsupplied conversations cannot be recovered; original JSON is retained.")
	}
	p.Items = items
	return nil
}

func importItemSourceSystem(p *ImportPlan, item *ImportItem) string {
	if item.SourceSystem != "" {
		return item.SourceSystem
	}
	return p.SourceSystem
}
