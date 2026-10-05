package workspace

import (
	"context"
	"fmt"
	"testing"
	"time"

	"trove.local/phase0/contracts"
)

func TestDerivedMemoReadsRecheckGrantAndScope(t *testing.T) {
	for _, change := range []string{"revoked", "policy", "caller_scope", "record_page", "deleted", "project_membership"} {
		t.Run(change, func(t *testing.T) {
			k, owner := testKernel(t)
			f := newDatabase(t, k, owner)
			record, page := newRecord(t, k, owner, f, "Visible")
			other := createPage(t, k, owner, "Private")
			grant := k.Grant(owner.Actor, "user", contracts.DomainScope{Value: contracts.DomainScopeVariant3{Kind: "pages", WorkspaceId: contracts.DomainId(k.ID()), PageIds: []contracts.DomainId{contracts.DomainId(f.page), contracts.DomainId(page)}}})
			project := ""
			if change == "project_membership" {
				project = NewID()
				testOK(t, dbBatch(t, k, owner, []any{map[string]any{"kind": "container.create", "containerId": project, "owningPageId": f.page, "label": "Project", "containerKind": "project", "parentProjectId": nil}}, "container", project, "page", f.page))
				testOK(t, dbBatch(t, k, owner, []any{map[string]any{"kind": "project.setScope", "projectId": project, "memberPageIds": []any{page}, "pinnedPageIds": []any{}, "excludedPageIds": []any{}, "inheritAncestorPreferences": false, "inheritWorkspacePreferences": false}}, "container", project))
				grant = k.Grant(owner.Actor, "user", contracts.DomainScope{Value: contracts.DomainScopeVariant2{Kind: "project", WorkspaceId: contracts.DomainId(k.ID()), ProjectId: contracts.DomainId(project)}})
			}
			// The production evaluator uses a read snapshot. A writable test-only
			// transaction lets us prove that scope/deletion is checked even on memo hits.
			pool := k.writeDB
			if change == "revoked" {
				pool = k.db // allow RevokeGrant's job-authorization write to finish.
			}
			tx, err := pool.BeginTx(context.Background(), nil)
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback()
			d := newDerivedEvaluator(context.Background(), k, tx, grant, time.Now(), "UTC")
			if value, err := d.value(record, f.title); err != nil || value.Value != "Visible" {
				t.Fatal(value, err)
			}
			// Exercise both the evaluator's own memo and the immutable pagination memo.
			d.baseCache, d.cache = d.cache, map[string]DerivedValue{}
			switch change {
			case "revoked":
				k.RevokeGrant(string(grant.ScopeGrantId))
			case "policy":
				k.mu.Lock()
				updated := k.grants[string(grant.ScopeGrantId)]
				updated.PolicyRevision = "2"
				k.grants[string(grant.ScopeGrantId)] = updated
				k.mu.Unlock()
			case "caller_scope":
				grant.Scope.Value.(contracts.DomainScopeVariant3).PageIds[1] = contracts.DomainId(other)
				if d.execution.Scope.Value.(contracts.DomainScopeVariant3).PageIds[1] != contracts.DomainId(page) {
					t.Fatal("caller mutation changed the evaluator's private context")
				}
			case "record_page":
				_, err = tx.Exec("UPDATE records SET page_id=? WHERE id=?", other, record)
			case "deleted":
				_, err = tx.Exec("UPDATE pages SET deleted_at='2026-10-05T00:00:00Z' WHERE id=?", page)
			case "project_membership":
				_, err = tx.Exec("DELETE FROM project_pages WHERE project_id=? AND page_id=?", project, page)
			}
			if err != nil {
				t.Fatal(err)
			}
			if value, err := d.value(record, f.title); err == nil || value.Value != nil {
				t.Fatalf("cached value survived %s: %#v %v", change, value, err)
			}
		})
	}
}

func TestDerivedProjectionMixedFilterAndFormulaEdit(t *testing.T) {
	k, e := testKernel(t)
	f := newDatabase(t, k, e)
	number, formula := NewID(), NewID()
	for _, p := range []struct {
		id, kind string
		config   any
	}{
		{number, "number", map[string]any{"format": "decimal"}},
		{formula, "formula", derivedConfig("formula", map[string]any{"translatedExpression": fmt.Sprintf("prop(%q) * 2", number)})},
	} {
		testOK(t, dbBatch(t, k, e, []any{map[string]any{"kind": "collection.addProperty", "collectionId": f.collection, "propertyId": p.id, "name": p.kind, "type": p.kind, "schemaVersion": 1, "config": p.config}}, "collection", f.collection, "property", p.id))
	}
	ids := []string{}
	for i, title := range []string{"keep first", "skip second", "keep third"} {
		r, page := newRecord(t, k, e, f, title)
		ids = append(ids, r)
		testOK(t, setValue(t, k, e, f, r, page, number, "number", map[string]any{"decimal": fmt.Sprint(i + 2), "format": "decimal"}))
		testOK(t, setValue(t, k, e, f, r, page, f.status, "status", f.open))
	}
	view := NewID()
	query := emptyQuery(f.title, formula)
	query["filter"] = map[string]any{"op": "and", "terms": []any{
		map[string]any{"propertyId": formula, "op": "gte", "value": map[string]any{"decimal": "4", "format": "decimal"}},
		map[string]any{"propertyId": f.title, "op": "contains", "value": "keep"},
		map[string]any{"propertyId": f.status, "op": "eq", "value": f.open},
	}}
	query["sorts"] = []any{map[string]any{"propertyId": number, "direction": "desc"}}
	testOK(t, dbBatch(t, k, e, []any{map[string]any{"kind": "view.save", "viewId": view, "collectionId": f.collection, "owningPageId": f.page, "name": "Mixed", "type": "table", "schemaVersion": 1, "config": map[string]any{"columnWidths": []any{}}, "query": query}}, "view", view, "collection", f.collection, "page", f.page))
	first := testOK(t, queryDB(t, k, e, f.collection, view, 1, ""))
	cursor := first["nextCursor"].(string)
	second := testOK(t, queryDB(t, k, e, f.collection, view, 1, cursor))
	if first["recordIds"].([]any)[0] != ids[2] || second["recordIds"].([]any)[0] != ids[0] || second["nextCursor"] != nil {
		t.Fatal(first, second)
	}
	// A new transaction must use the changed definition, not a cached schema.
	testOK(t, dbBatch(t, k, e, []any{map[string]any{"kind": "property.setFormula", "propertyId": formula, "expression": fmt.Sprintf("prop(%q)", number), "evaluatorVersion": 2}}, "property", formula, "property", number, "collection", f.collection, "page", f.page))
	testCode(t, queryDB(t, k, e, f.collection, view, 1, cursor), "CONFLICT")
	after := testOK(t, queryDB(t, k, e, f.collection, view, 200, ""))
	if got := after["recordIds"].([]any); len(got) != 1 || got[0] != ids[2] {
		t.Fatal(after)
	}
}
