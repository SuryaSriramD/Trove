package workspace

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"path/filepath"
	"strings"
	"testing"

	"trove.local/phase0/contracts"
)

func structureItem(t *testing.T, p ImportPlan, title string) ImportItem {
	t.Helper()
	for _, item := range p.Items {
		if item.Title == title && item.PageID != "" {
			return item
		}
	}
	t.Fatal("missing page", title)
	return ImportItem{}
}

func structureBlocks(t *testing.T, k *Kernel, page string) []contracts.DomainBlock {
	t.Helper()
	result, err := readPageTx(context.Background(), k.db, page)
	if err != nil {
		t.Fatal(err)
	}
	return result.Blocks
}

func structureFind(t *testing.T, blocks []contracts.DomainBlock, kind, text string) contracts.DomainBlock {
	t.Helper()
	found := []contracts.DomainBlock{}
	for _, block := range blocks {
		if block.Type == kind && (text == "" || strings.Contains(string(block.Data), text)) {
			found = append(found, block)
		}
	}
	if len(found) != 1 {
		t.Fatalf("wanted one %s %q, got %d in %s", kind, text, len(found), marshal(blocks))
	}
	return found[0]
}

func structureParent(t *testing.T, child, parent contracts.DomainBlock) {
	t.Helper()
	if child.ParentBlockId == nil || *child.ParentBlockId != parent.Id {
		t.Fatalf("%s parent=%v wanted %s", child.Data, child.ParentBlockId, parent.Id)
	}
}

func assertNestedHTML(t *testing.T, k *Kernel, page string) {
	t.Helper()
	blocks := structureBlocks(t, k, page)
	toggle := structureFind(t, blocks, "toggle", "Toggle")
	list := structureFind(t, blocks, "bulleted_list_item", "Parent bold")
	deep := structureFind(t, blocks, "numbered_list_item", "Deep item")
	structureParent(t, list, toggle)
	structureParent(t, deep, list)
	structureParent(t, structureFind(t, blocks, "paragraph", "Nested paragraph"), list)
	structureParent(t, structureFind(t, blocks, "code", "nested code"), list)
	if strings.Contains(string(list.Data), "Deep item") {
		t.Fatal("child text flattened into parent")
	}
	columns := structureFind(t, blocks, "columns", "")
	left := structureFind(t, blocks, "paragraph", "Left cell")
	right := structureFind(t, blocks, "paragraph", "Right cell")
	structureParent(t, columns, toggle)
	structureParent(t, left, columns)
	structureParent(t, right, columns)
	var data map[string]any
	json.Unmarshal(columns.Data, &data)
	groups := data["columns"].([]any)
	if len(groups) != 2 || groups[0].(map[string]any)["widthRatio"] != float64(25) || groups[0].(map[string]any)["childBlockIds"].([]any)[0] != string(left.Id) || groups[1].(map[string]any)["childBlockIds"].([]any)[0] != string(right.Id) {
		t.Fatal("column membership/widths changed", data)
	}
	if !strings.Contains(string(left.Data), `"color":"red"`) || !strings.Contains(string(left.Data), `"bold"`) {
		t.Fatal("safe rich marks lost", string(left.Data))
	}
	tabs := structureFind(t, blocks, "tab_container", "")
	a := structureFind(t, blocks, "paragraph", "First panel")
	b := structureFind(t, blocks, "table", "Second cell")
	structureParent(t, a, tabs)
	structureParent(t, b, tabs)
	json.Unmarshal(tabs.Data, &data)
	tabGroups := data["tabs"].([]any)
	if len(tabGroups) != 2 || tabGroups[0].(map[string]any)["title"] != "Alpha" || tabGroups[1].(map[string]any)["title"] != "Beta" {
		t.Fatal("tab labels changed", data)
	}
	for _, block := range blocks {
		if strings.Contains(string(block.Data), "do_not_execute") {
			t.Fatal("active source escaped into canonical content")
		}
	}
}

