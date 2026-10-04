package workspace

import (
	"bytes"
	"context"
	"encoding/base64"
	"image"
	"image/png"
	"strings"
	"testing"
	"trove.local/phase0/internal/clipper"
)

const clipExtension = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

func clipRequest() clipper.Capture {
	return clipper.Capture{Version: 1, RequestID: NewID(), Title: "Offline browser clip", URL: "https://example.test/article", HTML: `<article><h2>Retained heading</h2><p><strong>Offline rich text</strong></p><script>stolen()</script><iframe src="https://evil.test"></iframe><img src="https://example.test/picture.png" alt="Acquired picture"></article>`, Mode: "article", DestinationID: "inbox", DestinationRevision: "0", CapturedAt: "2026-10-04T10:00:00Z"}
}
func TestClipperCaptureReviewImagesDestinationAndRestart(t *testing.T) {
	k, e := testKernel(t)
	ctx := context.Background()
	parent := createPage(t, k, e, "Approved destination")
	request := clipRequest()
	request.DestinationID = parent
	request.DestinationRevision = "1"
	request.ResourceConsent = true
	var bytes bytes.Buffer
	png.Encode(&bytes, image.NewRGBA(image.Rect(0, 0, 20, 20)))
	request.Images = []clipper.Image{{URL: "https://example.test/picture.png", MediaType: "image/png", Base64: base64.StdEncoding.EncodeToString(bytes.Bytes())}}
	plan, err := k.PreviewClipperCapture(ctx, e, clipExtension, request)
	if err != nil {
		t.Fatal(err)
	}
	var pages int
	k.db.QueryRow("SELECT count(*) FROM pages").Scan(&pages)
	if pages != 1 || plan.Initialized || plan.RootParentID != parent {
		t.Fatal("browser bypassed review", pages, plan)
	}
	duplicate, err := k.PreviewClipperCapture(ctx, e, clipExtension, request)
	if err != nil || duplicate.ID != plan.ID {
		t.Fatal("capture retry duplicated plan", err)
	}
	var imageID string
	for _, item := range plan.Items {
		if item.Kind == "html" {
			raw := string(marshal(item.Blocks))
			if strings.Contains(raw, "stolen()") || strings.Contains(raw, "evil.test") {
				t.Fatal("active source leaked into blocks", raw)
			}
			for _, block := range item.Blocks {
				if block.Type == "image" {
					imageID = str(block.Data["fileId"])
				}
			}
			if !strings.Contains(string(marshal(item.Provenance)), "explicit_browser_fetch_without_credentials") {
				t.Fatal("image provenance missing")
			}
		}
	}
	if imageID == "" {
		t.Fatal("acquired image was not adapted", string(marshal(plan)))
	}
	testOK(t, testDispatch(t, k, e, "batch", testRequest(k, e, []any{map[string]any{"kind": "page.update", "pageId": parent, "title": "Renamed destination"}}, required("page", parent, "1"))))
	if _, err = commitPortableImport(ctx, k, e, map[string]any{"planId": plan.ID, "planHash": plan.Hash}); err == nil {
		t.Fatal("stale destination committed")
	}
	plan, err = k.RefreshClipperDestination(ctx, e, plan.ID, plan.Hash)
	if err != nil || plan.ExpectedParentRevision != "2" {
		t.Fatal(err)
	}
	plan = completeImportFixture(t, k, e, plan)
	var actualParent string
	if err = k.db.QueryRow("SELECT parent_page_id FROM pages WHERE id=?", plan.RootID).Scan(&actualParent); err != nil || actualParent != parent {
		t.Fatal("wrong destination", actualParent, err)
	}
	file, _, err := k.ReadFile(ctx, e, imageID)
	if err != nil {
		t.Fatal(err)
	}
	file.Close()
	path := k.path
	k.Close()
	reopened, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	plans, err := reopened.ClipperReviewPlans(ctx, reopened.OwnerContext())
	if err != nil || len(plans) != 1 || plans[0].State != "complete" {
		t.Fatal("capture/review lost on restart", err)
	}
}
func TestClipperCaptureRejectsUnconsentedExecutableAndChangedPayload(t *testing.T) {
	k, e := testKernel(t)
	ctx := context.Background()
	request := clipRequest()
	request.Images = []clipper.Image{{URL: "https://example.test/picture.svg", MediaType: "image/svg+xml", Base64: base64.StdEncoding.EncodeToString([]byte(`<svg onload="evil()"/>`))}}
	if _, err := k.PreviewClipperCapture(ctx, e, clipExtension, request); err == nil {
		t.Fatal("unconsented image accepted")
	}
	request.ResourceConsent = true
	if _, err := k.PreviewClipperCapture(ctx, e, clipExtension, request); err == nil {
		t.Fatal("executable image accepted")
	}
	request.Images = nil
	plan, err := k.PreviewClipperCapture(ctx, e, clipExtension, request)
	if err != nil {
		t.Fatal(err)
	}
	request.Text = "changed identity"
	if _, err = k.PreviewClipperCapture(ctx, e, clipExtension, request); err == nil {
		t.Fatal("changed idempotency payload accepted")
	}
	bad := e
	bad.Origin = "ai"
	if _, err = k.RefreshClipperDestination(ctx, bad, plan.ID, plan.Hash); err == nil {
		t.Fatal("owner review bypass")
	}
	request.RequestID = NewID()
	request.DestinationID = NewID()
	if _, err = k.PreviewClipperCapture(ctx, e, clipExtension, request); err == nil {
		t.Fatal("missing destination accepted")
	}
}
