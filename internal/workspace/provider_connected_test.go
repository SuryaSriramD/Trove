package workspace

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/mattn/go-sqlite3"
	"strings"
	"testing"
	"time"
	"trove.local/phase0/contracts"
	"trove.local/phase0/internal/connectors"
)

func reviewedProvider(t *testing.T, k *Kernel, e contracts.ServiceExecutionContext, s connectors.Snapshot, pages, tools []string) (*ProviderBridge, ProviderConnection) {
	t.Helper()
	b := NewProviderBridge(k, e)
	t.Cleanup(b.Close)
	p, err := b.PreviewConnectedContext(context.Background(), ConnectedSharingTarget{"codex", "registration"}, []ConnectedSourceReference{ConnectorContextReference(s)})
	if err != nil {
		t.Fatal(err)
	}
	c, err := b.ConnectManagedReviewed(context.Background(), "codex", "registration", "siwc", "chatgpt_plan", pages, tools, ConnectedSharingApproval{p.ID, p.Hash})
	if err != nil {
		t.Fatal(err)
	}
	return b, c
}
func connectedReader(k *Kernel, s connectors.Snapshot) {
	k.SetConnectorContextReader(func(context.Context, connectors.Account, connectors.Selection, string) (connectors.Snapshot, error) {
		return s, nil
	})
}
func TestProviderConnectedReviewCannotBeInferred(t *testing.T) {
	k, e, _, s := connectedFixture(t)
	ctx := context.Background()
	connectedReader(k, s)
	page := createPage(t, k, e, "Public choice")
	b := NewProviderBridge(k, e)
	defer b.Close()
	for _, pages := range [][]string{nil, {page}} {
		if _, err := b.ConnectManaged(ctx, "codex", "registration", "siwc", "chatgpt_plan", pages, []string{"readConnectedSource"}); err == nil {
			t.Fatal("unreviewed source authority")
		}
	}
	if _, err := b.ConnectManaged(ctx, "codex", "registration", "siwc", "chatgpt_plan", nil, []string{"readPage"}); err == nil {
		t.Fatal("ordinary empty scope accepted")
	}
	p, err := b.PreviewConnectedContext(ctx, ConnectedSharingTarget{"codex", "registration"}, []ConnectedSourceReference{ConnectorContextReference(s)})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = b.ConnectManagedReviewed(ctx, "codex", "another-registration", "siwc", "chatgpt_plan", nil, []string{"readConnectedSource"}, ConnectedSharingApproval{p.ID, p.Hash}); err == nil {
		t.Fatal("recipient substitution")
	}
	if _, err = b.ConnectManagedReviewed(ctx, "codex", "registration", "siwc", "chatgpt_plan", []string{page}, []string{"readPage"}, ConnectedSharingApproval{p.ID, p.Hash}); err == nil {
		t.Fatal("tool selected by source review")
	}
	c, err := b.ConnectManagedReviewed(ctx, "codex", "registration", "siwc", "chatgpt_plan", nil, []string{"readConnectedSource"}, ConnectedSharingApproval{p.ID, p.Hash})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = b.ConnectManagedReviewed(ctx, "codex", "registration", "siwc", "chatgpt_plan", nil, []string{"readConnectedSource"}, ConnectedSharingApproval{p.ID, p.Hash}); err == nil {
		t.Fatal("approval replayed")
	}
	raw, err := b.Call(ctx, c.ID, c.Token, "readConnectedSource", map[string]any{"snapshotId": s.ID})
	if err != nil || !strings.Contains(string(raw), s.Text) {
		t.Fatal(string(raw), err)
	}
	if _, err = b.Call(ctx, c.ID, c.Token, "readConnectedSource", map[string]any{"snapshotId": NewID()}); err == nil {
		t.Fatal("unselected source read")
	}
	if _, err = b.Call(ctx, c.ID, c.Token, "readConnectedSource", map[string]any{"snapshotId": s.ID, "tools": []string{"readPage"}}); err == nil {
		t.Fatal("unreviewed argument accepted")
	}
	if _, err = b.Call(ctx, c.ID, c.Token, "readPage", map[string]any{"pageId": page}); err == nil {
		t.Fatal("source instructions granted canonical tool")
	}
	status := b.Status()
	status[0].ConnectedSources[0].RemoteID = "forged"
	c.ConnectedSources[0].RemoteID = "forged"
	if err = b.ValidateConnectedContext(ctx, c.ID, c.Token); err != nil {
		t.Fatal("caller mutated retained receipt", err)
	}
}
func TestProviderConnectedEmptyScopeDeniesCanonicalSurfaces(t *testing.T) {
	k, e, _, s := connectedFixture(t)
	ctx := context.Background()
	connectedReader(k, s)
	page := createPage(t, k, e, "CANONICAL_PRIVATE_ORCHID")
	f := newDatabase(t, k, e)
	newRecord(t, k, e, f, "CANONICAL_PRIVATE_ORCHID")
	b, c := reviewedProvider(t, k, e, s, nil, []string{"readConnectedSource", "readPage", "search.query", "queryRecords", "resolveCitation", "proposeChanges", "proposeActions"})
	ext, err := b.authenticate(c.ID, c.Token)
	if err != nil {
		t.Fatal(err)
	}
	if err = k.authorize(ext.execution); err != nil {
		t.Fatal("explicit empty pages is not a valid deny-all grant", err)
	}
	for tool, args := range map[string]map[string]any{
		"readPage": {"pageId": page}, "queryRecords": {"collectionId": f.collection}, "resolveCitation": {"location": map[string]any{"pageId": page, "sourceRevision": "1"}},
	} {
		raw, err := b.Call(ctx, c.ID, c.Token, tool, args)
		if err != nil {
			t.Fatal(tool, err)
		}
		if strings.Contains(string(raw), "CANONICAL_PRIVATE_ORCHID") || !strings.Contains(string(raw), "SCOPE_DENIED") {
			t.Fatal(tool, string(raw))
		}
	}
	raw, err := b.Call(ctx, c.ID, c.Token, "search.query", map[string]any{"query": "CANONICAL_PRIVATE_ORCHID"})
	if err != nil {
		t.Fatal(err)
	}
	var search struct {
		OK     bool          `json:"ok"`
		Result SearchResults `json:"result"`
	}
	if err = json.Unmarshal(raw, &search); err != nil || !search.OK || len(search.Result.Hits) != 0 {
		t.Fatal(string(raw), err)
	}
	testCode(t, testDispatch(t, k, ext.execution, "page.freshness", advancedRequest(k, map[string]any{"pageId": page})), "SCOPE_DENIED")
	if k.authorizePage(ctx, k.db, ext.execution, page) == nil || k.authorizeEntity(ctx, k.db, ext.execution, "collection", f.collection) == nil {
		t.Fatal("empty scope became workspace")
	}
	for tool, args := range map[string]map[string]any{
		"proposeChanges": {"idempotencyKey": "not-allowed", "commands": []any{map[string]any{"kind": "page.update", "pageId": page, "title": "leak"}}, "expectedRevisions": []contracts.ServiceExpectedRevision{required("page", page, "1")}},
		"proposeActions": {"idempotencyKey": "not-allowed", "plan": map[string]any{"version": 1, "actions": []any{map[string]any{"kind": "create_page", "destination": "CANONICAL_PRIVATE_ORCHID", "title": "Forbidden"}}}},
	} {
		if _, err = b.Call(ctx, c.ID, c.Token, tool, args); err == nil {
			t.Fatal("empty canonical grant proposed write", tool)
		}
	}
	var count int
	if err = k.db.QueryRow("SELECT count(*) FROM provider_proposals").Scan(&count); err != nil || count != 0 {
		t.Fatal(count, err)
	}
}
func TestProviderConnectedReadsRejectChangesAndLateRevocation(t *testing.T) {
	for _, scenario := range []string{"remote_revision", "remote_denied", "account_selection", "owner_revoked", "connection_revoked", "host_reset", "cancelled"} {
		t.Run(scenario, func(t *testing.T) {
			k, e, a, s := connectedFixture(t)
			ctx := context.Background()
			changed := false
			var b *ProviderBridge
			var c ProviderConnection
			k.SetConnectorContextReader(func(_ context.Context, _ connectors.Account, _ connectors.Selection, _ string) (connectors.Snapshot, error) {
				if !changed {
					return s, nil
				}
				fresh := s
				switch scenario {
				case "remote_revision":
					fresh.RemoteRevision = "v2"
				case "remote_denied":
					return fresh, errors.New("revoked remote access")
				case "account_selection":
					if _, err := k.SetConnectorSelections(ctx, e, a.ID, a.Revision, nil); err != nil {
						t.Fatal(err)
					}
				case "owner_revoked":
					k.mu.Lock()
					delete(k.grants, string(e.ScopeGrantId))
					k.mu.Unlock()
				case "connection_revoked":
					b.mu.Lock()
					live := b.connections[c.ID]
					live.cancel()
					b.mu.Unlock()
				}
				return fresh, nil
			})
			b, c = reviewedProvider(t, k, e, s, nil, []string{"readConnectedSource"})
			changed = true
			if scenario == "host_reset" {
				connectedReader(k, s)
			}
			if scenario == "cancelled" {
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			}
			raw, err := b.Call(ctx, c.ID, c.Token, "readConnectedSource", map[string]any{"snapshotId": s.ID})
			if err == nil || len(raw) != 0 {
				t.Fatal("changed authority returned source", string(raw), err)
			}
		})
	}
}
func TestProviderConnectedProposalRecheckAndExactlyOnce(t *testing.T) {
	for _, scenario := range []string{"unchanged", "remote_changed", "selection_changed", "revoked_at_commit"} {
		t.Run(scenario, func(t *testing.T) {
			k, e, a, s := connectedFixture(t)
			ctx := context.Background()
			fresh := s
			k.SetConnectorContextReader(func(context.Context, connectors.Account, connectors.Selection, string) (connectors.Snapshot, error) {
				return fresh, nil
			})
			page := createPage(t, k, e, "Approved destination")
			b, c := reviewedProvider(t, k, e, s, []string{page}, []string{"readConnectedSource", "proposeChanges"})
			args := map[string]any{"idempotencyKey": "once", "commands": []any{map[string]any{"kind": "page.update", "pageId": page, "title": "Reviewed source result"}}, "expectedRevisions": []contracts.ServiceExpectedRevision{required("page", page, "1")}}
			raw, err := b.Call(ctx, c.ID, c.Token, "proposeChanges", args)
			if err != nil {
				t.Fatal(err)
			}
			var p struct {
				Result ProviderProposal `json:"result"`
			}
			if err = json.Unmarshal(raw, &p); err != nil || len(p.Result.ConnectedSources) != 1 {
				t.Fatal(string(raw), err)
			}
			if scenario == "remote_changed" {
				fresh.Text = "Different decision"
			}
			if scenario == "selection_changed" {
				if _, err = k.SetConnectorSelections(ctx, e, a.ID, a.Revision, nil); err != nil {
					t.Fatal(err)
				}
			}
			if scenario == "revoked_at_commit" {
				index := len(commitValidators)
				defer func() { commitValidators = commitValidators[:index] }()
				RegisterCommitValidator(func(m *Mutation) error {
					if m.Execution.Actor.Kind == "external_client" {
						k.mu.Lock()
						delete(k.grants, string(e.ScopeGrantId))
						k.mu.Unlock()
					}
					return nil
				})
			}
			response, err := b.Approve(ctx, p.Result.ID)
			if scenario == "unchanged" {
				if err != nil {
					t.Fatal(err)
				}
				var envelope map[string]any
				if json.Unmarshal(response, &envelope) != nil {
					t.Fatal(string(response))
				}
				testOK(t, envelope)
				replay, err := b.Approve(ctx, p.Result.ID)
				if err != nil || string(replay) != string(response) {
					t.Fatal("effect replay differs", err)
				}
				var rev string
				if err = k.db.QueryRow("SELECT revision FROM pages WHERE id=?", page).Scan(&rev); err != nil || rev != "2" {
					t.Fatal("duplicated effect", rev, err)
				}
			} else {
				if err == nil && !strings.Contains(string(response), "SCOPE_DENIED") {
					t.Fatal("stale source approved", string(response))
				}
				var title string
				if err = k.db.QueryRow("SELECT title FROM pages WHERE id=?", page).Scan(&title); err != nil || title != "Approved destination" {
					t.Fatal("invalid source wrote", title, err)
				}
			}
		})
	}
}
func TestProviderConnectedManagedPublicationRequiresReviewAndLiveReceipts(t *testing.T) {
	k, e, a, s := connectedFixture(t)
	ctx := context.Background()
	connectedReader(k, s)
	b, c := reviewedProvider(t, k, e, s, nil, []string{"readConnectedSource"})
	r := ManagedRun{ID: NewID(), SessionID: NewID(), ConnectionID: c.ID, Provider: "codex", Account: "registration", ConnectedRecipient: &ConnectedSharingTarget{"codex", "registration"}, State: "starting", ConnectedSources: c.ConnectedSources, Manifest: json.RawMessage(`{"pages":[]}`)}
	if err := k.SaveManagedRun(ctx, e, r); err == nil {
		t.Fatal("standalone receipt bypassed sharing binding")
	}
	if err := b.SaveManagedRun(ctx, c.ID, c.Token, r); err != nil {
		t.Fatal(err)
	}
	if _, err := k.SetConnectorSelections(ctx, e, a.ID, a.Revision, nil); err != nil {
		t.Fatal(err)
	}
	r.State = "completed"
	r.Text = "must not publish"
	if err := b.SaveManagedRun(ctx, c.ID, c.Token, r); err == nil {
		t.Fatal("stale connected result published")
	}
	runs, err := k.ManagedRuns(ctx, e)
	if err != nil || len(runs) != 1 || runs[0].State != "starting" || runs[0].Text != "" {
		t.Fatal(runs, err)
	}
	r.State = "failed"
	if err = k.SaveManagedRun(ctx, e, r); err == nil {
		t.Fatal("failure retained private output")
	}
	r.Text = ""
	if err = k.SaveManagedRun(ctx, e, r); err != nil {
		t.Fatal("safe recovery record failed", err)
	}
}
func TestProviderConnectedManagedRevocationAfterValidationBeforeCommit(t *testing.T) {
	for _, which := range []string{"owner", "provider"} {
		t.Run(which, func(t *testing.T) {
			k, e, _, s := connectedFixture(t)
			ctx := context.Background()
			connectedReader(k, s)
			b, c := reviewedProvider(t, k, e, s, nil, []string{"readConnectedSource"})
			entered, release := make(chan struct{}), make(chan struct{})
			conn, err := k.writeDB.Conn(ctx)
			if err != nil {
				t.Fatal(err)
			}
			err = conn.Raw(func(driver any) error {
				return driver.(*sqlite3.SQLiteConn).RegisterFunc("provider_connected_pause", func() int { close(entered); <-release; return 0 }, false)
			})
			conn.Close()
			if err != nil {
				t.Fatal(err)
			}
			if _, err = k.writeDB.ExecContext(ctx, "CREATE TRIGGER provider_connected_pause BEFORE INSERT ON managed_provider_runs BEGIN SELECT provider_connected_pause(); END"); err != nil {
				t.Fatal(err)
			}
			r := ManagedRun{ID: NewID(), SessionID: NewID(), ConnectionID: c.ID, Provider: "codex", Account: "registration", ConnectedRecipient: &ConnectedSharingTarget{"codex", "registration"}, State: "completed", Text: "PRIVATE", ConnectedSources: c.ConnectedSources, Manifest: json.RawMessage(`{"pages":[]}`)}
			done := make(chan error, 1)
			go func() { done <- b.SaveManagedRun(ctx, c.ID, c.Token, r) }()
			select {
			case <-entered:
			case <-time.After(3 * time.Second):
				close(release)
				t.Fatal("missing post-validation pause")
			}
			execution := e
			if which == "provider" {
				live, err := b.authenticate(c.ID, c.Token)
				if err != nil {
					t.Fatal(err)
				}
				execution = live.execution
			}
			k.mu.Lock()
			delete(k.grants, string(execution.ScopeGrantId))
			k.mu.Unlock()
			close(release)
			select {
			case err := <-done:
				if err == nil {
					t.Fatal("revoked publication committed")
				}
			case <-time.After(3 * time.Second):
				t.Fatal("blocked commit")
			}
			var count int
			if err = k.db.QueryRow("SELECT count(*) FROM managed_provider_runs").Scan(&count); err != nil || count != 0 {
				t.Fatal(count, err)
			}
		})
	}
}
