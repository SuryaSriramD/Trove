package workspace

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestReviewedProviderNamedCreationRetryStaleAndRevocation(t *testing.T) {
	k, e := testKernel(t)
	ctx := context.Background()
	parent := createPage(t, k, e, "Delivery")
	tasks := ensureTestTasks(t, k, e)
	bridge := NewProviderBridge(k, e)
	defer bridge.Close()
	c, err := bridge.Connect(ctx, "mcp", []string{parent, tasks.page}, []string{"proposeActions", "readPage"})
	if err != nil {
		t.Fatal(err)
	}
	args := map[string]any{"idempotencyKey": "new-page-and-task", "plan": map[string]any{"version": 1, "actions": []any{map[string]any{"kind": "create_page", "destination": "Delivery", "title": "Decision record", "body": "Keep original files."}, map[string]any{"kind": "create_task", "destination": "Tasks", "title": "Verify backup", "due": "2026-10-20"}}}}
	raw, err := bridge.Call(ctx, c.ID, c.Token, "proposeActions", args)
	if err != nil {
		t.Fatal(err)
	}
	var envelope struct {
		Result ProviderProposal `json:"result"`
	}
	json.Unmarshal(raw, &envelope)
	proposal := envelope.Result
	if len(proposal.Changes) != 2 || proposal.PlanHash == "" {
		t.Fatal("human review missing", proposal)
	}
	replay, err := bridge.Call(ctx, c.ID, c.Token, "proposeActions", args)
	if err != nil || string(raw) != string(replay) {
		t.Fatal("pending retry changed identities", err)
	}
	var count int
	k.db.QueryRow("SELECT count(*) FROM pages WHERE title IN('Decision record','Verify backup')").Scan(&count)
	if count != 0 {
		t.Fatal("proposal applied before approval")
	}
	approved, err := bridge.Approve(ctx, proposal.ID)
	if err != nil {
		t.Fatal(err)
	}
	var response map[string]any
	json.Unmarshal(approved, &response)
	testOK(t, response)
	k.db.QueryRow("SELECT count(*) FROM pages WHERE title IN('Decision record','Verify backup')").Scan(&count)
	if count != 2 {
		t.Fatal("creation incomplete", count)
	}
	var child string
	k.db.QueryRow("SELECT id FROM pages WHERE title='Decision record'").Scan(&child)
	denied, err := bridge.Call(ctx, c.ID, c.Token, "readPage", map[string]any{"pageId": child})
	if err != nil {
		t.Fatal(err)
	}
	json.Unmarshal(denied, &response)
	testCode(t, response, "SCOPE_DENIED")
	if again, err := bridge.Approve(ctx, proposal.ID); err != nil || string(again) != string(approved) {
		t.Fatal("approval retry changed effect", err)
	}
	if _, err = bridge.Call(ctx, c.ID, c.Token, "proposeActions", args); err != nil {
		t.Fatal("committed typed retry failed", err)
	}
	staleArgs := map[string]any{"idempotencyKey": "stale-parent", "plan": map[string]any{"version": 1, "actions": []any{map[string]any{"kind": "create_page", "destination": "Delivery", "title": "Must not exist"}}}}
	raw, err = bridge.Call(ctx, c.ID, c.Token, "proposeActions", staleArgs)
	if err != nil {
		t.Fatal(err)
	}
	json.Unmarshal(raw, &envelope)
	testOK(t, dbBatch(t, k, e, []any{map[string]any{"kind": "page.update", "pageId": parent, "title": "Delivery updated"}}, "page", parent))
	result, err := bridge.Approve(ctx, envelope.Result.ID)
	if err != nil {
		t.Fatal(err)
	}
	json.Unmarshal(result, &response)
	testCode(t, response, "CONFLICT")
	k.db.QueryRow("SELECT count(*) FROM pages WHERE title='Must not exist'").Scan(&count)
	if count != 0 {
		t.Fatal("stale parent created content")
	}
	original, _ := bridge.authenticate(c.ID, c.Token)
	k.RevokeGrant(string(original.execution.ScopeGrantId))
	if _, err = bridge.Call(ctx, c.ID, c.Token, "proposeActions", args); err == nil {
		t.Fatal("revoked grant replay exposed content")
	}
}
func TestNamedAIPlanReviewUsesSharedApprovalAndPreservesOrigin(t *testing.T) {
	k, e := testKernel(t)
	ctx := context.Background()
	parent := createPage(t, k, e, "Launch")
	prepared, err := k.PrepareAI(ctx, e, AIRequest{Mode: "workspace_agent", Prompt: "Create a page called Checklist under Launch", PageID: parent})
	if err != nil {
		t.Fatal(err)
	}
	draft := `{"version":1,"actions":[{"kind":"create_page","destination":"Launch","title":"Checklist","body":"- Verify backup"}]}`
	run, err := k.FinishAI(ctx, e, prepared.Run.ID, draft, "completed", "")
	if err != nil {
		t.Fatal(err)
	}
	value, err := prepareAIAction(ctx, k, e, map[string]any{"runId": run.ID, "kind": "workspace_patch"})
	if err != nil {
		t.Fatal(err)
	}
	proposal := value.(AIProposal)
	repeat, err := prepareAIAction(ctx, k, e, map[string]any{"runId": run.ID, "kind": "workspace_patch"})
	if err != nil || repeat.(AIProposal).ID != proposal.ID {
		t.Fatal("repeated review allocated a second proposal", err)
	}
	if len(proposal.Changes) != 1 || strings.Contains(proposal.After, "expectedRevisions") {
		t.Fatal("review is not understandable", proposal)
	}
	if _, err = applyAIAction(ctx, k, e, map[string]any{"proposalId": proposal.ID}); err != nil {
		t.Fatal(err)
	}
	if _, err = applyAIAction(ctx, k, e, map[string]any{"proposalId": proposal.ID}); err != nil {
		t.Fatal(err)
	}
	var origin, actor string
	if err = k.db.QueryRow("SELECT json_extract(payload_json,'$.context.origin'),json_extract(payload_json,'$.context.actor.kind') FROM committed_events ORDER BY cursor DESC LIMIT 1").Scan(&origin, &actor); err != nil {
		t.Fatal(err)
	}
	if origin != "ai" || actor != "local_ai" {
		t.Fatal("generated write lost provenance", origin, actor)
	}
	var n int
	k.db.QueryRow("SELECT count(*) FROM pages WHERE title='Checklist'").Scan(&n)
	if n != 1 {
		t.Fatal("repeated approval duplicated content", n)
	}
}
func TestAIApprovalCannotOutliveApprovingScope(t *testing.T) {
	k, e := testKernel(t)
	ctx := context.Background()
	page := createPage(t, k, e, "Current")
	prepared, err := k.PrepareAI(ctx, e, AIRequest{Mode: "generate", Prompt: "Write a note"})
	if err != nil {
		t.Fatal(err)
	}
	run, err := k.FinishAI(ctx, e, prepared.Run.ID, "draft", "completed", "")
	if err != nil {
		t.Fatal(err)
	}
	value, err := prepareAIAction(ctx, k, e, map[string]any{"runId": run.ID, "kind": "create_page", "title": "Revoked draft"})
	if err != nil {
		t.Fatal(err)
	}
	old := len(commitValidators)
	defer func() { commitValidators = commitValidators[:old] }()
	RegisterCommitValidator(func(m *Mutation) error {
		if m.Kernel == k && m.Execution.Origin == "ai" {
			k.mu.Lock()
			delete(k.grants, string(e.ScopeGrantId))
			k.mu.Unlock()
		}
		return nil
	})
	if _, err = applyAIAction(ctx, k, e, map[string]any{"proposalId": value.(AIProposal).ID}); err == nil {
		t.Fatal("revoked approving context committed")
	}
	var n int
	k.db.QueryRow("SELECT count(*) FROM pages WHERE title='Revoked draft'").Scan(&n)
	if n != 0 {
		t.Fatal("revoked proposal had partial effects")
	}
	_ = page
}

