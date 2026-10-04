// Package providers contains versioned vendor protocol adapters. Authentication
// and OS process containment are supplied by the trusted host, never a renderer.
package providers

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
)

type Identity struct {
	Provider       string `json:"provider"`
	Account        string `json:"account"`
	AuthMode       string `json:"authMode"`
	BillingMode    string `json:"billingMode"`
	RuntimeVersion string `json:"runtimeVersion"`
	AdapterVersion string `json:"adapterVersion"`
}
type Qualification struct {
	Identity               Identity
	Binary                 string
	TaskDirectory          string
	RuntimeVerified        bool
	AuthenticationVerified bool
	DistributionApproved   bool
	ContainmentVerified    bool
}
type Capabilities struct {
	Provider  string `json:"provider"`
	AuthOwner string `json:"authOwner"`
	Route     string `json:"route"`
	Enabled   bool   `json:"enabled"`
	Reason    string `json:"reason"`
}
type Event struct {
	Kind      string          `json:"kind"`
	Text      string          `json:"text,omitempty"`
	SessionID string          `json:"sessionId,omitempty"`
	State     string          `json:"state,omitempty"`
	Request   json.RawMessage `json:"request,omitempty"`
}
type Checkpoint struct {
	Identity       Identity `json:"identity"`
	SessionID      string   `json:"sessionId"`
	ScopeHash      string   `json:"scopeHash"`
	PolicyRevision string   `json:"policyRevision"`
}
type Adapter interface {
	ProbeCapabilities() Capabilities
	Connect(Qualification) error
	Status() string
	Start(context.Context, io.Reader, func(Event) error) error
	Resume(context.Context, Checkpoint, Identity, string, string, io.Reader, func(Event) error) error
	Cancel()
	Disconnect()
}
type StreamAdapter struct {
	mu       sync.Mutex
	provider string
	identity Identity
	state    string
	cancel   context.CancelFunc
}

