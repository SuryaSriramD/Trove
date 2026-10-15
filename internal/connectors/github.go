package connectors

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/google/uuid"
)

type githubAdapter struct{}

func NewGitHubAdapter() Adapter { return &githubAdapter{} }

var githubHeaders = map[string]string{"Accept": "application/vnd.github+json", "X-GitHub-Api-Version": "2026-03-10"}

func githubRequest(ctx context.Context, s Session, method, path string, input, output any) (http.Header, error) {
	return request(ctx, s, method, "https://api.github.com"+path, input, githubHeaders, output)
}
func remoteString(v any) string {
	switch n := v.(type) {
	case string:
		return n
	case float64:
		return strconv.FormatFloat(n, 'f', -1, 64)
	case json.Number:
		return string(n)
	}
	return ""
}
func remoteHash(v any) string {
	sum := sha256.Sum256([]byte(stringify(v)))
	return hex.EncodeToString(sum[:])
}
func remoteMap(v any) map[string]any { m, _ := v.(map[string]any); return m }
func githubNext(h http.Header) bool  { return strings.Contains(h.Get("Link"), `rel="next"`) }
func githubRepoPath(id string) (string, error) {
	parts := strings.Split(id, "/")
	if len(parts) != 2 {
		return "", fail("INVALID_REQUEST", "Choose an installed repository")
	}
	for _, p := range parts {
		if p == "" || p == "." || p == ".." || len(p) > 100 {
			return "", fail("INVALID_REQUEST", "Invalid repository")
		}
		for _, c := range p {
			if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-' || c == '_' || c == '.') {
				return "", fail("INVALID_REQUEST", "Invalid repository")
			}
		}
	}
	return "/repos/" + parts[0] + "/" + parts[1], nil
}
func githubNumber(v string) (string, error) {
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil || n <= 0 {
		return "", fail("INVALID_REQUEST", "A positive remote issue or comment number is required")
	}
	return strconv.FormatInt(n, 10), nil
}
func (g *githubAdapter) Identity(ctx context.Context, s Session) (Identity, error) {
	var user map[string]any
	if _, err := githubRequest(ctx, s, "GET", "/user", nil, &user); err != nil {
		return Identity{}, err
	}
	id := remoteString(user["id"])
	if id == "" {
		return Identity{}, fail("INVALID_RESPONSE", "GitHub account identity is missing")
	}
	return Identity{Subject: id, Label: remoteString(user["login"]), Provider: "github"}, nil
}

type githubDiscovery struct {
	InstallPage   int      `json:"installPage"`
	Installations []string `json:"installations"`
	InstallIndex  int      `json:"installIndex"`
	RepoPage      int      `json:"repoPage"`
	MoreInstalls  bool     `json:"moreInstalls"`
}

