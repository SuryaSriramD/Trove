package workspace

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"

	"trove.local/phase0/contracts"
)

func (b *ProviderBridge) proposeActions(ctx context.Context, c *providerConnection, args map[string]any) (json.RawMessage, error) {
	if len(args) != 2 || args["plan"] == nil {
		return nil, Fail("INVALID_REQUEST", "Supply only idempotencyKey and plan")
	}
	key := str(args["idempotencyKey"])
	if key == "" || len(key) > 80 {
		return nil, Fail("INVALID_REQUEST", "A stable idempotency key of 1–80 characters is required")
	}
	raw := marshal(args["plan"])
	if err := contracts.Validate("service", "ActionPlan", raw); err != nil {
		return nil, Fail("INVALID_REQUEST", err.Error())
	}
	var plan ActionPlan
	if err := decodeAIDraft(string(raw), &plan); err != nil {
		return nil, err
	}
	digest := sha256.Sum256(marshal(plan))
	planHash := hex.EncodeToString(digest[:])
	sum := sha256.Sum256([]byte(c.info.ID + ":" + key))
	id := hex.EncodeToString(sum[:])
	// Retry must return the already resolved plan even if names changed after its
	// commit. It must never allocate a second set of canonical identities.
	var body, state string
	var result sql.NullString
	err := b.kernel.db.QueryRowContext(ctx, "SELECT body_json,state,result_json FROM provider_proposals WHERE id=?", id).Scan(&body, &state, &result)
	if err == nil {
		var previous ProviderProposal
		if err = json.Unmarshal([]byte(body), &previous); err != nil {
			return nil, err
		}
		if previous.PlanHash != planHash {
			return nil, Fail("IDEMPOTENCY_CONFLICT", "Proposal retry changed its reviewed actions")
		}
		previous.State = state
		if result.Valid {
			previous.Result = json.RawMessage(result.String)
		}
		return success(previous), nil
	}
	if err != sql.ErrNoRows {
		return nil, err
	}
	resolved, err := b.kernel.ResolveActionPlan(ctx, c.execution, plan, nil)
	if err != nil {
		return nil, err
	}
	return b.persistProposal(ctx, c, map[string]any{"idempotencyKey": key, "commands": resolved.Commands, "expectedRevisions": resolved.Guards}, planHash, resolved.Changes)
}
