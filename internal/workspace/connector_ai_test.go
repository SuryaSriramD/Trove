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

func TestConnectedAIPreparationUsesOnlySelectedSource(t *testing.T) {
	k, e, _, s := connectedFixture(t)
	calls := 0
	createPage(t, k, e, "The launch is PRIVATE-UNSELECTED")
	k.SetConnectorContextReader(func(context.Context, connectors.Account, connectors.Selection, string) (connectors.Snapshot, error) {
		calls++
		return s, nil
	})
	req := AIRequest{Mode: "answer", Prompt: "When is the launch?", ConnectedSources: []ConnectedSourceReference{ConnectorContextReference(s)}}
	p, err := k.PrepareAIWithBudget(context.Background(), e, req, wordBudget())
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Run.Sources) != 1 || p.Run.Sources[0].Connected == nil || calls != 2 {
		t.Fatal("selected connected context was ignored or current-access recheck missing", p.Run.Sources, calls)
	}
	if strings.Contains(p.Prompt, "PRIVATE-UNSELECTED") {
		t.Fatal("unselected canonical context leaked")
	}
}
func TestConnectedAIPreparationRejectsChangedSelectionDuringTokenization(t *testing.T) {
	k, e, a, s := connectedFixture(t)
	ctx := context.Background()
	k.SetConnectorContextReader(func(context.Context, connectors.Account, connectors.Selection, string) (connectors.Snapshot, error) {
		return s, nil
	})
	budget := wordBudget()
	original := budget.CountTokens
	visited := false
	budget.CountTokens = func(ctx context.Context, p []AIPrompt) ([]int, error) {
		visited = true
		if _, err := k.SetConnectorSelections(ctx, e, a.ID, a.Revision, nil); err != nil {
			return nil, err
		}
		return original(ctx, p)
	}
	_, err := k.PrepareAIWithBudget(ctx, e, AIRequest{Mode: "answer", Prompt: "When?", ConnectedSources: []ConnectedSourceReference{ConnectorContextReference(s)}}, budget)
	if !visited || err == nil {
		t.Fatal("tokenization race did not reject", visited, err)
	}
	var count int
	k.db.QueryRow("SELECT count(*) FROM ai_runs").Scan(&count)
	if count != 0 {
		t.Fatal("runnable prompt persisted", count)
	}
}

