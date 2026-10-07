package workspace

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"trove.local/phase0/contracts"
)

func memoryFixture(t *testing.T, k *Kernel, e contracts.ServiceExecutionContext, source string) (string, string) {
	t.Helper()
	page := createPage(t, k, e, "Approved decision")
	id := NewID()
	r := testRequest(k, e, []any{map[string]any{"kind": "memory.propose", "memoryId": id, "pageId": page, "scope": map[string]any{"kind": "workspace", "workspaceId": k.ID()}, "memoryKind": "decision", "content": "Use concise prose because reviewers requested it.", "sources": []any{map[string]any{"pageId": source, "sourceRevision": "1"}}}}, required("page", page, "1"), required("memory", id, "0"))
	testOK(t, testDispatch(t, k, e, "batch", r))
	testOK(t, testDispatch(t, k, e, "batch", testRequest(k, e, []any{map[string]any{"kind": "memory.approve", "memoryId": id}}, required("page", page, "2"), required("memory", id, "1"))))
	return id, page
}
func TestMemoryApprovalInvalidationForgetRestore(t *testing.T) {
	k, e := testKernel(t)
	ctx := context.Background()
	source := createPage(t, k, e, "Reviewers asked for concise prose")
	id, page := memoryFixture(t, k, e, source)
	entries, err := readMemories(ctx, k, k.db, e, false)
	if err != nil || len(entries) != 1 {
		t.Fatal(entries, err)
	}
	testOK(t, testDispatch(t, k, e, "batch", testRequest(k, e, []any{map[string]any{"kind": "memory.forget", "memoryId": id}}, required("page", page, "3"), required("memory", id, "2"))))
	entries, err = readMemories(ctx, k, k.db, e, false)
	if err != nil || len(entries) != 0 {
		t.Fatal("forgotten evidence retrieved", entries, err)
	}
	testOK(t, testDispatch(t, k, e, "batch", testRequest(k, e, []any{map[string]any{"kind": "memory.approve", "memoryId": id}}, required("page", page, "4"), required("memory", id, "3"))))
	testOK(t, testDispatch(t, k, e, "batch", testRequest(k, e, []any{map[string]any{"kind": "page.update", "pageId": source, "title": "Reviewers changed their decision"}}, required("page", source, "1"))))
	var status, rev, pagerev string
	k.db.QueryRow("SELECT status,revision FROM memory_entries WHERE id=?", id).Scan(&status, &rev)
	k.db.QueryRow("SELECT revision FROM pages WHERE id=?", page).Scan(&pagerev)
	if status != "stale" {
		t.Fatal(status)
	}
	testCode(t, testDispatch(t, k, e, "batch", testRequest(k, e, []any{map[string]any{"kind": "memory.approve", "memoryId": id}}, required("page", page, pagerev), required("memory", id, rev))), "STALE_CITATION")
	testOK(t, testDispatch(t, k, e, "batch", testRequest(k, e, []any{map[string]any{"kind": "memory.update", "memoryId": id, "content": "New reviewed decision", "sources": []any{map[string]any{"pageId": source, "sourceRevision": "2"}}}}, required("page", page, pagerev), required("memory", id, rev))))
	k.db.QueryRow("SELECT status FROM memory_entries WHERE id=?", id).Scan(&status)
	if status != "proposed" {
		t.Fatal("edited memory became active without approval")
	}
}
func TestAIComparisonMemoryAndLiveAuthority(t *testing.T) {
	k, e := testKernel(t)
	ctx := context.Background()
	source := createPage(t, k, e, "Reviewers asked for concise prose")
	id, page := memoryFixture(t, k, e, source)
	req := AIRequest{Mode: "answer", Prompt: "Reviewers", UseMemory: false, ModelPackID: NewID()}
	without, err := k.PrepareAI(ctx, e, req)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(without.Prompt, "Use concise prose because") {
		t.Fatal("memory leaked into no-memory fixture")
	}
	req.UseMemory = true
	with, err := k.PrepareAI(ctx, e, req)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(with.Prompt, "Use concise prose because") {
		t.Fatal("approved memory missing")
	}
	if !strings.Contains(with.System, "Current source task/database values are authoritative") {
		t.Fatal("missing authority rule")
	}
	testOK(t, testDispatch(t, k, e, "batch", testRequest(k, e, []any{map[string]any{"kind": "memory.forget", "memoryId": id}}, required("page", page, "3"), required("memory", id, "2"))))
	finished, err := k.FinishAI(ctx, e, with.Run.ID, "Use concise prose [1].", "completed", "")
	if err != nil {
		t.Fatal(err)
	}
	if finished.State != "failed" || finished.Text != "" {
		t.Fatal("forgotten memory answer was published", finished)
	}
	next, err := k.PrepareAI(ctx, e, req)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(next.Prompt, "Use concise prose because") {
		t.Fatal("forgotten memory survived retrieval")
	}
}

