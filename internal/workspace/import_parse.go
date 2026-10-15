package workspace

import (
	"context"
	"database/sql"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"path"
	"regexp"
	"strings"
	"time"
	"trove.local/phase0/contracts"
)

var notionIDSuffix = regexp.MustCompile(`(?i)(?: |^)([0-9a-f]{32}|[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12})$`)
var decimalImport = regexp.MustCompile(`^-?(0|[1-9][0-9]*)(\.[0-9]+)?$`)
var markdownLink = regexp.MustCompile(`(!?)\[([^\]]*)\]\(([^)]+)\)`)
var htmlTagImport = regexp.MustCompile(`<[^>]*>`)

func sourceTitle(name string) string {
	base := strings.TrimSuffix(path.Base(name), path.Ext(name))
	base = notionIDSuffix.ReplaceAllString(base, "")
	if strings.TrimSpace(base) == "" {
		return "Untitled"
	}
	return base
}
func importIdentity(name string) (string, string) {
	base := strings.TrimSuffix(path.Base(name), path.Ext(name))
	if m := notionIDSuffix.FindStringSubmatch(base); m != nil {
		return "external_id", strings.ToLower(strings.ReplaceAll(m[1], "-", ""))
	}
	return "derived_locator", path.Clean(name)
}
func richImport(s string) []any { return []any{map[string]any{"text": s}} }
func validImportType(s string) bool {
	return s == "text" || s == "number" || s == "checkbox" || s == "date" || s == "url" || s == "email"
}
func inferImportType(values []string) string {
	count := 0
	num, check, date, urltype := true, true, true, true
	for _, v := range values {
		if v == "" {
			continue
		}
		count++
		num = num && decimalImport.MatchString(v)
		check = check && (strings.EqualFold(v, "true") || strings.EqualFold(v, "false"))
		_, err := time.Parse("2006-01-02", v)
		date = date && err == nil
		u, err := url.Parse(v)
		urltype = urltype && err == nil && u.Host != "" && (u.Scheme == "https" || u.Scheme == "http")
	}
	if count == 0 {
		return "text"
	}
	if check {
		return "checkbox"
	}
	if num {
		return "number"
	}
	if date {
		return "date"
	}
	if urltype {
		return "url"
	}
	return "text"
}

type portableHierarchy struct {
	WorkspaceID            string          `json:"workspaceId"`
	PageEnumerationVersion int             `json:"pageEnumerationVersion,omitempty"`
	Format                 string          `json:"format"`
	HTMLRenditions         []string        `json:"htmlRenditions"`
	Phase4Metadata         json.RawMessage `json:"phase4Metadata"`
	Pages                  []struct {
		AttachmentRelationshipsVersion int                            `json:"attachmentRelationshipsVersion,omitempty"`
		AttachmentRelationships        []ImportAttachmentRelationship `json:"attachmentRelationships,omitempty"`
		DisplayPreferences             *PageDisplayPreferences        `json:"displayPreferences,omitempty"`
		Path                           string                         `json:"path"`
		ParentPath                     string                         `json:"parentPath"`
		Provenance                     any                            `json:"provenance"`
		CanonicalPage                  contracts.DomainPage           `json:"canonicalPage"`
		CanonicalBlocks                []contracts.DomainBlock        `json:"canonicalBlocks"`
	} `json:"pages"`
	Attachments []struct {
		ID     string `json:"id"`
		Path   string `json:"path"`
		SHA256 string `json:"sha256"`
	} `json:"attachments"`
	Collections        []portableDatabaseSource `json:"collections"`
	SourceOmissions    []string                 `json:"sourceOmissions"`
	SourceCompleteness string                   `json:"sourceCompleteness"`
}

