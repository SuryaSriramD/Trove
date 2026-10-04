package workspace

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"trove.local/phase0/contracts"
)

// Objects are durable capture state, not live domain objects. Exact API bytes
// remain immutable evidence; property pages append only after the cursor CAS.
type notionCapturedObject struct {
	QueryComplete bool                              `json:"queryComplete,omitempty"`
	Task          notionCaptureTask                 `json:"task"`
	Raw           json.RawMessage                   `json:"raw"`
	RawFileID     string                            `json:"rawFileId"`
	Properties    map[string]notionCapturedProperty `json:"properties,omitempty"`
	Files         map[string]string                 `json:"files,omitempty"`
}
type notionCapturedProperty struct {
	Type     string            `json:"type"`
	Items    []json.RawMessage `json:"items"`
	Value    json.RawMessage   `json:"value,omitempty"`
	Complete bool              `json:"complete"`
}

func notionNormalizeID(id string) string { return strings.ToLower(strings.ReplaceAll(id, "-", "")) }
func notionRoots(r map[string]any) ([]notionCaptureTask, error) {
	list, _ := r["roots"].([]any)
	if list == nil {
		for _, v := range asList(r["rootIds"]) {
			list = append(list, map[string]any{"kind": "page", "id": v})
		}
	}
	if len(list) < 1 || len(list) > 20 {
		return nil, Fail("INVALID_REQUEST", "Choose 1–20 explicit Notion page, database or data source roots")
	}
	out := []notionCaptureTask{}
	for _, v := range list {
		obj := asObject(v)
		kind, id := str(obj["kind"]), notionNormalizeID(str(obj["id"]))
		if !notionID.MatchString(id) || (kind != "page" && kind != "database" && kind != "data_source") {
			return nil, Fail("INVALID_REQUEST", "Each root needs a supported kind and a Notion UUID")
		}
		out = append(out, notionCaptureTask{Kind: kind, ID: id})
	}
	return out, nil
}
func asObject(v any) map[string]any { out, _ := v.(map[string]any); return out }
func asList(v any) []any            { out, _ := v.([]any); return out }
func notionCursorKey(t notionCaptureTask, cursor string) string {
	return "cursor:" + t.Kind + ":" + t.ID + ":" + t.PropertyID + ":" + cursor
}
func notionEnqueue(s *NotionCapture, t notionCaptureTask) {
	key := "queued:" + t.Kind + ":" + t.ID + ":" + t.PropertyID + fmt.Sprintf(":%d", t.FileIndex)
	if s.Seen[key] {
		return
	}
	s.Seen[key] = true
	s.Pending = append(s.Pending, t)
}
func notionPropertyPath(id string) (string, error) {
	// Notion returns already-encoded property IDs. Decode once, then encode as
	// exactly one path segment, avoiding double-encoding and path/query injection.
	decoded, err := url.PathUnescape(id)
	if err != nil || decoded == "" || len(decoded) > 512 || strings.ContainsAny(decoded, "\x00\r\n") {
		return "", Fail("INVALID_REQUEST", "Invalid property identity")
	}
	return url.PathEscape(decoded), nil
}
func notionEndpoint(t notionCaptureTask) (string, []byte, error) {
	base := "https://api.notion.com/v1/"
	path := "pages/" + t.ID
	query := url.Values{}
	var body []byte
	switch t.Kind {
	case "page":
	case "children":
		path = "blocks/" + t.ID + "/children"
		query.Set("page_size", "100")
	case "database":
		path = "databases/" + t.ID
	case "data_source":
		path = "data_sources/" + t.ID
	case "templates":
		path = "data_sources/" + t.ID + "/templates"
		query.Set("page_size", "100")
	case "query":
		path = "data_sources/" + t.ID + "/query"
		b := map[string]any{"page_size": 100}
		if t.Cursor != "" {
			b["start_cursor"] = t.Cursor
		}
		body = marshal(b)
	case "views":
		path = "views"
		if t.PropertyID == "data_source" {
			query.Set("data_source_id", t.ID)
		} else {
			query.Set("database_id", t.ID)
		}
		query.Set("page_size", "100")
	case "view":
		path = "views/" + t.ID
	case "property":
		prop, err := notionPropertyPath(t.PropertyID)
		if err != nil {
			return "", nil, err
		}
		path = "pages/" + t.ID + "/properties/" + prop
		query.Set("page_size", "100")
	case "attachment":
		path = "blocks/" + t.ID
		if t.PropertyID != "" {
			prop, err := notionPropertyPath(t.PropertyID)
			if err != nil {
				return "", nil, err
			}
			path = "pages/" + t.ID + "/properties/" + prop
		}
	default:
		return "", nil, Fail("INVALID_REQUEST", "Unknown capture task")
	}
	if t.Cursor != "" && body == nil {
		query.Set("start_cursor", t.Cursor)
	}
	if len(query) > 0 {
		path += "?" + query.Encode()
	}
	return base + path, body, nil
}
func notionNext(s *NotionCapture, t notionCaptureTask, response map[string]any) error {
	if response["has_more"] != true {
		return nil
	}
	cursor := str(response["next_cursor"])
	if cursor == "" || len(cursor) > 4096 || cursor == t.Cursor || s.Seen[notionCursorKey(t, cursor)] {
		return Fail("INVALID_REQUEST", "Invalid or repeated Notion pagination cursor; previous cursor retained")
	}
	t.Cursor = cursor
	s.Pending = append(s.Pending, t)
	return nil
}
func notionObject(raw []byte) map[string]any {
	var v map[string]any
	d := json.NewDecoder(strings.NewReader(string(raw)))
	d.UseNumber()
	_ = d.Decode(&v)
	return v
}
func captureNotionRecord(s *NotionCapture, t notionCaptureTask, response map[string]any, raw []byte, rawID string) error {
	if notionNormalizeID(str(response["id"])) != t.ID || response["object"] != "page" {
		return Fail("INVALID_REQUEST", "Unexpected captured record identity")
	}
	if parent := notionNormalizeID(str(asObject(response["parent"])["data_source_id"])); parent != "" && parent != t.SourceID {
		return Fail("INVALID_REQUEST", "Record no longer belongs to the captured data source; retry capture")
	}
	obj := notionCapturedObject{Task: t, Raw: append([]byte{}, raw...), RawFileID: rawID, Properties: map[string]notionCapturedProperty{}, Files: map[string]string{}}
	props := asObject(response["properties"])
	keys := []string{}
	for key := range props {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		p := asObject(props[key])
		id, kind := str(p["id"]), str(p["type"])
		if id == "" {
			continue
		}
		if _, err := notionPropertyPath(id); err != nil {
			return err
		}
		switch kind {
		case "title", "rich_text", "people", "relation", "rollup", "formula", "files":
			notionEnqueue(s, notionCaptureTask{Kind: "property", ID: t.ID, SourceID: t.SourceID, PropertyID: id, PageID: t.PageID, ParentID: t.ParentID})
		}
	}
	s.Objects["record:"+t.ID] = obj
	return nil
}
func (k *Kernel) captureNotionObject(ctx context.Context, s *NotionCapture, p *ImportPlan, t notionCaptureTask, response map[string]any, raw []byte, rawID string, provenance map[string]any) error {
	locator := "notion/" + t.Kind + "/" + t.ID
	if t.PropertyID != "" {
		locator += "/" + t.PropertyID
	}
	locator += "#" + t.Cursor
	evidence := ImportItem{Locator: locator, Kind: "asset", Outcome: "preserved", Reason: "Complete supplied API response retained as inert evidence", RawFileID: rawID, Hash: importDigest(raw), Size: int64(len(raw)), Provenance: provenance}
	p.Items = append(p.Items, evidence)
	if t.Kind == "attachment" {
		return nil
	}
	switch t.Kind {
	case "database", "data_source", "view":
		if notionNormalizeID(str(response["id"])) != t.ID || str(response["object"]) != t.Kind {
			return Fail("INVALID_REQUEST", "Notion returned an unexpected object identity or kind")
		}
		if response["in_trash"] == true || response["archived"] == true {
			p.Items[len(p.Items)-1].Outcome = "unsupported_but_retained"
			p.Items[len(p.Items)-1].Reason = "Trashed source retained without activation"
			return nil
		}
		s.Objects[t.Kind+":"+t.ID] = notionCapturedObject{Task: t, Raw: append([]byte{}, raw...), RawFileID: rawID}
		if t.Kind == "view" {
			return nil
		}
		title := plainNotionText(response["title"])
		if title == "" {
			title = str(response["name"])
		}
		if title == "" {
			title = "Notion " + t.Kind
		}
		item := evidence
		item.Locator = "notion/" + t.Kind + "/" + t.ID
		item.Kind = "notion_" + t.Kind
		item.PageID = t.PageID
		item.ParentID = t.ParentID
		item.Title = title
		item.ExternalID = t.Kind + ":" + t.ID
		item.IdentityKind = "external_id"
		item.Blocks = []ImportBlock{}
		p.Items = append(p.Items, item)
		if t.Kind == "database" {
			sources, ok := response["data_sources"].([]any)
			if !ok {
				return Fail("INVALID_REQUEST", "Database omitted data_sources; authorization is not an empty database")
			}
			for _, v := range sources {
				id := notionNormalizeID(str(asObject(v)["id"]))
				if !notionID.MatchString(id) {
					return Fail("INVALID_REQUEST", "Invalid source identity")
				}
				notionEnqueue(s, notionCaptureTask{Kind: "data_source", ID: id, SourceID: t.ID, PageID: NewID(), ParentID: t.PageID, Depth: t.Depth + 1})
			}
			notionEnqueue(s, notionCaptureTask{Kind: "views", ID: t.ID, PageID: t.PageID, Depth: t.Depth})
		} else {
			if _, ok := response["properties"].(map[string]any); !ok {
				return Fail("INVALID_REQUEST", "Data source omitted its property schema")
			}
			notionEnqueue(s, notionCaptureTask{Kind: "query", ID: t.ID, PageID: t.PageID, Depth: t.Depth})
			notionEnqueue(s, notionCaptureTask{Kind: "templates", ID: t.ID, PageID: t.PageID, Depth: t.Depth})
			if t.SourceID == "" {
				notionEnqueue(s, notionCaptureTask{Kind: "views", ID: t.ID, PropertyID: "data_source", PageID: t.PageID})
			}
		}
	case "templates":
		templates, ok := response["templates"].([]any)
		if !ok || len(templates) > 100 {
			return Fail("INVALID_REQUEST", "Template endpoint omitted its bounded inventory")
		}
		if _, ok := response["has_more"].(bool); !ok {
			return Fail("INVALID_REQUEST", "Template inventory omitted pagination state")
		}
		for _, v := range templates {
			template := asObject(v)
			id := notionNormalizeID(str(template["id"]))
			if !notionID.MatchString(id) {
				return Fail("INVALID_REQUEST", "Invalid source template identity")
			}
			if err := notionEnqueuePage(s, p, notionCaptureTask{Kind: "page", ID: id, SourceID: t.ID, Template: true, PageID: NewID(), ParentID: t.PageID, Depth: t.Depth + 1}); err != nil {
				return err
			}
		}
		p.Items[len(p.Items)-1].Reason = "Template inventory and default designation retained; no template or recurring behavior is activated"
		return notionNext(s, t, response)
	case "query", "views":
		if _, ok := response["has_more"].(bool); !ok || response["object"] != "list" {
			return Fail("INVALID_REQUEST", "Paginated endpoint omitted its completion indicator")
		}
		results, ok := response["results"].([]any)
		if !ok || len(results) > 100 {
			return Fail("INVALID_REQUEST", "Paginated response omitted its bounded results list")
		}
		for _, v := range results {
			item := asObject(v)
			id := notionNormalizeID(str(item["id"]))
			if !notionID.MatchString(id) {
				return Fail("INVALID_REQUEST", "Invalid paginated source identity")
			}
			if t.Kind == "views" {
				notionEnqueue(s, notionCaptureTask{Kind: "view", ID: id, SourceID: t.ID, PageID: t.PageID})
				continue
			}
			if item["object"] != "page" {
				p.Items[len(p.Items)-1].Outcome = "partial"
				p.Items[len(p.Items)-1].Reason = "Non-page query result remains source evidence"
				continue
			}
			if err := notionEnqueuePage(s, p, notionCaptureTask{Kind: "page", ID: id, SourceID: t.ID, PageID: NewID(), ParentID: t.PageID, Depth: t.Depth + 1}); err != nil {
				return err
			}
		}
		status := asObject(response["request_status"])
		if t.Kind == "query" && response["has_more"] != true && (status["type"] == nil || status["type"] == "complete") {
			obj := s.Objects["data_source:"+t.ID]
			obj.QueryComplete = true
			s.Objects["data_source:"+t.ID] = obj
		}
		if kind := str(status["type"]); kind != "" && kind != "complete" {
			p.SourceOmissions = append(p.SourceOmissions, fmt.Sprintf("%s %s query is incomplete: %s", t.Kind, t.ID, str(status["incomplete_reason"])))
			p.Items[len(p.Items)-1].Outcome = "partial"
		}
		return notionNext(s, t, response)
	case "property":
		key := "record:" + t.ID
		record, ok := s.Objects[key]
		if !ok {
			return Fail("INVALID_REQUEST", "Property task has no captured record")
		}
		if record.Properties == nil {
			record.Properties = map[string]notionCapturedProperty{}
		}
		prop := record.Properties[t.PropertyID]
		obj := notionObject(raw)
		if response["object"] == "list" {
			if _, ok := response["has_more"].(bool); !ok {
				return Fail("INVALID_REQUEST", "Property pagination omitted its completion indicator")
			}
			meta := asObject(obj["property_item"])
			kind := str(meta["type"])
			if kind == "" {
				kind = str(asObject(firstNotionResult(obj))["type"])
			}
			if prop.Type != "" && prop.Type != kind {
				return Fail("INVALID_REQUEST", "Property type changed during pagination")
			}
			prop.Type = kind
			if supplied := str(meta["id"]); supplied != "" && !notionSamePropertyID(supplied, t.PropertyID) {
				return Fail("INVALID_REQUEST", "Property cursor returned a different identity")
			}
			results, ok := obj["results"].([]any)
			if !ok || len(results) > 100 {
				return Fail("INVALID_REQUEST", "Invalid property item list")
			}
			for _, v := range results {
				if id := str(asObject(v)["id"]); id != "" && !notionSamePropertyID(id, t.PropertyID) {
					return Fail("INVALID_REQUEST", "Property list item has a different identity")
				}
				prop.Items = append(prop.Items, marshal(v))
			}
			if len(prop.Items) > 10000 {
				return Fail("RESOURCE_EXHAUSTED", "Property exceeds 10,000 captured items")
			}
			if kind == "rollup" {
				prop.Value = marshal(meta["rollup"])
			}
			prop.Complete = response["has_more"] != true
			if err := notionNext(s, t, response); err != nil {
				return err
			}
		} else if response["object"] == "property_item" {
			if !notionSamePropertyID(str(obj["id"]), t.PropertyID) {
				return Fail("INVALID_REQUEST", "Property endpoint returned a different identity")
			}
			prop.Type = str(obj["type"])
			prop.Value = marshal(obj[prop.Type])
			prop.Complete = true
		} else {
			return Fail("INVALID_REQUEST", "Property endpoint returned an unexpected wrapper")
		}
		record.Properties[t.PropertyID] = prop
		s.Objects[key] = record
		if prop.Type == "files" {
			for i := range asList(obj["files"]) {
				notionEnqueue(s, notionCaptureTask{Kind: "attachment", ID: t.ID, SourceID: t.SourceID, PropertyID: t.PropertyID, FileIndex: i, PageID: t.PageID})
			}
		}
	}
	return nil
}
func firstNotionResult(v map[string]any) any {
	list := asList(v["results"])
	if len(list) > 0 {
		return list[0]
	}
	return nil
}
func plainNotionText(v any) string {
	var out strings.Builder
	for _, span := range notionRichText(v) {
		out.WriteString(str(asObject(span)["text"]))
	}
	return out.String()
}
func (k *Kernel) resolveNotionAttachment(ctx context.Context, client *http.Client, s *NotionCapture, p *ImportPlan, t notionCaptureTask, raw []byte) error {
	source := notionObject(raw)
	kind := str(source["type"])
	data := asObject(source[kind])
	var caption []any
	if t.PropertyID != "" {
		if source["object"] != "property_item" || source["type"] != "files" || !notionSamePropertyID(str(source["id"]), t.PropertyID) {
			return Fail("INVALID_REQUEST", "Attachment refresh returned a different property")
		}
		files := asList(source["files"])
		if t.FileIndex >= len(files) {
			return Fail("CONFLICT", "File property changed while capture was paused")
		}
		data = asObject(files[t.FileIndex])
		original := s.Objects["record:"+t.ID].Properties[t.PropertyID]
		var supplied []any
		_ = json.Unmarshal(original.Value, &supplied)
		if t.FileIndex >= len(supplied) || !notionSameFile(asObject(supplied[t.FileIndex]), data) {
			return Fail("CONFLICT", "Source attachment changed; recapture or finish partial")
		}
	} else {
		if notionNormalizeID(str(source["id"])) != t.ID {
			return Fail("INVALID_REQUEST", "Attachment refresh returned a different block identity")
		}
		original := map[string]any(nil)
		for _, item := range p.Items {
			if item.Kind == "notion_block" && item.ExternalID == t.ID {
				metadata := asObject(item.Provenance)
				if metadata["sourceType"] != kind {
					return Fail("CONFLICT", "Source attachment type changed")
				}
				original = asObject(metadata["sourceData"])
			}
		}
		if original == nil || !notionSameFile(original, data) {
			return Fail("CONFLICT", "Source attachment changed; recapture or finish partial")
		}
		caption = notionRichText(data["caption"])
	}
	attachmentURL := str(asObject(data[str(data["type"])])["url"])
	if !notionAttachmentURL(attachmentURL) {
		return Fail("INVALID_REQUEST", "Attachment source is outside the Notion storage allowlist; finish partial to retain its evidence")
	}
	bytes, _, err := fetchNotion(ctx, client, attachmentURL, "")
	if err != nil {
		return err
	}
	if s.Bytes+int64(len(bytes))+int64(len(raw)) > 128<<20 {
		return Fail("RESOURCE_EXHAUSTED", "Attachment exceeds remaining capture budget")
	}
	fileID, err := k.storeImportBytes(ctx, "notion-attachment-"+t.ID, bytes)
	if err != nil {
		return err
	}
	s.Bytes += int64(len(bytes))
	locator := fmt.Sprintf("notion/attachments/%s/%s/%d", t.ID, t.PropertyID, t.FileIndex)
	p.Items = append(p.Items, ImportItem{Locator: locator, Kind: "asset", Outcome: "preserved", Reason: "Fresh source reference downloaded for offline use", RawFileID: fileID, Hash: importDigest(bytes), Size: int64(len(bytes))})
	if t.PropertyID != "" {
		obj := s.Objects["record:"+t.ID]
		if obj.Files == nil {
			obj.Files = map[string]string{}
		}
		obj.Files[fmt.Sprintf("%s:%d", t.PropertyID, t.FileIndex)] = fileID
		s.Objects["record:"+t.ID] = obj
		return nil
	}
	for i := range p.Items {
		item := &p.Items[i]
		if item.Kind == "notion_block" && item.ExternalID == t.ID {
			item.Blocks = []ImportBlock{{NewID(), kind, map[string]any{"fileId": fileID, "caption": caption}}}
			item.Outcome = "adapted"
			item.Reason = "Attachment source refreshed and downloaded for offline use"
			return nil
		}
	}
	return Fail("INVALID_REQUEST", "Attachment task lost its source block")
}