func TestConnectedAIFinishPreservesCitationAndRejectsRevokedAccess(t *testing.T) {
	k, e, _, s := connectedFixture(t)
	ctx := context.Background()
	denied := false
	k.SetConnectorContextReader(func(context.Context, connectors.Account, connectors.Selection, string) (connectors.Snapshot, error) {
		if denied {
			return s, errors.New("revoked")
		}
		return s, nil
	})
	req := AIRequest{Mode: "answer", Prompt: "When?", ConnectedSources: []ConnectedSourceReference{ConnectorContextReference(s)}}
	p, err := k.PrepareAIWithBudget(ctx, e, req, wordBudget())
	if err != nil {
		t.Fatal(err)
	}
	run, err := k.FinishAI(ctx, e, p.Run.ID, "The launch is Friday. [1]", "completed", "")
	if err != nil || run.State != "completed" || len(run.Citations) != 1 || run.Citations[0].Connected == nil || run.Citations[0].Location.PageId != "" {
		t.Fatal("lost connected citation", run, err)
	}
	p, err = k.PrepareAIWithBudget(ctx, e, req, wordBudget())
	if err != nil {
		t.Fatal(err)
	}
	denied = true
	run, err = k.FinishAI(ctx, e, p.Run.ID, "The launch is Friday. [1]", "completed", "")
	if err != nil || run.State != "failed" || run.Text != "" || len(run.Citations) != 0 {
		t.Fatal("revoked result published", run, err)
	}
}
func TestConnectedAIConversationDoesNotReuseUnselectedEvidence(t *testing.T) {
	k, e, _, s := connectedFixture(t)
	ctx := context.Background()
	k.SetConnectorContextReader(func(context.Context, connectors.Account, connectors.Selection, string) (connectors.Snapshot, error) {
		return s, nil
	})
	p, err := k.PrepareAIWithBudget(ctx, e, AIRequest{Mode: "answer", Prompt: "When?", ConnectedSources: []ConnectedSourceReference{ConnectorContextReference(s)}}, wordBudget())
	if err != nil {
		t.Fatal(err)
	}
	if _, err = k.FinishAI(ctx, e, p.Run.ID, "The launch is Friday. [1]", "completed", ""); err != nil {
		t.Fatal(err)
	}
	page := createPage(t, k, e, "An unrelated project")
	next, err := k.PrepareAIWithBudget(ctx, e, AIRequest{Mode: "answer", Prompt: "Describe this project.", PageID: page, ConversationID: p.Run.ConversationID}, wordBudget())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(next.Prompt, "Friday") {
		t.Fatal("prior connected conversation silently expanded context")
	}
}
func TestConnectedAIGmailApprovedWriteRetainsRestrictionAndRetry(t *testing.T) {
	k, e := testKernel(t)
	ctx := context.Background()
	a, err := k.ConnectConnector(ctx, e, connectors.Account{Kind: connectors.Gmail, Identity: connectors.Identity{Provider: "google", Subject: "mail-owner"}, RegistrationID: NewID()}, "")
	if err != nil {
		t.Fatal(err)
	}
	a, err = k.SetConnectorSelections(ctx, e, a.ID, a.Revision, []connectors.Selection{{ID: "inbox", Kind: "label", Label: "Inbox"}})
	if err != nil {
		t.Fatal(err)
	}
	s, err := k.StoreConnectorPreview(ctx, e, a, "inbox", connectors.Snapshot{RemoteID: "thread:1", RemoteRevision: "v1", Title: "Mail decision", Text: "The launch is Friday.", State: "present", Content: json.RawMessage(`{}`)})
	if err != nil {
		t.Fatal(err)
	}
	denied := false
	k.SetConnectorContextReader(func(context.Context, connectors.Account, connectors.Selection, string) (connectors.Snapshot, error) {
		if denied {
			return s, errors.New("revoked")
		}
		return s, nil
	})
	p, err := k.PrepareAIWithBudget(ctx, e, AIRequest{Mode: "answer", Prompt: "When?", ConnectedSources: []ConnectedSourceReference{ConnectorContextReference(s)}}, wordBudget())
	if err != nil {
		t.Fatal(err)
	}
	if _, err = k.FinishAI(ctx, e, p.Run.ID, "The launch is Friday. [1]", "completed", ""); err != nil {
		t.Fatal(err)
	}
	raw, err := prepareAIAction(ctx, k, e, map[string]any{"runId": p.Run.ID, "kind": "create_page", "title": "Reviewed mail summary"})
	if err != nil {
		t.Fatal(err)
	}
	proposal := raw.(AIProposal)
	if _, err = applyAIAction(ctx, k, e, map[string]any{"proposalId": proposal.ID}); err != nil {
		t.Fatal(err)
	}
	requireExternalPolicyDenied(t, k, externalPolicyGrant(k, e), proposal.PageID, "")
	if _, err = applyAIAction(ctx, k, e, map[string]any{"proposalId": proposal.ID}); err != nil {
		t.Fatal("duplicate apply lost receipt", err)
	}
	raw, err = prepareAIAction(ctx, k, e, map[string]any{"runId": p.Run.ID, "kind": "create_page", "title": "Another reviewed summary"})
	if err != nil {
		t.Fatal(err)
	}
	proposal = raw.(AIProposal)
	denied = true
	if _, err = applyAIAction(ctx, k, e, map[string]any{"proposalId": proposal.ID}); err == nil {
		t.Fatal("revoked remote source allowed reviewed write")
	}
	var n int
	k.db.QueryRow("SELECT count(*) FROM pages WHERE id=?", proposal.PageID).Scan(&n)
	if n != 0 {
		t.Fatal("failed review created canonical page")
	}
}
func TestConnectedAIStaleAfterNativeHostReset(t *testing.T) {
	k, e, _, s := connectedFixture(t)
	ctx := context.Background()
	reader := func(context.Context, connectors.Account, connectors.Selection, string) (connectors.Snapshot, error) {
		return s, nil
	}
	k.SetConnectorContextReader(reader)
	p, err := k.PrepareAIWithBudget(ctx, e, AIRequest{Mode: "answer", Prompt: "When?", ConnectedSources: []ConnectedSourceReference{ConnectorContextReference(s)}}, wordBudget())
	if err != nil {
		t.Fatal(err)
	}
	if _, err = k.FinishAI(ctx, e, p.Run.ID, "The launch is Friday. [1]", "completed", ""); err != nil {
		t.Fatal(err)
	}
	k.SetConnectorContextReader(reader)
	run, err := k.AIRun(ctx, e, p.Run.ID)
	if err != nil || run.Sources[0].State != "stale" || run.Citations[0].State != "stale" {
		t.Fatal("persisted receipt restored authority", run, err)
	}
	if _, err = prepareAIAction(ctx, k, e, map[string]any{"runId": run.ID, "kind": "create_page", "title": "Unreviewed restart"}); err == nil {
		t.Fatal("restored evidence authorized write")
	}
}

