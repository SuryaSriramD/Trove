package workspace

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestOperationalDiagnosticsConsentExactPreviewAndPrivacy(t *testing.T) {
	k, e := testKernel(t)
	page := createPage(t, k, e, "secret-page-title")
	// A hostile method and payload must not become a diagnostic field.
	k.Dispatch(context.Background(), e, "imports.secret-token", json.RawMessage(`{"secret":"/private/account"}`))
	k.diagnostics.Record("model", "prepare", "failed", "secret-code", time.Second, 8192)
	plain, err := k.PrepareSupportBundle(context.Background(), e, SupportSelection{}, SupportRuntime{})
	if err != nil {
		t.Fatal(err)
	}
	if len(plain.Preview().Files) != 2 {
		t.Fatal("diagnostics enabled without consent")
	}
	b, err := k.PrepareSupportBundle(context.Background(), e, SupportSelection{Diagnostics: true}, SupportRuntime{})
	if err != nil {
		t.Fatal(err)
	}
	if len(b.Preview().Files) != 3 {
		t.Fatal(b.Preview())
	}
	// Later events must never alter the exact approved export bytes.
	k.diagnostics.Record("job", "transition", "failed", "INTERNAL", 0, 0)
	destination := filepath.Join(t.TempDir(), "support.zip")
	if err = k.ExportSupportBundle(context.Background(), e, b, destination); err != nil {
		t.Fatal(err)
	}
	files := supportArchive(t, destination)
	for _, f := range b.Preview().Files {
		if files[f.Name] != f.Content {
			t.Fatal("preview changed")
		}
	}
	for _, secret := range []string{"secret-", "/private/account", page, k.ID(), k.Path(), string(e.ScopeGrantId)} {
		if strings.Contains(files["diagnostics.json"], secret) {
			t.Fatal("diagnostic secret leak", secret)
		}
	}
	if !strings.Contains(files["diagnostics.json"], `"component": "import"`) || !strings.Contains(files["diagnostics.json"], `"count": 8192`) {
		t.Fatal("missing useful operational events", files)
	}
}

func TestDiagnosticsJobSuccessRequiresCommittedValidLease(t *testing.T) {
	k, e := testKernel(t)
	j := enqueueTestJob(t, k, e, "BACKUP_WORKSPACE", map[string]any{"kind": "backup", "destinationHandle": NewID()})
	claimed, err := k.ClaimJob(context.Background(), []string{"BACKUP_WORKSPACE"})
	if err != nil {
		t.Fatal(err)
	}
	before := len(k.diagnostics.Snapshot().Events)
	if err = k.FinishJob(context.Background(), string(j.JobId), "stale-invalid-lease", nil, false); err == nil {
		t.Fatal("invalid lease accepted")
	}
	current, err := k.ReadJob(context.Background(), string(j.JobId))
	if err != nil || current.State != "running" {
		t.Fatal(current, err)
	}
	for _, ev := range k.diagnostics.Snapshot().Events[before:] {
		if ev.Component == "job" && (ev.Outcome == "succeeded" || ev.Outcome == "passed") {
			t.Fatal("false committed success", ev)
		}
	}
	if err = k.FinishJob(context.Background(), string(j.JobId), string(*claimed.LeaseToken), nil, false); err != nil {
		t.Fatal(err)
	}
	current, err = k.ReadJob(context.Background(), string(j.JobId))
	if err != nil || current.State != "succeeded" {
		t.Fatal(current, err)
	}
	successes := 0
	for _, ev := range k.diagnostics.Snapshot().Events[before:] {
		if ev.Component == "job" && ev.Outcome == "succeeded" {
			if ev.Operation != "transition" {
				t.Fatal("ambiguous outcome", ev)
			}
			successes++
		}
	}
	if successes != 1 {
		t.Fatalf("got %d committed successes", successes)
	}
}
