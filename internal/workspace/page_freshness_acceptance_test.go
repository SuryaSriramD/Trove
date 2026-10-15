package workspace

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"trove.local/phase0/contracts"
)

func TestPageFreshnessMeetingMetadataCoverage(t *testing.T) {
	k, e := testKernel(t)
	page, meeting, _ := meetingFixture(t, k, e)
	block, segment := NewID(), NewID()
	testOK(t, dbBatch(t, k, e, []any{map[string]any{"kind": "meeting.appendSegments", "meetingId": meeting, "segments": []any{map[string]any{"segmentId": segment, "blockId": block, "startMs": 100, "endMs": 800, "text": "Ship the reviewed plan", "speakerLabel": "Speaker 1"}}}}, "page", page, "meeting", meeting, "block", block))
	identity := testFreshness(t, k, page).ContentIdentity
	testOK(t, dbBatch(t, k, e, []any{map[string]any{"kind": "page.review", "pageId": page, "contentIdentity": identity}}, "page", page))
	// Simulate a timing/speaker correction without changing paragraph text.
	if _, err := k.db.Exec("UPDATE transcript_segments SET speaker_label='Speaker 2',start_ms=200 WHERE id=?", segment); err != nil {
		t.Fatal(err)
	}
	if got := testFreshness(t, k, page); got.State != "stale" {
		t.Fatal("Transcript speaker/timing changes were outside review coverage", got)
	}
	identity = testFreshness(t, k, page).ContentIdentity
	testOK(t, dbBatch(t, k, e, []any{map[string]any{"kind": "page.review", "pageId": page, "contentIdentity": identity}}, "page", page))
	if _, err := k.db.Exec("UPDATE meetings SET started_at='2026-10-15T12:00:00Z' WHERE id=?", meeting); err != nil {
		t.Fatal(err)
	}
	if got := testFreshness(t, k, page); got.State != "stale" {
		t.Fatal("Meeting date changes were outside review coverage", got)
	}
}
func TestPageFreshnessDirectTodoReviewRequiresTaskScope(t *testing.T) {
	k, e := testKernel(t)
	f := newDatabase(t, k, e)
	record, taskPage := newRecord(t, k, e, f, "Private task")
	testOK(t, dbBatch(t, k, e, []any{map[string]any{"kind": "task.promote", "recordId": record, "statusPropertyId": f.status, "duePropertyId": f.date}}, "page", taskPage, "record", record, "collection", f.collection, "property", f.status, "property", f.date))
	page := createPage(t, k, e, "Occurrence")
	block := NewID()
	testOK(t, dbBatch(t, k, e, []any{map[string]any{"kind": "block.insert", "block": map[string]any{"blockId": block, "pageId": page, "type": "todo", "schemaVersion": 1, "data": map[string]any{"taskRecordId": record}}}}, "page", page, "block", block))
	scoped := k.Grant(e.Actor, "user", contracts.DomainScope{Value: contracts.DomainScopeVariant3{Kind: "pages", WorkspaceId: contracts.DomainId(k.ID()), PageIds: []contracts.DomainId{contracts.DomainId(page)}}})
	identity := testFreshness(t, k, page).ContentIdentity
	testCode(t, dbBatch(t, k, scoped, []any{map[string]any{"kind": "page.review", "pageId": page, "contentIdentity": identity}}, "page", page), "SCOPE_DENIED")
	if got := testFreshness(t, k, page); got.Review != nil {
		t.Fatal("Denied task review persisted", got)
	}
}
func TestPageFreshnessNativeRestoreDeadlineAndMemoryIndependence(t *testing.T) {
	k, e := testKernel(t)
	page := createPage(t, k, e, "Original decision")
	_, _ = memoryFixture(t, k, e, page)
	testOK(t, dbBatch(t, k, e, []any{map[string]any{"kind": "page.update", "pageId": page, "title": "Contradictory decision"}}, "page", page))
	expiry := time.Now().Add(time.Hour)
	identity := testFreshness(t, k, page).ContentIdentity
	testOK(t, dbBatch(t, k, e, []any{map[string]any{"kind": "page.review", "pageId": page, "contentIdentity": identity, "expiresAt": expiry.UTC().Format(time.RFC3339Nano)}}, "page", page))
	review := testFreshness(t, k, page).Review
	if entries, err := readMemories(context.Background(), k, k.db, e, false); err != nil || len(entries) != 0 {
		t.Fatal("Human page review reactivated stale memory", entries, err)
	}
	backup := filepath.Join(t.TempDir(), "backup")
	if _, err := k.CreateBackup(context.Background(), e, backup); err != nil {
		t.Fatal(err)
	}
	destination := filepath.Join(t.TempDir(), "restored")
	if _, err := RestoreBackup(context.Background(), backup, destination); err != nil {
		t.Fatal(err)
	}
	restored, err := Open(destination)
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()
	got, err := pageFreshness(context.Background(), restored.db, page, expiry.Add(time.Minute))
	if err != nil || got.State != "expired" || got.Review.Id != review.Id || got.Review.ReviewedAt != review.ReviewedAt {
		t.Fatal("Restore renewed the review/deadline", got, err)
	}
	if entries, err := readMemories(context.Background(), restored, restored.db, restored.OwnerContext(), false); err != nil || len(entries) != 0 {
		t.Fatal("Restore reactivated stale memory", entries, err)
	}
}

