package main

import (
	"context"
	"encoding/json"
	"testing"
	"trove.local/phase0/contracts"
	"trove.local/phase0/internal/workspace"
)

func TestRemoteHostRejectsUntrustedControlBeforeNativeAccess(t *testing.T) {
	k, err := workspace.Create(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer k.Close()
	app := NewApp()
	app.kernel = k
	app.ctx = context.Background()
	app.execution = k.OwnerContext()
	defer app.stopProviders()
	defer app.stopProviderRemote()
	request := func(extra map[string]any) json.RawMessage {
		v := map[string]any{"contractVersion": 1, "requestId": workspace.NewID(), "workspaceId": k.ID()}
		for key, value := range extra {
			v[key] = value
		}
		b, _ := json.Marshal(v)
		return b
	}
	check := func(method string, raw json.RawMessage, want string) {
		t.Helper()
		var out struct {
			OK    bool `json:"ok"`
			Error struct {
				Code string `json:"code"`
			} `json:"error"`
		}
		json.Unmarshal([]byte(app.providerRemoteDispatch(method, raw)), &out)
		if out.OK || out.Error.Code != want {
			t.Fatalf("got %+v want %s", out, want)
		}
		if _, ok := providerRemoteStates.Load(app); ok {
			t.Fatal("invalid caller touched native remote state")
		}
	}
	check("providers.remote.selectCertificate", request(map[string]any{"path": "/private/key.pem"}), "INVALID_REQUEST")
	check("providers.remote.enable", request(map[string]any{"certificateHandle": "/private/key.pem"}), "INVALID_REQUEST")
	check("providers.remote.status", request(map[string]any{"workspaceId": workspace.NewID()}), "WORKSPACE_LOCKED")
	owner := app.execution
	for _, kind := range []string{"external_client", "local_ai"} {
		app.execution = k.Grant(contracts.ServiceActor{Kind: kind, ActorId: contracts.DomainId(workspace.NewID())}, "external", owner.Scope)
		check("providers.remote.selectCertificate", request(nil), "SCOPE_DENIED")
	}
	app.execution = owner
	k.RevokeGrant(string(owner.ScopeGrantId))
	check("providers.remote.status", request(nil), "SCOPE_DENIED")
}
