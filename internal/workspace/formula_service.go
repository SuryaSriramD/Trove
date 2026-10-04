package workspace

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"
	"trove.local/phase0/contracts"
)

func init() {
	RegisterCommand("property.setFormula", setFormula)
	RegisterQuery("databaseFormulaRegistry", formulaRegistryQuery)
	RegisterQuery("databaseFormulaPreview", formulaPreview)
}
func formulaRegistryQuery(ctx context.Context, k *Kernel, e contracts.ServiceExecutionContext, req map[string]any) (any, error) {
	if err := contracts.Validate("service", "FormulaRegistryRequest", marshal(req)); err != nil {
		return nil, Fail("INVALID_REQUEST", err.Error())
	}
	if err := k.authorize(e); err != nil {
		return nil, err
	}
	return formulaRegistry(), nil
}
func setFormula(m *Mutation, c map[string]any) error {
	id, expression := str(c["propertyId"]), str(c["expression"])
	if err := m.Require("property", id); err != nil {
		return err
	}
	p, err := propertyDefinition(m.Context, m.Tx, id)
	if err != nil {
		return err
	}
	if p.Type != "formula" {
		return Fail("INVALID_REQUEST", "Only formula properties accept formula edits")
	}
	if err = m.Touch("property", id); err != nil {
		return err
	}
	if err = m.Touch("collection", string(p.CollectionId)); err != nil {
		return err
	}
	var owningPage string
	if err = m.Tx.QueryRowContext(m.Context, "SELECT owning_page_id FROM collections WHERE id=?", p.CollectionId).Scan(&owningPage); err != nil {
		return err
	}
	if err = m.TouchPage(owningPage); err != nil {
		return err
	}
	ast, deps, err := formulaCompileV2(expression)
	if err != nil {
		return Fail("INVALID_REQUEST", err.Error())
	}
	for _, dependency := range deps {
		if err = m.Require("property", dependency); err != nil {
			return err
		}
	}
	var cfg map[string]any
	if err = json.Unmarshal(p.Config, &cfg); err != nil {
		return err
	}
	raw, ok := cfg["raw"].(map[string]any)
	if !ok {
		raw = map[string]any{}
		cfg["raw"] = raw
	}
	if _, exists := raw["originalExpression"]; !exists {
		raw["originalExpression"] = expression
	}
	hash := sha256.Sum256([]byte(expression))
	raw["translatedExpression"] = expression
	raw["translatedAST"] = ast
	raw["expressionHash"] = hex.EncodeToString(hash[:])
	raw["evaluatorVersion"] = 2
	raw["editedAt"] = now()
	raw["editedBy"] = string(m.Execution.Actor.ActorId)
	_, err = m.Exec("UPDATE properties SET config_json=? WHERE id=?", string(marshal(cfg)), id)
	return err
}
func formulaPreview(ctx context.Context, k *Kernel, e contracts.ServiceExecutionContext, req map[string]any) (any, error) {
	if err := contracts.Validate("service", "FormulaPreviewRequest", marshal(req)); err != nil {
		return nil, Fail("INVALID_REQUEST", err.Error())
	}
	tx, err := k.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	property, record := str(req["propertyId"]), str(req["recordId"])
	if err = k.authorizeEntity(ctx, tx, e, "property", property); err != nil {
		return nil, err
	}
	if err = k.authorizeEntity(ctx, tx, e, "record", record); err != nil {
		return nil, err
	}
	p, err := propertyDefinition(ctx, tx, property)
	if err != nil {
		return nil, err
	}
	var collection string
	if err = tx.QueryRowContext(ctx, "SELECT collection_id FROM records WHERE id=?", record).Scan(&collection); err != nil {
		return nil, err
	}
	if p.Type != "formula" || collection != string(p.CollectionId) {
		return nil, Fail("INVALID_REQUEST", "Preview needs a formula and sample record from the same database")
	}
	at, err := time.Parse(time.RFC3339Nano, str(req["at"]))
	if err != nil {
		return nil, Fail("INVALID_REQUEST", "Explicit clock must be an RFC3339 instant")
	}
	zone := str(req["timezone"])
	if _, err = time.LoadLocation(zone); err != nil {
		return nil, Fail("INVALID_REQUEST", "Unknown timezone")
	}
	expression := str(req["expression"])
	ast, deps, compileErr := formulaCompileV2(expression)
	out := map[string]any{"version": 2, "status": "error", "value": nil, "resultType": "null", "computedAt": at.UTC().Format(time.RFC3339Nano), "timezone": zone, "dependencies": deps, "translatedAST": ast}
	if compileErr != nil {
		out["diagnostic"] = compileErr.Error()
		return out, nil
	}
	for _, id := range deps {
		dependency, err := propertyDefinition(ctx, tx, id)
		if err != nil || dependency.CollectionId != p.CollectionId {
			return nil, Fail("INVALID_REQUEST", "Referenced property is not in this database")
		}
		if err = k.authorizeEntity(ctx, tx, e, "property", id); err != nil {
			return nil, err
		}
	}
	d := newDerivedEvaluator(ctx, k, tx, e, at, zone)
	d.active[record+":"+property] = true
	value, err := EvaluateFormulaVersion(expression, 2, d.clock, func(id string) (any, error) { return d.formulaInput(record, id, 2) })
	if err != nil {
		out["diagnostic"] = err.Error()
	} else {
		out["status"] = "computed"
		out["value"] = value
		out["resultType"] = formulaV2Type(value)
	}
	if err = k.authorize(e); err != nil {
		return nil, err
	}
	return out, nil
}
func (d *derivedEvaluator) formulaInput(record, id string, version int) (any, error) {
	r, e := d.value(record, id)
	if e != nil {
		return nil, e
	}
	if r.Status != "computed" {
		return nil, fmt.Errorf("dependency %s: %s", id, r.Diagnostic)
	}
	if version != 2 || r.Value == nil {
		return r.Value, nil
	}
	p, e := propertyDefinition(d.ctx, d.tx, id)
	if e != nil {
		return nil, e
	}
	switch p.Type {
	case "text":
		xs, ok := r.Value.([]any)
		if !ok {
			return nil, fmt.Errorf("invalid text property")
		}
		var text strings.Builder
		for _, item := range xs {
			x, ok := item.(map[string]any)
			if !ok {
				return nil, fmt.Errorf("invalid text property")
			}
			if text.Len()+len(str(x["text"])) > 65536 {
				return nil, fmt.Errorf("text exceeds 65536 bytes")
			}
			text.WriteString(str(x["text"]))
		}
		return text.String(), nil
	case "relation":
		xs, ok := r.Value.([]any)
		if !ok || len(xs) > formulaListLimit {
			return nil, fmt.Errorf("relation list exceeds 256 values or is invalid")
		}
		out := []any{}
		for _, raw := range xs {
			d.work++
			limit := d.workLimit
			if limit == 0 {
				limit = 10000
			}
			if d.work > limit {
				d.exhausted = true
				return nil, Fail("RESOURCE_EXHAUSTED", "Derived reference work budget exceeded")
			}
			target := str(raw)
			if e = d.k.authorizeEntity(d.ctx, d.tx, d.execution, "record", target); e != nil {
				return nil, fmt.Errorf("related record is unavailable in this scope")
			}
			var page, title string
			var deleted sql.NullString
			if e = d.tx.QueryRowContext(d.ctx, "SELECT p.id,p.title,p.deleted_at FROM records r JOIN pages p ON p.id=r.page_id WHERE r.id=?", target).Scan(&page, &title, &deleted); e != nil || deleted.Valid {
				return nil, fmt.Errorf("related record is unavailable")
			}
			out = append(out, map[string]any{"kind": "page", "id": page, "recordId": target, "name": title})
		}
		return out, nil
	case "identity":
		xs, ok := r.Value.([]any)
		if !ok || len(xs) > formulaListLimit {
			return nil, fmt.Errorf("person list exceeds 256 values or is invalid")
		}
		out := []any{}
		for _, raw := range xs {
			identity, ok := raw.(map[string]any)
			if !ok {
				return nil, fmt.Errorf("invalid person reference")
			}
			out = append(out, map[string]any{"kind": "person", "id": str(identity["externalId"]), "name": str(identity["displayName"]), "sourceSystem": str(identity["sourceSystem"])})
		}
		return out, nil
	case "author":
		identity, ok := r.Value.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("invalid author reference")
		}
		return map[string]any{"kind": "person", "id": str(identity["externalId"]), "name": str(identity["displayName"]), "sourceSystem": str(identity["sourceSystem"])}, nil
	}
	if formulaV2Type(r.Value) == "unsupported" {
		return nil, fmt.Errorf("property type %s is not a supported formula input", p.Type)
	}
	if xs, ok := r.Value.([]any); ok {
		return formulaV2List(xs)
	}
	return r.Value, nil
}
