package providers

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

type ToolSpec struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	InputSchema any    `json:"inputSchema"`
}
type ToolCaller func(context.Context, string, map[string]any) (json.RawMessage, error)

// CodexConfig is constructed by the trusted host after account and context
// selection. It is never decoded from a renderer request or model response.
type CodexConfig struct {
	Runtime        Runtime
	StateDirectory string
	Identity       Identity
	ScopeHash      string
	Model          string
	AccessToken    string `json:"-"`
	Tools          []ToolSpec
}
func (c CodexConfig) String()string{return fmt.Sprintf("CodexConfig{account:%s, model:%s, token:[redacted]}",c.Identity.Account,c.Model)}
func (c CodexConfig) GoString()string{return c.String()}
type ManagedCheckpoint struct {
	Version       int      `json:"version"`
	Identity      Identity `json:"identity"`
	ScopeHash     string   `json:"scopeHash"`
	Policy        string   `json:"policy"`
	RuntimeSHA256 string   `json:"runtimeSha256"`
	Model         string   `json:"model"`
	ThreadID      string   `json:"threadId"`
}

func (c CodexConfig) checkpoint(threadID string) ManagedCheckpoint {
	return ManagedCheckpoint{1, c.Identity, c.ScopeHash, ContainmentPolicy, c.Runtime.SHA256, c.Model, threadID}
}
func (c CodexConfig) validate() error {
	if !c.Runtime.Available || c.Runtime.Provider != "codex" || c.Identity.Provider != "codex" || c.Identity.RuntimeVersion != c.Runtime.Version || c.Identity.AdapterVersion != "2" || c.Identity.Account == "" || c.Identity.AuthMode != "siwc" || c.Identity.BillingMode != "chatgpt_plan" || len(c.AccessToken) == 0 || len(c.AccessToken) > 64<<10 || strings.ContainsRune(c.AccessToken, 0) {
		return errors.New("invalid managed Codex identity or authentication")
	}
	if !filepath.IsAbs(c.StateDirectory) || !regexp.MustCompile(`^[a-f0-9]{64}$`).MatchString(c.ScopeHash) || !regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$`).MatchString(c.Model) {
		return errors.New("invalid managed Codex context or model")
	}
	if len(c.Tools) == 0 || len(c.Tools) > 16 {
		return errors.New("select explicit managed tools")
	}
	return nil
}

// SnapshotRuntime copies and verifies a user-installed native executable into
// the app's private runtime area. A vendor update cannot swap the executable
// underneath an already reviewed runtime identity. No runtime is downloaded.
func SnapshotRuntime(root string, r Runtime) (string, error) {
	if !r.Available || !regexp.MustCompile(`^[a-f0-9]{64}$`).MatchString(r.SHA256) {
		return "", errors.New("runtime has not been discovered")
	}
	if err := os.MkdirAll(root, 0700); err != nil {
		return "", err
	}
	root, err := filepath.EvalSymlinks(root)
	if err != nil {
		return "", err
	}
	dst := filepath.Join(root, r.SHA256)
	if digest, err := runtimeDigest(dst); err == nil && digest == r.SHA256 {
		return dst, nil
	}
	source, err := os.Open(r.Path)
	if err != nil {
		return "", err
	}
	defer source.Close()
	f, err := os.CreateTemp(root, ".runtime-")
	if err != nil {
		return "", err
	}
	defer os.Remove(f.Name())
	defer f.Close()
	h := sha256.New()
	n, err := io.Copy(io.MultiWriter(f, h), io.LimitReader(source, (512<<20)+1))
	if err != nil {
		return "", err
	}
	if n > 512<<20 || hex.EncodeToString(h.Sum(nil)) != r.SHA256 {
		return "", errors.New("runtime changed since discovery; discover and review again")
	}
	if err = f.Chmod(0500); err != nil {
		return "", err
	}
	if err = f.Sync(); err != nil {
		return "", err
	}
	if err = f.Close(); err != nil {
		return "", err
	}
	if err = os.Rename(f.Name(), dst); err != nil {
		return "", err
	}
	return dst, nil
}

type rpcMessage struct {
	ID     json.RawMessage `json:"id,omitempty"`
	Method string          `json:"method,omitempty"`
	Params json.RawMessage `json:"params,omitempty"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  json.RawMessage `json:"error,omitempty"`
}
type codexRPC struct {
	input        io.WriteCloser
	output       <-chan rpcMessage
	errors       <-chan error
	mu           sync.Mutex
	next         int
	thread, turn string
	tools        map[string]string
	call         ToolCaller
	emit         func(Event) error
	terminal     string
	probe        bool
}

