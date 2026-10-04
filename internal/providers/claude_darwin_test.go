//go:build darwin

package providers

import (
	"context"
	"encoding/json"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestClaudeLoginContainmentProbe(t *testing.T) {
	if os.Getenv("TROVE_CLAUDE_LOGIN_PROBE") != "1" {
		return
	}
	denied := map[string]bool{}
	for _, address := range []string{"0.0.0.0:0", "127.0.0.1:0", "[::]:0", "[::1]:0"} {
		listener, err := net.Listen("tcp", address)
		denied[address] = err != nil
		if listener != nil {
			listener.Close()
		}
	}
	denied["shell"] = exec.Command("/bin/sh", "-c", "exit 0").Run() != nil
	json.NewEncoder(os.Stdout).Encode(denied)
	os.Exit(0)
}
func TestClaudeLoginRetainsInboundAndShellDenial(t *testing.T) {
	if os.Getenv("TROVE_PROVIDER_INTEGRATION") != "1" {
		t.Skip("requires actual macOS containment")
	}
	binary, _ := os.Executable()
	binary, _ = filepath.EvalSymlinks(binary)
	state, _ := filepath.EvalSymlinks(t.TempDir())
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd, err := claudeLoginCommand(ctx, binary, state, []string{"-test.run=^TestClaudeLoginContainmentProbe$"}, append(isolatedEnv(state), "TROVE_CLAUDE_LOGIN_PROBE=1"))
	if err != nil {
		t.Fatal(err)
	}
	output, err := cmd.Output()
	if err != nil {
		t.Fatal(err)
	}
	var denied map[string]bool
	if json.Unmarshal(output, &denied) != nil || len(denied) != 5 {
		t.Fatal(string(output))
	}
	for capability, value := range denied {
		if !value {
			t.Errorf("login granted unexpected capability: %s", capability)
		}
	}
	t.Log("Native login command denies IPv4/IPv6 loopback and wildcard listeners and shell execution; no callback exception")
}
func TestInstalledClaudeReadinessGatesLoginAndExecution(t *testing.T) {
	if os.Getenv("TROVE_PROVIDER_INTEGRATION") != "1" {
		t.Skip("requires installed real native Claude runtime")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	r := Discover(ctx, "claude_code")
	if !r.Available {
		t.Fatal(r.Reason)
	}
	fresh, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(fresh, 0700); err != nil {
		t.Fatal(err)
	}
	status, err := ClaudeAuthStatus(ctx, r, fresh)
	if err != nil || status.LoggedIn || status.Method != "none" {
		t.Fatal("fresh profile reached an existing account", status, err)
	}
	qualified := QualifyClaudeRuntime(ctx, r)
	t.Logf("Claude %s startup qualified=%v reason=%s", r.Version, qualified.Available, qualified.Reason)
	if !qualified.Available {
		if _, err = ClaudeAuthLoginCommand(ctx, r, t.TempDir(), "claude_subscription"); err == nil {
			t.Fatal("failed startup did not disable login")
		}
		c := claudeFixture()
		c.Runtime = r
		c.StateDirectory = t.TempDir()
		c.SessionDirectory = filepath.Join(c.StateDirectory, "sessions", "probe")
		c.Identity.RuntimeVersion = r.Version
		if _, err = RunClaude(ctx, c, "Never sent; startup gate must deny", nil, nil); err == nil {
			t.Fatal("failed startup did not disable execution")
		}
	}
}
