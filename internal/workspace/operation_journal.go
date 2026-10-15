package workspace

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"sort"

	"trove.local/phase0/contracts"
)

type journalEntry struct {
	OwnerPageID      string         `json:"ownerPageId,omitempty"`
	Tombstone        string         `json:"tombstone,omitempty"`
	ChangedFields    []string       `json:"changedFields,omitempty"`
	PageID           string         `json:"pageId"`
	Target           string         `json:"target"`
	Before           map[string]any `json:"before"`
	After            map[string]any `json:"after"`
	PropertyRevision string         `json:"propertyRevision,omitempty"`
}

func init() {
	RegisterInitializer(func(ctx context.Context, k *Kernel) error {
		return k.write(ctx, func(tx *sql.Tx) error {
			_, err := tx.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS operation_journal(id TEXT PRIMARY KEY,version INTEGER NOT NULL CHECK(version=1),label TEXT NOT NULL,actor_id TEXT NOT NULL,created_at TEXT NOT NULL,state TEXT NOT NULL CHECK(state IN('applied','undone')),entries_json TEXT NOT NULL,undone_at TEXT);`)
			return err
		})
	})
	RegisterCommand("operation.apply", func(m *Mutation, c map[string]any) error {
		if len(m.Request.Commands) != 1 {
			return Fail("INVALID_REQUEST", "A grouped operation must be an isolated batch")
		}
		actions := []map[string]any{}
		if err := json.Unmarshal(marshal(c["actions"]), &actions); err != nil {
			return err
		}
		return applyJournal(m, str(c["operationId"]), str(c["label"]), actions)
	})
	RegisterCommand("operation.undo", undoJournal)
	RegisterQuery("operations.previewUndo", previewUndoJournal)
	RegisterQuery("operations.list", listJournal)
}
func journalCurrent(ctx context.Context, q querier, action map[string]any) (journalEntry, error) {
	entry := journalEntry{}
	kind := str(action["kind"])
	switch kind {
	case "task.facet.set", "task.occurrence.set":
		return taskJournalCurrent(ctx, q, action)
	case "collection.trash.set":
		return collectionTrashCurrent(ctx, q, action)
	case "page.trash.set":
		return pageTrashCurrent(ctx, q, action)
	case "record.trash.set":
		return recordTrashCurrent(ctx, q, action)
	case "collection.property.restore":
		return schemaJournalCurrent(ctx, q, action)
	case "record.setValue", "record.clearValue":
		record, property := str(action["recordId"]), str(action["propertyId"])
		definition, err := propertyDefinition(ctx, q, property)
		if err != nil {
			return entry, err
		}
		if !p2FormType(definition.Type) {
			return entry, Fail("INVALID_REQUEST", "Grouped operations support editable scalar properties only")
		}
		if err := q.QueryRowContext(ctx, "SELECT page_id FROM records WHERE id=?", record).Scan(&entry.PageID); err != nil {
			return entry, err
		}
		if err := q.QueryRowContext(ctx, "SELECT revision FROM properties WHERE id=?", property).Scan(&entry.PropertyRevision); err != nil {
			return entry, err
		}
		entry.Target = "value:" + record + ":" + property
		var typ, raw string
		var version int
		err = q.QueryRowContext(ctx, "SELECT property_type,schema_version,value_json FROM property_values WHERE record_id=? AND property_id=?", record, property).Scan(&typ, &version, &raw)
		if errors.Is(err, sql.ErrNoRows) {
			entry.Before = map[string]any{"kind": "record.clearValue", "recordId": record, "propertyId": property}
			return entry, nil
		}
		if err != nil {
			return entry, err
		}
		var value any
		if err = json.Unmarshal([]byte(raw), &value); err != nil {
			return entry, err
		}
		entry.Before = map[string]any{"kind": "record.setValue", "recordId": record, "propertyId": property, "value": map[string]any{"type": typ, "schemaVersion": version, "value": value}}
	case "collection.layout.set", "record.layout.set":
		owner, id, key := "record", str(action["recordId"]), "recordId"
		if kind == "collection.layout.set" {
			owner, id, key = "collection", str(action["collectionId"]), "collectionId"
		}
		page, _, err := layoutOwner(ctx, q, owner, id)
		if err != nil {
			return entry, err
		}
		layout, err := readRecordLayout(ctx, q, owner, id)
		if err != nil {
			return entry, err
		}
		entry.PageID = page
		entry.Target = "layout:" + owner + ":" + id
		entry.Before = map[string]any{"kind": kind, key: id, "layout": layout}
	case "page.update":
		entry.PageID = str(action["pageId"])
		entry.Target = "title:" + entry.PageID
		var title string
		if err := q.QueryRowContext(ctx, "SELECT title FROM pages WHERE id=?", entry.PageID).Scan(&title); err != nil {
			return entry, err
		}
		entry.Before = map[string]any{"kind": "page.update", "pageId": entry.PageID, "title": title}
	case "page.display.set":
		entry.PageID = str(action["pageId"])
		entry.Target = "display:" + entry.PageID
		d, err := readPageDisplay(ctx, q, entry.PageID)
		if err != nil {
			return entry, err
		}
		entry.Before = map[string]any{"kind": "page.display.set", "pageId": entry.PageID, "fullWidth": d.FullWidth, "presentation": d.Presentation}
	default:
		return entry, Fail("INVALID_REQUEST", "Unsupported journal action")
	}
	var normalized map[string]any
	if err := json.Unmarshal(marshal(entry.Before), &normalized); err != nil {
		return entry, err
	}
	entry.Before = normalized
	return entry, nil
}
func runJournalAction(m *Mutation, action map[string]any) error {
	// Internal planners and retained inverse entries must use the same decoded
	// JSON representation as dispatcher commands (including numeric versions).
	var decoded map[string]any
	if err := json.Unmarshal(marshal(action), &decoded); err != nil {
		return err
	}
	action = decoded
	if err := contracts.Validate("service", "JournalAction", marshal(action)); err != nil {
		return Fail("INVALID_REQUEST", "Invalid grouped action")
	}
	handler := commandHandlers[str(action["kind"])]
	if handler == nil {
		return Fail("INVALID_REQUEST", "Unknown grouped action")
	}
	return handler(m, action)
}
func applyJournal(m *Mutation, id, label string, actions []map[string]any) error {
	if len(actions) < 1 || len(actions) > 128 {
		return Fail("RESOURCE_EXHAUSTED", "A group needs 1–128 changes")
	}
	var exists int
	if err := m.Tx.QueryRowContext(m.Context, "SELECT count(*) FROM operation_journal WHERE id=?", id).Scan(&exists); err != nil {
		return err
	}
	if exists != 0 {
		return Fail("CONFLICT", "Operation identity already exists")
	}
	entries := []journalEntry{}
	seen := map[string]bool{}
	schemas := map[string]bool{}
	for _, action := range actions {
		if str(action["kind"]) == "collection.property.restore" {
			schemas[str(action["propertyId"])] = true
		}
	}
	for _, action := range actions {
		if (str(action["kind"]) == "record.setValue" || str(action["kind"]) == "record.clearValue") && schemas[str(action["propertyId"])] {
			return Fail("CONFLICT", "A schema operation cannot also change that property's values without a populated-value migration")
		}
	}
	for _, action := range actions {
		if err := contracts.Validate("service", "JournalAction", marshal(action)); err != nil {
			return Fail("INVALID_REQUEST", "Invalid grouped action")
		}
		entry, err := journalCurrent(m.Context, m.Tx, action)
		if err != nil {
			return err
		}
		if seen[entry.Target] {
			return Fail("INVALID_REQUEST", "An operation cannot change the same field twice")
		}
		seen[entry.Target] = true
		if err = m.AuthorizePage(entry.PageID); err != nil {
			return err
		}
		if err = runJournalAction(m, action); err != nil {
			return err
		}
		after, err := journalCurrent(m.Context, m.Tx, action)
		if err != nil {
			return err
		}
		entry.After = after.Before
		entry.Tombstone = after.Tombstone
		if str(action["kind"]) == "task.occurrence.set" && action["state"] == nil {
			entry.Tombstone = string(m.touched[guardKey("block", str(action["blockId"]))].After)
		}
		if str(action["kind"]) == "collection.property.restore" {
			change := m.touched[guardKey("property", str(action["propertyId"]))]
			entry.PropertyRevision = string(change.After)
			if change.Deleted {
				entry.PropertyRevision = "0"
			}
		}
		if str(action["kind"]) == "page.display.set" {
			for _, key := range []string{"fullWidth", "presentation"} {
				if entry.Before[key] != entry.After[key] {
					entry.ChangedFields = append(entry.ChangedFields, key)
				}
			}
		}
		if string(marshal(entry.Before)) != string(marshal(entry.After)) {
			entries = append(entries, entry)
		}
	}
	_, err := m.Exec("INSERT INTO operation_journal VALUES(?,1,?,?,?,'applied',?,NULL)", id, label, m.Execution.Actor.ActorId, now(), string(marshal(entries)))
	return err
}
func loadJournal(ctx context.Context, k *Kernel, q querier, e contracts.ServiceExecutionContext, id string) (string, []journalEntry, error) {
	var version int
	var label, state, raw string
	err := q.QueryRowContext(ctx, "SELECT version,label,state,entries_json FROM operation_journal WHERE id=?", id).Scan(&version, &label, &state, &raw)
	if err != nil {
		return "", nil, err
	}
	if version != 1 {
		return "", nil, Fail("UNSUPPORTED_VERSION", "Unsupported operation journal")
	}
	entries := []journalEntry{}
	if err = json.Unmarshal([]byte(raw), &entries); err != nil {
		return "", nil, err
	}
	for _, entry := range entries {
		if err = k.authorizePage(ctx, q, e, entry.PageID); err != nil {
			return "", nil, err
		}
	}
	if state != "applied" {
		return "", nil, Fail("CONFLICT", "Operation was already undone")
	}
	return label, entries, nil
}
func checkJournalCurrent(ctx context.Context, q querier, entry journalEntry) error {
	current, err := journalCurrent(ctx, q, entry.After)
	if err != nil {
		return err
	}
	matches := string(marshal(current.Before)) == string(marshal(entry.After))
	if len(entry.ChangedFields) > 0 {
		matches = true
		for _, key := range entry.ChangedFields {
			if current.Before[key] != entry.After[key] {
				matches = false
			}
		}
	}
	if !matches || current.PropertyRevision != entry.PropertyRevision || current.Tombstone != entry.Tombstone {
		return Fail("CONFLICT", "An affected field or its property changed after this operation")
	}
	return nil
}
func undoJournal(m *Mutation, c map[string]any) error {
	if len(m.Request.Commands) != 1 {
		return Fail("INVALID_REQUEST", "Undo must be an isolated batch")
	}
	_, entries, err := loadJournal(m.Context, m.Kernel, m.Tx, m.Execution, str(c["operationId"]))
	if err != nil {
		return err
	}
	// Check all postconditions before any inverse. The enclosing writer transaction
	// also rolls back every inverse if a later guard or validator fails.
	for _, entry := range entries {
		if err = checkJournalCurrent(m.Context, m.Tx, entry); err != nil {
			return err
		}
	}
	for i := len(entries) - 1; i >= 0; i-- {
		inverse, inverseErr := journalInverse(m.Context, m.Tx, entries[i])
		if inverseErr != nil {
			return inverseErr
		}
		if err = runJournalAction(m, inverse); err != nil {
			return err
		}
	}
	_, err = m.Exec("UPDATE operation_journal SET state='undone',undone_at=? WHERE id=? AND state='applied'", now(), c["operationId"])
	return err
}
func previewUndoJournal(ctx context.Context, k *Kernel, e contracts.ServiceExecutionContext, r map[string]any) (any, error) {
	tx, err := k.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	id := str(r["operationId"])
	label, entries, err := loadJournal(ctx, k, tx, e, id)
	if err != nil {
		return nil, err
	}
	targets := map[string]contracts.ServiceExpectedRevision{}
	add := func(kind, id string) error {
		var revision string
		err := tx.QueryRowContext(ctx, "SELECT revision FROM "+entityTables[kind]+" WHERE id=?", id).Scan(&revision)
		if errors.Is(err, sql.ErrNoRows) && (kind == "property" || kind == "block") {
			revision = "0"
		} else if err != nil {
			return err
		}
		if revision != "0" {
			if err := k.authorizeEntity(ctx, tx, e, kind, id); err != nil {
				return err
			}
		}
		targets[kind+":"+id] = required(kind, id, revision)
		return nil
	}
	actions := []map[string]any{}
	restoredRecords := map[string]bool{}
	restoredPages := map[string]bool{}
	restoredCollections := map[string]bool{}
	for i := len(entries) - 1; i >= 0; i-- {
		entry := entries[i]
		if err = checkJournalCurrent(ctx, tx, entry); err != nil {
			return nil, err
		}
		if err = add("page", entry.PageID); err != nil {
			return nil, err
		}
		var record, collection string
		err = tx.QueryRowContext(ctx, "SELECT id,collection_id FROM records WHERE page_id=?", entry.PageID).Scan(&record, &collection)
		if err == nil {
			if err = add("record", record); err != nil {
				return nil, err
			}
			if err = add("collection", collection); err != nil {
				return nil, err
			}
		} else if !errors.Is(err, sql.ErrNoRows) {
			return nil, err
		}
		if property := str(entry.Before["propertyId"]); property != "" {
			if err = add("property", property); err != nil {
				return nil, err
			}
		}
		if collection := str(entry.Before["collectionId"]); collection != "" {
			if err = add("collection", collection); err != nil {
				return nil, err
			}
		}
		inverse, err := journalInverse(ctx, tx, entry)
		if err != nil {
			return nil, err
		}
		if str(inverse["kind"]) == "task.occurrence.set" {
			if err := add("block", str(inverse["blockId"])); err != nil {
				return nil, err
			}
			if state, ok := inverse["state"].(map[string]any); ok {
				if parent := str(state["parentBlockId"]); parent != "" {
					if err := add("block", parent); err != nil {
						return nil, err
					}
				}
			}
		}
		if str(inverse["kind"]) == "task.facet.set" {
			var binding taskBinding
			if err := json.Unmarshal(marshal(inverse["binding"]), &binding); err != nil {
				return nil, err
			}
			if err := add("property", binding.StatusPropertyID); err != nil {
				return nil, err
			}
			if binding.DuePropertyID != nil {
				if err := add("property", *binding.DuePropertyID); err != nil {
					return nil, err
				}
			}
			if inverse["attached"] == true {
				if err := validateTaskBinding(ctx, tx, str(inverse["recordId"]), binding); err != nil {
					return nil, err
				}
			}
		}
		if inverse["trashed"] == false && collection != "" && (inverse["kind"] == "page.trash.set" || inverse["kind"] == "record.trash.set") && !restoredCollections[collection] {
			var hidden int
			if err := tx.QueryRowContext(ctx, "SELECT count(*) FROM collection_visibility WHERE collection_id=?", collection).Scan(&hidden); err != nil {
				return nil, err
			}
			if hidden != 0 {
				return nil, Fail("CONFLICT", "Restore the collection before restoring this record")
			}
		}
		if str(inverse["kind"]) == "collection.trash.set" {
			if inverse["trashed"] == false {
				restoredCollections[str(inverse["collectionId"])] = true
			}
			if err := validateCollectionTrash(ctx, tx, inverse); err != nil {
				return nil, err
			}
		}
		if str(inverse["kind"]) == "page.trash.set" {
			if err := validatePageTrash(ctx, tx, inverse, restoredPages); err != nil {
				return nil, err
			}
			if inverse["trashed"] == false {
				restoredPages[entry.PageID] = true
				var parent sql.NullString
				if err := tx.QueryRowContext(ctx, "SELECT parent_page_id FROM pages WHERE id=?", entry.PageID).Scan(&parent); err != nil {
					return nil, err
				}
				if parent.Valid {
					if err := add("page", parent.String); err != nil {
						return nil, err
					}
				}
			}
		}
		if str(inverse["kind"]) == "record.trash.set" {
			if err := validateRecordTrash(ctx, tx, inverse); err != nil {
				return nil, err
			}
			if inverse["trashed"] == false {
				restoredRecords[str(inverse["recordId"])] = true
				var parent sql.NullString
				if err := tx.QueryRowContext(ctx, "SELECT parent_page_id FROM pages WHERE id=?", entry.PageID).Scan(&parent); err != nil {
					return nil, err
				}
				if parent.Valid {
					if err := add("page", parent.String); err != nil {
						return nil, err
					}
				}
			}
		}
		if str(inverse["kind"]) == "collection.property.restore" {
			if err := validateReviewedPropertyRestore(ctx, tx, inverse); err != nil {
				return nil, err
			}
			for _, record := range schemaValueRecords(entry.After, inverse) {
				var page, collection string
				var deleted sql.NullString
				if err := tx.QueryRowContext(ctx, "SELECT r.page_id,r.collection_id,p.deleted_at FROM records r JOIN pages p ON p.id=r.page_id WHERE r.id=?", record).Scan(&page, &collection, &deleted); err != nil {
					return nil, err
				}
				if deleted.Valid && !restoredRecords[record] || collection != str(inverse["collectionId"]) {
					return nil, Fail("CONFLICT", "An affected record is trashed or moved outside the reviewed collection")
				}
				if err := add("record", record); err != nil {
					return nil, err
				}
				if err := add("page", page); err != nil {
					return nil, err
				}
			}
		}
		actions = append(actions, inverse)
	}
	keys := []string{}
	for key := range targets {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	guards := []contracts.ServiceExpectedRevision{}
	for _, key := range keys {
		guards = append(guards, targets[key])
	}
	return map[string]any{"operationId": id, "label": label, "actions": actions, "expectedRevisions": guards, "command": map[string]any{"kind": "operation.undo", "operationId": id}}, nil
}
func listJournal(ctx context.Context, k *Kernel, e contracts.ServiceExecutionContext, r map[string]any) (any, error) {
	page := str(r["pageId"])
	if err := k.authorizePage(ctx, k.db, e, page); err != nil {
		return nil, err
	}
	rows, err := k.db.QueryContext(ctx, `SELECT id,label,created_at,state,entries_json FROM operation_journal WHERE EXISTS(SELECT 1 FROM json_each(entries_json) WHERE json_extract(value,'$.pageId')=? OR json_extract(value,'$.ownerPageId')=?) ORDER BY created_at DESC,id DESC LIMIT 50`, page, page)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []any{}
	for rows.Next() {
		var id, label, created, state, raw string
		if err = rows.Scan(&id, &label, &created, &state, &raw); err != nil {
			return nil, err
		}
		var entries []journalEntry
		if err = json.Unmarshal([]byte(raw), &entries); err != nil {
			return nil, err
		}
		allowed := true
		for _, entry := range entries {
			if k.authorizePage(ctx, k.db, e, entry.PageID) != nil {
				allowed = false
				break
			}
		}
		if allowed {
			items = append(items, map[string]any{"id": id, "label": label, "createdAt": created, "state": state})
		}
	}
	return map[string]any{"operations": items}, rows.Err()
}

func journalInverse(ctx context.Context, q querier, entry journalEntry) (map[string]any, error) {
	if len(entry.ChangedFields) == 0 {
		inverse := map[string]any{}
		for key, value := range entry.Before {
			inverse[key] = value
		}
		if str(inverse["kind"]) == "collection.property.restore" {
			if _, present := inverse["values"]; !present && len(asList(entry.After["values"])) > 0 {
				inverse["values"] = []any{}
				inverse["valueInventoryVersion"] = 1
			}
		}
		return inverse, nil
	}
	current, err := journalCurrent(ctx, q, entry.After)
	if err != nil {
		return nil, err
	}
	for _, key := range entry.ChangedFields {
		current.Before[key] = entry.Before[key]
	}
	return current.Before, nil
}
