package workspace

// The retained source is the base, never a synthetic copy of today's page.
// Atomic paths deliberately keep rich text/block payloads intact. Missing and
// JSON null have distinct representations, including in persisted decisions.
import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"trove.local/phase0/contracts"
)

const importNormalizerVersion = 1

const importCSVArtifactIdentity = "normalizer-artifact:csv-evidence"

func importCSVArtifact(raw json.RawMessage) bool {
	var payload struct {
		Type string         `json:"type"`
		Data map[string]any `json:"data"`
	}
	return json.Unmarshal(raw, &payload) == nil && payload.Type == "unsupported_external" && str(payload.Data["sourceType"]) == "csv"
}

type ImportSnapshot struct {
	Version        int                        `json:"version"`
	SourceRevision string                     `json:"sourceRevision,omitempty"`
	Fields         map[string]json.RawMessage `json:"fields"`
	BlockSources   map[string]string          `json:"blockSources"`
}
type ImportPresence struct {
	Present bool            `json:"present"`
	Value   json.RawMessage `json:"value,omitempty"`
}
type ImportFieldDecision struct {
	Path     string         `json:"path"`
	Outcome  string         `json:"outcome"`
	Base     ImportPresence `json:"base"`
	Local    ImportPresence `json:"local"`
	Incoming ImportPresence `json:"incoming"`
}
type ImportFieldChoice struct {
	Path   string          `json:"path"`
	Choice string          `json:"choice"`
	Value  *ImportPresence `json:"value,omitempty"`
}
type ImportMergeReview struct {
	ID             string                `json:"id"`
	Version        int                   `json:"version"`
	BaselineStatus string                `json:"baselineStatus"`
	LocalDeleted   bool                  `json:"localDeleted"`
	Fields         []ImportFieldDecision `json:"fields"`
	Resolutions    []ImportFieldChoice   `json:"resolutions"`
}

func importPresence(fields map[string]json.RawMessage, path string) ImportPresence {
	v, ok := fields[path]
	return ImportPresence{Present: ok, Value: v}
}
func equalImportPresence(a, b ImportPresence) bool {
	return a.Present == b.Present && (!a.Present || bytes.Equal(a.Value, b.Value))
}

