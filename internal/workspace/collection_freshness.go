package workspace

import (
	"context"
	"encoding/json"
)

// A page's collection knowledge is independent of view sorting, filters, widths,
// tabs and record layouts. Collection identity and unavailable state are retained;
// hidden row facts are not represented as live knowledge.
func reviewPageCollectionKnowledge(ctx context.Context, q querier, page string) (map[string]any, map[string]bool, error) {
	rows, err := q.QueryContext(ctx, `SELECT c.id,c.owning_page_id,c.title,c.title_property_id,c.system_role,p.deleted_at IS NOT NULL OR EXISTS(SELECT 1 FROM collection_visibility h WHERE h.collection_id=c.id) FROM collections c JOIN pages p ON p.id=c.owning_page_id WHERE c.owning_page_id=? OR c.id IN(SELECT collection_id FROM views WHERE owning_page_id=?) ORDER BY c.id LIMIT 1001`, page, page)
	if err != nil {
		return nil, nil, err
	}
	type collection struct {
		id, owner, title, titleProperty, role string
		hidden                                bool
	}
	collections := []collection{}
	for rows.Next() {
		var c collection
		if err = rows.Scan(&c.id, &c.owner, &c.title, &c.titleProperty, &c.role, &c.hidden); err != nil {
			rows.Close()
			return nil, nil, err
		}
		collections = append(collections, c)
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return nil, nil, err
	}
	rows.Close()
	if len(collections) > 1000 {
		return nil, nil, Fail("RESOURCE_EXHAUSTED", "Review collection graph exceeds 1000 collections")
	}
	nodes := map[string]any{}
	pages := map[string]bool{}
	records := []string{}
	schemaCount := 0
	for _, c := range collections {
		pages[c.owner] = true
		node := map[string]any{"title": c.title, "titlePropertyId": c.titleProperty, "systemRole": c.role, "available": !c.hidden}
		nodes[c.id] = node
		if c.hidden {
			continue
		}
		props, err := q.QueryContext(ctx, "SELECT id,name,type,schema_version,config_json FROM properties WHERE collection_id=? ORDER BY id LIMIT 4097", c.id)
		if err != nil {
			return nil, nil, err
		}
		fields := []any{}
		for props.Next() {
			var id, name, kind, config string
			var version int
			if err = props.Scan(&id, &name, &kind, &version, &config); err != nil {
				props.Close()
				return nil, nil, err
			}
			schemaCount++
			if schemaCount > 4096 {
				props.Close()
				return nil, nil, Fail("RESOURCE_EXHAUSTED", "Review collection schema exceeds 4096 properties")
			}
			fields = append(fields, map[string]any{"id": id, "name": name, "type": kind, "schemaVersion": version, "config": json.RawMessage(config)})
		}
		if err = props.Err(); err != nil {
			props.Close()
			return nil, nil, err
		}
		props.Close()
		node["schema"] = fields
		rs, err := q.QueryContext(ctx, "SELECT r.id FROM records r JOIN pages p ON p.id=r.page_id WHERE r.collection_id=? AND p.deleted_at IS NULL ORDER BY r.id LIMIT 1001", c.id)
		if err != nil {
			return nil, nil, err
		}
		members := []string{}
		for rs.Next() {
			var id string
			if err = rs.Scan(&id); err != nil {
				rs.Close()
				return nil, nil, err
			}
			members = append(members, id)
		}
		if err = rs.Err(); err != nil {
			rs.Close()
			return nil, nil, err
		}
		rs.Close()
		if len(records)+len(members) > 1000 {
			return nil, nil, Fail("RESOURCE_EXHAUSTED", "Review collection graph exceeds 1000 live records")
		}
		node["records"] = members
		records = append(records, members...)
	}
	graph, related, err := reviewRecordsKnowledge(ctx, q, records)
	if err != nil {
		return nil, nil, err
	}
	for page := range related {
		pages[page] = true
	}
	return map[string]any{"collections": nodes, "records": graph}, pages, nil
}
