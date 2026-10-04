package workspace

import (
	"context"
	"database/sql"
	"encoding/json"
	"math"
	"sort"
	"sync"
	"trove.local/phase0/contracts"
)

type EmbeddingProvider func(context.Context, string, []string, []contracts.DomainLocation) ([][]float32, error)

var embeddingProviders sync.Map

func (k *Kernel) SetEmbeddingProvider(provider EmbeddingProvider) {
	k.mu.Lock()
	defer k.mu.Unlock()
	if provider == nil || k.closed {
		embeddingProviders.Delete(k)
	} else {
		embeddingProviders.Store(k, provider)
	}
}
func (k *Kernel) embed(ctx context.Context, model string, texts []string, locations []contracts.DomainLocation) ([][]float32, error) {
	p, ok := embeddingProviders.Load(k)
	if !ok {
		return nil, Fail("WORKER_FAILED", "Embedding worker is unavailable; keyword search remains available")
	}
	return p.(EmbeddingProvider)(ctx, model, texts, locations)
}
func init() {
	RegisterJobHandler("GENERATE_EMBEDDINGS", func(ctx context.Context, k *Kernel, j contracts.JobsJob) error {
		p, ok := j.Payload.Value.(contracts.JobsJobPayloadVariant4)
		if !ok {
			return Fail("INVALID_REQUEST", "Expected embedding payload")
		}
		e, err := k.JobExecution(ctx, string(j.JobId))
		if err != nil {
			return err
		}
		defer k.RevokeGrant(string(e.ScopeGrantId))
		if err = k.FlushSearch(ctx); err != nil {
			return err
		}
		seen := map[string]bool{}
		chunks := []SearchHit{}
		for _, loc := range p.Locations {
			if err = k.authorizePage(ctx, k.db, e, string(loc.PageId)); err != nil {
				return err
			}
			rows, err := k.db.QueryContext(ctx, "SELECT id,body,location_json,content_hash FROM search_chunks WHERE page_id=? AND source_revision=? AND (?='' OR block_id=?)", loc.PageId, loc.SourceRevision, optionalID(loc.BlockId), optionalID(loc.BlockId))
			if err != nil {
				return err
			}
			for rows.Next() {
				var h SearchHit
				var raw string
				if err = rows.Scan(&h.ChunkID, &h.Excerpt, &raw, &h.ContentHash); err != nil {
					rows.Close()
					return err
				}
				if err = json.Unmarshal([]byte(raw), &h.Location); err != nil {
					rows.Close()
					return err
				}
				if !seen[h.ChunkID] {
					seen[h.ChunkID] = true
					chunks = append(chunks, h)
				}
			}
			err = rows.Err()
			rows.Close()
			if err != nil {
				return err
			}
		}
		total := int64(len(chunks))
		for start := 0; start < len(chunks); start += 32 {
			end := start + 32
			if end > len(chunks) {
				end = len(chunks)
			}
			batch := chunks[start:end]
			texts := []string{}
			locations := []contracts.DomainLocation{}
			for _, h := range batch {
				texts = append(texts, h.Excerpt)
				locations = append(locations, h.Location)
			}
			vectors, err := k.embed(ctx, string(p.ModelPackId), texts, locations)
			if err != nil {
				return err
			}
			if len(vectors) != len(batch) {
				return Fail("WORKER_FAILED", "Embedding result count mismatch")
			}
			for _, v := range vectors {
				if len(v) != int(p.Dimension) || cosine(v, v) < 0 {
					return Fail("WORKER_FAILED", "Embedding dimension or values are invalid")
				}
			}
			err = k.write(ctx, func(tx *sql.Tx) error {
				if _, err := k.checkLease(ctx, tx, string(j.JobId), string(*j.LeaseToken), false); err != nil {
					return err
				}
				for i, h := range batch {
					if err := k.authorizePage(ctx, tx, e, string(h.Location.PageId)); err != nil {
						return err
					}
					var live int
					if err := tx.QueryRowContext(ctx, "SELECT count(*) FROM search_chunks c JOIN pages p ON p.id=c.page_id AND p.revision=c.source_revision AND p.deleted_at IS NULL WHERE c.id=? AND c.content_hash=? AND c.source_revision=?", h.ChunkID, h.ContentHash, h.Location.SourceRevision).Scan(&live); err != nil {
						return err
					}
					if live != 1 {
						return Fail("CONFLICT", "Embedding source changed; retry against the current revision")
					}
					if _, err := tx.ExecContext(ctx, "INSERT OR REPLACE INTO search_embeddings VALUES(?,?,?,?,?)", h.ChunkID, h.ContentHash, p.ModelPackId, p.Dimension, string(marshal(vectors[i]))); err != nil {
						return err
					}
				}
				return nil
			})
			if err != nil {
				return err
			}
			if j.LeaseToken != nil {
				_ = k.ProgressJob(ctx, string(j.JobId), string(*j.LeaseToken), contracts.JobsProgress{Sequence: int64(start / 32), Completed: int64(end), Total: &total, Unit: "items"})
			}
		}
		return nil
	})
}
func optionalID(p *contracts.DomainId) string {
	if p == nil {
		return ""
	}
	return string(*p)
}
func (k *Kernel) semanticSearch(ctx context.Context, tx *sql.Tx, e contracts.ServiceExecutionContext, requested *contracts.ServiceExecutionContext, r map[string]any, query string, keyword []SearchHit, kinds map[string]bool, limit int) ([]SearchHit, string) {
	model := str(r["modelPackId"])
	if model == "" {
		_ = tx.QueryRowContext(ctx, "SELECT model_pack_id FROM search_embeddings ORDER BY model_pack_id LIMIT 1").Scan(&model)
	}
	if model == "" {
		return nil, "No semantic index is ready. Showing keyword results."
	}
	// Query metadata uses a real scoped source location; it is not persisted as evidence.
	var queryLoc contracts.DomainLocation
	if len(keyword) > 0 {
		queryLoc = keyword[0].Location
	} else {
		rows, err := tx.QueryContext(ctx, "SELECT id,revision FROM pages WHERE deleted_at IS NULL ORDER BY id")
		if err != nil {
			return nil, "Semantic source lookup failed. Showing keyword results."
		}
		for rows.Next() {
			var id, rev string
			if rows.Scan(&id, &rev) != nil {
				continue
			}
			if k.authorizePage(ctx, k.db, e, id) == nil && (requested == nil || k.authorizePage(ctx, k.db, *requested, id) == nil) {
				queryLoc = contracts.DomainLocation{PageId: contracts.DomainId(id), SourceRevision: contracts.DomainRevision(rev)}
				break
			}
		}
		rows.Close()
	}
	if queryLoc.PageId == "" {
		return nil, "No accessible sources for semantic search."
	}
	vectors, err := k.embed(ctx, model, []string{query}, []contracts.DomainLocation{queryLoc})
	if err != nil || len(vectors) != 1 || cosine(vectors[0], vectors[0]) < 0 {
		return nil, "Embedding worker unavailable. Showing keyword results."
	}
	rows, err := tx.QueryContext(ctx, `SELECT c.id,c.title,c.kind,c.body,c.location_json,c.content_hash,COALESCE(c.file_id,''),c.document_page,v.vector_json FROM search_embeddings v JOIN search_chunks c ON c.id=v.chunk_id AND c.content_hash=v.content_hash JOIN pages p ON p.id=c.page_id AND p.revision=c.source_revision AND p.deleted_at IS NULL WHERE v.model_pack_id=? AND NOT EXISTS(SELECT 1 FROM memory_entries m WHERE m.page_id=p.id AND m.status<>'active')`, model)
	if err != nil {
		return nil, "Semantic index unavailable. Showing keyword results."
	}
	candidates := []SearchHit{}
	for rows.Next() {
		var h SearchHit
		var loc, raw string
		if rows.Scan(&h.ChunkID, &h.Title, &h.Kind, &h.Excerpt, &loc, &h.ContentHash, &h.FileID, &h.DocumentPage, &raw) != nil {
			continue
		}
		if json.Unmarshal([]byte(loc), &h.Location) != nil {
			continue
		}
		var v []float32
		if json.Unmarshal([]byte(raw), &v) != nil {
			continue
		}
		h.Score = cosine(vectors[0], v)
		h.SourceState = "current"
		if h.Score < 0 || math.IsNaN(h.Score) {
			continue
		}
		candidates = append(candidates, h)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, "Semantic index read failed. Showing keyword results."
	}
	visible := []SearchHit{}
	for _, h := range candidates {
		page := string(h.Location.PageId)
		if (len(kinds) == 0 || kinds[h.Kind]) && (str(r["pageId"]) == "" || str(r["pageId"]) == page) && k.authorizePage(ctx, tx, e, page) == nil && (requested == nil || k.authorizePage(ctx, tx, *requested, page) == nil) {
			visible = append(visible, h)
		}
	}
	sort.Slice(visible, func(i, j int) bool {
		if visible[i].Score == visible[j].Score {
			return visible[i].ChunkID < visible[j].ChunkID
		}
		return visible[i].Score > visible[j].Score
	})
	if str(r["mode"]) == "hybrid" {
		byID := map[string]SearchHit{}
		for i, h := range keyword {
			h.Score = 1 / float64(60+i+1)
			byID[h.ChunkID] = h
		}
		for i, h := range visible {
			score := 1 / float64(60+i+1)
			if previous, ok := byID[h.ChunkID]; ok {
				h.Score = previous.Score + score
			} else {
				h.Score = score
			}
			byID[h.ChunkID] = h
		}
		visible = []SearchHit{}
		for _, h := range byID {
			visible = append(visible, h)
		}
		sort.Slice(visible, func(i, j int) bool {
			if visible[i].Score == visible[j].Score {
				return visible[i].ChunkID < visible[j].ChunkID
			}
			return visible[i].Score > visible[j].Score
		})
	}
	if len(visible) > limit {
		visible = visible[:limit]
	}
	return visible, ""
}
