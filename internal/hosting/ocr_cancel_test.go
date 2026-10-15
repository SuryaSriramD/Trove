package hosting

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

func TestHostOCRCancellationDuringRealHTTPRequest(t *testing.T) {
	h, server := newHost(t)
	snapshot := hostOK(t, hostCall(t, h, server, "workspace.snapshot", nil))
	page := workspace.NewID()
	hostBatch(t, h, server, snapshot["scopeGrantId"], []any{map[string]any{"kind": "page.create", "pageId": page, "title": "Hosted image"}}, pageGuard(page, "0"))
	var imageBytes bytes.Buffer
	if err := png.Encode(&imageBytes, image.NewRGBA(image.Rect(0, 0, 2, 2))); err != nil {
		t.Fatal(err)
	}
	upload := request(t, server, "/upload", ownerToken, server.URL, map[string]any{"name": "fixture.png", "data": base64.StdEncoding.EncodeToString(imageBytes.Bytes())})
	var selected map[string]any
	if err := json.NewDecoder(upload.Body).Decode(&selected); err != nil {
		t.Fatal(err)
	}
	prepared := hostOK(t, hostCall(t, h, server, "prepareFile", map[string]any{"selectionHandle": selected["selectionHandle"]}))
	guards := append(pageGuard(page, "1"), map[string]any{"target": map[string]any{"entityType": "file", "entityId": prepared["fileId"]}, "revision": "0"})
	hostBatch(t, h, server, snapshot["scopeGrantId"], []any{map[string]any{"kind": "file.attach", "pageId": page, "fileId": prepared["fileId"], "ingestHandle": prepared["ingestHandle"]}}, guards)
	entered := make(chan struct{})
	h.Kernel.SetOCRProvider(func(ctx context.Context, _ *os.File) (workspace.OCRResult, error) {
		close(entered)
		<-ctx.Done()
		return workspace.OCRResult{}, ctx.Err()
	})
	runID := workspace.NewID()
	out := make(chan map[string]any, 1)
	go func() {
		out <- hostCall(t, h, server, "ocr.run", map[string]any{"requestId": runID, "fileId": prepared["fileId"]})
	}()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("host OCR unavailable")
	}
	denied := request(t, server, "/api", "", "", map[string]any{"method": "ocr.cancel", "request": map[string]any{}})
	if denied.StatusCode != 401 {
		t.Fatal("unauthenticated cancellation admitted", denied.StatusCode)
	}
	hostOK(t, hostCall(t, h, server, "ocr.cancel", map[string]any{"runRequestId": runID}))
	select {
	case result := <-out:
		if result["error"].(map[string]any)["code"] != "CANCELLED" {
			t.Fatal(result)
		}
	case <-time.After(time.Second):
		t.Fatal("host cancellation blocked by OCR request")
	}
}
