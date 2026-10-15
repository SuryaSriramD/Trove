package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"sync/atomic"
	"testing"
	"time"
	"trove.local/phase0/contracts"
	"trove.local/phase0/internal/connectors"
	"trove.local/phase0/internal/providers"
	"trove.local/phase0/internal/workspace"
)

func TestProviderConnectedScopeHashRetainsLegacyAndBindsResume(t *testing.T) {
	pages, tools := []string{"page"}, []string{"readPage"}
	old, _ := json.Marshal([]any{"workspace", pages, tools, providers.ContainmentPolicy})
	sum := sha256.Sum256(old)
	if providerScopeHash("workspace", pages, tools, nil, nil) != hex.EncodeToString(sum[:]) {
		t.Fatal("ordinary historical resume hash changed")
	}
	recipient := &workspace.ConnectedSharingTarget{Provider: "codex", Account: "recipient"}
	source := workspace.ConnectedEvidence{Version: 1, SnapshotID: "selected", AccountID: "source-account", AccountRevision: "1", SelectionID: "root", Provider: connectors.GitHub, RemoteID: "issue:1", RemoteRevision: "v1", ContentHash: strings.Repeat("a", 64), ReceiptID: "first", CheckedAt: "earlier"}
	original := providerScopeHash("workspace", nil, []string{"readConnectedSource"}, []workspace.ConnectedEvidence{source}, recipient)
	rechecked := source
	rechecked.ReceiptID = "new-native-check"
	rechecked.CheckedAt = "later"
	if providerScopeHash("workspace", nil, []string{"readConnectedSource"}, []workspace.ConnectedEvidence{rechecked}, recipient) != original {
		t.Fatal("fresh unchanged review cannot resume")
	}
	for _, mutate := range []func(*workspace.ConnectedEvidence){func(s *workspace.ConnectedEvidence) { s.RemoteRevision = "v2" }, func(s *workspace.ConnectedEvidence) { s.ContentHash = strings.Repeat("b", 64) }, func(s *workspace.ConnectedEvidence) { s.AccountRevision = "2" }, func(s *workspace.ConnectedEvidence) { s.SelectionID = "different" }} {
		changed := source
		mutate(&changed)
		if providerScopeHash("workspace", nil, []string{"readConnectedSource"}, []workspace.ConnectedEvidence{changed}, recipient) == original {
			t.Fatal("changed source can resume")
		}
	}
	other := &workspace.ConnectedSharingTarget{Provider: "codex", Account: "other"}
	if providerScopeHash("workspace", nil, []string{"readConnectedSource"}, []workspace.ConnectedEvidence{source}, other) == original {
		t.Fatal("recipient switch can resume")
	}
}
func TestProviderConnectedNativePreviewRejectsWorkspaceAndActorBeforeAuth(t *testing.T) {
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
	request := map[string]any{"contractVersion": 1, "requestId": workspace.NewID(), "workspaceId": workspace.NewID(), "provider": "codex", "registrationId": workspace.NewID(), "billingMode": "chatgpt_plan", "sources": []any{map[string]any{"snapshotId": workspace.NewID(), "remoteRevision": "v1", "contentHash": strings.Repeat("a", 64)}}}
	check := func(code string) {
		t.Helper()
		raw, _ := json.Marshal(request)
		out := a.providerDispatch("providers.connectedPreview", raw)
		if !strings.Contains(out, code) {
			t.Fatal(out)
		}
		if _, ok := providerAuthStates.Load(a); ok {
			t.Fatal("untrusted preview opened protected auth")
		}
	}
	check("WORKSPACE_LOCKED")
	request["workspaceId"] = k.ID()
	a.execution = k.Grant(contracts.ServiceActor{Kind: "external_client", ActorId: contracts.DomainId(workspace.NewID())}, "external", a.execution.Scope)
	check("SCOPE_DENIED")
}
func TestProviderConnectedNativeStartCannotInferReviewOrTool(t *testing.T) {
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
	for _, which := range []string{"empty_without_review", "tool_without_review", "review_without_tool"} {
		tools := []string{"readPage"}
		if which == "tool_without_review" {
			tools = []string{"readConnectedSource"}
		}
		request := map[string]any{"contractVersion": 1, "requestId": workspace.NewID(), "workspaceId": k.ID(), "provider": "codex", "registrationId": workspace.NewID(), "billingMode": "chatgpt_plan", "model": "selected-model", "prompt": "Use the selected source", "pageIds": []string{}, "tools": tools, "previewHash": strings.Repeat("a", 64)}
		if which == "review_without_tool" {
			request["connectedSharing"] = map[string]any{"id": workspace.NewID(), "hash": strings.Repeat("b", 64)}
		}
		raw, _ := json.Marshal(request)
		out := a.providerDispatch("providers.start", raw)
		if !strings.Contains(out, "APPROVAL_REQUIRED") {
			t.Fatal(which, out)
		}
		if _, ok := providerAuthStates.Load(a); ok {
			t.Fatal("unreviewed start opened protected auth")
		}
	}
}

