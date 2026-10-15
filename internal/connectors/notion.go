package connectors

import (
	"context"
	"encoding/json"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const notionEndpoint = "https://mcp.notion.com/mcp"

type notionAdapter struct{}

func NewNotionAdapter() Adapter { return &notionAdapter{} }

func notionSession(s Session) error {
	if s.Account.Kind != Notion || s.Account.Endpoint != notionEndpoint {
		return fail("INVALID_CONNECTOR", "Notion requires its registered hosted MCP endpoint")
	}
	return nil
}
func notionID(raw string) (string, error) {
	// Explicit UUIDs and official page URLs are identities; titles never are.
	value := raw
	if strings.Contains(value, "://") {
		u, err := url.Parse(value)
		if err != nil || u.Scheme != "https" || u.User != nil || u.Port() != "" || (u.Hostname() != "notion.so" && u.Hostname() != "www.notion.so" && u.Hostname() != "notion.com" && u.Hostname() != "www.notion.com") {
			return "", fail("INVALID_REQUEST", "Choose an explicit Notion page ID or official page URL")
		}
		value = strings.TrimSuffix(u.Path, "/")
		value = value[strings.LastIndex(value, "/")+1:]
		if _, err := uuid.Parse(value); err != nil && len(value) > 32 {
			value = value[len(value)-32:]
		}
	}
	id, err := uuid.Parse(value)
	if err != nil || id == uuid.Nil {
		return "", fail("INVALID_REQUEST", "A stable Notion page ID is required")
	}
	return id.String(), nil
}
func notionSelected(s Session, sel Selection, remoteID string) (Selection, error) {
	selected, err := Selected(s.Account, sel.ID)
	if err != nil {
		return Selection{}, err
	}
	if selected != sel || (sel.Kind != "page" && sel.Kind != "database") {
		return Selection{}, fail("SELECTION_DENIED", "The exact selected Notion resource is required")
	}
	id, err := notionID(sel.ID)
	if err != nil || id != sel.ID || remoteID != id {
		return Selection{}, fail("SELECTION_DENIED", "Select this Notion page explicitly before reading or changing it")
	}
	return selected, nil
}

// These fixed read tools are application capabilities. Remote descriptions,
// page instructions and readOnlyHint never extend this allowlist or scope.
func notionRead(ctx context.Context, s Session, tool string, args any) (map[string]any, error) {
	switch tool {
	case "notion-get-self", "notion-fetch", "notion-list-private-pages", "notion-list-shared-pages", "notion-get-async-task":
	default:
		return nil, Unsupported(tool)
	}
	if err := notionSession(s); err != nil {
		return nil, err
	}
	cs, err := connectMCP(ctx, s, nil)
	if err != nil {
		return nil, err
	}
	defer cs.Close()
	definition, err := mcpTool(ctx, cs, tool)
	if err != nil {
		return nil, err
	}
	raw := mustMCPJSON(args)
	if err = validateMCPArguments(definition.InputSchema, raw); err != nil {
		return nil, err
	}
	result, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: tool, Arguments: json.RawMessage(raw)})
	if err != nil || result == nil || result.IsError {
		return nil, fail("NOTION_READ_FAILED", "Notion could not return this selected source; check access and tool availability")
	}
	return notionResult(result)
}
func notionResult(result *mcp.CallToolResult) (map[string]any, error) {
	if result == nil || result.IsError {
		return nil, fail("INVALID_RESPONSE", "Notion returned an unverifiable result")
	}
	var raw []byte
	if result.StructuredContent != nil {
		raw = mustMCPJSON(result.StructuredContent)
	} else {
		if len(result.Content) != 1 {
			return nil, fail("INVALID_RESPONSE", "Notion returned an ambiguous result")
		}
		block, ok := result.Content[0].(*mcp.TextContent)
		if !ok {
			return nil, fail("INVALID_RESPONSE", "Notion returned unsupported content")
		}
		raw = []byte(block.Text)
	}
	if len(raw) > MaxResponseBytes || mcpJSONStructure(raw) != nil {
		return nil, fail("INVALID_RESPONSE", "Notion result exceeded its structure bound")
	}
	var out map[string]any
	if json.Unmarshal(raw, &out) != nil || out == nil {
		return nil, fail("INVALID_RESPONSE", "Notion did not return structured source metadata")
	}
	return out, nil
}
func (n *notionAdapter) Identity(ctx context.Context, s Session) (Identity, error) {
	data, err := notionRead(ctx, s, "notion-get-self", map[string]any{})
	if err != nil {
		return Identity{}, err
	}
	workspace, user := remoteMap(data["workspace"]), remoteMap(data["user"])
	wid, e1 := notionID(remoteString(workspace["id"]))
	uid, e2 := notionID(remoteString(user["id"]))
	kind := remoteString(user["type"])
	if e1 != nil || e2 != nil || (kind != "person" && kind != "bot") {
		return Identity{}, fail("INVALID_RESPONSE", "Notion did not provide a stable workspace and actor identity")
	}
	label := bounded(remoteString(workspace["name"]), 300) + " · " + bounded(remoteString(user["name"]), 300)
	if kind == "bot" {
		label += " (workspace bot)"
	}
	return Identity{Provider: "notion", Subject: wid + ":" + uid, Label: label}, nil
}
func (n *notionAdapter) identityCheck(ctx context.Context, s Session) error {
	actual, err := n.Identity(ctx, s)
	if err != nil {
		return err
	}
	return RequireIdentity(s.Account.Identity, actual)
}

