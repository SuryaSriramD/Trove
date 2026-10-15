package workspace

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
)

type notionFixtureTransport func(*http.Request) (*http.Response, error)

func (f notionFixtureTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func notionResponse(status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(body)), Header: http.Header{}}
}
func TestNotionCaptureResumesPaginationDownloadsOfflineAndRequiresReview(t *testing.T) {
	k, e := testKernel(t)
	ctx := context.Background()
	root := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	out, err := startNotionCapture(ctx, k, e, map[string]any{"rootIds": []any{root}, "sourceNamespace": "notion-fixture"})
	if err != nil {
		t.Fatal(err)
	}
	capture := out.(NotionCapture)
	apiCalls, assets := 0, 0
	retried := false
	client := &http.Client{Transport: notionFixtureTransport(func(r *http.Request) (*http.Response, error) {
		if r.URL.Host == "api.notion.com" {
			apiCalls++
			if r.Header.Get("Authorization") != "Bearer fixture-secret-token" || r.Header.Get("Notion-Version") != notionCaptureVersion {
				t.Fatal("missing authenticated version")
			}
			if strings.Contains(r.URL.Path, "/pages/") {
				return notionResponse(200, `{"object":"page","in_trash":false,"id":"`+root+`","properties":{"title":{"type":"title","title":[{"plain_text":"API page"}]},"formula":{"type":"formula","formula":{"number":42}}}}`), nil
			}
			if !retried {
				retried = true
				return notionResponse(429, `{"code":"rate_limited"}`), nil
			}
			if r.URL.Query().Get("start_cursor") == "next" {
				return notionResponse(200, `{"results":[{"id":"eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee","type":"paragraph","paragraph":{"rich_text":[{"plain_text":"Second page"}]}}],"has_more":false,"next_cursor":null}`), nil
			}
			return notionResponse(200, `{"results":[{"id":"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb","type":"paragraph","paragraph":{"rich_text":[{"plain_text":"Offline captured text"}]}},{"id":"cccccccccccccccccccccccccccccccc","type":"image","image":{"type":"file","file":{"url":"https://prod-files-secure.s3.us-west-2.amazonaws.com/photo.png"},"caption":[]}},{"id":"dddddddddddddddddddddddddddddddd","type":"unknown_block","unknown_block":{"evidence":"retained"}}],"has_more":true,"next_cursor":"next"}`), nil
		}
		if r.Header.Get("Authorization") != "" {
			t.Fatal("token leaked to attachment host")
		}
		assets++
		return notionResponse(200, "offline-image-bytes"), nil
	})}
	capture, err = k.CaptureNotionStep(ctx, e, capture.ID, capture.Revision, "fixture-secret-token", client)
	if err != nil {
		t.Fatal(err)
	}
	var stored string
	k.db.QueryRow("SELECT state_json FROM notion_captures WHERE id=?", capture.ID).Scan(&stored)
	if strings.Contains(stored, "fixture-secret-token") {
		t.Fatal("credential persisted")
	}
	partial, err := loadImportPlan(ctx, k.db, capture.PlanID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = commitPortableImport(ctx, k, e, map[string]any{"planId": partial.ID, "planHash": partial.Hash}); err == nil {
		t.Fatal("unreviewed capture committed")
	}
	oldRevision := capture.Revision
	for i := 0; i < 5 && capture.State != "ready"; i++ {
		capture, err = k.CaptureNotionStep(ctx, e, capture.ID, capture.Revision, "fixture-secret-token", client)
		if err != nil {
			t.Fatal(err)
		}
	}
	if capture.State != "ready" || assets != 1 || apiCalls != 4 {
		t.Fatalf("pagination/retry failed: %+v calls=%d assets=%d", capture, apiCalls, assets)
	}
	if _, err = k.CaptureNotionStep(ctx, e, capture.ID, oldRevision, "fixture-secret-token", client); err == nil {
		t.Fatal("stale cursor accepted")
	}
	finished, err := finishNotionCapture(ctx, k, e, map[string]any{"captureId": capture.ID, "revision": float64(capture.Revision)})
	if err != nil {
		t.Fatal(err)
	}
	plan := finished.(ImportPlan)
	completeImportFixture(t, k, e, plan)
	var text, photo int
	k.db.QueryRow("SELECT count(*) FROM blocks WHERE data_json LIKE '%Offline captured text%'").Scan(&text)
	k.db.QueryRow("SELECT count(*) FROM files WHERE content_hash=?", importDigest([]byte("offline-image-bytes"))).Scan(&photo)
	if text != 1 || photo != 1 {
		t.Fatal("offline content absent", text, photo)
	}
	for _, item := range plan.Items {
		if item.RawFileID == "" {
			t.Fatal("supplied item lacks evidence", item)
		}
	}
}
func TestNotionDeniedRootRetainsCursorAndNeedsExplicitPartialFinish(t *testing.T) {
	k, e := testKernel(t)
	ctx := context.Background()
	out, err := startNotionCapture(ctx, k, e, map[string]any{"rootIds": []any{"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}, "sourceNamespace": "denied"})
	if err != nil {
		t.Fatal(err)
	}
	capture := out.(NotionCapture)
	client := &http.Client{Transport: notionFixtureTransport(func(r *http.Request) (*http.Response, error) {
		return notionResponse(403, `{"code":"restricted_resource"}`), nil
	})}
	capture, err = k.CaptureNotionStep(ctx, e, capture.ID, capture.Revision, "fixture-token", client)
	if err != nil || capture.State != "blocked" || len(capture.Pending) != 1 || !strings.Contains(capture.Error, "403") {
		t.Fatal(capture, err)
	}
	request := map[string]any{"captureId": capture.ID, "revision": float64(capture.Revision)}
	if _, err = finishNotionCapture(ctx, k, e, request); err == nil {
		t.Fatal("partial capture silently finalized")
	}
	request["allowPartial"] = true
	finished, err := finishNotionCapture(ctx, k, e, request)
	if err != nil {
		t.Fatal(err)
	}
	plan := completeImportFixture(t, k, e, finished.(ImportPlan))
	if portableImportReport(plan)["state"] != "partial" {
		t.Fatal(fmt.Sprint(portableImportReport(plan)))
	}
	if notionAttachmentURL("https://localhost/secrets") || notionAttachmentURL("https://amazonaws.com.evil.test/file") || notionAttachmentURL("http://file.notion.so/file") {
		t.Fatal("attachment boundary escaped")
	}
}