// Cell, tab and column IDs are generated UI identities, not source content.
// Compare their semantic values while retaining the original concrete payload
// in the decision. Links, referenced block IDs and fallback raw evidence remain.
func semanticImportPresence(path string, value ImportPresence) ImportPresence {
	if !value.Present || !strings.HasSuffix(path, "/content") {
		return value
	}
	var payload map[string]any
	if json.Unmarshal(value.Value, &payload) != nil {
		return value
	}
	data, _ := payload["data"].(map[string]any)
	var stripIDs func(any)
	stripIDs = func(raw any) {
		switch v := raw.(type) {
		case map[string]any:
			delete(v, "id")
			if children, ok := v["preMergeCells"]; ok {
				stripIDs(children)
			}
		case []any:
			for _, child := range v {
				stripIDs(child)
			}
		}
	}
	switch str(payload["type"]) {
	case "table":
		stripIDs(data["cells"])
	case "columns":
		stripIDs(data["columns"])
	case "tab_container":
		stripIDs(data["tabs"])
	}
	value.Value = marshal(payload)
	return value
}
func mergeImportFields(base, local, incoming map[string]json.RawMessage, known bool) []ImportFieldDecision {
	keys := map[string]bool{}
	for _, fields := range []map[string]json.RawMessage{base, local, incoming} {
		for key := range fields {
			keys[key] = true
		}
	}
	paths := []string{}
	for key := range keys {
		paths = append(paths, key)
	}
	sort.Strings(paths)
	out := make([]ImportFieldDecision, 0, len(paths))
	for _, path := range paths {
		b, l, i := importPresence(base, path), importPresence(local, path), importPresence(incoming, path)
		cb, cl, ci := semanticImportPresence(path, b), semanticImportPresence(path, l), semanticImportPresence(path, i)
		result := "conflict"
		if known {
			switch {
			case equalImportPresence(cl, cb) && equalImportPresence(ci, cb):
				result = "unchanged"
			case equalImportPresence(cl, ci):
				result = "converged"
			case equalImportPresence(cl, cb):
				result = "apply_incoming"
			case equalImportPresence(ci, cb):
				result = "keep_local"
			}
		}
		out = append(out, ImportFieldDecision{path, result, b, l, i})
	}
	return out
}
func snapshotImportItem(item *ImportItem) ImportSnapshot {
	if item.BlockSources == nil {
		item.BlockSources = map[string]string{}
	}
	for index, block := range item.Blocks {
		if item.BlockSources[block.ID] == "" {
			if item.Kind == "portable_collection" && len(item.Blocks) == 1 && importCSVArtifact(marshal(map[string]any{"type": block.Type, "data": block.Data})) {
				// This is the normalizer's single retained CSV-evidence widget,
				// not a position guessed to identify an upstream content block.
				item.BlockSources[block.ID] = importCSVArtifactIdentity
			} else {
				item.BlockSources[block.ID] = fmt.Sprintf("normalizer-position:%d", index)
			}
		}
	}
	s := ImportSnapshot{Version: importNormalizerVersion, SourceRevision: item.Hash, Fields: map[string]json.RawMessage{"/title": marshal(item.Title), "/parent": marshal(nullable(item.ParentID))}, BlockSources: item.BlockSources}
	positions := map[string]int{}
	for _, b := range item.Blocks {
		parent := item.BlockParents[b.ID]
		prefix := "/blocks/" + b.ID
		s.Fields[prefix+"/content"] = marshal(map[string]any{"type": b.Type, "data": b.Data})
		s.Fields[prefix+"/parent"] = marshal(nullable(parent))
		s.Fields[prefix+"/order"] = marshal(positions[parent])
		positions[parent]++
	}
	for path, relationship := range item.AttachmentRelationships {
		s.Fields[path] = marshal(relationship)
	}
	return s
}
func localImportSnapshot(ctx context.Context, q querier, page string) (ImportSnapshot, bool, error) {
	s := ImportSnapshot{Version: importNormalizerVersion, Fields: map[string]json.RawMessage{}, BlockSources: map[string]string{}}
	var title string
	var parent, deleted sql.NullString
	if err := q.QueryRowContext(ctx, "SELECT title,parent_page_id,deleted_at FROM pages WHERE id=?", page).Scan(&title, &parent, &deleted); err != nil {
		return s, false, err
	}
	s.Fields["/title"], s.Fields["/parent"] = marshal(title), marshal(nullable(parent.String))
	rows, err := q.QueryContext(ctx, "SELECT id,type,data_json,parent_block_id FROM blocks WHERE page_id=? ORDER BY sort_key,id", page)
	if err != nil {
		return s, false, err
	}
	defer rows.Close()
	positions := map[string]int{}
	for rows.Next() {
		var id, kind, raw string
		var parent sql.NullString
		if err = rows.Scan(&id, &kind, &raw, &parent); err != nil {
			return s, false, err
		}
		var data any
		if err = json.Unmarshal([]byte(raw), &data); err != nil {
			return s, false, err
		}
		s.Fields["/blocks/"+id+"/content"] = marshal(map[string]any{"type": kind, "data": data})
		s.Fields["/blocks/"+id+"/parent"] = marshal(nullable(parent.String))
		s.Fields["/blocks/"+id+"/order"] = marshal(positions[parent.String])
		positions[parent.String]++
	}
	return s, deleted.Valid, rows.Err()
}
func loadImportBaseline(ctx context.Context, q querier, mapping string) (ImportSnapshot, string, bool, error) {
	var s ImportSnapshot
	var raw, revision string
	var version int
	err := q.QueryRowContext(ctx, "SELECT normalizer_version,snapshot_json,source_revision FROM import_baselines WHERE mapping_id=?", mapping).Scan(&version, &raw, &revision)
	if errors.Is(err, sql.ErrNoRows) {
		return reconstructRetainedImportBaseline(ctx, q, mapping)
	}
	if err != nil {
		return s, "", false, err
	}
	if version != importNormalizerVersion {
		return s, revision, false, nil
	}
	if err = json.Unmarshal([]byte(raw), &s); err != nil {
		return s, revision, false, err
	}
	return s, revision, s.Version == importNormalizerVersion, nil
}