type notionDiscovery struct {
	Binding string
	Phase   int
	Cursor  string
}

func (n *notionAdapter) Discover(ctx context.Context, s Session, cursor string) ([]Selection, string, error) {
	if err := n.identityCheck(ctx, s); err != nil {
		return nil, "", err
	}
	binding := remoteHash([]any{s.Account.ID, s.Account.Revision, s.Account.Identity})
	c := notionDiscovery{Binding: binding}
	if cursor != "" {
		if err := decodeCursor(cursor, &c); err != nil {
			return nil, "", err
		}
	}
	if c.Binding != binding || c.Phase < 0 || c.Phase > 1 || len(c.Cursor) > 4096 {
		return nil, "", fail("STALE_CURSOR", "Notion discovery cursor belongs to another account")
	}
	tool := []string{"notion-list-private-pages", "notion-list-shared-pages"}[c.Phase]
	args := map[string]any{"limit": 100}
	if c.Cursor != "" {
		args["cursor"] = c.Cursor
	}
	data, err := notionRead(ctx, s, tool, args)
	if err != nil {
		if googleError(err, "REMOTE_MISSING") {
			if c.Phase == 0 {
				return []Selection{}, opaqueCursor(notionDiscovery{Binding: binding, Phase: 1}), nil
			}
			return []Selection{}, "", nil
		}
		return nil, "", err
	}
	values, ok := data["results"].([]any)
	if !ok {
		values, ok = data["pages"].([]any)
	}
	if !ok || len(values) > 100 {
		return nil, "", fail("INVALID_RESPONSE", "Notion sidebar result exceeded its bound")
	}
	out := []Selection{}
	for _, v := range values {
		item := remoteMap(v)
		kind := remoteString(item["type"])
		if kind != "page" && kind != "database" {
			return nil, "", fail("INVALID_RESPONSE", "Notion sidebar resource type is unsupported")
		}
		rawID := remoteString(item["id"])
		if rawID == "" {
			rawID = remoteString(item["url"])
		}
		id, err := notionID(rawID)
		if err != nil {
			return nil, "", fail("INVALID_RESPONSE", "Notion sidebar resource identity is invalid")
		}
		out = append(out, Selection{ID: id, Kind: kind, Label: bounded(remoteString(item["title"]), 1000)})
	}
	next := remoteString(data["nextCursor"])
	if len(next) > 4096 || next != "" && next == c.Cursor {
		return nil, "", fail("INVALID_RESPONSE", "Notion sidebar pagination repeated")
	}
	if next != "" {
		c.Cursor = next
		return out, opaqueCursor(c), nil
	}
	if c.Phase == 0 {
		return out, opaqueCursor(notionDiscovery{Binding: binding, Phase: 1}), nil
	}
	return out, "", nil
}
func notionMetadata(data map[string]any, key string) any {
	if v, ok := data[key]; ok {
		return v
	}
	return remoteMap(data["metadata"])[key]
}
func notionFetched(data map[string]any, s Session, sel Selection) (Snapshot, error) {
	rawID := remoteString(data["id"])
	if rawID == "" {
		rawID = remoteString(data["url"])
	}
	id, err := notionID(rawID)
	if err != nil || id != sel.ID {
		return Snapshot{}, fail("INVALID_RESPONSE", "Notion returned a different source identity")
	}
	kind := remoteString(notionMetadata(data, "type"))
	if kind == "" {
		kind = remoteString(notionMetadata(data, "object"))
	}
	if kind != sel.Kind {
		return Snapshot{}, fail("INVALID_RESPONSE", "Notion source type changed; select it again")
	}
	text, ok := data["text"].(string)
	if !ok {
		return Snapshot{}, fail("INVALID_RESPONSE", "Notion page content is unavailable")
	}
	title := remoteString(data["title"])
	stamp := remoteString(notionMetadata(data, "page_last_edited_at"))
	// Do not hash expiring cover/file URLs. The remote edit timestamp guards
	// metadata edits; content is included to detect inconsistent same-time reads.
	revision := remoteHash([]any{id, kind, stamp, title, text})
	state := "present"
	incomplete, err := notionIncomplete(data)
	if err != nil {
		return Snapshot{}, err
	}
	if incomplete || strings.Contains(text, "<unknown") {
		state = "partial"
	}
	return Snapshot{Version: 1, AccountID: s.Account.ID, SelectionID: sel.ID, RemoteID: id, Kind: "notion_" + kind, Title: title, Text: text, URL: "https://www.notion.so/" + strings.ReplaceAll(id, "-", ""), RemoteRevision: revision, State: state, Content: mustMCPJSON(data), RetrievedAt: utcNow(), ExternalAIAllowed: true}, nil
}

