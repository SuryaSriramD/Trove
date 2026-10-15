package main

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"trove.local/phase0/contracts"
	"trove.local/phase0/internal/workspace"
)

func TestDesktopProviderDisconnectInterruptsApproval(t *testing.T) {
	ctx := context.Background()
	k, err := workspace.Create(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer k.Close()
	a := NewApp()
	a.ctx, a.kernel, a.execution = ctx, k, k.OwnerContext()
	a.completeStartup(nil)
	page := workspace.NewID()
	guard := func(revision string) contracts.ServiceExpectedRevision {
		return contracts.ServiceExpectedRevision{Target: contracts.ServiceEntityRef{EntityType: "page", EntityId: contracts.DomainId(page)}, Revision: revision}
	}
	request := map[string]any{"contractVersion": 1, "requestId": workspace.NewID(), "workspaceId": k.ID(), "mutation": map[string]any{"scopeGrantId": a.execution.ScopeGrantId, "idempotencyKey": workspace.NewID(), "expectedRevisions": []contracts.ServiceExpectedRevision{guard("0")}}, "commands": []any{map[string]any{"kind": "page.create", "pageId": page, "title": "Before"}}}
	raw, _ := json.Marshal(request)
	var created struct {
		OK bool `json:"ok"`
	}
	if err = json.Unmarshal(k.Dispatch(ctx, a.execution, "batch", raw), &created); err != nil || !created.OK {
		t.Fatal("create page", err)
	}
	bridge := workspace.NewProviderBridge(k, a.execution)
	providerHosts.Store(a, &providerHost{bridge: bridge})
	defer a.stopProviders()
	connection, err := bridge.Connect(ctx, "mcp", []string{page}, []string{"proposeChanges"})
	if err != nil {
		t.Fatal(err)
	}
	raw, err = bridge.Call(ctx, connection.ID, connection.Token, "proposeChanges", map[string]any{"idempotencyKey": "desktop-disconnect", "commands": []any{map[string]any{"kind": "page.update", "pageId": page, "title": "Must not commit"}}, "expectedRevisions": []contracts.ServiceExpectedRevision{guard("1")}})
	if err != nil {
		t.Fatal(err)
	}
	var proposal struct {
		Result workspace.ProviderProposal `json:"result"`
	}
	if err = json.Unmarshal(raw, &proposal); err != nil {
		t.Fatal(err)
	}
	entered, release := make(chan struct{}), make(chan struct{})
	workspace.RegisterCommitValidator(func(m *workspace.Mutation) error {
		if m.Kernel == k && m.Execution.Actor.Kind == "external_client" {
			close(entered)
			<-release
		}
		return nil
	})
	invoke := func(method string, fields map[string]any) string {
		fields["contractVersion"], fields["workspaceId"] = 1, k.ID()
		body, _ := json.Marshal(fields)
		return a.Dispatch(method, string(body))
	}
	approved := make(chan string, 1)
	go func() { approved <- invoke("providers.approve", map[string]any{"proposalId": proposal.Result.ID}) }()
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		close(release)
		t.Fatal("approval did not reach commit")
	}
	disconnected := make(chan string, 1)
	go func() { disconnected <- invoke("providers.disconnect", map[string]any{"connectionId": connection.ID}) }()
	revoked := false
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if bridge.Status()[0].State == "disconnected" {
			revoked = true
			break
		}
		time.Sleep(time.Millisecond)
	}
	close(release)
	for _, done := range []chan string{approved, disconnected} {
		select {
		case <-done:
		case <-time.After(3 * time.Second):
			t.Fatal("provider request did not exit")
		}
	}
	if !revoked {
		t.Fatal("desktop serialized disconnect behind approval")
	}
	raw, _ = json.Marshal(map[string]any{"contractVersion": 1, "requestId": workspace.NewID(), "workspaceId": k.ID(), "pageId": page})
	var read struct {
		OK     bool `json:"ok"`
		Result struct {
			Page struct {
				Title string `json:"title"`
			} `json:"page"`
		} `json:"result"`
	}
	if err = json.Unmarshal(k.Dispatch(ctx, a.execution, "readPage", raw), &read); err != nil || !read.OK || read.Result.Page.Title != "Before" {
		t.Fatalf("disconnected mutation committed: %s (%v)", raw, err)
	}
}

func TestDesktopProviderDisconnectRevokesBeforeManagedProcessExits(t *testing.T) {
	ctx := context.Background()
	k, err := workspace.Create(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer k.Close()
	a := NewApp()
	a.ctx, a.kernel, a.execution = ctx, k, k.OwnerContext()
	a.completeStartup(nil)
	bridge := workspace.NewProviderBridge(k, a.execution)
	page := workspace.NewID()
	raw, _ := json.Marshal(map[string]any{"contractVersion": 1, "requestId": workspace.NewID(), "workspaceId": k.ID(), "mutation": map[string]any{"scopeGrantId": a.execution.ScopeGrantId, "idempotencyKey": workspace.NewID(), "expectedRevisions": []any{map[string]any{"target": map[string]any{"entityType": "page", "entityId": page}, "revision": "0"}}}, "commands": []any{map[string]any{"kind": "page.create", "pageId": page, "title": "Scoped source"}}})
	var created struct {
		OK bool `json:"ok"`
	}
	if json.Unmarshal(k.Dispatch(ctx, a.execution, "batch", raw), &created) != nil || !created.OK {
		t.Fatal("fixture page creation failed")
	}
	connection, err := bridge.Connect(ctx, "mcp", []string{page}, []string{"readPage"})
	if err != nil {
		t.Fatal(err)
	}
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	exited := make(chan struct{})
	h := &providerHost{bridge: bridge, managed: &managedExecution{run: workspace.ManagedRun{ConnectionID: connection.ID}, cancel: cancel, done: exited}}
	providerHosts.Store(a, h)
	defer providerHosts.Delete(a)
	response := make(chan string, 1)
	raw, _ = json.Marshal(map[string]any{"contractVersion": 1, "workspaceId": k.ID(), "connectionId": connection.ID})
	go func() { response <- a.Dispatch("providers.disconnect", string(raw)) }()
	select {
	case <-runCtx.Done():
	case <-time.After(3 * time.Second):
		close(exited)
		t.Fatal("managed cancellation not requested")
	}
	_, readErr := bridge.Call(ctx, connection.ID, connection.Token, "readPage", map[string]any{"pageId": page})
	close(exited)
	select {
	case <-response:
	case <-time.After(3 * time.Second):
		t.Fatal("disconnect did not complete")
	}
	if readErr == nil {
		t.Fatal("managed process still had canonical access while disconnect waited for its exit")
	}
}
