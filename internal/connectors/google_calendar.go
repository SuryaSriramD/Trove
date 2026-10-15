package connectors

import (
	"context"
	"encoding/json"
	"fmt"
	"net/mail"
	"net/url"
	"strings"
	"time"
)

type calendarEntry struct{ ID, Summary, AccessRole, TimeZone string }
type calendarEvent struct {
	ID          string `json:"id"`
	ETag        string `json:"etag"`
	Summary     string `json:"summary"`
	Description string `json:"description"`
	HTMLLink    string `json:"htmlLink"`
	Status      string `json:"status"`
	Attendees   []struct {
		Email string `json:"email"`
	} `json:"attendees"`
	Extended struct {
		Private map[string]string `json:"private"`
	} `json:"extendedProperties"`
}
type calendarArguments struct {
	Event       map[string]json.RawMessage `json:"event,omitempty"`
	SendUpdates string                     `json:"sendUpdates"`
}

func calendarBase(id string) string { return googleAPI + "/calendar/v3/calendars/" + pathPart(id) }
func (g *googleAdapter) calendarDiscover(ctx context.Context, s Session, raw string) ([]Selection, string, error) {
	sel := Selection{ID: "calendar-discovery"}
	c, err := readGoogleCursor(raw, s, sel)
	if err != nil {
		return nil, "", err
	}
	var v struct {
		Items []calendarEntry `json:"items"`
		Next  string          `json:"nextPageToken"`
	}
	q := url.Values{"maxResults": {"100"}}
	if c.Page != "" {
		q.Set("pageToken", c.Page)
	}
	_, err = request(ctx, s, "GET", queryURL(googleAPI+"/calendar/v3/users/me/calendarList", q), nil, nil, &v)
	if err != nil {
		return nil, "", err
	}
	items := []Selection{}
	for _, x := range v.Items {
		if !googleSafeID(x.ID) {
			return nil, "", fail("INVALID_RESPONSE", "Calendar returned an invalid resource identity")
		}
		items = append(items, Selection{ID: x.ID, Kind: "calendar", Label: x.Summary})
	}
	next := ""
	if v.Next != "" {
		c.Page = v.Next
		next = c.encode()
	}
	return items, next, nil
}
func (g *googleAdapter) calendarRefresh(ctx context.Context, s Session, sel Selection, raw string) (RefreshPage, error) {
	if sel.Kind != "calendar" || !googleSafeID(sel.ID) {
		return RefreshPage{}, fail("SELECTION_DENIED", "Select an explicit calendar")
	}
	c, err := readGoogleCursor(raw, s, sel)
	if err != nil {
		return RefreshPage{}, err
	}
	q := url.Values{"maxResults": {"100"}, "showDeleted": {"true"}, "singleEvents": {"false"}}
	if c.Page != "" {
		q.Set("pageToken", c.Page)
	}
	if c.Sync != "" {
		q.Set("syncToken", c.Sync)
	}
	var v struct {
		Items []json.RawMessage `json:"items"`
		Next  string            `json:"nextPageToken"`
		Sync  string            `json:"nextSyncToken"`
	}
	_, err = request(ctx, s, "GET", queryURL(calendarBase(sel.ID)+"/events", q), nil, nil, &v)
	if err != nil && c.Sync != "" && googleError(err, "REMOTE_MISSING") {
		// An expired sync token requires a fresh enumeration. Never clear
		// retained evidence until the complete replacement has succeeded.
		c.Sync = ""
		c.Page = ""
		return g.calendarRefresh(ctx, s, sel, c.encode())
	}
	if err != nil {
		return RefreshPage{}, err
	}
	out := RefreshPage{Items: []Snapshot{}, FullEnumeration: c.Sync == ""}
	for _, rawEvent := range v.Items {
		var e calendarEvent
		if json.Unmarshal(rawEvent, &e) != nil || !googleSafeID(e.ID) {
			return RefreshPage{}, fail("INVALID_RESPONSE", "Calendar returned an invalid event")
		}
		state := "current"
		if e.Status == "cancelled" {
			state = "removed"
		}
		out.Items = append(out.Items, googleSnapshot(s, sel, e.ID, "calendar_event", e.Summary, calendarText(rawEvent), e.HTMLLink, e.ETag, state, rawEvent))
	}
	if v.Next != "" {
		c.Page = v.Next
		out.NextCursor = c.encode()
	} else {
		if v.Sync == "" {
			return RefreshPage{}, fail("INVALID_RESPONSE", "Calendar did not finish with a sync token")
		}
		out.Complete = true
		c.Page = ""
		c.Sync = v.Sync
		out.SyncCursor = c.encode()
	}
	return out, nil
}

