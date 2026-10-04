package workspace

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"golang.org/x/net/html"
)

func htmlHasClass(n *html.Node, class string) bool {
	for _, value := range strings.Fields(htmlImportAttr(n, "class")) {
		if value == class {
			return true
		}
	}
	return false
}

func htmlStructural(n *html.Node) bool {
	if n.Type != html.ElementNode {
		return false
	}
	switch n.Data {
	case "p", "h1", "h2", "h3", "h4", "h5", "h6", "blockquote", "li", "ul", "ol", "details", "pre", "hr", "img", "audio", "video", "table", "div", "section", "article", "figure":
		return true
	}
	return false
}

// Exclude descendant block content from a container's own inline text. The
// structural walk emits those descendants separately with canonical ownership.
func htmlOwnContent(n *html.Node, root bool) *html.Node {
	if !root && htmlStructural(n) {
		return nil
	}
	copy := &html.Node{Type: n.Type, DataAtom: n.DataAtom, Data: n.Data, Namespace: n.Namespace, Attr: append([]html.Attribute{}, n.Attr...)}
	for child := n.FirstChild; child != nil; child = child.NextSibling {
		if cloned := htmlOwnContent(child, false); cloned != nil {
			copy.AppendChild(cloned)
		}
	}
	return copy
}

func importHTMLTree(root *html.Node, item *ImportItem, pages, assets map[string]string, rich func(*html.Node) []any) ([]ImportBlock, int) {
	blocks := []ImportBlock{}
	item.BlockParents, item.BlockSources = map[string]string{}, map[string]string{}
	omitted := 0
	seenSources := map[string]bool{}
	add := func(n *html.Node, locator, parent, kind string, data map[string]any) string {
		id := NewID()
		blocks = append(blocks, ImportBlock{id, kind, data})
		item.BlockParents[id] = parent
		source := "html-path:" + locator
		if external := htmlImportAttr(n, "id"); external != "" {
			source = "html-id:" + external
			if seenSources[source] {
				source += "@" + locator
				omitted++
			}
		}
		seenSources[source] = true
		item.BlockSources[id] = source
		return id
	}
	var walk func(*html.Node, string, string, int) []string
	var children func(*html.Node, string, string, int, func(*html.Node) bool) []string
	children = func(n *html.Node, parent, locator string, depth int, skip func(*html.Node) bool) []string {
		ids := []string{}
		index := 0
		for child := n.FirstChild; child != nil; child = child.NextSibling {
			index++
			if skip != nil && skip(child) {
				continue
			}
			ids = append(ids, walk(child, parent, fmt.Sprintf("%s/%d", locator, index), depth)...)
		}
		return ids
	}
	walk = func(n *html.Node, parent, locator string, depth int) []string {
		if len(blocks) >= 10000 || depth > 64 {
			omitted++
			return nil
		}
		if n.Type == html.CommentNode {
			omitted++
			return nil
		}
		if n.Type == html.TextNode {
			if strings.TrimSpace(n.Data) == "" {
				return nil
			}
			return []string{add(n, locator, parent, "paragraph", map[string]any{"richText": rich(n)})}
		}
		if n.Type != html.ElementNode {
			return children(n, parent, locator, depth, nil)
		}
		switch n.Data {
		case "head":
			return nil
		case "script", "style", "iframe", "object", "embed", "template":
			omitted++
			return nil
		}
		// Only explicit exported columns and semantic HTML tab panels establish
		// groups. CSS positioning or arbitrary JavaScript never invents a layout.
		layout, groups := "", []*html.Node{}
		if htmlHasClass(n, "column-list") || htmlImportAttr(n, "data-trove-layout") == "columns" {
			layout = "columns"
		}
		if htmlImportAttr(n, "data-trove-layout") == "tabs" {
			layout = "tab_container"
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			if htmlImportAttr(c, "role") == "tablist" {
				layout = "tab_container"
			}
		}
		if layout != "" {
			for c := n.FirstChild; c != nil; c = c.NextSibling {
				if layout == "columns" && (htmlHasClass(c, "column") || htmlImportAttr(c, "data-trove-column") != "") || layout == "tab_container" && (htmlImportAttr(c, "role") == "tabpanel" || htmlImportAttr(c, "data-trove-tab") != "") {
					groups = append(groups, c)
				}
			}
			if len(groups) > 0 {
				key := "columns"
				if layout == "tab_container" {
					key = "tabs"
				}
				data := map[string]any{key: []any{}}
				id := add(n, locator, parent, layout, data)
				items := []any{}
				for i, g := range groups {
					childIDs := children(g, id, fmt.Sprintf("%s/group/%d", locator, i), depth+1, nil)
					group := map[string]any{"id": NewID(), "childBlockIds": childIDs}
					if layout == "columns" {
						width := 1.0
						for _, declaration := range strings.Split(htmlImportAttr(g, "style"), ";") {
							pair := strings.SplitN(declaration, ":", 2)
							if len(pair) == 2 && strings.TrimSpace(pair[0]) == "width" {
								if v, e := strconv.ParseFloat(strings.TrimSuffix(strings.TrimSpace(pair[1]), "%"), 64); e == nil && v > 0 && v <= 100 {
									width = v
								}
							}
						}
						group["widthRatio"] = width
					} else {
						title := htmlImportAttr(g, "aria-label")
						var findLabel func(*html.Node)
						findLabel = func(c *html.Node) {
							if htmlImportAttr(c, "aria-controls") == htmlImportAttr(g, "id") && htmlImportAttr(g, "id") != "" {
								title = strings.TrimSpace(inertHTMLText(c))
							}
							for d := c.FirstChild; d != nil; d = d.NextSibling {
								findLabel(d)
							}
						}
						findLabel(n)
						if title == "" {
							title = htmlImportAttr(g, "data-trove-tab")
						}
						group["title"] = title
					}
					items = append(items, group)
				}
				data[key] = items
				for c := n.FirstChild; c != nil; c = c.NextSibling {
					known := htmlImportAttr(c, "role") == "tablist"
					for _, g := range groups {
						known = known || c == g
					}
					if !known && strings.TrimSpace(inertHTMLText(c)) != "" {
						omitted++
						walk(c, parent, locator+"/ungrouped", depth+1)
					}
				}
				return []string{id}
			}
			omitted++
		}
		kind := ""
		switch n.Data {
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
		case "details":
			content := []any{}
			for c := n.FirstChild; c != nil; c = c.NextSibling {
				if c.Data == "summary" {
					content = rich(c)
					break
				}
			}
			closed := true
			for _, a := range n.Attr {
				if a.Key == "open" {
					closed = false
				}
			}
			id := add(n, locator, parent, "toggle", map[string]any{"richText": content, "collapsed": closed})
			children(n, id, locator, depth+1, func(c *html.Node) bool { return c.Data == "summary" })
			return []string{id}
		case "pre":
			return []string{add(n, locator, parent, "code", map[string]any{"source": inertHTMLText(n), "language": "text"})}
		case "hr":
			return []string{add(n, locator, parent, "divider", map[string]any{})}
		case "table":
			data, ok := importHTMLTable(n, rich)
			if ok {
				return []string{add(n, locator, parent, "table", data)}
			}
			omitted++
			fallback := fallbackImportBlock(item, strings.TrimSpace(inertHTMLText(n)))
			return []string{add(n, locator, parent, fallback.Type, fallback.Data)}
		case "img", "audio", "video":
			target, asset, found := resolveImportLink(item.Locator, htmlImportAttr(n, "src"), pages, assets)
			if found && asset {
				kind := n.Data
				if kind == "img" {
					kind = "image"
				}
				return []string{add(n, locator, parent, kind, map[string]any{"fileId": target, "caption": richImport(htmlImportAttr(n, "alt"))})}
			}
			omitted++
			fallback := fallbackImportBlock(item, "Media unavailable offline: "+htmlImportAttr(n, "alt"))
			return []string{add(n, locator, parent, fallback.Type, fallback.Data)}
		case "input", "button", "form", "canvas", "svg", "track":
			omitted++
		}
		if kind != "" {
			content := rich(htmlOwnContent(n, true))
			id := add(n, locator, parent, kind, map[string]any{"richText": content})
			var descend func(*html.Node, string)
			descend = func(current *html.Node, loc string) {
				i := 0
				for c := current.FirstChild; c != nil; c = c.NextSibling {
					i++
					next := fmt.Sprintf("%s/%d", loc, i)
					if htmlStructural(c) {
						walk(c, id, next, depth+1)
					} else {
						descend(c, next)
					}
				}
			}
			descend(n, locator)
			return []string{id}
		}
		if !htmlStructural(n) && n.Data != "html" && n.Data != "body" && n.Data != "ul" && n.Data != "ol" {
			if text := strings.TrimSpace(inertHTMLText(n)); text != "" {
				return []string{add(n, locator, parent, "paragraph", map[string]any{"richText": rich(n)})}
			}
		}
		return children(n, parent, locator, depth+1, nil)
	}
	walk(root, "", "", 0)
	return blocks, omitted
}

