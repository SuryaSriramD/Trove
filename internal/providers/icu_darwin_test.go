//go:build darwin

package providers

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestICUPolicyOnlyGrantsValidatedSystemData(t *testing.T) {
	policy, err := macOSICUReadPolicy()
	if err != nil {
		t.Fatal(err)
	}
	if policy == "" {
		t.Skip("this macOS has no separate ICU data archive")
	}
	if !strings.Contains(policy, "(allow file-read-data (literal \"/usr/share/icu/icudt") || strings.Contains(policy, "subpath") || strings.Contains(policy, "file-write") || strings.Contains(policy, "file-map-executable") {
		t.Fatal("locale policy is broader than data reads", policy)
	}
	for _, name := range []string{"../icudt78l.dat", "icudt78l.dat/link", "credentials.dat", "icutzformat.txt", "icudt78l.dat.bak"} {
		if macOSICUName.MatchString(name) {
			t.Fatal("accepted unrelated locale path", name)
		}
	}
	file := filepath.Join(t.TempDir(), "icudt78l.dat")
	if err = os.WriteFile(file, []byte("not system data"), 0600); err != nil {
		t.Fatal(err)
	}
	info, err := os.Lstat(file)
	if err != nil {
		t.Fatal(err)
	}
	if trustedICUObject(info, false) {
		t.Fatal("accepted user-owned ICU data")
	}
	if err = os.Symlink("/usr/share/icu", filepath.Join(filepath.Dir(file), "link")); err != nil {
		t.Fatal(err)
	}
	info, err = os.Lstat(filepath.Join(filepath.Dir(file), "link"))
	if err != nil || trustedICUObject(info, true) {
		t.Fatal("accepted linked directory", err)
	}
}

func TestMacOSICUCollatorRequiresOnlyLiteralSystemData(t *testing.T) {
	if os.Getenv("TROVE_PROVIDER_INTEGRATION") != "1" {
		t.Skip("requires actual macOS containment and C toolchain")
	}
	rule, err := macOSICUReadPolicy()
	if err != nil {
		t.Fatal(err)
	}
	if rule == "" {
		t.Skip("no separate ICU archive")
	}
	state, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	binary := filepath.Join(state, "icu-probe")
	build := exec.Command("/usr/bin/clang", "testdata/icu_probe.c", "-licucore", "-o", binary)
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("compile ICU fixture: %v %s", err, out)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	for _, allowed := range []bool{false, true} {
		cmd, err := containedCommand(ctx, binary, state, nil, isolatedEnv(state), false)
		if err != nil {
			t.Fatal(err)
		}
		if !allowed {
			cmd.Args[2] = strings.Replace(cmd.Args[2], rule, "", 1)
		}
		out, err := cmd.Output()
		if err != nil {
			t.Fatal(err)
		}
		var result struct {
			Status   int    `json:"status"`
			Name     string `json:"name"`
			Collator bool   `json:"collator"`
		}
		if err = json.Unmarshal(out, &result); err != nil {
			t.Fatal(err)
		}
		if allowed {
			if result.Status > 0 || !result.Collator {
				t.Fatal("literal data read did not initialize collator", string(out))
			}
		} else if result.Name != "U_FILE_ACCESS_ERROR" || result.Collator {
			t.Fatal("baseline did not reproduce missing ICU dependency", string(out))
		}
		t.Logf("literal system-data read=%v result=%s", allowed, strings.TrimSpace(string(out)))
	}
}

func TestInstalledClaudeLocaleStartupPassesWithContainedSystemData(t *testing.T) {
	if os.Getenv("TROVE_PROVIDER_INTEGRATION") != "1" {
		t.Skip("requires installed native Claude runtime")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	r := Discover(ctx, "claude_code")
	if !r.Available {
		t.Fatal(r.Reason)
	}
	qualified := QualifyClaudeRuntime(ctx, r)
	if !qualified.Available {
		t.Fatal(qualified.Reason)
	}
	for _, args := range [][]string{{"--version"}, {"--help"}, {"auth", "login", "--help"}} {
		state, err := filepath.EvalSymlinks(t.TempDir())
		if err != nil {
			t.Fatal(err)
		}
		cmd, err := containedCommand(ctx, r.Path, state, args, claudeEnv(state), false)
		if err != nil {
			t.Fatal(err)
		}
		var stdout, stderr limitedBuffer
		cmd.Stdout = &stdout
		cmd.Stderr = &stderr
		if err = cmd.Run(); err != nil {
			t.Fatalf("%v: %v %s", args, err, stderr.String())
		}
		if stdout.String() == "" {
			t.Fatal("missing official command output", args)
		}
	}
	t.Logf("Claude %s sha256=%s version/help/auth-help pass in fresh networkless profiles; no sign-in or inference", r.Version, r.SHA256)
}
