package workspace

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"trove.local/phase0/contracts"
)

type MeetingEvidence struct {
	SegmentID string `json:"segmentId"`
	BlockID   string `json:"blockId"`
	StartMS   int64  `json:"startMs"`
	EndMS     int64  `json:"endMs"`
	Text      string `json:"text"`
	Revision  string `json:"revision"`
}
type MeetingSource struct {
	MeetingID    string            `json:"meetingId"`
	PageID       string            `json:"pageId"`
	PageRevision string            `json:"pageRevision"`
	EvidenceHash string            `json:"evidenceHash"`
	Segments     []MeetingEvidence `json:"segments"`
}
type MeetingClaim struct {
	Text       string   `json:"text"`
	SegmentIDs []string `json:"segmentIds"`
}
type MeetingIntelligence struct {
	Summary     []MeetingClaim `json:"summary"`
	KeyPoints   []MeetingClaim `json:"keyPoints"`
	Decisions   []MeetingClaim `json:"decisions"`
	Questions   []MeetingClaim `json:"questions"`
	ActionItems []MeetingClaim `json:"actionItems"`
}
type MeetingDraft struct {
	ID           string              `json:"id"`
	MeetingID    string              `json:"meetingId"`
	PageID       string              `json:"pageId"`
	EvidenceHash string              `json:"evidenceHash"`
	State        string              `json:"state"`
	CreatedAt    string              `json:"createdAt"`
	Body         MeetingIntelligence `json:"body"`
	Stale        bool                `json:"stale"`
}

