package workspace

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"trove.local/phase0/contracts"
)

type ServiceFailure struct {
	Code         string
	Message      string
	Current      []contracts.ServiceExpectedRevision
	ApprovalHash string
}

func (e *ServiceFailure) Error() string { return e.Code + ": " + e.Message }
func Fail(code, message string) error   { return &ServiceFailure{Code: code, Message: message} }
func errorResponse(request string, err error) json.RawMessage {
	if err := contracts.Validate("domain", "Id", marshal(request)); err != nil {
		request = NewID()
	}
	code := "INTERNAL"
	message := "Workspace operation failed"
	retry := false
	details := map[string]any{}
	var f *ServiceFailure
	if errors.As(err, &f) {
		code = f.Code
		message = f.Message
		if len(f.Current) > 0 {
			details["currentRevisions"] = f.Current
		}
		if f.ApprovalHash != "" {
			details["requiredApprovalHash"] = f.ApprovalHash
		}
	} else if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		code = "CANCELLED"
		message = "Operation cancelled"
	} else if errors.Is(err, sql.ErrNoRows) {
		code = "NOT_FOUND"
		message = "Requested entity does not exist"
	} else if strings.Contains(err.Error(), "constraint") || strings.Contains(err.Error(), "cycle") || strings.Contains(err.Error(), "mismatch") || strings.Contains(err.Error(), "todo_shape") {
		code = "INVALID_REQUEST"
		message = "Mutation violates workspace constraints"
	} else if strings.Contains(err.Error(), "locked") {
		code = "WORKSPACE_LOCKED"
		message = "Workspace is busy"
		retry = true
	}
	e := map[string]any{"code": code, "message": message, "retryable": retry, "requestId": request}
	if len(details) > 0 {
		e["details"] = details
	}
	return marshal(map[string]any{"ok": false, "error": e})
}
func success(result any) json.RawMessage {
	return marshal(map[string]any{"ok": true, "result": result})
}
func (k *Kernel) authorize(e contracts.ServiceExecutionContext) error {
	k.mu.Lock()
	g, ok := k.grants[string(e.ScopeGrantId)]
	k.mu.Unlock()
	if !ok || string(marshal(g)) != string(marshal(e)) {
		return Fail("SCOPE_DENIED", "Scope grant is absent, expired, or does not match the authenticated context")
	}
	if e.Actor.Kind != "user" && e.Origin == "user" {
		return Fail("SCOPE_DENIED", "Only the direct user principal can originate user mutations")
	}
	if err := contracts.Validate("service", "ExecutionContext", marshal(e)); err != nil {
		return Fail("SCOPE_DENIED", "Invalid trusted execution context")
	}
	var scope struct {
		WorkspaceID string `json:"workspaceId"`
	}
	json.Unmarshal(marshal(e.Scope), &scope)
	if scope.WorkspaceID != k.id {
		return Fail("SCOPE_DENIED", "Scope belongs to another workspace")
	}
	return nil
}

type querier interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

func (k *Kernel) authorizePage(ctx context.Context, q querier, e contracts.ServiceExecutionContext, page string) error {
	if err := k.authorize(e); err != nil {
		return err
	}
	return k.authorizePageScope(ctx, q, e, page)
}

