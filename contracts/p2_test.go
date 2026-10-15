package contracts

import (
	"encoding/json"
	"testing"
)

func TestP2StrictContracts(t *testing.T) {
	id := "019a1234-5678-7000-8000-000000000002"
	tests := []struct {
		name     string
		good     map[string]any
		badKey   string
		badValue any
	}{
		{"SetPageDisplay", map[string]any{"kind": "page.display.set", "pageId": id, "fullWidth": true, "presentation": false}, "fullWidth", "true"},
		{"AcceptPersonalReview", map[string]any{"kind": "review.accept", "pageId": id, "runId": id, "blockId": id, "approved": true}, "approved", false},
		{"ActivatePersonalReview", map[string]any{"kind": "review.activate", "pageId": id, "planBlockId": id, "enabled": true}, "actor", "owner"},
		{"RunRowButton", map[string]any{"kind": "database.rowButtonRun", "collectionId": id, "recordId": id, "buttonId": id, "confirmed": false}, "commands", []any{}},
		{"GraphQueryRequest", map[string]any{"contractVersion": 1, "requestId": id, "workspaceId": id, "limit": 100}, "limit", 101},
		{"OCRRunRequest", map[string]any{"contractVersion": 1, "requestId": id, "workspaceId": id, "fileId": id}, "path", "/private/file.png"},
		{"OCRCancelRequest", map[string]any{"contractVersion": 1, "requestId": id, "workspaceId": id, "runRequestId": id}, "runRequestId", ""},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			b, _ := json.Marshal(test.good)
			if err := Validate("service", test.name, b); err != nil {
				t.Fatal(err)
			}
			test.good[test.badKey] = test.badValue
			b, _ = json.Marshal(test.good)
			if Validate("service", test.name, b) == nil {
				t.Fatal("invalid request accepted")
			}
		})
	}
}
