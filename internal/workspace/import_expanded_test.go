package workspace

import (
	"archive/zip"
	"context"
	"database/sql"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestHTMLImportRetainsInertEvidenceAndRewritesLinks(t *testing.T) {
	k, e := testKernel(t)
	p := importFixture(t, k, e, map[string]string{"Parent.html": `<html><head><script>malicious()</script></head><body><h4>Fourth</h4><p>Hello <strong>bold</strong> <a href="Child.html">child</a> <a href="javascript:alert(1)">blocked</a></p><script>malicious()</script><img src="image.png" alt="picture"><table><tr><td>retained cell</td></tr></table></body></html>`, "Child.html": "<p>Child body</p>", "image.png": "fixture"})
	var parent ImportItem
	for _, item := range p.Items {
		if item.Locator == "Parent.html" {
			parent = item
		}
	}
	if parent.Kind != "html" || parent.Outcome != "partial" {
		t.Fatalf("missing reconciliation: %+v", parent)
	}
	text := string(marshal(parent.Blocks))
	if strings.Contains(text, "malicious()") || strings.Contains(text, "javascript:") {
		t.Fatalf("active HTML entered blocks: %s", text)
	}
	for _, want := range []string{"heading_4", "bold", "trove://page/", "image", "retained cell"} {
		if !strings.Contains(text, want) {
			t.Fatalf("lost %s: %s", want, text)
		}
	}
	p = completeImportFixture(t, k, e, p)
	var links int
	if err := k.db.QueryRow("SELECT count(*) FROM page_links WHERE source_page_id=?", parent.PageID).Scan(&links); err != nil || links != 1 {
		t.Fatal("canonical link missing", links, err)
	}
	destination := filepath.Join(t.TempDir(), "export.zip")
	result, err := k.ExportHTML(context.Background(), e, destination)
	if err != nil {
		t.Fatal(err)
	}
	if result.Pages < 3 {
		t.Fatal("pages absent", result)
	}
	archive, err := zip.OpenReader(destination)
	if err != nil {
		t.Fatal(err)
	}
	defer archive.Close()
	found := false
	for _, entry := range archive.File {
		if strings.HasPrefix(entry.Name, "pages/") && strings.HasSuffix(entry.Name, ".html") && strings.Contains(entry.Name, "Parent") {
			f, _ := entry.Open()
			b, _ := io.ReadAll(f)
			f.Close()
			body := string(b)
			if strings.Contains(body, "javascript:") || strings.Contains(body, "<script") {
				t.Fatal("active export")
			}
			if !strings.Contains(body, ".html") || !strings.Contains(body, "Content-Security-Policy") {
				t.Fatal("offline export missing link or policy")
			}
			found = true
		}
	}
	if !found {
		t.Fatal("HTML page missing")
	}
}
func TestFolderAndDocumentImportUsesSelectionAndRetainsSource(t *testing.T) {
	k, e := testKernel(t)
	folder := t.TempDir()
	os.Mkdir(filepath.Join(folder, "Nested"), 0700)
	os.WriteFile(filepath.Join(folder, "Nested", "Notes.txt"), []byte("First line\nSecond line"), 0600)
	outside := filepath.Join(t.TempDir(), "outside.txt")
	os.WriteFile(outside, []byte("outside secret"), 0600)
	os.Symlink(outside, filepath.Join(folder, "linked.txt"))
	handle, err := k.RegisterImportFolder(context.Background(), e, folder)
	if err != nil {
		t.Fatal(err)
	}
	out, err := previewPortableImport(context.Background(), k, e, map[string]any{"sourceHandle": handle, "sourceNamespace": "folder"})
	if err != nil {
		t.Fatal(err)
	}
	p := out.(ImportPlan)
	for _, item := range p.Items {
		if strings.Contains(item.Locator, "linked") {
			t.Fatal("symlink imported")
		}
		if item.Kind == "document" && !strings.Contains(string(marshal(item.Blocks)), "Second line") {
			t.Fatal("document text missing")
		}
	}
	completeImportFixture(t, k, e, p)
}
func TestWebCaptureRequiresOwnerAndUsesImportReview(t *testing.T) {
	k, e := testKernel(t)
	bad := e
	bad.Origin = "ai"
	if _, err := previewWebCapture(context.Background(), k, bad, map[string]any{"text": "denied"}); err == nil {
		t.Fatal("capture bypassed authority")
	}
	out, err := previewWebCapture(context.Background(), k, e, map[string]any{"title": "Clip", "url": "https://example.test/article", "html": "<p>Offline content</p><iframe src='https://evil.test'></iframe>"})
	if err != nil {
		t.Fatal(err)
	}
	p := out.(ImportPlan)
	if p.Initialized || p.State != "preview" || p.Items[0].Provenance == nil {
		t.Fatal("capture skipped review or provenance")
	}
	completeImportFixture(t, k, e, p)
}

func TestPortableManifestRestoresAdvancedBlocksAndParentReferences(t *testing.T) {
	k, e := testKernel(t)
	p := importFixture(t, k, e, map[string]string{"Advanced.md": "Original"})
	item := &p.Items[0]
	container, heading, synced := NewID(), NewID(), NewID()
	item.Blocks = []ImportBlock{{container, "tab_container", map[string]any{"tabs": []any{map[string]any{"id": NewID(), "title": "Details", "childBlockIds": []any{heading}}}}}, {heading, "heading_4", map[string]any{"richText": richImport("Preserved fourth heading")}}, {synced, "synced_block", map[string]any{"contentSourceId": heading}}, {NewID(), "bookmark", map[string]any{"url": "https://example.test", "title": "Reference"}}, {NewID(), "table", map[string]any{"rowCount": 1, "columnCount": 2, "headerRows": 0, "headerColumns": 0, "columnWidths": []any{100, 100}, "cells": []any{map[string]any{"id": NewID(), "row": 0, "column": 0, "rowSpan": 1, "colSpan": 2, "richText": richImport("Merged content"), "preMergeCells": []any{map[string]any{"evidence": "before merge"}}}}}}}
	item.BlockParents = map[string]string{heading: container}
	p.Hash = importPlanHash(p)
	if err := k.write(context.Background(), func(tx *sql.Tx) error { return saveImportPlan(context.Background(), tx, p) }); err != nil {
		t.Fatal(err)
	}
	p = completeImportFixture(t, k, e, p)
	archive := filepath.Join(t.TempDir(), "portable.zip")
	if _, err := k.ExportPortable(context.Background(), e, archive); err != nil {
		t.Fatal(err)
	}
	other, owner := testKernel(t)
	handle, err := other.RegisterSelection(archive)
	if err != nil {
		t.Fatal(err)
	}
	out, err := previewPortableImport(context.Background(), other, owner, map[string]any{"sourceHandle": handle, "sourceNamespace": "roundtrip"})
	if err != nil {
		t.Fatal(err)
	}
	incoming := out.(ImportPlan)
	var restored ImportItem
	for _, candidate := range incoming.Items {
		if candidate.Title == "Advanced" {
			restored = candidate
		}
	}
	if len(restored.Blocks) != 5 || restored.Blocks[0].Type != "tab_container" {
		t.Fatalf("canonical blocks not restored: %s", marshal(restored))
	}
	if restored.Blocks[0].ID == container {
		t.Fatal("source block ID reused")
	}
	var restoredHeading, restoredSync ImportBlock
	for _, block := range restored.Blocks {
		if block.Type == "heading_4" {
			restoredHeading = block
		}
		if block.Type == "synced_block" {
			restoredSync = block
		}
	}
	if restored.BlockParents[restoredHeading.ID] != restored.Blocks[0].ID {
		t.Fatal("parent reference not remapped")
	}
	if str(restoredSync.Data["contentSourceId"]) != restoredHeading.ID {
		t.Fatal("synced source not remapped")
	}
	completeImportFixture(t, other, owner, incoming)
}
