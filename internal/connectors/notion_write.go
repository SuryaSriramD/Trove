package connectors

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func notionOnlyKeys(data map[string]any, keys ...string) bool {
	allowed := map[string]bool{}
	for _, k := range keys {
		allowed[k] = true
	}
	for k := range data {
		if !allowed[k] {
			return false
		}
	}
	return true
}
func notionAction(action Action, sel Selection) (string, map[string]any, error) {
	if sel.Kind != "page" {
		return "", nil, fail("SELECTION_DENIED", "Select a page explicitly for Notion page writes")
	}
	if len(action.Arguments) > 1<<20 || mcpJSONStructure(action.Arguments) != nil {
		return "", nil, fail("INVALID_REQUEST", "Notion reviewed arguments exceed their structure bound")
	}
	var args map[string]any
	if json.Unmarshal(action.Arguments, &args) != nil || args == nil {
		return "", nil, fail("INVALID_REQUEST", "Notion reviewed arguments must be an object")
	}
	if async, ok := args["allow_async"]; ok && async != false {
		return "", nil, fail("INVALID_REQUEST", "Notion writes initially request synchronous execution")
	}
	switch action.Kind {
	case "notion.page.create":
		if action.RemoteID != "" || !notionOnlyKeys(args, "parent", "pages", "allow_async") {
			return "", nil, fail("INVALID_REQUEST", "A Notion create requires one explicitly selected parent")
		}
		parent := remoteMap(args["parent"])
		if !notionOnlyKeys(parent, "page_id") || remoteString(parent["page_id"]) != sel.ID {
			return "", nil, fail("SELECTION_DENIED", "The Notion destination must be the exact selected parent page")
		}
		pages, ok := args["pages"].([]any)
		if !ok || len(pages) != 1 {
			return "", nil, fail("INVALID_REQUEST", "Review one Notion page creation at a time")
		}
		page := remoteMap(pages[0])
		properties := remoteMap(page["properties"])
		_, hasContent := page["content"].(string)
		title, hasTitle := properties["title"].(string)
		if !notionOnlyKeys(page, "properties", "content") || !notionOnlyKeys(properties, "title") || !hasContent || !hasTitle || strings.TrimSpace(title) == "" {
			return "", nil, fail("INVALID_REQUEST", "New Notion pages require a title and exact markdown content")
		}
		return "notion-create-pages", args, nil
	case "notion.page.update":
		if action.RemoteID != sel.ID || remoteString(args["page_id"]) != sel.ID {
			return "", nil, fail("SELECTION_DENIED", "The update must name the exact selected Notion page")
		}
		switch remoteString(args["command"]) {
		case "update_properties":
			if !notionOnlyKeys(args, "page_id", "command", "properties", "allow_async") || len(remoteMap(args["properties"])) == 0 {
				return "", nil, fail("INVALID_REQUEST", "Review an explicit nonempty set of page property changes")
			}
		case "replace_content":
			if !notionOnlyKeys(args, "page_id", "command", "new_str", "allow_async") {
				return "", nil, fail("INVALID_REQUEST", "Review the exact replacement page content")
			}
			if _, ok := args["new_str"].(string); !ok {
				return "", nil, fail("INVALID_REQUEST", "Replacement page content must be a string")
			}
		case "update_content":
			// Different supported tool versions name their batch updates differently.
			// The live JSON schema below must accept the exact selected spelling.
			if !notionOnlyKeys(args, "page_id", "command", "updates", "content_updates", "allow_async") {
				return "", nil, fail("INVALID_REQUEST", "Review exact content replacements only")
			}
			values, ok := args["updates"].([]any)
			if !ok {
				values, ok = args["content_updates"].([]any)
			}
			if _, a := args["updates"]; a {
				if _, b := args["content_updates"]; b {
					return "", nil, fail("INVALID_REQUEST", "Only one content replacement batch is allowed")
				}
			}
			if !ok || len(values) == 0 || len(values) > 100 {
				return "", nil, fail("INVALID_REQUEST", "Content replacement batch must contain between one and 100 changes")
			}
			for _, v := range values {
				change := remoteMap(v)
				old, oldOK := change["old_str"].(string)
				next, newOK := change["new_str"].(string)
				if !notionOnlyKeys(change, "old_str", "new_str") || !oldOK || !newOK || old == "" || old == next {
					return "", nil, fail("INVALID_REQUEST", "Each content change requires a nonempty exact match and a different replacement")
				}
			}
		default:
			return "", nil, Unsupported("Notion page update command")
		}
		return "notion-update-page", args, nil
	default:
		return "", nil, Unsupported(action.Kind)
	}
}
func notionReview(action Action, snapshot Snapshot, tool *mcp.Tool) Action {
	action.Version = 1
	action.ExpectedRevision = remoteHash([]any{snapshot.RemoteRevision, tool})
	action.Effects = []string{"Change only the explicitly selected Notion page or create one page beneath it.", "Notion page content, property values and destination are exactly those shown in the reviewed arguments.", "Notion may notify collaborators or expose changes according to the destination's existing sharing settings.", "The page edit timestamp and tool definition are rechecked immediately before submission. Notion MCP does not expose an atomic page revision precondition; a concurrent remote edit can still race submission.", "An interrupted, altered or asynchronous result remains unknown until reconciled. The connector never automatically resends it."}
	if action.Kind == "notion.page.update" {
		var a map[string]any
		_ = json.Unmarshal(action.Arguments, &a)
		if a["command"] == "replace_content" {
			action.Effects = append(action.Effects, "Replace the complete page body, including removed blocks, with the reviewed markdown.")
		}
	}
	return action
}
func (n *notionAdapter) prepare(ctx context.Context, s Session, action Action) (Action, *mcp.ClientSession, string, error) {
	if err := notionSession(s); err != nil {
		return action, nil, "", err
	}
	sel, err := Selected(s.Account, action.SelectionID)
	if err != nil {
		return action, nil, "", err
	}
	if _, err = notionSelected(s, sel, sel.ID); err != nil {
		return action, nil, "", err
	}
	name, args, err := notionAction(action, sel)
	if err != nil {
		return action, nil, "", err
	}
	if err = n.identityCheck(ctx, s); err != nil {
		return action, nil, "", err
	}
	snapshot, err := n.fetch(ctx, s, sel)
	if err != nil {
		return action, nil, "", err
	}
	if err = notionWritableSnapshot(snapshot); err != nil {
		return action, nil, "", err
	}
	cs, err := connectMCP(ctx, s, nil)
	if err != nil {
		return action, nil, "", err
	}
	tool, err := mcpTool(ctx, cs, name)
	if err == nil {
		err = validateMCPArguments(tool.InputSchema, action.Arguments)
	}
	if err != nil {
		cs.Close()
		return action, nil, "", err
	}
	// Enforce exact old-string matches before displaying the proposal. The
	// server repeats this check atomically for its update_content command.
	if args["command"] == "update_content" {
		values, _ := args["updates"].([]any)
		if values == nil {
			values, _ = args["content_updates"].([]any)
		}
		for _, v := range values {
			old := remoteString(remoteMap(v)["old_str"])
			if strings.Count(snapshot.Text, old) != 1 {
				cs.Close()
				return action, nil, "", fail("REMOTE_CHANGED", "A reviewed content replacement no longer has exactly one source match")
			}
		}
	}
	prepared := notionReview(action, snapshot, tool)
	if action.ExpectedRevision != "" && action.ExpectedRevision != prepared.ExpectedRevision {
		cs.Close()
		return action, nil, "", fail("REMOTE_CHANGED", "Notion source or tool definition changed after review")
	}
	return prepared, cs, name, nil
}
func (n *notionAdapter) Prepare(ctx context.Context, s Session, action Action) (Action, error) {
	prepared, cs, _, err := n.prepare(ctx, s, action)
	if cs != nil {
		cs.Close()
	}
	return prepared, err
}
func (n *notionAdapter) Check(ctx context.Context, s Session, action Action) error {
	current, err := n.Prepare(ctx, s, action)
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(current, action) {
		return fail("REMOTE_CHANGED", "Notion reviewed arguments or effects changed")
	}
	return nil
}
func (n *notionAdapter) Send(ctx context.Context, s Session, p Proposal) (Receipt, error) {
	if p.AccountID != s.Account.ID || p.AccountRevision != s.Account.Revision {
		return Receipt{}, fail("ACCOUNT_CHANGED", "Notion account selection changed after review")
	}
	if err := RequireIdentity(p.Identity, s.Account.Identity); err != nil {
		return Receipt{}, err
	}
	current, cs, name, err := n.prepare(ctx, s, p.Action)
	if err != nil {
		return Receipt{}, err
	}
	defer cs.Close()
	if !reflect.DeepEqual(current, p.Action) {
		return Receipt{}, fail("REMOTE_CHANGED", "Notion reviewed arguments or effects changed")
	}
	if err = ctx.Err(); err != nil {
		return Receipt{}, err
	}
	result, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: json.RawMessage(p.Action.Arguments)})
	if err != nil || result == nil || result.IsError {
		return Receipt{}, Unknown("Notion may have applied the change; verify the remote outcome before any new proposal")
	}
	data, err := notionResult(result)
	if err != nil {
		return Receipt{}, Unknown("Notion returned an unverifiable write result; the operation will not be resent")
	}
	return notionWriteReceipt(p, data), nil
}
func notionTask(data map[string]any) string {
	task := remoteMap(data["async_task"])
	if task == nil && remoteString(data["object"]) == "async_task" {
		task = data
	}
	id := remoteString(task["id"])
	if id == "" || len(id) > 200 {
		return ""
	}
	for _, c := range id {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_' || c == '-') {
			return ""
		}
	}
	return id
}
func notionWriteReceipt(p Proposal, data map[string]any) Receipt {
	if _, exists := data["error"]; exists {
		return ReceiptFor(p, "unknown", p.Action.RemoteID, remoteHash(data), "", "Notion returned a remote error without a definitive operation receipt; verify the source.")
	}
	if task := notionTask(data); task != "" {
		return ReceiptFor(p, "unknown", "notion-task:"+task, notionPollRevision(data), "", "Notion queued this write. Reconcile its task receipt; this is not completed execution.")
	}
	if warnings, ok := data["warnings"]; ok && warnings != nil {
		list, valid := warnings.([]any)
		if !valid || len(list) > 0 {
			return ReceiptFor(p, "unknown", p.Action.RemoteID, remoteHash(data), "", "Notion reported altered or ignored input. Inspect the source and exact result before making another proposal.")
		}
	}
	id := remoteString(data["page_id"])
	if p.Action.Kind == "notion.page.create" {
		pages, ok := data["pages"].([]any)
		if !ok || len(pages) != 1 {
			return ReceiptFor(p, "unknown", "", remoteHash(data), "", "Notion did not return exactly one created page identity; verify the destination.")
		}
		page := remoteMap(pages[0])
		id = remoteString(page["id"])
		if id == "" {
			id = remoteString(page["url"])
		}
	}
	parsed, err := notionID(id)
	if err != nil || p.Action.Kind == "notion.page.update" && parsed != p.Action.RemoteID {
		return ReceiptFor(p, "unknown", p.Action.RemoteID, remoteHash(data), "", "Notion returned an unexpected result identity; verify the remote page.")
	}
	return ReceiptFor(p, "succeeded", parsed, remoteHash(data), "https://www.notion.so/"+strings.ReplaceAll(parsed, "-", ""), "Notion acknowledged the exact reviewed page operation without warnings. The server acknowledgement does not prove a collaborator saw it.")
}