// The authenticated IPC surface does not send whole raw capture objects back to
// the renderer on every step. Internal persisted state remains independently
// resumable and auditable through the reviewed import evidence.
func publicNotionQuery(schema string, handler func(context.Context, *Kernel, contracts.ServiceExecutionContext, map[string]any) (any, error)) func(context.Context, *Kernel, contracts.ServiceExecutionContext, map[string]any) (any, error) {
	return func(ctx context.Context, k *Kernel, e contracts.ServiceExecutionContext, r map[string]any) (any, error) {
		if err := contracts.Validate("service", schema, marshal(r)); err != nil {
			return nil, Fail("INVALID_REQUEST", err.Error())
		}
		value, err := handler(ctx, k, e, r)
		if err != nil {
			return nil, err
		}
		switch v := value.(type) {
		case NotionCapture:
			return notionCaptureSummary(v), nil
		case []NotionCapture:
			out := []map[string]any{}
			for _, c := range v {
				out = append(out, notionCaptureSummary(c))
			}
			return out, nil
		}
		return value, nil
	}
}
func notionCaptureSummary(s NotionCapture) map[string]any {
	version := s.APIVersion
	if version == "" {
		version = "2025-09-03"
	}
	out := map[string]any{"captureId": s.ID, "revision": s.Revision, "state": s.State, "authorizedRoots": s.Roots, "apiVersion": version, "planId": s.PlanID, "pending": s.Pending, "completed": s.Completed, "bytes": s.Bytes}
	if s.Error != "" {
		out["error"] = s.Error
	}
	return out
}

