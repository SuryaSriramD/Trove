package connectors

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"mime/quotedprintable"
	"net/mail"
	"net/textproto"
	"net/url"
	"strings"
	"time"

	"golang.org/x/net/html"
)

const gmailBase = "https://gmail.googleapis.com/gmail/v1/users/me"

type gmailPart struct {
	PartID   string `json:"partId"`
	MIME     string `json:"mimeType"`
	Filename string `json:"filename"`
	Headers  []struct {
		Name  string `json:"name"`
		Value string `json:"value"`
	} `json:"headers"`
	Body struct {
		AttachmentID string `json:"attachmentId"`
		Size         int    `json:"size"`
		Data         string `json:"data"`
	} `json:"body"`
	Parts []gmailPart `json:"parts"`
}
type gmailMessage struct {
	ID       string    `json:"id"`
	ThreadID string    `json:"threadId"`
	Labels   []string  `json:"labelIds"`
	Snippet  string    `json:"snippet"`
	History  string    `json:"historyId"`
	Payload  gmailPart `json:"payload"`
}
type gmailThread struct {
	ID       string         `json:"id"`
	History  string         `json:"historyId"`
	Messages []gmailMessage `json:"messages"`
}
type gmailAttachmentKey struct {
	Thread     string `json:"thread"`
	Message    string `json:"message"`
	Part       string `json:"part"`
	Attachment string `json:"attachment"`
	Name       string `json:"name"`
	MIME       string `json:"mime"`
}

