package hosting

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
	"trove.local/phase0/internal/workspace"
)

const ownerToken = "owner-test-token-0123456789-abcdef0123456789"

func newHost(t *testing.T) (*Server, *httptest.Server) {
	t.Helper()
	k, e := workspace.Create(filepath.Join(t.TempDir(), "workspace"))
	if e != nil {
		t.Fatal(e)
	}
	h, e := New(k, ownerToken, fstest.MapFS{"index.html": {Data: []byte("<html><head></head><body>Trove</body></html>")}}, false)
	if e != nil {
		t.Fatal(e)
	}
	server := httptest.NewServer(h)
	t.Cleanup(func() { server.Close(); h.Close(); k.Close() })
	return h, server
}
func request(t *testing.T, server *httptest.Server, path, token, origin string, body any) *http.Response {
	t.Helper()
	raw, _ := json.Marshal(body)
	req, _ := http.NewRequest("POST", server.URL+path, bytes.NewReader(raw))
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if origin != "" {
		req.Header.Set("Origin", origin)
	}
	res, e := http.DefaultClient.Do(req)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { res.Body.Close() })
	return res
}
func TestHostAuthenticationAndSameOrigin(t *testing.T) {
	_, server := newHost(t)
	for _, test := range []struct {
		token, origin string
		want          int
	}{{"", "", 401}, {"wrong", "", 401}, {ownerToken, "https://evil.invalid", 403}, {ownerToken, "", 200}} {
		r := request(t, server, "/api", test.token, test.origin, map[string]any{"method": "workspace.snapshot", "request": map[string]any{}})
		if r.StatusCode != test.want {
			t.Fatalf("status %d want %d", r.StatusCode, test.want)
		}
	}
	r := request(t, server, "/login", "", server.URL, map[string]any{"token": ownerToken})
	if r.StatusCode != 204 {
		t.Fatal(r.Status)
	}
	cookies := r.Cookies()
	if len(cookies) != 1 || !cookies[0].HttpOnly || cookies[0].SameSite != http.SameSiteStrictMode {
		t.Fatal("session cookie protection missing")
	}
	req, _ := http.NewRequest("GET", server.URL+"/", nil)
	req.AddCookie(cookies[0])
	res, e := http.DefaultClient.Do(req)
	if e != nil {
		t.Fatal(e)
	}
	defer res.Body.Close()
	if res.Header.Get("Content-Security-Policy") != "frame-ancestors 'none'" || res.Header.Get("X-Frame-Options") != "DENY" {
		t.Fatal("hosted app can be framed", res.Header)
	}
	b, _ := io.ReadAll(res.Body)
	if !strings.Contains(string(b), "host-bridge.js") {
		t.Fatal("authenticated frontend has no bridge")
	}
}
func TestHostCanonicalWriteAndDesktopOwnership(t *testing.T) {
	h, server := newHost(t)
	if other, e := workspace.Open(h.Kernel.Path()); e == nil {
		other.Close()
		t.Fatal("desktop opened host-owned workspace")
	}
	res := request(t, server, "/api", ownerToken, "", map[string]any{"method": "workspace.snapshot", "request": map[string]any{}})
	var snap struct {
		OK     bool           `json:"ok"`
		Result map[string]any `json:"result"`
	}
	json.NewDecoder(res.Body).Decode(&snap)
	if !snap.OK {
		t.Fatal("snapshot failed")
	}
	id := workspace.NewID()
	body := map[string]any{"contractVersion": 1, "requestId": workspace.NewID(), "workspaceId": h.Kernel.ID(), "mutation": map[string]any{"scopeGrantId": snap.Result["scopeGrantId"], "idempotencyKey": workspace.NewID(), "expectedRevisions": []any{map[string]any{"target": map[string]any{"entityType": "page", "entityId": id}, "revision": "0"}}}, "commands": []any{map[string]any{"kind": "page.create", "pageId": id, "title": "Hosted canonical page"}}}
	res = request(t, server, "/api", ownerToken, "", map[string]any{"method": "batch", "request": body})
	var response map[string]any
	json.NewDecoder(res.Body).Decode(&response)
	if response["ok"] != true {
		t.Fatalf("write: %#v", response)
	}
	res = request(t, server, "/api", ownerToken, "", map[string]any{"method": "index.approve", "request": map[string]any{"path": "/"}})
	json.NewDecoder(res.Body).Decode(&response)
	if response["ok"] != false {
		t.Fatal("host exposed filesystem approval")
	}
}
func TestHostTLSAndUploadBoundaries(t *testing.T) {
	for _, address := range []string{"0.0.0.0:7337", "192.168.1.1:7337", ":7337", "localhost:7337"} {
		if ValidateListen(address, "", "") == nil {
			t.Fatalf("insecure listener %s", address)
		}
	}
	if ValidateListen("127.0.0.1:7337", "", "") != nil || ValidateListen("[::1]:7337", "", "") != nil {
		t.Fatal("loopback denied")
	}
	if ValidateListen("0.0.0.0:7337", "cert", "key") != nil {
		t.Fatal("TLS listener denied")
	}
	_, server := newHost(t)
	r := request(t, server, "/upload", ownerToken, "", map[string]any{"name": "../../notes.txt", "data": "aG9zdCB1cGxvYWQ="})
	var result map[string]any
	json.NewDecoder(r.Body).Decode(&result)
	if r.StatusCode != 200 || result["selectionHandle"] == nil {
		t.Fatal("upload capability failed")
	}
	r = request(t, server, "/upload", ownerToken, "", map[string]any{"name": "x", "data": "@@@"})
	if r.StatusCode != 400 {
		t.Fatal("invalid upload accepted")
	}
}

