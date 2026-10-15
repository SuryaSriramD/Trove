package connectors

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/mail"
	"strings"
	"testing"
)

type googleTestTransport func(*http.Request) (*http.Response, error)

func (f googleTestTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func googleTestResponse(status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body))}
}
func googleTestSession(kind Kind, handler googleTestTransport) Session {
	provider := "google"
	if kind == GoogleDrive {
		provider = "google_drive"
	}
	return Session{Account: Account{ID: "account", Revision: "revision1", Kind: kind, Identity: Identity{Subject: "subject", Provider: provider}, Selections: []Selection{{ID: "selected", Kind: "calendar"}}}, Token: Secret{AccessToken: "synthetic-token"}, Client: &http.Client{Transport: handler}}
}
func googleTestProposal(s Session, a Action) Proposal {
	return Proposal{ID: "proposal-1", AccountID: s.Account.ID, AccountRevision: s.Account.Revision, Identity: s.Account.Identity, Hash: "reviewed-hash", Action: a, CreatedAt: "2026-10-15T10:00:00Z"}
}
func googleTestAction(kind string, args any) Action {
	return Action{Version: 1, Kind: kind, SelectionID: "selected", Arguments: json.RawMessage(stringify(args))}
}
func googleTestIdentity(r *http.Request) *http.Response {
	if r.URL.Host == "openidconnect.googleapis.com" {
		return googleTestResponse(200, `{"sub":"subject","email":"me@example.test"}`)
	}
	if r.URL.Path == "/drive/v3/about" {
		return googleTestResponse(200, `{"user":{"permissionId":"subject","emailAddress":"me@example.test"}}`)
	}
	return nil
}
func calendarTestArgs() map[string]any {
	return map[string]any{"event": map[string]any{"summary": "Review", "start": map[string]string{"dateTime": "2026-11-01T01:30:00-04:00", "timeZone": "America/New_York"}, "end": map[string]string{"dateTime": "2026-11-01T01:30:00-05:00", "timeZone": "America/New_York"}}, "sendUpdates": "all"}
}

