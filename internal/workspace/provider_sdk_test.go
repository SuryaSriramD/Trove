package workspace

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type pairingTransport struct{ connection ProviderConnection }

func (p pairingTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	r.Header = r.Header.Clone()
	r.Header.Set("Authorization", "Bearer "+p.connection.Token)
	r.Header.Set("Trove-Connection-Id", p.connection.ID)
	return http.DefaultTransport.RoundTrip(r)
}
func TestOfficialMCPClientReviewedCreationAndRevocation(t *testing.T) {
	k, e := testKernel(t)
	parent := createPage(t, k, e, "Shared destination")
	b := NewProviderBridge(k, e)
	defer b.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	c, err := b.Connect(ctx, "mcp", []string{parent}, []string{"readPage", "proposeActions"})
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(b.MCPHandler())
	defer server.Close()
	client := mcp.NewClient(&mcp.Implementation{Name: "official-sdk-qualification", Version: "1.8.0"}, nil)
	session, err := client.Connect(ctx, &mcp.StreamableClientTransport{Endpoint: server.URL, HTTPClient: &http.Client{Transport: pairingTransport{c}, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	listed, err := session.ListTools(ctx, nil)
	if err != nil || len(listed.Tools) != 2 {
		t.Fatal(listed, err)
	}
	args := map[string]any{"idempotencyKey": "sdk-create", "plan": map[string]any{"version": 1, "actions": []any{map[string]any{"kind": "create_page", "destination": "Shared destination", "title": "SDK reviewed page", "body": "Exact reviewed content."}}}}
	result, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "proposeActions", Arguments: args})
	if err != nil || result.IsError {
		t.Fatal(result, err)
	}
	var envelope struct {
		Result ProviderProposal `json:"result"`
	}
	if err = json.Unmarshal([]byte(result.Content[0].(*mcp.TextContent).Text), &envelope); err != nil {
		t.Fatal(err)
	}
	var count int
	k.db.QueryRow("SELECT count(*) FROM pages WHERE title='SDK reviewed page'").Scan(&count)
	if count != 0 {
		t.Fatal("tool committed without approval")
	}
	approved, err := b.Approve(ctx, envelope.Result.ID)
	if err != nil || !strings.Contains(string(approved), `"ok":true`) {
		t.Fatal(string(approved), err)
	}
	result, err = session.CallTool(ctx, &mcp.CallToolParams{Name: "proposeActions", Arguments: args})
	if err != nil || result.IsError {
		t.Fatal(result, err)
	}
	k.db.QueryRow("SELECT count(*) FROM pages WHERE title='SDK reviewed page'").Scan(&count)
	if count != 1 {
		t.Fatal("SDK retry duplicated content")
	}
	if err = b.Disconnect(ctx, c.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = session.ListTools(ctx, nil); err == nil {
		t.Fatal("revoked SDK connection retained discovery")
	}
}
func TestOfficialMCPClientCancelsQueuedProposal(t *testing.T) {
	k, e := testKernel(t)
	parent := createPage(t, k, e, "Destination")
	b := NewProviderBridge(k, e)
	defer b.Close()
	ctx := context.Background()
	c, err := b.Connect(ctx, "mcp", []string{parent}, []string{"proposeActions"})
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(b.MCPHandler())
	defer server.Close()
	client := mcp.NewClient(&mcp.Implementation{Name: "cancellation-client", Version: "1"}, nil)
	session, err := client.Connect(ctx, &mcp.StreamableClientTransport{Endpoint: server.URL, HTTPClient: &http.Client{Transport: pairingTransport{c}}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	k.writer <- struct{}{}
	defer func() { <-k.writer }()
	request, cancel := context.WithCancel(ctx)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, err := session.CallTool(request, &mcp.CallToolParams{Name: "proposeActions", Arguments: map[string]any{"idempotencyKey": "cancel", "plan": map[string]any{"version": 1, "actions": []any{map[string]any{"kind": "create_page", "destination": "Destination", "title": "Cancelled"}}}}})
		done <- err
	}()
	deadline := time.Now().Add(2 * time.Second)
	for len(k.pending) == 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if len(k.pending) == 0 {
		t.Fatal("call did not reach serialized proposal storage")
	}
	cancel()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("cancelled call succeeded")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("client cancellation stalled")
	}
	deadline = time.Now().Add(2 * time.Second)
	for len(k.pending) > 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if len(k.pending) > 0 {
		t.Fatal("server did not cancel queued writer")
	}
}

func TestMCPHTTPIDBoundaryAndIsolatedSessions(t *testing.T) {
	k, e := testKernel(t)
	page := createPage(t, k, e, "Shared")
	b := NewProviderBridge(k, e)
	defer b.Close()
	c, err := b.Connect(context.Background(), "mcp", []string{page}, []string{"readPage"})
	if err != nil {
		t.Fatal(err)
	}
	h := b.MCPHandler()
	request := func(body, session string, c ProviderConnection) *httptest.ResponseRecorder {
		r := httptest.NewRequest("POST", "http://127.0.0.1/mcp", strings.NewReader(body))
		r.Header.Set("Authorization", "Bearer "+c.Token)
		r.Header.Set("Trove-Connection-Id", c.ID)
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Accept", "application/json, text/event-stream")
		if session != "" {
			r.Header.Set("Mcp-Session-Id", session)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	for _, id := range []string{"9007199254740992", "9007199254740993", "-9007199254740992", "1.5"} {
		w := request(`{"jsonrpc":"2.0","id":`+id+`,"method":"ping"}`, "", c)
		if w.Code != 400 {
			t.Fatal("imprecise ID accepted", id, w.Code)
		}
	}
	init := request(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-03-26","capabilities":{},"clientInfo":{"name":"isolation","version":"1"}}}`, "", c)
	if init.Code != 200 {
		t.Fatal(init.Body.String())
	}
	other, err := b.Connect(context.Background(), "mcp", []string{page}, []string{"readPage"})
	if err != nil {
		t.Fatal(err)
	}
	stolen := request(`{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}`, init.Header().Get("Mcp-Session-Id"), other)
	if stolen.Code != 404 {
		t.Fatal("session crossed paired connection", stolen.Code, stolen.Body.String())
	}
}
func TestProviderReadToolsSupplyBoundedScopedDefaults(t *testing.T) {
	k, e := testKernel(t)
	ctx := context.Background()
	parent := createPage(t, k, e, "Scoped findable")
	createPage(t, k, e, "Private findable")
	db := newDatabase(t, k, e)
	bridge := NewProviderBridge(k, e)
	defer bridge.Close()
	c, err := bridge.Connect(ctx, "mcp", []string{parent, db.page}, []string{"search.query", "queryRecords"})
	if err != nil {
		t.Fatal(err)
	}
	for tool, args := range map[string]map[string]any{"search.query": {"query": "findable"}, "queryRecords": {"collectionId": db.collection}} {
		raw, err := bridge.Call(ctx, c.ID, c.Token, tool, args)
		if err != nil {
			t.Fatal(err)
		}
		var result map[string]any
		if json.Unmarshal(raw, &result) != nil || result["ok"] != true {
			t.Fatal("discovered minimal tool arguments unusable", tool, string(raw))
		}
		if strings.Contains(string(raw), "Private findable") {
			t.Fatal("default search widened grant")
		}
	}
}
