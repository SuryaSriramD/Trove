// Package workers connects durable workspace jobs to supervised native processes.
package workers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"trove.local/phase0/contracts"
	"trove.local/phase0/internal/native"
	"trove.local/phase0/internal/workspace"
)

func Run(ctx context.Context, k *workspace.Kernel, _ contracts.ServiceExecutionContext, s *native.Supervisor) error {
	handlers := map[string]func(context.Context, contracts.JobsJob) error{}
	for _, kind := range []string{"IMPORT_NOTION", "EXTRACT_DOCUMENT", "CREATE_THUMBNAIL", "INDEX_CONTENT", "GENERATE_EMBEDDINGS", "SUMMARIZE_MEETING", "BACKUP_WORKSPACE", "MODEL_PACK_ACQUIRE", "INVALIDATE_MEMORY", "AUTOMATION_RUN"} {
		handlers[kind] = func(context.Context, contracts.JobsJob) error {
			return workspace.Fail("UNSUPPORTED_SCHEMA", "This job operation is not installed in the current release")
		}
	}
	handlers["SUMMARIZE_MEETING"] = func(ctx context.Context, j contracts.JobsJob) error { return summarizeMeeting(ctx, k, s, j) }
	handlers["TRANSCRIBE_AUDIO"] = func(ctx context.Context, j contracts.JobsJob) error { return transcribe(ctx, k, s, j) }
	for kind, handler := range k.RegisteredJobHandlers() {
		handlers[kind] = handler
	}
	return k.RunJobs(ctx, handlers)
}

type speechRunner interface {
	Run(context.Context, native.Request, func(native.Event)) (json.RawMessage, error)
	MemoryBudget() int64
}

func transcribe(ctx context.Context, k *workspace.Kernel, s *native.Supervisor, j contracts.JobsJob) error {
	if s == nil {
		return workspace.Fail("WORKER_FAILED", "Bundled native supervisor is unavailable")
	}
	return transcribeWithRunner(ctx, k, s, j)
}

// Each minute is independently committed. Durable source audio and stable IDs
// make interruption resumable without duplicating or re-recognizing earlier units.
func transcribeWithRunner(ctx context.Context, k *workspace.Kernel, s speechRunner, j contracts.JobsJob) error {
	done, err := k.JobUnitCommitted(ctx, string(j.JobId), "transcript-v1")
	if err != nil || done {
		return err
	}
	p, ok := j.Payload.Value.(contracts.JobsJobPayloadVariant5)
	if !ok || j.LeaseToken == nil {
		return workspace.Fail("INVALID_REQUEST", "Expected leased transcription payload")
	}
	e, err := k.JobExecution(ctx, string(j.JobId))
	if err != nil {
		return err
	}
	defer k.RevokeGrant(string(e.ScopeGrantId))
	meeting, _, err := k.ReadMeetingForJob(ctx, e, string(p.MeetingId))
	if err != nil {
		return err
	}
	if meeting.RecordingFileId == nil || *meeting.RecordingFileId != p.FileId {
		return workspace.Fail("INVALID_REQUEST", "Job recording does not match meeting")
	}
	source, meta, err := k.ReadFile(ctx, e, string(p.FileId))
	if err != nil {
		return err
	}
	defer source.Close()
	audio, cleanup, err := pcmInput(source, meta)
	if err != nil {
		return err
	}
	defer cleanup()
	info, err := audio.Stat()
	if err != nil {
		return err
	}
	const unitBytes int64 = 16000 * 4 * 60
	totalMS := info.Size() / 64
	sequence := int64(0)
	if j.Progress != nil {
		sequence = j.Progress.Sequence + 1
	}
	progress := func(completed int64, message string) {
		total := info.Size()
		_ = k.ProgressJob(ctx, string(j.JobId), string(*j.LeaseToken), contracts.JobsProgress{Sequence: sequence, Completed: completed * 64, Total: &total, Unit: "bytes", Message: &message})
		sequence++
	}
	for offset := int64(0); offset < info.Size(); offset += unitBytes {
		if err = ctx.Err(); err != nil {
			return err
		}
		unit := fmt.Sprintf("transcript-minute-%06d", offset/unitBytes)
		done, err = k.JobUnitCommitted(ctx, string(j.JobId), unit)
		if err != nil {
			return err
		}
		length := min(unitBytes, info.Size()-offset)
		if done {
			progress((offset+length)/64, "Resumed committed audio segment")
			continue
		}
		ids, err := k.ReserveJobIDs(ctx, string(j.JobId), string(*j.LeaseToken), unit, 512)
		if err != nil {
			return err
		}
		metadata := native.InputMetadata{SchemaVersion: 1, MeetingID: string(p.MeetingId), Language: "en", UseGPU: false}
		for i := 0; i < len(ids); i += 2 {
			metadata.SegmentIDs = append(metadata.SegmentIDs, native.SegmentIdentity{SegmentID: ids[i], BlockID: ids[i+1]})
		}
		body, err := recognizeUnit(ctx, s, j, p, audio, offset, length, metadata, func(event native.Event) {
			if event.Kind == "progress" {
				var nativeProgress contracts.JobsProgress
				if json.Unmarshal(event.Progress, &nativeProgress) == nil {
					progress(offset/64, fmt.Sprintf("Transcribing minute %d of %d", offset/unitBytes+1, (info.Size()+unitBytes-1)/unitBytes))
				}
			}
		})
		if err != nil {
			return err
		}
		if body.MeetingId != p.MeetingId {
			return workspace.Fail("INVALID_REQUEST", "Transcript returned a different meeting")
		}
		for i := range body.Segments {
			segment := &body.Segments[i]
			if segment.StartMs < 0 || segment.EndMs < segment.StartMs || segment.EndMs > length/64 {
				return workspace.Fail("INVALID_REQUEST", "Transcript timestamp exceeds audio segment")
			}
			segment.StartMs += offset / 64
			segment.EndMs += offset / 64
		}
		var appendCommand contracts.ServiceAppendTranscript
		appendRaw, _ := json.Marshal(map[string]any{"kind": "meeting.appendSegments", "meetingId": p.MeetingId, "segments": body.Segments})
		if err = json.Unmarshal(appendRaw, &appendCommand); err != nil {
			return err
		}
		guards, err := k.TranscriptJobGuards(ctx, e, string(j.JobId), string(*j.LeaseToken))
		if err != nil {
			return err
		}
		for _, segment := range body.Segments {
			guards = append(guards, contracts.ServiceExpectedRevision{Target: contracts.ServiceEntityRef{EntityType: "block", EntityId: segment.BlockId}, Revision: "0"})
		}
		req := contracts.ServiceBatchRequest{ContractVersion: 1, RequestId: contracts.DomainId(workspace.NewID()), WorkspaceId: contracts.DomainId(k.ID()), Mutation: contracts.ServiceMutationMeta{ScopeGrantId: e.ScopeGrantId, IdempotencyKey: "job-" + string(j.JobId) + "-" + unit, ExpectedRevisions: guards}, Commands: []contracts.ServiceCommand{{Value: appendCommand}}}
		response := k.CommitTranscriptJobBatch(ctx, e, string(j.JobId), string(*j.LeaseToken), unit, req)
		if fail, ok := response.Value.(contracts.ServiceBatchResponseVariant2); ok {
			return workspace.Fail(fail.Error.Code, fail.Error.Message)
		}
		progress((offset+length)/64, "Audio segment saved")
	}
	guards, err := k.TranscriptJobGuards(ctx, e, string(j.JobId), string(*j.LeaseToken))
	if err != nil {
		return err
	}
	req := contracts.ServiceBatchRequest{ContractVersion: 1, RequestId: contracts.DomainId(workspace.NewID()), WorkspaceId: contracts.DomainId(k.ID()), Mutation: contracts.ServiceMutationMeta{ScopeGrantId: e.ScopeGrantId, IdempotencyKey: "job-" + string(j.JobId) + "-transcript-v1", ExpectedRevisions: guards}, Commands: []contracts.ServiceCommand{{Value: contracts.ServiceFinalizeMeeting{Kind: "meeting.finalize", MeetingId: p.MeetingId, DurationMs: totalMS}}}}
	response := k.CommitTranscriptJobBatch(ctx, e, string(j.JobId), string(*j.LeaseToken), "transcript-v1", req)
	if fail, ok := response.Value.(contracts.ServiceBatchResponseVariant2); ok {
		return workspace.Fail(fail.Error.Code, fail.Error.Message)
	}
	return nil
}