// An accepted, versioned incoming snapshot retained in a completed import unit
// is source evidence. Materialized blocks may contain local edits and are never
// used to reconstruct a baseline. Older plans without a known normalizer remain
// explicitly unavailable, even when their raw attachment still exists.
func reconstructRetainedImportBaseline(ctx context.Context, q querier, mapping string) (ImportSnapshot, string, bool, error) {
	var empty ImportSnapshot
	var run, file, revision string
	err := q.QueryRowContext(ctx, "SELECT COALESCE(last_import_run_id,''),COALESCE(base_raw_file_id,''),COALESCE(base_source_revision,'') FROM import_mappings WHERE id=?", mapping).Scan(&run, &file, &revision)
	if errors.Is(err, sql.ErrNoRows) {
		return empty, "", false, nil
	}
	if err != nil {
		return empty, "", false, err
	}
	if run == "" || file == "" || revision == "" {
		return empty, revision, false, nil
	}
	plan, err := loadImportPlan(ctx, q, run)
	if errors.Is(err, sql.ErrNoRows) {
		return empty, revision, false, nil
	}
	if err != nil {
		return empty, revision, false, err
	}
	if plan.NormalizerVersion != importNormalizerVersion {
		return empty, revision, false, nil
	}
	for _, item := range plan.Items {
		if item.MappingID != mapping || !item.Committed || item.RawFileID != file || item.Hash != revision || item.Incoming == nil || item.Incoming.Version != importNormalizerVersion {
			continue
		}
		if item.Kind == "portable_collection" && plan.State != "complete" {
			continue
		}
		var retained int
		if err := q.QueryRowContext(ctx, "SELECT count(*) FROM files WHERE id=?", file).Scan(&retained); err != nil {
			return empty, revision, false, err
		}
		if retained != 1 || contracts.Validate("domain", "ImportNormalizedSnapshot", marshal(item.Incoming)) != nil {
			continue
		}
		return *item.Incoming, revision, true, nil
	}
	return empty, revision, false, nil
}

