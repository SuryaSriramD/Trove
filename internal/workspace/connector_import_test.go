package workspace

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"trove.local/phase0/internal/connectors"
)

func TestConnectorImportRetainsIdentityThreeWayAndPartialBoundary(t *testing.T) {
	k, e := testKernel(t)
	ctx := context.Background()
	a, _ := connectorFixture(t, k, e)
	capture := func(text, rev, state string) connectors.Snapshot {
		s, err := k.StoreConnectorPreview(ctx, e, a, "org/repo", connectors.Snapshot{RemoteID: "issue:1", Title: "A source", Text: text, URL: "https://github.com/org/repo/issues/1", RemoteRevision: rev, State: state, Content: json.RawMessage(`{}`)})
		if err != nil {
			t.Fatal(err)
		}
		return s
	}
	source := capture("Base fact", "v1", "present")
	p, err := k.PreviewConnectorImport(ctx, e, source.ID, "v1")
	if err != nil {
		t.Fatal(err)
	}
	first := completeImportFixture(t, k, e, p)
	item := first.Items[0]
	if len(item.Blocks) != 2 {
		t.Fatal("connector body was not imported", item)
	}
	var revision string
	k.db.QueryRow("SELECT revision FROM pages WHERE id=?", item.PageID).Scan(&revision)
	testOK(t, dbBatch(t, k, e, []any{map[string]any{"kind": "page.update", "pageId": item.PageID, "title": "My local title"}}, "page", item.PageID))
	source = capture("Incoming fact", "v2", "present")
	p, err = k.PreviewConnectorImport(ctx, e, source.ID, "v2")
	if err != nil {
		t.Fatal(err)
	}
	if p.Items[0].PageID != item.PageID {
		t.Fatal("reimport changed page identity")
	}
	p = configureMergeFixture(t, k, p, nil)
	completeImportFixture(t, k, e, p)
	var title, body string
	k.db.QueryRow("SELECT title FROM pages WHERE id=?", item.PageID).Scan(&title)
	k.db.QueryRow("SELECT data_json FROM blocks WHERE id=?", item.Blocks[0].ID).Scan(&body)
	if title != "My local title" || !strings.Contains(body, "Incoming fact") {
		t.Fatal("merge lost local title or stable body", title, body)
	}
	if _, err = k.PreviewConnectorImport(ctx, e, source.ID, "v1"); err == nil {
		t.Fatal("stale external review accepted")
	}
	source = capture("truncated", "v3", "partial")
	if _, err = k.PreviewConnectorImport(ctx, e, source.ID, "v3"); err == nil {
		t.Fatal("partial body could replace complete source")
	}
	if _, err = k.ConnectorSnapshot(ctx, e, source.ID); err != nil {
		t.Fatal("partial preview must remain inspectable", err)
	}
}
func TestConnectorGmailNestedPortableProvenanceDeniesExternalContext(t *testing.T) {
	k, e := testKernel(t)
	ctx := context.Background()
	p := createPage(t, k, e, "Mail-derived")
	var nested any = map[string]any{"provider": "gmail"}
	for i := 0; i < 5; i++ {
		nested = map[string]any{"raw": map[string]any{"supplied": nested}}
	}
	if _, err := k.db.Exec("UPDATE pages SET provenance_json=? WHERE id=?", string(marshal(nested)), p); err != nil {
		t.Fatal(err)
	}
	external := e
	external.Actor.Kind = "external_client"
	if err := k.authorizePageScope(ctx, k.db, external, p); err == nil {
		t.Fatal("portable nesting bypassed Gmail context policy")
	}
	if err := k.authorizePageScope(ctx, k.db, e, p); err != nil {
		t.Fatal("owner access blocked", err)
	}
}
func TestConnectorRateCooldownAndIncrementalAbsence(t *testing.T) {
	k, e := testKernel(t)
	ctx := context.Background()
	a, adapter := connectorFixture(t, k, e)
	for _, id := range []string{"one", "two"} {
		if _, err := k.StoreConnectorPreview(ctx, e, a, "org/repo", connectors.Snapshot{RemoteID: id, Title: id, Content: json.RawMessage(`{}`)}); err != nil {
			t.Fatal(err)
		}
	}
	adapter.refresh = func(context.Context, connectors.Selection, string) (connectors.RefreshPage, error) {
		return connectors.RefreshPage{Items: []connectors.Snapshot{{RemoteID: "one", Title: "updated", Content: json.RawMessage(`{}`)}}, Complete: true, SyncCursor: "opaque-sync"}, nil
	}
	j, err := k.PrepareConnectorRefresh(ctx, e, a.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	if err = k.RunConnectorRefresh(ctx, e, j, adapter, connectors.Session{}); err != nil {
		t.Fatal(err)
	}
	result, err := connectorSearch(ctx, k, e, map[string]any{"query": ""})
	if err != nil || len(result.([]connectors.Snapshot)) != 2 {
		t.Fatal("incremental absence deleted retained source", result, err)
	}
	adapter.refresh = func(_ context.Context, _ connectors.Selection, cursor string) (connectors.RefreshPage, error) {
		if cursor != "opaque-sync" {
			t.Fatal("sync cursor not restored", cursor)
		}
		return connectors.RefreshPage{}, &connectors.Failure{Code: "RATE_LIMITED", Message: "rate limited", RetryAfter: time.Hour}
	}
	j, err = k.PrepareConnectorRefresh(ctx, e, a.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	if err = k.RunConnectorRefresh(ctx, e, j, adapter, connectors.Session{}); err == nil {
		t.Fatal("expected rate limit")
	}
	if _, err = k.PrepareConnectorRefresh(ctx, e, a.ID, j.ID); err == nil {
		t.Fatal("resume bypassed cooldown")
	}
	if _, err = k.PrepareConnectorRefresh(ctx, e, a.ID, ""); err == nil {
		t.Fatal("new job bypassed cooldown")
	}
}

func TestConnectorDraftAndCredentialCleanupSurviveInterruptedWork(t *testing.T) {
	k, e := testKernel(t)
	ctx := context.Background()
	a, _ := connectorFixture(t, k, e)
	draft, err := saveConnectorDraft(ctx, k, e, map[string]any{"accountId": a.ID, "expectedRevision": "", "fields": map[string]any{"body": "Unsent private composition"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = saveConnectorDraft(ctx, k, e, map[string]any{"accountId": a.ID, "expectedRevision": "", "fields": map[string]any{"body": "Stale overwrite"}}); err == nil {
		t.Fatal("stale draft replaced local composition")
	}
	got, err := readConnectorDraft(ctx, k, e, map[string]any{"accountId": a.ID})
	if err != nil || string(got.(ConnectorDraft).Fields) != string(draft.(ConnectorDraft).Fields) {
		t.Fatal(got, err)
	}
	registration := a.RegistrationID
	if err = k.DisconnectConnector(ctx, e, a.ID, a.Revision); err != nil {
		t.Fatal(err)
	}
	pending, err := k.ConnectorCredentialCleanup(ctx, e, a.ID)
	if err != nil || len(pending) != 1 || pending[0] != registration {
		t.Fatal("lost retryable native cleanup", pending, err)
	}
	current, err := k.ConnectorAccount(ctx, e, a.ID)
	if err != nil || current.State != "disconnected" || current.RegistrationID != "" {
		t.Fatal("disconnect retained activation", current, err)
	}
	if err = k.CompleteConnectorCredentialCleanup(ctx, e, a.ID, registration); err != nil {
		t.Fatal(err)
	}
	pending, err = k.ConnectorCredentialCleanup(ctx, e, a.ID)
	if err != nil || len(pending) != 0 {
		t.Fatal(pending, err)
	}
}
