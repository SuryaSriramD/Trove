package workspace

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"trove.local/phase0/contracts"
)

type SkillDefinition struct {
	Name             string   `json:"name"`
	Purpose          string   `json:"purpose"`
	Instructions     string   `json:"instructions"`
	InputDescription string   `json:"inputDescription"`
	ExpectedOutput   string   `json:"expectedOutput"`
	Mode             string   `json:"mode"`
	SourcePageIDs    []string `json:"sourcePageIds"`
	Tools            []string `json:"tools"`
}
type SkillVersion struct {
	ID         string          `json:"id"`
	PageID     string          `json:"pageId"`
	Version    int             `json:"version"`
	Definition SkillDefinition `json:"definition"`
	Archived   bool            `json:"archived"`
	Imported   bool            `json:"imported"`
	ActorID    string          `json:"actorId"`
	CreatedAt  string          `json:"createdAt"`
}
type SkillRunReference struct {
	SkillID       string   `json:"skillId"`
	Version       int      `json:"version"`
	InvocationID  string   `json:"invocationId"`
	Input         string   `json:"input"`
	SourcePageIDs []string `json:"sourcePageIds"`
	Tools         []string `json:"tools"`
	PreviewHash   string   `json:"previewHash,omitempty"`
}
type SkillPreview struct {
	Skill    SkillVersion        `json:"skill"`
	Provider string              `json:"provider"`
	Prompt   string              `json:"prompt"`
	Sources  []map[string]string `json:"sources"`
	Tools    []string            `json:"tools"`
	Hash     string              `json:"hash"`
}