func prepareImportMerge(ctx context.Context, q querier, p *ImportPlan, item *ImportItem) error {
	if item.PageID == "" || item.Kind == "row" || item.Kind == "csv" {
		return nil
	}
	if item.MappingID == "" {
		s := snapshotImportItem(item)
		if c := importCollectionFor(p, item); c != nil {
			addIncomingDatabaseFields(&s, c)
		}
		item.Incoming = &s
		return nil
	}
	base, sourceRevision, known, err := loadImportBaseline(ctx, q, item.MappingID)
	if err != nil {
		return err
	}
	if base.SourceRevision != "" {
		sourceRevision = base.SourceRevision
	}
	// Populate anonymous positions before reconciling an identical source.
	_ = snapshotImportItem(item)
	// Unchanged raw evidence plus the same versioned normalizer establishes
	// positional identity; edited anonymous source never gets that privilege.
	ids := map[string]string{}
	bySource := map[string]string{}
	for id, source := range base.BlockSources {
		bySource[source] = id
		if item.Kind == "portable_collection" && len(base.BlockSources) == 1 && source == "normalizer-position:0" && importCSVArtifact(base.Fields["/blocks/"+id+"/content"]) {
			// Upgrade the known v1 normalizer's exact single-widget evidence;
			// arbitrary anonymous blocks never receive this identity.
			bySource[importCSVArtifactIdentity] = id
		}
	}
	for id, source := range item.BlockSources {
		if old := bySource[source]; old != "" && (sourceRevision == item.Hash || source == importCSVArtifactIdentity || strings.HasPrefix(source, "normalizer-position:"+item.Hash+":conversation-node:") || strings.HasPrefix(source, "notion-block:") || strings.HasPrefix(source, "html-id:") || strings.HasPrefix(source, "portable-block:") || strings.HasPrefix(source, "conversation-node:")) {
			ids[id] = old
		}
	}
	remapImportBlockIDs(item, ids)
	incoming := snapshotImportItem(item)
	collection := importCollectionFor(p, item)
	if collection != nil {
		addIncomingDatabaseFields(&incoming, collection)
		if known {
			observed := map[string]bool{}
			for _, record := range collection.Records {
				for _, property := range record.KnownProperties {
					observed["/values/"+record.ID+"/"+property] = true
				}
			}
			for path, value := range base.Fields {
				if strings.HasPrefix(path, "/values/") && !observed[path] {
					parts := strings.Split(path, "/")
					// A proven schema removal also removes its values. Each local
					// value is exposed below for explicit review, including edits.
					if len(parts) == 4 && collection.SchemaComplete && base.Fields["/schema/"+parts[3]] != nil && incoming.Fields["/schema/"+parts[3]] == nil {
						continue
					}
					if _, present := incoming.Fields[path]; !present {
						incoming.Fields[path] = value
					}
				}
				if strings.HasPrefix(path, "/records/") || strings.HasPrefix(path, "/schema/") && !collection.SchemaComplete {
					if _, present := incoming.Fields[path]; !present {
						incoming.Fields[path] = value
					}
				}
			}
		}
	}
	if known && ((p.SourceSystem == "notion_api" && p.SourceCompleteness != "complete") || item.Kind == "conversation") {
		// An incomplete API enumeration or supplied conversation graph is not
		// proof that an omitted prior block was deleted at its source.
		for path, value := range base.Fields {
			if strings.HasPrefix(path, "/blocks/") {
				if _, present := incoming.Fields[path]; !present {
					incoming.Fields[path] = value
					// The field and its source identity are one retained piece of
					// evidence. A later complete capture must reuse the same UUID.
					id := strings.Split(path, "/")[2]
					if source := base.BlockSources[id]; source != "" {
						incoming.BlockSources[id] = source
					}
				}
			}
		}
	}
	local, deleted, err := localImportSnapshot(ctx, q, item.PageID)
	if err != nil {
		return err
	}
	if err := prepareImportAttachmentFields(ctx, q, item, base, &local, &incoming); err != nil {
		return err
	}
	if collection != nil {
		if err := addLocalDatabaseFields(ctx, q, &local, item, collection); err != nil {
			return err
		}
	}
	// Each run has a new evidence root. It is not a source move. A supplied
	// parent with a matched identity is still compared as its own field.
	if item.ParentID == p.RootID {
		if known {
			incoming.Fields["/parent"] = base.Fields["/parent"]
		} else {
			incoming.Fields["/parent"] = local.Fields["/parent"]
		}
	}
	item.Incoming = &incoming
	if collection != nil {
		item.Hash = importDigest(marshal(incoming.Fields))
	}
	status := "retained"
	if !known {
		status = "unavailable"
	}
	item.Merge = &ImportMergeReview{ID: NewID(), Version: 1, BaselineStatus: status, LocalDeleted: deleted, Fields: mergeImportContentFields(base.Fields, local.Fields, incoming.Fields, known), Resolutions: []ImportFieldChoice{}}
	for i := range item.Merge.Fields {
		field := &item.Merge.Fields[i]
		if (strings.HasPrefix(field.Path, "/attachments/") || strings.HasPrefix(field.Path, "/blocks/") || strings.HasPrefix(field.Path, "/schema/")) && field.Base.Present && !field.Incoming.Present && field.Local.Present {
			// Even a proven complete removal needs an explicit reviewed decision.
			field.Outcome = "conflict"
		}
		if strings.HasPrefix(field.Path, "/values/") && collection != nil && collection.SchemaComplete && field.Local.Present {
			parts := strings.Split(field.Path, "/")
			if len(parts) == 4 && base.Fields["/schema/"+parts[3]] != nil && incoming.Fields["/schema/"+parts[3]] == nil {
				field.Outcome = "conflict"
			}
		}
		if strings.HasPrefix(field.Path, "/schema/") && field.Local.Present && field.Incoming.Present {
			var local, incoming struct {
				Type string `json:"type"`
			}
			if json.Unmarshal(field.Local.Value, &local) == nil && json.Unmarshal(field.Incoming.Value, &incoming) == nil && local.Type != incoming.Type {
				// Changing type is never an automatic scalar overwrite. The
				// reviewed selection still needs a supported value migration.
				field.Outcome = "conflict"
			}
		}
	}
	item.Choice = ""
	item.Conflict = "Review the retained base, local changes and incoming fields"
	if !known {
		item.Conflict = "Baseline unavailable: review every field explicitly; historical bytes are not interpreted by an unknown normalizer"
	}
	if deleted {
		item.Conflict = "The local page is trashed. Keep it or import a separate copy; restore it before merging"
	}
	unchanged := known && !deleted
	for _, f := range item.Merge.Fields {
		unchanged = unchanged && (f.Outcome == "unchanged" || f.Outcome == "keep_local" || f.Outcome == "converged")
	}
	if unchanged {
		item.Choice = "keep_local"
		item.Conflict = ""
	}
	return nil
}