func TestProviderConnectedCancellationDuringFinalAccessWithholdsOutput(t *testing.T) {
	for _, scenario := range []string{"cancelled", "remote_changed", "unchanged", "cancelled_before_launch"} {
		t.Run(scenario, func(t *testing.T) {
			ctx := context.Background()
			k, err := workspace.Create(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			defer k.Close()
			e := k.OwnerContext()
			account, err := k.ConnectConnector(ctx, e, connectors.Account{Kind: connectors.GitHub, Identity: connectors.Identity{Provider: "github", Subject: "test-actor", Label: "Fixture"}, RegistrationID: workspace.NewID()}, "")
			if err != nil {
				t.Fatal(err)
			}
			account, err = k.SetConnectorSelections(ctx, e, account.ID, account.Revision, []connectors.Selection{{ID: "org/repo", Kind: "repository", Label: "Selected"}})
			if err != nil {
				t.Fatal(err)
			}
			source, err := k.StoreConnectorPreview(ctx, e, account, "org/repo", connectors.Snapshot{RemoteID: "issue:1", RemoteRevision: "v1", Title: "Decision", Text: "Friday launch", State: "present", Content: json.RawMessage(`{}`)})
			if err != nil {
				t.Fatal(err)
			}
			var final atomic.Bool
			entered := make(chan struct{})
			release := make(chan struct{})
			k.SetConnectorContextReader(func(readCtx context.Context, _ connectors.Account, _ connectors.Selection, _ string) (connectors.Snapshot, error) {
				fresh := source
				if final.Load() {
					close(entered)
					select {
					case <-readCtx.Done():
						return fresh, readCtx.Err()
					case <-release:
					}
					if scenario == "remote_changed" {
						fresh.RemoteRevision = "v2"
					}
				}
				return fresh, nil
			})
			b := workspace.NewProviderBridge(k, e)
			defer b.Close()
			p, err := b.PreviewConnectedContext(ctx, workspace.ConnectedSharingTarget{Provider: "codex", Account: "registration"}, []workspace.ConnectedSourceReference{workspace.ConnectorContextReference(source)})
			if err != nil {
				t.Fatal(err)
			}
			c, err := b.ConnectManagedReviewed(ctx, "codex", "registration", "siwc", "chatgpt_plan", nil, []string{"readConnectedSource"}, workspace.ConnectedSharingApproval{ID: p.ID, Hash: p.Hash})
			if err != nil {
				t.Fatal(err)
			}
			if c.PageIDs == nil {
				t.Fatal("empty source-only canonical scope serialized as null")
			}
			r := workspace.ManagedRun{ID: workspace.NewID(), SessionID: workspace.NewID(), ConnectionID: c.ID, Provider: "codex", Account: "registration", ConnectedRecipient: &workspace.ConnectedSharingTarget{Provider: "codex", Account: "registration"}, ConnectedSources: c.ConnectedSources, State: "starting", Manifest: json.RawMessage(`{"pages":[]}`)}
			if err = b.SaveManagedRun(ctx, c.ID, c.Token, r); err != nil {
				t.Fatal(err)
			}
			if scenario == "cancelled_before_launch" {
				cancelled, cancel := context.WithCancel(ctx)
				cancel()
				r.State = "cancelled"
				r.Message = "Provider preparation stopped before native execution began."
				out, err := persistManagedProviderResult(cancelled, k, e, b, c, r)
				if err != nil || out.State != "cancelled" || out.Text != "" || out.Checkpoint != nil {
					t.Fatal(out, err)
				}
				rows, err := k.ManagedRuns(ctx, e)
				if err != nil || len(rows) != 1 || rows[0].State != "cancelled" || rows[0].Text != "" {
					t.Fatal("abandoned setup remained starting", rows, err)
				}
				return
			}
			r.State = "completed"
			r.Text = "Model output already returned"
			r.Checkpoint = &providers.ManagedCheckpoint{ThreadID: "private-checkpoint"}
			final.Store(true)
			runCtx, cancel := context.WithCancel(ctx)
			defer cancel()
			type result struct {
				run workspace.ManagedRun
				err error
			}
			done := make(chan result, 1)
			go func() { out, err := persistManagedProviderResult(runCtx, k, e, b, c, r); done <- result{out, err} }()
			select {
			case <-entered:
			case <-time.After(3 * time.Second):
				close(release)
				t.Fatal("final live source recheck never began")
			}
			if scenario == "cancelled" {
				cancel()
			} else {
				close(release)
			}
			var out result
			select {
			case out = <-done:
			case <-time.After(3 * time.Second):
				t.Fatal("final publication did not stop")
			}
			if out.err != nil {
				t.Fatal("sanitized final state failed persistence", out.err)
			}
			runs, err := k.ManagedRuns(ctx, e)
			if err != nil || len(runs) != 1 {
				t.Fatal(runs, err)
			}
			if scenario == "unchanged" {
				if runs[0].State != "completed" || runs[0].Text != r.Text || runs[0].Checkpoint == nil {
					t.Fatal(runs)
				}
			} else {
				state := "failed"
				if scenario == "cancelled" {
					state = "cancelled"
				}
				if out.run.State != state || out.run.Text != "" || out.run.Checkpoint != nil || runs[0].State != state || runs[0].Text != "" || runs[0].Checkpoint != nil {
					t.Fatal("late output or checkpoint published", out.run, runs)
				}
			}
		})
	}
}

func TestProviderConnectedApprovalReentersNativeHostWithoutAppMutex(t *testing.T) {
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
	guard := func(rev string) contracts.ServiceExpectedRevision {
		return contracts.ServiceExpectedRevision{Target: contracts.ServiceEntityRef{EntityType: "page", EntityId: contracts.DomainId(page)}, Revision: rev}
	}
	request := map[string]any{"contractVersion": 1, "requestId": workspace.NewID(), "workspaceId": k.ID(), "mutation": map[string]any{"scopeGrantId": a.execution.ScopeGrantId, "idempotencyKey": workspace.NewID(), "expectedRevisions": []contracts.ServiceExpectedRevision{guard("0")}}, "commands": []any{map[string]any{"kind": "page.create", "pageId": page, "title": "Before"}}}
	raw, _ := json.Marshal(request)
	if response := k.Dispatch(ctx, a.execution, "batch", raw); !strings.Contains(string(response), `"ok":true`) {
		t.Fatal(string(response))
	}
	account, err := k.ConnectConnector(ctx, a.execution, connectors.Account{Kind: connectors.GitHub, Identity: connectors.Identity{Provider: "github", Subject: "100", Label: "Fixture"}, RegistrationID: workspace.NewID()}, "")
	if err != nil {
		t.Fatal(err)
	}
	account, err = k.SetConnectorSelections(ctx, a.execution, account.ID, account.Revision, []connectors.Selection{{ID: "org/repo", Kind: "repository", Label: "Selected"}})
	if err != nil {
		t.Fatal(err)
	}
	source, err := k.StoreConnectorPreview(ctx, a.execution, account, "org/repo", connectors.Snapshot{RemoteID: "issue:1", RemoteRevision: "v1", Title: "Decision", Text: "Friday", State: "present", Content: json.RawMessage(`{}`)})
	if err != nil {
		t.Fatal(err)
	}
	var check atomic.Bool
	var checked atomic.Int64
	k.SetConnectorContextReader(func(context.Context, connectors.Account, connectors.Selection, string) (connectors.Snapshot, error) {
		if check.Load() {
			checked.Add(1)
			if !a.mu.TryLock() {
				return connectors.Snapshot{}, workspace.Fail("CONFLICT", "Native callback ran under app mutex")
			}
			a.mu.Unlock()
		}
		return source, nil
	})
	b := workspace.NewProviderBridge(k, a.execution)
	providerHosts.Store(a, &providerHost{bridge: b})
	defer a.stopProviders()
	preview, err := b.PreviewConnectedContext(ctx, workspace.ConnectedSharingTarget{Provider: "codex", Account: "registration"}, []workspace.ConnectedSourceReference{workspace.ConnectorContextReference(source)})
	if err != nil {
		t.Fatal(err)
	}
	c, err := b.ConnectManagedReviewed(ctx, "codex", "registration", "siwc", "chatgpt_plan", []string{page}, []string{"readConnectedSource", "proposeChanges"}, workspace.ConnectedSharingApproval{ID: preview.ID, Hash: preview.Hash})
	if err != nil {
		t.Fatal(err)
	}
	raw, err = b.Call(ctx, c.ID, c.Token, "proposeChanges", map[string]any{"idempotencyKey": "approved", "commands": []any{map[string]any{"kind": "page.update", "pageId": page, "title": "Reviewed result"}}, "expectedRevisions": []contracts.ServiceExpectedRevision{guard("1")}})
	if err != nil {
		t.Fatal(err)
	}
	var p struct {
		Result workspace.ProviderProposal `json:"result"`
	}
	if err = json.Unmarshal(raw, &p); err != nil {
		t.Fatal(err)
	}
	check.Store(true)
	raw, _ = json.Marshal(map[string]any{"contractVersion": 1, "requestId": workspace.NewID(), "workspaceId": k.ID(), "proposalId": p.Result.ID})
	result := a.Dispatch("providers.approve", string(raw))
	if !strings.Contains(result, `"ok":true`) || checked.Load() == 0 {
		t.Fatal("connected approval cannot safely reenter native host", result, checked.Load())
	}
}
