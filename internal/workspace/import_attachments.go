package workspace

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"strings"
)

// Membership is atomic, separate from immutable bytes and from references
// projected out of block/property content. The source tuple supplies identity;
// filenames, titles and hashes never match unrelated relationships.
type ImportAttachmentRelationship struct {
	FileID  string `json:"fileId"`
	BlockID string `json:"blockId,omitempty"`
}

func attachmentFieldPath(r ImportAttachmentRelationship) string {
	return "/attachments/" + base64.RawURLEncoding.EncodeToString(marshal(r))
}

func attachmentReference(r ImportAttachmentRelationship) (string, string) {
	if r.BlockID != "" {
		return "explicit_block", r.BlockID
	}
	return "explicit_user", r.FileID
}

func attachmentLegacyKind(r ImportAttachmentRelationship) string {
	if r.BlockID != "" {
		return "explicit_block"
	}
	return "explicit"
}

func restorePortableAttachments(p *ImportPlan, manifest portableHierarchy) error {
	if manifest.Format != "trove-portable" {
		return nil
	}
	items, files := map[string]*ImportItem{}, map[string]string{}
	for i := range p.Items {
		items[p.Items[i].Locator] = &p.Items[i]
	}
	for _, file := range manifest.Attachments {
		if item := items[file.Path]; item != nil && item.Kind == "asset" {
			if prior := files[file.ID]; prior != "" && prior != item.RawFileID {
				return Fail("INVALID_REQUEST", "Ambiguous portable attachment identity")
			}
			files[file.ID] = item.RawFileID
		}
	}
	for _, page := range manifest.Pages {
		item := items[page.Path]
		if item == nil || item.PageID == "" || page.AttachmentRelationshipsVersion != 1 {
			continue
		}
		item.AttachmentsComplete = true
		item.AttachmentRelationships = map[string]ImportAttachmentRelationship{}
		blocks := map[string]string{}
		for id, source := range item.BlockSources {
			if strings.HasPrefix(source, "portable-block:") {
				blocks[strings.TrimPrefix(source, "portable-block:")] = id
			}
		}
		for _, source := range page.AttachmentRelationships {
			path := attachmentFieldPath(source)
			if _, exists := item.AttachmentRelationships[path]; exists {
				return Fail("INVALID_REQUEST", "Duplicate portable attachment relationship")
			}
			local := ImportAttachmentRelationship{FileID: files[source.FileID]}
			if local.FileID == "" {
				return Fail("INVALID_REQUEST", "Portable attachment relationship has missing or invalid file evidence")
			}
			if source.BlockID != "" {
				local.BlockID = blocks[source.BlockID]
				if local.BlockID == "" {
					return Fail("INVALID_REQUEST", "Portable attachment relationship has missing canonical block evidence")
				}
			}
			item.AttachmentRelationships[path] = local
		}
	}
	return nil
}

func decodeAttachment(raw json.RawMessage) (ImportAttachmentRelationship, error) {
	var relationship ImportAttachmentRelationship
	if json.Unmarshal(raw, &relationship) != nil || !canonicalLinkID.MatchString(relationship.FileID) || (relationship.BlockID != "" && !canonicalLinkID.MatchString(relationship.BlockID)) {
		return relationship, Fail("INVALID_REQUEST", "An attachment relationship requires a valid file ID and optional block ID")
	}
	return relationship, nil
}

func prepareImportAttachmentFields(ctx context.Context, q querier, item *ImportItem, base ImportSnapshot, local, incoming *ImportSnapshot) error {
	paths := map[string]bool{}
	for path, raw := range base.Fields {
		if !strings.HasPrefix(path, "/attachments/") {
			continue
		}
		paths[path] = true
		if !item.AttachmentsComplete {
			// A legacy/partial manifest does not enumerate relationship removals.
			if _, present := incoming.Fields[path]; !present {
				incoming.Fields[path] = raw
			}
		}
	}
	for path := range incoming.Fields {
		if strings.HasPrefix(path, "/attachments/") {
			paths[path] = true
		}
	}
	for path := range paths {
		// Only tracked source relationships are compared. Independent local
		// attachments remain untouched and never acquire a fabricated identity.
		for _, fields := range []map[string]json.RawMessage{base.Fields, incoming.Fields} {
			raw, present := fields[path]
			if !present {
				continue
			}
			r, err := decodeAttachment(raw)
			if err != nil {
				return err
			}
			kind, source := attachmentReference(r)
			var count int
			if err := q.QueryRowContext(ctx, "SELECT count(*) FROM file_references WHERE page_id=? AND source_kind IN (?,?) AND source_id=? AND file_id=?", item.PageID, kind, attachmentLegacyKind(r), source, r.FileID).Scan(&count); err != nil {
				return err
			}
			if count != 0 {
				local.Fields[path] = raw
				break
			}
		}
	}
	return nil
}

