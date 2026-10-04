package workspace

import (
	"context"
	"strings"
	"testing"
	"trove.local/phase0/contracts"
)

func advancedRequest(k *Kernel, values map[string]any) map[string]any {
	r := map[string]any{"contractVersion": 1, "requestId": NewID(), "workspaceId": k.ID()}
	for key, value := range values {
		r[key] = value
	}
	return r
}
func TestAdvancedEditorLockCASAndMutationBoundary(t *testing.T) {
	k, e := testKernel(t)
	page := createPage(t, k, e, "Locked page")
	req := advancedRequest(k, map[string]any{"pageId": page, "locked": true, "expectedPageRevision": "1", "expectedRevision": "0"})
	testOK(t, testDispatch(t, k, e, "editor.setLock", req))
	testCode(t, testDispatch(t, k, e, "editor.setLock", req), "CONFLICT")
	testCode(t, dbBatch(t, k, e, []any{map[string]any{"kind": "page.update", "pageId": page, "title": "Forbidden"}}, "page", page), "CONFLICT")
	if readPage(t, k, e, page)["page"].(map[string]any)["title"] != "Locked page" {
		t.Fatal("locked page was mutated")
	}
	outsider := k.Grant(contracts.ServiceActor{Kind: "user", ActorId: contracts.DomainId(NewID())}, "user", contracts.DomainScope{Value: contracts.DomainScopeVariant3{Kind: "pages", WorkspaceId: contracts.DomainId(k.ID()), PageIds: []contracts.DomainId{contracts.DomainId(NewID())}}})
	testCode(t, testDispatch(t, k, outsider, "editor.lock", advancedRequest(k, map[string]any{"pageId": page})), "SCOPE_DENIED")
	testOK(t, testDispatch(t, k, e, "editor.setLock", advancedRequest(k, map[string]any{"pageId": page, "locked": false, "expectedPageRevision": "1", "expectedRevision": "1"})))
	testOK(t, dbBatch(t, k, e, []any{map[string]any{"kind": "page.update", "pageId": page, "title": "Allowed"}}, "page", page))
}
func TestAdvancedEditorCycleAndLayoutOwnershipAreAtomic(t *testing.T) {
	k, e := testKernel(t)
	page := createPage(t, k, e, "Advanced")
	a, b := NewID(), NewID()
	makeBlock := func(id, kind string, data any) any {
		return map[string]any{"kind": "block.insert", "block": map[string]any{"blockId": id, "pageId": page, "type": kind, "schemaVersion": 1, "data": data}}
	}
	testCode(t, dbBatch(t, k, e, []any{makeBlock(a, "synced_block", map[string]any{"contentSourceId": b}), makeBlock(b, "synced_block", map[string]any{"contentSourceId": a})}, "page", page, "block", a, "block", b), "INVALID_REQUEST")
	if len(readPage(t, k, e, page)["blocks"].([]any)) != 0 {
		t.Fatal("cycle partially committed")
	}
	testOK(t, dbBatch(t, k, e, []any{makeBlock(a, "paragraph", map[string]any{"richText": []any{map[string]any{"text": "canonical source"}}}), makeBlock(b, "synced_block", map[string]any{"contentSourceId": a})}, "page", page, "block", a, "block", b))
	testCode(t, dbBatch(t, k, e, []any{map[string]any{"kind": "block.update", "blockId": a, "type": "synced_block", "schemaVersion": 1, "data": map[string]any{"contentSourceId": b}}}, "page", page, "block", a), "INVALID_REQUEST")
	c := NewID()
	testCode(t, dbBatch(t, k, e, []any{makeBlock(c, "columns", map[string]any{"columns": []any{map[string]any{"id": NewID(), "widthRatio": 1, "childBlockIds": []any{a}}}})}, "page", page, "block", c), "INVALID_REQUEST")
}
func TestAdvancedEditorTemplateEvidenceReferencesAndSearch(t *testing.T) {
	k, e := testKernel(t)
	page := createPage(t, k, e, "Quantum notebook")
	other := createPage(t, k, e, "Related notes")
	block := NewID()
	testOK(t, dbBatch(t, k, e, []any{map[string]any{"kind": "block.insert", "block": map[string]any{"blockId": block, "pageId": other, "type": "embed", "schemaVersion": 1, "data": map[string]any{"url": "https://example.test", "provider": "test", "sandboxPolicy": "no_scripts_no_network", "offlineText": "Quantum notebook survives offline"}}}}, "page", other, "block", block))
	refs := testOK(t, testDispatch(t, k, e, "editor.references", advancedRequest(k, map[string]any{"pageId": page})))["references"].([]any)
	if len(refs) != 1 || refs[0].(map[string]any)["kind"] != "unlinked" {
		t.Fatal(refs)
	}
	saved := testOK(t, testDispatch(t, k, e, "editor.saveTemplate", advancedRequest(k, map[string]any{"pageId": other, "name": "Offline template", "expectedPageRevision": "2"})))
	id := saved["id"]
	testOK(t, dbBatch(t, k, e, []any{map[string]any{"kind": "block.update", "blockId": block, "type": "paragraph", "schemaVersion": 1, "data": map[string]any{"richText": []any{map[string]any{"text": "changed source"}}}}}, "page", other, "block", block))
	template := testOK(t, testDispatch(t, k, e, "editor.template", advancedRequest(k, map[string]any{"templateId": id})))
	if template["blocks"].([]any)[0].(map[string]any)["type"] != "embed" {
		t.Fatal("template capture was not immutable")
	}
	if !strings.Contains(searchReadable(map[string]any{"offlineText": "offline searchable", "raw": map[string]any{"text": "secret"}}), "offline searchable") {
		t.Fatal("embed text missing from search")
	}
	if strings.Contains(searchReadable(map[string]any{"raw": map[string]any{"text": "secret"}}), "secret") {
		t.Fatal("raw payload entered search")
	}
	path := k.Path()
	k.Close()
	reopened, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	testOK(t, testDispatch(t, reopened, reopened.OwnerContext(), "editor.template", advancedRequest(reopened, map[string]any{"templateId": id})))
}
func TestAdvancedEditorTableGeometryRejectsOverlaps(t *testing.T) {
	k, e := testKernel(t)
	page := createPage(t, k, e, "Table")
	block := NewID()
	cells := []any{}
	for range 2 {
		cells = append(cells, map[string]any{"id": NewID(), "row": 0, "column": 0, "rowSpan": 1, "colSpan": 1, "richText": []any{}, "preMergeCells": []any{}})
	}
	testCode(t, dbBatch(t, k, e, []any{map[string]any{"kind": "block.insert", "block": map[string]any{"blockId": block, "pageId": page, "type": "table", "schemaVersion": 1, "data": map[string]any{"rowCount": 1, "columnCount": 2, "headerRows": 0, "headerColumns": 0, "columnWidths": []any{100, 100}, "cells": cells}}}}, "page", page, "block", block), "INVALID_REQUEST")
	if err := k.FlushSearch(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestAdvancedEditorRecoveryPreservesContentAndButtonProposalsDoNotExecute(t *testing.T) {
	k, e := testKernel(t)
	page := createPage(t, k, e, "Evidence")
	block := NewID()
	original := map[string]any{"richText": []any{map[string]any{"text": "Native H4 evidence"}}}
	testOK(t, dbBatch(t, k, e, []any{map[string]any{"kind": "block.insert", "block": map[string]any{"blockId": block, "pageId": page, "type": "heading_4", "schemaVersion": 1, "data": original}}}, "page", page, "block", block))
	testOK(t, dbBatch(t, k, e, []any{map[string]any{"kind": "block.update", "blockId": block, "type": "paragraph", "schemaVersion": 1, "data": map[string]any{"richText": []any{}}}}, "page", page, "block", block))
	testOK(t, restoreTest(t, k, e, page, "2"))
	restored := readPage(t, k, e, page)["blocks"].([]any)[0].(map[string]any)
	if restored["type"] != "heading_4" || !strings.Contains(string(marshal(restored["data"])), "Native H4 evidence") {
		t.Fatal(restored)
	}
	command := map[string]any{"kind": "page.update", "pageId": page, "title": "Clicked"}
	saved := testOK(t, testDispatch(t, k, e, "editor.saveButton", advancedRequest(k, map[string]any{"pageId": page, "label": "Rename", "commands": []any{command}})))
	proposal := testOK(t, testDispatch(t, k, e, "editor.button", advancedRequest(k, map[string]any{"actionDefinitionId": saved["actionDefinitionId"]})))
	if readPage(t, k, e, page)["page"].(map[string]any)["title"] != "Evidence" {
		t.Fatal("preview executed an action")
	}
	testOK(t, dbBatch(t, k, e, proposal["commands"].([]any), "page", page))
	if readPage(t, k, e, page)["page"].(map[string]any)["title"] != "Clicked" {
		t.Fatal("reviewed canonical action missing")
	}
}
