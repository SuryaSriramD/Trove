package main

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"
	"trove.local/phase0/internal/providers"
	"trove.local/phase0/internal/workspace"
)

type providerHost struct {
	mu       sync.Mutex
	bridge   *workspace.ProviderBridge
	server   *http.Server
	listener net.Listener
	url      string
}

var providerHosts sync.Map

func (a *App) stopProviders() {
	if v, ok := providerHosts.LoadAndDelete(a); ok {
		h := v.(*providerHost)
		h.bridge.Close()
		if h.server != nil {
			_ = h.server.Close()
		}
	}
}
func (a *App) providerDispatch(method string, raw json.RawMessage) string {
	a.mu.RLock()
	defer a.mu.RUnlock()
	if a.kernel == nil {
		return errorEnvelope("WORKSPACE_LOCKED", "No workspace open")
	}
	var r struct {
		WorkspaceID     string   `json:"workspaceId"`
		ContractVersion int      `json:"contractVersion"`
		Provider        string   `json:"provider"`
		PageIDs         []string `json:"pageIds"`
		Tools           []string `json:"tools"`
		ConnectionID    string   `json:"connectionId"`
		ProposalID      string   `json:"proposalId"`
		Objective       string   `json:"objective"`
		Result          string   `json:"result"`
		Pending         string   `json:"pending"`
	}
	if json.Unmarshal(raw, &r) != nil || r.WorkspaceID != a.kernel.ID() || r.ContractVersion != 1 {
		return errorEnvelope("INVALID_REQUEST", "Invalid provider request")
	}
	ctx := a.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	v, _ := providerHosts.LoadOrStore(a, &providerHost{bridge: workspace.NewProviderBridge(a.kernel, a.execution)})
	h := v.(*providerHost)
	var result any
	var err error
	switch method {
	case "providers.status":
		h.mu.Lock()
		url := h.url
		h.mu.Unlock()
		result = map[string]any{"connections": h.bridge.Status(), "capabilities": []providers.Capabilities{providers.NewClaudeCode().ProbeCapabilities(), providers.NewCodex().ProbeCapabilities()}, "url": url, "remoteEnabled": false}
	case "providers.preview":
		result, err = h.bridge.PreviewContext(ctx, r.PageIDs)
	case "providers.connect":
		var connection workspace.ProviderConnection
		connection, err = h.bridge.Connect(ctx, r.Provider, r.PageIDs, r.Tools)
		if err == nil {
			h.mu.Lock()
			if h.listener == nil {
				h.listener, err = net.Listen("tcp4", "127.0.0.1:0")
				if err == nil {
					h.url = "http://" + h.listener.Addr().String() + "/mcp"
					h.server = &http.Server{Handler: h.bridge.MCPHandler(), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 30 * time.Second, IdleTimeout: 30 * time.Second, MaxHeaderBytes: 8192}
					go h.server.Serve(h.listener)
				}
			}
			url := h.url
			h.mu.Unlock()
			if err != nil {
				_ = h.bridge.Disconnect(ctx, connection.ID)
			}
			exe, _ := os.Executable()
			result = map[string]any{"connection": connection, "url": url, "relayPath": filepath.Join(filepath.Dir(exe), "trove-mcp")}
		}
	case "providers.disconnect":
		err = h.bridge.Disconnect(ctx, r.ConnectionID)
		result = map[string]any{"state": "disconnected"}
	case "providers.proposals":
		result, err = h.bridge.Proposals(ctx)
	case "providers.approve":
		var response json.RawMessage
		response, err = h.bridge.Approve(ctx, r.ProposalID)
		if err == nil {
			return string(response)
		}
	case "providers.reject":
		err = h.bridge.Reject(ctx, r.ProposalID)
		result = map[string]any{"state": "denied"}
	case "providers.handoff":
		result, err = h.bridge.Handoff(ctx, r.ConnectionID, r.Objective, r.Result, r.Pending)
	default:
		err = workspace.Fail("INVALID_REQUEST", "Unknown provider operation")
	}
	if err != nil {
		var f *workspace.ServiceFailure
		if errors.As(err, &f) {
			return errorEnvelope(f.Code, f.Message)
		}
		return errorEnvelope("INTERNAL", "Provider operation failed")
	}
	out, _ := json.Marshal(map[string]any{"ok": true, "result": result})
	return string(out)
}
