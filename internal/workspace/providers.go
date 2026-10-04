package workspace

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"trove.local/phase0/contracts"
)

// ProviderBridge is a capability broker, not a persistence owner. It is created
// by the trusted shell and only calls the same public workspace services as UI.
type ProviderBridge struct {
	mu          sync.Mutex
	approvalMu  sync.Mutex
	kernel      *Kernel
	owner       contracts.ServiceExecutionContext
	connections map[string]*providerConnection
}
type providerConnection struct {
	mcp       *providerMCP
	execution contracts.ServiceExecutionContext
	token     string
	tools     map[string]bool
	info      ProviderConnection
	cancel    context.CancelFunc
	ctx       context.Context
}
type ProviderConnection struct {
	ID             string   `json:"id"`
	Provider       string   `json:"provider"`
	State          string   `json:"state"`
	AuthMode       string   `json:"authMode"`
	BillingMode    string   `json:"billingMode"`
	Account        string   `json:"account"`
	AdapterVersion string   `json:"adapterVersion"`
	PageIDs        []string `json:"pageIds"`
	Tools          []string `json:"tools"`
	Token          string   `json:"token,omitempty"`
}
type ProviderProposal struct {
	PlanHash     string                        `json:"planHash,omitempty"`
	Changes      []ActionReview                `json:"changes,omitempty"`
	ID           string                        `json:"id"`
	ConnectionID string                        `json:"connectionId"`
	State        string                        `json:"state"`
	Request      contracts.ServiceBatchRequest `json:"request"`
	Result       json.RawMessage               `json:"result,omitempty"`
}

var providerTools = map[string]bool{"readPage": true, "queryRecords": true, "search.query": true, "resolveCitation": true, "allocateIds": true, "proposeChanges": true, "proposeActions": true}

