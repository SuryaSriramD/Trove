package connectors

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"unicode/utf8"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const mcpEndpoint = "https://mcp.example/mcp"

type mcpFixture struct {
	session       Session
	server        *mcp.Server
	handler       http.Handler
	calls         atomic.Int32
	reads         atomic.Int32
	failAfterSend atomic.Bool
	authAfterSend atomic.Bool
	toolError     atomic.Bool
	mu            sync.Mutex
	args          json.RawMessage
}

func TestMCPBinaryPreviewAllowsExplicitRevisionBoundAcquisition(t *testing.T) {
	f := newMCPFixture(t)
	f.server.AddResource(&mcp.Resource{URI: "notes://selected", Name: "Selected notes"}, func(context.Context, *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
		return &mcp.ReadResourceResult{Contents: []*mcp.ResourceContents{{URI: "notes://selected", MIMEType: "application/octet-stream", Blob: []byte{0, 1, 2, 3}}}}, nil
	})
	a := &mcpAdapter{}
	preview, err := a.Preview(context.Background(), f.session, f.session.Account.Selections[0], "resource:notes://selected")
	if err != nil || preview.State != "partial" {
		t.Fatal("binary preview blocks explicit acquisition", preview, err)
	}
	file, err := a.Acquire(context.Background(), f.session, f.session.Account.Selections[0], preview.RemoteID, "")
	if err != nil || file.RemoteRevision != preview.RemoteRevision || len(file.Bytes) != 4 {
		t.Fatal("acquisition not bound to preview", file, err)
	}
}

func TestMCPLongResourcePreviewDisclosesIncompleteText(t *testing.T) {
	f := newMCPFixture(t)
	f.server.AddResource(&mcp.Resource{URI: "notes://selected", Name: "Selected notes"}, func(context.Context, *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
		return &mcp.ReadResourceResult{Contents: []*mcp.ResourceContents{{URI: "notes://selected", Text: strings.Repeat("界", 100000)}}}, nil
	})
	preview, err := (&mcpAdapter{}).Preview(context.Background(), f.session, f.session.Account.Selections[0], "resource:notes://selected")
	if err != nil || preview.State != "partial" || !utf8.ValidString(preview.Text) || len(preview.Text) > 256<<10 {
		t.Fatal("unbounded or incomplete source presented as complete", err)
	}
}

