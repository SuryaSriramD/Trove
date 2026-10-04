package workspace

import (
	"context"
	"database/sql"
	"trove.local/phase0/contracts"
)

// Display settings are page-owned, revision-fenced metadata. They never alter
// text extraction, citation locations or the meaning of the page's content.
type PageDisplayPreferences struct {
	FullWidth    bool `json:"fullWidth"`
	Presentation bool `json:"presentation"`
}

func init() {
	RegisterInitializer(func(ctx context.Context, k *Kernel) error {
		return k.write(ctx, func(tx *sql.Tx) error {
			_, err := tx.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS p2_page_display(page_id TEXT PRIMARY KEY REFERENCES pages(id),full_width INTEGER NOT NULL CHECK(full_width IN(0,1)),presentation INTEGER NOT NULL CHECK(presentation IN(0,1)));`)
			return err
		})
	})
	RegisterQuery("pageDisplay.get", pageDisplayGet)
	RegisterCommand("page.display.set", pageDisplaySet)
}

func readPageDisplay(ctx context.Context, q querier, page string) (PageDisplayPreferences, error) {
	var out PageDisplayPreferences
	err := q.QueryRowContext(ctx, "SELECT full_width,presentation FROM p2_page_display WHERE page_id=?", page).Scan(&out.FullWidth, &out.Presentation)
	if err == sql.ErrNoRows {
		err = nil
	}
	return out, err
}

func pageDisplayGet(ctx context.Context, k *Kernel, e contracts.ServiceExecutionContext, r map[string]any) (any, error) {
	page := str(r["pageId"])
	if err := k.authorizePage(ctx, k.db, e, page); err != nil {
		return nil, err
	}
	tx, err := k.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	p, err := readPageDisplay(ctx, tx, page)
	if err != nil {
		return nil, err
	}
	var revision string
	if err = tx.QueryRowContext(ctx, "SELECT revision FROM pages WHERE id=? AND deleted_at IS NULL", page).Scan(&revision); err != nil {
		return nil, err
	}
	return map[string]any{"pageId": page, "fullWidth": p.FullWidth, "presentation": p.Presentation, "revision": revision}, nil
}

func pageDisplaySet(m *Mutation, c map[string]any) error {
	page := str(c["pageId"])
	if err := m.AuthorizePage(page); err != nil {
		return err
	}
	if err := m.TouchPage(page); err != nil {
		return err
	}
	full, ok := c["fullWidth"].(bool)
	if !ok {
		return Fail("INVALID_REQUEST", "fullWidth must be boolean")
	}
	presentation, ok := c["presentation"].(bool)
	if !ok {
		return Fail("INVALID_REQUEST", "presentation must be boolean")
	}
	_, err := m.Exec("INSERT INTO p2_page_display VALUES(?,?,?) ON CONFLICT(page_id) DO UPDATE SET full_width=excluded.full_width,presentation=excluded.presentation", page, full, presentation)
	return err
}