func (k gmailAttachmentKey) encode() string {
	return "gmail-attachment:" + base64.RawURLEncoding.EncodeToString([]byte(stringify(k)))
}
func gmailHeader(p gmailPart, name string) string {
	for _, h := range p.Headers {
		if strings.EqualFold(h.Name, name) {
			return h.Value
		}
	}
	return ""
}
func gmailHasLabel(m gmailMessage, label string) bool {
	for _, l := range m.Labels {
		if l == label {
			return true
		}
	}
	return false
}
func gmailLink(id string) string {
	return "https://mail.google.com/mail/u/0/#all/" + url.PathEscape(id)
}
func (g *googleAdapter) gmailDiscover(ctx context.Context, s Session, raw string) ([]Selection, string, error) {
	sel := Selection{ID: "gmail-discovery"}
	c, err := readGoogleCursor(raw, s, sel)
	if err != nil {
		return nil, "", err
	}
	var v struct {
		Labels []struct{ ID, Name string } `json:"labels"`
	}
	_, err = request(ctx, s, "GET", gmailBase+"/labels", nil, nil, &v)
	if err != nil {
		return nil, "", err
	}
	if c.Offset > len(v.Labels) {
		return nil, "", fail("STALE_CURSOR", "Gmail label inventory changed")
	}
	end := c.Offset + 100
	if end > len(v.Labels) {
		end = len(v.Labels)
	}
	out := []Selection{}
	for _, l := range v.Labels[c.Offset:end] {
		if !googleSafeID(l.ID) {
			return nil, "", fail("INVALID_RESPONSE", "Gmail label identity is invalid")
		}
		out = append(out, Selection{ID: l.ID, Kind: "label", Label: l.Name})
	}
	next := ""
	if end < len(v.Labels) {
		c.Offset = end
		next = c.encode()
	}
	return out, next, nil
}
func gmailSelection(sel Selection) error {
	switch sel.Kind {
	case "label", "thread":
		if !googleSafeID(sel.ID) {
			return fail("SELECTION_DENIED", "Gmail selection identity is invalid")
		}
	case "query":
		if !strings.HasPrefix(sel.ID, "query:") || !nonempty(sel.Query) || len(sel.Query) > 4096 {
			return fail("SELECTION_DENIED", "Gmail queries require an explicit stable selection and search expression")
		}
	default:
		return fail("SELECTION_DENIED", "Choose a selected Gmail label, query or thread")
	}
	return nil
}
func gmailGetThread(ctx context.Context, s Session, id string) (gmailThread, error) {
	var t gmailThread
	if !googleSafeID(id) {
		return t, fail("INVALID_REQUEST", "Gmail thread identity is invalid")
	}
	_, err := request(ctx, s, "GET", gmailBase+"/threads/"+pathPart(id)+"?format=full", nil, nil, &t)
	if err == nil && (t.ID != id || t.History == "" || len(t.Messages) > 100) {
		err = fail("INVALID_RESPONSE", "Gmail thread identity, revision or bounded message inventory is invalid")
	}
	if err == nil {
		for _, m := range t.Messages {
			if !googleSafeID(m.ID) || m.ThreadID != id {
				err = fail("INVALID_RESPONSE", "Gmail message belongs to another thread")
				break
			}
		}
	}
	return t, err
}
func (g *googleAdapter) gmailRefresh(ctx context.Context, s Session, sel Selection, raw string) (RefreshPage, error) {
	if err := gmailSelection(sel); err != nil {
		return RefreshPage{}, err
	}
	c, err := readGoogleCursor(raw, s, sel)
	if err != nil {
		return RefreshPage{}, err
	}
	ids := []string{}
	next := ""
	if sel.Kind == "thread" {
		ids = append(ids, sel.ID)
	} else {
		q := url.Values{"maxResults": {"20"}, "includeSpamTrash": {"true"}}
		if sel.Kind == "label" {
			q.Set("labelIds", sel.ID)
		} else {
			q.Set("q", sel.Query)
		}
		if c.Page != "" {
			q.Set("pageToken", c.Page)
		}
		var list struct {
			Threads []struct {
				ID string `json:"id"`
			} `json:"threads"`
			Next string `json:"nextPageToken"`
		}
		_, err = request(ctx, s, "GET", queryURL(gmailBase+"/threads", q), nil, nil, &list)
		if err != nil {
			return RefreshPage{}, err
		}
		if len(list.Threads) > 20 {
			return RefreshPage{}, fail("INVALID_RESPONSE", "Gmail exceeded the requested page limit")
		}
		for _, t := range list.Threads {
			ids = append(ids, t.ID)
		}
		next = list.Next
	}
	out := RefreshPage{Items: []Snapshot{}, FullEnumeration: true}
	total := 0
	for _, id := range ids {
		t, err := gmailGetThread(ctx, s, id)
		if err != nil {
			return RefreshPage{}, err
		}
		item, err := gmailSnapshot(s, sel, t)
		if err != nil {
			return RefreshPage{}, err
		}
		total += len(item.Content)
		if total > MaxResponseBytes {
			return RefreshPage{}, fail("ACQUISITION_LIMIT", "Gmail page exceeds the bounded refresh size")
		}
		out.Items = append(out.Items, item)
	}
	if next != "" {
		c.Page = next
		out.NextCursor = c.encode()
	} else {
		out.Complete = true
	}
	return out, nil
}
func gmailSnapshot(s Session, sel Selection, t gmailThread) (Snapshot, error) {
	title := "Gmail conversation"
	text, keys := []string{}, []string{}
	for _, m := range t.Messages {
		if subject := gmailHeader(m.Payload, "Subject"); subject != "" {
			title = subject
		}
		messageText := []string{}
		if err := gmailParts(t.ID, m.ID, m.Payload, &messageText, &keys, 0); err != nil {
			return Snapshot{}, err
		}
		if len(messageText) == 0 {
			messageText = append(messageText, m.Snippet)
		}
		text = append(text, strings.Join(messageText, "\n"))
	}
	return googleSnapshot(s, sel, t.ID, "gmail_thread", title, strings.Join(text, "\n\n"), gmailLink(t.ID), t.History, "current", map[string]any{"thread": t, "attachmentKeys": keys}), nil
}
func gmailParts(thread, message string, p gmailPart, text, keys *[]string, depth int) error {
	if depth > 20 {
		return fail("INVALID_RESPONSE", "Gmail MIME nesting is too deep")
	}
	if p.Filename != "" || p.Body.AttachmentID != "" {
		k := gmailAttachmentKey{Thread: thread, Message: message, Part: p.PartID, Attachment: p.Body.AttachmentID, Name: p.Filename, MIME: p.MIME}
		*keys = append(*keys, k.encode())
	} else if (p.MIME == "text/plain" || p.MIME == "text/html") && p.Body.Data != "" {
		b, err := base64.RawURLEncoding.DecodeString(strings.TrimRight(p.Body.Data, "="))
		if err != nil {
			return fail("INVALID_RESPONSE", "Gmail message content is invalid")
		}
		value := string(b)
		if p.MIME == "text/html" {
			value = googleInertHTML(value)
		}
		*text = append(*text, value)
	}
	for _, child := range p.Parts {
		if err := gmailParts(thread, message, child, text, keys, depth+1); err != nil {
			return err
		}
	}
	return nil
}

