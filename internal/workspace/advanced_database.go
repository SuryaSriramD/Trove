package workspace

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"sort"
	"strings"
	"time"
	"trove.local/phase0/contracts"
)

func init() {
	RegisterInitializer(func(ctx context.Context, k *Kernel) error {
		return k.write(ctx, func(tx *sql.Tx) error {
			_, err := tx.ExecContext(ctx, `
 CREATE TABLE IF NOT EXISTS database_settings(collection_id TEXT PRIMARY KEY REFERENCES collections(id) ON DELETE CASCADE,locked INTEGER NOT NULL DEFAULT 0);
 CREATE TABLE IF NOT EXISTS record_structure(record_id TEXT PRIMARY KEY REFERENCES records(id) ON DELETE CASCADE,parent_record_id TEXT REFERENCES records(id));
 CREATE TABLE IF NOT EXISTS record_dependencies(record_id TEXT NOT NULL REFERENCES records(id) ON DELETE CASCADE,depends_on_record_id TEXT NOT NULL REFERENCES records(id),PRIMARY KEY(record_id,depends_on_record_id));
 CREATE TABLE IF NOT EXISTS relation_edges(property_id TEXT NOT NULL REFERENCES properties(id) ON DELETE CASCADE,record_id TEXT NOT NULL REFERENCES records(id) ON DELETE CASCADE,target_record_id TEXT NOT NULL REFERENCES records(id) ON DELETE CASCADE,PRIMARY KEY(property_id,record_id,target_record_id));
 CREATE INDEX IF NOT EXISTS relation_edges_target ON relation_edges(target_record_id,property_id);
 `)
			return err
		})
	})
	RegisterCommand("collection.configure", advancedDatabaseCommand)
	RegisterCommand("record.configure", advancedDatabaseCommand)
	RegisterCommitValidator(validateAdvancedDatabase)
	RegisterCommitProjection(projectRelationEdges)
	RegisterQuery("databaseDerived", databaseDerived)
}
func advancedDatabaseCommand(m *Mutation, c map[string]any) error {
	if c["kind"] == "collection.configure" {
		id := str(c["collectionId"])
		if len(m.Request.Commands) != 1 {
			return Fail("INVALID_REQUEST", "Lock changes require an isolated batch")
		}
		if err := m.Touch("collection", id); err != nil {
			return err
		}
		_, err := m.Exec("INSERT INTO database_settings VALUES(?,?) ON CONFLICT(collection_id) DO UPDATE SET locked=excluded.locked", id, c["locked"])
		return err
	}
	id := str(c["recordId"])
	var page, collection string
	if err := m.Tx.QueryRowContext(m.Context, "SELECT page_id,collection_id FROM records WHERE id=?", id).Scan(&page, &collection); err != nil {
		return err
	}
	if err := m.TouchPage(page); err != nil {
		return err
	}
	if err := m.Touch("collection", collection); err != nil {
		return err
	}
	targets := []string{}
	if p := str(c["parentRecordId"]); p != "" {
		targets = append(targets, p)
	}
	for _, r := range c["dependencyRecordIds"].([]any) {
		targets = append(targets, str(r))
	}
	if len(targets) > 129 {
		return Fail("RESOURCE_EXHAUSTED", "At most 128 dependencies are supported")
	}
	for _, target := range targets {
		if target == id {
			return Fail("INVALID_REQUEST", "A record cannot depend on or parent itself")
		}
		if err := m.Require("record", target); err != nil {
			return err
		}
		var col string
		if err := m.Tx.QueryRowContext(m.Context, "SELECT collection_id FROM records WHERE id=?", target).Scan(&col); err != nil {
			return err
		}
		if col != collection {
			return Fail("INVALID_REQUEST", "Sub-items and dependencies must share a collection")
		}
		var deleted sql.NullString
		if err := m.Tx.QueryRowContext(m.Context, "SELECT p.deleted_at FROM records r JOIN pages p ON p.id=r.page_id WHERE r.id=?", target).Scan(&deleted); err != nil {
			return err
		}
		if deleted.Valid {
			return Fail("INVALID_REQUEST", "Parent/dependency record is trashed")
		}
	}
	if _, err := m.Exec("INSERT INTO record_structure VALUES(?,?) ON CONFLICT(record_id) DO UPDATE SET parent_record_id=excluded.parent_record_id", id, nullable(c["parentRecordId"])); err != nil {
		return err
	}
	if _, err := m.Exec("DELETE FROM record_dependencies WHERE record_id=?", id); err != nil {
		return err
	}
	for _, raw := range c["dependencyRecordIds"].([]any) {
		if _, err := m.Exec("INSERT INTO record_dependencies VALUES(?,?)", id, raw); err != nil {
			return err
		}
	}
	return nil
}
func validateAdvancedDatabase(m *Mutation) error {
	onlyLock := len(m.Request.Commands) == 1
	if onlyLock {
		var c map[string]any
		json.Unmarshal(marshal(m.Request.Commands[0]), &c)
		onlyLock = c["kind"] == "collection.configure"
	}
	for _, change := range m.touched {
		if change.Target.EntityType == "record" && !onlyLock {
			var locked int
			err := m.Tx.QueryRowContext(m.Context, "SELECT COALESCE(s.locked,0) FROM records r LEFT JOIN database_settings s ON s.collection_id=r.collection_id WHERE r.id=?", change.Target.EntityId).Scan(&locked)
			if err != nil && !errors.Is(err, sql.ErrNoRows) {
				return err
			}
			if locked == 1 {
				return Fail("SCOPE_DENIED", "Database is locked; unlock it before editing")
			}
		}
		if change.Target.EntityType == "collection" && !onlyLock {
			var locked int
			err := m.Tx.QueryRowContext(m.Context, "SELECT locked FROM database_settings WHERE collection_id=?", change.Target.EntityId).Scan(&locked)
			if err != nil && !errors.Is(err, sql.ErrNoRows) {
				return err
			}
			if locked == 1 {
				return Fail("SCOPE_DENIED", "Database is locked; unlock it before editing")
			}
		}
	}
	// Validate the complete graph so additions in the same transaction are checked.
	rows, err := m.Tx.QueryContext(m.Context, "SELECT id,collection_id,type,config_json FROM properties WHERE type IN ('formula','rollup','relation')")
	if err != nil {
		return err
	}
	type def struct {
		id, col, kind string
		config        map[string]any
	}
	defs := []def{}
	for rows.Next() {
		var d def
		var raw string
		if err = rows.Scan(&d.id, &d.col, &d.kind, &raw); err != nil {
			rows.Close()
			return err
		}
		json.Unmarshal([]byte(raw), &d.config)
		defs = append(defs, d)
	}
	rows.Close()
	graph := map[string][]string{}
	for _, d := range defs {
		if d.kind == "relation" {
			target := str(d.config["targetCollectionId"])
			var n int
			if err = m.Tx.QueryRowContext(m.Context, "SELECT count(*) FROM collections WHERE id=?", target).Scan(&n); err != nil {
				return err
			}
			if n != 1 {
				return Fail("INVALID_REQUEST", "Relation target collection is missing")
			}
			if reciprocal := str(d.config["bidirectionalPropertyId"]); reciprocal != "" {
				p, e := propertyDefinition(m.Context, m.Tx, reciprocal)
				if e != nil {
					return e
				}
				var cfg map[string]any
				json.Unmarshal(p.Config, &cfg)
				if p.Type != "relation" || string(p.CollectionId) != target || str(cfg["targetCollectionId"]) != d.col || str(cfg["bidirectionalPropertyId"]) != d.id {
					return Fail("INVALID_REQUEST", "Reciprocal relation definitions must point back to each other")
				}
			}
			continue
		}
		raw, _ := d.config["raw"].(map[string]any)
		if raw == nil {
			continue
		}
		deps := []string{}
		if d.kind == "formula" {
			expression := str(raw["translatedExpression"])
			if expression == "" {
				continue
			}
			var e error
			deps, e = formulaDependencies(expression)
			if e != nil {
				continue
			} // Unsupported imports retain evidence and diagnose at read time.
		} else {
			relation := str(raw["relationPropertyId"])
			target := str(raw["targetPropertyId"])
			if relation == "" || target == "" {
				continue
			}
			rp, e := propertyDefinition(m.Context, m.Tx, relation)
			if e != nil {
				return e
			}
			var cfg map[string]any
			json.Unmarshal(rp.Config, &cfg)
			tp, e := propertyDefinition(m.Context, m.Tx, target)
			if e != nil {
				return e
			}
			if rp.Type != "relation" || string(rp.CollectionId) != d.col || string(tp.CollectionId) != str(cfg["targetCollectionId"]) {
				return Fail("INVALID_REQUEST", "Rollup requires a relation and a property in its target collection")
			}
			deps = []string{target}
		}
		for _, dep := range deps {
			p, e := propertyDefinition(m.Context, m.Tx, dep)
			if e != nil {
				return Fail("INVALID_REQUEST", "Formula references an unknown stable property ID")
			}
			if d.kind == "formula" && string(p.CollectionId) != d.col {
				return Fail("INVALID_REQUEST", "Formula property references must be in the same collection")
			}
		}
		graph[d.id] = deps
	}
	if graphCycle(graph) {
		return Fail("INVALID_REQUEST", "Formula/rollup dependency cycle detected")
	}
	for _, query := range []string{"SELECT record_id,parent_record_id FROM record_structure WHERE parent_record_id IS NOT NULL", "SELECT record_id,depends_on_record_id FROM record_dependencies"} {
		g := map[string][]string{}
		rs, e := m.Tx.QueryContext(m.Context, query)
		if e != nil {
			return e
		}
		for rs.Next() {
			var a, b string
			if e = rs.Scan(&a, &b); e != nil {
				rs.Close()
				return e
			}
			g[a] = append(g[a], b)
		}
		rs.Close()
		if graphCycle(g) {
			return Fail("INVALID_REQUEST", "Sub-item or dependency cycle detected")
		}
	}
	return nil
}
func graphCycle(graph map[string][]string) bool {
	visiting := map[string]bool{}
	done := map[string]bool{}
	var visit func(string, int) bool
	visit = func(id string, depth int) bool {
		if depth > 1024 {
			return true
		}
		if visiting[id] {
			return true
		}
		if done[id] {
			return false
		}
		visiting[id] = true
		for _, d := range graph[id] {
			if visit(d, depth+1) {
				return true
			}
		}
		visiting[id] = false
		done[id] = true
		return false
	}
	for id := range graph {
		if visit(id, 0) {
			return true
		}
	}
	return false
}
func projectRelationEdges(m *Mutation) error {
	for _, change := range m.touched {
		if change.Target.EntityType != "record" {
			continue
		}
		id := string(change.Target.EntityId)
		if _, err := m.Exec("DELETE FROM relation_edges WHERE record_id=?", id); err != nil {
			return err
		}
		_, err := m.Exec(`INSERT INTO relation_edges(property_id,record_id,target_record_id) SELECT v.property_id,v.record_id,j.value FROM property_values v,json_each(v.value_json) j WHERE v.record_id=? AND v.property_type='relation'`, id)
		if err != nil {
			return err
		}
	}
	return nil
}

