package main

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
	"trove.local/phase0/contracts"
	"trove.local/phase0/internal/workspace"
)

func TestProviderPreparationCancelBeforeAdmissionIsOwnerBound(t *testing.T) {
	h := &providerHost{}
	if err := h.cancelPreparation("owner-one", "request"); err != nil {
		t.Fatal(err)
	}
	other, f, err := h.beginPreparation(context.Background(), "owner-two", "request")
	if err != nil || other.Err() != nil {
		t.Fatal("other owner blocked", err)
	}
	h.finishPreparation(f)
	if _, _, err = h.beginPreparation(context.Background(), "owner-one", "request"); !errors.Is(err, context.Canceled) {
		t.Fatal("early cancellation lost", err)
	}
}
func TestProviderPreparationCancellationAndShutdownBoundaries(t *testing.T) {
	h := &providerHost{}
	ctx := context.Background()
	flow, f, err := h.beginPreparation(ctx, "owner", "preview")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err = h.beginPreparation(ctx, "owner", "preview"); err == nil {
		t.Fatal("duplicate request admitted")
	}
	if err = h.cancelPreparation("owner", "preview"); err != nil || flow.Err() == nil {
		t.Fatal("preview not cancelled", err)
	}
	h.finishPreparation(f)
	h.finishPreparation(f)
	for i := 0; i < 128; i++ {
		if err = h.cancelPreparation("owner", workspace.NewID()); err != nil {
			t.Fatal(err)
		}
	}
	if h.cancelPreparation("owner", workspace.NewID()) == nil {
		t.Fatal("early cancellation map unbounded")
	}
	h.mu.Lock()
	for key := range h.preparationCancelled {
		h.preparationCancelled[key] = time.Now().Add(-time.Second)
	}
	h.mu.Unlock()
	if err = h.cancelPreparation("owner", "fresh"); err != nil {
		t.Fatal("expired tombstones retained", err)
	}
	active, f, err := h.beginPreparation(ctx, "owner", "start")
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() { h.stopPreparations(); close(done) }()
	select {
	case <-active.Done():
	case <-time.After(time.Second):
		t.Fatal("shutdown did not cancel preparation")
	}
	h.finishPreparation(f)
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("shutdown did not drain preparation")
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.preparations) != 0 || len(h.preparationCancelled) != 0 {
		t.Fatal("host shutdown retained preparation state")
	}
}
func TestProviderPreparationAdmissionBoundAndGenerationAlias(t *testing.T) {
	h := &providerHost{}
	flights := []*providerPreparation{}
	for i := 0; i < 4; i++ {
		_, f, err := h.beginPreparation(context.Background(), "owner", workspace.NewID())
		if err != nil {
			t.Fatal(err)
		}
		flights = append(flights, f)
	}
	if _, _, err := h.beginPreparation(context.Background(), "owner", "fifth"); err == nil {
		t.Fatal("unbounded preparations")
	}
	for _, f := range flights {
		h.finishPreparation(f)
	}
	flow, f, err := h.beginPreparation(context.Background(), "owner", "starting-request")
	if err != nil {
		t.Fatal(err)
	}
	generation, cancel := context.WithCancel(flow)
	defer cancel()
	// Preparation completion must not close a flight handed to generation. The
	// original request identity continues to cancel the exact child context.
	if err = h.cancelPreparation("owner", "starting-request"); err != nil || generation.Err() == nil {
		t.Fatal("request alias cannot cancel generation", err)
	}
	h.finishPreparation(f)
}
func TestProviderNativeEarlyCancelAvoidsAccountAndSourceAccess(t *testing.T) {
	k, err := workspace.Create(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer k.Close()
	a := NewApp()
	a.kernel = k
	a.execution = k.OwnerContext()
	a.ctx = context.Background()
	defer a.stopProviders()
	for _, method := range []string{"providers.connectedPreview", "providers.start"} {
		requestID := workspace.NewID()
		cancelRequest, _ := json.Marshal(map[string]any{"contractVersion": 1, "requestId": workspace.NewID(), "workspaceId": k.ID(), "runId": requestID})
		if out := a.providerDispatch("providers.cancel", cancelRequest); !strings.Contains(out, "cancellation_requested") {
			t.Fatal(out)
		}
		request := map[string]any{"contractVersion": 1, "requestId": requestID, "workspaceId": k.ID(), "provider": "codex", "registrationId": workspace.NewID(), "billingMode": "chatgpt_plan"}
		if method == "providers.connectedPreview" {
			request["sources"] = []any{map[string]any{"snapshotId": workspace.NewID(), "remoteRevision": "v1", "contentHash": strings.Repeat("a", 64)}}
		} else {
			request["model"] = "model"
			request["prompt"] = "Read"
			request["pageIds"] = []string{workspace.NewID()}
			request["tools"] = []string{"readPage"}
			request["previewHash"] = strings.Repeat("a", 64)
		}
		raw, _ := json.Marshal(request)
		if out := a.providerDispatch(method, raw); !strings.Contains(out, "CANCELLED") {
			t.Fatal("cancelled request entered preflight", method, out)
		}
		if _, ok := providerAuthStates.Load(a); ok {
			t.Fatal("cancelled preparation opened credential storage")
		}
	}
}
func TestProviderNativeCancelRequiresAuthenticatedOwnerAndWorkspace(t *testing.T) {
	k, err := workspace.Create(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer k.Close()
	a := NewApp()
	a.kernel = k
	a.execution = k.OwnerContext()
	a.ctx = context.Background()
	defer a.stopProviders()
	request := map[string]any{"contractVersion": 1, "requestId": workspace.NewID(), "workspaceId": workspace.NewID(), "runId": workspace.NewID()}
	raw, _ := json.Marshal(request)
	if out := a.providerDispatch("providers.cancel", raw); !strings.Contains(out, "WORKSPACE_LOCKED") {
		t.Fatal(out)
	}
	request["workspaceId"] = k.ID()
	a.execution = k.Grant(contracts.ServiceActor{Kind: "external_client", ActorId: contracts.DomainId(workspace.NewID())}, "external", a.execution.Scope)
	raw, _ = json.Marshal(request)
	if out := a.providerDispatch("providers.cancel", raw); !strings.Contains(out, "SCOPE_DENIED") {
		t.Fatal(out)
	}
	if _, ok := providerHosts.Load(a); ok {
		t.Fatal("untrusted cancellation created a provider host/tombstone")
	}
}
