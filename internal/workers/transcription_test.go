package workers

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"trove.local/phase0/contracts"
	"trove.local/phase0/internal/native"
	"trove.local/phase0/internal/workspace"
)

type fakeSpeech struct {
	calls      int
	transcript string
	before     func(context.Context, int) error
}

func (s *fakeSpeech) MemoryBudget() int64 { return 1 << 30 }
func (s *fakeSpeech) Run(ctx context.Context, r native.Request, onEvent func(native.Event)) (json.RawMessage, error) {
	s.calls++
	if s.before != nil {
		if err := s.before(ctx, s.calls); err != nil {
			return nil, err
		}
	}
	var metadata native.InputMetadata
	raw := make([]byte, r.Inputs[0].Handle.SizeBytes)
	if _, err := r.Inputs[0].File.ReadAt(raw, 0); err != nil {
		return nil, err
	}
	if err := json.Unmarshal(raw, &metadata); err != nil {
		return nil, err
	}
	text := s.transcript
	if text == "" {
		text = "Launch checklist decision"
	}
	return json.Marshal(map[string]any{"kind": "transcript", "meetingId": metadata.MeetingID, "segments": []any{map[string]any{"segmentId": metadata.SegmentIDs[0].SegmentID, "blockId": metadata.SegmentIDs[0].BlockID, "startMs": 0, "endMs": r.Inputs[1].Handle.SizeBytes / 64, "text": text}}})
}
func recordingJobFixture(t *testing.T, seconds int) (*workspace.Kernel, contracts.ServiceExecutionContext, workspace.RecordingDraft, contracts.JobsJob) {
	return recordingJobFixturePCM(t, seconds, nil, "", true)
}
func recordingJobFixturePCM(t *testing.T, seconds int, pcm []byte, pack string, claim bool) (*workspace.Kernel, contracts.ServiceExecutionContext, workspace.RecordingDraft, contracts.JobsJob) {
	t.Helper()
	ctx := context.Background()
	k, err := workspace.Create(filepath.Join(t.TempDir(), "workspace"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { k.Close() })
	e := k.OwnerContext()
	path := filepath.Join(t.TempDir(), "recording.f32le")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(pcm) == 0 {
		if err = f.Truncate(int64(seconds) * 16000 * 4); err != nil {
			t.Fatal(err)
		}
	} else {
		remaining := int64(seconds) * 16000 * 4
		for remaining > 0 {
			count := min(int64(len(pcm)), remaining)
			if _, err = f.Write(pcm[:count]); err != nil {
				t.Fatal(err)
			}
			remaining -= count
		}
	}
	f.Close()
	handle, err := k.RegisterSelection(path)
	if err != nil {
		t.Fatal(err)
	}
	meeting, err := k.CreateMeetingFromSelection(ctx, e, handle, "Long recording")
	if err != nil {
		t.Fatal(err)
	}
	if pack == "" {
		pack = workspace.NewID()
	}
	queued, err := k.QueueMeetingTranscription(ctx, e, meeting.MeetingID, pack)
	if err != nil {
		t.Fatal(err)
	}
	var job contracts.JobsJob
	raw, _ := json.Marshal(queued)
	if err = json.Unmarshal(raw, &job); err != nil {
		t.Fatal(err)
	}
	if claim {
		job, err = k.ClaimJob(ctx, []string{"TRANSCRIBE_AUDIO"})
		if err != nil {
			t.Fatal(err)
		}
	}
	return k, e, meeting, job
}
func TestTranscriptionResumesCommittedMinutesAfterRestart(t *testing.T) {
	ctx := context.Background()
	k, e, meeting, j := recordingJobFixture(t, 125)
	first := &fakeSpeech{before: func(ctx context.Context, call int) error {
		if call == 2 {
			return context.Canceled
		}
		return nil
	}}
	if err := transcribeWithRunner(ctx, k, first, j); !errors.Is(err, context.Canceled) {
		t.Fatalf("expected interruption, got %v", err)
	}
	if err := k.FinishJob(ctx, string(j.JobId), string(*j.LeaseToken), context.Canceled, false); err != nil {
		t.Fatal(err)
	}
	path := k.Path()
	k.Close()
	var err error
	k, err = workspace.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer k.Close()
	e = k.OwnerContext()
	file, _, err := k.ReadFile(ctx, e, meeting.FileID)
	if err != nil {
		t.Fatal("source audio lost", err)
	}
	st, _ := file.Stat()
	file.Close()
	if st.Size() != 44+125*16000*2 {
		t.Fatal("source audio changed")
	}
	payload := j.Payload.Value.(contracts.JobsJobPayloadVariant5)
	if _, err = k.QueueMeetingTranscription(ctx, e, meeting.MeetingID, string(payload.ModelPackId)); err != nil {
		t.Fatal(err)
	}
	j, err = k.ClaimJob(ctx, []string{"TRANSCRIBE_AUDIO"})
	if err != nil {
		t.Fatal(err)
	}
	resumed := &fakeSpeech{}
	if err = transcribeWithRunner(ctx, k, resumed, j); err != nil {
		t.Fatal(err)
	}
	if resumed.calls != 2 {
		t.Fatalf("re-recognized committed minutes: %d calls", resumed.calls)
	}
	source, err := k.MeetingSource(ctx, e, meeting.MeetingID)
	if err != nil {
		t.Fatal(err)
	}
	if len(source.Segments) != 3 || source.Segments[1].StartMS != 60000 || source.Segments[2].StartMS != 120000 || source.Segments[2].EndMS != 125000 {
		t.Fatalf("bad absolute timestamps: %+v", source.Segments)
	}
	terminal, err := k.ReadJob(ctx, string(j.JobId))
	if err != nil || terminal.State != "succeeded" {
		t.Fatalf("not finalized %+v %v", terminal, err)
	}
}
func TestOneHourSourceDoesNotBlockEditingDuringRecognition(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	k, e, meeting, j := recordingJobFixture(t, 3600)
	admitted := make(chan struct{})
	runner := &fakeSpeech{before: func(ctx context.Context, call int) error { close(admitted); <-ctx.Done(); return ctx.Err() }}
	done := make(chan error, 1)
	go func() { done <- transcribeWithRunner(ctx, k, runner, j) }()
	select {
	case <-admitted:
	case <-time.After(15 * time.Second):
		t.Fatal("one-hour audio was not admitted")
	}
	m, rev, err := k.ReadMeetingForJob(context.Background(), e, meeting.MeetingID)
	if err != nil {
		t.Fatal(err)
	}
	block := workspace.NewID()
	request := map[string]any{"contractVersion": 1, "requestId": workspace.NewID(), "workspaceId": k.ID(), "mutation": map[string]any{"scopeGrantId": e.ScopeGrantId, "idempotencyKey": workspace.NewID(), "expectedRevisions": []any{map[string]any{"target": map[string]any{"entityType": "page", "entityId": m.PageId}, "revision": rev}, map[string]any{"target": map[string]any{"entityType": "block", "entityId": block}, "revision": "0"}}}, "commands": []any{map[string]any{"kind": "block.insert", "block": map[string]any{"blockId": block, "pageId": m.PageId, "type": "paragraph", "schemaVersion": 1, "data": map[string]any{"richText": []any{map[string]any{"text": "Editing while a one-hour source is transcribing"}}}}}}}
	raw, _ := json.Marshal(request)
	start := time.Now()
	response := k.Dispatch(context.Background(), e, "batch", raw)
	if !strings.Contains(string(response), `"ok":true`) {
		t.Fatal(string(response))
	}
	if time.Since(start) > 2*time.Second {
		t.Fatal("recognition held the editor writer")
	}
	cancel()
	if err = <-done; !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	// This test qualifies scheduling with a one-hour-sized source and a controlled
	// worker, not one-hour microphone hardware or Whisper accuracy/performance.
}
func TestRestrictedAudioFloatValidationAndWAVConversion(t *testing.T) {
	f, err := os.CreateTemp(t.TempDir(), "malformed-")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var bytes [4]byte
	binary.LittleEndian.PutUint32(bytes[:], math.Float32bits(float32(math.NaN())))
	f.Write(bytes[:])
	if _, _, err = pcmInput(f, contracts.DomainFile{OriginalName: "renamed.f32le"}); err == nil {
		t.Fatal("NaN accepted through raw suffix")
	}
	wav, err := os.CreateTemp(t.TempDir(), "wav-")
	if err != nil {
		t.Fatal(err)
	}
	defer wav.Close()
	header := make([]byte, 44)
	copy(header, "RIFF")
	binary.LittleEndian.PutUint32(header[4:], 38)
	copy(header[8:], "WAVEfmt ")
	binary.LittleEndian.PutUint32(header[16:], 16)
	binary.LittleEndian.PutUint16(header[20:], 1)
	binary.LittleEndian.PutUint16(header[22:], 1)
	binary.LittleEndian.PutUint32(header[24:], 16000)
	binary.LittleEndian.PutUint16(header[34:], 16)
	copy(header[36:], "data")
	binary.LittleEndian.PutUint32(header[40:], 2)
	wav.Write(header)
	wav.Write([]byte{0, 64})
	converted, cleanup, err := pcmInput(wav, contracts.DomainFile{OriginalName: "mono.wav"})
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	out := make([]byte, 4)
	if _, err = io.NewSectionReader(converted, 0, 4).Read(out); err != nil {
		t.Fatal(err)
	}
	if math.Float32frombits(binary.LittleEndian.Uint32(out)) != .5 {
		t.Fatal("wrong PCM conversion")
	}
}
func TestMeetingPromptChunksKeepAllUnicodeAndEvidence(t *testing.T) {
	text := strings.Repeat("世界 👋 ", 1000)
	source := workspace.MeetingSource{Segments: []workspace.MeetingEvidence{{SegmentID: workspace.NewID(), Text: text}}}
	chunks := meetingPromptChunks(source, 1000)
	if len(chunks) < 2 {
		t.Fatal("long source not chunked")
	}
	var recovered strings.Builder
	for _, chunk := range chunks {
		if len(chunk) > 1000 {
			t.Fatal("prompt budget exceeded")
		}
		for _, line := range strings.Split(strings.TrimSuffix(chunk, "\n"), "\n") {
			index := strings.Index(line, "] ")
			if index < 0 {
				t.Fatal("evidence marker lost")
			}
			recovered.WriteString(line[index+2:])
		}
	}
	if recovered.String() != text {
		t.Fatal("source was silently truncated")
	}
}

func TestRealLocalMeetingSummaryRequiresReview(t *testing.T) {
	if os.Getenv("TROVE_NATIVE_INTEGRATION") != "1" {
		t.Skip("requires bundled native helper and verified instruction pack")
	}
	k, e, meeting, j := recordingJobFixture(t, 10)
	if err := transcribeWithRunner(context.Background(), k, &fakeSpeech{transcript: "Maya will send the launch checklist by Friday. We decided to ship the beta on Monday. The open question is whether the vendor supports offline activation."}, j); err != nil {
		t.Fatal(err)
	}
	source, err := k.MeetingSource(context.Background(), e, meeting.MeetingID)
	if err != nil {
		t.Fatal(err)
	}
	root, _ := filepath.Abs("../..")
	bundle := os.Getenv("TROVE_NATIVE_BUNDLE")
	if bundle == "" {
		bundle = filepath.Join(root, "build/native")
	}
	models := os.Getenv("TROVE_MODEL_ROOT")
	if models == "" {
		models = "/private/tmp/trove-phase1-models"
	}
	manager, err := native.NewManager(models)
	if err != nil {
		t.Fatal(err)
	}
	supervisor, err := native.NewSupervisor(bundle, manager)
	if err != nil {
		t.Fatal(err)
	}
	modelPack := os.Getenv("TROVE_MEETING_MODEL_PACK")
	if modelPack == "" {
		modelPack = "01990000-0000-7000-8000-000000000105"
	}
	req := contracts.JobsEnqueueJobRequest{ContractVersion: 1, RequestId: contracts.DomainId(workspace.NewID()), WorkspaceId: contracts.DomainId(k.ID()), Kind: "SUMMARIZE_MEETING", IdempotencyKey: workspace.NewID(), Payload: contracts.JobsJobPayload{Value: contracts.JobsJobPayloadVariant6{Kind: "summarize", MeetingId: contracts.DomainId(meeting.MeetingID), SourceRevision: contracts.DomainRevision(source.PageRevision), ModelPackId: contracts.DomainId(modelPack)}}}
	response := k.EnqueueJob(context.Background(), e, req)
	queued, ok := response.Value.(contracts.JobsJobResponseVariant1)
	if !ok {
		t.Fatalf("enqueue failed %+v", response)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		done <- k.RunJobs(ctx, map[string]func(context.Context, contracts.JobsJob) error{"SUMMARIZE_MEETING": func(ctx context.Context, job contracts.JobsJob) error {
			return summarizeMeetingWithGenerator(ctx, k, tracedMeetingGenerator{supervisor, t}, job)
		}})
	}()
	defer func() { cancel(); <-done }()
	for {
		job, err := k.ReadJob(ctx, string(queued.Result.JobId))
		if err != nil {
			t.Fatal(err)
		}
		if job.State == "succeeded" {
			break
		}
		if job.State == "failed" {
			t.Fatalf("summary failed %+v", job.Error)
		}
		select {
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		case <-time.After(100 * time.Millisecond):
		}
	}
	drafts, err := k.MeetingDrafts(ctx, e, meeting.MeetingID)
	if err != nil || len(drafts) != 1 {
		t.Fatalf("missing review draft %+v %v", drafts, err)
	}
	if len(drafts[0].Body.Summary) == 0 {
		t.Fatal("summary missing")
	}
	if len(drafts[0].Body.Decisions) == 0 || len(drafts[0].Body.ActionItems) == 0 {
		t.Fatalf("useful decisions/action items missing: %+v", drafts[0].Body)
	}
	decisions, _ := json.Marshal(drafts[0].Body.Decisions)
	actions, _ := json.Marshal(drafts[0].Body.ActionItems)
	questions, _ := json.Marshal(drafts[0].Body.Questions)
	if !strings.Contains(strings.ToLower(string(decisions)), "beta") || !strings.Contains(strings.ToLower(string(actions)), "maya") || !strings.Contains(strings.ToLower(string(questions)), "offline") {
		t.Fatalf("meeting sections lost their concrete content: %+v", drafts[0].Body)
	}
	rawBody, _ := json.Marshal(drafts[0].Body)
	if !strings.Contains(strings.ToLower(string(rawBody)), "checklist") || !strings.Contains(strings.ToLower(string(rawBody)), "beta") {
		t.Fatalf("summary omitted key fixture content: %s", rawBody)
	}
	t.Logf("local draft: %+v", drafts[0].Body)
	if _, err = k.ApplyMeetingDraft(ctx, e, drafts[0].ID); err != nil {
		t.Fatal(err)
	}
	drafts, err = k.MeetingDrafts(ctx, e, meeting.MeetingID)
	if err != nil || drafts[0].State != "applied" {
		t.Fatalf("approval not durable %v", err)
	}
	evidence := map[string]any{"verifiedAt": time.Now().UTC().Format(time.RFC3339Nano), "modelPackId": modelPack, "target": "darwin/arm64", "passed": true, "input": "Deterministic canonical meeting transcript with a commitment, decision, and open question", "checks": []string{"real local instruction inference", "strict structured output and duplicate key rejection", "canonical segment citation mapping", "summary contains fixture launch checklist and beta", "beta decision, named checklist action, and offline activation question preserved", "no canonical write until user approval", "approval appends notes and persists applied state"}, "draft": drafts[0].Body}
	encoded, _ := json.MarshalIndent(evidence, "", "  ")
	if err = os.MkdirAll(filepath.Join(root, "evidence/phase2"), 0755); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(root, "evidence/phase2/meeting-native.json"), encoded, 0644); err != nil {
		t.Fatal(err)
	}
}