func TestNestedHTMLReopensExportsAndReimportsWithExplicitIdentities(t *testing.T) {
	k, e := testKernel(t)
	source := `<details id="toggle"><summary>Toggle</summary><ul><li id="list">Parent bold<ol><li id="deep">Deep item</li></ol><p id="nested">Nested paragraph</p><pre id="code">nested code</pre></li></ul><div class="column-list" id="columns"><div class="column" style="width:25%"><p id="left"><strong style="color:red">Left cell</strong></p></div><div class="column" style="width:75%"><p id="right">Right cell</p></div></div></details><div id="tabs"><div role="tablist"><button aria-controls="alpha">Alpha</button><button aria-controls="beta">Beta</button></div><div role="tabpanel" id="alpha"><p id="first">First panel</p></div><div role="tabpanel" id="beta"><table><tr><td>Second cell</td></tr></table></div></div><script>do_not_execute()</script>`
	p := completeImportFixture(t, k, e, importFixture(t, k, e, map[string]string{"Nested.html": source}))
	item := structureItem(t, p, "Nested")
	assertNestedHTML(t, k, item.PageID)
	if item.Outcome != "partial" {
		t.Fatal("active source omission was not reported")
	}
	raw, err := k.readImportBytes(context.Background(), item.RawFileID)
	if err != nil || string(raw) != source {
		t.Fatal("source evidence changed", err)
	}
	before := structureFind(t, structureBlocks(t, k, item.PageID), "numbered_list_item", "Deep item")
	again := importFixture(t, k, e, map[string]string{"Nested.html": source})
	same := structureItem(t, again, "Nested")
	if same.PageID != item.PageID || same.Choice != "keep_local" {
		t.Fatal("identical import is not idempotent", same)
	}
	completeImportFixture(t, k, e, again)
	changed := importFixture(t, k, e, map[string]string{"Nested.html": strings.Replace(source, "Nested paragraph", "Updated paragraph", 1)})
	changedItem := structureItem(t, changed, "Nested")
	if changedItem.Conflict == "" {
		t.Fatal("changed source skipped review")
	}
	configured, err := configurePortableImport(context.Background(), k, e, map[string]any{"planId": changed.ID, "planHash": changed.Hash, "choices": []any{map[string]any{"sourceLocator": changedItem.Locator, "choice": "replace"}}})
	if err != nil {
		t.Fatal(err)
	}
	completeImportFixture(t, k, e, configured.(ImportPlan))
	after := structureFind(t, structureBlocks(t, k, item.PageID), "numbered_list_item", "Deep item")
	if after.Id != before.Id {
		t.Fatal("explicit source block identity changed on reviewed replacement")
	}
	// Replace once more to exercise parked order keys from the previous replacement.
	changed = importFixture(t, k, e, map[string]string{"Nested.html": source})
	changedItem = structureItem(t, changed, "Nested")
	configured, err = configurePortableImport(context.Background(), k, e, map[string]any{"planId": changed.ID, "planHash": changed.Hash, "choices": []any{map[string]any{"sourceLocator": changedItem.Locator, "choice": "replace"}}})
	if err != nil {
		t.Fatal(err)
	}
	completeImportFixture(t, k, e, configured.(ImportPlan))
	workspacePath := k.Path()
	k.Close()
	k, err = Open(workspacePath)
	if err != nil {
		t.Fatal(err)
	}
	defer k.Close()
	e = k.OwnerContext()
	assertNestedHTML(t, k, item.PageID)
	archive := filepath.Join(t.TempDir(), "nested.zip")
	if _, err = k.ExportPortable(context.Background(), e, archive); err != nil {
		t.Fatal(err)
	}
	other, owner := testKernel(t)
	handle, err := other.RegisterSelection(archive)
	if err != nil {
		t.Fatal(err)
	}
	preview, err := previewPortableImport(context.Background(), other, owner, map[string]any{"sourceHandle": handle, "sourceNamespace": "nested-roundtrip"})
	if err != nil {
		t.Fatal(err)
	}
	roundtrip := completeImportFixture(t, other, owner, preview.(ImportPlan))
	restored := structureItem(t, roundtrip, "Nested")
	assertNestedHTML(t, other, restored.PageID)
}

