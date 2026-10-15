package workspace

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"trove.local/phase0/contracts"
)

func taskJournalPermitted(m *Mutation) error {
	if m.Execution.Actor.Kind != "user" || m.Execution.Origin != "user" || len(m.Request.Commands) != 1 {
		return Fail("SCOPE_DENIED", "Task detachment and undo require direct user review")
	}
	var outer map[string]any
	if err := json.Unmarshal(marshal(m.Request.Commands[0]), &outer); err != nil {
		return err
	}
	if str(outer["kind"]) != "task.detach.apply" && str(outer["kind"]) != "operation.undo" {
		return Fail("APPROVAL_REQUIRED", "Use the complete task detach preview or its guarded undo")
	}
	return nil
}
func taskOccurrenceState(ctx context.Context, q querier, id, page string) (map[string]any, error) {
	var actual, parent, sortKey, typ, raw string
	var version int
	err := q.QueryRowContext(ctx, "SELECT page_id,COALESCE(parent_block_id,''),sort_key,type,schema_version,data_json FROM blocks WHERE id=?", id).Scan(&actual, &parent, &sortKey, &typ, &version, &raw)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if actual != page {
		return nil, Fail("CONFLICT", "Affected occurrence moved to another page")
	}
	var data any
	if err = json.Unmarshal([]byte(raw), &data); err != nil {
		return nil, err
	}
	var parentValue any
	if parent != "" {
		parentValue = parent
	}
	return map[string]any{"parentBlockId": parentValue, "sortKey": sortKey, "type": typ, "schemaVersion": version, "data": data}, nil
}
func taskJournalCurrent(ctx context.Context, q querier, action map[string]any) (journalEntry, error) {
	entry := journalEntry{}
	if str(action["kind"]) == "task.occurrence.set" {
		id, page := str(action["blockId"]), str(action["pageId"])
		state, err := taskOccurrenceState(ctx, q, id, page)
		if err != nil {
			return entry, err
		}
		entry.PageID, entry.Target = page, "task-occurrence:"+id
		var value any
		if state != nil {
			value = state
		} else {
			err = q.QueryRowContext(ctx, "SELECT revision FROM entity_tombstones WHERE entity_type='block' AND entity_id=?", id).Scan(&entry.Tombstone)
			if err != nil && !errors.Is(err, sql.ErrNoRows) {
				return entry, err
			}
		}
		entry.Before = map[string]any{"kind": "task.occurrence.set", "blockId": id, "pageId": page, "state": value}
		return entry, nil
	}
	record := str(action["recordId"])
	binding := taskBinding{}
	if err := json.Unmarshal(marshal(action["binding"]), &binding); err != nil {
		return entry, err
	}
	var status string
	var due sql.NullString
	err := q.QueryRowContext(ctx, "SELECT status_property_id,due_property_id FROM task_facets WHERE record_id=?", record).Scan(&status, &due)
	attached := err == nil
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return entry, err
	}
	if attached {
		binding.StatusPropertyID = status
		binding.DuePropertyID = nil
		if due.Valid {
			binding.DuePropertyID = &due.String
		}
	}
	if err = q.QueryRowContext(ctx, "SELECT page_id FROM records WHERE id=?", record).Scan(&entry.PageID); err != nil {
		return entry, err
	}
	revisions := map[string]string{}
	ids := []string{binding.StatusPropertyID}
	if binding.DuePropertyID != nil {
		ids = append(ids, *binding.DuePropertyID)
	}
	for _, id := range ids {
		var rev string
		if err = q.QueryRowContext(ctx, "SELECT revision FROM properties WHERE id=?", id).Scan(&rev); err != nil {
			return entry, err
		}
		revisions[id] = rev
	}
	entry.PropertyRevision = snapshotDigest(string(marshal(revisions)))
	entry.Target = "task-facet:" + record
	entry.Before = map[string]any{"kind": "task.facet.set", "recordId": record, "binding": map[string]any{"statusPropertyId": binding.StatusPropertyID, "duePropertyId": binding.DuePropertyID}, "attached": attached}
	return entry, nil
}
func validateTaskBinding(ctx context.Context, q querier, record string, binding taskBinding) error {
	var collection string
	if err := q.QueryRowContext(ctx, "SELECT collection_id FROM records WHERE id=?", record).Scan(&collection); err != nil {
		return err
	}
	status, err := propertyDefinition(ctx, q, binding.StatusPropertyID)
	if err != nil {
		return err
	}
	if status.Type != "status" || string(status.CollectionId) != collection {
		return Fail("CONFLICT", "Task status binding is no longer compatible")
	}
	var config map[string]any
	if err = json.Unmarshal(status.Config, &config); err != nil {
		return err
	}
	for _, key := range []string{"defaultOptionId", "doneOptionId", "reopenOptionId"} {
		if str(config[key]) == "" {
			return Fail("CONFLICT", "Task status mapping is no longer compatible")
		}
	}
	var present int
	if err = q.QueryRowContext(ctx, "SELECT count(*) FROM property_values WHERE record_id=? AND property_id=? AND property_type='status'", record, binding.StatusPropertyID).Scan(&present); err != nil {
		return err
	}
	if present != 1 {
		return Fail("CONFLICT", "Restore a valid status value before undoing task detachment")
	}
	if binding.DuePropertyID != nil {
		p, err := propertyDefinition(ctx, q, *binding.DuePropertyID)
		if err != nil {
			return err
		}
		if p.Type != "date" || string(p.CollectionId) != collection {
			return Fail("CONFLICT", "Task due binding is no longer compatible")
		}
	}
	return nil
}
func setTaskFacetJournal(m *Mutation, c map[string]any) error {
	if err := taskJournalPermitted(m); err != nil {
		return err
	}
	record := str(c["recordId"])
	var binding taskBinding
	if err := json.Unmarshal(marshal(c["binding"]), &binding); err != nil {
		return err
	}
	var page, collection string
	if err := m.Tx.QueryRowContext(m.Context, "SELECT page_id,collection_id FROM records WHERE id=?", record).Scan(&page, &collection); err != nil {
		return err
	}
	if err := m.TouchPage(page); err != nil {
		return err
	}
	if err := m.Touch("collection", collection); err != nil {
		return err
	}
	if err := m.Require("property", binding.StatusPropertyID); err != nil {
		return err
	}
	if binding.DuePropertyID != nil {
		if err := m.Require("property", *binding.DuePropertyID); err != nil {
			return err
		}
	}
	if c["attached"] == true {
		if err := validateTaskBinding(m.Context, m.Tx, record, binding); err != nil {
			return err
		}
		_, err := m.Exec("INSERT INTO task_facets(record_id,collection_id,status_property_id,due_property_id) VALUES(?,?,?,?)", record, collection, binding.StatusPropertyID, binding.DuePropertyID)
		return err
	}
	var count int
	if err := m.Tx.QueryRowContext(m.Context, "SELECT count(*) FROM todo_references WHERE task_record_id=?", record).Scan(&count); err != nil {
		return err
	}
	if count != 0 {
		return Fail("CONFLICT", "Every retained todo occurrence must be explicitly converted or removed")
	}
	_, err := m.Exec("DELETE FROM task_facets WHERE record_id=?", record)
	return err
}
func setTaskOccurrenceJournal(m *Mutation, c map[string]any) error {
	if err := taskJournalPermitted(m); err != nil {
		return err
	}
	id, page := str(c["blockId"]), str(c["pageId"])
	current, err := taskOccurrenceState(m.Context, m.Tx, id, page)
	if err != nil {
		return err
	}
	state, _ := c["state"].(map[string]any)
	if state == nil {
		if current == nil {
			return Fail("CONFLICT", "Occurrence is already absent")
		}
		var related int
		if err = m.Tx.QueryRowContext(m.Context, "SELECT (SELECT count(*) FROM blocks WHERE parent_block_id=?)+(SELECT count(*) FROM attachments WHERE block_id=?)+(SELECT count(*) FROM transcript_segments WHERE block_id=?)", id, id, id).Scan(&related); err != nil {
			return err
		}
		if related != 0 {
			return Fail("CONFLICT", "Convert this occurrence to preserve its nested or attached content")
		}
		return pageCommand(m, map[string]any{"kind": "block.delete", "blockId": id, "includeDescendants": false})
	}
	if err = validateBlock(str(state["type"]), number(state["schemaVersion"]), state["data"]); err != nil {
		return err
	}
	if current != nil {
		if current["parentBlockId"] != state["parentBlockId"] || current["sortKey"] != state["sortKey"] {
			return Fail("CONFLICT", "Affected occurrence moved after review")
		}
		return pageCommand(m, map[string]any{"kind": "block.update", "blockId": id, "type": state["type"], "schemaVersion": state["schemaVersion"], "data": state["data"]})
	}
	if err = m.TouchPage(page); err != nil {
		return err
	}
	if err = m.Touch("block", id); err != nil {
		return err
	}
	if parent := str(state["parentBlockId"]); parent != "" {
		if err = m.Require("block", parent); err != nil {
			return err
		}
		var actual string
		if err = m.Tx.QueryRowContext(m.Context, "SELECT page_id FROM blocks WHERE id=?", parent).Scan(&actual); err != nil {
			return err
		}
		if actual != page {
			return Fail("CONFLICT", "Occurrence parent moved to another page")
		}
	}
	var tombstone string
	if err = m.Tx.QueryRowContext(m.Context, "SELECT revision FROM entity_tombstones WHERE entity_type='block' AND entity_id=?", id).Scan(&tombstone); err != nil {
		return err
	}
	next, err := revNext(tombstone)
	if err != nil {
		return err
	}
	change := m.touched[guardKey("block", id)]
	change.After = contracts.DomainRevision(next)
	m.touched[guardKey("block", id)] = change
	if _, err = m.Exec("INSERT INTO blocks(id,page_id,parent_block_id,sort_key,type,schema_version,data_json,revision) VALUES(?,?,?,?,?,?,?,?)", id, page, state["parentBlockId"], state["sortKey"], state["type"], number(state["schemaVersion"]), string(marshal(state["data"])), next); err != nil {
		return err
	}
	if _, err = m.Exec("DELETE FROM entity_tombstones WHERE entity_type='block' AND entity_id=?", id); err != nil {
		return err
	}
	return updateLinks(m, id, page, state["data"])
}
