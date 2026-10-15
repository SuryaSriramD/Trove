package workspace

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"trove.local/phase0/contracts"
)

// An extraction item identifies a reviewed proposal, never the wording of a task.
// The effect receipt is committed with canonical content so a lost response can
// be recovered without replaying the write or recreating a deleted task.
type MeetingExtraction struct {
	Version      int    `json:"version"`
	ProposalID   string `json:"proposalId"`
	ItemID       string `json:"itemId"`
	MeetingID    string `json:"meetingId"`
	EvidenceHash string `json:"evidenceHash"`
	Kind         string `json:"kind"`
	Title        string `json:"title"`
	Body         string `json:"body"`
	PageID       string `json:"pageId"`
	RecordID     string `json:"recordId,omitempty"`
	BlockID      string `json:"-"`
	AppliedHash  string `json:"-"`
	AppliedAt    string `json:"appliedAt,omitempty"`
	Stale        bool   `json:"stale"`
}

const extractionColumns = "proposal_id,item_id,meeting_id,evidence_hash,kind,title,body,page_id,record_id,block_id,applied_hash,applied_at"

type extractionScanner interface{ Scan(...any) error }

func scanExtraction(row extractionScanner) (MeetingExtraction, error) {
	x := MeetingExtraction{Version: 1}
	err := row.Scan(&x.ProposalID, &x.ItemID, &x.MeetingID, &x.EvidenceHash, &x.Kind, &x.Title, &x.Body, &x.PageID, &x.RecordID, &x.BlockID, &x.AppliedHash, &x.AppliedAt)
	return x, err
}
func init() {
	RegisterInitializer(func(ctx context.Context, k *Kernel) error {
		return k.write(ctx, func(tx *sql.Tx) error {
			_, err := tx.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS meeting_extraction_items(
   version INTEGER NOT NULL DEFAULT 1 CHECK(version=1), proposal_id TEXT NOT NULL,item_id TEXT NOT NULL,
   meeting_id TEXT NOT NULL REFERENCES meetings(id),evidence_hash TEXT NOT NULL,kind TEXT NOT NULL CHECK(kind IN('page','task')),
   title TEXT NOT NULL,body TEXT NOT NULL,page_id TEXT NOT NULL,record_id TEXT NOT NULL,block_id TEXT NOT NULL,
   applied_hash TEXT NOT NULL DEFAULT '',applied_at TEXT NOT NULL DEFAULT '',created_at TEXT NOT NULL,
   PRIMARY KEY(proposal_id,item_id),UNIQUE(page_id),UNIQUE(block_id));`)
			return err
		})
	})
	RegisterQuery("meetings.source", func(ctx context.Context, k *Kernel, e contracts.ServiceExecutionContext, r map[string]any) (any, error) {
		return k.MeetingSource(ctx, e, str(r["meetingId"]))
	})
	RegisterQuery("meetings.prepareExtraction", func(ctx context.Context, k *Kernel, e contracts.ServiceExecutionContext, r map[string]any) (any, error) {
		return k.prepareMeetingExtraction(ctx, e, r)
	})
	RegisterQuery("meetings.extractions", func(ctx context.Context, k *Kernel, e contracts.ServiceExecutionContext, r map[string]any) (any, error) {
		source, err := k.MeetingSource(ctx, e, str(r["meetingId"]))
		if err != nil {
			return nil, err
		}
		rows, err := k.db.QueryContext(ctx, "SELECT "+extractionColumns+" FROM meeting_extraction_items WHERE meeting_id=? AND (applied_hash<>'' OR proposal_id NOT IN(SELECT draft_id FROM meeting_archived_proposals_v1)) ORDER BY created_at,proposal_id,item_id", source.MeetingID)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		out := []MeetingExtraction{}
		for rows.Next() {
			x, err := scanExtraction(rows)
			if err != nil {
				return nil, err
			}
			x.Stale = x.EvidenceHash != source.EvidenceHash
			out = append(out, x)
		}
		return out, rows.Err()
	})
	RegisterQuery("meetings.createFromTranscript", func(ctx context.Context, k *Kernel, e contracts.ServiceExecutionContext, r map[string]any) (any, error) {
		return k.createMeetingExtraction(ctx, e, r)
	})
	RegisterCommand("meeting.extraction.record", recordMeetingExtraction)
}
func (k *Kernel) prepareMeetingExtraction(ctx context.Context, e contracts.ServiceExecutionContext, r map[string]any) (MeetingExtraction, error) {
	var out MeetingExtraction
	if e.Actor.Kind != "user" || e.Origin != "user" {
		return out, Fail("APPROVAL_REQUIRED", "Reviewing an extraction requires the user")
	}
	source, err := k.MeetingSource(ctx, e, str(r["meetingId"]))
	if err != nil {
		return out, err
	}
	kind := str(r["kind"])
	if kind != "task" && kind != "page" {
		return out, Fail("INVALID_REQUEST", "Choose a task or page")
	}
	proposal, item := str(r["requestId"]), "transcript"
	var title string
	if err = k.db.QueryRowContext(ctx, "SELECT title FROM pages WHERE id=?", source.PageID).Scan(&title); err != nil {
		return out, err
	}
	title = "Notes from " + title
	parts := []string{}
	for _, segment := range source.Segments {
		parts = append(parts, segment.Text)
	}
	body := strings.Join(parts, "\n\n")
	if draft := str(r["draftId"]); draft != "" {
		var meeting, hash, raw string
		if err = k.db.QueryRowContext(ctx, "SELECT meeting_id,evidence_hash,body_json FROM meeting_intelligence WHERE id=? AND id NOT IN(SELECT draft_id FROM meeting_archived_proposals_v1)", draft).Scan(&meeting, &hash, &raw); err != nil {
			return out, err
		}
		if meeting != source.MeetingID {
			return out, Fail("SCOPE_DENIED", "Action item belongs to a different meeting")
		}
		if hash != source.EvidenceHash {
			return out, Fail("STALE_CITATION", "Transcript changed; regenerate this action item")
		}
		var generated MeetingIntelligence
		if err = json.Unmarshal([]byte(raw), &generated); err != nil {
			return out, err
		}
		index := int(number(r["itemIndex"]))
		if index < 0 || index >= len(generated.ActionItems) {
			return out, Fail("INVALID_REQUEST", "Action item is unavailable")
		}
		proposal, item = draft, fmt.Sprintf("action:%d", index)
		title = generated.ActionItems[index].Text
		body = title
	}
	if len(title) > 512 {
		title = string([]rune(title)[:min(len([]rune(title)), 128)])
	}
	if len(body) > 100000 {
		return out, Fail("RESOURCE_EXHAUSTED", "Select a smaller transcript before conversion")
	}
	out = MeetingExtraction{Version: 1, ProposalID: proposal, ItemID: item, MeetingID: source.MeetingID, EvidenceHash: source.EvidenceHash, Kind: kind, Title: title, Body: body, PageID: NewID(), BlockID: NewID()}
	if kind == "task" {
		out.RecordID = NewID()
	}
	err = k.write(ctx, func(tx *sql.Tx) error {
		var archived int
		if err := tx.QueryRowContext(ctx, "SELECT count(*) FROM meeting_archived_proposals_v1 WHERE draft_id=?", proposal).Scan(&archived); err != nil {
			return err
		}
		if archived != 0 {
			return Fail("CONFLICT", "Generated proposal was archived during review")
		}
		if err := k.authorizePage(ctx, tx, e, source.PageID); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, "INSERT INTO meeting_extraction_items(proposal_id,item_id,meeting_id,evidence_hash,kind,title,body,page_id,record_id,block_id,created_at) VALUES(?,?,?,?,?,?,?,?,?,?,?) ON CONFLICT(proposal_id,item_id) DO NOTHING", out.ProposalID, out.ItemID, out.MeetingID, out.EvidenceHash, out.Kind, out.Title, out.Body, out.PageID, out.RecordID, out.BlockID, now())
		if err != nil {
			return err
		}
		out, err = scanExtraction(tx.QueryRowContext(ctx, "SELECT "+extractionColumns+" FROM meeting_extraction_items WHERE proposal_id=? AND item_id=?", proposal, item))
		return err
	})
	if err == nil && (out.MeetingID != source.MeetingID || out.Kind != kind) {
		return out, Fail("IDEMPOTENCY_CONFLICT", "Extraction identity already belongs to another operation")
	}
	out.Stale = out.EvidenceHash != source.EvidenceHash
	return out, err
}
func extractionHash(title, body string) string {
	hash := sha256.Sum256(marshal([]string{title, body}))
	return hex.EncodeToString(hash[:])
}
func (k *Kernel) createMeetingExtraction(ctx context.Context, e contracts.ServiceExecutionContext, r map[string]any) (any, error) {
	if e.Actor.Kind != "user" || e.Origin != "user" {
		return nil, Fail("APPROVAL_REQUIRED", "Voice-note conversion requires user approval")
	}
	source, err := k.MeetingSource(ctx, e, str(r["meetingId"]))
	if err != nil {
		return nil, err
	}
	title, body, kind := strings.TrimSpace(str(r["title"])), str(r["body"]), str(r["kind"])
	if title == "" || len(title) > 512 || len(body) > 100000 || (kind != "page" && kind != "task") {
		return nil, Fail("INVALID_REQUEST", "Review a title and text before creating a page or task")
	}
	var x MeetingExtraction
	if str(r["proposalId"]) == "" {
		// Older clients retain request identity. New clients persist proposal/item IDs
		// at review time so a restart or new transport request reuses the same effect.
		if source.EvidenceHash != str(r["evidenceHash"]) {
			return nil, Fail("STALE_CITATION", "Transcript changed; review the conversion again")
		}
		x, err = k.prepareMeetingExtraction(ctx, e, r)
	} else {
		x, err = scanExtraction(k.db.QueryRowContext(ctx, "SELECT "+extractionColumns+" FROM meeting_extraction_items WHERE proposal_id=? AND item_id=?", r["proposalId"], r["itemId"]))
	}
	if err != nil {
		return nil, err
	}
	if x.MeetingID != source.MeetingID || x.Kind != kind || x.EvidenceHash != str(r["evidenceHash"]) {
		return nil, Fail("IDEMPOTENCY_CONFLICT", "Extraction identity does not match the reviewed source")
	}
	result := map[string]any{"pageId": x.PageID, "recordId": x.RecordID, "kind": kind, "proposalId": x.ProposalID, "itemId": x.ItemID}
	if x.AppliedHash != "" {
		if x.AppliedHash != extractionHash(title, body) {
			return nil, Fail("IDEMPOTENCY_CONFLICT", "This extraction was already applied with different reviewed content")
		}
		if err = k.authorizePage(ctx, k.db, e, x.PageID); err != nil {
			return nil, err
		}
		result["reused"] = true
		return result, nil
	}
	var archived int
	if err = k.db.QueryRowContext(ctx, "SELECT count(*) FROM meeting_archived_proposals_v1 WHERE draft_id=?", x.ProposalID).Scan(&archived); err != nil {
		return nil, err
	}
	if archived != 0 {
		return nil, Fail("CONFLICT", "Generated proposal is archived")
	}
	if source.EvidenceHash != x.EvidenceHash {
		return nil, Fail("STALE_CITATION", "Transcript changed; review a new extraction")
	}
	page, block := x.PageID, x.BlockID
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
			for _, target := range [][2]string{{"collection", collection}, {"property", status}} {
				g, err := k.currentGuard(ctx, target[0], target[1])
				if err != nil {
					return nil, err
				}
				guards = append(guards, g)
			}
		}
		guards = append(guards, required("record", x.RecordID, "0"))
		commands = append(commands, map[string]any{"kind": "task.create", "recordId": x.RecordID, "pageId": page, "collectionId": collection, "statusPropertyId": status, "title": title})
	}
	commands = append(commands, map[string]any{"kind": "block.insert", "block": map[string]any{"blockId": block, "pageId": page, "type": "paragraph", "schemaVersion": 1, "data": map[string]any{"richText": []any{map[string]any{"text": body}, map[string]any{"text": "\nSource meeting", "link": "trove://page/" + source.PageID}}}}}, map[string]any{"kind": "meeting.extraction.record", "proposalId": x.ProposalID, "itemId": x.ItemID, "title": title, "body": body})
	if err = k.recordingBatch(ctx, e, "meeting-extraction-"+NewID(), commands, guards); err != nil {
		// Another retry may have won the writer transaction while this request built
		// its guards. Reconcile the committed receipt before reporting the conflict.
		prior, readErr := scanExtraction(k.db.QueryRowContext(ctx, "SELECT "+extractionColumns+" FROM meeting_extraction_items WHERE proposal_id=? AND item_id=?", x.ProposalID, x.ItemID))
		if readErr == nil && prior.AppliedHash == extractionHash(title, body) {
			if authErr := k.authorizePage(ctx, k.db, e, prior.PageID); authErr != nil {
				return nil, authErr
			}
			result["reused"] = true
			return result, nil
		}
		return nil, err
	}
	result["reused"] = false
	return result, nil
}
func recordMeetingExtraction(m *Mutation, c map[string]any) error {
	if m.Execution.Actor.Kind != "user" || m.Execution.Origin != "user" {
		return Fail("APPROVAL_REQUIRED", "Extraction approval requires the user")
	}
	x, err := scanExtraction(m.Tx.QueryRowContext(m.Context, "SELECT "+extractionColumns+" FROM meeting_extraction_items WHERE proposal_id=? AND item_id=?", c["proposalId"], c["itemId"]))
	if err != nil {
		return err
	}
	if x.AppliedHash != "" {
		return Fail("ALREADY_COMMITTED", "Extraction was already applied")
	}
	var archived int
	if err = m.Tx.QueryRowContext(m.Context, "SELECT count(*) FROM meeting_archived_proposals_v1 WHERE draft_id=?", x.ProposalID).Scan(&archived); err != nil {
		return err
	}
	if archived != 0 {
		return Fail("CONFLICT", "Generated proposal was archived during review")
	}
	var sourcePage, title string
	if err = m.Tx.QueryRowContext(m.Context, "SELECT page_id FROM meetings WHERE id=?", x.MeetingID).Scan(&sourcePage); err != nil {
		return err
	}
	if err = m.Require("page", sourcePage); err != nil {
		return err
	}
	segments, err := readMeetingEvidence(m.Context, m.Tx, x.MeetingID)
	if err != nil {
		return err
	}
	hash := sha256.Sum256(marshal(segments))
	if hex.EncodeToString(hash[:]) != x.EvidenceHash {
		return Fail("STALE_CITATION", "Transcript changed; review a new extraction")
	}
	if _, ok := m.touched[guardKey("page", x.PageID)]; !ok || m.guards[guardKey("page", x.PageID)] != "0" {
		return Fail("INVALID_REQUEST", "Extraction receipt requires its newly created target")
	}
	if err = m.Tx.QueryRowContext(m.Context, "SELECT title FROM pages WHERE id=?", x.PageID).Scan(&title); err != nil {
		return err
	}
	var raw string
	if err = m.Tx.QueryRowContext(m.Context, "SELECT data_json FROM blocks WHERE id=? AND page_id=?", x.BlockID, x.PageID).Scan(&raw); err != nil {
		return err
	}
	var data struct {
		RichText []struct {
			Text string `json:"text"`
			Link string `json:"link"`
		} `json:"richText"`
	}
	if err = json.Unmarshal([]byte(raw), &data); err != nil {
		return err
	}
	if title != str(c["title"]) || len(data.RichText) != 2 || data.RichText[0].Text != str(c["body"]) || data.RichText[1].Link != "trove://page/"+sourcePage {
		return Fail("INVALID_REQUEST", "Extraction receipt must match the approved canonical content")
	}
	if x.Kind == "task" {
		var page string
		if err = m.Tx.QueryRowContext(m.Context, "SELECT r.page_id FROM task_facets f JOIN records r ON r.id=f.record_id WHERE f.record_id=?", x.RecordID).Scan(&page); err != nil {
			return err
		}
		if page != x.PageID {
			return Fail("INVALID_REQUEST", "Extraction task identity changed")
		}
	}
	if restricted, err := pageExternalContextRestricted(m.Context, m.Tx, sourcePage); err != nil {
		return err
	} else if restricted {
		if err := retainPageExternalContextPolicy(m.Context, m.Tx, x.PageID, gmailExternalContextPolicy); err != nil {
			return err
		}
	}
	_, err = m.Exec("UPDATE meeting_extraction_items SET title=?,body=?,applied_hash=?,applied_at=? WHERE proposal_id=? AND item_id=?", title, str(c["body"]), extractionHash(title, str(c["body"])), now(), x.ProposalID, x.ItemID)
	return err
}
