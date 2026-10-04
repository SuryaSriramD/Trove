package main

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"
	"trove.local/phase0/contracts"
	"trove.local/phase0/internal/providers/auth"
	"trove.local/phase0/internal/workspace"
)

type providerAuthState struct {
	mu      sync.Mutex
	manager *auth.Manager
}

var providerAuthStates sync.Map

func (a *App) getProviderAuth() (*auth.Manager, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return nil, errors.New("Account storage location is unavailable")
	}
	root := filepath.Join(base, "Trove", "provider-auth")
	a.mu.RLock()
	k := a.kernel
	a.mu.RUnlock()
	if k == nil {
		return nil, errors.New("Open a workspace before managing accounts")
	}
	// Workspace selection is configurable; never let that selection encompass the
	// fixed host credential metadata directory (or vice versa).
	if pathsOverlap(root, k.Path()) {
		return nil, errors.New("Account storage must be outside the workspace")
	}
	value, _ := providerAuthStates.LoadOrStore(a, &providerAuthState{})
	state := value.(*providerAuthState)
	state.mu.Lock()
	defer state.mu.Unlock()
	if state.manager == nil {
		state.manager, err = auth.NewManager(root)
	}
	return state.manager, err
}

// Called after managed runs stop, during workspace close and application shutdown.
func (a *App) stopProviderAuth() {
	if value, ok := providerAuthStates.LoadAndDelete(a); ok {
		state := value.(*providerAuthState)
		state.mu.Lock()
		defer state.mu.Unlock()
		if state.manager != nil {
			_ = state.manager.Close()
			state.manager = nil
		}
	}
}
func (a *App) providerAuthDispatch(method string, raw json.RawMessage) string {
	definitions := map[string]string{"providers.auth.list": "ProviderAuthListRequest", "providers.auth.begin": "ProviderAuthBeginRequest", "providers.auth.attempt": "ProviderAuthAttemptRequest", "providers.auth.cancel": "ProviderAuthAttemptRequest", "providers.auth.retry": "ProviderAuthAttemptRequest", "providers.auth.signOut": "ProviderAuthSignOutRequest"}
	definition, ok := definitions[method]
	if !ok || contracts.Validate("service", definition, raw) != nil {
		return errorEnvelope("INVALID_REQUEST", "Invalid account operation")
	}
	var request struct {
		WorkspaceID    string `json:"workspaceId"`
		RegistrationID string `json:"registrationId"`
		AttemptID      string `json:"attemptId"`
	}
	if json.Unmarshal(raw, &request) != nil {
		return errorEnvelope("INVALID_REQUEST", "Invalid account operation")
	}
	a.mu.RLock()
	k, e, ctx := a.kernel, a.execution, a.ctx
	a.mu.RUnlock()
	if k == nil || request.WorkspaceID != k.ID() {
		return errorEnvelope("WORKSPACE_LOCKED", "Account operation belongs to another workspace")
	}
	if k.AuthorizeProviderAuth(e) != nil {
		return errorEnvelope("SCOPE_DENIED", "Only the current workspace owner can manage accounts")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	success := func(result any) string {
		b, _ := json.Marshal(map[string]any{"ok": true, "result": result})
		return string(b)
	}
	manager, err := a.getProviderAuth()
	if err != nil {
		if method == "providers.auth.list" {
			return success(map[string]any{"available": false, "reason": "Protected account storage is unavailable on this host", "registrations": []auth.Registration{}})
		}
		return errorEnvelope("WORKER_FAILED", "Protected account storage is unavailable on this host")
	}
	var result any
	openURL := func(target string) error { runtime.BrowserOpenURL(ctx, target); return nil }
	switch method {
	case "providers.auth.list":
		var list []auth.Registration
		list, err = manager.List()
		result = map[string]any{"available": true, "reason": "", "registrations": list}
	case "providers.auth.begin":
		result, err = manager.Begin(ctx, request.RegistrationID, openURL)
	case "providers.auth.attempt":
		result, err = manager.Attempt(request.AttemptID)
	case "providers.auth.cancel":
		err = manager.Cancel(request.AttemptID)
		result = map[string]any{"state": "cancelled"}
	case "providers.auth.retry":
		result, err = manager.Retry(ctx, request.AttemptID, openURL)
	case "providers.auth.signOut":
		if value, ok := providerHosts.Load(a); ok {
			h := value.(*providerHost)
			h.stopMatchingManaged(func(run workspace.ManagedRun) bool { return run.Account == request.RegistrationID })
			h.mu.Lock()
			delete(h.models, request.RegistrationID)
			h.mu.Unlock()
		}
		signoutCtx, cancel := context.WithTimeout(ctx, 35*time.Second)
		defer cancel()
		result, err = manager.SignOut(signoutCtx, request.RegistrationID)
	}
	if err != nil {
		var safe *auth.Error
		if errors.As(err, &safe) {
			return errorEnvelope("WORKER_FAILED", safe.Message)
		}
		return errorEnvelope("WORKER_FAILED", "Account operation failed")
	}
	return success(result)
}
