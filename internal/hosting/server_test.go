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
