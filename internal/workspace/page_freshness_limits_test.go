package workspace

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"
	"trove.local/phase0/contracts"
)

func TestFreshnessLimitKeepsSearchAIAndQueueUsable(t *testing.T) {
	k, e := testKernel(t)
	f := newDatabase(t, k, e)
	testOK(t, dbBatch(t, k, e, []any{map[string]any{"kind": "page.update", "pageId": f.page, "title": "Vela collection evidence"}}, "page", f.page))
	first := testFreshness(t, k, f.page)
	testOK(t, dbBatch(t, k, e, []any{map[string]any{"kind": "page.review", "pageId": f.page, "contentIdentity": first.ContentIdentity}}, "page", f.page))
	prior := testFreshness(t, k, f.page).Review
	// Bulk canonical fixture setup follows the existing 10k database fixture;
	// this exercises a correctness boundary, not a performance measurement.
	err := k.write(context.Background(), func(tx *sql.Tx) error {
		for i := 0; i < 1001; i++ {
			page, record := NewID(), NewID()
			if _, err := tx.Exec("INSERT INTO pages(id,workspace_id,title,revision,created_at,updated_at) VALUES(?,?,?,'1',?,?)", page, k.ID(), fmt.Sprintf("Row %d", i), now(), now()); err != nil {
				return err
			}
			if _, err := tx.Exec("INSERT INTO records(id,collection_id,page_id,sort_key,revision) VALUES(?,?,?,?,'1')", record, f.collection, page, fmt.Sprintf("%012d", i)); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Run("freshness", func(t *testing.T) {
		got, err := pageFreshness(context.Background(), k.db, f.page, time.Now())
		if err != nil || got.State != "unavailable" {
			t.Fatalf("freshness should degrade explicitly, got %+v err %v", got, err)
		}
		if got.ContentIdentity != nil || got.Message == nil {
			t.Fatal("unavailable state fabricated a computed identity")
		}
		if err := contracts.Validate("domain", "PageFreshness", marshal(got)); err != nil {
			t.Fatal(err)
		}
		var wire map[string]any
		_ = json.Unmarshal(marshal(got), &wire)
		if _, ok := wire["contentIdentity"]; ok {
			t.Fatal("unknown digest was serialized")
		}
		if got.Review == nil || got.Review.Id != prior.Id {
			t.Fatal("attributable review was lost")
		}
	})
	t.Run("search", func(t *testing.T) {
		result := querySearch(t, k, e, "Vela", nil)
		hits := result["hits"].([]any)
		if len(hits) != 1 {
			t.Fatalf("search lost collection page: %+v", result)
		}
		if hits[0].(map[string]any)["freshness"].(map[string]any)["state"] != "unavailable" {
			t.Fatal("search falsely presents review")
		}
	})
	t.Run("ai", func(t *testing.T) {
		sources, _, err := k.aiSources(context.Background(), e, AIRequest{Mode: "answer", PageID: f.page, Prompt: "Vela"})
		if err != nil || len(sources) == 0 {
			t.Fatal("AI evidence unavailable due only to review bound", err)
		}
		if sources[0].Freshness == nil || sources[0].Freshness.State != "unavailable" {
			t.Fatal("missing explicit unavailable status")
		}
	})
	t.Run("queue", func(t *testing.T) {
		_, err := pageReviewQueue(context.Background(), k, e, map[string]any{"limit": 30})
		if err != nil {
			t.Fatal("review queue aborted at one bounded graph", err)
		}
	})
	t.Run("cancel_is_not_hidden", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		_, err := pageFreshness(ctx, k.db, f.page, time.Now())
		if !errors.Is(err, context.Canceled) {
			t.Fatal("cancellation hidden as freshness status", err)
		}
	})
	t.Run("new_review_stays_guarded", func(t *testing.T) {
		testCode(t, dbBatch(t, k, e, []any{map[string]any{"kind": "page.review", "pageId": f.page, "contentIdentity": first.ContentIdentity}}, "page", f.page), "RESOURCE_EXHAUSTED")
	})
}
