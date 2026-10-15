package workspace

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"trove.local/phase0/contracts"
)

func finishMeetingFixtureJobs(t *testing.T, k *Kernel) {
	t.Helper()
	rows, err := k.db.Query("SELECT job_id,lease_token FROM jobs WHERE state='running'")
	if err != nil {
		t.Fatal(err)
	}
	pairs := [][2]string{}
	for rows.Next() {
		var p [2]string
		rows.Scan(&p[0], &p[1])
		pairs = append(pairs, p)
	}
	rows.Close()
	for _, p := range pairs {
		if err = k.FinishJob(context.Background(), p[0], p[1], nil, false); err != nil {
			t.Fatal(err)
		}
	}
}
func retentionPreviewTest(t *testing.T, k *Kernel, e contracts.ServiceExecutionContext, meeting string, selected ...string) MeetingRetentionPreview {
	t.Helper()
	choices := []any{}
	for _, choice := range selected {
		choices = append(choices, choice)
	}
	raw := testOK(t, testDispatch(t, k, e, "meetings.retentionPreview", recoveryRequest(k, map[string]any{"meetingId": meeting, "selected": choices})))
	var p MeetingRetentionPreview
	if err := json.Unmarshal(marshal(raw), &p); err != nil {
		t.Fatal(err)
	}
	if err := contracts.Validate("service", "MeetingRetentionPreview", marshal(p)); err != nil {
		t.Fatal(err)
	}
	return p
}
func TestMeetingCorrectionPreservesIdentityHistoryAndStalesDerivedEvidence(t *testing.T) {
	k, e := testKernel(t)
	ctx := context.Background()
	meeting, source := extractionFixture(t, k, e)
	segment := source.Segments[0]
	extraction, err := k.prepareMeetingExtraction(ctx, e, recoveryRequest(k, map[string]any{"meetingId": meeting, "kind": "task"}))
	if err != nil {
		t.Fatal(err)
	}
	testOK(t, testDispatch(t, k, e, "meetings.createFromTranscript", extractionApproval(k, extraction, "Approved task")))
	var collection string
	k.db.QueryRow("SELECT collection_id FROM records WHERE id=?", extraction.RecordID).Scan(&collection)
	testOK(t, dbBatch(t, k, e, []any{map[string]any{"kind": "page.update", "pageId": extraction.PageID, "title": "User edited task"}}, "page", extraction.PageID, "record", extraction.RecordID, "collection", collection))
	memoryPage := createPage(t, k, e, "Meeting memory")
	memory := NewID()
	testOK(t, dbBatch(t, k, e, []any{map[string]any{"kind": "memory.propose", "memoryId": memory, "pageId": memoryPage, "scope": map[string]any{"kind": "workspace", "workspaceId": k.ID()}, "memoryKind": "decision", "content": "Original decision", "sources": []any{map[string]any{"pageId": source.PageID, "sourceRevision": source.PageRevision, "blockId": segment.BlockID, "transcript": map[string]any{"meetingId": meeting, "segmentId": segment.SegmentID, "startMs": segment.StartMS, "endMs": segment.EndMS}}}}}, "page", memoryPage, "memory", memory))
	testOK(t, dbBatch(t, k, e, []any{map[string]any{"kind": "memory.approve", "memoryId": memory}}, "page", memoryPage, "memory", memory))
	correction := map[string]any{"kind": "meeting.correctSegment", "meetingId": meeting, "segmentId": segment.SegmentID, "text": "Corrected decision", "startMs": 10, "endMs": 450}
	guards := []contracts.ServiceExpectedRevision{}
	for _, entity := range [][2]string{{"page", source.PageID}, {"meeting", meeting}, {"block", segment.BlockID}, {"transcript_segment", segment.SegmentID}} {
		g, err := k.currentGuard(ctx, entity[0], entity[1])
		if err != nil {
			t.Fatal(err)
		}
		guards = append(guards, g)
	}
	request := testRequest(k, e, []any{correction}, guards...)
	testOK(t, testDispatch(t, k, e, "batch", request))
	// Retry receipt is inert; a new stale request cannot overwrite this correction.
	testOK(t, testDispatch(t, k, e, "batch", request))
	testCode(t, testDispatch(t, k, e, "batch", testRequest(k, e, []any{correction}, guards...)), "CONFLICT")
	current, err := k.MeetingSource(ctx, e, meeting)
	if err != nil {
		t.Fatal(err)
	}
	if current.EvidenceHash == source.EvidenceHash || current.Segments[0].SegmentID != segment.SegmentID || current.Segments[0].BlockID != segment.BlockID || current.Segments[0].StartMS != 10 || current.Segments[0].Text != "Corrected decision" {
		t.Fatal(current)
	}
	if err = memoryEvidenceCurrent(ctx, k.db, memory); err == nil {
		t.Fatal("approved memory retained stale evidence as current")
	}
	var old string
	if err = k.db.QueryRow("SELECT snapshot_json FROM page_snapshots WHERE page_id=? AND revision=?", source.PageID, source.PageRevision).Scan(&old); err != nil || !strings.Contains(old, segment.Text) {
		t.Fatal("original evidence lost", err)
	}
	var title string
	k.db.QueryRow("SELECT title FROM pages WHERE id=?", extraction.PageID).Scan(&title)
	if title != "User edited task" {
		t.Fatal("correction rewound approved task")
	}
	listed := testDispatch(t, k, e, "meetings.extractions", recoveryRequest(k, map[string]any{"meetingId": meeting}))
	if listed["ok"] != true {
		t.Fatal(listed)
	}
	items := listed["result"].([]any)
	if len(items) != 1 || items[0].(map[string]any)["stale"] != true {
		t.Fatal("approved extraction not marked stale", items)
	}
}

