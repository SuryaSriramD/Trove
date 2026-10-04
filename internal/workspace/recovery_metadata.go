package workspace

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"trove.local/phase0/contracts"
)

func captureRecoveryMeeting(ctx context.Context, q querier, page string, out *recoverySnapshot) error {
	var meeting contracts.DomainMeeting
	var file sql.NullString
	err := q.QueryRowContext(ctx, "SELECT id,page_id,recording_file_id,started_at,duration_ms,state,revision FROM meetings WHERE page_id=?", page).Scan(&meeting.Id, &meeting.PageId, &file, &meeting.StartedAt, &meeting.DurationMs, &meeting.State, &meeting.Revision)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if file.Valid {
		id := contracts.DomainId(file.String)
		meeting.RecordingFileId = &id
	}
	out.Meeting = &meeting
	rows, err := q.QueryContext(ctx, "SELECT id,meeting_id,block_id,start_ms,end_ms,text,speaker_label,revision FROM transcript_segments WHERE meeting_id=? ORDER BY start_ms,id", meeting.Id)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var s contracts.DomainTranscriptSegment
		var speaker sql.NullString
		if err = rows.Scan(&s.Id, &s.MeetingId, &s.BlockId, &s.StartMs, &s.EndMs, &s.Text, &speaker, &s.Revision); err != nil {
			return err
		}
		if speaker.Valid {
			s.SpeakerLabel = &speaker.String
		}
		out.Segments = append(out.Segments, s)
	}
	return rows.Err()
}

