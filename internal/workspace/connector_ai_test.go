package workspace

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"trove.local/phase0/contracts"
	"trove.local/phase0/internal/connectors"
)

func connectedFixture(t *testing.T) (*Kernel, contracts.ServiceExecutionContext, connectors.Account, connectors.Snapshot) {
	t.Helper()
	k, e := testKernel(t)
	a, _ := connectorFixture(t, k, e)
	s, err := k.StoreConnectorPreview(context.Background(), e, a, "org/repo", connectors.Snapshot{RemoteID: "issue:1", RemoteRevision: "v1", Title: "Launch decision", Text: "The launch is Friday.", State: "present", Content: json.RawMessage(`{}`)})
	if err != nil {
		t.Fatal(err)
	}
	return k, e, a, s
}
func TestConnectedContextExactSelectionAndInertReceipts(t *testing.T) {
	k, e, a, s := connectedFixture(t)
	ctx := context.Background()
	calls := 0
	reader := func(_ context.Context, account connectors.Account, selection connectors.Selection, id string) (connectors.Snapshot, error) {
		calls++
		if account.ID != a.ID || selection.ID != "org/repo" || id != s.RemoteID {
			t.Fatal("expanded scope")
		}
		return s, nil
	}
	h := NewConnectorContext(k, reader)
	out, err := h.Read(ctx, e, ConnectorContextReference(s))
	if err != nil {
		t.Fatal(err)
	}
	if out.Text != s.Text || out.Connected.Provider != connectors.GitHub || calls != 1 {
		t.Fatal(out, calls)
	}
	if err = h.Revalidate(ctx, e, out.Connected); err != nil || calls != 2 {
		t.Fatal("missing current access check", calls, err)
	}
	forged := out.Connected
	forged.RemoteID = "issue:2"
	if h.validate(ctx, k.db, e, forged) == nil {
		t.Fatal("forged receipt accepted")
	}
	reopened := NewConnectorContext(k, reader)
	if reopened.validate(ctx, k.db, e, out.Connected) == nil {
		t.Fatal("retained receipt restored activation")
	}
	if _, err = NewConnectorContext(k, nil).Read(ctx, e, ConnectorContextReference(s)); err == nil {
		t.Fatal("missing native host accepted")
	}
}
func TestConnectedContextLiveChangesAndWriterRace(t *testing.T) {
	for _, scenario := range []string{"revision", "same-revision-content", "removed", "access-revoked", "selection-changed", "cancelled"} {
		t.Run(scenario, func(t *testing.T) {
			k, e, a, s := connectedFixture(t)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			h := NewConnectorContext(k, func(context.Context, connectors.Account, connectors.Selection, string) (connectors.Snapshot, error) {
				fresh := s
				switch scenario {
				case "revision":
					fresh.RemoteRevision = "v2"
				case "same-revision-content":
					fresh.Text = "The launch is Monday."
				case "removed":
					fresh.State = "removed"
				case "access-revoked":
					return fresh, errors.New("remote denied")
				case "selection-changed":
					if _, err := k.SetConnectorSelections(ctx, e, a.ID, a.Revision, nil); err != nil {
						t.Fatal(err)
					}
				case "cancelled":
					cancel()
				}
				return fresh, nil
			})
			if _, err := h.Read(ctx, e, ConnectorContextReference(s)); err == nil {
				t.Fatal("accepted changed or unavailable context")
			}
		})
	}
}
func TestConnectedContextRejectsNonOwnerAndStaleReview(t *testing.T) {
	k, e, _, s := connectedFixture(t)
	calls := 0
	h := NewConnectorContext(k, func(context.Context, connectors.Account, connectors.Selection, string) (connectors.Snapshot, error) {
		calls++
		return s, nil
	})
	ref := ConnectorContextReference(s)
	ref.ContentHash = connectorHash("other")
	if _, err := h.Read(context.Background(), e, ref); err == nil || calls != 0 {
		t.Fatal("stale review reached network")
	}
	external := k.Grant(contracts.ServiceActor{Kind: "external_client", ActorId: contracts.DomainId(NewID())}, "external", e.Scope)
	if _, err := h.Read(context.Background(), external, ConnectorContextReference(s)); err == nil || calls != 0 {
		t.Fatal("external client bypassed sharing approval")
	}
}