func (k *Kernel) parseImportPlan(ctx context.Context, p *ImportPlan) error {
	p.NormalizerVersion = importNormalizerVersion
	pages := map[string]string{}
	assets := map[string]string{}
	seenIDs := map[string]bool{}
	manifest := portableHierarchy{}
	for _, item := range p.Items {
		if item.Locator == "trove-manifest.json" && item.RawFileID != "" {
			b, err := k.readImportBytes(ctx, item.RawFileID)
			if err != nil {
				return err
			}
			_ = json.Unmarshal(b, &manifest)
		}
	}
	canonicalIdentities := map[string]string{}
	retainedMappings := map[string]portableMapping{}
	if manifest.Format == "trove-portable" {
		var err error
		retainedMappings, err = retainedPortableMappings(ctx, k.db, p)
		if err != nil {
			return err
		}
		for _, page := range manifest.Pages {
			if page.CanonicalPage.Id != "" {
				canonicalIdentities[page.Path] = portableSourceKey("page", string(page.CanonicalPage.Id))
			}
		}
		for _, collection := range manifest.Collections {
			if collection.ID != "" {
				canonicalIdentities[collection.Path] = portableSourceKey("collection", collection.ID)
			}
		}
	}
	initialPages := map[string]string{}
	for _, item := range p.Items {
		initialPages[item.Locator] = item.PageID
	}
	for i := range p.Items {
		item := &p.Items[i]
		if manifest.Format == "trove-portable" && item.Locator == "trove-manifest.json" {
			item.PageID = ""
			item.Kind = "asset"
			item.Outcome = "preserved"
			item.Reason = "Portable inventory retained as inert source evidence; canonical pages are imported individually"
			item.Provenance = map[string]any{"format": "trove-portable-inventory", "activationPolicy": "retained evidence only; review before creating rules, actions or filesystem approvals", "phase4Metadata": manifest.Phase4Metadata}
		}
		for _, rendition := range manifest.HTMLRenditions {
			if item.Locator == rendition {
				item.PageID = ""
				item.Kind = "asset"
				item.Outcome = "preserved"
				item.Reason = "Offline HTML rendition retained; canonical page imported from its manifest source"
			}
		}
		for _, attachment := range manifest.Attachments {
			if item.Locator == attachment.Path {
				item.PageID = ""
				item.Kind = "asset"
				item.Outcome = "preserved"
				item.Reason = "Portable attachment preserved and manifest hash verified"
				if attachment.SHA256 != item.Hash {
					item.Kind = "rejected"
					item.Outcome = "failed"
					item.Reason = "Attachment hash differs from supplied manifest; original bytes retained"
				}
			}
		}
		for _, collection := range manifest.Collections {
			if item.Locator == collection.Path && initialPages[collection.OwningPagePath] != "" {
				item.PageID = initialPages[collection.OwningPagePath]
			}
		}
	}
	// Portable attachments are evidence, not new import sources. Classify them
	// before expanding conversation JSON so a round trip cannot duplicate pages.
	if err := k.expandConversationImports(ctx, p); err != nil {
		return err
	}
	if manifest.SourceCompleteness == "partial" {
		p.SourceCompleteness = "partial"
	}
	for i := range p.Items {
		item := &p.Items[i]
		if item.Kind == "directory" || item.Kind == "rejected" {
			continue
		}
		assets[item.Locator] = item.RawFileID
		if path.Base(item.Locator) == "trove-manifest.json" {
			b, err := k.readImportBytes(ctx, item.RawFileID)
			if err != nil {
				return err
			}
			_ = json.Unmarshal(b, &manifest)
		}
		if item.PageID != "" {
			if identity := canonicalIdentities[item.Locator]; identity != "" {
				item.IdentityKind, item.ExternalID = "external_id", identity
				item.PortableIdentity = identity
			}
			if item.IdentityKind == "" {
				item.IdentityKind, item.ExternalID = importIdentity(item.Locator)
			}
			originalPageID := item.PageID
			key := item.IdentityKind + ":" + item.ExternalID
			if seenIDs[key] {
				item.Outcome = "partial"
				item.Reason += "; repeated source identity uses its locator"
				item.IdentityKind = "derived_locator"
				item.ExternalID = item.Locator
			}
			seenIDs[key] = true
			var id, local, hash, revision string
			err := k.db.QueryRowContext(ctx, `SELECT m.id,m.local_id,m.base_content_hash,COALESCE(p.revision,'0') FROM import_mappings m LEFT JOIN pages p ON p.id=m.local_id WHERE m.workspace_id=? AND m.source_system=? AND m.source_namespace=? AND m.external_kind='page' AND m.identity_kind=? AND m.identity_value=?`, k.id, importItemSourceSystem(p, item), p.Namespace, item.IdentityKind, item.ExternalID).Scan(&id, &local, &hash, &revision)
			if retained, ok := retainedMappings[canonicalIdentities[item.Locator]]; ok {
				// Keep the exact historic mapping address; the explicit canonical
				// key above has already prevented rendition identity collisions.
				id, local, hash, revision, err = retained.ID, retained.Page, retained.Hash, retained.Revision, nil
				item.IdentityKind, item.ExternalID = retained.IdentityKind, retained.IdentityValue
			}
			if err == nil {
				item.MappingID = id
				item.PageID = local
				item.ExpectedRevision = revision
				var base string
				_ = k.db.QueryRowContext(ctx, "SELECT local_revision FROM import_mapping_state WHERE mapping_id=?", id).Scan(&base)
				if hash == item.Hash && base == revision {
					item.Choice = "keep_local"
					item.Reason += "; identical source and unchanged local page"
				} else {
					item.Conflict = "Existing source mapping: choose whether to keep local content, replace the page body, or import a separate copy"
				}
			} else if !errors.Is(err, sql.ErrNoRows) {
				return err
			}
			if originalPageID != item.PageID {
				for _, b := range item.Blocks {
					rewriteImportBlock(b.Data, map[string]string{originalPageID: item.PageID})
				}
			}
			pages[item.Locator] = item.PageID
		}
	}
	// Reconstruct directories from supplied page stems. Missing intermediate
	// directory nodes become explicit pages, so the path remains reconstructable.
	stems := map[string]string{}
	for locator, id := range pages {
		stems[strings.TrimSuffix(locator, path.Ext(locator))] = id
	}
	folders := map[string]string{}
	var ensureFolder func(string) string
	ensureFolder = func(dir string) string {
		if dir == "." || dir == "" || dir == "/" {
			return p.RootID
		}
		if id := stems[dir]; id != "" {
			return id
		}
		if id := folders[dir]; id != "" {
			return id
		}
		parent := ensureFolder(path.Dir(dir))
		id := NewID()
		folders[dir] = id
		p.Items = append(p.Items, ImportItem{Locator: dir + "/", Kind: "folder", Outcome: "adapted", Reason: "Directory reconstructed as a page", PageID: id, ParentID: parent, Title: sourceTitle(dir), IdentityKind: "derived_locator", ExternalID: dir + "/"})
		return id
	}
	original := len(p.Items)
	for i := 0; i < original; i++ {
		if p.Items[i].PageID != "" {
			dir := path.Dir(p.Items[i].Locator)
			parent := ensureFolder(dir)
			p.Items[i].ParentID = parent
		}
	}
	for _, entry := range manifest.Pages {
		for i := range p.Items {
			item := &p.Items[i]
			if item.Locator == entry.Path {
				if parent := pages[entry.ParentPath]; parent != "" {
					item.ParentID = parent
				}
				item.Provenance = entry.Provenance
				item.DisplayPreferences = entry.DisplayPreferences
			}
		}
	}
	if len(manifest.SourceOmissions) > 0 {
		p.SourceOmissions = append(p.SourceOmissions, manifest.SourceOmissions...)
	}
	if len(manifest.Phase4Metadata) > 0 {
		p.SourceOmissions = append(p.SourceOmissions, "Supplied Phase 4 templates, buttons, database settings and automation definitions are retained in manifest evidence; they are not activated by import.")
	}
	rows := []ImportItem{}
	for i := range p.Items {
		item := &p.Items[i]
		if item.Kind == "folder" || item.Kind == "directory" || item.Kind == "rejected" {
			continue
		}
		b, err := k.readImportBytes(ctx, item.RawFileID)
		if err != nil {
			return err
		}
		switch item.Kind {
		case "html":
			item.Blocks = parseImportHTML(string(b), item, pages, assets)
		case "document":
			item.Blocks = k.parseImportDocument(ctx, item)
		case "markdown":
			item.Blocks = parseImportMarkdownTree(string(b), item, pages, assets)
		case "csv":
			reader := csv.NewReader(strings.NewReader(strings.TrimPrefix(string(b), "\ufeff")))
			reader.FieldsPerRecord = -1
			header, err := reader.Read()
			if err != nil || len(header) == 0 {
				item.Kind = "unsupported"
				item.Outcome = "unsupported_but_retained"
				item.Reason = "CSV header is invalid; raw bytes retained"
				item.Blocks = []ImportBlock{fallbackImportBlock(item, string(b))}
				continue
			}
			if len(header) > 256 {
				item.Kind = "unsupported"
				item.Outcome = "unsupported_but_retained"
				item.Reason = "CSV has more than 256 columns; raw data retained"
				item.Blocks = []ImportBlock{fallbackImportBlock(item, string(b))}
				continue
			}
			records := [][]string{}
			for {
				cells, err := reader.Read()
				if err == io.EOF {
					break
				}
				if err != nil {
					item.Outcome = "partial"
					item.Reason = "CSV contains malformed rows; readable and raw source retained; successfully parsed rows imported"
					break
				}
				records = append(records, cells)
				if len(records)+len(p.Items) > maxImportEntries {
					return fmt.Errorf("CSV exceeds 10,000 records and files")
				}
			}
			item.CollectionID = NewID()
			item.Columns = []ImportColumn{}
			for col, name := range header {
				vals := []string{}
				for _, record := range records {
					if col < len(record) {
						vals = append(vals, record[col])
					}
				}
				kind := inferImportType(vals)
				if col == 0 {
					kind = "title"
				}
				samples := vals
				if len(samples) > 3 {
					samples = samples[:3]
				}
				if samples == nil {
					samples = []string{}
				}
				column := ImportColumn{SourceHeader: &name, Name: name, Type: kind, PropertyID: NewID(), Samples: samples}
				if name == "" {
					column.Name = fmt.Sprintf("Column %d", col+1)
				}
				lower := strings.ToLower(name)
				if strings.Contains(lower, "formula") || strings.Contains(lower, "rollup") {
					column.Warning = "Display values only; formula/rollup definitions are not inferred"
				}
				item.Columns = append(item.Columns, column)
			}
			p.UnknownCompleteness = append(p.UnknownCompleteness, "CSV "+item.Locator+": original property types, formula definitions, relations, rollups, views, and row IDs may be absent.")
			if item.Outcome == "partial" {
				item.Blocks = append(item.Blocks, fallbackImportBlock(item, string(b)))
			}
			for index, cells := range records {
				raw := marshal(cells)
				rawID, err := k.storeImportBytes(ctx, fmt.Sprintf("row-%d.json", index+1), raw)
				if err != nil {
					return err
				}
				title := "Untitled"
				if len(cells) > 0 && cells[0] != "" {
					title = cells[0]
				}
				row := ImportItem{Locator: fmt.Sprintf("%s#row=%d", item.Locator, index+1), Kind: "row", Outcome: "adapted", Reason: "CSV row values preserved; blank values remain unset", RawFileID: rawID, Hash: importDigest(raw), Size: int64(len(raw)), PageID: NewID(), ParentID: item.PageID, Title: title, RecordID: NewID(), CollectionID: item.CollectionID, CSVSource: item.Locator, Cells: cells, IdentityKind: "derived_locator", ExternalID: fmt.Sprintf("%s#row=%d", item.Locator, index+1)}
				// A matching title is not a row/page identity. Portable manifests
				// may bind an explicit record body later; otherwise keep both.
				if len(cells) != len(header) {
					row.Outcome = "partial"
					row.Reason = "Row width differs from header; every source cell retained in readable fallback and raw evidence"
					row.Blocks = []ImportBlock{fallbackImportBlock(&row, strings.Join(cells, " | "))}
				}
				rows = append(rows, row)
			}
		case "unsupported":
			item.Blocks = []ImportBlock{fallbackImportBlock(item, readableImportFallback(b, item.Locator))}
		}
	}
	p.Items = append(p.Items, rows...)
	k.restorePortableBlocks(p, manifest)
	if err := restorePortableAttachments(p, manifest); err != nil {
		return err
	}
	k.restorePortableDatabase(p, manifest)
	if err := restoreCSVKeyBindings(ctx, k.db, p); err != nil {
		return err
	}
	if err := reconcileImportFolderIDs(ctx, k.db, p); err != nil {
		return err
	}
	for i := range p.Items {
		if err := reconcileImportBlockIDs(ctx, k.db, &p.Items[i]); err != nil {
			return err
		}
	}
	if err := reuseImportDatabaseIdentities(ctx, k.db, p); err != nil {
		return err
	}
	if err := preparePortablePageInventory(ctx, k.db, p, manifest); err != nil {
		return err
	}
	for i := range p.Items {
		if err := prepareImportMerge(ctx, k.db, p, &p.Items[i]); err != nil {
			return err
		}
	}
	if err := appendMissingSourcePages(ctx, k.db, p); err != nil {
		return err
	}
	sortImportItems(p.Items)
	return nil
}
func readableImportFallback(b []byte, name string) string {
	if len(b) > 1024*1024 {
		return "Retained source file: " + name + " (open the attachment for full content)"
	}
	text := strings.ToValidUTF8(string(b), "�")
	if strings.EqualFold(path.Ext(name), ".html") {
		text = htmlTagImport.ReplaceAllString(text, "")
	}
	return text
}
func fallbackImportBlock(item *ImportItem, readable string) ImportBlock {
	return ImportBlock{ID: NewID(), Type: "unsupported_external", Data: map[string]any{"sourceType": item.Kind, "sourceVersion": 1, "rawFileId": item.RawFileID, "readableText": readable, "reason": "unsupported_behavior"}}
}

