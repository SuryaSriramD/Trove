package providers

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"
	"time"
)

type bufferCloser struct{ strings.Builder }

func (*bufferCloser) Close() error { return nil }

func TestCodexDynamicToolsRemainScopedAndApprovalsAreDenied(t *testing.T) {
	var out bufferCloser
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	calls := 0
	r := newCodexRPC(ctx, &out, strings.NewReader(""), func(_ context.Context, tool string, args map[string]any) (json.RawMessage, error) {
		calls++
		if tool != "readPage" || args["pageId"] != "chosen" {
			t.Fatal(tool, args)
		}
		return json.RawMessage(`{"ok":true,"result":{"title":"Chosen"}}`), nil
	}, nil)
	r.thread, r.turn = "thread-1", "turn-1"
	r.tools["trove_readPage"] = "readPage"
	request := func(method, params string) {
		t.Helper()
		if err := r.handle(ctx, rpcMessage{ID: json.RawMessage(`"server-request"`), Method: method, Params: json.RawMessage(params)}); err != nil {
			t.Fatal(err)
		}
	}
	request("item/tool/call", `{"threadId":"thread-1","turnId":"turn-1","tool":"trove_readPage","namespace":null,"arguments":{"pageId":"chosen"}}`)
	request("item/tool/call", `{"threadId":"other-thread","turnId":"turn-1","tool":"trove_readPage","arguments":{"pageId":"chosen"}}`)
	request("item/tool/call", `{"threadId":"thread-1","turnId":"turn-1","tool":"exec_command","arguments":{"cmd":"cat secret"}}`)
	request("item/tool/call", `{"threadId":"thread-1","turnId":"different-turn","tool":"trove_readPage","arguments":{"pageId":"chosen"}}`)
	request("item/commandExecution/requestApproval", `{"command":"write outside workspace"}`)
	request("item/fileChange/requestApproval", `{}`)
	request("item/permissions/requestApproval", `{"permissions":{"network":{"enabled":true}}}`)
	request("account/login/start", `{"type":"apiKey","apiKey":"never-consume"}`)
	if calls != 1 {
		t.Fatal("unscoped call crossed host boundary", calls)
	}
	s := bufio.NewScanner(strings.NewReader(out.String()))
	responses := []map[string]any{}
	for s.Scan() {
		var m map[string]any
		if json.Unmarshal(s.Bytes(), &m) != nil {
			t.Fatal("bad response")
		}
		responses = append(responses, m)
	}
	if len(responses) != 8 {
		t.Fatal(responses)
	}
	if responses[0]["result"].(map[string]any)["success"] != true {
		t.Fatal("scoped read failed")
	}
	for _, i := range []int{1, 2, 3} {
		if responses[i]["result"].(map[string]any)["success"] != false {
			t.Fatal("denied tool accepted")
		}
	}
	for _, i := range []int{4, 5} {
		if responses[i]["result"].(map[string]any)["decision"] != "decline" {
			t.Fatal("runtime could approve its own write")
		}
	}
	if len(responses[6]["result"].(map[string]any)["permissions"].(map[string]any)) != 0 || responses[7]["error"] == nil {
		t.Fatal("permission or account request escalated")
	}
}

