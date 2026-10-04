package workspace

import (
	"archive/zip"
	"context"
	"database/sql"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"html"
	"os"
	"path"
	"regexp"
	"strconv"
	"strings"
	"trove.local/phase0/contracts"
)

type PortableExportResult struct {
	Path            string   `json:"path"`
	Pages           int      `json:"pages"`
	Collections     int      `json:"collections"`
	Assets          int      `json:"assets"`
	SourceOmissions []string `json:"sourceOmissions"`
}

var exportUnsafeName = regexp.MustCompile(`[^\pL\pN ._-]+`)

func exportName(title, id string) string {
	title = exportUnsafeName.ReplaceAllString(title, "_")
	title = strings.Trim(title, " .")
	if len(title) > 100 {
		title = string([]rune(title)[:min(len([]rune(title)), 60)])
	}
	if title == "" {
		title = "Untitled"
	}
	if id == "" {
		return title
	}
	return title + " " + strings.ReplaceAll(id, "-", "")
}

// ExportPortable emits readable Markdown/CSV and a versioned manifest carrying
// exact canonical blocks, typed cells, views, provenance and attachment hashes.
// The destination is provided only by a trusted native save-dialog adapter.
func (k *Kernel) ExportPortable(ctx context.Context, e contracts.ServiceExecutionContext, destination string) (result PortableExportResult, err error) {
	if err = requireOwnerScope(k, e); err != nil {
		return result, err
	}
	result.Path = destination
	result.SourceOmissions = []string{"Portable export excludes trash, mutation history, approval grants, and model packs; use a native backup for full workspace restoration."}
	result.SourceOmissions = append(result.SourceOmissions, portableEvidencePolicy)
	tx, err := k.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return result, err
	}
	defer tx.Rollback()
	rows, err := tx.QueryContext(ctx, "SELECT id FROM pages WHERE deleted_at IS NULL ORDER BY id")
	if err != nil {
		return result, err
	}
	pageIDs := []string{}
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return result, err
		}
		pageIDs = append(pageIDs, id)
	}
	rows.Close()
	pages := map[string]contracts.ServiceReadPageResult{}
	livePageIDs := []string{}
	for _, id := range pageIDs {
		p, er := readPageTx(ctx, tx, id)
		if er != nil {
			return result, er
		}
		inventory, er := k.portableInventoryPage(ctx, tx, p)
		if er != nil {
			return result, er
		}
		if inventory {
			continue
		}
		pages[id] = p
		livePageIDs = append(livePageIDs, id)
	}
	pageIDs = livePageIDs
	paths := map[string]string{}
	var buildPath func(string, int) string
	buildPath = func(id string, depth int) string {
		if old := paths[id]; old != "" {
			return old
		}
		p := pages[id]
		dir := "pages"
		if p.Page.ParentPageId != nil && depth < 256 {
			parent := string(*p.Page.ParentPageId)
			if _, ok := pages[parent]; ok {
				dir = strings.TrimSuffix(buildPath(parent, depth+1), ".md")
			}
		}
		name := path.Join(dir, exportName(p.Page.Title, id)+".md")
		paths[id] = name
		return name
	}
	for _, id := range pageIDs {
		buildPath(id, 0)
	}
	for _, id := range pageIDs {
		for _, block := range pages[id].Blocks {
			paths["block:"+string(block.Id)] = paths[id] + "#" + string(block.Id)
		}
	}
	filePaths := map[string]string{}
	phase4Metadata, err := exportPhase4Metadata(ctx, tx)
	if err != nil {
		return result, err
	}
	assets, evidence, err := k.portableAssets(ctx, tx, pages, phase4Metadata)
	if err != nil {
		return result, err
	}
	for _, asset := range assets {
		f := asset.metadata
		filePaths[string(f.Id)] = "attachments/" + string(f.Id) + "/" + exportName(f.OriginalName, "")
	}
	out, err := os.OpenFile(destination, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return result, err
	}
	complete := false
	defer func() {
		out.Close()
		if !complete {
			os.Remove(destination)
		}
	}()
	zw := zip.NewWriter(out)
	write := func(name string, b []byte) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		w, er := zw.Create(name)
		if er != nil {
			return er
		}
		_, er = w.Write(b)
		return er
	}
	manifest := map[string]any{"format": "trove-portable", "formatVersion": 1, "workspaceId": k.id, "exportedAt": now(), "sourceCompleteness": "partial", "sourceOmissions": result.SourceOmissions, "pages": []any{}, "collections": []any{}, "attachments": []any{}}
	for _, id := range pageIDs {
		p := pages[id]
		name := paths[id]
		text := "# " + p.Page.Title + "\n\n"
		for _, block := range p.Blocks {
			text += "<a id=\"" + string(block.Id) + "\"></a>\n" + exportBlockMarkdown(block, name, paths, filePaths) + "\n\n"
		}
		if err = write(name, []byte(text)); err != nil {
			return result, err
		}
		parentPath := ""
		if p.Page.ParentPageId != nil {
			parentPath = paths[string(*p.Page.ParentPageId)]
		}
		display, displayErr := readPageDisplay(ctx, tx, id)
		if displayErr != nil {
			return result, displayErr
		}
		manifest["pages"] = append(manifest["pages"].([]any), map[string]any{"displayPreferences": display, "id": id, "path": name, "parentPath": parentPath, "provenance": p.Page.Provenance, "canonicalPage": p.Page, "canonicalBlocks": p.Blocks})
		result.Pages++
	}
	rows, err = tx.QueryContext(ctx, "SELECT id,owning_page_id,title,title_property_id FROM collections ORDER BY id")
	if err != nil {
		return result, err
	}
	type col struct{ id, page, title, titleProperty string }
	collections := []col{}
	for rows.Next() {
		var c col
		if err = rows.Scan(&c.id, &c.page, &c.title, &c.titleProperty); err != nil {
			rows.Close()
			return result, err
		}
		if _, ok := pages[c.page]; ok {
			collections = append(collections, c)
		}
	}
	rows.Close()
	for _, collection := range collections {
		cols, er := tx.QueryContext(ctx, "SELECT id FROM properties WHERE collection_id=? ORDER BY CASE WHEN id=? THEN 0 ELSE 1 END,id", collection.id, collection.titleProperty)
		if er != nil {
			return result, er
		}
		propIDs := []string{}
		for cols.Next() {
			var id string
			if er = cols.Scan(&id); er != nil {
				cols.Close()
				return result, er
			}
			propIDs = append(propIDs, id)
		}
		cols.Close()
		properties := []contracts.DomainPropertyDefinition{}
		header := []string{}
		for _, id := range propIDs {
			p, er := propertyDefinition(ctx, tx, id)
			if er != nil {
				return result, er
			}
			properties = append(properties, p)
			header = append(header, p.Name)
		}
		var csvText strings.Builder
		cw := csv.NewWriter(&csvText)
		cw.Write(header)
		records, er := tx.QueryContext(ctx, "SELECT r.id,r.page_id,p.title FROM records r JOIN pages p ON p.id=r.page_id WHERE r.collection_id=? AND p.deleted_at IS NULL ORDER BY r.sort_key", collection.id)
		if er != nil {
			return result, er
		}
		type record struct{ id, page, title string }
		rs := []record{}
		for records.Next() {
			var r record
			if er = records.Scan(&r.id, &r.page, &r.title); er != nil {
				records.Close()
				return result, er
			}
			rs = append(rs, r)
		}
		records.Close()
		canonical := []any{}
		for _, record := range rs {
			cells := []string{record.title}
			values := map[string]any{}
			for _, prop := range properties[1:] {
				var raw string
				er := tx.QueryRowContext(ctx, "SELECT value_json FROM property_values WHERE record_id=? AND property_id=?", record.id, prop.Id).Scan(&raw)
				if er == sql.ErrNoRows {
					cells = append(cells, "")
					continue
				}
				if er != nil {
					return result, er
				}
				var v any
				if er = json.Unmarshal([]byte(raw), &v); er != nil {
					return result, er
				}
				values[string(prop.Id)] = v
				cells = append(cells, exportCellText(ctx, tx, prop.Type, v))
			}
			cw.Write(cells)
			canonical = append(canonical, map[string]any{"id": record.id, "pageId": record.page, "pagePath": paths[record.page], "values": values})
		}
		cw.Flush()
		if er = cw.Error(); er != nil {
			return result, er
		}
		name := strings.TrimSuffix(paths[collection.page], ".md") + ".csv"
		if err = write(name, []byte(csvText.String())); err != nil {
			return result, err
		}
		views, er := tx.QueryContext(ctx, "SELECT id,name,type,schema_version,query_json,config_json,owning_page_id FROM views WHERE collection_id=? ORDER BY id", collection.id)
		if er != nil {
			return result, er
		}
		vs := []any{}
		for views.Next() {
			var id, name, kind, query, config, viewPage string
			var version int
			if er = views.Scan(&id, &name, &kind, &version, &query, &config, &viewPage); er != nil {
				views.Close()
				return result, er
			}
			if _, live := pages[viewPage]; !live {
				continue
			}
			vs = append(vs, map[string]any{"id": id, "name": name, "type": kind, "schemaVersion": version, "query": json.RawMessage(query), "config": json.RawMessage(config), "owningPagePath": paths[viewPage]})
		}
		views.Close()
		manifest["collections"] = append(manifest["collections"].([]any), map[string]any{"id": collection.id, "title": collection.title, "path": name, "owningPagePath": paths[collection.page], "properties": properties, "records": canonical, "views": vs})
		result.Collections++
	}
	for _, asset := range assets {
		f := asset.metadata
		if err = ctx.Err(); err != nil {
			return result, err
		}
		name := filePaths[string(f.Id)]
		if asset.content != nil {
			if err = write(name, asset.content); err != nil {
				return result, err
			}
			manifest["attachments"] = append(manifest["attachments"].([]any), map[string]any{"id": f.Id, "path": name, "sha256": f.ContentHash, "sizeBytes": f.SizeBytes, "mediaType": f.MediaType, "originalName": f.OriginalName})
			result.Assets++
			continue
		}
		full := k.path + "/" + f.StorageKey
		size, er := strconv.ParseInt(f.SizeBytes, 10, 64)
		if er != nil {
			return result, er
		}
		if er = verifyAsset(full, string(f.ContentHash), size); er != nil {
			return result, er
		}
		input, er := openAsset(full)
		if er != nil {
			return result, er
		}
		w, er := zw.Create(name)
		if er == nil {
			_, er = copyBounded(ctx, w, input, maxAssetBytes)
		}
		input.Close()
		if er != nil {
			return result, er
		}
		manifest["attachments"] = append(manifest["attachments"].([]any), map[string]any{"id": f.Id, "path": name, "sha256": f.ContentHash, "sizeBytes": f.SizeBytes, "mediaType": f.MediaType, "originalName": f.OriginalName})
		result.Assets++
	}
	// Evidence is scoped to exported pages; a complete import inventory may
	// contain trashed pages, rejected members and private historical values.
	manifest["importEvidence"] = evidence
	manifest["phase4Metadata"] = phase4Metadata
	if err = write("trove-manifest.json", marshal(manifest)); err != nil {
		return result, err
	}
	if err = zw.Close(); err != nil {
		return result, err
	}
	if err = out.Sync(); err != nil {
		return result, err
	}
	if err = out.Close(); err != nil {
		return result, err
	}
	complete = true
	return result, nil
}
func relativeExportLink(source, target string) string {
	a, b := strings.Split(path.Dir(source), "/"), strings.Split(target, "/")
	for len(a) > 0 && len(b) > 0 && a[0] == b[0] {
		a = a[1:]
		b = b[1:]
	}
	return strings.Repeat("../", len(a)) + strings.Join(b, "/")
}
func exportRichText(v any, source string, pages, files map[string]string) string {
	parts, _ := v.([]any)
	var out strings.Builder
	for _, raw := range parts {
		span, _ := raw.(map[string]any)
		if str(span["color"]) != "" || str(span["highlight"]) != "" {
			out.WriteString(exportRichHTML([]any{span}, source, pages, files))
			continue
		}
		text := str(span["text"])
		if marks, ok := span["marks"].([]any); ok {
			for _, mark := range marks {
				switch str(mark) {
				case "bold":
					text = "**" + text + "**"
				case "italic":
					text = "*" + text + "*"
				case "strike":
					text = "~~" + text + "~~"
				case "code":
					text = "`" + text + "`"
				}
			}
		}
		if link := str(span["link"]); link != "" {
			if strings.HasPrefix(link, "trove://page/") {
				link = portablePageLink(link, source, pages)
			} else if strings.HasPrefix(link, "/files/") {
				if target := files[strings.TrimPrefix(link, "/files/")]; target != "" {
					link = relativeExportLink(source, target)
				}
			}
			text = "[" + text + "](" + strings.ReplaceAll(link, " ", "%20") + ")"
		}
		out.WriteString(text)
	}
	return out.String()
}
func exportBlockMarkdown(block contracts.DomainBlock, source string, pages, files map[string]string) string {
	var data map[string]any
	_ = json.Unmarshal(block.Data, &data)
	text := exportRichText(data["richText"], source, pages, files)
	switch block.Type {
	case "paragraph":
		return text
	case "heading_1":
		return "# " + text
	case "heading_2":
		return "## " + text
	case "heading_3":
		return "### " + text
	case "heading_4":
		return "#### " + text
	case "bookmark":
		return "[" + str(data["title"]) + "](" + str(data["url"]) + ")\n\n" + str(data["description"])
	case "embed":
		return str(data["offlineText"]) + "\n\n[" + str(data["provider"]) + "](" + str(data["url"]) + ")"
	case "page_mention":
		return "[" + str(data["displaySnapshot"]) + "](" + relativeExportLink(source, pages[str(data["targetPageId"])]) + ")"
	case "date_mention":
		return str(data["start"]) + " " + str(data["end"]) + " (" + str(data["timezone"]) + ")"
	case "button":
		return str(data["label"]) + " (workspace action; inactive in export)"
	case "synced_block":
		target := pages["block:"+str(data["contentSourceId"])]
		if target != "" {
			return "[Reusable content source](" + relativeExportLink(source, target) + ")"
		}
		return "Reusable content source: " + str(data["contentSourceId"])
	case "tab_container", "columns":
		key := "tabs"
		if block.Type == "columns" {
			key = "columns"
		}
		groups, _ := data[key].([]any)
		parts := []string{}
		for i, raw := range groups {
			group, _ := raw.(map[string]any)
			label := str(group["title"])
			if label == "" {
				label = "Column " + strconv.Itoa(i+1)
			}
			parts = append(parts, "### "+label)
			children, _ := group["childBlockIds"].([]any)
			for _, id := range children {
				if target := pages["block:"+str(id)]; target != "" {
					parts = append(parts, "[Content]("+relativeExportLink(source, target)+")")
				}
			}
		}
		return strings.Join(parts, "\n\n")
	case "table":
		return exportTableHTML(data, source, pages, files)
	case "breadcrumb":
		return "Page trail: " + source
	case "table_of_contents":
		return "Table of contents: see page headings"
	case "quote":
		return "> " + strings.ReplaceAll(text, "\n", "\n> ")
	case "bulleted_list_item":
		return "- " + text
	case "numbered_list_item":
		return "1. " + text
	case "divider":
		return "---"
	case "code":
		fence := "```"
		for strings.Contains(str(data["source"]), fence) {
			fence += "`"
		}
		return fence + str(data["language"]) + "\n" + str(data["source"]) + "\n" + fence
	case "unsupported_external":
		return str(data["readableText"]) + "\n\n[Original source](" + strings.ReplaceAll(relativeExportLink(source, files[str(data["rawFileId"])]), " ", "%20") + ")"
	case "image", "audio", "video", "pdf", "file":
		prefix := ""
		if block.Type == "image" {
			prefix = "!"
		}
		caption := exportRichText(data["caption"], source, pages, files)
		if caption == "" {
			caption = block.Type
		}
		return prefix + "[" + caption + "](" + strings.ReplaceAll(relativeExportLink(source, files[str(data["fileId"])]), " ", "%20") + ")"
	default:
		pretty, _ := json.MarshalIndent(data, "", "  ")
		return "```json\n" + string(pretty) + "\n```"
	}
}
func exportCellText(ctx context.Context, q querier, kind string, v any) string {
	switch kind {
	case "text":
		return exportRichText(v, "", nil, nil)
	case "number":
		obj, _ := v.(map[string]any)
		return str(obj["decimal"])
	case "date":
		obj, _ := v.(map[string]any)
		return str(obj["start"])
	case "checkbox":
		if v == true {
			return "true"
		}
		return "false"
	case "select", "status":
		var label string
		if q.QueryRowContext(ctx, "SELECT label FROM property_options WHERE id=?", str(v)).Scan(&label) == nil {
			return label
		}
	case "multi_select":
		values, _ := v.([]any)
		labels := []string{}
		for _, id := range values {
			var label string
			if q.QueryRowContext(ctx, "SELECT label FROM property_options WHERE id=?", str(id)).Scan(&label) == nil {
				labels = append(labels, label)
			}
		}
		return strings.Join(labels, ", ")
	case "url", "email", "phone":
		return str(v)
	}
	b, _ := json.Marshal(v)
	return string(b)
}

