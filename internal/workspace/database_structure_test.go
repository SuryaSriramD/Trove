package workspace

import (
	"testing"
	"trove.local/phase0/contracts"
)

func TestTimelineStructurePreservesScopeAndSavedConfiguration(t *testing.T) {
	k, e := testKernel(t)
	f := newDatabase(t, k, e)
	a, ap := newRecord(t, k, e, f, "A")
	b, bp := newRecord(t, k, e, f, "B")
	testOK(t, dbBatch(t, k, e, []any{map[string]any{"kind": "record.configure", "recordId": a, "parentRecordId": b, "dependencyRecordIds": []string{b}}}, "record", a, "record", b, "page", ap, "collection", f.collection))
	request := map[string]any{"contractVersion": 1, "requestId": NewID(), "workspaceId": k.ID(), "collectionId": f.collection}
	all := testOK(t, testDispatch(t, k, e, "databaseStructure", request))["structure"].(map[string]any)
	if all[a].(map[string]any)["dependencyRecordIds"].([]any)[0] != b {
		t.Fatal(all)
	}
	scoped := k.Grant(e.Actor, "user", contracts.DomainScope{Value: contracts.DomainScopeVariant3{Kind: "pages", WorkspaceId: contracts.DomainId(k.ID()), PageIds: []contracts.DomainId{contracts.DomainId(f.page), contracts.DomainId(ap)}}})
	limited := testOK(t, testDispatch(t, k, scoped, "databaseStructure", request))["structure"].(map[string]any)
	if len(limited) != 1 || limited[a].(map[string]any)["parentRecordId"] != nil || len(limited[a].(map[string]any)["dependencyRecordIds"].([]any)) != 0 {
		t.Fatal("out-of-scope structure leaked", limited, bp)
	}
	view := NewID()
	query := emptyQuery(f.title, f.date)
	query["groupByPropertyId"] = f.status
	query["sorts"] = []any{map[string]any{"propertyId": f.title, "direction": "desc"}}
	testOK(t, dbBatch(t, k, e, []any{map[string]any{"kind": "view.save", "viewId": view, "collectionId": f.collection, "owningPageId": f.page, "name": "Timeline", "type": "timeline", "schemaVersion": 1, "config": map[string]any{"datePropertyId": f.date, "scale": "quarter", "showDependencies": false}, "query": query}}, "view", view, "collection", f.collection, "page", f.page))
	path := k.Path()
	k.Close()
	reopened, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	owner := reopened.OwnerContext()
	result := testOK(t, queryDB(t, reopened, owner, f.collection, view, 200, ""))
	if result["recordIds"].([]any)[0] != b {
		t.Fatal("saved ordering lost", result)
	}
	var config string
	reopened.db.QueryRow("SELECT config_json FROM views WHERE id=?", view).Scan(&config)
	if config != string(marshal(map[string]any{"datePropertyId": f.date, "scale": "quarter", "showDependencies": false})) {
		t.Fatal(config)
	}
}