func configureImportMerge(item *ImportItem, raw []any) error {
	if item.Merge == nil {
		return Fail("INVALID_REQUEST", "This source has no field merge preview")
	}
	if item.Merge.LocalDeleted {
		return Fail("CONFLICT", "Restore the trashed page before merging")
	}
	known := map[string]bool{}
	for _, f := range item.Merge.Fields {
		known[f.Path] = true
	}
	choices := map[string]ImportFieldChoice{}
	for _, f := range item.Merge.Resolutions {
		choices[f.Path] = f
	}
	seen := map[string]bool{}
	for _, v := range raw {
		var f ImportFieldChoice
		if err := json.Unmarshal(marshal(v), &f); err != nil {
			return Fail("INVALID_REQUEST", "Invalid field resolution")
		}
		if !known[f.Path] || seen[f.Path] {
			return Fail("INVALID_REQUEST", "Unknown or duplicate import field")
		}
		seen[f.Path] = true
		if f.Choice != "keep_local" && f.Choice != "take_incoming" && f.Choice != "merged" {
			return Fail("INVALID_REQUEST", "Invalid field choice")
		}
		if f.Choice == "merged" {
			if f.Value == nil || f.Value.Present && len(f.Value.Value) == 0 || !f.Value.Present && len(f.Value.Value) > 0 {
				return Fail("INVALID_REQUEST", "Merged values must distinguish null from absent")
			}
		} else if f.Value != nil {
			return Fail("INVALID_REQUEST", "Only a merged choice accepts a value")
		}
		choices[f.Path] = f
	}
	item.Merge.Resolutions = []ImportFieldChoice{}
	ready := true
	for _, f := range item.Merge.Fields {
		if c, ok := choices[f.Path]; ok {
			item.Merge.Resolutions = append(item.Merge.Resolutions, c)
		} else if f.Outcome == "conflict" {
			ready = false
		}
	}
	item.Choice = ""
	if ready {
		item.Choice = "merge"
	}
	return nil
}