type DerivedValue struct {
	PropertyID           string   `json:"propertyId"`
	Status               string   `json:"status"`
	Value                any      `json:"value"`
	OriginalExpression   string   `json:"originalExpression,omitempty"`
	TranslatedExpression string   `json:"translatedExpression,omitempty"`
	ImportedCachedValue  any      `json:"importedCachedValue,omitempty"`
	Diagnostic           string   `json:"diagnostic,omitempty"`
	ComputedAt           string   `json:"computedAt,omitempty"`
	Timezone             string   `json:"timezone"`
	EvaluatorVersion     int      `json:"evaluatorVersion,omitempty"`
	Dependencies         []string `json:"dependencies,omitempty"`
}

// Metadata is immutable for the evaluator's read transaction. Its cache is
// deliberately small; exceeding it falls back to ordinary reads.
type derivedPropertyMetadata struct {
	definition contracts.DomainPropertyDefinition
	raw        map[string]any
}

type derivedEvaluator struct {
	ctx       context.Context
	k         *Kernel
	tx        *sql.Tx
	execution contracts.ServiceExecutionContext
	clock     FormulaClock
	active    map[string]bool
	cache     map[string]DerivedValue
	work      int
	// Query paging can reuse an immutable memo without sharing mutable maps.
	baseCache          map[string]DerivedValue
	workLimit          int
	byteLimit          int
	memoBytes          int
	exhausted          bool
	properties         map[string]derivedPropertyMetadata
	propertyBytes      int
	executionJSON      string
	executionValidated bool
}

