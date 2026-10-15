package workspace

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"trove.local/phase0/contracts"
)

const collectionVisibilityDDL = `CREATE TABLE collection_visibility(collection_id TEXT PRIMARY KEY REFERENCES collections(id),version INTEGER NOT NULL CHECK(version=1),deleted_at TEXT NOT NULL)`

type recoveryCollectionVisibility struct {
	CollectionID string  `json:"collectionId"`
	DeletedAt    *string `json:"deletedAt"`
}

func init() {
	RegisterInitializer(func(ctx context.Context, k *Kernel) error {
		return k.write(ctx, func(tx *sql.Tx) error {
			if err := validateCollectionVisibilityRestore(ctx, tx); err != nil {
				return err
			}
			_, err := tx.ExecContext(ctx, strings.Replace(collectionVisibilityDDL, "CREATE TABLE ", "CREATE TABLE IF NOT EXISTS ", 1))
			return err
		})
	})
	RegisterCommand("collection.trash.set", setCollectionTrash)
	RegisterCommitValidator(func(m *Mutation) error {
		for _, change := range m.touched {
			kind, id := change.Target.EntityType, string(change.Target.EntityId)
			if kind == "collection" && !m.collectionVisibilityChanges[id] {
				var hidden int
				if err := m.Tx.QueryRowContext(m.Context, "SELECT count(*) FROM collection_visibility WHERE collection_id=?", id).Scan(&hidden); err != nil {
					return err
				}
				if hidden != 0 {
					return Fail("NOT_FOUND", "Collection is in Trash")
				}
			}
			if kind == "property" || kind == "view" {
				var collection string
				if err := m.Tx.QueryRowContext(m.Context, "SELECT collection_id FROM "+entityTables[kind]+" WHERE id=?", id).Scan(&collection); err == nil {
					if err := requireActiveCollection(m.Context, m.Tx, collection); err != nil {
						return err
					}
				} else if !errors.Is(err, sql.ErrNoRows) {
					return err
				}
			}
		}
		return validateCollectionVisibilityInvariant(m.Context, m.Tx)
	})
}
func validateCollectionVisibilityRestore(ctx context.Context, q querier) error {
	var kind, ddl string
	err := q.QueryRowContext(ctx, "SELECT type,sql FROM sqlite_schema WHERE name='collection_visibility'").Scan(&kind, &ddl)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if kind != "table" || normalizeCompatibleDDL(ddl) != normalizeCompatibleDDL(collectionVisibilityDDL) {
		return Fail("UNSUPPORTED_SCHEMA", "Collection visibility restore schema is not supported")
	}
	var count int
	if err = q.QueryRowContext(ctx, "SELECT count(*) FROM sqlite_schema WHERE type='trigger' AND tbl_name='collection_visibility' COLLATE NOCASE").Scan(&count); err != nil {
		return err
	}
	if count != 0 {
		return Fail("INTEGRITY_ERROR", "Collection visibility cannot contain schema triggers")
	}
	rows, err := q.QueryContext(ctx, "SELECT collection_id,version,deleted_at FROM collection_visibility")
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var id, stamp string
		var version int
		if err = rows.Scan(&id, &version, &stamp); err != nil {
			return err
		}
		if version != 1 || contracts.Validate("domain", "Id", marshal(id)) != nil || contracts.Validate("domain", "Timestamp", marshal(stamp)) != nil {
			return Fail("INTEGRITY_ERROR", "Invalid collection visibility metadata")
		}
	}
	if err = rows.Err(); err != nil {
		return err
	}
	rows.Close()
	return validateCollectionVisibilityInvariant(ctx, q)
}
func validateCollectionVisibilityInvariant(ctx context.Context, q querier) error {
	var count int
	if err := q.QueryRowContext(ctx, `SELECT count(*) FROM collection_visibility v LEFT JOIN collections c ON c.id=v.collection_id WHERE c.id IS NULL OR EXISTS(SELECT 1 FROM records r JOIN pages p ON p.id=r.page_id WHERE r.collection_id=v.collection_id AND p.deleted_at IS NULL)`).Scan(&count); err != nil {
		return err
	}
	if count > 0 {
		return Fail("CONFLICT", "A hidden collection cannot contain live records; restore the collection before restoring its records")
	}
	return nil
}
func requireActiveCollection(ctx context.Context, q querier, id string) error {
	var hidden bool
	if err := q.QueryRowContext(ctx, `SELECT p.deleted_at IS NOT NULL OR EXISTS(SELECT 1 FROM collection_visibility v WHERE v.collection_id=c.id) FROM collections c JOIN pages p ON p.id=c.owning_page_id WHERE c.id=?`, id).Scan(&hidden); err != nil {
		return err
	}
	if hidden {
		return Fail("NOT_FOUND", "Collection is in Trash; restore it before querying or editing its records")
	}
	return nil
}
func collectionTrashCurrent(ctx context.Context, q querier, action map[string]any) (journalEntry, error) {
	id := str(action["collectionId"])
	entry := journalEntry{Target: "collection-trash:" + id}
	if err := q.QueryRowContext(ctx, `SELECT c.owning_page_id,COALESCE(v.deleted_at,'') FROM collections c LEFT JOIN collection_visibility v ON v.collection_id=c.id WHERE c.id=?`, id).Scan(&entry.PageID, &entry.Tombstone); err != nil {
		return entry, err
	}
	entry.Before = map[string]any{"kind": "collection.trash.set", "collectionId": id, "trashed": entry.Tombstone != ""}
	return entry, nil
}
func validateCollectionTrash(ctx context.Context, q querier, action map[string]any) error {
	return validateCollectionTrashSelected(ctx, q, action, nil)
}
func validateCollectionTrashSelected(ctx context.Context, q querier, action map[string]any, selected map[string]bool) error {
	id := str(action["collectionId"])
	var role sql.NullString
	if err := q.QueryRowContext(ctx, "SELECT system_role FROM collections WHERE id=?", id).Scan(&role); err != nil {
		return err
	}
	if action["trashed"] != true {
		return nil
	}
	if role.String != "" && role.String != "none" {
		return Fail("CONFLICT", "System collections require a separate migration before removal")
	}
	live, err := q.QueryContext(ctx, "SELECT p.id FROM records r JOIN pages p ON p.id=r.page_id WHERE r.collection_id=? AND p.deleted_at IS NULL", id)
	if err != nil {
		return err
	}
	for live.Next() {
		var page string
		if err = live.Scan(&page); err != nil {
			live.Close()
			return err
		}
		if !selected[page] {
			live.Close()
			return Fail("CONFLICT", "Review and trash every affected record before hiding its collection")
		}
	}
	if err = live.Err(); err != nil {
		live.Close()
		return err
	}
	live.Close()
	// Retain external references and require a separate dependency migration;
	// removing their target cannot silently change relation/formula semantics.
	rows, err := q.QueryContext(ctx, "SELECT id FROM properties WHERE collection_id=?", id)
	if err != nil {
		return err
	}
	owned := map[string]bool{}
	for rows.Next() {
		var property string
		if err = rows.Scan(&property); err != nil {
			rows.Close()
			return err
		}
		owned[property] = true
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()
	rows, err = q.QueryContext(ctx, "SELECT type,config_json FROM properties WHERE collection_id<>?", id)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var typ, raw string
		if err = rows.Scan(&typ, &raw); err != nil {
			return err
		}
		var config map[string]any
		if json.Unmarshal([]byte(raw), &config) != nil {
			return Fail("INTEGRITY_ERROR", "Stored property configuration is invalid")
		}
		if schemaReference(config, id) {
			return Fail("CONFLICT", "Another collection still references this collection; review a dependency migration first")
		}
		if typ == "formula" {
			body := asObject(config["raw"])
			expression := str(body["translatedExpression"])
			if expression == "" {
				continue
			}
			var dependencies []string
			if number(body["evaluatorVersion"]) == 2 {
				_, dependencies, err = formulaCompileV2(expression)
			} else {
				dependencies, err = formulaDependencies(expression)
			}
			if err != nil {
				return Fail("CONFLICT", "An external formula dependency cannot be verified")
			}
			for _, property := range dependencies {
				if owned[property] {
					return Fail("CONFLICT", "An external formula still references this collection")
				}
			}
		}
		for property := range owned {
			if schemaReference(config, property) {
				return Fail("CONFLICT", "An external property still references this collection's schema")
			}
		}
	}
	return rows.Err()
}
func setCollectionTrash(m *Mutation, action map[string]any) error {
	if err := validateCollectionTrash(m.Context, m.Tx, action); err != nil {
		return err
	}
	id := str(action["collectionId"])
	entry, err := collectionTrashCurrent(m.Context, m.Tx, action)
	if err != nil {
		return err
	}
	if err = m.TouchPage(entry.PageID); err != nil {
		return err
	}
	if err = m.Touch("collection", id); err != nil {
		return err
	}
	if m.collectionVisibilityChanges == nil {
		m.collectionVisibilityChanges = map[string]bool{}
	}
	m.collectionVisibilityChanges[id] = true
	if action["trashed"] == true {
		_, err = m.Exec("INSERT OR IGNORE INTO collection_visibility VALUES(?,1,?)", id, now())
	} else {
		_, err = m.Exec("DELETE FROM collection_visibility WHERE collection_id=?", id)
	}
	return err
}
func captureRecoveryCollectionVisibility(ctx context.Context, q querier, page string) ([]recoveryCollectionVisibility, error) {
	rows, err := q.QueryContext(ctx, `SELECT c.id,v.deleted_at FROM collections c LEFT JOIN collection_visibility v ON v.collection_id=c.id WHERE c.owning_page_id=? ORDER BY c.id`, page)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []recoveryCollectionVisibility{}
	for rows.Next() {
		var entry recoveryCollectionVisibility
		var stamp sql.NullString
		if err = rows.Scan(&entry.CollectionID, &stamp); err != nil {
			return nil, err
		}
		if stamp.Valid {
			entry.DeletedAt = &stamp.String
		}
		out = append(out, entry)
	}
	return out, rows.Err()
}
func restoreRecoveryCollectionVisibility(m *Mutation, snap recoverySnapshot) error {
	if snap.CollectionVisibility == nil {
		return nil
	}
	for _, entry := range *snap.CollectionVisibility {
		var owner string
		if err := m.Tx.QueryRowContext(m.Context, "SELECT owning_page_id FROM collections WHERE id=?", entry.CollectionID).Scan(&owner); err != nil {
			return err
		}
		if owner != string(snap.Page.Id) {
			return Fail("CONFLICT", "Historical collection visibility belongs to another owner")
		}
		if m.collectionVisibilityChanges == nil {
			m.collectionVisibilityChanges = map[string]bool{}
		}
		m.collectionVisibilityChanges[entry.CollectionID] = true
		if err := m.Touch("collection", entry.CollectionID); err != nil {
			return err
		}
		if entry.DeletedAt != nil {
			if contracts.Validate("domain", "Timestamp", marshal(*entry.DeletedAt)) != nil {
				return Fail("INTEGRITY_ERROR", "Invalid historical collection visibility")
			}
			if err := validateCollectionTrash(m.Context, m.Tx, map[string]any{"collectionId": entry.CollectionID, "trashed": true}); err != nil {
				return err
			}
			if _, err := m.Exec("INSERT INTO collection_visibility VALUES(?,1,?) ON CONFLICT(collection_id) DO UPDATE SET deleted_at=excluded.deleted_at", entry.CollectionID, now()); err != nil {
				return err
			}
		} else {
			if _, err := m.Exec("DELETE FROM collection_visibility WHERE collection_id=?", entry.CollectionID); err != nil {
				return err
			}
		}
	}
	return nil
}