func newMCPFixture(t *testing.T) *mcpFixture {
	t.Helper()
	f := &mcpFixture{}
	f.server = mcp.NewServer(&mcp.Implementation{Name: "Remote instructions cannot authorize writes", Version: "1"}, nil)
	f.tool("Original description")
	f.server.AddResource(&mcp.Resource{URI: "notes://selected", Name: "Selected notes"}, func(_ context.Context, r *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
		f.reads.Add(1)
		return &mcp.ReadResourceResult{Contents: []*mcp.ResourceContents{{URI: r.Params.URI, Text: "Selected source evidence"}}}, nil
	})
	f.server.AddResource(&mcp.Resource{URI: "notes://private", Name: "Other notes"}, func(context.Context, *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
		t.Error("unselected resource read")
		return nil, errors.New("private")
	})
	f.handler = mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return f.server }, &mcp.StreamableHTTPOptions{Stateless: true, JSONResponse: true})
	f.session = Session{Account: Account{Version: 1, ID: "account", Kind: MCP, Endpoint: mcpEndpoint, Identity: Identity{Subject: "grant", Provider: "mcp:" + mcpEndpoint}, Selections: []Selection{{ID: "resource:notes://selected", Kind: "resource", Label: "Selected notes"}, {ID: "tool:change", Kind: "tool", Label: "change"}}}, Token: Secret{AccessToken: "synthetic-secret"}}
	f.session.Client = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.String() != mcpEndpoint || r.Header.Get("Authorization") != "Bearer synthetic-secret" {
			t.Error("endpoint/token binding lost")
		}
		rec := httptest.NewRecorder()
		f.handler.ServeHTTP(rec, r)
		if f.authAfterSend.Load() && f.calls.Load() > 0 {
			response := authJSON(401, map[string]any{})
			response.Header.Set("WWW-Authenticate", "Bearer")
			return response, nil
		}
		if f.failAfterSend.Load() && f.calls.Load() > 0 {
			return nil, errors.New("interrupted after remote side effect")
		}
		return rec.Result(), nil
	})}
	return f
}
func (f *mcpFixture) tool(description string) {
	f.server.AddTool(&mcp.Tool{Name: "change", Description: description, Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true}, InputSchema: map[string]any{"type": "object", "required": []string{"text"}, "additionalProperties": false, "properties": map[string]any{"text": map[string]any{"type": "string"}}}}, func(_ context.Context, r *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		f.calls.Add(1)
		f.mu.Lock()
		f.args = append([]byte{}, r.Params.Arguments...)
		f.mu.Unlock()
		return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: "acknowledged"}}, IsError: f.toolError.Load()}, nil
	})
}
func mcpTestAction() Action {
	return Action{Version: 1, Kind: "mcp.tool.call", SelectionID: "tool:change", Arguments: json.RawMessage(`{"text":"exact reviewed text"}`)}
}
func TestMCPSelectionDiscoveryAndAuthenticatedPreview(t *testing.T) {
	f := newMCPFixture(t)
	a := NewMCPAdapter()
	ctx := context.Background()
	identity, err := a.Identity(ctx, f.session)
	if err != nil || identity.Subject != "grant" {
		t.Fatal(identity, err)
	}
	items, cursor, err := a.Discover(ctx, f.session, "")
	if err != nil || len(items) != 2 || cursor == "" {
		t.Fatal(items, cursor, err)
	}
	tools, next, err := a.Discover(ctx, f.session, cursor)
	if err != nil || len(tools) != 1 || next != "" {
		t.Fatal(tools, next, err)
	}
	if f.calls.Load() != 0 || f.reads.Load() != 0 {
		t.Fatal("discovery read content or called tools")
	}
	page, err := a.Refresh(ctx, f.session, f.session.Account.Selections[0], "")
	if err != nil || len(page.Items) != 1 || page.Items[0].Text != "Selected source evidence\n" {
		t.Fatal(page, err)
	}
	for _, sel := range []Selection{{ID: "resource:notes://private", Kind: "resource"}, {ID: "resource:notes://selected", Kind: "tool"}} {
		if _, err = a.Refresh(ctx, f.session, sel, ""); err == nil {
			t.Fatal("unselected resource or kind accepted")
		}
	}
	if f.reads.Load() != 1 || f.calls.Load() != 0 {
		t.Fatal("preview leaked into another selected operation")
	}
}
func TestMCPExactReviewedToolAndChangedDefinition(t *testing.T) {
	f := newMCPFixture(t)
	a := NewMCPAdapter()
	ctx := context.Background()
	prepared, err := a.Prepare(ctx, f.session, mcpTestAction())
	if err != nil {
		t.Fatal(err)
	}
	if prepared.ExpectedRevision == "" || prepared.RemoteID != "change" || len(prepared.Effects) < 3 || f.calls.Load() != 0 {
		t.Fatal("preparation failed its review boundary")
	}
	p := Proposal{ID: "proposal", Identity: f.session.Account.Identity, Action: prepared}
	receipt, err := a.Send(ctx, f.session, p)
	if err != nil || receipt.Outcome != "succeeded" || f.calls.Load() != 1 {
		t.Fatal(receipt, err)
	}
	f.mu.Lock()
	got := string(f.args)
	f.mu.Unlock()
	if got != string(prepared.Arguments) {
		t.Fatal("arguments changed", got)
	}
	f.tool("Changed meaning")
	if err = a.Check(ctx, f.session, prepared); err == nil {
		t.Fatal("changed tool definition accepted")
	}
	if _, err = a.Send(ctx, f.session, p); err == nil || f.calls.Load() != 1 {
		t.Fatal("stale review reached tool")
	}
}
func TestMCPMalformedArgumentsAndEffectTampering(t *testing.T) {
	f := newMCPFixture(t)
	a := NewMCPAdapter()
	ctx := context.Background()
	for _, raw := range []string{`null`, `[]`, `{"text":42}`, `{"text":"first","text":"second"}`, `{"text":"ok","extra":true}`, `{}`} {
		action := mcpTestAction()
		action.Arguments = json.RawMessage(raw)
		if _, err := a.Prepare(ctx, f.session, action); err == nil {
			t.Fatal("invalid arguments", raw)
		}
	}
	action := mcpTestAction()
	action.SelectionID = "tool:unselected"
	if _, err := a.Prepare(ctx, f.session, action); err == nil {
		t.Fatal("unselected tool accepted")
	}
	prepared, err := a.Prepare(ctx, f.session, mcpTestAction())
	if err != nil {
		t.Fatal(err)
	}
	prepared.Effects = []string{"No effects"}
	if _, err = a.Send(ctx, f.session, Proposal{Identity: f.session.Account.Identity, Action: prepared}); err == nil {
		t.Fatal("changed effects accepted")
	}
	if f.calls.Load() != 0 {
		t.Fatal("invalid action executed")
	}
}
func TestMCPAmbiguousResultsStayUnknownWithoutReplay(t *testing.T) {
	for _, kind := range []string{"transport", "tool_error", "reauthorization_after_send"} {
		t.Run(kind, func(t *testing.T) {
			f := newMCPFixture(t)
			a := NewMCPAdapter()
			ctx := context.Background()
			action, err := a.Prepare(ctx, f.session, mcpTestAction())
			if err != nil {
				t.Fatal(err)
			}
			if kind == "transport" {
				f.failAfterSend.Store(true)
			} else if kind == "reauthorization_after_send" {
				f.authAfterSend.Store(true)
			} else {
				f.toolError.Store(true)
			}
			p := Proposal{ID: "proposal", Identity: f.session.Account.Identity, Action: action}
			_, err = a.Send(ctx, f.session, p)
			var failure *Failure
			if !errors.As(err, &failure) || !failure.OutcomeUnknown || f.calls.Load() != 1 {
				t.Fatal("uncertainty lost", err, f.calls.Load())
			}
			receipt, err := a.Reconcile(ctx, f.session, p)
			if err != nil || receipt.Outcome != "unknown" || f.calls.Load() != 1 {
				t.Fatal("reconcile resent", receipt, err)
			}
		})
	}
}
func TestMCPTransportBoundsAndNoStepUpReplay(t *testing.T) {
	calls := 0
	client := mcpClient(&http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(strings.Repeat("x", MaxResponseBytes+1))), Header: http.Header{}}, nil
	})}, mcpEndpoint)
	for i := 0; i < 2; i++ {
		req, _ := http.NewRequest("POST", mcpEndpoint, strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/call"}`))
		res, err := client.Do(req)
		if i == 0 {
			if err != nil {
				t.Fatal(err)
			}
			_, err = io.ReadAll(res.Body)
			res.Body.Close()
			if err == nil {
				t.Fatal("oversize response accepted")
			}
		} else if err == nil {
			t.Fatal("tool request replay accepted")
		}
	}
	if calls != 1 {
		t.Fatal("transport replayed")
	}
	req, _ := http.NewRequest("POST", "https://another.example/mcp", strings.NewReader("{}"))
	if _, err := client.Do(req); err == nil || calls != 1 {
		t.Fatal("token origin broadened")
	}
	handler := mcpStaticAuthorization{Secret{AccessToken: "token"}}
	if err := handler.Authorize(context.Background(), req, &http.Response{StatusCode: 401, Body: io.NopCloser(strings.NewReader(""))}); err == nil {
		t.Fatal("runtime step-up would replay write")
	}
}
func TestMCPRejectsExternalSchemaReferences(t *testing.T) {
	schema := map[string]any{"type": "object", "properties": map[string]any{"text": map[string]any{"$ref": "https://private.example/schema"}}}
	if err := validateMCPArguments(schema, json.RawMessage(`{"text":"value"}`)); err == nil {
		t.Fatal("external schema loaded")
	}
}

