package workspace

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"

	"trove.local/phase0/contracts"
	"trove.local/phase0/internal/platformfs"
)

type RecordingSegment struct {
	StartByte int64  `json:"startByte"`
	EndByte   int64  `json:"endByte"`
	SHA256    string `json:"sha256"`
}
type RecordingDraft struct {
	ID         string             `json:"id"`
	Title      string             `json:"title"`
	ActorID    string             `json:"actorId"`
	StartedAt  string             `json:"startedAt"`
	State      string             `json:"state"`
	Bytes      int64              `json:"bytes"`
	DurationMS int64              `json:"durationMs"`
	PageID     string             `json:"pageId"`
	MeetingID  string             `json:"meetingId"`
	FileID     string             `json:"fileId,omitempty"`
	Error      string             `json:"error,omitempty"`
	Segments   []RecordingSegment `json:"segments"`
}

var recordingID = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)
var recordingLocks sync.Map

func recordingLock(k *Kernel) *sync.Mutex {
	v, _ := recordingLocks.LoadOrStore(k.Path(), &sync.Mutex{})
	return v.(*sync.Mutex)
}
func (k *Kernel) recordingPath(id, ext string) (string, error) {
	if !recordingID.MatchString(id) {
		return "", Fail("INVALID_REQUEST", "Invalid recording identity")
	}
	directory := filepath.Join(k.path, "recordings")
	if err := safeDirectory(directory); err != nil {
		return "", err
	}
	return filepath.Join(directory, id+ext), nil
}
func (k *Kernel) writeRecording(d RecordingDraft) error {
	path, err := k.recordingPath(d.ID, ".json")
	if err != nil {
		return err
	}
	raw, err := json.Marshal(d)
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), "journal-")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err = f.Write(raw); err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	return platformfs.PublishFile(f.Name(), path, true)
}
func (k *Kernel) readRecording(e contracts.ServiceExecutionContext, id string) (RecordingDraft, error) {
	var d RecordingDraft
	if err := requireOwnerScope(k, e); err != nil {
		return d, err
	}
	path, err := k.recordingPath(id, ".json")
	if err != nil {
		return d, err
	}
	f, err := openAsset(path)
	if err != nil {
		return d, err
	}
	defer f.Close()
	if err = json.NewDecoder(io.LimitReader(f, 4<<20)).Decode(&d); err != nil {
		return d, err
	}
	if d.ID != id || d.ActorID != string(e.Actor.ActorId) {
		return d, Fail("SCOPE_DENIED", "Recording belongs to another owner")
	}
	return d, nil
}

// BeginRecording creates the recovery journal before the microphone process is
// admitted. The returned descriptor is workspace-owned and never a /tmp file.
func (k *Kernel) BeginRecording(ctx context.Context, e contracts.ServiceExecutionContext, title string) (RecordingDraft, *os.File, error) {
	lock := recordingLock(k)
	lock.Lock()
	defer lock.Unlock()
	var d RecordingDraft
	if err := requireOwnerScope(k, e); err != nil {
		return d, nil, err
	}
	title = strings.TrimSpace(title)
	if title == "" {
		title = "Meeting recording"
	}
	if len(title) > 512 {
		return d, nil, Fail("INVALID_REQUEST", "Recording title is too long")
	}
	d = RecordingDraft{ID: NewID(), Title: title, ActorID: string(e.Actor.ActorId), StartedAt: now(), State: "recording", PageID: NewID(), MeetingID: NewID(), Segments: []RecordingSegment{}}
	path, err := k.recordingPath(d.ID, ".f32le")
	if err != nil {
		return d, nil, err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0600)
	if err != nil {
		return d, nil, err
	}
	if err = f.Sync(); err == nil {
		err = k.writeRecording(d)
	}
	if err != nil {
		f.Close()
		return d, nil, err
	}
	return d, f, nil
}