func (d *derivedEvaluator) property(id string) (derivedPropertyMetadata, error) {
	if p, ok := d.properties[id]; ok {
		return p, nil
	}
	definition, err := propertyDefinition(d.ctx, d.tx, id)
	if err != nil {
		return derivedPropertyMetadata{}, err
	}
	var config map[string]any
	if err = json.Unmarshal(definition.Config, &config); err != nil {
		return derivedPropertyMetadata{}, err
	}
	raw, _ := config["raw"].(map[string]any)
	p := derivedPropertyMetadata{definition: definition, raw: raw}
	size := len(marshal(definition))
	if len(d.properties) < 256 && d.propertyBytes+size <= 512*1024 {
		d.properties[id] = p
		d.propertyBytes += size
		// Count cached schema bytes against the same conservative working budget.
		d.memoBytes += size
	}
	return p, nil
}

// The evaluator owns a deep copy of its context. Schema validation can be
// reused only while its canonical bytes still match the live kernel grant.
// Revocation/policy changes and page/project scope are checked for every value,
// including memo hits; only immutable property definitions are cached.
func (d *derivedEvaluator) authorizeRecord(record string) error {
	if !d.executionValidated {
		if err := d.k.authorize(d.execution); err != nil {
			return err
		}
		d.executionValidated = true
	}
	d.k.mu.Lock()
	grant, live := d.k.grants[string(d.execution.ScopeGrantId)]
	d.k.mu.Unlock()
	if !live || string(marshal(grant)) != d.executionJSON {
		return Fail("SCOPE_DENIED", "Scope grant is absent or changed during evaluation")
	}
	var page string
	var deleted sql.NullString
	if err := d.tx.QueryRowContext(d.ctx, "SELECT r.page_id,p.deleted_at FROM records r JOIN pages p ON p.id=r.page_id WHERE r.id=?", record).Scan(&page, &deleted); err != nil {
		return err
	}
	if deleted.Valid {
		return fmt.Errorf("related record is unavailable")
	}
	return d.k.authorizePageScope(d.ctx, d.tx, d.execution, page)
}