// Escape content before emitting the finite presentation vocabulary. This is
// readable outside Trove while the manifest retains exact canonical structure.
func exportRichHTML(v any, source string, pages, files map[string]string) string {
	parts, _ := v.([]any)
	var out strings.Builder
	for _, raw := range parts {
		span, _ := raw.(map[string]any)
		text := strings.ReplaceAll(html.EscapeString(str(span["text"])), "\n", "<br>")
		marks, _ := span["marks"].([]any)
		for _, mark := range marks {
			if tag := map[string]string{"bold": "strong", "italic": "em", "underline": "u", "strike": "s", "code": "code"}[str(mark)]; tag != "" {
				text = "<" + tag + ">" + text + "</" + tag + ">"
			}
		}
		attributes := ""
		styles := []string{}
		if token := str(span["color"]); richForeground[token] != "" {
			attributes += ` data-trove-color="` + token + `"`
			styles = append(styles, "color:"+richForeground[token])
		}
		if token := str(span["highlight"]); richBackground[token] != "" {
			attributes += ` data-trove-highlight="` + token + `"`
			styles = append(styles, "background-color:"+richBackground[token])
		}
		if len(styles) > 0 {
			text = "<span" + attributes + ` style="` + strings.Join(styles, ";") + `">` + text + "</span>"
		}
		if link := str(span["link"]); link != "" {
			if strings.HasPrefix(link, "trove://page/") {
				link = portablePageLink(link, source, pages)
			} else if strings.HasPrefix(link, "/files/") {
				if target := files[strings.TrimPrefix(link, "/files/")]; target != "" {
					link = relativeExportLink(source, target)
				}
			}
			if safeImportURL(link) {
				text = `<a href="` + html.EscapeString(strings.ReplaceAll(link, " ", "%20")) + `">` + text + "</a>"
			}
		}
		out.WriteString(text)
	}
	return out.String()
}
func exportTableHTML(data map[string]any, source string, pages, files map[string]string) string {
	var out strings.Builder
	out.WriteString("<table><colgroup>")
	widths, _ := data["columnWidths"].([]any)
	for column := int64(0); column < number(data["columnCount"]); column++ {
		width := int64(180)
		if int(column) < len(widths) && number(widths[column]) > 0 {
			width = number(widths[column])
		}
		out.WriteString(fmt.Sprintf(`<col style="width:%dpx">`, width))
	}
	out.WriteString("</colgroup><tbody>")
	cells, _ := data["cells"].([]any)
	positions := map[[2]int64]map[string]any{}
	for _, raw := range cells {
		cell, _ := raw.(map[string]any)
		positions[[2]int64{number(cell["row"]), number(cell["column"])}] = cell
	}
	for row := int64(0); row < number(data["rowCount"]); row++ {
		out.WriteString("<tr>")
		for column := int64(0); column < number(data["columnCount"]); column++ {
			cell, exists := positions[[2]int64{row, column}]
			if !exists {
				continue
			}
			tag, scope := "td", ""
			if row < number(data["headerRows"]) {
				tag, scope = "th", ` scope="col"`
			} else if column < number(data["headerColumns"]) {
				tag, scope = "th", ` scope="row"`
			}
			out.WriteString(fmt.Sprintf(`<%s%s rowspan="%d" colspan="%d">`, tag, scope, number(cell["rowSpan"]), number(cell["colSpan"])))
			out.WriteString(exportRichHTML(cell["richText"], source, pages, files))
			out.WriteString("</" + tag + ">")
		}
		out.WriteString("</tr>")
	}
	out.WriteString("</tbody></table>")
	return out.String()
}