// CheckpointRecording verifies and hashes every newly flushed byte range before
// publishing its journal entry. A crash can leave an unjournaled tail; recovery
// explicitly retains it and verifies its float samples at finalization.
func (k *Kernel) CheckpointRecording(ctx context.Context, e contracts.ServiceExecutionContext, id string, terminal bool, captureError string) (RecordingDraft, error) {
	lock := recordingLock(k)
	lock.Lock()
	defer lock.Unlock()
	d, err := k.readRecording(e, id)
	if err != nil {
		return d, err
	}
	if d.State == "saved" {
		return d, nil
	}
	path, err := k.recordingPath(id, ".f32le")
	if err != nil {
		return d, err
	}
	f, err := openAsset(path)
	if err != nil {
		return d, err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return d, err
	}
	size := st.Size() - st.Size()%4
	if size > 16000*4*7200 || size < d.Bytes {
		return d, Fail("INVALID_REQUEST", "Recording length is invalid")
	}
	if size > d.Bytes {
		h := sha256.New()
		if _, err = io.Copy(h, io.NewSectionReader(f, d.Bytes, size-d.Bytes)); err != nil {
			return d, err
		}
		d.Segments = append(d.Segments, RecordingSegment{StartByte: d.Bytes, EndByte: size, SHA256: hex.EncodeToString(h.Sum(nil))})
		d.Bytes = size
		d.DurationMS = size / 64
	}
	if terminal {
		d.State = "captured"
		if captureError != "" {
			d.Error = captureError
		}
		if d.Error != "" {
			d.State = "interrupted"
		}
	}
	return d, k.writeRecording(d)
}
func (k *Kernel) ListRecordings(ctx context.Context, e contracts.ServiceExecutionContext) ([]RecordingDraft, error) {
	if err := requireOwnerScope(k, e); err != nil {
		return nil, err
	}
	directory := filepath.Join(k.path, "recordings")
	if err := safeDirectory(directory); err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(directory)
	if err != nil {
		return nil, err
	}
	out := []RecordingDraft{}
	lock := recordingLock(k)
	lock.Lock()
	defer lock.Unlock()
	for _, entry := range entries {
		if !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		id := strings.TrimSuffix(entry.Name(), ".json")
		d, err := k.readRecording(e, id)
		if err != nil {
			return nil, err
		}
		if d.State != "saved" {
			out = append(out, d)
		}
	}
	return out, nil
}
func (k *Kernel) recordingBatch(ctx context.Context, e contracts.ServiceExecutionContext, key string, commands []any, guards []contracts.ServiceExpectedRevision) error {
	var req contracts.ServiceBatchRequest
	if err := json.Unmarshal(marshal(map[string]any{"contractVersion": 1, "requestId": NewID(), "workspaceId": k.ID(), "mutation": map[string]any{"idempotencyKey": key, "scopeGrantId": e.ScopeGrantId, "expectedRevisions": guards}, "commands": commands}), &req); err != nil {
		return err
	}
	result := k.Batch(ctx, e, req)
	if fail, ok := result.Value.(contracts.ServiceBatchResponseVariant2); ok {
		return Fail(fail.Error.Code, fail.Error.Message)
	}
	return nil
}
func (k *Kernel) currentGuard(ctx context.Context, kind, id string) (contracts.ServiceExpectedRevision, error) {
	var revision string
	err := k.db.QueryRowContext(ctx, "SELECT revision FROM "+entityTables[kind]+" WHERE id=?", id).Scan(&revision)
	if errors.Is(err, sql.ErrNoRows) {
		revision = "0"
		err = nil
	}
	return required(kind, id, revision), err
}