func calendarText(raw json.RawMessage) string {
	var fields map[string]json.RawMessage
	_ = json.Unmarshal(raw, &fields)
	lines := []string{}
	for _, key := range []string{"summary", "description", "location", "start", "end", "recurrence", "recurringEventId", "originalStartTime", "status", "attendees"} {
		if value, ok := fields[key]; ok {
			var text string
			if json.Unmarshal(value, &text) == nil {
				if key == "description" {
					text = googleInertHTML(text)
				}
				lines = append(lines, key+": "+text)
			} else {
				lines = append(lines, key+": "+string(value))
			}
		}
	}
	return strings.Join(lines, "\n")
}
func calendarArgs(a Action) (calendarArguments, error) {
	var v calendarArguments
	if err := googleDecode(a.Arguments, &v); err != nil {
		return v, err
	}
	if a.Kind != "calendar.create" && a.Kind != "calendar.update" && a.Kind != "calendar.cancel" {
		return v, Unsupported(a.Kind)
	}
	if v.SendUpdates != "all" && v.SendUpdates != "externalOnly" && v.SendUpdates != "none" {
		return v, fail("INVALID_REQUEST", "Choose whether Google sends attendee updates")
	}
	if a.Kind == "calendar.cancel" {
		if len(v.Event) != 0 {
			return v, fail("INVALID_REQUEST", "Cancellation cannot contain event edits")
		}
		return v, nil
	}
	allowed := map[string]bool{"summary": true, "description": true, "location": true, "start": true, "end": true, "recurrence": true, "attendees": true, "transparency": true, "visibility": true}
	for key, raw := range v.Event {
		if !allowed[key] {
			return v, fail("INVALID_REQUEST", "Event contains an unsupported field")
		}
		switch key {
		case "summary", "description", "location", "transparency", "visibility":
			var value string
			if json.Unmarshal(raw, &value) != nil || len(value) > 100000 {
				return v, fail("INVALID_REQUEST", "Event text is invalid")
			}
			if key == "visibility" && value != "default" && value != "public" && value != "private" && value != "confidential" {
				return v, fail("INVALID_REQUEST", "Event visibility is invalid")
			}
			if key == "transparency" && value != "opaque" && value != "transparent" {
				return v, fail("INVALID_REQUEST", "Event availability is invalid")
			}
		case "attendees":
			var people []struct {
				Email    string `json:"email"`
				Optional bool   `json:"optional,omitempty"`
			}
			if googleDecode(raw, &people) != nil || len(people) > 200 {
				return v, fail("INVALID_REQUEST", "Event attendees are invalid or exceed 200")
			}
			seen := map[string]bool{}
			for _, p := range people {
				email, err := mail.ParseAddress(p.Email)
				if err != nil || email.Address != p.Email || seen[strings.ToLower(p.Email)] {
					return v, fail("INVALID_REQUEST", "Event attendees need distinct email addresses")
				}
				seen[strings.ToLower(p.Email)] = true
			}
		case "recurrence":
			var lines []string
			if json.Unmarshal(raw, &lines) != nil || len(lines) > 100 {
				return v, fail("INVALID_REQUEST", "Event recurrence is invalid")
			}
			for _, line := range lines {
				if len(line) > 4096 || strings.ContainsAny(line, "\r\n") || (!strings.HasPrefix(line, "RRULE:") && !strings.HasPrefix(line, "EXRULE:") && !strings.HasPrefix(line, "RDATE") && !strings.HasPrefix(line, "EXDATE")) {
					return v, fail("INVALID_REQUEST", "Event recurrence contains an unsupported rule")
				}
			}
		}
	}
	start, hasStart := v.Event["start"]
	end, hasEnd := v.Event["end"]
	if a.Kind == "calendar.create" && (!hasStart || !hasEnd) {
		return v, fail("INVALID_REQUEST", "Event creation needs start and end")
	}
	if hasStart != hasEnd {
		return v, fail("INVALID_REQUEST", "Review start and end together")
	}
	if hasStart {
		st, sd, err := calendarTime(start)
		if err != nil {
			return v, err
		}
		et, ed, err := calendarTime(end)
		if err != nil {
			return v, err
		}
		if sd != ed || !et.After(st) {
			return v, fail("INVALID_REQUEST", "Event end must follow start using the same date kind")
		}
	}
	if len(v.Event) == 0 {
		return v, fail("INVALID_REQUEST", "No event changes were supplied")
	}
	return v, nil
}
func calendarTime(raw json.RawMessage) (time.Time, bool, error) {
	var v struct {
		Date     string `json:"date,omitempty"`
		DateTime string `json:"dateTime,omitempty"`
		TimeZone string `json:"timeZone,omitempty"`
	}
	if googleDecode(raw, &v) != nil || (v.Date == "") == (v.DateTime == "") {
		return time.Time{}, false, fail("INVALID_REQUEST", "Choose either a date or a timed event boundary")
	}
	if v.TimeZone != "" {
		if _, err := time.LoadLocation(v.TimeZone); err != nil {
			return time.Time{}, false, fail("INVALID_REQUEST", "Event timezone is invalid")
		}
	}
	if v.Date != "" {
		t, err := time.Parse("2006-01-02", v.Date)
		if err == nil {
			return t, true, nil
		}
	} else {
		t, err := time.Parse(time.RFC3339, v.DateTime)
		if err == nil {
			return t, false, nil
		}
	}
	return time.Time{}, false, fail("INVALID_REQUEST", "Event boundary must be an ISO date or timestamp with an offset")
}
func (g *googleAdapter) calendarAccess(ctx context.Context, s Session, id string) error {
	sel, err := Selected(s.Account, id)
	if err != nil {
		return err
	}
	if sel.Kind != "calendar" || !googleSafeID(id) {
		return fail("SELECTION_DENIED", "Choose a selected calendar")
	}
	var c calendarEntry
	_, err = request(ctx, s, "GET", googleAPI+"/calendar/v3/users/me/calendarList/"+pathPart(id), nil, nil, &c)
	if err != nil {
		return err
	}
	if c.ID != id || (c.AccessRole != "owner" && c.AccessRole != "writer") {
		return fail("ACCESS_REVOKED", "Selected calendar is not writable")
	}
	return nil
}
func calendarGet(ctx context.Context, s Session, cal, id string) (calendarEvent, error) {
	var e calendarEvent
	if !googleSafeID(id) {
		return e, fail("INVALID_REQUEST", "Event identity is invalid")
	}
	_, err := request(ctx, s, "GET", calendarBase(cal)+"/events/"+pathPart(id), nil, nil, &e)
	if err == nil && (e.ID != id || e.ETag == "") {
		err = fail("INVALID_RESPONSE", "Calendar event identity or revision is missing")
	}
	return e, err
}
func (g *googleAdapter) calendarPrepare(ctx context.Context, s Session, a Action) (Action, error) {
	v, err := calendarArgs(a)
	if err != nil {
		return Action{}, err
	}
	if err = g.calendarAccess(ctx, s, a.SelectionID); err != nil {
		return Action{}, err
	}
	effects := []string{"Change the selected Google Calendar", "Attendee notifications: " + v.SendUpdates + " (Google may still send some email)"}
	if v.SendUpdates == "none" {
		effects = append(effects, "Suppressing updates can prevent external calendars from synchronizing")
	}
	if a.Kind == "calendar.create" {
		if a.RemoteID != "" || a.ExpectedRevision != "" {
			return Action{}, fail("INVALID_REQUEST", "New events cannot target an existing event")
		}
	} else {
		e, err := calendarGet(ctx, s, a.SelectionID, a.RemoteID)
		if err != nil {
			return Action{}, err
		}
		if a.ExpectedRevision != "" && a.ExpectedRevision != e.ETag {
			return Action{}, fail("REMOTE_CHANGED", "Calendar event changed before review")
		}
		a.ExpectedRevision = e.ETag
		for _, p := range e.Attendees {
			effects = append(effects, "Existing attendee: "+p.Email)
		}
	}
	if raw, ok := v.Event["attendees"]; ok {
		effects = append(effects, "Reviewed attendee list: "+string(raw))
	}
	if _, ok := v.Event["recurrence"]; ok {
		effects = append(effects, "Change this event's recurrence rules and future occurrences")
	}
	if a.Kind == "calendar.cancel" {
		effects = append(effects, "Cancel this event or recurring series, including attendee effects")
	}
	return googleReview(a, v, effects...), nil
}
func (g *googleAdapter) calendarCheck(ctx context.Context, s Session, a Action) error {
	if _, err := calendarArgs(a); err != nil {
		return err
	}
	if err := g.calendarAccess(ctx, s, a.SelectionID); err != nil {
		return err
	}
	if a.Kind == "calendar.create" {
		if a.RemoteID != "" || a.ExpectedRevision != "" {
			return fail("INVALID_REQUEST", "New event cannot target an existing event")
		}
		return nil
	}
	e, err := calendarGet(ctx, s, a.SelectionID, a.RemoteID)
	if err != nil {
		return err
	}
	if a.ExpectedRevision == "" || a.ExpectedRevision != e.ETag {
		return fail("REMOTE_CHANGED", "Calendar event changed after review")
	}
	return nil
}
func calendarProposalID(p Proposal) string { return "trove" + googleHash(p.AccountID+"\x00"+p.ID) }
func (g *googleAdapter) calendarSend(ctx context.Context, s Session, p Proposal) (Receipt, error) {
	v, _ := calendarArgs(p.Action)
	body := map[string]any{}
	for k, raw := range v.Event {
		body[k] = raw
	}
	body["extendedProperties"] = map[string]any{"private": map[string]string{"troveProposal": p.ID, "troveProposalHash": p.Hash}}
	id := p.Action.RemoteID
	method := "PATCH"
	target := calendarBase(p.Action.SelectionID) + "/events"
	headers := map[string]string{}
	if p.Action.Kind == "calendar.create" {
		method = "POST"
		id = calendarProposalID(p)
		body["id"] = id
	} else {
		target += "/" + pathPart(id)
		headers["If-Match"] = p.Action.ExpectedRevision
	}
	if p.Action.Kind == "calendar.cancel" {
		body["status"] = "cancelled"
	}
	target = queryURL(target, url.Values{"sendUpdates": {v.SendUpdates}})
	var e calendarEvent
	_, err := request(ctx, s, method, target, body, headers, &e)
	if err != nil {
		return Receipt{}, err
	}
	if e.ID != id || e.ETag == "" {
		return Receipt{}, Unknown("Calendar returned an unverifiable event; reconcile before retrying")
	}
	return ReceiptFor(p, "succeeded", e.ID, e.ETag, e.HTMLLink, "Google confirmed the reviewed calendar operation"), nil
}
func (g *googleAdapter) calendarReconcile(ctx context.Context, s Session, p Proposal) (Receipt, error) {
	if _, err := calendarArgs(p.Action); err != nil {
		return Receipt{}, err
	}
	id := p.Action.RemoteID
	if p.Action.Kind == "calendar.create" {
		id = calendarProposalID(p)
	}
	e, err := calendarGet(ctx, s, p.Action.SelectionID, id)
	if googleError(err, "REMOTE_MISSING") {
		return ReceiptFor(p, "unknown", id, "", "", "No conclusive event receipt is available; do not resend"), nil
	}
	if err != nil {
		return Receipt{}, err
	}
	if e.Extended.Private["troveProposal"] == p.ID && e.Extended.Private["troveProposalHash"] == p.Hash {
		return ReceiptFor(p, "succeeded", id, e.ETag, e.HTMLLink, "Google event contains the reviewed operation receipt"), nil
	}
	return ReceiptFor(p, "unknown", id, e.ETag, e.HTMLLink, fmt.Sprintf("Event exists without this operation's receipt; %s must not be retried automatically", p.Action.Kind)), nil
}
