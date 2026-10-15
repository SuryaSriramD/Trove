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

type ImportCSVBinding struct {
	Version   int            `json:"version"`
	Identity  string         `json:"identity"`
	Locator   string         `json:"sourceLocator"`
	KeyColumn string         `json:"keyColumn"`
	Columns   []ImportColumn `json:"columns"`
}

func csvSourceIdentity(item *ImportItem) string {
	return string(marshal([]string{item.IdentityKind, item.ExternalID}))
}

func loadCSVBinding(ctx context.Context, q querier, p *ImportPlan, item *ImportItem) (ImportCSVBinding, bool, error) {
	var binding ImportCSVBinding
	var raw string
	err := q.QueryRowContext(ctx, "SELECT binding_json FROM import_csv_bindings WHERE source_system=? AND source_namespace=? AND source_identity=?", p.SourceSystem, p.Namespace, csvSourceIdentity(item)).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return binding, false, nil
	}
	if err != nil {
		return binding, false, err
	}
	if err = json.Unmarshal([]byte(raw), &binding); err != nil {
		return binding, false, err
	}
	if binding.Version != 1 {
		return binding, false, Fail("UNSUPPORTED_VERSION", "CSV key binding uses an unsupported version")
	}
	return binding, true, nil
}

func configureCSVKey(ctx context.Context, q querier, p *ImportPlan, index, keyIndex int, prepare bool) error {
	item := &p.Items[index]
	if keyIndex < 1 || keyIndex >= len(item.Columns) {
		return Fail("INVALID_REQUEST", "Choose a non-title source column containing unique stable row keys")
	}
	prior, bound, err := loadCSVBinding(ctx, q, p, item)
	if err != nil {
		return err
	}
	if item.MappingID != "" && !bound {
		return Fail("CONFLICT", "This historical CSV has no retained row-key binding. Use a new source namespace to establish one; current titles and row positions cannot identify old records")
	}
	key := item.Columns[keyIndex].Name
	if bound && prior.KeyColumn != key {
		return Fail("CONFLICT", "The retained row-key column cannot change during re-import")
	}
	seenHeaders := map[string]bool{}
	for i := range item.Columns {
		column := &item.Columns[i]
		if strings.TrimSpace(column.Name) == "" || column.SourceHeader != nil && strings.TrimSpace(*column.SourceHeader) == "" || seenHeaders[column.Name] {
			return Fail("INVALID_REQUEST", "Keyed CSV imports require distinct nonempty column names")
		}
		seenHeaders[column.Name] = true
		if bound && i != 0 {
			for _, old := range prior.Columns {
				if old.Name == column.Name && old.Type != "title" {
					column.Type = old.Type
				}
			}
		}
	}
	identity := csvSourceIdentity(item)
	entitySource := func(kind, value string) string { return "csv:" + string(marshal([]string{identity, kind, value})) }
	trackImportIdentity(p, "collection", entitySource("collection", ""), item.CollectionID)
	collection := PortableCollectionImport{CollectionID: item.CollectionID, PageID: item.PageID, Title: item.Title, Locator: item.Locator, Properties: []contracts.DomainPropertyDefinition{}, Records: []portableRecordSource{}, Views: []portableViewSource{}}
	for i, column := range item.Columns {
		name := column.Name
		kind := "property"
		if i == 0 {
			name, kind = "", "title_property"
		}
		trackImportIdentity(p, "property", entitySource(kind, name), column.PropertyID)
		config := map[string]any{}
		if column.Type == "number" {
			config["format"] = "number"
		}
		collection.Properties = append(collection.Properties, contracts.DomainPropertyDefinition{Id: contracts.DomainId(column.PropertyID), CollectionId: contracts.DomainId(item.CollectionID), Name: column.Name, Type: column.Type, SchemaVersion: 1, Config: marshal(config), Revision: "1"})
	}
	keys := map[string]bool{}
	for i := range p.Items {
		row := &p.Items[i]
		if row.Kind != "row" || row.CSVSource != item.Locator {
			continue
		}
		if len(row.Cells) != len(item.Columns) {
			return Fail("INVALID_REQUEST", "Every keyed CSV row must have the complete reviewed column shape")
		}
		value := row.Cells[keyIndex]
		if strings.TrimSpace(value) == "" || len(value) > 1024 || keys[value] {
			return Fail("INVALID_REQUEST", "CSV row keys must be nonempty, unique, and at most 1024 bytes")
		}
		keys[value] = true
		row.IdentityKind, row.ExternalID = "external_id", entitySource("row:"+key, value)
		row.MappingID, row.ExpectedRevision, row.Conflict, row.Choice = "", "", "", ""
		err := q.QueryRowContext(ctx, "SELECT m.id,m.local_id,p.revision FROM import_mappings m JOIN pages p ON p.id=m.local_id WHERE m.source_system=? AND m.source_namespace=? AND m.external_kind='page' AND m.identity_kind='external_id' AND m.identity_value=?", p.SourceSystem, p.Namespace, row.ExternalID).Scan(&row.MappingID, &row.PageID, &row.ExpectedRevision)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		trackImportIdentity(p, "record", row.ExternalID, row.RecordID)
		row.Kind, row.ParentID = "portable_record", item.PageID
		row.Reason = "Explicit source row key retained; page title and row position are not identities"
		record := portableRecordSource{ID: row.RecordID, PageID: row.PageID, PagePath: row.Locator, Values: map[string]any{}, KnownProperties: []string{}}
		for n, column := range item.Columns {
			if n == 0 {
				continue
			}
			record.KnownProperties = append(record.KnownProperties, column.PropertyID)
			if row.Cells[n] == "" {
				continue
			}
			converted, err := importCellValue(column.Type, row.Cells[n])
			if err != nil {
				return Fail("INVALID_REQUEST", fmt.Sprintf("CSV row %s does not match the reviewed %s type for %s; correct the source or choose another type", row.Locator, column.Type, column.Name))
			}
			record.Values[column.PropertyID] = converted
		}
		collection.Records = append(collection.Records, record)
	}
	item.Kind, item.CSVKeyColumn = "portable_collection", key
	item.Reason += "; explicit row-key binding selected for typed re-import"
	p.PortableCollections = append(p.PortableCollections, collection)
	p.CSVBindings = append(p.CSVBindings, ImportCSVBinding{Version: 1, Identity: identity, Locator: item.Locator, KeyColumn: key, Columns: item.Columns})
	if err = reuseImportDatabaseIdentities(ctx, q, p); err != nil {
		return err
	}
	// Property IDs in the visible binding and the retained schema are the same.
	for i := range p.CSVBindings {
		for _, candidate := range p.Items {
			if candidate.Locator == p.CSVBindings[i].Locator {
				p.CSVBindings[i].Columns = candidate.Columns
			}
		}
	}
	if !prepare {
		return nil
	}
	for i := range p.Items {
		candidate := &p.Items[i]
		if candidate.Locator == item.Locator || candidate.CSVSource == item.Locator {
			if err = prepareImportMerge(ctx, q, p, candidate); err != nil {
				return err
			}
		}
	}
	return nil
}

