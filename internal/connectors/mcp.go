package connectors

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync/atomic"

	mcpauth "github.com/modelcontextprotocol/go-sdk/auth"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/santhosh-tekuri/jsonschema/v6"
	"golang.org/x/oauth2"
)

// MCP descriptions, hints and tool annotations are untrusted remote content.
// Discovery does not select a resource or authorize a call, including tools
// advertised as read-only. All calls go through an exact reviewed proposal.
type mcpAdapter struct{}

func NewMCPAdapter() Adapter { return &mcpAdapter{} }

type mcpStaticAuthorization struct{ token Secret }

func (h mcpStaticAuthorization) TokenSource(context.Context) (oauth2.TokenSource, error) {
	if h.token.AccessToken == "" {
		return nil, fail("AUTH_REQUIRED", "Authorize this MCP server explicitly")
	}
	return oauth2.StaticTokenSource(&oauth2.Token{AccessToken: h.token.AccessToken, TokenType: "Bearer", Expiry: h.token.ExpiresAt}), nil
}
func (mcpStaticAuthorization) Authorize(_ context.Context, _ *http.Request, r *http.Response) error {
	r.Body.Close()
	return fail("ACCESS_REVOKED", "MCP authorization changed; reconnect and review again")
}

// Bound both JSON and SSE bodies. Refuse a second tools/call even if transport
// behavior changes in a later SDK. An interrupted write is never replayed.
type mcpTransport struct {
	next      http.RoundTripper
	endpoint  string
	toolCalls atomic.Int32
}

func (t *mcpTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	if r.URL.String() != t.endpoint {
		return nil, fail("INVALID_ENDPOINT", "MCP requests must use the exact trusted endpoint")
	}
	if err := ValidateEndpoint(r.URL.String()); err != nil {
		return nil, err
	}
	if r.Body != nil {
		b, err := io.ReadAll(io.LimitReader(r.Body, MaxResponseBytes+1))
		r.Body.Close()
		if err != nil || len(b) > MaxResponseBytes {
			return nil, fail("INVALID_REQUEST", "MCP request exceeded its bound")
		}
		r = r.Clone(r.Context())
		r.Body = io.NopCloser(bytes.NewReader(b))
		var msg struct {
			Method string `json:"method"`
		}
		if json.Unmarshal(b, &msg) != nil {
			return nil, fail("INVALID_REQUEST", "Invalid MCP protocol message")
		}
		if msg.Method == "tools/call" && t.toolCalls.Add(1) != 1 {
			return nil, Unknown("A tool call cannot be replayed; verify its remote outcome")
		}
	}
	resp, err := t.next.RoundTrip(r)
	if resp != nil && resp.Body != nil {
		resp.Body = &mcpBoundedBody{ReadCloser: resp.Body, remaining: MaxResponseBytes}
	}
	return resp, err
}

type mcpBoundedBody struct {
	io.ReadCloser
	remaining int64
}