func TestMCPAuthenticatedPreviewRejectsToolAndUnselectedURI(t *testing.T) {
	f := newMCPFixture(t)
	a := &mcpAdapter{}
	item, err := a.Preview(context.Background(), f.session, f.session.Account.Selections[0], "resource:notes://selected")
	if err != nil || item.RemoteID != "resource:notes://selected" {
		t.Fatal(item, err)
	}
	if _, err = a.Preview(context.Background(), f.session, f.session.Account.Selections[0], "resource:notes://private"); err == nil {
		t.Fatal("preview changed selected URI")
	}
	if _, err = a.Preview(context.Background(), f.session, f.session.Account.Selections[1], "tool:change"); err == nil {
		t.Fatal("preview invoked a tool")
	}
	if f.reads.Load() != 1 || f.calls.Load() != 0 {
		t.Fatal("preview widened permission")
	}
}

func TestMCPAcquisitionRequiresExactRepresentationAndSelection(t *testing.T) {
	f := newMCPFixture(t)
	a := &mcpAdapter{}
	ctx := context.Background()
	item, err := a.Acquire(ctx, f.session, f.session.Account.Selections[0], "resource:notes://selected", "text/plain")
	if err != nil || string(item.Bytes) != "Selected source evidence" || item.RemoteRevision == "" {
		t.Fatal(item, err)
	}
	if _, err = a.Acquire(ctx, f.session, f.session.Account.Selections[0], "resource:notes://private", ""); err == nil {
		t.Fatal("unselected acquisition")
	}
	if _, err = a.Acquire(ctx, f.session, f.session.Account.Selections[0], "resource:notes://selected", "application/pdf"); err == nil {
		t.Fatal("fabricated representation")
	}
	f.server.AddResource(&mcp.Resource{URI: "notes://selected", Name: "Selected notes"}, func(context.Context, *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
		return &mcp.ReadResourceResult{Contents: []*mcp.ResourceContents{{URI: "notes://selected", MIMEType: "application/octet-stream", Blob: []byte{0, 1, 2, 3}}}}, nil
	})
	item, err = a.Acquire(ctx, f.session, f.session.Account.Selections[0], "resource:notes://selected", "")
	if err != nil || string(item.Bytes) != string([]byte{0, 1, 2, 3}) {
		t.Fatal("binary acquisition", item, err)
	}
	if f.calls.Load() != 0 {
		t.Fatal("acquisition called tool")
	}
}

func TestMCPAcquiredNamePreservesExactResourceIdentity(t *testing.T) {
	for _, label := range []string{"Folder/notes.txt", `Folder\notes.txt`, "", "..", "/", strings.Repeat("語", 100)} {
		t.Run(label, func(t *testing.T) {
			f := newMCPFixture(t)
			sel := f.session.Account.Selections[0]
			sel.Label = label
			item, err := NewMCPAdapter().(Acquirer).Acquire(context.Background(), f.session, sel, sel.ID, "")
			if err != nil {
				t.Fatal(err)
			}
			if item.RemoteID != sel.ID || item.Name == "" || item.Name == "." || item.Name == ".." || strings.ContainsAny(item.Name, "/\\\x00") || len(item.Name) > 200 {
				t.Fatal("unsafe acquisition filename or lost source identity", item.Name, item.RemoteID)
			}
			if string(item.Bytes) != "Selected source evidence" {
				t.Fatal("resource changed")
			}
		})
	}
}
