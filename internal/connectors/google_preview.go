package connectors

import (
	"context"
	"encoding/json"
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
		return page.Items[0], nil
	case Gmail:
		t, err := gmailSelectedThread(ctx, s, sel, id)
		if err != nil {
			return Snapshot{}, err
		}
		return gmailSnapshot(s, sel, t)
	}
	return Snapshot{}, Unsupported("preview")
}
