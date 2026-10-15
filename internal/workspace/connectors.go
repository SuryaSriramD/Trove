package workspace

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"trove.local/phase0/contracts"
	"trove.local/phase0/internal/connectors"
)

type ConnectorRefresh struct {
	Version         int    `json:"version"`
	ID              string `json:"id"`
	AccountID       string `json:"accountId"`
	AccountRevision string `json:"accountRevision"`
	State           string `json:"state"`
	SelectionIndex  int    `json:"selectionIndex"`
	Cursor          string `json:"cursor"`
	Pages           int    `json:"pages"`
	Items           int    `json:"items"`
	Error           string `json:"error,omitempty"`
	RetryAfter      string `json:"retryAfter,omitempty"`
}

func init() {
	RegisterInitializer(func(ctx context.Context, k *Kernel) error {
		return k.write(ctx, func(tx *sql.Tx) error {
			_, err := tx.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS connector_accounts(id TEXT PRIMARY KEY,body_json TEXT NOT NULL CHECK(json_valid(body_json)));
CREATE TABLE IF NOT EXISTS connector_credential_cleanup(registration_id TEXT PRIMARY KEY,account_id TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS connector_account_history(sequence INTEGER PRIMARY KEY AUTOINCREMENT,account_id TEXT NOT NULL,body_json TEXT NOT NULL CHECK(json_valid(body_json)),created_at TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS connector_snapshots(id TEXT PRIMARY KEY,account_id TEXT NOT NULL,selection_id TEXT NOT NULL,refresh_id TEXT NOT NULL,body_json TEXT NOT NULL CHECK(json_valid(body_json)));
CREATE INDEX IF NOT EXISTS connector_snapshot_account ON connector_snapshots(account_id,selection_id);
CREATE TABLE IF NOT EXISTS connector_cursors(account_id TEXT NOT NULL,selection_id TEXT NOT NULL,account_revision TEXT NOT NULL,cursor TEXT NOT NULL,PRIMARY KEY(account_id,selection_id));
CREATE TABLE IF NOT EXISTS connector_refreshes(id TEXT PRIMARY KEY,account_id TEXT NOT NULL,body_json TEXT NOT NULL CHECK(json_valid(body_json)));
CREATE TABLE IF NOT EXISTS connector_proposals(id TEXT PRIMARY KEY,account_id TEXT NOT NULL,fingerprint TEXT NOT NULL,body_json TEXT NOT NULL CHECK(json_valid(body_json)));
CREATE TABLE IF NOT EXISTS connector_receipts(sequence INTEGER PRIMARY KEY AUTOINCREMENT,proposal_id TEXT NOT NULL,body_json TEXT NOT NULL CHECK(json_valid(body_json)));
UPDATE connector_refreshes SET body_json=json_set(body_json,'$.state','interrupted','$.error','Application closed during refresh; resume explicitly.') WHERE json_extract(body_json,'$.state')='running';
UPDATE connector_proposals SET body_json=json_set(body_json,'$.state','unknown') WHERE json_extract(body_json,'$.state')='sending';`)
			return err
		})
	})
	RegisterQuery("connectors.list", func(ctx context.Context, k *Kernel, e contracts.ServiceExecutionContext, r map[string]any) (any, error) {
		return k.ConnectorAccounts(ctx, e)
	})
	RegisterQuery("connectors.selections", func(ctx context.Context, k *Kernel, e contracts.ServiceExecutionContext, r map[string]any) (any, error) {
		var ss []connectors.Selection
		if err := json.Unmarshal(marshal(r["selections"]), &ss); err != nil {
			return nil, Fail("INVALID_REQUEST", "Invalid resource selection")
		}
		return k.SetConnectorSelections(ctx, e, str(r["accountId"]), str(r["expectedRevision"]), ss)
	})
	RegisterQuery("connectors.search", connectorSearch)
	RegisterQuery("connectors.history", connectorHistory)
	RegisterQuery("connectors.discard", func(ctx context.Context, k *Kernel, e contracts.ServiceExecutionContext, r map[string]any) (any, error) {
		return nil, k.DiscardConnectorProposal(ctx, e, str(r["proposalId"]))
	})
}

func connectorOwner(k *Kernel, e contracts.ServiceExecutionContext) error {
	if e.Actor.Kind != "user" || e.Origin != "user" {
		return Fail("SCOPE_DENIED", "Connector configuration and writes require direct user review")
	}
	return requireOwnerScope(k, e)
}
func (k *Kernel) AuthorizeConnector(e contracts.ServiceExecutionContext) error {
	return connectorOwner(k, e)
}
func (k *Kernel) ConnectorProposal(ctx context.Context, e contracts.ServiceExecutionContext, id string) (connectors.Proposal, error) {
	if err := connectorOwner(k, e); err != nil {
		return connectors.Proposal{}, err
	}
	return readConnectorProposal(ctx, k.db, id)
}
func connectorAccount(ctx context.Context, q querier, id string) (a connectors.Account, err error) {
	var raw string
	err = q.QueryRowContext(ctx, "SELECT body_json FROM connector_accounts WHERE id=?", id).Scan(&raw)
	if err == nil {
		err = json.Unmarshal([]byte(raw), &a)
	}
	return
}
func saveConnectorAccount(ctx context.Context, tx *sql.Tx, a connectors.Account) error {
	if _, err := tx.ExecContext(ctx, "INSERT INTO connector_accounts(id,body_json) VALUES(?,?) ON CONFLICT(id) DO UPDATE SET body_json=excluded.body_json", a.ID, string(marshal(a))); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, "INSERT INTO connector_account_history(account_id,body_json,created_at) VALUES(?,?,?)", a.ID, string(marshal(a)), now())
	return err
}
func (k *Kernel) ConnectorAccounts(ctx context.Context, e contracts.ServiceExecutionContext) ([]connectors.Account, error) {
	if err := connectorOwner(k, e); err != nil {
		return nil, err
	}
	rows, err := k.db.QueryContext(ctx, "SELECT body_json FROM connector_accounts ORDER BY id")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []connectors.Account{}
	for rows.Next() {
		var a connectors.Account
		var raw string
		if err = rows.Scan(&raw); err != nil {
			return nil, err
		}
		if err = json.Unmarshal([]byte(raw), &a); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}
func (k *Kernel) ConnectorAccount(ctx context.Context, e contracts.ServiceExecutionContext, id string) (connectors.Account, error) {
	if err := connectorOwner(k, e); err != nil {
		return connectors.Account{}, err
	}
	return connectorAccount(ctx, k.db, id)
}

// ConnectConnector receives an identity proven by the native authorization
// broker, never arbitrary renderer account claims or credentials.
func (k *Kernel) ConnectConnector(ctx context.Context, e contracts.ServiceExecutionContext, a connectors.Account, expected string) (connectors.Account, error) {
	if err := connectorOwner(k, e); err != nil {
		return a, err
	}
	if !a.Kind.Valid() || a.Identity.Subject == "" || a.Identity.Provider == "" || a.RegistrationID == "" {
		return a, Fail("INVALID_REQUEST", "Verified connector account is incomplete")
	}
	if a.ID == "" {
		a.ID = NewID()
	}
	if contracts.Validate("domain", "Id", marshal(a.ID)) != nil {
		return a, Fail("INVALID_REQUEST", "Invalid connector identity")
	}
	if a.Endpoint != "" {
		if err := connectors.ValidateEndpoint(a.Endpoint); err != nil {
			return a, err
		}
	}
	err := k.write(ctx, func(tx *sql.Tx) error {
		if err := connectorOwner(k, e); err != nil {
			return err
		}
		old, err := connectorAccount(ctx, tx, a.ID)
		if err == nil {
			if old.Revision != expected || old.Kind != a.Kind || connectors.RequireIdentity(old.Identity, a.Identity) != nil {
				return Fail("CONFLICT", "Account changed; use a new connection for another identity")
			}
			if old.RegistrationID != "" && old.RegistrationID != a.RegistrationID {
				if _, err = tx.ExecContext(ctx, "INSERT OR IGNORE INTO connector_credential_cleanup VALUES(?,?)", old.RegistrationID, old.ID); err != nil {
					return err
				}
			}
			a.Selections = old.Selections
		} else if !errors.Is(err, sql.ErrNoRows) {
			return err
		} else if expected != "" {
			return Fail("CONFLICT", "Connector no longer exists")
		}
		if a.Selections == nil {
			a.Selections = []connectors.Selection{}
		}
		a.Version = 1
		a.State = "active"
		a.Revision = NewID()
		a.UpdatedAt = now()
		return saveConnectorAccount(ctx, tx, a)
	})
	return a, err
}
func (k *Kernel) SetConnectorSelections(ctx context.Context, e contracts.ServiceExecutionContext, id, expected string, selections []connectors.Selection) (a connectors.Account, err error) {
	if err = connectorOwner(k, e); err != nil {
		return
	}
	if len(selections) > 100 {
		return a, Fail("RESOURCE_EXHAUSTED", "Select at most 100 resources per account")
	}
	seen := map[string]bool{}
	for _, s := range selections {
		if s.ID == "" || len(s.ID) > 2048 || s.Kind == "" || len(s.Kind) > 64 || len(s.Query) > 2000 || len(s.Label) > 1000 || seen[s.ID] {
			return a, Fail("INVALID_REQUEST", "Invalid or duplicate connector selection")
		}
		seen[s.ID] = true
	}
	err = k.write(ctx, func(tx *sql.Tx) error {
		if err := connectorOwner(k, e); err != nil {
			return err
		}
		var err error
		a, err = connectorAccount(ctx, tx, id)
		if err != nil {
			return err
		}
		if a.Revision != expected {
			return Fail("CONFLICT", "Connection changed; reload selected resources")
		}
		a.Selections = selections
		a.Revision = NewID()
		a.UpdatedAt = now()
		if err = saveConnectorAccount(ctx, tx, a); err != nil {
			return err
		}
		// Retain provenance but immediately stop exposing deselected evidence.
		if _, err = tx.ExecContext(ctx, "UPDATE connector_snapshots SET body_json=json_set(body_json,'$.state','deselected') WHERE account_id=?", id); err != nil {
			return err
		}
		return nil
	})
	return
}
func (k *Kernel) DisconnectConnector(ctx context.Context, e contracts.ServiceExecutionContext, id, expected string) error {
	if err := connectorOwner(k, e); err != nil {
		return err
	}
	return k.write(ctx, func(tx *sql.Tx) error {
		if err := connectorOwner(k, e); err != nil {
			return err
		}
		a, err := connectorAccount(ctx, tx, id)
		if err != nil {
			return err
		}
		if a.Revision != expected {
			return Fail("CONFLICT", "Connection changed; reload before disconnecting")
		}
		if a.RegistrationID != "" {
			if _, err = tx.ExecContext(ctx, "INSERT OR IGNORE INTO connector_credential_cleanup VALUES(?,?)", a.RegistrationID, a.ID); err != nil {
				return err
			}
		}
		a.State = "disconnected"
		a.RegistrationID = ""
		a.Revision = NewID()
		a.UpdatedAt = now()
		if err = saveConnectorAccount(ctx, tx, a); err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, "UPDATE connector_snapshots SET body_json=json_set(body_json,'$.state','disconnected') WHERE account_id=?", id)
		return err
	})
}
func activeConnector(ctx context.Context, k *Kernel, q querier, e contracts.ServiceExecutionContext, id, rev string) (connectors.Account, error) {
	if err := connectorOwner(k, e); err != nil {
		return connectors.Account{}, err
	}
	a, err := connectorAccount(ctx, q, id)
	if err != nil {
		return a, err
	}
	if a.State != "active" || a.RegistrationID == "" {
		return a, Fail("SCOPE_DENIED", "Connection is inactive; authorize it explicitly")
	}
	if a.Revision != rev {
		return a, Fail("CONFLICT", "Connection or selected resources changed")
	}
	return a, nil
}
func connectorHash(v any) string { sum := sha256.Sum256(marshal(v)); return hex.EncodeToString(sum[:]) }
func connectorFailure(err error) string {
	var f *connectors.Failure
	if errors.As(err, &f) {
		return f.Message
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return "Operation interrupted; resume explicitly"
	}
	return "Connector operation failed; inspect the account and retry explicitly"
}
func saveConnectorRefresh(ctx context.Context, tx *sql.Tx, j ConnectorRefresh) error {
	_, err := tx.ExecContext(ctx, "INSERT INTO connector_refreshes(id,account_id,body_json) VALUES(?,?,?) ON CONFLICT(id) DO UPDATE SET body_json=excluded.body_json", j.ID, j.AccountID, string(marshal(j)))
	return err
}

func (k *Kernel) PrepareConnectorRefresh(ctx context.Context, e contracts.ServiceExecutionContext, id, resume string) (j ConnectorRefresh, err error) {
	if err = connectorOwner(k, e); err != nil {
		return
	}
	err = k.write(ctx, func(tx *sql.Tx) error {
		a, err := connectorAccount(ctx, tx, id)
		if err != nil {
			return err
		}
		if _, err = activeConnector(ctx, k, tx, e, id, a.Revision); err != nil {
			return err
		}
		var cooldown string
		if err = tx.QueryRowContext(ctx, "SELECT COALESCE(MAX(json_extract(body_json,'$.retryAfter')),'') FROM connector_refreshes WHERE account_id=?", id).Scan(&cooldown); err != nil {
			return err
		}
		if deadline, parseErr := time.Parse(time.RFC3339Nano, cooldown); parseErr == nil && time.Now().Before(deadline) {
			return Fail("RESOURCE_EXHAUSTED", "Provider rate limit is still active; refresh after "+cooldown)
		}
		if resume != "" {
			var raw string
			if err = tx.QueryRowContext(ctx, "SELECT body_json FROM connector_refreshes WHERE id=? AND account_id=?", resume, id).Scan(&raw); err != nil {
				return err
			}
			if err = json.Unmarshal([]byte(raw), &j); err != nil {
				return err
			}
			if j.AccountRevision != a.Revision || j.State == "complete" || j.State == "running" {
				return Fail("CONFLICT", "Refresh cannot resume from this state")
			}
			if deadline, parseErr := time.Parse(time.RFC3339Nano, j.RetryAfter); parseErr == nil && time.Now().Before(deadline) {
				return Fail("RESOURCE_EXHAUSTED", "Provider rate limit is still active; resume after "+j.RetryAfter)
			}
		} else {
			j = ConnectorRefresh{Version: 1, ID: NewID(), AccountID: id, AccountRevision: a.Revision}
		}
		var count int
		if err = tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM connector_refreshes WHERE account_id=? AND json_extract(body_json,'$.state')='running'", id).Scan(&count); err != nil {
			return err
		}
		if count > 0 {
			return Fail("RESOURCE_EXHAUSTED", "This account already has a refresh in progress")
		}
		j.State = "running"
		j.Error = ""
		j.RetryAfter = ""
		return saveConnectorRefresh(ctx, tx, j)
	})
	return
}

// RunConnectorRefresh checkpoints after every bounded provider page. An absent
// cursor alone is not proof of complete enumeration; adapters attest Complete.
func (k *Kernel) RunConnectorRefresh(ctx context.Context, e contracts.ServiceExecutionContext, j ConnectorRefresh, adapter connectors.Adapter, s connectors.Session) (err error) {
	defer func() {
		if err != nil {
			j.State = "interrupted"
			j.Error = connectorFailure(err)
			clean := context.WithoutCancel(ctx)
			var failure *connectors.Failure
			if errors.As(err, &failure) && failure.RetryAfter > 0 {
				j.RetryAfter = time.Now().Add(failure.RetryAfter).UTC().Format(time.RFC3339Nano)
			}
			_ = k.write(clean, func(tx *sql.Tx) error {
				if failure != nil && (failure.Code == "ACCESS_REVOKED" || failure.Code == "REMOTE_MISSING") {
					if _, writeErr := tx.ExecContext(clean, "UPDATE connector_snapshots SET body_json=json_set(body_json,'$.state','inaccessible') WHERE account_id=?", j.AccountID); writeErr != nil {
						return writeErr
					}
				}
				return saveConnectorRefresh(clean, tx, j)
			})
		}
	}()
	a, err := activeConnector(ctx, k, k.db, e, j.AccountID, j.AccountRevision)
	if err != nil {
		return err
	}
	s.Account = a
	identity, err := adapter.Identity(ctx, s)
	if err != nil {
		return err
	}
	if err = connectors.RequireIdentity(a.Identity, identity); err != nil {
		return err
	}
	for count := 0; count < 100 && j.SelectionIndex < len(a.Selections); count++ {
		if err = ctx.Err(); err != nil {
			return err
		}
		if _, err = activeConnector(ctx, k, k.db, e, a.ID, a.Revision); err != nil {
			return err
		}
		sel := a.Selections[j.SelectionIndex]
		if j.Cursor == "" {
			var cursor string
			cursorErr := k.db.QueryRowContext(ctx, "SELECT cursor FROM connector_cursors WHERE account_id=? AND selection_id=? AND account_revision=?", a.ID, sel.ID, a.Revision).Scan(&cursor)
			if cursorErr != nil && !errors.Is(cursorErr, sql.ErrNoRows) {
				return cursorErr
			}
			j.Cursor = cursor
		}
		page, err := adapter.Refresh(ctx, s, sel, j.Cursor)
		if err != nil {
			return err
		}
		if len(page.Items) > 1000 || len(page.NextCursor) > 32768 || len(page.SyncCursor) > 32768 || (!page.Complete && page.NextCursor == "") || (page.NextCursor != "" && page.NextCursor == j.Cursor) || (page.Complete && page.NextCursor != "") {
			return Fail("INVALID_RESPONSE", "Provider refresh cursor or completion evidence is invalid")
		}
		if j.Items+len(page.Items) > 20000 {
			return Fail("RESOURCE_EXHAUSTED", "Refresh reached 20,000 items; narrow selected resources")
		}
		beforePage := j
		err = k.write(ctx, func(tx *sql.Tx) error {
			if _, err := activeConnector(ctx, k, tx, e, a.ID, a.Revision); err != nil {
				return err
			}
			for _, item := range page.Items {
				if item.RemoteID == "" || len(item.RemoteID) > 2048 || len(item.Text) > 256<<10 || len(item.Content) > 8<<20 {
					return Fail("INVALID_RESPONSE", "Provider returned invalid evidence")
				}
				item.Version = 1
				item.AccountID = a.ID
				item.SelectionID = sel.ID
				item.ID = connectorHash([]string{a.ID, sel.ID, item.RemoteID})
				item.RetrievedAt = now()
				item.ExternalAIAllowed = false
				if item.State != "removed" && item.State != "inaccessible" && item.State != "partial" {
					item.State = "present"
				}
				if _, err := tx.ExecContext(ctx, "INSERT INTO connector_snapshots(id,account_id,selection_id,refresh_id,body_json) VALUES(?,?,?,?,?) ON CONFLICT(id) DO UPDATE SET refresh_id=excluded.refresh_id,body_json=excluded.body_json", item.ID, a.ID, sel.ID, j.ID, string(marshal(item))); err != nil {
					return err
				}
			}
			j.Pages++
			j.Items += len(page.Items)
			j.Cursor = page.NextCursor
			if page.Complete {
				if _, err := tx.ExecContext(ctx, "INSERT INTO connector_cursors(account_id,selection_id,account_revision,cursor) VALUES(?,?,?,?) ON CONFLICT(account_id,selection_id) DO UPDATE SET account_revision=excluded.account_revision,cursor=excluded.cursor", a.ID, sel.ID, a.Revision, page.SyncCursor); err != nil {
					return err
				}
				if page.FullEnumeration {
					if _, err := tx.ExecContext(ctx, "UPDATE connector_snapshots SET body_json=json_set(body_json,'$.state','removed') WHERE account_id=? AND selection_id=? AND refresh_id<>? AND json_extract(body_json,'$.state')='present'", a.ID, sel.ID, j.ID); err != nil {
						return err
					}
				}
				j.SelectionIndex++
			}
			if j.SelectionIndex == len(a.Selections) {
				j.State = "complete"
			}
			return saveConnectorRefresh(ctx, tx, j)
		})
		if err != nil {
			j = beforePage
			return err
		}
	}
	if len(a.Selections) == 0 {
		j.State = "complete"
		return k.write(ctx, func(tx *sql.Tx) error { return saveConnectorRefresh(ctx, tx, j) })
	}
	if j.State != "complete" {
		j.State = "paused"
		return k.write(ctx, func(tx *sql.Tx) error { return saveConnectorRefresh(ctx, tx, j) })
	}
	return nil
}

func connectorSearch(ctx context.Context, k *Kernel, e contracts.ServiceExecutionContext, r map[string]any) (any, error) {
	if err := connectorOwner(k, e); err != nil {
		return nil, err
	}
	q := str(r["query"])
	if len(q) > 1000 {
		return nil, Fail("INVALID_REQUEST", "Search query is too long")
	}
	rows, err := k.db.QueryContext(ctx, `SELECT s.body_json FROM connector_snapshots s JOIN connector_accounts a ON a.id=s.account_id WHERE json_extract(a.body_json,'$.state')='active' AND json_extract(s.body_json,'$.state') IN('present','partial') AND (instr(lower(json_extract(s.body_json,'$.title')),lower(?))>0 OR instr(lower(json_extract(s.body_json,'$.text')),lower(?))>0) ORDER BY s.id LIMIT 100`, q, q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []connectors.Snapshot{}
	for rows.Next() {
		var v connectors.Snapshot
		var raw string
		if err = rows.Scan(&raw); err != nil {
			return nil, err
		}
		if err = json.Unmarshal([]byte(raw), &v); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}
func connectorHistory(ctx context.Context, k *Kernel, e contracts.ServiceExecutionContext, r map[string]any) (any, error) {
	if err := connectorOwner(k, e); err != nil {
		return nil, err
	}
	out := map[string]any{}
	for name, table := range map[string]string{"refreshes": "connector_refreshes", "proposals": "connector_proposals"} {
		rows, err := k.db.QueryContext(ctx, "SELECT body_json FROM "+table+" WHERE account_id=? ORDER BY id DESC LIMIT 100", str(r["accountId"]))
		if err != nil {
			return nil, err
		}
		values := []json.RawMessage{}
		for rows.Next() {
			var raw string
			if err = rows.Scan(&raw); err != nil {
				rows.Close()
				return nil, err
			}
			values = append(values, json.RawMessage(raw))
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return nil, err
		}
		out[name] = values
	}
	return out, nil
}

func readConnectorProposal(ctx context.Context, q querier, id string) (p connectors.Proposal, err error) {
	var raw string
	err = q.QueryRowContext(ctx, "SELECT body_json FROM connector_proposals WHERE id=?", id).Scan(&raw)
	if err == nil {
		err = json.Unmarshal([]byte(raw), &p)
	}
	return
}
func saveConnectorProposal(ctx context.Context, tx *sql.Tx, p connectors.Proposal) error {
	_, err := tx.ExecContext(ctx, "INSERT INTO connector_proposals(id,account_id,fingerprint,body_json) VALUES(?,?,?,?) ON CONFLICT(id) DO UPDATE SET body_json=excluded.body_json", p.ID, p.AccountID, connectorHash(p.Action), string(marshal(p)))
	return err
}
func (k *Kernel) PrepareConnectorProposal(ctx context.Context, e contracts.ServiceExecutionContext, id string, action connectors.Action, adapter connectors.Adapter, s connectors.Session) (p connectors.Proposal, err error) {
	if err = connectorOwner(k, e); err != nil {
		return
	}
	a, err := connectorAccount(ctx, k.db, id)
	if err != nil {
		return p, err
	}
	if _, err = activeConnector(ctx, k, k.db, e, id, a.Revision); err != nil {
		return p, err
	}
	if _, err = connectors.Selected(a, action.SelectionID); err != nil {
		return p, err
	}
	s.Account = a
	identity, err := adapter.Identity(ctx, s)
	if err != nil {
		return p, err
	}
	if err = connectors.RequireIdentity(a.Identity, identity); err != nil {
		return p, err
	}
	action, err = adapter.Prepare(ctx, s, action)
	if err != nil {
		return p, err
	}
	if _, err = connectors.Selected(a, action.SelectionID); err != nil {
		return p, err
	}
	if action.Version != 1 || len(action.Arguments) > 8<<20 || !json.Valid(action.Arguments) || len(action.Effects) == 0 {
		return p, Fail("INVALID_REQUEST", "Connector must declare exact arguments and effects")
	}
	p = connectors.Proposal{Version: 1, ID: NewID(), AccountID: a.ID, AccountRevision: a.Revision, Identity: a.Identity, Action: action, State: "review", CreatedAt: now()}
	p.Hash = connectorHash(p)
	err = k.write(ctx, func(tx *sql.Tx) error {
		if _, err := activeConnector(ctx, k, tx, e, id, a.Revision); err != nil {
			return err
		}
		var n int
		if err := tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM connector_proposals WHERE account_id=? AND json_extract(body_json,'$.state') IN('sending','unknown')", id).Scan(&n); err != nil {
			return err
		}
		if n > 0 {
			return Fail("CONFLICT", "This account has an unknown write outcome; reconcile it before preparing another write")
		}
		return saveConnectorProposal(ctx, tx, p)
	})
	return
}
func (k *Kernel) ExecuteConnectorProposal(ctx context.Context, e contracts.ServiceExecutionContext, id, hash string, adapter connectors.Adapter, s connectors.Session) (receipt connectors.Receipt, err error) {
	if err = connectorOwner(k, e); err != nil {
		return
	}
	p, err := readConnectorProposal(ctx, k.db, id)
	if err != nil {
		return receipt, err
	}
	if p.Hash != hash {
		return receipt, Fail("CONFLICT", "Reviewed proposal hash does not match")
	}
	if p.State == "succeeded" && p.Receipt != nil {
		return *p.Receipt, nil
	}
	if p.State != "review" {
		return receipt, Fail("CONFLICT", "This proposal is no longer pending review; inspect its receipt")
	}
	defer func() {
		if err == nil || p.State != "review" {
			return
		}
		clean := context.WithoutCancel(ctx)
		_ = k.write(clean, func(tx *sql.Tx) error {
			current, readErr := readConnectorProposal(clean, tx, p.ID)
			if readErr != nil {
				return readErr
			}
			if current.State != "review" || current.Hash != p.Hash {
				return nil
			}
			receipt = connectors.ReceiptFor(p, "failed", "", "", "", "No external write was attempted. "+connectorFailure(err))
			current.Receipt = &receipt
			current.State = "failed"
			if saveErr := saveConnectorProposal(clean, tx, current); saveErr != nil {
				return saveErr
			}
			_, saveErr := tx.ExecContext(clean, "INSERT INTO connector_receipts(proposal_id,body_json) VALUES(?,?)", p.ID, string(marshal(receipt)))
			return saveErr
		})
	}()
	a, err := activeConnector(ctx, k, k.db, e, p.AccountID, p.AccountRevision)
	if err != nil {
		return receipt, err
	}
	s.Account = a
	identity, err := adapter.Identity(ctx, s)
	if err != nil {
		return receipt, err
	}
	if err = connectors.RequireIdentity(p.Identity, identity); err != nil {
		return receipt, err
	}
	if err = adapter.Check(ctx, s, p.Action); err != nil {
		return receipt, err
	}
	err = k.write(ctx, func(tx *sql.Tx) error {
		if _, err := activeConnector(ctx, k, tx, e, p.AccountID, p.AccountRevision); err != nil {
			return err
		}
		current, err := readConnectorProposal(ctx, tx, id)
		if err != nil {
			return err
		}
		if current.State != "review" || current.Hash != hash {
			return Fail("CONFLICT", "Proposal was already executed or changed")
		}
		p.State = "sending"
		return saveConnectorProposal(ctx, tx, p)
	})
	if err != nil {
		return receipt, err
	}
	// Persist before dispatch. Even a crash after successful remote commit leaves
	// an unknown outbox item, never a replayable pending action.
	receipt, err = adapter.Send(ctx, s, p)
	if err != nil {
		outcome := "unknown"
		var f *connectors.Failure
		if errors.As(err, &f) && !f.OutcomeUnknown {
			outcome = "failed"
		}
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			outcome = "unknown"
		}
		receipt = connectors.ReceiptFor(p, outcome, "", "", "", connectorFailure(err))
	}
	if receipt.Outcome != "succeeded" && receipt.Outcome != "failed" && receipt.Outcome != "unknown" {
		receipt = connectors.ReceiptFor(p, "unknown", "", "", "", "Provider outcome was not verified")
	}
	receipt.Version = 1
	receipt.ProposalID = p.ID
	receipt.RecordedAt = now()
	p.Receipt = &receipt
	p.State = receipt.Outcome
	clean := context.WithoutCancel(ctx)
	persistErr := k.write(clean, func(tx *sql.Tx) error {
		if err := saveConnectorProposal(clean, tx, p); err != nil {
			return err
		}
		_, err := tx.ExecContext(clean, "INSERT INTO connector_receipts(proposal_id,body_json) VALUES(?,?)", p.ID, string(marshal(receipt)))
		return err
	})
	if persistErr != nil {
		return receipt, persistErr
	}
	return receipt, err
}
func (k *Kernel) ReconcileConnectorProposal(ctx context.Context, e contracts.ServiceExecutionContext, id string, adapter connectors.Adapter, s connectors.Session) (receipt connectors.Receipt, err error) {
	if err = connectorOwner(k, e); err != nil {
		return
	}
	p, err := readConnectorProposal(ctx, k.db, id)
	if err != nil {
		return receipt, err
	}
	if p.State != "unknown" {
		return receipt, Fail("CONFLICT", "Only uncertain outcomes require reconciliation")
	}
	a, err := connectorAccount(ctx, k.db, p.AccountID)
	if err == nil {
		a, err = activeConnector(ctx, k, k.db, e, p.AccountID, a.Revision)
	}
	if err != nil {
		return receipt, err
	}
	if _, err = connectors.Selected(a, p.Action.SelectionID); err != nil {
		return receipt, err
	}
	s.Account = a
	identity, err := adapter.Identity(ctx, s)
	if err != nil {
		return receipt, err
	}
	if err = connectors.RequireIdentity(p.Identity, identity); err != nil {
		return receipt, err
	}
	receipt, err = adapter.Reconcile(ctx, s, p)
	if err != nil {
		return receipt, err
	}
	if receipt.Outcome != "succeeded" && receipt.Outcome != "failed" && receipt.Outcome != "unknown" {
		return receipt, Fail("INVALID_RESPONSE", "Reconciliation did not provide a valid outcome")
	}
	receipt.Version = 1
	receipt.ProposalID = id
	receipt.RecordedAt = now()
	err = k.write(ctx, func(tx *sql.Tx) error {
		if _, err := activeConnector(ctx, k, tx, e, p.AccountID, a.Revision); err != nil {
			return err
		}
		current, err := readConnectorProposal(ctx, tx, id)
		if err != nil {
			return err
		}
		if current.State != "unknown" {
			return Fail("CONFLICT", "Proposal outcome changed during reconciliation")
		}
		p.Receipt = &receipt
		p.State = receipt.Outcome
		if err = saveConnectorProposal(ctx, tx, p); err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, "INSERT INTO connector_receipts(proposal_id,body_json) VALUES(?,?)", id, string(marshal(receipt)))
		return err
	})
	return
}
func (k *Kernel) DiscardConnectorProposal(ctx context.Context, e contracts.ServiceExecutionContext, id string) error {
	if err := connectorOwner(k, e); err != nil {
		return err
	}
	return k.write(ctx, func(tx *sql.Tx) error {
		if err := connectorOwner(k, e); err != nil {
			return err
		}
		p, err := readConnectorProposal(ctx, tx, id)
		if err != nil {
			return err
		}
		if p.State != "review" {
			return Fail("CONFLICT", "Only an unsent proposal can be discarded")
		}
		p.State = "discarded"
		return saveConnectorProposal(ctx, tx, p)
	})
}

// Native and forward-compatible restores retain connection evidence, never
// activation or an executable outbox. Unknown outcomes remain unknown.
func resetConnectorRestore(ctx context.Context, tx *sql.Tx) error {
	// Forward-compatible restore cannot execute unknown table shapes/triggers.
	// These optional tables did not exist in earlier readable backups.
	shapes := map[string]string{
		"connector_credential_cleanup": `CREATE TABLE connector_credential_cleanup(registration_id TEXT PRIMARY KEY,account_id TEXT NOT NULL)`,
		"connector_accounts":           `CREATE TABLE connector_accounts(id TEXT PRIMARY KEY,body_json TEXT NOT NULL CHECK(json_valid(body_json)))`,
		"connector_snapshots":          `CREATE TABLE connector_snapshots(id TEXT PRIMARY KEY,account_id TEXT NOT NULL,selection_id TEXT NOT NULL,refresh_id TEXT NOT NULL,body_json TEXT NOT NULL CHECK(json_valid(body_json)))`,
		"connector_cursors":            `CREATE TABLE connector_cursors(account_id TEXT NOT NULL,selection_id TEXT NOT NULL,account_revision TEXT NOT NULL,cursor TEXT NOT NULL,PRIMARY KEY(account_id,selection_id))`,
		"connector_refreshes":          `CREATE TABLE connector_refreshes(id TEXT PRIMARY KEY,account_id TEXT NOT NULL,body_json TEXT NOT NULL CHECK(json_valid(body_json)))`,
		"connector_proposals":          `CREATE TABLE connector_proposals(id TEXT PRIMARY KEY,account_id TEXT NOT NULL,fingerprint TEXT NOT NULL,body_json TEXT NOT NULL CHECK(json_valid(body_json)))`,
	}
	for name, expected := range shapes {
		var kind, ddl string
		err := tx.QueryRowContext(ctx, "SELECT type,sql FROM sqlite_schema WHERE name=?", name).Scan(&kind, &ddl)
		if errors.Is(err, sql.ErrNoRows) {
			continue
		}
		if err != nil {
			return err
		}
		if kind != "table" || normalizeCompatibleDDL(ddl) != normalizeCompatibleDDL(expected) {
			return Fail("UNSUPPORTED_SCHEMA", "Connector restore schema is not supported")
		}
		var triggers, rows int
		if err = tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM sqlite_schema WHERE type='trigger' AND tbl_name=? COLLATE NOCASE", name).Scan(&triggers); err != nil {
			return err
		}
		if triggers != 0 {
			return Fail("INTEGRITY_ERROR", "Connector restore cannot run schema triggers")
		}
		if err = tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+name).Scan(&rows); err != nil {
			return err
		}
		if rows > 1000000 {
			return Fail("RESOURCE_EXHAUSTED", "Connector restore exceeds the supported row limit")
		}
	}
	dependencies, err := tx.QueryContext(ctx, `SELECT f."table",f.on_update,f.on_delete FROM sqlite_schema s JOIN pragma_foreign_key_list(s.name) f WHERE s.type='table' LIMIT 10001`)
	if err != nil {
		return err
	}
	count := 0
	for dependencies.Next() {
		var table, update, del string
		if err = dependencies.Scan(&table, &update, &del); err != nil {
			dependencies.Close()
			return err
		}
		count++
		if _, exists := shapes[strings.ToLower(table)]; exists && ((update != "NO ACTION" && update != "RESTRICT") || (del != "NO ACTION" && del != "RESTRICT")) {
			dependencies.Close()
			return Fail("INTEGRITY_ERROR", "Connector restore has an unsafe foreign-key dependency")
		}
	}
	err = dependencies.Err()
	dependencies.Close()
	if err != nil {
		return err
	}
	if count > 10000 {
		return Fail("RESOURCE_EXHAUSTED", "Connector restore has too many schema relationships")
	}
	for table, statement := range map[string]string{
		"connector_cursors":   `DELETE FROM connector_cursors`,
		"connector_accounts":  `UPDATE connector_accounts SET body_json=json_set(body_json,'$.state','restored_inactive','$.registrationId','','$.revision',lower(hex(randomblob(16))))`,
		"connector_snapshots": `UPDATE connector_snapshots SET body_json=json_set(body_json,'$.state','restored_inactive')`,
		"connector_refreshes": `UPDATE connector_refreshes SET body_json=json_set(body_json,'$.state','restored_inactive') WHERE json_extract(body_json,'$.state')!='complete'`,
		"connector_proposals": `UPDATE connector_proposals SET body_json=json_set(body_json,'$.state',CASE WHEN json_extract(body_json,'$.state') IN('sending','unknown') THEN 'unknown' ELSE 'restored_inactive' END) WHERE json_extract(body_json,'$.state') IN('review','sending','unknown')`,
	} {
		var n int
		if err := tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name=?", table).Scan(&n); err != nil {
			return err
		}
		if n > 0 {
			if _, err := tx.ExecContext(ctx, statement); err != nil {
				return err
			}
		}
	}
	var cleanupTable int
	if err := tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM sqlite_schema WHERE type='table' AND name='connector_credential_cleanup'").Scan(&cleanupTable); err != nil {
		return err
	}
	if cleanupTable > 0 {
		if _, err := tx.ExecContext(ctx, "DELETE FROM connector_credential_cleanup"); err != nil {
			return err
		}
	}

	return nil
}

func (k *Kernel) ConnectorCredentialCleanup(ctx context.Context, e contracts.ServiceExecutionContext, account string) ([]string, error) {
	if err := connectorOwner(k, e); err != nil {
		return nil, err
	}
	rows, err := k.db.QueryContext(ctx, "SELECT registration_id FROM connector_credential_cleanup WHERE account_id=?", account)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}
func (k *Kernel) CompleteConnectorCredentialCleanup(ctx context.Context, e contracts.ServiceExecutionContext, account, id string) error {
	return k.write(ctx, func(tx *sql.Tx) error {
		if err := connectorOwner(k, e); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, "DELETE FROM connector_credential_cleanup WHERE registration_id=? AND account_id=?", id, account)
		return err
	})
}
