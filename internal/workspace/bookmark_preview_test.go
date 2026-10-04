package workspace

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"trove.local/phase0/contracts"
)

type bookmarkRoundTrip func(*http.Request) (*http.Response, error)

func (f bookmarkRoundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func TestBookmarkTransportPinsPublicIPsAndRejectsRebindingTargets(t *testing.T) {
	t.Setenv("HTTPS_PROXY", "http://127.0.0.1:9999")
	lookups, dials := 0, []string{}
	client := bookmarkHTTPClient(func(context.Context, string) ([]net.IPAddr, error) {
		lookups++
		return []net.IPAddr{{IP: net.ParseIP("8.8.8.8")}}, nil
	}, func(_ context.Context, _ string, address string) (net.Conn, error) {
		dials = append(dials, address)
		return nil, errors.New("fixture dial")
	})
	transport := client.Transport.(*http.Transport)
	if transport.Proxy != nil || client.Jar != nil {
		t.Fatal("ambient network state enabled")
	}
	_, _ = transport.DialContext(context.Background(), "tcp", "source.example:443")
	if lookups != 1 || len(dials) != 1 || dials[0] != "8.8.8.8:443" {
		t.Fatal("dial must use the exact validated IP", lookups, dials)
	}
	for _, address := range []string{"127.0.0.1", "10.0.0.1", "169.254.169.254", "100.64.0.1", "192.0.2.2", "198.18.0.1", "::1", "fc00::1", "fe80::1", "::ffff:127.0.0.1", "64:ff9b::7f00:1", "2002:7f00:1::", "2001:db8::1"} {
		if bookmarkPublicIP(net.ParseIP(address)) {
			t.Fatalf("accepted nonpublic %s", address)
		}
	}
	client = bookmarkHTTPClient(func(context.Context, string) ([]net.IPAddr, error) {
		return []net.IPAddr{{IP: net.ParseIP("8.8.8.8")}, {IP: net.ParseIP("127.0.0.1")}}, nil
	}, func(context.Context, string, string) (net.Conn, error) {
		t.Fatal("mixed DNS answer must never dial")
		return nil, nil
	})
	if _, err := client.Transport.(*http.Transport).DialContext(context.Background(), "tcp", "rebound.example:443"); err == nil {
		t.Fatal("accepted mixed private answer")
	}
}
func TestBookmarkMetadataIsBoundedTextOnlyAndRedirectSafe(t *testing.T) {
	for _, url := range []string{"http://example.com", "https://user:secret@example.com", "https://127.0.0.1", "https://example.com:8443", "file:///tmp/secret", "javascript:alert(1)"} {
		if _, err := bookmarkURL(url); err == nil {
			t.Fatal("accepted unsafe URL", url)
		}
	}
	requests := 0
	client := bookmarkHTTPClient(nil, nil)
	client.Transport = bookmarkRoundTrip(func(r *http.Request) (*http.Response, error) {
		requests++
		if r.Header.Get("Cookie") != "" || r.Header.Get("Authorization") != "" {
			t.Fatal("credentials leaked")
		}
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"text/html; charset=utf-8"}}, Body: io.NopCloser(strings.NewReader(`<title> Original </title><meta property="og:title" content="Cached &amp; safe 🌱"><meta name="description" content="&lt;script&gt;source text&lt;/script&gt;"><script>fetch('https://other.example')</script><img src="https://other.example/image">`)), Request: r}, nil
	})
	title, description, err := fetchBookmarkMetadata(context.Background(), client, "https://source.example")
	if err != nil || title != "Cached & safe 🌱" || description != "<script>source text</script>" || requests != 1 {
		t.Fatal(title, description, err, requests)
	}
	client.Transport = bookmarkRoundTrip(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"text/html"}}, Body: io.NopCloser(strings.NewReader(strings.Repeat("x", bookmarkMaxBytes+1))), Request: r}, nil
	})
	if _, _, err = fetchBookmarkMetadata(context.Background(), client, "https://source.example"); err == nil {
		t.Fatal("oversized source accepted")
	}
	requests = 0
	client.Transport = bookmarkRoundTrip(func(r *http.Request) (*http.Response, error) {
		requests++
		return &http.Response{StatusCode: 302, Header: http.Header{"Location": []string{"https://127.0.0.1/secrets"}}, Body: io.NopCloser(strings.NewReader("")), Request: r}, nil
	})
	if _, _, err = fetchBookmarkMetadata(context.Background(), client, "https://source.example"); err == nil || requests != 1 {
		t.Fatal("redirect escaped public destination policy", err, requests)
	}
	client.Transport = bookmarkRoundTrip(func(r *http.Request) (*http.Response, error) {
		requests++
		return &http.Response{StatusCode: 302, Header: http.Header{"Location": []string{"https://source.example/loop"}}, Body: io.NopCloser(strings.NewReader("")), Request: r}, nil
	})
	requests = 0
	if _, _, err = fetchBookmarkMetadata(context.Background(), client, "https://source.example"); err == nil || requests != 3 {
		t.Fatal("redirect budget not enforced", err, requests)
	}
	if client.Timeout != 8*time.Second {
		t.Fatal("missing overall request timeout")
	}
}
func TestBookmarkPreviewRequiresUserConsentScopeAndRetainsCache(t *testing.T) {
	k, e := testKernel(t)
	page := createPage(t, k, e, "Cached source")
	request := advancedRequest(k, map[string]any{"pageId": page, "url": "https://127.0.0.1"})
	testCode(t, testDispatch(t, k, e, "editor.bookmarkPreview", request), "INVALID_REQUEST")
	request["consent"] = "fetch_once"
	outsider := k.Grant(contracts.ServiceActor{Kind: "user", ActorId: contracts.DomainId(NewID())}, "user", contracts.DomainScope{Value: contracts.DomainScopeVariant3{Kind: "pages", WorkspaceId: contracts.DomainId(k.ID()), PageIds: []contracts.DomainId{contracts.DomainId(NewID())}}})
	testCode(t, testDispatch(t, k, outsider, "editor.bookmarkPreview", request), "SCOPE_DENIED")
	automation := e
	automation.Origin = "automation"
	testCode(t, testDispatch(t, k, automation, "editor.bookmarkPreview", request), "SCOPE_DENIED")
	testCode(t, testDispatch(t, k, e, "editor.bookmarkPreview", request), "INVALID_REQUEST")
	block := NewID()
	data := map[string]any{"url": "https://source.example/article", "title": "Saved title", "description": "Saved description", "cachedSourceUrl": "https://source.example/article", "fetchedAt": now()}
	testOK(t, dbBatch(t, k, e, []any{map[string]any{"kind": "block.insert", "block": map[string]any{"blockId": block, "pageId": page, "type": "bookmark", "schemaVersion": 1, "data": data}}}, "page", page, "block", block))
	revision := str(readPage(t, k, e, page)["page"].(map[string]any)["revision"])
	testOK(t, dbBatch(t, k, e, []any{map[string]any{"kind": "block.update", "blockId": block, "type": "bookmark", "schemaVersion": 1, "data": map[string]any{"url": "https://changed.example", "title": "Manual"}}}, "page", page, "block", block))
	testOK(t, restoreTest(t, k, e, page, revision))
	path := k.Path()
	k.Close()
	reopened, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	stored := readPage(t, reopened, reopened.OwnerContext(), page)["blocks"].([]any)[0].(map[string]any)["data"]
	if string(marshal(stored)) != string(marshal(data)) {
		t.Fatal("cache changed across history/restart")
	}
	destination := filepath.Join(t.TempDir(), "bookmark.zip")
	if _, err = reopened.ExportPortable(context.Background(), reopened.OwnerContext(), destination); err != nil {
		t.Fatal(err)
	}
	target, owner := testKernel(t)
	handle, err := target.RegisterSelection(destination)
	if err != nil {
		t.Fatal(err)
	}
	preview, err := previewPortableImport(context.Background(), target, owner, map[string]any{"sourceHandle": handle, "sourceNamespace": "bookmark-cache"})
	if err != nil {
		t.Fatal(err)
	}
	plan := completeImportFixture(t, target, owner, preview.(ImportPlan))
	found := false
	for _, item := range plan.Items {
		if item.Title != "Cached source" {
			continue
		}
		for _, raw := range readPage(t, target, owner, item.PageID)["blocks"].([]any) {
			b := raw.(map[string]any)
			if b["type"] == "bookmark" {
				if string(marshal(b["data"])) != string(marshal(data)) {
					t.Fatal("portable bookmark cache changed")
				}
				found = true
			}
		}
	}
	if !found {
		t.Fatal("portable bookmark missing")
	}
}

func TestBookmarkLiveMetadataSmoke(t *testing.T) {
	if os.Getenv("TROVE_LIVE_BOOKMARK_SMOKE") != "1" {
		t.Skip("requires explicit network qualification")
	}
	k, e := testKernel(t)
	page := createPage(t, k, e, "Live metadata qualification")
	result := testOK(t, testDispatch(t, k, e, "editor.bookmarkPreview", advancedRequest(k, map[string]any{"pageId": page, "url": "https://example.com", "consent": "fetch_once"})))
	if result["sourceUrl"] != "https://example.com" || result["title"] != "Example Domain" {
		t.Fatal(result)
	}
	if _, err := time.Parse(time.RFC3339Nano, str(result["fetchedAt"])); err != nil {
		t.Fatal(err)
	}
}
