package workspace

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"trove.local/phase0/contracts"
)

const notionCaptureVersion = "2026-03-11"

type notionCaptureTask struct {
	Kind       string `json:"kind"`
	Template   bool   `json:"template,omitempty"`
	SourceID   string `json:"sourceId,omitempty"`
	PropertyID string `json:"propertyId,omitempty"`
	FileIndex  int    `json:"fileIndex,omitempty"`
	ID         string `json:"id"`
	PageID     string `json:"pageId"`
	ParentID   string `json:"parentId"`
	Cursor     string `json:"cursor,omitempty"`
	Depth      int    `json:"depth"`
}
type NotionCapture struct {
	ID         string                          `json:"captureId"`
	APIVersion string                          `json:"apiVersion,omitempty"`
	Objects    map[string]notionCapturedObject `json:"objects,omitempty"`
	Revision   int                             `json:"revision"`
	State      string                          `json:"state"`
	Roots      []string                        `json:"authorizedRoots"`
	PlanID     string                          `json:"planId"`
	Pending    []notionCaptureTask             `json:"pending"`
	Seen       map[string]bool                 `json:"seen"`
	Completed  int                             `json:"completed"`
	Bytes      int64                           `json:"bytes"`
	Error      string                          `json:"error,omitempty"`
}

var notionID = regexp.MustCompile(`^[0-9a-fA-F]{32}$`)