func NewClaudeCode() *StreamAdapter {
	return &StreamAdapter{provider: "claude_code", state: "disconnected"}
}
func NewCodex() *StreamAdapter { return &StreamAdapter{provider: "codex", state: "disconnected"} }
func (a *StreamAdapter) ProbeCapabilities() Capabilities {
	c := Capabilities{Provider: a.provider, Reason: "Requires qualified runtime, authentication, distribution and OS containment"}
	if a.provider == "claude_code" {
		c.AuthOwner = "Claude Code"
		c.Route = "unchanged official CLI; vendor-owned sign-in"
	} else {
		c.AuthOwner = "Trove"
		c.Route = "official Sign in with ChatGPT + Codex app-server"
	}
	return c
}
func (a *StreamAdapter) Connect(q Qualification) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.cancel != nil {
		return errors.New("provider turn is still active")
	}
	if !q.RuntimeVerified || !q.AuthenticationVerified || !q.DistributionApproved || !q.ContainmentVerified {
		return errors.New("provider mode is unqualified")
	}
	if q.Identity.Provider != a.provider || q.Identity.Account == "" || q.Identity.RuntimeVersion == "" || q.Identity.AdapterVersion != "1" || !filepath.IsAbs(q.Binary) || !filepath.IsAbs(q.TaskDirectory) {
		return errors.New("invalid qualified profile")
	}
	if a.provider == "claude_code" && q.Identity.AuthMode != "vendor_owned" {
		return errors.New("Claude authentication belongs to its official runtime")
	}
	if a.provider == "codex" && q.Identity.AuthMode != "siwc" && q.Identity.AuthMode != "api_key" {
		return errors.New("Codex requires explicit SIWC or API billing")
	}
	if q.Identity.BillingMode == "" {
		return errors.New("billing mode must be selected explicitly")
	}
	a.identity = q.Identity
	a.state = "connected"
	return nil
}
func (a *StreamAdapter) Status() string { a.mu.Lock(); defer a.mu.Unlock(); return a.state }
func (a *StreamAdapter) Cancel() {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.cancel != nil {
		a.cancel()
	}
	if a.state == "running" {
		a.state = "interrupted"
	}
}
func (a *StreamAdapter) Disconnect() {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.cancel != nil {
		a.cancel()
	}
	a.state = "disconnected"
}
func (a *StreamAdapter) Resume(ctx context.Context, c Checkpoint, i Identity, scope, policy string, r io.Reader, emit func(Event) error) error {
	if c.Identity != i || c.SessionID == "" || c.ScopeHash != scope || c.PolicyRevision != policy {
		return errors.New("resume is incompatible; review a fresh handoff")
	}
	a.mu.Lock()
	identity := a.identity
	a.mu.Unlock()
	if identity != i {
		return errors.New("connected identity differs from checkpoint")
	}
	return a.Start(ctx, r, emit)
}
func (a *StreamAdapter) Start(ctx context.Context, r io.Reader, emit func(Event) error) error {
	a.mu.Lock()
	if a.cancel != nil || (a.state != "connected" && a.state != "completed" && a.state != "interrupted") {
		a.mu.Unlock()
		return errors.New("provider is not connected or is already active")
	}
	// Live transport streams must be closeable so cancellation can interrupt a
	// blocked Read. Finite in-memory fixtures need no asynchronous interruption.
	closer, interruptible := r.(io.Closer)
	if !interruptible {
		switch r.(type) {
		case *strings.Reader, *bytes.Reader, *bytes.Buffer:
		default:
			a.mu.Unlock()
			return errors.New("provider stream must support cancellation through Close")
		}
	}
	ctx, cancel := context.WithCancel(ctx)
	a.cancel = cancel
	a.state = "running"
	a.mu.Unlock()
	defer cancel()
	if interruptible {
		stop := context.AfterFunc(ctx, func() { _ = closer.Close() })
		defer stop()
	}
	// A qualified host supplies a bounded process stream. EOF is never success.
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 4096), 1024*1024)
	complete := false
	total := 0
	for scanner.Scan() {
		if err := ctx.Err(); err != nil {
			return a.finish("interrupted", err)
		}
		total += len(scanner.Bytes())
		if total > 8*1024*1024 {
			return a.finish("failed", errors.New("provider stream exceeds 8 MiB"))
		}
		event, err := DecodeEvent(a.provider, scanner.Bytes())
		if err != nil {
			return a.finish("failed", err)
		}
		if event.Kind == "" {
			continue
		}
		if err = emit(event); err != nil {
			return a.finish("failed", err)
		}
		if event.Kind == "terminal" {
			if event.State != "completed" {
				return a.finish(event.State, errors.New("provider turn did not complete"))
			}
			complete = true
			break
		}
	}
	if ctx.Err() != nil {
		return a.finish("interrupted", ctx.Err())
	}
	if err := scanner.Err(); err != nil {
		return a.finish("failed", err)
	}
	if !complete {
		return a.finish("interrupted", errors.New("stream closed before successful terminal event"))
	}
	return a.finish("completed", nil)
}
func (a *StreamAdapter) finish(state string, err error) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.state != "disconnected" {
		a.state = state
	}
	a.cancel = nil
	return err
}
func DecodeEvent(provider string, line []byte) (Event, error) {
	var v map[string]json.RawMessage
	if err := json.Unmarshal(line, &v); err != nil {
		return Event{}, errors.New("malformed provider event")
	}
	get := func(k string) string { var s string; json.Unmarshal(v[k], &s); return s }
	if provider == "claude_code" {
		switch get("type") {
		case "system":
			return Event{Kind: "session", SessionID: get("session_id")}, nil
		case "assistant":
			var m struct {
				Content []struct {
					Type string `json:"type"`
					Text string `json:"text"`
				} `json:"content"`
			}
			json.Unmarshal(v["message"], &m)
			text := ""
			for _, c := range m.Content {
				if c.Type == "text" {
					text += c.Text
				}
			}
			return Event{Kind: "text", Text: text}, nil
		case "stream_event":
			var envelope struct {
				Delta struct {
					Type string `json:"type"`
					Text string `json:"text"`
				} `json:"delta"`
			}
			json.Unmarshal(v["event"], &envelope)
			if envelope.Delta.Type == "text_delta" {
				return Event{Kind: "text", Text: envelope.Delta.Text}, nil
			}
		case "result":
			state := "failed"
			var failed bool
			json.Unmarshal(v["is_error"], &failed)
			if get("subtype") == "success" && !failed {
				state = "completed"
			}
			return Event{Kind: "terminal", State: state, SessionID: get("session_id")}, nil
		case "control_request":
			return Event{Kind: "approval", Request: append([]byte(nil), line...)}, nil
		}
		return Event{}, nil
	}
	if provider != "codex" {
		return Event{}, errors.New("unknown provider")
	}
	var params struct {
		Delta  string `json:"delta"`
		Thread struct {
			ID string `json:"id"`
		} `json:"thread"`
		Turn struct {
			Status string `json:"status"`
		} `json:"turn"`
	}
	json.Unmarshal(v["params"], &params)
	switch get("method") {
	case "item/agentMessage/delta":
		return Event{Kind: "text", Text: params.Delta}, nil
	case "thread/started":
		return Event{Kind: "session", SessionID: params.Thread.ID}, nil
	case "turn/completed":
		return Event{Kind: "terminal", State: params.Turn.Status}, nil
	}
	if len(v["id"]) > 0 && get("method") != "" {
		return Event{Kind: "approval", Request: append([]byte(nil), line...)}, nil
	}
	return Event{}, nil
}

