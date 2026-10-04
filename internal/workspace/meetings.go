package workspace

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"trove.local/phase0/contracts"
)

func init() {
	for _, kind := range []string{"meeting.create", "meeting.appendSegments", "meeting.finalize"} {
		RegisterCommand(kind, meetingCommand)
	}
	RegisterSnapshotReader("meetings", meetingSnapshot)
	RegisterSnapshotReader("transcriptSegments", segmentSnapshot)
}
func meetingCommand(m *Mutation, c map[string]any) error {
	id := str(c["meetingId"])
	if err := m.Touch("meeting", id); err != nil {
		return err
	}
	if str(c["kind"]) == "meeting.create" {
		page := str(c["pageId"])
		if m.GuardRevision("meeting", id) != "0" {
			return Fail("CONFLICT", "Meeting already exists")
		}
		if err := m.TouchPage(page); err != nil {
			return err
		}
		if err := m.Require("file", str(c["recordingFileId"])); err != nil {
			return err
		}
		var attached int
		if err := m.Tx.QueryRowContext(m.Context, "SELECT count(*) FROM attachments WHERE page_id=? AND file_id=?", page, c["recordingFileId"]).Scan(&attached); err != nil {
			return err
		}
		if attached != 1 {
			return Fail("INVALID_REQUEST", "Recording must be attached to the meeting page")
		}
		duration, err := m.recordingDurationMS(str(c["recordingFileId"]))
		if err != nil {
			return err
		}
		_, err = m.Exec("INSERT INTO meetings VALUES(?,?,?,?,?,'captured',1)", id, page, c["recordingFileId"], c["startedAt"], duration)
		return err
	}
	var page, state string
	var duration int64
	if err := m.Tx.QueryRowContext(m.Context, "SELECT page_id,state,duration_ms FROM meetings WHERE id=?", id).Scan(&page, &state, &duration); err != nil {
		return err
	}
	if err := m.TouchPage(page); err != nil {
		return err
	}
	if state == "recording" {
		return Fail("CONFLICT", "Recording must finish before transcription")
	}
	switch str(c["kind"]) {
	case "meeting.appendSegments":
		segments, _ := c["segments"].([]any)
		for _, raw := range segments {
			s := raw.(map[string]any)
			segmentID, block := str(s["segmentId"]), str(s["blockId"])
			if number(s["endMs"]) < number(s["startMs"]) {
				return Fail("INVALID_REQUEST", "Transcript offsets are reversed")
			}
			var count int
			if err := m.Tx.QueryRowContext(m.Context, "SELECT count(*) FROM transcript_segments WHERE id=? OR block_id=?", segmentID, block).Scan(&count); err != nil {
				return err
			}
			if count > 0 {
				return Fail("CONFLICT", "Transcript identity already exists")
			}
			if err := m.Touch("block", block); err != nil {
				return err
			}
			if m.GuardRevision("block", block) != "0" {
				return Fail("CONFLICT", "Transcript block already exists")
			}
			key, err := appendKey(m.Context, m.Tx, page, "")
			if err != nil {
				return err
			}
			data := map[string]any{"richText": []any{map[string]any{"text": str(s["text"])}}}
			if _, err = m.Exec("INSERT INTO blocks(id,page_id,sort_key,type,schema_version,data_json,revision) VALUES(?,?,?,'paragraph',1,?,1)", block, page, key, string(marshal(data))); err != nil {
				return err
			}
			if _, err = m.Exec("INSERT INTO transcript_segments VALUES(?,?,?,?,?,?,?,1)", segmentID, id, block, number(s["startMs"]), number(s["endMs"]), str(s["text"]), nullable(s["speakerLabel"])); err != nil {
				return err
			}
		}
		_, err := m.Exec("UPDATE meetings SET state='transcribing' WHERE id=?", id)
		return err
	case "meeting.finalize":
		duration = number(c["durationMs"])
		var max int64
		if err := m.Tx.QueryRowContext(m.Context, "SELECT COALESCE(MAX(end_ms),0) FROM transcript_segments WHERE meeting_id=?", id).Scan(&max); err != nil {
			return err
		}
		if max > duration {
			return Fail("INVALID_REQUEST", "Transcript exceeds recording duration")
		}
		_, err := m.Exec("UPDATE meetings SET duration_ms=?,state='ready' WHERE id=?", duration, id)
		return err
	}
	return Fail("UNSUPPORTED_SCHEMA", "Unsupported meeting mutation")
}
func (k *Kernel) ReadMeetingForJob(ctx context.Context, e contracts.ServiceExecutionContext, id string) (contracts.DomainMeeting, contracts.DomainRevision, error) {
	var meeting contracts.DomainMeeting
	var pageRevision contracts.DomainRevision
	if err := k.authorizeEntity(ctx, k.db, e, "meeting", id); err != nil {
		return meeting, pageRevision, err
	}
	var file sql.NullString
	err := k.db.QueryRowContext(ctx, "SELECT m.id,m.page_id,m.recording_file_id,m.started_at,m.duration_ms,m.state,m.revision,p.revision FROM meetings m JOIN pages p ON p.id=m.page_id WHERE m.id=? AND p.deleted_at IS NULL", id).Scan(&meeting.Id, &meeting.PageId, &file, &meeting.StartedAt, &meeting.DurationMs, &meeting.State, &meeting.Revision, &pageRevision)
	if file.Valid {
		v := contracts.DomainId(file.String)
		meeting.RecordingFileId = &v
	}
	return meeting, pageRevision, err
}
func meetingSnapshot(ctx context.Context, k *Kernel, tx *sql.Tx, e contracts.ServiceExecutionContext) (any, error) {
	rows, err := tx.QueryContext(ctx, "SELECT id,page_id,recording_file_id,started_at,duration_ms,state,revision FROM meetings")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []contracts.DomainMeeting{}
	for rows.Next() {
		var m contracts.DomainMeeting
		var f sql.NullString
		if err = rows.Scan(&m.Id, &m.PageId, &f, &m.StartedAt, &m.DurationMs, &m.State, &m.Revision); err != nil {
			return nil, err
		}
		if f.Valid {
			v := contracts.DomainId(f.String)
			m.RecordingFileId = &v
		}
		if k.authorizePage(ctx, tx, e, string(m.PageId)) == nil {
			out = append(out, m)
		}
	}
	return out, rows.Err()
}
func segmentSnapshot(ctx context.Context, k *Kernel, tx *sql.Tx, e contracts.ServiceExecutionContext) (any, error) {
	rows, err := tx.QueryContext(ctx, "SELECT s.id,s.meeting_id,s.block_id,s.start_ms,s.end_ms,s.text,s.speaker_label,s.revision,m.page_id FROM transcript_segments s JOIN meetings m ON m.id=s.meeting_id ORDER BY s.start_ms,s.id")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []contracts.DomainTranscriptSegment{}
	for rows.Next() {
		var s contracts.DomainTranscriptSegment
		var speaker sql.NullString
		var page string
		if err = rows.Scan(&s.Id, &s.MeetingId, &s.BlockId, &s.StartMs, &s.EndMs, &s.Text, &speaker, &s.Revision, &page); err != nil {
			return nil, err
		}
		if speaker.Valid {
			s.SpeakerLabel = &speaker.String
		}
		if k.authorizePage(ctx, tx, e, page) == nil {
			out = append(out, s)
		}
	}
	return out, rows.Err()
}