func recognizeUnit(ctx context.Context, s speechRunner, j contracts.JobsJob, p contracts.JobsJobPayloadVariant5, audio *os.File, offset, length int64, metadata native.InputMetadata, onEvent func(native.Event)) (contracts.JobsWorkerResultVariant3, error) {
	var body contracts.JobsWorkerResultVariant3
	mf, err := os.CreateTemp("", "trove-transcript-metadata-")
	if err != nil {
		return body, err
	}
	defer os.Remove(mf.Name())
	defer mf.Close()
	if err = json.NewEncoder(mf).Encode(metadata); err != nil {
		return body, err
	}
	chunk, err := os.CreateTemp("", "trove-transcript-segment-")
	if err != nil {
		return body, err
	}
	defer os.Remove(chunk.Name())
	defer chunk.Close()
	if _, err = io.Copy(chunk, io.NewSectionReader(audio, offset, length)); err != nil {
		return body, err
	}
	mi, err := native.OpenInput(mf, "text", workspace.NewID())
	if err != nil {
		return body, err
	}
	ai, err := native.OpenInput(chunk, "audio", workspace.NewID())
	if err != nil {
		return body, err
	}
	wire := contracts.JobsWorkerRequest{ProtocolVersion: 1, RequestId: contracts.DomainId(workspace.NewID()), JobId: j.JobId, LeaseToken: *j.LeaseToken, CancellationId: contracts.DomainId(workspace.NewID()), Operation: "transcribe", ModelPackId: p.ModelPackId, Limits: contracts.JobsResourceLimits{Threads: 4, MemoryBytes: s.MemoryBudget(), TimeoutMs: 600000, MaxOutputBytes: 16 << 20, MaxTokens: 4096}}
	raw, err := s.Run(ctx, native.Request{Wire: wire, Inputs: []native.Input{mi, ai}, Priority: native.PriorityTranscription}, onEvent)
	if err != nil {
		return body, err
	}
	var result contracts.JobsWorkerResult
	if err = json.Unmarshal(raw, &result); err != nil {
		return body, err
	}
	var ok bool
	body, ok = result.Value.(contracts.JobsWorkerResultVariant3)
	if !ok {
		return body, errors.New("native result was not a transcript")
	}
	return body, nil
}
