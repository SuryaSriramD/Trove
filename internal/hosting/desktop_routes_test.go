package hosting

import (
	"testing"
	"trove.local/phase0/internal/workspace"
)

func TestHostEditorRecordAutomationAndSearchRecovery(t *testing.T) {
	h, server := newHost(t)
	snapshot := hostOK(t, hostCall(t, h, server, "workspace.snapshot", nil))
	page, block, collection, title, record, recordPage := workspace.NewID(), workspace.NewID(), workspace.NewID(), workspace.NewID(), workspace.NewID(), workspace.NewID()
	guards := []any{}
	for _, pair := range [][2]string{{"page", page}, {"block", block}, {"collection", collection}, {"property", title}, {"record", record}, {"page", recordPage}} {
		guards = append(guards, map[string]any{"target": map[string]any{"entityType": pair[0], "entityId": pair[1]}, "revision": "0"})
	}
	hostBatch(t, h, server, snapshot["scopeGrantId"], []any{
		map[string]any{"kind": "page.create", "pageId": page, "title": "Hosted durable source"},
		map[string]any{"kind": "block.insert", "block": map[string]any{"blockId": block, "pageId": page, "type": "paragraph", "schemaVersion": 1, "data": map[string]any{"richText": []any{map[string]any{"text": "Indexed recovery evidence"}}}}},
		map[string]any{"kind": "collection.create", "collectionId": collection, "owningPageId": page, "titlePropertyId": title, "title": "Hosted rows"},
		map[string]any{"kind": "record.create", "recordId": record, "pageId": recordPage, "collectionId": collection, "title": "Hosted row"},
	}, guards)
	t.Run("projected_block", func(t *testing.T) {
		result := hostOK(t, hostCall(t, h, server, "readBlock", map[string]any{"blockId": block}))
		if result["block"].(map[string]any)["id"] != block {
			t.Fatal(result)
		}
	})
	t.Run("record_layout", func(t *testing.T) {
		result := hostOK(t, hostCall(t, h, server, "readRecord", map[string]any{"recordId": record}))
		if result["record"].(map[string]any)["pageId"] != recordPage {
			t.Fatal(result)
		}
		layout := hostOK(t, hostCall(t, h, server, "recordLayouts.get", map[string]any{"collectionId": collection, "recordId": record}))
		if layout["effectiveLayout"] == nil {
			t.Fatal(layout)
		}
	})
	t.Run("automation_choices", func(t *testing.T) {
		result := hostOK(t, hostCall(t, h, server, "automationOptions", map[string]any{"pageId": page}))
		if result["templates"] == nil || result["events"] == nil {
			t.Fatal(result)
		}
		hostOK(t, hostCall(t, h, server, "automationHistory", map[string]any{"pageId": page}))
	})
	t.Run("keyword_recovery", func(t *testing.T) {
		hostOK(t, hostCall(t, h, server, "search.rebuild", nil))
		result := hostOK(t, hostCall(t, h, server, "search.query", map[string]any{"query": "Indexed recovery evidence", "mode": "keyword", "limit": 10, "kinds": []any{}}))
		if len(result["hits"].([]any)) == 0 {
			t.Fatal("rebuilt source not searchable", result)
		}
	})
}
