package workspace

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"

	"trove.local/phase0/contracts"
)

func init() {
	RegisterInitializer(func(ctx context.Context, k *Kernel) error {
		return k.write(ctx, func(tx *sql.Tx) error {
			_, err := tx.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS meeting_retention_v1(meeting_id TEXT PRIMARY KEY REFERENCES meetings(id),exclude_search INTEGER NOT NULL DEFAULT 0 CHECK(exclude_search IN(0,1)));
CREATE TABLE IF NOT EXISTS meeting_archived_proposals_v1(draft_id TEXT PRIMARY KEY REFERENCES meeting_intelligence(id),archived_at TEXT NOT NULL);`)
			return err
		})
	})
	RegisterCommand("meeting.correctSegment", correctMeetingSegment)
	RegisterCommand("meeting.retention.apply", applyMeetingRetention)
	RegisterQuery("meetings.retentionPreview", previewMeetingRetention)
}

// Corrections update the existing canonical block and segment, so page history
// preserves prior text/timing and all citation, extraction and memory revisions
// follow the same writer transaction as an ordinary user edit.
func correctMeetingSegment(m *Mutation, c map[string]any) error {
	if m.Execution.Actor.Kind != "user" || m.Execution.Origin != "user" {
		return Fail("APPROVAL_REQUIRED", "Transcript correction requires the user")
	}
	meeting, segment := str(c["meetingId"]), str(c["segmentId"])
	var page, block string
	var duration int64
	if err := m.Tx.QueryRowContext(m.Context, "SELECT m.page_id,s.block_id,m.duration_ms FROM transcript_segments s JOIN meetings m ON m.id=s.meeting_id WHERE s.id=? AND s.meeting_id=?", segment, meeting).Scan(&page, &block, &duration); err != nil {
		return err
	}
	start, end := number(c["startMs"]), number(c["endMs"])
	if start < 0 || end < start || end > duration || strings.TrimSpace(str(c["text"])) == "" {
		return Fail("INVALID_REQUEST", "Correction needs text and a valid range inside the captured audio")
	}
	for _, entity := range [][2]string{{"page", page}, {"meeting", meeting}, {"block", block}, {"transcript_segment", segment}} {
		if err := m.Touch(entity[0], entity[1]); err != nil {
			return err
		}
	}
	if err := pageCommand(m, map[string]any{"kind": "block.update", "blockId": block, "type": "paragraph", "schemaVersion": float64(1), "data": map[string]any{"richText": []any{map[string]any{"text": str(c["text"])}}}}); err != nil {
		return err
	}
	_, err := m.Exec("UPDATE transcript_segments SET start_ms=?,end_ms=?,text=? WHERE id=?", start, end, str(c["text"]), segment)
	return err
}

type retentionMemory struct{ ID, Page, Revision, PageRevision, Status string }
type retentionState struct {
	Meeting, Page, Revision, PageRevision, Audio string
	RecordID, RecordRevision                     string
	CollectionID, CollectionRevision             string
	OtherAudioReferences                         int
	Segments                                     []MeetingEvidence
	Proposals                                    []string
	Memories                                     []retentionMemory
	ExcludeSearch                                bool
	ActiveJobs, TranscriptChildren               int
}
type MeetingRetentionCategory struct {
	ID         string `json:"id"`
	Count      int    `json:"count"`
	Action     string `json:"action"`
	Detail     string `json:"detail"`
	Selectable bool   `json:"selectable"`
}
type MeetingRetentionPreview struct {
	Version           int                                 `json:"version"`
	MeetingID         string                              `json:"meetingId"`
	PageID            string                              `json:"pageId"`
	Categories        []MeetingRetentionCategory          `json:"categories"`
	Selected          []string                            `json:"selected"`
	ExpectedRevisions []contracts.ServiceExpectedRevision `json:"expectedRevisions"`
	Command           map[string]any                      `json:"command"`
	Warnings          []string                            `json:"warnings"`
	CanApply          bool                                `json:"canApply"`
}

func readMeetingRetention(ctx context.Context, q querier, meeting string) (retentionState, error) {
	s := retentionState{Meeting: meeting, Segments: []MeetingEvidence{}, Proposals: []string{}, Memories: []retentionMemory{}}
	err := q.QueryRowContext(ctx, "SELECT m.page_id,m.revision,p.revision,COALESCE(m.recording_file_id,''),COALESCE(r.exclude_search,0) FROM meetings m JOIN pages p ON p.id=m.page_id LEFT JOIN meeting_retention_v1 r ON r.meeting_id=m.id WHERE m.id=? AND p.deleted_at IS NULL", meeting).Scan(&s.Page, &s.Revision, &s.PageRevision, &s.Audio, &s.ExcludeSearch)
	if err != nil {
		return s, err
	}
	err = q.QueryRowContext(ctx, "SELECT r.id,r.revision,c.id,c.revision FROM records r JOIN collections c ON c.id=r.collection_id WHERE r.page_id=?", s.Page).Scan(&s.RecordID, &s.RecordRevision, &s.CollectionID, &s.CollectionRevision)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return s, err
	}
	if s.Audio != "" {
		if err = q.QueryRowContext(ctx, "SELECT count(*) FROM file_references WHERE file_id=? AND NOT(page_id=? AND source_kind IN('explicit','explicit_user','meeting'))", s.Audio, s.Page).Scan(&s.OtherAudioReferences); err != nil {
			return s, err
		}
	}
	if s.Segments, err = readMeetingEvidence(ctx, q, meeting); err != nil {
		return s, err
	}
	rows, err := q.QueryContext(ctx, "SELECT d.id FROM meeting_intelligence d LEFT JOIN meeting_archived_proposals_v1 a ON a.draft_id=d.id WHERE d.meeting_id=? AND d.state='draft' AND a.draft_id IS NULL ORDER BY d.id", meeting)
	if err != nil {
		return s, err
	}
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return s, err
		}
		s.Proposals = append(s.Proposals, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return s, err
	}
	rows, err = q.QueryContext(ctx, "SELECT DISTINCT m.id,m.page_id,m.revision,p.revision,m.status FROM memory_entries m JOIN memory_sources src ON src.memory_id=m.id JOIN pages p ON p.id=m.page_id WHERE (src.meeting_id=? OR src.page_id=?) AND m.status<>'forgotten' ORDER BY m.id", meeting, s.Page)
	if err != nil {
		return s, err
	}
	for rows.Next() {
		var item retentionMemory
		if err = rows.Scan(&item.ID, &item.Page, &item.Revision, &item.PageRevision, &item.Status); err != nil {
			rows.Close()
			return s, err
		}
		s.Memories = append(s.Memories, item)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return s, err
	}
	if err = q.QueryRowContext(ctx, "SELECT count(*) FROM jobs WHERE json_extract(payload_json,'$.meetingId')=? AND state IN('queued','running','retry_wait','cancel_requested')", meeting).Scan(&s.ActiveJobs); err != nil {
		return s, err
	}
	err = q.QueryRowContext(ctx, "SELECT count(*) FROM blocks b JOIN transcript_segments s ON s.block_id=b.parent_block_id WHERE s.meeting_id=?", meeting).Scan(&s.TranscriptChildren)
	return s, err
}
func retentionSelection(value any) ([]string, error) {
	selected := []string{}
	seen := map[string]bool{}
	items, ok := value.([]any)
	if !ok {
		return nil, Fail("INVALID_REQUEST", "Retention choices must be a list")
	}
	for _, v := range items {
		id := str(v)
		if seen[id] || (id != "audio" && id != "transcripts" && id != "generated" && id != "indexes" && id != "memory") {
			return nil, Fail("INVALID_REQUEST", "Unsupported or repeated retention choice")
		}
		seen[id] = true
		selected = append(selected, id)
	}
	sort.Strings(selected)
	return selected, nil
}
func retentionHash(s retentionState, selected []string) string {
	sum := sha256.Sum256(marshal([]any{1, s, selected}))
	return hex.EncodeToString(sum[:])
}
func retentionAvailable(s retentionState, selected []string) bool {
	if len(selected) == 0 || s.ActiveJobs > 0 || len(retentionGuards(s, selected)) > 1000 {
		return false
	}
	for _, choice := range selected {
		if (choice == "audio" && s.Audio == "") || (choice == "transcripts" && (len(s.Segments) == 0 || s.TranscriptChildren > 0)) || (choice == "generated" && len(s.Proposals) == 0) || (choice == "memory" && len(s.Memories) == 0) {
			return false
		}
	}
	return true
}
func retentionGuards(s retentionState, selected []string) []contracts.ServiceExpectedRevision {
	guards := []contracts.ServiceExpectedRevision{required("page", s.Page, s.PageRevision), required("meeting", s.Meeting, s.Revision)}
	if s.RecordID != "" {
		guards = append(guards, required("record", s.RecordID, s.RecordRevision), required("collection", s.CollectionID, s.CollectionRevision))
	}
	seen := map[string]bool{"page:" + s.Page: true}
	for _, choice := range selected {
		switch choice {
		case "transcripts":
			for _, segment := range s.Segments {
				guards = append(guards, required("block", segment.BlockID, segment.Revision))
			}
		case "memory":
			for _, memory := range s.Memories {
				guards = append(guards, required("memory", memory.ID, memory.Revision))
				if !seen["page:"+memory.Page] {
					seen["page:"+memory.Page] = true
					guards = append(guards, required("page", memory.Page, memory.PageRevision))
				}
			}
		}
	}
	unique := []contracts.ServiceExpectedRevision{}
	identities := map[string]bool{}
	for _, guard := range guards {
		key := guardKey(guard.Target.EntityType, string(guard.Target.EntityId))
		if !identities[key] {
			unique = append(unique, guard)
			identities[key] = true
		}
	}
	return unique
}
func previewMeetingRetention(ctx context.Context, k *Kernel, e contracts.ServiceExecutionContext, r map[string]any) (any, error) {
	if err := requireOwnerScope(k, e); err != nil {
		return nil, err
	}
	selected, err := retentionSelection(r["selected"])
	if err != nil {
		return nil, err
	}
	tx, err := k.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	s, err := readMeetingRetention(ctx, tx, str(r["meetingId"]))
	if err != nil {
		return nil, err
	}
	if err = k.authorizePage(ctx, tx, e, s.Page); err != nil {
		return nil, err
	}
	var history, backups, indexes, approved int
	for _, query := range []struct {
		sql  string
		args []any
		out  *int
	}{
		{"SELECT count(*) FROM page_snapshots WHERE page_id=?", []any{s.Page}, &history},
		{"SELECT count(*) FROM backups WHERE state='complete'", nil, &backups},
		{"SELECT count(*) FROM search_chunks WHERE page_id=?", []any{s.Page}, &indexes},
		{"SELECT count(*) FROM meeting_extraction_items WHERE meeting_id=? AND applied_at<>''", []any{s.Meeting}, &approved},
	} {
		if err = tx.QueryRowContext(ctx, query.sql, query.args...).Scan(query.out); err != nil {
			return nil, err
		}
	}
	audioCount := 0
	if s.Audio != "" {
		audioCount = 1
	}
	indexAction := "Exclude this meeting page from search"
	if s.ExcludeSearch {
		indexAction = "Include this meeting page in search again"
	}
	out := MeetingRetentionPreview{Version: 1, MeetingID: s.Meeting, PageID: s.Page, Selected: selected, ExpectedRevisions: retentionGuards(s, selected), Warnings: []string{fmt.Sprintf("%d approved extracted pages/tasks and subsequent edits remain unchanged.", approved), "This changes current availability. Retained history, managed assets and separate backup copies can still contain the original material. Automatic history pruning remains off."}, Categories: []MeetingRetentionCategory{
		{"audio", audioCount, "Detach current recording", fmt.Sprintf("Removes this meeting's playback reference and explicit attachment. %d other current references, historical evidence and backup copies retain audio bytes.", s.OtherAudioReferences), audioCount > 0},
		{"transcripts", len(s.Segments), "Remove current transcript", "Removes transcript blocks and segment references; prior versions stay in page history. Related evidence becomes stale. Move any nested user content first.", len(s.Segments) > 0 && s.TranscriptChildren == 0},
		{"generated", len(s.Proposals), "Archive unapproved generated proposals", "Archived proposals remain stored for evidence. Approved notes, extracted pages and tasks are retained and can be edited separately.", len(s.Proposals) > 0},
		{"indexes", indexes, indexAction, "A retained preference controls this meeting page's derived search chunks and embeddings. Sources and approved memory have separate controls.", true},
		{"memory", len(s.Memories), "Forget source-linked memories", "Excludes these memories from active retrieval; their records and history remain retained for recovery.", len(s.Memories) > 0},
		{"history", history, "Keep history", "All page revisions remain retained. No automatic pruning policy is enabled; this operation cannot remove recovery evidence.", false},
		{"backups", backups, "Review backups separately", "Count is tracked completed workspace backups, which may contain this meeting; external copies are not scanned or deleted. Use Settings → Recovery to review native backup cleanup.", false},
	}}
	out.CanApply = retentionAvailable(s, selected)
	if s.ActiveJobs > 0 {
		out.Warnings = append(out.Warnings, "Cancel or finish meeting processing before applying retention choices.")
	}
	if len(out.ExpectedRevisions) > 1000 {
		out.Warnings = append(out.Warnings, "This selection exceeds one guarded batch; reduce selected categories.")
		out.ExpectedRevisions = []contracts.ServiceExpectedRevision{}
	}
	out.Command = map[string]any{"kind": "meeting.retention.apply", "meetingId": s.Meeting, "selected": selected, "previewHash": retentionHash(s, selected)}
	return out, nil
}
func applyMeetingRetention(m *Mutation, c map[string]any) error {
	if err := requireOwnerScope(m.Kernel, m.Execution); err != nil {
		return err
	}
	selected, err := retentionSelection(c["selected"])
	if err != nil {
		return err
	}
	if len(selected) == 0 {
		return Fail("INVALID_REQUEST", "Choose at least one retention action")
	}
	s, err := readMeetingRetention(m.Context, m.Tx, str(c["meetingId"]))
	if err != nil {
		return err
	}
	if !retentionAvailable(s, selected) || retentionHash(s, selected) != str(c["previewHash"]) {
		return Fail("CONFLICT", "Meeting retention preview is stale; review current effects")
	}
	if err = m.TouchPage(s.Page); err != nil {
		return err
	}
	if err = m.Touch("meeting", s.Meeting); err != nil {
		return err
	}
	for _, choice := range selected {
		switch choice {
		case "audio":
			if _, err = m.Exec("UPDATE meetings SET recording_file_id=NULL WHERE id=?", s.Meeting); err != nil {
				return err
			}
			if _, err = m.Exec("DELETE FROM file_references WHERE page_id=? AND file_id=? AND source_kind IN('explicit','explicit_user')", s.Page, s.Audio); err != nil {
				return err
			}
		case "transcripts":
			if s.TranscriptChildren > 0 {
				return Fail("CONFLICT", "Transcript contains nested user content; move it before cleanup")
			}
			for _, segment := range s.Segments {
				if err = pageCommand(m, map[string]any{"kind": "block.delete", "blockId": segment.BlockID, "includeDescendants": false}); err != nil {
					return err
				}
			}
		case "generated":
			for _, id := range s.Proposals {
				if _, err = m.Exec("INSERT INTO meeting_archived_proposals_v1 VALUES(?,?) ON CONFLICT DO NOTHING", id, now()); err != nil {
					return err
				}
			}
		case "indexes":
			if _, err = m.Exec("INSERT INTO meeting_retention_v1(meeting_id,exclude_search) VALUES(?,?) ON CONFLICT(meeting_id) DO UPDATE SET exclude_search=excluded.exclude_search", s.Meeting, !s.ExcludeSearch); err != nil {
				return err
			}
			if !s.ExcludeSearch {
				for _, query := range []string{"DELETE FROM search_embeddings WHERE chunk_id IN(SELECT id FROM search_chunks WHERE page_id=?)", "DELETE FROM search_chunks WHERE page_id=?", "DELETE FROM semantic_auto_attempts WHERE page_id=?"} {
					if _, err = m.Exec(query, s.Page); err != nil {
						return err
					}
				}
			}
		case "memory":
			for _, memory := range s.Memories {
				if err = memoryCommand(m, map[string]any{"kind": "memory.forget", "memoryId": memory.ID}); err != nil {
					return err
				}
			}
		}
	}
	return nil
}