func (b *mcpBoundedBody) Read(p []byte) (int, error) {
	if b.remaining <= 0 {
		return 0, errors.New("MCP response exceeded its bound")
	}
	if int64(len(p)) > b.remaining {
		p = p[:b.remaining]
	}
	n, err := b.ReadCloser.Read(p)
	b.remaining -= int64(n)
	return n, err
}
func mcpClient(base *http.Client, endpoint string) *http.Client {
	if base == nil {
		base = NewHTTPClient()
	}
	c := *base
	next := base.Transport
	if next == nil {
		next = http.DefaultTransport
	}
	c.Transport = &mcpTransport{next: next, endpoint: endpoint}
	c.CheckRedirect = func(*http.Request, []*http.Request) error {
		return fail("REDIRECT_DENIED", "MCP endpoint redirects require explicit registration")
	}
	return &c
}
func validateMCPEndpoint(raw string) error {
	if err := ValidateEndpoint(raw); err != nil {
		return err
	}
	u, _ := url.Parse(raw)
	if u.RawQuery != "" || u.ForceQuery {
		return fail("INVALID_ENDPOINT", "MCP registration endpoints cannot contain query credentials or parameters")
	}
	return nil
}
func connectMCP(ctx context.Context, s Session, handler mcpauth.OAuthHandler) (*mcp.ClientSession, error) {
	if err := validateMCPEndpoint(s.Account.Endpoint); err != nil {
		return nil, err
	}
	if s.Account.Kind != MCP && s.Account.Kind != Notion {
		return nil, Unsupported("MCP endpoint")
	}
	client := mcp.NewClient(&mcp.Implementation{Name: "Trove", Version: "1"}, &mcp.ClientOptions{Capabilities: &mcp.ClientCapabilities{}, Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	if handler == nil {
		handler = mcpStaticAuthorization{s.Token}
	}
	session, err := client.Connect(ctx, &mcp.StreamableClientTransport{Endpoint: s.Account.Endpoint, HTTPClient: mcpClient(s.Client, s.Account.Endpoint), OAuthHandler: handler, MaxRetries: -1, DisableStandaloneSSE: true, MaxEventSize: MaxResponseBytes}, nil)
	if err != nil {
		return nil, fail("MCP_UNAVAILABLE", "MCP session could not be established; check authorization and server availability")
	}
	return session, nil
}
func mcpGrantIdentity(s Session) (Identity, error) {
	if s.Account.Identity.Subject == "" || s.Account.Identity.Provider != "mcp:"+s.Account.Endpoint {
		return Identity{}, fail("AUTH_REQUIRED", "MCP grant identity is unavailable; authorize explicitly")
	}
	return s.Account.Identity, nil
}
func (a *mcpAdapter) Identity(ctx context.Context, s Session) (Identity, error) {
	identity, err := mcpGrantIdentity(s)
	if err != nil {
		return Identity{}, err
	}
	cs, err := connectMCP(ctx, s, nil)
	if err != nil {
		return Identity{}, err
	}
	defer cs.Close()
	if cs.InitializeResult() == nil || cs.InitializeResult().ServerInfo == nil {
		return Identity{}, fail("INVALID_RESPONSE", "MCP server identity is missing")
	}
	return identity, nil
}

type mcpCursor struct {
	Phase  string `json:"phase"`
	Cursor string `json:"cursor"`
}

func validMCPName(s string) bool {
	return s != "" && len(s) <= 2039 && !strings.ContainsAny(s, "\x00\r\n")
}
func (a *mcpAdapter) Discover(ctx context.Context, s Session, cursor string) ([]Selection, string, error) {
	c := mcpCursor{Phase: "resources"}
	if cursor != "" {
		if err := decodeCursor(cursor, &c); err != nil {
			return nil, "", err
		}
	}
	if (c.Phase != "resources" && c.Phase != "tools") || len(c.Cursor) > 4096 {
		return nil, "", fail("INVALID_REQUEST", "MCP discovery cursor is invalid")
	}
	cs, err := connectMCP(ctx, s, nil)
	if err != nil {
		return nil, "", err
	}
	defer cs.Close()
	out := []Selection{}
	caps := cs.InitializeResult().Capabilities
	if caps == nil {
		caps = &mcp.ServerCapabilities{}
	}
	if c.Phase == "resources" {
		if caps.Resources != nil {
			res, err := cs.ListResources(ctx, &mcp.ListResourcesParams{Cursor: c.Cursor})
			if err != nil {
				return nil, "", fail("MCP_UNAVAILABLE", "MCP resources could not be listed")
			}
			if len(res.Resources) > 1000 || len(res.NextCursor) > 4096 || res.NextCursor != "" && res.NextCursor == c.Cursor {
				return nil, "", fail("INVALID_RESPONSE", "MCP resource pagination exceeded its bound")
			}
			for _, r := range res.Resources {
				if r == nil || !validMCPName(r.URI) {
					return nil, "", fail("INVALID_RESPONSE", "MCP resource identity is invalid")
				}
				out = append(out, Selection{ID: "resource:" + r.URI, Kind: "resource", Label: bounded(r.Name, 1000)})
			}
			if res.NextCursor != "" {
				return out, opaqueCursor(mcpCursor{Phase: "resources", Cursor: res.NextCursor}), nil
			}
		}
		if caps.Tools != nil {
			return out, opaqueCursor(mcpCursor{Phase: "tools"}), nil
		}
		return out, "", nil
	}
	if caps.Tools == nil {
		return out, "", nil
	}
	res, err := cs.ListTools(ctx, &mcp.ListToolsParams{Cursor: c.Cursor})
	if err != nil {
		return nil, "", fail("MCP_UNAVAILABLE", "MCP tools could not be listed")
	}
	if len(res.Tools) > 1000 || len(res.NextCursor) > 4096 || res.NextCursor != "" && res.NextCursor == c.Cursor {
		return nil, "", fail("INVALID_RESPONSE", "MCP tool pagination exceeded its bound")
	}
	for _, tool := range res.Tools {
		if tool == nil || !validMCPName(tool.Name) {
			return nil, "", fail("INVALID_RESPONSE", "MCP tool identity is invalid")
		}
		out = append(out, Selection{ID: "tool:" + tool.Name, Kind: "tool", Label: bounded(tool.Name, 1000)})
	}
	if res.NextCursor != "" {
		return out, opaqueCursor(mcpCursor{Phase: "tools", Cursor: res.NextCursor}), nil
	}
	return out, "", nil
}
func mcpSelected(s Session, id, kind string) (string, error) {
	sel, err := Selected(s.Account, id)
	if err != nil {
		return "", err
	}
	name := strings.TrimPrefix(sel.ID, kind+":")
	if sel.Kind != kind || name == sel.ID || !validMCPName(name) {
		return "", fail("SELECTION_DENIED", "Select this exact MCP resource or tool first")
	}
	return name, nil
}
func mcpTool(ctx context.Context, cs *mcp.ClientSession, name string) (*mcp.Tool, error) {
	cursor := ""
	seen := map[string]bool{}
	for page := 0; page < 20; page++ {
		res, err := cs.ListTools(ctx, &mcp.ListToolsParams{Cursor: cursor})
		if err != nil {
			return nil, fail("MCP_UNAVAILABLE", "MCP tool definition could not be verified")
		}
		if len(res.Tools) > 1000 {
			return nil, fail("INVALID_RESPONSE", "MCP tool list exceeded its bound")
		}
		for _, tool := range res.Tools {
			if tool != nil && tool.Name == name {
				return tool, nil
			}
		}
		if res.NextCursor == "" {
			return nil, fail("REMOTE_MISSING", "Selected MCP tool is unavailable")
		}
		if len(res.NextCursor) > 4096 || seen[res.NextCursor] {
			return nil, fail("INVALID_RESPONSE", "MCP tool pagination repeated")
		}
		seen[res.NextCursor] = true
		cursor = res.NextCursor
	}
	return nil, fail("RESOURCE_EXHAUSTED", "MCP tool definition lookup exceeded its page bound")
}
func (a *mcpAdapter) Refresh(ctx context.Context, s Session, sel Selection, cursor string) (RefreshPage, error) {
	if cursor != "" {
		return RefreshPage{}, fail("INVALID_REQUEST", "MCP resource refresh has no continuation cursor")
	}
	name, err := mcpSelected(s, sel.ID, sel.Kind)
	if err != nil {
		return RefreshPage{}, err
	}
	cs, err := connectMCP(ctx, s, nil)
	if err != nil {
		return RefreshPage{}, err
	}
	defer cs.Close()
	var content any
	text := ""
	switch sel.Kind {
	case "resource":
		res, err := cs.ReadResource(ctx, &mcp.ReadResourceParams{URI: name})
		if err != nil {
			return RefreshPage{}, fail("REMOTE_MISSING", "Selected MCP resource is inaccessible")
		}
		if len(res.Contents) == 0 || len(res.Contents) > 1000 {
			return RefreshPage{}, fail("INVALID_RESPONSE", "MCP resource content is invalid")
		}
		for _, c := range res.Contents {
			if c == nil || c.URI != name {
				return RefreshPage{}, fail("SELECTION_DENIED", "MCP returned content outside the selected resource")
			}
			if len(c.Blob) > 0 {
				return RefreshPage{}, fail("UNSUPPORTED_CONTENT", "This MCP resource is binary; select a text resource")
			}
			text += c.Text + "\n"
		}
		content = res
	case "tool":
		tool, err := mcpTool(ctx, cs, name)
		if err != nil {
			return RefreshPage{}, err
		}
		content = tool
		text = tool.Description
	default:
		return RefreshPage{}, Unsupported("MCP selection kind")
	}
	data, err := json.Marshal(content)
	if err != nil || len(data) > MaxResponseBytes || len(text) > MaxResponseBytes {
		return RefreshPage{}, fail("INVALID_RESPONSE", "MCP evidence exceeded its bound")
	}
	return RefreshPage{Items: []Snapshot{{RemoteID: sel.ID, Kind: "mcp_" + sel.Kind, Title: bounded(sel.Label, 2000), Text: text, URL: s.Account.Endpoint, RemoteRevision: remoteHash(content), Content: data}}, Complete: true, FullEnumeration: true}, nil
}
func validateMCPArguments(schema any, raw json.RawMessage) error {
	if len(raw) == 0 || len(raw) > 1<<20 {
		return fail("INVALID_REQUEST", "MCP arguments must be a bounded JSON object")
	}
	if err := mcpJSONStructure(raw); err != nil {
		return fail("INVALID_REQUEST", "MCP arguments contain repeated keys or exceed the nesting bound")
	}
	var args map[string]any
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if decoder.Decode(&args) != nil || args == nil {
		return fail("INVALID_REQUEST", "MCP arguments must be a JSON object")
	}
	schemaBytes, err := json.Marshal(schema)
	if err != nil || len(schemaBytes) > 1<<20 || mcpJSONStructure(schemaBytes) != nil {
		return fail("INVALID_RESPONSE", "MCP input schema exceeds its structural bound")
	}
	compiler := jsonschema.NewCompiler()
	compiler.DefaultDraft(jsonschema.Draft2020)
	// Disable all network resolution. Remote schema references never authorize
	// another request or a credential transfer.
	compiler.UseLoader(mcpNoSchemaLoader{})
	if compiler.AddResource("https://trove.invalid/mcp-tool-schema", schema) != nil {
		return fail("INVALID_RESPONSE", "MCP input schema is invalid")
	}
	compiled, err := compiler.Compile("https://trove.invalid/mcp-tool-schema")
	if err != nil {
		return fail("INVALID_RESPONSE", "MCP input schema is invalid or references an external document")
	}
	if compiled.Validate(args) != nil {
		return fail("INVALID_REQUEST", "Arguments do not match the selected MCP tool schema")
	}
	return nil
}

type mcpNoSchemaLoader struct{}

func (mcpNoSchemaLoader) Load(string) (any, error) {
	return nil, errors.New("external schema resolution is disabled")
}
func (a *mcpAdapter) prepareWithSession(ctx context.Context, s Session, cs *mcp.ClientSession, action Action) (Action, error) {
	if action.Kind != "mcp.tool.call" || action.Version != 1 {
		return action, Unsupported(action.Kind)
	}
	name, err := mcpSelected(s, action.SelectionID, "tool")
	if err != nil {
		return action, err
	}
	if action.RemoteID != "" && action.RemoteID != name {
		return action, fail("INVALID_REQUEST", "Reviewed MCP tool identity differs")
	}
	tool, err := mcpTool(ctx, cs, name)
	if err != nil {
		return action, err
	}
	if err = validateMCPArguments(tool.InputSchema, action.Arguments); err != nil {
		return action, err
	}
	revision := remoteHash(tool)
	if action.ExpectedRevision != "" && action.ExpectedRevision != revision {
		return action, fail("REMOTE_CHANGED", "MCP tool definition changed; review its arguments and effects again")
	}
	action.RemoteID = name
	action.ExpectedRevision = revision
	action.Effects = []string{"Call the selected MCP tool once with exactly the displayed arguments.", "Tool descriptions and annotations are untrusted; the server may change remote content or contact other services.", "The revision guard covers the tool definition. Remote record revisions are enforced only when the reviewed arguments include server-supported conditions.", "An interrupted or error result may have applied effects and will remain unknown until verified remotely."}
	return action, nil
}
func (a *mcpAdapter) Prepare(ctx context.Context, s Session, action Action) (Action, error) {
	cs, err := connectMCP(ctx, s, nil)
	if err != nil {
		return action, err
	}
	defer cs.Close()
	return a.prepareWithSession(ctx, s, cs, action)
}
func (a *mcpAdapter) Check(ctx context.Context, s Session, action Action) error {
	current, err := a.Prepare(ctx, s, action)
	if err != nil {
		return err
	}
	if string(mustMCPJSON(current)) != string(mustMCPJSON(action)) {
		return fail("REMOTE_CHANGED", "MCP reviewed arguments or effect disclosure changed")
	}
	return nil
}
func mustMCPJSON(v any) []byte { b, _ := json.Marshal(v); return b }
func (a *mcpAdapter) Send(ctx context.Context, s Session, p Proposal) (Receipt, error) {
	if err := RequireIdentity(p.Identity, s.Account.Identity); err != nil {
		return Receipt{}, err
	}
	cs, err := connectMCP(ctx, s, nil)
	if err != nil {
		return Receipt{}, err
	}
	defer cs.Close()
	action, err := a.prepareWithSession(ctx, s, cs, p.Action)
	if err != nil {
		return Receipt{}, err
	}
	if string(mustMCPJSON(action)) != string(mustMCPJSON(p.Action)) {
		return Receipt{}, fail("REMOTE_CHANGED", "MCP reviewed action changed")
	}
	if err = ctx.Err(); err != nil {
		return Receipt{}, err
	}
	result, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: action.RemoteID, Arguments: json.RawMessage(action.Arguments)})
	if err != nil || result == nil || result.IsError {
		return Receipt{}, Unknown("The MCP tool may have applied effects; inspect the remote service before any retry")
	}
	raw, err := json.Marshal(result)
	if err != nil || len(raw) > MaxResponseBytes {
		return Receipt{}, Unknown("The MCP result exceeded its bound; verify the remote outcome")
	}
	detail := "The server acknowledged the reviewed tool call. Remote output is untrusted; acknowledgement does not prove a person observed its effects."
	output := ""
	for _, content := range result.Content {
		if text, ok := content.(*mcp.TextContent); ok {
			output += text.Text + "\n"
		}
	}
	if result.StructuredContent != nil {
		output += stringify(result.StructuredContent)
	}
	if output != "" {
		if len(output) > 3000 {
			output = bounded(output, 3000) + "\n[Result excerpt shortened; full result digest retained.]"
		}
		detail += "\n\n" + output
	}
	return ReceiptFor(p, "succeeded", action.RemoteID, remoteHash(result), s.Account.Endpoint, detail), nil
}
func (a *mcpAdapter) Reconcile(_ context.Context, _ Session, p Proposal) (Receipt, error) {
	return ReceiptFor(p, "unknown", p.Action.RemoteID, "", "", "MCP has no universal operation receipt lookup. Verify the remote result explicitly; this connector will not resend the uncertain tool call."), nil
}

