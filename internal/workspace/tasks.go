package workspace

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"trove.local/phase0/contracts"
)

func init() {
	for _, kind := range []string{"tasks.ensure", "task.create", "task.promote", "task.setStatus"} {
		RegisterCommand(kind, taskCommand)
	}
	RegisterSnapshotReader("taskFacets", taskSnapshot)
}
func taskCommand(m *Mutation, c map[string]any) error {
	switch str(c["kind"]) {
	case "tasks.ensure":
		// Bootstrap itself is a normal guarded batch. Racing callers observe the one
		// canonical collection; a later ensure never changes the schema or IDs.
		if _, ok := m.Execution.Scope.Value.(contracts.DomainScopeVariant1); !ok {
			return Fail("SCOPE_DENIED", "Task bootstrap requires workspace scope")
		}
		var exists string
		err := m.Tx.QueryRowContext(m.Context, "SELECT id FROM collections WHERE system_role='tasks'").Scan(&exists)
		if err == nil {
			return nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		page, collection, title, status, due := str(c["pageId"]), str(c["collectionId"]), str(c["titlePropertyId"]), str(c["statusPropertyId"]), str(c["duePropertyId"])
		if err = pageCommand(m, map[string]any{"kind": "page.create", "pageId": page, "title": "Tasks"}); err != nil {
			return err
		}
		if err = databaseCommand(m, map[string]any{"kind": "collection.create", "collectionId": collection, "owningPageId": page, "titlePropertyId": title, "title": "Tasks"}); err != nil {
			return err
		}
		if _, err = m.Exec("UPDATE collections SET system_role='tasks' WHERE id=?", collection); err != nil {
			return err
		}
		open, done := str(c["defaultOptionId"]), str(c["doneOptionId"])
		config := map[string]any{"options": []any{map[string]any{"id": open, "label": "To do", "group": "not_started"}, map[string]any{"id": done, "label": "Done", "group": "done"}}, "defaultOptionId": open, "doneOptionId": done, "reopenOptionId": open}
		if err = addProperty(m, map[string]any{"collectionId": collection, "propertyId": status, "name": "Status", "type": "status", "schemaVersion": float64(1), "config": config}); err != nil {
			return err
		}
		return addProperty(m, map[string]any{"collectionId": collection, "propertyId": due, "name": "Due", "type": "date", "schemaVersion": float64(1), "config": map[string]any{}})
	case "task.create":
		collection, status := str(c["collectionId"]), str(c["statusPropertyId"])
		if collection == "" {
			if err := m.Tx.QueryRowContext(m.Context, "SELECT c.id,p.id FROM collections c JOIN properties p ON p.collection_id=c.id AND p.type='status' WHERE c.system_role='tasks' ORDER BY p.id LIMIT 1").Scan(&collection, &status); err != nil {
				if errors.Is(err, sql.ErrNoRows) {
					return Fail("INVALID_REQUEST", "Create the system Tasks collection with tasks.ensure first")
				}
				return err
			}
		}
		if err := createRecord(m, map[string]any{"recordId": c["recordId"], "pageId": c["pageId"], "collectionId": collection, "title": c["title"]}); err != nil {
			return err
		}
		return promoteTask(m, str(c["recordId"]), status, "")
	case "task.promote":
		return promoteTask(m, str(c["recordId"]), str(c["statusPropertyId"]), str(c["duePropertyId"]))
	case "task.setStatus":
		var property string
		if err := m.Tx.QueryRowContext(m.Context, "SELECT status_property_id FROM task_facets WHERE record_id=?", c["recordId"]).Scan(&property); err != nil {
			return err
		}
		return setRecordValue(m, str(c["recordId"]), property, map[string]any{"type": "status", "schemaVersion": float64(1), "value": c["statusOptionId"]})
	}
	return Fail("UNSUPPORTED_SCHEMA", "Unsupported task command")
}
func promoteTask(m *Mutation, record, status, dueProperty string) error {
	var collection, page string
	if err := m.Tx.QueryRowContext(m.Context, "SELECT collection_id,page_id FROM records WHERE id=?", record).Scan(&collection, &page); err != nil {
		return err
	}
	if err := requireActiveCollection(m.Context, m.Tx, collection); err != nil {
		return err
	}
	if err := m.TouchPage(page); err != nil {
		return err
	}
	if err := m.Touch("collection", collection); err != nil {
		return err
	}
	if err := m.Require("property", status); err != nil {
		return err
	}
	p, err := propertyDefinition(m.Context, m.Tx, status)
	if err != nil {
		return err
	}
	if p.Type != "status" || string(p.CollectionId) != collection {
		return Fail("INVALID_REQUEST", "Task status must belong to this record's collection")
	}
	var config map[string]any
	json.Unmarshal(p.Config, &config)
	for _, key := range []string{"defaultOptionId", "doneOptionId", "reopenOptionId"} {
		if str(config[key]) == "" {
			return Fail("INVALID_REQUEST", "Task status requires explicit default, done, and reopen mappings")
		}
	}
	if dueProperty != "" {
		if err := m.Require("property", dueProperty); err != nil {
			return err
		}
		due, err := propertyDefinition(m.Context, m.Tx, dueProperty)
		if err != nil {
			return err
		}
		if due.Type != "date" || string(due.CollectionId) != collection {
			return Fail("INVALID_REQUEST", "Task due date must be a date property in this record's collection")
		}
	}
	var bound string
	err = m.Tx.QueryRowContext(m.Context, "SELECT status_property_id FROM task_facets WHERE record_id=?", record).Scan(&bound)
	if err == nil {
		if bound != status {
			return Fail("INVALID_REQUEST", "Task status is already bound")
		}
		if dueProperty != "" {
			_, err = m.Exec("UPDATE task_facets SET due_property_id=? WHERE record_id=?", dueProperty, record)
			return err
		}
		return nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	var value string
	err = m.Tx.QueryRowContext(m.Context, "SELECT value_json FROM property_values WHERE record_id=? AND property_id=?", record, status).Scan(&value)
	if errors.Is(err, sql.ErrNoRows) {
		if err = setRecordValue(m, record, status, map[string]any{"type": "status", "schemaVersion": float64(1), "value": config["defaultOptionId"]}); err != nil {
			return err
		}
	} else if err != nil {
		return err
	}
	var due sql.NullString
	if dueProperty != "" {
		due = sql.NullString{String: dueProperty, Valid: true}
	}
	if dueProperty == "" {
		if err = m.Tx.QueryRowContext(m.Context, "SELECT p.id FROM properties p JOIN collections c ON c.id=p.collection_id WHERE p.collection_id=? AND p.type='date' AND c.system_role='tasks' ORDER BY p.id LIMIT 1", collection).Scan(&due); err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
	}
	_, err = m.Exec("INSERT INTO task_facets(record_id,collection_id,status_property_id,due_property_id) VALUES(?,?,?,?)", record, collection, status, due)
	return err
}
func taskSnapshot(ctx context.Context, k *Kernel, tx *sql.Tx, e contracts.ServiceExecutionContext) (any, error) {
	rows, err := tx.QueryContext(ctx, "SELECT t.record_id,t.status_property_id,t.due_property_id FROM task_facets t JOIN records r ON r.id=t.record_id JOIN pages p ON p.id=r.page_id WHERE p.deleted_at IS NULL AND NOT EXISTS(SELECT 1 FROM collection_visibility v WHERE v.collection_id=r.collection_id) ORDER BY t.record_id")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []contracts.DomainTaskFacet{}
	for rows.Next() {
		var facet contracts.DomainTaskFacet
		var due sql.NullString
		if err = rows.Scan(&facet.RecordId, &facet.StatusPropertyId, &due); err != nil {
			return nil, err
		}
		if due.Valid {
			id := contracts.DomainId(due.String)
			facet.DuePropertyId = &id
		}
		if k.authorizeEntity(ctx, tx, e, "record", string(facet.RecordId)) == nil {
			out = append(out, facet)
		}
	}
	return out, rows.Err()
}
