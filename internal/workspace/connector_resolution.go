package workspace

import (
	"context"
	"database/sql"
	"strings"
	"trove.local/phase0/contracts"
	"trove.local/phase0/internal/connectors"
)

// ResolveConnectorProposal records a direct user's remote inspection. The
// original provider outcome and append-only receipt history remain unknown.
// This proposal can never be sent again; further work requires a new review.
func (k *Kernel) ResolveConnectorProposal(ctx context.Context, e contracts.ServiceExecutionContext, id, hash, accountRevision, observation, evidence string, adapter connectors.Adapter, s connectors.Session) (result connectors.Proposal, err error) {
	if err = connectorOwner(k, e); err != nil {
		return
	}
	evidence = strings.TrimSpace(evidence)
	if (observation != "applied" && observation != "not_applied") || len(evidence) < 10 || len(evidence) > 4000 {
		return result, Fail("INVALID_REQUEST", "Describe the remote result you inspected before recording a manual resolution")
	}
	p, err := readConnectorProposal(ctx, k.db, id)
	if err != nil {
		return result, err
	}
	if p.Hash != hash {
		return result, Fail("CONFLICT", "Reviewed proposal changed")
	}
	a, err := activeConnector(ctx, k, k.db, e, p.AccountID, accountRevision)
	if err != nil {
		return result, err
	}
	if _, err = connectors.Selected(a, p.Action.SelectionID); err != nil {
		return result, err
	}
	s.Account = a
	identity, err := adapter.Identity(ctx, s)
	if err != nil {
		return result, err
	}
	if err = connectors.RequireIdentity(p.Identity, identity); err != nil {
		return result, err
	}
	err = k.write(ctx, func(tx *sql.Tx) error {
		if _, err := activeConnector(ctx, k, tx, e, p.AccountID, a.Revision); err != nil {
			return err
		}
		current, err := readConnectorProposal(ctx, tx, id)
		if err != nil {
			return err
		}
		if current.Hash != hash {
			return Fail("CONFLICT", "Reviewed proposal changed")
		}
		if current.State == "resolved" && current.Receipt != nil && current.Receipt.UserResolution != nil {
			old := current.Receipt.UserResolution
			if old.Observation == observation && old.Evidence == evidence && old.ActorID == string(e.Actor.ActorId) {
				result = current
				return nil
			}
		}
		if current.State != "unknown" {
			return Fail("CONFLICT", "Only an uncertain outcome can receive a new manual resolution")
		}
		receipt := connectors.ReceiptFor(current, "unknown", "", "", "", "Provider outcome remains unknown. The user recorded a separate inspection; this proposal will not be resent.")
		if current.Receipt != nil {
			receipt = *current.Receipt
			receipt.Outcome = "unknown"
			receipt.RecordedAt = now()
		}
		receipt.UserResolution = &connectors.UserResolution{Version: 1, Observation: observation, Evidence: evidence, ActorID: string(e.Actor.ActorId), RecordedAt: now()}
		current.Receipt = &receipt
		current.State = "resolved"
		if err = saveConnectorProposal(ctx, tx, current); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, "INSERT INTO connector_receipts(proposal_id,body_json) VALUES(?,?)", id, string(marshal(receipt))); err != nil {
			return err
		}
		result = current
		return nil
	})
	return
}
