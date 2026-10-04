package main

import (
	"context"
	"encoding/json"
	"testing"

	"trove.local/phase0/contracts"
	"trove.local/phase0/internal/workspace"
)

func TestClaudeProfileRoutesRejectUntrustedRequestsBeforeHostStorage(t *testing.T) {
	k, err := workspace.Create(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer k.Close()
	app := NewApp()
	app.kernel = k
	app.ctx = context.Background()
	app.execution = k.OwnerContext()
	request := func(extra map[string]any) json.RawMessage {
		v := map[string]any{"contractVersion": 1, "workspaceId": k.ID(), "requestId": workspace.NewID()}
		for key, value := range extra {
			v[key] = value
		}
		b, _ := json.Marshal(v)
		return b
	}
	check := func(method string, raw json.RawMessage, code string) {
		t.Helper()
		var result struct {
			OK    bool `json:"ok"`
			Error struct {
				Code string `json:"code"`
			} `json:"error"`
		}
		_ = json.Unmarshal([]byte(app.providerClaudeDispatch(method, raw)), &result)
		if result.OK || result.Error.Code != code {
			t.Fatal(result)
		}
		if _, ok := providerClaudeStates.Load(app); ok {
			t.Fatal("untrusted request touched host profile storage")
		}
	}
	check("providers.claude.list", request(map[string]any{"path": "/private/credentials"}), "INVALID_REQUEST")
	check("providers.claude.login", request(map[string]any{"profileId": "../../escape"}), "INVALID_REQUEST")
	check("providers.claude.login", request(map[string]any{"profileId": workspace.NewID(), "code": "secret"}), "INVALID_REQUEST")
	check("providers.claude.create", request(map[string]any{"label": "Work", "billingMode": "automatic"}), "INVALID_REQUEST")
	check("providers.claude.list", request(map[string]any{"workspaceId": workspace.NewID()}), "WORKSPACE_LOCKED")
	owner := app.execution
	app.execution = k.Grant(contracts.ServiceActor{Kind: "external_client", ActorId: contracts.DomainId(workspace.NewID())}, "external", owner.Scope)
	check("providers.claude.list", request(nil), "SCOPE_DENIED")
	app.execution = owner
	k.RevokeGrant(string(owner.ScopeGrantId))
	check("providers.claude.list", request(nil), "SCOPE_DENIED")
}