// Extract text without rendering HTML or requesting images, links or other
// remote resources. Retained MIME content remains available as source evidence.
func googleInertHTML(source string) string {
	z := html.NewTokenizer(strings.NewReader(source))
	var out strings.Builder
	hidden := 0
	for {
		token := z.Next()
		if token == html.ErrorToken {
			return strings.TrimSpace(out.String())
		}
		switch token {
		case html.StartTagToken, html.SelfClosingTagToken:
			name, _ := z.TagName()
			tag := string(name)
			if tag == "script" || tag == "style" || tag == "iframe" || tag == "object" || tag == "template" {
				if token == html.StartTagToken {
					hidden++
				}
			}
			if hidden == 0 && (tag == "br" || tag == "p" || tag == "div" || tag == "li" || tag == "tr") {
				out.WriteByte('\n')
			}
		case html.EndTagToken:
			name, _ := z.TagName()
			tag := string(name)
			if hidden > 0 && (tag == "script" || tag == "style" || tag == "iframe" || tag == "object" || tag == "template") {
				hidden--
			}
		case html.TextToken:
			if hidden == 0 {
				out.Write(z.Text())
			}
		}
	}
}
func gmailScopeThread(ctx context.Context, s Session, sel Selection, t gmailThread) error {
	if err := gmailSelection(sel); err != nil {
		return err
	}
	if sel.Kind == "thread" {
		if sel.ID == t.ID {
			return nil
		}
		return fail("SELECTION_DENIED", "Thread is outside the selected scope")
	}
	if sel.Kind == "label" {
		for _, m := range t.Messages {
			if gmailHasLabel(m, sel.ID) {
				return nil
			}
		}
		return fail("SELECTION_DENIED", "Thread no longer has the selected label")
	}
	return gmailThreadAccess(ctx, s, sel, t.ID)
}

