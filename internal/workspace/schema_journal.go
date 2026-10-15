package workspace

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	"trove.local/phase0/contracts"
)

// Schema inverses are deliberately separate from page restoration: changing a
// shared property must not rewind unrelated record values or page content.
func init() { RegisterCommand("collection.property.restore", restoreUnusedProperty) }
func schemaJournalCurrent(ctx context.Context, q querier, action map[string]any) (journalEntry, error) {
	entry := journalEntry{Target: "schema:" + str(action["propertyId"])}
	collection, id := str(action["collectionId"]), str(action["propertyId"])
	if err := q.QueryRowContext(ctx, "SELECT owning_page_id FROM collections WHERE id=?", collection).Scan(&entry.PageID); err != nil {
		return entry, err
	}
	definition, err := propertyDefinition(ctx, q, id)
	entry.PropertyRevision = "0"
	var value any
	if err == nil {
		if string(definition.CollectionId) != collection {
			return entry, Fail("CONFLICT", "Property identity belongs to another collection")
		}
		entry.PropertyRevision = string(definition.Revision)
		if err = json.Unmarshal(importPropertyField(definition), &value); err != nil {
			return entry, err
		}
	} else if !errors.Is(err, sql.ErrNoRows) {
		return entry, err
	}
	var count int
	if err = q.QueryRowContext(ctx, "SELECT count(*) FROM property_values WHERE property_id=?", id).Scan(&count); err != nil {
		return entry, err
	}
	if count != 0 {
		return entry, Fail("CONFLICT", "This schema change requires an explicit populated-value migration")
	}
	entry.Before = map[string]any{"kind": "collection.property.restore", "collectionId": collection, "propertyId": id, "definition": value}
	return entry, nil
}
func schemaReference(value any, id string) bool {
	switch v := value.(type) {
	case string:
		return v == id
	case []any:
		for _, x := range v {
			if schemaReference(x, id) {
				return true
			}
		}
	case map[string]any:
		for key, x := range v {
			if key == id || schemaReference(x, id) {
				return true
			}
		}
	}
	return false
}
func unusedPropertyDependencies(ctx context.Context, q querier, collection, id string) error {
	var count int
	if err := q.QueryRowContext(ctx, "SELECT count(*) FROM task_facets WHERE status_property_id=? OR due_property_id=?", id, id).Scan(&count); err != nil {
		return err
	}
	if count > 0 {
		return Fail("CONFLICT", "A task binding still depends on this property")
	}
	// These are canonical definitions, not historical evidence. An exact stored
	// ID reference is conservatively retained until its own migration is reviewed.
	sources := [][2]string{{"properties", "SELECT config_json FROM properties WHERE id<>?"}, {"views", "SELECT query_json FROM views UNION ALL SELECT config_json FROM views"}, {"record_layouts", "SELECT layout_json FROM record_layouts"}, {"p2_database_config", "SELECT config_json FROM p2_database_config"}, {"automation_rules", "SELECT rule_json FROM automation_rules"}}
	for _, source := range sources {
		if err := q.QueryRowContext(ctx, "SELECT count(*) FROM sqlite_master WHERE type='table' AND name=?", source[0]).Scan(&count); err != nil {
			return err
		}
		if count == 0 {
			continue
		}
		args := []any{}
		if source[0] == "properties" {
			args = append(args, id)
		}
		rows, err := q.QueryContext(ctx, source[1], args...)
		if err != nil {
			return err
		}
		for rows.Next() {
			var raw string
			if err = rows.Scan(&raw); err != nil {
				rows.Close()
				return err
			}
			var config any
			if json.Unmarshal([]byte(raw), &config) != nil {
				rows.Close()
				return Fail("INTEGRITY_ERROR", "Stored schema dependency is invalid")
			}
			if schemaReference(config, id) {
				rows.Close()
				return Fail("CONFLICT", "A saved property, view, layout, form or automation still depends on this property")
			}
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return err
		}
	}
	var raw string
	err := q.QueryRowContext(ctx, "SELECT config_json FROM p2_database_config WHERE collection_id=?", collection).Scan(&raw)
	if err == nil {
		var cfg P2DatabaseConfig
		if json.Unmarshal([]byte(raw), &cfg) != nil {
			return Fail("INTEGRITY_ERROR", "Stored form configuration is invalid")
		}
		if cfg.Form.Enabled {
			return Fail("CONFLICT", "Disable the local form before changing its shared schema")
		}
	} else if !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	return nil
}
func validateUnusedPropertyRestore(ctx context.Context, q querier, action map[string]any) error {
	entry, err := schemaJournalCurrent(ctx, q, action)
	if err != nil {
		return err
	}
	_ = entry
	collection, id := str(action["collectionId"]), str(action["propertyId"])
	if err = unusedPropertyDependencies(ctx, q, collection, id); err != nil {
		return err
	}
	current, err := propertyDefinition(ctx, q, id)
	if err == nil && (!p2FormType(current.Type) || current.SchemaVersion != 1 || contracts.Validate("domain", "property_"+current.Type+"_config_v1", current.Config) != nil) {
		return Fail("CONFLICT", "Title, relation, files, derived and unsupported historical properties require a separate schema migration")
	}
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if action["definition"] == nil {
		return nil
	}
	var def struct {
		Name          string          `json:"name"`
		Type          string          `json:"type"`
		SchemaVersion int64           `json:"schemaVersion"`
		Config        json.RawMessage `json:"config"`
	}
	if json.Unmarshal(marshal(action["definition"]), &def) != nil || def.SchemaVersion != 1 || !p2FormType(def.Type) {
		return Fail("INVALID_REQUEST", "Unsupported reviewed scalar property definition")
	}
	if contracts.Validate("domain", "property_"+def.Type+"_config_v1", def.Config) != nil {
		return Fail("INVALID_REQUEST", "Invalid reviewed property configuration")
	}
	return nil
}
func restoreUnusedProperty(m *Mutation, action map[string]any) error {
	if err := validateUnusedPropertyRestore(m.Context, m.Tx, action); err != nil {
		return err
	}
	collection, id := str(action["collectionId"]), str(action["propertyId"])
	var page string
	if err := m.Tx.QueryRowContext(m.Context, "SELECT owning_page_id FROM collections WHERE id=?", collection).Scan(&page); err != nil {
		return err
	}
	if err := m.TouchPage(page); err != nil {
		return err
	}
	if err := m.Touch("collection", collection); err != nil {
		return err
	}
	if err := m.Touch("property", id); err != nil {
		return err
	}
	if _, err := m.Exec("DELETE FROM property_options WHERE property_id=?", id); err != nil {
		return err
	}
	if action["definition"] == nil {
		if err := m.Deleted("property", id); err != nil {
			return err
		}
		_, err := m.Exec("DELETE FROM properties WHERE id=? AND collection_id=?", id, collection)
		return err
	}
	def := asObject(action["definition"])
	if _, err := m.Exec("INSERT INTO properties(id,collection_id,name,type,schema_version,config_json,revision) VALUES(?,?,?,?,?,?,1) ON CONFLICT(id) DO UPDATE SET name=excluded.name,type=excluded.type,schema_version=excluded.schema_version,config_json=excluded.config_json", id, collection, def["name"], def["type"], int64(number(def["schemaVersion"])), string(marshal(def["config"]))); err != nil {
		return err
	}
	if m.GuardRevision("property", id) == "0" {
		var previous string
		err := m.Tx.QueryRowContext(m.Context, "SELECT revision FROM entity_tombstones WHERE entity_type='property' AND entity_id=?", id).Scan(&previous)
		if err == nil {
			next, e := revNext(previous)
			if e != nil {
				return e
			}
			change := m.touched[guardKey("property", id)]
			change.After = contracts.DomainRevision(next)
			m.touched[guardKey("property", id)] = change
		} else if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
	}
	if _, err := m.Exec("DELETE FROM entity_tombstones WHERE entity_type='property' AND entity_id=?", id); err != nil {
		return err
	}
	cfg := asObject(def["config"])
	groups := map[string]string{}
	for _, raw := range asList(cfg["options"]) {
		option := asObject(raw)
		oid := str(option["id"])
		if _, exists := groups[oid]; exists {
			return Fail("INVALID_REQUEST", "Duplicate reviewed option identity")
		}
		groups[oid] = str(option["group"])
		if _, err := m.Exec("INSERT INTO property_options VALUES(?,?,?,?)", oid, id, option["label"], nullable(option["group"])); err != nil {
			return err
		}
	}
	for _, key := range []string{"defaultOptionId", "doneOptionId", "reopenOptionId"} {
		if oid := str(cfg[key]); oid != "" {
			group, ok := groups[oid]
			if !ok || key == "doneOptionId" && group != "done" || key == "reopenOptionId" && group == "done" {
				return Fail("INVALID_REQUEST", "Reviewed option mapping is incompatible")
			}
		}
	}
	return nil
}
