package workspace

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"trove.local/phase0/contracts"
)

// Schema inverses are deliberately separate from page restoration: changing a
// shared property must not rewind unrelated record values or page content.
func init() { RegisterCommand("collection.property.restore", restoreReviewedProperty) }
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
	values := []any{}
	rows, err := q.QueryContext(ctx, "SELECT record_id,value_json FROM property_values WHERE property_id=? ORDER BY record_id LIMIT 129", id)
	if err != nil {
		return entry, err
	}
	for rows.Next() {
		var record, encoded string
		if err = rows.Scan(&record, &encoded); err != nil {
			rows.Close()
			return entry, err
		}
		var v any
		if err = json.Unmarshal([]byte(encoded), &v); err != nil {
			rows.Close()
			return entry, err
		}
		values = append(values, map[string]any{"recordId": record, "value": v})
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return entry, err
	}
	if len(values) > 128 {
		return entry, Fail("RESOURCE_EXHAUSTED", "A reviewed property migration supports at most 128 populated rows")
	}
	entry.Before = map[string]any{"kind": "collection.property.restore", "collectionId": collection, "propertyId": id, "definition": value}
	// Omit the empty inventory for compatibility with pre-migration journals.
	// An explicit null value is retained as a row entry, never as absence.
	if len(values) > 0 {
		entry.Before["values"] = values
		entry.Before["valueInventoryVersion"] = 1
	}
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
	// Formula dependencies may exist only in executable expression text. Exact
	// JSON string matching cannot see prop("stable-id") inside an expression.
	rows, err := q.QueryContext(ctx, "SELECT config_json FROM properties WHERE type='formula' AND id<>?", id)
	if err != nil {
		return err
	}
	for rows.Next() {
		var encoded string
		if err = rows.Scan(&encoded); err != nil {
			rows.Close()
			return err
		}
		var config map[string]any
		if json.Unmarshal([]byte(encoded), &config) != nil {
			rows.Close()
			return Fail("INTEGRITY_ERROR", "Stored formula configuration is invalid")
		}
		raw := asObject(config["raw"])
		expression := str(raw["translatedExpression"])
		if expression == "" {
			continue
		}
		var deps []string
		if number(raw["evaluatorVersion"]) == 2 {
			_, deps, err = formulaCompileV2(expression)
		} else {
			deps, err = formulaDependencies(expression)
		}
		if err != nil {
			rows.Close()
			return Fail("CONFLICT", "A stored formula has unresolved dependencies; repair it before changing schema")
		}
		for _, dep := range deps {
			if dep == id {
				rows.Close()
				return Fail("CONFLICT", "A formula still depends on this property")
			}
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
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
	err = q.QueryRowContext(ctx, "SELECT config_json FROM p2_database_config WHERE collection_id=?", collection).Scan(&raw)
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
func validateReviewedPropertyRestore(ctx context.Context, q querier, action map[string]any) error {
	entry, err := schemaJournalCurrent(ctx, q, action)
	if err != nil {
		return err
	}
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
	values := asList(action["values"])
	oldValues := asList(entry.Before["values"])
	if len(oldValues) > 0 {
		if string(marshal(action["valueInventoryVersion"])) != "1" {
			return Fail("CONFLICT", "A populated schema change requires an explicit complete reviewed value inventory")
		}
		if !schemaMigrationScalar(current.Type) {
			return Fail("CONFLICT", "Populated option, phone and derived properties require a separate value migration")
		}
	}
	if action["definition"] == nil {
		if len(values) > 0 {
			return Fail("CONFLICT", "A deleted property cannot retain reviewed row values; resolve each value before deleting the schema")
		}
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
	if len(values) > 128 {
		return Fail("RESOURCE_EXHAUSTED", "A reviewed property migration supports at most 128 populated rows")
	}
	if len(values) > 0 && !schemaMigrationScalar(def.Type) {
		return Fail("CONFLICT", "Populated schema migration supports text, number, checkbox, date, URL and email properties")
	}
	failures := []string{}
	seen := map[string]bool{}
	for _, raw := range values {
		value := asObject(raw)
		record := str(value["recordId"])
		if seen[record] {
			return Fail("INVALID_REQUEST", "Duplicate reviewed record identity")
		}
		seen[record] = true
		if _, present := value["value"]; !present {
			return Fail("INVALID_REQUEST", "A reviewed value must distinguish null from an absent row")
		}
		if err := validateSchemaScalar(def.Type, value["value"]); err != nil {
			failures = append(failures, fmt.Sprintf("record %s: %s", record, err))
		}
	}
	if len(failures) > 0 {
		return Fail("CONFLICT", "No schema changes applied; incompatible reviewed values: "+strings.Join(failures, "; "))
	}
	return nil
}
func schemaMigrationScalar(kind string) bool {
	switch kind {
	case "text", "number", "checkbox", "date", "url", "email":
		return true
	}
	return false
}
func validateSchemaScalar(kind string, value any) error {
	// These scalar validators do not access a transaction. The common validator
	// also enforces calendar validity and accepted URL/email semantics.
	return validatePropertyValue(&Mutation{}, contracts.DomainPropertyDefinition{Type: kind}, value)
}
func schemaValueRecords(actions ...map[string]any) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, action := range actions {
		for _, raw := range asList(action["values"]) {
			id := str(asObject(raw)["recordId"])
			if !seen[id] {
				seen[id] = true
				out = append(out, id)
			}
		}
	}
	return out
}
func restoreReviewedProperty(m *Mutation, action map[string]any) error {
	if err := validateReviewedPropertyRestore(m.Context, m.Tx, action); err != nil {
		return err
	}
	collection, id := str(action["collectionId"]), str(action["propertyId"])
	before, err := schemaJournalCurrent(m.Context, m.Tx, action)
	if err != nil {
		return err
	}
	// Capture and guard every affected record before replacing any value, so the
	// preimage includes the old schema and all rows fail atomically on stale scope.
	for _, record := range schemaValueRecords(before.Before, action) {
		var page, owner string
		var deleted sql.NullString
		if err = m.Tx.QueryRowContext(m.Context, "SELECT r.page_id,r.collection_id,p.deleted_at FROM records r JOIN pages p ON p.id=r.page_id WHERE r.id=?", record).Scan(&page, &owner, &deleted); err != nil {
			return err
		}
		if owner != collection || deleted.Valid {
			return Fail("CONFLICT", "An affected record is missing, trashed or outside the reviewed collection")
		}
		if err = m.TouchPage(page); err != nil {
			return err
		}
	}
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
	// Remove values before changing the composite schema foreign key. They are
	// restored with the reviewed target type within this same writer transaction.
	if _, err := m.Exec("DELETE FROM property_values WHERE property_id=?", id); err != nil {
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
	for _, raw := range asList(action["values"]) {
		value := asObject(raw)
		if _, err := m.Exec("INSERT INTO property_values(record_id,collection_id,property_id,property_type,schema_version,value_json) VALUES(?,?,?,?,?,?)", value["recordId"], collection, id, def["type"], 1, string(marshal(value["value"]))); err != nil {
			return err
		}
	}
	return nil
}