func hostCall(t *testing.T, h *Server, server *httptest.Server, method string, fields map[string]any) map[string]any {
	t.Helper()
	body := map[string]any{"contractVersion": 1, "requestId": workspace.NewID(), "workspaceId": h.Kernel.ID()}
	for key, value := range fields {
		body[key] = value
	}
	response := request(t, server, "/api", ownerToken, server.URL, map[string]any{"method": method, "request": body})
	var result map[string]any
	if err := json.NewDecoder(response.Body).Decode(&result); err != nil {
		t.Fatal(err)
	}
	return result
}
func hostOK(t *testing.T, result map[string]any) map[string]any {
	t.Helper()
	if result["ok"] != true {
		t.Fatal(result)
	}
	out, _ := result["result"].(map[string]any)
	return out
}
func hostBatch(t *testing.T, h *Server, server *httptest.Server, grant any, commands []any, guards any) {
	t.Helper()
	hostOK(t, hostCall(t, h, server, "batch", map[string]any{"commands": commands, "mutation": map[string]any{"scopeGrantId": grant, "idempotencyKey": workspace.NewID(), "expectedRevisions": guards}}))
}
func pageGuard(id, revision string) []any {
	return []any{map[string]any{"target": map[string]any{"entityType": "page", "entityId": id}, "revision": revision}}
}
func TestHostFreshnessAndGroupedUndoWorkflow(t *testing.T) {
	h, server := newHost(t)
	snapshot := hostOK(t, hostCall(t, h, server, "workspace.snapshot", nil))
	grant := snapshot["scopeGrantId"]
	page := workspace.NewID()
	hostBatch(t, h, server, grant, []any{map[string]any{"kind": "page.create", "pageId": page, "title": "Reviewed source"}}, pageGuard(page, "0"))
	first := hostOK(t, hostCall(t, h, server, "page.freshness", map[string]any{"pageId": page}))
	freshness := first["freshness"].(map[string]any)
	hostBatch(t, h, server, grant, []any{map[string]any{"kind": "page.review", "pageId": page, "contentIdentity": freshness["contentIdentity"]}}, pageGuard(page, "1"))
	reviewed := hostOK(t, hostCall(t, h, server, "page.freshness", map[string]any{"pageId": page}))["freshness"].(map[string]any)
	if reviewed["state"] != "reviewed" {
		t.Fatal("review unavailable in hosted UI", reviewed)
	}
	queue := hostOK(t, hostCall(t, h, server, "page.reviewQueue", map[string]any{"limit": 10}))
	if len(queue["items"].([]any)) != 0 {
		t.Fatal("reviewed source still queued")
	}
	// The review is canonical metadata and advances the page revision.
	current := hostOK(t, hostCall(t, h, server, "readPage", map[string]any{"pageId": page}))
	revision := current["page"].(map[string]any)["revision"].(string)
	operation := workspace.NewID()
	hostBatch(t, h, server, grant, []any{map[string]any{"kind": "operation.apply", "operationId": operation, "label": "Rename reviewed source", "actions": []any{map[string]any{"kind": "page.update", "pageId": page, "title": "Updated source"}}}}, pageGuard(page, revision))
	entries := hostOK(t, hostCall(t, h, server, "operations.list", map[string]any{"pageId": page}))
	if len(entries["operations"].([]any)) != 1 {
		t.Fatal("hosted history omitted grouped operation")
	}
	preview := hostOK(t, hostCall(t, h, server, "operations.previewUndo", map[string]any{"operationId": operation}))
	hostBatch(t, h, server, grant, []any{preview["command"]}, preview["expectedRevisions"])
	restored := hostOK(t, hostCall(t, h, server, "readPage", map[string]any{"pageId": page}))
	if restored["page"].(map[string]any)["title"] != "Reviewed source" {
		t.Fatal("hosted undo failed")
	}
	hostOK(t, hostCall(t, h, server, "skills.list", nil))
}
func TestHostSharedRoutesRetainServiceValidationAndDesktopBoundary(t *testing.T) {
	h, server := newHost(t)
	for _, method := range []string{"recordLayouts.get", "scheduling.preview", "skills.preview", "review.propose", "review.apply", "review.cancel", "review.retry", "meetings.prepareExtraction", "meetings.extractions"} {
		t.Run(method, func(t *testing.T) {
			response := hostCall(t, h, server, method, nil)
			failure, _ := response["error"].(map[string]any)
			if response["ok"] != false || failure["code"] != "INVALID_REQUEST" {
				t.Fatal("shared route did not reach contract validation", response)
			}
		})
	}
	for _, method := range []string{"index.approve", "provider.auth.begin", "connectors.auth.begin", "backup.createEncrypted", "meeting.capture.start", "shell.openExternal"} {
		response := hostCall(t, h, server, method, nil)
		failure, _ := response["error"].(map[string]any)
		if response["ok"] != false || failure["code"] != "SCOPE_DENIED" {
			t.Fatal("desktop/native route exposed", method, response)
		}
	}
}