func init() {
	RegisterInitializer(func(ctx context.Context, k *Kernel) error {
		return k.write(ctx, func(tx *sql.Tx) error {
			_, err := tx.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS provider_connections(id TEXT PRIMARY KEY,body_json TEXT NOT NULL,state TEXT NOT NULL); CREATE TABLE IF NOT EXISTS provider_proposals(id TEXT PRIMARY KEY,connection_id TEXT NOT NULL,state TEXT NOT NULL,body_json TEXT NOT NULL,result_json TEXT,created_at TEXT NOT NULL); CREATE TABLE IF NOT EXISTS provider_audit(id TEXT PRIMARY KEY,connection_id TEXT NOT NULL,action TEXT NOT NULL,state TEXT NOT NULL,detail TEXT NOT NULL,created_at TEXT NOT NULL); UPDATE provider_connections SET state='disconnected'; UPDATE provider_proposals SET state='applied',result_json=(SELECT json_object('ok',json('true'),'result',json(response_json)) FROM idempotency_receipts WHERE actor_id=provider_proposals.connection_id AND idempotency_key=json_extract(provider_proposals.body_json,'$.request.mutation.idempotencyKey')) WHERE result_json IS NULL AND EXISTS(SELECT 1 FROM idempotency_receipts WHERE actor_id=provider_proposals.connection_id AND idempotency_key=json_extract(provider_proposals.body_json,'$.request.mutation.idempotencyKey')); UPDATE provider_proposals SET state='interrupted' WHERE state='pending';`)
			return err
		})
	})
}
func NewProviderBridge(k *Kernel, owner contracts.ServiceExecutionContext) *ProviderBridge {
	return &ProviderBridge{kernel: k, owner: owner, connections: map[string]*providerConnection{}}
}
func (b *ProviderBridge) audit(ctx context.Context, id, action, state, detail string) {
	_ = b.kernel.write(ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, "INSERT INTO provider_audit VALUES(?,?,?,?,?,?)", NewID(), id, action, state, truncateAI(detail, 1000), now())
		return err
	})
}
func (b *ProviderBridge) Connect(ctx context.Context, provider string, pages, tools []string) (ProviderConnection, error) {
	if provider != "mcp" {
		return ProviderConnection{}, Fail("UNSUPPORTED_VERSION", "Managed vendors must use the authenticated native runtime adapter")
	}
	return b.connect(ctx, ProviderConnection{Provider: provider, AuthMode: "paired_local", BillingMode: "client_owned", Account: "paired client", AdapterVersion: "1"}, pages, tools)
}

// ConnectManaged is a trusted-host entry point, never a workspace RPC tool.
// The host establishes vendor identity and OS containment before creating this
// same narrowly scoped, revocable proposal-only capability.
func (b *ProviderBridge) ConnectManaged(ctx context.Context, provider, account, authMode, billingMode string, pages, tools []string) (ProviderConnection, error) {
	valid := provider == "codex" && authMode == "siwc" && billingMode == "chatgpt_plan" || provider == "claude_code" && authMode == "vendor_owned" && (billingMode == "subscription" || billingMode == "api")
	if !valid || strings.TrimSpace(account) == "" || len(account) > 512 {
		return ProviderConnection{}, Fail("INVALID_REQUEST", "Invalid managed provider identity")
	}
	return b.connect(ctx, ProviderConnection{Provider: provider, AuthMode: authMode, BillingMode: billingMode, Account: account, AdapterVersion: "2"}, pages, tools)
}

func (b *ProviderBridge) connect(ctx context.Context, identity ProviderConnection, pages, tools []string) (ProviderConnection, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if err := requireOwnerScope(b.kernel, b.owner); err != nil {
		return ProviderConnection{}, err
	}
	if len(pages) == 0 || len(pages) > 100 || len(tools) == 0 {
		return ProviderConnection{}, Fail("INVALID_REQUEST", "Select 1–100 pages and explicit capabilities")
	}
	seen := map[string]bool{}
	for _, p := range pages {
		if seen[p] {
			return ProviderConnection{}, Fail("INVALID_REQUEST", "Duplicate context page")
		}
		seen[p] = true
		if err := b.kernel.authorizePage(ctx, b.kernel.db, b.owner, p); err != nil {
			return ProviderConnection{}, err
		}
		var n int
		if err := b.kernel.db.QueryRowContext(ctx, "SELECT count(*) FROM pages WHERE id=? AND deleted_at IS NULL", p).Scan(&n); err != nil || n != 1 {
			return ProviderConnection{}, Fail("NOT_FOUND", "Context page is unavailable")
		}
	}
	allow := map[string]bool{}
	for _, t := range tools {
		if !providerTools[t] {
			return ProviderConnection{}, Fail("SCOPE_DENIED", "Unsupported provider capability: "+t)
		}
		allow[t] = true
	}
	id := NewID()
	scopeIDs := []contracts.DomainId{}
	for _, p := range pages {
		scopeIDs = append(scopeIDs, contracts.DomainId(p))
	}
	e := b.kernel.Grant(contracts.ServiceActor{Kind: "external_client", ActorId: contracts.DomainId(id)}, "external", contracts.DomainScope{Value: contracts.DomainScopeVariant3{Kind: "pages", WorkspaceId: contracts.DomainId(b.kernel.id), PageIds: scopeIDs}})
	info := identity
	info.ID, info.State, info.PageIDs, info.Tools = id, "connected", append([]string(nil), pages...), append([]string(nil), tools...)
	err := b.kernel.write(ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, "INSERT INTO provider_connections VALUES(?,?,'connected')", id, string(marshal(info)))
		return err
	})
	if err != nil {
		b.kernel.RevokeGrant(string(e.ScopeGrantId))
		return ProviderConnection{}, err
	}
	live, cancel := context.WithCancel(context.Background())
	token := strings.ReplaceAll(NewID()+NewID(), "-", "")
	b.connections[id] = &providerConnection{execution: e, token: token, tools: allow, info: info, ctx: live, cancel: cancel}
	info.Token = token
	b.audit(ctx, id, "connect", "connected", "Explicit pages and capabilities approved")
	return info, nil
}
func (b *ProviderBridge) Status() []ProviderConnection {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := []ProviderConnection{}
	for _, c := range b.connections {
		out = append(out, c.info)
	}
	return out
}
func (b *ProviderBridge) Disconnect(ctx context.Context, id string) error {
	b.mu.Lock()
	c := b.connections[id]
	if c == nil {
		b.mu.Unlock()
		return nil
	}
	c.info.State = "disconnected"
	c.token = ""
	c.cancel()
	transport := c.mcp
	b.mu.Unlock()
	if transport != nil {
		transport.close()
	}
	b.kernel.RevokeGrant(string(c.execution.ScopeGrantId))
	err := b.kernel.write(ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, "UPDATE provider_connections SET state='disconnected' WHERE id=?", id)
		if err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, "UPDATE provider_proposals SET state='interrupted' WHERE connection_id=? AND state='pending'", id)
		return err
	})
	b.audit(ctx, id, "disconnect", "disconnected", "Local grant revoked; vendor sign-out is separate")
	return err
}
func (b *ProviderBridge) Close() {
	for _, c := range b.Status() {
		_ = b.Disconnect(context.Background(), c.ID)
	}
}
func (b *ProviderBridge) authenticate(id, token string) (*providerConnection, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	c := b.connections[id]
	if c == nil || c.info.State != "connected" {
		return nil, Fail("SCOPE_DENIED", "Connection is disconnected")
	}
	want, got := sha256.Sum256([]byte(c.token)), sha256.Sum256([]byte(token))
	if token == "" || subtle.ConstantTimeCompare(want[:], got[:]) != 1 {
		return nil, Fail("SCOPE_DENIED", "Invalid pairing credential")
	}
	copy := *c
	return &copy, nil
}
func (b *ProviderBridge) Call(ctx context.Context, id, token, tool string, args map[string]any) (json.RawMessage, error) {
	c, err := b.authenticate(id, token)
	if err != nil {
		return nil, err
	}
	if err := b.kernel.authorize(c.execution); err != nil {
		return nil, err
	}
	if !c.tools[tool] {
		b.audit(ctx, id, tool, "denied", "Capability not approved")
		return nil, Fail("SCOPE_DENIED", "Tool is not approved")
	}
	live, cancel := context.WithCancel(ctx)
	defer cancel()
	stop := context.AfterFunc(c.ctx, cancel)
	defer stop()
	if tool == "proposeActions" {
		return b.proposeActions(live, c, args)
	}
	if tool == "proposeChanges" {
		return b.propose(live, c, args)
	}
	clean := map[string]any{}
	for key, v := range args {
		clean[key] = v
	}
	if tool == "queryRecords" {
		if _, ok := clean["limit"]; !ok {
			clean["limit"] = 50
		}
	}
	if tool == "search.query" {
		clean["scope"] = c.execution.Scope
		if _, ok := clean["limit"]; !ok {
			clean["limit"] = 20
		}
		if _, ok := clean["mode"]; !ok {
			clean["mode"] = "keyword"
		}
	}
	clean["contractVersion"] = 1
	clean["requestId"] = NewID()
	clean["workspaceId"] = b.kernel.id
	response := b.kernel.Dispatch(live, c.execution, tool, marshal(clean))
	b.audit(ctx, id, tool, "completed", "Scoped service read")
	return response, nil
}
func (b *ProviderBridge) propose(ctx context.Context, c *providerConnection, args map[string]any) (json.RawMessage, error) {
	return b.persistProposal(ctx, c, args, "", nil)
}
func (b *ProviderBridge) persistProposal(ctx context.Context, c *providerConnection, args map[string]any, planHash string, changes []ActionReview) (json.RawMessage, error) {
	key := str(args["idempotencyKey"])
	if key == "" || len(key) > 80 {
		return nil, Fail("INVALID_REQUEST", "Proposal requires a stable idempotencyKey of 1–80 characters")
	}
	raw := marshal(map[string]any{"contractVersion": 1, "requestId": NewID(), "workspaceId": b.kernel.id, "mutation": map[string]any{"scopeGrantId": c.execution.ScopeGrantId, "idempotencyKey": "provider-" + key, "expectedRevisions": args["expectedRevisions"]}, "commands": args["commands"]})
	req, err := contracts.DecodeBatch(raw)
	if err != nil {
		return nil, Fail("INVALID_REQUEST", err.Error())
	}
	// Only the trusted typed planner may supply creation commands. New pages do
	// not become readable through the connection's unchanged explicit manifest.
	for _, g := range req.Mutation.ExpectedRevisions {
		if g.Revision != "0" {
			if err := b.kernel.authorizeEntity(ctx, b.kernel.db, c.execution, g.Target.EntityType, string(g.Target.EntityId)); err != nil {
				return nil, err
			}
		}
	}
	for _, command := range req.Commands {
		var v map[string]any
		json.Unmarshal(marshal(command), &v)
		switch str(v["kind"]) {
		case "page.update", "block.insert", "block.update", "record.setValue", "record.clearValue", "view.save":
		case "page.create", "collection.create", "collection.addProperty", "task.create", "record.create":
			if planHash == "" {
				return nil, Fail("SCOPE_DENIED", "Use reviewed named actions to create content")
			}
		default:
			return nil, Fail("SCOPE_DENIED", "This bridge allows only updates to approved context; deletion and broader access require separate authorization")
		}
	}
	hash, err := commandHash(req, true)
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256([]byte(c.info.ID + ":" + key))
	id := hex.EncodeToString(sum[:])
	p := ProviderProposal{ID: id, ConnectionID: c.info.ID, State: "pending", Request: req, PlanHash: planHash, Changes: changes}
	err = b.kernel.write(ctx, func(tx *sql.Tx) error {
		var previous, previousState string
		var previousResult sql.NullString
		err := tx.QueryRowContext(ctx, "SELECT body_json,state,result_json FROM provider_proposals WHERE id=?", id).Scan(&previous, &previousState, &previousResult)
		if err == nil {
			var old ProviderProposal
			if err = json.Unmarshal([]byte(previous), &old); err != nil {
				return err
			}
			h, _ := commandHash(old.Request, true)
			if (planHash == "" && (old.PlanHash != "" || h != hash)) || (planHash != "" && old.PlanHash != planHash) {
				return Fail("IDEMPOTENCY_CONFLICT", "Proposal retry changed its reviewed content")
			}
			p = old
			p.State = previousState
			if previousResult.Valid {
				p.Result = json.RawMessage(previousResult.String)
			}
			return nil
		}
		if err != sql.ErrNoRows {
			return err
		}
		_, err = tx.ExecContext(ctx, "INSERT INTO provider_proposals VALUES(?,?,'pending',?,NULL,?)", id, c.info.ID, string(marshal(p)), now())
		return err
	})
	return success(p), err
}
func (b *ProviderBridge) Proposals(ctx context.Context) ([]ProviderProposal, error) {
	rows, err := b.kernel.db.QueryContext(ctx, "SELECT body_json,state,result_json FROM provider_proposals ORDER BY created_at DESC LIMIT 100")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ProviderProposal{}
	for rows.Next() {
		var raw, state string
		var result sql.NullString
		if err = rows.Scan(&raw, &state, &result); err != nil {
			return nil, err
		}
		var p ProviderProposal
		if err = json.Unmarshal([]byte(raw), &p); err != nil {
			return nil, err
		}
		p.State = state
		if result.Valid {
			p.Result = json.RawMessage(result.String)
		}
		out = append(out, p)
	}
	return out, rows.Err()
}
func (b *ProviderBridge) Approve(ctx context.Context, id string) (json.RawMessage, error) {
	b.approvalMu.Lock()
	defer b.approvalMu.Unlock()
	var raw, state string
	var result sql.NullString
	if err := b.kernel.db.QueryRowContext(ctx, "SELECT body_json,state,result_json FROM provider_proposals WHERE id=?", id).Scan(&raw, &state, &result); err != nil {
		return nil, err
	}
	var p ProviderProposal
	if err := json.Unmarshal([]byte(raw), &p); err != nil {
		return nil, err
	}
	b.mu.Lock()
	c := b.connections[p.ConnectionID]
	connected := c != nil && c.info.State == "connected"
	b.mu.Unlock()
	if !connected {
		return nil, Fail("SCOPE_DENIED", "Connection was revoked; reconnect and review a fresh proposal")
	}
	if err := b.kernel.authorize(c.execution); err != nil {
		return nil, err
	}
	if result.Valid {
		return json.RawMessage(result.String), nil
	}
	if state != "pending" {
		return nil, Fail("CONFLICT", "Proposal is no longer pending")
	}
	live, cancel := context.WithCancel(ctx)
	defer cancel()
	stop := context.AfterFunc(c.ctx, cancel)
	defer stop()
	// Persist the approval-bearing request before dispatch. A crash after commit
	// retries the exact same idempotent request and returns its receipt.
	if p.Request.Mutation.ApprovalId == nil {
		approval, err := b.kernel.Approve(live, b.owner, p.Request, c.execution)
		if err != nil {
			return nil, err
		}
		aid := contracts.DomainId(approval)
		p.Request.Mutation.ApprovalId = &aid
		if err = b.kernel.write(live, func(tx *sql.Tx) error {
			_, err := tx.ExecContext(live, "UPDATE provider_proposals SET body_json=? WHERE id=?", string(marshal(p)), id)
			return err
		}); err != nil {
			return nil, err
		}
	}
	response := b.kernel.Dispatch(live, c.execution, "batch", marshal(p.Request))
	var envelope struct {
		OK bool `json:"ok"`
	}
	json.Unmarshal(response, &envelope)
	state = "failed"
	if envelope.OK {
		state = "applied"
	}
	err := b.kernel.write(ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, "UPDATE provider_proposals SET state=?,result_json=? WHERE id=?", state, string(response), id)
		return err
	})
	b.audit(ctx, c.info.ID, "approve", state, "Reviewed service mutation")
	return response, err
}
func (b *ProviderBridge) Reject(ctx context.Context, id string) error {
	b.approvalMu.Lock()
	defer b.approvalMu.Unlock()
	if err := requireOwnerScope(b.kernel, b.owner); err != nil {
		return err
	}
	return b.kernel.write(ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, "UPDATE provider_proposals SET state='denied' WHERE id=? AND state='pending'", id)
		return err
	})
}
func (b *ProviderBridge) PreviewContext(ctx context.Context, pages []string) (any, error) {
	manifest := []map[string]any{}
	if len(pages) > 100 {
		return nil, Fail("RESOURCE_EXHAUSTED", "Context is limited to 100 pages")
	}
	for _, id := range pages {
		if err := b.kernel.authorizePage(ctx, b.kernel.db, b.owner, id); err != nil {
			return nil, err
		}
		p, err := readPageTx(ctx, b.kernel.db, id)
		if err != nil {
			return nil, err
		}
		manifest = append(manifest, map[string]any{"pageId": id, "revision": p.Page.Revision, "title": p.Page.Title, "blocks": len(p.Blocks)})
	}
	hash := sha256.Sum256(marshal(manifest))
	return map[string]any{"pages": manifest, "hash": hex.EncodeToString(hash[:]), "notice": "Only these pages are shared. Changes require review. Pairing does not choose a paid billing mode."}, nil
}
func (b *ProviderBridge) Handoff(ctx context.Context, id, objective, result, pending string) (any, error) {
	b.mu.Lock()
	c := b.connections[id]
	if c == nil {
		b.mu.Unlock()
		return nil, Fail("NOT_FOUND", "Unknown connection")
	}
	info := c.info
	b.mu.Unlock()
	if len(objective)+len(result)+len(pending) > 16000 {
		return nil, Fail("RESOURCE_EXHAUSTED", "Handoff exceeds 16000 bytes")
	}
	manifest, err := b.PreviewContext(ctx, info.PageIDs)
	if err != nil {
		return nil, err
	}
	return map[string]any{"fromProvider": info.Provider, "objective": objective, "reviewedResult": result, "pendingWork": pending, "context": manifest, "requiresFreshApproval": true, "nativeSessionTransferred": false}, nil
}
func (b *ProviderBridge) String() string { return fmt.Sprintf("ProviderBridge(%s)", b.kernel.ID()) }
