package main

import (
	"context"
	"encoding/json"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"trove.local/phase0/internal/connectors"
	"trove.local/phase0/internal/workspace"
)

func TestConnectedProposalDispatchReleasesAppMutexDuringSourceRead(t *testing.T) {
	ctx := context.Background()
	k, err := workspace.Create(filepath.Join(t.TempDir(), "workspace"))
	if err != nil {
		t.Fatal(err)
	}
	defer k.Close()
	e := k.OwnerContext()
	a := NewApp()
	a.kernel = k
	a.execution = e
	a.ctx = ctx
	a.completeStartup(nil)
	account, err := k.ConnectConnector(ctx, e, connectors.Account{Kind: connectors.GitHub, Identity: connectors.Identity{Provider: "github", Subject: "fixture"}, RegistrationID: workspace.NewID()}, "")
	if err != nil {
		t.Fatal(err)
	}
	account, err = k.SetConnectorSelections(ctx, e, account.ID, account.Revision, []connectors.Selection{{ID: "fixture/repo", Kind: "repository"}})
	if err != nil {
		t.Fatal(err)
	}
	source, err := k.StoreConnectorPreview(ctx, e, account, "fixture/repo", connectors.Snapshot{RemoteID: "issue:1", RemoteRevision: "r1", Kind: "issue", Title: "Launch", Text: "The launch code is AMBER-27.", State: "present", Content: json.RawMessage(`{}`)})
	if err != nil {
		t.Fatal(err)
	}
	var checking atomic.Bool
	entered, release := make(chan struct{}, 1), make(chan struct{})
	k.SetConnectorContextReader(func(context.Context, connectors.Account, connectors.Selection, string) (connectors.Snapshot, error) {
		if checking.Load() {
			entered <- struct{}{}
			<-release
			a.mu.RLock()
			active := a.kernel == k
			a.mu.RUnlock()
			if !active {
				t.Error("workspace changed during admitted call")
			}
		}
		return source, nil
	})
	p, err := k.PrepareAIWithBudget(ctx, e, workspace.AIRequest{Mode: "answer", Prompt: "What is the launch code?", ConnectedSources: []workspace.ConnectedSourceReference{workspace.ConnectorContextReference(source)}}, workspace.AIBudget{ContextTokens: 8192, OutputTokens: 512, CountTokens: func(_ context.Context, p []workspace.AIPrompt) ([]int, error) {
		out := make([]int, len(p))
		for i := range out {
			out[i] = 512
		}
		return out, nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	run, err := k.FinishAI(ctx, e, p.Run.ID, "The launch code is AMBER-27 [1].", "completed", "")
	if err != nil {
		t.Fatal(err)
	}
	checking.Store(true)
	raw, _ := json.Marshal(map[string]any{"contractVersion": 1, "requestId": workspace.NewID(), "workspaceId": k.ID(), "runId": run.ID, "kind": "create_page", "title": "Reviewed note"})
	completed := make(chan string, 1)
	go func() { completed <- a.Dispatch("ai.prepareAction", string(raw)) }()
	select {
	case <-entered:
	case result := <-completed:
		t.Fatal("source read not reached", result)
	case <-time.After(2 * time.Second):
		t.Fatal("source read not reached")
	}
	written := make(chan struct{})
	go func() { a.mu.Lock(); a.pendingDeepLink = "fixture"; a.mu.Unlock(); close(written) }()
	select {
	case <-written:
	case <-time.After(time.Second):
		close(release)
		t.Fatal("service dispatch held app mutex across native callback")
	}
	close(release)
	select {
	case result := <-completed:
		var wire struct {
			OK bool `json:"ok"`
		}
		if json.Unmarshal([]byte(result), &wire) != nil || !wire.OK {
			t.Fatal(result)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("native callback deadlocked with app writer")
	}
}
