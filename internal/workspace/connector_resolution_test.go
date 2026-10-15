package workspace

import (
	"context"
	"testing"
	"trove.local/phase0/internal/connectors"
)

func TestConnectorManualResolutionPreservesUnknownHistoryAndNeverReplays(t *testing.T) {
	k, e := testKernel(t)
	ctx := context.Background()
	a, adapter := connectorFixture(t, k, e)
	p, err := k.PrepareConnectorProposal(ctx, e, a.ID, fixtureConnectorAction(), adapter, connectors.Session{})
	if err != nil {
		t.Fatal(err)
	}
	adapter.send = func(connectors.Proposal) (connectors.Receipt, error) {
		return connectors.Receipt{}, connectors.Unknown("Remote response was lost")
	}
	if _, err = k.ExecuteConnectorProposal(ctx, e, p.ID, p.Hash, adapter, connectors.Session{}); err == nil {
		t.Fatal("fixture did not become unknown")
	}
	evidence := "Inspected the remote issue list and found issue 42 with the reviewed title and operation marker."
	resolved, err := k.ResolveConnectorProposal(ctx, e, p.ID, p.Hash, a.Revision, "applied", evidence, adapter, connectors.Session{})
	if err != nil || resolved.State != "resolved" || resolved.Receipt.Outcome != "unknown" || resolved.Receipt.UserResolution.Observation != "applied" {
		t.Fatal(resolved, err)
	}
	if _, err = k.ResolveConnectorProposal(ctx, e, p.ID, p.Hash, a.Revision, "applied", evidence, adapter, connectors.Session{}); err != nil {
		t.Fatal("identical retry failed", err)
	}
	var n int
	k.db.QueryRow("SELECT COUNT(*) FROM connector_receipts WHERE proposal_id=?", p.ID).Scan(&n)
	if n != 2 {
		t.Fatal("history replaced or retry duplicated", n)
	}
	if _, err = k.ResolveConnectorProposal(ctx, e, p.ID, p.Hash, a.Revision, "not_applied", evidence, adapter, connectors.Session{}); err == nil {
		t.Fatal("resolution rewritten")
	}
	if _, err = k.ExecuteConnectorProposal(ctx, e, p.ID, p.Hash, adapter, connectors.Session{}); err == nil || adapter.sends != 1 {
		t.Fatal("resolved proposal replayed", err, adapter.sends)
	}
	if _, err = k.PrepareConnectorProposal(ctx, e, a.ID, fixtureConnectorAction(), adapter, connectors.Session{}); err != nil {
		t.Fatal("new reviewed work remains blocked", err)
	}
}

func TestConnectorManualResolutionRequiresOwnerIdentitySelectionAndCurrentReview(t *testing.T) {
	for _, invalid := range []string{"owner", "identity", "selection", "account_revision", "hash", "evidence", "observation", "unsent"} {
		t.Run(invalid, func(t *testing.T) {
			k, e := testKernel(t)
			ctx := context.Background()
			a, adapter := connectorFixture(t, k, e)
			p, err := k.PrepareConnectorProposal(ctx, e, a.ID, fixtureConnectorAction(), adapter, connectors.Session{})
			if err != nil {
				t.Fatal(err)
			}
			adapter.send = func(connectors.Proposal) (connectors.Receipt, error) {
				return connectors.Receipt{}, connectors.Unknown("Remote response was lost")
			}
			if invalid != "unsent" {
				_, _ = k.ExecuteConnectorProposal(ctx, e, p.ID, p.Hash, adapter, connectors.Session{})
			}
			hash, rev, observation, evidence := p.Hash, a.Revision, "not_applied", "Inspected the remote operation log; no matching request was applied."
			switch invalid {
			case "owner":
				e.Actor.Kind = "external_client"
			case "identity":
				adapter.identity.Subject = "other"
			case "selection":
				a, err = k.SetConnectorSelections(ctx, e, a.ID, a.Revision, []connectors.Selection{})
				if err != nil {
					t.Fatal(err)
				}
				rev = a.Revision
			case "account_revision":
				rev = "stale"
			case "hash":
				hash = "stale"
			case "evidence":
				evidence = "  "
			case "observation":
				observation = "guess"
			}
			if _, err = k.ResolveConnectorProposal(ctx, e, p.ID, hash, rev, observation, evidence, adapter, connectors.Session{}); err == nil {
				t.Fatal("invalid manual resolution admitted")
			}
			stored, err := readConnectorProposal(ctx, k.db, p.ID)
			if err != nil || stored.State == "resolved" {
				t.Fatal(stored, err)
			}
		})
	}
}

func TestConnectorPreviouslyPreparedWriteCannotBypassUnknownAccountFence(t *testing.T) {
	k, e := testKernel(t)
	ctx := context.Background()
	a, adapter := connectorFixture(t, k, e)
	first, err := k.PrepareConnectorProposal(ctx, e, a.ID, fixtureConnectorAction(), adapter, connectors.Session{})
	if err != nil {
		t.Fatal(err)
	}
	second, err := k.PrepareConnectorProposal(ctx, e, a.ID, fixtureConnectorAction(), adapter, connectors.Session{})
	if err != nil {
		t.Fatal(err)
	}
	adapter.send = func(connectors.Proposal) (connectors.Receipt, error) {
		return connectors.Receipt{}, connectors.Unknown("Remote response was lost")
	}
	_, _ = k.ExecuteConnectorProposal(ctx, e, first.ID, first.Hash, adapter, connectors.Session{})
	if _, err = k.ExecuteConnectorProposal(ctx, e, second.ID, second.Hash, adapter, connectors.Session{}); err == nil || adapter.sends != 1 {
		t.Fatal("pre-existing reviewed proposal bypassed uncertain outcome fence", err, adapter.sends)
	}
}
