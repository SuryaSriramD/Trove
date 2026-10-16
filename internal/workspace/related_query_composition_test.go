package workspace

import (
	"context"
	"database/sql"
	"sort"
	"strings"
	"testing"

	"trove.local/phase0/contracts"
)

func captureRelatedQuery(t *testing.T, k *Kernel, e contracts.ServiceExecutionContext, page, model string) string {
	t.Helper()
	queries := []string{}
	k.SetEmbeddingProvider(func(_ context.Context, got string, texts []string, _ []contracts.DomainLocation) ([][]float32, error) {
		if got != model || len(texts) != 1 {
			t.Errorf("unexpected query request: model=%s texts=%q", got, texts)
		}
		queries = append(queries, texts...)
		return [][]float32{{1, 0}}, nil
	})
	relatedResult(t, k, e, page)
	if len(queries) != 1 {
		t.Fatalf("expected one captured query, got %q", queries)
	}
	return queries[0]
}

func TestRelatedQueryCompositionStableAcrossIndexOrder(t *testing.T) {
	k, e, _, _, model := aiSemanticFixture(t)
	ctx := context.Background()
	page := createPage(t, k, e, "Field notebook")
	ids := []string{NewID(), NewID()}
	sort.Strings(ids)
	// Deliberately insert in the opposite order to stable chunk IDs.
	for i := 1; i >= 0; i-- {
		text := []string{"Early sample.", "Later sample."}[i]
		testOK(t, dbBatch(t, k, e, []any{map[string]any{"kind": "block.insert", "block": map[string]any{"blockId": ids[i], "pageId": page, "type": "paragraph", "schemaVersion": 1, "data": map[string]any{"richText": []any{map[string]any{"text": text}}}}}}, "page", page, "block", ids[i]))
	}
	if err := k.FlushSearch(ctx); err != nil {
		t.Fatal(err)
	}
	want := "Field notebook\nEarly sample. Later sample."
	check := func(label string) {
		t.Helper()
		if got := captureRelatedQuery(t, k, e, page, model); got != want {
			t.Errorf("%s query differs: got %q want %q", label, got, want)
		}
	}
	check("initial index")
	// Change only the derived chunk insertion order, retaining all exact rows.
	if err := k.write(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, "CREATE TEMP TABLE reordered_related_chunks AS SELECT * FROM search_chunks WHERE page_id=?", page); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, "DELETE FROM search_chunks WHERE page_id=?", page); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, "INSERT INTO search_chunks SELECT * FROM reordered_related_chunks ORDER BY id DESC"); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, "DROP TABLE reordered_related_chunks")
		return err
	}); err != nil {
		t.Fatal(err)
	}
	check("reversed derived insertion")
	if err := k.write(ctx, func(tx *sql.Tx) error { return indexSearchPage(ctx, tx, page) }); err != nil {
		t.Fatal(err)
	}
	check("production reindex")
}

func TestRelatedQueryCompositionPreservesTitlePropertiesAndBody(t *testing.T) {
	k, e, _, _, model := aiSemanticFixture(t)
	ctx := context.Background()
	t.Run("title_only", func(t *testing.T) {
		page := createPage(t, k, e, "Rare pigment reference")
		if got := captureRelatedQuery(t, k, e, page, model); got != "Rare pigment reference" {
			t.Fatalf("title-only page changed: %q", got)
		}
	})
	t.Run("body_equal_to_title", func(t *testing.T) {
		page, _ := workflowParagraph(t, k, e, "A retained statement", "A retained statement")
		if got := captureRelatedQuery(t, k, e, page, model); got != "A retained statement\nA retained statement" {
			t.Fatalf("real body text equal to the title was lost or multiplied: %q", got)
		}
	})
	t.Run("record_properties", func(t *testing.T) {
		f := newDatabase(t, k, e)
		r, page := newRecord(t, k, e, f, "Specimen register")
		testOK(t, setValue(t, k, e, f, r, page, f.status, "status", f.open))
		property := NewID()
		testOK(t, dbBatch(t, k, e, []any{map[string]any{"kind": "collection.addProperty", "collectionId": f.collection, "propertyId": property, "name": "Reference", "type": "text", "schemaVersion": 1, "config": map[string]any{}}}, "collection", f.collection, "property", property, "page", f.page))
		testOK(t, setValue(t, k, e, f, r, page, property, "text", []any{map[string]any{"text": "Specimen register"}}))
		block := NewID()
		testOK(t, dbBatch(t, k, e, []any{map[string]any{"kind": "block.insert", "block": map[string]any{"blockId": block, "pageId": page, "type": "paragraph", "schemaVersion": 1, "data": map[string]any{"richText": []any{map[string]any{"text": "Inspection remains pending."}}}}}}, "page", page, "block", block, "record", r, "collection", f.collection))
		if err := k.FlushSearch(ctx); err != nil {
			t.Fatal(err)
		}
		var properties string
		if err := k.db.QueryRowContext(ctx, "SELECT body FROM search_chunks WHERE id=?", page+":properties").Scan(&properties); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(properties, "Status: Open") || !strings.Contains(properties, "Specimen register") {
			t.Fatalf("fixture lacks retained canonical property facts: %q", properties)
		}
		parts := map[string]string{page + ":properties": properties, block + ":0": "Inspection remains pending."}
		keys := []string{page + ":properties", block + ":0"}
		sort.Strings(keys)
		want := "Specimen register\n" + parts[keys[0]] + " " + parts[keys[1]]
		if got := captureRelatedQuery(t, k, e, page, model); got != want {
			t.Fatalf("record knowledge changed: got %q want %q", got, want)
		}
	})
}
