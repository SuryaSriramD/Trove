package workspace

import (
	"context"
	"database/sql"
	"encoding/json"
	"regexp"
	"strings"

	"trove.local/phase0/contracts"
	"trove.local/phase0/internal/providers"
)

// ManagedTools returns exactly the selected connection's canonical tool shapes.
func (b *ProviderBridge) ManagedTools(id, token string) ([]providers.ToolSpec, error) {
	c, err := b.authenticate(id, token)
	if err != nil {
		return nil, err
	}
	if err = b.kernel.authorize(c.execution); err != nil {
		return nil, err
	}
	out := []providers.ToolSpec{}
	for _, name := range c.info.Tools {
		out = append(out, providers.ToolSpec{Name: name, Description: "Scoped Trove service: " + name + ". Proposed writes require owner approval in Trove.", InputSchema: mcpInputSchema(name)})
	}
	return out, nil
}

type ManagedRun struct {
	Skill        *SkillRunReference           `json:"skill,omitempty"`
	ID           string                       `json:"id"`
	SessionID    string                       `json:"sessionId"`
	ConnectionID string                       `json:"connectionId"`
	Provider     string                       `json:"provider"`
	Account      string                       `json:"account"`
	AccountLabel string                       `json:"accountLabel"`
	BillingMode  string                       `json:"billingMode"`
	Model        string                       `json:"model"`
	State        string                       `json:"state"`
	Prompt       string                       `json:"prompt"`
	Text         string                       `json:"text"`
	Message      string                       `json:"message,omitempty"`
	PageIDs      []string                     `json:"pageIds"`
	Tools        []string                     `json:"tools"`
	Manifest     json.RawMessage              `json:"manifest"`
	Checkpoint   *providers.ManagedCheckpoint `json:"checkpoint,omitempty"`
	CreatedAt    string                       `json:"createdAt"`
	UpdatedAt    string                       `json:"updatedAt"`
}

var managedIDPattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

func init() {
	RegisterInitializer(func(ctx context.Context, k *Kernel) error {
		return k.write(ctx, func(tx *sql.Tx) error {
			_, err := tx.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS managed_provider_runs(id TEXT PRIMARY KEY, body_json TEXT NOT NULL, state TEXT NOT NULL, created_at TEXT NOT NULL); UPDATE managed_provider_runs SET state='interrupted',body_json=json_set(body_json,'$.state','interrupted','$.message','Trove stopped before this run completed') WHERE state IN ('starting','running');`)
			return err
		})
	})
}

func (k *Kernel) ManagedRuns(ctx context.Context, e contracts.ServiceExecutionContext) ([]ManagedRun, error) {
	if err := requireOwnerScope(k, e); err != nil {
		return nil, err
	}
	rows, err := k.db.QueryContext(ctx, "SELECT body_json FROM managed_provider_runs ORDER BY created_at DESC,id DESC LIMIT 50")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ManagedRun{}
	for rows.Next() {
		var raw string
		var r ManagedRun
		if err = rows.Scan(&raw); err != nil {
			return nil, err
		}
		if err = json.Unmarshal([]byte(raw), &r); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
func (k *Kernel) SaveManagedRun(ctx context.Context, e contracts.ServiceExecutionContext, r ManagedRun) error {
	if err := requireOwnerScope(k, e); err != nil {
		return err
	}
	if !managedIDPattern.MatchString(r.ID) || !managedIDPattern.MatchString(r.SessionID) || !managedIDPattern.MatchString(r.ConnectionID) || len(r.Prompt) > 64<<10 || len(r.Text) > 1<<20 || len(r.Manifest) > 256<<10 || len(r.Message) > 1000 || len(r.PageIDs) > 100 || r.Account == "" || strings.ContainsAny(r.State, "\n\r") {
		return Fail("INVALID_REQUEST", "Invalid managed execution record")
	}
	switch r.State {
	case "starting", "running", "completed", "failed", "interrupted", "cancelled", "disconnected":
	default:
		return Fail("INVALID_REQUEST", "Invalid managed execution state")
	}
	if r.CreatedAt == "" {
		r.CreatedAt = now()
	}
	r.UpdatedAt = now()
	return k.write(ctx, func(tx *sql.Tx) error {
		if err := k.authorize(e); err != nil {
			return err
		}
		if r.Skill != nil && r.State == "starting" {
			preview, err := k.previewSkillTx(ctx, tx, e, *r.Skill, r.Provider)
			if err != nil {
				return err
			}
			if preview.Hash != r.Skill.PreviewHash {
				return Fail("CONFLICT", "Reviewed skill changed before provider execution")
			}
		}
		_, err := tx.ExecContext(ctx, "INSERT INTO managed_provider_runs(id,body_json,state,created_at) VALUES(?,?,?,?) ON CONFLICT(id) DO UPDATE SET body_json=excluded.body_json,state=excluded.state", r.ID, string(marshal(r)), r.State, r.CreatedAt)
		return err
	})
}