var _ Adapter = (*mcpAdapter)(nil)
var _ mcpauth.OAuthHandler = mcpStaticAuthorization{}

// Preview reads only the exact selected resource; it never invokes a tool or
// follows a resource URI as a network URL.
func (a *mcpAdapter) Preview(ctx context.Context, s Session, sel Selection, remoteID string) (Snapshot, error) {
	if sel.Kind != "resource" || remoteID != sel.ID {
		return Snapshot{}, fail("SELECTION_DENIED", "Preview requires the exact selected MCP resource")
	}
	page, err := a.Refresh(ctx, s, sel, "")
	if err != nil {
		return Snapshot{}, err
	}
	if len(page.Items) != 1 {
		return Snapshot{}, fail("INVALID_RESPONSE", "MCP resource preview is unavailable")
	}
	return page.Items[0], nil
}

// The ordinary JSON decoder accepts duplicate object keys. A reviewed tool
// argument must have one unambiguous value for every key, including nested keys.
func mcpJSONStructure(raw []byte) error {
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	var value func(int) error
	value = func(depth int) error {
		if depth > 64 {
			return errors.New("nesting bound")
		}
		token, err := d.Token()
		if err != nil {
			return err
		}
		delim, compound := token.(json.Delim)
		if !compound {
			return nil
		}
		switch delim {
		case '{':
			keys := map[string]bool{}
			for d.More() {
				key, err := d.Token()
				if err != nil {
					return err
				}
				name, ok := key.(string)
				if !ok || keys[name] {
					return errors.New("duplicate key")
				}
				keys[name] = true
				if err = value(depth + 1); err != nil {
					return err
				}
			}
		case '[':
			for d.More() {
				if err = value(depth + 1); err != nil {
					return err
				}
			}
		default:
			return errors.New("unexpected delimiter")
		}
		_, err = d.Token()
		return err
	}
	if err := value(0); err != nil {
		return err
	}
	if _, err := d.Token(); err != io.EOF {
		return errors.New("trailing JSON")
	}
	return nil
}

