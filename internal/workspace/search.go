package workspace

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
	"unicode"

	"trove.local/phase0/contracts"
)

// SearchHit retains a canonical, revision-bound location, including transcript
// offsets. File metadata supplements that location for opening a PDF page.
type SearchHit struct {
	contracts.ServiceSearchHit
	ChunkID      string `json:"chunkId"`
	Title        string `json:"title"`
	Kind         string `json:"kind"`
	FileID       string `json:"fileId,omitempty"`
	DocumentPage int    `json:"documentPage,omitempty"`
}
type SearchResults struct {
	Hits            []SearchHit `json:"hits"`
	SnapshotCursor  string      `json:"snapshotCursor"`
	IndexGeneration string      `json:"indexGeneration"`
	Partial         bool        `json:"partial"`
	Mode            string      `json:"mode"`
	Warning         string      `json:"warning,omitempty"`
}

func init() {
	RegisterInitializer(func(ctx context.Context, k *Kernel) error { return k.InitSearch(ctx) })
	RegisterCommitProjection(searchChangedPages)
	RegisterQuery("search", canonicalSearch)
	RegisterQuery("search.query", searchQuery)
	RegisterQuery("search.status", searchStatus)
	RegisterQuery("search.rebuild", func(ctx context.Context, k *Kernel, e contracts.ServiceExecutionContext, r map[string]any) (any, error) {
		if err := requireOwnerScope(k, e); err != nil {
			return nil, err
		}
		if err := k.RebuildSearch(ctx); err != nil {
			return nil, err
		}
		return searchStatus(ctx, k, e, r)
	})
	RegisterJobHandler("INDEX_CONTENT", func(ctx context.Context, k *Kernel, j contracts.JobsJob) error {
		p, ok := j.Payload.Value.(contracts.JobsJobPayloadVariant3)
		if !ok {
			return Fail("INVALID_REQUEST", "Expected index payload")
		}
		e, err := k.JobExecution(ctx, string(j.JobId))
		if err != nil {
			return err
		}
		defer k.RevokeGrant(string(e.ScopeGrantId))
		if err = k.authorizePage(ctx, k.db, e, string(p.Location.PageId)); err != nil {
			return err
		}
		return k.write(ctx, func(tx *sql.Tx) error { return indexSearchPage(ctx, tx, string(p.Location.PageId)) })
	})
}
func (k *Kernel) InitSearch(ctx context.Context) error {
	return k.write(ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `
 CREATE TABLE IF NOT EXISTS semantic_auto_attempts(page_id TEXT NOT NULL,revision TEXT NOT NULL,model_id TEXT NOT NULL,PRIMARY KEY(page_id,revision,model_id));
 CREATE TABLE IF NOT EXISTS search_meta(key TEXT PRIMARY KEY,value TEXT NOT NULL);
 CREATE TABLE IF NOT EXISTS search_dirty(page_id TEXT PRIMARY KEY);
 CREATE TABLE IF NOT EXISTS search_chunks(id TEXT PRIMARY KEY,page_id TEXT NOT NULL,source_revision TEXT NOT NULL,block_id TEXT,kind TEXT NOT NULL,title TEXT NOT NULL,body TEXT NOT NULL,location_json TEXT NOT NULL,content_hash TEXT NOT NULL,file_id TEXT,document_page INTEGER NOT NULL DEFAULT 0);
 CREATE INDEX IF NOT EXISTS search_chunks_page ON search_chunks(page_id);
 CREATE VIRTUAL TABLE IF NOT EXISTS search_fts USING fts5(title,body,content='search_chunks',content_rowid='rowid',tokenize='unicode61 remove_diacritics 2');
 CREATE TRIGGER IF NOT EXISTS search_chunks_ai AFTER INSERT ON search_chunks BEGIN INSERT INTO search_fts(rowid,title,body) VALUES(new.rowid,new.title,new.body); END;
 CREATE TRIGGER IF NOT EXISTS search_chunks_ad AFTER DELETE ON search_chunks BEGIN INSERT INTO search_fts(search_fts,rowid,title,body) VALUES('delete',old.rowid,old.title,old.body); END;
 CREATE TRIGGER IF NOT EXISTS search_chunks_au AFTER UPDATE ON search_chunks BEGIN INSERT INTO search_fts(search_fts,rowid,title,body) VALUES('delete',old.rowid,old.title,old.body); INSERT INTO search_fts(rowid,title,body) VALUES(new.rowid,new.title,new.body); END;
 CREATE TABLE IF NOT EXISTS document_extractions(file_id TEXT PRIMARY KEY,file_revision TEXT NOT NULL,extractor_version TEXT NOT NULL,state TEXT NOT NULL,warning TEXT NOT NULL DEFAULT '',updated_at TEXT NOT NULL);
 CREATE TABLE IF NOT EXISTS document_sections(file_id TEXT NOT NULL,ordinal INTEGER NOT NULL,document_page INTEGER NOT NULL,heading TEXT NOT NULL,body TEXT NOT NULL,PRIMARY KEY(file_id,ordinal));
 CREATE TABLE IF NOT EXISTS search_embeddings(chunk_id TEXT NOT NULL,content_hash TEXT NOT NULL,model_pack_id TEXT NOT NULL,dimension INTEGER NOT NULL,vector_json TEXT NOT NULL,PRIMARY KEY(chunk_id,model_pack_id));
 INSERT OR IGNORE INTO search_dirty SELECT id FROM pages WHERE id NOT IN(SELECT page_id FROM search_chunks);
 `)
		if err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, "INSERT OR IGNORE INTO search_meta VALUES('generation',?)", NewID())
		return err
	})
}
func searchChangedPages(m *Mutation) error {
	for _, c := range m.touched {
		if c.Target.EntityType == "page" {
			if _, err := m.Exec("INSERT OR IGNORE INTO search_dirty VALUES(?)", c.Target.EntityId); err != nil {
				return err
			}
		}
	}
	return nil
}