func notionSamePropertyID(a, b string) bool {
	x, e := notionPropertyPath(a)
	y, f := notionPropertyPath(b)
	return e == nil && f == nil && x == y
}

func notionSameFile(a, b map[string]any) bool {
	if str(a["name"]) != str(b["name"]) || a["type"] != b["type"] {
		return false
	}
	first, e := url.Parse(str(asObject(a[str(a["type"])])["url"]))
	second, f := url.Parse(str(asObject(b[str(b["type"])])["url"]))
	return e == nil && f == nil && first.Host == second.Host && first.Path == second.Path
}

// Overlapping explicit roots and query results refer to one page. Hydrate a
// previously captured standalone page with its record schema without assigning
// another local identity or discarding already captured descendant blocks.
func notionEnqueuePage(s *NotionCapture, p *ImportPlan, t notionCaptureTask) error {
	if prior, ok := s.Objects["record:"+t.ID]; ok {
		if prior.Task.SourceID != t.SourceID || prior.Task.Template != t.Template {
			return Fail("CONFLICT", "Source page changed record/template ownership during capture")
		}
		return nil
	}
	for i := range s.Pending {
		pending := &s.Pending[i]
		if pending.Kind == "page" && pending.ID == t.ID {
			if pending.SourceID != "" && (pending.SourceID != t.SourceID || pending.Template != t.Template) {
				return Fail("CONFLICT", "Source page has conflicting database ownership")
			}
			pending.SourceID = t.SourceID
			pending.Template = t.Template
			return nil
		}
	}
	for _, item := range p.Items {
		if item.Kind == "notion_page" && item.ExternalID == t.ID {
			t.PageID = item.PageID
			t.ParentID = item.ParentID
			delete(s.Seen, "queued:page:"+t.ID+"::0")
			break
		}
	}
	notionEnqueue(s, t)
	return nil
}

func notionLocalPage(s *NotionCapture, p *ImportPlan, kind, id string) string {
	objectKey := kind + ":" + id
	if kind == "page" {
		objectKey = "record:" + id
	}
	if obj, ok := s.Objects[objectKey]; ok {
		return obj.Task.PageID
	}
	for _, task := range s.Pending {
		if task.Kind == kind && task.ID == id {
			return task.PageID
		}
	}
	external := id
	if kind != "page" {
		external = kind + ":" + id
	}
	for _, item := range p.Items {
		if item.PageID != "" && item.ExternalID == external {
			return item.PageID
		}
	}
	return NewID()
}
