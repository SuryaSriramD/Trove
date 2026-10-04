package workspace

import (
	"context"
	"fmt"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"

	"golang.org/x/net/html"
)

// No source HTML is ever rendered: only allowlisted canonical block data leaves
// this adapter. Active/unknown constructs retain their original source evidence.
func safeImportURL(raw string) bool {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return false
	}
	switch strings.ToLower(u.Scheme) {
	case "", "https", "http", "mailto", "trove":
		return u.Host == "" || u.Scheme == "https" || u.Scheme == "http"
	}
	return false
}
func htmlImportAttr(n *html.Node, key string) string {
	for _, a := range n.Attr {
		if a.Key == key {
			return a.Val
		}
	}
	return ""
}
func inertHTMLText(n *html.Node) string {
	if n.Type == html.ElementNode {
		switch n.Data {
		case "script", "style", "iframe", "object", "embed", "template":
			return ""
		}
	}
	if n.Type == html.TextNode {
		return n.Data
	}
	var b strings.Builder
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		b.WriteString(inertHTMLText(c))
		if c.Data == "br" || c.Data == "p" || c.Data == "li" || c.Data == "tr" {
			b.WriteByte('\n')
		}
	}
	return b.String()
}
func parseImportHTML(source string, item *ImportItem, pages, assets map[string]string) []ImportBlock {
	root, err := html.Parse(strings.NewReader(source))
	if err != nil {
		item.Outcome = "unsupported_but_retained"
		item.Reason += "; HTML parser failed"
		return []ImportBlock{fallbackImportBlock(item, "HTML source retained")}
	}
	blocks := []ImportBlock{}
	omitted, unresolved := 0, 0
	var spans func(*html.Node, []string, string) []any
	spans = func(n *html.Node, marks []string, link string) []any {
		if n.Type == html.TextNode {
			if n.Data == "" {
				return nil
			}
			s := map[string]any{"text": n.Data}
			if len(marks) > 0 {
				s["marks"] = append([]string{}, marks...)
			}
			if link != "" {
				s["link"] = link
			}
			return []any{s}
		}
		if n.Type == html.ElementNode {
			switch n.Data {
			case "script", "style", "iframe", "object", "embed", "template":
				omitted++
				return nil
			case "strong", "b":
				marks = append(append([]string{}, marks...), "bold")
			case "em", "i":
				marks = append(append([]string{}, marks...), "italic")
			case "del", "s":
				marks = append(append([]string{}, marks...), "strike")
			case "code":
				marks = append(append([]string{}, marks...), "code")
			case "br":
				return richImport("\n")
			case "a":
				raw := htmlImportAttr(n, "href")
				if safeImportURL(raw) {
					var asset, found bool
					link, asset, found = resolveImportLink(item.Locator, raw, pages, assets)
					if asset {
						link = "/files/" + link
					}
					if !found {
						unresolved++
					}
				} else {
					omitted++
				}
			}
		}
		out := []any{}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			out = append(out, spans(c, marks, link)...)
		}
		return out
	}
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if len(blocks) >= 10000 {
			omitted++
			return
		}
		if n.Type == html.ElementNode {
			kind := ""
			switch n.Data {
			case "head", "script", "style", "iframe", "object", "embed", "template":
				if n.Data != "head" {
					omitted++
				}
				return
			case "h1", "h2", "h3", "h4":
				kind = "heading_" + n.Data[1:]
			case "h5", "h6":
				kind = "heading_4"
				omitted++
			case "p":
				kind = "paragraph"
			case "blockquote":
				kind = "quote"
			case "li":
				kind = "bulleted_list_item"
				if n.Parent != nil && n.Parent.Data == "ol" {
					kind = "numbered_list_item"
				}
			case "pre":
				blocks = append(blocks, ImportBlock{NewID(), "code", map[string]any{"source": inertHTMLText(n), "language": "text"}})
				return
			case "hr":
				blocks = append(blocks, ImportBlock{NewID(), "divider", map[string]any{}})
				return
			case "img", "audio", "video":
				target, asset, found := resolveImportLink(item.Locator, htmlImportAttr(n, "src"), pages, assets)
				if found && asset {
					kind := n.Data
					if kind == "img" {
						kind = "image"
					}
					blocks = append(blocks, ImportBlock{NewID(), kind, map[string]any{"fileId": target, "caption": richImport(htmlImportAttr(n, "alt"))}})
				} else {
					omitted++
					if text := htmlImportAttr(n, "alt"); text != "" {
						blocks = append(blocks, ImportBlock{NewID(), "paragraph", map[string]any{"richText": richImport(text)}})
					}
				}
				return
			case "table":
				if data, ok := importHTMLTable(n, func(cell *html.Node) []any { return spans(cell, nil, "") }); ok {
					blocks = append(blocks, ImportBlock{NewID(), "table", data})
				} else {
					blocks = append(blocks, fallbackImportBlock(item, strings.TrimSpace(inertHTMLText(n))))
					omitted++
				}
				return
			case "input", "button", "form", "canvas", "svg":
				omitted++
			}
			if kind != "" {
				content := spans(n, nil, "")
				if len(content) > 0 {
					blocks = append(blocks, ImportBlock{NewID(), kind, map[string]any{"richText": content}})
				}
				return
			}
		}
		if n.Type == html.TextNode && strings.TrimSpace(n.Data) != "" {
			blocks = append(blocks, ImportBlock{NewID(), "paragraph", map[string]any{"richText": richImport(n.Data)}})
			return
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(root)
	if omitted > 0 || unresolved > 0 {
		item.Outcome = "partial"
		item.Reason += fmt.Sprintf("; %d active/unsupported constructs kept in source evidence, %d unresolved links", omitted, unresolved)
	}
	return blocks
}
func (k *Kernel) parseImportDocument(ctx context.Context, item *ImportItem) []ImportBlock {
	var storage, media string
	if err := k.db.QueryRowContext(ctx, "SELECT storage_key,media_type FROM files WHERE id=?", item.RawFileID).Scan(&storage, &media); err != nil {
		item.Outcome = "unsupported_but_retained"
		item.Reason += "; metadata unavailable"
		return []ImportBlock{fallbackImportBlock(item, item.Title)}
	}
	f, err := openAsset(filepath.Join(k.path, storage))
	if err != nil {
		item.Outcome = "unsupported_but_retained"
		item.Reason += "; source unavailable"
		return []ImportBlock{fallbackImportBlock(item, item.Title)}
	}
	defer f.Close()
	out, err := ExtractDocument(ctx, f, item.Locator, media)
	if err != nil {
		item.Outcome = "unsupported_but_retained"
		item.Reason += "; " + err.Error()
		return []ImportBlock{fallbackImportBlock(item, item.Title)}
	}
	if out.Warning != "" {
		item.Outcome = "partial"
		item.Reason += "; " + out.Warning
	}
	blocks := []ImportBlock{}
	for _, section := range out.Sections {
		if section.Heading != "" {
			blocks = append(blocks, ImportBlock{NewID(), "heading_2", map[string]any{"richText": richImport(section.Heading)}})
		}
		for _, line := range strings.Split(section.Text, "\n") {
			if strings.TrimSpace(line) != "" {
				blocks = append(blocks, ImportBlock{NewID(), "paragraph", map[string]any{"richText": richImport(line)}})
			}
		}
	}
	return blocks
}

