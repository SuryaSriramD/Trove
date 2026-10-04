package workspace

import (
	"context"
	"database/sql"
	"fmt"
	"testing"
)

func performanceParagraph(id, page, text string) any {
	return map[string]any{"kind": "block.insert", "block": map[string]any{"blockId": id, "pageId": page, "type": "paragraph", "schemaVersion": 1, "data": map[string]any{"richText": []any{map[string]any{"text": text}}}}}
}

// Allocation growth gives a deterministic complexity check instead of a fragile
// wall-clock threshold. Thousands of unrelated blocks must not be decoded when
// the public service creates ordinary pages or edits a title/paragraph.
func TestPhase5EditorOrdinaryEditsDoNotScaleWithUnrelatedBlocks(t *testing.T) {
	k, e := testKernel(t)
	page := createPage(t, k, e, "Measured page")
	block := NewID()
	testOK(t, dbBatch(t, k, e, []any{performanceParagraph(block, page, "initial")}, "page", page, "block", block))
	operations := []struct {
		name string
		run  func()
	}{
		{"create_page_with_paragraphs", func() {
			id, a, b := NewID(), NewID(), NewID()
			testOK(t, testDispatch(t, k, e, "batch", testRequest(k, e, []any{map[string]any{"kind": "page.create", "pageId": id, "title": "Created"}, performanceParagraph(a, id, "first"), performanceParagraph(b, id, "second")}, required("page", id, "0"), required("block", a, "0"), required("block", b, "0"))))
		}},
		{"title_edit", func() {
			testOK(t, dbBatch(t, k, e, []any{map[string]any{"kind": "page.update", "pageId": page, "title": "Edited title"}}, "page", page))
		}},
		{"paragraph_edit", func() {
			testOK(t, dbBatch(t, k, e, []any{map[string]any{"kind": "block.update", "blockId": block, "type": "paragraph", "schemaVersion": 1, "data": map[string]any{"richText": []any{map[string]any{"text": "Edited paragraph"}}}}}, "page", page, "block", block))
		}},
	}
	baseline := map[string]float64{}
	for _, operation := range operations {
		baseline[operation.name] = testing.AllocsPerRun(3, operation.run)
	}
	unrelated := createPage(t, k, e, "Unrelated fixture")
	// Only fixture preparation uses SQL; every measured mutation uses Dispatch.
	err := k.write(context.Background(), func(tx *sql.Tx) error {
		statement, err := tx.Prepare("INSERT INTO blocks(id,page_id,sort_key,type,schema_version,data_json,revision) VALUES(?,?,?,'paragraph',1,?,1)")
		if err != nil {
			return err
		}
		defer statement.Close()
		data := string(marshal(map[string]any{"richText": []any{map[string]any{"text": "Unrelated preserved content with no connection to the edited page"}}}))
		for n := 0; n < 2500; n++ {
			if _, err = statement.Exec(NewID(), unrelated, fmt.Sprintf("%012d", n+1), data); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, operation := range operations {
		after := testing.AllocsPerRun(3, operation.run)
		t.Logf("%s allocations: small=%.0f unrelated2500=%.0f", operation.name, baseline[operation.name], after)
		if after > baseline[operation.name]*2+5000 {
			t.Errorf("%s allocations grew with unrelated workspace content: %.0f -> %.0f", operation.name, baseline[operation.name], after)
		}
	}
}

func TestPhase5EditorOrdinaryBatchStillEnforcesLocksAndMixedGraphInvariants(t *testing.T) {
	k, e := testKernel(t)
	page := createPage(t, k, e, "Locked context")
	a, b := NewID(), NewID()
	testOK(t, dbBatch(t, k, e, []any{performanceParagraph(a, page, "source"), performanceParagraph(b, page, "ordinary")}, "page", page, "block", a, "block", b))
	testOK(t, testDispatch(t, k, e, "editor.setLock", advancedRequest(k, map[string]any{"pageId": page, "locked": true, "expectedPageRevision": "2", "expectedRevision": "0"})))
	testCode(t, dbBatch(t, k, e, []any{map[string]any{"kind": "page.update", "pageId": page, "title": "must remain locked"}}, "page", page), "CONFLICT")
	testCode(t, dbBatch(t, k, e, []any{map[string]any{"kind": "block.update", "blockId": a, "type": "paragraph", "schemaVersion": 1, "data": map[string]any{"richText": []any{}}}}, "page", page, "block", a), "CONFLICT")
	lockedInsert := NewID()
	testCode(t, dbBatch(t, k, e, []any{performanceParagraph(lockedInsert, page, "locked insert")}, "page", page, "block", lockedInsert), "CONFLICT")
	testOK(t, testDispatch(t, k, e, "editor.setLock", advancedRequest(k, map[string]any{"pageId": page, "locked": false, "expectedPageRevision": "2", "expectedRevision": "1"})))
	synced := NewID()
	testOK(t, dbBatch(t, k, e, []any{map[string]any{"kind": "block.insert", "block": map[string]any{"blockId": synced, "pageId": page, "type": "synced_block", "schemaVersion": 1, "data": map[string]any{"contentSourceId": a}}}}, "page", page, "block", synced))
	ordinary := map[string]any{"kind": "block.update", "blockId": b, "type": "paragraph", "schemaVersion": 1, "data": map[string]any{"richText": []any{map[string]any{"text": "Must roll back"}}}}
	testCode(t, dbBatch(t, k, e, []any{ordinary, map[string]any{"kind": "block.update", "blockId": a, "type": "synced_block", "schemaVersion": 1, "data": map[string]any{"contentSourceId": synced}}}, "page", page, "block", a, "block", b), "INVALID_REQUEST")
	foreign := createPage(t, k, e, "Foreign page")
	child := NewID()
	testOK(t, dbBatch(t, k, e, []any{performanceParagraph(child, foreign, "owned elsewhere")}, "page", foreign, "block", child))
	layout := NewID()
	testCode(t, dbBatch(t, k, e, []any{ordinary, map[string]any{"kind": "block.insert", "block": map[string]any{"blockId": layout, "pageId": page, "type": "columns", "schemaVersion": 1, "data": map[string]any{"columns": []any{map[string]any{"id": NewID(), "widthRatio": 1, "childBlockIds": []any{child}}}}}}}, "page", page, "block", b, "block", layout), "INVALID_REQUEST")
	missing := NewID()
	testCode(t, dbBatch(t, k, e, []any{ordinary, map[string]any{"kind": "block.update", "blockId": a, "type": "synced_block", "schemaVersion": 1, "data": map[string]any{"contentSourceId": missing}}}, "page", page, "block", a, "block", b), "INVALID_REQUEST")
	snapshot := readPage(t, k, e, page)
	if snapshot["page"].(map[string]any)["revision"] != "3" {
		t.Fatal("rejected mixed batch mutated page", snapshot)
	}
	// The original synced edge is still valid after a normal source-text edit.
	testOK(t, dbBatch(t, k, e, []any{map[string]any{"kind": "block.update", "blockId": a, "type": "paragraph", "schemaVersion": 1, "data": map[string]any{"richText": []any{map[string]any{"text": "Updated canonical source"}}}}}, "page", page, "block", a))
	// A plain paragraph may own a synced descendant. Moving that plain ancestor
	// can introduce a reference cycle and must still use full graph validation.
	ancestor, descendant := NewID(), NewID()
	testOK(t, dbBatch(t, k, e, []any{performanceParagraph(ancestor, page, "ordinary ancestor"), map[string]any{"kind": "block.insert", "block": map[string]any{"blockId": descendant, "pageId": page, "parentBlockId": ancestor, "type": "synced_block", "schemaVersion": 1, "data": map[string]any{"contentSourceId": a}}}}, "page", page, "block", ancestor, "block", descendant))
	testCode(t, dbBatch(t, k, e, []any{map[string]any{"kind": "block.move", "blockId": ancestor, "pageId": page, "parentBlockId": a}}, "page", page, "block", ancestor, "block", descendant, "block", a), "INVALID_REQUEST")
}
