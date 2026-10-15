package connectors

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const notionTestPage = "e732c86f-8076-4daf-bf82-174ae94e5a70"
const notionTestOther = "500282a4-f7c3-4a11-8bc5-0ff1199bb650"
const notionTestWorkspace = "7dd69011-fd22-4250-8aaf-4cb97e2b8faf"
const notionTestUser = "52bea93d-852b-4cfe-8825-f94d913424b8"

type notionFixture struct {
	session        Session
	server         *mcp.Server
	handler        http.Handler
	mu             sync.Mutex
	actor, page    map[string]any
	writes         atomic.Int32
	reads          atomic.Int32
	identityReads  atomic.Int32
	failAfterWrite atomic.Bool
	toolError      atomic.Bool
	result         map[string]any
	args           json.RawMessage
}

func notionObject(properties map[string]any, required ...string) map[string]any {
	if required == nil {
		required = []string{}
	}
	return map[string]any{"type": "object", "properties": properties, "required": required, "additionalProperties": false}
}
func notionString() map[string]any { return map[string]any{"type": "string"} }
func newNotionFixture(t *testing.T) *notionFixture {
	t.Helper()
	f := &notionFixture{}
	f.actor = map[string]any{"workspace": map[string]any{"id": notionTestWorkspace, "name": "Fixture workspace"}, "user": map[string]any{"id": notionTestUser, "name": "Fixture person", "type": "person"}}
	f.page = map[string]any{"url": "https://www.notion.so/" + strings.ReplaceAll(notionTestPage, "-", ""), "title": "Selected page", "text": "First stable block\nSecond stable block\n[Other](https://www.notion.so/" + notionTestOther + ")", "metadata": map[string]any{"type": "page"}, "page_last_edited_at": "2026-10-15T10:00:00Z", "cover": nil}
	f.result = map[string]any{"page_id": notionTestPage}
	f.server = mcp.NewServer(&mcp.Implementation{Name: "Notion fixture", Version: "1"}, nil)
	f.addRead("notion-get-self", notionObject(map[string]any{}), func(map[string]any) map[string]any { f.identityReads.Add(1); return f.actor })
	f.addRead("notion-fetch", notionObject(map[string]any{"id": notionString()}, "id"), func(args map[string]any) map[string]any {
		f.reads.Add(1)
		if args["id"] != notionTestPage {
			t.Error("unselected source fetched")
		}
		return f.page
	})
	f.addRead("notion-list-private-pages", notionObject(map[string]any{"limit": map[string]any{"type": "integer"}, "cursor": notionString()}, "limit"), func(args map[string]any) map[string]any {
		if args["cursor"] == "next" {
			return map[string]any{"results": []any{map[string]any{"type": "page", "url": f.page["url"], "title": "Selected"}}}
		}
		return map[string]any{"results": []any{}, "nextCursor": "next"}
	})
	f.addRead("notion-list-shared-pages", notionObject(map[string]any{"limit": map[string]any{"type": "integer"}, "cursor": notionString()}, "limit"), func(map[string]any) map[string]any { return map[string]any{"results": []any{}} })
	f.addRead("notion-get-async-task", notionObject(map[string]any{"task_id": notionString()}, "task_id"), func(args map[string]any) map[string]any {
		return map[string]any{"id": args["task_id"], "status": "succeeded", "result": map[string]any{"page_id": notionTestPage}}
	})
	f.writeTool("notion-update-page", "Original definition")
	f.writeTool("notion-create-pages", "Original definition")
	f.handler = mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return f.server }, &mcp.StreamableHTTPOptions{Stateless: true, JSONResponse: true})
	f.session = Session{Account: Account{Version: 1, ID: "notion-account", Revision: "account-revision1", Kind: Notion, Endpoint: notionEndpoint, Identity: Identity{Provider: "notion", Subject: notionTestWorkspace + ":" + notionTestUser}, Selections: []Selection{{ID: notionTestPage, Kind: "page", Label: "Selected page"}}}, Token: Secret{AccessToken: "synthetic-notion-token"}}
	f.session.Client = &http.Client{Transport: googleTestTransport(func(r *http.Request) (*http.Response, error) {
		if r.URL.String() != notionEndpoint || r.Header.Get("Authorization") != "Bearer synthetic-notion-token" {
			t.Error("Notion credential origin changed")
		}
		rec := httptest.NewRecorder()
		f.handler.ServeHTTP(rec, r)
		if f.failAfterWrite.Load() && f.writes.Load() > 0 {
			return nil, errors.New("connection lost after side effect")
		}
		return rec.Result(), nil
	})}
	return f
}
func (f *notionFixture) addRead(name string, schema any, run func(map[string]any) map[string]any) {
	f.server.AddTool(&mcp.Tool{Name: name, InputSchema: schema}, func(_ context.Context, r *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		var args map[string]any
		_ = json.Unmarshal(r.Params.Arguments, &args)
		f.mu.Lock()
		defer f.mu.Unlock()
		return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: string(mustMCPJSON(run(args)))}}}, nil
	})
}
func (f *notionFixture) writeTool(name, description string) {
	props := map[string]any{"page_id": notionString(), "command": notionString(), "new_str": notionString(), "properties": map[string]any{"type": "object"}, "updates": map[string]any{"type": "array"}, "allow_async": map[string]any{"type": "boolean"}}
	required := []string{"page_id", "command"}
	if name == "notion-create-pages" {
		props = map[string]any{"parent": map[string]any{"type": "object"}, "pages": map[string]any{"type": "array"}, "allow_async": map[string]any{"type": "boolean"}}
		required = []string{"parent", "pages"}
	}
	f.server.AddTool(&mcp.Tool{Name: name, Description: description, InputSchema: notionObject(props, required...)}, func(_ context.Context, r *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		f.writes.Add(1)
		f.mu.Lock()
		defer f.mu.Unlock()
		f.args = append([]byte{}, r.Params.Arguments...)
		return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: string(mustMCPJSON(f.result))}}, IsError: f.toolError.Load()}, nil
	})
}
func notionTestUpdate() Action {
	return Action{Version: 1, Kind: "notion.page.update", SelectionID: notionTestPage, RemoteID: notionTestPage, Arguments: mustMCPJSON(map[string]any{"page_id": notionTestPage, "command": "replace_content", "new_str": "Reviewed complete body", "allow_async": false})}
}
func notionTestCreate() Action {
	return Action{Version: 1, Kind: "notion.page.create", SelectionID: notionTestPage, Arguments: mustMCPJSON(map[string]any{"parent": map[string]any{"page_id": notionTestPage}, "pages": []any{map[string]any{"properties": map[string]any{"title": "New page"}, "content": "Reviewed markdown"}}, "allow_async": false})}
}
func TestNotionVerifiedIdentityAndBotLabel(t *testing.T) {
	f := newNotionFixture(t)
	n := NewNotionAdapter()
	ctx := context.Background()
	got, err := n.Identity(ctx, f.session)
	if err != nil || got.Subject != f.session.Account.Identity.Subject || got.Provider != "notion" {
		t.Fatal(got, err)
	}
	f.mu.Lock()
	remoteMap(f.actor["user"])["type"] = "bot"
	f.mu.Unlock()
	got, err = n.Identity(ctx, f.session)
	if err != nil || !strings.Contains(got.Label, "workspace bot") {
		t.Fatal(got, err)
	}
	f.mu.Lock()
	delete(remoteMap(f.actor["workspace"]), "id")
	f.mu.Unlock()
	if _, err = n.Identity(ctx, f.session); err == nil {
		t.Fatal("unidentified workspace accepted")
	}
}
func TestNotionSelectedPagePreviewDoesNotFollowLinks(t *testing.T) {
	f := newNotionFixture(t)
	n := NewNotionAdapter().(Previewer)
	ctx := context.Background()
	sel := f.session.Account.Selections[0]
	got, err := n.Preview(ctx, f.session, sel, sel.ID)
	if err != nil || got.State != "present" || got.RemoteID != sel.ID || !strings.Contains(got.Text, "Other") || f.reads.Load() != 1 {
		t.Fatal(got, err)
	}
	var retained map[string]any
	_ = json.Unmarshal(got.Content, &retained)
	if v, ok := retained["cover"]; !ok || v != nil {
		t.Fatal("explicit null metadata lost")
	}
	if _, ok := retained["icon"]; ok {
		t.Fatal("missing metadata fabricated")
	}
	forged := sel
	forged.Kind = "database"
	for _, r := range []struct {
		sel Selection
		id  string
	}{{sel, notionTestOther}, {forged, sel.ID}, {Selection{ID: notionTestOther, Kind: "page"}, notionTestOther}} {
		if _, err = n.Preview(ctx, f.session, r.sel, r.id); err == nil {
			t.Fatal("scope widened")
		}
	}
	if f.reads.Load() != 1 {
		t.Fatal("unselected data fetched")
	}
}
func TestNotionPartialCaptureCannotReviewWrite(t *testing.T) {
	for _, marker := range []string{"truncated", "unknown_block_ids", "unknown_tag"} {
		t.Run(marker, func(t *testing.T) {
			f := newNotionFixture(t)
			f.mu.Lock()
			switch marker {
			case "truncated":
				f.page["truncated"] = true
			case "unknown_block_ids":
				f.page["unknown_block_ids"] = []any{notionTestOther}
			case "unknown_tag":
				f.page["text"] = "<unknown url=\"other\"/>"
			}
			f.mu.Unlock()
			n := NewNotionAdapter()
			got, err := n.Refresh(context.Background(), f.session, f.session.Account.Selections[0], "")
			if err != nil || got.FullEnumeration || !got.Complete || got.Items[0].State != "partial" {
				t.Fatal(got, err)
			}
			if _, err = n.Prepare(context.Background(), f.session, notionTestUpdate()); err == nil || f.writes.Load() != 0 {
				t.Fatal("partial source writable")
			}
		})
	}
}
func TestNotionDiscoveryEmptyPageKeepsCursorBoundToAccount(t *testing.T) {
	f := newNotionFixture(t)
	n := NewNotionAdapter()
	ctx := context.Background()
	first, cursor, err := n.Discover(ctx, f.session, "")
	if err != nil || len(first) != 0 || cursor == "" {
		t.Fatal(first, cursor, err)
	}
	second, next, err := n.Discover(ctx, f.session, cursor)
	if err != nil || len(second) != 1 || second[0].ID != notionTestPage || next == "" {
		t.Fatal(second, next, err)
	}
	third, last, err := n.Discover(ctx, f.session, next)
	if err != nil || len(third) != 0 || last != "" || f.reads.Load() != 0 {
		t.Fatal(third, last, err)
	}
	other := f.session
	other.Account.ID = "other"
	if _, _, err = n.Discover(ctx, other, cursor); err == nil {
		t.Fatal("cursor crossed account")
	}
}
func TestNotionReviewedUpdateUsesExactArgumentsAndEffects(t *testing.T) {
	f := newNotionFixture(t)
	n := NewNotionAdapter()
	ctx := context.Background()
	action := notionTestUpdate()
	prepared, err := n.Prepare(ctx, f.session, action)
	if err != nil || prepared.ExpectedRevision == "" || len(prepared.Effects) < 5 || f.writes.Load() != 0 {
		t.Fatal(prepared, err)
	}
	p := googleTestProposal(f.session, prepared)
	got, err := n.Send(ctx, f.session, p)
	if err != nil || got.Outcome != "succeeded" || f.writes.Load() != 1 || !reflect.DeepEqual(prepared.Arguments, f.args) {
		t.Fatal(got, err, string(f.args))
	}
	tampered := prepared
	tampered.Effects = []string{"No side effects"}
	if _, err = n.Send(ctx, f.session, googleTestProposal(f.session, tampered)); err == nil || f.writes.Load() != 1 {
		t.Fatal("altered effect disclosure sent")
	}
}
func TestNotionStalePageSchemaAndAccountRefuseWrite(t *testing.T) {
	for _, which := range []string{"content", "timestamp", "tool", "identity", "account_revision"} {
		t.Run(which, func(t *testing.T) {
			f := newNotionFixture(t)
			n := NewNotionAdapter()
			ctx := context.Background()
			prepared, err := n.Prepare(ctx, f.session, notionTestUpdate())
			if err != nil {
				t.Fatal(err)
			}
			p := googleTestProposal(f.session, prepared)
			f.mu.Lock()
			switch which {
			case "content":
				f.page["text"] = "Remote edit"
			case "timestamp":
				f.page["page_last_edited_at"] = "2026-10-15T10:01:00Z"
			case "identity":
				remoteMap(f.actor["user"])["id"] = notionTestOther
			}
			f.mu.Unlock()
			if which == "tool" {
				f.writeTool("notion-update-page", "Changed operation contract")
			}
			if which == "account_revision" {
				f.session.Account.Revision = "new-selection"
			}
			if _, err = n.Send(ctx, f.session, p); err == nil || f.writes.Load() != 0 {
				t.Fatal("stale operation sent", which, err)
			}
		})
	}
}
func TestNotionExactPropertyAndContentBatchUpdates(t *testing.T) {
	for _, args := range []map[string]any{
		{"page_id": notionTestPage, "command": "update_properties", "properties": map[string]any{"Status": "Done", "Cleared": nil}},
		{"page_id": notionTestPage, "command": "update_content", "updates": []any{map[string]any{"old_str": "First stable block", "new_str": "Changed first block"}}},
	} {
		f := newNotionFixture(t)
		n := NewNotionAdapter()
		action := notionTestUpdate()
		action.Arguments = mustMCPJSON(args)
		prepared, err := n.Prepare(context.Background(), f.session, action)
		if err != nil {
			t.Fatal(err)
		}
		receipt, err := n.Send(context.Background(), f.session, googleTestProposal(f.session, prepared))
		if err != nil || receipt.Outcome != "succeeded" || string(f.args) != string(action.Arguments) {
			t.Fatal(receipt, err)
		}
	}
}
func TestNotionCreateDestinationIsExplicitAndReceiptHasIdentity(t *testing.T) {
	f := newNotionFixture(t)
	f.result = map[string]any{"pages": []any{map[string]any{"id": notionTestOther}}}
	n := NewNotionAdapter()
	ctx := context.Background()
	prepared, err := n.Prepare(ctx, f.session, notionTestCreate())
	if err != nil {
		t.Fatal(err)
	}
	receipt, err := n.Send(ctx, f.session, googleTestProposal(f.session, prepared))
	if err != nil || receipt.Outcome != "succeeded" || receipt.RemoteID != notionTestOther {
		t.Fatal(receipt, err)
	}
	var args map[string]any
	_ = json.Unmarshal(notionTestCreate().Arguments, &args)
	delete(args, "parent")
	a := notionTestCreate()
	a.Arguments = mustMCPJSON(args)
	if _, err = n.Prepare(ctx, f.session, a); err == nil {
		t.Fatal("implicit private destination accepted")
	}
}
func TestNotionUnsupportedActionsAndHiddenArgumentsRefused(t *testing.T) {
	invalid := []map[string]any{
		{"page_id": notionTestOther, "command": "replace_content", "new_str": "text"},
		{"page_id": notionTestPage, "command": "replace_content", "new_str": "text", "is_skill": true},
		{"page_id": notionTestPage, "command": "replace_content", "new_str": "text", "allow_async": true},
		{"page_id": notionTestPage, "command": "apply_template", "template_id": notionTestOther},
		{"page_id": notionTestPage, "command": "update_content", "updates": []any{map[string]any{"old_str": "", "new_str": "text"}}},
		{"page_id": notionTestPage, "command": "update_content", "updates": []any{map[string]any{"old_str": "First stable block", "new_str": "next", "replace_all_matches": true}}},
	}
	f := newNotionFixture(t)
	n := NewNotionAdapter()
	for _, args := range invalid {
		a := notionTestUpdate()
		a.Arguments = mustMCPJSON(args)
		if _, err := n.Prepare(context.Background(), f.session, a); err == nil {
			t.Fatal("unsupported arguments accepted", args)
		}
	}
	if f.writes.Load() != 0 || f.reads.Load() != 0 {
		t.Fatal("invalid proposal caused remote content access")
	}
}
func TestNotionContentBatchRejectsUnmatchedAndAmbiguousSources(t *testing.T) {
	for _, body := range []string{"no source match", "First stable block and First stable block"} {
		f := newNotionFixture(t)
		f.page["text"] = body
		n := NewNotionAdapter()
		a := notionTestUpdate()
		a.Arguments = mustMCPJSON(map[string]any{"page_id": notionTestPage, "command": "update_content", "updates": []any{map[string]any{"old_str": "First stable block", "new_str": "next"}}})
		if _, err := n.Prepare(context.Background(), f.session, a); err == nil || f.writes.Load() != 0 {
			t.Fatal("unsafe source replacement allowed")
		}
	}
}
func TestNotionUnknownWritesNeverReplay(t *testing.T) {
	for _, which := range []string{"transport", "tool_error", "warnings", "wrong_page"} {
		t.Run(which, func(t *testing.T) {
			f := newNotionFixture(t)
			n := NewNotionAdapter()
			ctx := context.Background()
			prepared, err := n.Prepare(ctx, f.session, notionTestUpdate())
			if err != nil {
				t.Fatal(err)
			}
			p := googleTestProposal(f.session, prepared)
			switch which {
			case "transport":
				f.failAfterWrite.Store(true)
			case "tool_error":
				f.toolError.Store(true)
			case "warnings":
				f.result["warnings"] = []any{map[string]any{"code": "update_warning", "message": "altered"}}
			case "wrong_page":
				f.result["page_id"] = notionTestOther
			}
			got, err := n.Send(ctx, f.session, p)
			if err == nil && got.Outcome != "unknown" || err != nil && !googleError(err, "UNKNOWN_OUTCOME") || f.writes.Load() != 1 {
				t.Fatal(got, err, f.writes.Load())
			}
			f.failAfterWrite.Store(false)
			p.Receipt = &got
			reconciled, err := n.Reconcile(ctx, f.session, p)
			if err != nil || reconciled.Outcome != "unknown" || f.writes.Load() != 1 {
				t.Fatal(reconciled, err)
			}
		})
	}
}
func TestNotionAsyncReceiptReconcilesReadOnly(t *testing.T) {
	f := newNotionFixture(t)
	f.result = map[string]any{"async_task": map[string]any{"id": "task_fixture", "status": "queued"}}
	n := NewNotionAdapter()
	ctx := context.Background()
	prepared, err := n.Prepare(ctx, f.session, notionTestUpdate())
	if err != nil {
		t.Fatal(err)
	}
	p := googleTestProposal(f.session, prepared)
	receipt, err := n.Send(ctx, f.session, p)
	if err != nil || receipt.Outcome != "unknown" || receipt.RemoteID != "notion-task:task_fixture" {
		t.Fatal(receipt, err)
	}
	p.Receipt = &receipt
	if _, err = n.Reconcile(ctx, f.session, p); !googleError(err, "RATE_LIMITED") {
		t.Fatal("queued receipt did not retain polling delay", err)
	}
	p.Receipt.RecordedAt = time.Now().Add(-3 * time.Second).UTC().Format(time.RFC3339Nano)
	receipt, err = n.Reconcile(ctx, f.session, p)
	if err != nil || receipt.Outcome != "succeeded" || f.writes.Load() != 1 {
		t.Fatal(receipt, err)
	}
}
func TestNotionMissingRevisionAndCancellationCannotWrite(t *testing.T) {
	f := newNotionFixture(t)
	n := NewNotionAdapter()
	ctx := context.Background()
	delete(f.page, "page_last_edited_at")
	if _, err := n.Prepare(ctx, f.session, notionTestUpdate()); err == nil {
		t.Fatal("missing edit timestamp accepted")
	}
	f.page["page_last_edited_at"] = "2026-10-15T10:00:00Z"
	prepared, err := n.Prepare(ctx, f.session, notionTestUpdate())
	if err != nil {
		t.Fatal(err)
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err = n.Send(cancelled, f.session, googleTestProposal(f.session, prepared)); err == nil || f.writes.Load() != 0 {
		t.Fatal("cancelled write sent")
	}
}
func TestNotionCredentialOriginAndSourceIdentityBoundaries(t *testing.T) {
	f := newNotionFixture(t)
	n := NewNotionAdapter()
	other := f.session
	other.Account.Endpoint = "https://api.notion.com/v1/users/me"
	if _, err := n.Identity(context.Background(), other); err == nil || f.identityReads.Load() != 0 {
		t.Fatal("MCP credential sent to REST endpoint")
	}
	f.page["url"] = "https://www.notion.so/" + notionTestOther
	if _, err := n.Refresh(context.Background(), f.session, f.session.Account.Selections[0], ""); err == nil {
		t.Fatal("wrong source returned")
	}
	for _, raw := range []string{"https://evil.example/" + notionTestPage, "https://www.notion.so@evil.example/" + notionTestPage, "https://www.notion.so:443/" + notionTestPage, "Same title"} {
		if _, err := notionID(raw); err == nil {
			t.Fatal("unstable identity accepted", raw)
		}
	}
}

func TestNotionMalformedCompletenessNeverBecomesComplete(t *testing.T) {
	for key, value := range map[string]any{"truncated": "true", "unknown_block_count": -1.0, "unknown_block_ids": nil} {
		t.Run(key, func(t *testing.T) {
			f := newNotionFixture(t)
			f.page[key] = value
			n := NewNotionAdapter()
			if _, err := n.Refresh(context.Background(), f.session, f.session.Account.Selections[0], ""); err == nil {
				t.Fatal("malformed completeness accepted")
			}
		})
	}
	f := newNotionFixture(t)
	f.page["truncated"] = false
	remoteMap(f.page["metadata"])["truncated"] = true
	result, err := NewNotionAdapter().Refresh(context.Background(), f.session, f.session.Account.Selections[0], "")
	if err != nil || result.Items[0].State != "partial" || result.FullEnumeration {
		t.Fatal(result, err)
	}
}
func TestNotionAsyncReconcileRequiresExactTaskIdentity(t *testing.T) {
	for _, id := range []string{"", "task_other"} {
		t.Run(id, func(t *testing.T) {
			f := newNotionFixture(t)
			f.addRead("notion-get-async-task", notionObject(map[string]any{"task_id": notionString()}, "task_id"), func(map[string]any) map[string]any {
				return map[string]any{"id": id, "status": "succeeded", "result": map[string]any{"page_id": notionTestPage}}
			})
			p := googleTestProposal(f.session, notionTestUpdate())
			p.Receipt = &Receipt{Outcome: "unknown", RemoteID: "notion-task:task_expected", RecordedAt: time.Now().Add(-3 * time.Second).UTC().Format(time.RFC3339Nano)}
			if _, err := NewNotionAdapter().Reconcile(context.Background(), f.session, p); err == nil || f.writes.Load() != 0 {
				t.Fatal("task identity was not verified")
			}
		})
	}
}
func TestNotionPageIDsPreserveStableURLIdentity(t *testing.T) {
	for _, raw := range []string{notionTestPage, strings.ReplaceAll(notionTestPage, "-", ""), "https://www.notion.so/a-b-" + strings.ReplaceAll(notionTestPage, "-", "") + "?v=ignored", "https://notion.com/Title-" + strings.ReplaceAll(notionTestPage, "-", "")} {
		got, err := notionID(raw)
		if err != nil || got != notionTestPage {
			t.Fatal(raw, got, err)
		}
	}
}