func resolveImportLink(source, target string, pages, assets map[string]string) (string, bool, bool) {
	target = strings.Trim(target, "<>")
	u, err := url.Parse(target)
	if err != nil {
		return target, false, false
	}
	if u.Scheme != "" || u.Host != "" || strings.HasPrefix(target, "#") {
		return target, false, true
	}
	decoded, err := url.PathUnescape(u.Path)
	if err != nil {
		return target, false, false
	}
	locator := path.Clean(path.Join(path.Dir(source), decoded))
	if id := pages[locator]; id != "" {
		return "trove://page/" + id, false, true
	}
	if id := assets[locator]; id != "" {
		return id, true, true
	}
	return target, false, false
}
func parseImportMarkdown(text, source string, pages, assets map[string]string, outcome, reason *string) []ImportBlock {
	blocks := []ImportBlock{}
	lines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	inCode := false
	lang := ""
	code := []string{}
	fence := ""
	unresolved := 0
	for _, line := range lines {
		if strings.HasPrefix(line, "```") || strings.HasPrefix(line, "~~~") {
			mark := line[:3]
			if !inCode {
				inCode = true
				fence = mark
				lang = strings.TrimSpace(line[3:])
				code = []string{}
				continue
			}
			if mark == fence {
				blocks = append(blocks, ImportBlock{NewID(), "code", map[string]any{"source": strings.Join(code, "\n"), "language": lang}})
				inCode = false
				continue
			}
		}
		if inCode {
			code = append(code, line)
			continue
		}
		if line == "" {
			continue
		}
		kind := "paragraph"
		content := line
		for n := 4; n >= 1; n-- {
			prefix := strings.Repeat("#", n) + " "
			if strings.HasPrefix(line, prefix) {
				kind = fmt.Sprintf("heading_%d", n)
				content = strings.TrimPrefix(line, prefix)
				break
			}
		}
		if strings.HasPrefix(line, "> ") {
			kind = "quote"
			content = line[2:]
		} else if strings.HasPrefix(line, "- ") || strings.HasPrefix(line, "* ") {
			kind = "bulleted_list_item"
			content = line[2:]
		}
		if line == "---" || line == "***" {
			blocks = append(blocks, ImportBlock{NewID(), "divider", map[string]any{}})
			continue
		}
		spans := []any{}
		last := 0
		for _, m := range markdownLink.FindAllStringSubmatchIndex(content, -1) {
			if m[0] > last {
				spans = append(spans, map[string]any{"text": content[last:m[0]]})
			}
			image := content[m[2]:m[3]] == "!"
			label := content[m[4]:m[5]]
			target := content[m[6]:m[7]]
			rewritten, asset, found := resolveImportLink(source, target, pages, assets)
			if !found {
				unresolved++
			}
			if image && asset && m[0] == 0 && m[1] == len(content) {
				blocks = append(blocks, ImportBlock{NewID(), "image", map[string]any{"fileId": rewritten, "caption": richImport(label)}})
				last = len(content)
				spans = nil
				break
			}
			if asset {
				rewritten = "/files/" + rewritten
			}
			spans = append(spans, map[string]any{"text": label, "link": rewritten})
			last = m[1]
		}
		if last < len(content) {
			spans = append(spans, map[string]any{"text": content[last:]})
		}
		if len(spans) > 0 {
			blocks = append(blocks, ImportBlock{NewID(), kind, map[string]any{"richText": spans}})
		}
	}
	if inCode {
		blocks = append(blocks, ImportBlock{NewID(), "code", map[string]any{"source": strings.Join(code, "\n"), "language": lang}})
		*outcome = "partial"
		*reason += "; unclosed code fence retained"
	}
	if unresolved > 0 {
		*outcome = "partial"
		*reason += fmt.Sprintf("; %d unresolved relative links remain readable in exact source", unresolved)
	}
	return blocks
}