// Query checks preserve the exact manual expression. Adding search operators
// around user syntax could broaden a query with OR or unbalanced grouping.
func gmailThreadAccess(ctx context.Context, s Session, sel Selection, id string) error {
	if err := gmailSelection(sel); err != nil {
		return err
	}
	if !googleSafeID(id) {
		return fail("INVALID_REQUEST", "Gmail thread identity is invalid")
	}
	if sel.Kind == "thread" {
		if sel.ID == id {
			return nil
		}
		return fail("SELECTION_DENIED", "Thread is outside selected scope")
	}
	if sel.Kind == "label" {
		var t gmailThread
		_, err := request(ctx, s, "GET", gmailBase+"/threads/"+pathPart(id)+"?format=metadata&fields=id,messages(id,threadId,labelIds)", nil, nil, &t)
		if err != nil {
			return err
		}
		if t.ID != id {
			return fail("INVALID_RESPONSE", "Gmail returned another thread")
		}
		for _, m := range t.Messages {
			if gmailHasLabel(m, sel.ID) {
				return nil
			}
		}
		return fail("SELECTION_DENIED", "Thread no longer has the selected label")
	}
	page := ""
	for i := 0; i < 20; i++ {
		q := url.Values{"q": {sel.Query}, "maxResults": {"100"}, "includeSpamTrash": {"true"}}
		if page != "" {
			q.Set("pageToken", page)
		}
		var result struct {
			Threads []struct {
				ID string `json:"id"`
			} `json:"threads"`
			Next string `json:"nextPageToken"`
		}
		_, err := request(ctx, s, "GET", queryURL(gmailBase+"/threads", q), nil, nil, &result)
		if err != nil {
			return err
		}
		for _, t := range result.Threads {
			if t.ID == id {
				return nil
			}
		}
		if result.Next == "" {
			return fail("SELECTION_DENIED", "Thread no longer matches the selected query")
		}
		if result.Next == page {
			return fail("INVALID_RESPONSE", "Gmail repeated a query cursor")
		}
		page = result.Next
	}
	return fail("SCOPE_CHECK_LIMIT", "Query membership exceeds the 2,000-thread verification bound; explicitly select this thread")
}
func gmailSelectedThread(ctx context.Context, s Session, sel Selection, id string) (gmailThread, error) {
	if err := gmailThreadAccess(ctx, s, sel, id); err != nil {
		return gmailThread{}, err
	}
	t, err := gmailGetThread(ctx, s, id)
	if err != nil {
		return t, err
	}
	if sel.Kind == "label" {
		if err = gmailScopeThread(ctx, s, sel, t); err != nil {
			return t, err
		}
	}
	return t, nil

}
func (g *googleAdapter) gmailAcquire(ctx context.Context, s Session, sel Selection, id, format string) (Acquired, error) {
	if format != "" {
		return Acquired{}, fail("UNSUPPORTED_FORMAT", "Gmail attachments are acquired in their original format")
	}
	if !strings.HasPrefix(id, "gmail-attachment:") {
		return Acquired{}, fail("INVALID_REQUEST", "Choose a captured Gmail attachment")
	}
	b, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(id, "gmail-attachment:"))
	if err != nil || len(b) > 8192 {
		return Acquired{}, fail("INVALID_REQUEST", "Attachment reference is invalid")
	}
	var key gmailAttachmentKey
	if googleDecode(b, &key) != nil {
		return Acquired{}, fail("INVALID_REQUEST", "Attachment reference is invalid")
	}
	t, err := gmailSelectedThread(ctx, s, sel, key.Thread)
	if err != nil {
		return Acquired{}, err
	}
	var part *gmailPart
	for i := range t.Messages {
		if t.Messages[i].ID == key.Message {
			part = gmailFindPart(&t.Messages[i].Payload, key.Part, 0)
		}
	}
	if part == nil || part.Body.AttachmentID != key.Attachment || part.Filename != key.Name || part.MIME != key.MIME {
		return Acquired{}, fail("REMOTE_CHANGED", "Attachment reference no longer matches its selected message")
	}
	data := part.Body.Data
	if key.Attachment != "" {
		if !googleSafeID(key.Attachment) {
			return Acquired{}, fail("INVALID_REQUEST", "Attachment identity is invalid")
		}
		raw, _, err := googleBytes(ctx, s, "GET", gmailBase+"/messages/"+pathPart(key.Message)+"/attachments/"+pathPart(key.Attachment), "", nil, nil)
		if err != nil {
			return Acquired{}, err
		}
		var v struct {
			Data string `json:"data"`
			Size int    `json:"size"`
		}
		if json.Unmarshal(raw, &v) != nil || v.Size != part.Body.Size {
			return Acquired{}, fail("INVALID_RESPONSE", "Attachment size could not be verified")
		}
		data = v.Data
	}
	decoded, err := base64.RawURLEncoding.DecodeString(strings.TrimRight(data, "="))
	if err != nil || len(decoded) != part.Body.Size || len(decoded) > googleAcquisitionLimit {
		return Acquired{}, fail("INVALID_RESPONSE", "Attachment bytes or size are invalid")
	}
	after, err := gmailGetThread(ctx, s, key.Thread)
	if err != nil {
		return Acquired{}, err
	}
	if after.History != t.History {
		return Acquired{}, fail("REMOTE_CHANGED", "Thread changed while acquiring the attachment")
	}
	name := key.Name
	if name == "" {
		name = "message-part"
	}
	return Acquired{RemoteID: id, Name: name, MediaType: key.MIME, Bytes: decoded, RemoteRevision: t.History}, nil
}
func gmailFindPart(p *gmailPart, id string, depth int) *gmailPart {
	if depth > 20 {
		return nil
	}
	if p.PartID == id {
		return p
	}
	for i := range p.Parts {
		if found := gmailFindPart(&p.Parts[i], id, depth+1); found != nil {
			return found
		}
	}
	return nil
}

