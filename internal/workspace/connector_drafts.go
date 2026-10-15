package workspace

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	"trove.local/phase0/contracts"
)

type ConnectorDraft struct {
	AccountID string          `json:"accountId"`
	Revision  string          `json:"revision"`
	Fields    json.RawMessage `json:"fields"`
	UpdatedAt string          `json:"updatedAt"`
}

func init() {
	RegisterInitializer(func(ctx context.Context, k *Kernel) error {
		return k.write(ctx, func(tx *sql.Tx) error {
			_, err := tx.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS connector_drafts(account_id TEXT PRIMARY KEY,body_json TEXT NOT NULL CHECK(json_valid(body_json))); CREATE TABLE IF NOT EXISTS connector_draft_history(sequence INTEGER PRIMARY KEY AUTOINCREMENT,account_id TEXT NOT NULL,body_json TEXT NOT NULL CHECK(json_valid(body_json)));`)
			return err
		})
	})
	RegisterQuery("connectors.draft", readConnectorDraft)
	RegisterQuery("connectors.saveDraft", saveConnectorDraft)
}
func readConnectorDraft(ctx context.Context, k *Kernel, e contracts.ServiceExecutionContext, r map[string]any) (any, error) {
	if err := connectorOwner(k, e); err != nil {
		return nil, err
	}
	id := str(r["accountId"])
	if _, err := connectorAccount(ctx, k.db, id); err != nil {
		return nil, err
	}
	result := ConnectorDraft{AccountID: id, Fields: json.RawMessage(`{}`)}
	var raw string
	err := k.db.QueryRowContext(ctx, "SELECT body_json FROM connector_drafts WHERE account_id=?", id).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return result, nil
	}
	if err != nil {
		return nil, err
	}
	err = json.Unmarshal([]byte(raw), &result)
	return result, err
}
func saveConnectorDraft(ctx context.Context, k *Kernel, e contracts.ServiceExecutionContext, r map[string]any) (any, error) {
	if err := connectorOwner(k, e); err != nil {
		return nil, err
	}
	data := marshal(r["fields"])
	if len(data) > 8<<20 {
		return nil, Fail("RESOURCE_EXHAUSTED", "Composition exceeds the 8 MiB draft limit")
	}
	d := ConnectorDraft{AccountID: str(r["accountId"]), Revision: NewID(), Fields: data, UpdatedAt: now()}
	err := k.write(ctx, func(tx *sql.Tx) error {
		if err := connectorOwner(k, e); err != nil {
			return err
		}
		if _, err := connectorAccount(ctx, tx, d.AccountID); err != nil {
			return err
		}
		var revision string
		err := tx.QueryRowContext(ctx, "SELECT json_extract(body_json,'$.revision') FROM connector_drafts WHERE account_id=?", d.AccountID).Scan(&revision)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if revision != str(r["expectedRevision"]) {
			return Fail("CONFLICT", "Saved composition changed; reload it before replacing it")
		}
		raw := string(marshal(d))
		if _, err = tx.ExecContext(ctx, "INSERT INTO connector_drafts VALUES(?,?) ON CONFLICT(account_id) DO UPDATE SET body_json=excluded.body_json", d.AccountID, raw); err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, "INSERT INTO connector_draft_history(account_id,body_json) VALUES(?,?)", d.AccountID, raw)
		return err
	})
	return d, err
}
