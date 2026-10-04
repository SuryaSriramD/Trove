package workspace

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"math"
	"os"
	"strings"
	"testing"

	"trove.local/phase0/contracts"
)

func TestRecordingCrashRecoveryRetainsFlushedTailAndCanonicalWAV(t *testing.T) {
	k, e := testKernel(t)
	ctx := context.Background()
	draft, f, err := k.BeginRecording(ctx, e, "Durable voice note")
	if err != nil {
		t.Fatal(err)
	}
	samples := make([]byte, 64000)
	for i := 0; i < len(samples); i += 4 {
		binary.LittleEndian.PutUint32(samples[i:], math.Float32bits(.25))
	}
	if _, err = f.Write(samples); err != nil {
		t.Fatal(err)
	}
	if err = f.Sync(); err != nil {
		t.Fatal(err)
	}
	first, err := k.CheckpointRecording(ctx, e, draft.ID, false, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Segments) != 1 || first.DurationMS != 1000 {
		t.Fatalf("bad checkpoint %+v", first)
	}
	// Simulate a crash after the next durable write and before journal update.
	if _, err = f.Write(samples); err != nil {
		t.Fatal(err)
	}
	f.Sync()
	f.Close()
	path := k.Path()
	k.Close()
	k, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer k.Close()
	e = k.OwnerContext()
	pending, err := k.ListRecordings(ctx, e)
	if err != nil || len(pending) != 1 {
		t.Fatalf("pending recordings: %+v %v", pending, err)
	}
	if pending[0].State != "interrupted" || pending[0].Error == "" {
		t.Fatalf("orphan capture was not marked recoverable: %+v", pending[0])
	}
	saved, err := k.FinalizeRecording(ctx, e, draft.ID)
	if err != nil {
		t.Fatal(err)
	}
	if saved.State != "saved" || saved.DurationMS != 2000 || len(saved.Segments) != 2 {
		t.Fatalf("tail lost %+v", saved)
	}
	audio, meta, err := k.ReadFile(ctx, e, saved.FileID)
	if err != nil {
		t.Fatal(err)
	}
	defer audio.Close()
	header := make([]byte, 44)
	if _, err = audio.Read(header); err != nil {
		t.Fatal(err)
	}
	if string(header[:4]) != "RIFF" || binary.LittleEndian.Uint32(header[40:]) != 64000 || !strings.HasPrefix(meta.MediaType, "audio/") {
		t.Fatalf("invalid playable WAV %s", marshal(meta))
	}
	meeting, _, err := k.ReadMeetingForJob(ctx, e, saved.MeetingID)
	if err != nil || meeting.DurationMs != 2000 || meeting.State != "captured" {
		t.Fatalf("captured duration should be available before transcription: %+v %v", meeting, err)
	}
	again, err := k.FinalizeRecording(ctx, e, draft.ID)
	if err != nil || again.MeetingID != saved.MeetingID {
		t.Fatalf("finalization not idempotent %+v %v", again, err)
	}
	var n int
	k.db.QueryRow("SELECT count(*) FROM meetings").Scan(&n)
	if n != 1 {
		t.Fatal("recovery duplicated meeting")
	}
}