func TestNestedMarkdownListsAndCodeRetainOwnership(t *testing.T) {
	k, e := testKernel(t)
	p := completeImportFixture(t, k, e, importFixture(t, k, e, map[string]string{"Lists.md": "- Parent\n  1. Numbered child\n    - Grandchild\n  continuation\n  ```go\n  if ready {\n    run()\n  }\n  ```\n- Sibling\n"}))
	item := structureItem(t, p, "Lists")
	blocks := structureBlocks(t, k, item.PageID)
	parent := structureFind(t, blocks, "bulleted_list_item", "Parent")
	child := structureFind(t, blocks, "numbered_list_item", "Numbered child")
	grandchild := structureFind(t, blocks, "bulleted_list_item", "Grandchild")
	structureParent(t, child, parent)
	structureParent(t, grandchild, child)
	structureParent(t, structureFind(t, blocks, "paragraph", "continuation"), parent)
	code := structureFind(t, blocks, "code", "run()")
	structureParent(t, code, parent)
	var data map[string]any
	json.Unmarshal(code.Data, &data)
	if data["source"] != "if ready {\n  run()\n}" {
		t.Fatal("code indentation changed", data)
	}
	if structureFind(t, blocks, "bulleted_list_item", "Sibling").ParentBlockId != nil {
		t.Fatal("list sibling incorrectly nested")
	}
}

func captureNestedNotion(t *testing.T, k *Kernel, e contracts.ServiceExecutionContext, changed bool) ImportPlan {
	t.Helper()
	root := strings.Repeat("a", 32)
	id := func(n int) string { return fmt.Sprintf("%032x", n) }
	block := func(n int, kind, text string, children bool) map[string]any {
		return map[string]any{"id": id(n), "type": kind, "has_children": children, kind: map[string]any{"rich_text": []any{map[string]any{"plain_text": text}}}}
	}
	toggle := block(1, "toggle", "API toggle", true)
	list := block(2, "bulleted_list_item", "API list", true)
	deep := block(3, "paragraph", "API deep", false)
	if changed {
		deep["paragraph"].(map[string]any)["rich_text"] = []any{map[string]any{"plain_text": "API deep changed"}}
	}
	heading := block(4, "heading_2", "API heading", true)
	heading["heading_2"].(map[string]any)["is_toggleable"] = true
	columnList := block(5, "column_list", "", true)
	left := block(6, "column", "", true)
	left["column"].(map[string]any)["width_ratio"] = 0.3
	right := block(7, "column", "", true)
	original := block(10, "synced_block", "", true)
	original["synced_block"] = map[string]any{"synced_from": nil}
	reference := block(11, "synced_block", "", true)
	reference["synced_block"] = map[string]any{"synced_from": map[string]any{"block_id": id(10)}}
	unknown := block(13, "future_widget", "opaque behavior", true)
	missing := block(16, "synced_block", "", true)
	missing["synced_block"] = map[string]any{"synced_from": map[string]any{"block_id": id(99)}}
	cyclic := block(17, "synced_block", "", true)
	cyclic["synced_block"] = map[string]any{"synced_from": map[string]any{"block_id": id(10)}}
	mention := block(18, "paragraph", "Person label", false)
	mention["paragraph"].(map[string]any)["rich_text"] = []any{map[string]any{"type": "mention", "plain_text": "Person label", "mention": map[string]any{"type": "user", "user": map[string]any{"id": id(99)}}}}
	children := map[string][]any{root: {toggle, columnList, original, reference, unknown, missing, mention}, id(1): {list, heading}, id(2): {deep}, id(4): {block(15, "paragraph", "Heading child", false)}, id(5): {left, right}, id(6): {block(8, "paragraph", "API left", false)}, id(7): {block(9, "paragraph", "API right", false)}, id(10): {block(12, "paragraph", "Reusable child", false), cyclic}, id(13): {block(14, "paragraph", "Unsupported child", false)}}
	client := &http.Client{Transport: notionFixtureTransport(func(r *http.Request) (*http.Response, error) {
		if strings.Contains(r.URL.Path, "/pages/") {
			return notionResponse(200, `{"id":"`+root+`","properties":{"title":{"type":"title","title":[{"plain_text":"Nested API"}]}}}`), nil
		}
		parts := strings.Split(r.URL.Path, "/")
		source := parts[len(parts)-2]
		values, ok := children[source]
		if !ok {
			t.Fatalf("unexpected endpoint %s", r.URL)
		}
		return notionResponse(200, string(marshal(map[string]any{"results": values, "has_more": false}))), nil
	})}
	out, err := startNotionCapture(context.Background(), k, e, map[string]any{"rootIds": []any{root}, "sourceNamespace": "nested-api"})
	if err != nil {
		t.Fatal(err)
	}
	capture := out.(NotionCapture)
	for i := 0; i < 30 && capture.State != "ready"; i++ {
		capture, err = k.CaptureNotionStep(context.Background(), e, capture.ID, capture.Revision, "fixture-token", client)
		if err != nil {
			t.Fatal(err)
		}
	}
	if capture.State != "ready" {
		t.Fatal("capture did not finish", capture)
	}
	finished, err := finishNotionCapture(context.Background(), k, e, map[string]any{"captureId": capture.ID, "revision": float64(capture.Revision)})
	if err != nil {
		t.Fatal(err)
	}
	return finished.(ImportPlan)
}

