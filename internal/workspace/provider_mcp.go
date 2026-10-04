package workspace

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"sync"
	"trove.local/phase0/contracts"
)

// MCPHandler is a bounded JSON-response Streamable HTTP endpoint. The shell
// binds it only to loopback; remote deployment needs a separately qualified
// OAuth transport. Browser origins are denied and no CORS headers are emitted.
func (b *ProviderBridge) MCPHandler() http.Handler {
	var mu sync.Mutex
	active := map[string]context.CancelFunc{}
	requestKey := func(connection string, raw json.RawMessage) string {
		var value any
		decoder := json.NewDecoder(bytes.NewReader(raw))
		decoder.UseNumber()
		if decoder.Decode(&value) != nil {
			return ""
		}
		switch value.(type) {
		case string, json.Number:
			return connection + ":" + string(marshal(value))
		default:
			return ""
		}
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Header.Get("Origin") != "" {
			http.Error(w, "Browser origins are not permitted", http.StatusForbidden)
			return
		}
		id := r.Header.Get("Trove-Connection-Id")
		token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		c, err := b.authenticate(id, token)
		if err == nil {
			err = b.kernel.authorize(c.execution)
		}
		if err != nil {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		if r.Method == http.MethodDelete {
			if err = b.Disconnect(r.Context(), id); err != nil {
				http.Error(w, "Disconnect failed", 500)
				return
			}
			w.WriteHeader(204)
			return
		}
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		var req struct {
			JSONRPC string          `json:"jsonrpc"`
			ID      json.RawMessage `json:"id"`
			Method  string          `json:"method"`
			Params  json.RawMessage `json:"params"`
		}
		raw, err := io.ReadAll(io.LimitReader(r.Body, 1024*1024+1))
		if err != nil || len(raw) > 1024*1024 {
			http.Error(w, "Request too large", 413)
			return
		}
		if json.Unmarshal(raw, &req) != nil || req.JSONRPC != "2.0" {
			http.Error(w, "Invalid JSON-RPC request", 400)
			return
		}
		reply := func(value any) {
			_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": req.ID, "result": value})
		}
		fail := func(code int, msg string) {
			_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": req.ID, "error": map[string]any{"code": code, "message": msg}})
		}
		if len(req.ID) == 0 {
			if req.Method == "notifications/cancelled" {
				var params struct {
					RequestID json.RawMessage `json:"requestId"`
				}
				if json.Unmarshal(req.Params, &params) != nil {
					w.WriteHeader(400)
					return
				}
				key := requestKey(id, params.RequestID)
				mu.Lock()
				cancel := active[key]
				mu.Unlock()
				if cancel != nil {
					cancel()
				}
				w.WriteHeader(202)
				return
			}
			if req.Method == "notifications/initialized" {
				w.WriteHeader(202)
				return
			}
			w.WriteHeader(400)
			return
		}
		if requestKey(id, req.ID) == "" {
			fail(-32600, "Request ID must be a string or number")
			return
		}
		switch req.Method {
		case "initialize":
			w.Header().Set("Mcp-Session-Id", id)
			reply(map[string]any{"protocolVersion": "2025-03-26", "capabilities": map[string]any{"tools": map[string]any{"listChanged": false}}, "serverInfo": map[string]any{"name": "Trove", "version": "0.5.0"}, "instructions": "Only explicitly selected Trove context is accessible. Submit named create/update actions with proposeActions or existing scoped edits with proposeChanges; a person approves them in Trove. Creating a child does not share that new page with this connection."})
		case "ping":
			reply(map[string]any{})
		case "tools/list":
			tools := []map[string]any{}
			for _, name := range c.info.Tools {
				tools = append(tools, map[string]any{"name": name, "description": "Scoped Trove workspace service: " + name, "inputSchema": mcpInputSchema(name), "annotations": map[string]any{"readOnlyHint": name != "proposeChanges" && name != "proposeActions", "destructiveHint": false, "openWorldHint": false}})
			}
			reply(map[string]any{"tools": tools})
		case "tools/call":
			var p struct {
				Name      string         `json:"name"`
				Arguments map[string]any `json:"arguments"`
			}
			if json.Unmarshal(req.Params, &p) != nil || p.Arguments == nil {
				fail(-32602, "Invalid tool arguments")
				return
			}
			callContext, cancel := context.WithCancel(r.Context())
			key := requestKey(id, req.ID)
			mu.Lock()
			_, duplicate := active[key]
			if !duplicate {
				active[key] = cancel
			}
			mu.Unlock()
			if duplicate {
				cancel()
				fail(-32600, "Request ID is already active")
				return
			}
			defer func() { cancel(); mu.Lock(); delete(active, key); mu.Unlock() }()
			result, callErr := b.Call(callContext, id, token, p.Name, p.Arguments)
			if callErr != nil {
				reply(map[string]any{"isError": true, "content": []any{map[string]any{"type": "text", "text": callErr.Error()}}})
				return
			}
			var envelope map[string]any
			json.Unmarshal(result, &envelope)
			reply(map[string]any{"isError": envelope["ok"] == false, "content": []any{map[string]any{"type": "text", "text": string(result)}}})
		default:
			fail(-32601, "Method not supported")
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
	case "search.query":
		props["query"] = text
		props["mode"] = map[string]any{"type": "string", "enum": []string{"keyword", "semantic", "hybrid"}}
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