func TestConnectedEvidenceWireIdentityAndHostReset(t *testing.T) {
	k, e, _, s := connectedFixture(t)
	reader := func(context.Context, connectors.Account, connectors.Selection, string) (connectors.Snapshot, error) {
		return s, nil
	}
	k.SetConnectorContextReader(reader)
	source, err := k.connectedContext().Read(context.Background(), e, ConnectorContextReference(s))
	if err != nil {
		t.Fatal(err)
	}
	if err := contracts.Validate("service", "ConnectedEvidence", marshal(source.Connected)); err != nil {
		t.Fatal(err)
	}
	if err := contracts.Validate("service", "ConnectedSourceReference", marshal(ConnectorContextReference(s))); err != nil {
		t.Fatal(err)
	}
	evidence := AIEvidence{Title: source.Title, Text: source.Text, Connected: &source.Connected}
	var wire map[string]any
	if err = json.Unmarshal(marshal(evidence), &wire); err != nil {
		t.Fatal(err)
	}
	if _, ok := wire["location"]; ok || wire["connected"] == nil {
		t.Fatal("fabricated canonical location", wire)
	}
	var restored AIEvidence
	if err = json.Unmarshal(marshal(evidence), &restored); err != nil || restored.Connected == nil || restored.Connected.ReceiptID != source.Connected.ReceiptID {
		t.Fatal("receipt lost", restored, err)
	}
	k.SetConnectorContextReader(reader)
	if k.connectedContext().validate(context.Background(), k.db, e, source.Connected) == nil {
		t.Fatal("host reset restored authority")
	}
}

func TestConnectedContextReferenceQueryRequiresCurrentOwnerSelection(t *testing.T) {
	k, e, a, s := connectedFixture(t)
	request := map[string]any{"contractVersion": 1, "requestId": NewID(), "workspaceId": k.ID(), "snapshotId": s.ID, "expectedRevision": s.RemoteRevision}
	result := testDispatch(t, k, e, "connectors.contextReference", request)
	testOK(t, result)
	var ref ConnectedSourceReference
	if err := json.Unmarshal(marshal(result["result"]), &ref); err != nil {
		t.Fatal(err)
	}
	if ref != ConnectorContextReference(s) {
		t.Fatal("reference differs from canonical preview hash", result)
	}
	request["expectedRevision"] = "stale"
	testCode(t, testDispatch(t, k, e, "connectors.contextReference", request), "CONFLICT")
	request["expectedRevision"] = s.RemoteRevision
	external := externalPolicyGrant(k, e)
	testCode(t, testDispatch(t, k, external, "connectors.contextReference", request), "SCOPE_DENIED")
	if err := k.DisconnectConnector(context.Background(), e, a.ID, a.Revision); err != nil {
		t.Fatal(err)
	}
	testCode(t, testDispatch(t, k, e, "connectors.contextReference", request), "SCOPE_DENIED")
}

func TestConnectedContextIgnoresRotatingUnsharedSignedURLs(t *testing.T) {
	k, e, _, s := connectedFixture(t)
	ctx := context.Background()
	s.Content = json.RawMessage(`{"cover":{"url":"https://files.example/cover?signature=old"}}`)
	a, err := k.ConnectorAccount(ctx, e, s.AccountID)
	if err != nil {
		t.Fatal(err)
	}
	s, err = k.StoreConnectorPreview(ctx, e, a, s.SelectionID, s)
	if err != nil {
		t.Fatal(err)
	}
	fresh := s
	fresh.Content = json.RawMessage(`{"cover":{"url":"https://files.example/cover?signature=new"}}`)
	h := NewConnectorContext(k, func(context.Context, connectors.Account, connectors.Selection, string) (connectors.Snapshot, error) {
		return fresh, nil
	})
	if _, err = h.Read(ctx, e, ConnectorContextReference(s)); err != nil {
		t.Fatal("rotating unshared metadata rejected unchanged knowledge", err)
	}
	fresh.RemoteRevision = "v2"
	if _, err = h.Read(ctx, e, ConnectorContextReference(s)); err == nil {
		t.Fatal("changed remote revision ignored because text stayed equal")
	}
}
