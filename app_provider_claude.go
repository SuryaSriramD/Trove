package main

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"time"

	"trove.local/phase0/contracts"
	"trove.local/phase0/internal/providers"
	"trove.local/phase0/internal/workspace"
)

type providerClaudeState struct {
	mu      sync.Mutex
	manager *providers.ClaudeProfiles
}

var providerClaudeStates sync.Map

func (a *App) getProviderClaude() (*providers.ClaudeProfiles, error) {
	root, err := providers.DefaultClaudeProfilesRoot()
	if err != nil {
		return nil, err
	}
	a.mu.RLock()
	k := a.kernel
	a.mu.RUnlock()
	if k == nil {
		return nil, errors.New("Open a workspace before managing Claude profiles")
	}
	if pathsOverlap(root, k.Path()) {
		return nil, errors.New("Claude account state must stay outside the workspace")
	}
	value, _ := providerClaudeStates.LoadOrStore(a, &providerClaudeState{})
	state := value.(*providerClaudeState)
	state.mu.Lock()
	defer state.mu.Unlock()
	if state.manager == nil {
		state.manager, err = providers.NewClaudeProfiles(root)
	}
	return state.manager, err
}
func (a *App) stopProviderClaude() {
	if value, ok := providerClaudeStates.LoadAndDelete(a); ok {
		state := value.(*providerClaudeState)
		state.mu.Lock()
		defer state.mu.Unlock()
		if state.manager != nil {
			_ = state.manager.Close()
			state.manager = nil
		}
	}
}
func (a *App) resolveClaudeProfile(ctx context.Context, profileID, expectedBilling string, runtime providers.Runtime) (providers.ClaudeConfig, string, error) {
	manager, err := a.getProviderClaude()
	if err != nil {
		return providers.ClaudeConfig{}, "", err
	}
	return manager.Resolve(ctx, runtime, profileID, expectedBilling)
}
func (a *App) providerClaudeDispatch(method string, raw json.RawMessage) string {
	definitions := map[string]string{"providers.claude.list": "ProviderClaudeListRequest", "providers.claude.create": "ProviderClaudeCreateRequest", "providers.claude.status": "ProviderClaudeProfileRequest", "providers.claude.login": "ProviderClaudeProfileRequest", "providers.claude.cancel": "ProviderClaudeProfileRequest", "providers.claude.logout": "ProviderClaudeProfileRequest"}
	definition := definitions[method]
	if definition == "" || contracts.Validate("service", definition, raw) != nil {
		return errorEnvelope("INVALID_REQUEST", "Invalid Claude profile operation")
	}
	var req struct {
		WorkspaceID string `json:"workspaceId"`
		ProfileID   string `json:"profileId"`
		Label       string `json:"label"`
		BillingMode string `json:"billingMode"`
	}
	_ = json.Unmarshal(raw, &req)
	a.mu.RLock()
	k, e, ctx := a.kernel, a.execution, a.ctx
	a.mu.RUnlock()
	if k == nil || req.WorkspaceID != k.ID() {
		return errorEnvelope("WORKSPACE_LOCKED", "Claude profile operation belongs to another workspace")
	}
	if k.AuthorizeProviderAuth(e) != nil {
		return errorEnvelope("SCOPE_DENIED", "Only the workspace owner can manage Claude profiles")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	success := func(result any) string {
		b, _ := json.Marshal(map[string]any{"ok": true, "result": result})
		return string(b)
	}
	manager, err := a.getProviderClaude()
	if err != nil {
		if method == "providers.claude.list" {
			return success(map[string]any{"available": false, "reason": err.Error(), "profiles": []providers.ClaudeProfile{}})
		}
		return errorEnvelope("RUNTIME_UNAVAILABLE", err.Error())
	}
	var result any
	switch method {
	case "providers.claude.list":
		var profiles []providers.ClaudeProfile
		profiles, err = manager.List()
		result = map[string]any{"available": true, "reason": "", "profiles": profiles}
	case "providers.claude.create":
		result, err = manager.Create(req.Label, req.BillingMode)
	case "providers.claude.cancel":
		result, err = manager.Cancel(req.ProfileID)
	default:
		bounded, cancel := context.WithTimeout(ctx, 35*time.Second)
		defer cancel()
		r := providers.Discover(bounded, "claude_code")
		if !r.Available {
			return errorEnvelope("RUNTIME_UNAVAILABLE", r.Reason)
		}
		if method == "providers.claude.login" || method == "providers.claude.logout" {
			// Login can change identity; stop any run before changing its vendor state.
			if value, ok := providerHosts.Load(a); ok {
				value.(*providerHost).stopMatchingManaged(func(run workspace.ManagedRun) bool {
					return run.Provider == "claude_code" && run.Account == req.ProfileID
				})
			}
		}
		switch method {
		case "providers.claude.status":
			result, err = manager.Status(bounded, r, req.ProfileID)
		case "providers.claude.login":
			result, err = manager.Login(bounded, r, req.ProfileID)
		case "providers.claude.logout":
			result, err = manager.Logout(bounded, r, req.ProfileID)
		}
	}
	if err != nil {
		return errorEnvelope("WORKER_FAILED", err.Error())
	}
	return success(result)
}
