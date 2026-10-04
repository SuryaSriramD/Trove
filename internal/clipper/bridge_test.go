package clipper

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

const testExtension = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

func TestPairedBridgeOriginScopeIdempotencyAndRevocation(t *testing.T) {
	calls := 0
	b, pair, err := Start(context.Background(), Config{ExtensionID: testExtension, Destinations: func(context.Context) ([]Destination, error) {
		return []Destination{{"inbox", "Inbox", "0"}, {"approved", "Approved page", "7"}}, nil
	}, Preview: func(_ context.Context, c Capture) (Receipt, error) {
		calls++
		return Receipt{"plan-1", "awaiting_owner_review", c.Title}, nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	send := func(path string, value any, token, origin, host string) (int, map[string]any) {
		t.Helper()
		raw, _ := json.Marshal(value)
		req, _ := http.NewRequest("POST", pair.Endpoint+path, bytes.NewReader(raw))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Origin", origin)
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		if host != "" {
			req.Host = host
		}
		client := http.Client{Timeout: time.Second}
		resp, err := client.Do(req)
		if err != nil {
			return 0, nil
		}
		defer resp.Body.Close()
		var out map[string]any
		json.NewDecoder(resp.Body).Decode(&out)
		return resp.StatusCode, out
	}
	origin := "chrome-extension://" + testExtension
	if code, _ := send("/pair", map[string]any{"version": 1, "code": pair.Code}, "", "https://web.invalid", ""); code != 403 {
		t.Fatal(code)
	}
	if code, _ := send("/pair", map[string]any{"version": 1, "code": pair.Code}, "", origin, "rebind.invalid"); code != 403 {
		t.Fatal(code)
	}
	if code, _ := send("/destinations", map[string]any{"version": 1}, "", origin, ""); code != 401 {
		t.Fatal(code)
	}
	code, out := send("/pair", map[string]any{"version": 1, "code": pair.Code}, "", origin, "")
	if code != 200 {
		t.Fatal(code, out)
	}
	token := out["token"].(string)
	if code, _ = send("/pair", map[string]any{"version": 1, "code": pair.Code}, "", origin, ""); code != 401 {
		t.Fatal("one-time code reused", code)
	}
	capture := Capture{Version: 1, RequestID: "browser-request-1234", DestinationID: "unapproved", DestinationRevision: "0", Title: "Clip"}
	if code, _ = send("/capture", capture, token, origin, ""); code != 403 {
		t.Fatal("unapproved destination", code)
	}
	capture.DestinationID = "approved"
	if code, _ = send("/capture", capture, token, origin, ""); code != 403 {
		t.Fatal("stale destination", code)
	}
	capture.DestinationRevision = "7"
	for range 2 {
		if code, out = send("/capture", capture, token, origin, ""); code != 200 {
			t.Fatal(code, out)
		}
	}
	if calls != 1 {
		t.Fatal("duplicate capture applied", calls)
	}
	capture.Title = "Changed"
	if code, _ = send("/capture", capture, token, origin, ""); code != 409 {
		t.Fatal("idempotency content changed", code)
	}
	if code, _ = send("/capture", capture, token, "chrome-extension://bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", ""); code != 403 {
		t.Fatal("other extension accepted", code)
	}
	req, _ := http.NewRequest("POST", pair.Endpoint+"/capture", strings.NewReader(strings.Repeat("x", MaximumRequestBytes+1)))
	req.Header.Set("Origin", origin)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 413 {
		t.Fatal("body limit", resp.StatusCode)
	}
	b.Close()
	if b.Paired() {
		t.Fatal("revoked session still paired")
	}
	if code, _ = send("/destinations", map[string]any{"version": 1}, token, origin, ""); code != 0 {
		t.Fatal("listener survived revocation", code)
	}
}
func TestPairExpiryAndInvalidExtension(t *testing.T) {
	if _, _, err := Start(context.Background(), Config{ExtensionID: "https://site.test"}); err == nil {
		t.Fatal("non-extension identity accepted")
	}
	b, pair, err := Start(context.Background(), Config{ExtensionID: testExtension, Destinations: func(context.Context) ([]Destination, error) { return nil, nil }, Preview: func(context.Context, Capture) (Receipt, error) { return Receipt{}, nil }})
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	b.mu.Lock()
	b.expires = time.Now().Add(-time.Second)
	b.mu.Unlock()
	req, _ := http.NewRequest("POST", pair.Endpoint+"/pair", strings.NewReader(`{"version":1,"code":"`+pair.Code+`"}`))
	req.Header.Set("Origin", "chrome-extension://"+testExtension)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 401 {
		t.Fatal("expired pairing accepted")
	}
}