func opaqueCursor(v any) string { return base64.RawURLEncoding.EncodeToString([]byte(stringify(v))) }
func decodeCursor(raw string, v any) error {
	b, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil || len(b) > 16384 || json.Unmarshal(b, v) != nil {
		return fail("INVALID_REQUEST", "Provider cursor is invalid")
	}
	return nil
}
func (g *githubAdapter) Discover(ctx context.Context, s Session, cursor string) ([]Selection, string, error) {
	c := githubDiscovery{InstallPage: 1, RepoPage: 1}
	if cursor != "" {
		if err := decodeCursor(cursor, &c); err != nil {
			return nil, "", err
		}
	}
	if c.InstallPage < 1 || c.InstallPage > 100 || c.RepoPage < 1 || c.RepoPage > 100 || c.InstallIndex < 0 || c.InstallIndex > len(c.Installations) {
		return nil, "", fail("INVALID_REQUEST", "Repository discovery cursor is invalid")
	}
	if c.InstallIndex == len(c.Installations) {
		var result struct {
			Installations []struct {
				ID int64 `json:"id"`
			} `json:"installations"`
		}
		headers, err := githubRequest(ctx, s, "GET", fmt.Sprintf("/user/installations?per_page=100&page=%d", c.InstallPage), nil, &result)
		if err != nil {
			return nil, "", err
		}
		c.Installations = nil
		for _, i := range result.Installations {
			if i.ID > 0 {
				c.Installations = append(c.Installations, strconv.FormatInt(i.ID, 10))
			}
		}
		c.InstallIndex = 0
		c.MoreInstalls = githubNext(headers)
		if len(c.Installations) == 0 {
			return []Selection{}, "", nil
		}
	}
	installation, err := githubNumber(c.Installations[c.InstallIndex])
	if err != nil {
		return nil, "", err
	}
	var result struct {
		Repositories []struct {
			FullName string `json:"full_name"`
		} `json:"repositories"`
	}
	headers, err := githubRequest(ctx, s, "GET", fmt.Sprintf("/user/installations/%s/repositories?per_page=100&page=%d", installation, c.RepoPage), nil, &result)
	if err != nil {
		return nil, "", err
	}
	out := []Selection{}
	for _, repo := range result.Repositories {
		if _, err := githubRepoPath(repo.FullName); err == nil {
			out = append(out, Selection{ID: repo.FullName, Kind: "repository", Label: repo.FullName, ParentID: installation})
		}
	}
	if githubNext(headers) {
		c.RepoPage++
	} else {
		c.RepoPage = 1
		c.InstallIndex++
	}
	if c.InstallIndex == len(c.Installations) {
		if !c.MoreInstalls {
			return out, "", nil
		}
		c.InstallPage++
		c.Installations = nil
		c.InstallIndex = 0
	}
	return out, opaqueCursor(c), nil
}
func githubSelected(ctx context.Context, s Session, id string) (Selection, string, error) {
	sel, err := Selected(s.Account, id)
	if err != nil {
		return sel, "", err
	}
	if sel.Kind != "repository" {
		return sel, "", fail("INVALID_REQUEST", "GitHub operations require a selected installed repository")
	}
	path, err := githubRepoPath(sel.ID)
	if err != nil {
		return sel, "", err
	}
	installation, err := githubNumber(sel.ParentID)
	if err != nil {
		return sel, "", err
	}
	// A user access token can read some public repositories outside an install.
	// Recheck the exact installation membership rather than treating that as a
	// selected installed repository authorization.
	for page := 1; page <= 10; page++ {
		var result struct {
			Repositories []struct {
				FullName string `json:"full_name"`
			} `json:"repositories"`
		}
		headers, err := githubRequest(ctx, s, "GET", fmt.Sprintf("/user/installations/%s/repositories?per_page=100&page=%d", installation, page), nil, &result)
		if err != nil {
			return sel, "", err
		}
		for _, repo := range result.Repositories {
			if strings.EqualFold(repo.FullName, sel.ID) {
				return sel, path, nil
			}
		}
		if !githubNext(headers) {
			break
		}
	}
	return sel, "", fail("SELECTION_DENIED", "Repository is no longer available in the selected GitHub App installation")
}

type githubRefresh struct {
	Stage    string `json:"stage"`
	Page     int    `json:"page"`
	Document int    `json:"document"`
}