func TestMCPNamedPlanDiscoveryCallAndDisconnect(t *testing.T) {
	k, e := testKernel(t)
	ctx := context.Background()
	parent := createPage(t, k, e, "Destination")
	b := NewProviderBridge(k, e)
	defer b.Close()
	c, err := b.Connect(ctx, "mcp", []string{parent}, []string{"proposeActions"})
	if err != nil {
		t.Fatal(err)
	}
	h := b.MCPHandler()
	call := func(method string, params any) *httptest.ResponseRecorder {
		request := httptest.NewRequest(http.MethodPost, "http://127.0.0.1/mcp", strings.NewReader(string(marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": method, "params": params}))))
		request.Header.Set("Authorization", "Bearer "+c.Token)
		request.Header.Set("Trove-Connection-Id", c.ID)
		response := httptest.NewRecorder()
		h.ServeHTTP(response, request)
		return response
	}
	discovered := call("tools/list", map[string]any{}).Body.String()
	if !strings.Contains(discovered, `"create_task"`) || strings.Contains(discovered, `"$ref"`) || !strings.Contains(discovered, `"additionalProperties":false`) {
		t.Fatal("authoritative closed named-plan schema missing", discovered)
	}
	args := map[string]any{"idempotencyKey": "disconnect-plan", "plan": map[string]any{"version": 1, "actions": []any{map[string]any{"kind": "create_page", "destination": "Destination", "title": "Cancelled child"}}}}
	response := call("tools/call", map[string]any{"name": "proposeActions", "arguments": args})
	if response.Code != 200 || !strings.Contains(response.Body.String(), `"isError":false`) {
		t.Fatal(response.Body.String())
	}
	proposals, err := b.Proposals(ctx)
	if err != nil || len(proposals) != 1 {
		t.Fatal(proposals, err)
	}
	if err = b.Disconnect(ctx, c.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = b.Approve(ctx, proposals[0].ID); err == nil {
		t.Fatal("disconnected proposal applied")
	}
	if response = call("tools/list", map[string]any{}); response.Code != 401 {
		t.Fatal("disconnected discovery accepted", response.Code)
	}
	var count int
	k.db.QueryRow("SELECT count(*) FROM pages WHERE title='Cancelled child'").Scan(&count)
	if count != 0 {
		t.Fatal("disconnected client changed content")
	}
}
