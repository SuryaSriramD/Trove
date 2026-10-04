package workspace

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
)

func TestRichLayoutMovesSourceUpdatesRecoveryAndPortableReimport(t *testing.T) {
	k, e := testKernel(t)
	page := createPage(t, k, e, "Rich layout")
	columns, tabs, code, reference, colGroup, tabGroup := NewID(), NewID(), NewID(), NewID(), NewID(), NewID()
	columnData := func(children ...string) map[string]any {
		if children == nil {
			children = []string{}
		}
		return map[string]any{"columns": []any{map[string]any{"id": colGroup, "widthRatio": 1, "childBlockIds": children}}}
	}
	tabData := func(children ...string) map[string]any {
		if children == nil {
			children = []string{}
		}
		return map[string]any{"tabs": []any{map[string]any{"id": tabGroup, "title": "Technical evidence", "childBlockIds": children}}}
	}
	source := map[string]any{"source": "const retained = `🌱`;\n", "language": "typescript", "filename": "source.ts", "wrap": true, "lineNumbers": true}
	insert := func(id, kind, parent string, data any) any {
		b := map[string]any{"blockId": id, "pageId": page, "type": kind, "schemaVersion": 1, "data": data}
		if parent != "" {
			b["parentBlockId"] = parent
		}
		return map[string]any{"kind": "block.insert", "block": b}
	}
	testOK(t, dbBatch(t, k, e, []any{insert(columns, "columns", "", columnData(code)), insert(tabs, "tab_container", "", tabData()), insert(code, "code", columns, source), insert(reference, "synced_block", "", map[string]any{"contentSourceId": code})}, "page", page, "block", columns, "block", tabs, "block", code, "block", reference))
	update := func(id, kind string, data any) any {
		return map[string]any{"kind": "block.update", "blockId": id, "type": kind, "schemaVersion": 1, "data": data}
	}
	testOK(t, dbBatch(t, k, e, []any{update(columns, "columns", columnData()), update(tabs, "tab_container", tabData(code)), map[string]any{"kind": "block.move", "blockId": code, "pageId": page, "parentBlockId": tabs}}, "page", page, "block", columns, "block", tabs, "block", code))
	rev := str(readPage(t, k, e, page)["page"].(map[string]any)["revision"])
	testOK(t, dbBatch(t, k, e, []any{update(code, "code", map[string]any{"source": "changed source", "language": "typescript"})}, "page", page, "block", code))
	testOK(t, restoreTest(t, k, e, page, rev))
	path := k.Path()
	k.Close()
	reopened, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	current := readPage(t, reopened, reopened.OwnerContext(), page)
	seen := map[string]bool{}
	for _, raw := range current["blocks"].([]any) {
		b := raw.(map[string]any)
		switch b["id"] {
		case code:
			if b["parentBlockId"] != tabs || string(marshal(b["data"])) != string(marshal(source)) {
				t.Fatal("source or ownership changed", b)
			}
			seen[code] = true
		case reference:
			if str(b["data"].(map[string]any)["contentSourceId"]) != code {
				t.Fatal("reference changed")
			}
			seen[reference] = true
		}
	}
	if len(seen) != 2 {
		t.Fatal("missing canonical source/reference")
	}
	destination := filepath.Join(t.TempDir(), "layout.zip")
	if _, err = reopened.ExportPortable(context.Background(), reopened.OwnerContext(), destination); err != nil {
		t.Fatal(err)
	}
	target, owner := testKernel(t)
	handle, err := target.RegisterSelection(destination)
	if err != nil {
		t.Fatal(err)
	}
	preview, err := previewPortableImport(context.Background(), target, owner, map[string]any{"sourceHandle": handle, "sourceNamespace": "rich-layout"})
	if err != nil {
		t.Fatal(err)
	}
	plan := completeImportFixture(t, target, owner, preview.(ImportPlan))
	matched := false
	for _, item := range plan.Items {
		if item.Title != "Rich layout" {
			continue
		}
		var pageState struct {
			Blocks []struct {
				ID     string         `json:"id"`
				Parent *string        `json:"parentBlockId"`
				Type   string         `json:"type"`
				Data   map[string]any `json:"data"`
			}
		}
		if err = json.Unmarshal(marshal(readPage(t, target, owner, item.PageID)), &pageState); err != nil {
			t.Fatal(err)
		}
		var importedCode, importedTabs, importedReference string
		for _, b := range pageState.Blocks {
			switch b.Type {
			case "code":
				importedCode = b.ID
				if string(marshal(b.Data)) != string(marshal(source)) {
					t.Fatal("portable code changed")
				}
			case "tab_container":
				importedTabs = b.ID
			case "synced_block":
				importedReference = str(b.Data["contentSourceId"])
			}
		}
		for _, b := range pageState.Blocks {
			if b.Type == "code" && (b.Parent == nil || *b.Parent != importedTabs) {
				t.Fatal("portable layout lost parent")
			}
		}
		if importedCode == "" || importedTabs == "" || importedReference != importedCode {
			t.Fatal("portable source identity remapping failed")
		}
		matched = true
	}
	if !matched {
		t.Fatal("portable layout page missing")
	}
}