// BuildCommand never invokes a shell and never starts a process itself. The host
// must wrap it in its qualified containment launcher before execution.
func BuildCommand(ctx context.Context, q Qualification, prompt, session, bridgeConfig string, credentialEnv []string) (*exec.Cmd, error) {
	a := NewCodex()
	if q.Identity.Provider == "claude_code" {
		a = NewClaudeCode()
	}
	if err := a.Connect(q); err != nil {
		return nil, err
	}
	if len(prompt) > 64000 {
		return nil, errors.New("prompt too large")
	}
	args := []string{}
	if q.Identity.Provider == "claude_code" {
		args = []string{"-p", prompt, "--output-format", "stream-json", "--verbose", "--include-partial-messages", "--tools", "", "--strict-mcp-config", "--mcp-config", bridgeConfig}
		if session != "" {
			args = append(args, "--resume", session)
		}
	} else {
		args = []string{"app-server", "--listen", "stdio://"}
		if q.Identity.AuthMode == "siwc" {
			for _, setting := range []string{`model_provider="openai_chatgpt_plan"`, `model_providers.openai_chatgpt_plan.name="ChatGPT plan"`, `model_providers.openai_chatgpt_plan.base_url="https://api.openai.com/v1"`, `model_providers.openai_chatgpt_plan.env_key="ACCESS_TOKEN"`, `model_providers.openai_chatgpt_plan.wire_api="responses"`, `model_providers.openai_chatgpt_plan.requires_openai_auth=false`, `model_providers.openai_chatgpt_plan.supports_websockets=false`} {
				args = append(args, "-c", setting)
			}
		}
	}
	cmd := exec.CommandContext(ctx, q.Binary, args...)
	cmd.Dir = q.TaskDirectory
	cmd.Env = []string{"PATH=/usr/bin:/bin", "LANG=en_US.UTF-8"}
	seenCredentials := map[string]bool{}
	for _, entry := range credentialEnv {
		key, value, ok := strings.Cut(entry, "=")
		if !ok || (key != "ACCESS_TOKEN" && key != "OPENAI_API_KEY") {
			return nil, fmt.Errorf("environment key is not approved")
		}
		if q.Identity.Provider == "claude_code" {
			return nil, errors.New("Trove does not copy Claude credentials")
		}
		expected := "OPENAI_API_KEY"
		if q.Identity.AuthMode == "siwc" {
			expected = "ACCESS_TOKEN"
		}
		if key != expected || seenCredentials[key] || value == "" || strings.ContainsRune(value, 0) {
			return nil, errors.New("credential does not match the selected authentication mode")
		}
		seenCredentials[key] = true
		cmd.Env = append(cmd.Env, entry)
	}
	if q.Identity.Provider == "codex" && len(seenCredentials) != 1 {
		return nil, errors.New("selected Codex mode requires exactly one matching credential")
	}
	return cmd, nil
}
