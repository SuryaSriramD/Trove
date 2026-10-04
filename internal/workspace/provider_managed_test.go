package workspace

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestManagedProviderUsesSameScopedReviewAndRevocation(t *testing.T) {
	k, e := testKernel(t)
	ctx := context.Background()
	parent := createPage(t, k, e, "Approved parent")
	private := createPage(t, k, e, "Private")
	b := NewProviderBridge(k, e)
	defer b.Close()
	if _, err := b.Connect(ctx, "codex", []string{parent}, []string{"readPage"}); err == nil {
		t.Fatal("generic pairing enabled vendor mode")
	}
	if _, err := b.ConnectManaged(ctx, "codex", "account", "siwc", "api", []string{parent}, []string{"readPage"}); err == nil {
		t.Fatal("implicit billing switch")
	}
	c, err := b.ConnectManaged(ctx, "codex", "registration", "siwc", "chatgpt_plan", []string{parent}, []string{"readPage", "proposeActions"})
	if err != nil {
		t.Fatal(err)
	}
	tools, err := b.ManagedTools(c.ID, c.Token)
	if err != nil || len(tools) != 2 {
		t.Fatal(tools, err)
	}
	denied, err := b.Call(ctx, c.ID, c.Token, "readPage", map[string]any{"pageId": private})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(denied), "SCOPE_DENIED") {
		t.Fatal("unselected content exposed")
	}
	args := map[string]any{"idempotencyKey": "managed-create", "plan": map[string]any{"version": 1, "actions": []any{map[string]any{"kind": "create_page", "destination": "Approved parent", "title": "Reviewed native proposal"}}}}
	raw, err := b.Call(ctx, c.ID, c.Token, "proposeActions", args)
	if err != nil {
		t.Fatal(err)
	}
	var proposal struct {
		Result ProviderProposal `json:"result"`
	}
	if json.Unmarshal(raw, &proposal) != nil || proposal.Result.ID == "" {
		t.Fatal(string(raw))
	}
	var count int
	k.db.QueryRow("SELECT count(*) FROM pages WHERE title='Reviewed native proposal'").Scan(&count)
	if count != 0 {
		t.Fatal("managed runtime bypassed approval")
	}
	if _, err = b.Approve(ctx, proposal.Result.ID); err != nil {
		t.Fatal(err)
	}
	k.db.QueryRow("SELECT count(*) FROM pages WHERE title='Reviewed native proposal'").Scan(&count)
	if count != 1 {
		t.Fatal("approved managed proposal did not commit")
	}
	if err = b.Disconnect(ctx, c.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = b.ManagedTools(c.ID, c.Token); err == nil {
		t.Fatal("disconnected runtime retained tool schemas")
	}
	if _, err = b.Call(ctx, c.ID, c.Token, "proposeActions", args); err == nil {
		t.Fatal("disconnected runtime still called tools")
	}
}

func TestManagedExecutionRecordsRemainOwnerOnlyAndRecoverInterrupted(t *testing.T) {
	k, e := testKernel(t)
	ctx := context.Background()
	r := ManagedRun{ID: NewID(), SessionID: NewID(), ConnectionID: NewID(), Provider: "codex", Account: "synthetic-registration", State: "running", Manifest: json.RawMessage(`{"pages":[]}`)}
	if err := k.SaveManagedRun(ctx, e, r); err != nil {
		t.Fatal(err)
	}
	runs, err := k.ManagedRuns(ctx, e)
	if err != nil || len(runs) != 1 || runs[0].SessionID != r.SessionID {
		t.Fatal(runs, err)
	}
	// Reopening initializers recover in-flight provenance as interrupted.
	path := k.Path()
	if err = k.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	owner := reopened.OwnerContext()
	runs, err = reopened.ManagedRuns(ctx, owner)
	if err != nil || len(runs) != 1 || runs[0].State != "interrupted" {
		t.Fatal("recovery", runs, err)
	}
	reopened.RevokeGrant(string(owner.ScopeGrantId))
	if _, err = reopened.ManagedRuns(ctx, owner); err == nil {
		t.Fatal("revoked owner read execution provenance")
	}
}
