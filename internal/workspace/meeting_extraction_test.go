package workspace

import (
	"context"
	"sync"
	"testing"

	"trove.local/phase0/contracts"
)

func extractionFixture(t *testing.T, k *Kernel, e contracts.ServiceExecutionContext) (string, MeetingSource) {
	t.Helper()
	_, meeting, file := meetingFixture(t, k, e)
	job := enqueueTestJob(t, k, e, "TRANSCRIBE_AUDIO", map[string]any{"kind": "transcribe", "meetingId": meeting, "fileId": file, "modelPackId": NewID()})
	job, err := k.ClaimJob(context.Background(), []string{"TRANSCRIBE_AUDIO"})
	if err != nil {
		t.Fatal(err)
	}
	execution, err := k.JobExecution(context.Background(), string(job.JobId))
	if err != nil {
		t.Fatal(err)
	}
	result := k.CommitJobBatch(context.Background(), execution, string(job.JobId), string(*job.LeaseToken), "extraction-fixture", jobTranscriptRequest(t, k, execution, job, meeting))
	if failure, ok := result.Value.(contracts.ServiceBatchResponseVariant2); ok {
		t.Fatal(failure.Error)
	}
	source, err := k.MeetingSource(context.Background(), e, meeting)
	if err != nil {
		t.Fatal(err)
	}
	return meeting, source
}
func extractionApproval(k *Kernel, x MeetingExtraction, title string) map[string]any {
	return recoveryRequest(k, map[string]any{"meetingId": x.MeetingID, "evidenceHash": x.EvidenceHash, "proposalId": x.ProposalID, "itemId": x.ItemID, "kind": x.Kind, "title": title, "body": "Approved body"})
}
func TestMeetingExtractionIdentityRetryRestartAndEqualTitles(t *testing.T) {
	k, e := testKernel(t)
	meeting, _ := extractionFixture(t, k, e)
	prepare := recoveryRequest(k, map[string]any{"meetingId": meeting, "kind": "task"})
	x, err := k.prepareMeetingExtraction(context.Background(), e, prepare)
	if err != nil {
		t.Fatal(err)
	}
	same, err := k.prepareMeetingExtraction(context.Background(), e, prepare)
	if err != nil || same.PageID != x.PageID || same.RecordID != x.RecordID {
		t.Fatalf("Prepare retry changed identities: %v", err)
	}
	approved := testOK(t, testDispatch(t, k, e, "meetings.createFromTranscript", extractionApproval(k, x, "Same title")))
	if approved["reused"] != false {
		t.Fatal("First application reported a prior effect")
	}
	// A lost response may be retried with a new transport request after restart.
	path := k.Path()
	k.Close()
	k, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer k.Close()
	e = k.OwnerContext()
	retry := testOK(t, testDispatch(t, k, e, "meetings.createFromTranscript", extractionApproval(k, x, "Same title")))
	if retry["pageId"] != x.PageID || retry["reused"] != true {
		t.Fatal("Retry duplicated the approved extraction")
	}
	testCode(t, testDispatch(t, k, e, "meetings.createFromTranscript", extractionApproval(k, x, "Different edited title")), "IDEMPOTENCY_CONFLICT")
	distinct, err := k.prepareMeetingExtraction(context.Background(), e, recoveryRequest(k, map[string]any{"meetingId": meeting, "kind": "task"}))
	if err != nil {
		t.Fatal(err)
	}
	testOK(t, testDispatch(t, k, e, "meetings.createFromTranscript", extractionApproval(k, distinct, "Same title")))
	var count int
	k.db.QueryRow("SELECT count(*) FROM task_facets").Scan(&count)
	if count != 2 || x.RecordID == distinct.RecordID {
		t.Fatal("Equal titles collapsed independent extraction identities")
	}
}
func TestMeetingExtractionRejectsStaleReviewAndConcurrentRetry(t *testing.T) {
	k, e := testKernel(t)
	meeting, source := extractionFixture(t, k, e)
	ensureTestTasks(t, k, e)
	x, err := k.prepareMeetingExtraction(context.Background(), e, recoveryRequest(k, map[string]any{"meetingId": meeting, "kind": "task"}))
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := k.createMeetingExtraction(context.Background(), e, extractionApproval(k, x, "Concurrent approval"))
			results <- err
		}()
	}
	wg.Wait()
	close(results)
	for err := range results {
		if err != nil {
			t.Fatal(err)
		}
	}
	var count int
	k.db.QueryRow("SELECT count(*) FROM task_facets").Scan(&count)
	if count != 1 {
		t.Fatal("Concurrent retries duplicated a task")
	}
	pending, err := k.prepareMeetingExtraction(context.Background(), e, recoveryRequest(k, map[string]any{"meetingId": meeting, "kind": "task"}))
	if err != nil {
		t.Fatal(err)
	}
	testOK(t, dbBatch(t, k, e, []any{map[string]any{"kind": "block.update", "blockId": source.Segments[0].BlockID, "type": "paragraph", "schemaVersion": 1, "data": map[string]any{"richText": []any{map[string]any{"text": "Changed after review"}}}}}, "page", source.PageID, "block", source.Segments[0].BlockID))
	testCode(t, testDispatch(t, k, e, "meetings.createFromTranscript", extractionApproval(k, pending, "Stale approval")), "STALE_CITATION")
	// Applied receipts remain evidence of an earlier effect; a new source revision
	// must not cause an already approved item to be created again.
	testOK(t, testDispatch(t, k, e, "meetings.createFromTranscript", extractionApproval(k, x, "Concurrent approval")))
	k.db.QueryRow("SELECT count(*) FROM task_facets").Scan(&count)
	if count != 1 {
		t.Fatal("Stale review partially wrote canonical content")
	}
}