func (d *derivedEvaluator) value(record, property string) (DerivedValue, error) {
	if err := d.ctx.Err(); err != nil {
		return DerivedValue{}, err
	}
	if err := d.authorizeRecord(record); err != nil {
		return DerivedValue{}, fmt.Errorf("related record is unavailable in this scope")
	}
	key := record + ":" + property
	if v, ok := d.cache[key]; ok {
		return v, nil
	}
	if v, ok := d.baseCache[key]; ok {
		return v, nil
	}
	v := DerivedValue{PropertyID: property, Status: "computed", Timezone: d.clock.Timezone}
	d.work++
	limit := d.workLimit
	if limit == 0 {
		limit = 10000
	}
	if d.work > limit || d.exhausted {
		d.exhausted = true
		return v, Fail("RESOURCE_EXHAUSTED", "Derived query work budget exceeded; narrow the view")
	}
	if d.active[key] {
		return v, fmt.Errorf("dependency cycle")
	}
	d.active[key] = true
	defer delete(d.active, key)
	metadata, err := d.property(property)
	if err != nil {
		return v, err
	}
	p := metadata.definition
	var col, page, title, created, edited string
	var deleted sql.NullString
	if err = d.tx.QueryRowContext(d.ctx, "SELECT r.collection_id,r.page_id,p.title,p.created_at,p.updated_at,p.deleted_at FROM records r JOIN pages p ON p.id=r.page_id WHERE r.id=?", record).Scan(&col, &page, &title, &created, &edited, &deleted); err != nil {
		return v, err
	}
	if col != string(p.CollectionId) || deleted.Valid {
		return v, fmt.Errorf("related record is unavailable")
	}
	raw := metadata.raw
	switch p.Type {
	case "created_time":
		v.Value = created
	case "last_edited_time":
		v.Value = edited
	case "unique_id":
		v.Value = record
	case "title":
		v.Value = title
	case "formula":
		v.OriginalExpression = str(raw["originalExpression"])
		v.TranslatedExpression = str(raw["translatedExpression"])
		v.ImportedCachedValue = raw["importedCachedValue"]
		if v.ImportedCachedValue == nil {
			var captured string
			if d.tx.QueryRowContext(d.ctx, "SELECT value_json FROM property_values WHERE record_id=? AND property_id=?", record, property).Scan(&captured) == nil {
				var stored map[string]any
				if json.Unmarshal([]byte(captured), &stored) == nil {
					if evidence, ok := stored["raw"].(map[string]any); ok && evidence["importedCachedValue"] != nil {
						v.ImportedCachedValue = evidence["importedCachedValue"]
					} else {
						v.ImportedCachedValue = stored
					}
				}
			}
		}
		v.Dependencies, _ = formulaDependencies(v.TranslatedExpression)
		if v.TranslatedExpression == "" {
			err = fmt.Errorf("No supported translated expression; captured evidence is retained")
		} else {
			version := 1
			if configured, exists := raw["evaluatorVersion"]; exists {
				version = int(number(configured))
			}
			v.EvaluatorVersion = version
			v.Value, err = EvaluateFormulaVersion(v.TranslatedExpression, version, d.clock, func(id string) (any, error) { return d.formulaInput(record, id, version) })
		}
	case "rollup":
		relation, target, aggregation := str(raw["relationPropertyId"]), str(raw["targetPropertyId"]), str(raw["aggregation"])
		v.ImportedCachedValue = raw["importedCachedValue"]
		if v.ImportedCachedValue == nil {
			var captured string
			if d.tx.QueryRowContext(d.ctx, "SELECT value_json FROM property_values WHERE record_id=? AND property_id=?", record, property).Scan(&captured) == nil {
				var stored map[string]any
				if json.Unmarshal([]byte(captured), &stored) == nil {
					if evidence, ok := stored["raw"].(map[string]any); ok && evidence["importedCachedValue"] != nil {
						v.ImportedCachedValue = evidence["importedCachedValue"]
					} else {
						v.ImportedCachedValue = stored
					}
				}
			}
		}
		v.Dependencies = []string{relation, target}
		var data string
		err = d.tx.QueryRowContext(d.ctx, "SELECT value_json FROM property_values WHERE record_id=? AND property_id=? AND property_type='relation'", record, relation).Scan(&data)
		if errors.Is(err, sql.ErrNoRows) {
			data = "[]"
			err = nil
		}
		if err != nil {
			break
		}
		var ids []string
		json.Unmarshal([]byte(data), &ids)
		if len(ids) > 1000 {
			err = fmt.Errorf("rollup exceeds 1000 related records")
			break
		}
		vals := []any{}
		for _, id := range ids {
			var r DerivedValue
			r, err = d.value(id, target)
			if err != nil {
				break
			}
			if r.Status != "computed" {
				err = fmt.Errorf("related derived value: %s", r.Diagnostic)
				break
			}
			vals = append(vals, r.Value)
		}
		if err == nil {
			v.Value, err = aggregateRollup(aggregation, vals)
		}
	default:
		var data string
		err = d.tx.QueryRowContext(d.ctx, "SELECT value_json FROM property_values WHERE record_id=? AND property_id=?", record, property).Scan(&data)
		if errors.Is(err, sql.ErrNoRows) {
			err = nil
			v.Value = nil
		} else if err == nil {
			json.Unmarshal([]byte(data), &v.Value)
		}
	}
	if err != nil {
		v.Status = "error"
		v.Value = nil
		v.Diagnostic = err.Error()
		if v.ImportedCachedValue != nil {
			v.Status = "captured"
		}
	} else {
		v.ComputedAt = d.clock.At.UTC().Format(time.RFC3339Nano)
	}
	if err := d.ctx.Err(); err != nil {
		return v, err
	}
	d.memoBytes += len(key) + len(marshal(v))
	if d.byteLimit > 0 && d.memoBytes > d.byteLimit {
		d.exhausted = true
	}
	if d.exhausted {
		return v, Fail("RESOURCE_EXHAUSTED", "Derived query memo budget exceeded; narrow the view")
	}
	d.cache[key] = v
	return v, nil
}
func aggregateRollup(op string, values []any) (any, error) {
	if op == "count" {
		return map[string]any{"decimal": fmt.Sprint(len(values)), "format": "decimal"}, nil
	}
	if op == "show_original" {
		return values, nil
	}
	if len(values) == 0 {
		return nil, nil
	}
	sum := new(big.Rat)
	var best *big.Rat
	n := 0
	for _, v := range values {
		if v == nil {
			continue
		}
		r, e := formulaNumber(v)
		if e != nil {
			return nil, fmt.Errorf("%s rollup requires numeric values", op)
		}
		sum.Add(sum, r)
		n++
		if best == nil || op == "min" && r.Cmp(best) < 0 || op == "max" && r.Cmp(best) > 0 {
			best = r
		}
	}
	if n == 0 {
		return nil, nil
	}
	switch op {
	case "sum":
		return formulaPublicValue(sum), nil
	case "average":
		return formulaPublicValue(new(big.Rat).Quo(sum, new(big.Rat).SetInt64(int64(n)))), nil
	case "min", "max":
		return formulaPublicValue(best), nil
	}
	return nil, fmt.Errorf("unsupported rollup aggregation %q", op)
}
func newDerivedEvaluator(ctx context.Context, k *Kernel, tx *sql.Tx, e contracts.ServiceExecutionContext, at time.Time, zone string) *derivedEvaluator {
	raw := marshal(e)
	var execution contracts.ServiceExecutionContext
	// Both types are the same generated contract. A decode failure remains an
	// invalid context and is rejected on the first authorization check.
	_ = json.Unmarshal(raw, &execution)
	return &derivedEvaluator{ctx: ctx, k: k, tx: tx, execution: execution, executionJSON: string(raw), clock: FormulaClock{at, zone}, active: map[string]bool{}, cache: map[string]DerivedValue{}, properties: map[string]derivedPropertyMetadata{}}
}
func databaseDerived(ctx context.Context, k *Kernel, e contracts.ServiceExecutionContext, req map[string]any) (any, error) {
	collection := str(req["collectionId"])
	tx, err := k.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if err = k.authorizeEntity(ctx, tx, e, "collection", collection); err != nil {
		return nil, err
	}
	at := time.Now().UTC().Truncate(time.Minute)
	if s := str(req["at"]); s != "" {
		at, err = time.Parse(time.RFC3339Nano, s)
		if err != nil {
			return nil, Fail("INVALID_REQUEST", "Invalid clock instant")
		}
	}
	zone := str(req["timezone"])
	if zone == "" {
		zone = "UTC"
	}
	if _, err = time.LoadLocation(zone); err != nil {
		return nil, Fail("INVALID_REQUEST", "Unknown timezone")
	}
	props, err := tx.QueryContext(ctx, "SELECT id FROM properties WHERE collection_id=? AND type IN ('formula','rollup','created_time','last_edited_time','unique_id') ORDER BY id", collection)
	if err != nil {
		return nil, err
	}
	ids := []string{}
	for props.Next() {
		var id string
		props.Scan(&id)
		ids = append(ids, id)
	}
	props.Close()
	rs, err := readRecords(ctx, tx, collection)
	if err != nil {
		return nil, err
	}
	d := newDerivedEvaluator(ctx, k, tx, e, at, zone)
	out := map[string][]DerivedValue{}
	structure := map[string]any{}
	for _, r := range rs {
		if k.authorizeEntity(ctx, tx, e, "record", string(r.Id)) != nil {
			continue
		}
		vs := []DerivedValue{}
		for _, id := range ids {
			v, e := d.value(string(r.Id), id)
			if e != nil {
				return nil, e
			}
			vs = append(vs, v)
		}
		out[string(r.Id)] = vs
		var parent sql.NullString
		_ = tx.QueryRowContext(ctx, "SELECT parent_record_id FROM record_structure WHERE record_id=?", r.Id).Scan(&parent)
		deps := []string{}
		rows, depErr := tx.QueryContext(ctx, "SELECT depends_on_record_id FROM record_dependencies WHERE record_id=?", r.Id)
		if depErr != nil {
			return nil, depErr
		}
		for rows.Next() {
			var id string
			rows.Scan(&id)
			if k.authorizeEntity(ctx, tx, d.execution, "record", id) == nil {
				deps = append(deps, id)
			}
		}
		rows.Close()
		var parentID any
		if parent.Valid && k.authorizeEntity(ctx, tx, e, "record", parent.String) == nil {
			parentID = parent.String
		}
		structure[string(r.Id)] = map[string]any{"parentRecordId": parentID, "dependencyRecordIds": deps}
	}
	var locked int
	_ = tx.QueryRowContext(ctx, "SELECT locked FROM database_settings WHERE collection_id=?", collection).Scan(&locked)
	return map[string]any{"values": out, "structure": structure, "locked": locked == 1, "clock": at.Format(time.RFC3339Nano), "timezone": zone}, nil
}