func TestPageFreshnessOldCoverageRemainsAttributable(t *testing.T) {
	k, e := testKernel(t)
	page := createPage(t, k, e, "Older review")
	identity := testFreshness(t, k, page).ContentIdentity
	testOK(t, dbBatch(t, k, e, []any{map[string]any{"kind": "page.review", "pageId": page, "contentIdentity": identity}}, "page", page))
	review := testFreshness(t, k, page).Review
	for _, version := range []int{1, 2} {
		if _, err := k.db.Exec("UPDATE page_review_events SET identity_version=? WHERE id=?", version, review.Id); err != nil {
			t.Fatal(err)
		}
		result := testOK(t, testDispatch(t, k, e, "page.freshness", advancedRequest(k, map[string]any{"pageId": page})))
		if err := contracts.Validate("service", "PageFreshnessResult", marshal(result)); err != nil {
			t.Fatal(err)
		}
		got := testFreshness(t, k, page)
		if got.State != "stale" || got.ContentIdentity.Version != 3 || got.Review.ContentIdentity.Version != int64(version) || got.Review.Id != review.Id || got.Review.ReviewedAt != review.ReviewedAt {
			t.Fatal("Older coverage silently renewed or lost attribution", got)
		}
	}
}

func TestPageFreshnessStaleEvidenceRemainsInspectable(t *testing.T) {
	k, e := testKernel(t)
	page := createPage(t, k, e, "Evidence orchard original decision")
	identity := testFreshness(t, k, page).ContentIdentity
	testOK(t, dbBatch(t, k, e, []any{map[string]any{"kind": "page.review", "pageId": page, "contentIdentity": identity}}, "page", page))
	testOK(t, dbBatch(t, k, e, []any{map[string]any{"kind": "page.update", "pageId": page, "title": "Evidence orchard changed decision"}}, "page", page))
	hits := testOK(t, testDispatch(t, k, e, "search.query", advancedRequest(k, map[string]any{"query": "orchard", "mode": "keyword", "limit": 10})))["hits"].([]any)
	if len(hits) == 0 || hits[0].(map[string]any)["freshness"].(map[string]any)["state"] != "stale" {
		t.Fatal("Stale knowledge was hidden or mislabeled", hits)
	}
	citation := testOK(t, testDispatch(t, k, e, "resolveCitation", advancedRequest(k, map[string]any{"location": map[string]any{"pageId": page, "sourceRevision": "3"}})))
	if citation["freshness"].(map[string]any)["state"] != "stale" {
		t.Fatal(citation)
	}
	prepared, err := k.PrepareAIWithBudget(context.Background(), e, AIRequest{PageID: page, Mode: "answer", Prompt: "What is the orchard decision?"}, wordBudget())
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, source := range prepared.Run.Sources {
		if string(source.Location.PageId) == page {
			found = true
			if source.Freshness == nil || source.Freshness.State != "stale" {
				t.Fatal("AI evidence lost freshness", source)
			}
		}
	}
	if !found {
		t.Fatal("Stale ordinary page disappeared from deliberate AI context")
	}
}
