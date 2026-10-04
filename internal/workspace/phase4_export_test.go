package workspace

import (
	"archive/zip"
	"context"
	"encoding/json"
	"io"
	"path/filepath"
	"strings"
	"testing"
)

func TestPhase4PortableMetadataRetainedWithoutActivation(t *testing.T) {
	k, e := testKernel(t)
	ctx := context.Background()
	page := createPage(t, k, e, "Reusable workflow")
	saveRule(t, k, e, page, scheduleRule(page))
	testOK(t, testDispatch(t, k, e, "editor.saveTemplate", advancedRequest(k, map[string]any{"pageId": page, "name": "Daily body", "expectedPageRevision": "2"})))
	testOK(t, testDispatch(t, k, e, "editor.saveButton", advancedRequest(k, map[string]any{"pageId": page, "label": "Rename", "commands": []any{map[string]any{"kind": "page.update", "pageId": page, "title": "Clicked"}}})))
	dest := filepath.Join(t.TempDir(), "portable.zip")
	if _, err := k.ExportPortable(ctx, e, dest); err != nil {
		t.Fatal(err)
	}
	z, err := zip.OpenReader(dest)
	if err != nil {
		t.Fatal(err)
	}
	defer z.Close()
	entries := map[string]string{}
	var manifest map[string]any
	for _, f := range z.File {
		r, err := f.Open()
		if err != nil {
			t.Fatal(err)
		}
		data, err := io.ReadAll(r)
		r.Close()
		if err != nil {
			t.Fatal(err)
		}
		entries[f.Name] = string(data)
		if f.Name == "trove-manifest.json" {
			if err = json.Unmarshal(data, &manifest); err != nil {
				t.Fatal(err)
			}
		}
	}
	meta, ok := manifest["phase4Metadata"].(map[string]any)
	if !ok {
		t.Fatal("metadata missing")
	}
	for _, name := range []string{"templates", "buttonActions", "automationDefinitions"} {
		if len(meta[name].([]any)) != 1 {
			t.Fatal("lost definitions", name)
		}
	}
	if !strings.Contains(str(meta["activationPolicy"]), "review") {
		t.Fatal("missing activation policy")
	}
	restored, owner := testKernel(t)
	plan := importFixture(t, restored, owner, entries)
	completeImportFixture(t, restored, owner, plan)
	var rules, buttons int
	restored.db.QueryRow("SELECT count(*) FROM automation_rules").Scan(&rules)
	restored.db.QueryRow("SELECT count(*) FROM advanced_button_actions").Scan(&buttons)
	if rules != 0 || buttons != 0 {
		t.Fatal("untrusted actions activated on import")
	}
}
