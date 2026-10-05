package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"trove.local/phase0/internal/providers/auth"

	"trove.local/phase0/contracts"
	"trove.local/phase0/internal/workspace"
)

func TestProviderAuthRejectsUntrustedInputsBeforeCredentialStorage(t *testing.T) {
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
		var got struct {
			OK    bool `json:"ok"`
			Error struct {
				Code string `json:"code"`
			} `json:"error"`
		}
		json.Unmarshal([]byte(app.providerAuthDispatch(method, raw)), &got)
		if got.OK || got.Error.Code != code {
			t.Fatalf("unsafe auth request: %+v", got)
		}
		if _, created := providerAuthStates.Load(app); created {
			t.Fatal("invalid input touched account manager")
		}
	}
	check("providers.auth.list", request(map[string]any{"issuer": "https://attacker.invalid"}), "INVALID_REQUEST")
	check("providers.auth.begin", request(map[string]any{"registrationId": "../../credential"}), "INVALID_REQUEST")
	check("providers.auth.begin", request(map[string]any{"url": "https://attacker.invalid"}), "INVALID_REQUEST")
	check("providers.auth.token", request(nil), "INVALID_REQUEST")
	check("providers.auth.list", request(map[string]any{"workspaceId": workspace.NewID()}), "WORKSPACE_LOCKED")
	owner := app.execution
	app.execution = k.Grant(contracts.ServiceActor{Kind: "external_client", ActorId: contracts.DomainId(workspace.NewID())}, "external", owner.Scope)
	check("providers.auth.list", request(nil), "SCOPE_DENIED")
	app.execution = k.Grant(contracts.ServiceActor{Kind: "local_ai", ActorId: contracts.DomainId(workspace.NewID())}, "ai", owner.Scope)
	check("providers.auth.begin", request(nil), "SCOPE_DENIED")
	app.execution = owner
	k.RevokeGrant(string(owner.ScopeGrantId))
	check("providers.auth.list", request(nil), "SCOPE_DENIED")
}

func TestProviderAuthStorageReasonOnlyExposesTypedPublicMessages(t *testing.T) {
	safe := &auth.Error{Code: "CREDENTIAL_STORE_UNAVAILABLE", Message: "An encrypted desktop keyring is required"}
	if got := providerAuthStorageReason(fmt.Errorf("wrapped: %w", safe)); got != safe.Message {
		t.Fatal(got)
	}
	if got := providerAuthStorageReason(fmt.Errorf("raw OS error with synthetic-secret-value")); strings.Contains(got, "synthetic-secret-value") || got != "Protected account storage is unavailable on this host" {
		t.Fatal(got)
	}
}