func TestMeetingRetentionSeparatesCurrentSourcesHistoryAndApprovedTasks(t *testing.T) {
	k, e := testKernel(t)
	ctx := context.Background()
	meeting, source := extractionFixture(t, k, e)
	x, err := k.prepareMeetingExtraction(ctx, e, recoveryRequest(k, map[string]any{"meetingId": meeting, "kind": "task"}))
	if err != nil {
		t.Fatal(err)
	}
	testOK(t, testDispatch(t, k, e, "meetings.createFromTranscript", extractionApproval(k, x, "Keep this task")))
	finishMeetingFixtureJobs(t, k)
	initial := retentionPreviewTest(t, k, e, meeting)
	if len(initial.Categories) != 7 || initial.CanApply {
		t.Fatal(initial)
	}
	p := retentionPreviewTest(t, k, e, meeting, "audio", "transcripts", "indexes")
	if !p.CanApply {
		t.Fatal(p)
	}
	request := testRequest(k, e, []any{p.Command}, p.ExpectedRevisions...)
	testOK(t, testDispatch(t, k, e, "batch", request))
	testOK(t, testDispatch(t, k, e, "batch", request))
	var segments, chunks, tasks, history, attachments int
	var recording *string
	k.db.QueryRow("SELECT count(*) FROM transcript_segments WHERE meeting_id=?", meeting).Scan(&segments)
	k.db.QueryRow("SELECT recording_file_id FROM meetings WHERE id=?", meeting).Scan(&recording)
	k.db.QueryRow("SELECT count(*) FROM search_chunks WHERE page_id=?", source.PageID).Scan(&chunks)
	k.db.QueryRow("SELECT count(*) FROM task_facets WHERE record_id=?", x.RecordID).Scan(&tasks)
	k.db.QueryRow("SELECT count(*) FROM page_snapshots WHERE page_id=?", source.PageID).Scan(&history)
	k.db.QueryRow("SELECT count(*) FROM attachments WHERE page_id=?", source.PageID).Scan(&attachments)
	if segments != 0 || recording != nil || chunks != 0 || tasks != 1 || history < 1 || attachments != 0 {
		t.Fatalf("retention boundaries: segments=%d audio=%v chunks=%d tasks=%d history=%d attachments=%d", segments, recording, chunks, tasks, history, attachments)
	}
	testOK(t, restoreTest(t, k, e, source.PageID, source.PageRevision))
	restored, restoreErr := k.MeetingSource(ctx, e, meeting)
	if restoreErr != nil || len(restored.Segments) != len(source.Segments) || restored.Segments[0].SegmentID != source.Segments[0].SegmentID {
		t.Fatal("retained transcript history could not restore identity", restored, restoreErr)
	}
	// Search exclusion survives subsequent edits and restart until explicitly reversed.
	testOK(t, dbBatch(t, k, e, []any{map[string]any{"kind": "page.update", "pageId": source.PageID, "title": "Still excluded"}}, "page", source.PageID))
	k.db.QueryRow("SELECT count(*) FROM search_chunks WHERE page_id=?", source.PageID).Scan(&chunks)
	if chunks != 0 {
		t.Fatal("edit recreated excluded search content")
	}
	path := k.Path()
	k.Close()
	k, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer k.Close()
	e = k.OwnerContext()
	p = retentionPreviewTest(t, k, e, meeting, "indexes")
	testOK(t, testDispatch(t, k, e, "batch", testRequest(k, e, []any{p.Command}, p.ExpectedRevisions...)))
	if err = k.FlushSearch(ctx); err != nil {
		t.Fatal(err)
	}
	k.db.QueryRow("SELECT count(*) FROM search_chunks WHERE page_id=?", source.PageID).Scan(&chunks)
	if chunks == 0 {
		t.Fatal("reviewed reindex did not restore search")
	}
}