func TestRecordingDurationReadsOnlyBoundedValidWAVHeaders(t *testing.T) {
	var wav bytes.Buffer
	if err := writeRecordingWAV(context.Background(), &wav, bytes.NewReader(make([]byte, 64000)), 64000); err != nil {
		t.Fatal(err)
	}
	valid := wav.Bytes()
	if duration := wavDurationMS(bytes.NewReader(valid), int64(len(valid))); duration != 1000 {
		t.Fatalf("valid PCM duration = %d", duration)
	}
	for name, change := range map[string]func([]byte){
		"truncated_riff":         func(b []byte) { binary.LittleEndian.PutUint32(b[4:], uint32(len(b)+1000)) },
		"oversized_data":         func(b []byte) { binary.LittleEndian.PutUint32(b[40:], ^uint32(0)) },
		"zero_rate":              func(b []byte) { binary.LittleEndian.PutUint32(b[28:], 0) },
		"inconsistent_rate":      func(b []byte) { binary.LittleEndian.PutUint32(b[28:], 1) },
		"inconsistent_alignment": func(b []byte) { binary.LittleEndian.PutUint16(b[32:], 1) },
		"unsupported_codec":      func(b []byte) { binary.LittleEndian.PutUint16(b[20:], 17) },
		"missing_format":         func(b []byte) { copy(b[12:], "JUNK") },
		"missing_data":           func(b []byte) { copy(b[36:], "JUNK") },
	} {
		t.Run(name, func(t *testing.T) {
			input := bytes.Clone(valid)
			change(input)
			if duration := wavDurationMS(bytes.NewReader(input), int64(len(input))); duration != 0 {
				t.Fatalf("invalid WAV advertised duration %d", duration)
			}
		})
	}
}
func TestRecordingRejectsChangedAcknowledgedAudioAndKeepsOriginal(t *testing.T) {
	k, e := testKernel(t)
	ctx := context.Background()
	draft, f, err := k.BeginRecording(ctx, e, "Corruption")
	if err != nil {
		t.Fatal(err)
	}
	f.Write(make([]byte, 640))
	f.Sync()
	if _, err = k.CheckpointRecording(ctx, e, draft.ID, true, ""); err != nil {
		t.Fatal(err)
	}
	f.WriteAt([]byte{1, 0, 0, 0}, 0)
	f.Sync()
	f.Close()
	if _, err = k.FinalizeRecording(ctx, e, draft.ID); err == nil || !strings.Contains(err.Error(), "hash mismatch") {
		t.Fatalf("corruption accepted %v", err)
	}
	path, _ := k.recordingPath(draft.ID, ".f32le")
	if _, err = os.Stat(path); err != nil {
		t.Fatal("original corrupted audio deleted")
	}
	if _, _, err = k.BeginRecording(ctx, k.Grant(contracts.ServiceActor{Kind: "agent", ActorId: contracts.DomainId(NewID())}, "agent", e.Scope), "Denied"); err == nil {
		t.Fatal("agent opened microphone capture")
	}
}
func TestMeetingEvidenceReviewStalenessAndCanonicalConversion(t *testing.T) {
	k, e := testKernel(t)
	ctx := context.Background()
	page, meeting, file := meetingFixture(t, k, e)
	j := enqueueTestJob(t, k, e, "TRANSCRIBE_AUDIO", map[string]any{"kind": "transcribe", "meetingId": meeting, "fileId": file, "modelPackId": NewID()})
	j, err := k.ClaimJob(ctx, []string{"TRANSCRIBE_AUDIO"})
	if err != nil {
		t.Fatal(err)
	}
	je, err := k.JobExecution(ctx, string(j.JobId))
	if err != nil {
		t.Fatal(err)
	}
	result := k.CommitJobBatch(ctx, je, string(j.JobId), string(*j.LeaseToken), "transcript-v1", jobTranscriptRequest(t, k, je, j, meeting))
	if f, ok := result.Value.(contracts.ServiceBatchResponseVariant2); ok {
		t.Fatal(f.Error)
	}
	source, err := k.MeetingSource(ctx, e, meeting)
	if err != nil {
		t.Fatal(err)
	}
	claim := MeetingClaim{Text: "Review the launch checklist", SegmentIDs: []string{source.Segments[0].SegmentID}}
	body := MeetingIntelligence{Summary: []MeetingClaim{claim}, KeyPoints: []MeetingClaim{}, Decisions: []MeetingClaim{}, Questions: []MeetingClaim{}, ActionItems: []MeetingClaim{claim}}
	summary := enqueueTestJob(t, k, e, "SUMMARIZE_MEETING", map[string]any{"kind": "summarize", "meetingId": meeting, "sourceRevision": source.PageRevision, "modelPackId": NewID()})
	summary, err = k.ClaimJob(ctx, []string{"SUMMARIZE_MEETING"})
	if err != nil {
		t.Fatal(err)
	}
	se, err := k.JobExecution(ctx, string(summary.JobId))
	if err != nil {
		t.Fatal(err)
	}
	if err = k.SaveMeetingDraft(ctx, se, string(summary.JobId), string(*summary.LeaseToken), source, body); err != nil {
		t.Fatal(err)
	}
	drafts, err := k.MeetingDrafts(ctx, e, meeting)
	if err != nil || len(drafts) != 1 {
		t.Fatalf("draft missing %v", err)
	}
	before := readPage(t, k, e, page)
	if len(before["blocks"].([]any)) != 1 {
		t.Fatal("model mutated canonical page before approval")
	}
	if _, err = k.ApplyMeetingDraft(ctx, se, drafts[0].ID); err == nil {
		t.Fatal("job approved its own draft")
	}
	testOK(t, dbBatch(t, k, e, []any{map[string]any{"kind": "block.update", "blockId": source.Segments[0].BlockID, "type": "paragraph", "schemaVersion": 1, "data": map[string]any{"richText": []any{map[string]any{"text": "The current transcript changed"}}}}}, "page", page, "block", source.Segments[0].BlockID))
	var projected string
	k.db.QueryRow("SELECT text FROM transcript_segments WHERE id=?", source.Segments[0].SegmentID).Scan(&projected)
	if projected != "The current transcript changed" {
		t.Fatal("edited transcript projection stayed stale")
	}
	if _, err = k.ApplyMeetingDraft(ctx, e, drafts[0].ID); err == nil {
		t.Fatal("stale summary applied")
	}
	stale := testDispatch(t, k, e, "meetings.createFromTranscript", recoveryRequest(k, map[string]any{"meetingId": meeting, "evidenceHash": source.EvidenceHash, "kind": "task", "title": "Review checklist", "body": "Approved text"}))
	testCode(t, stale, "STALE_CITATION")
	source, err = k.MeetingSource(ctx, e, meeting)
	if err != nil {
		t.Fatal(err)
	}
	out := testOK(t, testDispatch(t, k, e, "meetings.createFromTranscript", recoveryRequest(k, map[string]any{"meetingId": meeting, "evidenceHash": source.EvidenceHash, "kind": "task", "title": "Review checklist", "body": "Approved text"})))
	var taskCount int
	k.db.QueryRow("SELECT count(*) FROM task_facets f JOIN records r ON r.id=f.record_id WHERE r.page_id=?", out["pageId"]).Scan(&taskCount)
	if taskCount != 1 {
		t.Fatal("review did not create canonical task")
	}
	raw, _ := json.Marshal(readPage(t, k, e, str(out["pageId"])))
	if !strings.Contains(string(raw), "Approved text") {
		t.Fatal("reviewed body lost")
	}
}
func TestMeetingSummaryRejectsUnknownEvidence(t *testing.T) {
	source := MeetingSource{Segments: []MeetingEvidence{{SegmentID: NewID()}}}
	body := MeetingIntelligence{Summary: []MeetingClaim{{Text: "An unsupported claim", SegmentIDs: []string{NewID()}}}}
	if err := ValidateMeetingIntelligence(body, source); err == nil {
		t.Fatal("fabricated citation accepted")
	}
}