func TestGoogleIdentityUsesSeparateDriveGrant(t *testing.T) {
	for _, kind := range []Kind{GoogleDrive, GoogleCalendar, Gmail} {
		t.Run(string(kind), func(t *testing.T) {
			s := googleTestSession(kind, func(r *http.Request) (*http.Response, error) {
				if r.Header.Get("Authorization") != "Bearer synthetic-token" {
					t.Fatal("missing credential")
				}
				v := googleTestIdentity(r)
				if v == nil {
					t.Fatalf("unexpected identity endpoint %s", r.URL)
				}
				return v, nil
			})
			id, err := NewGoogleAdapter(kind).Identity(context.Background(), s)
			if err != nil || id.Subject != "subject" || id.Provider != s.Account.Identity.Provider {
				t.Fatalf("identity=%+v err=%v", id, err)
			}
		})
	}
}
func TestGoogleCursorCannotCrossAccountsOrSelections(t *testing.T) {
	s := googleTestSession(GoogleCalendar, nil)
	sel := s.Account.Selections[0]
	c, _ := readGoogleCursor("", s, sel)
	c.Page = "opaque"
	for _, mutate := range []func(*Session, *Selection){func(s *Session, _ *Selection) { s.Account.Revision = "new" }, func(_ *Session, p *Selection) { p.ID = "other" }, func(s *Session, _ *Selection) { s.Account.ID = "other" }, func(_ *Session, p *Selection) { p.Query = "other" }} {
		other, selection := s, sel
		mutate(&other, &selection)
		if _, err := readGoogleCursor(c.encode(), other, selection); !googleError(err, "STALE_CURSOR") {
			t.Fatalf("cursor crossed scope: %v", err)
		}
	}
}
func TestGoogleCalendarPaginationPreservesRecurrenceAndSync(t *testing.T) {
	calls := 0
	s := googleTestSession(GoogleCalendar, func(r *http.Request) (*http.Response, error) {
		calls++
		q := r.URL.Query()
		if q.Get("singleEvents") != "false" || q.Get("showDeleted") != "true" {
			t.Fatal("recurrence/deletion contract lost")
		}
		switch calls {
		case 1:
			if q.Get("syncToken") != "" {
				t.Fatal("unexpected initial sync")
			}
			return googleTestResponse(200, `{"items":[{"id":"series","etag":"e1","summary":"Recurring","recurrence":["RRULE:FREQ=WEEKLY"],"start":{"dateTime":"2026-10-15T09:00:00+05:30","timeZone":"Asia/Kolkata"}}],"nextPageToken":"page2"}`), nil
		case 2:
			if q.Get("pageToken") != "page2" {
				t.Fatal("page cursor missing")
			}
			return googleTestResponse(200, `{"items":[{"id":"exception","status":"cancelled","recurringEventId":"series","originalStartTime":{"date":"2026-10-22"}}],"nextSyncToken":"sync1"}`), nil
		case 3:
			if q.Get("syncToken") != "sync1" || q.Get("pageToken") != "" {
				t.Fatal("sync cursor missing")
			}
			return googleTestResponse(200, `{"items":[],"nextSyncToken":"sync2"}`), nil
		}
		t.Fatal("unexpected call")
		return nil, nil
	})
	g := NewGoogleAdapter(GoogleCalendar)
	a, err := g.Refresh(context.Background(), s, s.Account.Selections[0], "")
	if err != nil || a.Complete || !a.FullEnumeration || !strings.Contains(string(a.Items[0].Content), "RRULE:FREQ=WEEKLY") {
		t.Fatalf("first %+v %v", a, err)
	}
	b, err := g.Refresh(context.Background(), s, s.Account.Selections[0], a.NextCursor)
	if err != nil || !b.Complete || b.NextCursor != "" || b.Items[0].State != "removed" || !strings.Contains(string(b.Items[0].Content), "originalStartTime") {
		t.Fatalf("second %+v %v", b, err)
	}
	c, err := g.Refresh(context.Background(), s, s.Account.Selections[0], b.SyncCursor)
	if err != nil || c.FullEnumeration || !c.Complete {
		t.Fatalf("incremental %+v %v", c, err)
	}
}
func TestGoogleCalendarExpiredSyncRestartsWithoutDeletingEvidence(t *testing.T) {
	calls := 0
	s := googleTestSession(GoogleCalendar, func(r *http.Request) (*http.Response, error) {
		calls++
		if calls == 1 {
			return googleTestResponse(410, `{}`), nil
		}
		if r.URL.Query().Get("syncToken") != "" {
			t.Fatal("expired cursor retained")
		}
		return googleTestResponse(200, `{"items":[],"nextSyncToken":"fresh"}`), nil
	})
	c, _ := readGoogleCursor("", s, s.Account.Selections[0])
	c.Sync = "expired"
	out, err := NewGoogleAdapter(GoogleCalendar).Refresh(context.Background(), s, s.Account.Selections[0], c.encode())
	if err != nil || !out.FullEnumeration || !out.Complete || calls != 2 {
		t.Fatalf("refresh %+v %v", out, err)
	}
}
func TestGoogleCalendarReviewRejectsHiddenFieldsAndInvalidTimes(t *testing.T) {
	cases := []map[string]any{calendarTestArgs(), calendarTestArgs(), calendarTestArgs()}
	cases[0]["sendUpdates"] = ""
	cases[1]["event"].(map[string]any)["extendedProperties"] = map[string]any{"private": map[string]string{"troveProposal": "fake"}}
	cases[2]["event"].(map[string]any)["end"] = map[string]string{"date": "2026-11-02"}
	for _, args := range cases {
		if _, err := calendarArgs(googleTestAction("calendar.create", args)); err == nil {
			t.Fatal("invalid event accepted")
		}
	}
}
func TestGoogleCalendarStaleCheckWritesNothing(t *testing.T) {
	writes := 0
	s := googleTestSession(GoogleCalendar, func(r *http.Request) (*http.Response, error) {
		if r.Method != "GET" {
			writes++
		}
		if v := googleTestIdentity(r); v != nil {
			return v, nil
		}
		if strings.Contains(r.URL.Path, "calendarList") {
			return googleTestResponse(200, `{"id":"selected","accessRole":"writer"}`), nil
		}
		return googleTestResponse(200, `{"id":"event","etag":"new"}`), nil
	})
	a := googleTestAction("calendar.update", calendarTestArgs())
	a.RemoteID = "event"
	a.ExpectedRevision = "old"
	_, err := NewGoogleAdapter(GoogleCalendar).Send(context.Background(), s, googleTestProposal(s, a))
	if !googleError(err, "REMOTE_CHANGED") || writes != 0 {
		t.Fatalf("stale mutation err=%v writes=%d", err, writes)
	}
}
func TestGoogleCalendarMutationCarriesReviewGuardAndReconcilesWithoutSend(t *testing.T) {
	writes := 0
	var proposal Proposal
	s := googleTestSession(GoogleCalendar, func(r *http.Request) (*http.Response, error) {
		if v := googleTestIdentity(r); v != nil {
			return v, nil
		}
		if strings.Contains(r.URL.Path, "calendarList") {
			return googleTestResponse(200, `{"id":"selected","accessRole":"owner"}`), nil
		}
		if r.Method == "PATCH" {
			writes++
			if r.Header.Get("If-Match") != "\"old\"" || r.URL.Query().Get("sendUpdates") != "all" {
				t.Fatal("review guard or notification effects lost")
			}
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			if body["status"] != "cancelled" {
				t.Fatal("wrong cancellation")
			}
			return nil, errors.New("timeout after send")
		}
		if writes == 0 {
			return googleTestResponse(200, `{"id":"event","etag":"\"old\""}`), nil
		}
		return googleTestResponse(200, stringify(map[string]any{"id": "event", "etag": "\"new\"", "status": "cancelled", "extendedProperties": map[string]any{"private": map[string]string{"troveProposal": proposal.ID, "troveProposalHash": proposal.Hash}}})), nil
	})
	a := googleTestAction("calendar.cancel", map[string]any{"sendUpdates": "all"})
	a.RemoteID = "event"
	a.ExpectedRevision = "\"old\""
	proposal = googleTestProposal(s, a)
	g := NewGoogleAdapter(GoogleCalendar)
	_, err := g.Send(context.Background(), s, proposal)
	if !googleError(err, "UNKNOWN_OUTCOME") {
		t.Fatal(err)
	}
	receipt, err := g.Reconcile(context.Background(), s, proposal)
	if err != nil || receipt.Outcome != "succeeded" || writes != 1 {
		t.Fatalf("receipt %+v %v writes=%d", receipt, err, writes)
	}
}
func TestGoogleAccountSwitchRefusesWrite(t *testing.T) {
	s := googleTestSession(GoogleCalendar, func(r *http.Request) (*http.Response, error) {
		if r.Method != "GET" {
			t.Fatal("wrote after account switch")
		}
		return googleTestResponse(200, `{"sub":"other"}`), nil
	})
	_, err := NewGoogleAdapter(GoogleCalendar).Send(context.Background(), s, googleTestProposal(s, googleTestAction("calendar.create", calendarTestArgs())))
	if !googleError(err, "ACCOUNT_CHANGED") {
		t.Fatal(err)
	}
}