func TestMeetingRetentionRejectsStalePreviewJobsNestedContentAndUnapprovedActor(t *testing.T) {
	k, e := testKernel(t)
	meeting, source := extractionFixture(t, k, e)
	queued := enqueueTestJob(t, k, e, "SUMMARIZE_MEETING", map[string]any{"kind": "summarize", "meetingId": meeting, "sourceRevision": source.PageRevision, "modelPackId": NewID()})
	p := retentionPreviewTest(t, k, e, meeting, "transcripts")
	if p.CanApply {
		t.Fatal("active processing not disclosed")
	}
	if _, err := k.cancelJob(context.Background(), e, string(queued.JobId), string(queued.Revision)); err != nil {
		t.Fatal(err)
	}
	finishMeetingFixtureJobs(t, k)
	p = retentionPreviewTest(t, k, e, meeting, "transcripts")
	testOK(t, dbBatch(t, k, e, []any{map[string]any{"kind": "page.update", "pageId": source.PageID, "title": "Changed while reviewing"}}, "page", source.PageID))
	testCode(t, testDispatch(t, k, e, "batch", testRequest(k, e, []any{p.Command}, p.ExpectedRevisions...)), "CONFLICT")
	nested := NewID()
	testOK(t, dbBatch(t, k, e, []any{map[string]any{"kind": "block.insert", "block": map[string]any{"blockId": nested, "pageId": source.PageID, "parentBlockId": source.Segments[0].BlockID, "type": "paragraph", "schemaVersion": 1, "data": map[string]any{"richText": []any{map[string]any{"text": "Unrelated nested note"}}}}}}, "page", source.PageID, "block", nested, "block", source.Segments[0].BlockID))
	p = retentionPreviewTest(t, k, e, meeting, "transcripts")
	if p.CanApply {
		t.Fatal("nested user content deletion permitted")
	}
	testCode(t, testDispatch(t, k, e, "batch", testRequest(k, e, []any{p.Command}, p.ExpectedRevisions...)), "CONFLICT")
	external := k.Grant(contracts.ServiceActor{Kind: "local_ai", ActorId: contracts.DomainId(NewID())}, "ai", e.Scope)
	if _, err := previewMeetingRetention(context.Background(), k, external, map[string]any{"meetingId": meeting, "selected": []any{}}); err == nil {
		t.Fatal("non-owner retention allowed")
	}
}

