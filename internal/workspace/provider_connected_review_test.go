package workspace

import (
	"context"
	"encoding/json"
	"testing"
	"time"
	"trove.local/phase0/contracts"

	"trove.local/phase0/internal/connectors"
)

func TestConnectedSharingExactRecipientReviewAndOneUse(t *testing.T) {
	k, e, _, s := connectedFixture(t)
	ctx := context.Background()
	calls := 0
	k.SetConnectorContextReader(func(context.Context, connectors.Account, connectors.Selection, string) (connectors.Snapshot, error) {
		calls++
		return s, nil
	})
	reviews := NewConnectedSharingReviews(k)
	target := ConnectedSharingTarget{Provider: "codex", Account: "reviewed-registration"}
	p, err := reviews.Preview(ctx, e, target, []ConnectedSourceReference{ConnectorContextReference(s)})
	if err != nil {
		t.Fatal(err)
	}
	if err := contracts.Validate("service", "ConnectedSharingPreview", marshal(p)); err != nil {
		t.Fatal(err)
	}
	if calls != 1 || len(p.Sources) != 1 {
		t.Fatal("missing exact source preview")
	}
	p.Sources[0].Text = "Injected after review"
	if _, err = reviews.Consume(ctx, e, ConnectedSharingTarget{Provider: "claude_code", Account: target.Account}, p.ID, p.Hash); err == nil {
		t.Fatal("switched recipient accepted")
	}
	if _, err = reviews.Consume(ctx, e, target, p.ID, "wrong"); err == nil {
		t.Fatal("changed review accepted")
	}
	approved, err := reviews.Consume(ctx, e, target, p.ID, p.Hash)
	if err != nil || approved.Sources[0].Text != s.Text || calls != 2 {
		t.Fatal("review mutated or live recheck missing", approved, calls, err)
	}
	if _, err = reviews.Consume(ctx, e, target, p.ID, p.Hash); err == nil {
		t.Fatal("sharing approval replayed")
	}
	if _, err = NewConnectedSharingReviews(k).Consume(ctx, e, target, p.ID, p.Hash); err == nil {
		t.Fatal("restored evidence reactivated sharing")
	}
}
func TestConnectedSharingDeniesGmailAndChangedSource(t *testing.T) {
	k, e, a, s := connectedFixture(t)
	ctx := context.Background()
	fresh := s
	calls := 0
	k.SetConnectorContextReader(func(context.Context, connectors.Account, connectors.Selection, string) (connectors.Snapshot, error) {
		calls++
		return fresh, nil
	})
	reviews := NewConnectedSharingReviews(k)
	target := ConnectedSharingTarget{"codex", "registration"}
	p, err := reviews.Preview(ctx, e, target, []ConnectedSourceReference{ConnectorContextReference(s)})
	if err != nil {
		t.Fatal(err)
	}
	fresh.Text = "Changed remote decision"
	if _, err = reviews.Consume(ctx, e, target, p.ID, p.Hash); err == nil {
		t.Fatal("changed source approved")
	}
	// Fixture the canonical kind, never rely on a caller-supplied provider claim.
	a.Kind = connectors.Gmail
	raw, _ := json.Marshal(a)
	if _, err = k.db.Exec("UPDATE connector_accounts SET body_json=? WHERE id=?", string(raw), a.ID); err != nil {
		t.Fatal(err)
	}
	before := calls
	if _, err = reviews.Preview(ctx, e, target, []ConnectedSourceReference{ConnectorContextReference(s)}); err == nil || calls != before {
		t.Fatal("Gmail external sharing reached retrieval")
	}
}

func TestConnectedSharingExpiryCancellationAndScope(t *testing.T) {
	k, e, _, s := connectedFixture(t)
	ctx := context.Background()
	k.SetConnectorContextReader(func(context.Context, connectors.Account, connectors.Selection, string) (connectors.Snapshot, error) {
		return s, nil
	})
	reviews := NewConnectedSharingReviews(k)
	target := ConnectedSharingTarget{"codex", "registration"}
	p, err := reviews.Preview(ctx, e, target, []ConnectedSourceReference{ConnectorContextReference(s)})
	if err != nil {
		t.Fatal(err)
	}
	external := externalPolicyGrant(k, e)
	if _, err = reviews.Consume(ctx, external, target, p.ID, p.Hash); err == nil {
		t.Fatal("external actor approved its own sharing")
	}
	if _, err = reviews.Preview(ctx, external, target, []ConnectedSourceReference{ConnectorContextReference(s)}); err == nil {
		t.Fatal("external actor expanded source scope")
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err = reviews.Consume(cancelled, e, target, p.ID, p.Hash); err == nil {
		t.Fatal("cancelled review consumed")
	}
	reviews.mu.Lock()
	expired := reviews.previews[p.ID]
	expired.ExpiresAt = time.Now().Add(-time.Minute).UTC().Format(time.RFC3339Nano)
	reviews.previews[p.ID] = expired
	reviews.mu.Unlock()
	if _, err = reviews.Consume(ctx, e, target, p.ID, p.Hash); err == nil {
		t.Fatal("expired approval accepted")
	}
}