// CreateMeetingFromSelection retains any format as an ordinary canonical asset.
// Recognition separately rejects unsupported codecs with an actionable error.
func (k *Kernel) CreateMeetingFromSelection(ctx context.Context, e contracts.ServiceExecutionContext, handle, title string) (RecordingDraft, error) {
	if err := requireOwnerScope(k, e); err != nil {
		return RecordingDraft{}, err
	}
	title = strings.TrimSpace(title)
	if title == "" {
		title = "Uploaded recording"
	}
	if len(title) > 512 {
		return RecordingDraft{}, Fail("INVALID_REQUEST", "Recording title is too long")
	}
	prepared, err := prepareFile(ctx, k, e, map[string]any{"selectionHandle": handle})
	if err != nil {
		return RecordingDraft{}, err
	}
	d := RecordingDraft{ID: NewID(), Title: title, ActorID: string(e.Actor.ActorId), StartedAt: now(), State: "captured", PageID: NewID(), MeetingID: NewID(), Segments: []RecordingSegment{}}
	p := prepared.(contracts.ServicePrepareFileResult)
	var originalName string
	if err = k.db.QueryRowContext(ctx, "SELECT original_name FROM file_ingests WHERE handle=?", p.IngestHandle).Scan(&originalName); err != nil {
		return d, err
	}
	// Preserve the raw source and publish a browser-playable PCM WAV for seeking.
	// Malformed raw input stays retained and receives the normal decoder error.
	if strings.HasSuffix(strings.ToLower(originalName), ".f32le") {
		source, readErr := openAsset(filepath.Join(k.path, "files", string(p.ContentHash)[:2], string(p.ContentHash)))
		if readErr != nil {
			return d, readErr
		}
		defer source.Close()
		size, readErr := source.Stat()
		if readErr != nil {
			return d, readErr
		}
		wav, readErr := os.CreateTemp(filepath.Join(k.path, ".staging"), "recording-*.wav")
		if readErr != nil {
			return d, readErr
		}
		defer os.Remove(wav.Name())
		readErr = writeRecordingWAV(ctx, wav, source, size.Size())
		if readErr == nil {
			readErr = wav.Sync()
		}
		wav.Close()
		if readErr == nil {
			selection, err := k.RegisterSelection(wav.Name())
			if err != nil {
				return d, err
			}
			converted, err := prepareFile(ctx, k, e, map[string]any{"selectionHandle": selection})
			if err != nil {
				return d, err
			}
			d, err = k.commitRecording(ctx, e, d, converted.(contracts.ServicePrepareFileResult))
			if err != nil {
				return d, err
			}
			pageGuard, err := k.currentGuard(ctx, "page", d.PageID)
			if err != nil {
				return d, err
			}
			fileGuard, err := k.currentGuard(ctx, "file", string(p.FileId))
			if err != nil {
				return d, err
			}
			if err = k.recordingBatch(ctx, e, "recording-original-"+d.ID, []any{map[string]any{"kind": "file.attach", "pageId": d.PageID, "fileId": p.FileId, "ingestHandle": p.IngestHandle}}, []contracts.ServiceExpectedRevision{pageGuard, fileGuard}); err != nil {
				return d, err
			}
			return d, nil
		}
	}
	return k.commitRecording(ctx, e, d, p)
}
func (k *Kernel) commitRecording(ctx context.Context, e contracts.ServiceExecutionContext, d RecordingDraft, p contracts.ServicePrepareFileResult) (RecordingDraft, error) {
	pageGuard, err := k.currentGuard(ctx, "page", d.PageID)
	if err != nil {
		return d, err
	}
	if pageGuard.Revision == "0" {
		if err = k.recordingBatch(ctx, e, "recording-page-"+d.ID, []any{map[string]any{"kind": "page.create", "pageId": d.PageID, "title": d.Title}}, []contracts.ServiceExpectedRevision{pageGuard}); err != nil {
			return d, err
		}
	}
	pageGuard, err = k.currentGuard(ctx, "page", d.PageID)
	if err != nil {
		return d, err
	}
	fileGuard, err := k.currentGuard(ctx, "file", string(p.FileId))
	if err != nil {
		return d, err
	}
	var attached int
	if err = k.db.QueryRowContext(ctx, "SELECT count(*) FROM attachments WHERE page_id=? AND file_id=?", d.PageID, p.FileId).Scan(&attached); err != nil {
		return d, err
	}
	if attached == 0 {
		if err = k.recordingBatch(ctx, e, "recording-attach-"+d.ID+"-"+string(p.IngestHandle), []any{map[string]any{"kind": "file.attach", "pageId": d.PageID, "fileId": p.FileId, "ingestHandle": p.IngestHandle}}, []contracts.ServiceExpectedRevision{pageGuard, fileGuard}); err != nil {
			return d, err
		}
	}
	meetingGuard, err := k.currentGuard(ctx, "meeting", d.MeetingID)
	if err != nil {
		return d, err
	}
	if meetingGuard.Revision == "0" {
		pageGuard, err = k.currentGuard(ctx, "page", d.PageID)
		if err != nil {
			return d, err
		}
		fileGuard, err = k.currentGuard(ctx, "file", string(p.FileId))
		if err != nil {
			return d, err
		}
		if err = k.recordingBatch(ctx, e, "recording-meeting-"+d.ID, []any{map[string]any{"kind": "meeting.create", "meetingId": d.MeetingID, "pageId": d.PageID, "recordingFileId": p.FileId, "startedAt": d.StartedAt}}, []contracts.ServiceExpectedRevision{pageGuard, fileGuard, meetingGuard}); err != nil {
			return d, err
		}
	}
	d.FileID = string(p.FileId)
	d.State = "saved"
	return d, nil
}
func (k *Kernel) FinalizeRecording(ctx context.Context, e contracts.ServiceExecutionContext, id string) (RecordingDraft, error) {
	d, err := k.CheckpointRecording(ctx, e, id, true, "")
	if err != nil {
		return d, err
	}
	lock := recordingLock(k)
	lock.Lock()
	defer lock.Unlock()
	d, err = k.readRecording(e, id)
	if err != nil || d.State == "saved" {
		return d, err
	}
	if d.Bytes == 0 {
		return d, Fail("INVALID_REQUEST", "Recording contains no audio; the capture journal is retained")
	}
	rawPath, err := k.recordingPath(id, ".f32le")
	if err != nil {
		return d, err
	}
	source, err := openAsset(rawPath)
	if err != nil {
		return d, err
	}
	defer source.Close()
	// Detect corruption of previously acknowledged segments, including a restore.
	for _, segment := range d.Segments {
		h := sha256.New()
		if _, err = io.Copy(h, io.NewSectionReader(source, segment.StartByte, segment.EndByte-segment.StartByte)); err != nil {
			return d, err
		}
		if hex.EncodeToString(h.Sum(nil)) != segment.SHA256 {
			return d, Fail("INVALID_REQUEST", "Recorded segment hash mismatch; original audio retained")
		}
	}
	wavPath, err := k.recordingPath(id, ".wav")
	if err != nil {
		return d, err
	}
	out, err := os.OpenFile(wavPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0600)
	if err != nil {
		return d, err
	}
	err = writeRecordingWAV(ctx, out, source, d.Bytes)
	if err == nil {
		err = out.Sync()
	}
	closeErr := out.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		return d, err
	}
	handle, err := k.RegisterSelection(wavPath)
	if err != nil {
		return d, err
	}
	p, err := prepareFile(ctx, k, e, map[string]any{"selectionHandle": handle})
	if err != nil {
		return d, err
	}
	d, err = k.commitRecording(ctx, e, d, p.(contracts.ServicePrepareFileResult))
	if err != nil {
		return d, err
	}
	if err = k.writeRecording(d); err != nil {
		return d, err
	}
	// Native WAV is now hash-verified and retained by canonical history. Keep the
	// segment journal as provenance, avoiding duplicate permanent audio storage.
	_ = os.Remove(rawPath)
	_ = os.Remove(wavPath)
	return d, nil
}
func writeRecordingWAV(ctx context.Context, out io.Writer, source io.ReaderAt, size int64) error {
	if size <= 0 || size%4 != 0 || size > 16000*4*7200 {
		return Fail("INVALID_REQUEST", "Invalid captured PCM duration")
	}
	header := make([]byte, 44)
	copy(header, "RIFF")
	binary.LittleEndian.PutUint32(header[4:], uint32(36+size/2))
	copy(header[8:], "WAVEfmt ")
	binary.LittleEndian.PutUint32(header[16:], 16)
	binary.LittleEndian.PutUint16(header[20:], 1)
	binary.LittleEndian.PutUint16(header[22:], 1)
	binary.LittleEndian.PutUint32(header[24:], 16000)
	binary.LittleEndian.PutUint32(header[28:], 32000)
	binary.LittleEndian.PutUint16(header[32:], 2)
	binary.LittleEndian.PutUint16(header[34:], 16)
	copy(header[36:], "data")
	binary.LittleEndian.PutUint32(header[40:], uint32(size/2))
	if _, err := out.Write(header); err != nil {
		return err
	}
	reader := io.NewSectionReader(source, 0, size)
	buf := make([]byte, 64<<10)
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		n, err := reader.Read(buf)
		if n > 0 {
			converted := make([]byte, n/2)
			for i := 0; i < n; i += 4 {
				sample := math.Float32frombits(binary.LittleEndian.Uint32(buf[i : i+4]))
				if math.IsNaN(float64(sample)) || math.IsInf(float64(sample), 0) || math.Abs(float64(sample)) > 1.01 {
					return Fail("INVALID_REQUEST", "Captured audio contains invalid PCM samples")
				}
				value := int16(math.Round(max(-1, min(1, float64(sample))) * 32767))
				binary.LittleEndian.PutUint16(converted[i/2:], uint16(value))
			}
			if _, err = out.Write(converted); err != nil {
				return err
			}
		}
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
	}
}

