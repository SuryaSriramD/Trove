package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"trove.local/phase0/internal/native"
	"trove.local/phase0/internal/workspace"
)

func TestRealLocalAIWorkflow(t *testing.T) {
	if os.Getenv("TROVE_NATIVE_INTEGRATION") != "1" {
		t.Skip("set TROVE_NATIVE_INTEGRATION=1 with verified model packs")
	}
	k, err := workspace.Create(filepath.Join(t.TempDir(), "ai-workspace"))
	if err != nil {
		t.Fatal(err)
	}
	defer k.Close()
	owner := k.OwnerContext()
	page := workspace.NewID()
	request := map[string]any{"contractVersion": 1, "requestId": workspace.NewID(), "workspaceId": k.ID(), "mutation": map[string]any{"scopeGrantId": owner.ScopeGrantId, "idempotencyKey": workspace.NewID(), "expectedRevisions": []any{map[string]any{"target": map[string]any{"entityType": "page", "entityId": page}, "revision": "0"}}}, "commands": []any{map[string]any{"kind": "page.create", "pageId": page, "title": "The launch code is AMBER-27"}}}
	raw, _ := json.Marshal(request)
	var response map[string]any
	json.Unmarshal(k.Dispatch(context.Background(), owner, "batch", raw), &response)
	if response["ok"] != true {
		t.Fatal(response)
	}
	modelRoot := os.Getenv("TROVE_MODEL_ROOT")
	if modelRoot == "" {
		modelRoot = "/private/tmp/trove-phase1-models"
	}
	manager, err := native.NewManager(modelRoot)
	if err != nil {
		t.Fatal(err)
	}
	bundle := os.Getenv("TROVE_NATIVE_BUNDLE")
	if bundle == "" {
		bundle, _ = filepath.Abs("build/native")
	}
	supervisor, err := native.NewSupervisor(bundle, manager)
	if err != nil {
		t.Fatal(err)
	}
	a := NewApp()
	a.kernel = k
	a.execution = owner
	a.native = &nativeState{manager: manager, supervisor: supervisor}
	defer a.stopAI()
	raw, _ = json.Marshal(map[string]any{"contractVersion": 1, "workspaceId": k.ID(), "pageId": page, "mode": "answer", "prompt": "What is the launch code?", "useMemory": false, "modelPackId": "01990000-0000-7000-8000-000000000105"})
	var started struct {
		OK     bool            `json:"ok"`
		Result workspace.AIRun `json:"result"`
		Error  any             `json:"error"`
	}
	json.Unmarshal([]byte(a.aiDispatch("ai.start", raw)), &started)
	if !started.OK {
		t.Fatal(started.Error)
	}
	deadline := time.Now().Add(3 * time.Minute)
	for time.Now().Before(deadline) {
		run, err := k.AIRun(context.Background(), owner, started.Result.ID)
		if err != nil {
			t.Fatal(err)
		}
		if run.State != "running" {
			if run.State != "completed" || !strings.Contains(strings.ToUpper(run.Text), "AMBER-27") {
				t.Fatalf("local answer qualification: %+v", run)
			}
			if len(run.Citations) != 1 || run.Citations[0].State != "current" || string(run.Citations[0].Location.PageId) != page || string(run.Citations[0].Location.SourceRevision) != "1" || !strings.Contains(run.Text, "[1]") {
				t.Fatalf("exact-source fixture must produce a live citation to its canonical page: %+v", run)
			}
			t.Logf("answer=%q citations=%d warning=%q", run.Text, len(run.Citations), run.Warning)
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatal("local answer timed out")
}
