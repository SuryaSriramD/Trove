//go:build darwin

package providers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func newClaudeProfileFixture(t *testing.T) *ClaudeProfiles {
	t.Helper()
	root, _ := filepath.EvalSymlinks(t.TempDir())
	if err := os.Chmod(root, 0700); err != nil {
		t.Fatal(err)
	}
	m, err := NewClaudeProfiles(root)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = m.Close() })
	m.qualify = func(_ context.Context, r Runtime) Runtime { return r }
	m.executable = func() (string, error) { return "/Applications/Trove's App.app/Contents/MacOS/Trove", nil }
	m.launch = func(context.Context, string) error { return nil }
	m.status = func(context.Context, Runtime, string) (ClaudeAuth, error) {
		return ClaudeAuth{LoggedIn: true, Method: "claude.ai", Account: strings.Repeat("f", 64), AccountLabel: "account@example.test", BillingMode: "claude_subscription"}, nil
	}
	m.logout = func(context.Context, Runtime, string) error { return nil }
	return m
}
func profileRuntime() Runtime {
	r := claudeFixture().Runtime
	r.Path = "/private/vendor-runtime"
	return r
}
func TestClaudeProfilesNativeHelperIsOneTimeAndContainsNoCredentials(t *testing.T) {
	m := newClaudeProfileFixture(t)
	p, err := m.Create("Work", "claude_subscription")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = NewClaudeProfiles(m.root); err == nil {
		t.Fatal("duplicate owner lock accepted")
	}
	var launched string
	m.launch = func(_ context.Context, path string) error { launched = path; return nil }
	a, err := m.Login(context.Background(), profileRuntime(), p.ID)
	if err != nil {
		t.Fatal(err)
	}
	script, err := os.ReadFile(launched)
	if err != nil || !strings.Contains(string(script), "Trove'\\''s App.app") || !strings.Contains(string(script), "--trove-claude-login") {
		t.Fatal(string(script), err)
	}
	if _, _, err = m.Resolve(context.Background(), profileRuntime(), p.ID, p.BillingMode); err == nil {
		t.Fatal("active vendor login permitted a run")
	}
	control := filepath.Join(m.attemptPath(a.ID), "control.json")
	starts := 0
	login := func(ctx context.Context, r Runtime, state, billing string) (*exec.Cmd, error) {
		starts++
		if r.Path != profileRuntime().Path || state != m.statePath(p.ID) || billing != p.BillingMode {
			t.Fatal("helper changed selected account")
		}
		return exec.CommandContext(ctx, "/usr/bin/true"), nil
	}
	if code := runClaudeLoginHelper(m.root, control, strings.NewReader("synthetic-secret-code"), io.Discard, io.Discard, login); code != 0 {
		t.Fatal("native helper failed", code)
	}
	if code := runClaudeLoginHelper(m.root, control, nil, io.Discard, io.Discard, login); code == 0 || starts != 1 {
		t.Fatal("login control replayed", code, starts)
	}
	profiles, err := m.List()
	if err != nil || profiles[0].Attempt.State != "completed" {
		t.Fatal(profiles, err)
	}
	cfg, label, err := m.Resolve(context.Background(), profileRuntime(), p.ID, p.BillingMode)
	if err != nil || cfg.Identity.Account == p.ID || cfg.Identity.Account != strings.Repeat("f", 64) || label != "account@example.test" {
		t.Fatal(cfg, label, err)
	}
	if cfg.StateDirectory != m.statePath(p.ID) || !strings.HasSuffix(cfg.StateDirectory, "/state") {
		t.Fatal("incorrect private vendor state")
	}
	_ = filepath.WalkDir(m.root, func(path string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			raw, _ := os.ReadFile(path)
			if strings.Contains(string(raw), "synthetic-secret-code") {
				t.Error("terminal input leaked into host control")
			}
		}
		return nil
	})
}
func TestClaudeProfileStartupGatePreventsAnyTerminalLaunch(t *testing.T) {
	m := newClaudeProfileFixture(t)
	p, _ := m.Create("Work", "claude_subscription")
	launched := false
	m.launch = func(context.Context, string) error { launched = true; return nil }
	m.qualify = func(_ context.Context, r Runtime) Runtime {
		r.Available = false
		r.Reason = "native startup failed"
		return r
	}
	if _, err := m.Login(context.Background(), profileRuntime(), p.ID); err == nil || launched {
		t.Fatal("unqualified runtime launched", err)
	}
	files, _ := os.ReadDir(filepath.Join(m.root, "attempts"))
	if len(files) != 0 {
		t.Fatal("unqualified login wrote launch controls")
	}
	if _, _, err := m.Resolve(context.Background(), profileRuntime(), p.ID, p.BillingMode); err == nil {
		t.Fatal("unqualified runtime became usable")
	}
}
func TestClaudeLoginCancellationStopsHelperAndPreventsLateLaunch(t *testing.T) {
	m := newClaudeProfileFixture(t)
	p, _ := m.Create("Work", "claude_subscription")
	a, err := m.Login(context.Background(), profileRuntime(), p.ID)
	if err != nil {
		t.Fatal(err)
	}
	started := make(chan struct{})
	done := make(chan int, 1)
	go func() {
		done <- runClaudeLoginHelper(m.root, filepath.Join(m.attemptPath(a.ID), "control.json"), nil, io.Discard, io.Discard, func(ctx context.Context, _ Runtime, _, _ string) (*exec.Cmd, error) {
			close(started)
			return exec.CommandContext(ctx, "/bin/sleep", "30"), nil
		})
	}()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("helper did not claim login")
	}
	if _, err = m.Cancel(p.ID); err != nil {
		t.Fatal(err)
	}
	select {
	case code := <-done:
		if code == 0 {
			t.Fatal("cancelled login succeeded")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("login ignored cancellation")
	}
	profiles, err := m.List()
	if err != nil || profiles[0].Attempt.State != "cancelled" {
		t.Fatal(profiles, err)
	}
	a, err = m.Login(context.Background(), profileRuntime(), p.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = m.Cancel(p.ID); err != nil {
		t.Fatal(err)
	}
	called := false
	code := runClaudeLoginHelper(m.root, filepath.Join(m.attemptPath(a.ID), "control.json"), nil, io.Discard, io.Discard, func(context.Context, Runtime, string, string) (*exec.Cmd, error) { called = true; return nil, nil })
	if code == 0 || called {
		t.Fatal("cancelled unopened terminal started vendor")
	}
}
func TestClaudeLogoutFailureStaysBlockedAcrossRestart(t *testing.T) {
	m := newClaudeProfileFixture(t)
	p, _ := m.Create("Work", "claude_subscription")
	m.logout = func(context.Context, Runtime, string) error { return errors.New("synthetic failure") }
	if _, err := m.Logout(context.Background(), profileRuntime(), p.ID); err == nil {
		t.Fatal("logout falsely succeeded")
	}
	if err := m.Close(); err != nil {
		t.Fatal(err)
	}
	restored, err := NewClaudeProfiles(m.root)
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()
	restored.qualify = m.qualify
	restored.status = m.status
	if _, _, err = restored.Resolve(context.Background(), profileRuntime(), p.ID, p.BillingMode); err == nil {
		t.Fatal("failed logout lost its execution block")
	}
	if _, err = restored.Login(context.Background(), profileRuntime(), p.ID); err == nil {
		t.Fatal("failed logout allowed new login")
	}
	restored.logout = func(context.Context, Runtime, string) error { return nil }
	restored.status = func(context.Context, Runtime, string) (ClaudeAuth, error) {
		return ClaudeAuth{Method: "none", Reason: "Sign in through the official runtime"}, nil
	}
	result, err := restored.Logout(context.Background(), profileRuntime(), p.ID)
	if err != nil || result.Auth.LoggedIn {
		t.Fatal(result, err)
	}
}
func TestClaudeLoginControlRejectsWrongOwnerScopeAndExpiredRequests(t *testing.T) {
	m := newClaudeProfileFixture(t)
	p, _ := m.Create("Work", "claude_subscription")
	a, _ := m.Login(context.Background(), profileRuntime(), p.ID)
	path := filepath.Join(m.attemptPath(a.ID), "control.json")
	var original claudeLoginControl
	if err := readPrivateClaudeJSON(path, 16<<10, &original); err != nil {
		t.Fatal(err)
	}
	for name, change := range map[string]func(*claudeLoginControl){"expired": func(c *claudeLoginControl) { c.Attempt.ExpiresAt = time.Now().Add(-time.Second) }, "other profile": func(c *claudeLoginControl) { c.Attempt.ProfileID = "00000000-0000-4000-8000-000000000001" }, "billing drift": func(c *claudeLoginControl) { c.BillingMode = "anthropic_api" }, "runtime changed": func(c *claudeLoginControl) { c.Runtime.Version = "999.0.0" }} {
		t.Run(name, func(t *testing.T) {
			changed := original
			change(&changed)
			if err := writePrivateClaudeJSON(path, changed); err != nil {
				t.Fatal(err)
			}
			if _, _, err := validateClaudeLoginControl(m.root, path); err == nil {
				t.Fatal("invalid native control accepted")
			}
		})
	}
	_ = writePrivateClaudeJSON(path, original)
	_ = os.Chmod(path, 0644)
	if _, _, err := validateClaudeLoginControl(m.root, path); err == nil {
		t.Fatal("public control accepted")
	}
	_ = os.Chmod(path, 0600)
	outside := filepath.Join(m.root, "outside.json")
	_ = writePrivateClaudeJSON(outside, original)
	if _, _, err := validateClaudeLoginControl(m.root, outside); err == nil {
		t.Fatal("arbitrary control path accepted")
	}
	_ = os.Remove(path)
	_ = os.Symlink(outside, path)
	if _, _, err := validateClaudeLoginControl(m.root, path); err == nil {
		t.Fatal("symlink control accepted")
	}
}
func TestClaudeProfileCloseCancelsPendingLoginAndMetadataHasNoVendorState(t *testing.T) {
	m := newClaudeProfileFixture(t)
	p, _ := m.Create("Work", "claude_subscription")
	a, _ := m.Login(context.Background(), profileRuntime(), p.ID)
	if err := m.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(m.attemptPath(a.ID), "cancel")); err != nil {
		t.Fatal("app shutdown did not cancel vendor login")
	}
	raw, _ := os.ReadFile(filepath.Join(m.root, "profiles.json"))
	if strings.Contains(string(raw), "account@example.test") || strings.Contains(string(raw), "accessToken") || strings.Contains(string(raw), "refreshToken") {
		t.Fatal("profile metadata contains vendor credential state")
	}
}
func TestClaudeConfigNeverFormatsItsMCPBearerAndAcceptsScopedDottedTools(t *testing.T) {
	c := claudeFixture()
	c.Token = "secret-mcp-bearer-never-display"
	c.Tools = append(c.Tools, ToolSpec{Name: "search.query"})
	if err := c.validate(); err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(c)
	for _, text := range []string{string(raw), fmt.Sprintf("%v %+v %#v", c, c, c)} {
		if strings.Contains(text, c.Token) {
			t.Fatal("ephemeral MCP bearer leaked")
		}
	}
	if !strings.Contains(strings.Join(claudeArguments(c, "/private/mcp.json", nil), "|"), "mcp__trove__search.query") {
		t.Fatal("canonical dotted MCP tool was lost")
	}
}
func TestClaudeNativeHelperRechecksStartupBeforeVendorInvocation(t *testing.T) {
	m := newClaudeProfileFixture(t)
	p, _ := m.Create("Work", "claude_subscription")
	a, _ := m.Login(context.Background(), profileRuntime(), p.ID)
	var calls atomic.Int32
	code := runClaudeLoginHelper(m.root, filepath.Join(m.attemptPath(a.ID), "control.json"), nil, io.Discard, io.Discard, func(context.Context, Runtime, string, string) (*exec.Cmd, error) {
		calls.Add(1)
		return nil, errors.New("startup no longer qualified")
	})
	if code == 0 || calls.Load() != 1 {
		t.Fatal("helper skipped runtime gate")
	}
	profiles, err := m.List()
	if err != nil || profiles[0].Attempt.State != "failed" {
		t.Fatal(profiles, err)
	}
}

func TestClaudeVendorCannotWidenNextLaunchScopeThroughStateSymlink(t *testing.T) {
	m := newClaudeProfileFixture(t)
	p, _ := m.Create("Work", "claude_subscription")
	outside, _ := filepath.EvalSymlinks(t.TempDir())
	if err := os.Chmod(outside, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(m.statePath(p.ID)); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, m.statePath(p.ID)); err != nil {
		t.Fatal(err)
	}
	if _, _, err := prepareClaudeState(profileRuntime(), m.statePath(p.ID)); err == nil || !strings.Contains(err.Error(), "symlink") {
		t.Fatal("state symlink widened next runtime grant", err)
	}
	if _, err := os.Stat(filepath.Join(outside, "vendor")); !os.IsNotExist(err) {
		t.Fatal("host wrote through substituted vendor state")
	}
}
