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
	omitted, unresolved := 0, 0
	var spans func(*html.Node, []string, string, string, string) []any
	spans = func(n *html.Node, marks []string, link, color, highlight string) []any {
		if n.Type == html.TextNode {
			if n.Data == "" {
				return nil
			}
			s := map[string]any{"text": n.Data}
			if color != "" {
				s["color"] = color
			}
			if highlight != "" {
				s["highlight"] = highlight
			}
			if len(marks) > 0 {
				s["marks"] = append([]string{}, marks...)
			}
			if link != "" {
				s["link"] = link
			}
			return []any{s}
		}
		if n.Type == html.ElementNode {
			for _, field := range []string{"color", "highlight"} {
				if raw := htmlImportAttr(n, "data-trove-"+field); raw != "" {
					if _, ok := richForeground[raw]; ok {
						if field == "color" {
							color = raw
						} else {
							highlight = raw
						}
					} else {
						omitted++
					}
				}
			}
			if n.Data == "mark" && highlight == "" {
				highlight = "yellow"
			}
			for _, declaration := range strings.Split(htmlImportAttr(n, "style"), ";") {
				pair := strings.SplitN(declaration, ":", 2)
				if len(pair) != 2 {
					continue
				}
				key := strings.ToLower(strings.TrimSpace(pair[0]))
				isHighlight := key == "background-color" || key == "background"
				if key != "color" && !isHighlight {
					continue
				}
				if token := richColorToken(pair[1], isHighlight); token != "" {
					if isHighlight {
						highlight = token
					} else {
						color = token
					}
				} else {
					omitted++
				}
			}
			switch n.Data {
			case "script", "style", "iframe", "object", "embed", "template":
				omitted++
				return nil
			case "strong", "b":
				marks = append(append([]string{}, marks...), "bold")
			case "em", "i":
				marks = append(append([]string{}, marks...), "italic")
			case "u":
				marks = append(append([]string{}, marks...), "underline")
			case "del", "s":
				marks = append(append([]string{}, marks...), "strike")
			case "code":
				marks = append(append([]string{}, marks...), "code")
			case "br":
				span := map[string]any{"text": "\n"}
				if color != "" {
					span["color"] = color
				}
				if highlight != "" {
					span["highlight"] = highlight
				}
				if len(marks) > 0 {
					span["marks"] = append([]string{}, marks...)
				}
				if link != "" {
					span["link"] = link
				}
				return []any{span}
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
			out = append(out, spans(c, marks, link, color, highlight)...)
		}
		return out
	}
	blocks, structuralOmissions := importHTMLTree(root, item, pages, assets, func(n *html.Node) []any { return spans(n, nil, "", "", "") })
	omitted += structuralOmissions
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
	rowHeaderColumns := []int{}
	for row, tr := range rows {
		col := 0
		headers := true
		leadingHeaders := 0
		count := 0
		for td := tr.FirstChild; td != nil; td = td.NextSibling {
			if td.Type != html.ElementNode || (td.Data != "td" && td.Data != "th") {
				continue
			}
			count++
			headers = headers && td.Data == "th" && htmlImportAttr(td, "scope") != "row"
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
			if td.Data == "th" && htmlImportAttr(td, "scope") != "col" && col == leadingHeaders {
				leadingHeaders += cs
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
		rowHeaderColumns = append(rowHeaderColumns, leadingHeaders)
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
	headerColumns := cols
	for row := headerRows; row < len(rows); row++ {
		if rowHeaderColumns[row] < headerColumns {
			headerColumns = rowHeaderColumns[row]
		}
	}
	if headerRows == len(rows) {
		headerColumns = 0
	}
	widths := []any{}
	var collectWidths func(*html.Node)
	collectWidths = func(parent *html.Node) {
		for child := parent.FirstChild; child != nil; child = child.NextSibling {
			if child.Data == "colgroup" {
				collectWidths(child)
			} else if child.Data == "col" {
				raw := htmlImportAttr(child, "width")
				for _, declaration := range strings.Split(htmlImportAttr(child, "style"), ";") {
					pair := strings.SplitN(declaration, ":", 2)
					if len(pair) == 2 && strings.TrimSpace(strings.ToLower(pair[0])) == "width" {
						raw = strings.TrimSpace(pair[1])
					}
				}
				width, err := strconv.ParseFloat(strings.TrimSuffix(raw, "px"), 64)
				if err != nil || width <= 0 || width > 100000 {
					width = 160
				}
				span, err := strconv.Atoi(htmlImportAttr(child, "span"))
				if err != nil || span < 1 || span > cols {
					span = 1
				}
				for i := 0; i < span && len(widths) < cols; i++ {
					widths = append(widths, width)
				}
			}
		}
	}
	collectWidths(table)
	for len(widths) < cols {
		widths = append(widths, 160)
	}
	return map[string]any{"rowCount": len(rows), "columnCount": cols, "headerRows": headerRows, "headerColumns": headerColumns, "columnWidths": widths, "cells": cells}, true
}
