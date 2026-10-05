//go:build darwin

package providers

import (
	"context"
	"encoding/json"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestContainedProcessProbe(t *testing.T) {
	if os.Getenv("TROVE_CONTAINMENT_CHILD") != "1" {
		return
	}
	outside := os.Getenv("TROVE_TEST_OUTSIDE")
	denied := map[string]bool{}
	_, err := os.ReadFile(outside)
	denied["read"] = err != nil
	err = os.WriteFile(outside, []byte("changed"), 0600)
	denied["write"] = err != nil
	_, err = os.ReadFile(filepath.Join(os.Getenv("HOME"), "outside-link"))
	denied["symlink"] = err != nil
	_, err = os.ReadFile("/System/Volumes/Data" + outside)
	denied["dataVolumeAlias"] = err != nil
	err = exec.Command("/bin/sh", "-c", "exit 0").Run()
	denied["shell"] = err != nil
	_, err = os.ReadFile("/usr/share/icu/icutzformat.txt")
	denied["unrelatedSystemLocaleData"] = err != nil
	_, err = os.ReadDir("/usr/share/icu")
	denied["localeDirectoryListing"] = err != nil
	c, err := net.DialTimeout("tcp", os.Getenv("TROVE_TEST_LISTENER"), time.Second)
	denied["network"] = err != nil
	if c != nil {
		c.Close()
	}
	err = os.WriteFile(filepath.Join(os.Getenv("HOME"), "allowed"), []byte("private state"), 0600)
	denied["privateStateWorks"] = err == nil
	json.NewEncoder(os.Stdout).Encode(denied)
	os.Exit(0)
}

func TestMacOSContainmentDeniesWorkspaceShellAndUnapprovedNetwork(t *testing.T) {
	if os.Getenv("TROVE_PROVIDER_INTEGRATION") != "1" {
		t.Skip("requires actual macOS Seatbelt process execution")
	}
	if _, err := os.Stat("/usr/share/icu/icutzformat.txt"); err != nil {
		t.Fatalf("missing public locale neighbor for denial check: %v", err)
	}
	exe, _ := os.Executable()
	exe, _ = filepath.EvalSymlinks(exe)
	dir := t.TempDir()
	dir, _ = filepath.EvalSymlinks(dir)
	outside := filepath.Join(t.TempDir(), "workspace-canary")
	if err := os.WriteFile(outside, []byte("secret canary"), 0600); err != nil {
		t.Fatal(err)
	}
	outside, _ = filepath.EvalSymlinks(outside)
	if err := os.Symlink(outside, filepath.Join(dir, "outside-link")); err != nil {
		t.Fatal(err)
	}
	l, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	env := append(isolatedEnv(dir), "TROVE_CONTAINMENT_CHILD=1", "TROVE_TEST_OUTSIDE="+outside, "TROVE_TEST_LISTENER="+l.Addr().String())
	cmd, err := containedCommand(ctx, exe, dir, []string{"-test.run=^TestContainedProcessProbe$"}, env, false)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("contained process: %v %s", err, raw)
	}
	var outcome map[string]bool
	if json.Unmarshal(raw, &outcome) != nil {
		t.Fatalf("invalid probe: %s", raw)
	}
	for _, key := range []string{"read", "write", "symlink", "shell", "network", "privateStateWorks", "unrelatedSystemLocaleData", "localeDirectoryListing"} {
		if !outcome[key] {
			t.Errorf("containment check failed: %s", key)
		}
	}
	if !outcome["dataVolumeAlias"] {
		t.Fatal("data volume alias exposed external content")
	}
	retained, _ := os.ReadFile(outside)
	if string(retained) != "secret canary" {
		t.Fatal("outside file changed")
	}
	t.Log("actual Seatbelt denied external content, symlink escape, writes, shell, network; private state retained")
}

func TestInstalledRuntimeDiscovery(t *testing.T) {
	if os.Getenv("TROVE_PROVIDER_INTEGRATION") != "1" {
		t.Skip("requires installed official runtime")
	}
	for _, provider := range []string{"codex", "claude_code"} {
		r := Discover(context.Background(), provider)
		if !r.Available || r.SHA256 == "" || strings.Contains(r.Reason, "credential") {
			t.Fatalf("%s discovery: %+v", provider, r)
		}
		t.Logf("%s version=%s sha256=%s", provider, r.Version, r.SHA256)
	}
}

func TestInstalledCodexSchema(t *testing.T) {
	if os.Getenv("TROVE_PROVIDER_SCHEMA_PROBE") != "1" {
		t.Skip("offline installed-runtime schema probe")
	}
	r := Discover(context.Background(), "codex")
	if !r.Available {
		t.Fatal(r.Reason)
	}
	dir, err := os.MkdirTemp("", "trove-codex-schema-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	dir, _ = filepath.EvalSymlinks(dir)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	cmd, err := containedCommand(ctx, r.Path, dir, []string{"app-server", "generate-json-schema", "--out", filepath.Join(dir, "schema"), "--experimental"}, isolatedEnv(dir), false)
	if err != nil {
		t.Fatal(err)
	}
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("schema: %v %s", err, out)
	}
	t.Logf("offline runtime schemas: %s", filepath.Join(dir, "schema"))
}

func TestInstalledCodexContainedInitializationAndEmptyResumeRefusal(t *testing.T) {
	if os.Getenv("TROVE_PROVIDER_INTEGRATION") != "1" {
		t.Skip("actual installed app-server; no sign-in or inference")
	}
	r := Discover(context.Background(), "codex")
	if !r.Available {
		t.Fatal(r.Reason)
	}
	dir := filepath.Join(t.TempDir(), "session")
	c := CodexConfig{Runtime: r, StateDirectory: dir, Identity: Identity{Provider: "codex", Account: "synthetic-offline-registration", AuthMode: "siwc", BillingMode: "chatgpt_plan", RuntimeVersion: r.Version, AdapterVersion: "2"}, ScopeHash: strings.Repeat("a", 64), Model: "gpt-6-astra", AccessToken: "offline-placeholder-not-a-token", Tools: []ToolSpec{{Name: "readPage", Description: "Read an explicitly selected Trove page", InputSchema: map[string]any{"type": "object", "properties": map[string]any{"pageId": map[string]string{"type": "string"}}, "required": []string{"pageId"}}}}}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	checkpoint, err := runCodex(ctx, c, "offline protocol probe", nil, nil, nil, false, true)
	if err != nil {
		t.Fatal(err)
	}
	if checkpoint.ThreadID == "" {
		t.Fatal("missing durable thread identity")
	}
	// The official runtime does not persist an empty rollout until a turn is
	// recorded. It must refuse this resume, not silently create another thread.
	_, err = runCodex(ctx, c, "offline protocol probe", &checkpoint, nil, nil, false, true)
	if err == nil || !strings.Contains(err.Error(), "no rollout found") {
		t.Fatal("empty native rollout was not refused", err)
	}
	c.ScopeHash = strings.Repeat("b", 64)
	if _, err = runCodex(ctx, c, "offline protocol probe", &checkpoint, nil, nil, false, true); err == nil {
		t.Fatal("accepted changed scope")
	}
	t.Logf("real Codex %s initialized and created a scoped dynamic-tool thread offline; empty-session resume and changed scope rejected; live inference and completed-turn resume remain unqualified", r.Version)
}
