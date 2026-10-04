package providers

import (
	"context"
	"io"
	"strings"
	"testing"
	"time"
)

func fixture(provider string) Qualification {
	auth := "siwc"
	if provider == "claude_code" {
		auth = "vendor_owned"
	}
	return Qualification{Identity: Identity{Provider: provider, Account: "fixture", AuthMode: auth, BillingMode: "subscription", RuntimeVersion: "fixture-only", AdapterVersion: "1"}, Binary: "/fixture/vendor", TaskDirectory: "/fixture/task", RuntimeVerified: true, AuthenticationVerified: true, DistributionApproved: true, ContainmentVerified: true}
}
func TestVendorTerminalStreamsAndResume(t *testing.T) {
	for _, provider := range []string{"codex", "claude_code"} {
		a := NewCodex()
		terminal := `{"method":"turn/completed","params":{"turn":{"status":"completed"}}}`
		if provider == "claude_code" {
			a = NewClaudeCode()
			terminal = `{"type":"result","subtype":"success","is_error":false}`
		}
		q := fixture(provider)
		if err := a.Connect(q); err != nil {
			t.Fatal(err)
		}
		if err := a.Start(context.Background(), strings.NewReader("{}\n"), func(Event) error { return nil }); err == nil || a.Status() != "interrupted" {
			t.Fatal("EOF treated as completion")
		}
		if err := a.Start(context.Background(), strings.NewReader(terminal), func(Event) error { return nil }); err != nil || a.Status() != "completed" {
			t.Fatal(err)
		}
		if err := a.Resume(context.Background(), Checkpoint{Identity: q.Identity, SessionID: "session", ScopeHash: "a", PolicyRevision: "1"}, q.Identity, "changed", "1", strings.NewReader(terminal), func(Event) error { return nil }); err == nil {
			t.Fatal("resume widened scope")
		}
		a.Disconnect()
		if a.Status() != "disconnected" {
			t.Fatal(a.Status())
		}
	}
}
func TestProviderQualificationAndCommandConstruction(t *testing.T) {
	a := NewCodex()
	if err := a.Connect(Qualification{}); err == nil {
		t.Fatal("accepted unqualified profile")
	}
	q := fixture("codex")
	cmd, err := BuildCommand(context.Background(), q, "prompt", "", "", []string{"ACCESS_TOKEN=fixture"})
	if err != nil {
		t.Fatal(err)
	}
	if cmd.Path != q.Binary || cmd.Dir != q.TaskDirectory || strings.Contains(strings.Join(cmd.Args, " "), "chatgpt.com/backend") {
		t.Fatal("invalid process route")
	}
	if _, err = BuildCommand(context.Background(), q, "", "", "", []string{"HOME=/private"}); err == nil {
		t.Fatal("unapproved environment")
	}
	q = fixture("claude_code")
	cmd, err = BuildCommand(context.Background(), q, "literal $(touch no)", "", "{}", nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.Join(cmd.Args, " "), "--strict-mcp-config") {
		t.Fatal("unrestricted config")
	}
}

func TestCancelInterruptsBlockedReadAndPreventsOverlappingTurns(t *testing.T) {
	a := NewCodex()
	q := fixture("codex")
	if err := a.Connect(q); err != nil {
		t.Fatal(err)
	}
	reader, writer := io.Pipe()
	defer writer.Close()
	done := make(chan error, 1)
	go func() { done <- a.Start(context.Background(), reader, func(Event) error { return nil }) }()
	deadline := time.Now().Add(time.Second)
	for a.Status() != "running" && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if err := a.Connect(q); err == nil {
		t.Fatal("reconnected during active stream")
	}
	a.Cancel()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("cancellation completed successfully")
		}
	case <-time.After(time.Second):
		t.Fatal("cancel did not interrupt blocked read")
	}
	if a.Status() != "interrupted" {
		t.Fatal(a.Status())
	}
}
func TestCredentialsCannotChangeSelectedBillingRoute(t *testing.T) {
	q := fixture("codex")
	for _, env := range [][]string{{"OPENAI_API_KEY=paid"}, {"ACCESS_TOKEN=a", "OPENAI_API_KEY=paid"}, {"ACCESS_TOKEN=a", "ACCESS_TOKEN=b"}, nil} {
		if _, err := BuildCommand(context.Background(), q, "", "", "", env); err == nil {
			t.Fatalf("accepted mismatched credentials %v", env)
		}
	}
	q.Identity.AuthMode = "api_key"
	q.Identity.BillingMode = "api"
	if _, err := BuildCommand(context.Background(), q, "", "", "", []string{"ACCESS_TOKEN=subscription"}); err == nil {
		t.Fatal("API route accepted subscription credential")
	}
	if _, err := BuildCommand(context.Background(), q, "", "", "", []string{"OPENAI_API_KEY=explicit"}); err != nil {
		t.Fatal(err)
	}
}