func materializeImportMerge(item *ImportItem) error {
	if item.Merge == nil || item.Choice != "merge" {
		return nil
	}
	if item.Merge.LocalDeleted {
		return Fail("CONFLICT", "The local page is trashed")
	}
	choices := map[string]ImportFieldChoice{}
	for _, c := range item.Merge.Resolutions {
		choices[c.Path] = c
	}
	fields := map[string]json.RawMessage{}
	for _, f := range item.Merge.Fields {
		value := f.Local
		if f.Outcome == "apply_incoming" {
			value = f.Incoming
		}
		if c, ok := choices[f.Path]; ok {
			switch c.Choice {
			case "keep_local":
				value = f.Local
			case "take_incoming":
				value = f.Incoming
			case "merged":
				value = *c.Value
			}
		} else if f.Outcome == "conflict" {
			return Fail("CONFLICT", "Unresolved import field: "+f.Path)
		}
		if value.Present {
			fields[f.Path] = value.Value
		}
	}
	if err := expandImportSiblingOrders(fields, item.Merge); err != nil {
		return err
	}
	if err := json.Unmarshal(fields["/title"], &item.Title); err != nil {
		return Fail("INVALID_REQUEST", "Merged title must be present text")
	}
	var parent *string
	if err := json.Unmarshal(fields["/parent"], &parent); err != nil {
		return Fail("INVALID_REQUEST", "Merged parent must be a page ID or null")
	}
	item.ParentID = ""
	if parent != nil {
		item.ParentID = *parent
	}
	type placed struct {
		block    ImportBlock
		parent   string
		position int
	}
	blocks := map[string]placed{}
	for path, raw := range fields {
		if !strings.HasPrefix(path, "/blocks/") {
			continue
		}
		parts := strings.Split(path, "/")
		if len(parts) != 4 {
			return Fail("INVALID_REQUEST", "Invalid block field path")
		}
		id := parts[2]
		if parts[3] != "content" {
			continue
		}
		var content struct {
			Type string         `json:"type"`
			Data map[string]any `json:"data"`
		}
		if err := json.Unmarshal(raw, &content); err != nil || content.Type == "" || content.Data == nil {
			return Fail("INVALID_REQUEST", "Merged block content is invalid")
		}
		var parent *string
		var position int
		if err := json.Unmarshal(fields["/blocks/"+id+"/parent"], &parent); err != nil {
			return Fail("INVALID_REQUEST", "A retained block requires a valid parent")
		}
		if err := json.Unmarshal(fields["/blocks/"+id+"/order"], &position); err != nil || position < 0 {
			return Fail("INVALID_REQUEST", "A retained block requires a valid order")
		}
		parentID := ""
		if parent != nil {
			parentID = *parent
		}
		blocks[id] = placed{ImportBlock{id, content.Type, content.Data}, parentID, position}
	}
	for path := range fields {
		if strings.HasPrefix(path, "/blocks/") && (strings.HasSuffix(path, "/parent") || strings.HasSuffix(path, "/order")) {
			if _, ok := blocks[strings.Split(path, "/")[2]]; !ok {
				return Fail("CONFLICT", "Resolve block content, parent and order together when deleting a block")
			}
		}
	}
	ordered := []placed{}
	positions := map[string]map[int]string{}
	for _, b := range blocks {
		if positions[b.parent] == nil {
			positions[b.parent] = map[int]string{}
		}
		if prior := positions[b.parent][b.position]; prior != "" {
			return Fail("CONFLICT", "Merged sibling order is ambiguous; review distinct order values for every affected sibling")
		}
		positions[b.parent][b.position] = b.block.ID
		ordered = append(ordered, b)
	}
	sort.Slice(ordered, func(i, j int) bool {
		if ordered[i].position != ordered[j].position {
			return ordered[i].position < ordered[j].position
		}
		return ordered[i].block.ID < ordered[j].block.ID
	})
	item.Blocks = []ImportBlock{}
	item.BlockParents = map[string]string{}
	done := map[string]bool{"": true}
	for len(done)-1 < len(blocks) {
		progress := false
		for _, b := range ordered {
			if done[b.block.ID] || !done[b.parent] {
				continue
			}
			item.Blocks = append(item.Blocks, b.block)
			item.BlockParents[b.block.ID] = b.parent
			done[b.block.ID] = true
			progress = true
		}
		if !progress {
			return Fail("CONFLICT", "Merged block locations have a cycle or missing parent")
		}
	}
	// Preserve identity evidence for local-only blocks across subsequent runs.
	if item.BlockSources == nil {
		item.BlockSources = map[string]string{}
	}
	item.ResolvedFields = fields
	for _, field := range item.Merge.Fields {
		if field.Local.Present && strings.HasPrefix(field.Path, "/records/") {
			if _, present := fields[field.Path]; !present {
				return Fail("CONFLICT", "Deleting a property or record requires a separately reviewed deletion; retain it in this import")
			}
		}
	}
	item.Choice = "replace"
	return nil
}

