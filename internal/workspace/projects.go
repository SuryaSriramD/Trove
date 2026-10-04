package workspace

import (
	"context"
	"database/sql"
	"trove.local/phase0/contracts"
)

func init() {
	RegisterCommand("project.setScope", setProjectScope)
	RegisterSnapshotReader("projectScopes", projectScopeSnapshot)
}
func setProjectScope(m *Mutation, c map[string]any) error {
	id := str(c["projectId"])
	if err := m.Touch("container", id); err != nil {
		return err
	}
	var kind string
	if err := m.Tx.QueryRowContext(m.Context, "SELECT kind FROM containers WHERE id=?", id).Scan(&kind); err != nil {
		return err
	}
	if kind != "project" {
		return Fail("INVALID_REQUEST", "Memory scope requires a project container")
	}
	if _, err := m.Exec("INSERT INTO project_scopes(project_id,revision) VALUES(?,1) ON CONFLICT(project_id) DO NOTHING", id); err != nil {
		return err
	}
	if _, err := m.Exec("DELETE FROM project_pages WHERE project_id=?", id); err != nil {
		return err
	}
	for field, relationship := range map[string]string{"memberPageIds": "member", "pinnedPageIds": "pin", "excludedPageIds": "exclude"} {
		seen := map[string]bool{}
		for _, raw := range c[field].([]any) {
			page := str(raw)
			if seen[page] {
				return Fail("INVALID_REQUEST", "Duplicate project scope page")
			}
			seen[page] = true
			if err := m.AuthorizePage(page); err != nil {
				return err
			}
			if _, err := m.Exec("INSERT INTO project_pages VALUES(?,?,?)", id, page, relationship); err != nil {
				return err
			}
		}
	}
	next, err := revNext(m.GuardRevision("container", id))
	if err != nil {
		return err
	}
	_, err = m.Exec("UPDATE project_scopes SET inherit_ancestor_preferences=?,inherit_workspace_preferences=?,revision=? WHERE project_id=?", c["inheritAncestorPreferences"], c["inheritWorkspacePreferences"], next, id)
	return err
}
func projectScopeSnapshot(ctx context.Context, k *Kernel, tx *sql.Tx, e contracts.ServiceExecutionContext) (any, error) {
	rows, err := tx.QueryContext(ctx, "SELECT project_id,inherit_ancestor_preferences,inherit_workspace_preferences,revision FROM project_scopes")
	if err != nil {
		return nil, err
	}
	scopes := []contracts.DomainProjectScope{}
	for rows.Next() {
		var s contracts.DomainProjectScope
		if err = rows.Scan(&s.ProjectId, &s.InheritAncestorPreferences, &s.InheritWorkspacePreferences, &s.Revision); err != nil {
			rows.Close()
			return nil, err
		}
		scopes = append(scopes, s)
	}
	rows.Close()
	out := []contracts.DomainProjectScope{}
	for _, s := range scopes {
		if k.authorizeEntity(ctx, tx, e, "container", string(s.ProjectId)) != nil {
			continue
		}
		s.MemberPageIds = []contracts.DomainId{}
		s.PinnedPageIds = []contracts.DomainId{}
		s.ExcludedPageIds = []contracts.DomainId{}
		rows, err := tx.QueryContext(ctx, "SELECT page_id,relationship FROM project_pages WHERE project_id=? ORDER BY page_id", s.ProjectId)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var id contracts.DomainId
			var rel string
			if err = rows.Scan(&id, &rel); err != nil {
				rows.Close()
				return nil, err
			}
			if k.authorizePage(ctx, tx, e, string(id)) != nil {
				continue
			}
			switch rel {
			case "member":
				s.MemberPageIds = append(s.MemberPageIds, id)
			case "pin":
				s.PinnedPageIds = append(s.PinnedPageIds, id)
			case "exclude":
				s.ExcludedPageIds = append(s.ExcludedPageIds, id)
			}
		}
		rows.Close()
		out = append(out, s)
	}
	return out, nil
}