// RebuildSearch is safe after native restore. Chunk IDs derive from canonical
// identities; no canonical content or historical revisions are modified.
func (k *Kernel) RebuildSearch(ctx context.Context) error {
	if err := k.write(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, "DELETE FROM semantic_auto_attempts; DELETE FROM search_chunks; DELETE FROM search_embeddings; INSERT OR IGNORE INTO search_dirty SELECT id FROM pages;"); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, "UPDATE search_meta SET value=? WHERE key='generation'", NewID())
		return err
	}); err != nil {
		return err
	}
	return k.FlushSearch(ctx)
}

// FlushSearch drains durable invalidations in bounded transactions. Inference
// and document extraction never run under the canonical mutation writer.
func (k *Kernel) FlushSearch(ctx context.Context) error {
	for {
		n := 0
		err := k.write(ctx, func(tx *sql.Tx) error {
			rows, err := tx.QueryContext(ctx, "SELECT page_id FROM search_dirty ORDER BY page_id LIMIT 32")
			if err != nil {
				return err
			}
			ids := []string{}
			for rows.Next() {
				var id string
				if err = rows.Scan(&id); err != nil {
					rows.Close()
					return err
				}
				ids = append(ids, id)
			}
			err = rows.Err()
			rows.Close()
			if err != nil {
				return err
			}
			n = len(ids)
			for _, id := range ids {
				if err = indexSearchPage(ctx, tx, id); err != nil {
					return err
				}
			}
			return nil
		})
		if err != nil {
			return err
		}
		if n == 0 {
			return nil
		}
	}
}
func searchDigest(s string) string { h := sha256.Sum256([]byte(s)); return hex.EncodeToString(h[:]) }
func searchReadable(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case []any:
		parts := []string{}
		for _, v := range x {
			if s := searchReadable(v); s != "" {
				parts = append(parts, s)
			}
		}
		return strings.Join(parts, " ")
	case map[string]any:
		// IDs, raw opaque payloads and executable formula definitions are deliberately
		// excluded. Only readable projections are searchable.
		parts := []string{}
		for _, key := range []string{"text", "richText", "caption", "readable", "readableText", "plainText", "code", "source", "latex", "cells", "label", "displayName", "displayValue", "language", "url", "title", "description", "offlineText", "displaySnapshot", "tabs", "start", "end", "timezone"} {
			if v, ok := x[key]; ok {
				if s := searchReadable(v); s != "" {
					parts = append(parts, s)
				}
			}
		}
		return strings.Join(parts, " ")
	}
	return ""
}
func indexSearchPage(ctx context.Context, tx *sql.Tx, page string) error {
	if _, err := tx.ExecContext(ctx, "DELETE FROM semantic_auto_attempts WHERE page_id=?", page); err != nil {
		return err
	}
	var title, revision string
	var deleted sql.NullString
	err := tx.QueryRowContext(ctx, "SELECT title,revision,deleted_at FROM pages WHERE id=?", page).Scan(&title, &revision, &deleted)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if _, e := tx.ExecContext(ctx, "DELETE FROM search_embeddings WHERE chunk_id IN(SELECT id FROM search_chunks WHERE page_id=?)", page); e != nil {
		return e
	}
	if _, e := tx.ExecContext(ctx, "DELETE FROM search_chunks WHERE page_id=?", page); e != nil {
		return e
	}
	if err == nil && !deleted.Valid {
		// Memory pages are evidence/policy records, never an alternate source of
		// authoritative task values. Inactive memory is excluded at retrieval too.
		var hidden int
		if e := tx.QueryRowContext(ctx, "SELECT count(*) FROM memory_entries WHERE page_id=? AND status<>'active'", page).Scan(&hidden); e != nil {
			return e
		}
		if hidden == 0 {
			kind := "page"
			var task int
			if e := tx.QueryRowContext(ctx, "SELECT count(*) FROM task_facets t JOIN records r ON r.id=t.record_id WHERE r.page_id=?", page).Scan(&task); e != nil {
				return e
			}
			if task > 0 {
				kind = "task"
			}
			insert := func(id, block, kind, text, file string, docPage int, loc map[string]any) error {
				text = strings.TrimSpace(text)
				if text == "" {
					return nil
				}
				hash := searchDigest(text)
				_, e := tx.ExecContext(ctx, "INSERT INTO search_chunks VALUES(?,?,?,?,?,?,?,?,?,?,?)", id, page, revision, nullable(block), kind, title, text, string(marshal(loc)), hash, nullable(file), docPage)
				return e
			}
			location := func(block string) map[string]any {
				loc := map[string]any{"pageId": page, "sourceRevision": revision}
				if block != "" {
					loc["blockId"] = block
				}
				return loc
			}
			if e := insert(page+":title", "", kind, title, "", 0, location("")); e != nil {
				return e
			}

			propertyRows, propertyErr := tx.QueryContext(ctx, `SELECT pr.name,pv.property_type,pv.value_json,COALESCE(o.label,'') FROM records r JOIN property_values pv ON pv.record_id=r.id JOIN properties pr ON pr.id=pv.property_id LEFT JOIN property_options o ON o.id=pv.status_option_id WHERE r.page_id=? ORDER BY pr.id`, page)
			if propertyErr != nil {
				return propertyErr
			}
			propertyText := []string{}
			for propertyRows.Next() {
				var name, typ, raw, label string
				if propertyErr = propertyRows.Scan(&name, &typ, &raw, &label); propertyErr != nil {
					propertyRows.Close()
					return propertyErr
				}
				if typ == "relation" || typ == "files" || typ == "button" {
					continue
				}
				var value any
				if json.Unmarshal([]byte(raw), &value) != nil {
					continue
				}
				text := searchReadable(value)
				if label != "" {
					text = label
				}
				if typ == "number" || typ == "checkbox" {
					text = fmt.Sprint(value)
				}
				if text != "" {
					propertyText = append(propertyText, name+": "+text)
				}
			}
			propertyErr = propertyRows.Err()
			propertyRows.Close()
			if propertyErr != nil {
				return propertyErr
			}
			if len(propertyText) > 0 {
				if err := insert(page+":properties", "", kind, strings.Join(propertyText, "\n"), "", 0, location("")); err != nil {
					return err
				}
			}
			rows, e := tx.QueryContext(ctx, `SELECT b.id,b.data_json,s.id,s.meeting_id,s.start_ms,s.end_ms,s.text FROM blocks b LEFT JOIN transcript_segments s ON s.block_id=b.id WHERE b.page_id=? ORDER BY b.sort_key,b.id`, page)
			if e != nil {
				return e
			}
			type item struct {
				id, raw                string
				segment, meeting, text sql.NullString
				start, end             sql.NullInt64
			}
			items := []item{}
			for rows.Next() {
				var b item
				if e = rows.Scan(&b.id, &b.raw, &b.segment, &b.meeting, &b.start, &b.end, &b.text); e != nil {
					rows.Close()
					return e
				}
				items = append(items, b)
			}
			e = rows.Err()
			rows.Close()
			if e != nil {
				return e
			}
			for _, b := range items {
				var data any
				json.Unmarshal([]byte(b.raw), &data)
				body := searchReadable(data)
				loc := location(b.id)
				blockKind := kind
				if b.segment.Valid {
					blockKind = "transcript"
					if body == "" {
						body = b.text.String
					}
					loc["transcript"] = map[string]any{"segmentId": b.segment.String, "meetingId": b.meeting.String, "startMs": b.start.Int64, "endMs": b.end.Int64}
				}
				for i, chunk := range structuralText(body, 2400) {
					if e = insert(b.id+fmt.Sprintf(":%d", i), b.id, blockKind, chunk, "", 0, loc); e != nil {
						return e
					}
				}
			}
			rows, e = tx.QueryContext(ctx, `SELECT f.id,f.original_name,s.ordinal,s.document_page,s.heading,s.body,COALESCE((SELECT b.id FROM blocks b WHERE b.page_id=? AND json_extract(b.data_json,'$.fileId')=f.id LIMIT 1),'') FROM attachments a JOIN files f ON f.id=a.file_id JOIN document_extractions e ON e.file_id=f.id AND e.file_revision=f.revision AND e.state IN('ready','partial') JOIN document_sections s ON s.file_id=f.id WHERE a.page_id=?`, page, page)
			if e != nil {
				return e
			}
			type section struct {
				id, name, heading, body, block string
				ordinal, doc                   int
			}
			sections := []section{}
			for rows.Next() {
				var s section
				if e = rows.Scan(&s.id, &s.name, &s.ordinal, &s.doc, &s.heading, &s.body, &s.block); e != nil {
					rows.Close()
					return e
				}
				sections = append(sections, s)
			}
			e = rows.Err()
			rows.Close()
			if e != nil {
				return e
			}
			for _, s := range sections {
				if e = insert(page+":"+s.id+fmt.Sprintf(":%d", s.ordinal), s.block, "file", s.name+"\n"+s.heading+"\n"+s.body, s.id, s.doc, location(s.block)); e != nil {
					return e
				}
			}
		}
	}
	_, err = tx.ExecContext(ctx, "DELETE FROM search_dirty WHERE page_id=?", page)
	return err
}