func TestConnectedAIRevocationAfterValidationBeforeCommit(t *testing.T) {
	for _, stage := range []string{"prepare", "finish"} {
		t.Run(stage, func(t *testing.T) {
			k, e, _, source := connectedFixture(t)
			ctx := context.Background()
			k.SetConnectorContextReader(func(context.Context, connectors.Account, connectors.Selection, string) (connectors.Snapshot, error) {
				return source, nil
			})
			request := AIRequest{Mode: "answer", Prompt: "When?", ConnectedSources: []ConnectedSourceReference{ConnectorContextReference(source)}}
			runID := ""
			if stage == "finish" {
				p, err := k.PrepareAIWithBudget(ctx, e, request, wordBudget())
				if err != nil {
					t.Fatal(err)
				}
				runID = p.Run.ID
			}
			entered, release := make(chan struct{}), make(chan struct{})
			conn, err := k.writeDB.Conn(ctx)
			if err != nil {
				t.Fatal(err)
			}
			err = conn.Raw(func(driver any) error {
				return driver.(*sqlite3.SQLiteConn).RegisterFunc("connected_commit_pause", func() int { close(entered); <-release; return 0 }, false)
			})
			conn.Close()
			if err != nil {
				t.Fatal(err)
			}
			operation := "INSERT"
			if stage == "finish" {
				operation = "UPDATE"
			}
			if _, err = k.writeDB.ExecContext(ctx, "CREATE TRIGGER connected_pause BEFORE "+operation+" ON ai_runs BEGIN SELECT connected_commit_pause(); END"); err != nil {
				t.Fatal(err)
			}
			done := make(chan error, 1)
			go func() {
				if stage == "prepare" {
					_, err := k.PrepareAIWithBudget(ctx, e, request, wordBudget())
					done <- err
				} else {
					_, err := k.FinishAI(ctx, e, runID, "The launch is Friday. [1]", "completed", "")
					done <- err
				}
			}()
			select {
			case <-entered:
			case <-time.After(3 * time.Second):
				close(release)
				t.Fatal("did not reach SQL after connected evidence validation")
			}
			// Match the atomic grant removal in RevokeGrant, without its job cleanup
			// waiting on the intentionally paused writer used by this regression.
			k.mu.Lock()
			delete(k.grants, string(e.ScopeGrantId))
			k.mu.Unlock()
			close(release)
			select {
			case err := <-done:
				if err == nil {
					t.Fatal("revoked owner committed")
				}
			case <-time.After(3 * time.Second):
				t.Fatal("writer did not return")
			}
			var count int
			if err := k.db.QueryRow("SELECT count(*) FROM ai_runs WHERE state='completed'").Scan(&count); err != nil {
				t.Fatal(err)
			}
			if count != 0 {
				t.Fatal("revoked result became completed")
			}
			if stage == "prepare" {
				if err := k.db.QueryRow("SELECT count(*) FROM ai_runs").Scan(&count); err != nil {
					t.Fatal(err)
				}
				if count != 0 {
					t.Fatal("revoked runnable prompt committed")
				}
			} else {
				var state, text string
				if err := k.db.QueryRow("SELECT state,text FROM ai_runs WHERE id=?", runID).Scan(&state, &text); err != nil {
					t.Fatal(err)
				}
				if state != "running" || text != "" {
					t.Fatal("revoked completion was not rolled back", state, text)
				}
			}
		})
	}
}