func init() {
	RegisterInitializer(func(ctx context.Context, k *Kernel) error {
		return k.write(ctx, func(tx *sql.Tx) error {
			_, err := tx.ExecContext(ctx, "CREATE TABLE IF NOT EXISTS notion_captures(id TEXT PRIMARY KEY,revision INTEGER NOT NULL,state_json TEXT NOT NULL)")
			return err
		})
	})
	RegisterQuery("imports.notion.start", publicNotionQuery("NotionCaptureStartRequest", startNotionCapture))
	RegisterQuery("imports.notion.status", publicNotionQuery("NotionCaptureStatusRequest", getNotionCapture))
	RegisterQuery("imports.notion.list", publicNotionQuery("NotionCaptureListRequest", func(ctx context.Context, k *Kernel, e contracts.ServiceExecutionContext, r map[string]any) (any, error) {
		if err := requireOwnerScope(k, e); err != nil {
			return nil, err
		}
		rows, err := k.db.QueryContext(ctx, "SELECT state_json FROM notion_captures ORDER BY rowid DESC LIMIT 50")
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		out := []NotionCapture{}
		for rows.Next() {
			var raw string
			var s NotionCapture
			if err = rows.Scan(&raw); err != nil {
				return nil, err
			}
			if err = json.Unmarshal([]byte(raw), &s); err != nil {
				return nil, err
			}
			out = append(out, s)
		}
		return out, rows.Err()
	}))
	RegisterQuery("imports.notion.step", publicNotionQuery("NotionCaptureStepRequest", func(ctx context.Context, k *Kernel, e contracts.ServiceExecutionContext, r map[string]any) (any, error) {
		return k.CaptureNotionStep(ctx, e, str(r["captureId"]), int(number(r["revision"])), str(r["token"]), nil)
	}))
	RegisterQuery("imports.notion.finish", publicNotionQuery("NotionCaptureFinishRequest", finishNotionCapture))
}
func loadNotionCapture(ctx context.Context, q querier, id string) (NotionCapture, error) {
	var s NotionCapture
	var raw string
	err := q.QueryRowContext(ctx, "SELECT state_json FROM notion_captures WHERE id=?", id).Scan(&raw)
	if err == nil {
		err = json.Unmarshal([]byte(raw), &s)
	}
	return s, err
}
func saveNotionCapture(ctx context.Context, tx *sql.Tx, s NotionCapture, previous int) error {
	result, err := tx.ExecContext(ctx, "UPDATE notion_captures SET revision=?,state_json=? WHERE id=? AND revision=?", s.Revision, string(marshal(s)), s.ID, previous)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err == nil && n != 1 {
		return Fail("CONFLICT", "Capture cursor changed; reload before retry")
	}
	return err
}
func startNotionCapture(ctx context.Context, k *Kernel, e contracts.ServiceExecutionContext, r map[string]any) (any, error) {
	if err := requireOwnerScope(k, e); err != nil {
		return nil, err
	}
	roots, err := notionRoots(r)
	if err != nil {
		return nil, err
	}
	namespace := strings.TrimSpace(str(r["sourceNamespace"]))
	if namespace == "" || len(namespace) > 400 {
		return nil, Fail("INVALID_REQUEST", "Choose a stable source namespace up to 400 characters")
	}
	s := NotionCapture{ID: NewID(), Revision: 1, State: "capturing", APIVersion: notionCaptureVersion, Roots: []string{}, Pending: []notionCaptureTask{}, Seen: map[string]bool{}, Objects: map[string]notionCapturedObject{}}
	p := ImportPlan{ID: NewID(), Namespace: namespace, SourceSystem: "notion_api", SourceName: "Notion API capture", RootID: NewID(), State: "capturing", Items: []ImportItem{}, SourceCompleteness: "partial", SourceOmissions: []string{"Only selected roots and their supplied descendants are authorized. Comments, permissions and history are not captured. Source settings without equivalent local behavior remain inert evidence."}, UnknownCompleteness: []string{"API authorization may omit inaccessible material. Denied or unfinished endpoints are reported, never treated as empty data."}, CommittedBatchIDs: []string{}, CreatedAt: now()}
	s.PlanID = p.ID
	for _, root := range roots {
		key := root.Kind + ":" + root.ID
		if s.Seen["root:"+key] {
			continue
		}
		s.Seen["root:"+key] = true
		s.Roots = append(s.Roots, key)
		notionEnqueue(&s, notionCaptureTask{Kind: root.Kind, ID: root.ID, PageID: NewID(), ParentID: p.RootID})
	}
	p.SourceFileID, err = k.storeImportBytes(ctx, "notion-authorized-roots.json", marshal(map[string]any{"authorizedRoots": s.Roots, "apiVersion": s.APIVersion, "capturedAt": now()}))
	if err != nil {
		return nil, err
	}
	p.Hash = importPlanHash(p)
	err = k.write(ctx, func(tx *sql.Tx) error {
		if err := saveImportPlan(ctx, tx, p); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, "INSERT INTO notion_captures VALUES(?,?,?)", s.ID, s.Revision, string(marshal(s)))
		return err
	})
	return s, err
}
func getNotionCapture(ctx context.Context, k *Kernel, e contracts.ServiceExecutionContext, r map[string]any) (any, error) {
	if err := requireOwnerScope(k, e); err != nil {
		return nil, err
	}
	return loadNotionCapture(ctx, k.db, str(r["captureId"]))
}
func notionHTTPClient(client *http.Client) *http.Client {
	if client == nil {
		client = &http.Client{Timeout: 20 * time.Second}
	}
	copy := *client
	copy.CheckRedirect = func(req *http.Request, via []*http.Request) error { return http.ErrUseLastResponse }
	return &copy
}
func fetchNotion(ctx context.Context, client *http.Client, endpoint, token string) ([]byte, int, error) {
	return fetchNotionRequest(ctx, client, endpoint, token, notionCaptureVersion, nil)
}
func fetchNotionRequest(ctx context.Context, client *http.Client, endpoint, token, version string, body []byte) ([]byte, int, error) {
	for attempt := 0; attempt < 3; attempt++ {
		method := http.MethodGet
		if body != nil {
			method = http.MethodPost
		}
		req, err := http.NewRequestWithContext(ctx, method, endpoint, strings.NewReader(string(body)))
		if err != nil {
			return nil, 0, err
		}
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
			req.Header.Set("Notion-Version", version)
			if body != nil {
				req.Header.Set("Content-Type", "application/json")
			}
		}
		resp, err := client.Do(req)
		if err != nil {
			return nil, 0, Fail("INTERNAL", "Notion capture connection failed; retry the persisted cursor")
		}
		limit := int64(16 << 20)
		if token != "" {
			limit = 4 << 20
		}
		b, readErr := io.ReadAll(io.LimitReader(resp.Body, limit+1))
		resp.Body.Close()
		if readErr != nil {
			return nil, resp.StatusCode, readErr
		}
		if int64(len(b)) > limit {
			return nil, resp.StatusCode, Fail("RESOURCE_EXHAUSTED", "Notion response exceeds capture limit")
		}
		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			return b, resp.StatusCode, nil
		}
		if (resp.StatusCode == 429 || resp.StatusCode >= 500) && attempt < 2 {
			delay := time.Duration(1<<attempt) * 100 * time.Millisecond
			if seconds, er := strconv.Atoi(resp.Header.Get("Retry-After")); er == nil && seconds > 0 {
				if seconds > 2 {
					return b, resp.StatusCode, Fail("RESOURCE_EXHAUSTED", "Notion requests a longer rate-limit delay; resume this cursor later")
				}
				delay = time.Duration(seconds) * time.Second
			}
			select {
			case <-ctx.Done():
				return nil, 0, ctx.Err()
			case <-time.After(delay):
				continue
			}
		}
		return b, resp.StatusCode, Fail("INVALID_REQUEST", fmt.Sprintf("Notion returned HTTP %d; authorization or capture retry is required", resp.StatusCode))
	}
	return nil, 0, Fail("INTERNAL", "Retry limit reached")
}
func notionRichText(v any) []any {
	list, _ := v.([]any)
	out := []any{}
	for _, raw := range list {
		span, _ := raw.(map[string]any)
		text := str(span["plain_text"])
		if text == "" {
			value, _ := span["text"].(map[string]any)
			text = str(value["content"])
		}
		s := map[string]any{"text": text}
		if link := str(span["href"]); link != "" && safeImportURL(link) {
			s["link"] = link
		}
		annotations, _ := span["annotations"].(map[string]any)
		if value := str(annotations["color"]); value != "" && value != "default" {
			highlight := strings.HasSuffix(value, "_background")
			if token := richColorToken(strings.TrimSuffix(value, "_background"), highlight); token != "" {
				if highlight {
					s["highlight"] = token
				} else {
					s["color"] = token
				}
			}
		}
		marks := []string{}
		for key, mark := range map[string]string{"bold": "bold", "italic": "italic", "underline": "underline", "strikethrough": "strike", "code": "code"} {
			if annotations[key] == true {
				marks = append(marks, mark)
			}
		}
		if len(marks) > 0 {
			s["marks"] = marks
		}
		out = append(out, s)
	}
	return out
}
func notionAttachmentURL(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.User != nil || u.Port() != "" {
		return false
	}
	host := strings.ToLower(u.Hostname())
	return strings.HasSuffix(host, ".amazonaws.com") || host == "file.notion.so" || host == "prod-files-secure.s3.us-west-2.amazonaws.com"
}

