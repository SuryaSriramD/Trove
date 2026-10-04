package workspace

import (
	"encoding/json"
	"strings"
	"testing"
	"trove.local/phase0/contracts"
)

func TestRichColorsSafeImportExportAndUnknownEvidence(t *testing.T) {
	for token, css := range richForeground {
		if richColorToken(css, false) != token || richColorToken(richBackground[token], true) != token {
			t.Fatal("palette mismatch", token)
		}
	}
	for _, unsafe := range []string{"#123456", "url(https://example.test)", "expression(alert(1))", "rgb(999,0,0)"} {
		if richColorToken(unsafe, false) != "" {
			t.Fatal("unknown CSS executed", unsafe)
		}
	}
	item := ImportItem{Locator: "colors.html", Outcome: "adapted", RawFileID: NewID()}
	source := `<p><span style="color:rgb(255,0,0);background-color:#ffff00"><strong>Known</strong></span><span style="color:#123456">Unknown</span></p>`
	blocks := parseImportHTML(source, &item, nil, nil)
	spans := blocks[0].Data["richText"].([]any)
	known := spans[0].(map[string]any)
	if known["color"] != "red" || known["highlight"] != "yellow" {
		t.Fatal("known colors lost", known)
	}
	if _, ok := spans[1].(map[string]any)["color"]; ok {
		t.Fatal("unknown style escaped allowlist")
	}
	if item.Outcome != "partial" || item.RawFileID == "" || !strings.Contains(item.Reason, "source evidence") {
		t.Fatal("unknown styles must remain linked to source evidence", item)
	}
	// Normalize through JSON, as data from the canonical store is normalized.
	var rich any
	json.Unmarshal(marshal(spans), &rich)
	exported := exportRichHTML(rich, "", nil, nil)
	if !strings.Contains(exported, `data-trove-color="red"`) || !strings.Contains(exported, `data-trove-highlight="yellow"`) || strings.Contains(exported, "#123456") {
		t.Fatal(exported)
	}
	round := parseImportHTML("<p>"+exported+"</p>", &ImportItem{}, nil, nil)
	if round[0].Data["richText"].([]any)[0].(map[string]any)["highlight"] != "yellow" {
		t.Fatal("exported token did not import")
	}
	escaped := exportRichHTML([]any{map[string]any{"text": "<script>alert(1)</script>", "color": "red", "link": "javascript:alert(1)"}}, "", nil, nil)
	if strings.Contains(escaped, "<script>") || strings.Contains(escaped, "javascript:") {
		t.Fatal("unsafe HTML export", escaped)
	}
	notion := notionRichText([]any{map[string]any{"plain_text": "Color", "annotations": map[string]any{"color": "purple_background", "underline": true}}})
	if notion[0].(map[string]any)["highlight"] != "purple" {
		t.Fatal("Notion highlight lost", notion)
	}
}
func TestRichColorContractAndReadableTableExport(t *testing.T) {
	for _, raw := range []string{`[{"text":"Legacy","marks":["bold"]}]`, `[{"text":"Styled","color":"blue","highlight":"yellow"}]`} {
		if err := contracts.Validate("domain", "RichText", []byte(raw)); err != nil {
			t.Fatal(err)
		}
	}
	for _, raw := range []string{`[{"text":"Unsafe","color":"url(https://example.test)"}]`, `[{"text":"Unknown","highlight":"#123456"}]`} {
		if err := contracts.Validate("domain", "RichText", []byte(raw)); err == nil {
			t.Fatal("accepted arbitrary CSS", raw)
		}
	}

	rich := map[string]any{"text": "Styled", "color": "blue", "highlight": "yellow"}
	table := map[string]any{"rowCount": 2, "columnCount": 1, "headerRows": 1, "headerColumns": 1, "columnWidths": []any{220}, "cells": []any{map[string]any{"id": NewID(), "row": 0, "column": 0, "rowSpan": 1, "colSpan": 1, "richText": []any{rich}, "preMergeCells": []any{}}, map[string]any{"id": NewID(), "row": 1, "column": 0, "rowSpan": 1, "colSpan": 1, "richText": []any{rich}, "preMergeCells": []any{}}}}
	block := contracts.DomainBlock{Type: "table", Data: marshal(table)}
	html := exportBlockMarkdown(block, "", nil, nil)
	imported := parseImportHTML(html, &ImportItem{}, nil, nil)
	if len(imported) != 1 || imported[0].Type != "table" {
		t.Fatal("exported table lost on HTML import", imported)
	}
	for _, key := range []string{"rowCount", "columnCount", "headerRows", "headerColumns", "columnWidths"} {
		if string(marshal(imported[0].Data[key])) != string(marshal(table[key])) {
			t.Fatal("table HTML round trip changed", key, imported[0].Data)
		}
	}
	for _, fragment := range []string{`width:220px`, `scope="col"`, `scope="row"`, `data-trove-color="blue"`, `data-trove-highlight="yellow"`} {
		if !strings.Contains(html, fragment) {
			t.Fatal("missing table semantics", fragment, html)
		}
	}
}