func TestMeetingRetentionArchivesProposalsAndForgetsMemoryWithoutErasingEvidence(t *testing.T) {
	k, e := testKernel(t)
	ctx := context.Background()
	meeting, source := extractionFixture(t, k, e)
	job := enqueueTestJob(t, k, e, "SUMMARIZE_MEETING", map[string]any{"kind": "summarize", "meetingId": meeting, "sourceRevision": source.PageRevision, "modelPackId": NewID()})
	job, err := k.ClaimJob(ctx, []string{"SUMMARIZE_MEETING"})
	if err != nil {
		t.Fatal(err)
	}
	execution, err := k.JobExecution(ctx, string(job.JobId))
	if err != nil {
		t.Fatal(err)
	}
	claim := MeetingClaim{Text: "Retained generated evidence", SegmentIDs: []string{source.Segments[0].SegmentID}}
	body := MeetingIntelligence{Summary: []MeetingClaim{claim}, ActionItems: []MeetingClaim{claim, claim}}
	if err = k.SaveMeetingDraft(ctx, execution, string(job.JobId), string(*job.LeaseToken), source, body); err != nil {
		t.Fatal(err)
	}
	finishMeetingFixtureJobs(t, k)
	drafts, err := k.MeetingDrafts(ctx, e, meeting)
	if err != nil || len(drafts) != 1 {
		t.Fatal(drafts, err)
	}
	prepare := func(index int) map[string]any {
		return recoveryRequest(k, map[string]any{"meetingId": meeting, "kind": "task", "draftId": drafts[0].ID, "itemIndex": float64(index)})
	}
	applied, err := k.prepareMeetingExtraction(ctx, e, prepare(0))
	if err != nil {
		t.Fatal(err)
	}
	pending, err := k.prepareMeetingExtraction(ctx, e, prepare(1))
	if err != nil {
		t.Fatal(err)
	}
	testOK(t, testDispatch(t, k, e, "meetings.createFromTranscript", extractionApproval(k, applied, "Keep approved task")))
	memoryPage := createPage(t, k, e, "Meeting memory")
	memory := NewID()
	testOK(t, dbBatch(t, k, e, []any{map[string]any{"kind": "memory.propose", "memoryId": memory, "pageId": memoryPage, "scope": map[string]any{"kind": "workspace", "workspaceId": k.ID()}, "memoryKind": "decision", "content": "Keep source evidence", "sources": []any{map[string]any{"pageId": source.PageID, "sourceRevision": source.PageRevision}}}}, "page", memoryPage, "memory", memory))
	p := retentionPreviewTest(t, k, e, meeting, "generated", "memory")
	if !p.CanApply {
		t.Fatal(p)
	}
	testOK(t, testDispatch(t, k, e, "batch", testRequest(k, e, []any{p.Command}, p.ExpectedRevisions...)))
	visibleDrafts, err := k.MeetingDrafts(ctx, e, meeting)
	if err != nil || len(visibleDrafts) != 0 {
		t.Fatal(visibleDrafts, err)
	}
	if _, err := k.ApplyMeetingDraft(ctx, e, drafts[0].ID); err == nil {
		t.Fatal("archived generated notes were approved")
	}
	guard, err := k.currentGuard(ctx, "page", source.PageID)
	if err != nil {
		t.Fatal(err)
	}
	if err = k.recordingBatch(ctx, e, "meeting-draft-"+drafts[0].ID, []any{map[string]any{"kind": "page.update", "pageId": source.PageID, "title": "Should roll back"}}, []contracts.ServiceExpectedRevision{guard}); err == nil {
		t.Fatal("archived approval bypassed writer fence")
	}
	if _, err := k.prepareMeetingExtraction(ctx, e, prepare(1)); err == nil {
		t.Fatal("archived proposal was re-prepared")
	}
	testCode(t, testDispatch(t, k, e, "meetings.createFromTranscript", extractionApproval(k, pending, "Do not apply")), "CONFLICT")
	testOK(t, testDispatch(t, k, e, "meetings.createFromTranscript", extractionApproval(k, applied, "Keep approved task")))
	response := testDispatch(t, k, e, "meetings.extractions", recoveryRequest(k, map[string]any{"meetingId": meeting}))
	if response["ok"] != true {
		t.Fatal(response)
	}
	listed := response["result"].([]any)
	if len(listed) != 1 || listed[0].(map[string]any)["itemId"] != applied.ItemID {
		t.Fatal("archive did not separate pending and applied extractions", listed)
	}
	var generated, status, content string
	if err = k.db.QueryRow("SELECT body_json FROM meeting_intelligence WHERE meeting_id=?", meeting).Scan(&generated); err != nil || !strings.Contains(generated, "Retained generated evidence") {
		t.Fatal("proposal evidence erased", err)
	}
	k.db.QueryRow("SELECT status,content FROM memory_entries WHERE id=?", memory).Scan(&status, &content)
	if status != "forgotten" || content != "Keep source evidence" {
		t.Fatal(status, content)
	}
	current, err := k.MeetingSource(ctx, e, meeting)
	if err != nil || current.EvidenceHash != source.EvidenceHash {
		t.Fatal("retention altered unrelated transcript", current, err)
	}
}

func TestMeetingRetentionGuardsRecordOwnedMeeting(t *testing.T) {
	k, e := testKernel(t)
	f := newDatabase(t, k, e)
	record, page := newRecord(t, k, e, f, "Meeting record")
	prepared := stageTestFile(t, k, e, "fixture audio")
	file := str(prepared["fileId"])
	meeting := NewID()
	testOK(t, dbBatch(t, k, e, []any{map[string]any{"kind": "file.attach", "fileId": file, "pageId": page, "ingestHandle": prepared["ingestHandle"]}}, "page", page, "record", record, "collection", f.collection, "file", file))
	testOK(t, dbBatch(t, k, e, []any{map[string]any{"kind": "meeting.create", "meetingId": meeting, "pageId": page, "recordingFileId": file, "startedAt": now()}}, "page", page, "record", record, "collection", f.collection, "file", file, "meeting", meeting))
	p := retentionPreviewTest(t, k, e, meeting, "audio", "indexes")
	testOK(t, testDispatch(t, k, e, "batch", testRequest(k, e, []any{p.Command}, p.ExpectedRevisions...)))
	var recording *string
	k.db.QueryRow("SELECT recording_file_id FROM meetings WHERE id=?", meeting).Scan(&recording)
	if recording != nil {
		t.Fatal("record-owned meeting retention failed")
	}
}