type tracedMeetingGenerator struct {
	s *native.Supervisor
	t *testing.T
}

func (g tracedMeetingGenerator) GenerationContextTokens(id string) (int, error) {
	return g.s.GenerationContextTokens(id)
}
func (g tracedMeetingGenerator) GenerateTextWithGrammar(ctx context.Context, id, prompt string, max int, grammar string, onToken func(string)) (string, error) {
	text, err := g.s.GenerateTextWithGrammar(ctx, id, prompt, max, grammar, onToken)
	g.t.Logf("local model response: %s", text)
	return text, err
}

func TestMeetingGenerationRejectsSilentFieldLossAndFabricatedAliases(t *testing.T) {
	for _, raw := range []string{
		`{"items":[{"text":"Action","segmentIds":["S1"]}],"items":[]}`,
		`{"items":[{"text":"Action","text":"Changed","segmentIds":["S1"]}]}`,
		`{"items":["Uncited plain text"]}`,
		`{"items":[]} {"items":[]}`,
	} {
		if _, err := parseMeetingSection(raw); err == nil {
			t.Fatalf("malformed model output accepted: %s", raw)
		}
	}
	sourceID := workspace.NewID()
	body := workspace.MeetingIntelligence{Summary: []workspace.MeetingClaim{{Text: "Grounded claim", SegmentIDs: []string{"S1"}}}}
	if err := resolveMeetingAliases(&body, map[string]string{"S1": sourceID}); err != nil {
		t.Fatal(err)
	}
	if body.Summary[0].SegmentIDs[0] != sourceID {
		t.Fatal("citation was not mapped to the canonical source")
	}
	body.Summary[0].SegmentIDs = []string{"S99"}
	if err := resolveMeetingAliases(&body, map[string]string{"S1": sourceID}); err == nil {
		t.Fatal("invented source alias accepted")
	}
}