// Property IDs and the historical storage shape determine compatibility. Names
// may change freely; schemas and option identities are never rewound by restore.
func checkHistoricalProperties(ctx context.Context, q querier, snap recoverySnapshot) error {
	if snap.Record == nil {
		return nil
	}
	old := map[contracts.DomainId]contracts.DomainPropertyDefinition{}
	for _, p := range snap.Properties {
		old[p.Id] = p
	}
	for _, value := range snap.Record.Values {
		current, err := propertyDefinition(ctx, q, string(value.PropertyId))
		if err != nil {
			return Fail("CONFLICT", "Historical property no longer exists; its raw value remains in page history")
		}
		if current.CollectionId != snap.Record.CollectionId || current.SchemaVersion != value.SchemaVersion {
			return Fail("CONFLICT", "Historical property schema is incompatible; current shared schemas are preserved")
		}
		if current.Type == "title" {
			continue
		}
		previous, ok := old[value.PropertyId]
		if !ok {
			return Fail("CONFLICT", "This older snapshot has no property schema evidence; its values are retained for manual recovery")
		}
		var a, b any
		json.Unmarshal(previous.Config, &a)
		json.Unmarshal(current.Config, &b)
		if previous.Type != current.Type || string(marshal(a)) != string(marshal(b)) {
			return Fail("CONFLICT", "Property type or configuration changed; review retained historical values before restoring")
		}
		if err = contracts.Validate("domain", "property_"+current.Type+"_v1", value.Value); err != nil {
			return Fail("CONFLICT", "Historical value requires a schema adapter and remains retained")
		}
		if current.Type == "status" || current.Type == "select" || current.Type == "multi_select" {
			var v any
			json.Unmarshal(value.Value, &v)
			values := []any{v}
			if current.Type == "multi_select" {
				values, _ = v.([]any)
			}
			for _, option := range values {
				if option == nil {
					continue
				}
				var n int
				if err = q.QueryRowContext(ctx, "SELECT count(*) FROM property_options WHERE property_id=? AND id=?", current.Id, option).Scan(&n); err != nil {
					return err
				}
				if n != 1 {
					return Fail("CONFLICT", "A historical option was removed; its value remains retained in history")
				}
			}
		}
	}
	return nil
}
func addRecoveryMeetingGuards(ctx context.Context, q querier, page string, snap recoverySnapshot, add func(string, string) error) error {
	var current recoverySnapshot
	if err := captureRecoveryMeeting(ctx, q, page, &current); err != nil {
		return err
	}
	if current.Meeting != nil && (snap.MetadataVersion < 2 || snap.Meeting == nil) {
		return Fail("CONFLICT", "This snapshot predates recoverable meeting metadata; restore a revision that includes the recording")
	}
	if current.Meeting != nil && snap.Meeting != nil && current.Meeting.Id != snap.Meeting.Id {
		return Fail("CONFLICT", "Historical meeting identity differs")
	}
	if snap.Meeting != nil {
		if string(snap.Meeting.PageId) != page {
			return Fail("INTEGRITY_ERROR", "Historical meeting belongs to another page")
		}
		if err := add("meeting", string(snap.Meeting.Id)); err != nil {
			return err
		}
	}
	for _, s := range append(current.Segments, snap.Segments...) {
		if err := add("transcript_segment", string(s.Id)); err != nil {
			return err
		}
	}
	return nil
}
func validateRecoveryReferences(ctx context.Context, q querier, page string, snap recoverySnapshot, add func(string, string) error) error {
	blocks := map[contracts.DomainId]bool{}
	for _, b := range snap.Blocks {
		if string(b.PageId) != page || blocks[b.Id] {
			return Fail("INTEGRITY_ERROR", "Invalid historical block ownership")
		}
		blocks[b.Id] = true
	}
	var walk func(any) error
	walk = func(v any) error {
		switch x := v.(type) {
		case map[string]any:
			for key, val := range x {
				kind := ""
				id := str(val)
				switch key {
				case "targetPageId":
					kind = "page"
				case "taskRecordId":
					kind = "record"
				case "link":
					if strings.HasPrefix(id, "trove://page/") {
						kind = "page"
						var ok bool
						id, _, ok = parseCanonicalPageLink(id)
						if !ok {
							return Fail("INTEGRITY_ERROR", "Historical page/block link is malformed")
						}
					}
				}
				if kind != "" && id != "" {
					var n int
					if err := q.QueryRowContext(ctx, "SELECT count(*) FROM "+entityTables[kind]+" WHERE id=?", id).Scan(&n); err != nil {
						return err
					}
					if n != 1 {
						return Fail("CONFLICT", "Historical source reference is missing; content remains in history")
					}
					if err := add(kind, id); err != nil {
						return err
					}
				}
				if err := walk(val); err != nil {
					return err
				}
			}
		case []any:
			for _, item := range x {
				if err := walk(item); err != nil {
					return err
				}
			}
		}
		return nil
	}
	for _, b := range snap.Blocks {
		if b.ParentBlockId != nil && !blocks[*b.ParentBlockId] {
			return Fail("INTEGRITY_ERROR", "Historical block parent is unavailable")
		}
		var value any
		if err := json.Unmarshal(b.Data, &value); err != nil {
			return err
		}
		if err := walk(value); err != nil {
			return err
		}
	}
	if snap.Task != nil {
		if err := add("property", string(snap.Task.StatusPropertyId)); err != nil {
			return err
		}
		if snap.Task.DuePropertyId != nil {
			if err := add("property", string(*snap.Task.DuePropertyId)); err != nil {
				return err
			}
		}
	}
	seen := map[contracts.DomainId]bool{}
	for _, s := range snap.Segments {
		if snap.Meeting == nil || s.MeetingId != snap.Meeting.Id || !blocks[s.BlockId] || seen[s.Id] || s.StartMs < 0 || s.EndMs < s.StartMs {
			return Fail("INTEGRITY_ERROR", "Historical transcript mapping is invalid")
		}
		if snap.Meeting.State == "ready" && s.EndMs > snap.Meeting.DurationMs {
			return Fail("INTEGRITY_ERROR", "Historical transcript exceeds its recording")
		}
		seen[s.Id] = true
	}
	return nil
}
func prepareRecoverySegments(m *Mutation, page string, snap recoverySnapshot) error {
	var current recoverySnapshot
	if err := captureRecoveryMeeting(m.Context, m.Tx, page, &current); err != nil {
		return err
	}
	wanted := map[contracts.DomainId]bool{}
	for _, s := range snap.Segments {
		wanted[s.Id] = true
		var owner string
		err := m.Tx.QueryRowContext(m.Context, "SELECT m.page_id FROM transcript_segments s JOIN meetings m ON m.id=s.meeting_id WHERE s.id=?", s.Id).Scan(&owner)
		if err == nil && owner != page {
			return Fail("CONFLICT", "Historical transcript identity moved to another page")
		}
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
	}
	for _, s := range current.Segments {
		if !wanted[s.Id] {
			if err := m.Deleted("transcript_segment", string(s.Id)); err != nil {
				return err
			}
		}
	}
	_, err := m.Exec("DELETE FROM transcript_segments WHERE meeting_id IN(SELECT id FROM meetings WHERE page_id=?)", page)
	return err
}
func restoreRecoveryMeeting(m *Mutation, page string, snap recoverySnapshot) error {
	if snap.Meeting == nil {
		return nil
	}
	meeting := snap.Meeting
	if err := m.Touch("meeting", string(meeting.Id)); err != nil {
		return err
	}
	if _, err := m.Exec("INSERT INTO meetings(id,page_id,recording_file_id,started_at,duration_ms,state,revision) VALUES(?,?,?,?,?,?,1) ON CONFLICT(id) DO UPDATE SET recording_file_id=excluded.recording_file_id,started_at=excluded.started_at,duration_ms=excluded.duration_ms,state=excluded.state", meeting.Id, page, meeting.RecordingFileId, meeting.StartedAt, meeting.DurationMs, meeting.State); err != nil {
		return err
	}
	for _, s := range snap.Segments {
		if err := m.Touch("transcript_segment", string(s.Id)); err != nil {
			return err
		}
		// Transcript text is a projection of its block and carries that block's
		// revision. Keep the emitted change equal to the post-commit row.
		key := guardKey("transcript_segment", string(s.Id))
		change := m.touched[key]
		change.After = m.touched[guardKey("block", string(s.BlockId))].After
		m.touched[key] = change
		if _, err := m.Exec("DELETE FROM entity_tombstones WHERE entity_type='transcript_segment' AND entity_id=?", s.Id); err != nil {
			return err
		}
		if _, err := m.Exec("INSERT INTO transcript_segments VALUES(?,?,?,?,?,?,?,1)", s.Id, s.MeetingId, s.BlockId, s.StartMs, s.EndMs, s.Text, s.SpeakerLabel); err != nil {
			return err
		}
	}
	return nil
}

