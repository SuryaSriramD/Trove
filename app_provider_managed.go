package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"trove.local/phase0/contracts"
	"trove.local/phase0/internal/providers"
	"trove.local/phase0/internal/workspace"
)

type managedExecution struct {
	run    workspace.ManagedRun
	cancel context.CancelFunc
	done   chan struct{}
}

type accountModels struct {
	models  []providers.AccountModel
	expires time.Time
}

func (h *providerHost) stopMatchingManaged(matches func(workspace.ManagedRun) bool) {
	h.mu.Lock()
	active := h.managed
	if active != nil && matches(active.run) {
		active.cancel()
	} else {
		active = nil
	}
	h.mu.Unlock()
	if active != nil {
		<-active.done
	}
}

func (h *providerHost) stopManaged() {
	h.mu.Lock()
	active := h.managed
	if active != nil {
		active.cancel()
	}
	h.mu.Unlock()
	if active != nil {
		<-active.done
	}
}

func providerScopeHash(workspaceID string, pages, tools []string) string {
	pages = append([]string(nil), pages...)
	tools = append([]string(nil), tools...)
	sort.Strings(pages)
	sort.Strings(tools)
	raw, _ := json.Marshal([]any{workspaceID, pages, tools, providers.ContainmentPolicy})
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func (a *App) managedProviderDispatch(method string, raw json.RawMessage) string {
	definition := map[string]string{"providers.models": "ProviderModelsRequest", "providers.discover": "ProviderDiscoverRequest", "providers.runs": "ProviderManagedStatusRequest", "providers.start": "ProviderManagedStartRequest", "providers.cancel": "ProviderManagedCancelRequest"}[method]
	if definition == "" || contracts.Validate("service", definition, raw) != nil {
		return errorEnvelope("INVALID_REQUEST", "Invalid managed provider request")
	}
	var req struct {
		WorkspaceID    string   `json:"workspaceId"`
		Provider       string   `json:"provider"`
		RegistrationID string   `json:"registrationId"`
		BillingMode    string   `json:"billingMode"`
		Model          string   `json:"model"`
		Prompt         string   `json:"prompt"`
		PageIDs        []string `json:"pageIds"`
		Tools          []string `json:"tools"`
		PreviewHash    string   `json:"previewHash"`
		ResumeRunID    string   `json:"resumeRunId"`
		RunID          string   `json:"runId"`
	}
	_ = json.Unmarshal(raw, &req)
	// Lifecycle is held by Dispatch. Validate the requesting workspace and owner
	// before opening protected storage or refreshing any selected account token.
	a.mu.RLock()
	k, execution, ctx := a.kernel, a.execution, a.ctx
	a.mu.RUnlock()
	if k == nil || req.WorkspaceID != k.ID() {
		return errorEnvelope("WORKSPACE_LOCKED", "No matching workspace is open")
	}
	if k.AuthorizeProviderAuth(execution) != nil {
		return errorEnvelope("SCOPE_DENIED", "Only the workspace owner can run providers")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	v, _ := providerHosts.LoadOrStore(a, &providerHost{bridge: workspace.NewProviderBridge(k, execution)})
	h := v.(*providerHost)
	var selectedRuntime providers.Runtime
	if method == "providers.start" {
		if req.Provider == "codex" && req.BillingMode != "chatgpt_plan" {
			return errorEnvelope("INVALID_REQUEST", "Choose ChatGPT plan billing for Codex")
		}
		if req.Provider == "claude_code" && (req.BillingMode != "claude_subscription" && req.BillingMode != "anthropic_api" || !regexp.MustCompile(`^claude-[A-Za-z0-9][A-Za-z0-9._-]{1,120}$`).MatchString(req.Model)) {
			return errorEnvelope("INVALID_REQUEST", "Choose the Claude profile billing route and an explicit full Claude model identifier")
		}
		h.mu.Lock()
		if h.managed != nil || h.starting {
			h.mu.Unlock()
			return errorEnvelope("CONFLICT", "Another provider run is active; cancel or finish it first")
		}
		h.starting = true
		selectedRuntime = h.runtimes[req.Provider]
		catalog := h.models[req.RegistrationID]
		h.mu.Unlock()
		defer func() { h.mu.Lock(); h.starting = false; h.mu.Unlock() }()
		if !selectedRuntime.Available {
			return errorEnvelope("RUNTIME_UNAVAILABLE", "Discover a supported native runtime first")
		}
		if req.Provider == "codex" {
			selected := false
			for _, model := range catalog.models {
				if model.ID == req.Model {
					selected = true
				}
			}
			if !selected || !time.Now().Before(catalog.expires) {
				return errorEnvelope("CONFLICT", "Load this account’s model list and explicitly select an available model")
			}
		}
	}
	// Host credential/profile lookup may briefly acquire App.mu itself.
	var accessToken, accountLabel string
	var identity providers.Identity
	var claudeConfiguration providers.ClaudeConfig
	if method == "providers.models" || method == "providers.start" && req.Provider == "codex" {
		manager, err := a.getProviderAuth()
		if err != nil {
			return errorEnvelope("AUTH_REQUIRED", err.Error())
		}
		authCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		token, err := manager.AccessToken(authCtx, req.RegistrationID)
		cancel()
		if err != nil {
			return errorEnvelope("AUTH_REQUIRED", err.Error())
		}
		accessToken = token.Value
		identity = providers.Identity{Provider: "codex", Account: token.Identity.RegistrationID, AuthMode: "siwc", BillingMode: "chatgpt_plan", AdapterVersion: "2"}
		accountLabel = token.Identity.Email
		if accountLabel == "" {
			accountLabel = token.Identity.RegistrationID
		}
	} else if method == "providers.start" && req.Provider == "claude_code" {
		var err error
		claudeConfiguration, accountLabel, err = a.resolveClaudeProfile(ctx, req.RegistrationID, req.BillingMode, selectedRuntime)
		if err != nil {
			return errorEnvelope("AUTH_REQUIRED", err.Error())
		}
		identity = claudeConfiguration.Identity
	}
	result := any(nil)
	var err error
	switch method {
	case "providers.models":
		var models []providers.AccountModel
		models, err = providers.ChatGPTModels(ctx, accessToken)
		if err == nil {
			h.mu.Lock()
			if h.models == nil {
				h.models = map[string]accountModels{}
			}
			h.models[req.RegistrationID] = accountModels{models, time.Now().Add(15 * time.Minute)}
			h.mu.Unlock()
			result = map[string]any{"registrationId": req.RegistrationID, "models": models}
		}
	case "providers.discover":
		r := providers.Discover(ctx, req.Provider)
		if req.Provider == "claude_code" {
			r = providers.QualifyClaudeRuntime(ctx, r)
		}
		h.mu.Lock()
		if h.runtimes == nil {
			h.runtimes = map[string]providers.Runtime{}
		}
		h.runtimes[req.Provider] = r
		h.mu.Unlock()
		result = r
	case "providers.runs":
		var runs []workspace.ManagedRun
		runs, err = k.ManagedRuns(ctx, execution)
		h.mu.Lock()
		for i := range runs {
			if saved, ok := h.unsaved[runs[i].ID]; ok {
				runs[i] = saved
			}
		}
		active := ""
		if h.managed != nil {
			active = h.managed.run.ID
			for i := range runs {
				if runs[i].ID == active {
					runs[i] = h.managed.run
				}
			}
		}
		runtimeStates := []providers.Runtime{}
		for _, r := range h.runtimes {
			runtimeStates = append(runtimeStates, r)
		}
		h.mu.Unlock()
		result = map[string]any{"runs": runs, "activeRunId": active, "runtimes": runtimeStates}
	case "providers.cancel":
		h.mu.Lock()
		if h.managed == nil || h.managed.run.ID != req.RunID {
			err = errors.New("No matching active provider run")
		} else {
			h.managed.cancel()
		}
		h.mu.Unlock()
		result = map[string]string{"state": "cancellation_requested"}
	case "providers.start":
		r := selectedRuntime
		identity.RuntimeVersion = r.Version
		var manifest any
		manifest, err = h.bridge.PreviewContext(ctx, req.PageIDs)
		if err != nil {
			break
		}
		manifestMap := manifest.(map[string]any)
		if manifestMap["hash"] != req.PreviewHash {
			return errorEnvelope("REVISION_CONFLICT", "Selected content changed; preview the shared context again")
		}
		manifestJSON, marshalErr := json.Marshal(manifest)
		limit := 256 << 10
		if req.Provider == "claude_code" {
			limit = 64 << 10
		}
		if marshalErr != nil || len(manifestJSON) > limit {
			return errorEnvelope("RESOURCE_EXHAUSTED", "Selected context exceeds this provider’s input limit; select fewer pages and review again")
		}
		scopeHash := providerScopeHash(k.ID(), req.PageIDs, req.Tools)
		var checkpoint *providers.ManagedCheckpoint
		sessionID := workspace.NewID()
		if req.ResumeRunID != "" {
			var previous []workspace.ManagedRun
			previous, err = k.ManagedRuns(ctx, execution)
			if err != nil {
				break
			}
			for _, old := range previous {
				if old.ID == req.ResumeRunID && old.Account == req.RegistrationID && old.Provider == req.Provider {
					checkpoint = old.Checkpoint
					sessionID = old.SessionID
					break
				}
			}
			if checkpoint == nil || sessionID == "" {
				return errorEnvelope("CONFLICT", "That run has no compatible saved runtime session")
			}
			expected := providers.ManagedCheckpoint{Version: 1, Identity: identity, ScopeHash: scopeHash, Policy: providers.ContainmentPolicy, RuntimeSHA256: r.SHA256, Model: req.Model, ThreadID: checkpoint.ThreadID}
			if *checkpoint != expected {
				return errorEnvelope("CONFLICT", "Resume requires the same account, billing, model, runtime and page/tool scope")
			}
		}
		base, e := os.UserConfigDir()
		if e != nil {
			err = e
			break
		}
		managedRoot := filepath.Join(base, "Trove", "providers", "managed")
		if pathsOverlap(managedRoot, k.Path()) {
			return errorEnvelope("SCOPE_DENIED", "Provider runtime state must be outside the workspace")
		}
		var connection workspace.ProviderConnection
		connection, err = h.bridge.ConnectManaged(ctx, req.Provider, identity.Account, identity.AuthMode, identity.BillingMode, req.PageIDs, req.Tools)
		if err != nil {
			break
		}
		var specs []providers.ToolSpec
		specs, err = h.bridge.ManagedTools(connection.ID, connection.Token)
		if err != nil {
			_ = h.bridge.Disconnect(ctx, connection.ID)
			break
		}
		configuration := providers.CodexConfig{Runtime: r, StateDirectory: filepath.Join(managedRoot, k.ID(), sessionID), Identity: identity, ScopeHash: scopeHash, Model: req.Model, AccessToken: accessToken, Tools: specs}
		configuration.ContextManifest = manifestJSON
		if req.Provider == "claude_code" {
			claudeConfiguration.SessionDirectory = filepath.Join(claudeConfiguration.StateDirectory, "sessions", sessionID)
			claudeConfiguration.ScopeHash, claudeConfiguration.Model = scopeHash, req.Model
			claudeConfiguration.ConnectionID, claudeConfiguration.Token = connection.ID, connection.Token
			claudeConfiguration.Tools, claudeConfiguration.ContextManifest = specs, manifestJSON
			claudeConfiguration.BridgeURL, err = h.ensureLocalServer()
			if err != nil {
				_ = h.bridge.Disconnect(ctx, connection.ID)
				break
			}
		}
		run := workspace.ManagedRun{ID: workspace.NewID(), SessionID: sessionID, ConnectionID: connection.ID, Provider: req.Provider, Account: req.RegistrationID, AccountLabel: accountLabel, BillingMode: identity.BillingMode, Model: req.Model, State: "starting", Prompt: req.Prompt, PageIDs: req.PageIDs, Tools: req.Tools, Manifest: manifestJSON, CreatedAt: time.Now().UTC().Format(time.RFC3339Nano)}
		if err = k.SaveManagedRun(ctx, execution, run); err != nil {
			_ = h.bridge.Disconnect(ctx, connection.ID)
			break
		}
		runCtx, cancel := context.WithCancel(ctx)
		active := &managedExecution{run: run, cancel: cancel, done: make(chan struct{})}
		h.mu.Lock()
		if h.managed != nil {
			h.mu.Unlock()
			cancel()
			_ = h.bridge.Disconnect(ctx, connection.ID)
			return errorEnvelope("CONFLICT", "Another provider run started")
		}
		h.managed = active
		h.mu.Unlock()
		go func() {
			defer close(active.done)
			defer cancel()
			h.mu.Lock()
			active.run.State = "running"
			h.mu.Unlock()
			emit := func(event providers.Event) error {
				h.mu.Lock()
				defer h.mu.Unlock()
				switch event.Kind {
				case "text":
					if len(active.run.Text)+len(event.Text) > 1<<20 {
						return errors.New("Provider answer exceeds 1 MiB")
					}
					active.run.Text += event.Text
				case "message":
					if len(event.Text) > 1<<20 {
						return errors.New("Provider answer exceeds 1 MiB")
					}
					active.run.Text = event.Text
				}
				return nil
			}
			var finalCheckpoint providers.ManagedCheckpoint
			var runErr error
			if req.Provider == "claude_code" {
				finalCheckpoint, runErr = providers.RunClaude(runCtx, claudeConfiguration, req.Prompt, checkpoint, emit)
			} else {
				finalCheckpoint, runErr = providers.RunCodex(runCtx, configuration, req.Prompt, checkpoint, func(callCtx context.Context, tool string, args map[string]any) (json.RawMessage, error) {
					return h.bridge.Call(callCtx, connection.ID, connection.Token, tool, args)
				}, emit)
			}
			h.mu.Lock()
			if finalCheckpoint.ThreadID != "" {
				active.run.Checkpoint = &finalCheckpoint
			}
			active.run.State = "completed"
			if runErr != nil {
				active.run.State = "failed"
				active.run.Message = runErr.Error()
				if errors.Is(runErr, context.Canceled) || runCtx.Err() != nil {
					active.run.State = "cancelled"
					active.run.Message = "Provider execution was cancelled; partial output is retained"
				}
				if len(active.run.Message) > 1000 {
					active.run.Message = active.run.Message[:1000]
				}
			}
			finished := active.run
			h.mu.Unlock()
			persist, persistCancel := context.WithTimeout(context.Background(), 5*time.Second)
			persistErr := k.SaveManagedRun(persist, execution, finished)
			persistCancel()
			// The proposal connection remains available for explicit review. Its
			// native process has exited and cannot send further tool requests.
			h.mu.Lock()
			if persistErr != nil {
				finished.State = "failed"
				finished.Message = "The provider stopped, but its final result could not be saved. Copy the visible output before closing Trove."
				if h.unsaved == nil {
					h.unsaved = map[string]workspace.ManagedRun{}
				}
				h.unsaved[finished.ID] = finished
			}
			if h.managed == active {
				h.managed = nil
			}
			h.mu.Unlock()
		}()
		result = run
	}
	if err != nil {
		return errorEnvelope("PROVIDER_ERROR", err.Error())
	}
	out, _ := json.Marshal(map[string]any{"ok": true, "result": result})
	return string(out)
}

func pathsOverlap(a, b string) bool {
	var err error
	a, err = canonicalFuturePath(a)
	if err != nil {
		return true
	}
	b, err = canonicalFuturePath(b)
	if err != nil {
		return true
	}
	contains := func(parent, child string) bool {
		rel, e := filepath.Rel(parent, child)
		return e == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
	}
	return contains(a, b) || contains(b, a)
}

// Resolve the nearest existing parent as well as existing complete paths. A
// not-yet-created provider directory must not hide a symlink into a workspace.
func canonicalFuturePath(path string) (string, error) {
	current, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	suffix := []string{}
	for {
		resolved, e := filepath.EvalSymlinks(current)
		if e == nil {
			for i := len(suffix) - 1; i >= 0; i-- {
				resolved = filepath.Join(resolved, suffix[i])
			}
			return resolved, nil
		}
		if !os.IsNotExist(e) {
			return "", e
		}
		if st, se := os.Lstat(current); se == nil && st.Mode()&os.ModeSymlink != 0 {
			return "", errors.New("broken storage symlink")
		}
		parent := filepath.Dir(current)
		if parent == current {
			return "", e
		}
		suffix = append(suffix, filepath.Base(current))
		current = parent
	}
}
