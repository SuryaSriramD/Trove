package workspace

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"
)

// Imported record deletion is a tombstone, not destructive row removal. The
// canonical page, values, attachments and history retain their original IDs.
func init() { RegisterCommand("record.trash.set", setRecordTrash) }
func recordTrashCurrent(ctx context.Context, q querier, action map[string]any) (journalEntry, error) {
	entry := journalEntry{Target: "record-trash:" + str(action["recordId"])}
	var deleted sql.NullString
	err := q.QueryRowContext(ctx, "SELECT r.page_id,p.deleted_at,c.owning_page_id FROM records r JOIN pages p ON p.id=r.page_id JOIN collections c ON c.id=r.collection_id WHERE r.id=?", action["recordId"]).Scan(&entry.PageID, &deleted, &entry.OwnerPageID)
	if err != nil {
		return entry, err
	}
	entry.Tombstone = deleted.String
	entry.Before = map[string]any{"kind": "record.trash.set", "recordId": action["recordId"], "trashed": deleted.Valid}
	return entry, nil
}
func validateRecordTrash(ctx context.Context, q querier, action map[string]any) error {
	var page string
	var parent sql.NullString
	if err := q.QueryRowContext(ctx, "SELECT r.page_id,p.parent_page_id FROM records r JOIN pages p ON p.id=r.page_id WHERE r.id=?", action["recordId"]).Scan(&page, &parent); err != nil {
		return err
	}
	if action["trashed"] == true {
		var descendants int
		if err := q.QueryRowContext(ctx, "SELECT count(*) FROM pages WHERE parent_page_id=? AND deleted_at IS NULL", page).Scan(&descendants); err != nil {
			return err
		}
		if descendants > 0 {
			return Fail("CONFLICT", "This record has live child pages; review and move or trash them before accepting source deletion")
		}
	} else if parent.Valid {
		var live int
		if err := q.QueryRowContext(ctx, "SELECT count(*) FROM pages WHERE id=? AND deleted_at IS NULL", parent.String).Scan(&live); err != nil {
			return err
		}
		if live != 1 {
			return Fail("CONFLICT", "Restore the record's parent before restoring this record")
		}
	}
	return nil
}
func setRecordTrash(m *Mutation, action map[string]any) error {
	if err := validateRecordTrash(m.Context, m.Tx, action); err != nil {
		return err
	}
	var page, collection string
	var parent sql.NullString
	if err := m.Tx.QueryRowContext(m.Context, "SELECT r.page_id,r.collection_id,p.parent_page_id FROM records r JOIN pages p ON p.id=r.page_id WHERE r.id=?", action["recordId"]).Scan(&page, &collection, &parent); err != nil {
		return err
	}
	if err := m.TouchPage(page); err != nil {
		return err
	}
	if err := m.Touch("collection", collection); err != nil {
		return err
	}
	if action["trashed"] == true {
		if err := m.Deleted("page", page); err != nil {
			return err
		}
		_, err := m.Exec("UPDATE pages SET deleted_at=COALESCE(deleted_at,?) WHERE id=?", now(), page)
		return err
	}
	if parent.Valid {
		if err := m.Require("page", parent.String); err != nil {
			return err
		}
	}
	_, err := m.Exec("UPDATE pages SET deleted_at=NULL WHERE id=?", page)
	return err
}
func importRecordDeletions(ctx context.Context, q querier, p *ImportPlan, item *ImportItem) ([]map[string]any, error) {
	actions := []map[string]any{}
	if item.Merge == nil {
		return actions, nil
	}
	fields := selectedImportDatabaseFields(item)
	for _, field := range item.Merge.Fields {
		if !strings.HasPrefix(field.Path, "/records/") || !field.Local.Present || fields[field.Path] != nil {
			continue
		}
		collection := importCollectionFor(p, item)
		if collection == nil || collection.RecordEnumerationVersion != 1 || !field.Base.Present || field.Incoming.Present {
			return nil, Fail("CONFLICT", "Record deletion requires a complete authorized source enumeration and an explicit source-absent decision")
		}
		explicit := false
		for _, choice := range item.Merge.Resolutions {
			if choice.Path == field.Path && choice.Choice == "take_incoming" {
				explicit = true
			}
		}
		if !explicit {
			return nil, Fail("CONFLICT", "Choose Accept source deletion explicitly for this missing record")
		}
		id := strings.TrimPrefix(field.Path, "/records/")
		var localPage, owner string
		if err := q.QueryRowContext(ctx, "SELECT page_id,collection_id FROM records WHERE id=?", id).Scan(&localPage, &owner); err != nil {
			return nil, err
		}
		var expectedPage string
		if json.Unmarshal(field.Local.Value, &expectedPage) != nil || expectedPage != localPage || owner != item.CollectionID {
			return nil, Fail("CONFLICT", "Reviewed record identity changed")
		}
		action := map[string]any{"kind": "record.trash.set", "recordId": id, "trashed": true}
		if err := validateRecordTrash(ctx, q, action); err != nil {
			return nil, err
		}
		// A deletion cannot silently discard a separately edited value choice.
		// Schema migrations own their complete value inventory and execute in
		// the same journal; otherwise the record's retained values stay local.
		for _, valueField := range item.Merge.Fields {
			if !strings.HasPrefix(valueField.Path, "/values/"+id+"/") {
				continue
			}
			property := strings.TrimPrefix(valueField.Path, "/values/"+id+"/")
			schemaChanged := false
			for _, schema := range item.Merge.Fields {
				if schema.Path != "/schema/"+property || !schema.Local.Present {
					continue
				}
				var before, after map[string]any
				_ = json.Unmarshal(schema.Local.Value, &before)
				if fields[schema.Path] == nil {
					schemaChanged = true
				} else {
					_ = json.Unmarshal(fields[schema.Path], &after)
					schemaChanged = before["type"] != after["type"]
				}
			}
			if !schemaChanged && !equalImportPresence(valueField.Local, importPresence(fields, valueField.Path)) {
				return nil, Fail("CONFLICT", "Keep this deleted record's local values, or resolve its schema migration explicitly before accepting deletion")
			}
		}
		actions = append(actions, action)
	}
	return actions, nil
}
