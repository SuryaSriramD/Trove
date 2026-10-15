package workspace

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"

	"trove.local/phase0/contracts"
)

func init() {
	for _, kind := range []string{"memory.propose", "memory.approve", "memory.forget", "memory.update", "memory.setStatus"} {
		RegisterCommand(kind, memoryCommand)
	}
	RegisterQuery("listMemory", listMemory)
	RegisterQuery("memory.list", listMemory)
	RegisterSnapshotReader("memories", func(ctx context.Context, k *Kernel, tx *sql.Tx, e contracts.ServiceExecutionContext) (any, error) {
		return readMemories(ctx, k, tx, e, true)
	})
}

func memoryCommand(m *Mutation, c map[string]any) error {
	id, kind := str(c["memoryId"]), str(c["kind"])
	if err := m.Touch("memory", id); err != nil {
		return err
	}
	if kind == "memory.propose" {
		if m.GuardRevision("memory", id) != "0" {
			return Fail("CONFLICT", "Memory already exists")
		}
		page := str(c["pageId"])
		if err := m.TouchPage(page); err != nil {
			return err
		}
		var record int
		if err := m.Tx.QueryRowContext(m.Context, "SELECT count(*) FROM records WHERE page_id=?", page).Scan(&record); err != nil {
			return err
		}
		if record > 0 {
			return Fail("INVALID_REQUEST", "A database record cannot become a competing memory record")
		}
		scope := c["scope"].(map[string]any)
		if str(scope["workspaceId"]) != m.Kernel.id {
			return Fail("SCOPE_DENIED", "Memory scope belongs to another workspace")
		}
		if _, all := m.Execution.Scope.Value.(contracts.DomainScopeVariant1); !all && string(marshal(scope)) != string(marshal(m.Execution.Scope)) {
			return Fail("SCOPE_DENIED", "Memory cannot widen its granted scope")
		}
		mk := str(c["memoryKind"])
		if mk == "project_context" {
			if err := m.AuthorizePage(str(c["pageRef"])); err != nil {
				return err
			}
		}
		if mk != "project_context" && (strings.TrimSpace(str(c["content"])) == "" || len(str(c["content"])) > 16000) {
			return Fail("INVALID_REQUEST", "Memory requires text up to 16000 bytes")
		}
		sources, _ := c["sources"].([]any)
		if mk == "decision" && len(sources) == 0 {
			return Fail("INVALID_REQUEST", "Decisions require source evidence")
		}
		if mk == "project_context" {
			found := false
			for _, v := range sources {
				if str(v.(map[string]any)["pageId"]) == str(c["pageRef"]) {
					found = true
				}
			}
			if !found {
				return Fail("INVALID_REQUEST", "Project context requires a revision-bound reference to its page")
			}
		}
		if _, err := m.Exec("INSERT INTO memory_entries(id,page_id,scope_kind,scope_json,kind,status,content,page_ref,revision,supersedes_id) VALUES(?,?,?,?,?,'proposed',?,?,1,?)", id, page, str(scope["kind"]), string(marshal(scope)), mk, nullable(c["content"]), nullable(c["pageRef"]), nullable(c["supersedesId"])); err != nil {
			return err
		}
		return replaceMemorySources(m, id, page, sources)
	}
	var page, status, mk string
	if err := m.Tx.QueryRowContext(m.Context, "SELECT page_id,status,kind FROM memory_entries WHERE id=?", id).Scan(&page, &status, &mk); err != nil {
		return err
	}
	if err := m.TouchPage(page); err != nil {
		return err
	}
	switch kind {
	case "memory.approve":
		if m.Execution.Actor.Kind != "user" || m.Execution.Origin != "user" {
			return Fail("APPROVAL_REQUIRED", "Only the user may approve memory")
		}
		if status == "superseded" {
			return Fail("CONFLICT", "Superseded memory must be edited and reviewed before approval")
		}
		if err := memoryEvidenceCurrent(m.Context, m.Tx, id); err != nil {
			return err
		}
		newerActive := func(target string) error {
			var count int
			if err := m.Tx.QueryRowContext(m.Context, `WITH RECURSIVE replacements(id) AS (SELECT id FROM memory_entries WHERE supersedes_id=? UNION SELECT n.id FROM memory_entries n JOIN replacements r ON n.supersedes_id=r.id) SELECT count(*) FROM memory_entries WHERE id IN(SELECT id FROM replacements) AND id<>? AND status='active'`, target, id).Scan(&count); err != nil {
				return err
			}
			if count > 0 {
				return Fail("CONFLICT", "A newer active memory replaces this entry; review and supersede that current entry instead")
			}
			return nil
		}
		if err := newerActive(id); err != nil {
			return err
		}
		var prior sql.NullString
		if err := m.Tx.QueryRowContext(m.Context, "SELECT supersedes_id FROM memory_entries WHERE id=?", id).Scan(&prior); err != nil {
			return err
		}
		if prior.Valid {
			if err := newerActive(prior.String); err != nil {
				return err
			}
			if err := m.Touch("memory", prior.String); err != nil {
				return err
			}
			var priorPage string
			if err := m.Tx.QueryRowContext(m.Context, "SELECT page_id FROM memory_entries WHERE id=?", prior.String).Scan(&priorPage); err != nil {
				return err
			}
			if err := m.TouchPage(priorPage); err != nil {
				return err
			}
			if _, err := m.Exec("UPDATE memory_entries SET status='superseded' WHERE id=?", prior.String); err != nil {
				return err
			}
		}
		_, err := m.Exec("UPDATE memory_entries SET status='active',approved_at=?,approved_by_actor_id=? WHERE id=?", now(), m.Execution.Actor.ActorId, id)
		return err
	case "memory.forget":
		_, err := m.Exec("UPDATE memory_entries SET status='forgotten' WHERE id=?", id)
		return err
	case "memory.setStatus":
		value := str(c["status"])
		if value != "proposed" && value != "disabled" && value != "superseded" {
			return Fail("INVALID_REQUEST", "Unsupported memory transition")
		}
		_, err := m.Exec("UPDATE memory_entries SET status=? WHERE id=?", value, id)
		return err
	case "memory.update":
		if value, ok := c["content"]; ok {
			if mk == "project_context" || len(str(value)) > 16000 || strings.TrimSpace(str(value)) == "" {
				return Fail("INVALID_REQUEST", "Project context uses a live page reference; other memory needs readable text")
			}
			if _, err := m.Exec("UPDATE memory_entries SET content=? WHERE id=?", value, id); err != nil {
				return err
			}
		}
		if v, ok := c["sources"]; ok {
			sources := v.([]any)
			if mk == "decision" && len(sources) == 0 {
				return Fail("INVALID_REQUEST", "Decisions need evidence")
			}
			if mk == "project_context" {
				var ref string
				if err := m.Tx.QueryRowContext(m.Context, "SELECT page_ref FROM memory_entries WHERE id=?", id).Scan(&ref); err != nil {
					return err
				}
				found := false
				for _, v := range sources {
					if str(v.(map[string]any)["pageId"]) == ref {
						found = true
					}
				}
				if !found {
					return Fail("INVALID_REQUEST", "Project context must retain evidence for its live page reference")
				}
			}
			if err := replaceMemorySources(m, id, page, sources); err != nil {
				return err
			}
		}
		_, err := m.Exec("UPDATE memory_entries SET status='proposed',approved_at=NULL,approved_by_actor_id=NULL WHERE id=?", id)
		return err
	}
	return Fail("INVALID_REQUEST", "Unknown memory operation")
}
func replaceMemorySources(m *Mutation, id, page string, sources []any) error {
	if len(sources) > 64 {
		return Fail("INVALID_REQUEST", "Too many memory sources")
	}
	if _, err := m.Exec("DELETE FROM memory_sources WHERE memory_id=?", id); err != nil {
		return err
	}
	for i, v := range sources {
		loc, ok := v.(map[string]any)
		if !ok {
			return Fail("INVALID_REQUEST", "Invalid evidence")
		}
		source := str(loc["pageId"])
		if source == page {
			return Fail("INVALID_REQUEST", "Memory cannot cite itself")
		}
		if err := m.AuthorizePage(source); err != nil {
			return err
		}
		var policySource int
		if err := m.Tx.QueryRowContext(m.Context, "SELECT count(*) FROM memory_entries WHERE page_id=?", source).Scan(&policySource); err != nil {
			return err
		}
		if policySource > 0 {
			return Fail("INVALID_REQUEST", "Memory evidence must refer to canonical content, not another memory record")
		}
		if err := validateMemoryLocation(m.Context, m.Tx, loc); err != nil {
			return err
		}
		if restricted, err := pageExternalContextRestricted(m.Context, m.Tx, source); err != nil {
			return err
		} else if restricted {
			if err := retainPageExternalContextPolicy(m.Context, m.Tx, page, gmailExternalContextPolicy); err != nil {
				return err
			}
		}
		transcript, _ := loc["transcript"].(map[string]any)
		if _, err := m.Exec("INSERT INTO memory_sources(memory_id,page_id,source_revision,block_id,meeting_id,segment_id,start_ms,end_ms,source_index) VALUES(?,?,?,?,?,?,?,?,?)", id, source, loc["sourceRevision"], nullable(loc["blockId"]), nullable(transcript["meetingId"]), nullable(transcript["segmentId"]), transcript["startMs"], transcript["endMs"], i); err != nil {
			return err
		}
	}
	return nil
}
func validateMemoryLocation(ctx context.Context, q querier, loc map[string]any) error {
	if contracts.Validate("domain", "Location", marshal(loc)) != nil {
		return Fail("INVALID_REQUEST", "Invalid source location")
	}
	var revision string
	if err := q.QueryRowContext(ctx, "SELECT revision FROM pages WHERE id=? AND deleted_at IS NULL", loc["pageId"]).Scan(&revision); err != nil {
		return Fail("STALE_CITATION", "Evidence page is unavailable")
	}
	if revision != str(loc["sourceRevision"]) {
		return Fail("STALE_CITATION", "Evidence revision changed")
	}
	var n int
	if b := str(loc["blockId"]); b != "" {
		if err := q.QueryRowContext(ctx, "SELECT count(*) FROM blocks WHERE id=? AND page_id=?", b, loc["pageId"]).Scan(&n); err != nil {
			return err
		}
		if n != 1 {
			return Fail("STALE_CITATION", "Evidence block is unavailable")
		}
	}
	if t, ok := loc["transcript"].(map[string]any); ok {
		if err := q.QueryRowContext(ctx, `SELECT count(*) FROM transcript_segments s JOIN meetings m ON m.id=s.meeting_id WHERE s.id=? AND s.meeting_id=? AND s.block_id=? AND m.page_id=? AND s.start_ms<=? AND s.end_ms>=?`, t["segmentId"], t["meetingId"], loc["blockId"], loc["pageId"], t["startMs"], t["endMs"]).Scan(&n); err != nil {
			return err
		}
		if n != 1 {
			return Fail("STALE_CITATION", "Transcript evidence is unavailable")
		}
	}
	return nil
}
func memoryEvidenceCurrent(ctx context.Context, q querier, id string) error {
	sources, err := memorySources(ctx, q, id)
	if err != nil {
		return err
	}
	for _, loc := range sources {
		var raw map[string]any
		json.Unmarshal(marshal(loc), &raw)
		if err = validateMemoryLocation(ctx, q, raw); err != nil {
			return err
		}
	}
	return nil
}
func memorySources(ctx context.Context, q querier, id string) ([]contracts.DomainLocation, error) {
	rows, err := q.QueryContext(ctx, "SELECT page_id,source_revision,block_id,meeting_id,segment_id,start_ms,end_ms FROM memory_sources WHERE memory_id=? ORDER BY source_index", id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []contracts.DomainLocation{}
	for rows.Next() {
		var page, rev string
		var block, meeting, segment sql.NullString
		var start, end sql.NullInt64
		if err = rows.Scan(&page, &rev, &block, &meeting, &segment, &start, &end); err != nil {
			return nil, err
		}
		v := map[string]any{"pageId": page, "sourceRevision": rev}
		if block.Valid {
			v["blockId"] = block.String
		}
		if meeting.Valid {
			v["transcript"] = map[string]any{"meetingId": meeting.String, "segmentId": segment.String, "startMs": start.Int64, "endMs": end.Int64}
		}
		var loc contracts.DomainLocation
		json.Unmarshal(marshal(v), &loc)
		out = append(out, loc)
	}
	return out, rows.Err()
}
func readMemories(ctx context.Context, k *Kernel, q querier, e contracts.ServiceExecutionContext, inactive bool) ([]contracts.DomainMemory, error) {
	rows, err := q.QueryContext(ctx, `SELECT id,page_id,scope_json,kind,status,content,page_ref,revision,approved_at,approved_by_actor_id,supersedes_id FROM memory_entries ORDER BY id`)
	if err != nil {
		return nil, err
	}
	all := []contracts.DomainMemory{}
	for rows.Next() {
		var m contracts.DomainMemory
		var scope string
		if err = rows.Scan(&m.Id, &m.PageId, &scope, &m.Kind, &m.Status, &m.Content, &m.PageRef, &m.Revision, &m.ApprovedAt, &m.ApprovedByActorId, &m.SupersedesId); err != nil {
			rows.Close()
			return nil, err
		}
		if err = json.Unmarshal([]byte(scope), &m.Scope); err != nil {
			rows.Close()
			return nil, err
		}
		all = append(all, m)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	out := []contracts.DomainMemory{}
	for _, m := range all {
		if k.authorizePage(ctx, q, e, string(m.PageId)) != nil {
			continue
		}
		m.Sources, err = memorySources(ctx, q, string(m.Id))
		if err != nil {
			return nil, err
		}
		allowed := true
		for _, loc := range m.Sources {
			if k.authorizePage(ctx, q, e, string(loc.PageId)) != nil {
				allowed = false
			}
		}
		if !allowed {
			continue
		}
		var live int
		if err = q.QueryRowContext(ctx, "SELECT count(*) FROM pages WHERE id=? AND deleted_at IS NULL", m.PageId).Scan(&live); err != nil {
			return nil, err
		}
		if !inactive && (live != 1 || m.Status != "active" || memoryEvidenceCurrent(ctx, q, string(m.Id)) != nil) {
			continue
		}
		out = append(out, m)
	}
	return out, nil
}
func listMemory(ctx context.Context, k *Kernel, e contracts.ServiceExecutionContext, r map[string]any) (any, error) {
	entries, err := readMemories(ctx, k, k.db, e, r["includeInactive"] == true)
	if err != nil {
		return nil, err
	}
	if raw, ok := r["scope"]; ok {
		if contracts.Validate("domain", "Scope", marshal(raw)) != nil {
			return nil, Fail("INVALID_REQUEST", "Invalid memory scope")
		}
		var scope contracts.DomainScope
		json.Unmarshal(marshal(raw), &scope)
		filtered := []contracts.DomainMemory{}
		for _, m := range entries {
			if memoryScopeMatches(ctx, k, e, m, scope) {
				filtered = append(filtered, m)
			}
		}
		entries = filtered
	}
	var cursor string
	err = k.db.QueryRowContext(ctx, "SELECT COALESCE(MAX(cursor),'0') FROM committed_events").Scan(&cursor)
	return contracts.ServiceListMemoryResult{Entries: entries, SnapshotCursor: cursor}, err
}
func memoryScopeMatches(ctx context.Context, k *Kernel, e contracts.ServiceExecutionContext, m contracts.DomainMemory, scope contracts.DomainScope) bool {
	if string(marshal(scope)) == string(marshal(m.Scope)) {
		return true
	}
	if s, ok := scope.Value.(contracts.DomainScopeVariant1); ok {
		return string(s.WorkspaceId) == k.id
	}
	if s, ok := scope.Value.(contracts.DomainScopeVariant3); ok {
		if ms, ok := m.Scope.Value.(contracts.DomainScopeVariant3); ok {
			for _, a := range s.PageIds {
				for _, b := range ms.PageIds {
					if a == b {
						return true
					}
				}
			}
		}
		if m.Kind == "preference" && scopeKind(m.Scope) == "workspace" {
			return true
		}
		for _, page := range s.PageIds {
			for _, source := range m.Sources {
				if source.PageId == page && scopeKind(m.Scope) == "workspace" {
					return true
				}
			}
			if project, ok := m.Scope.Value.(contracts.DomainScopeVariant2); ok {
				var n int
				k.db.QueryRowContext(ctx, `SELECT count(*) FROM (SELECT owning_page_id AS id FROM containers WHERE id=? UNION SELECT page_id FROM project_pages WHERE project_id=? AND relationship IN('member','pin')) WHERE id=? AND id NOT IN(SELECT page_id FROM project_pages WHERE project_id=? AND relationship='exclude')`, project.ProjectId, project.ProjectId, page, project.ProjectId).Scan(&n)
				if n > 0 {
					return true
				}
			}
		}
		return false
	}
	if s, ok := scope.Value.(contracts.DomainScopeVariant2); ok && m.Kind == "preference" {
		var ancestor, workspace int
		k.db.QueryRowContext(ctx, "SELECT inherit_ancestor_preferences,inherit_workspace_preferences FROM project_scopes WHERE project_id=?", s.ProjectId).Scan(&ancestor, &workspace)
		if scopeKind(m.Scope) == "workspace" {
			return workspace == 1
		}
		if p, ok := m.Scope.Value.(contracts.DomainScopeVariant2); ok && ancestor == 1 {
			var n int
			k.db.QueryRowContext(ctx, `WITH RECURSIVE parents(id) AS (SELECT parent_project_id FROM containers WHERE id=? UNION SELECT c.parent_project_id FROM containers c JOIN parents p ON c.id=p.id WHERE c.parent_project_id IS NOT NULL) SELECT count(*) FROM parents WHERE id=?`, s.ProjectId, p.ProjectId).Scan(&n)
			return n > 0
		}
	}
	return false
}
func scopeKind(s contracts.DomainScope) string {
	var v map[string]any
	json.Unmarshal(marshal(s), &v)
	return str(v["kind"])
}
