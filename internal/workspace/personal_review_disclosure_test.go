package workspace

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestPersonalReviewCaptureOmissionCountIsLowerBound(t *testing.T) {
	k, e := testKernel(t)
	initial, source, planBlock := generativeReviewFixture(t, k, e)
	if strings.Contains(initial.Warning, "additional excerpts") {
		t.Fatal("unbounded omission warning for fully captured source", initial.Warning)
	}
	for n := 0; n < 6; n++ {
		block := NewID()
		testOK(t, dbBatch(t, k, e, []any{map[string]any{"kind": "block.insert", "block": map[string]any{"blockId": block, "pageId": source, "type": "paragraph", "schemaVersion": 1, "data": map[string]any{"richText": []any{map[string]any{"text": fmt.Sprintf("Unrelated inventory section %d.", n)}}}}}}, "page", source, "block", block))
	}
	ctx := context.Background()
	plan, err := readReviewPlan(ctx, k.db, planBlock)
	if err != nil {
		t.Fatal(err)
	}
	run, err := k.preparePersonalReview(ctx, e, plan, "manual:bounded-disclosure", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	var total int
	if err = k.db.QueryRowContext(ctx, "SELECT count(*) FROM search_chunks WHERE page_id=?", source).Scan(&total); err != nil {
		t.Fatal(err)
	}
	captured := 0
	for _, evidence := range run.Evidence {
		if string(evidence.Location.PageId) == source {
			captured++
		}
	}
	if total != 8 || captured != 4 || total-captured != 4 {
		t.Fatalf("fixture must have four omitted excerpts: total=%d captured=%d", total, captured)
	}
	// Capture reads only one extra row to detect the quota; it does not count
	// all four omitted excerpts. The persisted warning must state a lower bound.
	const warning = "At least 1 additional excerpts were omitted by the capture bound."
	if !strings.Contains(run.Warning, warning) {
		t.Fatalf("sentinel presented as an exact omitted count: %q", run.Warning)
	}
	stored, err := readPersonalReviewRun(ctx, k.db, run.ID)
	if err != nil || stored.Warning != run.Warning {
		t.Fatal("persisted review lost its omission disclosure", stored.Warning, err)
	}
}