func TestNotionNestedSourceGraphAndIdempotentReviewedReplacement(t *testing.T) {
	k, e := testKernel(t)
	p := completeImportFixture(t, k, e, captureNestedNotion(t, k, e, false))
	item := structureItem(t, p, "Nested API")
	blocks := structureBlocks(t, k, item.PageID)
	toggle := structureFind(t, blocks, "toggle", "API toggle")
	list := structureFind(t, blocks, "bulleted_list_item", "API list")
	deep := structureFind(t, blocks, "paragraph", "API deep")
	structureParent(t, list, toggle)
	structureParent(t, deep, list)
	heading := structureFind(t, blocks, "toggle_heading", "API heading")
	structureParent(t, heading, toggle)
	structureParent(t, structureFind(t, blocks, "paragraph", "Heading child"), heading)
	columns := structureFind(t, blocks, "columns", "")
	structureParent(t, structureFind(t, blocks, "paragraph", "API left"), columns)
	structureParent(t, structureFind(t, blocks, "paragraph", "API right"), columns)
	ref := structureFind(t, blocks, "synced_block", "")
	var data map[string]any
	json.Unmarshal(ref.Data, &data)
	reusable := structureFind(t, blocks, "paragraph", "Reusable child")
	if reusable.ParentBlockId == nil || string(*reusable.ParentBlockId) != str(data["contentSourceId"]) {
		t.Fatal("synced original relationship not reconstructed", ref, reusable)
	}
	unknown := structureFind(t, blocks, "unsupported_external", "future_widget")
	structureParent(t, structureFind(t, blocks, "paragraph", "Unsupported child"), unknown)
	if len(p.SourceOmissions) == 0 || len(p.UnknownCompleteness) == 0 || portableImportReport(p)["state"] != "partial" {
		t.Fatal("source channel limits were overstated")
	}
	retainedRefs, retainedMentions := 0, 0
	for _, evidence := range p.Items {
		if strings.Contains(evidence.Reason, "reusable source unavailable or cyclic") {
			retainedRefs++
		}
		if strings.Contains(evidence.Reason, "mention/identity") {
			retainedMentions++
		}
	}
	if retainedRefs != 2 || retainedMentions != 1 {
		t.Fatal("supplied unsupported semantics were not reconciled", retainedRefs, retainedMentions)
	}
	again := captureNestedNotion(t, k, e, false)
	same := structureItem(t, again, "Nested API")
	if same.Choice != "keep_local" || same.PageID != item.PageID {
		t.Fatal("unchanged API capture requires spurious conflict", same)
	}
	completeImportFixture(t, k, e, again)
	changed := captureNestedNotion(t, k, e, true)
	incoming := structureItem(t, changed, "Nested API")
	if incoming.Conflict == "" {
		t.Fatal("changed capture bypassed review")
	}
	configured, err := configurePortableImport(context.Background(), k, e, map[string]any{"planId": changed.ID, "planHash": changed.Hash, "choices": []any{map[string]any{"sourceLocator": incoming.Locator, "choice": "replace"}}})
	if err != nil {
		t.Fatal(err)
	}
	completeImportFixture(t, k, e, configured.(ImportPlan))
	if got := structureFind(t, structureBlocks(t, k, item.PageID), "paragraph", "API deep changed"); got.Id != deep.Id {
		t.Fatal("API block identity replaced", deep.Id, got.Id)
	}
	archive := filepath.Join(t.TempDir(), "api-nested.zip")
	if _, err = k.ExportPortable(context.Background(), e, archive); err != nil {
		t.Fatal(err)
	}
	other, owner := testKernel(t)
	handle, err := other.RegisterSelection(archive)
	if err != nil {
		t.Fatal(err)
	}
	preview, err := previewPortableImport(context.Background(), other, owner, map[string]any{"sourceHandle": handle, "sourceNamespace": "nested-api-roundtrip"})
	if err != nil {
		t.Fatal(err)
	}
	roundtrip := completeImportFixture(t, other, owner, preview.(ImportPlan))
	restored := structureItem(t, roundtrip, "Nested API")
	restoredBlocks := structureBlocks(t, other, restored.PageID)
	structureParent(t, structureFind(t, restoredBlocks, "paragraph", "API deep changed"), structureFind(t, restoredBlocks, "bulleted_list_item", "API list"))
	structureParent(t, structureFind(t, restoredBlocks, "paragraph", "Heading child"), structureFind(t, restoredBlocks, "toggle_heading", "API heading"))
	structureFind(t, restoredBlocks, "synced_block", "")
}