func importHTMLTable(table *html.Node, rich func(*html.Node) []any) (map[string]any, bool) {
	rows := []*html.Node{}
	var collect func(*html.Node)
	collect = func(n *html.Node) {
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			if c.Type != html.ElementNode {
				continue
			}
			if c.Data == "tr" {
				rows = append(rows, c)
			} else if c.Data == "tbody" || c.Data == "thead" || c.Data == "tfoot" {
				collect(c)
			}
		}
	}
	collect(table)
	if len(rows) == 0 || len(rows) > 1000 {
		return nil, false
	}
	occupied := map[[2]int]bool{}
	cells := []any{}
	cols := 0
	headerRows := 0
	for row, tr := range rows {
		col := 0
		headers := true
		count := 0
		for td := tr.FirstChild; td != nil; td = td.NextSibling {
			if td.Type != html.ElementNode || (td.Data != "td" && td.Data != "th") {
				continue
			}
			count++
			headers = headers && td.Data == "th"
			for occupied[[2]int{row, col}] {
				col++
			}
			span := func(key string) int {
				value := htmlImportAttr(td, key)
				if value == "" {
					return 1
				}
				n, err := strconv.Atoi(value)
				if err != nil {
					return -1
				}
				return n
			}
			rs, cs := span("rowspan"), span("colspan")
			if rs < 1 || cs < 1 || rs > 1000 || cs > 256 || row+rs > len(rows) || col+cs > 256 {
				return nil, false
			}
			for r := row; r < row+rs; r++ {
				for c := col; c < col+cs; c++ {
					key := [2]int{r, c}
					if occupied[key] {
						return nil, false
					}
					occupied[key] = true
				}
			}
			cells = append(cells, map[string]any{"id": NewID(), "row": row, "column": col, "rowSpan": rs, "colSpan": cs, "richText": rich(td), "preMergeCells": []any{}})
			col += cs
			if col > cols {
				cols = col
			}
		}
		if headers && count > 0 && headerRows == row {
			headerRows++
		}
	}
	if cols == 0 {
		return nil, false
	}
	for r := range rows {
		for c := 0; c < cols; c++ {
			if !occupied[[2]int{r, c}] {
				cells = append(cells, map[string]any{"id": NewID(), "row": r, "column": c, "rowSpan": 1, "colSpan": 1, "richText": richImport(""), "preMergeCells": []any{}})
			}
		}
	}
	widths := []any{}
	for c := 0; c < cols; c++ {
		widths = append(widths, 160)
	}
	return map[string]any{"rowCount": len(rows), "columnCount": cols, "headerRows": headerRows, "headerColumns": 0, "columnWidths": widths, "cells": cells}, true
}
