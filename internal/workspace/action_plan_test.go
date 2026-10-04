package workspace

import (
	"context"
	"encoding/json"
	"testing"
	"trove.local/phase0/contracts"
)

func TestActionPlanAllocatesIDsAndResolvesNamesWithoutGrantExpansion(t *testing.T) {
	k, e := testKernel(t)
	ctx := context.Background()
	parent := createPage(t, k, e, "Project Atlas")
	restricted := k.Grant(contracts.ServiceActor{Kind: "external_client", ActorId: contracts.DomainId(NewID())}, "external", contracts.DomainScope{Value: contracts.DomainScopeVariant3{Kind: "pages", WorkspaceId: contracts.DomainId(k.ID()), PageIds: []contracts.DomainId{contracts.DomainId(parent)}}})
	plan := ActionPlan{Version: 1, Actions: []PlannedAction{{Kind: "create_page", Destination: "Project Atlas", Title: "Review notes", Body: "# Decisions\nKeep the original files."}}}
	resolved, err := k.ResolveActionPlan(ctx, restricted, plan, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(resolved.PageIDs) != 1 || len(resolved.Changes) != 1 || resolved.Changes[0].Destination != "Project Atlas" {
		t.Fatal(resolved)
	}
	var n int
	k.db.QueryRow("SELECT count(*) FROM pages WHERE title='Review notes'").Scan(&n)
	if n != 0 {
		t.Fatal("planning wrote content")
	}
	req := testRequest(k, restricted, resolved.Commands, resolved.Guards...)
	var typed contracts.ServiceBatchRequest
	json.Unmarshal(marshal(req), &typed)
	testCode(t, testDispatch(t, k, restricted, "batch", req), "APPROVAL_REQUIRED")
	approval, err := k.Approve(ctx, e, typed, restricted)
	if err != nil {
		t.Fatal(err)
	}
	a := contracts.DomainId(approval)
	typed.Mutation.ApprovalId = &a
	req["mutation"].(map[string]any)["approvalId"] = approval
	testOK(t, testDispatch(t, k, restricted, "batch", req))
	var gotParent string
	k.db.QueryRow("SELECT parent_page_id FROM pages WHERE id=?", resolved.PageIDs[0]).Scan(&gotParent)
	if gotParent != parent {
		t.Fatal(gotParent)
	}
	if err = k.authorizePage(ctx, k.db, restricted, resolved.PageIDs[0]); err == nil {
		t.Fatal("creation silently widened read scope")
	}
	if _, err = k.ResolveActionPlan(ctx, restricted, ActionPlan{Version: 1, Actions: []PlannedAction{{Kind: "create_page", Destination: "@workspace", Title: "Unscoped"}}}, nil); err == nil {
		t.Fatal("restricted root creation allowed")
	}
}
func TestActionPlanRejectsAmbiguousStaleAndInvalidValues(t *testing.T) {
	k, e := testKernel(t)
	ctx := context.Background()
	page := createPage(t, k, e, "Duplicate")
	createPage(t, k, e, "Duplicate")
	if _, err := k.ResolveActionPlan(ctx, e, ActionPlan{Version: 1, Actions: []PlannedAction{{Kind: "update_page", Target: "Duplicate", Title: "Guess"}}}, nil); err == nil {
		t.Fatal("ambiguous title guessed")
	}
	if _, err := k.ResolveActionPlan(ctx, e, ActionPlan{Version: 1, Actions: []PlannedAction{{Kind: "create_page", Destination: "@workspace", Title: "Child"}}}, []contracts.ServiceExpectedRevision{required("page", page, "999")}); err == nil {
		t.Fatal("stale source accepted")
	}
	f := newDatabase(t, k, e)
	resolved, err := k.ResolveActionPlan(ctx, e, ActionPlan{Version: 1, Actions: []PlannedAction{{Kind: "create_record", Destination: "Project", Title: "New record", Body: "Original body"}}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	testOK(t, testDispatch(t, k, e, "batch", testRequest(k, e, resolved.Commands, resolved.Guards...)))
	bad := ActionPlan{Version: 1, Actions: []PlannedAction{{Kind: "update_record", Target: "New record", Property: "When", Value: "2026-02-31"}}}
	if _, err = k.ResolveActionPlan(ctx, e, bad, nil); err == nil {
		t.Fatal("invalid calendar date accepted")
	}
	good := ActionPlan{Version: 1, Actions: []PlannedAction{{Kind: "update_record", Target: "New record", Property: "Status", Value: "Done"}}}
	result, err := k.ResolveActionPlan(ctx, e, good, nil)
	if err != nil {
		t.Fatal(err)
	}
	testOK(t, testDispatch(t, k, e, "batch", testRequest(k, e, result.Commands, result.Guards...)))
	var stored string
	k.db.QueryRow("SELECT value_json FROM property_values WHERE property_id=?", f.status).Scan(&stored)
	if stored != string(marshal(f.done)) {
		t.Fatal("human option label was not resolved", stored)
	}
	_ = f
}

func TestActionPlanUsesTaskBindingsAndExplainsDatabaseCreation(t *testing.T) {
	k, e := testKernel(t)
	ctx := context.Background()
	f := ensureTestTasks(t, k, e)
	// Names are presentation; persisted task bindings remain authoritative.
	if _, err := k.writeDB.Exec("UPDATE properties SET name=CASE id WHEN ? THEN 'Progress' ELSE 'Deadline' END WHERE id IN (?,?)", f.status, f.status, f.date); err != nil {
		t.Fatal(err)
	}
	plan := ActionPlan{Version: 1, Actions: []PlannedAction{{Kind: "create_task", Destination: "Tasks", Title: "Review archives", Due: "2026-10-21"}}}
	resolved, err := k.ResolveActionPlan(ctx, e, plan, nil)
	if err != nil {
		t.Fatal(err)
	}
	testOK(t, testDispatch(t, k, e, "batch", testRequest(k, e, resolved.Commands, resolved.Guards...)))
	plan.Actions = []PlannedAction{{Kind: "update_task", Target: "Review archives", Status: "Done", Due: "2026-10-22"}}
	resolved, err = k.ResolveActionPlan(ctx, e, plan, nil)
	if err != nil {
		t.Fatal(err)
	}
	if resolved.Changes[0].Label != "Set Progress" || resolved.Changes[0].Before != "To do" || resolved.Changes[0].After != "Done" {
		t.Fatal("unreadable status review", resolved.Changes)
	}
	testOK(t, testDispatch(t, k, e, "batch", testRequest(k, e, resolved.Commands, resolved.Guards...)))
	var completed bool
	if err = k.db.QueryRow("SELECT completed FROM task_state WHERE record_id=(SELECT id FROM records WHERE page_id=?)", resolved.PageIDs[0]).Scan(&completed); err != nil || !completed {
		t.Fatal("bound status did not complete task", err)
	}
	plan.Actions = []PlannedAction{{Kind: "create_database", Destination: "@workspace", Title: "Inventory", Properties: []PlannedProperty{{Name: "Quantity", Type: "number"}}}}
	resolved, err = k.ResolveActionPlan(ctx, e, plan, nil)
	if err != nil {
		t.Fatal(err)
	}
	if resolved.Changes[0].After != "Table view with Name (title)\nProperty: Quantity (number)" {
		t.Fatal(resolved.Changes)
	}
	plan.Actions = []PlannedAction{{Kind: "create_page", Destination: "@workspace", Title: "   "}}
	if _, err = k.ResolveActionPlan(ctx, e, plan, nil); err == nil {
		t.Fatal("blank title accepted")
	}
}