func recoveryPreviewWarnings(snap recoverySnapshot) []string {
	warnings := []string{}
	if snap.Record != nil {
		for _, value := range snap.Record.Values {
			for _, p := range snap.Properties {
				if p.Id == value.PropertyId && p.Type != "title" {
					warnings = append(warnings, fmt.Sprintf("Restore %s (%s): %s", p.Name, p.Type, string(value.Value)))
				}
			}
		}
		warnings = append(warnings, "This page’s property values and captured parent/dependency structure are restored. Reciprocal relation changes update both guarded endpoints; shared property definitions remain current.")
		if snap.RecordStructure == nil {
			warnings = append(warnings, "This older snapshot has no captured record hierarchy; restoring with active parent/dependencies requires a newer snapshot.")
		}
	}
	if snap.Meeting != nil {
		warnings = append(warnings, fmt.Sprintf("Restore %d timed transcript segments and recording metadata.", len(snap.Segments)))
	}
	warnings = append(warnings, "Memory approval and forgotten states are preserved. Restored evidence receives a new revision.")
	return warnings
}

func captureOwnedRecord(ctx context.Context, q querier, page string) (contracts.DomainRecord, error) {
	var r contracts.DomainRecord
	var title contracts.DomainId
	err := q.QueryRowContext(ctx, "SELECT r.id,r.collection_id,r.page_id,r.revision,r.sort_key,c.title_property_id FROM records r JOIN collections c ON c.id=r.collection_id WHERE r.page_id=?", page).Scan(&r.Id, &r.CollectionId, &r.PageId, &r.Revision, &r.SortKey, &title)
	if err != nil {
		return r, err
	}
	r.Values = []contracts.DomainPropertyValue{{PropertyId: title, SchemaVersion: 1, Value: marshal(map[string]any{"pageId": r.PageId})}}
	rows, err := q.QueryContext(ctx, "SELECT property_id,schema_version,value_json FROM property_values WHERE record_id=? ORDER BY property_id", r.Id)
	if err != nil {
		return r, err
	}
	defer rows.Close()
	for rows.Next() {
		var value contracts.DomainPropertyValue
		var raw string
		if err = rows.Scan(&value.PropertyId, &value.SchemaVersion, &raw); err != nil {
			return r, err
		}
		value.Value = json.RawMessage(raw)
		r.Values = append(r.Values, value)
	}
	return r, rows.Err()
}
