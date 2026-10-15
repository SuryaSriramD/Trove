package workspace

import (
	"encoding/json"
	"sort"
	"strings"
)

// D002 treats a sibling reorder as one list edit. Stable-membership lists can
// be derived from every known snapshot without changing the stored normalizer
// or fabricating identity for inserted, omitted or moved source blocks.
func mergeImportContentFields(base, local, incoming map[string]json.RawMessage, known bool) []ImportFieldDecision {
	if !known {
		return mergeImportFields(base, local, incoming, false)
	}
	base, local, incoming = copyImportFields(base), copyImportFields(local), copyImportFields(incoming)
	b, l, r := importSiblingLists(base), importSiblingLists(local), importSiblingLists(incoming)
	for parent, order := range b {
		if len(order) < 2 || !sameImportMembers(order, l[parent]) || !sameImportMembers(order, r[parent]) {
			continue
		}
		path := "/siblingOrder/" + parent
		if parent == "" {
			path = "/siblingOrder/root"
		}
		for _, side := range []struct {
			fields map[string]json.RawMessage
			order  []string
		}{{base, order}, {local, l[parent]}, {incoming, r[parent]}} {
			side.fields[path] = marshal(side.order)
			for _, id := range order {
				delete(side.fields, "/blocks/"+id+"/order")
			}
		}
	}
	return mergeImportFields(base, local, incoming, true)
}
func copyImportFields(fields map[string]json.RawMessage) map[string]json.RawMessage {
	out := make(map[string]json.RawMessage, len(fields))
	for k, v := range fields {
		out[k] = v
	}
	return out
}
func importSiblingLists(fields map[string]json.RawMessage) map[string][]string {
	type positioned struct {
		id       string
		position int
	}
	groups := map[string][]positioned{}
	invalid := map[string]bool{}
	for path := range fields {
		if !strings.HasPrefix(path, "/blocks/") || !strings.HasSuffix(path, "/content") {
			continue
		}
		parts := strings.Split(path, "/")
		if len(parts) != 4 {
			continue
		}
		id := parts[2]
		var parent *string
		var position int
		if json.Unmarshal(fields["/blocks/"+id+"/parent"], &parent) != nil || json.Unmarshal(fields["/blocks/"+id+"/order"], &position) != nil || position < 0 {
			continue
		}
		key := ""
		if parent != nil {
			key = *parent
		}
		groups[key] = append(groups[key], positioned{id, position})
	}
	out := map[string][]string{}
	for parent, items := range groups {
		sort.Slice(items, func(i, j int) bool { return items[i].position < items[j].position })
		for i, item := range items {
			if i > 0 && items[i-1].position == item.position {
				invalid[parent] = true
			}
			out[parent] = append(out[parent], item.id)
		}
		if invalid[parent] {
			delete(out, parent)
		}
	}
	return out
}
func sameImportMembers(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	members := map[string]bool{}
	for _, id := range a {
		members[id] = true
	}
	for _, id := range b {
		if !members[id] {
			return false
		}
		delete(members, id)
	}
	return len(members) == 0
}

func expandImportSiblingOrders(fields map[string]json.RawMessage, review *ImportMergeReview) error {
	for _, field := range review.Fields {
		if !strings.HasPrefix(field.Path, "/siblingOrder/") {
			continue
		}
		var expected, selected []string
		if json.Unmarshal(field.Base.Value, &expected) != nil || json.Unmarshal(fields[field.Path], &selected) != nil || !sameImportMembers(expected, selected) {
			return Fail("CONFLICT", "A reviewed sibling order must contain every affected block exactly once")
		}
		parent := strings.TrimPrefix(field.Path, "/siblingOrder/")
		if parent == "root" {
			parent = ""
		}
		for index, id := range selected {
			if _, ok := fields["/blocks/"+id+"/content"]; !ok {
				return Fail("CONFLICT", "Sibling order references a removed block; review its complete placement")
			}
			var actual *string
			if json.Unmarshal(fields["/blocks/"+id+"/parent"], &actual) != nil {
				return Fail("CONFLICT", "Sibling order requires a valid block parent")
			}
			actualID := ""
			if actual != nil {
				actualID = *actual
			}
			if actualID != parent {
				return Fail("CONFLICT", "Sibling order cannot move blocks to another parent")
			}
			if _, exists := fields["/blocks/"+id+"/order"]; exists {
				return Fail("CONFLICT", "A block order cannot be resolved both individually and as a list")
			}
			fields["/blocks/"+id+"/order"] = marshal(index)
		}
		delete(fields, field.Path)
	}
	return nil
}
