package workspace

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"trove.local/phase0/contracts"
)

func testKernel(t *testing.T) (*Kernel, contracts.ServiceExecutionContext) {
	t.Helper()
	k, err := Create(filepath.Join(t.TempDir(), "workspace"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { k.Close() })
	return k, k.OwnerContext()
}
func testRequest(k *Kernel, e contracts.ServiceExecutionContext, commands []any, guards ...contracts.ServiceExpectedRevision) map[string]any {
	return map[string]any{"contractVersion": 1, "requestId": NewID(), "workspaceId": k.ID(), "mutation": map[string]any{"scopeGrantId": e.ScopeGrantId, "idempotencyKey": NewID(), "expectedRevisions": guards}, "commands": commands}
}
func testDispatch(t *testing.T, k *Kernel, e contracts.ServiceExecutionContext, method string, r map[string]any) map[string]any {
	t.Helper()
	raw := k.Dispatch(context.Background(), e, method, marshal(r))
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	return out
}
func testOK(t *testing.T, r map[string]any) map[string]any {
	t.Helper()
	if r["ok"] != true {
		t.Fatalf("expected success: %s", marshal(r))
	}
	return r["result"].(map[string]any)
}
func testCode(t *testing.T, r map[string]any, code string) {
	t.Helper()
	if r["ok"] != false || r["error"].(map[string]any)["code"] != code {
		t.Fatalf("expected %s: %s", code, marshal(r))
	}
}
func createPage(t *testing.T, k *Kernel, e contracts.ServiceExecutionContext, title string) string {
	t.Helper()
	id := NewID()
	r := testRequest(k, e, []any{map[string]any{"kind": "page.create", "pageId": id, "title": title}}, required("page", id, "0"))
	testOK(t, testDispatch(t, k, e, "batch", r))
	return id
}
func readPage(t *testing.T, k *Kernel, e contracts.ServiceExecutionContext, id string) map[string]any {
	t.Helper()
	return testOK(t, testDispatch(t, k, e, "readPage", map[string]any{"contractVersion": 1, "requestId": NewID(), "workspaceId": k.ID(), "pageId": id}))
}
func TestWorkspaceRestartAndExclusiveOwnership(t *testing.T) {
	k, e := testKernel(t)
	id := createPage(t, k, e, "नमस्ते 世界 مرحبا 👩🏽‍💻")
	if other, err := Open(k.Path()); err == nil {
		other.Close()
		t.Fatal("second owner acquired workspace")
	}
	path, wid := k.Path(), k.ID()
	k.Close()
	reopened, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	if reopened.ID() != wid {
		t.Fatal("workspace identity changed")
	}
	p := readPage(t, reopened, reopened.OwnerContext(), id)
	if p["page"].(map[string]any)["title"] != "नमस्ते 世界 مرحبا 👩🏽‍💻" {
		t.Fatal(p)
	}
	var fk int
	var wal string
	reopened.db.QueryRow("PRAGMA foreign_keys").Scan(&fk)
	reopened.db.QueryRow("PRAGMA journal_mode").Scan(&wal)
	if fk != 1 || wal != "wal" {
		t.Fatal(fk, wal)
	}
}
func TestAtomicBatchRollbackAndConflict(t *testing.T) {
	k, e := testKernel(t)
	id := createPage(t, k, e, "original")
	unknown := NewID()
	r := testRequest(k, e, []any{map[string]any{"kind": "page.update", "pageId": id, "title": "should rollback"}, map[string]any{"kind": "page.move", "pageId": id, "parentPageId": unknown}}, required("page", id, "1"), required("page", unknown, "0"))
	testCode(t, testDispatch(t, k, e, "batch", r), "INVALID_REQUEST")
	p := readPage(t, k, e, id)["page"].(map[string]any)
	if p["title"] != "original" || p["revision"] != "1" {
		t.Fatal(p)
	}
	r = testRequest(k, e, []any{map[string]any{"kind": "page.update", "pageId": id, "title": "new"}}, required("page", id, "1"))
	testOK(t, testDispatch(t, k, e, "batch", r))
	r["mutation"].(map[string]any)["idempotencyKey"] = NewID()
	testCode(t, testDispatch(t, k, e, "batch", r), "CONFLICT")
}
func TestIdempotencyAndCommittedEvents(t *testing.T) {
	k, e := testKernel(t)
	ch, stop := k.Subscribe()
	defer stop()
	id := NewID()
	r := testRequest(k, e, []any{map[string]any{"kind": "page.create", "pageId": id, "title": "one"}}, required("page", id, "0"))
	first := testOK(t, testDispatch(t, k, e, "batch", r))
	event := <-ch
	if err := contracts.Validate("service", "CommittedChangeEvent", event); err != nil {
		t.Fatal(err)
	}
	r["requestId"] = NewID()
	second := testOK(t, testDispatch(t, k, e, "batch", r))
	if second["replayed"] != true || first["transactionId"] != second["transactionId"] {
		t.Fatal(second)
	}
	r["commands"].([]any)[0].(map[string]any)["title"] = "different"
	testCode(t, testDispatch(t, k, e, "batch", r), "IDEMPOTENCY_CONFLICT")
	var count int
	k.db.QueryRow("SELECT count(*) FROM committed_events").Scan(&count)
	if count != 1 {
		t.Fatal(count)
	}
}
func TestScopeAndForgedContext(t *testing.T) {
	k, e := testKernel(t)
	id := createPage(t, k, e, "allowed")
	hidden := createPage(t, k, e, "secret")
	scoped := k.Grant(e.Actor, "user", contracts.DomainScope{Value: contracts.DomainScopeVariant3{Kind: "pages", WorkspaceId: contracts.DomainId(k.ID()), PageIds: []contracts.DomainId{contracts.DomainId(id)}}})
	r := testRequest(k, scoped, []any{map[string]any{"kind": "page.update", "pageId": hidden, "title": "leaked"}}, required("page", hidden, "1"))
	testCode(t, testDispatch(t, k, scoped, "batch", r), "SCOPE_DENIED")
	forged := scoped
	forged.Scope = e.Scope
	testCode(t, testDispatch(t, k, forged, "batch", r), "SCOPE_DENIED")
	k.RevokeGrant(string(scoped.ScopeGrantId))
	testCode(t, testDispatch(t, k, scoped, "batch", r), "SCOPE_DENIED")
}
func TestBlockRegistryRoundtripAndHistory(t *testing.T) {
	k, e := testKernel(t)
	page := createPage(t, k, e, "blocks")
	id := NewID()
	data := map[string]any{"richText": []any{map[string]any{"text": "Hello 世界", "marks": []any{"bold"}}}}
	r := testRequest(k, e, []any{map[string]any{"kind": "block.insert", "block": map[string]any{"blockId": id, "pageId": page, "type": "paragraph", "schemaVersion": 1, "data": data}}}, required("page", page, "1"), required("block", id, "0"))
	testOK(t, testDispatch(t, k, e, "batch", r))
	p := readPage(t, k, e, page)
	if len(p["blocks"].([]any)) != 1 || p["page"].(map[string]any)["revision"] != "2" {
		t.Fatal(p)
	}
	var count int
	k.db.QueryRow("SELECT count(*) FROM page_snapshots WHERE page_id=?", page).Scan(&count)
	if count != 2 {
		t.Fatal(count)
	}
	bad := testRequest(k, e, []any{map[string]any{"kind": "block.update", "blockId": id, "type": "paragraph", "schemaVersion": 1, "data": map[string]any{"text": false}}}, required("page", page, "2"), required("block", id, "1"))
	testCode(t, testDispatch(t, k, e, "batch", bad), "INVALID_REQUEST")
}
func TestConcurrentStaleWriters(t *testing.T) {
	k, e := testKernel(t)
	id := createPage(t, k, e, "race")
	var wg sync.WaitGroup
	responses := make(chan map[string]any, 2)
	for n := 0; n < 2; n++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r := testRequest(k, e, []any{map[string]any{"kind": "page.update", "pageId": id, "title": NewID()}}, required("page", id, "1"))
			responses <- testDispatch(t, k, e, "batch", r)
		}()
	}
	wg.Wait()
	close(responses)
	successes := 0
	for r := range responses {
		if r["ok"] == true {
			successes++
		} else {
			testCode(t, r, "CONFLICT")
		}
	}
	if successes != 1 {
		t.Fatal(successes)
	}
}
func TestCancellationIsolation(t *testing.T) {
	k, e := testKernel(t)
	cancel := NewID()
	testOK(t, testDispatch(t, k, e, "cancel", map[string]any{"contractVersion": 1, "workspaceId": k.ID(), "requestId": NewID(), "cancellationId": cancel}))
	id := NewID()
	r := testRequest(k, e, []any{map[string]any{"kind": "page.create", "pageId": id, "title": "cancelled"}}, required("page", id, "0"))
	r["cancellationId"] = cancel
	testCode(t, testDispatch(t, k, e, "batch", r), "CANCELLED")
	createPage(t, k, e, "unrelated still works")
}
func TestJobsRestartCancellationAndLeaseFencing(t *testing.T) {
	k, e := testKernel(t)
	r := map[string]any{"contractVersion": 1, "requestId": NewID(), "workspaceId": k.ID(), "kind": "BACKUP_WORKSPACE", "payload": map[string]any{"kind": "backup", "destinationHandle": NewID()}, "idempotencyKey": NewID()}
	direct, er := k.enqueueJob(context.Background(), e, r)
	if er != nil {
		t.Fatal(er)
	}
	result := map[string]any{}
	json.Unmarshal(marshal(direct), &result)
	jobID := str(result["jobId"])
	j, err := k.ClaimJob(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	oldLease := string(*j.LeaseToken)
	if err = k.ProgressJob(context.Background(), jobID, oldLease, contracts.JobsProgress{Sequence: 0, Completed: 1, Unit: "items"}); err != nil {
		t.Fatal(err)
	}
	if err = k.ProgressJob(context.Background(), jobID, oldLease, contracts.JobsProgress{Sequence: 0, Completed: 2, Unit: "items"}); err == nil {
		t.Fatal("nonmonotonic progress accepted")
	}
	path := k.Path()
	k.Close()
	k, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer k.Close()
	j, err = k.ReadJob(context.Background(), jobID)
	if err != nil || j.State != "retry_wait" {
		t.Fatal(j, err)
	}
	j, err = k.ClaimJob(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if j.Attempt != 2 || string(*j.LeaseToken) == oldLease {
		t.Fatal(j)
	}
	if err = k.FinishJob(context.Background(), jobID, oldLease, nil, false); err == nil {
		t.Fatal("old worker committed")
	}
	owner := k.OwnerContext()
	testOK(t, testDispatch(t, k, owner, "cancelJob", map[string]any{"contractVersion": 1, "requestId": NewID(), "workspaceId": k.ID(), "jobId": jobID, "expectedRevision": j.Revision}))
	if err = k.FinishJob(context.Background(), jobID, string(*j.LeaseToken), nil, false); err == nil {
		t.Fatal("late success won cancellation")
	}
	if err = k.ConfirmJobStopped(context.Background(), jobID, string(*j.LeaseToken)); err != nil {
		t.Fatal(err)
	}
	j, _ = k.ReadJob(context.Background(), jobID)
	if j.State != "cancelled" {
		t.Fatal(j)
	}
}
func TestRejectedRequestsLeaveDatabaseIntact(t *testing.T) {
	k, e := testKernel(t)
	id := NewID()
	r := testRequest(k, e, []any{map[string]any{"kind": "page.create", "pageId": id, "title": "bad", "sql": "DROP TABLE pages"}}, required("page", id, "0"))
	testCode(t, testDispatch(t, k, e, "batch", r), "INVALID_REQUEST")
	var ok string
	if err := k.db.QueryRow("PRAGMA integrity_check").Scan(&ok); err != nil || ok != "ok" {
		t.Fatal(err, ok)
	}
	if _, err := os.Stat(filepath.Join(k.Path(), "workspace.db")); err != nil {
		t.Fatal(err)
	}
}

func TestExactApprovalRequiredConsumedAndReplay(t *testing.T) {
	k, user := testKernel(t)
	id := createPage(t, k, user, "original")
	ai := k.Grant(contracts.ServiceActor{Kind: "local_ai", ActorId: contracts.DomainId(NewID())}, "ai", user.Scope)
	r := testRequest(k, ai, []any{map[string]any{"kind": "page.update", "pageId": id, "title": "approved"}}, required("page", id, "1"))
	testCode(t, testDispatch(t, k, ai, "batch", r), "APPROVAL_REQUIRED")
	req, err := contracts.DecodeBatch(marshal(r))
	if err != nil {
		t.Fatal(err)
	}
	approval, err := k.Approve(context.Background(), user, req, ai)
	if err != nil {
		t.Fatal(err)
	}
	r["mutation"].(map[string]any)["approvalId"] = approval
	testOK(t, testDispatch(t, k, ai, "batch", r))
	result := testOK(t, testDispatch(t, k, ai, "batch", r))
	if result["replayed"] != true {
		t.Fatal(result)
	}
	var state string
	k.db.QueryRow("SELECT state FROM approvals WHERE approval_id=?", approval).Scan(&state)
	if state != "consumed" {
		t.Fatal(state)
	}
}
func TestCanonicalApprovalHashEscapesAndOrdering(t *testing.T) {
	value := map[string]any{"\U0001f600": "<>&\a\u2028", "\ue000": json.Number("1e-7"), "x": json.Number("1.0"), "z": json.Number("-0")}
	b, err := canonicalJSON(value)
	if err != nil {
		t.Fatal(err)
	}
	if !json.Valid(b) {
		t.Fatalf("invalid canonical JSON: %q", b)
	}
	if string(b) != `{"x":1,"z":0,"😀":"<>&\u0007`+"\u2028"+`","":1e-7}` {
		t.Fatalf("unexpected canonical form %s", b)
	}
}

func TestProposalHashPreservesExplicitNull(t *testing.T) {
	k, e := testKernel(t)
	page := createPage(t, k, e, "nullable hash")
	base := testRequest(k, e, []any{map[string]any{"kind": "page.update", "pageId": page, "title": "same"}}, required("page", page, "1"))
	absent, err := contracts.DecodeBatch(marshal(base))
	if err != nil {
		t.Fatal(err)
	}
	base["commands"].([]any)[0].(map[string]any)["coverFileId"] = nil
	explicit, err := contracts.DecodeBatch(marshal(base))
	if err != nil {
		t.Fatal(err)
	}
	a, _ := commandHash(absent, true)
	b, _ := commandHash(explicit, true)
	if a == b {
		t.Fatal("explicit removal hashes as unchanged field")
	}
	encoded, _ := json.Marshal(explicit.Commands[0])
	var c map[string]any
	json.Unmarshal(encoded, &c)
	if value, present := c["coverFileId"]; !present || value != nil {
		t.Fatal("nullable removal lost through typed boundary")
	}
}

func TestDurableCatchupPreservesBlockTombstones(t *testing.T) {
	k, e := testKernel(t)
	page := createPage(t, k, e, "outbox")
	block := NewID()
	testOK(t, dbBatch(t, k, e, []any{map[string]any{"kind": "block.insert", "block": map[string]any{"blockId": block, "pageId": page, "type": "paragraph", "schemaVersion": 1, "data": map[string]any{"richText": []any{map[string]any{"text": "remove"}}}}}}, "page", page, "block", block))
	testOK(t, dbBatch(t, k, e, []any{map[string]any{"kind": "block.delete", "blockId": block, "includeDescendants": false}}, "page", page, "block", block))
	result := testOK(t, testDispatch(t, k, e, "readEvents", recoveryRequest(k, map[string]any{"afterCursor": "0", "limit": 100})))
	found := false
	for _, item := range result["events"].([]any) {
		for _, raw := range item.(map[string]any)["revisions"].([]any) {
			change := raw.(map[string]any)
			target := change["target"].(map[string]any)
			if target["entityId"] == block && change["deleted"] == true {
				found = true
			}
		}
	}
	if !found {
		t.Fatal("catch-up lost a committed tombstone")
	}
}

func TestArchiveTimestampSurvivesRenameAndRestore(t *testing.T) {
	k, e := testKernel(t)
	page := createPage(t, k, e, "archive")
	testOK(t, dbBatch(t, k, e, []any{map[string]any{"kind": "page.update", "pageId": page, "archived": true}}, "page", page))
	original := readPage(t, k, e, page)["page"].(map[string]any)["archivedAt"]
	testOK(t, dbBatch(t, k, e, []any{map[string]any{"kind": "page.update", "pageId": page, "title": "renamed", "archived": true}}, "page", page))
	if current := readPage(t, k, e, page)["page"].(map[string]any)["archivedAt"]; current != original {
		t.Fatal("archive timestamp changed on rename", original, current)
	}
	testOK(t, dbBatch(t, k, e, []any{map[string]any{"kind": "page.update", "pageId": page, "archived": false}}, "page", page))
	testOK(t, restoreTest(t, k, e, page, "2"))
	if current := readPage(t, k, e, page)["page"].(map[string]any)["archivedAt"]; current != original {
		t.Fatal("archive timestamp changed on restore", original, current)
	}
}
