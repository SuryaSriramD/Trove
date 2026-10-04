package workspace

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"trove.local/phase0/contracts"
)

type recoveryRecordStructure struct {
	ParentRecordID      *string  `json:"parentRecordId"`
	DependencyRecordIDs []string `json:"dependencyRecordIds"`
}

func captureRecordStructure(ctx context.Context, q querier, record string) (*recoveryRecordStructure, error) {
	var exists int
	if err := q.QueryRowContext(ctx, "SELECT count(*) FROM sqlite_master WHERE type='table' AND name='record_structure'").Scan(&exists); err != nil {
		return nil, err
	}
	if exists == 0 {
		return nil, nil
	}
	s := &recoveryRecordStructure{DependencyRecordIDs: []string{}}
	var parent sql.NullString
	err := q.QueryRowContext(ctx, "SELECT parent_record_id FROM record_structure WHERE record_id=?", record).Scan(&parent)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	if parent.Valid {
		s.ParentRecordID = &parent.String
	}
	rows, err := q.QueryContext(ctx, "SELECT depends_on_record_id FROM record_dependencies WHERE record_id=? ORDER BY depends_on_record_id", record)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			return nil, err
		}
		s.DependencyRecordIDs = append(s.DependencyRecordIDs, id)
	}
	return s, rows.Err()
}
func addRecoveryDatabaseGuards(ctx context.Context, q querier, snap recoverySnapshot, add func(string, string) error) error {
	if snap.Record == nil {
		return nil
	}
	id := string(snap.Record.Id)
	current, err := captureRecordStructure(ctx, q, id)
	if err != nil {
		return err
	}
	if snap.RecordStructure == nil {
		if current != nil && (current.ParentRecordID != nil || len(current.DependencyRecordIDs) > 0) {
			return Fail("CONFLICT", "This historical snapshot predates record hierarchy recovery; current parent/dependencies are retained. Choose a newer snapshot.")
		}
	} else {
		ids := append([]string{}, snap.RecordStructure.DependencyRecordIDs...)
		if snap.RecordStructure.ParentRecordID != nil {
			ids = append(ids, *snap.RecordStructure.ParentRecordID)
		}
		for _, target := range ids {
			if err = add("record", target); err != nil {
				return err
			}
		}
	}
	// Recovery of a reciprocal edge must fence both its old and historical targets.
	rows, err := q.QueryContext(ctx, "SELECT id,config_json FROM properties WHERE collection_id=? AND type='relation'", snap.Record.CollectionId)
	if err != nil {
		return err
	}
	type rel struct {
		id  string
		cfg map[string]any
	}
	rels := []rel{}
	for rows.Next() {
		var r rel
		var raw string
		if err = rows.Scan(&r.id, &raw); err != nil {
			rows.Close()
			return err
		}
		json.Unmarshal([]byte(raw), &r.cfg)
		rels = append(rels, r)
	}
	rows.Close()
	for _, r := range rels {
		back := str(r.cfg["bidirectionalPropertyId"])
		if back == "" {
			continue
		}
		if err = add("property", r.id); err != nil {
			return err
		}
		if err = add("property", back); err != nil {
			return err
		}
		targets := []string{}
		var old string
		err = q.QueryRowContext(ctx, "SELECT value_json FROM property_values WHERE record_id=? AND property_id=?", id, r.id).Scan(&old)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		json.Unmarshal([]byte(old), &targets)
		for _, v := range snap.Record.Values {
			if string(v.PropertyId) == r.id {
				var historical []string
				json.Unmarshal(v.Value, &historical)
				targets = append(targets, historical...)
			}
		}
		for _, target := range targets {
			var page, col string
			if err = q.QueryRowContext(ctx, "SELECT page_id,collection_id FROM records WHERE id=?", target).Scan(&page, &col); err != nil {
				return err
			}
			for _, ref := range [][2]string{{"record", target}, {"page", page}, {"collection", col}} {
				if err = add(ref[0], ref[1]); err != nil {
					return err
				}
			}
		}
	}
	return nil
}
func restoreRecoveryDatabase(m *Mutation, snap recoverySnapshot) error {
	if snap.Record == nil {
		return nil
	}
	if snap.RecordStructure != nil {
		deps := []any{}
		for _, id := range snap.RecordStructure.DependencyRecordIDs {
			deps = append(deps, id)
		}
		var parent any
		if snap.RecordStructure.ParentRecordID != nil {
			parent = *snap.RecordStructure.ParentRecordID
		}
		if err := advancedDatabaseCommand(m, map[string]any{"kind": "record.configure", "recordId": string(snap.Record.Id), "parentRecordId": parent, "dependencyRecordIds": deps}); err != nil {
			return err
		}
	}
	rows, err := m.Tx.QueryContext(m.Context, "SELECT id FROM properties WHERE collection_id=? AND type='relation'", snap.Record.CollectionId)
	if err != nil {
		return err
	}
	ids := []string{}
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		ids = append(ids, id)
	}
	rows.Close()
	for _, id := range ids {
		p, err := propertyDefinition(m.Context, m.Tx, id)
		if err != nil {
			return err
		}
		var cfg map[string]any
		json.Unmarshal(p.Config, &cfg)
		if str(cfg["bidirectionalPropertyId"]) == "" {
			continue
		}
		var value any = []any{}
		for _, v := range snap.Record.Values {
			if v.PropertyId == contracts.DomainId(id) {
				json.Unmarshal(v.Value, &value)
			}
		}
		if err = syncReciprocalRelation(m, string(snap.Record.Id), p, value); err != nil {
			return err
		}
	}
	return nil
}
