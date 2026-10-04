package workspace

import (
	"archive/zip"
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
)

func TestEditorIntegrityRichContentAndCollapseSurviveRecoveryAndPortability(t *testing.T) {
	k, e := testKernel(t)
	page := createPage(t, k, e, "Rich editing evidence")
	toggle, paragraph, table := NewID(), NewID(), NewID()
	rich := []any{map[string]any{"text": "Keep! ", "marks": []any{"bold"}}, map[string]any{"text": "linked", "link": "https://example.test/source"}, map[string]any{"text": " తెలుగు 🌱"}}
	toggleData := map[string]any{"richText": richImport("Toggle"), "collapsed": true}
	paragraphData := map[string]any{"richText": rich}
	cell := map[string]any{"id": NewID(), "row": 0, "column": 0, "rowSpan": 1, "colSpan": 1, "richText": rich, "preMergeCells": []any{map[string]any{"retained": "original source evidence"}}}
	tableData := map[string]any{"rowCount": 1, "columnCount": 1, "headerRows": 1, "headerColumns": 0, "columnWidths": []any{180}, "cells": []any{cell}}
	insert := func(id, kind, parent string, data any) any {
		block := map[string]any{"blockId": id, "pageId": page, "type": kind, "schemaVersion": 1, "data": data}
		if parent != "" {
			block["parentBlockId"] = parent
		}
		return map[string]any{"kind": "block.insert", "block": block}
	}
	testOK(t, dbBatch(t, k, e, []any{insert(toggle, "toggle", "", toggleData), insert(paragraph, "paragraph", toggle, paragraphData), insert(table, "table", toggle, tableData)}, "page", page, "block", toggle, "block", paragraph, "block", table))
	revision := str(readPage(t, k, e, page)["page"].(map[string]any)["revision"])
	testOK(t, dbBatch(t, k, e, []any{map[string]any{"kind": "block.update", "blockId": toggle, "type": "toggle", "schemaVersion": 1, "data": map[string]any{"richText": richImport("Toggle"), "collapsed": false}}, map[string]any{"kind": "block.update", "blockId": paragraph, "type": "paragraph", "schemaVersion": 1, "data": map[string]any{"richText": richImport("Changed later")}}}, "page", page, "block", toggle, "block", paragraph))
	testOK(t, restoreTest(t, k, e, page, revision))
	path := k.Path()
	k.Close()
	reopened, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	expected := map[string]any{toggle: toggleData, paragraph: paragraphData, table: tableData}
	for _, raw := range readPage(t, reopened, reopened.OwnerContext(), page)["blocks"].([]any) {
		block := raw.(map[string]any)
		if string(marshal(block["data"])) != string(marshal(expected[str(block["id"])])) {
			t.Fatalf("history/restart changed rich content or collapse: %s", marshal(block))
		}
		if block["id"] != toggle && block["parentBlockId"] != toggle {
			t.Fatal("presentation changed child ownership", block)
		}
	}
	destination := filepath.Join(t.TempDir(), "editor-portable.zip")
	if _, err = reopened.ExportPortable(context.Background(), reopened.OwnerContext(), destination); err != nil {
		t.Fatal(err)
	}
	archive, err := zip.OpenReader(destination)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, member := range archive.File {
		if member.Name != "trove-manifest.json" {
			continue
		}
		reader, err := member.Open()
		if err != nil {
			t.Fatal(err)
		}
		var manifest map[string]any
		err = json.NewDecoder(reader).Decode(&manifest)
		reader.Close()
		if err != nil {
			t.Fatal(err)
		}
		for _, p := range manifest["pages"].([]any) {
			for _, raw := range p.(map[string]any)["canonicalBlocks"].([]any) {
				block := raw.(map[string]any)
				if string(marshal(block["data"])) != string(marshal(expected[str(block["id"])])) {
					t.Fatalf("portable bytes changed canonical data: %s", marshal(block))
				}
			}
		}
		found = true
	}
	archive.Close()
	if !found {
		t.Fatal("export omitted canonical manifest")
	}
	k2, e2 := testKernel(t)
	handle, err := k2.RegisterSelection(destination)
	if err != nil {
		t.Fatal(err)
	}
	preview, err := previewPortableImport(context.Background(), k2, e2, map[string]any{"sourceHandle": handle, "sourceNamespace": "editor-integrity"})
	if err != nil {
		t.Fatal(err)
	}
	plan := completeImportFixture(t, k2, e2, preview.(ImportPlan))
	matched := false
	for _, item := range plan.Items {
		if item.PageID == "" || item.Title != "Rich editing evidence" {
			continue
		}
		for _, raw := range readPage(t, k2, e2, item.PageID)["blocks"].([]any) {
			block := raw.(map[string]any)
			data := block["data"].(map[string]any)
			if block["type"] == "toggle" {
				if data["collapsed"] != true {
					t.Fatal("portable import lost collapse", data)
				}
				matched = true
			}
			if block["type"] == "paragraph" && string(marshal(data["richText"])) != string(marshal(rich)) {
				t.Fatal("portable import flattened marks/links", data)
			}
		}
	}
	if !matched {
		t.Fatalf("canonical toggle not reconstructed on portable import: %s", marshal(plan.Items))
	}
}
