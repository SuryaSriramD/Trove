package workspace

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"strings"

	"trove.local/phase0/contracts"
)

type AIProposal struct {
	RequestHash string                              `json:"requestHash,omitempty"`
	Changes     []ActionReview                      `json:"changes,omitempty"`
	Before      string                              `json:"before,omitempty"`
	After       string                              `json:"after"`
	ID          string                              `json:"id"`
	RunID       string                              `json:"runId"`
	State       string                              `json:"state"`
	Kind        string                              `json:"kind"`
	PageID      string                              `json:"pageId"`
	Title       string                              `json:"title"`
	Body        string                              `json:"body"`
	Commands    []any                               `json:"commands"`
	Guards      []contracts.ServiceExpectedRevision `json:"expectedRevisions"`
}

func prepareAIAction(ctx context.Context, k *Kernel, e contracts.ServiceExecutionContext, r map[string]any) (any, error) {
	if e.Actor.Kind != "user" || e.Origin != "user" {
		return nil, Fail("APPROVAL_REQUIRED", "Only the user may review AI actions")
	}
	run, err := k.AIRun(ctx, e, str(r["runId"]))
	if err != nil {
		return nil, err
	}
	if run.State != "completed" {
		return nil, Fail("CONFLICT", "Only completed generations can be reviewed")
	}
	for _, s := range run.Sources {
		if err = k.aiEvidenceValid(ctx, e, s); err != nil {
			return nil, err
		}
	}
	if !skillAllowsProposal(run.Request.Skill, str(r["kind"])) {
		return nil, Fail("SCOPE_DENIED", "The skill run did not permit this proposal category")
	}
	p := AIProposal{ID: NewID(), RunID: run.ID, State: "draft", Kind: str(r["kind"]), PageID: run.Request.PageID, Title: strings.TrimSpace(str(r["title"])), Body: str(r["body"]), Commands: []any{}, Guards: []contracts.ServiceExpectedRevision{}}
	if p.Body == "" {
		p.Body = run.Text
	}
	if strings.TrimSpace(p.Body) == "" || len(p.Body) > 64000 {
		return nil, Fail("INVALID_REQUEST", "Review text between 1 and 64000 bytes")
	}
	effectRun := p.RunID
	if run.Request.Skill != nil {
		effectRun = run.Request.Skill.InvocationID
	}
	hash := sha256.Sum256(marshal(map[string]any{"runId": effectRun, "kind": p.Kind, "title": p.Title, "body": p.Body}))
	p.RequestHash = hex.EncodeToString(hash[:])
	p.After = p.Body
	guards := map[string]bool{}
	guard := func(kind, id, rev string) {
		key := kind + ":" + id
		if !guards[key] {
			guards[key] = true
			p.Guards = append(p.Guards, required(kind, id, rev))
		}
	}
	guardCurrent := func(kind, id string) error {
		var rev string
		table, ok := entityTables[kind]
		if !ok {
			return Fail("INVALID_REQUEST", "Unknown guard")
		}
		if err := k.db.QueryRowContext(ctx, "SELECT revision FROM "+table+" WHERE id=?", id).Scan(&rev); err != nil {
			return err
		}
		guard(kind, id, rev)
		return nil
	}
	for _, s := range run.Sources {
		guard("page", string(s.Location.PageId), string(s.Location.SourceRevision))
		if s.MemoryID != "" {
			guard("memory", s.MemoryID, s.MemoryRevision)
		}
	}
	appendBlocks := func(page string) {
		for _, b := range aiDraftBlocks(p.Body) {
			id := NewID()
			b["blockId"] = id
			b["pageId"] = page
			p.Commands = append(p.Commands, map[string]any{"kind": "block.insert", "block": b})
			guard("block", id, "0")
		}
	}
	switch p.Kind {
	case "create_database", "mermaid", "replace_code", "workspace_patch":
		if err = prepareAdvancedAI(ctx, k, e, run, &p, guard, guardCurrent); err != nil {
			return nil, err
		}
	case "append", "replace_block", "replace_page":
		if p.PageID == "" {
			return nil, Fail("INVALID_REQUEST", "Open the source page to apply a block patch")
		}
		if err = k.authorizePage(ctx, k.db, e, p.PageID); err != nil {
			return nil, err
		}
		if err = guardCurrent("page", p.PageID); err != nil {
			return nil, err
		}
		var record, collection string
		if err = k.db.QueryRowContext(ctx, "SELECT id,collection_id FROM records WHERE page_id=?", p.PageID).Scan(&record, &collection); err == nil {
			if err = guardCurrent("record", record); err != nil {
				return nil, err
			}
			if err = guardCurrent("collection", collection); err != nil {
				return nil, err
			}
		} else if err != sql.ErrNoRows {
			return nil, err
		}
		if p.Kind == "replace_block" {
			if run.Request.BlockID == "" {
				return nil, Fail("INVALID_REQUEST", "Select a source block before requesting a rewrite")
			}
			var raw, typ, rev string
			if err = k.db.QueryRowContext(ctx, "SELECT data_json,type,revision FROM blocks WHERE id=? AND page_id=?", run.Request.BlockID, p.PageID).Scan(&raw, &typ, &rev); err != nil {
				return nil, err
			}
			if !aiPlainBlock(typ) {
				return nil, Fail("INVALID_REQUEST", "This block type cannot be safely replaced by a text patch")
			}
			var data map[string]any
			json.Unmarshal([]byte(raw), &data)
			text := p.Body
			p.Before = searchReadable(data)
			if run.Request.Selection != "" {
				before := searchReadable(data)
				if strings.Count(before, run.Request.Selection) != 1 {
					return nil, Fail("CONFLICT", "Selection must occur exactly once in the source block")
				}
				text = strings.Replace(before, run.Request.Selection, p.Body, 1)
			}
			p.After = text
			data["richText"] = []any{map[string]any{"text": text}}
			p.Commands = append(p.Commands, map[string]any{"kind": "block.update", "blockId": run.Request.BlockID, "type": typ, "schemaVersion": 1, "data": data})
			guard("block", run.Request.BlockID, rev)
		} else {
			if p.Kind == "replace_page" {
				rows, err := k.db.QueryContext(ctx, "SELECT id,type,revision,parent_block_id,data_json FROM blocks WHERE page_id=? ORDER BY sort_key", p.PageID)
				if err != nil {
					return nil, err
				}
				for rows.Next() {
					var id, typ, rev, raw string
					var parent sql.NullString
					if err = rows.Scan(&id, &typ, &rev, &parent, &raw); err != nil {
						rows.Close()
						return nil, err
					}
					if !aiPlainBlock(typ) || parent.Valid {
						rows.Close()
						return nil, Fail("INVALID_REQUEST", "Whole-page replacement requires plain text blocks without nesting; use append or a selected block patch")
					}
					var data any
					if err = json.Unmarshal([]byte(raw), &data); err != nil {
						rows.Close()
						return nil, err
					}
					p.Before += searchReadable(data) + "\n"
					p.Commands = append(p.Commands, map[string]any{"kind": "block.delete", "blockId": id, "includeDescendants": false})
					guard("block", id, rev)
				}
				err = rows.Err()
				rows.Close()
				if err != nil {
					return nil, err
				}
			}
			appendBlocks(p.PageID)
		}
	case "create_page", "create_task":
		if p.Title == "" || len(p.Title) > 512 {
			return nil, Fail("INVALID_REQUEST", "Review a title up to 512 bytes")
		}
		p.PageID = NewID()
		guard("page", p.PageID, "0")
		if p.Kind == "create_page" {
			p.Commands = append(p.Commands, map[string]any{"kind": "page.create", "pageId": p.PageID, "title": p.Title})
		} else {
			var collection, status string
			err = k.db.QueryRowContext(ctx, "SELECT c.id,p.id FROM collections c JOIN properties p ON p.collection_id=c.id AND p.type='status' WHERE c.system_role='tasks' ORDER BY p.id LIMIT 1").Scan(&collection, &status)
			if err == sql.ErrNoRows {
				taskPage, titleProperty, due := NewID(), NewID(), NewID()
				collection, status = NewID(), NewID()
				p.Commands = append(p.Commands, map[string]any{"kind": "tasks.ensure", "pageId": taskPage, "collectionId": collection, "titlePropertyId": titleProperty, "statusPropertyId": status, "duePropertyId": due, "defaultOptionId": NewID(), "doneOptionId": NewID()})
				guard("page", taskPage, "0")
				guard("collection", collection, "0")
				guard("property", titleProperty, "0")
				guard("property", status, "0")
				guard("property", due, "0")
			} else if err != nil {
				return nil, err
			} else {
				if err = guardCurrent("collection", collection); err != nil {
					return nil, err
				}
				if err = guardCurrent("property", status); err != nil {
					return nil, err
				}
			}
			record := NewID()
			guard("record", record, "0")
			p.Commands = append(p.Commands, map[string]any{"kind": "task.create", "recordId": record, "pageId": p.PageID, "title": p.Title, "collectionId": collection, "statusPropertyId": status})
		}
		appendBlocks(p.PageID)
	default:
		return nil, Fail("INVALID_REQUEST", "Unknown reviewed action")
	}
	if run.Request.Skill != nil {
		for _, guard := range p.Guards {
			if guard.Target.EntityType == "page" && string(guard.Revision) != "0" && !skillSubset([]string{string(guard.Target.EntityId)}, run.Request.Skill.SourcePageIDs) {
				return nil, Fail("SCOPE_DENIED", "A skill proposal cannot change or read an unselected page")
			}
		}
	}
	if len(p.Commands) > 200 {
		return nil, Fail("RESOURCE_EXHAUSTED", "Draft contains too many block changes")
	}
	// Validate the same public batch contract used at commit before showing a preview.
	req := map[string]any{"contractVersion": 1, "requestId": NewID(), "workspaceId": k.id, "mutation": map[string]any{"idempotencyKey": "ai-proposal-" + p.ID, "scopeGrantId": e.ScopeGrantId, "expectedRevisions": p.Guards}, "commands": p.Commands}
	if _, err = contracts.DecodeBatch(marshal(req)); err != nil {
		return nil, Fail("INVALID_REQUEST", err.Error())
	}
	err = k.write(ctx, func(tx *sql.Tx) error {
		var previous, state string
		query := "SELECT body_json,state FROM ai_proposals WHERE run_id=? AND json_extract(body_json,'$.requestHash')=? LIMIT 1"
		key := p.RunID
		if run.Request.Skill != nil {
			query = "SELECT p.body_json,p.state FROM ai_proposals p JOIN ai_runs r ON r.id=p.run_id WHERE json_extract(r.request_json,'$.skill.invocationId')=? AND json_extract(p.body_json,'$.requestHash')=? LIMIT 1"
			key = run.Request.Skill.InvocationID
		}
		err := tx.QueryRowContext(ctx, query, key, p.RequestHash).Scan(&previous, &state)
		if err == nil {
			if err = json.Unmarshal([]byte(previous), &p); err != nil {
				return err
			}
			p.State = state
			return nil
		}
		if err != sql.ErrNoRows {
			return err
		}
		_, err = tx.ExecContext(ctx, "INSERT INTO ai_proposals VALUES(?,?,'draft',?,?)", p.ID, run.ID, string(marshal(p)), now())
		return err
	})
	return p, err
}
func aiPlainBlock(t string) bool {
	switch t {
	case "paragraph", "heading_1", "heading_2", "heading_3", "heading_4", "bulleted_list_item", "numbered_list_item", "quote":
		return true
	}
	return false
}
func aiDraftBlocks(body string) []map[string]any {
	out := []map[string]any{}
	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		typ := "paragraph"
		for _, h := range []struct{ prefix, kind string }{{"#### ", "heading_4"}, {"### ", "heading_3"}, {"## ", "heading_2"}, {"# ", "heading_1"}, {"- ", "bulleted_list_item"}, {"* ", "bulleted_list_item"}, {"> ", "quote"}} {
			if strings.HasPrefix(line, h.prefix) {
				typ = h.kind
				line = strings.TrimPrefix(line, h.prefix)
				break
			}
		}
		out = append(out, map[string]any{"type": typ, "schemaVersion": 1, "data": map[string]any{"richText": []any{map[string]any{"text": line}}}})
	}
	return out
}
func applyAIAction(ctx context.Context, k *Kernel, e contracts.ServiceExecutionContext, r map[string]any) (any, error) {
	k.reviewMu.Lock()
	defer k.reviewMu.Unlock()
	if e.Actor.Kind != "user" || e.Origin != "user" {
		return nil, Fail("APPROVAL_REQUIRED", "AI proposals require explicit user approval")
	}
	var raw, state string
	if err := k.db.QueryRowContext(ctx, "SELECT body_json,state FROM ai_proposals WHERE id=?", r["proposalId"]).Scan(&raw, &state); err != nil {
		return nil, err
	}
	var p AIProposal
	if err := json.Unmarshal([]byte(raw), &p); err != nil {
		return nil, err
	}
	run, err := k.AIRun(ctx, e, p.RunID)
	if err != nil {
		return nil, err
	}
	if state == "applied" {
		return map[string]any{"pageId": p.PageID, "state": state}, nil
	}
	for _, s := range run.Sources {
		if err = k.aiEvidenceValid(ctx, e, s); err != nil {
			return nil, err
		}
	}
	runID := contracts.DomainId(p.RunID)
	target := k.Grant(contracts.ServiceActor{Kind: "local_ai", ActorId: e.Actor.ActorId, RunId: &runID}, "ai", e.Scope)
	defer k.RevokeGrant(string(target.ScopeGrantId))
	req := map[string]any{"contractVersion": 1, "requestId": NewID(), "workspaceId": k.id, "mutation": map[string]any{"idempotencyKey": "ai-proposal-" + p.ID, "scopeGrantId": target.ScopeGrantId, "expectedRevisions": p.Guards}, "commands": p.Commands}
	typed, err := contracts.DecodeBatch(marshal(req))
	if err != nil {
		return nil, err
	}
	approval, err := k.Approve(ctx, e, typed, target)
	if err != nil {
		return nil, err
	}
	req["mutation"].(map[string]any)["approvalId"] = approval
	ctx = context.WithValue(ctx, approvalSourceGrantKey{}, e)
	var response map[string]any
	json.Unmarshal(k.batch(ctx, target, marshal(req)), &response)
	if response["ok"] != true {
		d := response["error"].(map[string]any)
		return nil, Fail(str(d["code"]), str(d["message"]))
	}
	return map[string]any{"pageId": p.PageID, "state": "applied"}, nil
}
func init() {
	RegisterCommitProjection(func(m *Mutation) error {
		key := m.Request.Mutation.IdempotencyKey
		if !strings.HasPrefix(key, "ai-proposal-") {
			return nil
		}
		var raw string
		id := strings.TrimPrefix(key, "ai-proposal-")
		if err := m.Tx.QueryRowContext(m.Context, "SELECT body_json FROM ai_proposals WHERE id=? AND state='draft'", id).Scan(&raw); err != nil {
			return err
		}
		var p AIProposal
		if err := json.Unmarshal([]byte(raw), &p); err != nil {
			return err
		}
		// A renderer cannot use a proposal receipt to apply a different command batch.
		var canonicalCommands any
		json.Unmarshal(marshal(m.Request.Commands), &canonicalCommands)
		if string(marshal(canonicalCommands)) != string(marshal(p.Commands)) || string(marshal(m.Request.Mutation.ExpectedRevisions)) != string(marshal(p.Guards)) {
			return Fail("APPROVAL_REQUIRED", "The reviewed proposal or its source revision guards changed")
		}
		var actor string
		if err := m.Tx.QueryRowContext(m.Context, "SELECT actor_id FROM ai_runs WHERE id=?", p.RunID).Scan(&actor); err != nil {
			return err
		}
		if actor != string(m.Execution.Actor.ActorId) {
			return Fail("SCOPE_DENIED", "Proposal belongs to another actor")
		}
		if m.Execution.Actor.Kind != "local_ai" || m.Execution.Origin != "ai" || m.Request.Mutation.ApprovalId == nil {
			return Fail("APPROVAL_REQUIRED", "Only an explicitly approved local AI proposal may commit")
		}
		_, err := m.Exec("UPDATE ai_proposals SET state='applied' WHERE id=?", id)
		return err
	})
}