// Queries calculate from the transaction's current canonical values. No stale
// cache can be mistaken for a live value, including cross-collection rollups.
func enrichDerivedRecord(d *derivedEvaluator, r *contracts.DomainRecord) error {
	rows, err := d.tx.QueryContext(d.ctx, "SELECT id,type FROM properties WHERE collection_id=? AND type IN ('formula','rollup','created_time','last_edited_time','unique_id')", r.CollectionId)
	if err != nil {
		return err
	}
	types := map[string]string{}
	ids := []string{}
	for rows.Next() {
		var id, kind string
		rows.Scan(&id, &kind)
		ids = append(ids, id)
		types[id] = kind
	}
	rows.Close()
	sort.Strings(ids)
	for _, id := range ids {
		v, err := d.value(string(r.Id), id)
		if err != nil {
			return err
		}
		var value any = v.Value
		if types[id] == "formula" || types[id] == "rollup" {
			label := textValue(value)
			if v.Status != "computed" {
				label = "Captured value (not recalculated): " + textValue(v.ImportedCachedValue)
				if v.ImportedCachedValue == nil {
					label = "Formula error: " + v.Diagnostic
				}
			}
			value = map[string]any{"sourceType": "trove." + types[id], "sourceVersion": 1, "reason": "unsupported_behavior", "readableText": label, "raw": v}
		}
		kept := []contracts.DomainPropertyValue{}
		for _, old := range r.Values {
			if string(old.PropertyId) != id {
				kept = append(kept, old)
			}
		}
		r.Values = append(kept, contracts.DomainPropertyValue{PropertyId: contracts.DomainId(id), SchemaVersion: 1, Value: marshal(value)})
	}
	return nil
}
func derivedQueryValue(value any) any {
	if f, ok := value.(map[string]any); ok && strings.HasPrefix(str(f["sourceType"]), "trove.") {
		if raw, ok := f["raw"].(map[string]any); ok {
			if raw["status"] == "computed" {
				return raw["value"]
			}
			return nil
		}
	}
	return value
}

