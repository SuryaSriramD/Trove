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

// P2DatabaseConfig is guarded by its collection revision. These optional local
// interfaces never create a second copy of canonical record data.
type P2DatabaseConfig struct {
	Chart   P2ChartConfig `json:"chart"`
	Form    P2FormConfig  `json:"form"`
	Buttons []P2RowButton `json:"buttons"`
}
type P2ChartConfig struct {
	GroupPropertyID *string `json:"groupPropertyId"`
	ValuePropertyID *string `json:"valuePropertyId"`
	Aggregation     string  `json:"aggregation"`
	Visualization   string  `json:"visualization"`
}
type P2FormConfig struct {
	Enabled             bool     `json:"enabled"`
	RequiredPropertyIDs []string `json:"requiredPropertyIds"`
}
type P2RowButton struct {
	ID                   string         `json:"id"`
	Label                string         `json:"label"`
	PropertyID           string         `json:"propertyId"`
	Value                map[string]any `json:"value"`
	ConfirmationRequired bool           `json:"confirmationRequired"`
}

func init() {
	RegisterInitializer(func(ctx context.Context, k *Kernel) error {
		return k.write(ctx, func(tx *sql.Tx) error {
			_, err := tx.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS p2_database_config(collection_id TEXT PRIMARY KEY REFERENCES collections(id) ON DELETE CASCADE,config_json TEXT NOT NULL); CREATE TABLE IF NOT EXISTS p2_form_submissions(record_id TEXT PRIMARY KEY REFERENCES records(id) ON DELETE CASCADE,collection_revision TEXT NOT NULL,submitted_at TEXT NOT NULL);`)
			return err
		})
	})
	for _, kind := range []string{"database.p2Configure", "database.formSubmit", "database.rowButtonRun"} {
		RegisterCommand(kind, p2DatabaseCommand)
	}
	RegisterQuery("databaseP2", p2DatabaseQuery)
	RegisterQuery("databaseChart", p2DatabaseChart)
}
func defaultP2Config() P2DatabaseConfig {
	return P2DatabaseConfig{Chart: P2ChartConfig{Aggregation: "count", Visualization: "bar"}, Form: P2FormConfig{RequiredPropertyIDs: []string{}}, Buttons: []P2RowButton{}}
}
func readP2DatabaseConfig(ctx context.Context, q querier, collection string) (P2DatabaseConfig, error) {
	c := defaultP2Config()
	var raw string
	err := q.QueryRowContext(ctx, "SELECT config_json FROM p2_database_config WHERE collection_id=?", collection).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return c, nil
	}
	if err != nil {
		return c, err
	}
	err = json.Unmarshal([]byte(raw), &c)
	return c, err
}
func p2FormType(kind string) bool {
	switch kind {
	case "text", "number", "select", "multi_select", "status", "checkbox", "date", "url", "email", "phone":
		return true
	}
	return false
}
func p2CollectionProperty(ctx context.Context, q querier, collection, id string) (contracts.DomainPropertyDefinition, error) {
	p, err := propertyDefinition(ctx, q, id)
	if err == nil && string(p.CollectionId) != collection {
		err = Fail("INVALID_REQUEST", "Property belongs to another collection")
	}
	return p, err
}
func validateP2Config(m *Mutation, collection string, c P2DatabaseConfig) error {
	if len(marshal(c)) > 65536 || len(c.Buttons) > 16 || len(c.Form.RequiredPropertyIDs) > 64 {
		return Fail("RESOURCE_EXHAUSTED", "Configuration exceeds 64 KiB, 16 buttons, or 64 required fields")
	}
	switch c.Chart.Aggregation {
	case "count", "sum", "average", "min", "max":
	default:
		return Fail("INVALID_REQUEST", "Unknown chart aggregation")
	}
	if c.Chart.Visualization != "bar" && c.Chart.Visualization != "donut" {
		return Fail("INVALID_REQUEST", "Unknown chart visualization")
	}
	if c.Chart.GroupPropertyID != nil {
		p, err := p2CollectionProperty(m.Context, m.Tx, collection, *c.Chart.GroupPropertyID)
		if err != nil {
			return err
		}
		switch p.Type {
		case "select", "status", "checkbox", "text", "title", "date":
		default:
			return Fail("INVALID_REQUEST", "Charts group by title, text, select, status, checkbox, or date")
		}
	}
	if c.Chart.Aggregation != "count" && c.Chart.ValuePropertyID == nil {
		return Fail("INVALID_REQUEST", "Numeric aggregation needs a value property")
	}
	if c.Chart.ValuePropertyID != nil {
		p, err := p2CollectionProperty(m.Context, m.Tx, collection, *c.Chart.ValuePropertyID)
		if err != nil {
			return err
		}
		if p.Type != "number" && p.Type != "formula" && p.Type != "rollup" {
			return Fail("INVALID_REQUEST", "Chart values require a numeric, formula, or rollup property")
		}
	}
	seen := map[string]bool{}
	for _, id := range c.Form.RequiredPropertyIDs {
		if seen[id] {
			return Fail("INVALID_REQUEST", "Duplicate required form property")
		}
		seen[id] = true
		p, err := p2CollectionProperty(m.Context, m.Tx, collection, id)
		if err != nil {
			return err
		}
		if !p2FormType(p.Type) {
			return Fail("INVALID_REQUEST", "Required form field is not an editable local field")
		}
	}
	seen = map[string]bool{}
	for _, b := range c.Buttons {
		if contracts.Validate("domain", "Id", marshal(b.ID)) != nil || seen[b.ID] || strings.TrimSpace(b.Label) == "" || len(b.Label) > 120 {
			return Fail("INVALID_REQUEST", "Buttons need unique IDs and labels up to 120 bytes")
		}
		seen[b.ID] = true
		p, err := p2CollectionProperty(m.Context, m.Tx, collection, b.PropertyID)
		if err != nil {
			return err
		}
		if !p2FormType(p.Type) {
			return Fail("INVALID_REQUEST", "A row button may only set an editable scalar field on its own row")
		}
		if str(b.Value["type"]) != p.Type || number(b.Value["schemaVersion"]) != p.SchemaVersion {
			return Fail("INVALID_REQUEST", "Button value type or version does not match its property")
		}
		if err = validatePropertyValue(m, p, b.Value["value"]); err != nil {
			return err
		}
	}
	return nil
}
func p2DatabaseCommand(m *Mutation, c map[string]any) error {
	collection := str(c["collectionId"])
	if err := m.Touch("collection", collection); err != nil {
		return err
	}
	var exists string
	if err := m.Tx.QueryRowContext(m.Context, "SELECT id FROM collections WHERE id=?", collection).Scan(&exists); err != nil {
		return err
	}
	if c["kind"] == "database.p2Configure" {
		var config P2DatabaseConfig
		if err := json.Unmarshal(marshal(c["config"]), &config); err != nil {
			return Fail("INVALID_REQUEST", "Invalid database configuration")
		}
		if err := validateP2Config(m, collection, config); err != nil {
			return err
		}
		_, err := m.Exec("INSERT INTO p2_database_config VALUES(?,?) ON CONFLICT(collection_id) DO UPDATE SET config_json=excluded.config_json", collection, string(marshal(config)))
		return err
	}
	config, err := readP2DatabaseConfig(m.Context, m.Tx, collection)
	if err != nil {
		return err
	}
	if c["kind"] == "database.formSubmit" {
		if !config.Form.Enabled {
			return Fail("SCOPE_DENIED", "Local form is disabled")
		}
		if strings.TrimSpace(str(c["title"])) == "" {
			return Fail("INVALID_REQUEST", "A title is required")
		}
		values, ok := c["values"].([]any)
		if !ok || len(values) > 64 {
			return Fail("RESOURCE_EXHAUSTED", "A local form accepts at most 64 fields")
		}
		seen := map[string]bool{}
		for _, raw := range values {
			field, ok := raw.(map[string]any)
			if !ok {
				return Fail("INVALID_REQUEST", "Malformed form field")
			}
			id := str(field["propertyId"])
			if seen[id] {
				return Fail("INVALID_REQUEST", "Duplicate form field")
			}
			seen[id] = true
			p, err := p2CollectionProperty(m.Context, m.Tx, collection, id)
			if err != nil {
				return err
			}
			if !p2FormType(p.Type) {
				return Fail("INVALID_REQUEST", "Unsupported local form field")
			}
			input, ok := field["value"].(map[string]any)
			if !ok {
				return Fail("INVALID_REQUEST", "Malformed field value")
			}
			for _, required := range config.Form.RequiredPropertyIDs {
				if required == id && p2Empty(input["value"]) {
					return Fail("INVALID_REQUEST", "Required field is empty: "+p.Name)
				}
			}
		}
		for _, id := range config.Form.RequiredPropertyIDs {
			if !seen[id] {
				return Fail("INVALID_REQUEST", "A required form field is missing")
			}
		}
		if err = createRecord(m, c); err != nil {
			return err
		}
		for _, raw := range values {
			f := raw.(map[string]any)
			if err = setRecordValue(m, str(c["recordId"]), str(f["propertyId"]), f["value"].(map[string]any)); err != nil {
				return err
			}
		}
		_, err = m.Exec("INSERT INTO p2_form_submissions VALUES(?,?,?)", c["recordId"], m.GuardRevision("collection", collection), now())
		return err
	}
	var rowCollection string
	if err = m.Tx.QueryRowContext(m.Context, "SELECT collection_id FROM records WHERE id=?", c["recordId"]).Scan(&rowCollection); err != nil {
		return err
	}
	if rowCollection != collection {
		return Fail("SCOPE_DENIED", "Button cannot act outside its own collection")
	}
	for _, b := range config.Buttons {
		if b.ID == str(c["buttonId"]) {
			if b.ConfirmationRequired && c["confirmed"] != true {
				return Fail("APPROVAL_REQUIRED", "Confirm the displayed row action before running it")
			}
			id := str(c["operationId"])
			if id == "" {
				id = NewID()
			}
			return applyJournal(m, id, "Row action: "+b.Label, []map[string]any{{"kind": "record.setValue", "recordId": c["recordId"], "propertyId": b.PropertyID, "value": b.Value}})
		}
	}
	return Fail("NOT_FOUND", "Row button is unavailable")
}
func p2Empty(v any) bool {
	if v == nil {
		return true
	}
	switch x := v.(type) {
	case string:
		return strings.TrimSpace(x) == ""
	case []any:
		return len(x) == 0 || strings.TrimSpace(searchReadable(x)) == ""
	case map[string]any:
		if d, ok := x["decimal"]; ok {
			return str(d) == ""
		}
	}
	return false
}
func p2DatabaseQuery(ctx context.Context, k *Kernel, e contracts.ServiceExecutionContext, r map[string]any) (any, error) {
	tx, err := k.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	collection := str(r["collectionId"])
	if err = k.authorizeEntity(ctx, tx, e, "collection", collection); err != nil {
		return nil, err
	}
	config, err := readP2DatabaseConfig(ctx, tx, collection)
	if err != nil {
		return nil, err
	}
	var revision string
	if err = tx.QueryRowContext(ctx, "SELECT revision FROM collections WHERE id=?", collection).Scan(&revision); err != nil {
		return nil, err
	}
	return map[string]any{"collectionId": collection, "revision": revision, "config": config, "representation": p2ConfigText(config), "limits": map[string]any{"chartRecords": 10000, "chartGroups": 100, "formFields": 64, "rowButtons": 16, "averageDecimalPlaces": 12}}, nil
}
func p2ConfigText(c P2DatabaseConfig) string {
	labels := []string{}
	for _, b := range c.Buttons {
		labels = append(labels, b.Label)
	}
	return fmt.Sprintf("Local chart: %s (%s). Local form enabled: %t; required field IDs: %s. Row actions: %s. Actions set one field on the selected record only.", c.Chart.Aggregation, c.Chart.Visualization, c.Form.Enabled, strings.Join(c.Form.RequiredPropertyIDs, ", "), strings.Join(labels, ", "))
}

type p2ChartGroup struct {
	Key           string   `json:"key"`
	Label         string   `json:"label"`
	Decimal       *string  `json:"decimal"`
	ExactFraction *string  `json:"exactFraction"`
	RecordIDs     []string `json:"recordIds"`
	NumericCount  int      `json:"numericCount"`
	sum           *big.Rat
	best          *big.Rat
	scale         int
}

func p2DatabaseChart(ctx context.Context, k *Kernel, e contracts.ServiceExecutionContext, r map[string]any) (any, error) {
	tx, err := k.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	collection := str(r["collectionId"])
	if err = k.authorizeEntity(ctx, tx, e, "collection", collection); err != nil {
		return nil, err
	}
	config, err := readP2DatabaseConfig(ctx, tx, collection)
	if err != nil {
		return nil, err
	}
	var revision string
	var count int
	if err = tx.QueryRowContext(ctx, "SELECT revision FROM collections WHERE id=?", collection).Scan(&revision); err != nil {
		return nil, err
	}
	if err = tx.QueryRowContext(ctx, "SELECT count(*) FROM records r JOIN pages p ON p.id=r.page_id WHERE r.collection_id=? AND p.deleted_at IS NULL", collection).Scan(&count); err != nil {
		return nil, err
	}
	if count > 10000 {
		return nil, Fail("RESOURCE_EXHAUSTED", "Chart exceeds 10000 records; split the collection before aggregating")
	}
	rows, err := tx.QueryContext(ctx, "SELECT r.id FROM records r JOIN pages p ON p.id=r.page_id WHERE r.collection_id=? AND p.deleted_at IS NULL ORDER BY r.id", collection)
	if err != nil {
		return nil, err
	}
	recordIDs := []string{}
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return nil, err
		}
		recordIDs = append(recordIDs, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	evaluator := newDerivedEvaluator(ctx, k, tx, e, time.Now().UTC().Truncate(time.Minute), "UTC")
	groups := map[string]*p2ChartGroup{}
	authorized := 0
	for _, id := range recordIDs {
		if err = ctx.Err(); err != nil {
			return nil, err
		}
		if k.authorizeEntity(ctx, tx, e, "record", id) != nil {
			continue
		}
		authorized++
		key, label := "all", "All records"
		if config.Chart.GroupPropertyID != nil {
			value, err := evaluator.value(id, *config.Chart.GroupPropertyID)
			if err != nil {
				return nil, err
			}
			if value.Status != "computed" {
				return nil, Fail("INVALID_REQUEST", "Chart group could not be computed")
			}
			key = string(marshal(value.Value))
			label = textValue(value.Value)
			if value.Value == nil || label == "" {
				label = "Empty"
			}
			var option string
			if tx.QueryRowContext(ctx, "SELECT label FROM property_options WHERE id=? AND property_id=?", str(value.Value), *config.Chart.GroupPropertyID).Scan(&option) == nil {
				label = option
			}
			if d, ok := value.Value.(map[string]any); ok && d["start"] != nil {
				label = str(d["start"])
			}
		}
		group := groups[key]
		if group == nil {
			if len(groups) >= 100 {
				return nil, Fail("RESOURCE_EXHAUSTED", "Chart exceeds 100 groups; choose a lower-cardinality property")
			}
			group = &p2ChartGroup{Key: key, Label: label, RecordIDs: []string{}, sum: new(big.Rat)}
			groups[key] = group
		}
		group.RecordIDs = append(group.RecordIDs, id)
		if config.Chart.Aggregation != "count" {
			if config.Chart.ValuePropertyID == nil {
				return nil, Fail("INVALID_REQUEST", "Chart needs a numeric property")
			}
			value, err := evaluator.value(id, *config.Chart.ValuePropertyID)
			if err != nil {
				return nil, err
			}
			if value.Status != "computed" {
				return nil, Fail("INVALID_REQUEST", "Chart cannot aggregate captured or failed derived values")
			}
			if value.Value == nil {
				continue
			}
			n, err := formulaNumber(value.Value)
			if err != nil {
				return nil, Fail("INVALID_REQUEST", "Chart contains a nonnumeric derived value")
			}
			group.NumericCount++
			group.sum.Add(group.sum, n)
			if v, ok := value.Value.(map[string]any); ok {
				if decimal, ok := v["decimal"].(string); ok {
					if i := strings.IndexByte(decimal, '.'); i >= 0 {
						group.scale = max(group.scale, len(decimal)-i-1)
					}
				}
			}
			if group.best == nil || config.Chart.Aggregation == "min" && n.Cmp(group.best) < 0 || config.Chart.Aggregation == "max" && n.Cmp(group.best) > 0 {
				group.best = new(big.Rat).Set(n)
			}
		}
	}
	result := []*p2ChartGroup{}
	for _, g := range groups {
		var n *big.Rat
		switch config.Chart.Aggregation {
		case "count":
			n = new(big.Rat).SetInt64(int64(len(g.RecordIDs)))
		case "sum":
			if g.NumericCount > 0 {
				n = g.sum
			}
		case "average":
			if g.NumericCount > 0 {
				n = new(big.Rat).Quo(g.sum, new(big.Rat).SetInt64(int64(g.NumericCount)))
				g.scale = 12
			}
		case "min", "max":
			n = g.best
		}
		if n != nil {
			decimal := n.FloatString(g.scale)
			if strings.Contains(decimal, ".") {
				decimal = strings.TrimRight(strings.TrimRight(decimal, "0"), ".")
			}
			fraction := n.RatString()
			g.Decimal = &decimal
			g.ExactFraction = &fraction
		}
		result = append(result, g)
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Label == result[j].Label {
			return result[i].Key < result[j].Key
		}
		return result[i].Label < result[j].Label
	})
	return map[string]any{"collectionId": collection, "revision": revision, "config": config.Chart, "groups": result, "recordCount": authorized, "representation": "Calculated from current authorized, non-trashed canonical records. Numeric nulls are excluded; averages display 12 decimal places and preserve exactFraction.", "averageDecimalPlaces": 12}, nil
}