// Acquire deliberately reads a selected resource into a bounded native byte
// handoff. Remote URIs are protocol arguments, never local paths or fetch URLs.
func (a *mcpAdapter) Acquire(ctx context.Context, s Session, sel Selection, remoteID, format string) (Acquired, error) {
	if sel.Kind != "resource" || remoteID != sel.ID {
		return Acquired{}, fail("SELECTION_DENIED", "Acquisition requires the exact selected MCP resource")
	}
	name, err := mcpSelected(s, sel.ID, "resource")
	if err != nil {
		return Acquired{}, err
	}
	cs, err := connectMCP(ctx, s, nil)
	if err != nil {
		return Acquired{}, err
	}
	defer cs.Close()
	res, err := cs.ReadResource(ctx, &mcp.ReadResourceParams{URI: name})
	if err != nil {
		return Acquired{}, fail("REMOTE_MISSING", "Selected MCP resource is inaccessible")
	}
	if len(res.Contents) != 1 || res.Contents[0] == nil || res.Contents[0].URI != name {
		return Acquired{}, fail("UNSUPPORTED_CONTENT", "MCP acquisition requires one exact selected resource representation")
	}
	item := res.Contents[0]
	if len(item.Blob) > 0 && item.Text != "" {
		return Acquired{}, fail("INVALID_RESPONSE", "MCP resource has conflicting text and binary representations")
	}
	mime := item.MIMEType
	if mime == "" {
		if len(item.Blob) > 0 {
			mime = "application/octet-stream"
		} else {
			mime = "text/plain"
		}
	}
	if format != "" && format != mime {
		return Acquired{}, fail("UNSUPPORTED_CONTENT", "MCP resource does not provide the requested representation")
	}
	data := item.Blob
	if len(data) == 0 {
		data = []byte(item.Text)
	}
	if len(data) > MaxResponseBytes {
		return Acquired{}, fail("RESOURCE_EXHAUSTED", "MCP resource exceeds the acquisition bound")
	}
	return Acquired{Name: bounded(sel.Label, 255), MediaType: mime, Bytes: append([]byte{}, data...), RemoteRevision: remoteHash(res)}, nil
}

var _ Previewer = (*mcpAdapter)(nil)
var _ Acquirer = (*mcpAdapter)(nil)