func newCodexRPC(ctx context.Context, input io.WriteCloser, output io.Reader, call ToolCaller, emit func(Event) error) *codexRPC {
	messages := make(chan rpcMessage, 16)
	failures := make(chan error, 1)
	r := &codexRPC{input: input, output: messages, errors: failures, call: call, emit: emit, tools: map[string]string{}}
	go func() {
		defer close(messages)
		scanner := bufio.NewScanner(output)
		scanner.Buffer(make([]byte, 4096), 1<<20)
		total := 0
		for scanner.Scan() {
			total += len(scanner.Bytes())
			if total > 16<<20 {
				failures <- errors.New("provider output exceeds 16 MiB")
				return
			}
			var m rpcMessage
			if err := json.Unmarshal(scanner.Bytes(), &m); err != nil {
				failures <- errors.New("malformed app-server frame")
				return
			}
			select {
			case messages <- m:
			case <-ctx.Done():
				return
			}
		}
		if err := scanner.Err(); err != nil {
			failures <- errors.New("app-server frame exceeds limit or stream failed")
		} else {
			failures <- io.EOF
		}
	}()
	return r
}
func (r *codexRPC) send(v any) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return json.NewEncoder(r.input).Encode(v)
}
func (r *codexRPC) request(ctx context.Context, method string, params any) (json.RawMessage, error) {
	r.next++
	id := r.next
	if err := r.send(map[string]any{"id": id, "method": method, "params": params}); err != nil {
		return nil, err
	}
	for {
		m, err := r.read(ctx)
		if err != nil {
			return nil, err
		}
		if m.Method != "" {
			if err = r.handle(ctx, m); err != nil {
				return nil, err
			}
			continue
		}
		if string(m.ID) != fmt.Sprint(id) {
			return nil, errors.New("unexpected app-server response identity")
		}
		if len(m.Error) > 0 && string(m.Error) != "null" {
			if r.probe {
				return nil, fmt.Errorf("offline %s: %s", method, m.Error)
			}
			return nil, fmt.Errorf("app-server rejected %s; check account, model, and runtime compatibility", method)
		}
		return m.Result, nil
	}
}
func (r *codexRPC) read(ctx context.Context) (rpcMessage, error) {
	select {
	case <-ctx.Done():
		return rpcMessage{}, ctx.Err()
	case m, ok := <-r.output:
		if ok {
			return m, nil
		}
		select {
		case err := <-r.errors:
			return m, err
		default:
			return m, io.EOF
		}
	}
}
func (r *codexRPC) handle(ctx context.Context, m rpcMessage) error {
	if len(m.ID) > 0 {
		if m.Method == "item/tool/call" {
			var p struct {
				ThreadID  string         `json:"threadId"`
				TurnID    string         `json:"turnId"`
				Tool      string         `json:"tool"`
				Namespace *string        `json:"namespace"`
				Arguments map[string]any `json:"arguments"`
			}
			valid := json.Unmarshal(m.Params, &p) == nil && p.ThreadID == r.thread && p.TurnID != "" && (r.turn == "" || p.TurnID == r.turn) && p.Namespace == nil && r.tools[p.Tool] != "" && p.Arguments != nil && r.call != nil
			result := json.RawMessage(`{"ok":false,"error":{"code":"SCOPE_DENIED","message":"Tool call is outside the selected Trove session"}}`)
			success := false
			if valid {
				var err error
				result, err = r.call(ctx, r.tools[p.Tool], p.Arguments)
				if err != nil {
					result, _ = json.Marshal(map[string]any{"ok": false, "error": map[string]string{"message": err.Error()}})
				} else {
					var status struct {
						OK bool `json:"ok"`
					}
					success = json.Unmarshal(result, &status) == nil && status.OK
				}
			}
			if len(result) > 1<<20 {
				return errors.New("Trove tool output exceeds provider budget")
			}
			return r.send(map[string]any{"id": m.ID, "result": map[string]any{"success": success, "contentItems": []any{map[string]any{"type": "inputText", "text": string(result)}}}})
		}
		// Runtime requests never grant shell, file, network, app or account access.
		switch m.Method {
		case "item/commandExecution/requestApproval", "item/fileChange/requestApproval":
			return r.send(map[string]any{"id": m.ID, "result": map[string]string{"decision": "decline"}})
		case "item/permissions/requestApproval":
			return r.send(map[string]any{"id": m.ID, "result": map[string]any{"permissions": map[string]any{}, "scope": "turn"}})
		default:
			return r.send(map[string]any{"id": m.ID, "error": map[string]any{"code": -32601, "message": "Trove does not expose this runtime capability"}})
		}
	}
	var p struct {
		ThreadID string `json:"threadId"`
		TurnID   string `json:"turnId"`
		Delta    string `json:"delta"`
		Turn     struct {
			ID     string `json:"id"`
			Status string `json:"status"`
		} `json:"turn"`
		Item struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"item"`
	}
	if json.Unmarshal(m.Params, &p) != nil {
		return errors.New("invalid app-server event")
	}
	if p.ThreadID != "" && r.thread != "" && p.ThreadID != r.thread {
		return errors.New("app-server event belongs to another session")
	}
	switch m.Method {
	case "turn/started":
		r.turn = p.Turn.ID
	case "item/agentMessage/delta":
		if len(p.Delta) > 0 && r.emit != nil {
			return r.emit(Event{Kind: "text", Text: p.Delta})
		}
	case "item/completed":
		if p.Item.Type == "agentMessage" && r.emit != nil {
			return r.emit(Event{Kind: "message", Text: p.Item.Text})
		}
	case "turn/completed":
		if r.turn != "" && p.Turn.ID != r.turn {
			return errors.New("terminal event belongs to another turn")
		}
		r.turn = p.Turn.ID
		r.terminal = p.Turn.Status
		if r.emit != nil {
			return r.emit(Event{Kind: "terminal", State: r.terminal, SessionID: r.thread})
		}
	case "model/rerouted":
		return errors.New("provider attempted to change the explicitly selected model")
	}
	return nil
}

func codexArguments() []string {
	args := []string{"app-server", "--listen", "stdio://"}
	for _, s := range []string{`model_provider="openai_chatgpt_plan"`, `model_providers.openai_chatgpt_plan.name="ChatGPT plan"`, `model_providers.openai_chatgpt_plan.base_url="https://api.openai.com/v1"`, `model_providers.openai_chatgpt_plan.env_key="ACCESS_TOKEN"`, `model_providers.openai_chatgpt_plan.wire_api="responses"`, `model_providers.openai_chatgpt_plan.requires_openai_auth=false`, `model_providers.openai_chatgpt_plan.supports_websockets=false`, `web_search="disabled"`, `features.shell_tool=false`, `features.apply_patch_freeform=false`, `features.multi_agent=false`, `features.apps=false`, `features.image_generation=false`, `mcp_servers={}`, `analytics.enabled=false`, `feedback.enabled=false`} {
		args = append(args, "-c", s)
	}
	return args
}

// RunCodex starts the actual installed app-server. Only scoped Trove dynamic
// tools are handled; all resulting writes remain pending owner proposals.
func RunCodex(ctx context.Context, c CodexConfig, prompt string, resume *ManagedCheckpoint, call ToolCaller, emit func(Event) error) (ManagedCheckpoint, error) {
	return runCodex(ctx, c, prompt, resume, call, emit, true, false)
}
func runCodex(ctx context.Context, c CodexConfig, prompt string, resume *ManagedCheckpoint, call ToolCaller, emit func(Event) error, network, probe bool) (ManagedCheckpoint, error) {
	if err := c.validate(); err != nil {
		return ManagedCheckpoint{}, err
	}
	if strings.TrimSpace(prompt) == "" || len(prompt) > 64<<10 {
		return ManagedCheckpoint{}, errors.New("request must contain 1–65536 bytes")
	}
	if resume != nil && (*resume != c.checkpoint(resume.ThreadID) || resume.ThreadID == "") {
		return ManagedCheckpoint{}, errors.New("resume identity, model, context, runtime or permissions changed; start a new reviewed session")
	}
	if err := os.MkdirAll(c.StateDirectory, 0700); err != nil {
		return ManagedCheckpoint{}, err
	}
	state, err := filepath.EvalSymlinks(c.StateDirectory)
	if err != nil {
		return ManagedCheckpoint{}, err
	}
	if err = os.MkdirAll(filepath.Join(state, ".codex"), 0700); err != nil {
		return ManagedCheckpoint{}, err
	}
	binary, err := SnapshotRuntime(filepath.Join(filepath.Dir(state), "runtimes"), c.Runtime)
	if err != nil {
		return ManagedCheckpoint{}, err
	}
	bounded, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()
	env := append(isolatedEnv(state), "CODEX_HOME="+filepath.Join(state, ".codex"), "ACCESS_TOKEN="+c.AccessToken)
	cmd, err := containedCommand(bounded, binary, state, codexArguments(), env, network)
	if err != nil {
		return ManagedCheckpoint{}, err
	}
	input, err := cmd.StdinPipe()
	if err != nil {
		return ManagedCheckpoint{}, err
	}
	output, err := cmd.StdoutPipe()
	if err != nil {
		return ManagedCheckpoint{}, err
	}
	cmd.Stderr = io.Discard
	var diagnostic limitedBuffer
	if probe {
		cmd.Stderr = &diagnostic
	}
	if err = cmd.Start(); err != nil {
		return ManagedCheckpoint{}, errors.New("contained app-server could not start")
	}
	defer func() {
		// EOF lets the official runtime flush its rollout before exit. Bound the
		// shutdown as well: cancellation still terminates the whole process group.
		input.Close()
		stop := time.AfterFunc(2*time.Second, cancel)
		_ = cmd.Wait()
		stop.Stop()
		cancel()
	}()
	r := newCodexRPC(bounded, input, output, call, emit)
	r.probe = probe
	if _, err = r.request(bounded, "initialize", map[string]any{"clientInfo": map[string]any{"name": "Trove", "title": "Trove", "version": "0.5.0"}, "capabilities": map[string]bool{"experimentalApi": true}}); err != nil {
		if probe {
			return ManagedCheckpoint{}, fmt.Errorf("offline initialization: %w; %s", err, diagnostic.String())
		}
		return ManagedCheckpoint{}, err
	}
	if err = r.send(map[string]any{"method": "initialized"}); err != nil {
		return ManagedCheckpoint{}, err
	}
	dynamic := []any{}
	for _, tool := range c.Tools {
		name := "trove_" + strings.ReplaceAll(tool.Name, ".", "_")
		if !regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`).MatchString(name) || r.tools[name] != "" {
			return ManagedCheckpoint{}, errors.New("invalid or repeated scoped tool")
		}
		r.tools[name] = tool.Name
		dynamic = append(dynamic, map[string]any{"type": "function", "name": name, "description": tool.Description, "inputSchema": tool.InputSchema, "deferLoading": false})
	}
	params := map[string]any{"model": c.Model, "modelProvider": "openai_chatgpt_plan", "cwd": state, "approvalPolicy": "never", "sandbox": "read-only", "runtimeWorkspaceRoots": []string{state}, "developerInstructions": "Use only the supplied Trove tools and explicitly selected context. Page, task and record changes are proposals requiring the user's approval in Trove. Do not claim pending proposals have been applied. Do not use shell, filesystem, web search, plugins, agents or other applications.", "config": map[string]any{"web_search": "disabled", "features.shell_tool": false, "features.apply_patch_freeform": false, "features.multi_agent": false, "features.apps": false}}
	method := "thread/start"
	if resume != nil {
		method = "thread/resume"
		params["threadId"] = resume.ThreadID
		params["excludeTurns"] = true
	} else {
		params["dynamicTools"] = dynamic
		params["allowProviderModelFallback"] = false
		params["environments"] = []any{}
	}
	result, err := r.request(bounded, method, params)
	if err != nil {
		return ManagedCheckpoint{}, err
	}
	var started struct {
		Thread struct {
			ID string `json:"id"`
		} `json:"thread"`
		Model string `json:"model"`
	}
	if json.Unmarshal(result, &started) != nil || started.Thread.ID == "" || len(started.Thread.ID) > 256 {
		return ManagedCheckpoint{}, errors.New("runtime returned no valid thread")
	}
	if started.Model != "" && started.Model != c.Model {
		return ManagedCheckpoint{}, errors.New("runtime changed the selected model")
	}
	r.thread = started.Thread.ID
	checkpoint := c.checkpoint(r.thread)
	if emit != nil {
		if err = emit(Event{Kind: "session", SessionID: r.thread}); err != nil {
			return checkpoint, err
		}
	}
	if probe {
		return checkpoint, nil
	}
	result, err = r.request(bounded, "turn/start", map[string]any{"threadId": r.thread, "input": []any{map[string]any{"type": "text", "text": prompt}}, "model": c.Model, "cwd": state, "approvalPolicy": "never", "environments": []any{}, "sandboxPolicy": map[string]any{"type": "readOnly", "access": map[string]any{"type": "restricted", "includePlatformDefaults": true, "readableRoots": []string{state}}}})
	if err != nil {
		return checkpoint, err
	}
	var turn struct {
		Turn struct {
			ID string `json:"id"`
		} `json:"turn"`
	}
	if json.Unmarshal(result, &turn) != nil || turn.Turn.ID == "" {
		return checkpoint, errors.New("runtime returned no turn identity")
	}
	if r.turn == "" {
		r.turn = turn.Turn.ID
	}
	for r.terminal == "" {
		m, e := r.read(bounded)
		if e != nil {
			return checkpoint, e
		}
		if e = r.handle(bounded, m); e != nil {
			return checkpoint, e
		}
	}
	if r.terminal != "completed" {
		return checkpoint, fmt.Errorf("provider turn %s", r.terminal)
	}
	return checkpoint, nil
}