var markdownListPrefix = regexp.MustCompile(`^([-+*]|[0-9]+[.)])\s+(.*)$`)

func parseImportMarkdownTree(text string, item *ImportItem, pages, assets map[string]string) []ImportBlock {
	item.BlockParents, item.BlockSources = map[string]string{}, map[string]string{}
	type ancestor struct {
		indent int
		id     string
	}
	stack := []ancestor{}
	blocks := []ImportBlock{}
	lines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	for i := 0; i < len(lines); i++ {
		line := lines[i]
		trimmed := strings.TrimLeft(line, " \t")
		if trimmed == "" {
			continue
		}
		indent := 0
		for _, c := range line[:len(line)-len(trimmed)] {
			if c == '\t' {
				indent += 4
			} else {
				indent++
			}
		}
		for len(stack) > 0 && indent <= stack[len(stack)-1].indent {
			stack = stack[:len(stack)-1]
		}
		parent := ""
		if len(stack) > 0 {
			parent = stack[len(stack)-1].id
		}
		start := i
		kind := ""
		match := markdownListPrefix.FindStringSubmatch(trimmed)
		if match != nil {
			kind = "bulleted_list_item"
			if match[1][0] >= '0' && match[1][0] <= '9' {
				kind = "numbered_list_item"
			}
			trimmed = "- " + match[2]
		}
		if strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~") {
			fence := trimmed[:3]
			chunk := []string{trimmed}
			for i++; i < len(lines); i++ {
				next := lines[i]
				removed := 0
				for len(next) > 0 && removed < indent && (next[0] == ' ' || next[0] == '\t') {
					if next[0] == '\t' {
						removed += 4
					} else {
						removed++
					}
					next = next[1:]
				}
				chunk = append(chunk, next)
				if strings.HasPrefix(next, fence) {
					break
				}
			}
			trimmed = strings.Join(chunk, "\n")
		}
		parsed := parseImportMarkdown(trimmed, item.Locator, pages, assets, &item.Outcome, &item.Reason)
		for j, b := range parsed {
			if kind != "" {
				b.Type = kind
			}
			blocks = append(blocks, b)
			item.BlockParents[b.ID] = parent
			item.BlockSources[b.ID] = fmt.Sprintf("markdown-line:%d:%d", start+1, j)
			if kind != "" {
				stack = append(stack, ancestor{indent, b.ID})
			}
		}
		if len(stack) > 64 || len(blocks) > 10000 {
			item.Outcome = "partial"
			item.Reason += "; nesting or block limit reached; complete Markdown retained"
			break
		}
	}
	return blocks
}
