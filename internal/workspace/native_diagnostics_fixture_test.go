package workspace

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"trove.local/phase0/contracts"
	"trove.local/phase0/internal/platformfs"
)

// This source-only fixture is independent of editor gesture success. Every
// canonical row/file is created through the real service command boundary.
var nativeDiagnosticsSentinels = []string{
	"PRIVATE_TITLE_SENTINEL_20261009",
	"PRIVATE_BODY_SENTINEL_20261009",
	"PRIVATE_MEMORY_SENTINEL_20261009",
	"PRIVATE_ASSET_SENTINEL_20261009",
	"PRIVATE_PATH_SENTINEL_20261009",
	"PRIVATE_METHOD_SENTINEL_20261009",
	"PRIVATE_TOKEN_SENTINEL_20261009",
}

func seedNativeDiagnosticsFixture(t *testing.T, k *Kernel, e contracts.ServiceExecutionContext) map[string]any {
	t.Helper()
	page := seedExpandedQA(t, k, e, expandedQACase{Title: nativeDiagnosticsSentinels[0], Source: nativeDiagnosticsSentinels[1] + " — invented private fixture content, not user data.", Memories: []expandedMemory{{Kind: "preference", Content: nativeDiagnosticsSentinels[2] + " synthetic confidential preference."}}})
	prepared := stageTestFile(t, k, e, nativeDiagnosticsSentinels[3]+"\nExact nonempty attachment bytes: తెలుగు 中文 🌱\n")
	file, block := attachTestFile(t, k, e, page, prepared, true)
	// This is an actual refused service dispatch, not a fabricated log event.
	response := k.Dispatch(context.Background(), e, "imports."+nativeDiagnosticsSentinels[5], json.RawMessage(`{"token":"PRIVATE_TOKEN_SENTINEL_20261009","path":"/PRIVATE_PATH_SENTINEL_20261009/private-account"}`))
	var rejected map[string]any
	if err := json.Unmarshal(response, &rejected); err != nil || rejected["ok"] != false {
		t.Fatal("expected denied unknown method", string(response), err)
	}
	before, err := readAICanonicalSnapshot(k)
	if err != nil {
		t.Fatal(err)
	}
	if len(before.Assets) != 1 {
		t.Fatalf("expected nonempty canonical asset inventory, got %d", len(before.Assets))
	}
	preview, err := k.PrepareSupportBundle(context.Background(), e, SupportSelection{Diagnostics: true}, SupportRuntime{})
	if err != nil {
		t.Fatal(err)
	}
	files := preview.Preview().Files
	if len(files) != 3 {
		t.Fatal("metadata/manifest/diagnostics inventory", len(files))
	}
	forbidden := append([]string{}, nativeDiagnosticsSentinels...)
	forbidden = append(forbidden, page, file, block, k.ID(), k.Path(), string(e.ScopeGrantId))
	diagnosticNonempty := false
	for _, f := range files {
		for _, v := range forbidden {
			if strings.Contains(f.Content, v) {
				t.Fatalf("private sentinel leaked in model-free preview %s", f.Name)
			}
		}
		if f.Name == "diagnostics.json" && strings.Contains(f.Content, `"component": "import"`) {
			diagnosticNonempty = true
		}
	}
	if !diagnosticNonempty {
		t.Fatal("actual rejected import dispatch missing from safe operational diagnostics")
	}
	after, err := readAICanonicalSnapshot(k)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(before)
	a, _ := json.Marshal(after)
	if string(b) != string(a) {
		t.Fatal("support preview changed protected rows/assets")
	}
	return map[string]any{"schemaVersion": 1, "fixtureKind": "independent-nonempty-diagnostics", "pageId": page, "fileId": file, "fileBlockId": block, "workspaceId": k.ID(), "scopeGrantId": string(e.ScopeGrantId), "sentinels": forbidden, "canonicalRowsAndAssetsBeforeSupportPreview": before, "modelFreePreviewPrivacyPassed": true, "diagnosticEvent": "actual rejected imports.PRIVATE_METHOD_SENTINEL_20261009 service dispatch; no private payload retained", "nativePreviewDiscardExportExecuted": false, "releaseQualified": false}
}
func TestNativeDiagnosticsFixtureSentinels(t *testing.T) {
	k, e := testKernel(t)
	seedNativeDiagnosticsFixture(t, k, e)
}

func TestCreateNativeDiagnosticsQualificationFixture(t *testing.T) {
	destination := os.Getenv("TROVE_NATIVE_DIAGNOSTICS_FIXTURE")
	if destination == "" {
		t.Skip("requires explicit unused durable qualification fixture destination")
	}
	if !filepath.IsAbs(destination) || filepath.Clean(destination) != destination {
		t.Fatal("absolute clean destination required")
	}
	parent, err := filepath.EvalSymlinks(filepath.Dir(destination))
	if err != nil || parent != filepath.Dir(destination) {
		t.Fatal("destination parent must exist without symlink ancestors", err)
	}
	if err = os.Mkdir(destination, 0700); err != nil {
		t.Fatal("exclusive unused destination required", err)
	}
	workspace := filepath.Join(destination, "PRIVATE_PATH_SENTINEL_20261009")
	k, err := Create(workspace)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { k.Close() })
	report := seedNativeDiagnosticsFixture(t, k, k.OwnerContext())
	report["workspacePath"] = workspace
	if err = k.Close(); err != nil {
		t.Fatal(err)
	}
	lock, err := platformfs.AcquireLock(filepath.Join(workspace, ".owner.lock"))
	if err != nil {
		t.Fatal(err)
	}
	defer platformfs.ReleaseLock(lock)
	inventory := map[string]any{}
	err = filepath.WalkDir(workspace, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("unexpected link %s", path)
		}
		f, err := os.Open(path)
		if err != nil {
			return err
		}
		defer f.Close()
		if err = f.Sync(); err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		info, err := f.Stat()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("nonregular fixture entry")
		}
		h := sha256.New()
		n, err := io.Copy(h, f)
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(workspace, path)
		inventory[filepath.ToSlash(rel)] = map[string]any{"size": n, "sha256": fmt.Sprintf("%x", h.Sum(nil))}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	report["closedWorkspaceInventory"] = inventory
	report["closedOwnerLockHeldDuringFsyncAndInventory"] = true
	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	out, err := os.OpenFile(filepath.Join(destination, "fixture.json"), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = out.Write(append(data, '\n')); err == nil {
		err = out.Sync()
	}
	closeErr := out.Close()
	if err != nil || closeErr != nil {
		t.Fatal(err, closeErr)
	}
	dir, err := os.Open(destination)
	if err != nil {
		t.Fatal(err)
	}
	err = dir.Sync()
	dir.Close()
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("Retained actual canonical diagnostics fixture and inventory at %s; no native UI observation claimed", destination)
}