// A retained server task handle is the only automatic reconciliation path.
// Keep the suggested backoff in the receipt's opaque revision field so a
// restart cannot turn queued work into rapid polling or a write replay.
func notionPollRevision(data map[string]any) string {
	source := data
	if task := remoteMap(data["async_task"]); task != nil {
		source = task
	}
	seconds := 2
	if value, ok := source["poll_after_seconds"].(float64); ok && value >= 1 && value <= 86400 {
		seconds = int(value)
		if float64(seconds) < value {
			seconds++
		}
	}
	return "poll-after:" + strconv.Itoa(seconds)
}
func notionPollReady(receipt *Receipt) error {
	seconds := 2
	if strings.HasPrefix(receipt.RemoteRevision, "poll-after:") {
		parsed, err := strconv.Atoi(strings.TrimPrefix(receipt.RemoteRevision, "poll-after:"))
		if err != nil || parsed < 1 || parsed > 86400 {
			return fail("INVALID_RESPONSE", "Notion task polling receipt is invalid")
		}
		seconds = parsed
	}
	recorded, err := time.Parse(time.RFC3339Nano, receipt.RecordedAt)
	if err != nil {
		return fail("INVALID_RESPONSE", "Notion task polling receipt has no valid timestamp")
	}
	remaining := time.Until(recorded.Add(time.Duration(seconds) * time.Second))
	if remaining > 0 {
		return &Failure{Code: "RATE_LIMITED", Message: "Wait for the Notion task polling interval before reconciling again", RetryAfter: remaining}
	}
	return nil
}
func (n *notionAdapter) Reconcile(ctx context.Context, s Session, p Proposal) (Receipt, error) {
	if p.AccountID != s.Account.ID || p.AccountRevision != s.Account.Revision {
		return Receipt{}, fail("ACCOUNT_CHANGED", "Notion account selection changed")
	}
	if err := RequireIdentity(p.Identity, s.Account.Identity); err != nil {
		return Receipt{}, err
	}
	sel, err := Selected(s.Account, p.Action.SelectionID)
	if err != nil {
		return Receipt{}, err
	}
	if _, err = notionSelected(s, sel, sel.ID); err != nil {
		return Receipt{}, err
	}
	if err = n.identityCheck(ctx, s); err != nil {
		return Receipt{}, err
	}
	if p.Receipt == nil || !strings.HasPrefix(p.Receipt.RemoteID, "notion-task:") {
		return ReceiptFor(p, "unknown", p.Action.RemoteID, "", "", "No remote operation receipt is available. Inspect Notion explicitly; this connector never resends an uncertain write."), nil
	}
	if err := notionPollReady(p.Receipt); err != nil {
		return Receipt{}, err
	}
	id := strings.TrimPrefix(p.Receipt.RemoteID, "notion-task:")
	if notionTask(map[string]any{"object": "async_task", "id": id}) != id || id == "" {
		return Receipt{}, fail("INVALID_REQUEST", "Notion task receipt is invalid")
	}
	data, err := notionRead(ctx, s, "notion-get-async-task", map[string]any{"task_id": id})
	if err != nil {
		return Receipt{}, err
	}
	if got := remoteString(data["id"]); got != id {
		return Receipt{}, fail("INVALID_RESPONSE", "Notion returned a different task receipt")
	}
	switch remoteString(data["status"]) {
	case "succeeded":
		result := remoteMap(data["result"])
		if result == nil {
			return ReceiptFor(p, "unknown", p.Receipt.RemoteID, "", "", "Notion reported task completion without a verifiable operation result."), nil
		}
		receipt := notionWriteReceipt(p, result)
		if receipt.Outcome == "unknown" {
			receipt.RemoteID = p.Receipt.RemoteID
		}
		return receipt, nil
	case "failed":
		return ReceiptFor(p, "failed", p.Receipt.RemoteID, remoteHash(data), "", "Notion reported this asynchronous operation failed. Inspect the source before creating another reviewed proposal."), nil
	case "queued", "running", "retrying":
		return ReceiptFor(p, "unknown", p.Receipt.RemoteID, notionPollRevision(data), "", fmt.Sprintf("Notion task is %s. Reconcile again after the remote task has progressed; no operation was resent.", remoteString(data["status"]))), nil
	default:
		return Receipt{}, fail("INVALID_RESPONSE", "Notion task outcome is unavailable")
	}
}