func TestRealOneHourTranscriptionKeepsEditorResponsive(t *testing.T) {
	if os.Getenv("TROVE_LONG_AUDIO_INTEGRATION") != "1" {
		t.Skip("requires verified native speech pack and a full one-hour transcription run")
	}
	root, _ := filepath.Abs("../..")
	bundle := os.Getenv("TROVE_NATIVE_BUNDLE")
	if bundle == "" {
		bundle = filepath.Join(root, "build/native")
	}
	models := os.Getenv("TROVE_MODEL_ROOT")
	if models == "" {
		models = "/private/tmp/trove-phase1-models"
	}
	manager, err := native.NewManager(models)
	if err != nil {
		t.Fatal(err)
	}
	supervisor, err := native.NewSupervisor(bundle, manager)
	if err != nil {
		t.Fatal(err)
	}
	sample, err := os.Open(filepath.Join(bundle, "fixtures", "jfk.wav"))
	if err != nil {
		t.Fatal(err)
	}
	defer sample.Close()
	pcm, cleanup, err := pcmInput(sample, contracts.DomainFile{OriginalName: "jfk.wav", MediaType: "audio/wav"})
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	stat, _ := pcm.Stat()
	speech, err := io.ReadAll(io.NewSectionReader(pcm, 0, stat.Size()))
	if err != nil {
		t.Fatal(err)
	}
	k, e, meeting, j := recordingJobFixturePCM(t, 3600, speech, "01990000-0000-7000-8000-000000000103", false)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- Run(ctx, k, e, supervisor) }()
	defer func() { cancel(); <-done }()
	started := time.Now()
	nextEdit := started
	edits := 0
	maxLatency := time.Duration(0)
	lastCompleted := int64(-1)
	for {
		job, err := k.ReadJob(ctx, string(j.JobId))
		if err != nil {
			t.Fatal(err)
		}
		if job.State == "succeeded" {
			break
		}
		if job.State == "failed" || job.State == "cancelled" {
			t.Fatalf("native one-hour job stopped: %+v", job.Error)
		}
		if job.Progress != nil && job.Progress.Completed != lastCompleted {
			lastCompleted = job.Progress.Completed
			totalBytes := int64(0)
			if job.Progress.Total != nil {
				totalBytes = *job.Progress.Total
			}
			t.Logf("durable audio progress %d / %d bytes", lastCompleted, totalBytes)
		}
		if job.State == "running" && time.Now().After(nextEdit) {
			_, revision, err := k.ReadMeetingForJob(ctx, e, meeting.MeetingID)
			if err != nil {
				t.Fatal(err)
			}
			title := fmt.Sprintf("Edited during native transcription %d", edits+1)
			request := contracts.ServiceBatchRequest{ContractVersion: 1, RequestId: contracts.DomainId(workspace.NewID()), WorkspaceId: contracts.DomainId(k.ID()), Mutation: contracts.ServiceMutationMeta{ScopeGrantId: e.ScopeGrantId, IdempotencyKey: workspace.NewID(), ExpectedRevisions: []contracts.ServiceExpectedRevision{{Target: contracts.ServiceEntityRef{EntityType: "page", EntityId: contracts.DomainId(meeting.PageID)}, Revision: string(revision)}}}, Commands: []contracts.ServiceCommand{{Value: contracts.ServiceUpdatePage{Kind: "page.update", PageId: contracts.DomainId(meeting.PageID), Title: &title}}}}
			editStart := time.Now()
			response := k.Batch(ctx, e, request)
			latency := time.Since(editStart)
			if fail, ok := response.Value.(contracts.ServiceBatchResponseVariant2); ok {
				if fail.Error.Code != "CONFLICT" {
					t.Fatal(fail.Error)
				}
			} else {
				edits++
				maxLatency = max(maxLatency, latency)
				if latency > 2*time.Second {
					t.Fatalf("editing blocked for %s during native recognition", latency)
				}
			}
			nextEdit = time.Now().Add(2 * time.Second)
		}
		select {
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		case <-time.After(50 * time.Millisecond):
		}
	}
	source, err := k.MeetingSource(ctx, e, meeting.MeetingID)
	if err != nil {
		t.Fatal(err)
	}
	if edits < 2 || len(source.Segments) < 60 {
		t.Fatalf("insufficient live evidence: edits=%d segments=%d", edits, len(source.Segments))
	}
	var text strings.Builder
	for _, segment := range source.Segments {
		text.WriteString(strings.ToLower(segment.Text))
	}
	if !strings.Contains(text.String(), "country") {
		t.Fatal("native speech transcript missing source content")
	}
	final, _, err := k.ReadMeetingForJob(ctx, e, meeting.MeetingID)
	if err != nil || final.DurationMs != 3600000 || final.State != "ready" {
		t.Fatalf("full hour not finalized: %+v %v", final, err)
	}
	evidence := map[string]any{"verifiedAt": time.Now().UTC().Format(time.RFC3339Nano), "passed": true, "target": "darwin/arm64", "speechPackId": "01990000-0000-7000-8000-000000000103", "source": "public JFK speech sample repeated to exactly one hour; synthetic repetition, not microphone capture or real-meeting accuracy qualification", "durationMs": 3600000, "elapsedSeconds": time.Since(started).Seconds(), "successfulEditorMutations": edits, "maxEditLatencyMs": float64(maxLatency) / float64(time.Millisecond), "transcriptSegments": len(source.Segments), "checks": []string{"actual bundled whisper.cpp inference for all 60 minute units", "queued durable job and canonical finalization", "timestamped transcript retains recognizable source speech", "same-page guarded edits during recognition", "all observed edit commits below two seconds"}}
	raw, _ := json.MarshalIndent(evidence, "", "  ")
	if err = os.MkdirAll(filepath.Join(root, "evidence/phase2"), 0755); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(root, "evidence/phase2/one-hour-native.json"), raw, 0644); err != nil {
		t.Fatal(err)
	}
	t.Log(string(raw))
}
