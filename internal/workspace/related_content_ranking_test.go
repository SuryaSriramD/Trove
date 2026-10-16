package workspace

import (
	"context"
	"database/sql"
	"strings"
	"testing"

	"trove.local/phase0/contracts"
)

// Independently chosen coordinates make a generic title closer than a relevant
// body. These test ranking/authority plumbing, never real embedding quality.
func TestRelatedContentRankingUsesBodyBeforeLimitAndKeepsTitleSearch(t *testing.T) {
	ctx := context.Background()
	k, owner, _, _, model := aiSemanticFixture(t)
	source, _ := workflowParagraph(t, k, owner, "Current research", "Retain a reserve for unexpected costs.")
	relevant, _ := workflowParagraph(t, k, owner, "Planning notes", "Keep contingency money for unforeseen expenses.")
	distractor, _ := workflowParagraph(t, k, owner, "Related material", "A workshop discusses violin finger positions.")
	empty := createPage(t, k, owner, "Reserve planning glossary")
	blank, _ := workflowParagraph(t, k, owner, "Blank body reference", "  \n\t")
	f := newDatabase(t, k, owner)
	record, properties := newRecord(t, k, owner, f, "Register")
	testOK(t, setValue(t, k, owner, f, record, properties, f.status, "status", f.open))
	if err := k.FlushSearch(ctx); err != nil {
		t.Fatal(err)
	}
	if err := k.write(ctx, func(tx *sql.Tx) error {
		var table string
		if err := tx.QueryRow("SELECT table_name FROM search_vector_models WHERE model_pack_id=?", model).Scan(&table); err != nil {
			return err
		}
		rows, err := tx.Query("SELECT id,page_id,content_hash FROM search_chunks")
		if err != nil {
			return err
		}
		type item struct {
			hit  SearchHit
			page string
		}
		items := []item{}
		for rows.Next() {
			var x item
			if err := rows.Scan(&x.hit.ChunkID, &x.page, &x.hit.ContentHash); err != nil {
				rows.Close()
				return err
			}
			items = append(items, x)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return err
		}
		for _, x := range items {
			vector := []float32{0, 1}
			switch {
			case x.hit.ChunkID == distractor+":title":
				vector = []float32{1, 0}
			case x.page == relevant && !strings.HasSuffix(x.hit.ChunkID, ":title"):
				vector = []float32{.9, .1}
			case x.page == empty || x.page == blank:
				vector = []float32{.8, .2}
			case x.hit.ChunkID == properties+":properties":
				vector = []float32{.7, .3}
			}
			if err := storeSearchVector(ctx, tx, table, x.hit, model, vector); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	k.SetEmbeddingProvider(func(context.Context, string, []string, []contracts.DomainLocation) ([][]float32, error) {
		return [][]float32{{1, 0}}, nil
	})
	scoped := relatedScope(k, owner, source, relevant, distractor, empty, blank, properties)
	defer k.RevokeGrant(string(scoped.ScopeGrantId))
	t.Run("body_before_candidate_limit", func(t *testing.T) {
		result, err := configuredRelatedSearch(ctx, k, scoped, "reserve", source, 1)
		if err != nil || result.Mode != "semantic" || len(result.Hits) != 1 || string(result.Hits[0].Location.PageId) != relevant || result.Hits[0].Excerpt != "Keep contingency money for unforeseen expenses." {
			t.Fatalf("generic title displaced substantive evidence: %s, %v", marshal(result), err)
		}
	})
	t.Run("title_only_and_record_values", func(t *testing.T) {
		result, err := configuredRelatedSearch(ctx, k, scoped, "reserve", source, 20)
		if err != nil {
			t.Fatal(err)
		}
		seen := map[string]bool{}
		for _, hit := range result.Hits {
			seen[hit.ChunkID] = true
		}
		if !seen[empty+":title"] || !seen[blank+":title"] || !seen[properties+":properties"] || seen[properties+":title"] || seen[relevant+":title"] || seen[distractor+":title"] {
			t.Fatalf("content/title eligibility differs: %s", marshal(result))
		}
	})
	t.Run("ordinary_page_search_unchanged", func(t *testing.T) {
		raw, err := searchQuery(ctx, k, scoped, map[string]any{"query": "reserve", "mode": "semantic", "modelPackId": model, "excludePageId": source, "limit": int64(1)})
		if err != nil {
			t.Fatal(err)
		}
		result := raw.(SearchResults)
		if len(result.Hits) != 1 || result.Hits[0].ChunkID != distractor+":title" {
			t.Fatalf("ordinary search inherited related-only ranking: %s", marshal(result))
		}
	})
	t.Run("disabled_semantics_keeps_bounded_keyword_fallback", func(t *testing.T) {
		testOK(t, testDispatch(t, k, owner, "search.configureSemantic", recoveryRequest(k, map[string]any{"enabled": false})))
		k.SetEmbeddingProvider(func(context.Context, string, []string, []contracts.DomainLocation) ([][]float32, error) {
			t.Error("disabled embedding model was invoked")
			return nil, nil
		})
		result, err := configuredRelatedSearch(ctx, k, scoped, "reserve", source, 1)
		if err != nil || result.Mode != "keyword" || result.Warning == "" || len(result.Hits) != 1 || result.Hits[0].ChunkID != empty+":title" {
			t.Fatalf("disabled related retrieval lost its keyword/title-only fallback: %s, %v", marshal(result), err)
		}
	})
}
