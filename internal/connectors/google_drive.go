package connectors

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"mime"
	"mime/multipart"
	"net/textproto"
	"net/url"
	"strings"
)

const driveFolder = "application/vnd.google-apps.folder"
const driveFields = "id,name,mimeType,description,version,modifiedTime,webViewLink,trashed,parents,appProperties,capabilities(canDownload,canEdit,canAddChildren),size"

type driveFile struct {
	ID           string            `json:"id"`
	Name         string            `json:"name"`
	MIME         string            `json:"mimeType"`
	Description  string            `json:"description"`
	Version      string            `json:"version"`
	Modified     string            `json:"modifiedTime"`
	URL          string            `json:"webViewLink"`
	Trashed      bool              `json:"trashed"`
	Parents      []string          `json:"parents"`
	Properties   map[string]string `json:"appProperties"`
	Size         string            `json:"size"`
	Capabilities struct {
		Download    bool `json:"canDownload"`
		Edit        bool `json:"canEdit"`
		AddChildren bool `json:"canAddChildren"`
	} `json:"capabilities"`
	ETag string `json:"-"`
}

func driveBase(id string) string { return googleAPI + "/drive/v3/files/" + pathPart(id) }
func driveRevision(f driveFile) string {
	if f.ETag != "" {
		return f.ETag
	}
	return "version:" + f.Version
}
func driveGet(ctx context.Context, s Session, id string) (driveFile, error) {
	var f driveFile
	if !googleSafeID(id) {
		return f, fail("INVALID_REQUEST", "Drive file identity is invalid")
	}
	h, err := request(ctx, s, "GET", queryURL(driveBase(id), url.Values{"fields": {driveFields}, "supportsAllDrives": {"true"}}), nil, nil, &f)
	if err != nil {
		return f, err
	}
	f.ETag = h.Get("ETag")
	if f.ID != id || f.Version == "" {
		return f, fail("INVALID_RESPONSE", "Drive returned no stable file identity or revision")
	}
	return f, nil
}
func (g *googleAdapter) driveDiscover(ctx context.Context, s Session, raw string) ([]Selection, string, error) {
	sel := Selection{ID: "drive-discovery"}
	c, err := readGoogleCursor(raw, s, sel)
	if err != nil {
		return nil, "", err
	}
	q := url.Values{"q": {"trashed = false"}, "spaces": {"drive"}, "pageSize": {"100"}, "fields": {"nextPageToken,files(id,name,mimeType)"}}
	if c.Page != "" {
		q.Set("pageToken", c.Page)
	}
	var v struct {
		Files []driveFile `json:"files"`
		Next  string      `json:"nextPageToken"`
	}
	_, err = request(ctx, s, "GET", queryURL(googleAPI+"/drive/v3/files", q), nil, nil, &v)
	if err != nil {
		return nil, "", err
	}
	items := []Selection{}
	for _, f := range v.Files {
		if !googleSafeID(f.ID) {
			return nil, "", fail("INVALID_RESPONSE", "Drive returned an invalid identity")
		}
		kind := "file"
		if f.MIME == driveFolder {
			kind = "folder"
		}
		items = append(items, Selection{ID: f.ID, Kind: kind, Label: f.Name})
	}
	next := ""
	if v.Next != "" {
		c.Page = v.Next
		next = c.encode()
	}
	return items, next, nil
}
func driveSelection(sel Selection) error {
	if (sel.Kind != "file" && sel.Kind != "folder") || !googleSafeID(sel.ID) {
		return fail("SELECTION_DENIED", "Choose an explicitly selected Drive file or folder")
	}
	return nil
}
func (g *googleAdapter) driveRefresh(ctx context.Context, s Session, sel Selection, raw string) (RefreshPage, error) {
	if err := driveSelection(sel); err != nil {
		return RefreshPage{}, err
	}
	if _, err := readGoogleCursor(raw, s, sel); err != nil {
		return RefreshPage{}, err
	}
	f, err := driveGet(ctx, s, sel.ID)
	if err != nil {
		return RefreshPage{}, err
	}
	state := "current"
	if f.Trashed {
		state = "removed"
	}
	item := googleSnapshot(s, sel, f.ID, "drive_file", f.Name, f.Description, f.URL, driveRevision(f), state, f)
	// A selected folder authorizes its own metadata and an upload destination,
	// never implicit enumeration or acquisition of its children.
	return RefreshPage{Items: []Snapshot{item}, Complete: true, FullEnumeration: true}, nil
}
func driveExport(mimeType, format string) (string, string, error) {
	formats := map[string]map[string]string{
		"application/vnd.google-apps.document":     {"text/plain": ".txt", "application/pdf": ".pdf", "application/vnd.openxmlformats-officedocument.wordprocessingml.document": ".docx"},
		"application/vnd.google-apps.spreadsheet":  {"text/csv": ".csv", "application/pdf": ".pdf", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet": ".xlsx"},
		"application/vnd.google-apps.presentation": {"text/plain": ".txt", "application/pdf": ".pdf", "application/vnd.openxmlformats-officedocument.presentationml.presentation": ".pptx"},
		"application/vnd.google-apps.drawing":      {"image/png": ".png", "application/pdf": ".pdf", "image/svg+xml": ".svg"},
	}
	choices, ok := formats[mimeType]
	if !ok {
		return "", "", fail("UNSUPPORTED_FORMAT", "This Google Workspace file cannot be acquired in a supported format")
	}
	if format == "" {
		switch mimeType {
		case "application/vnd.google-apps.document", "application/vnd.google-apps.presentation":
			format = "text/plain"
		case "application/vnd.google-apps.spreadsheet":
			format = "text/csv"
		default:
			format = "image/png"
		}
	}
	suffix, ok := choices[format]
	if !ok {
		return "", "", fail("UNSUPPORTED_FORMAT", "Choose a supported export format for this file")
	}
	return format, suffix, nil
}
func (g *googleAdapter) driveAcquire(ctx context.Context, s Session, sel Selection, id, format string) (Acquired, error) {
	if err := driveSelection(sel); err != nil {
		return Acquired{}, err
	}
	if id != sel.ID {
		return Acquired{}, fail("SELECTION_DENIED", "Only the explicitly selected file can be acquired")
	}
	f, err := driveGet(ctx, s, id)
	if err != nil {
		return Acquired{}, err
	}
	if f.Trashed || !f.Capabilities.Download || f.MIME == driveFolder {
		return Acquired{}, fail("ACCESS_REVOKED", "Selected file cannot be downloaded")
	}
	target := queryURL(driveBase(id), url.Values{"alt": {"media"}, "supportsAllDrives": {"true"}})
	mediaType := f.MIME
	name := f.Name
	if strings.HasPrefix(f.MIME, "application/vnd.google-apps.") {
		var suffix string
		mediaType, suffix, err = driveExport(f.MIME, format)
		if err != nil {
			return Acquired{}, err
		}
		name += suffix
		target = queryURL(driveBase(id)+"/export", url.Values{"mimeType": {mediaType}})
	} else if format != "" && format != f.MIME {
		return Acquired{}, fail("UNSUPPORTED_FORMAT", "Binary files are downloaded in their original format")
	}
	headers := map[string]string{}
	if f.ETag != "" {
		headers["If-Match"] = f.ETag
	}
	data, _, err := googleBytes(ctx, s, "GET", target, "", nil, headers)
	if err != nil {
		return Acquired{}, err
	}
	after, err := driveGet(ctx, s, id)
	if err != nil {
		return Acquired{}, err
	}
	if driveRevision(after) != driveRevision(f) || after.Version != f.Version || after.Trashed {
		return Acquired{}, fail("REMOTE_CHANGED", "File changed while downloading; review the current revision")
	}
	return Acquired{RemoteID: id, Name: name, MediaType: mediaType, Bytes: data, RemoteRevision: driveRevision(f)}, nil
}

type driveArguments struct {
	Name       string `json:"name"`
	MediaType  string `json:"mediaType"`
	Data       string `json:"dataBase64"`
	PreparedID string `json:"preparedId,omitempty"`
}

func driveArgs(a Action) (driveArguments, []byte, error) {
	var v driveArguments
	if err := googleDecode(a.Arguments, &v); err != nil {
		return v, nil, err
	}
	if a.Kind != "drive.upload" && a.Kind != "drive.replace" {
		return v, nil, Unsupported(a.Kind)
	}
	if !nonempty(v.Name) || len(v.Name) > 1024 || strings.ContainsAny(v.Name, "\x00\r\n") {
		return v, nil, fail("INVALID_REQUEST", "Choose a valid file name")
	}
	mt, params, err := mime.ParseMediaType(v.MediaType)
	if err != nil || len(params) > 0 || mt != v.MediaType || strings.HasPrefix(mt, "application/vnd.google-apps.") {
		return v, nil, fail("INVALID_REQUEST", "Upload needs an explicit binary MIME type; Workspace conversion is not supported")
	}
	data, err := base64.StdEncoding.DecodeString(v.Data)
	if err != nil || len(data) > 4<<20 {
		return v, nil, fail("INVALID_REQUEST", "Upload content must be base64 and at most 4 MiB")
	}
	return v, data, nil
}
func (g *googleAdapter) driveTarget(ctx context.Context, s Session, a Action) (driveFile, error) {
	sel, err := Selected(s.Account, a.SelectionID)
	if err != nil {
		return driveFile{}, err
	}
	if err = driveSelection(sel); err != nil {
		return driveFile{}, err
	}
	f, err := driveGet(ctx, s, sel.ID)
	if err != nil {
		return f, err
	}
	if f.Trashed {
		return f, fail("REMOTE_MISSING", "Selected Drive target is in trash")
	}
	if a.Kind == "drive.upload" {
		if f.MIME != driveFolder || !f.Capabilities.AddChildren || a.RemoteID != "" {
			return f, fail("SELECTION_DENIED", "Uploads require an explicitly selected writable folder")
		}
	} else {
		if a.RemoteID != sel.ID || f.MIME == driveFolder || strings.HasPrefix(f.MIME, "application/vnd.google-apps.") || !f.Capabilities.Edit {
			return f, fail("SELECTION_DENIED", "Replacement requires the explicitly selected writable binary file")
		}
	}
	return f, nil
}
func (g *googleAdapter) drivePrepare(ctx context.Context, s Session, a Action) (Action, error) {
	v, data, err := driveArgs(a)
	if err != nil {
		return Action{}, err
	}
	f, err := g.driveTarget(ctx, s, a)
	if err != nil {
		return Action{}, err
	}
	if a.ExpectedRevision != "" && a.ExpectedRevision != driveRevision(f) {
		return Action{}, fail("REMOTE_CHANGED", "Drive target changed before review")
	}
	a.ExpectedRevision = driveRevision(f)
	if a.Kind == "drive.upload" {
		var result struct {
			IDs []string `json:"ids"`
		}
		_, err = request(ctx, s, "GET", googleAPI+"/drive/v3/files/generateIds?count=1&space=drive&type=files", nil, nil, &result)
		if err != nil {
			return Action{}, err
		}
		if len(result.IDs) != 1 || !googleSafeID(result.IDs[0]) {
			return Action{}, fail("INVALID_RESPONSE", "Drive did not allocate a stable upload identity")
		}
		v.PreparedID = result.IDs[0]
	} else {
		v.PreparedID = ""
		if !strongETag(f.ETag) {
			return Action{}, fail("UNSUPPORTED_CONCURRENCY", "Drive did not provide a strong revision guard for safe replacement")
		}
	}
	effects := []string{"Upload " + v.Name + " (" + stringify(len(data)) + " bytes; " + v.MediaType + ")", "Target: " + f.Name + " (" + f.ID + ")"}
	if a.Kind == "drive.replace" {
		effects = append(effects, "Replace the reviewed binary file content and name; existing sharing remains")
	} else {
		effects = append(effects, "Create a new file in the selected folder; its sharing may be inherited from the folder")
	}
	return googleReview(a, v, effects...), nil
}
func strongETag(v string) bool {
	return len(v) > 2 && strings.HasPrefix(v, "\"") && strings.HasSuffix(v, "\"") && !strings.ContainsAny(v, "\r\n")
}
func (g *googleAdapter) driveCheck(ctx context.Context, s Session, a Action) error {
	v, _, err := driveArgs(a)
	if err != nil {
		return err
	}
	f, err := g.driveTarget(ctx, s, a)
	if err != nil {
		return err
	}
	if a.ExpectedRevision == "" || a.ExpectedRevision != driveRevision(f) {
		return fail("REMOTE_CHANGED", "Drive target changed after review")
	}
	if a.Kind == "drive.upload" {
		if !googleSafeID(v.PreparedID) {
			return fail("INVALID_REQUEST", "Upload has no reviewed allocated identity")
		}
	} else if !strongETag(f.ETag) || v.PreparedID != "" {
		return fail("UNSUPPORTED_CONCURRENCY", "Replacement has no strong remote revision guard")
	}
	return nil
}
func (g *googleAdapter) driveSend(ctx context.Context, s Session, p Proposal) (Receipt, error) {
	v, data, _ := driveArgs(p.Action)
	meta := map[string]any{"name": v.Name, "mimeType": v.MediaType, "appProperties": map[string]string{"troveProposal": p.ID, "troveProposalHash": p.Hash}}
	method := "POST"
	id := v.PreparedID
	target := googleAPI + "/upload/drive/v3/files"
	headers := map[string]string{}
	if p.Action.Kind == "drive.upload" {
		meta["id"] = id
		meta["parents"] = []string{p.Action.SelectionID}
	} else {
		id = p.Action.RemoteID
		target += "/" + pathPart(id)
		method = "PATCH"
		headers["If-Match"] = p.Action.ExpectedRevision
	}
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, _ := writer.CreatePart(textproto.MIMEHeader{"Content-Type": {"application/json; charset=UTF-8"}})
	_, _ = part.Write([]byte(stringify(meta)))
	part, _ = writer.CreatePart(textproto.MIMEHeader{"Content-Type": {v.MediaType}})
	_, _ = part.Write(data)
	_ = writer.Close()
	target = queryURL(target, url.Values{"uploadType": {"multipart"}, "supportsAllDrives": {"true"}, "fields": {driveFields}})
	raw, h, err := googleBytes(ctx, s, method, target, "multipart/related; boundary="+writer.Boundary(), body.Bytes(), headers)
	if err != nil {
		return Receipt{}, err
	}
	var f driveFile
	if json.Unmarshal(raw, &f) != nil || f.ID != id || f.Version == "" {
		return Receipt{}, Unknown("Drive upload response could not be verified; reconcile before retrying")
	}
	f.ETag = h.Get("ETag")
	return ReceiptFor(p, "succeeded", id, driveRevision(f), f.URL, "Drive confirmed the reviewed upload"), nil
}
func (g *googleAdapter) driveReconcile(ctx context.Context, s Session, p Proposal) (Receipt, error) {
	v, _, err := driveArgs(p.Action)
	if err != nil {
		return Receipt{}, err
	}
	id := p.Action.RemoteID
	if p.Action.Kind == "drive.upload" {
		id = v.PreparedID
	}
	f, err := driveGet(ctx, s, id)
	if googleError(err, "REMOTE_MISSING") {
		return ReceiptFor(p, "unknown", id, "", "", "No conclusive upload receipt is available; do not resend"), nil
	}
	if err != nil {
		return Receipt{}, err
	}
	targetOK := id == p.Action.SelectionID
	if p.Action.Kind == "drive.upload" {
		targetOK = false
		for _, parent := range f.Parents {
			if parent == p.Action.SelectionID {
				targetOK = true
			}
		}
	}
	if targetOK && f.Properties["troveProposal"] == p.ID && f.Properties["troveProposalHash"] == p.Hash {
		return ReceiptFor(p, "succeeded", id, driveRevision(f), f.URL, "Drive file contains the reviewed operation receipt"), nil
	}
	return ReceiptFor(p, "unknown", id, driveRevision(f), f.URL, "File exists without a matching operation receipt; do not resend"), nil
}
