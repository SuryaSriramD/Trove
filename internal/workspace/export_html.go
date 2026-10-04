package workspace

import (
	"archive/zip"
	"context"
	"encoding/json"
	"html"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"

	"trove.local/phase0/contracts"
)

// ExportHTML adds offline, script-free HTML pages to the canonical portable
// archive. The original Markdown, database CSV and exact canonical manifest
// remain present so a rendering fallback cannot discard structured content.
func (k *Kernel) ExportHTML(ctx context.Context, e contracts.ServiceExecutionContext, destination string) (PortableExportResult, error) {
	var out PortableExportResult
	if err := requireOwnerScope(k, e); err != nil {
		return out, err
	}
	temp, err := os.CreateTemp(filepath.Join(k.path, ".staging"), "html-export-*.zip")
	if err != nil {
		return out, err
	}
	temp.Close()
	os.Remove(temp.Name())
	defer os.Remove(temp.Name())
	out, err = k.ExportPortable(ctx, e, temp.Name())
	if err != nil {
		return out, err
	}
	input, err := zip.OpenReader(temp.Name())
	if err != nil {
		return out, err
	}
	defer input.Close()
	var manifest struct {
		Pages []struct {
			ID     string                  `json:"id"`
			Path   string                  `json:"path"`
			Page   contracts.DomainPage    `json:"canonicalPage"`
			Blocks []contracts.DomainBlock `json:"canonicalBlocks"`
		} `json:"pages"`
		Attachments []struct {
			ID   string `json:"id"`
			Path string `json:"path"`
		} `json:"attachments"`
	}
	for _, entry := range input.File {
		if entry.Name == "trove-manifest.json" {
			r, er := entry.Open()
			if er != nil {
				return out, er
			}
			er = json.NewDecoder(r).Decode(&manifest)
			r.Close()
			if er != nil {
				return out, er
			}
		}
	}
	target, err := os.OpenFile(destination, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return out, err
	}
	complete := false
	defer func() {
		target.Close()
		if !complete {
			os.Remove(destination)
		}
	}()
	zw := zip.NewWriter(target)
	for _, entry := range input.File {
		if err = ctx.Err(); err != nil {
			return out, err
		}
		r, er := entry.Open()
		if er != nil {
			return out, er
		}
		w, er := zw.Create(entry.Name)
		if er == nil {
			if entry.Name == "trove-manifest.json" {
				var exact map[string]any
				er = json.NewDecoder(r).Decode(&exact)
				if er == nil {
					renditions := []string{"index.html"}
					for _, p := range manifest.Pages {
						renditions = append(renditions, strings.TrimSuffix(p.Path, ".md")+".html")
					}
					exact["htmlRenditions"] = renditions
					_, er = w.Write(marshal(exact))
				}
			} else {
				_, er = io.Copy(w, r)
			}
		}
		r.Close()
		if er != nil {
			return out, er
		}
	}
	pages, files := map[string]string{}, map[string]string{}
	for _, p := range manifest.Pages {
		pages[p.ID] = strings.TrimSuffix(p.Path, ".md") + ".html"
	}
	for _, p := range manifest.Pages {
		for _, b := range p.Blocks {
			pages["block:"+string(b.Id)] = pages[p.ID] + "#" + string(b.Id)
		}
	}
	for _, f := range manifest.Attachments {
		files[f.ID] = f.Path
	}
	index := "<h1>Trove export</h1><p>Offline pages with canonical source, CSV databases and attachments.</p><ul>"
	for _, p := range manifest.Pages {
		file := pages[p.ID]
		body := "<h1>" + html.EscapeString(p.Page.Title) + "</h1>"
		for _, block := range p.Blocks {
			body += exportBlockHTML(block, file, pages, files)
		}
		w, er := zw.Create(file)
		if er == nil {
			_, er = io.WriteString(w, exportHTMLDocument(p.Page.Title, body))
		}
		if er != nil {
			return out, er
		}
		index += "<li><a href=\"" + html.EscapeString(strings.ReplaceAll(file, " ", "%20")) + "\">" + html.EscapeString(p.Page.Title) + "</a></li>"
	}
	w, err := zw.Create("index.html")
	if err == nil {
		_, err = io.WriteString(w, exportHTMLDocument("Trove export", index+"</ul>"))
	}
	if err != nil {
		return out, err
	}
	if err = zw.Close(); err != nil {
		return out, err
	}
	if err = target.Sync(); err != nil {
		return out, err
	}
	if err = target.Close(); err != nil {
		return out, err
	}
	complete = true
	out.Path = destination
	return out, nil
}
func exportHTMLDocument(title, body string) string {
	return "<!doctype html><html lang=\"en\"><head><meta charset=\"utf-8\"><meta http-equiv=\"Content-Security-Policy\" content=\"default-src 'none'; img-src 'self' data:; media-src 'self'; style-src 'unsafe-inline'\"><title>" + html.EscapeString(title) + "</title><style>body{max-width:900px;margin:3rem auto;padding:0 1rem;font:16px/1.6 system-ui}pre{white-space:pre-wrap;overflow-wrap:anywhere;padding:1rem;background:#f3f4f6}img,video{max-width:100%}blockquote{border-left:3px solid #bbb;padding-left:1rem}a{overflow-wrap:anywhere}@media print{body{margin:0;max-width:none}pre{break-inside:avoid}}</style></head><body>" + body + "</body></html>"
}
func exportBlockHTML(block contracts.DomainBlock, source string, pages, files map[string]string) string {
	return "<div id=\"" + html.EscapeString(string(block.Id)) + "\">" + exportBlockHTMLContent(block, source, pages, files) + "</div>"
}
func exportBlockHTMLContent(block contracts.DomainBlock, source string, pages, files map[string]string) string {
	var d map[string]any
	json.Unmarshal(block.Data, &d)
	rich := func(value any) string {
		spans, _ := value.([]any)
		out := ""
		for _, raw := range spans {
			s, _ := raw.(map[string]any)
			text := html.EscapeString(str(s["text"]))
			marks, _ := s["marks"].([]any)
			for _, rawMark := range marks {
				tag := map[string]string{"bold": "strong", "italic": "em", "strike": "s", "code": "code"}[str(rawMark)]
				if tag != "" {
					text = "<" + tag + ">" + text + "</" + tag + ">"
				}
			}
			link := str(s["link"])
			link = portablePageLink(link, source, pages)
			if target := files[strings.TrimPrefix(link, "/files/")]; target != "" {
				link = relativeExportLink(source, target)
			}
			if link != "" && safeImportURL(link) {
				text = "<a href=\"" + html.EscapeString(strings.ReplaceAll(link, " ", "%20")) + "\">" + text + "</a>"
			}
			out += text
		}
		return out
	}
	text := rich(d["richText"])
	tag := map[string]string{"paragraph": "p", "heading_1": "h1", "heading_2": "h2", "heading_3": "h3", "heading_4": "h4", "quote": "blockquote", "bulleted_list_item": "p", "numbered_list_item": "p"}[block.Type]
	if tag != "" {
		return "<" + tag + ">" + text + "</" + tag + ">"
	}
	switch block.Type {
	case "synced_block":
		if target := pages["block:"+str(d["contentSourceId"])]; target != "" {
			return "<p><a href=\"" + html.EscapeString(strings.ReplaceAll(relativeExportLink(source, target), " ", "%20")) + "\">Reusable content source</a></p>"
		}
		return "<p>Unavailable reusable source: " + html.EscapeString(str(d["contentSourceId"])) + "</p>"
	case "page_mention":
		return "<p>" + rich([]any{map[string]any{"text": str(d["displaySnapshot"]), "link": canonicalPageLink(str(d["targetPageId"]), "")}}) + "</p>"
	case "tab_container", "columns":
		key := "tabs"
		if block.Type == "columns" {
			key = "columns"
		}
		groups, _ := d[key].([]any)
		out := "<section>"
		for _, raw := range groups {
			group, _ := raw.(map[string]any)
			label := str(group["title"])
			if label == "" {
				label = "Column"
			}
			out += "<h3>" + html.EscapeString(label) + "</h3>"
			children, _ := group["childBlockIds"].([]any)
			for _, child := range children {
				if target := pages["block:"+str(child)]; target != "" {
					out += "<p><a href=\"" + html.EscapeString(strings.ReplaceAll(relativeExportLink(source, target), " ", "%20")) + "\">Content</a></p>"
				}
			}
		}
		return out + "</section>"
	case "bookmark", "embed", "date_mention", "button", "table", "breadcrumb", "table_of_contents":
		return "<section><pre>" + html.EscapeString(exportBlockMarkdown(block, source, pages, files)) + "</pre></section>"
	case "divider":
		return "<hr>"
	case "code":
		return "<pre><code>" + html.EscapeString(str(d["source"])) + "</code></pre>"
	case "image", "file", "audio", "video", "pdf":
		target := files[str(d["fileId"])]
		if target == "" {
			return "<p>Attachment unavailable</p>"
		}
		link := html.EscapeString(strings.ReplaceAll(relativeExportLink(source, target), " ", "%20"))
		if block.Type == "image" {
			return "<figure><img src=\"" + link + "\" alt=\"" + html.EscapeString(path.Base(target)) + "\"><figcaption>" + rich(d["caption"]) + "</figcaption></figure>"
		}
		return "<p><a href=\"" + link + "\">" + html.EscapeString(path.Base(target)) + "</a></p>"
	case "unsupported_external":
		return "<pre>" + html.EscapeString(str(d["readableText"])) + "</pre>"
	}
	pretty, _ := json.MarshalIndent(d, "", "  ")
	return "<details open><summary>" + html.EscapeString(block.Type) + " — canonical content</summary><pre>" + html.EscapeString(string(pretty)) + "</pre></details>"
}
