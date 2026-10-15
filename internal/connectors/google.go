package connectors

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const googleAPI = "https://www.googleapis.com"
const googleAcquisitionLimit = 32 << 20

type googleAdapter struct{ kind Kind }

// NewGoogleAdapter uses fixed Google API origins. Authorization and credential
// storage belong to the native broker; adapters hold no credentials or state.
func NewGoogleAdapter(kind Kind) Adapter { return &googleAdapter{kind: kind} }

func (g *googleAdapter) valid(s Session) error {
	if s.Account.Kind != g.kind || (g.kind != GoogleCalendar && g.kind != GoogleDrive && g.kind != Gmail) {
		return fail("INVALID_CONNECTOR", "Google adapter does not match this account")
	}
	return nil
}
func (g *googleAdapter) Identity(ctx context.Context, s Session) (Identity, error) {
	if err := g.valid(s); err != nil {
		return Identity{}, err
	}
	if g.kind == GoogleDrive {
		// Picker permits drive.file alone, so an OpenID identity scope cannot
		// be silently added. Drive's stable permission ID identifies this grant.
		var v struct {
			User struct {
				PermissionID string `json:"permissionId"`
				Email        string `json:"emailAddress"`
				Name         string `json:"displayName"`
			} `json:"user"`
		}
		_, err := request(ctx, s, "GET", googleAPI+"/drive/v3/about?fields=user(permissionId,emailAddress,displayName)", nil, nil, &v)
		if err != nil {
			return Identity{}, err
		}
		if v.User.PermissionID == "" {
			return Identity{}, fail("INVALID_RESPONSE", "Drive did not provide an account identity")
		}
		label := v.User.Email
		if label == "" {
			label = v.User.Name
		}
		return Identity{Subject: v.User.PermissionID, Provider: "google_drive", Label: label}, nil
	}
	var v struct{ Sub, Email string }
	_, err := request(ctx, s, "GET", "https://openidconnect.googleapis.com/v1/userinfo", nil, nil, &v)
	if err != nil {
		return Identity{}, err
	}
	if v.Sub == "" {
		return Identity{}, fail("INVALID_RESPONSE", "Google did not provide an account identity")
	}
	return Identity{Subject: v.Sub, Provider: "google", Label: v.Email}, nil
}
func (g *googleAdapter) identityCheck(ctx context.Context, s Session) error {
	i, err := g.Identity(ctx, s)
	if err != nil {
		return err
	}
	return RequireIdentity(s.Account.Identity, i)
}
func (g *googleAdapter) Discover(ctx context.Context, s Session, cursor string) ([]Selection, string, error) {
	if err := g.valid(s); err != nil {
		return nil, "", err
	}
	switch g.kind {
	case GoogleCalendar:
		return g.calendarDiscover(ctx, s, cursor)
	case GoogleDrive:
		return g.driveDiscover(ctx, s, cursor)
	case Gmail:
		return g.gmailDiscover(ctx, s, cursor)
	}
	return nil, "", Unsupported(string(g.kind))
}
func (g *googleAdapter) Refresh(ctx context.Context, s Session, sel Selection, cursor string) (RefreshPage, error) {
	if err := g.valid(s); err != nil {
		return RefreshPage{}, err
	}
	selected, err := Selected(s.Account, sel.ID)
	if err != nil {
		return RefreshPage{}, err
	}
	sel = selected
	switch g.kind {
	case GoogleCalendar:
		return g.calendarRefresh(ctx, s, sel, cursor)
	case GoogleDrive:
		return g.driveRefresh(ctx, s, sel, cursor)
	case Gmail:
		return g.gmailRefresh(ctx, s, sel, cursor)
	}
	return RefreshPage{}, Unsupported(string(g.kind))
}
func (g *googleAdapter) Prepare(ctx context.Context, s Session, a Action) (Action, error) {
	if err := g.valid(s); err != nil {
		return Action{}, err
	}
	if _, err := Selected(s.Account, a.SelectionID); err != nil {
		return Action{}, err
	}
	switch g.kind {
	case GoogleCalendar:
		return g.calendarPrepare(ctx, s, a)
	case GoogleDrive:
		return g.drivePrepare(ctx, s, a)
	case Gmail:
		return g.gmailPrepare(ctx, s, a)
	}
	return Action{}, Unsupported(a.Kind)
}
func (g *googleAdapter) Check(ctx context.Context, s Session, a Action) error {
	if err := g.identityCheck(ctx, s); err != nil {
		return err
	}
	if _, err := Selected(s.Account, a.SelectionID); err != nil {
		return err
	}
	switch g.kind {
	case GoogleCalendar:
		return g.calendarCheck(ctx, s, a)
	case GoogleDrive:
		return g.driveCheck(ctx, s, a)
	case Gmail:
		return g.gmailCheck(ctx, s, a)
	}
	return Unsupported(a.Kind)
}
func (g *googleAdapter) Send(ctx context.Context, s Session, p Proposal) (Receipt, error) {
	if p.ID == "" || p.AccountID != s.Account.ID || p.AccountRevision != s.Account.Revision {
		return Receipt{}, fail("ACCOUNT_CHANGED", "Proposal no longer matches the selected account")
	}
	if err := RequireIdentity(p.Identity, s.Account.Identity); err != nil {
		return Receipt{}, err
	}
	if err := g.Check(ctx, s, p.Action); err != nil {
		return Receipt{}, err
	}
	switch g.kind {
	case GoogleCalendar:
		return g.calendarSend(ctx, s, p)
	case GoogleDrive:
		return g.driveSend(ctx, s, p)
	case Gmail:
		return g.gmailSend(ctx, s, p)
	}
	return Receipt{}, Unsupported(p.Action.Kind)
}
func (g *googleAdapter) Reconcile(ctx context.Context, s Session, p Proposal) (Receipt, error) {
	if p.AccountID != s.Account.ID {
		return Receipt{}, fail("ACCOUNT_CHANGED", "Proposal belongs to another account")
	}
	if err := RequireIdentity(p.Identity, s.Account.Identity); err != nil {
		return Receipt{}, err
	}
	if err := g.identityCheck(ctx, s); err != nil {
		return Receipt{}, err
	}
	if _, err := Selected(s.Account, p.Action.SelectionID); err != nil {
		return Receipt{}, err
	}
	switch g.kind {
	case GoogleCalendar:
		return g.calendarReconcile(ctx, s, p)
	case GoogleDrive:
		return g.driveReconcile(ctx, s, p)
	case Gmail:
		return g.gmailReconcile(ctx, s, p)
	}
	return Receipt{}, Unsupported(p.Action.Kind)
}
func (g *googleAdapter) Acquire(ctx context.Context, s Session, sel Selection, remoteID, format string) (Acquired, error) {
	if err := g.valid(s); err != nil {
		return Acquired{}, err
	}
	selected, err := Selected(s.Account, sel.ID)
	if err != nil {
		return Acquired{}, err
	}
	switch g.kind {
	case GoogleDrive:
		return g.driveAcquire(ctx, s, selected, remoteID, format)
	case Gmail:
		return g.gmailAcquire(ctx, s, selected, remoteID, format)
	}
	return Acquired{}, Unsupported("acquire")
}