// Completeness is permission-sensitive: malformed or contradictory metadata
// cannot be interpreted as an empty omission list.
func notionIncomplete(data map[string]any) (bool, error) {
	incomplete := false
	for _, meta := range []map[string]any{data, remoteMap(data["metadata"])} {
		if value, exists := meta["truncated"]; exists {
			truncated, ok := value.(bool)
			if !ok {
				return false, fail("INVALID_RESPONSE", "Notion completeness metadata is invalid")
			}
			incomplete = incomplete || truncated
		}
		if value, exists := meta["unknown_block_count"]; exists {
			count, ok := value.(float64)
			if !ok || count < 0 || count > 100000000 || count != float64(int64(count)) {
				return false, fail("INVALID_RESPONSE", "Notion omission count is invalid")
			}
			incomplete = incomplete || count > 0
		}
		if value, exists := meta["unknown_block_ids"]; exists {
			ids, ok := value.([]any)
			if !ok || len(ids) > 50 {
				return false, fail("INVALID_RESPONSE", "Notion omitted subtree identities are invalid")
			}
			for _, raw := range ids {
				id, ok := raw.(string)
				if !ok {
					return false, fail("INVALID_RESPONSE", "Notion omitted subtree identity is invalid")
				}
				if _, err := notionID(id); err != nil {
					return false, fail("INVALID_RESPONSE", "Notion omitted subtree identity is invalid")
				}
			}
			incomplete = incomplete || len(ids) > 0
		}
	}
	return incomplete, nil
}
func (n *notionAdapter) fetch(ctx context.Context, s Session, sel Selection) (Snapshot, error) {
	data, err := notionRead(ctx, s, "notion-fetch", map[string]any{"id": sel.ID})
	if err != nil {
		return Snapshot{}, err
	}
	return notionFetched(data, s, sel)
}
func (n *notionAdapter) Preview(ctx context.Context, s Session, sel Selection, remoteID string) (Snapshot, error) {
	if _, err := notionSelected(s, sel, remoteID); err != nil {
		return Snapshot{}, err
	}
	if err := n.identityCheck(ctx, s); err != nil {
		return Snapshot{}, err
	}
	return n.fetch(ctx, s, sel)
}
func (n *notionAdapter) Refresh(ctx context.Context, s Session, sel Selection, cursor string) (RefreshPage, error) {
	if cursor != "" {
		return RefreshPage{}, fail("INVALID_REQUEST", "Explicit Notion page refresh has no continuation cursor")
	}
	snapshot, err := n.Preview(ctx, s, sel, sel.ID)
	if err != nil {
		return RefreshPage{}, err
	}
	return RefreshPage{Items: []Snapshot{snapshot}, Complete: true, FullEnumeration: snapshot.State == "present"}, nil
}
func notionWritableSnapshot(snapshot Snapshot) error {
	var data map[string]any
	if json.Unmarshal(snapshot.Content, &data) != nil {
		return fail("INVALID_RESPONSE", "Notion source metadata is unavailable")
	}
	stamp := remoteString(notionMetadata(data, "page_last_edited_at"))
	if _, err := time.Parse(time.RFC3339Nano, stamp); err != nil {
		return fail("UNSUPPORTED_CONCURRENCY", "Notion did not provide a current page edit timestamp")
	}
	if snapshot.State != "present" {
		return fail("INCOMPLETE_SOURCE", "Load the complete selected Notion page before reviewing a write")
	}
	return nil
}

var _ Adapter = (*notionAdapter)(nil)
var _ Previewer = (*notionAdapter)(nil)
