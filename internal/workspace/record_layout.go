package workspace

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"sort"

	"trove.local/phase0/contracts"
)

type recordLayoutSection struct {
	ID          string   `json:"id"`
	Title       string   `json:"title"`
	Placement   string   `json:"placement"`
	PropertyIDs []string `json:"propertyIds"`
}
type recordLayout struct {
	Version           int                   `json:"version"`
	Sections          []recordLayoutSection `json:"sections"`
	HiddenPropertyIDs []string              `json:"hiddenPropertyIds"`
}
type recoveryLayoutEntry struct {
	CollectionID string        `json:"collectionId"`
	Layout       *recordLayout `json:"layout"`
}
type recoveryLayouts struct {
	Version     int                   `json:"version"`
	RecordID    string                `json:"recordId,omitempty"`
	Override    *recordLayout         `json:"override"`
	Collections []recoveryLayoutEntry `json:"collections"`
}

func init() {
	RegisterInitializer(func(ctx context.Context, k *Kernel) error {
		return k.write(ctx, func(tx *sql.Tx) error {
			_, err := tx.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS record_layouts(owner_kind TEXT NOT NULL CHECK(owner_kind IN('collection','record')),owner_id TEXT NOT NULL,version INTEGER NOT NULL CHECK(version=1),layout_json TEXT NOT NULL CHECK(json_valid(layout_json)),PRIMARY KEY(owner_kind,owner_id));`)
			return err
		})
	})
	RegisterCommand("collection.layout.set", setRecordLayout)
	RegisterCommand("record.layout.set", setRecordLayout)
	RegisterQuery("recordLayouts.get", getRecordLayout)
}
func readRecordLayout(ctx context.Context, q querier, kind, id string) (*recordLayout, error) {
	var raw string
	var version int
	err := q.QueryRowContext(ctx, "SELECT version,layout_json FROM record_layouts WHERE owner_kind=? AND owner_id=?", kind, id).Scan(&version, &raw)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if version != 1 {
		return nil, Fail("UNSUPPORTED_VERSION", "Unsupported record layout")
	}
	var layout recordLayout
	if err = json.Unmarshal([]byte(raw), &layout); err != nil {
		return nil, err
	}
	return &layout, nil
}
func layoutOwner(ctx context.Context, q querier, kind, id string) (page, collection string, err error) {
	if kind == "record" {
		err = q.QueryRowContext(ctx, "SELECT page_id,collection_id FROM records WHERE id=?", id).Scan(&page, &collection)
	} else if kind == "collection" {
		collection = id
		err = q.QueryRowContext(ctx, "SELECT owning_page_id FROM collections WHERE id=?", id).Scan(&page)
	} else {
		err = Fail("INVALID_REQUEST", "Invalid layout owner")
	}
	return
}
func layoutPropertyIDs(layout *recordLayout) []string {
	ids := []string{}
	if layout == nil {
		return ids
	}
	for _, section := range layout.Sections {
		ids = append(ids, section.PropertyIDs...)
	}
	return append(ids, layout.HiddenPropertyIDs...)
}
func validateRecordLayout(ctx context.Context, q querier, collection string, layout *recordLayout) error {
	if layout == nil {
		return nil
	}
	if err := contracts.Validate("service", "RecordLayout", marshal(layout)); err != nil {
		return Fail("INVALID_REQUEST", "Invalid versioned record layout")
	}
	sections, properties := map[string]bool{}, map[string]bool{}
	for _, section := range layout.Sections {
		if sections[section.ID] {
			return Fail("INVALID_REQUEST", "Layout sections need unique identities")
		}
		sections[section.ID] = true
	}
	for _, id := range layoutPropertyIDs(layout) {
		if properties[id] {
			return Fail("INVALID_REQUEST", "A property appears more than once in this layout")
		}
		properties[id] = true
		var owner, kind string
		err := q.QueryRowContext(ctx, "SELECT collection_id,type FROM properties WHERE id=?", id).Scan(&owner, &kind)
		// Removed definitions remain as inert stable references, surfaced to the UI.
		if errors.Is(err, sql.ErrNoRows) {
			continue
		}
		if err != nil {
			return err
		}
		if owner != collection {
			return Fail("INVALID_REQUEST", "Layout property belongs to another collection")
		}
		if kind == "title" {
			return Fail("INVALID_REQUEST", "Record titles remain in the page heading")
		}
	}
	return nil
}
func setRecordLayout(m *Mutation, c map[string]any) error {
	kind, id := "record", str(c["recordId"])
	if c["kind"] == "collection.layout.set" {
		kind, id = "collection", str(c["collectionId"])
	}
	page, collection, err := layoutOwner(m.Context, m.Tx, kind, id)
	if err != nil {
		return err
	}
	var layout *recordLayout
	if err = json.Unmarshal(marshal(c["layout"]), &layout); err != nil {
		return err
	}
	if err = validateRecordLayout(m.Context, m.Tx, collection, layout); err != nil {
		return err
	}
	if err = m.TouchPage(page); err != nil {
		return err
	}
	if kind == "collection" {
		err = m.Touch("collection", collection)
	} else {
		err = m.Require("collection", collection)
	}
	if err != nil {
		return err
	}
	return storeRecordLayout(m, kind, id, layout)
}
func storeRecordLayout(m *Mutation, kind, id string, layout *recordLayout) error {
	if layout == nil {
		_, err := m.Exec("DELETE FROM record_layouts WHERE owner_kind=? AND owner_id=?", kind, id)
		return err
	}
	_, err := m.Exec("INSERT INTO record_layouts VALUES(?,?,1,?) ON CONFLICT(owner_kind,owner_id) DO UPDATE SET version=1,layout_json=excluded.layout_json", kind, id, string(marshal(layout)))
	return err
}
func getRecordLayout(ctx context.Context, k *Kernel, e contracts.ServiceExecutionContext, r map[string]any) (any, error) {
	tx, err := k.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	collection, record := str(r["collectionId"]), str(r["recordId"])
	if err = k.authorizeEntity(ctx, tx, e, "collection", collection); err != nil {
		return nil, err
	}
	defaults, err := readRecordLayout(ctx, tx, "collection", collection)
	if err != nil {
		return nil, err
	}
	var override *recordLayout
	if record != "" {
		if err = k.authorizeEntity(ctx, tx, e, "record", record); err != nil {
			return nil, err
		}
		_, owner, err := layoutOwner(ctx, tx, "record", record)
		if err != nil {
			return nil, err
		}
		if owner != collection {
			return nil, Fail("INVALID_REQUEST", "Record belongs to a different collection")
		}
		override, err = readRecordLayout(ctx, tx, "record", record)
		if err != nil {
			return nil, err
		}
	}
	effective := defaults
	if override != nil {
		effective = override
	}
	if effective == nil {
		effective = &recordLayout{Version: 1, Sections: []recordLayoutSection{}, HiddenPropertyIDs: []string{}}
	}
	missing := []string{}
	for _, id := range layoutPropertyIDs(effective) {
		var n int
		if err = tx.QueryRowContext(ctx, "SELECT count(*) FROM properties WHERE id=? AND collection_id=?", id, collection).Scan(&n); err != nil {
			return nil, err
		}
		if n == 0 {
			missing = append(missing, id)
		}
	}
	sort.Strings(missing)
	out := map[string]any{"collectionId": collection, "defaultLayout": defaults, "overrideLayout": override, "effectiveLayout": effective, "missingPropertyIds": missing}
	if record != "" {
		out["recordId"] = record
	}
	return out, nil
}
func captureRecoveryLayouts(ctx context.Context, q querier, page string) (*recoveryLayouts, error) {
	var exists int
	if err := q.QueryRowContext(ctx, "SELECT count(*) FROM sqlite_master WHERE type='table' AND name='record_layouts'").Scan(&exists); err != nil {
		return nil, err
	}
	if exists == 0 {
		return nil, nil
	}
	out := &recoveryLayouts{Version: 1, Collections: []recoveryLayoutEntry{}}
	err := q.QueryRowContext(ctx, "SELECT id FROM records WHERE page_id=?", page).Scan(&out.RecordID)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	if out.RecordID != "" {
		out.Override, err = readRecordLayout(ctx, q, "record", out.RecordID)
		if err != nil {
			return nil, err
		}
	}
	rows, err := q.QueryContext(ctx, "SELECT id FROM collections WHERE owning_page_id=? ORDER BY id", page)
	if err != nil {
		return nil, err
	}
	ids := []string{}
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return nil, err
		}
		ids = append(ids, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	for _, id := range ids {
		layout, err := readRecordLayout(ctx, q, "collection", id)
		if err != nil {
			return nil, err
		}
		out.Collections = append(out.Collections, recoveryLayoutEntry{id, layout})
	}
	return out, nil
}
func restoreRecoveryLayouts(m *Mutation, snap recoverySnapshot) error {
	if snap.RecordLayouts == nil {
		return nil
	}
	layout := snap.RecordLayouts
	if layout.Version != 1 {
		return Fail("UNSUPPORTED_VERSION", "Unsupported historical record layout")
	}
	if layout.RecordID != "" {
		page, _, err := layoutOwner(m.Context, m.Tx, "record", layout.RecordID)
		if err != nil {
			return err
		}
		if page != string(snap.Page.Id) {
			return Fail("CONFLICT", "Historical layout record identity changed")
		}
		if err = storeRecordLayout(m, "record", layout.RecordID, layout.Override); err != nil {
			return err
		}
	}
	for _, entry := range layout.Collections {
		page, _, err := layoutOwner(m.Context, m.Tx, "collection", entry.CollectionID)
		if err != nil {
			return err
		}
		if page != string(snap.Page.Id) {
			return Fail("CONFLICT", "Historical layout collection identity changed")
		}
		if err = m.Touch("collection", entry.CollectionID); err != nil {
			return err
		}
		if err = storeRecordLayout(m, "collection", entry.CollectionID, entry.Layout); err != nil {
			return err
		}
	}
	return nil
}
