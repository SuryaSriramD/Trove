package workspace

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"trove.local/phase0/contracts"
)

// Version 2 adds meeting speaker/timing/audio-binding knowledge. Version 1
// review events remain readable; an older coverage version requires review.
// The projection excludes revisions, decoration, and review state.
// A change to this projection requires a new version, never silent reinterpretation.
func semanticContentIdentity(ctx context.Context, q querier, page string) (contracts.DomainSemanticContentIdentity, error) {
	out := contracts.DomainSemanticContentIdentity{Version: 2}
	p, err := readPageTx(ctx, q, page)
	if err != nil {
		return out, err
	}
	blocks := []any{}
	for _, block := range p.Blocks {
		var data map[string]any
		if err = json.Unmarshal(block.Data, &data); err != nil {
			return out, err
		}
		normalizeReviewBlock(block.Type, data)
		blocks = append(blocks, map[string]any{"id": block.Id, "parent": block.ParentBlockId, "type": block.Type, "schemaVersion": block.SchemaVersion, "data": data})
	}
	values, _, err := reviewRecordKnowledge(ctx, q, page)
	if err != nil {
		return out, err
	}
	assets := []any{}
	rows, err := q.QueryContext(ctx, `SELECT a.file_id,f.content_hash FROM attachments a JOIN files f ON f.id=a.file_id WHERE a.page_id=? ORDER BY a.file_id`, page)
	if err != nil {
		return out, err
	}
	for rows.Next() {
		var id, hash string
		if err = rows.Scan(&id, &hash); err != nil {
			rows.Close()
			return out, err
		}
		assets = append(assets, []string{id, hash})
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return out, err
	}
	// Todo occurrences show canonical task facts; their visible title/status/due
	// changes must invalidate a review even though the reference block is stable.
	tasks := []any{}
	rows, err = q.QueryContext(ctx, `SELECT DISTINCT r.id,p.title,p.deleted_at,f.status_property_id,f.due_property_id FROM todo_references t JOIN blocks b ON b.id=t.block_id JOIN records r ON r.id=t.task_record_id JOIN pages p ON p.id=r.page_id JOIN task_facets f ON f.record_id=r.id WHERE b.page_id=? ORDER BY r.id`, page)
	if err != nil {
		return out, err
	}
	type refTask struct {
		id, title, status string
		deleted, due      sql.NullString
	}
	refs := []refTask{}
	for rows.Next() {
		var r refTask
		if err = rows.Scan(&r.id, &r.title, &r.deleted, &r.status, &r.due); err != nil {
			rows.Close()
			return out, err
		}
		refs = append(refs, r)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return out, err
	}
	for _, r := range refs {
		task := map[string]any{"id": r.id, "title": r.title, "deleted": r.deleted.Valid, "statusPropertyId": r.status, "duePropertyId": r.due.String}
		for key, property := range map[string]string{"status": r.status, "due": r.due.String} {
			var value string
			err = q.QueryRowContext(ctx, "SELECT value_json FROM property_values WHERE record_id=? AND property_id=?", r.id, property).Scan(&value)
			if err == nil {
				task[key] = json.RawMessage(value)
				definition, definitionErr := propertyDefinition(ctx, q, property)
				if definitionErr != nil {
					return out, definitionErr
				}
				task[key+"Definition"] = map[string]any{"name": definition.Name, "config": definition.Config}
			} else if !errors.Is(err, sql.ErrNoRows) {
				return out, err
			}
		}
		tasks = append(tasks, task)
	}
	var facetStatus string
	var facetDue sql.NullString
	facet := map[string]any{}
	err = q.QueryRowContext(ctx, `SELECT f.status_property_id,f.due_property_id FROM task_facets f JOIN records r ON r.id=f.record_id WHERE r.page_id=?`, page).Scan(&facetStatus, &facetDue)
	if err == nil {
		facet["statusPropertyId"] = facetStatus
		facet["duePropertyId"] = facetDue.String
	} else if !errors.Is(err, sql.ErrNoRows) {
		return out, err
	}
	dependencies, _, err := reviewBlockDependencies(ctx, q, p.Blocks)
	if err != nil {
		return out, err
	}
	var projection any
	payload := map[string]any{"version": out.Version, "title": p.Page.Title, "blocks": blocks, "values": values, "assets": assets, "reusableSources": dependencies, "referencedTasks": tasks, "taskFacet": facet}
	meeting := recoverySnapshot{}
	if err = captureRecoveryMeeting(ctx, q, page, &meeting); err != nil {
		return out, err
	}
	if meeting.Meeting != nil {
		segments := []any{}
		for _, segment := range meeting.Segments {
			segments = append(segments, map[string]any{"id": segment.Id, "blockId": segment.BlockId, "startMs": segment.StartMs, "endMs": segment.EndMs, "text": segment.Text, "speakerLabel": segment.SpeakerLabel})
		}
		payload["meeting"] = map[string]any{"id": meeting.Meeting.Id, "startedAt": meeting.Meeting.StartedAt, "durationMs": meeting.Meeting.DurationMs, "recordingFileId": meeting.Meeting.RecordingFileId, "segments": segments}
	}
	skillRows, err := q.QueryContext(ctx, "SELECT s.skill_id,s.definition_json FROM skill_versions s WHERE s.page_id=? AND s.version=(SELECT max(version) FROM skill_versions WHERE skill_id=s.skill_id) ORDER BY s.skill_id", page)
	if err != nil {
		return out, err
	}
	skillDefinitions := map[string]any{}
	for skillRows.Next() {
		var id, raw string
		if err = skillRows.Scan(&id, &raw); err != nil {
			skillRows.Close()
			return out, err
		}
		var definition any
		if err = json.Unmarshal([]byte(raw), &definition); err != nil {
			skillRows.Close()
			return out, err
		}
		skillDefinitions[id] = definition
	}
	err = skillRows.Err()
	skillRows.Close()
	if err != nil {
		return out, err
	}
	if len(skillDefinitions) > 0 {
		payload["skillDefinitions"] = skillDefinitions
	}
	decoder := json.NewDecoder(bytes.NewReader(marshal(payload)))
	decoder.UseNumber()
	if err = decoder.Decode(&projection); err != nil {
		return out, err
	}
	raw, err := canonicalJSON(projection)
	if err != nil {
		return out, err
	}
	out.Digest = snapshotDigest(string(raw))
	return out, nil
}

