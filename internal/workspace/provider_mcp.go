package workspace

import (
	"context"
	"encoding/json"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"net/http"
	"strings"
	"time"
	"trove.local/phase0/contracts"
)

// A paired connection owns its SDK server and transports. Sessions from one
// connection can never select another connection's tool handlers.
type providerMCP struct {
	server          *mcp.Server
	legacy, current *mcp.StreamableHTTPHandler
	calls           chan struct{}
}

func (p *providerMCP) close() {
	for session := range p.server.Sessions() {
		_ = session.Close()
	}
}
func (b *ProviderBridge) pairedMCP(c *providerConnection) *providerMCP {
	b.mu.Lock()
	defer b.mu.Unlock()
	live := b.connections[c.info.ID]
	if live == nil || live.info.State != "connected" {
		return nil
	}
	if live.mcp != nil {
		return live.mcp
	}
	p := &providerMCP{calls: make(chan struct{}, 32)}
	p.server = mcp.NewServer(&mcp.Implementation{Name: "Trove", Version: "0.5.0"}, &mcp.ServerOptions{Instructions: "Only explicitly selected Trove context is accessible. Submit named create/update actions with proposeActions or existing scoped edits with proposeChanges. A person approves them in Trove. Creating a child does not share that new page with this connection."})
	for _, name := range c.info.Tools {
		p.server.AddTool(&mcp.Tool{Name: name, Description: "Scoped Trove workspace service: " + name, InputSchema: mcpInputSchema(name), Annotations: &mcp.ToolAnnotations{ReadOnlyHint: name != "proposeActions" && name != "proposeChanges", DestructiveHint: new(false), OpenWorldHint: new(false)}}, func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			select {
			case p.calls <- struct{}{}:
				defer func() { <-p.calls }()
			default:
				return mcpToolError("RESOURCE_EXHAUSTED: at most 32 concurrent calls"), nil
			}
			var args map[string]any
			if json.Unmarshal(req.Params.Arguments, &args) != nil || args == nil {
				return mcpToolError("INVALID_REQUEST: tool arguments must be an object"), nil
			}
			result, err := b.Call(ctx, c.info.ID, c.token, name, args)
			if err != nil {
				return mcpToolError(err.Error()), nil
			}
			var envelope map[string]any
			if err = json.Unmarshal(result, &envelope); err != nil {
				return mcpToolError("INTERNAL: invalid service result"), nil
			}
			return &mcp.CallToolResult{IsError: envelope["ok"] == false, Content: []mcp.Content{&mcp.TextContent{Text: string(result)}}, StructuredContent: envelope}, nil
		})
	}
	get := func(*http.Request) *mcp.Server {
		count := 0
		for range p.server.Sessions() {
			count++
		}
		if count >= 32 {
			return nil
		}
		return p.server
	}
	p.legacy = mcp.NewStreamableHTTPHandler(get, &mcp.StreamableHTTPOptions{JSONResponse: true, SessionTimeout: 5 * time.Minute, MaxRequestBodyBytes: 1 << 20})
	p.current = mcp.NewStreamableHTTPHandler(get, &mcp.StreamableHTTPOptions{Stateless: true, JSONResponse: true, MaxRequestBodyBytes: 1 << 20, PropagateRequestCancellation: true})
	live.mcp = p
	return p
}
func mcpToolError(message string) *mcp.CallToolResult {
	return &mcp.CallToolResult{IsError: true, Content: []mcp.Content{&mcp.TextContent{Text: message}}}
}

// MCPHandler delegates protocol negotiation, validation, sessions and cancellation
// to the pinned official SDK. The shell alone chooses loopback/TLS listeners.
func (b *ProviderBridge) MCPHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		if r.Header.Get("Origin") != "" {
			http.Error(w, "Browser origins are not permitted", http.StatusForbidden)
			return
		}
		authorization := r.Header.Get("Authorization")
		if !strings.HasPrefix(authorization, "Bearer ") {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		c, err := b.authenticate(r.Header.Get("Trove-Connection-Id"), strings.TrimPrefix(authorization, "Bearer "))
		if err == nil {
			err = b.kernel.authorize(c.execution)
		}
		if err != nil {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		if !validateMCPHTTPIDs(w, r) {
			return
		}
		p := b.pairedMCP(c)
		if p == nil {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		if r.Header.Get("MCP-Protocol-Version") >= "2026-07-28" {
			p.current.ServeHTTP(w, r)
		} else {
			p.legacy.ServeHTTP(w, r)
		}
	})
}
func mcpInputSchema(name string) any {
	props := map[string]any{}
	required := []string{}
	text := map[string]any{"type": "string"}
	switch name {
	case "readPage":
		props["pageId"] = text
		required = []string{"pageId"}
	case "queryRecords":
		props["collectionId"] = text
		props["viewId"] = text
		props["cursor"] = text
		props["limit"] = map[string]any{"type": "integer", "minimum": 1, "maximum": 200}
		required = []string{"collectionId"}
	case "search.query":
		props["query"] = text
		props["mode"] = map[string]any{"type": "string", "enum": []string{"keyword", "hybrid"}}
		props["limit"] = map[string]any{"type": "integer", "minimum": 1, "maximum": 100}
		required = []string{"query"}
	case "resolveCitation":
		props["location"] = map[string]any{"type": "object"}
		required = []string{"location"}
	case "allocateIds":
		props["count"] = map[string]any{"type": "integer", "minimum": 1, "maximum": 1000}
		required = []string{"count"}
	case "proposeActions":
		plan, err := contracts.InlineDefinition("service", "ActionPlan")
		if err != nil {
			return map[string]any{"not": map[string]any{}}
		}
		props["idempotencyKey"] = map[string]any{"type": "string", "minLength": 1, "maxLength": 80}
		props["plan"] = plan
		required = []string{"idempotencyKey", "plan"}
	case "proposeChanges":
		props["idempotencyKey"] = text
		props["commands"] = map[string]any{"type": "array", "maxItems": 256, "items": map[string]any{"type": "object"}}
		props["expectedRevisions"] = map[string]any{"type": "array", "items": map[string]any{"type": "object"}}
		required = []string{"idempotencyKey", "commands", "expectedRevisions"}
	}
	return map[string]any{"type": "object", "properties": props, "required": required, "additionalProperties": false}
}