func TestMeetingGeneratedActionItemIdentityDoesNotDependOnTitle(t *testing.T) {
	k, e := testKernel(t)
	meeting, source := extractionFixture(t, k, e)
	ctx := context.Background()
	claim := MeetingClaim{Text: "Same extracted task", SegmentIDs: []string{source.Segments[0].SegmentID}}
	generated := MeetingIntelligence{Summary: []MeetingClaim{claim}, ActionItems: []MeetingClaim{claim, claim}}
	job := enqueueTestJob(t, k, e, "SUMMARIZE_MEETING", map[string]any{"kind": "summarize", "meetingId": meeting, "sourceRevision": source.PageRevision, "modelPackId": NewID()})
	job, err := k.ClaimJob(ctx, []string{"SUMMARIZE_MEETING"})
	if err != nil {
		t.Fatal(err)
	}
	execution, err := k.JobExecution(ctx, string(job.JobId))
	if err != nil {
		t.Fatal(err)
	}
	if err = k.SaveMeetingDraft(ctx, execution, string(job.JobId), string(*job.LeaseToken), source, generated); err != nil {
		t.Fatal(err)
	}
	drafts, err := k.MeetingDrafts(ctx, e, meeting)
	if err != nil || len(drafts) != 1 {
		t.Fatalf("Missing saved draft: %v", err)
	}
	request := func(index int) map[string]any {
		return recoveryRequest(k, map[string]any{"meetingId": meeting, "kind": "task", "draftId": drafts[0].ID, "itemIndex": float64(index)})
	}
	first, err := k.prepareMeetingExtraction(ctx, e, request(0))
	if err != nil {
		t.Fatal(err)
	}
	second, err := k.prepareMeetingExtraction(ctx, e, request(1))
	if err != nil {
		t.Fatal(err)
	}
	if first.ProposalID != second.ProposalID || first.ItemID == second.ItemID || first.RecordID == second.RecordID {
		t.Fatal("Action item identity collapsed equal titles")
	}
	testOK(t, testDispatch(t, k, e, "meetings.createFromTranscript", extractionApproval(k, first, "Reviewed title")))
	again, err := k.prepareMeetingExtraction(ctx, e, request(0))
	if err != nil {
		t.Fatal(err)
	}
	if again.PageID != first.PageID || again.AppliedAt == "" || again.Title != "Reviewed title" {
		t.Fatal("Revisiting a generated item lost its committed effect")
	}
	testOK(t, testDispatch(t, k, e, "meetings.createFromTranscript", extractionApproval(k, second, "Reviewed title")))
	var count int
	k.db.QueryRow("SELECT count(*) FROM task_facets").Scan(&count)
	if count != 2 {
		t.Fatal("Equal reviewed titles were deduplicated")
	}
}
func TestMeetingExtractionReceiptRechecksEvidenceInsideAtomicBatch(t *testing.T) {
	k, e := testKernel(t)
	meeting, source := extractionFixture(t, k, e)
	x, err := k.prepareMeetingExtraction(context.Background(), e, recoveryRequest(k, map[string]any{"meetingId": meeting, "kind": "page"}))
	if err != nil {
		t.Fatal(err)
	}
	testOK(t, dbBatch(t, k, e, []any{map[string]any{"kind": "block.update", "blockId": source.Segments[0].BlockID, "type": "paragraph", "schemaVersion": 1, "data": map[string]any{"richText": []any{map[string]any{"text": "Corrected transcript"}}}}}, "page", source.PageID, "block", source.Segments[0].BlockID))
	// Bypass the friendly query handler as another user-facing client could. The
	// single-writer command still checks evidence and rolls every effect back.
	commands := []any{map[string]any{"kind": "page.create", "pageId": x.PageID, "parentPageId": source.PageID, "title": "Stale content"}, map[string]any{"kind": "block.insert", "block": map[string]any{"blockId": x.BlockID, "pageId": x.PageID, "type": "paragraph", "schemaVersion": 1, "data": map[string]any{"richText": []any{map[string]any{"text": "Body"}, map[string]any{"text": "\nSource meeting", "link": "trove://page/" + source.PageID}}}}}, map[string]any{"kind": "meeting.extraction.record", "proposalId": x.ProposalID, "itemId": x.ItemID, "title": "Stale content", "body": "Body"}}
	testCode(t, dbBatch(t, k, e, commands, "page", source.PageID, "page", x.PageID, "block", x.BlockID), "STALE_CITATION")
	var count int
	k.db.QueryRow("SELECT count(*) FROM pages WHERE id=?", x.PageID).Scan(&count)
	if count != 0 {
		t.Fatal("Rejected extraction left a partial canonical page")
	}
	var applied string
	k.db.QueryRow("SELECT applied_at FROM meeting_extraction_items WHERE proposal_id=? AND item_id=?", x.ProposalID, x.ItemID).Scan(&applied)
	if applied != "" {
		t.Fatal("Rejected extraction recorded an effect")
	}
}
