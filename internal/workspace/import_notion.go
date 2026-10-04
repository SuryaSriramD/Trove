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

const notionCaptureVersion = "2025-09-03"

type notionCaptureTask struct {
	Kind     string `json:"kind"`
	ID       string `json:"id"`
	PageID   string `json:"pageId"`
	ParentID string `json:"parentId"`
	Cursor   string `json:"cursor,omitempty"`
	Depth    int    `json:"depth"`
}
type NotionCapture struct {
	ID        string              `json:"captureId"`
	Revision  int                 `json:"revision"`
	State     string              `json:"state"`
	Roots     []string            `json:"authorizedRoots"`
	PlanID    string              `json:"planId"`
	Pending   []notionCaptureTask `json:"pending"`
	Seen      map[string]bool     `json:"seen"`
	Completed int                 `json:"completed"`
	Bytes     int64               `json:"bytes"`
	Error     string              `json:"error,omitempty"`
}

var notionID = regexp.MustCompile(`^[0-9a-fA-F]{32}$`)

func init() {
	RegisterInitializer(func(ctx context.Context, k *Kernel) error {
		return k.write(ctx, func(tx *sql.Tx) error {
			_, err := tx.ExecContext(ctx, "CREATE TABLE IF NOT EXISTS notion_captures(id TEXT PRIMARY KEY,revision INTEGER NOT NULL,state_json TEXT NOT NULL)")
			return err
		})
	})
	RegisterQuery("imports.notion.start", startNotionCapture)
	RegisterQuery("imports.notion.status", getNotionCapture)
	RegisterQuery("imports.notion.list", func(ctx context.Context, k *Kernel, e contracts.ServiceExecutionContext, r map[string]any) (any, error) {
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
	})
	RegisterQuery("imports.notion.step", func(ctx context.Context, k *Kernel, e contracts.ServiceExecutionContext, r map[string]any) (any, error) {
		return k.CaptureNotionStep(ctx, e, str(r["captureId"]), int(number(r["revision"])), str(r["token"]), nil)
	})
	RegisterQuery("imports.notion.finish", finishNotionCapture)
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
	roots, _ := r["rootIds"].([]any)
	if len(roots) < 1 || len(roots) > 20 {
		return nil, Fail("INVALID_REQUEST", "Choose 1–20 explicit Notion page root IDs")
	}
	namespace := strings.TrimSpace(str(r["sourceNamespace"]))
	if namespace == "" || len(namespace) > 400 {
		return nil, Fail("INVALID_REQUEST", "Choose a stable source namespace up to 400 characters")
	}
	s := NotionCapture{ID: NewID(), Revision: 1, State: "capturing", Roots: []string{}, Pending: []notionCaptureTask{}, Seen: map[string]bool{}}
	p := ImportPlan{ID: NewID(), Namespace: namespace, SourceSystem: "notion_api", SourceName: "Notion API capture", RootID: NewID(), State: "capturing", Items: []ImportItem{}, SourceCompleteness: "partial", SourceOmissions: []string{"Only explicitly selected page roots and their supplied descendants are captured. Comments, permissions, history, and database definitions unavailable from the page/block endpoints are not inferred."}, UnknownCompleteness: []string{"API authorization may omit inaccessible material. Unsupported supplied fields are retained in raw response evidence."}, CommittedBatchIDs: []string{}, CreatedAt: now()}
	s.PlanID = p.ID
	for _, raw := range roots {
		id := strings.ToLower(strings.ReplaceAll(str(raw), "-", ""))
		if !notionID.MatchString(id) {
			return nil, Fail("INVALID_REQUEST", "Notion roots must be page UUIDs")
		}
		if s.Seen["root:"+id] {
			continue
		}
		s.Seen["root:"+id] = true
		s.Roots = append(s.Roots, id)
		s.Pending = append(s.Pending, notionCaptureTask{Kind: "page", ID: id, PageID: NewID(), ParentID: p.RootID})
	}
	var err error
	p.SourceFileID, err = k.storeImportBytes(ctx, "notion-authorized-roots.json", marshal(map[string]any{"authorizedRoots": s.Roots, "apiVersion": notionCaptureVersion, "capturedAt": now()}))
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
	for attempt := 0; attempt < 3; attempt++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
		if err != nil {
			return nil, 0, err
		}
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
			req.Header.Set("Notion-Version", notionCaptureVersion)
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
		marks := []string{}
		for key, mark := range map[string]string{"bold": "bold", "italic": "italic", "strikethrough": "strike", "code": "code"} {
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
	endpoint := "https://api.notion.com/v1/pages/" + task.ID
	if task.Kind == "children" {
		endpoint = "https://api.notion.com/v1/blocks/" + task.ID + "/children?page_size=100"
		if task.Cursor != "" {
			endpoint += "&start_cursor=" + url.QueryEscape(task.Cursor)
		}
	}
	bounded, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	client = notionHTTPClient(client)
	raw, status, fetchErr := fetchNotion(bounded, client, endpoint, token)
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
		}
		err = k.write(ctx, func(tx *sql.Tx) error { return saveNotionCapture(ctx, tx, s, revision) })
		return s, err
	}
	var response map[string]any
	if err = json.Unmarshal(raw, &response); err != nil {
		return s, Fail("INVALID_REQUEST", "Notion returned invalid JSON; cursor retained")
	}
	s.Pending = s.Pending[1:]
	s.Seen["cursor:"+task.Kind+":"+task.ID+":"+task.Cursor] = true
	s.Completed++
	s.Bytes += int64(len(raw))
	rawID, err := k.storeImportBytes(ctx, "notion-"+task.ID+".json", raw)
	if err != nil {
		return s, err
	}
	provenance := map[string]any{"channel": "notion_api", "apiVersion": notionCaptureVersion, "sourceId": task.ID, "cursor": task.Cursor, "capturedAt": now(), "responseFileId": rawID}
	if task.Kind == "page" {
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
		if len(properties) > 0 {
			item.Blocks = append(item.Blocks, fallbackImportBlock(&item, "Captured property values (source snapshots, not freshly calculated results):\n"+string(marshal(properties))))
		}
		p.Items = append(p.Items, item)
		s.Pending = append([]notionCaptureTask{{Kind: "children", ID: task.ID, PageID: task.PageID, ParentID: task.ParentID, Depth: task.Depth}}, s.Pending...)
	} else {
		results, ok := response["results"].([]any)
		if !ok {
			return s, Fail("INVALID_REQUEST", "Notion children response has no results list; cursor retained")
		}
		if len(results) > 100 {
			return s, Fail("RESOURCE_EXHAUSTED", "Notion returned more than requested 100 blocks")
		}
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
			if s.Seen["block:"+blockID] {
				continue
			}
			s.Seen["block:"+blockID] = true
			kind := str(block["type"])
			data, _ := block[kind].(map[string]any)
			item := ImportItem{Locator: fmt.Sprintf("notion/blocks/%s#%d", blockID, index), Kind: "notion_block", Outcome: "adapted", Reason: "Supported text and marks reconstructed; full block fields retained in API response evidence", RawFileID: rawID, Hash: importDigest(marshal(block)), PageID: task.PageID, ParentID: task.ParentID, Title: "Notion content", Provenance: provenance, Blocks: []ImportBlock{}}
			canonical := ImportBlock{ID: NewID(), Type: kind, Data: map[string]any{"richText": notionRichText(data["rich_text"])}}
			switch kind {
			case "paragraph", "heading_1", "heading_2", "heading_3", "quote", "bulleted_list_item", "numbered_list_item":
				item.Blocks = append(item.Blocks, canonical)
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
						item.Reason = "Attachment download failed or exceeded budget; URL and source response retained"
					}
				} else {
					item.Outcome = "partial"
					item.Reason = "External attachment host is outside the Notion storage allowlist; original URL retained, offline bytes unavailable"
				}
				if len(item.Blocks) == 0 {
					item.Blocks = append(item.Blocks, fallbackImportBlock(&item, "Attachment unavailable offline: "+kind))
				}
			case "child_page":
				childPage := NewID()
				next = append(next, notionCaptureTask{Kind: "page", ID: blockID, PageID: childPage, ParentID: task.PageID, Depth: task.Depth + 1})
				item.Blocks = append(item.Blocks, ImportBlock{NewID(), "paragraph", map[string]any{"richText": []any{map[string]any{"text": str(data["title"]), "link": "trove://page/" + childPage}}}})
			default:
				item.Outcome = "unsupported_but_retained"
				item.Reason = "Unsupported block/schema/view behavior retained as readable source evidence; no calculated result or relation was invented"
				item.Blocks = append(item.Blocks, fallbackImportBlock(&item, string(marshal(block))))
			}
			if block["has_children"] == true && kind != "child_page" {
				next = append(next, notionCaptureTask{Kind: "children", ID: blockID, PageID: task.PageID, ParentID: task.ParentID, Depth: task.Depth + 1})
				item.Outcome = "partial"
				item.Reason += "; nested child content flattened with parent identity retained in API evidence"
			}
			p.Items = append(p.Items, item)
		}
		if response["has_more"] == true {
			cursor := str(response["next_cursor"])
			if cursor == "" || cursor == task.Cursor || len(cursor) > 4096 || s.Seen["cursor:"+task.Kind+":"+task.ID+":"+cursor] {
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
	if len(p.Items) > 10000 {
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
		// Collapse content into each canonical page before normal re-import
		// conflict handling. Per-block evidence remains separately reconciled.
		pageItems := map[string]int{}
		for i := range p.Items {
			if p.Items[i].Kind == "notion_page" {
				pageItems[p.Items[i].PageID] = i
			}
		}
		for i := range p.Items {
			item := &p.Items[i]
			if item.Kind == "notion_block" {
				if at, ok := pageItems[item.PageID]; ok {
					p.Items[at].Blocks = append(p.Items[at].Blocks, item.Blocks...)
					item.Blocks = nil
					item.Kind = "asset"
					item.PageID = ""
					item.ParentID = ""
				}
			}
		}
		remap := map[string]string{}
		for _, i := range pageItems {
			item := &p.Items[i]
			item.Hash = importDigest(marshal(map[string]any{"sourceHash": item.Hash, "blocks": item.Blocks}))
			var mapping, local, base, revision string
			er := tx.QueryRowContext(ctx, `SELECT m.id,m.local_id,m.base_content_hash,COALESCE(p.revision,'0') FROM import_mappings m LEFT JOIN pages p ON p.id=m.local_id WHERE m.workspace_id=? AND m.source_system=? AND m.source_namespace=? AND m.external_kind='page' AND m.identity_kind='external_id' AND m.identity_value=?`, k.id, p.SourceSystem, p.Namespace, item.ExternalID).Scan(&mapping, &local, &base, &revision)
			if er == nil {
				remap[item.PageID] = local
				item.PageID = local
				item.MappingID = mapping
				item.ExpectedRevision = revision
				item.Conflict = "Existing Notion page: keep local, replace its captured body, or import a separate copy"
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
		}
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
