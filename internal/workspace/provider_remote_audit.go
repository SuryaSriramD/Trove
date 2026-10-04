package workspace

import (
	"context"
	"database/sql"
	"encoding/hex"
	"encoding/json"
)

// RecordRemoteDecision is a host-only owner operation. It adds the public client
// identity and immutable native review hash to the same provider audit trail as
// pairing, approval and disconnect, without storing OAuth URLs or credentials.
func (b *ProviderBridge) RecordRemoteDecision(ctx context.Context, connectionID, clientID, reviewHash string, approve bool) error {
	if err := requireOwnerScope(b.kernel, b.owner); err != nil {
		return err
	}
	digest, err := hex.DecodeString(reviewHash)
	if err != nil || len(digest) != 32 || len(clientID) < 1 || len(clientID) > 2048 {
		return Fail("INVALID_REQUEST", "Invalid remote review identity")
	}
	b.mu.Lock()
	c := b.connections[connectionID]
	connected := c != nil && c.info.State == "connected"
	b.mu.Unlock()
	if !connected {
		return Fail("SCOPE_DENIED", "Remote connection is no longer active")
	}
	decision := "denied"
	if approve {
		decision = "approved"
	}
	detail, _ := json.Marshal(map[string]string{"clientId": clientID, "reviewHash": reviewHash})
	return b.kernel.write(ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, "INSERT INTO provider_audit VALUES(?,?,?,?,?,?)", NewID(), connectionID, "remote_authorization", decision, string(detail), now())
		return err
	})
}