func restoreCSVKeyBindings(ctx context.Context, q querier, p *ImportPlan) error {
	for i := range p.Items {
		item := &p.Items[i]
		if item.Kind != "csv" {
			continue
		}
		binding, exists, err := loadCSVBinding(ctx, q, p, item)
		if err != nil {
			return err
		}
		if !exists {
			continue
		}
		index := -1
		for n, column := range item.Columns {
			if column.Name == binding.KeyColumn {
				index = n
			}
		}
		if index < 1 {
			return Fail("CONFLICT", "The retained CSV row-key column is missing or became the title column; source bytes remain retained")
		}
		if err = configureCSVKey(ctx, q, p, i, index, false); err != nil {
			return err
		}
	}
	return nil
}

func persistCSVKeyBindings(m *Mutation, p *ImportPlan) error {
	if p.NotionCopyGraph {
		return nil
	}
	for _, binding := range p.CSVBindings {
		var previous string
		err := m.Tx.QueryRowContext(m.Context, "SELECT binding_json FROM import_csv_bindings WHERE source_system=? AND source_namespace=? AND source_identity=?", p.SourceSystem, p.Namespace, binding.Identity).Scan(&previous)
		if err == nil {
			var old ImportCSVBinding
			if json.Unmarshal([]byte(previous), &old) != nil || old.KeyColumn != binding.KeyColumn {
				return Fail("CONFLICT", "CSV source key binding changed after preview")
			}
		} else if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if _, err = m.Exec("INSERT INTO import_csv_bindings VALUES(?,?,?,?) ON CONFLICT(source_system,source_namespace,source_identity) DO UPDATE SET binding_json=excluded.binding_json", p.SourceSystem, p.Namespace, binding.Identity, string(marshal(binding))); err != nil {
			return err
		}
	}
	return nil
}
