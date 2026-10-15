package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"trove.local/phase0/internal/connectors"
	"trove.local/phase0/internal/native"
	"trove.local/phase0/internal/workspace"
)

// This exercises actual tokenization/generation with a controlled external
// evidence reader. It is not a live connector/account acceptance result.
func TestRealConnectedLocalAIWorkflow(t *testing.T) {
	if os.Getenv("TROVE_NATIVE_INTEGRATION") != "1" {
		t.Skip("requires selected native model execution")
	}
	k, err := workspace.Create(filepath.Join(t.TempDir(), "connected-ai"))
	if err != nil {
		t.Fatal(err)
	}
	defer k.Close()
	owner := k.OwnerContext()
	ctx := context.Background()
	account, err := k.ConnectConnector(ctx, owner, connectors.Account{Kind: connectors.GitHub, Identity: connectors.Identity{Provider: "github", Subject: "synthetic-owner", Label: "Independent connected fixture"}, RegistrationID: workspace.NewID()}, "")
	if err != nil {
		t.Fatal(err)
	}
	account, err = k.SetConnectorSelections(ctx, owner, account.ID, account.Revision, []connectors.Selection{{ID: "fixture/repo", Kind: "repository", Label: "Selected repository"}})
	if err != nil {
		t.Fatal(err)
	}
	source, err := k.StoreConnectorPreview(ctx, owner, account, "fixture/repo", connectors.Snapshot{RemoteID: "issue:7", RemoteRevision: "fixture-v1", Kind: "github_issue", Title: "Launch code", Text: "The launch code is AMBER-27.", URL: "https://github.com/fixture/repo/issues/7", State: "present", Content: json.RawMessage(`{}`)})
	if err != nil {
		t.Fatal(err)
	}
	var reads atomic.Int32
	k.SetConnectorContextReader(func(_ context.Context, a connectors.Account, s connectors.Selection, id string) (connectors.Snapshot, error) {
		if a.ID != account.ID || s.ID != "fixture/repo" || id != source.RemoteID {
			t.Error("expanded source context")
		}
		reads.Add(1)
		return source, nil
	})
	manager, err := native.NewManager(os.Getenv("TROVE_MODEL_ROOT"))
	if err != nil {
		t.Fatal(err)
	}
	supervisor, err := native.NewSupervisor(os.Getenv("TROVE_NATIVE_BUNDLE"), manager)
	if err != nil {
		t.Fatal(err)
	}
	a := NewApp()
	a.kernel = k
	a.execution = owner
	a.native = &nativeState{manager: manager, supervisor: supervisor}
	defer a.stopAI()
	raw, _ := json.Marshal(map[string]any{"contractVersion": 1, "requestId": workspace.NewID(), "workspaceId": k.ID(), "mode": "answer", "prompt": "What is the launch code?", "useMemory": false, "modelPackId": "01990000-0000-7000-8000-000000000106", "connectedSources": []workspace.ConnectedSourceReference{workspace.ConnectorContextReference(source)}})
	var started struct {
		OK     bool            `json:"ok"`
		Result workspace.AIRun `json:"result"`
		Error  any             `json:"error"`
	}
	if err = json.Unmarshal([]byte(a.aiDispatch("ai.start", raw)), &started); err != nil || !started.OK {
		t.Fatal(err, started.Error)
	}
	deadline := time.Now().Add(3 * time.Minute)
	for time.Now().Before(deadline) {
		run, err := k.AIRun(ctx, owner, started.Result.ID)
		if err != nil {
			t.Fatal(err)
		}
		if run.State != "running" {
			if run.State != "completed" || !strings.Contains(strings.ToUpper(run.Text), "AMBER-27") || !strings.Contains(run.Text, "[1]") {
				t.Fatalf("actual connected answer failed: %+v", run)
			}
			if len(run.Citations) != 1 || run.Citations[0].Connected == nil || run.Citations[0].Connected.RemoteRevision != "fixture-v1" || run.Citations[0].Location.PageId != "" || run.Citations[0].State != "current" {
				t.Fatalf("external citation identity lost: %+v", run.Citations)
			}
			if reads.Load() < 3 {
				t.Fatal("source access was not rechecked through publication", reads.Load())
			}
			t.Logf("controlled-source/native-model answer=%q currentConnectedCitations=%d authenticatedReaderChecks=%d liveAccount=false", run.Text, len(run.Citations), reads.Load())
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatal("connected native answer timed out")
}