func TestNestedImportDuplicateUsesIndependentBlockOwnership(t *testing.T) {
	k, e := testKernel(t)
	source := `<details id="parent"><summary>Copy toggle</summary><p id="child">Copy child</p></details>`
	first := completeImportFixture(t, k, e, importFixture(t, k, e, map[string]string{"Copy.html": source}))
	original := structureItem(t, first, "Copy")
	preview := importFixture(t, k, e, map[string]string{"Copy.html": source})
	incoming := structureItem(t, preview, "Copy")
	configured, err := configurePortableImport(context.Background(), k, e, map[string]any{"planId": preview.ID, "planHash": preview.Hash, "choices": []any{map[string]any{"sourceLocator": incoming.Locator, "choice": "duplicate"}}})
	if err != nil {
		t.Fatal(err)
	}
	copied := completeImportFixture(t, k, e, configured.(ImportPlan))
	duplicate := structureItem(t, copied, "Copy")
	oldBlocks, newBlocks := structureBlocks(t, k, original.PageID), structureBlocks(t, k, duplicate.PageID)
	if original.PageID == duplicate.PageID || oldBlocks[0].Id == newBlocks[0].Id {
		t.Fatal("copy reused source local identities")
	}
	structureParent(t, structureFind(t, newBlocks, "paragraph", "Copy child"), structureFind(t, newBlocks, "toggle", "Copy toggle"))
	structureParent(t, structureFind(t, oldBlocks, "paragraph", "Copy child"), structureFind(t, oldBlocks, "toggle", "Copy toggle"))
}

func TestNotionBlockEvidenceRetainsExactRawRepresentation(t *testing.T) {
	k, e := testKernel(t)
	root := strings.Repeat("a", 32)
	original := `{ "id":"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", "type":"future_widget", "future_widget":{"large":123456789012345678901234567890,"unknown":"kept"} }`
	client := &http.Client{Transport: notionFixtureTransport(func(r *http.Request) (*http.Response, error) {
		if strings.Contains(r.URL.Path, "/pages/") {
			return notionResponse(200, `{"properties":{}}`), nil
		}
		return notionResponse(200, `{"results":[`+original+`],"has_more":false}`), nil
	})}
	out, err := startNotionCapture(context.Background(), k, e, map[string]any{"rootIds": []any{root}, "sourceNamespace": "raw-json"})
	if err != nil {
		t.Fatal(err)
	}
	capture := out.(NotionCapture)
	for capture.State != "ready" {
		capture, err = k.CaptureNotionStep(context.Background(), e, capture.ID, capture.Revision, "fixture-token", client)
		if err != nil {
			t.Fatal(err)
		}
	}
	p, err := loadImportPlan(context.Background(), k.db, capture.PlanID)
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range p.Items {
		if item.Kind != "notion_block" {
			continue
		}
		raw, err := k.readImportBytes(context.Background(), item.RawFileID)
		if err != nil || string(raw) != original {
			t.Fatal("isolated raw block representation changed", string(raw), err)
		}
		if !strings.Contains(string(marshal(item.Blocks)), "123456789012345678901234567890") {
			t.Fatal("readable fallback rounded supplied numbers")
		}
		return
	}
	t.Fatal("raw block evidence missing")
}