type googleCursor struct {
	Version int    `json:"v"`
	Binding string `json:"b"`
	Page    string `json:"p,omitempty"`
	Sync    string `json:"s,omitempty"`
	Offset  int    `json:"o,omitempty"`
}

func googleBinding(s Session, sel Selection) string {
	return googleHash(string(s.Account.Kind) + "\x00" + s.Account.ID + "\x00" + s.Account.Revision + "\x00" + stringify(sel))
}
func googleHash(v string) string { h := sha256.Sum256([]byte(v)); return hex.EncodeToString(h[:]) }
func readGoogleCursor(raw string, s Session, sel Selection) (googleCursor, error) {
	c := googleCursor{Version: 1, Binding: googleBinding(s, sel)}
	if raw == "" {
		return c, nil
	}
	b, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil || len(b) > 32768 {
		return c, fail("INVALID_CURSOR", "Refresh cursor is invalid")
	}
	var got googleCursor
	if json.Unmarshal(b, &got) != nil || got.Version != 1 || got.Binding != c.Binding || got.Offset < 0 {
		return c, fail("STALE_CURSOR", "Refresh cursor belongs to another account or selection")
	}
	return got, nil
}
func (c googleCursor) encode() string {
	return base64.RawURLEncoding.EncodeToString([]byte(stringify(c)))
}
func googleDecode(raw json.RawMessage, v any) error {
	if len(raw) > MaxResponseBytes {
		return fail("INVALID_REQUEST", "Reviewed arguments are too large")
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if d.Decode(v) != nil || d.Decode(new(any)) != io.EOF {
		return fail("INVALID_REQUEST", "Reviewed arguments are invalid or contain unsupported fields")
	}
	return nil
}
func googleError(err error, code string) bool {
	var f *Failure
	return errors.As(err, &f) && f.Code == code
}
func googleSnapshot(s Session, sel Selection, id, kind, title, text, link, rev, state string, content any) Snapshot {
	return Snapshot{Version: 1, AccountID: s.Account.ID, SelectionID: sel.ID, RemoteID: id, Kind: kind, Title: title, Text: text, URL: link, RemoteRevision: rev, State: state, Content: json.RawMessage(stringify(content)), RetrievedAt: utcNow(), ExternalAIAllowed: s.Account.Kind != Gmail}
}
func googleReview(a Action, args any, effects ...string) Action {
	a.Version = 1
	a.Arguments = json.RawMessage(stringify(args))
	a.Effects = effects
	return a
}

// googleBytes supports deliberate downloads and multipart uploads. No remote
// supplied URL is followed, including redirects on an injected test client.
func googleBytes(ctx context.Context, s Session, method, target, mediaType string, body []byte, headers map[string]string) ([]byte, http.Header, error) {
	if err := ValidateEndpoint(target); err != nil {
		return nil, nil, err
	}
	u, _ := url.Parse(target)
	if u.Host != "www.googleapis.com" && u.Host != "gmail.googleapis.com" {
		return nil, nil, fail("INVALID_ENDPOINT", "Google media request has an unsupported origin")
	}
	if !allowedAPI(s.Account, target) {
		return nil, nil, fail("INVALID_ENDPOINT", "Media request does not belong to this connector")
	}
	if len(body) > MaxResponseBytes {
		return nil, nil, fail("INVALID_REQUEST", "Upload exceeds the bounded connector request limit")
	}
	r, err := http.NewRequestWithContext(ctx, method, target, bytes.NewReader(body))
	if err != nil {
		return nil, nil, fail("INVALID_REQUEST", "Media request is invalid")
	}
	r.Header.Set("Authorization", "Bearer "+s.Token.AccessToken)
	if mediaType != "" {
		r.Header.Set("Content-Type", mediaType)
	}
	for k, v := range headers {
		r.Header.Set(k, v)
	}
	client := s.Client
	if client == nil {
		client = NewHTTPClient()
	}
	copyClient := *client
	copyClient.CheckRedirect = func(*http.Request, []*http.Request) error {
		return fail("REDIRECT_DENIED", "Google media redirects are not followed")
	}
	if err = ctx.Err(); err != nil {
		return nil, nil, err
	}
	mutating := method != "GET" && method != "HEAD"
	resp, err := copyClient.Do(r)
	if err != nil {
		if mutating {
			return nil, nil, Unknown("Upload may have reached Google; reconcile before retrying")
		}
		return nil, nil, fail("NETWORK_UNAVAILABLE", "Google media request did not complete")
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		switch {
		case mutating && resp.StatusCode >= 500:
			return nil, resp.Header, Unknown("Google upload outcome is uncertain; reconcile before retrying")
		case resp.StatusCode == 401 || resp.StatusCode == 403:
			return nil, resp.Header, fail("ACCESS_REVOKED", "Google denied access to the selected file")
		case resp.StatusCode == 404 || resp.StatusCode == 410:
			return nil, resp.Header, fail("REMOTE_MISSING", "Selected file is missing or inaccessible")
		case resp.StatusCode == 409 || resp.StatusCode == 412:
			return nil, resp.Header, fail("REMOTE_CHANGED", "Google file changed; prepare a new review")
		case resp.StatusCode == 429:
			n, _ := strconv.Atoi(resp.Header.Get("Retry-After"))
			if n < 1 {
				n = 60
			}
			if n > 86400 {
				n = 86400
			}
			return nil, resp.Header, &Failure{Code: "RATE_LIMITED", Message: "Google rate limit reached", RetryAfter: time.Duration(n) * time.Second}
		}
		return nil, resp.Header, fail("PROVIDER_REJECTED", "Google rejected the media request")
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, googleAcquisitionLimit+1))
	if err != nil || len(b) > googleAcquisitionLimit {
		if mutating {
			return nil, resp.Header, Unknown("Google upload response could not be verified; reconcile before retrying")
		}
		return nil, resp.Header, fail("ACQUISITION_LIMIT", "Download was interrupted or exceeds 32 MiB")
	}
	return b, resp.Header, nil
}
func googleSafeID(s string) bool {
	return nonempty(s) && len(s) <= 2048 && !strings.ContainsAny(s, "\r\n\x00/") && s != "." && s != ".."
}
