package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"trove.local/phase0/contracts"
	"trove.local/phase0/internal/workspace"
)

func TestManagedProviderRejectsWrongWorkspaceAndActorBeforeAuth(t *testing.T) {
	k, err := workspace.Create(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer k.Close()
	a := NewApp()
	a.kernel = k
	a.ctx = context.Background()
	a.execution = k.OwnerContext()
	request := map[string]any{"contractVersion": 1, "requestId": workspace.NewID(), "workspaceId": workspace.NewID(), "registrationId": workspace.NewID()}
	check := func(code string) {
		t.Helper()
		raw, _ := json.Marshal(request)
		var result struct {
			OK    bool `json:"ok"`
			Error struct {
				Code string `json:"code"`
			} `json:"error"`
		}
		json.Unmarshal([]byte(a.managedProviderDispatch("providers.models", raw)), &result)
		if result.OK || result.Error.Code != code {
			t.Fatalf("unexpected boundary response: %+v", result)
		}
		if _, ok := providerAuthStates.Load(a); ok {
			t.Fatal("untrusted request opened account storage")
		}
	}
	check("WORKSPACE_LOCKED")
	request["workspaceId"] = k.ID()
	a.execution = k.Grant(contracts.ServiceActor{Kind: "external_client", ActorId: contracts.DomainId(workspace.NewID())}, "client", a.execution.Scope)
	check("SCOPE_DENIED")
}

func TestManagedStateCannotNestThroughUncreatedSymlinkParent(t *testing.T) {
	root := t.TempDir()
	workspacePath := filepath.Join(root, "workspace")
	if err := os.Mkdir(workspacePath, 0700); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(root, "alias")
	if err := os.Symlink(workspacePath, alias); err != nil {
		t.Fatal(err)
	}
	if !pathsOverlap(filepath.Join(alias, "not-created", "providers"), workspacePath) {
		t.Fatal("provider state escaped disjoint-path check via symlink")
	}
	if pathsOverlap(filepath.Join(root, "outside", "future"), workspacePath) {
		t.Fatal("independent state falsely denied")
	}
	if !pathsOverlap(string(filepath.Separator), workspacePath) {
		t.Fatal("filesystem root can encompass credential state")
	}
}

func TestInvalidSignOutCannotCancelProviderRun(t *testing.T) {
	a := NewApp()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	h := &providerHost{managed: &managedExecution{run: workspace.ManagedRun{Account: "account"}, cancel: cancel, done: make(chan struct{})}}
	providerHosts.Store(a, h)
	defer providerHosts.Delete(a)
	a.providerDispatch("providers.auth.signOut", json.RawMessage(`{"registrationId":"account"}`))
	if ctx.Err() != nil {
		t.Fatal("malformed sign-out cancelled an active run")
	}
}