func TestAIMemoryKindPersistsAndLegacyEvidenceRemainsValid(t *testing.T) {
	k, e := testKernel(t)
	ctx := context.Background()
	source := createPage(t, k, e, "Reviewers asked for concise prose")
	id, _ := memoryFixture(t, k, e, source)
	prepared, err := k.PrepareAIWithBudget(ctx, e, AIRequest{PageID: source, Mode: "answer", Prompt: "What did reviewers decide?", UseMemory: true}, wordBudget())
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := k.AIRun(ctx, e, prepared.Run.ID)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for i, evidence := range loaded.Sources {
		if evidence.MemoryID != id {
			continue
		}
		found = true
		if evidence.MemoryKind != "decision" || evidence.State != "current" {
			t.Fatal("canonical memory kind did not survive run persistence", evidence)
		}
		mismatched := evidence
		mismatched.MemoryKind = "preference"
		if err := k.aiEvidenceValid(ctx, e, mismatched); err == nil {
			t.Fatal("a different captured memory kind was accepted")
		}
		loaded.Sources[i].MemoryKind = ""
		loaded.Sources[i].Title = "Approved preference: misleading legacy display title"
	}
	if !found {
		t.Fatal("approved memory missing")
	}
	// Simulate the persisted source JSON from before memoryKind existed.
	if _, err := k.db.Exec("UPDATE ai_runs SET sources_json=? WHERE id=?", string(marshal(loaded.Sources)), loaded.ID); err != nil {
		t.Fatal(err)
	}
	legacy, err := k.AIRun(ctx, e, loaded.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, evidence := range legacy.Sources {
		if evidence.MemoryID == id && (evidence.MemoryKind != "" || evidence.State != "current") {
			t.Fatal("legacy memory kind was inferred or compatibility lost", evidence)
		}
	}
}
func TestAIValidatedCitationsAndReviewedAction(t *testing.T) {
	k, e := testKernel(t)
	ctx := context.Background()
	page := createPage(t, k, e, "The launch code is AMBER-27")
	req := AIRequest{PageID: page, Mode: "answer", Prompt: "What is the launch code?", ModelPackID: NewID()}
	p, err := k.PrepareAI(ctx, e, req)
	if err != nil {
		t.Fatal(err)
	}
	result, err := k.FinishAI(ctx, e, p.Run.ID, "The code is AMBER-27 [1]. Unsupported [99].", "completed", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Citations) != 1 || strings.Contains(result.Text, "[99]") || result.Warning == "" {
		t.Fatal(result)
	}
	raw, err := prepareAIAction(ctx, k, e, map[string]any{"runId": p.Run.ID, "kind": "append", "body": "Launch code: AMBER-27"})
	if err != nil {
		t.Fatal(err)
	}
	proposal := raw.(AIProposal)
	var n int
	k.db.QueryRow("SELECT count(*) FROM blocks WHERE page_id=?", page).Scan(&n)
	if n != 0 {
		t.Fatal("proposal mutated before approval")
	}
	denied := k.Grant(contracts.ServiceActor{Kind: "ai", ActorId: contracts.DomainId(NewID())}, "ai", e.Scope)
	defer k.RevokeGrant(string(denied.ScopeGrantId))
	if _, err = applyAIAction(ctx, k, denied, map[string]any{"proposalId": proposal.ID}); err == nil {
		t.Fatal("AI self-approved")
	}
	if _, err = applyAIAction(ctx, k, e, map[string]any{"proposalId": proposal.ID}); err != nil {
		t.Fatal(err)
	}
	if _, err = applyAIAction(ctx, k, e, map[string]any{"proposalId": proposal.ID}); err != nil {
		t.Fatal("idempotent review", err)
	}
	k.db.QueryRow("SELECT count(*) FROM blocks WHERE page_id=?", page).Scan(&n)
	if n != 1 {
		t.Fatal(n)
	}
}
func TestAIConflictCancellationAndScope(t *testing.T) {
	k, e := testKernel(t)
	ctx := context.Background()
	page := createPage(t, k, e, "Sensitive fact")
	prepared, err := k.PrepareAI(ctx, e, AIRequest{PageID: page, Mode: "summarize"})
	if err != nil {
		t.Fatal(err)
	}
	run, err := k.FinishAI(ctx, e, prepared.Run.ID, "Partial text", "cancelled", "Cancelled")
	if err != nil || run.State != "cancelled" {
		t.Fatal(run, err)
	}
	if _, err = prepareAIAction(ctx, k, e, map[string]any{"runId": run.ID, "kind": "append"}); err == nil {
		t.Fatal("cancelled action accepted")
	}
	other := createPage(t, k, e, "Other")
	scoped := k.Grant(e.Actor, "user", contracts.DomainScope{Value: contracts.DomainScopeVariant3{Kind: "pages", WorkspaceId: contracts.DomainId(k.ID()), PageIds: []contracts.DomainId{contracts.DomainId(other)}}})
	defer k.RevokeGrant(string(scoped.ScopeGrantId))
	if _, err = k.AIRun(ctx, scoped, run.ID); err == nil {
		t.Fatal("conversation leaked outside grant")
	}
	prepared, err = k.PrepareAI(ctx, e, AIRequest{PageID: page, Mode: "summarize"})
	if err != nil {
		t.Fatal(err)
	}
	run, err = k.FinishAI(ctx, e, prepared.Run.ID, "Sensitive fact [1]", "completed", "")
	if err != nil {
		t.Fatal(err)
	}
	prop, err := prepareAIAction(ctx, k, e, map[string]any{"runId": run.ID, "kind": "create_page", "title": "Draft", "body": "Reviewed draft"})
	if err != nil {
		t.Fatal(err)
	}
	testOK(t, testDispatch(t, k, e, "batch", testRequest(k, e, []any{map[string]any{"kind": "page.update", "pageId": page, "title": "Changed"}}, required("page", page, "1"))))
	if _, err = applyAIAction(ctx, k, e, map[string]any{"proposalId": prop.(AIProposal).ID}); err == nil {
		t.Fatal("stale evidence applied")
	}
	// Stored source citations report their current validity on reopening.
	persisted, err := k.AIRun(ctx, e, run.ID)
	if err != nil || persisted.Citations[0].State != "stale" {
		t.Fatal(persisted, err)
	}
	data, _ := json.Marshal(persisted)
	if len(data) == 0 {
		t.Fatal("missing conversation")
	}
}

func TestAISelectionAndStructuredPageTaskReview(t *testing.T) {
	k, e := testKernel(t)
	ctx := context.Background()
	page := createPage(t, k, e, "Planning")
	block := NewID()
	testOK(t, testDispatch(t, k, e, "batch", testRequest(k, e, []any{map[string]any{"kind": "block.insert", "block": map[string]any{"blockId": block, "pageId": page, "type": "paragraph", "schemaVersion": 1, "data": map[string]any{"richText": []any{map[string]any{"text": "A slow launch begins tomorrow."}}}}}}, required("page", page, "1"), required("block", block, "0"))))
	prepared, err := k.PrepareAI(ctx, e, AIRequest{PageID: page, BlockID: block, Selection: "slow launch", Mode: "rewrite"})
	if err != nil {
		t.Fatal(err)
	}
	run, err := k.FinishAI(ctx, e, prepared.Run.ID, "careful launch", "completed", "")
	if err != nil {
		t.Fatal(err)
	}
	raw, err := prepareAIAction(ctx, k, e, map[string]any{"runId": run.ID, "kind": "replace_block"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = applyAIAction(ctx, k, e, map[string]any{"proposalId": raw.(AIProposal).ID}); err != nil {
		t.Fatal(err)
	}
	var data string
	k.db.QueryRow("SELECT data_json FROM blocks WHERE id=?", block).Scan(&data)
	if !strings.Contains(data, "A careful launch begins tomorrow.") {
		t.Fatal(data)
	}
	for _, kind := range []string{"create_page", "create_task", "replace_page"} {
		prepared, err = k.PrepareAI(ctx, e, AIRequest{PageID: page, Mode: "outline", Prompt: "Plan"})
		if err != nil {
			t.Fatal(err)
		}
		run, err = k.FinishAI(ctx, e, prepared.Run.ID, "# Launch\n- Review\n- Ship", "completed", "")
		if err != nil {
			t.Fatal(err)
		}
		raw, err = prepareAIAction(ctx, k, e, map[string]any{"runId": run.ID, "kind": kind, "title": "Launch plan"})
		if err != nil {
			t.Fatal(kind, err)
		}
		proposal := raw.(AIProposal)
		if _, err = applyAIAction(ctx, k, e, map[string]any{"proposalId": proposal.ID}); err != nil {
			t.Fatal(kind, err)
		}
		var n int
		k.db.QueryRow("SELECT count(*) FROM blocks WHERE page_id=?", proposal.PageID).Scan(&n)
		if n != 3 {
			t.Fatal(kind, n)
		}
		if kind == "create_task" {
			k.db.QueryRow("SELECT count(*) FROM task_facets t JOIN records r ON t.record_id=r.id WHERE r.page_id=?", proposal.PageID).Scan(&n)
			if n != 1 {
				t.Fatal("created page lacks task facet")
			}
		}
	}
}

func TestAIExactQuoteAttributionAndStaleConversationContext(t *testing.T) {
	k, e := testKernel(t)
	ctx := context.Background()
	source := createPage(t, k, e, "The launch code is AMBER-27")
	prepared, err := k.PrepareAI(ctx, e, AIRequest{PageID: source, Mode: "answer", Prompt: "What is the launch code?"})
	if err != nil {
		t.Fatal(err)
	}
	run, err := k.FinishAI(ctx, e, prepared.Run.ID, "The launch code is AMBER-27.", "completed", "")
	if err != nil || len(run.Citations) != 1 || !strings.HasSuffix(run.Text, "[1]") {
		t.Fatal(run, err)
	}
	follow, err := k.PrepareAI(ctx, e, AIRequest{PageID: source, ConversationID: run.ConversationID, Mode: "answer", Prompt: "Repeat that code"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(follow.Prompt, "PREVIOUS USER QUESTION") {
		t.Fatal("live conversation context was lost")
	}
	run, err = k.FinishAI(ctx, e, follow.Run.ID, "Unrelated assertion with no source support.", "completed", "")
	if err != nil || len(run.Citations) != 0 || !strings.Contains(run.Warning, "did not cite") {
		t.Fatal("fabricated attribution", run, err)
	}
	testOK(t, testDispatch(t, k, e, "batch", testRequest(k, e, []any{map[string]any{"kind": "page.update", "pageId": source, "title": "The launch code changed"}}, required("page", source, "1"))))
	follow, err = k.PrepareAI(ctx, e, AIRequest{PageID: source, ConversationID: run.ConversationID, Mode: "answer", Prompt: "What now?"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(follow.Prompt, "PREVIOUS USER QUESTION") {
		t.Fatal("stale historical context was reused")
	}
}

func TestMemorySupersessionCannotReviveCompetingPolicy(t *testing.T) {
	k, e := testKernel(t)
	source := createPage(t, k, e, "Approved writing decision")
	first, firstPage := memoryFixture(t, k, e, source)
	second, secondPage := NewID(), createPage(t, k, e, "Replacement decision")
	testOK(t, testDispatch(t, k, e, "batch", testRequest(k, e, []any{map[string]any{"kind": "memory.propose", "memoryId": second, "pageId": secondPage, "scope": map[string]any{"kind": "workspace", "workspaceId": k.ID()}, "memoryKind": "decision", "content": "Use the updated decision.", "sources": []any{map[string]any{"pageId": source, "sourceRevision": "1"}}, "supersedesId": first}}, required("memory", second, "0"), required("page", secondPage, "1"))))
	testOK(t, testDispatch(t, k, e, "batch", testRequest(k, e, []any{map[string]any{"kind": "memory.approve", "memoryId": second}}, required("memory", second, "1"), required("page", secondPage, "2"), required("memory", first, "2"), required("page", firstPage, "3"))))
	testOK(t, testDispatch(t, k, e, "batch", testRequest(k, e, []any{map[string]any{"kind": "memory.update", "memoryId": first, "content": "Restoring an older policy"}}, required("memory", first, "3"), required("page", firstPage, "4"))))
	testCode(t, testDispatch(t, k, e, "batch", testRequest(k, e, []any{map[string]any{"kind": "memory.approve", "memoryId": first}}, required("memory", first, "4"), required("page", firstPage, "5"))), "CONFLICT")
	entries, err := readMemories(context.Background(), k, k.db, e, false)
	if err != nil || len(entries) != 1 || string(entries[0].Id) != second {
		t.Fatal(entries, err)
	}
}