func githubSnapshot(item map[string]any, kind, id string) Snapshot {
	title := remoteString(item["title"])
	text := remoteString(item["body"])
	if title == "" {
		title = kind + " " + id
	}
	return Snapshot{RemoteID: kind + ":" + id, Kind: kind, Title: bounded(title, 2000), Text: bounded(text, 256<<10), URL: remoteString(item["html_url"]), RemoteRevision: remoteHash(item), Content: json.RawMessage(stringify(item))}
}
func githubDocuments(sel Selection) ([]string, error) {
	if strings.TrimSpace(sel.Query) == "" {
		return []string{""}, nil
	}
	docs := strings.Split(strings.TrimSpace(sel.Query), "\n")
	if len(docs) > 20 {
		return nil, fail("RESOURCE_EXHAUSTED", "Select at most 20 documentation paths")
	}
	for i, doc := range docs {
		doc = strings.TrimSpace(doc)
		if doc == "" || len(doc) > 1024 || strings.HasPrefix(doc, "/") || strings.Contains(doc, "\\") {
			return nil, fail("INVALID_REQUEST", "Invalid repository documentation path")
		}
		for _, part := range strings.Split(doc, "/") {
			if part == "." || part == ".." || part == "" {
				return nil, fail("INVALID_REQUEST", "Documentation paths cannot traverse directories")
			}
		}
		docs[i] = doc
	}
	return docs, nil
}
func (g *githubAdapter) Refresh(ctx context.Context, s Session, sel Selection, cursor string) (RefreshPage, error) {
	_, path, err := githubSelected(ctx, s, sel.ID)
	if err != nil {
		return RefreshPage{}, err
	}
	c := githubRefresh{Stage: "issues", Page: 1}
	if cursor != "" {
		if err = decodeCursor(cursor, &c); err != nil {
			return RefreshPage{}, err
		}
	}
	if c.Page < 1 || c.Page > 100 || c.Document < 0 {
		return RefreshPage{}, fail("INVALID_REQUEST", "GitHub refresh cursor is invalid")
	}
	out := RefreshPage{Items: []Snapshot{}}
	switch c.Stage {
	case "issues", "comments":
		suffix := "/issues?state=all&sort=created&direction=asc"
		if c.Stage == "comments" {
			suffix = "/issues/comments?sort=created&direction=asc"
		}
		var items []map[string]any
		headers, err := githubRequest(ctx, s, "GET", fmt.Sprintf("%s%s&per_page=100&page=%d", path, suffix, c.Page), nil, &items)
		if err != nil {
			return out, err
		}
		for _, item := range items {
			kind, id := "issue", remoteString(item["number"])
			if c.Stage == "comments" {
				kind, id = "comment", remoteString(item["id"])
			} else if item["pull_request"] != nil {
				kind = "pull_request"
			}
			if id == "" {
				return out, fail("INVALID_RESPONSE", "GitHub item identity is missing")
			}
			out.Items = append(out.Items, githubSnapshot(item, kind, id))
		}
		if githubNext(headers) {
			c.Page++
		} else if c.Stage == "issues" {
			c.Stage = "comments"
			c.Page = 1
		} else {
			c.Stage = "documents"
			c.Page = 1
		}
	case "documents":
		docs, err := githubDocuments(sel)
		if err != nil {
			return out, err
		}
		if c.Document >= len(docs) {
			return out, fail("INVALID_REQUEST", "Documentation cursor is invalid")
		}
		doc := docs[c.Document]
		target := path + "/readme"
		if doc != "" {
			parts := strings.Split(doc, "/")
			for i := range parts {
				parts[i] = pathPart(parts[i])
			}
			target = path + "/contents/" + strings.Join(parts, "/")
		}
		var file map[string]any
		_, err = githubRequest(ctx, s, "GET", target, nil, &file)
		if err != nil {
			f, ok := err.(*Failure)
			if !ok || f.Code != "REMOTE_MISSING" {
				return out, err
			}
			out.Items = append(out.Items, Snapshot{RemoteID: "document:" + doc, Kind: "document", Title: doc, State: "inaccessible", Content: json.RawMessage(`{}`)})
		} else {
			encoded := remoteString(file["content"])
			data, decodeErr := base64.StdEncoding.DecodeString(strings.ReplaceAll(encoded, "\n", ""))
			if decodeErr != nil || len(data) > 256<<10 || remoteString(file["encoding"]) != "base64" || remoteString(file["type"]) != "file" {
				return out, fail("RESOURCE_EXHAUSTED", "Selected documentation is not a supported text file of at most 256 KiB")
			}
			out.Items = append(out.Items, Snapshot{RemoteID: "document:" + doc, Kind: "document", Title: remoteString(file["path"]), Text: string(data), URL: remoteString(file["html_url"]), RemoteRevision: remoteString(file["sha"]), Content: json.RawMessage(stringify(map[string]any{"path": file["path"], "sha": file["sha"]}))})
		}
		c.Document++
		if c.Document == len(docs) {
			out.Complete = true
			out.FullEnumeration = true
			return out, nil
		}
	default:
		return out, fail("INVALID_REQUEST", "Unknown GitHub refresh stage")
	}
	out.NextCursor = opaqueCursor(c)
	return out, nil
}
func githubArguments(a Action) (map[string]any, error) {
	var args map[string]any
	if len(a.Arguments) > 128<<10 || json.Unmarshal(a.Arguments, &args) != nil || args == nil {
		return nil, fail("INVALID_REQUEST", "GitHub arguments must be a bounded object")
	}
	allowed := map[string]bool{}
	switch a.Kind {
	case "issue.create":
		allowed = map[string]bool{"title": true, "body": true, "labels": true, "assignees": true}
	case "issue.update":
		allowed = map[string]bool{"title": true, "body": true, "state": true, "labels": true, "assignees": true}
	case "comment.create", "comment.update":
		allowed = map[string]bool{"body": true}
	case "pull_request.create":
		allowed = map[string]bool{"title": true, "body": true, "head": true, "base": true, "draft": true}
	default:
		return nil, Unsupported(a.Kind)
	}
	for key, value := range args {
		if !allowed[key] {
			return nil, fail("INVALID_REQUEST", "Unsupported GitHub argument: "+key)
		}
		switch key {
		case "labels", "assignees":
			items, ok := value.([]any)
			if !ok || len(items) > 30 {
				return nil, fail("INVALID_REQUEST", "Invalid GitHub labels or assignees")
			}
			for _, v := range items {
				if text, ok := v.(string); !ok || len(text) > 100 {
					return nil, fail("INVALID_REQUEST", "Invalid GitHub label or assignee")
				}
			}
		case "draft":
			if _, ok := value.(bool); !ok {
				return nil, fail("INVALID_REQUEST", "Draft must be a boolean")
			}
		default:
			text, ok := value.(string)
			if !ok || len(text) > 60000 {
				return nil, fail("INVALID_REQUEST", "Invalid GitHub text argument")
			}
		}
	}
	if strings.HasSuffix(a.Kind, ".create") && !strings.HasPrefix(a.Kind, "comment") && strings.TrimSpace(remoteString(args["title"])) == "" {
		return nil, fail("INVALID_REQUEST", "A title is required")
	}
	if strings.HasPrefix(a.Kind, "comment") && strings.TrimSpace(remoteString(args["body"])) == "" {
		return nil, fail("INVALID_REQUEST", "A comment body is required")
	}
	if state, ok := args["state"]; ok && state != "open" && state != "closed" {
		return nil, fail("INVALID_REQUEST", "Issue state must be open or closed")
	}
	return args, nil
}
func githubTarget(path string, a Action) (string, error) {
	switch a.Kind {
	case "issue.create":
		return path + "/issues", nil
	case "pull_request.create":
		return path + "/pulls", nil
	case "issue.update", "comment.create":
		n, err := githubNumber(a.RemoteID)
		if err != nil {
			return "", err
		}
		target := path + "/issues/" + n
		if a.Kind == "comment.create" {
			target += "/comments"
		}
		return target, nil
	case "comment.update":
		n, err := githubNumber(a.RemoteID)
		return path + "/issues/comments/" + n, err
	}
	return "", Unsupported(a.Kind)
}
func githubBranches(ctx context.Context, s Session, path string, args map[string]any) (string, error) {
	result := map[string]string{}
	for _, key := range []string{"head", "base"} {
		branch := remoteString(args[key])
		if branch == "" || len(branch) > 250 || strings.Contains(branch, ":") || strings.ContainsAny(branch, "\r\n\x00") {
			return "", fail("INVALID_REQUEST", "Select existing branches in the selected repository")
		}
		var value map[string]any
		if _, err := githubRequest(ctx, s, "GET", path+"/branches/"+url.PathEscape(branch), nil, &value); err != nil {
			return "", err
		}
		sha := remoteString(remoteMap(value["commit"])["sha"])
		if len(sha) != 40 {
			return "", fail("INVALID_RESPONSE", "Branch commit identity is missing")
		}
		result[key] = sha
	}
	return remoteHash(result), nil
}
func (g *githubAdapter) Prepare(ctx context.Context, s Session, a Action) (Action, error) {
	_, path, err := githubSelected(ctx, s, a.SelectionID)
	if err != nil {
		return a, err
	}
	args, err := githubArguments(a)
	if err != nil {
		return a, err
	}
	a.Version = 1
	target, err := githubTarget(path, a)
	if err != nil {
		return a, err
	}
	if a.Kind == "pull_request.create" {
		a.ExpectedRevision, err = githubBranches(ctx, s, path, args)
		if err != nil {
			return a, err
		}
	} else if a.Kind == "issue.update" || a.Kind == "comment.update" || a.Kind == "comment.create" {
		currentTarget := target
		if a.Kind == "comment.create" {
			currentTarget = strings.TrimSuffix(target, "/comments")
		}
		var current map[string]any
		if _, err = githubRequest(ctx, s, "GET", currentTarget, nil, &current); err != nil {
			return a, err
		}
		revision := remoteHash(current)
		if a.ExpectedRevision != "" && a.ExpectedRevision != revision {
			return a, fail("REMOTE_CHANGED", "GitHub content changed since it was selected")
		}
		a.ExpectedRevision = revision
		if _, provided := args["body"]; !provided && a.Kind != "comment.create" {
			args["body"] = remoteString(current["body"])
		}
	}
	// The reconciliation marker is visible in the reviewed body, before hashing.
	args["body"] = remoteString(args["body"]) + "\n\n<!-- trove-operation:" + uuid.NewString() + " -->"
	a.Arguments = json.RawMessage(stringify(args))
	a.Effects = []string{"Execute " + a.Kind + " in " + a.SelectionID, "Submit exactly the displayed fields, including the reconciliation marker", "GitHub may notify repository participants and subscribers", "Remote contents and branch commits are checked immediately before sending; GitHub does not provide a compare-and-swap guarantee for these edits"}
	return a, nil
}
func (g *githubAdapter) Check(ctx context.Context, s Session, a Action) error {
	_, path, err := githubSelected(ctx, s, a.SelectionID)
	if err != nil {
		return err
	}
	args, err := githubArguments(a)
	if err != nil {
		return err
	}
	target, err := githubTarget(path, a)
	if err != nil {
		return err
	}
	if a.Kind == "issue.create" {
		return nil
	}
	var revision string
	if a.Kind == "pull_request.create" {
		revision, err = githubBranches(ctx, s, path, args)
	} else {
		if a.Kind == "comment.create" {
			target = strings.TrimSuffix(target, "/comments")
		}
		var current map[string]any
		_, err = githubRequest(ctx, s, "GET", target, nil, &current)
		revision = remoteHash(current)
	}
	if err != nil {
		return err
	}
	if a.ExpectedRevision == "" || revision != a.ExpectedRevision {
		return fail("REMOTE_CHANGED", "GitHub content or branch commits changed; prepare a new review")
	}
	return nil
}
func (g *githubAdapter) Send(ctx context.Context, s Session, p Proposal) (Receipt, error) {
	path, err := githubRepoPath(p.Action.SelectionID)
	if err != nil {
		return Receipt{}, err
	}
	if _, err = Selected(s.Account, p.Action.SelectionID); err != nil {
		return Receipt{}, err
	}
	target, err := githubTarget(path, p.Action)
	if err != nil {
		return Receipt{}, err
	}
	args, err := githubArguments(p.Action)
	if err != nil {
		return Receipt{}, err
	}
	method := "POST"
	if strings.HasSuffix(p.Action.Kind, ".update") {
		method = "PATCH"
	}
	var result map[string]any
	if _, err = githubRequest(ctx, s, method, target, args, &result); err != nil {
		return Receipt{}, err
	}
	id := remoteString(result["number"])
	if strings.HasPrefix(p.Action.Kind, "comment") {
		id = remoteString(result["id"])
	}
	if id == "" {
		return Receipt{}, Unknown("GitHub response did not identify the resulting content")
	}
	return ReceiptFor(p, "succeeded", id, remoteHash(result), remoteString(result["html_url"]), "GitHub confirmed the reviewed write"), nil
}
func (g *githubAdapter) Reconcile(ctx context.Context, s Session, p Proposal) (Receipt, error) {
	_, path, err := githubSelected(ctx, s, p.Action.SelectionID)
	if err != nil {
		return Receipt{}, err
	}
	args, err := githubArguments(p.Action)
	if err != nil {
		return Receipt{}, err
	}
	body := remoteString(args["body"])
	at := strings.LastIndex(body, "<!-- trove-operation:")
	if at < 0 {
		return ReceiptFor(p, "unknown", "", "", "", "No durable remote operation marker is available"), nil
	}
	marker := body[at:]
	target, err := githubTarget(path, p.Action)
	if err != nil {
		return Receipt{}, err
	}
	if strings.HasSuffix(p.Action.Kind, ".update") {
		var current map[string]any
		if _, err = githubRequest(ctx, s, "GET", target, nil, &current); err != nil {
			return Receipt{}, err
		}
		if strings.Contains(remoteString(current["body"]), marker) {
			return ReceiptFor(p, "succeeded", p.Action.RemoteID, remoteHash(current), remoteString(current["html_url"]), "Verified the reviewed operation marker"), nil
		}
		return ReceiptFor(p, "unknown", "", "", "", "Current remote content does not prove this edit's outcome"), nil
	}
	if p.Action.Kind == "pull_request.create" {
		target = path + "/pulls"
	}
	separator := "?"
	if strings.Contains(target, "?") {
		separator = "&"
	}
	for page := 1; page <= 10; page++ {
		var items []map[string]any
		headers, err := githubRequest(ctx, s, "GET", fmt.Sprintf("%s%sstate=all&sort=created&direction=desc&per_page=100&page=%d", target, separator, page), nil, &items)
		if err != nil {
			return Receipt{}, err
		}
		for _, item := range items {
			if strings.Contains(remoteString(item["body"]), marker) {
				id := remoteString(item["number"])
				if p.Action.Kind == "comment.create" {
					id = remoteString(item["id"])
				}
				return ReceiptFor(p, "succeeded", id, remoteHash(item), remoteString(item["html_url"]), "Verified unique remote operation marker without resending"), nil
			}
		}
		if !githubNext(headers) {
			break
		}
	}
	return ReceiptFor(p, "unknown", "", "", "", "No matching remote receipt was found; do not resend this operation"), nil
}