func (k *Kernel) QueueMeetingTranscription(ctx context.Context, e contracts.ServiceExecutionContext, meeting, pack string) (any, error) {
	if e.Actor.Kind != "user" || e.Origin != "user" {
		return nil, Fail("APPROVAL_REQUIRED", "Transcription requires an explicit action")
	}
	m, _, err := k.ReadMeetingForJob(ctx, e, meeting)
	if err != nil {
		return nil, err
	}
	if m.RecordingFileId == nil {
		return nil, Fail("NOT_FOUND", "Meeting has no audio")
	}
	if m.State == "ready" {
		return nil, Fail("CONFLICT", "This recording has already been transcribed")
	}
	var existing string
	err = k.db.QueryRowContext(ctx, "SELECT j.job_id FROM jobs j JOIN job_context c USING(job_id) WHERE j.kind='TRANSCRIBE_AUDIO' AND json_extract(j.payload_json,'$.meetingId')=? AND c.actor_id=? ORDER BY j.created_at LIMIT 1", meeting, e.Actor.ActorId).Scan(&existing)
	if err == nil {
		job, err := k.ReadJob(ctx, existing)
		if err != nil {
			return nil, err
		}
		p := job.Payload.Value.(contracts.JobsJobPayloadVariant5)
		if string(p.ModelPackId) != pack {
			return nil, Fail("CONFLICT", "Resume this transcript with its original speech pack")
		}
		if job.State == "failed" || job.State == "cancelled" {
			err = k.transitionJob(ctx, existing, func(tx *sql.Tx) error {
				if err = k.authorizeEntity(ctx, tx, e, "meeting", meeting); err != nil {
					return err
				}
				_, err := tx.ExecContext(ctx, "UPDATE jobs SET state='queued',revision=revision+1,max_attempts=attempt+3,lease_token=NULL,lease_expires_at=NULL,next_attempt_at=NULL,error_json=NULL,updated_at=? WHERE job_id=? AND state IN('failed','cancelled')", now(), existing)
				return err
			})
			if err != nil {
				return nil, err
			}
		}
		return k.getAuthorizedJob(ctx, e, existing)
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	request := map[string]any{"contractVersion": 1, "requestId": NewID(), "workspaceId": k.ID(), "kind": "TRANSCRIBE_AUDIO", "idempotencyKey": "meeting-transcription-" + meeting, "payload": map[string]any{"kind": "transcribe", "meetingId": meeting, "fileId": *m.RecordingFileId, "modelPackId": pack}}
	if err = contracts.Validate("jobs", "EnqueueJobRequest", marshal(request)); err != nil {
		return nil, Fail("INVALID_REQUEST", fmt.Sprint(err))
	}
	return k.enqueueJob(ctx, e, request)
}

func init() {
	// A workspace lock is acquired before initializers run, so no microphone
	// capture can still own this journal in a newly opened process. Mark orphaned
	// captures as interrupted without changing any audio bytes or segment hashes.
	RegisterInitializer(func(ctx context.Context, k *Kernel) error {
		directory := filepath.Join(k.path, "recordings")
		entries, err := os.ReadDir(directory)
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		if err != nil {
			return err
		}
		execution := k.OwnerContext()
		defer k.RevokeGrant(string(execution.ScopeGrantId))
		lock := recordingLock(k)
		lock.Lock()
		defer lock.Unlock()
		for _, entry := range entries {
			if !strings.HasSuffix(entry.Name(), ".json") {
				continue
			}
			id := strings.TrimSuffix(entry.Name(), ".json")
			draft, err := k.readRecording(execution, id)
			if err != nil {
				return err
			}
			if draft.State != "recording" {
				continue
			}
			draft.State = "interrupted"
			if draft.Error == "" {
				draft.Error = "Capture was interrupted before completion; flushed audio is retained for recovery"
			}
			if err = k.writeRecording(draft); err != nil {
				return err
			}
		}
		return nil
	})
}