func saveImportBaseline(m *Mutation, p *ImportPlan, item *ImportItem) error {
	if item.MappingID == "" || item.RawFileID == "" || item.Incoming == nil {
		return nil
	}
	_, err := m.Exec(`INSERT INTO import_baselines VALUES(?,?,?,?,?,?) ON CONFLICT(mapping_id) DO UPDATE SET normalizer_version=excluded.normalizer_version,snapshot_json=excluded.snapshot_json,source_revision=excluded.source_revision,raw_file_id=excluded.raw_file_id,import_run_id=excluded.import_run_id`, item.MappingID, importNormalizerVersion, string(marshal(item.Incoming)), item.Hash, item.RawFileID, p.ID)
	if err != nil {
		return err
	}
	_, err = m.Exec("UPDATE import_mappings SET base_source_revision=?,base_content_hash=?,base_raw_file_id=?,last_import_run_id=? WHERE id=?", item.Hash, item.Hash, item.RawFileID, p.ID, item.MappingID)
	return err
}

func importMergeContract(item ImportItem, p ImportPlan) map[string]any {
	resolution := "needs_review"
	fields := []ImportFieldDecision{}
	id := item.MappingID
	deleted := false
	if item.Merge != nil {
		fields = item.Merge.Fields
		id = item.Merge.ID
		deleted = item.Merge.LocalDeleted
	}
	if item.Choice == "keep_local" {
		resolution = "no_change"
	} else if item.Choice == "merge" || item.Choice == "replace" {
		resolution = "apply"
	}
	return map[string]any{"id": id, "mappingId": item.MappingID, "expectedLocalRevision": item.ExpectedRevision, "incomingSourceRevision": item.Hash, "identityStatus": "matched", "sourceCompleteness": p.SourceCompleteness, "fields": fields, "sourceDeleted": false, "localDeleted": deleted, "resolution": resolution, "normalizerVersion": importNormalizerVersion, "baselineStatus": func() string {
		if item.Merge != nil {
			return item.Merge.BaselineStatus
		}
		return "unavailable"
	}()}
}

func applyImportFieldResolutions(p *ImportPlan, raw []any) error {
	for _, value := range raw {
		resolution, _ := value.(map[string]any)
		found := false
		for i := range p.Items {
			item := &p.Items[i]
			if item.MappingID == "" || item.MappingID != str(resolution["mappingId"]) {
				continue
			}
			if item.Merge == nil || str(resolution["planId"]) != item.Merge.ID || str(resolution["expectedLocalRevision"]) != item.ExpectedRevision || str(resolution["incomingSourceRevision"]) != item.Hash {
				return Fail("CONFLICT", "Re-import resolution no longer matches its preview")
			}
			if resolution["identityChoice"] != nil || resolution["deletionChoice"] != nil {
				return Fail("INVALID_REQUEST", "Matched live import fields cannot remap or delete pages")
			}
			fields, _ := resolution["fields"].([]any)
			if err := configureImportMerge(item, fields); err != nil {
				return err
			}
			if item.Choice != "merge" {
				return Fail("CONFLICT", "Resolve every conflicting field before applying")
			}
			found = true
		}
		if !found {
			return Fail("NOT_FOUND", fmt.Sprintf("Import resolution mapping %s is not part of this plan", str(resolution["mappingId"])))
		}
	}
	return nil
}
