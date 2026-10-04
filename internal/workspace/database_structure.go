package workspace

import (
	"context"
	"database/sql"
	"trove.local/phase0/contracts"
)

func init() { RegisterQuery("databaseStructure", databaseStructure) }

// Structure reads do not hydrate cells or trigger formula evaluation. Dependency
// identities are visible only when both endpoints are in the granted scope.
func databaseStructure(ctx context.Context, k *Kernel, e contracts.ServiceExecutionContext, request map[string]any) (any, error) {
	if err := contracts.Validate("service", "DatabaseStructureRequest", marshal(request)); err != nil {
		return nil, Fail("INVALID_REQUEST", err.Error())
	}
	tx, err := k.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	collection := str(request["collectionId"])
	if err = k.authorizeEntity(ctx, tx, e, "collection", collection); err != nil {
		return nil, err
	}
	plan := &recordSQL{}
	scope := plan.scope(e)
	where := "r.collection_id=" + plan.bind(collection) + " AND p.deleted_at IS NULL AND (" + scope + ")"
	rows, err := tx.QueryContext(ctx, "SELECT r.id,s.parent_record_id FROM records r JOIN pages p ON p.id=r.page_id LEFT JOIN record_structure s ON s.record_id=r.id WHERE "+where+" ORDER BY r.sort_key,r.id LIMIT 10001", plan.args...)
	if err != nil {
		return nil, err
	}
	type structure struct {
		Parent       *string  `json:"parentRecordId"`
		Dependencies []string `json:"dependencyRecordIds"`
	}
	result := map[string]*structure{}
	for rows.Next() {
		var id string
		var parent sql.NullString
		if err = rows.Scan(&id, &parent); err != nil {
			rows.Close()
			return nil, err
		}
		v := &structure{Dependencies: []string{}}
		if parent.Valid {
			v.Parent = &parent.String
		}
		result[id] = v
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	if len(result) > 10000 {
		return nil, Fail("RESOURCE_EXHAUSTED", "Database structure exceeds 10000 records")
	}
	rows, err = tx.QueryContext(ctx, "SELECT d.record_id,d.depends_on_record_id FROM record_dependencies d JOIN records r ON r.id=d.record_id JOIN pages p ON p.id=r.page_id WHERE "+where+" ORDER BY d.record_id,d.depends_on_record_id", plan.args...)
	if err != nil {
		return nil, err
	}
	edgeCount := 0
	for rows.Next() {
		edgeCount++
		if edgeCount > 100000 {
			rows.Close()
			return nil, Fail("RESOURCE_EXHAUSTED", "Database structure exceeds 100000 dependency edges")
		}
		var id, target string
		if err = rows.Scan(&id, &target); err != nil {
			rows.Close()
			return nil, err
		}
		if result[id] != nil && result[target] != nil {
			result[id].Dependencies = append(result[id].Dependencies, target)
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	for _, value := range result {
		if value.Parent != nil && result[*value.Parent] == nil {
			value.Parent = nil
		}
	}
	if err = k.authorize(e); err != nil {
		return nil, err
	}
	return map[string]any{"structure": result}, nil
}