func applyImportAttachments(m *Mutation, item *ImportItem) error {
	selected, affected := map[ImportAttachmentRelationship]bool{}, map[ImportAttachmentRelationship]bool{}
	fields := item.ResolvedFields
	if fields == nil && item.Incoming != nil {
		fields = item.Incoming.Fields
	}
	for path, raw := range fields {
		if !strings.HasPrefix(path, "/attachments/") {
			continue
		}
		r, err := decodeAttachment(raw)
		if err != nil {
			return err
		}
		selected[r] = true
	}
	if item.Merge != nil {
		for _, field := range item.Merge.Fields {
			if !strings.HasPrefix(field.Path, "/attachments/") || !field.Local.Present {
				continue
			}
			r, err := decodeAttachment(field.Local.Value)
			if err != nil {
				return err
			}
			affected[r] = true
		}
	}
	for r := range selected {
		if err := guardImport(m, "file", r.FileID); err != nil {
			return err
		}
		if err := m.Require("file", r.FileID); err != nil {
			return err
		}
		var count int
		if err := m.Tx.QueryRowContext(m.Context, "SELECT count(*) FROM files WHERE id=?", r.FileID).Scan(&count); err != nil {
			return err
		}
		if count != 1 {
			return Fail("CONFLICT", "Reviewed attachment file is absent")
		}
		if r.BlockID != "" {
			if err := m.Tx.QueryRowContext(m.Context, "SELECT count(*) FROM blocks WHERE id=? AND page_id=?", r.BlockID, item.PageID).Scan(&count); err != nil {
				return err
			}
			if count != 1 {
				return Fail("CONFLICT", "An attachment requires a retained block on its page; review the relationship and block together")
			}
		}
	}
	for r := range affected {
		if selected[r] {
			continue
		}
		kind, source := attachmentReference(r)
		if _, err := m.Exec("DELETE FROM file_references WHERE page_id=? AND source_kind IN (?,?) AND source_id=? AND file_id=?", item.PageID, kind, attachmentLegacyKind(r), source, r.FileID); err != nil {
			return err
		}
	}
	for r := range selected {
		kind, source := attachmentReference(r)
		if _, err := m.Exec("INSERT OR IGNORE INTO file_references VALUES(?,?,?,?)", item.PageID, kind, source, r.FileID); err != nil {
			return err
		}
	}
	return nil
}

// Automatic import evidence is archived independently. It must not become a
// user attachment merely because its bytes are eligible for portable export.
func portableAttachmentRelationships(ctx context.Context, q querier, allowed map[string]string) (map[string][]ImportAttachmentRelationship, error) {
	ignored := map[string]map[string]bool{}
	mark := func(page, file string) {
		if ignored[page] == nil {
			ignored[page] = map[string]bool{}
		}
		ignored[page][file] = true
	}
	rows, err := q.QueryContext(ctx, "SELECT plan_json FROM import_runs")
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var raw string
		var plan ImportPlan
		if err = rows.Scan(&raw); err != nil {
			rows.Close()
			return nil, err
		}
		if err = json.Unmarshal([]byte(raw), &plan); err != nil {
			rows.Close()
			return nil, err
		}
		mark(plan.RootID, plan.SourceFileID)
		for _, item := range plan.Items {
			mark(plan.RootID, item.RawFileID)
			mark(item.PageID, item.RawFileID)
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	rows, err = q.QueryContext(ctx, "SELECT page_id,source_kind,source_id,file_id FROM file_references WHERE source_kind IN ('explicit','explicit_user','explicit_block') ORDER BY page_id,source_kind,source_id,file_id")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string][]ImportAttachmentRelationship{}
	seen := map[string]map[ImportAttachmentRelationship]bool{}
	for rows.Next() {
		var page, kind, source, file string
		if err = rows.Scan(&page, &kind, &source, &file); err != nil {
			return nil, err
		}
		if allowed[file] == "" || (kind == "explicit" && ignored[page][file]) {
			continue
		}
		r := ImportAttachmentRelationship{FileID: file}
		if kind == "explicit_block" {
			r.BlockID = source
		}
		if seen[page] == nil {
			seen[page] = map[ImportAttachmentRelationship]bool{}
		}
		if seen[page][r] {
			continue
		}
		seen[page][r] = true
		out[page] = append(out[page], r)
	}
	return out, rows.Err()
}
