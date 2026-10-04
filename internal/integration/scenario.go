// Package integration exercises the same services used by the packaged desktop.
// It is invoked only by explicit development qualification environment variables.
package integration

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"trove.local/phase0/contracts"
	"trove.local/phase0/internal/workspace"
)

type State struct {
	WorkspaceID  string `json:"workspaceId"`
	PageID       string `json:"pageId"`
	BlockID      string `json:"blockId"`
	CollectionID string `json:"collectionId"`
	PropertyID   string `json:"propertyId"`
	RecordID     string `json:"recordId"`
	RecordPageID string `json:"recordPageId"`
	FileID       string `json:"fileId"`
	FileBlockID  string `json:"fileBlockId"`
	ContentHash  string `json:"contentHash"`
}
type Result struct {
	Step       string   `json:"step"`
	Passed     bool     `json:"passed"`
	RecordedAt string   `json:"recordedAt"`
	Error      string   `json:"error,omitempty"`
	State      State    `json:"state"`
	Checks     []string `json:"checks"`
}

func dispatch(ctx context.Context, k *workspace.Kernel, e contracts.ServiceExecutionContext, method string, payload map[string]any) (map[string]any, error) {
	payload["contractVersion"] = 1
	payload["workspaceId"] = k.ID()
	payload["requestId"] = workspace.NewID()
	raw, _ := json.Marshal(payload)
	out := k.Dispatch(ctx, e, method, raw)
	var envelope struct {
		OK     bool           `json:"ok"`
		Result map[string]any `json:"result"`
		Error  any            `json:"error"`
	}
	if err := json.Unmarshal(out, &envelope); err != nil {
		return nil, err
	}
	if !envelope.OK {
		return nil, fmt.Errorf("%s: %s", method, out)
	}
	return envelope.Result, nil
}
func snapshot(ctx context.Context, k *workspace.Kernel, e contracts.ServiceExecutionContext) (map[string]any, error) {
	return dispatch(ctx, k, e, "workspace.snapshot", map[string]any{})
}
func batch(ctx context.Context, k *workspace.Kernel, e contracts.ServiceExecutionContext, commands []map[string]any, newEntities map[string]string) error {
	snap, err := snapshot(ctx, k, e)
	if err != nil {
		return err
	}
	guards := []any{}
	seen := map[string]bool{}
	kinds := map[string]string{"pages": "page", "blocks": "block", "collections": "collection", "properties": "property", "records": "record", "views": "view", "containers": "container", "files": "file", "meetings": "meeting", "memories": "memory"}
	for list, kind := range kinds {
		items, _ := snap[list].([]any)
		for _, item := range items {
			obj := item.(map[string]any)
			id, _ := obj["id"].(string)
			revision, _ := obj["revision"].(string)
			if id == "" || revision == "" {
				continue
			}
			guards = append(guards, map[string]any{"target": map[string]any{"entityType": kind, "entityId": id}, "revision": revision})
			seen[kind+":"+id] = true
		}
	}
	for id, kind := range newEntities {
		if !seen[kind+":"+id] {
			guards = append(guards, map[string]any{"target": map[string]any{"entityType": kind, "entityId": id}, "revision": "0"})
		}
	}
	_, err = dispatch(ctx, k, e, "batch", map[string]any{"mutation": map[string]any{"scopeGrantId": e.ScopeGrantId, "idempotencyKey": "qualification-" + workspace.NewID(), "expectedRevisions": guards}, "commands": commands})
	return err
}
func Run(ctx context.Context, k *workspace.Kernel, e contracts.ServiceExecutionContext, step, statePath string) Result {
	result := Result{Step: step, RecordedAt: time.Now().UTC().Format(time.RFC3339Nano), Checks: []string{}}
	state := State{}
	if step == "create" {
		if _, err := os.Stat(statePath); err == nil {
			result.Error = "refusing to overwrite existing integration fixture"
			return result
		}
		state = State{WorkspaceID: k.ID(), PageID: workspace.NewID(), BlockID: workspace.NewID(), CollectionID: workspace.NewID(), PropertyID: workspace.NewID(), RecordID: workspace.NewID(), RecordPageID: workspace.NewID(), FileBlockID: workspace.NewID()}
		err := batch(ctx, k, e, []map[string]any{{"kind": "page.create", "pageId": state.PageID, "title": "Phase 1 integration"}}, map[string]string{state.PageID: "page"})
		if err == nil {
			err = batch(ctx, k, e, []map[string]any{{"kind": "block.insert", "block": map[string]any{"blockId": state.BlockID, "pageId": state.PageID, "type": "paragraph", "schemaVersion": 1, "data": map[string]any{"richText": []any{map[string]any{"text": "Persistent notes — తెలుగు 中文 العربية 🌱"}}}}}}, map[string]string{state.BlockID: "block"})
		}
		if err == nil {
			err = batch(ctx, k, e, []map[string]any{{"kind": "collection.create", "collectionId": state.CollectionID, "owningPageId": state.PageID, "titlePropertyId": state.PropertyID, "title": "Integration records"}}, map[string]string{state.CollectionID: "collection", state.PropertyID: "property"})
		}
		if err == nil {
			err = batch(ctx, k, e, []map[string]any{{"kind": "record.create", "recordId": state.RecordID, "collectionId": state.CollectionID, "pageId": state.RecordPageID, "title": "Record with a page body"}}, map[string]string{state.RecordID: "record", state.RecordPageID: "page"})
		}
		var temp *os.File
		if err == nil {
			temp, err = os.CreateTemp("", "trove-integration-file-*.txt")
		}
		const content = "Trove Phase 1 durable attachment\nతెలుగు · 中文 · العربية · 🌱\n"
		if err == nil {
			defer os.Remove(temp.Name())
			_, err = temp.WriteString(content)
			temp.Close()
		}
		var selected string
		if err == nil {
			selected, err = k.RegisterSelection(temp.Name())
		}
		var staged map[string]any
		if err == nil {
			staged, err = dispatch(ctx, k, e, "prepareFile", map[string]any{"selectionHandle": selected})
		}
		if err == nil {
			state.FileID, _ = staged["fileId"].(string)
			state.ContentHash, _ = staged["contentHash"].(string)
			if state.FileID == "" {
				err = fmt.Errorf("PrepareFile did not return canonical fileId")
			}
		}
		if err == nil {
			err = batch(ctx, k, e, []map[string]any{{"kind": "file.attach", "fileId": state.FileID, "ingestHandle": staged["ingestHandle"], "pageId": state.PageID, "blockId": state.FileBlockID}, {"kind": "block.insert", "block": map[string]any{"blockId": state.FileBlockID, "pageId": state.PageID, "type": "file", "schemaVersion": 1, "data": map[string]any{"fileId": state.FileID, "caption": []any{map[string]any{"text": "Durable attachment"}}}}}}, map[string]string{state.FileID: "file", state.FileBlockID: "block"})
		}
		if err != nil {
			result.Error = err.Error()
			result.State = state
			return result
		}
		raw, _ := json.MarshalIndent(state, "", "  ")
		if err = os.WriteFile(statePath, raw, 0600); err != nil {
			result.Error = err.Error()
			return result
		}
	} else if step == "verify" {
		raw, err := os.ReadFile(statePath)
		if err != nil {
			result.Error = err.Error()
			return result
		}
		if err = json.Unmarshal(raw, &state); err != nil {
			result.Error = err.Error()
			return result
		}
	} else {
		result.Error = "unknown integration step"
		return result
	}
	result.State = state
	if state.WorkspaceID != k.ID() {
		result.Error = "workspace identity changed across restart"
		return result
	}
	snap, err := snapshot(ctx, k, e)
	if err != nil {
		result.Error = err.Error()
		return result
	}
	expected := map[string][]string{"pages": {state.PageID, state.RecordPageID}, "blocks": {state.BlockID, state.FileBlockID}, "collections": {state.CollectionID}, "records": {state.RecordID}, "files": {state.FileID}}
	for list, ids := range expected {
		items, _ := snap[list].([]any)
		for _, id := range ids {
			found := false
			for _, item := range items {
				obj := item.(map[string]any)
				if obj["id"] == id {
					found = true
					if list == "records" && obj["pageId"] != state.RecordPageID {
						result.Error = "record body identity changed"
						return result
					}
				}
			}
			if !found {
				result.Error = "missing " + list + " entity " + id
				return result
			}
		}
		result.Checks = append(result.Checks, list+" persisted")
	}
	for _, item := range snap["pages"].([]any) {
		p := item.(map[string]any)
		if (p["id"] == state.PageID && p["title"] != "Phase 1 integration") || (p["id"] == state.RecordPageID && p["title"] != "Record with a page body") {
			result.Error = "page title changed across persistence"
			return result
		}
	}
	for _, item := range snap["blocks"].([]any) {
		b := item.(map[string]any)
		d, _ := b["data"].(map[string]any)
		if b["id"] == state.BlockID {
			rich, _ := d["richText"].([]any)
			if len(rich) != 1 {
				result.Error = "rich text structure changed"
				return result
			}
			r, _ := rich[0].(map[string]any)
			if b["type"] != "paragraph" || r["text"] != "Persistent notes — తెలుగు 中文 العربية 🌱" {
				result.Error = "Unicode content changed"
				return result
			}
		}
		if b["id"] == state.FileBlockID && d["fileId"] != state.FileID {
			result.Error = "attachment reference changed"
			return result
		}
	}
	result.Checks = append(result.Checks, "page titles, Unicode rich text and attachment references unchanged")
	f, meta, err := k.ReadFile(ctx, e, state.FileID)
	if err != nil {
		result.Error = err.Error()
		return result
	}
	defer f.Close()
	h := sha256.New()
	if _, err = io.Copy(h, f); err != nil {
		result.Error = err.Error()
		return result
	}
	if hex.EncodeToString(h.Sum(nil)) != state.ContentHash || string(meta.ContentHash) != state.ContentHash {
		result.Error = "attachment bytes changed"
		return result
	}
	result.Checks = append(result.Checks, "record has canonical page body", "attachment hash and scoped file read verified")
	result.Passed = true
	return result
}
func WriteResult(path string, result Result) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, raw, 0600)
}