func init() {
	RegisterInitializer(func(ctx context.Context, k *Kernel) error {
		return k.write(ctx, func(tx *sql.Tx) error {
			_, err := tx.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS meeting_intelligence(id TEXT PRIMARY KEY,meeting_id TEXT NOT NULL REFERENCES meetings(id),page_id TEXT NOT NULL REFERENCES pages(id),evidence_hash TEXT NOT NULL,state TEXT NOT NULL CHECK(state IN('draft','applied')),body_json TEXT NOT NULL CHECK(json_valid(body_json)),created_at TEXT NOT NULL,job_id TEXT NOT NULL UNIQUE REFERENCES jobs(job_id));`)
			return err
		})
	})
	RegisterQuery("meetings.drafts", func(ctx context.Context, k *Kernel, e contracts.ServiceExecutionContext, r map[string]any) (any, error) {
		return k.MeetingDrafts(ctx, e, str(r["meetingId"]))
	})
	RegisterQuery("meetings.transcribe", func(ctx context.Context, k *Kernel, e contracts.ServiceExecutionContext, r map[string]any) (any, error) {
		return k.QueueMeetingTranscription(ctx, e, str(r["meetingId"]), str(r["modelPackId"]))
	})
	RegisterQuery("meetings.applyDraft", func(ctx context.Context, k *Kernel, e contracts.ServiceExecutionContext, r map[string]any) (any, error) {
		return k.ApplyMeetingDraft(ctx, e, str(r["draftId"]))
	})
}
func (k *Kernel) MeetingSource(ctx context.Context, e contracts.ServiceExecutionContext, meeting string) (MeetingSource, error) {
	var out MeetingSource
	m, rev, err := k.ReadMeetingForJob(ctx, e, meeting)
	if err != nil {
		return out, err
	}
	out = MeetingSource{MeetingID: meeting, PageID: string(m.PageId), PageRevision: string(rev), Segments: []MeetingEvidence{}}
	rows, err := k.db.QueryContext(ctx, "SELECT s.id,s.block_id,s.start_ms,s.end_ms,b.data_json,b.revision FROM transcript_segments s JOIN blocks b ON b.id=s.block_id WHERE s.meeting_id=? ORDER BY s.start_ms,s.id", meeting)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var s MeetingEvidence
		var raw string
		if err = rows.Scan(&s.SegmentID, &s.BlockID, &s.StartMS, &s.EndMS, &raw, &s.Revision); err != nil {
			return out, err
		}
		var data struct {
			RichText []struct {
				Text string `json:"text"`
			} `json:"richText"`
		}
		if err = json.Unmarshal([]byte(raw), &data); err != nil {
			return out, err
		}
		for _, span := range data.RichText {
			s.Text += span.Text
		}
		out.Segments = append(out.Segments, s)
	}
	if err = rows.Err(); err != nil {
		return out, err
	}
	hash := sha256.Sum256(marshal(out.Segments))
	out.EvidenceHash = hex.EncodeToString(hash[:])
	return out, nil
}
func ValidateMeetingIntelligence(body MeetingIntelligence, source MeetingSource) error {
	valid := map[string]bool{}
	for _, s := range source.Segments {
		valid[s.SegmentID] = true
	}
	count := 0
	for _, section := range [][]MeetingClaim{body.Summary, body.KeyPoints, body.Decisions, body.Questions, body.ActionItems} {
		for _, claim := range section {
			count++
			if len(strings.TrimSpace(claim.Text)) == 0 || len(claim.Text) > 8000 || len(claim.SegmentIDs) == 0 || len(claim.SegmentIDs) > 32 {
				return Fail("INVALID_REQUEST", "Each meeting claim needs readable text and transcript evidence")
			}
			for _, id := range claim.SegmentIDs {
				if !valid[id] {
					return Fail("STALE_CITATION", "Meeting claim references missing transcript evidence")
				}
			}
		}
	}
	if count == 0 || count > 250 {
		return Fail("INVALID_REQUEST", "Meeting draft must contain between 1 and 250 evidence-linked claims")
	}
	return nil
}

// SaveMeetingDraft is a lease-fenced derived result. A model cannot mutate any
// page/task through this API; approval remains a separate user operation.
func (k *Kernel) SaveMeetingDraft(ctx context.Context, e contracts.ServiceExecutionContext, job, lease string, source MeetingSource, body MeetingIntelligence) error {
	if err := ValidateMeetingIntelligence(body, source); err != nil {
		return err
	}
	return k.write(ctx, func(tx *sql.Tx) error {
		j, err := k.checkLease(ctx, tx, job, lease, false)
		if err != nil {
			return err
		}
		p, ok := j.Payload.Value.(contracts.JobsJobPayloadVariant6)
		if !ok || j.Kind != "SUMMARIZE_MEETING" || string(p.MeetingId) != source.MeetingID || e.Actor.Kind != "job" || string(e.Actor.ActorId) != job {
			return Fail("SCOPE_DENIED", "Meeting summary does not match its approved job")
		}
		var pageRev string
		if err = tx.QueryRowContext(ctx, "SELECT revision FROM pages WHERE id=? AND deleted_at IS NULL", source.PageID).Scan(&pageRev); err != nil {
			return err
		}
		if pageRev != source.PageRevision {
			return Fail("CONFLICT", "Meeting changed during summary generation; regenerate from current evidence")
		}
		if err = k.authorizePage(ctx, tx, e, source.PageID); err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, "INSERT INTO meeting_intelligence VALUES(?,?,?,?, 'draft',?,?,?) ON CONFLICT(job_id) DO NOTHING", NewID(), source.MeetingID, source.PageID, source.EvidenceHash, string(marshal(body)), now(), job)
		return err
	})
}
func (k *Kernel) MeetingDrafts(ctx context.Context, e contracts.ServiceExecutionContext, meeting string) ([]MeetingDraft, error) {
	source, err := k.MeetingSource(ctx, e, meeting)
	if err != nil {
		return nil, err
	}
	rows, err := k.db.QueryContext(ctx, "SELECT id,meeting_id,page_id,evidence_hash,state,body_json,created_at FROM meeting_intelligence WHERE meeting_id=? ORDER BY created_at DESC", meeting)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []MeetingDraft{}
	for rows.Next() {
		var d MeetingDraft
		var raw string
		if err = rows.Scan(&d.ID, &d.MeetingID, &d.PageID, &d.EvidenceHash, &d.State, &raw, &d.CreatedAt); err != nil {
			return nil, err
		}
		if err = json.Unmarshal([]byte(raw), &d.Body); err != nil {
			return nil, err
		}
		d.Stale = d.EvidenceHash != source.EvidenceHash
		out = append(out, d)
	}
	return out, rows.Err()
}

// ApplyMeetingDraft only appends approved notes. Actions remain proposals and
// must individually be converted using the reviewed page/task creation flow.
func (k *Kernel) ApplyMeetingDraft(ctx context.Context, e contracts.ServiceExecutionContext, id string) (any, error) {
	if e.Actor.Kind != "user" || e.Origin != "user" {
		return nil, Fail("APPROVAL_REQUIRED", "Meeting notes require explicit user approval")
	}
	var meeting string
	if err := k.db.QueryRowContext(ctx, "SELECT meeting_id FROM meeting_intelligence WHERE id=?", id).Scan(&meeting); err != nil {
		return nil, err
	}
	drafts, err := k.MeetingDrafts(ctx, e, meeting)
	if err != nil {
		return nil, err
	}
	var selected *MeetingDraft
	for i := range drafts {
		if drafts[i].ID == id {
			selected = &drafts[i]
			break
		}
	}
	if selected == nil {
		return nil, Fail("NOT_FOUND", "Summary draft not found")
	}
	d := *selected
	if d.Stale {
		return nil, Fail("STALE_CITATION", "Transcript changed; regenerate the meeting draft before approval")
	}
	if d.State == "applied" {
		return map[string]any{"pageId": d.PageID, "state": "applied"}, nil
	}
	source, err := k.MeetingSource(ctx, e, meeting)
	if err != nil {
		return nil, err
	}
	if source.EvidenceHash != d.EvidenceHash {
		return nil, Fail("STALE_CITATION", "Transcript changed before approval")
	}
	guards := []contracts.ServiceExpectedRevision{required("page", d.PageID, source.PageRevision)}
	commands := []any{}
	sections := []struct {
		name   string
		claims []MeetingClaim
	}{{"Summary", d.Body.Summary}, {"Key points", d.Body.KeyPoints}, {"Decisions", d.Body.Decisions}, {"Open questions", d.Body.Questions}, {"Proposed action items", d.Body.ActionItems}}
	for _, section := range sections {
		if len(section.claims) == 0 {
			continue
		}
		for i, claim := range append([]MeetingClaim{{Text: section.name}}, section.claims...) {
			block := NewID()
			kind := "paragraph"
			if i == 0 {
				kind = "heading_2"
			}
			text := claim.Text
			if i > 0 {
				times := []int64{}
				for _, segment := range source.Segments {
					for _, ref := range claim.SegmentIDs {
						if ref == segment.SegmentID {
							times = append(times, segment.StartMS)
						}
					}
				}
				sort.Slice(times, func(i, j int) bool { return times[i] < times[j] })
				if len(times) > 0 {
					text += fmt.Sprintf(" [%02d:%02d]", times[0]/60000, times[0]/1000%60)
				}
			}
			commands = append(commands, map[string]any{"kind": "block.insert", "block": map[string]any{"blockId": block, "pageId": d.PageID, "type": kind, "schemaVersion": 1, "data": map[string]any{"richText": []any{map[string]any{"text": text}}}}})
			guards = append(guards, required("block", block, "0"))
		}
	}
	// Stable receipt prevents accidental double application if the UI reconnects.
	// IDs are persisted with the same batch payload so a crash cannot replay a
	// changed proposal; applied marker is a commit projection below.
	if len(commands) > 256 {
		return nil, Fail("RESOURCE_EXHAUSTED", "Meeting draft exceeds the reviewed batch limit")
	}
	if err = k.recordingBatch(ctx, e, "meeting-draft-"+id, commands, guards); err != nil {
		return nil, err
	}
	return map[string]any{"pageId": d.PageID, "state": "applied"}, nil
}
func init() {
	RegisterCommitProjection(func(m *Mutation) error {
		key := m.Request.Mutation.IdempotencyKey
		if !strings.HasPrefix(key, "meeting-draft-") {
			return nil
		}
		id := strings.TrimPrefix(key, "meeting-draft-")
		var page, hash, state string
		err := m.Tx.QueryRowContext(m.Context, "SELECT page_id,evidence_hash,state FROM meeting_intelligence WHERE id=?", id).Scan(&page, &hash, &state)
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		if m.Execution.Actor.Kind != "user" || m.Execution.Origin != "user" {
			return Fail("APPROVAL_REQUIRED", "Meeting draft approval requires the user")
		}
		if _, ok := m.touched[guardKey("page", page)]; !ok {
			return Fail("INVALID_REQUEST", "Meeting draft must append to its source page")
		}
		_, err = m.Exec("UPDATE meeting_intelligence SET state='applied' WHERE id=? AND state='draft'", id)
		return err
	})
}

func init() {
	RegisterQuery("meetings.source", func(ctx context.Context, k *Kernel, e contracts.ServiceExecutionContext, r map[string]any) (any, error) {
		return k.MeetingSource(ctx, e, str(r["meetingId"]))
	})
	RegisterQuery("meetings.createFromTranscript", func(ctx context.Context, k *Kernel, e contracts.ServiceExecutionContext, r map[string]any) (any, error) {
		if e.Actor.Kind != "user" || e.Origin != "user" {
			return nil, Fail("APPROVAL_REQUIRED", "Voice-note conversion requires user approval")
		}
		source, err := k.MeetingSource(ctx, e, str(r["meetingId"]))
		if err != nil {
			return nil, err
		}
		if source.EvidenceHash != str(r["evidenceHash"]) {
			return nil, Fail("STALE_CITATION", "Transcript changed; review the conversion again")
		}
		title, body, kind := strings.TrimSpace(str(r["title"])), str(r["body"]), str(r["kind"])
		if title == "" || len(title) > 512 || len(body) > 100000 || (kind != "page" && kind != "task") {
			return nil, Fail("INVALID_REQUEST", "Review a title and text before creating a page or task")
		}
		page, block := NewID(), NewID()
		commands := []any{}
		guards := []contracts.ServiceExpectedRevision{required("page", source.PageID, source.PageRevision), required("page", page, "0"), required("block", block, "0")}
		if kind == "page" {
			commands = append(commands, map[string]any{"kind": "page.create", "pageId": page, "parentPageId": source.PageID, "title": title})
		} else {
			var collection, status string
			err = k.db.QueryRowContext(ctx, "SELECT c.id,p.id FROM collections c JOIN properties p ON p.collection_id=c.id AND p.type='status' WHERE c.system_role='tasks' ORDER BY p.id LIMIT 1").Scan(&collection, &status)
			if errors.Is(err, sql.ErrNoRows) {
				taskPage, titleProperty, due := NewID(), NewID(), NewID()
				collection, status = NewID(), NewID()
				commands = append(commands, map[string]any{"kind": "tasks.ensure", "pageId": taskPage, "collectionId": collection, "titlePropertyId": titleProperty, "statusPropertyId": status, "duePropertyId": due, "defaultOptionId": NewID(), "doneOptionId": NewID()})
				guards = append(guards, required("page", taskPage, "0"), required("collection", collection, "0"), required("property", titleProperty, "0"), required("property", status, "0"), required("property", due, "0"))
			} else if err != nil {
				return nil, err
			} else {
				cg, err := k.currentGuard(ctx, "collection", collection)
				if err != nil {
					return nil, err
				}
				sg, err := k.currentGuard(ctx, "property", status)
				if err != nil {
					return nil, err
				}
				guards = append(guards, cg, sg)
			}
			record := NewID()
			guards = append(guards, required("record", record, "0"))
			commands = append(commands, map[string]any{"kind": "task.create", "recordId": record, "pageId": page, "collectionId": collection, "statusPropertyId": status, "title": title})
		}
		commands = append(commands, map[string]any{"kind": "block.insert", "block": map[string]any{"blockId": block, "pageId": page, "type": "paragraph", "schemaVersion": 1, "data": map[string]any{"richText": []any{map[string]any{"text": body}, map[string]any{"text": "\nSource meeting", "link": "trove://page/" + source.PageID}}}}})
		if err = k.recordingBatch(ctx, e, "voice-note-"+NewID(), commands, guards); err != nil {
			return nil, err
		}
		return map[string]any{"pageId": page, "kind": kind}, nil
	})
}
