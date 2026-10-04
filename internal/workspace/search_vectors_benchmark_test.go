package workspace

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"testing"

	"trove.local/phase0/contracts"
)

// This measures the real SQLite/vec0 retrieval path over 10k 384-dimensional
// vectors; query embeddings are deterministic, so no model latency is claimed.
func BenchmarkHybridVec10K(b *testing.B) {
	k, err := Create(filepath.Join(b.TempDir(), "workspace"))
	if err != nil {
		b.Fatal(err)
	}
	defer k.Close()
	ctx := context.Background()
	e := k.OwnerContext()
	model := NewID()
	query := make([]float32, 384)
	query[0] = 1
	k.SetEmbeddingProvider(func(context.Context, string, []string, []contracts.DomainLocation) ([][]float32, error) {
		return [][]float32{query}, nil
	})
	identity, err := k.embeddingIdentity(ctx, model)
	if err != nil {
		b.Fatal(err)
	}
	err = k.write(ctx, func(tx *sql.Tx) error {
		table, err := ensureVectorGeneration(ctx, tx, model, identity, 384)
		if err != nil {
			return err
		}
		for i := 0; i < 10000; i++ {
			page := NewID()
			title := fmt.Sprintf("Research note %d", i)
			stamp := now()
			if _, err = tx.Exec("INSERT INTO pages(id,workspace_id,title,revision,created_at,updated_at) VALUES(?,?,?,'1',?,?)", page, k.ID(), title, stamp, stamp); err != nil {
				return err
			}
			h := SearchHit{ChunkID: searchDigest(page), Title: title, Kind: "page"}
			h.ContentHash = searchDigest(title)
			h.Location = contracts.DomainLocation{PageId: contracts.DomainId(page), SourceRevision: "1"}
			if _, err = tx.Exec("INSERT INTO search_chunks(id,page_id,source_revision,kind,title,body,location_json,content_hash) VALUES(?,?,'1','page',?,?,?,?)", h.ChunkID, page, title, title, string(marshal(h.Location)), h.ContentHash); err != nil {
				return err
			}
			v := make([]float32, 384)
			v[0] = 1
			v[1] = float32(i) / 10000
			if err = storeSearchVector(ctx, tx, table, h, model, v); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		r, err := searchQuery(ctx, k, e, map[string]any{"query": "conceptual question", "mode": "hybrid", "modelPackId": model, "limit": float64(12)})
		if err != nil {
			b.Fatal(err)
		}
		out := r.(SearchResults)
		if out.Mode != "hybrid" || len(out.Hits) != 12 {
			b.Fatal(out)
		}
	}
}