func init() {
	RegisterInitializer(func(ctx context.Context, k *Kernel) error {
		return k.write(ctx, func(tx *sql.Tx) error {
			_, err := tx.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS page_review_events(sequence INTEGER PRIMARY KEY AUTOINCREMENT,id TEXT NOT NULL UNIQUE,page_id TEXT NOT NULL REFERENCES pages(id),actor_id TEXT NOT NULL,reviewed_at TEXT NOT NULL,expires_at TEXT,identity_version INTEGER NOT NULL,content_digest TEXT NOT NULL); CREATE INDEX IF NOT EXISTS page_review_events_page ON page_review_events(page_id,sequence DESC);`)
			return err
		})
	})
	RegisterCommand("page.review", reviewPage)
	RegisterQuery("page.freshness", pageFreshnessGet)
	RegisterQuery("page.reviewQueue", pageReviewQueue)
}

func scanReview(row interface{ Scan(...any) error }) (contracts.DomainHumanReviewEvent, error) {
	var r contracts.DomainHumanReviewEvent
	var expires sql.NullString
	err := row.Scan(&r.Id, &r.PageId, &r.ActorId, &r.ReviewedAt, &expires, &r.ContentIdentity.Version, &r.ContentIdentity.Digest)
	if expires.Valid {
		x := contracts.DomainTimestamp(expires.String)
		r.ExpiresAt = &x
	}
	return r, err
}
func pageFreshness(ctx context.Context, q querier, page string, at time.Time) (contracts.DomainPageFreshness, error) {
	identity, err := semanticContentIdentity(ctx, q, page)
	out := contracts.DomainPageFreshness{State: "never_reviewed", ContentIdentity: identity}
	if err != nil {
		return out, err
	}
	r, err := scanReview(q.QueryRowContext(ctx, `SELECT id,page_id,actor_id,reviewed_at,expires_at,identity_version,content_digest FROM page_review_events WHERE page_id=? ORDER BY sequence DESC LIMIT 1`, page))
	if errors.Is(err, sql.ErrNoRows) {
		return out, nil
	}
	if err != nil {
		return out, err
	}
	out.Review = &r
	out.State = "reviewed"
	if r.ContentIdentity != identity {
		out.State = "stale"
	} else if r.ExpiresAt != nil {
		expires, err := time.Parse(time.RFC3339Nano, string(*r.ExpiresAt))
		if err != nil {
			return out, err
		}
		if !at.Before(expires) {
			out.State = "expired"
		}
	}
	return out, nil
}
func reviewPage(m *Mutation, c map[string]any) error {
	if m.Execution.Actor.Kind != "user" || m.Execution.Origin != "user" {
		return Fail("SCOPE_DENIED", "A human review must be recorded by the user")
	}
	page := str(c["pageId"])
	if err := m.AuthorizePage(page); err != nil {
		return err
	}
	var deleted sql.NullString
	if err := m.Tx.QueryRowContext(m.Context, "SELECT deleted_at FROM pages WHERE id=?", page).Scan(&deleted); err != nil {
		return err
	}
	if deleted.Valid {
		return Fail("CONFLICT", "Trashed content cannot be reviewed")
	}
	sourcePage, err := readPageTx(m.Context, m.Tx, page)
	if err != nil {
		return err
	}
	_, sourcePages, err := reviewBlockDependencies(m.Context, m.Tx, sourcePage.Blocks)
	if err != nil {
		return err
	}
	_, recordPages, err := reviewRecordKnowledge(m.Context, m.Tx, page)
	if err != nil {
		return err
	}
	for source := range recordPages {
		sourcePages[source] = true
	}
	// Direct todo occurrences display canonical task facts even when this page
	// is not itself a record or reusable source. The review must cover only
	// knowledge the user is authorized to inspect.
	taskRows, err := m.Tx.QueryContext(m.Context, `SELECT DISTINCT r.page_id FROM todo_references t JOIN blocks b ON b.id=t.block_id JOIN records r ON r.id=t.task_record_id WHERE b.page_id=?`, page)
	if err != nil {
		return err
	}
	for taskRows.Next() {
		var source string
		if err = taskRows.Scan(&source); err != nil {
			taskRows.Close()
			return err
		}
		sourcePages[source] = true
	}
	err = taskRows.Err()
	taskRows.Close()
	if err != nil {
		return err
	}
	for source := range sourcePages {
		if err = m.AuthorizePage(source); err != nil {
			return err
		}
	}
	identity, err := semanticContentIdentity(m.Context, m.Tx, page)
	if err != nil {
		return err
	}
	var expected contracts.DomainSemanticContentIdentity
	if err = json.Unmarshal(marshal(c["contentIdentity"]), &expected); err != nil {
		return err
	}
	if identity != expected {
		return Fail("CONFLICT", "Content changed after review; inspect the current content")
	}
	var expiry any
	if value := str(c["expiresAt"]); value != "" {
		at, err := time.Parse(time.RFC3339Nano, value)
		if err != nil || !at.After(time.Now()) {
			return Fail("INVALID_REQUEST", "Review expiry must be in the future")
		}
		expiry = at.UTC().Format(time.RFC3339Nano)
	}
	if err = m.TouchPage(page); err != nil {
		return err
	}
	_, err = m.Exec(`INSERT INTO page_review_events(id,page_id,actor_id,reviewed_at,expires_at,identity_version,content_digest) VALUES(?,?,?,?,?,?,?)`, NewID(), page, m.Execution.Actor.ActorId, now(), expiry, identity.Version, identity.Digest)
	return err
}
func pageFreshnessGet(ctx context.Context, k *Kernel, e contracts.ServiceExecutionContext, r map[string]any) (any, error) {
	tx, err := k.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	page := str(r["pageId"])
	if err = k.authorizePage(ctx, tx, e, page); err != nil {
		return nil, err
	}
	status, err := pageFreshness(ctx, tx, page, time.Now())
	if err != nil {
		return nil, err
	}
	history := []contracts.DomainHumanReviewEvent{}
	rows, err := tx.QueryContext(ctx, `SELECT id,page_id,actor_id,reviewed_at,expires_at,identity_version,content_digest FROM page_review_events WHERE page_id=? ORDER BY sequence DESC LIMIT 100`, page)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		event, err := scanReview(rows)
		if err != nil {
			return nil, err
		}
		history = append(history, event)
	}
	return map[string]any{"pageId": page, "freshness": status, "history": history}, rows.Err()
}
func pageReviewQueue(ctx context.Context, k *Kernel, e contracts.ServiceExecutionContext, r map[string]any) (any, error) {
	tx, err := k.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	rows, err := tx.QueryContext(ctx, "SELECT id,title FROM pages WHERE deleted_at IS NULL AND id>? ORDER BY id LIMIT 501", str(r["afterPageId"]))
	if err != nil {
		return nil, err
	}
	type item struct{ id, title string }
	pages := []item{}
	for rows.Next() {
		var p item
		if err = rows.Scan(&p.id, &p.title); err != nil {
			rows.Close()
			return nil, err
		}
		pages = append(pages, p)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	items := []any{}
	out := map[string]any{"items": items}
	limit := int(number(r["limit"]))
	for i, p := range pages {
		if i == 500 {
			out["nextPageId"] = pages[i-1].id
			break
		}
		if err = k.authorizePage(ctx, tx, e, p.id); err != nil {
			continue
		}
		status, err := pageFreshness(ctx, tx, p.id, time.Now())
		if err != nil {
			return nil, err
		}
		if status.State != "reviewed" {
			items = append(items, map[string]any{"pageId": p.id, "title": p.title, "freshness": status})
		}
		if len(items) == limit {
			if i+1 < len(pages) {
				out["nextPageId"] = p.id
			}
			break
		}
	}
	out["items"] = items
	return out, nil
}

func normalizeReviewRichText(data map[string]any) {
	for _, key := range []string{"richText", "caption"} {
		spans, ok := data[key].([]any)
		if !ok {
			continue
		}
		normalized := []any{}
		for _, span := range spans {
			s, ok := span.(map[string]any)
			if !ok {
				continue
			}
			text := str(s["text"])
			link := str(s["link"])
			if len(normalized) > 0 {
				previous := normalized[len(normalized)-1].(map[string]any)
				if str(previous["link"]) == link {
					previous["text"] = str(previous["text"]) + text
					continue
				}
			}
			normalized = append(normalized, map[string]any{"text": text, "link": link})
		}
		data[key] = normalized
	}
	if cells, ok := data["cells"].([]any); ok {
		for _, cell := range cells {
			if c, ok := cell.(map[string]any); ok {
				normalizeReviewRichText(c)
			}
		}
	}
}

func normalizeReviewBlock(kind string, data map[string]any) {
	switch kind {
	case "toggle", "toggle_heading":
		delete(data, "collapsed")
	case "callout":
		delete(data, "icon")
		delete(data, "backgroundToken")
	case "code":
		delete(data, "wrap")
		delete(data, "lineNumbers")
	case "mermaid":
		delete(data, "theme")
		delete(data, "renderMode")
	case "equation":
		delete(data, "display")
	case "table":
		delete(data, "columnWidths")
	case "image", "audio", "video", "file", "pdf":
		delete(data, "width")
		delete(data, "alignment")
		delete(data, "previewPage")
	case "bookmark":
		delete(data, "fetchedAt")
	case "columns":
		if columns, ok := data["columns"].([]any); ok {
			for _, column := range columns {
				if c, ok := column.(map[string]any); ok {
					delete(c, "widthRatio")
				}
			}
		}
	}
	normalizeReviewRichText(data)

}

func reviewPropertyValue(kind, raw string) any {
	if kind != "text" {
		return json.RawMessage(raw)
	}
	var value any
	if json.Unmarshal([]byte(raw), &value) != nil {
		return json.RawMessage(raw)
	}
	wrapper := map[string]any{"richText": value}
	normalizeReviewRichText(wrapper)
	return wrapper["richText"]
}
func reviewBlockDependencies(ctx context.Context, q querier, blocks []contracts.DomainBlock) (map[string]any, map[string]bool, error) {
	nodes := map[string]any{}
	pages := map[string]bool{}
	var visit func(string, int) error
	visit = func(id string, depth int) error {
		if _, ok := nodes[id]; ok {
			return nil
		}
		if depth > 64 || len(nodes) >= 10000 {
			return Fail("RESOURCE_EXHAUSTED", "Reusable review content exceeds traversal bounds")
		}
		var page, kind, raw string
		var version int
		var deleted sql.NullString
		err := q.QueryRowContext(ctx, `SELECT b.page_id,b.type,b.schema_version,b.data_json,p.deleted_at FROM blocks b JOIN pages p ON p.id=b.page_id WHERE b.id=?`, id).Scan(&page, &kind, &version, &raw, &deleted)
		if errors.Is(err, sql.ErrNoRows) {
			nodes[id] = map[string]any{"state": "unavailable"}
			return nil
		}
		if err != nil {
			return err
		}
		pages[page] = true
		if deleted.Valid {
			nodes[id] = map[string]any{"state": "deleted"}
			return nil
		}
		var data map[string]any
		if err = json.Unmarshal([]byte(raw), &data); err != nil {
			return err
		}
		normalizeReviewBlock(kind, data)
		nodes[id] = map[string]any{"type": kind, "schemaVersion": version, "data": data}
		if kind == "todo" {
			var taskPage string
			err := q.QueryRowContext(ctx, "SELECT page_id FROM records WHERE id=?", str(data["taskRecordId"])).Scan(&taskPage)
			if err != nil {
				return err
			}
			knowledge, taskPages, err := reviewRecordKnowledge(ctx, q, taskPage)
			if err != nil {
				return err
			}
			nodes[id].(map[string]any)["task"] = knowledge
			for p := range taskPages {
				pages[p] = true
			}
		}
		rows, err := q.QueryContext(ctx, "SELECT id FROM blocks WHERE parent_block_id=? ORDER BY sort_key,id", id)
		if err != nil {
			return err
		}
		children := []string{}
		for rows.Next() {
			var child string
			if err = rows.Scan(&child); err != nil {
				rows.Close()
				return err
			}
			children = append(children, child)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return err
		}
		nodes[id].(map[string]any)["children"] = children
		if kind == "synced_block" {
			if err = visit(str(data["contentSourceId"]), depth+1); err != nil {
				return err
			}
		}
		for _, child := range children {
			if err = visit(child, depth+1); err != nil {
				return err
			}
		}
		return nil
	}
	for _, block := range blocks {
		if block.Type == "synced_block" {
			var data map[string]any
			if err := json.Unmarshal(block.Data, &data); err != nil {
				return nil, nil, err
			}
			if err := visit(str(data["contentSourceId"]), 0); err != nil {
				return nil, nil, err
			}
		}
	}
	return nodes, pages, nil
}

// Include absent cells and computed definitions. Rollup source records form a
// bounded semantic graph so editing an input invalidates the covered review.
// Volatile clocks and presentation timestamps are not persisted knowledge.
func reviewRecordKnowledge(ctx context.Context, q querier, page string) (map[string]any, map[string]bool, error) {
	graph := map[string]any{}
	pages := map[string]bool{}
	var root string
	err := q.QueryRowContext(ctx, "SELECT id FROM records WHERE page_id=?", page).Scan(&root)
	if errors.Is(err, sql.ErrNoRows) {
		return graph, pages, nil
	}
	if err != nil {
		return nil, nil, err
	}
	pending := []string{root}
	for len(pending) > 0 {
		id := pending[0]
		pending = pending[1:]
		if _, ok := graph[id]; ok {
			continue
		}
		if len(graph) >= 1000 {
			return nil, nil, Fail("RESOURCE_EXHAUSTED", "Review dependency graph exceeds 1000 records")
		}
		var p, title string
		var deleted sql.NullString
		err = q.QueryRowContext(ctx, "SELECT r.page_id,p.title,p.deleted_at FROM records r JOIN pages p ON p.id=r.page_id WHERE r.id=?", id).Scan(&p, &title, &deleted)
		if errors.Is(err, sql.ErrNoRows) {
			graph[id] = map[string]any{"state": "unavailable"}
			continue
		}
		if err != nil {
			return nil, nil, err
		}
		pages[p] = true
		fields := []any{}
		relations := []string{}
		hasDerived := false
		rows, err := q.QueryContext(ctx, `SELECT p.id,p.schema_version,p.name,p.type,p.config_json,v.value_json FROM records r JOIN properties p ON p.collection_id=r.collection_id LEFT JOIN property_values v ON v.record_id=r.id AND v.property_id=p.id WHERE r.id=? ORDER BY p.id`, id)
		if err != nil {
			return nil, nil, err
		}
		for rows.Next() {
			var property, name, kind, config string
			var version int
			var value sql.NullString
			if err = rows.Scan(&property, &version, &name, &kind, &config, &value); err != nil {
				rows.Close()
				return nil, nil, err
			}
			field := map[string]any{"propertyId": property, "schemaVersion": version, "name": name, "type": kind, "config": json.RawMessage(config), "present": value.Valid}
			if value.Valid {
				field["value"] = reviewPropertyValue(kind, value.String)
			}
			fields = append(fields, field)
			if kind == "rollup" || kind == "formula" {
				hasDerived = true
			}
			if kind == "relation" && value.Valid {
				var ids []string
				if json.Unmarshal([]byte(value.String), &ids) == nil {
					relations = append(relations, ids...)
				}
			}
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return nil, nil, err
		}
		facet := map[string]any{}
		var status string
		var due sql.NullString
		err = q.QueryRowContext(ctx, "SELECT status_property_id,due_property_id FROM task_facets WHERE record_id=?", id).Scan(&status, &due)
		if err == nil {
			facet["statusPropertyId"] = status
			facet["duePropertyId"] = due.String
		} else if !errors.Is(err, sql.ErrNoRows) {
			return nil, nil, err
		}
		graph[id] = map[string]any{"title": title, "deleted": deleted.Valid, "fields": fields, "taskFacet": facet}
		if hasDerived {
			pending = append(pending, relations...)
		}
	}
	return graph, pages, nil
}