// JobUnitCommitted allows recovery to skip expensive repeated native inference.
func (k *Kernel) JobUnitCommitted(ctx context.Context, id, unit string) (bool, error) {
	var one int
	err := k.db.QueryRowContext(ctx, "SELECT 1 FROM job_results WHERE job_id=? AND unit_key=?", id, unit).Scan(&one)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return one == 1, err
}
func (k *Kernel) JobSourceRevisions(ctx context.Context, id string) ([]contracts.ServiceExpectedRevision, error) {
	rows, err := k.db.QueryContext(ctx, "SELECT entity_type,entity_id,revision FROM job_sources WHERE job_id=?", id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []contracts.ServiceExpectedRevision{}
	for rows.Next() {
		var kind, id, rev string
		if err = rows.Scan(&kind, &id, &rev); err != nil {
			return nil, err
		}
		out = append(out, required(kind, id, rev))
	}
	return out, rows.Err()
}

// TranscriptJobGuards supplies a snapshot of the job's guards. The dedicated
// transcript commit resolves the destination page revision atomically; these
// advisory guards may become stale while the user continues typing.
func (k *Kernel) TranscriptJobGuards(ctx context.Context, e contracts.ServiceExecutionContext, id, lease string) ([]contracts.ServiceExpectedRevision, error) {
	err := k.write(ctx, func(tx *sql.Tx) error {
		job, err := k.checkLease(ctx, tx, id, lease, false)
		if err != nil {
			return err
		}
		p, ok := job.Payload.Value.(contracts.JobsJobPayloadVariant5)
		if !ok || job.Kind != "TRANSCRIBE_AUDIO" || e.Actor.Kind != "job" || string(e.Actor.ActorId) != id {
			return Fail("SCOPE_DENIED", "Only the leased transcription job can rebase its append destination")
		}
		var page string
		if err = tx.QueryRowContext(ctx, "SELECT p.id FROM pages p JOIN meetings m ON m.page_id=p.id WHERE m.id=? AND p.deleted_at IS NULL", p.MeetingId).Scan(&page); err != nil {
			return err
		}
		if err = k.authorizePage(ctx, tx, e, page); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return k.JobSourceRevisions(ctx, id)
}

func init() {
	RegisterCommitProjection(func(m *Mutation) error {
		for _, change := range m.touched {
			if change.Target.EntityType != "block" {
				continue
			}
			var raw, revision string
			err := m.Tx.QueryRowContext(m.Context, "SELECT b.data_json,b.revision FROM blocks b JOIN transcript_segments s ON s.block_id=b.id WHERE b.id=?", change.Target.EntityId).Scan(&raw, &revision)
			if errors.Is(err, sql.ErrNoRows) {
				continue
			}
			if err != nil {
				return err
			}
			var data struct {
				RichText []struct {
					Text string `json:"text"`
				} `json:"richText"`
			}
			if err = json.Unmarshal([]byte(raw), &data); err != nil {
				return err
			}
			text := ""
			for _, span := range data.RichText {
				text += span.Text
			}
			if _, err = m.Exec("UPDATE transcript_segments SET text=?,revision=? WHERE block_id=?", text, revision, change.Target.EntityId); err != nil {
				return err
			}
		}
		return nil
	})
}

// Invoked only after validateJobCommit has fenced the lease/delegation and
// allowed commands, while holding the canonical writer transaction. It never
// rebases the meeting/audio inputs or any other page/entity guard.
func (k *Kernel) rebaseTranscriptDestination(ctx context.Context, tx *sql.Tx, e contracts.ServiceExecutionContext, jobID string, p contracts.JobsJobPayloadVariant5, r *contracts.ServiceBatchRequest) error {
	var page, revision, file string
	err := tx.QueryRowContext(ctx, "SELECT p.id,p.revision,m.recording_file_id FROM meetings m JOIN pages p ON p.id=m.page_id WHERE m.id=? AND p.deleted_at IS NULL", p.MeetingId).Scan(&page, &revision, &file)
	if err != nil {
		return err
	}
	if file != string(p.FileId) {
		return Fail("CONFLICT", "Recording input no longer matches the meeting")
	}
	if err = k.authorizePage(ctx, tx, e, page); err != nil {
		return err
	}
	// The destination must be the same page bound when this job was delegated.
	var bound int
	if err = tx.QueryRowContext(ctx, "SELECT count(*) FROM job_sources WHERE job_id=? AND entity_type='page' AND entity_id=?", jobID, page).Scan(&bound); err != nil {
		return err
	}
	if bound != 1 {
		return Fail("CONFLICT", "Transcription destination changed after enqueue")
	}
	guarded := false
	for i := range r.Mutation.ExpectedRevisions {
		guard := &r.Mutation.ExpectedRevisions[i]
		if guard.Target.EntityType == "page" && string(guard.Target.EntityId) == page {
			if guarded {
				return Fail("INVALID_REQUEST", "Duplicate transcription destination guard")
			}
			guard.Revision = revision
			guarded = true
		}
	}
	if !guarded {
		return Fail("INVALID_REQUEST", "Transcription requires a destination page guard")
	}
	_, err = tx.ExecContext(ctx, "UPDATE job_sources SET revision=? WHERE job_id=? AND entity_type='page' AND entity_id=?", revision, jobID, page)
	return err
}