type gmailOutgoingAttachment struct {
	Name      string `json:"name"`
	MediaType string `json:"mediaType"`
	Data      string `json:"dataBase64"`
}
type gmailArguments struct {
	To             []string                  `json:"to"`
	CC             []string                  `json:"cc,omitempty"`
	BCC            []string                  `json:"bcc,omitempty"`
	Subject        string                    `json:"subject"`
	Text           string                    `json:"text"`
	Attachments    []gmailOutgoingAttachment `json:"attachments,omitempty"`
	From           string                    `json:"from,omitempty"`
	ReplyMessageID string                    `json:"replyMessageId,omitempty"`
	InReplyTo      string                    `json:"inReplyTo,omitempty"`
	References     string                    `json:"references,omitempty"`
}

func gmailArgs(a Action) (gmailArguments, error) {
	var v gmailArguments
	if err := googleDecode(a.Arguments, &v); err != nil {
		return v, err
	}
	if a.Kind != "gmail.send" && a.Kind != "gmail.reply" {
		return v, Unsupported(a.Kind)
	}
	if len(v.To)+len(v.CC)+len(v.BCC) == 0 || len(v.To)+len(v.CC)+len(v.BCC) > 100 {
		return v, fail("INVALID_REQUEST", "Message needs between one and 100 recipients")
	}
	for _, list := range [][]string{v.To, v.CC, v.BCC} {
		for _, address := range list {
			if strings.ContainsAny(address, "\r\n") {
				return v, fail("INVALID_REQUEST", "Email addresses cannot contain line breaks")
			}
			if _, err := mail.ParseAddress(address); err != nil {
				return v, fail("INVALID_REQUEST", "Email recipient is invalid")
			}
		}
	}
	if strings.ContainsAny(v.Subject+v.From+v.InReplyTo+v.References, "\r\n") || len(v.Subject) > 998 || len(v.Text) > 2<<20 || len(v.Attachments) > 20 {
		return v, fail("INVALID_REQUEST", "Message headers, text or attachment count is invalid")
	}
	total := len(v.Text)
	for _, at := range v.Attachments {
		mt, _, err := mime.ParseMediaType(at.MediaType)
		if err != nil || mt != at.MediaType || strings.ContainsAny(at.Name, "\r\n\x00") || at.Name == "" {
			return v, fail("INVALID_REQUEST", "Attachment name or media type is invalid")
		}
		data, err := base64.StdEncoding.DecodeString(at.Data)
		if err != nil {
			return v, fail("INVALID_REQUEST", "Attachment encoding is invalid")
		}
		total += len(data)
	}
	if total > 4<<20 {
		return v, fail("INVALID_REQUEST", "Message and attachments exceed the 4 MiB compose limit")
	}
	if a.Kind == "gmail.send" && (a.RemoteID != "" || a.ExpectedRevision != "" || v.ReplyMessageID != "" || v.InReplyTo != "" || v.References != "") {
		return v, fail("INVALID_REQUEST", "New message cannot contain reply metadata")
	}
	return v, nil
}
func gmailAddress(ctx context.Context, s Session) (string, error) {
	var v struct {
		Email string `json:"emailAddress"`
	}
	_, err := request(ctx, s, "GET", gmailBase+"/profile", nil, nil, &v)
	if err != nil {
		return "", err
	}
	email, err := mail.ParseAddress(v.Email)
	if err != nil || email.Address != v.Email {
		return "", fail("INVALID_RESPONSE", "Gmail did not provide a valid sender address")
	}
	return v.Email, nil
}
func (g *googleAdapter) gmailPrepare(ctx context.Context, s Session, a Action) (Action, error) {
	v, err := gmailArgs(a)
	if err != nil {
		return Action{}, err
	}
	sel, _ := Selected(s.Account, a.SelectionID)
	if err = gmailSelection(sel); err != nil {
		return Action{}, err
	}
	sender, err := gmailAddress(ctx, s)
	if err != nil {
		return Action{}, err
	}
	if v.From != "" && v.From != sender {
		return Action{}, fail("ACCOUNT_CHANGED", "Sender does not match the connected Gmail account")
	}
	v.From = sender
	if a.Kind == "gmail.reply" {
		t, err := gmailSelectedThread(ctx, s, sel, a.RemoteID)
		if err != nil {
			return Action{}, err
		}
		if a.ExpectedRevision != "" && a.ExpectedRevision != t.History {
			return Action{}, fail("REMOTE_CHANGED", "Gmail thread changed before review")
		}
		a.ExpectedRevision = t.History
		var parent *gmailMessage
		for i := range t.Messages {
			if v.ReplyMessageID == "" || v.ReplyMessageID == t.Messages[i].ID {
				parent = &t.Messages[i]
			}
		}
		if parent == nil {
			return Action{}, fail("INVALID_REQUEST", "Reply needs a message in the selected thread")
		}
		v.ReplyMessageID = parent.ID
		v.InReplyTo = gmailHeader(parent.Payload, "Message-ID")
		v.References = strings.TrimSpace(gmailHeader(parent.Payload, "References") + " " + v.InReplyTo)
		subject := gmailHeader(parent.Payload, "Subject")
		if v.Subject != "" && v.Subject != subject {
			return Action{}, fail("INVALID_REQUEST", "A Gmail reply must keep the selected message subject")
		}
		v.Subject = subject
		if !strings.HasPrefix(v.InReplyTo, "<") || !strings.HasSuffix(v.InReplyTo, ">") {
			return Action{}, fail("INVALID_RESPONSE", "Selected message has no valid reply identity")
		}
	}
	a = googleReview(a, v, "Send email from "+sender, "To: "+strings.Join(v.To, ", "), "Cc: "+strings.Join(v.CC, ", "), "Bcc: "+strings.Join(v.BCC, ", "), fmt.Sprintf("Send %d acquired attachments; email cannot be recalled by this connector", len(v.Attachments)))
	if _, err = gmailArgs(a); err != nil {
		return Action{}, err
	}
	return a, nil
}
func (g *googleAdapter) gmailCheck(ctx context.Context, s Session, a Action) error {
	v, err := gmailArgs(a)
	if err != nil {
		return err
	}
	prepared, err := g.gmailPrepare(ctx, s, a)
	if err != nil {
		return err
	}
	var got gmailArguments
	_ = json.Unmarshal(prepared.Arguments, &got)
	if v.From == "" || stringify(v) != stringify(got) {
		return fail("REMOTE_CHANGED", "Sender or reply context changed after review")
	}
	return nil
}
func gmailMessageID(p Proposal) string {
	return "<trove." + googleHash(p.AccountID+"\x00"+p.ID) + "@notifications.trove.invalid>"
}
func gmailMIME(v gmailArguments, p Proposal) ([]byte, error) {
	var out bytes.Buffer
	headers := textproto.MIMEHeader{}
	headers.Set("From", v.From)
	for key, list := range map[string][]string{"To": v.To, "Cc": v.CC, "Bcc": v.BCC} {
		if len(list) > 0 {
			headers.Set(key, strings.Join(list, ", "))
		}
	}
	headers.Set("Subject", mime.QEncoding.Encode("UTF-8", v.Subject))
	headers.Set("Message-ID", gmailMessageID(p))
	headers.Set("X-Trove-Proposal-Hash", p.Hash)
	created, err := time.Parse(time.RFC3339Nano, p.CreatedAt)
	if err != nil {
		return nil, fail("INVALID_REQUEST", "Proposal creation timestamp is invalid")
	}
	headers.Set("Date", created.Format(time.RFC1123Z))
	headers.Set("MIME-Version", "1.0")
	if v.InReplyTo != "" {
		headers.Set("In-Reply-To", v.InReplyTo)
		headers.Set("References", v.References)
	}
	var body bytes.Buffer
	if len(v.Attachments) == 0 {
		headers.Set("Content-Type", "text/plain; charset=UTF-8")
		headers.Set("Content-Transfer-Encoding", "quoted-printable")
		w := quotedprintable.NewWriter(&body)
		_, _ = w.Write([]byte(v.Text))
		_ = w.Close()
	} else {
		w := multipart.NewWriter(&body)
		_ = w.SetBoundary("trove-" + googleHash(p.ID)[:40])
		headers.Set("Content-Type", "multipart/mixed; boundary="+w.Boundary())
		part, _ := w.CreatePart(textproto.MIMEHeader{"Content-Type": {"text/plain; charset=UTF-8"}, "Content-Transfer-Encoding": {"quoted-printable"}})
		quoted := quotedprintable.NewWriter(part)
		_, _ = quoted.Write([]byte(v.Text))
		_ = quoted.Close()
		for _, at := range v.Attachments {
			part, _ = w.CreatePart(textproto.MIMEHeader{"Content-Type": {at.MediaType}, "Content-Disposition": {mime.FormatMediaType("attachment", map[string]string{"filename": at.Name})}, "Content-Transfer-Encoding": {"base64"}})
			for raw := at.Data; len(raw) > 0; {
				n := 76
				if len(raw) < n {
					n = len(raw)
				}
				_, _ = io.WriteString(part, raw[:n]+"\r\n")
				raw = raw[n:]
			}
		}
		_ = w.Close()
	}
	for _, name := range []string{"From", "To", "Cc", "Bcc", "Subject", "Message-ID", "X-Trove-Proposal-Hash", "Date", "MIME-Version", "In-Reply-To", "References", "Content-Type", "Content-Transfer-Encoding"} {
		if value := headers.Get(name); value != "" {
			fmt.Fprintf(&out, "%s: %s\r\n", name, value)
		}
	}
	out.WriteString("\r\n")
	out.Write(body.Bytes())
	return out.Bytes(), nil
}
func (g *googleAdapter) gmailSend(ctx context.Context, s Session, p Proposal) (Receipt, error) {
	v, _ := gmailArgs(p.Action)
	raw, err := gmailMIME(v, p)
	if err != nil {
		return Receipt{}, err
	}
	body := map[string]string{"raw": base64.RawURLEncoding.EncodeToString(raw)}
	if p.Action.Kind == "gmail.reply" {
		body["threadId"] = p.Action.RemoteID
	}
	var m gmailMessage
	_, err = request(ctx, s, "POST", gmailBase+"/messages/send", body, nil, &m)
	if err != nil {
		return Receipt{}, err
	}
	if m.ID == "" || m.ThreadID == "" {
		return Receipt{}, Unknown("Gmail send response could not be verified; reconcile before retrying")
	}
	return ReceiptFor(p, "succeeded", m.ID, m.History, gmailLink(m.ThreadID), "Gmail accepted the email; this does not prove recipient delivery or reading"), nil
}
func (g *googleAdapter) gmailReconcile(ctx context.Context, s Session, p Proposal) (Receipt, error) {
	if _, err := gmailArgs(p.Action); err != nil {
		return Receipt{}, err
	}
	q := url.Values{"q": {"in:sent rfc822msgid:" + gmailMessageID(p)}, "maxResults": {"2"}}
	var list struct {
		Messages []struct {
			ID string `json:"id"`
		} `json:"messages"`
		Next string `json:"nextPageToken"`
	}
	_, err := request(ctx, s, "GET", queryURL(gmailBase+"/messages", q), nil, nil, &list)
	if err != nil {
		return Receipt{}, err
	}
	if len(list.Messages) != 1 || list.Next != "" {
		return ReceiptFor(p, "unknown", "", "", "", "Gmail has no unique confirmed sent receipt; do not resend"), nil
	}
	var m gmailMessage
	_, err = request(ctx, s, "GET", gmailBase+"/messages/"+pathPart(list.Messages[0].ID)+"?format=full", nil, nil, &m)
	if err != nil {
		return Receipt{}, err
	}
	if gmailHasLabel(m, "SENT") && gmailHeader(m.Payload, "Message-ID") == gmailMessageID(p) && gmailHeader(m.Payload, "X-Trove-Proposal-Hash") == p.Hash {
		return ReceiptFor(p, "succeeded", m.ID, m.History, gmailLink(m.ThreadID), "Gmail sent-message evidence matches this reviewed operation"), nil
	}
	return ReceiptFor(p, "unknown", m.ID, m.History, gmailLink(m.ThreadID), "Gmail message does not prove this reviewed operation; do not resend"), nil
}
