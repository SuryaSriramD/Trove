package workspace

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
	"trove.local/phase0/contracts"
)

func TestProviderScopedReadReviewRetryAndDisconnect(t *testing.T) {
	k, e := testKernel(t)
	ctx := context.Background()
	page := createPage(t, k, e, "approved")
	secret := createPage(t, k, e, "not shared")
	bridge := NewProviderBridge(k, e)
	defer bridge.Close()
	c, err := bridge.Connect(ctx, "mcp", []string{page}, []string{"readPage", "proposeChanges"})
	if err != nil {
		t.Fatal(err)
	}
	denied, err := bridge.Call(ctx, c.ID, c.Token, "readPage", map[string]any{"pageId": secret})
	if err != nil {
		t.Fatal(err)
	}
	var envelope map[string]any
	json.Unmarshal(denied, &envelope)
	testCode(t, envelope, "SCOPE_DENIED")
	if _, err = bridge.Call(ctx, c.ID, c.Token, "batch", map[string]any{}); err == nil {
		t.Fatal("denied tool ran")
	}
	args := map[string]any{"idempotencyKey": "test", "commands": []any{map[string]any{"kind": "page.update", "pageId": page, "title": "reviewed"}}, "expectedRevisions": []contracts.ServiceExpectedRevision{required("page", page, "1")}}
	raw, err := bridge.Call(ctx, c.ID, c.Token, "proposeChanges", args)
	if err != nil {
		t.Fatal(err)
	}
	var proposal struct {
		Result ProviderProposal `json:"result"`
	}
	json.Unmarshal(raw, &proposal)
	if readPage(t, k, e, page)["page"].(map[string]any)["title"] != "approved" {
		t.Fatal("proposal applied without user")
	}
	first, err := bridge.Approve(ctx, proposal.Result.ID)
	if err != nil {
		t.Fatal(err)
	}
	json.Unmarshal(first, &envelope)
	testOK(t, envelope)
	second, err := bridge.Approve(ctx, proposal.Result.ID)
	if err != nil || string(first) != string(second) {
		t.Fatal("retry differs", err)
	}
	replay, err := bridge.Call(ctx, c.ID, c.Token, "proposeChanges", args)
	if err != nil {
		t.Fatal(err)
	}
	var replayEnvelope struct {
		Result ProviderProposal `json:"result"`
	}
	json.Unmarshal(replay, &replayEnvelope)
	if replayEnvelope.Result.State != "applied" || string(replayEnvelope.Result.Result) != string(first) {
		t.Fatal("proposal retry did not return committed state")
	}
	args["commands"] = []any{map[string]any{"kind": "page.update", "pageId": page, "title": "changed payload"}}
	if _, err = bridge.Call(ctx, c.ID, c.Token, "proposeChanges", args); err == nil {
		t.Fatal("retry accepted changed content")
	}
	if err = bridge.Disconnect(ctx, c.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = bridge.Call(ctx, c.ID, c.Token, "readPage", map[string]any{"pageId": page}); err == nil {
		t.Fatal("revoked connection read")
	}
}
func TestProviderMCPAuthenticationAndOrigin(t *testing.T) {
	k, e := testKernel(t)
	page := createPage(t, k, e, "Context")
	b := NewProviderBridge(k, e)
	defer b.Close()
	c, err := b.Connect(context.Background(), "mcp", []string{page}, []string{"readPage"})
	if err != nil {
		t.Fatal(err)
	}
	h := b.MCPHandler()
	call := func(token, origin string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodPost, "http://127.0.0.1/mcp", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`))
		r.Header.Set("Authorization", "Bearer "+token)
		r.Header.Set("Trove-Connection-Id", c.ID)
		if origin != "" {
			r.Header.Set("Origin", origin)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	if w := call("wrong", ""); w.Code != 401 {
		t.Fatal(w.Code)
	}
	if w := call(c.Token, "https://untrusted.example"); w.Code != 403 {
		t.Fatal(w.Code)
	}
	if w := call(c.Token, ""); w.Code != 200 || !strings.Contains(w.Body.String(), "protocolVersion") {
		t.Fatal(w.Body.String())
	}
}
func TestProviderUnqualifiedVendorBlocked(t *testing.T) {
	k, e := testKernel(t)
	b := NewProviderBridge(k, e)
	for _, vendor := range []string{"claude_code", "codex"} {
		if _, err := b.Connect(context.Background(), vendor, []string{}, []string{}); err == nil {
			t.Fatal("unqualified vendor enabled")
		}
	}
}

func TestProviderStaleRevisionAndRevocationBeforeCommit(t *testing.T) {
	k, e := testKernel(t)
	ctx := context.Background()
	page := createPage(t, k, e, "initial")
	b := NewProviderBridge(k, e)
	defer b.Close()
	c, err := b.Connect(ctx, "mcp", []string{page}, []string{"proposeChanges"})
	if err != nil {
		t.Fatal(err)
	}
	raw, err := b.Call(ctx, c.ID, c.Token, "proposeChanges", map[string]any{"idempotencyKey": "stale", "commands": []any{map[string]any{"kind": "page.update", "pageId": page, "title": "provider"}}, "expectedRevisions": []contracts.ServiceExpectedRevision{required("page", page, "1")}})
	if err != nil {
		t.Fatal(err)
	}
	var proposal struct {
		Result ProviderProposal `json:"result"`
	}
	json.Unmarshal(raw, &proposal)
	testOK(t, testDispatch(t, k, e, "batch", testRequest(k, e, []any{map[string]any{"kind": "page.update", "pageId": page, "title": "user edit"}}, required("page", page, "1"))))
	response, err := b.Approve(ctx, proposal.Result.ID)
	if err != nil {
		t.Fatal(err)
	}
	var envelope map[string]any
	json.Unmarshal(response, &envelope)
	testCode(t, envelope, "CONFLICT")
	// A late revocation is checked by the transaction's final commit fence.
	entered, proceed := make(chan struct{}), make(chan struct{})
	index := len(commitValidators)
	RegisterCommitValidator(func(m *Mutation) error {
		if _, ok := m.before[page]; ok {
			close(entered)
			<-proceed
		}
		return nil
	})
	defer func() { commitValidators = commitValidators[:index] }()
	grant := k.OwnerContext()
	done := make(chan json.RawMessage, 1)
	go func() {
		done <- k.Dispatch(ctx, grant, "batch", marshal(testRequest(k, grant, []any{map[string]any{"kind": "page.update", "pageId": page, "title": "revoked"}}, required("page", page, "2"))))
	}()
	<-entered
	// This is the same atomic in-memory revocation performed by RevokeGrant;
	// avoiding its job cleanup here lets the paused transaction reach its fence.
	k.mu.Lock()
	delete(k.grants, string(grant.ScopeGrantId))
	k.mu.Unlock()
	close(proceed)
	json.Unmarshal(<-done, &envelope)
	testCode(t, envelope, "SCOPE_DENIED")
	if readPage(t, k, e, page)["page"].(map[string]any)["title"] != "user edit" {
		t.Fatal("revoked write committed")
	}
}

func TestProviderDisconnectCancelsApprovalBeforeCommit(t *testing.T) {
	k, e := testKernel(t)
	ctx := context.Background()
	page := createPage(t, k, e, "before")
	b := NewProviderBridge(k, e)
	defer b.Close()
	c, err := b.Connect(ctx, "mcp", []string{page}, []string{"proposeChanges"})
	if err != nil {
		t.Fatal(err)
	}
	raw, err := b.Call(ctx, c.ID, c.Token, "proposeChanges", map[string]any{"idempotencyKey": "disconnect-fence", "commands": []any{map[string]any{"kind": "page.update", "pageId": page, "title": "must not commit"}}, "expectedRevisions": []contracts.ServiceExpectedRevision{required("page", page, "1")}})
	if err != nil {
		t.Fatal(err)
	}
	var proposal struct {
		Result ProviderProposal `json:"result"`
	}
	json.Unmarshal(raw, &proposal)
	entered, release := make(chan struct{}), make(chan struct{})
	index := len(commitValidators)
	RegisterCommitValidator(func(m *Mutation) error {
		if m.Execution.Actor.Kind == "external_client" {
			close(entered)
			<-release
		}
		return nil
	})
	defer func() { commitValidators = commitValidators[:index] }()
	done := make(chan error, 1)
	go func() { _, err := b.Approve(ctx, proposal.Result.ID); done <- err }()
	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		t.Fatal("approval never reached commit fence")
	}
	disconnected := make(chan error, 1)
	go func() { disconnected <- b.Disconnect(ctx, c.ID) }()
	deadline := time.Now().Add(time.Second)
	revoked := false
	for time.Now().Before(deadline) {
		b.mu.Lock()
		revoked = b.connections[c.ID].info.State == "disconnected"
		b.mu.Unlock()
		if revoked {
			break
		}
		time.Sleep(time.Millisecond)
	}
	close(release)
	if !revoked {
		t.Fatal("disconnect was blocked behind the mutation")
	}
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("approval did not exit")
	}
	if err = <-disconnected; err != nil {
		t.Fatal(err)
	}
	if readPage(t, k, e, page)["page"].(map[string]any)["title"] != "before" {
		t.Fatal("disconnected mutation committed")
	}
}

func TestMCPCancellationInterruptsQueuedProposal(t *testing.T) {
	k, e := testKernel(t)
	page := createPage(t, k, e, "Cancellation")
	b := NewProviderBridge(k, e)
	defer b.Close()
	c, err := b.Connect(context.Background(), "mcp", []string{page}, []string{"proposeChanges"})
	if err != nil {
		t.Fatal(err)
	}
	handler := b.MCPHandler()
	k.writer <- struct{}{}
	released := false
	defer func() {
		if !released {
			<-k.writer
		}
	}()
	call := func(body []byte) *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodPost, "http://127.0.0.1/mcp", strings.NewReader(string(body)))
		r.Header.Set("Authorization", "Bearer "+c.Token)
		r.Header.Set("Trove-Connection-Id", c.ID)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w
	}
	done := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		done <- call(marshal(map[string]any{"jsonrpc": "2.0", "id": 7, "method": "tools/call", "params": map[string]any{"name": "proposeChanges", "arguments": map[string]any{"idempotencyKey": "cancelled", "commands": []any{map[string]any{"kind": "page.update", "pageId": page, "title": "never"}}, "expectedRevisions": []contracts.ServiceExpectedRevision{required("page", page, "1")}}}}))
	}()
	deadline := time.Now().Add(time.Second)
	for len(k.pending) == 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	w := call([]byte(`{"jsonrpc":"2.0","method":"notifications/cancelled","params":{"requestId":7}}`))
	if w.Code != 202 {
		t.Fatal(w.Code)
	}
	select {
	case result := <-done:
		if !strings.Contains(result.Body.String(), `"isError":true`) {
			t.Fatal(result.Body.String())
		}
	case <-time.After(time.Second):
		t.Fatal("MCP cancellation did not interrupt queued work")
	}
	<-k.writer
	released = true
}
func TestProviderRestartRecoversCommittedReceipt(t *testing.T) {
	k, e := testKernel(t)
	page := createPage(t, k, e, "before")
	b := NewProviderBridge(k, e)
	c, err := b.Connect(context.Background(), "mcp", []string{page}, []string{"proposeChanges"})
	if err != nil {
		t.Fatal(err)
	}
	raw, err := b.Call(context.Background(), c.ID, c.Token, "proposeChanges", map[string]any{"idempotencyKey": "receipt", "commands": []any{map[string]any{"kind": "page.update", "pageId": page, "title": "committed"}}, "expectedRevisions": []contracts.ServiceExpectedRevision{required("page", page, "1")}})
	if err != nil {
		t.Fatal(err)
	}
	var p struct {
		Result ProviderProposal `json:"result"`
	}
	json.Unmarshal(raw, &p)
	if _, err = b.Approve(context.Background(), p.Result.ID); err != nil {
		t.Fatal(err)
	}
	// Simulate a crash after canonical commit but before recording UI status.
	if _, err = k.writeDB.Exec("UPDATE provider_proposals SET state='pending',result_json=NULL WHERE id=?", p.Result.ID); err != nil {
		t.Fatal(err)
	}
	b.Close()
	path := k.Path()
	k.Close()
	reopened, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	fresh := NewProviderBridge(reopened, reopened.OwnerContext())
	proposals, err := fresh.Proposals(context.Background())
	if err != nil || len(proposals) != 1 || proposals[0].State != "applied" || len(proposals[0].Result) == 0 {
		t.Fatal(proposals, err)
	}
	if readPage(t, reopened, reopened.OwnerContext(), page)["page"].(map[string]any)["revision"] != "2" {
		t.Fatal("restart replayed committed effects")
	}
}
