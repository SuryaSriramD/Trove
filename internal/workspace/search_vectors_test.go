package workspace

import (
	"context"
	"database/sql"
	"fmt"
	"testing"

	"trove.local/phase0/contracts"
)

func TestVectorGenerationChangeInvalidatesOnlyDerivedEvidence(t *testing.T) {
	k, e, page, _, model := aiSemanticFixture(t)
	ctx := context.Background()
	before, err := readPageTx(ctx, k.db, page)
	if err != nil {
		t.Fatal(err)
	}
	k.SetEmbeddingIdentityProvider(func(context.Context, string) (string, error) { return "new-model-hash", nil })
	out := querySearch(t, k, e, "Canine", map[string]any{"mode": "hybrid", "modelPackId": model})
	if out["mode"] != "keyword" || out["warning"] == "" {
		t.Fatal("incompatible model returned semantic results", out)
	}
	if err = k.QueueSemanticUpdates(ctx, e); err != nil {
		t.Fatal(err)
	}
	var n int
	if err = k.db.QueryRow("SELECT count(*) FROM search_embeddings WHERE model_pack_id=?", model).Scan(&n); err != nil || n != 0 {
		t.Fatal("old vectors retained", n, err)
	}
	var title string
	if err = k.db.QueryRow("SELECT title FROM pages WHERE id=?", page).Scan(&title); err != nil || title != "Canine care requires daily exercise" {
		t.Fatal("canonical content changed", title, err)
	}
	after, err := readPageTx(ctx, k.db, page)
	if err != nil {
		t.Fatal(err)
	}
	if string(marshal(before.Page)) != string(marshal(after.Page)) || string(marshal(before.Blocks)) != string(marshal(after.Blocks)) {
		t.Fatal("rebuild changed canonical content")
	}
}

func TestVectorGenerationChangeDuringInferenceCannotPublishOldCoordinates(t *testing.T) {
	k, e, _, _, model := aiSemanticFixture(t)
	k.SetEmbeddingProvider(func(context.Context, string, []string, []contracts.DomainLocation) ([][]float32, error) {
		k.SetEmbeddingIdentityProvider(func(context.Context, string) (string, error) { return "replacement-model", nil })
		return [][]float32{{1, 0}}, nil
	})
	out := querySearch(t, k, e, "Canine", map[string]any{"mode": "hybrid", "modelPackId": model})
	if out["mode"] != "keyword" || out["warning"] == "" {
		t.Fatal(out)
	}
}

func TestVectorScopeFiltersBeforeNearestNeighbourLimitAndDeletes(t *testing.T) {
	k, e, allowed, _, model := aiSemanticFixture(t)
	ctx := context.Background()
	// Add many closer, inaccessible candidates. Filtering after KNN LIMIT
	// would return no visible result even though an allowed neighbour exists.
	for i := 0; i < 24; i++ {
		page := createPage(t, k, e, fmt.Sprintf("Private close neighbour %d", i))
		if err := k.FlushSearch(ctx); err != nil {
			t.Fatal(err)
		}
		err := k.write(ctx, func(tx *sql.Tx) error {
			var h SearchHit
			if err := tx.QueryRow("SELECT id,content_hash FROM search_chunks WHERE page_id=? LIMIT 1", page).Scan(&h.ChunkID, &h.ContentHash); err != nil {
				return err
			}
			var table string
			if err := tx.QueryRow("SELECT table_name FROM search_vector_models WHERE model_pack_id=?", model).Scan(&table); err != nil {
				return err
			}
			return storeSearchVector(ctx, tx, table, h, model, []float32{1, 0})
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	scoped := k.Grant(e.Actor, "user", contracts.DomainScope{Value: contracts.DomainScopeVariant3{Kind: "pages", WorkspaceId: contracts.DomainId(k.ID()), PageIds: []contracts.DomainId{contracts.DomainId(allowed)}}})
	defer k.RevokeGrant(string(scoped.ScopeGrantId))
	out := querySearch(t, k, scoped, "dog", map[string]any{"mode": "semantic", "modelPackId": model, "limit": 1})
	hits := out["hits"].([]any)
	if out["mode"] != "semantic" || len(hits) != 1 || hits[0].(map[string]any)["location"].(map[string]any)["pageId"] != allowed {
		t.Fatal(out)
	}
	testOK(t, dbBatch(t, k, e, []any{map[string]any{"kind": "page.trash", "pageId": allowed, "includeDescendants": false}}, "page", allowed))
	out = querySearch(t, k, scoped, "dog", map[string]any{"mode": "semantic", "modelPackId": model, "limit": 1})
	if len(out["hits"].([]any)) != 0 {
		t.Fatal("trashed source survived", out)
	}
}
