package workspace

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"
)

// Export additive P1 metadata as inert evidence. Import never treats saved rule
// enablement, button commands or external paths as a new execution permission.
func exportPhase4Metadata(ctx context.Context, tx *sql.Tx) (map[string]any, error) {
	out := map[string]any{"schemaVersion": 1, "activationPolicy": "retained evidence only; review before creating rules, actions or filesystem approvals"}
	queries := map[string]string{
		"p2DatabaseConfigs": "SELECT s.collection_id,s.config_json FROM p2_database_config s JOIN collections c ON c.id=s.collection_id JOIN pages p ON p.id=c.owning_page_id WHERE p.deleted_at IS NULL ORDER BY s.collection_id",
		"p2FormSubmissions": "SELECT s.record_id,s.collection_revision,s.submitted_at FROM p2_form_submissions s JOIN records r ON r.id=s.record_id JOIN pages p ON p.id=r.page_id WHERE p.deleted_at IS NULL ORDER BY s.record_id",

		"pageLocks":             "SELECT l.page_id,l.locked,l.revision FROM advanced_page_locks l JOIN pages p ON p.id=l.page_id WHERE p.deleted_at IS NULL ORDER BY l.page_id",
		"templates":             "SELECT t.id,t.source_page_id,t.name,t.snapshot_json,t.created_at FROM advanced_templates t JOIN pages p ON p.id=t.source_page_id WHERE p.deleted_at IS NULL ORDER BY t.id",
		"buttonActions":         "SELECT a.id,a.page_id,a.label,a.commands_json FROM advanced_button_actions a JOIN pages p ON p.id=a.page_id WHERE p.deleted_at IS NULL ORDER BY a.id",
		"databaseSettings":      "SELECT s.collection_id,s.locked FROM database_settings s JOIN collections c ON c.id=s.collection_id JOIN pages p ON p.id=c.owning_page_id WHERE p.deleted_at IS NULL ORDER BY s.collection_id",
		"recordStructure":       "SELECT s.record_id,s.parent_record_id FROM record_structure s JOIN records r ON r.id=s.record_id JOIN pages p ON p.id=r.page_id WHERE p.deleted_at IS NULL ORDER BY s.record_id",
		"recordDependencies":    "SELECT d.record_id,d.depends_on_record_id FROM record_dependencies d JOIN records r ON r.id=d.record_id JOIN pages p ON p.id=r.page_id WHERE p.deleted_at IS NULL ORDER BY d.record_id,d.depends_on_record_id",
		"automationDefinitions": "SELECT a.id,a.page_id,a.version,a.rule_json FROM automation_rules a JOIN pages p ON p.id=a.page_id WHERE p.deleted_at IS NULL ORDER BY a.id",
		"externalReferences":    "SELECT r.page_id,s.id AS source_id,s.path,s.content_hash,s.state,s.language,s.extractor_version FROM external_page_references r JOIN external_sources s ON s.id=r.source_id JOIN pages p ON p.id=r.page_id WHERE p.deleted_at IS NULL ORDER BY r.page_id,s.id",
	}
	for name, query := range queries {
		rows, err := tx.QueryContext(ctx, query)
		if err != nil {
			return nil, err
		}
		columns, err := rows.Columns()
		if err != nil {
			rows.Close()
			return nil, err
		}
		items := []map[string]any{}
		for rows.Next() {
			values := make([]any, len(columns))
			dest := make([]any, len(columns))
			for i := range dest {
				dest[i] = &values[i]
			}
			if err = rows.Scan(dest...); err != nil {
				rows.Close()
				return nil, err
			}
			item := map[string]any{}
			for i, key := range columns {
				v := values[i]
				if bytes, ok := v.([]byte); ok {
					v = string(bytes)
				}
				if strings.HasSuffix(key, "_json") {
					if text, ok := v.(string); ok {
						var decoded any
						if err = json.Unmarshal([]byte(text), &decoded); err != nil {
							rows.Close()
							return nil, err
						}
						v = decoded
					}
				}
				item[key] = v
			}
			items = append(items, item)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return nil, err
		}
		out[name] = items
	}
	return out, nil
}