func TestCodexOnlySuccessfulBoundTerminalCompletes(t *testing.T) {
	for _, state := range []string{"completed", "failed", "interrupted"} {
		var out bufferCloser
		events := []Event{}
		ctx, cancel := context.WithCancel(context.Background())
		r := newCodexRPC(ctx, &out, strings.NewReader(""), nil, func(e Event) error { events = append(events, e); return nil })
		r.thread, r.turn = "thread", "turn"
		if err := r.handle(ctx, rpcMessage{Method: "item/agentMessage/delta", Params: json.RawMessage(`{"threadId":"thread","turnId":"turn","delta":"partial"}`)}); err != nil {
			t.Fatal(err)
		}
		if r.terminal != "" {
			t.Fatal("partial output became completed")
		}
		params, _ := json.Marshal(map[string]any{"threadId": "thread", "turn": map[string]string{"id": "turn", "status": state}})
		if err := r.handle(ctx, rpcMessage{Method: "turn/completed", Params: params}); err != nil {
			t.Fatal(err)
		}
		if r.terminal != state || len(events) != 2 || events[1].State != state {
			t.Fatal("terminal state lost", events)
		}
		if err := r.handle(ctx, rpcMessage{Method: "turn/completed", Params: json.RawMessage(`{"threadId":"thread","turn":{"id":"different","status":"completed"}}`)}); err == nil {
			t.Fatal("unrelated turn completed")
		}
		cancel()
	}
}

func TestCodexRejectsUnboundAndCrossTurnOutput(t *testing.T) {
	for _, method := range []string{"item/agentMessage/delta", "item/completed", "turn/completed", "turn/started"} {
		var out bufferCloser
		ctx, cancel := context.WithCancel(context.Background())
		r := newCodexRPC(ctx, &out, strings.NewReader(""), nil, nil)
		r.thread, r.turn = "thread", "selected-turn"
		if err := r.handle(ctx, rpcMessage{Method: method, Params: json.RawMessage(`{"threadId":"thread","turnId":"unrelated-turn","delta":"unrelated output","item":{"type":"agentMessage","text":"unrelated output"},"turn":{"id":"unrelated-turn","status":"completed"}}`)}); err == nil {
			t.Fatalf("accepted cross-turn event %s", method)
		}
		cancel()
	}
}

func TestCodexProtocolBoundsAndCancellation(t *testing.T) {
	for _, input := range []string{"not-json\n", strings.Repeat("x", (1<<20)+1) + "\n", ""} {
		ctx, cancel := context.WithCancel(context.Background())
		var out bufferCloser
		r := newCodexRPC(ctx, &out, strings.NewReader(input), nil, nil)
		if _, err := r.read(ctx); err == nil {
			t.Fatal("invalid stream accepted")
		}
		cancel()
	}
	ctx, cancel := context.WithCancel(context.Background())
	rd, wr := io.Pipe()
	defer rd.Close()
	defer wr.Close()
	var out bufferCloser
	r := newCodexRPC(ctx, &out, rd, nil, nil)
	done := make(chan error, 1)
	go func() { _, err := r.read(ctx); done <- err }()
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("cancellation blocked on vendor stream")
	}
}

func TestCodexResumeRejectsChangedIdentityModelScopeAndRuntime(t *testing.T) {
	c := CodexConfig{Runtime: Runtime{Provider: "codex", Version: "0.153.4", SHA256: strings.Repeat("a", 64), Available: true}, Identity: Identity{Provider: "codex", Account: "account", AuthMode: "siwc", BillingMode: "chatgpt_plan", RuntimeVersion: "0.153.4", AdapterVersion: "2"}, ScopeHash: strings.Repeat("b", 64), StateDirectory: "/nonexistent", Model: "chosen-model", AccessToken: "test", Tools: []ToolSpec{{Name: "readPage"}}}
	cp := c.checkpoint("thread")
	for _, change := range []func(*ManagedCheckpoint){func(v *ManagedCheckpoint) { v.Identity.Account = "other" }, func(v *ManagedCheckpoint) { v.Identity.BillingMode = "api" }, func(v *ManagedCheckpoint) { v.Model = "other" }, func(v *ManagedCheckpoint) { v.RuntimeSHA256 = "other" }, func(v *ManagedCheckpoint) { v.ScopeHash = "other" }, func(v *ManagedCheckpoint) { v.Policy = "other" }} {
		v := cp
		change(&v)
		if _, err := RunCodex(context.Background(), c, "Continue", &v, nil, nil); err == nil || !strings.Contains(err.Error(), "resume identity") {
			t.Fatal("incompatible resume", err)
		}
	}
}