func (g *githubAdapter) Preview(ctx context.Context, s Session, sel Selection, remoteID string) (Snapshot, error) {
	_, path, err := githubSelected(ctx, s, sel.ID)
	if err != nil {
		return Snapshot{}, err
	}
	kind, id, ok := strings.Cut(remoteID, ":")
	if !ok {
		return Snapshot{}, fail("INVALID_REQUEST", "Invalid GitHub source identity")
	}
	if kind == "document" {
		docs, err := githubDocuments(sel)
		if err != nil {
			return Snapshot{}, err
		}
		index := -1
		for i, doc := range docs {
			if doc == id {
				index = i
			}
		}
		if index < 0 {
			return Snapshot{}, fail("SELECTION_DENIED", "Documentation path is not selected")
		}
		page, err := g.Refresh(ctx, s, sel, opaqueCursor(githubRefresh{Stage: "documents", Page: 1, Document: index}))
		if err != nil {
			return Snapshot{}, err
		}
		if len(page.Items) != 1 || page.Items[0].State == "inaccessible" {
			return Snapshot{}, fail("REMOTE_MISSING", "Selected documentation is unavailable")
		}
		return page.Items[0], nil
	}
	number, err := githubNumber(id)
	if err != nil {
		return Snapshot{}, err
	}
	target := path + "/issues/" + number
	switch kind {
	case "issue", "pull_request":
	case "comment":
		target = path + "/issues/comments/" + number
	default:
		return Snapshot{}, Unsupported(kind)
	}
	var item map[string]any
	if _, err = githubRequest(ctx, s, "GET", target, nil, &item); err != nil {
		return Snapshot{}, err
	}
	if kind == "issue" && item["pull_request"] != nil || kind == "pull_request" && item["pull_request"] == nil {
		return Snapshot{}, fail("REMOTE_CHANGED", "GitHub source kind changed")
	}
	return githubSnapshot(item, kind, number), nil
}
