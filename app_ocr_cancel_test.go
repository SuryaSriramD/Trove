package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"image"
	"image/png"
	"os"
	"testing"
	"time"

	"trove.local/phase0/internal/workspace"
)

func TestNativeOCRCancelsWhileServiceReadIsActive(t *testing.T) {
	k, err := workspace.Create(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer k.Close()
	a := NewApp()
	a.ctx = context.Background()
	a.kernel = k
	a.execution = k.OwnerContext()
	a.completeStartup(nil)
	call := func(method string, fields map[string]any) map[string]any {
		req := map[string]any{"contractVersion": 1, "requestId": workspace.NewID(), "workspaceId": k.ID()}
		for key, value := range fields {
			req[key] = value
		}
		raw, _ := json.Marshal(req)
		var out map[string]any
		if err := json.Unmarshal([]byte(a.Dispatch(method, string(raw))), &out); err != nil {
			t.Fatal(err)
		}
		return out
	}
	ok := func(out map[string]any) map[string]any {
		if out["ok"] != true {
			t.Fatal(out)
		}
		r, _ := out["result"].(map[string]any)
		return r
	}
	page := workspace.NewID()
	guard := func(kind, id, rev string) any {
		return map[string]any{"target": map[string]any{"entityType": kind, "entityId": id}, "revision": rev}
	}
	batch := func(commands, guards []any) {
		ok(call("batch", map[string]any{"commands": commands, "mutation": map[string]any{"scopeGrantId": a.execution.ScopeGrantId, "idempotencyKey": workspace.NewID(), "expectedRevisions": guards}}))
	}
	batch([]any{map[string]any{"kind": "page.create", "pageId": page, "title": "Native OCR"}}, []any{guard("page", page, "0")})
	var b bytes.Buffer
	if err := png.Encode(&b, image.NewRGBA(image.Rect(0, 0, 2, 2))); err != nil {
		t.Fatal(err)
	}
	handle, err := a.StageUpload("image.png", "image/png", base64.StdEncoding.EncodeToString(b.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	prepared := ok(call("prepareFile", map[string]any{"selectionHandle": handle}))
	file := prepared["fileId"].(string)
	batch([]any{map[string]any{"kind": "file.attach", "pageId": page, "fileId": file, "ingestHandle": prepared["ingestHandle"]}}, []any{guard("page", page, "1"), guard("file", file, "0")})
	for _, lifecycle := range []bool{false, true} {
		entered := make(chan struct{})
		k.SetOCRProvider(func(ctx context.Context, _ *os.File) (workspace.OCRResult, error) {
			close(entered)
			<-ctx.Done()
			return workspace.OCRResult{}, ctx.Err()
		})
		runID := workspace.NewID()
		out := make(chan map[string]any, 1)
		go func() { out <- call("ocr.run", map[string]any{"requestId": runID, "fileId": file}) }()
		select {
		case <-entered:
		case <-time.After(time.Second):
			t.Fatal("native OCR not admitted")
		}
		if lifecycle {
			resume := a.cancelOCRForLifecycle()
			defer resume()
		} else {
			ok(call("ocr.cancel", map[string]any{"runRequestId": runID}))
		}
		select {
		case result := <-out:
			if result["error"].(map[string]any)["code"] != "CANCELLED" {
				t.Fatal(result)
			}
		case <-time.After(time.Second):
			t.Fatal("native OCR cancellation blocked")
		}
	}
}