// CaptureNotionStep accepts a trusted HTTP client for host qualification/tests;
// renderer requests always use the fixed HTTPS Notion origin. Credentials exist
// only for this call, never in the resumable cursor, plan, logs or file store.
func (k *Kernel) CaptureNotionStep(ctx context.Context, e contracts.ServiceExecutionContext, id string, revision int, token string, client *http.Client) (NotionCapture, error) {
	var s NotionCapture
	if err := requireOwnerScope(k, e); err != nil {
		return s, err
	}
	if len(token) < 8 || len(token) > 4096 || strings.ContainsAny(token, "\r\n") {
		return s, Fail("INVALID_REQUEST", "Supply an integration token for this capture request")
	}
	s, err := loadNotionCapture(ctx, k.db, id)
	if err != nil {
		return s, err
	}
	if s.Revision != revision {
		return s, Fail("CONFLICT", "Capture cursor changed; reload before retry")
	}
	if s.State == "ready" || s.State == "finished" {
		return s, nil
	}
	if len(s.Pending) == 0 {
		return s, Fail("INVALID_REQUEST", "Capture has no pending work")
	}
	if s.Completed >= 10000 || s.Bytes >= 128<<20 {
		return s, Fail("RESOURCE_EXHAUSTED", "Capture exceeds 10,000 responses or 128 MiB; finish the partial capture")
	}
	p, err := loadImportPlan(ctx, k.db, s.PlanID)
	if err != nil {
		return s, err
	}
	task := s.Pending[0]
	endpoint, requestBody, err := notionEndpoint(task)
	if err != nil {
		return s, err
	}
	version := s.APIVersion
	if version == "" {
		version = "2025-09-03"
	}
	if s.Objects == nil {
		s.Objects = map[string]notionCapturedObject{}
	}
	bounded, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	client = notionHTTPClient(client)
	raw, status, fetchErr := fetchNotionRequest(bounded, client, endpoint, token, version, requestBody)
	if fetchErr == nil && task.Kind == "attachment" {
		fetchErr = k.resolveNotionAttachment(bounded, client, &s, &p, task, raw)
	}
	s.Revision++
	s.Error = ""
	s.State = "capturing"
	if fetchErr != nil {
		s.State = "blocked"
		s.Error = fetchErr.Error()
		if len(raw) > 0 {
			evidenceID, er := k.storeImportBytes(ctx, "notion-response-error.json", raw)
			if er != nil {
				return s, er
			}
			s.Error += fmt.Sprintf(" (HTTP %d; retained response %s)", status, evidenceID)
			if len(p.Items) >= 10000 || s.Bytes+int64(len(raw)) > 128<<20 {
				return s, Fail("RESOURCE_EXHAUSTED", "Failed capture responses exhausted the evidence budget")
			}
			p.Items = append(p.Items, ImportItem{Locator: fmt.Sprintf("notion/errors/%s/%d", task.ID, s.Revision), Kind: "asset", Outcome: "failed", Reason: s.Error, RawFileID: evidenceID, Hash: importDigest(raw), Size: int64(len(raw))})
			s.Bytes += int64(len(raw))
		}
		p.Hash = importPlanHash(p)
		err = k.write(ctx, func(tx *sql.Tx) error {
			if err := saveNotionCapture(ctx, tx, s, revision); err != nil {
				return err
			}
			return saveImportPlan(ctx, tx, p)
		})
		return s, err
	}
	var response map[string]any
	if err = json.Unmarshal(raw, &response); err != nil {
		return s, Fail("INVALID_REQUEST", "Notion returned invalid JSON; cursor retained")
	}
	if s.Bytes+int64(len(raw)) > 128<<20 {
		return s, Fail("RESOURCE_EXHAUSTED", "Capture response exceeds remaining byte budget")
	}
	s.Pending = s.Pending[1:]
	s.Seen[notionCursorKey(task, task.Cursor)] = true
	s.Completed++
	s.Bytes += int64(len(raw))
	rawID, err := k.storeImportBytes(ctx, "notion-"+task.ID+".json", raw)
	if err != nil {
		return s, err
	}
	provenance := map[string]any{"channel": "notion_api", "apiVersion": version, "sourceId": task.ID, "cursor": task.Cursor, "capturedAt": now(), "responseFileId": rawID}
	if task.Kind != "page" && task.Kind != "children" {
		if err = k.captureNotionObject(ctx, &s, &p, task, response, raw, rawID, provenance); err != nil {
			return s, err
		}
	} else if task.Kind == "page" {
		if task.SourceID != "" {
			if err = captureNotionRecord(&s, task, response, raw, rawID); err != nil {
				return s, err
			}
		}
		title := "Notion page " + task.ID
		properties, _ := response["properties"].(map[string]any)
		for _, rawProp := range properties {
			prop, _ := rawProp.(map[string]any)
			if prop["type"] == "title" {
				text := ""
				for _, v := range notionRichText(prop["title"]) {
					text += str(v.(map[string]any)["text"])
				}
				if text != "" {
					title = text
				}
			}
		}
		item := ImportItem{Locator: "notion/pages/" + task.ID, Kind: "notion_page", Outcome: "partial", Reason: "Page title and descendants captured; every supplied property and metadata field retained in response evidence", RawFileID: rawID, Hash: importDigest(raw), Size: int64(len(raw)), PageID: task.PageID, ParentID: task.ParentID, Title: title, IdentityKind: "external_id", ExternalID: task.ID, Provenance: provenance, Blocks: []ImportBlock{}}
		if len(properties) > 0 && task.SourceID == "" {
			item.Blocks = append(item.Blocks, fallbackImportBlock(&item, "Captured property values (source snapshots, not freshly calculated results):\n"+string(marshal(properties))))
		}
		replaced := false
		for i := range p.Items {
			if p.Items[i].Kind == "notion_page" && p.Items[i].ExternalID == task.ID {
				prior := p.Items[i]
				prior.Kind = "asset"
				prior.Locator += "#prior-page-response"
				prior.PageID = ""
				prior.ParentID = ""
				prior.Blocks = nil
				p.Items[i] = item
				p.Items = append(p.Items, prior)
				replaced = true
				break
			}
		}
		if !replaced {
			p.Items = append(p.Items, item)
		}
		childTask := notionCaptureTask{Kind: "children", ID: task.ID, PageID: task.PageID, ParentID: task.ParentID, Depth: task.Depth}
		if !s.Seen[notionCursorKey(childTask, "")] {
			s.Pending = append([]notionCaptureTask{childTask}, s.Pending...)
		}
	} else {
		results, ok := response["results"].([]any)
		if !ok {
			return s, Fail("INVALID_REQUEST", "Notion children response has no results list; cursor retained")
		}
		if len(results) > 100 {
			return s, Fail("RESOURCE_EXHAUSTED", "Notion returned more than requested 100 blocks")
		}
		var sourceBatch struct {
			Results []json.RawMessage `json:"results"`
		}
		if err = json.Unmarshal(raw, &sourceBatch); err != nil {
			return s, err
		}
		p.Items = append(p.Items, ImportItem{Locator: "notion/responses/" + task.ID + "#" + task.Cursor, Kind: "asset", Outcome: "preserved", Reason: "Complete paginated response retained as inert capture evidence", RawFileID: rawID, Hash: importDigest(raw), Size: int64(len(raw)), Provenance: provenance})
		next := []notionCaptureTask{}
		for index, rawBlock := range results {
			block, ok := rawBlock.(map[string]any)
			if !ok {
				return s, Fail("INVALID_REQUEST", "Invalid supplied Notion block")
			}
			blockID := strings.ToLower(strings.ReplaceAll(str(block["id"]), "-", ""))
			if !notionID.MatchString(blockID) {
				return s, Fail("INVALID_REQUEST", "Invalid supplied Notion block identity")
			}
			if blockID == task.ID {
				return s, Fail("INVALID_REQUEST", "Notion block cannot be its own child; source response retained")
			}
			if s.Seen["block:"+blockID] {
				continue
			}
			s.Seen["block:"+blockID] = true
			kind := str(block["type"])
			data, _ := block[kind].(map[string]any)
			blockRaw := sourceBatch.Results[index]
			blockRawID, storeErr := k.storeImportBytes(ctx, "notion-block-"+blockID+".json", blockRaw)
			if storeErr != nil {
				return s, storeErr
			}
			blockEvidence := map[string]any{"response": provenance, "sourceBlockId": blockID, "sourceParentId": task.ID, "sourceType": kind, "sourceData": data}
			item := ImportItem{Locator: fmt.Sprintf("notion/blocks/%s#%d", blockID, index), Kind: "notion_block", Outcome: "adapted", Reason: "Supported structure, text and marks reconstructed; full block fields retained in API response evidence", RawFileID: blockRawID, Hash: importDigest(blockRaw), PageID: task.PageID, ParentID: task.ParentID, Title: "Notion content", ExternalID: blockID, IdentityKind: "external_id", Provenance: blockEvidence, Blocks: []ImportBlock{}}
			canonical := ImportBlock{ID: NewID(), Type: kind, Data: map[string]any{"richText": notionRichText(data["rich_text"])}}
			switch kind {
			case "paragraph", "heading_1", "heading_2", "heading_3", "quote", "bulleted_list_item", "numbered_list_item", "toggle", "callout":
				if strings.HasPrefix(kind, "heading_") && data["is_toggleable"] == true {
					canonical.Type = "toggle_heading"
					item.Outcome = "partial"
					item.Reason += "; toggle ownership reconstructed; original heading level remains in source evidence"
				}
				item.Blocks = append(item.Blocks, canonical)
			case "column_list":
				item.Blocks = append(item.Blocks, ImportBlock{canonical.ID, "columns", map[string]any{"columns": []any{}}})
			case "column":
				blockEvidence["groupId"] = NewID()
			case "synced_block":
				if data["synced_from"] == nil {
					item.Blocks = append(item.Blocks, ImportBlock{canonical.ID, "paragraph", map[string]any{"richText": []any{}}})
				} else {
					item.Blocks = append(item.Blocks, fallbackImportBlock(&item, "Supplied reusable-content reference: "+string(marshal(data["synced_from"]))))
				}
			case "code":
				text := ""
				for _, span := range notionRichText(data["rich_text"]) {
					text += str(span.(map[string]any)["text"])
				}
				item.Blocks = append(item.Blocks, ImportBlock{NewID(), "code", map[string]any{"source": text, "language": str(data["language"])}})
			case "divider":
				item.Blocks = append(item.Blocks, ImportBlock{NewID(), "divider", map[string]any{}})
			case "image", "file", "pdf", "audio", "video":
				location, _ := data[str(data["type"])].(map[string]any)
				attachmentURL := str(location["url"])
				if notionAttachmentURL(attachmentURL) {
					bytes, _, er := fetchNotion(bounded, client, attachmentURL, "")
					if er == nil && s.Bytes+int64(len(bytes)) <= 128<<20 {
						fileID, er := k.storeImportBytes(ctx, "notion-attachment-"+blockID, bytes)
						if er != nil {
							return s, er
						}
						s.Bytes += int64(len(bytes))
						item.Blocks = append(item.Blocks, ImportBlock{NewID(), kind, map[string]any{"fileId": fileID, "caption": notionRichText(data["caption"])}})
						asset := ImportItem{Locator: "notion/attachments/" + blockID, Kind: "asset", Outcome: "preserved", Reason: "Attachment downloaded and hash-verified for offline use; API URL remains in source evidence", RawFileID: fileID, Hash: importDigest(bytes), Size: int64(len(bytes)), Provenance: provenance}
						p.Items = append(p.Items, asset)
					} else {
						item.Outcome = "partial"
						item.Reason = "Attachment download pending; resume refreshes its source reference before retry"
						next = append(next, notionCaptureTask{Kind: "attachment", ID: blockID, PageID: task.PageID})
					}
				} else {
					item.Outcome = "partial"
					item.Reason = "External attachment host is outside the Notion storage allowlist; original URL retained, offline bytes unavailable"
				}
				if len(item.Blocks) == 0 {
					item.Blocks = append(item.Blocks, fallbackImportBlock(&item, "Attachment unavailable offline: "+kind))
				}
			case "child_database":
				childPage := notionLocalPage(&s, &p, "database", blockID)
				notionEnqueue(&s, notionCaptureTask{Kind: "database", ID: blockID, PageID: childPage, ParentID: task.PageID, Depth: task.Depth + 1})
				item.Blocks = append(item.Blocks, ImportBlock{NewID(), "paragraph", map[string]any{"richText": []any{map[string]any{"text": str(data["title"]), "link": "trove://page/" + childPage}}}})
			case "child_page":
				childPage := notionLocalPage(&s, &p, "page", blockID)
				notionEnqueue(&s, notionCaptureTask{Kind: "page", ID: blockID, PageID: childPage, ParentID: task.PageID, Depth: task.Depth + 1})
				item.Blocks = append(item.Blocks, ImportBlock{NewID(), "paragraph", map[string]any{"richText": []any{map[string]any{"text": str(data["title"]), "link": "trove://page/" + childPage}}}})
			default:
				item.Outcome = "unsupported_but_retained"
				item.Reason = "Unsupported block/schema/view behavior retained as readable source evidence; no calculated result or relation was invented"
				item.Blocks = append(item.Blocks, fallbackImportBlock(&item, string(blockRaw)))
			}
			for _, field := range []string{"rich_text", "caption"} {
				spans, _ := data[field].([]any)
				for _, rawSpan := range spans {
					span, _ := rawSpan.(map[string]any)
					if span["type"] == "mention" {
						item.Outcome = "partial"
						item.Reason += "; supplied mention/identity semantics remain in source evidence; readable labels preserved"
						break
					}
				}
			}
			if block["has_children"] == true && kind != "child_page" && kind != "child_database" {
				// A synced reference resolves only to content inside this authorized
				// capture. It never expands access by fetching a referenced source.
				if kind != "synced_block" || data["synced_from"] == nil {
					next = append(next, notionCaptureTask{Kind: "children", ID: blockID, PageID: task.PageID, ParentID: task.ParentID, Depth: task.Depth + 1})
				}
			}
			p.Items = append(p.Items, item)
		}
		if response["has_more"] == true {
			cursor := str(response["next_cursor"])
			if cursor == "" || cursor == task.Cursor || len(cursor) > 4096 || s.Seen[notionCursorKey(task, cursor)] {
				return s, Fail("INVALID_REQUEST", "Notion pagination cursor is invalid or repeated")
			}
			task.Cursor = cursor
			next = append(next, task)
		}
		for _, task := range next {
			if task.Depth > 64 {
				return s, Fail("RESOURCE_EXHAUSTED", "Notion nesting exceeds 64 levels")
			}
		}
		s.Pending = append(next, s.Pending...)
	}
	for _, pending := range s.Pending {
		if pending.Depth > 64 {
			return s, Fail("RESOURCE_EXHAUSTED", "Notion nesting exceeds 64 levels")
		}
	}
	if len(p.Items) > 10000 || len(s.Pending) > 10000 {
		return s, Fail("RESOURCE_EXHAUSTED", "Capture exceeds 10,000 supplied items")
	}
	if len(s.Pending) == 0 {
		s.State = "ready"
	}
	p.Hash = importPlanHash(p)
	err = k.write(ctx, func(tx *sql.Tx) error {
		if err := requireOwnerScope(k, e); err != nil {
			return err
		}
		if err := saveNotionCapture(ctx, tx, s, revision); err != nil {
			return err
		}
		return saveImportPlan(ctx, tx, p)
	})
	return s, err
}
func finishNotionCapture(ctx context.Context, k *Kernel, e contracts.ServiceExecutionContext, r map[string]any) (any, error) {
	if err := requireOwnerScope(k, e); err != nil {
		return nil, err
	}
	var p ImportPlan
	err := k.write(ctx, func(tx *sql.Tx) error {
		s, err := loadNotionCapture(ctx, tx, str(r["captureId"]))
		if err != nil {
			return err
		}
		if s.Revision != int(number(r["revision"])) {
			return Fail("CONFLICT", "Capture cursor changed")
		}
		p, err = loadImportPlan(ctx, tx, s.PlanID)
		if err != nil {
			return err
		}
		if s.State == "finished" {
			return nil
		}
		if len(s.Pending) > 0 && r["allowPartial"] != true {
			return Fail("CONFLICT", "Capture is incomplete; explicitly choose a partial import or resume")
		}
		// Reconstruct source relationships before normal re-import conflict
		// handling. Per-block evidence remains separately reconciled.
		pageItems := map[string]int{}
		for i := range p.Items {
			if p.Items[i].Kind == "notion_page" {
				pageItems[p.Items[i].PageID] = i
			}
		}
		reconstructNotionStructure(&p, pageItems)
		if err = k.reconstructNotionDatabases(ctx, tx, &s, &p); err != nil {
			return err
		}
		for i := range p.Items {
			if p.Items[i].Kind == "notion_page" || p.Items[i].Kind == "notion_template" || p.Items[i].Kind == "portable_collection" || p.Items[i].Kind == "portable_record" || p.Items[i].Kind == "notion_container" {
				pageItems[p.Items[i].PageID] = i
			}
		}
		remap := map[string]string{}
		for _, i := range pageItems {
			item := &p.Items[i]
			var mapping, local, base, revision string
			er := tx.QueryRowContext(ctx, `SELECT m.id,m.local_id,m.base_content_hash,COALESCE(p.revision,'0') FROM import_mappings m LEFT JOIN pages p ON p.id=m.local_id WHERE m.workspace_id=? AND m.source_system=? AND m.source_namespace=? AND m.external_kind='page' AND m.identity_kind='external_id' AND m.identity_value=?`, k.id, p.SourceSystem, p.Namespace, item.ExternalID).Scan(&mapping, &local, &base, &revision)
			if er == nil {
				remap[item.PageID] = local
				item.PageID = local
				item.MappingID = mapping
				item.ExpectedRevision = revision
				var importedRevision string
				_ = tx.QueryRowContext(ctx, "SELECT local_revision FROM import_mapping_state WHERE mapping_id=?", mapping).Scan(&importedRevision)
				if base == item.Hash && importedRevision == revision {
					item.Choice = "keep_local"
					item.Reason += "; identical supplied source and unchanged local page"
				} else {
					item.Conflict = "Existing Notion page: keep local, replace its captured body, or import a separate copy"
				}
			} else if er != sql.ErrNoRows {
				return er
			}
		}
		for i := range p.Items {
			item := &p.Items[i]
			if id := remap[item.ParentID]; id != "" {
				item.ParentID = id
			}
			for b := range item.Blocks {
				rewriteImportBlock(item.Blocks[b].Data, remap)
			}
			if err := reconcileImportBlockIDs(ctx, tx, item); err != nil {
				return err
			}
		}
		remapPortableDatabasePages(&p, remap)
		for i := range p.NotionContainers {
			if id := remap[p.NotionContainers[i].PageID]; id != "" {
				p.NotionContainers[i].PageID = id
			}
		}
		setNotionGraphConflicts(&p)
		for _, task := range s.Pending {
			item := ImportItem{Locator: "notion/pending/" + task.ID, Kind: "rejected", Outcome: "failed", Reason: "Capture finished before this authorized source could be read: " + s.Error, RawFileID: p.SourceFileID}
			if task.Kind == "page" {
				item.Kind = "notion_page"
				item.IdentityKind = "derived_locator"
				item.ExternalID = item.Locator
				item.Hash = importDigest(marshal(task))
				item.PageID = task.PageID
				item.ParentID = task.ParentID
				item.Title = "Unavailable Notion page " + task.ID
				item.Blocks = []ImportBlock{fallbackImportBlock(&item, item.Reason)}
			}
			p.Items = append(p.Items, item)
		}
		if len(p.Items) > 10000 {
			return Fail("RESOURCE_EXHAUSTED", "Import review exceeds 10,000 items; capture fewer roots")
		}
		p.State = "preview"
		p.Hash = importPlanHash(p)
		previous := s.Revision
		s.Revision++
		s.State = "finished"
		if err = saveNotionCapture(ctx, tx, s, previous); err != nil {
			return err
		}
		return saveImportPlan(ctx, tx, p)
	})
	return p, err
}