// Reciprocal writes are explicit canonical effects in the same guarded batch.
// Missing guards or target permissions roll back both directions.
func syncReciprocalRelation(m *Mutation, record string, p contracts.DomainPropertyDefinition, value any) error {
	if p.Type != "relation" {
		return nil
	}
	var cfg map[string]any
	json.Unmarshal(p.Config, &cfg)
	back := str(cfg["bidirectionalPropertyId"])
	if back == "" {
		return nil
	}
	if err := m.Require("property", back); err != nil {
		return err
	}
	bp, err := propertyDefinition(m.Context, m.Tx, back)
	if err != nil {
		return err
	}
	var backCfg map[string]any
	json.Unmarshal(bp.Config, &backCfg)
	targets := map[string]bool{}
	var old string
	err = m.Tx.QueryRowContext(m.Context, "SELECT value_json FROM property_values WHERE record_id=? AND property_id=?", record, p.Id).Scan(&old)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	var oldIDs []string
	json.Unmarshal([]byte(old), &oldIDs)
	for _, id := range oldIDs {
		targets[id] = false
	}
	newIDs, _ := value.([]any)
	for _, id := range newIDs {
		targets[str(id)] = true
	}
	for target, linked := range targets {
		if err = m.Require("record", target); err != nil {
			return err
		}
		var page, col string
		if err = m.Tx.QueryRowContext(m.Context, "SELECT page_id,collection_id FROM records WHERE id=?", target).Scan(&page, &col); err != nil {
			return err
		}
		if col != string(bp.CollectionId) {
			return Fail("INVALID_REQUEST", "Reciprocal property belongs to another collection")
		}
		if err = m.TouchPage(page); err != nil {
			return err
		}
		if err = m.Touch("collection", col); err != nil {
			return err
		}
		var raw string
		err = m.Tx.QueryRowContext(m.Context, "SELECT value_json FROM property_values WHERE record_id=? AND property_id=?", target, back).Scan(&raw)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		var ids []string
		json.Unmarshal([]byte(raw), &ids)
		out := []string{}
		for _, id := range ids {
			if id != record {
				out = append(out, id)
			}
		}
		if linked {
			out = append(out, record)
		}
		sort.Strings(out)
		if backCfg["cardinality"] == "one" && len(out) > 1 {
			return Fail("INVALID_REQUEST", "Reciprocal one-to-one relation already has a target")
		}
		if _, err = m.Exec("INSERT INTO property_values(record_id,collection_id,property_id,property_type,schema_version,value_json) VALUES(?,?,?,'relation',1,?) ON CONFLICT(record_id,property_id) DO UPDATE SET value_json=excluded.value_json", target, col, back, string(marshal(out))); err != nil {
			return err
		}
	}
	return nil
}