func driveTestFile(id, mimeType, version string) string {
	return stringify(map[string]any{"id": id, "name": "File", "mimeType": mimeType, "version": version, "capabilities": map[string]bool{"canDownload": true, "canEdit": true, "canAddChildren": true}})
}
func TestGoogleDriveSelectedFolderNeverEnumeratesChildren(t *testing.T) {
	calls := 0
	s := googleTestSession(GoogleDrive, func(r *http.Request) (*http.Response, error) {
		calls++
		if r.URL.Path != "/drive/v3/files/selected" {
			t.Fatalf("expanded folder scope: %s", r.URL)
		}
		return googleTestResponse(200, driveTestFile("selected", driveFolder, "1")), nil
	})
	s.Account.Selections[0].Kind = "folder"
	g := NewGoogleAdapter(GoogleDrive)
	page, err := g.Refresh(context.Background(), s, s.Account.Selections[0], "")
	if err != nil || len(page.Items) != 1 || calls != 1 {
		t.Fatalf("page %+v %v", page, err)
	}
	_, err = g.(Acquirer).Acquire(context.Background(), s, s.Account.Selections[0], "child", "")
	if !googleError(err, "SELECTION_DENIED") || calls != 1 {
		t.Fatalf("child acquired: %v", err)
	}
}
func TestGoogleDriveAcquisitionVerifiesRevisionAfterExport(t *testing.T) {
	meta := 0
	s := googleTestSession(GoogleDrive, func(r *http.Request) (*http.Response, error) {
		if strings.HasSuffix(r.URL.Path, "/export") {
			if r.URL.Query().Get("mimeType") != "text/plain" {
				t.Fatal("wrong export")
			}
			return googleTestResponse(200, "retained bytes"), nil
		}
		meta++
		version := "1"
		if meta > 1 {
			version = "2"
		}
		return googleTestResponse(200, driveTestFile("selected", "application/vnd.google-apps.document", version)), nil
	})
	s.Account.Selections[0].Kind = "file"
	_, err := NewGoogleAdapter(GoogleDrive).(Acquirer).Acquire(context.Background(), s, s.Account.Selections[0], "selected", "")
	if !googleError(err, "REMOTE_CHANGED") {
		t.Fatalf("stale bytes accepted: %v", err)
	}
}
func TestGoogleDriveAcquisitionRetainsBytesAndFormat(t *testing.T) {
	s := googleTestSession(GoogleDrive, func(r *http.Request) (*http.Response, error) {
		if r.URL.Query().Get("alt") == "media" {
			return googleTestResponse(200, "binary\x00bytes"), nil
		}
		return googleTestResponse(200, driveTestFile("selected", "application/octet-stream", "1")), nil
	})
	s.Account.Selections[0].Kind = "file"
	got, err := NewGoogleAdapter(GoogleDrive).(Acquirer).Acquire(context.Background(), s, s.Account.Selections[0], "selected", "")
	if err != nil || string(got.Bytes) != "binary\x00bytes" || got.RemoteRevision != "version:1" {
		t.Fatalf("acquired %+v %v", got, err)
	}
	encoded := stringify(got)
	if strings.Contains(encoded, "binary") {
		t.Fatal("acquired private bytes serialized")
	}
}
func TestGoogleDriveReplaceWithoutStrongGuardFailsBeforeWrite(t *testing.T) {
	s := googleTestSession(GoogleDrive, func(r *http.Request) (*http.Response, error) {
		if r.Method != "GET" {
			t.Fatal("wrote")
		}
		return googleTestResponse(200, driveTestFile("selected", "text/plain", "1")), nil
	})
	s.Account.Selections[0].Kind = "file"
	a := googleTestAction("drive.replace", driveArguments{Name: "new.txt", MediaType: "text/plain", Data: base64.StdEncoding.EncodeToString([]byte("new"))})
	a.RemoteID = "selected"
	_, err := NewGoogleAdapter(GoogleDrive).Prepare(context.Background(), s, a)
	if !googleError(err, "UNSUPPORTED_CONCURRENCY") {
		t.Fatal(err)
	}
}
func TestGoogleDriveUploadUsesAllocatedIdentityAndMultipart(t *testing.T) {
	writes := 0
	s := googleTestSession(GoogleDrive, func(r *http.Request) (*http.Response, error) {
		if v := googleTestIdentity(r); v != nil {
			return v, nil
		}
		if strings.HasSuffix(r.URL.Path, "generateIds") {
			return googleTestResponse(200, `{"ids":["allocated"]}`), nil
		}
		if r.Method == "POST" {
			writes++
			_, params, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
			mr := multipart.NewReader(r.Body, params["boundary"])
			part, _ := mr.NextPart()
			var meta map[string]any
			_ = json.NewDecoder(part).Decode(&meta)
			if meta["id"] != "allocated" || meta["parents"].([]any)[0] != "selected" {
				t.Fatalf("wrong target %v", meta)
			}
			part, _ = mr.NextPart()
			data, _ := io.ReadAll(part)
			if string(data) != "body" {
				t.Fatal("upload bytes changed")
			}
			return googleTestResponse(200, driveTestFile("allocated", "text/plain", "1")), nil
		}
		return googleTestResponse(200, driveTestFile("selected", driveFolder, "1")), nil
	})
	s.Account.Selections[0].Kind = "folder"
	g := NewGoogleAdapter(GoogleDrive)
	a, err := g.Prepare(context.Background(), s, googleTestAction("drive.upload", driveArguments{Name: "hello.txt", MediaType: "text/plain", Data: base64.StdEncoding.EncodeToString([]byte("body"))}))
	if err != nil {
		t.Fatal(err)
	}
	receipt, err := g.Send(context.Background(), s, googleTestProposal(s, a))
	if err != nil || receipt.Outcome != "succeeded" || writes != 1 {
		t.Fatalf("send %+v %v", receipt, err)
	}
}
func TestGoogleDriveUnknownReconcileNeverResends(t *testing.T) {
	writes := 0
	s := googleTestSession(GoogleDrive, func(r *http.Request) (*http.Response, error) {
		if r.Method != "GET" {
			writes++
		}
		if v := googleTestIdentity(r); v != nil {
			return v, nil
		}
		return googleTestResponse(404, `{}`), nil
	})
	s.Account.Selections[0].Kind = "folder"
	a := googleTestAction("drive.upload", driveArguments{Name: "file", MediaType: "text/plain", Data: "", PreparedID: "allocated"})
	got, err := NewGoogleAdapter(GoogleDrive).Reconcile(context.Background(), s, googleTestProposal(s, a))
	if err != nil || got.Outcome != "unknown" || writes != 0 {
		t.Fatalf("reconciliation %+v %v", got, err)
	}
}
func TestGoogleMediaOriginAndCancellationBoundaries(t *testing.T) {
	calls := 0
	s := googleTestSession(GoogleDrive, func(r *http.Request) (*http.Response, error) { calls++; return nil, errors.New("interrupted") })
	if _, _, err := googleBytes(context.Background(), s, "GET", "https://evil.example/file", "", nil, nil); !googleError(err, "INVALID_ENDPOINT") {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, err := googleBytes(ctx, s, "POST", googleAPI+"/upload/drive/v3/files", "text/plain", []byte("data"), nil); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if calls != 0 {
		t.Fatal("network before cancellation check")
	}
	if _, _, err := googleBytes(context.Background(), s, "POST", googleAPI+"/upload/drive/v3/files", "text/plain", []byte("data"), nil); !googleError(err, "UNKNOWN_OUTCOME") {
		t.Fatal(err)
	}
}
func TestGoogleMediaRedirectNeverForwardsCredential(t *testing.T) {
	calls := 0
	s := googleTestSession(GoogleDrive, func(r *http.Request) (*http.Response, error) {
		calls++
		resp := googleTestResponse(302, "")
		resp.Header.Set("Location", "https://evil.example/download")
		return resp, nil
	})
	_, _, err := googleBytes(context.Background(), s, "GET", googleAPI+"/drive/v3/files/selected?alt=media", "", nil, nil)
	if err == nil || calls != 1 {
		t.Fatalf("redirect followed %d %v", calls, err)
	}
}

func gmailTestThread() gmailThread {
	var t gmailThread
	_ = json.Unmarshal([]byte(`{"id":"thread","historyId":"h1","messages":[{"id":"message","threadId":"thread","labelIds":["selected"],"payload":{"partId":"","mimeType":"multipart/mixed","headers":[{"name":"Subject","value":"Existing subject"},{"name":"Message-ID","value":"<original@example.test>"}],"parts":[{"partId":"0","mimeType":"text/plain","body":{"data":"cHJpdmF0ZSBib2R5","size":12}},{"partId":"1","filename":"report.txt","mimeType":"text/plain","body":{"attachmentId":"attachment","size":5}}]}}]}`), &t)
	return t
}
func TestGoogleGmailRefreshKeepsQueryExactAndDisablesExternalAI(t *testing.T) {
	s := googleTestSession(Gmail, func(r *http.Request) (*http.Response, error) {
		if strings.HasSuffix(r.URL.Path, "/threads") {
			if r.URL.Query().Get("q") != "label:work newer_than:30d" {
				t.Fatal("manual query changed")
			}
			return googleTestResponse(200, `{"threads":[{"id":"thread"}],"nextPageToken":"p2"}`), nil
		}
		return googleTestResponse(200, stringify(gmailTestThread())), nil
	})
	s.Account.Selections = []Selection{{ID: "query:work", Kind: "query", Query: "label:work newer_than:30d"}}
	page, err := NewGoogleAdapter(Gmail).Refresh(context.Background(), s, s.Account.Selections[0], "")
	if err != nil || len(page.Items) != 1 || page.Items[0].ExternalAIAllowed || page.Complete || page.NextCursor == "" || !strings.Contains(page.Items[0].Text, "private body") || !strings.Contains(string(page.Items[0].Content), "gmail-attachment:") {
		t.Fatalf("page %+v %v", page, err)
	}
}
func TestGoogleGmailAttachmentRequiresExactMessagePartAndScope(t *testing.T) {
	calls := 0
	s := googleTestSession(Gmail, func(r *http.Request) (*http.Response, error) {
		calls++
		if strings.Contains(r.URL.Path, "/attachments/") {
			return googleTestResponse(200, `{"size":5,"data":"Ynl0ZXM"}`), nil
		}
		return googleTestResponse(200, stringify(gmailTestThread())), nil
	})
	s.Account.Selections[0].Kind = "label"
	g := NewGoogleAdapter(Gmail).(Acquirer)
	key := gmailAttachmentKey{Thread: "thread", Message: "message", Part: "1", Attachment: "attachment", Name: "report.txt", MIME: "text/plain"}
	result, err := g.Acquire(context.Background(), s, s.Account.Selections[0], key.encode(), "")
	if err != nil || string(result.Bytes) != "bytes" || result.RemoteRevision != "h1" {
		t.Fatalf("acquire %+v %v", result, err)
	}
	before := calls
	key.Message = "another"
	_, err = g.Acquire(context.Background(), s, s.Account.Selections[0], key.encode(), "")
	if !googleError(err, "REMOTE_CHANGED") || calls != before+2 {
		t.Fatal("forged part accepted", err)
	}
	s.Account.Selections[0].ID = "otherlabel"
	_, err = g.Acquire(context.Background(), s, s.Account.Selections[0], key.encode(), "")
	if !googleError(err, "SELECTION_DENIED") {
		t.Fatal("cross-label acquisition", err)
	}
}
func TestGoogleGmailReplyBindsThreadRecipientsAndHeaders(t *testing.T) {
	writes := 0
	s := googleTestSession(Gmail, func(r *http.Request) (*http.Response, error) {
		if v := googleTestIdentity(r); v != nil {
			return v, nil
		}
		if strings.HasSuffix(r.URL.Path, "/profile") {
			return googleTestResponse(200, `{"emailAddress":"me@example.test"}`), nil
		}
		if r.Method == "POST" {
			writes++
			var payload map[string]string
			_ = json.NewDecoder(r.Body).Decode(&payload)
			if payload["threadId"] != "thread" {
				t.Fatal("reply thread missing")
			}
			raw, err := base64.RawURLEncoding.DecodeString(payload["raw"])
			if err != nil {
				t.Fatal(err)
			}
			message, err := mail.ReadMessage(strings.NewReader(string(raw)))
			if err != nil {
				t.Fatal(err)
			}
			if message.Header.Get("To") != "recipient@example.test" || message.Header.Get("In-Reply-To") != "<original@example.test>" || message.Header.Get("Subject") != "Existing subject" || !strings.Contains(message.Header.Get("References"), "<original@example.test>") {
				t.Fatalf("headers %v", message.Header)
			}
			return googleTestResponse(200, `{"id":"sent","threadId":"thread","historyId":"h2"}`), nil
		}
		return googleTestResponse(200, stringify(gmailTestThread())), nil
	})
	s.Account.Selections[0].Kind = "label"
	g := NewGoogleAdapter(Gmail)
	a := googleTestAction("gmail.reply", gmailArguments{To: []string{"recipient@example.test"}, Text: "Reply body"})
	a.RemoteID = "thread"
	prepared, err := g.Prepare(context.Background(), s, a)
	if err != nil {
		t.Fatal(err)
	}
	if prepared.ExpectedRevision != "h1" {
		t.Fatal("missing revision")
	}
	receipt, err := g.Send(context.Background(), s, googleTestProposal(s, prepared))
	if err != nil || writes != 1 || receipt.Outcome != "succeeded" {
		t.Fatalf("send %+v %v", receipt, err)
	}
}
func TestGoogleGmailStaleReplyDoesNotSend(t *testing.T) {
	writes := 0
	s := googleTestSession(Gmail, func(r *http.Request) (*http.Response, error) {
		if r.Method == "POST" {
			writes++
		}
		if v := googleTestIdentity(r); v != nil {
			return v, nil
		}
		if strings.HasSuffix(r.URL.Path, "/profile") {
			return googleTestResponse(200, `{"emailAddress":"me@example.test"}`), nil
		}
		thread := gmailTestThread()
		thread.History = "h2"
		return googleTestResponse(200, stringify(thread)), nil
	})
	s.Account.Selections[0].Kind = "label"
	a := googleTestAction("gmail.reply", gmailArguments{To: []string{"recipient@example.test"}, Text: "Reply", From: "me@example.test", Subject: "Existing subject", ReplyMessageID: "message", InReplyTo: "<original@example.test>", References: "<original@example.test>"})
	a.RemoteID = "thread"
	a.ExpectedRevision = "h1"
	_, err := NewGoogleAdapter(Gmail).Send(context.Background(), s, googleTestProposal(s, a))
	if !googleError(err, "REMOTE_CHANGED") || writes != 0 {
		t.Fatalf("stale reply %v", err)
	}
}
func TestGoogleGmailRejectsHeaderInjection(t *testing.T) {
	for _, args := range []gmailArguments{{To: []string{"recipient@example.test\r\nBcc: other@example.test"}}, {To: []string{"recipient@example.test"}, Subject: "subject\nBcc: other@example.test"}, {To: []string{"recipient@example.test"}, Attachments: []gmailOutgoingAttachment{{Name: "file\r\nx", MediaType: "text/plain"}}}} {
		if _, err := gmailArgs(googleTestAction("gmail.send", args)); err == nil {
			t.Fatal("header injection accepted")
		}
	}
}
func TestGoogleGmailUnknownOutcomeReconcilesReadOnly(t *testing.T) {
	writes := 0
	var p Proposal
	s := googleTestSession(Gmail, func(r *http.Request) (*http.Response, error) {
		if v := googleTestIdentity(r); v != nil {
			return v, nil
		}
		if strings.HasSuffix(r.URL.Path, "/profile") {
			return googleTestResponse(200, `{"emailAddress":"me@example.test"}`), nil
		}
		if r.Method == "POST" {
			writes++
			return googleTestResponse(503, `{}`), nil
		}
		if strings.HasSuffix(r.URL.Path, "/messages") {
			if !strings.Contains(r.URL.Query().Get("q"), gmailMessageID(p)) {
				t.Fatal("wrong reconciliation identity")
			}
			return googleTestResponse(200, `{"messages":[{"id":"sent"}]}`), nil
		}
		return googleTestResponse(200, stringify(map[string]any{"id": "sent", "threadId": "thread", "historyId": "h2", "labelIds": []string{"SENT"}, "payload": map[string]any{"headers": []map[string]string{{"name": "Message-ID", "value": gmailMessageID(p)}, {"name": "X-Trove-Proposal-Hash", "value": p.Hash}}}})), nil
	})
	s.Account.Selections[0].Kind = "label"
	g := NewGoogleAdapter(Gmail)
	a, err := g.Prepare(context.Background(), s, googleTestAction("gmail.send", gmailArguments{To: []string{"recipient@example.test"}, Subject: "Subject", Text: "Hello"}))
	if err != nil {
		t.Fatal(err)
	}
	p = googleTestProposal(s, a)
	_, err = g.Send(context.Background(), s, p)
	if !googleError(err, "UNKNOWN_OUTCOME") {
		t.Fatal(err)
	}
	receipt, err := g.Reconcile(context.Background(), s, p)
	if err != nil || receipt.Outcome != "succeeded" || writes != 1 {
		t.Fatalf("receipt %+v %v writes=%d", receipt, err, writes)
	}
}

func TestGooglePreviewReacquiresExactSelectedSource(t *testing.T) {
	s := googleTestSession(GoogleCalendar, func(r *http.Request) (*http.Response, error) {
		if v := googleTestIdentity(r); v != nil {
			return v, nil
		}
		if r.URL.Path != "/calendar/v3/calendars/selected/events/event" {
			t.Fatalf("unexpected source %s", r.URL)
		}
		return googleTestResponse(200, `{"id":"event","etag":"new","summary":"Current title","recurrence":["RRULE:FREQ=DAILY"],"start":{"date":"2026-11-01"}}`), nil
	})
	got, err := NewGoogleAdapter(GoogleCalendar).(Previewer).Preview(context.Background(), s, s.Account.Selections[0], "event")
	if err != nil || got.RemoteRevision != "new" || !strings.Contains(string(got.Content), "RRULE:FREQ=DAILY") {
		t.Fatalf("preview %+v %v", got, err)
	}
}
func TestGoogleGmailPreviewDoesNotReadUnselectedBodies(t *testing.T) {
	s := googleTestSession(Gmail, func(r *http.Request) (*http.Response, error) {
		if v := googleTestIdentity(r); v != nil {
			return v, nil
		}
		if r.URL.Query().Get("format") != "metadata" {
			t.Fatal("unselected body read")
		}
		return googleTestResponse(200, `{"id":"thread","messages":[{"id":"message","threadId":"thread","labelIds":["other"]}]}`), nil
	})
	s.Account.Selections[0].Kind = "label"
	_, err := NewGoogleAdapter(Gmail).(Previewer).Preview(context.Background(), s, s.Account.Selections[0], "thread")
	if !googleError(err, "SELECTION_DENIED") {
		t.Fatal(err)
	}
}
func TestGoogleGmailQueryPreviewUsesExactQueryAndNoOtherBodies(t *testing.T) {
	query := "(from:a@example.test OR subject:report) -label:private"
	s := googleTestSession(Gmail, func(r *http.Request) (*http.Response, error) {
		if v := googleTestIdentity(r); v != nil {
			return v, nil
		}
		if r.URL.Path == "/gmail/v1/users/me/threads" {
			if r.URL.Query().Get("q") != query {
				t.Fatal("query was rewritten")
			}
			return googleTestResponse(200, `{"threads":[{"id":"thread"}]}`), nil
		}
		if r.URL.Path != "/gmail/v1/users/me/threads/thread" {
			t.Fatal("unselected body read")
		}
		return googleTestResponse(200, stringify(gmailTestThread())), nil
	})
	s.Account.Selections = []Selection{{ID: "query:manual", Kind: "query", Query: query}}
	got, err := NewGoogleAdapter(Gmail).(Previewer).Preview(context.Background(), s, s.Account.Selections[0], "thread")
	if err != nil || got.ExternalAIAllowed || !strings.Contains(got.Text, "private body") {
		t.Fatalf("preview %+v %v", got, err)
	}
}
func TestGoogleRateLimitAndRevocationAreExplicit(t *testing.T) {
	for _, status := range []int{401, 403, 429} {
		t.Run(stringify(status), func(t *testing.T) {
			s := googleTestSession(GoogleCalendar, func(r *http.Request) (*http.Response, error) {
				resp := googleTestResponse(status, `{"private":"must not enter diagnostics"}`)
				resp.Header.Set("Retry-After", "999999")
				return resp, nil
			})
			_, err := NewGoogleAdapter(GoogleCalendar).Refresh(context.Background(), s, s.Account.Selections[0], "")
			if status == 429 {
				var f *Failure
				if !errors.As(err, &f) || f.Code != "RATE_LIMITED" || f.RetryAfter.Seconds() != 86400 {
					t.Fatal(err)
				}
			} else if !googleError(err, "ACCESS_REVOKED") {
				t.Fatal(err)
			}
			if strings.Contains(err.Error(), "private") {
				t.Fatal("provider payload leaked")
			}
		})
	}
}
func TestGoogleMalformedMutationResponseStaysUnknown(t *testing.T) {
	s := googleTestSession(GoogleDrive, func(r *http.Request) (*http.Response, error) { return googleTestResponse(200, "not-json"), nil })
	var output driveFile
	_, err := request(context.Background(), s, "POST", googleAPI+"/upload/drive/v3/files", map[string]string{"name": "x"}, nil, &output)
	if !googleError(err, "UNKNOWN_OUTCOME") {
		t.Fatal(err)
	}
}

func TestGoogleHTMLOnlyMessageKeepsTextWithoutActiveContent(t *testing.T) {
	thread := gmailTestThread()
	thread.Messages[0].Payload.Parts = nil
	thread.Messages[0].Payload.MIME = "text/html"
	thread.Messages[0].Payload.Body.Data = base64.RawURLEncoding.EncodeToString([]byte(`<p>Full body &amp; context</p><script>fetch('https://example.test')</script><img src="https://example.test/tracker"><div>Final fact</div>`))
	s := googleTestSession(Gmail, nil)
	snapshot, err := gmailSnapshot(s, s.Account.Selections[0], thread)
	if err != nil || !strings.Contains(snapshot.Text, "Full body & context") || !strings.Contains(snapshot.Text, "Final fact") || strings.Contains(snapshot.Text, "fetch") || strings.Contains(snapshot.Text, "tracker") {
		t.Fatalf("inert message %+v %v", snapshot, err)
	}
}
