package workspace

import (
	"archive/zip"
	"context"
	"io"
	"path/filepath"
	"strings"
	"testing"
)

func TestCanonicalBlockLinksSaveRecoverAndExportAnchors(t *testing.T) {
	ctx := context.Background()
	k, e := testKernel(t)
	targetPage := createPage(t, k, e, "Target")
	sourcePage := createPage(t, k, e, "Links")
	code, linkBlock := NewID(), NewID()
	link := canonicalPageLink(targetPage, code)
	testOK(t, dbBatch(t, k, e, []any{map[string]any{"kind": "block.insert", "block": map[string]any{"blockId": code, "pageId": targetPage, "type": "code", "schemaVersion": 1, "data": map[string]any{"language": "go", "source": "func Preserved() {}"}}}}, "page", targetPage, "block", code))
	testOK(t, dbBatch(t, k, e, []any{map[string]any{"kind": "block.insert", "block": map[string]any{"blockId": linkBlock, "pageId": sourcePage, "type": "paragraph", "schemaVersion": 1, "data": map[string]any{"richText": []any{map[string]any{"text": "Code source", "link": link}}}}}}, "page", sourcePage, "block", linkBlock))
	var target string
	if err := k.db.QueryRow("SELECT target_page_id FROM page_links WHERE source_block_id=?", linkBlock).Scan(&target); err != nil || target != targetPage {
		t.Fatal("block URL did not record canonical page reference", target, err)
	}
	testOK(t, dbBatch(t, k, e, []any{map[string]any{"kind": "block.update", "blockId": linkBlock, "type": "paragraph", "schemaVersion": 1, "data": map[string]any{"richText": richImport("Changed")}}}, "page", sourcePage, "block", linkBlock))
	testOK(t, restoreTest(t, k, e, sourcePage, "2"))
	var body string
	k.db.QueryRow("SELECT data_json FROM blocks WHERE id=?", linkBlock).Scan(&body)
	if !strings.Contains(body, link) {
		t.Fatal("restored block link changed", body)
	}
	archive := filepath.Join(t.TempDir(), "html.zip")
	if _, err := k.ExportHTML(ctx, e, archive); err != nil {
		t.Fatal(err)
	}
	zr, err := zip.OpenReader(archive)
	if err != nil {
		t.Fatal(err)
	}
	defer zr.Close()
	anchor, htmlLink, markdownAnchor := false, false, false
	for _, f := range zr.File {
		if !strings.HasPrefix(f.Name, "pages/") {
			continue
		}
		in, _ := f.Open()
		raw, _ := io.ReadAll(in)
		in.Close()
		text := string(raw)
		if strings.HasSuffix(f.Name, ".html") {
			anchor = anchor || strings.Contains(text, `id="`+code+`"`)
			htmlLink = htmlLink || (strings.Contains(text, "#"+code) && strings.Contains(text, "Code source"))
			if strings.Contains(text, link) {
				t.Fatal("native URI leaked into offline HTML")
			}
		}
		if strings.HasSuffix(f.Name, ".md") {
			markdownAnchor = markdownAnchor || strings.Contains(text, `<a id="`+code+`"></a>`)
		}
	}
	if !anchor || !htmlLink || !markdownAnchor {
		t.Fatal("offline block target or link missing", anchor, htmlLink, markdownAnchor)
	}
	other, owner := testKernel(t)
	handle, err := other.RegisterSelection(archive)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := previewPortableImport(ctx, other, owner, map[string]any{"sourceHandle": handle, "sourceNamespace": "block-link-roundtrip"})
	if err != nil {
		t.Fatal(err)
	}
	plan := raw.(ImportPlan)
	newCode, newPage, newLink := "", "", ""
	for _, item := range plan.Items {
		for _, block := range item.Blocks {
			if block.Type == "code" {
				newCode = block.ID
				newPage = item.PageID
			}
			if block.Type == "paragraph" {
				spans, _ := block.Data["richText"].([]any)
				for _, s := range spans {
					span, _ := s.(map[string]any)
					if str(span["text"]) == "Code source" {
						newLink = str(span["link"])
					}
				}
			}
		}
	}
	if newLink != canonicalPageLink(newPage, newCode) || newCode == code {
		t.Fatal("canonical block identities not remapped", newLink, newPage, newCode)
	}
	completeImportFixture(t, other, owner, plan)
}
func TestDuplicateReimportRemapsPageMentionTargets(t *testing.T) {
	ctx := context.Background()
	source, e := testKernel(t)
	a := createPage(t, source, e, "A")
	b := createPage(t, source, e, "B")
	mention := NewID()
	testOK(t, dbBatch(t, source, e, []any{map[string]any{"kind": "block.insert", "block": map[string]any{"blockId": mention, "pageId": a, "type": "page_mention", "schemaVersion": 1, "data": map[string]any{"targetPageId": b, "displaySnapshot": "B"}}}}, "page", a, "block", mention))
	archive := filepath.Join(t.TempDir(), "portable.zip")
	if _, err := source.ExportPortable(ctx, e, archive); err != nil {
		t.Fatal(err)
	}
	target, owner := testKernel(t)
	preview := func() ImportPlan {
		handle, err := target.RegisterSelection(archive)
		if err != nil {
			t.Fatal(err)
		}
		value, err := previewPortableImport(ctx, target, owner, map[string]any{"sourceHandle": handle, "sourceNamespace": "repeat"})
		if err != nil {
			t.Fatal(err)
		}
		return value.(ImportPlan)
	}
	first := completeImportFixture(t, target, owner, preview())
	oldB := ""
	for _, item := range first.Items {
		if item.Title == "B" && item.PageID != "" {
			oldB = item.PageID
		}
	}
	second := preview()
	choices := []any{}
	for _, item := range second.Items {
		if (item.Title == "A" || item.Title == "B") && item.PageID != "" {
			choices = append(choices, map[string]any{"sourceLocator": item.Locator, "choice": "duplicate"})
		}
	}
	configured, err := configurePortableImport(ctx, target, owner, map[string]any{"planId": second.ID, "planHash": second.Hash, "choices": choices})
	if err != nil {
		t.Fatal(err)
	}
	second = completeImportFixture(t, target, owner, configured.(ImportPlan))
	newA, newB := "", ""
	for _, item := range second.Items {
		if item.Title == "A" {
			newA = item.PageID
		}
		if item.Title == "B" {
			newB = item.PageID
		}
	}
	var actual string
	if err := target.db.QueryRow("SELECT json_extract(data_json,'$.targetPageId') FROM blocks WHERE page_id=? AND type='page_mention'", newA).Scan(&actual); err != nil {
		t.Fatal(err)
	}
	if actual != newB || newB == oldB {
		t.Fatal("duplicate mention retained old page identity", actual, newB, oldB)
	}
}
func TestRecoveryAllowsOnlyHistoricalUnavailableSyncedReferences(t *testing.T) {
	k, e := testKernel(t)
	sourcePage := createPage(t, k, e, "Reusable source")
	refPage := createPage(t, k, e, "Referencing page")
	source, ref, paragraph := NewID(), NewID(), NewID()
	insert := func(id, page, kind string, data map[string]any) any {
		return map[string]any{"kind": "block.insert", "block": map[string]any{"blockId": id, "pageId": page, "type": kind, "schemaVersion": 1, "data": data}}
	}
	testOK(t, dbBatch(t, k, e, []any{insert(source, sourcePage, "paragraph", map[string]any{"richText": richImport("Source text")})}, "page", sourcePage, "block", source))
	testOK(t, dbBatch(t, k, e, []any{insert(ref, refPage, "synced_block", map[string]any{"contentSourceId": source}), insert(paragraph, refPage, "paragraph", map[string]any{"richText": richImport("Recover me")})}, "page", refPage, "block", ref, "block", paragraph))
	testOK(t, dbBatch(t, k, e, []any{map[string]any{"kind": "block.update", "blockId": paragraph, "type": "paragraph", "schemaVersion": 1, "data": map[string]any{"richText": richImport("Changed")}}}, "page", refPage, "block", paragraph))
	testOK(t, dbBatch(t, k, e, []any{map[string]any{"kind": "block.delete", "blockId": source, "includeDescendants": true}}, "page", sourcePage, "block", source))
	testOK(t, restoreTest(t, k, e, refPage, "2"))
	var content string
	k.db.QueryRow("SELECT data_json FROM blocks WHERE id=?", paragraph).Scan(&content)
	if !strings.Contains(content, "Recover me") {
		t.Fatal("historical content not restored", content)
	}
	var target string
	k.db.QueryRow("SELECT json_extract(data_json,'$.contentSourceId') FROM blocks WHERE id=?", ref).Scan(&target)
	if target != source {
		t.Fatal("unavailable source reference lost")
	}
	invalid := NewID()
	testCode(t, dbBatch(t, k, e, []any{insert(invalid, refPage, "synced_block", map[string]any{"contentSourceId": source})}, "page", refPage, "block", invalid), "INVALID_REQUEST")
	testCode(t, dbBatch(t, k, e, []any{map[string]any{"kind": "block.update", "blockId": ref, "type": "synced_block", "schemaVersion": 1, "data": map[string]any{"contentSourceId": NewID()}}}, "page", refPage, "block", ref), "INVALID_REQUEST")
}
func TestCanonicalPageLinkParsingRejectsAmbiguousRoutes(t *testing.T) {
	page, block := NewID(), NewID()
	for _, link := range []string{canonicalPageLink(page, ""), canonicalPageLink(page, block), "trove://page/" + page + "#" + block} {
		p, b, ok := parseCanonicalPageLink(link)
		if !ok || p != page || (strings.Contains(link, "block") && b != block) {
			t.Fatal(link, p, b, ok)
		}
	}
	for _, link := range []string{"trove://evil/" + page, "trove://page/" + page + "/block/", "trove://page/" + page + "/extra", "trove://page/" + page + "?override=1"} {
		if _, _, ok := parseCanonicalPageLink(link); ok {
			t.Fatal("ambiguous URL accepted", link)
		}
	}
}
