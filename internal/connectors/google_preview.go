package connectors

import (
	"context"
	"encoding/json"
	"strings"
	"unicode/utf8"
)

// Preview reacquires evidence through the authenticated API. It never fetches
// a URL from source content and does not treat an old snapshot as current access.
func (g *googleAdapter) Preview(ctx context.Context, s Session, sel Selection, id string) (Snapshot, error) {
	if err := g.identityCheck(ctx, s); err != nil {
		return Snapshot{}, err
	}
	selected, err := Selected(s.Account, sel.ID)
	if err != nil {
		return Snapshot{}, err
	}
	sel = selected
	switch g.kind {
	case GoogleCalendar:
		if sel.Kind != "calendar" || !googleSafeID(sel.ID) || !googleSafeID(id) {
			return Snapshot{}, fail("SELECTION_DENIED", "Choose an event in the selected calendar")
		}
		var raw json.RawMessage
		_, err = request(ctx, s, "GET", calendarBase(sel.ID)+"/events/"+pathPart(id), nil, nil, &raw)
		if err != nil {
			return Snapshot{}, err
		}
		var e calendarEvent
		if json.Unmarshal(raw, &e) != nil || e.ID != id || e.ETag == "" {
			return Snapshot{}, fail("INVALID_RESPONSE", "Calendar event identity or revision is missing")
		}
		state := "current"
		if e.Status == "cancelled" {
			state = "removed"
		}
		return googleSnapshot(s, sel, id, "calendar_event", e.Summary, calendarText(raw), e.HTMLLink, e.ETag, state, raw), nil
	case GoogleDrive:
		if id != sel.ID {
			return Snapshot{}, fail("SELECTION_DENIED", "Only the explicitly selected Drive file can be previewed")
		}
		page, err := g.driveRefresh(ctx, s, sel, "")
		if err != nil {
			return Snapshot{}, err
		}
		item := page.Items[0]
		if item.State == "removed" {
			return item, nil
		}
		var metadata driveFile
		if json.Unmarshal(item.Content, &metadata) != nil {
			return Snapshot{}, fail("INVALID_RESPONSE", "Drive file metadata is invalid")
		}
		if metadata.MIME == "application/vnd.google-apps.folder" {
			item.Text = "Selected folder metadata only. Child files require explicit selection.\n" + item.Text
			return item, nil
		}
		readable := strings.HasPrefix(metadata.MIME, "text/") || metadata.MIME == "application/json" || metadata.MIME == "application/vnd.google-apps.document" || metadata.MIME == "application/vnd.google-apps.spreadsheet" || metadata.MIME == "application/vnd.google-apps.presentation"
		if !readable {
			item.State = "partial"
			item.Text = "This preview contains file metadata only. Acquire the file to retain its full content.\n" + item.Text
			return item, nil
		}
		file, err := g.driveAcquire(ctx, s, sel, id, "")
		if err != nil {
			return Snapshot{}, err
		}
		if file.RemoteRevision != item.RemoteRevision {
			return Snapshot{}, fail("REMOTE_CHANGED", "Drive file changed during preview")
		}
		if !utf8.Valid(file.Bytes) || strings.ContainsRune(string(file.Bytes), '\x00') {
			item.State = "partial"
			item.Text = "This file is not readable UTF-8 text. Acquire it to retain its content."
			return item, nil
		}
		item.Text = string(file.Bytes)
		if len(item.Text) > 256<<10 {
			item.Text = item.Text[:256<<10]
			for !utf8.ValidString(item.Text) {
				item.Text = item.Text[:len(item.Text)-1]
			}
			item.State = "partial"
		}
		return item, nil
	case Gmail:
		t, err := gmailSelectedThread(ctx, s, sel, id)
		if err != nil {
			return Snapshot{}, err
		}
		return gmailSnapshot(s, sel, t)
	}
	return Snapshot{}, Unsupported("preview")
}
