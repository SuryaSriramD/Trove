package workspace

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"

	"trove.local/phase0/contracts"
	"trove.local/phase0/internal/connectors"
)

type connectorFixtureAdapter struct {
	identity  connectors.Identity
	refresh   func(context.Context, connectors.Selection, string) (connectors.RefreshPage, error)
	check     func() error
	send      func(connectors.Proposal) (connectors.Receipt, error)
	reconcile func(connectors.Proposal) (connectors.Receipt, error)
	sends     int
}

func (a *connectorFixtureAdapter) Identity(context.Context, connectors.Session) (connectors.Identity, error) {
	return a.identity, nil
}
func (a *connectorFixtureAdapter) Discover(context.Context, connectors.Session, string) ([]connectors.Selection, string, error) {
	return nil, "", nil
}
func (a *connectorFixtureAdapter) Refresh(ctx context.Context, _ connectors.Session, s connectors.Selection, c string) (connectors.RefreshPage, error) {
	return a.refresh(ctx, s, c)
}
func (a *connectorFixtureAdapter) Prepare(_ context.Context, _ connectors.Session, v connectors.Action) (connectors.Action, error) {
	v.Version = 1
	v.Effects = []string{"Create an issue in the selected repository"}
	return v, nil
}
func (a *connectorFixtureAdapter) Check(context.Context, connectors.Session, connectors.Action) error {
	if a.check != nil {
		return a.check()
	}
	return nil
}
func (a *connectorFixtureAdapter) Send(_ context.Context, _ connectors.Session, p connectors.Proposal) (connectors.Receipt, error) {
	a.sends++
	if a.send != nil {
		return a.send(p)
	}
	return connectors.ReceiptFor(p, "succeeded", "42", "v2", "https://github.com/example/repo/issues/42", "Created issue"), nil
}
func (a *connectorFixtureAdapter) Reconcile(_ context.Context, _ connectors.Session, p connectors.Proposal) (connectors.Receipt, error) {
	return a.reconcile(p)
}
func connectorFixture(t *testing.T, k *Kernel, e contracts.ServiceExecutionContext) (connectors.Account, *connectorFixtureAdapter) {
	t.Helper()
	identity := connectors.Identity{Subject: "100", Provider: "github", Label: "Example"}
	a, err := k.ConnectConnector(context.Background(), e, connectors.Account{Kind: connectors.GitHub, Identity: identity, RegistrationID: NewID()}, "")
	if err != nil {
		t.Fatal(err)
	}
	a, err = k.SetConnectorSelections(context.Background(), e, a.ID, a.Revision, []connectors.Selection{{ID: "org/repo", Kind: "repository", Label: "Selected repo"}})
	if err != nil {
		t.Fatal(err)
	}
	return a, &connectorFixtureAdapter{identity: identity}
}
func fixtureConnectorAction() connectors.Action {
	return connectors.Action{Version: 1, Kind: "issue.create", SelectionID: "org/repo", Arguments: json.RawMessage(`{"title":"Reviewed issue","body":"Exact body"}`)}
}
func TestConnectorsRefreshInterruptedCursorSelectionAndRemoval(t *testing.T) {
	k, e := testKernel(t)
	ctx := context.Background()
	a, adapter := connectorFixture(t, k, e)
	j, err := k.PrepareConnectorRefresh(ctx, e, a.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	adapter.refresh = func(_ context.Context, s connectors.Selection, c string) (connectors.RefreshPage, error) {
		if s.ID != "org/repo" {
			t.Fatal("selection leaked")
		}
		if c == "" {
			return connectors.RefreshPage{Items: []connectors.Snapshot{{RemoteID: "1", Title: "Kept", Text: "Fact", Content: json.RawMessage(`{}`)}}, NextCursor: "page2"}, nil
		}
		return connectors.RefreshPage{}, errors.New("interrupted page")
	}
	if err = k.RunConnectorRefresh(ctx, e, j, adapter, connectors.Session{}); err == nil {
		t.Fatal("expected interrupted page")
	}
	var raw string
	if err = k.db.QueryRow("SELECT body_json FROM connector_refreshes WHERE id=?", j.ID).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	_ = json.Unmarshal([]byte(raw), &j)
	if j.State != "interrupted" || j.Cursor != "page2" || j.Items != 1 {
		t.Fatal(j)
	}
	j, err = k.PrepareConnectorRefresh(ctx, e, a.ID, j.ID)
	if err != nil {
		t.Fatal(err)
	}
	adapter.refresh = func(_ context.Context, _ connectors.Selection, c string) (connectors.RefreshPage, error) {
		if c != "page2" {
			t.Fatal("lost durable cursor", c)
		}
		return connectors.RefreshPage{Items: []connectors.Snapshot{{RemoteID: "2", Title: "Second", Content: json.RawMessage(`{}`)}}, Complete: true, FullEnumeration: true}, nil
	}
	if err = k.RunConnectorRefresh(ctx, e, j, adapter, connectors.Session{}); err != nil {
		t.Fatal(err)
	}
	result, err := connectorSearch(ctx, k, e, map[string]any{"query": ""})
	if err != nil || len(result.([]connectors.Snapshot)) != 2 {
		t.Fatal(result, err)
	}
	// A later complete enumeration removes only external evidence. Incomplete
	// pagination above did not discard the first page's retained evidence.
	j, err = k.PrepareConnectorRefresh(ctx, e, a.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	adapter.refresh = func(context.Context, connectors.Selection, string) (connectors.RefreshPage, error) {
		return connectors.RefreshPage{Items: []connectors.Snapshot{{RemoteID: "2", Title: "Still second", Content: json.RawMessage(`{}`)}}, Complete: true, FullEnumeration: true}, nil
	}
	if err = k.RunConnectorRefresh(ctx, e, j, adapter, connectors.Session{}); err != nil {
		t.Fatal(err)
	}
	result, _ = connectorSearch(ctx, k, e, map[string]any{"query": ""})
	if len(result.([]connectors.Snapshot)) != 1 {
		t.Fatal("complete enumeration did not mark absence", result)
	}
	j, err = k.PrepareConnectorRefresh(ctx, e, a.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	a, err = k.SetConnectorSelections(ctx, e, a.ID, a.Revision, []connectors.Selection{})
	if err != nil {
		t.Fatal(err)
	}
	if err = k.RunConnectorRefresh(ctx, e, j, adapter, connectors.Session{}); err == nil {
		t.Fatal("stale selected resources refreshed")
	}
	result, _ = connectorSearch(ctx, k, e, map[string]any{"query": ""})
	if len(result.([]connectors.Snapshot)) != 0 {
		t.Fatal("deselected evidence visible")
	}
}
func TestConnectorsReviewedOutboxUnknownReconciliationAndIdempotency(t *testing.T) {
	k, e := testKernel(t)
	ctx := context.Background()
	a, adapter := connectorFixture(t, k, e)
	p, err := k.PrepareConnectorProposal(ctx, e, a.ID, fixtureConnectorAction(), adapter, connectors.Session{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = k.ExecuteConnectorProposal(ctx, e, p.ID, "wrong", adapter, connectors.Session{}); err == nil || adapter.sends != 0 {
		t.Fatal("unreviewed hash executed")
	}
	adapter.send = func(p connectors.Proposal) (connectors.Receipt, error) {
		return connectors.Receipt{}, errors.New("connection lost after commit")
	}
	receipt, err := k.ExecuteConnectorProposal(ctx, e, p.ID, p.Hash, adapter, connectors.Session{})
	if err == nil || receipt.Outcome != "unknown" || adapter.sends != 1 {
		t.Fatal(receipt, err, adapter.sends)
	}
	if _, err = k.ExecuteConnectorProposal(ctx, e, p.ID, p.Hash, adapter, connectors.Session{}); err == nil || adapter.sends != 1 {
		t.Fatal("uncertain action replayed")
	}
	if _, err = k.PrepareConnectorProposal(ctx, e, a.ID, fixtureConnectorAction(), adapter, connectors.Session{}); err == nil {
		t.Fatal("duplicate unknown write prepared")
	}
	adapter.reconcile = func(p connectors.Proposal) (connectors.Receipt, error) {
		return connectors.ReceiptFor(p, "succeeded", "42", "v2", "https://github.com/org/repo/issues/42", "Verified exact remote effect"), nil
	}
	receipt, err = k.ReconcileConnectorProposal(ctx, e, p.ID, adapter, connectors.Session{})
	if err != nil || receipt.Outcome != "succeeded" || adapter.sends != 1 {
		t.Fatal(receipt, err)
	}
	receipt, err = k.ExecuteConnectorProposal(ctx, e, p.ID, p.Hash, adapter, connectors.Session{})
	if err != nil || receipt.RemoteID != "42" || adapter.sends != 1 {
		t.Fatal("idempotent receipt missing", receipt, err)
	}
}
func TestConnectorsRecheckAccountRemoteRevisionAndSelection(t *testing.T) {
	k, e := testKernel(t)
	ctx := context.Background()
	a, adapter := connectorFixture(t, k, e)
	p, err := k.PrepareConnectorProposal(ctx, e, a.ID, fixtureConnectorAction(), adapter, connectors.Session{})
	if err != nil {
		t.Fatal(err)
	}
	adapter.identity.Subject = "different"
	if _, err = k.ExecuteConnectorProposal(ctx, e, p.ID, p.Hash, adapter, connectors.Session{}); err == nil || adapter.sends != 0 {
		t.Fatal("switched account wrote")
	}
	adapter.identity = a.Identity
	adapter.check = func() error { return &connectors.Failure{Code: "REMOTE_CHANGED", Message: "Changed revision"} }
	if _, err = k.ExecuteConnectorProposal(ctx, e, p.ID, p.Hash, adapter, connectors.Session{}); err == nil || adapter.sends != 0 {
		t.Fatal("stale remote wrote")
	}
	adapter.check = nil
	if _, err = k.SetConnectorSelections(ctx, e, a.ID, a.Revision, []connectors.Selection{}); err != nil {
		t.Fatal(err)
	}
	if _, err = k.ExecuteConnectorProposal(ctx, e, p.ID, p.Hash, adapter, connectors.Session{}); err == nil || adapter.sends != 0 {
		t.Fatal("deselected account wrote")
	}
}
func TestConnectorsRestoreIsInertAndUnknownRemainsReconcilable(t *testing.T) {
	k, e := testKernel(t)
	ctx := context.Background()
	a, adapter := connectorFixture(t, k, e)
	pending, err := k.PrepareConnectorProposal(ctx, e, a.ID, fixtureConnectorAction(), adapter, connectors.Session{})
	if err != nil {
		t.Fatal(err)
	}
	unknown, err := k.PrepareConnectorProposal(ctx, e, a.ID, fixtureConnectorAction(), adapter, connectors.Session{})
	if err != nil {
		t.Fatal(err)
	}
	adapter.send = func(connectors.Proposal) (connectors.Receipt, error) {
		return connectors.Receipt{}, connectors.Unknown("No response")
	}
	_, _ = k.ExecuteConnectorProposal(ctx, e, unknown.ID, unknown.Hash, adapter, connectors.Session{})
	backup := filepath.Join(t.TempDir(), "backup")
	if _, err = k.CreateBackup(ctx, e, backup); err != nil {
		t.Fatal(err)
	}
	destination := filepath.Join(t.TempDir(), "restored")
	if _, err = RestoreBackup(ctx, backup, destination); err != nil {
		t.Fatal(err)
	}
	restored, err := Open(destination)
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()
	owner := restored.OwnerContext()
	accounts, err := restored.ConnectorAccounts(ctx, owner)
	if err != nil || len(accounts) != 1 || accounts[0].State != "restored_inactive" || accounts[0].RegistrationID != "" {
		t.Fatal(accounts, err)
	}
	got, err := readConnectorProposal(ctx, restored.db, pending.ID)
	if err != nil || got.State != "restored_inactive" {
		t.Fatal(got, err)
	}
	got, err = readConnectorProposal(ctx, restored.db, unknown.ID)
	if err != nil || got.State != "unknown" {
		t.Fatal(got, err)
	}
	if _, err = restored.ExecuteConnectorProposal(ctx, owner, pending.ID, pending.Hash, adapter, connectors.Session{}); err == nil {
		t.Fatal("restored pending action executed")
	}
	reconnect := accounts[0]
	reconnect.RegistrationID = NewID()
	if _, err = restored.ConnectConnector(ctx, owner, reconnect, reconnect.Revision); err != nil {
		t.Fatal(err)
	}
	adapter.reconcile = func(p connectors.Proposal) (connectors.Receipt, error) {
		return connectors.ReceiptFor(p, "succeeded", "42", "v1", "", "Verified after reconnection"), nil
	}
	receipt, err := restored.ReconcileConnectorProposal(ctx, owner, unknown.ID, adapter, connectors.Session{})
	if err != nil || receipt.Outcome != "succeeded" || adapter.sends != 1 {
		t.Fatal(receipt, err, adapter.sends)
	}
}