// Split on paragraph/line boundaries, then words; never cut a UTF-8 sequence.
func structuralText(text string, max int) []string {
	result := []string{}
	current := ""
	flush := func() {
		if strings.TrimSpace(current) != "" {
			result = append(result, strings.TrimSpace(current))
		}
		current = ""
	}
	for _, line := range strings.Split(text, "\n") {
		if strings.TrimSpace(line) == "" {
			flush()
			continue
		}
		if len([]rune(current))+len([]rune(line))+1 <= max {
			current += line + "\n"
			continue
		}
		flush()
		for _, word := range strings.Fields(line) {
			r := []rune(word)
			for len(r) > max {
				flush()
				result = append(result, string(r[:max]))
				r = r[max:]
			}
			word = string(r)
			if len([]rune(current))+len([]rune(word))+1 > max {
				flush()
			}
			current += word + " "
		}
	}
	flush()
	return result
}
func literalFTS(s string) string {
	terms := strings.FieldsFunc(s, func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsNumber(r) })
	if len(terms) > 32 {
		terms = terms[:32]
	}
	for i, v := range terms {
		terms[i] = `"` + strings.ReplaceAll(v, `"`, `""`) + `"`
	}
	return strings.Join(terms, " AND ")
}
func canonicalSearch(ctx context.Context, k *Kernel, e contracts.ServiceExecutionContext, r map[string]any) (any, error) {
	result, err := searchQuery(ctx, k, e, r)
	if err != nil {
		return nil, err
	}
	full := result.(SearchResults)
	out := contracts.ServiceSearchResult{Hits: []contracts.ServiceSearchHit{}, SnapshotCursor: full.SnapshotCursor, IndexGeneration: contracts.DomainId(full.IndexGeneration), Partial: full.Partial}
	for _, h := range full.Hits {
		out.Hits = append(out.Hits, h.ServiceSearchHit)
	}
	return out, nil
}
func searchQuery(ctx context.Context, k *Kernel, e contracts.ServiceExecutionContext, r map[string]any) (any, error) {
	query := str(r["query"])
	if len(query) > 4096 {
		return nil, Fail("INVALID_REQUEST", "Search query exceeds 4096 bytes")
	}
	limit := number(r["limit"])
	if limit == 0 {
		limit = 30
	}
	if limit < 1 || limit > 200 {
		return nil, Fail("INVALID_REQUEST", "Search limit must be between 1 and 200")
	}
	mode := str(r["mode"])
	if mode == "" {
		mode = "keyword"
	}
	if mode != "keyword" && mode != "semantic" && mode != "hybrid" {
		return nil, Fail("INVALID_REQUEST", "Unknown search mode")
	}
	kinds := map[string]bool{}
	if values, ok := r["kinds"].([]any); ok {
		for _, v := range values {
			s := str(v)
			if s != "page" && s != "task" && s != "file" && s != "transcript" {
				return nil, Fail("INVALID_REQUEST", "Unknown source kind")
			}
			kinds[s] = true
		}
	}
	var requested *contracts.ServiceExecutionContext
	if raw, ok := r["scope"]; ok {
		if err := contracts.Validate("domain", "Scope", marshal(raw)); err != nil {
			return nil, Fail("INVALID_REQUEST", "Invalid search scope")
		}
		var scope contracts.DomainScope
		if err := json.Unmarshal(marshal(raw), &scope); err != nil {
			return nil, err
		}
		g := k.Grant(e.Actor, e.Origin, scope)
		defer k.RevokeGrant(string(g.ScopeGrantId))
		requested = &g
	}
	if err := k.FlushSearch(ctx); err != nil {
		return nil, err
	}
	tx, err := k.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	out := SearchResults{Hits: []SearchHit{}, Mode: "keyword", SnapshotCursor: "0"}
	if err = tx.QueryRowContext(ctx, "SELECT value FROM search_meta WHERE key='generation'").Scan(&out.IndexGeneration); err != nil {
		return nil, err
	}
	if err = tx.QueryRowContext(ctx, "SELECT COALESCE(MAX(cursor),'0') FROM committed_events").Scan(&out.SnapshotCursor); err != nil {
		return nil, err
	}
	fts := literalFTS(query)
	if fts == "" {
		return out, nil
	}
	sqlQuery := `SELECT c.id,c.title,c.kind,c.body,c.location_json,c.content_hash,COALESCE(c.file_id,''),c.document_page,bm25(search_fts,4.0,1.0) FROM search_fts JOIN search_chunks c ON c.rowid=search_fts.rowid JOIN pages p ON p.id=c.page_id AND p.revision=c.source_revision AND p.deleted_at IS NULL WHERE search_fts MATCH ? AND NOT EXISTS(SELECT 1 FROM memory_entries m WHERE m.page_id=p.id AND m.status<>'active')`
	args := []any{fts}
	if page := str(r["pageId"]); page != "" {
		sqlQuery += " AND c.page_id=?"
		args = append(args, page)
	}
	if len(kinds) > 0 {
		marks := []string{}
		for _, kind := range []string{"page", "task", "file", "transcript"} {
			if kinds[kind] {
				marks = append(marks, "?")
				args = append(args, kind)
			}
		}
		sqlQuery += " AND c.kind IN (" + strings.Join(marks, ",") + ")"
	}
	sqlQuery += " ORDER BY bm25(search_fts,4.0,1.0),c.id"
	wholeWorkspace := func(grant contracts.ServiceExecutionContext) bool {
		scope, ok := grant.Scope.Value.(contracts.DomainScopeVariant1)
		return ok && string(scope.WorkspaceId) == k.id && k.authorize(grant) == nil
	}
	// An unrestricted query can let SQLite keep only the best requested hits.
	// Restricted grants must continue past denied candidates: limiting before
	// authorization would hide valid lower-ranked results. Both paths stream
	// results and stop as soon as the visible limit is filled.
	if wholeWorkspace(e) && (requested == nil || wholeWorkspace(*requested)) {
		sqlQuery += " LIMIT ?"
		args = append(args, limit)
	}
	rows, err := tx.QueryContext(ctx, sqlQuery, args...)
	if err != nil {
		return nil, err
	}
	visible := func(h SearchHit) bool {
		page := string(h.Location.PageId)
		return k.authorizePage(ctx, tx, e, page) == nil && (requested == nil || k.authorizePage(ctx, tx, *requested, page) == nil)
	}
	for rows.Next() {
		var h SearchHit
		var loc string
		var rank float64
		if err = rows.Scan(&h.ChunkID, &h.Title, &h.Kind, &h.Excerpt, &loc, &h.ContentHash, &h.FileID, &h.DocumentPage, &rank); err != nil {
			rows.Close()
			return nil, err
		}
		if err = json.Unmarshal([]byte(loc), &h.Location); err != nil {
			rows.Close()
			return nil, err
		}
		h.Score = -rank
		h.SourceState = "current"
		if visible(h) {
			out.Hits = append(out.Hits, h)
			if len(out.Hits) >= int(limit) {
				break
			}
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	if mode != "keyword" {
		hybrid, warning := k.semanticSearch(ctx, tx, e, requested, r, query, out.Hits, kinds, int(limit))
		if warning != "" {
			out.Partial = true
			out.Warning = warning
		} else {
			out.Hits = hybrid
			out.Mode = mode
		}
	}
	// Native query embedding can outlive its read snapshot. Recheck each
	// location against fresh canonical state before presenting it as current.
	current := make([]SearchHit, 0, len(out.Hits))
	for _, hit := range out.Hits {
		page := string(hit.Location.PageId)
		var live int
		if err := k.db.QueryRowContext(ctx, `SELECT count(*) FROM pages p WHERE p.id=? AND p.revision=? AND p.deleted_at IS NULL AND NOT EXISTS(SELECT 1 FROM memory_entries m WHERE m.page_id=p.id AND (m.status<>'active' OR EXISTS(SELECT 1 FROM memory_sources ms LEFT JOIN pages evidence ON evidence.id=ms.page_id WHERE ms.memory_id=m.id AND (evidence.id IS NULL OR evidence.deleted_at IS NOT NULL OR evidence.revision<>ms.source_revision))))`, page, hit.Location.SourceRevision).Scan(&live); err != nil {
			return nil, err
		}
		if live == 1 && k.authorizePage(ctx, k.db, e, page) == nil && (requested == nil || k.authorizePage(ctx, k.db, *requested, page) == nil) {
			current = append(current, hit)
		}
	}
	out.Hits = current
	return out, nil
}
func searchStatus(ctx context.Context, k *Kernel, e contracts.ServiceExecutionContext, _ map[string]any) (any, error) {
	if err := requireOwnerScope(k, e); err != nil {
		return nil, err
	}
	out := map[string]any{"keywordAvailable": true}
	for key, query := range map[string]string{"chunks": "SELECT count(*) FROM search_chunks", "pendingPages": "SELECT count(*) FROM search_dirty", "extractedFiles": "SELECT count(*) FROM document_extractions WHERE state IN('ready','partial')", "failedFiles": "SELECT count(*) FROM document_extractions WHERE state='failed'", "embeddedChunks": "SELECT count(*) FROM search_embeddings"} {
		var n int
		if err := k.db.QueryRowContext(ctx, query).Scan(&n); err != nil {
			return nil, err
		}
		out[key] = n
	}
	return out, nil
}

// Cosine is shared by hybrid retrieval and tests; invalid/zero vectors cannot rank.
func cosine(a, b []float32) float64 {
	if len(a) == 0 || len(a) != len(b) {
		return -1
	}
	var dot, aa, bb float64
	for i, x := range a {
		y := float64(b[i])
		v := float64(x)
		if math.IsNaN(v) || math.IsInf(v, 0) || math.IsNaN(y) || math.IsInf(y, 0) {
			return -1
		}
		dot += v * y
		aa += v * v
		bb += y * y
	}
	if aa == 0 || bb == 0 {
		return -1
	}
	return dot / math.Sqrt(aa*bb)
}
