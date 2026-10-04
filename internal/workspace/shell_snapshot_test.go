package workspace

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"trove.local/phase0/contracts"
)

func TestShellSnapshotOmitsBodiesAndCellsButPreservesTaskGuards(t *testing.T) {
	k, e := testKernel(t)
	f := newDatabase(t, k, e)
	record, page := newRecord(t, k, e, f, "Task row")
	block, text := NewID(), NewID()
	testOK(t, dbBatch(t, k, e, []any{map[string]any{"kind": "collection.addProperty", "collectionId": f.collection, "propertyId": text, "name": "Notes", "type": "text", "schemaVersion": 1, "config": map[string]any{}}}, "collection", f.collection, "property", text))
	testOK(t, setValue(t, k, e, f, record, page, text, "text", []any{map[string]any{"text": strings.Repeat("CELL PAYLOAD ", 100)}}))
	testOK(t, dbBatch(t, k, e, []any{map[string]any{"kind": "block.insert", "block": map[string]any{"blockId": block, "pageId": page, "type": "paragraph", "schemaVersion": 1, "data": map[string]any{"richText": []any{map[string]any{"text": "BODY MUST LOAD ON DEMAND"}}}}}, map[string]any{"kind": "task.promote", "recordId": record, "statusPropertyId": f.status}}, "page", page, "record", record, "collection", f.collection, "block", block, "property", f.status))
	request := map[string]any{"contractVersion": 1, "requestId": NewID(), "workspaceId": k.ID(), "contentMode": "shell"}
	shell := testOK(t, testDispatch(t, k, e, "workspace.snapshot", request))
	if len(shell["blocks"].([]any)) != 0 || strings.Contains(string(marshal(shell)), "CELL PAYLOAD") || strings.Contains(string(marshal(shell)), "BODY MUST LOAD") {
		t.Fatal("shell preloaded bodies or cells")
	}
	r := shell["records"].([]any)[0].(map[string]any)
	if r["id"] != record || r["revision"] == nil || r["pageId"] != page {
		t.Fatal("canonical identities missing", r)
	}
	statusFound := false
	for _, raw := range r["values"].([]any) {
		value := raw.(map[string]any)
		if value["propertyId"] == f.status && value["value"] == f.open {
			statusFound = true
		}
	}
	if !statusFound {
		t.Fatal("task status missing from shell")
	}
	delete(request, "contentMode")
	full := testOK(t, testDispatch(t, k, e, "workspace.snapshot", request))
	if !strings.Contains(string(marshal(full)), "CELL PAYLOAD") || len(full["blocks"].([]any)) != 1 {
		t.Fatal("legacy full snapshot changed")
	}
	read := testOK(t, testDispatch(t, k, e, "readBlock", map[string]any{"contractVersion": 1, "requestId": NewID(), "workspaceId": k.ID(), "blockId": block}))
	if read["block"].(map[string]any)["id"] != block {
		t.Fatal(read)
	}
	canonical := testOK(t, testDispatch(t, k, e, "readRecord", map[string]any{"contractVersion": 1, "requestId": NewID(), "workspaceId": k.ID(), "recordId": record}))
	if !strings.Contains(string(marshal(canonical)), "CELL PAYLOAD") {
		t.Fatal("lazy record lookup dropped canonical values")
	}
	scoped := k.Grant(e.Actor, "user", contracts.DomainScope{Value: contracts.DomainScopeVariant3{Kind: "pages", WorkspaceId: contracts.DomainId(k.ID()), PageIds: []contracts.DomainId{contracts.DomainId(f.page)}}})
	testCode(t, testDispatch(t, k, scoped, "readBlock", map[string]any{"contractVersion": 1, "requestId": NewID(), "workspaceId": k.ID(), "blockId": block}), "SCOPE_DENIED")
	testCode(t, testDispatch(t, k, scoped, "readRecord", map[string]any{"contractVersion": 1, "requestId": NewID(), "workspaceId": k.ID(), "recordId": record}), "SCOPE_DENIED")
	request["contentMode"] = "shell"
	restricted := testOK(t, testDispatch(t, k, scoped, "workspace.snapshot", request))
	if len(restricted["pages"].([]any)) != 1 || len(restricted["records"].([]any)) != 0 {
		t.Fatal("shell leaked scoped identities")
	}
}

func TestRecordQueryTenThousandRows(t *testing.T) {
	k, e := testKernel(t)
	f := newDatabase(t, k, e)
	// Bulk fixture setup intentionally excludes canonical mutation/event overhead.
	tx, err := k.writeDB.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 10000; i++ {
		page, record := NewID(), NewID()
		if _, err = tx.Exec("INSERT INTO pages(id,workspace_id,title,revision,created_at,updated_at) VALUES(?,?,?,'1','2026-10-04T00:00:00Z','2026-10-04T00:00:00Z')", page, k.ID(), fmt.Sprintf("Row %05d", i)); err != nil {
			t.Fatal(err)
		}
		if _, err = tx.Exec("INSERT INTO records(id,collection_id,page_id,sort_key,revision) VALUES(?,?,?,?,'1')", record, f.collection, page, fmt.Sprintf("%012d", i)); err != nil {
			t.Fatal(err)
		}
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	started := time.Now()
	first := testOK(t, queryDB(t, k, e, f.collection, "", 200, ""))
	second := testOK(t, queryDB(t, k, e, f.collection, "", 200, first["nextCursor"].(string)))
	if len(first["records"].([]any)) != 200 || len(second["records"].([]any)) != 200 {
		t.Fatal("page hydration exceeded bounds")
	}
	seen := map[string]bool{}
	for _, result := range []map[string]any{first, second} {
		for _, id := range result["recordIds"].([]any) {
			if seen[id.(string)] {
				t.Fatal("duplicate keyset result")
			}
			seen[id.(string)] = true
		}
	}
	t.Logf("10,000 canonical records: first + second 200-row pages %s (developer smoke measurement, no release threshold)", time.Since(started))
}