func init() {
	RegisterInitializer(func(ctx context.Context, k *Kernel) error {
		return k.write(ctx, func(tx *sql.Tx) error {
			_, err := tx.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS skill_versions(skill_id TEXT NOT NULL,page_id TEXT NOT NULL REFERENCES pages(id),version INTEGER NOT NULL,definition_json TEXT NOT NULL,archived INTEGER NOT NULL,imported INTEGER NOT NULL,actor_id TEXT NOT NULL,created_at TEXT NOT NULL,PRIMARY KEY(skill_id,version)); CREATE TABLE IF NOT EXISTS skill_invocations(id TEXT PRIMARY KEY,actor_id TEXT NOT NULL,identity_hash TEXT NOT NULL,created_at TEXT NOT NULL);`)
			return err
		})
	})
	RegisterCommand("skill.save", saveSkill)
	RegisterQuery("skills.list", listSkills)
	RegisterQuery("skills.preview", func(ctx context.Context, k *Kernel, e contracts.ServiceExecutionContext, r map[string]any) (any, error) {
		var ref SkillRunReference
		if err := json.Unmarshal(marshal(r["skill"]), &ref); err != nil {
			return nil, err
		}
		return k.PreviewSkill(ctx, e, ref, str(r["provider"]))
	})
}
func saveSkill(m *Mutation, c map[string]any) error {
	if m.Execution.Actor.Kind != "user" || m.Execution.Origin != "user" {
		return Fail("APPROVAL_REQUIRED", "Only a person may save or archive a skill")
	}
	page, id := str(c["pageId"]), str(c["skillId"])
	if err := m.TouchPage(page); err != nil {
		return err
	}
	var def SkillDefinition
	if err := contracts.Validate("service", "SkillDefinition", marshal(c["definition"])); err != nil {
		return Fail("INVALID_REQUEST", "Invalid skill definition")
	}
	if err := json.Unmarshal(marshal(c["definition"]), &def); err != nil {
		return err
	}
	if strings.TrimSpace(def.Name) == "" || strings.TrimSpace(def.Instructions) == "" {
		return Fail("INVALID_REQUEST", "Name and instructions are required")
	}
	for _, source := range def.SourcePageIDs {
		if err := m.AuthorizePage(source); err != nil {
			return err
		}
	}
	var version int
	var priorPage string
	err := m.Tx.QueryRowContext(m.Context, "SELECT version,page_id FROM skill_versions WHERE skill_id=? ORDER BY version DESC LIMIT 1", id).Scan(&version, &priorPage)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if int64(version) != number(c["expectedVersion"]) || version > 0 && priorPage != page {
		return Fail("CONFLICT", "Skill changed; reload its latest version")
	}
	_, err = m.Exec("INSERT INTO skill_versions VALUES(?,?,?,?,?,?,?,?)", id, page, version+1, string(marshal(def)), c["archived"], c["imported"], m.Execution.Actor.ActorId, now())
	return err
}
func scanSkill(row interface{ Scan(...any) error }) (SkillVersion, error) {
	var s SkillVersion
	var raw string
	err := row.Scan(&s.ID, &s.PageID, &s.Version, &raw, &s.Archived, &s.Imported, &s.ActorID, &s.CreatedAt)
	if err == nil {
		err = json.Unmarshal([]byte(raw), &s.Definition)
	}
	return s, err
}
func skillVersion(ctx context.Context, q querier, id string, version int) (SkillVersion, error) {
	return scanSkill(q.QueryRowContext(ctx, "SELECT skill_id,page_id,version,definition_json,archived,imported,actor_id,created_at FROM skill_versions WHERE skill_id=? AND version=?", id, version))
}
func listSkills(ctx context.Context, k *Kernel, e contracts.ServiceExecutionContext, r map[string]any) (any, error) {
	query := "SELECT s.skill_id,s.page_id,s.version,s.definition_json,s.archived,s.imported,s.actor_id,s.created_at FROM skill_versions s JOIN pages p ON p.id=s.page_id AND p.deleted_at IS NULL WHERE s.version=(SELECT max(version) FROM skill_versions WHERE skill_id=s.skill_id) ORDER BY s.created_at DESC LIMIT 200"
	args := []any{}
	if id := str(r["skillId"]); id != "" {
		query = "SELECT s.skill_id,s.page_id,s.version,s.definition_json,s.archived,s.imported,s.actor_id,s.created_at FROM skill_versions s JOIN pages p ON p.id=s.page_id AND p.deleted_at IS NULL WHERE s.skill_id=? ORDER BY s.version DESC LIMIT 200"
		args = append(args, id)
	}
	rows, err := k.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []SkillVersion{}
	for rows.Next() {
		s, err := scanSkill(rows)
		if err != nil {
			return nil, err
		}
		if k.authorizePage(ctx, k.db, e, s.PageID) == nil {
			out = append(out, s)
		}
	}
	return out, rows.Err()
}
func sameStringSet(a, b []string) bool {
	a = append([]string{}, a...)
	b = append([]string{}, b...)
	sort.Strings(a)
	sort.Strings(b)
	return string(marshal(a)) == string(marshal(b))
}
func skillSubset(selected, allowed []string) bool {
	set := map[string]bool{}
	for _, s := range allowed {
		set[s] = true
	}
	seen := map[string]bool{}
	for _, s := range selected {
		if !set[s] || seen[s] {
			return false
		}
		seen[s] = true
	}
	return true
}
func (k *Kernel) PreviewSkill(ctx context.Context, e contracts.ServiceExecutionContext, ref SkillRunReference, provider string) (SkillPreview, error) {
	tx, err := k.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return SkillPreview{}, err
	}
	defer tx.Rollback()
	return k.previewSkillTx(ctx, tx, e, ref, provider)
}
func (k *Kernel) previewSkillTx(ctx context.Context, tx querier, e contracts.ServiceExecutionContext, ref SkillRunReference, provider string) (SkillPreview, error) {
	out := SkillPreview{}
	if err := requireOwnerScope(k, e); err != nil {
		return out, err
	}
	if e.Actor.Kind != "user" || e.Origin != "user" {
		return out, Fail("APPROVAL_REQUIRED", "Skills run only after manual review")
	}
	if err := contracts.Validate("service", "SkillRunReference", marshal(ref)); err != nil {
		return out, Fail("INVALID_REQUEST", "Invalid skill invocation")
	}
	if provider != "local" && provider != "codex" && provider != "claude_code" {
		return out, Fail("INVALID_REQUEST", "Choose a supported provider")
	}
	s, err := skillVersion(ctx, tx, ref.SkillID, ref.Version)
	if err != nil {
		return out, err
	}
	if err = k.authorizePage(ctx, tx, e, s.PageID); err != nil {
		return out, err
	}
	var live int
	if err = tx.QueryRowContext(ctx, "SELECT count(*) FROM pages WHERE id=? AND deleted_at IS NULL", s.PageID).Scan(&live); err != nil {
		return out, err
	}
	if live != 1 {
		return out, Fail("NOT_FOUND", "Skill page is in trash")
	}

	var latest int
	var archived bool
	if err = tx.QueryRowContext(ctx, "SELECT version,archived FROM skill_versions WHERE skill_id=? ORDER BY version DESC LIMIT 1", ref.SkillID).Scan(&latest, &archived); err != nil {
		return out, err
	}
	if archived || s.Archived {
		return out, Fail("CONFLICT", "Unarchive the skill before invoking it")
	}
	if !skillSubset(ref.SourcePageIDs, s.Definition.SourcePageIDs) || !skillSubset(ref.Tools, s.Definition.Tools) {
		return out, Fail("SCOPE_DENIED", "Skill run exceeds the saved source or tool selection")
	}
	if (s.Definition.Mode == "answer" || s.Definition.Mode == "summarize") && len(ref.SourcePageIDs) == 0 {
		return out, Fail("INVALID_REQUEST", "Select at least one source")
	}
	out = SkillPreview{Skill: s, Provider: provider, Tools: ref.Tools, Sources: []map[string]string{}, Prompt: fmt.Sprintf("Purpose: %s\nInstructions: %s\nExpected output: %s\nInput: %s", s.Definition.Purpose, s.Definition.Instructions, s.Definition.ExpectedOutput, ref.Input)}
	for _, id := range ref.SourcePageIDs {
		if err = k.authorizePage(ctx, tx, e, id); err != nil {
			return out, err
		}
		var title, revision string
		if err = tx.QueryRowContext(ctx, "SELECT title,revision FROM pages WHERE id=? AND deleted_at IS NULL", id).Scan(&title, &revision); err != nil {
			return out, err
		}
		out.Sources = append(out.Sources, map[string]string{"pageId": id, "title": title, "revision": revision})
	}
	hash := sha256.Sum256(marshal(map[string]any{"preview": out, "input": ref.Input, "invocationId": ref.InvocationID, "latestVersion": latest}))
	out.Hash = hex.EncodeToString(hash[:])
	return out, nil
}

// ResolveSkillRun rechecks the preview and binds retries/handoffs to one exact
// definition/input/source/tool selection. It creates no grants or approvals.
func (k *Kernel) ResolveSkillRun(ctx context.Context, e contracts.ServiceExecutionContext, ref SkillRunReference, provider string) (SkillPreview, error) {
	p, err := k.PreviewSkill(ctx, e, ref, provider)
	if err != nil {
		return p, err
	}
	if ref.PreviewHash == "" || ref.PreviewHash != p.Hash {
		return p, Fail("CONFLICT", "Skill or sources changed; review this run again")
	}
	identity := ref
	identity.SourcePageIDs = append([]string{}, ref.SourcePageIDs...)
	identity.Tools = append([]string{}, ref.Tools...)
	identity.PreviewHash = ""
	sort.Strings(identity.SourcePageIDs)
	sort.Strings(identity.Tools)
	sum := sha256.Sum256(marshal(identity))
	hash := hex.EncodeToString(sum[:])
	err = k.write(ctx, func(tx *sql.Tx) error {
		if err := k.authorize(e); err != nil {
			return err
		}
		current, err := k.previewSkillTx(ctx, tx, e, ref, provider)
		if err != nil {
			return err
		}
		if current.Hash != ref.PreviewHash {
			return Fail("CONFLICT", "Skill or sources changed; review this run again")
		}

		var prior, actor string
		err = tx.QueryRowContext(ctx, "SELECT identity_hash,actor_id FROM skill_invocations WHERE id=?", ref.InvocationID).Scan(&prior, &actor)
		if err == nil {
			if prior != hash || actor != string(e.Actor.ActorId) {
				return Fail("CONFLICT", "Retry or provider handoff cannot change skill version, input, sources or tools")
			}
			return nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		_, err = tx.ExecContext(ctx, "INSERT INTO skill_invocations VALUES(?,?,?,?)", ref.InvocationID, e.Actor.ActorId, hash, now())
		return err
	})
	return p, err
}
func skillAllowsProposal(ref *SkillRunReference, kind string) bool {
	if ref == nil {
		return true
	}
	tool := "proposeChanges"
	if kind == "create_page" || kind == "create_task" || kind == "create_database" {
		tool = "proposeActions"
	}
	return skillSubset([]string{tool}, ref.Tools)
}