// Caller must first validate the execution context against the current grant.
// Keep membership checks in the caller's transaction, never in a scope cache.
func (k *Kernel) authorizePageScope(ctx context.Context, q querier, e contracts.ServiceExecutionContext, page string) error {
	if e.Actor.Kind == "external_client" {
		// Gmail context remains unqualified for external AI even after deliberate
		// canonical import or a portable round trip. Imported instructions cannot
		// override this policy, and a page-title edit does not clear provenance.
		var restricted int
		if err := q.QueryRowContext(ctx, `SELECT COUNT(*) FROM pages p WHERE p.id=? AND (
EXISTS(SELECT 1 FROM json_tree(p.provenance_json) j WHERE
 (j.key='sourceSystem' AND j.value='connector:gmail') OR (j.key='provider' AND j.value='gmail')) OR
EXISTS(SELECT 1 FROM import_mappings m WHERE m.local_id=p.id AND m.source_system='connector:gmail'))`, page).Scan(&restricted); err != nil {
			return err
		}
		if restricted > 0 {
			return Fail("SCOPE_DENIED", "Gmail-derived context is disabled for external AI pending separate qualification")
		}
	}
	switch s := e.Scope.Value.(type) {
	case contracts.DomainScopeVariant1:
		if string(s.WorkspaceId) == k.id {
			return nil
		}
	case contracts.DomainScopeVariant3:
		if string(s.WorkspaceId) != k.id {
			break
		}
		for _, id := range s.PageIds {
			if string(id) == page {
				return nil
			}
		}
	case contracts.DomainScopeVariant2:
		if string(s.WorkspaceId) != k.id {
			break
		}
		var count int
		// Project retrieval grants are explicit membership, never page-tree ancestry.
		err := q.QueryRowContext(ctx, `SELECT count(*) FROM (SELECT owning_page_id AS id FROM containers WHERE id=? AND kind='project' UNION SELECT page_id FROM project_pages WHERE project_id=? AND relationship IN('member','pin')) WHERE id=? AND id NOT IN(SELECT page_id FROM project_pages WHERE project_id=? AND relationship='exclude')`, s.ProjectId, s.ProjectId, page, s.ProjectId).Scan(&count)
		if err != nil {
			return err
		}
		if count > 0 {
			return nil
		}
	}
	return Fail("SCOPE_DENIED", "Page is outside the granted scope")
}
func (k *Kernel) authorizeEntity(ctx context.Context, q querier, e contracts.ServiceExecutionContext, kind, id string) error {
	if kind == "page" {
		return k.authorizePage(ctx, q, e, id)
	}
	var page string
	var query string
	switch kind {
	case "block":
		query = "SELECT page_id FROM blocks WHERE id=?"
	case "collection":
		query = "SELECT owning_page_id FROM collections WHERE id=?"
	case "property":
		query = "SELECT c.owning_page_id FROM collections c JOIN properties p ON p.collection_id=c.id WHERE p.id=?"
	case "record":
		query = "SELECT page_id FROM records WHERE id=?"
	case "view":
		query = "SELECT owning_page_id FROM views WHERE id=?"
	case "container":
		query = "SELECT owning_page_id FROM containers WHERE id=?"
	case "file":
		if s, ok := e.Scope.Value.(contracts.DomainScopeVariant1); ok && string(s.WorkspaceId) == k.id {
			return k.authorize(e)
		}
		rows, err := q.QueryContext(ctx, "SELECT page_id FROM attachments WHERE file_id=?", id)
		if err != nil {
			return err
		}
		var ids []string
		for rows.Next() {
			var id string
			rows.Scan(&id)
			ids = append(ids, id)
		}
		rows.Close()
		for _, id := range ids {
			if k.authorizePage(ctx, q, e, id) == nil {
				return nil
			}
		}
		return Fail("SCOPE_DENIED", "File is outside the granted scope")
	case "meeting":
		query = "SELECT page_id FROM meetings WHERE id=?"
	case "transcript_segment":
		query = "SELECT m.page_id FROM transcript_segments s JOIN meetings m ON m.id=s.meeting_id WHERE s.id=?"
	case "memory":
		query = "SELECT page_id FROM memory_entries WHERE id=?"
	default:
		return Fail("INVALID_REQUEST", "Unknown entity kind "+kind)
	}
	if err := q.QueryRowContext(ctx, query, id).Scan(&page); err != nil {
		return err
	}
	return k.authorizePage(ctx, q, e, page)
}
func (k *Kernel) checkEnvelope(e contracts.ServiceExecutionContext, r map[string]any) error {
	if err := k.authorize(e); err != nil {
		return err
	}
	if str(r["workspaceId"]) != k.id || number(r["contractVersion"]) != 1 {
		return Fail("INVALID_REQUEST", "Workspace or contract version does not match")
	}
	return nil
}
func (k *Kernel) Approve(ctx context.Context, e contracts.ServiceExecutionContext, request contracts.ServiceBatchRequest, target contracts.ServiceExecutionContext) (string, error) {
	if e.Origin != "user" || e.Actor.Kind != "user" {
		return "", Fail("SCOPE_DENIED", "Only an authenticated user can approve a proposal")
	}
	if err := k.authorize(e); err != nil {
		return "", err
	}
	if err := k.authorize(target); err != nil {
		return "", err
	}
	hash, err := commandHash(request, true)
	if err != nil {
		return "", err
	}
	id := NewID()
	err = k.write(ctx, func(tx *sql.Tx) error {
		_, e := tx.ExecContext(ctx, `INSERT INTO approvals VALUES(?,?,?,?,?,?,?,?,'approved')`, id, target.Actor.ActorId, target.ScopeGrantId, target.PolicyRevision, hash, string(marshal(request.Mutation.ExpectedRevisions)), timeAfterMinutes(10), e.Actor.ActorId)
		return e
	})
	return id, err
}
func required(kind, id string, revision string) contracts.ServiceExpectedRevision {
	return contracts.ServiceExpectedRevision{Target: contracts.ServiceEntityRef{EntityType: kind, EntityId: contracts.DomainId(id)}, Revision: revision}
}
func guardKey(kind, id string) string { return fmt.Sprintf("%s:%s", kind, id) }
