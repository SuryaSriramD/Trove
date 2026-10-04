package workspace

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"sort"

	"trove.local/phase0/contracts"
)

type historicalValueDecision struct {
	SourcePropertyID string          `json:"sourcePropertyId"`
	Action           string          `json:"action"`
	TargetPropertyID string          `json:"targetPropertyId,omitempty"`
	Value            json.RawMessage `json:"value,omitempty"`
}

type historicalValueIssue struct {
	HistoricalValue    contracts.DomainPropertyValue       `json:"historicalValue"`
	HistoricalProperty *contracts.DomainPropertyDefinition `json:"historicalProperty,omitempty"`
	CurrentProperty    *contracts.DomainPropertyDefinition `json:"currentProperty,omitempty"`
	Reason             string                              `json:"reason"`
}

func historicalMapType(kind string) bool {
	switch kind {
	case "text", "number", "select", "multi_select", "status", "checkbox", "date", "url", "email", "phone", "files", "relation", "identity", "place":
		return true
	}
	return false
}

// resolveHistoricalValues never rewrites a property schema or guesses a value
// conversion. Explicit decisions produce a fresh, guarded preview. Retention
// keeps the current typed value, while the old value remains inert provenance.
func resolveHistoricalValues(ctx context.Context, q querier, original recoverySnapshot, decisions []historicalValueDecision) (snap recoverySnapshot, issues []historicalValueIssue, targets []contracts.DomainPropertyDefinition, ready bool, err error) {
	issues = []historicalValueIssue{}
	targets = []contracts.DomainPropertyDefinition{}
	if err = json.Unmarshal(marshal(original), &snap); err != nil {
		return
	}
	if snap.Record == nil {
		if len(decisions) != 0 {
			err = Fail("INVALID_REQUEST", "This page has no historical property values to map")
		}
		ready = err == nil
		return
	}
	if len(decisions) > 256 || len(snap.Record.Values) > 256 || len(marshal(decisions)) > 1<<20 {
		err = Fail("RESOURCE_EXHAUSTED", "Property recovery is limited to 256 values and 1 MiB per preview")
		return
	}
	current := map[string]contracts.DomainPropertyDefinition{}
	rows, queryErr := q.QueryContext(ctx, "SELECT id FROM properties WHERE collection_id=? ORDER BY id", snap.Record.CollectionId)
	if queryErr != nil {
		err = queryErr
		return
	}
	ids := []string{}
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return
		}
		ids = append(ids, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return
	}
	if len(ids) > 256 {
		err = Fail("RESOURCE_EXHAUSTED", "Property recovery is limited to 256 current properties")
		return
	}
	for _, id := range ids {
		var p contracts.DomainPropertyDefinition
		p, err = propertyDefinition(ctx, q, id)
		if err != nil {
			return
		}
		current[id] = p
	}
	previous := map[string]contracts.DomainPropertyDefinition{}
	for _, p := range original.Properties {
		previous[string(p.Id)] = p
	}
	compatible := map[string]contracts.DomainPropertyValue{}
	historicalIDs := map[string]bool{}
	for _, value := range original.Record.Values {
		id := string(value.PropertyId)
		historicalIDs[id] = true
		one := original
		record := *original.Record
		record.Values = []contracts.DomainPropertyValue{value}
		one.Record = &record
		if problem := checkHistoricalProperties(ctx, q, one); problem != nil {
			var failure *ServiceFailure
			if !errors.As(problem, &failure) || failure.Code != "CONFLICT" {
				err = problem
				return
			}
			issue := historicalValueIssue{HistoricalValue: value, Reason: failure.Message}
			if p, ok := previous[id]; ok {
				copy := p
				issue.HistoricalProperty = &copy
			}
			if p, ok := current[id]; ok {
				copy := p
				issue.CurrentProperty = &copy
			}
			issues = append(issues, issue)
		} else {
			compatible[id] = value
		}
	}
	byID := map[string]historicalValueDecision{}
	for _, id := range ids {
		p := current[id]
		if p.SchemaVersion == 1 && historicalMapType(p.Type) {
			if _, occupied := compatible[id]; !occupied {
				targets = append(targets, p)
			}
		}
	}
	for _, decision := range decisions {
		if _, duplicate := byID[decision.SourcePropertyID]; duplicate {
			err = Fail("INVALID_REQUEST", "Duplicate historical property decision")
			return
		}
		known := false
		for _, issue := range issues {
			if string(issue.HistoricalValue.PropertyId) == decision.SourcePropertyID {
				known = true
			}
		}
		if !known {
			err = Fail("INVALID_REQUEST", "Decision does not identify an incompatible historical property")
			return
		}
		if decision.Action != "retain" && decision.Action != "map" {
			err = Fail("INVALID_REQUEST", "Unknown historical value decision")
			return
		}
		byID[decision.SourcePropertyID] = decision
	}
	currentRecord, captureErr := captureOwnedRecord(ctx, q, string(original.Page.Id))
	if captureErr != nil {
		err = captureErr
		return
	}
	currentValues := map[string]contracts.DomainPropertyValue{}
	for _, value := range currentRecord.Values {
		currentValues[string(value.PropertyId)] = value
	}
	ready = true
	mapped := map[string]bool{}
	retained := []any{}
	for _, issue := range issues {
		id := string(issue.HistoricalValue.PropertyId)
		decision, reviewed := byID[id]
		if !reviewed {
			ready = false
		}
		// Preserving an incompatible value as evidence must not erase the current
		// typed value. A map to another property also leaves that current value.
		if value, ok := currentValues[id]; ok && reviewed && (decision.Action == "retain" || decision.TargetPropertyID != id) {
			compatible[id] = value
		}
		if !reviewed {
			continue
		}
		if decision.Action == "retain" {
			retained = append(retained, map[string]any{"sourceRevision": original.Page.Revision, "historicalValue": issue.HistoricalValue, "historicalProperty": issue.HistoricalProperty, "reason": issue.Reason, "policy": "retained evidence only; not an active typed property"})
			continue
		}
		target, ok := current[decision.TargetPropertyID]
		if !ok || target.SchemaVersion != 1 || !historicalMapType(target.Type) {
			err = Fail("INVALID_REQUEST", "Map to an editable current property in this collection")
			return
		}
		if mapped[decision.TargetPropertyID] || (historicalIDs[decision.TargetPropertyID] && decision.TargetPropertyID != id) {
			err = Fail("INVALID_REQUEST", "Mapping would overwrite another historical property; choose a separate current property")
			return
		}
		if len(decision.Value) == 0 {
			err = Fail("INVALID_REQUEST", "Mapping requires an explicit current typed value")
			return
		}
		if err = contracts.Validate("domain", "property_"+target.Type+"_v1", decision.Value); err != nil {
			err = Fail("INVALID_REQUEST", "Mapped value does not match the current property type")
			return
		}
		mapped[decision.TargetPropertyID] = true
		compatible[decision.TargetPropertyID] = contracts.DomainPropertyValue{PropertyId: target.Id, SchemaVersion: target.SchemaVersion, Value: decision.Value}
	}
	snap.Record.Values = []contracts.DomainPropertyValue{}
	snap.Properties = []contracts.DomainPropertyDefinition{}
	ids = ids[:0]
	for id := range compatible {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		p, exists := current[id]
		if !exists {
			err = Fail("CONFLICT", "Current retained property no longer exists")
			return
		}
		snap.Record.Values = append(snap.Record.Values, compatible[id])
		snap.Properties = append(snap.Properties, p)
	}
	if len(retained) > 0 {
		if snap.Page.Provenance == nil {
			snap.Page.Provenance = &contracts.DomainProvenance{SourceSystem: "trove-history"}
		}
		raw := map[string]any{}
		if snap.Page.Provenance.Raw != nil {
			var supplied any
			if err = json.Unmarshal(*snap.Page.Provenance.Raw, &supplied); err != nil {
				return
			}
			if object, ok := supplied.(map[string]any); ok {
				raw = object
			} else {
				raw["supplied"] = supplied
			}
		}
		// Preserve a non-object raw provenance value without interpreting it.
		prior, _ := raw["retainedHistoricalValues"].([]any)
		if len(prior)+len(retained) > 256 {
			err = Fail("RESOURCE_EXHAUSTED", "Retained historical value evidence exceeds 256 items")
			return
		}
		raw["retainedHistoricalValues"] = append(prior, retained...)
		encoded := marshal(raw)
		if len(encoded) > 1<<20 {
			err = Fail("RESOURCE_EXHAUSTED", "Retained historical value evidence exceeds 1 MiB")
			return
		}
		snap.Page.Provenance.Raw = &encoded
	}
	return
}

func validateRestoreValues(ctx context.Context, k *Kernel, tx *sql.Tx, e contracts.ServiceExecutionContext, snap recoverySnapshot, guards []contracts.ServiceExpectedRevision) error {
	if snap.Record == nil {
		return nil
	}
	m := &Mutation{Context: ctx, Kernel: k, Tx: tx, Execution: e, guards: map[string]string{}}
	for _, guard := range guards {
		m.guards[guardKey(guard.Target.EntityType, string(guard.Target.EntityId))] = string(guard.Revision)
	}
	for _, value := range snap.Record.Values {
		p, err := propertyDefinition(ctx, tx, string(value.PropertyId))
		if err != nil {
			return err
		}
		if p.Type == "title" {
			continue
		}
		var decoded any
		if err = json.Unmarshal(value.Value, &decoded); err != nil {
			return err
		}
		if err = validatePropertyValue(m, p, decoded); err != nil {
			return err
		}
	}
	return nil
}
