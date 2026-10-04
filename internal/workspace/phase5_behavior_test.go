package workspace

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"trove.local/phase0/contracts"
)

func TestPhase5FormulaTypesBlanksClocksAndLimits(t *testing.T) {
	at := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	clock := FormulaClock{At: at, Timezone: "America/Los_Angeles"}
	lookup := func(id string) (any, error) {
		switch id {
		case "blank":
			return nil, nil
		case "list":
			return []any{}, nil
		case "amount":
			return map[string]any{"decimal": "0.0000000000004", "format": "decimal"}, nil
		case "oversized":
			return map[string]any{"decimal": "1" + strings.Repeat("0", 1500), "format": "decimal"}, nil
		default:
			return nil, fmt.Errorf("unknown property")
		}
	}
	// Expected values are independently specified examples of the published v1
	// contract: blanks are distinct from zero/false, and dates use local midnight.
	for _, f := range []struct{ expression, want string }{
		{`empty(prop("blank"))`, "true"}, {`empty(prop("list"))`, "true"},
		{`empty(0)`, "false"}, {`empty(false)`, "false"}, {`empty("")`, "true"},
		{`null == null`, "true"}, {`null == 0`, "false"}, {`"2" == 2`, "false"},
		{`prop("amount") < 0.0000000000005`, "true"}, {`1 / 3`, "0.333333333333"},
		{`round(-0.5)`, "-1"}, {`today()`, "2026-09-30"},
		{`ifElse(empty(prop("blank")), 12, prop("unknown"))`, "12"},
	} {
		t.Run(f.expression, func(t *testing.T) {
			got, err := EvaluateFormula(f.expression, clock, lookup)
			if err != nil || textValue(got) != f.want {
				t.Fatalf("got %v, %v; want %s", got, err, f.want)
			}
		})
	}
	for _, expression := range []string{`prop("blank") + 1`, `1 && true`, `prop(concat("am", "ount"))`, `1e2000`, `1e-2000`, `abs(1e2000)`, `1e999999999`, `prop("oversized")`} {
		t.Run("reject/"+expression, func(t *testing.T) {
			if value, err := EvaluateFormula(expression, clock, lookup); err == nil {
				t.Fatalf("accepted unsupported or unbounded formula %s => %v", expression, value)
			}
		})
	}
	clock.At = at.Add(7 * time.Hour)
	if got, err := EvaluateFormula(`today()`, clock, nil); err != nil || got != "2026-10-01" {
		t.Fatalf("local midnight did not invalidate today: %v %v", got, err)
	}
}

func TestPhase5EditorMergedTablesTabsRestoreCanonicalContent(t *testing.T) {
	k, e := testKernel(t)
	page := createPage(t, k, e, "Advanced recovery")
	tabs, table, paragraph := NewID(), NewID(), NewID()
	left, right := NewID(), NewID()
	cell := func(id string, column int, text string) map[string]any {
		return map[string]any{"id": id, "row": 0, "column": column, "rowSpan": 1, "colSpan": 1, "richText": []any{map[string]any{"text": text, "marks": []any{"bold"}}}, "preMergeCells": []any{}}
	}
	merged := cell(left, 0, "Merged text edited later")
	merged["colSpan"] = 2
	merged["preMergeCells"] = []any{cell(left, 0, "Original עברית 🌱"), cell(right, 1, "Original తెలుగు 中文")}
	tableData := map[string]any{"rowCount": 1, "columnCount": 2, "headerRows": 0, "headerColumns": 0, "columnWidths": []any{120, 180}, "cells": []any{merged}}
	tabsData := map[string]any{"tabs": []any{map[string]any{"id": NewID(), "title": "Evidence", "childBlockIds": []any{table}}, map[string]any{"id": NewID(), "title": "Notes", "childBlockIds": []any{paragraph}}}}
	paragraphData := map[string]any{"richText": []any{map[string]any{"text": "Unrelated P0 paragraph", "marks": []any{"italic"}}}}
	insert := func(id, kind, parent string, data any) any {
		block := map[string]any{"blockId": id, "pageId": page, "type": kind, "schemaVersion": 1, "data": data}
		if parent != "" {
			block["parentBlockId"] = parent
		}
		return map[string]any{"kind": "block.insert", "block": block}
	}
	testOK(t, dbBatch(t, k, e, []any{insert(tabs, "tab_container", "", tabsData), insert(table, "table", tabs, tableData), insert(paragraph, "paragraph", tabs, paragraphData)}, "page", page, "block", tabs, "block", table, "block", paragraph))
	original := readPage(t, k, e, page)
	revision := original["page"].(map[string]any)["revision"].(string)
	testOK(t, dbBatch(t, k, e, []any{map[string]any{"kind": "block.update", "blockId": table, "type": "paragraph", "schemaVersion": 1, "data": paragraphData}}, "page", page, "block", table, "block", tabs))
	testOK(t, restoreTest(t, k, e, page, revision))
	path := k.Path()
	k.Close()
	reopened, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	got := readPage(t, reopened, reopened.OwnerContext(), page)["blocks"].([]any)
	if len(got) != 3 {
		t.Fatalf("restored %d blocks, want 3", len(got))
	}
	want := map[string]map[string]any{tabs: {"type": "tab_container", "data": tabsData}, table: {"type": "table", "data": tableData}, paragraph: {"type": "paragraph", "data": paragraphData}}
	for _, raw := range got {
		block := raw.(map[string]any)
		expected, ok := want[str(block["id"])]
		if !ok || block["type"] != expected["type"] || string(marshal(block["data"])) != string(marshal(expected["data"])) {
			t.Fatalf("canonical identity, marks, merge evidence, or tab membership changed: %s", marshal(block))
		}
		if block["id"] != tabs && block["parentBlockId"] != tabs {
			t.Fatalf("nested ownership changed: %v", block)
		}
	}
}

func TestPhase5FormulaRelationScopeAndEmptyAggregation(t *testing.T) {
	k, e := testKernel(t)
	f := newDatabase(t, k, e)
	a, ap := newRecord(t, k, e, f, "Visible")
	b, bp := newRecord(t, k, e, f, "Private target")
	number, relation, rollup := NewID(), NewID(), NewID()
	for _, p := range []struct {
		id, kind string
		config   any
	}{
		{number, "number", map[string]any{"format": "decimal"}},
		{relation, "relation", map[string]any{"targetCollectionId": f.collection, "bidirectionalPropertyId": nil}},
		{rollup, "rollup", derivedConfig("rollup", map[string]any{"relationPropertyId": relation, "targetPropertyId": number, "aggregation": "average"})},
	} {
		testOK(t, dbBatch(t, k, e, []any{map[string]any{"kind": "collection.addProperty", "collectionId": f.collection, "propertyId": p.id, "name": p.kind, "type": p.kind, "schemaVersion": 1, "config": p.config}}, "collection", f.collection, "property", p.id))
	}
	testOK(t, setValue(t, k, e, f, b, bp, number, "number", map[string]any{"decimal": "7654321.25", "format": "decimal"}))
	derive := func(execution contracts.ServiceExecutionContext) DerivedValue {
		t.Helper()
		tx, err := k.db.BeginTx(context.Background(), nil)
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback()
		value, err := newDerivedEvaluator(context.Background(), k, tx, execution, time.Date(2026, 9, 30, 0, 0, 0, 0, time.UTC), "UTC").value(a, rollup)
		if err != nil {
			t.Fatal(err)
		}
		return value
	}
	if value := derive(e); value.Status != "computed" || value.Value != nil {
		t.Fatalf("empty average must stay empty: %#v", value)
	}
	testOK(t, dbBatch(t, k, e, []any{map[string]any{"kind": "record.setValue", "recordId": a, "propertyId": relation, "value": map[string]any{"type": "relation", "schemaVersion": 1, "value": []string{b}}}}, "page", ap, "record", a, "record", b, "collection", f.collection, "property", relation))
	if value := derive(e); textValue(value.Value) != "7654321.25" {
		t.Fatalf("owner rollup: %#v", value)
	}
	scoped := k.Grant(e.Actor, "user", contracts.DomainScope{Value: contracts.DomainScopeVariant3{Kind: "pages", WorkspaceId: contracts.DomainId(k.ID()), PageIds: []contracts.DomainId{contracts.DomainId(ap)}}})
	value := derive(scoped)
	if value.Status != "error" || value.Value != nil || value.ComputedAt != "" || !strings.Contains(value.Diagnostic, "unavailable") || strings.Contains(string(marshal(value)), "7654321") {
		t.Fatalf("rollup leaked or fabricated a scoped value: %#v", value)
	}
}

func TestPhase5AutomationHalfHourDSTUsesFirstOccurrence(t *testing.T) {
	// Lord Howe's autumn clock repeats 01:30–02:00 by thirty minutes. The
	// first 01:45 is 14:45Z; the later repeated 01:45 must never become another run.
	trigger := AutomationTrigger{Timezone: "Australia/Lord_Howe", Frequency: "daily", Hour: 1, Minute: 45}
	for _, f := range []struct{ after, want string }{
		{"2026-04-04T12:00:00Z", "2026-04-04T14:45:00Z"},
		{"2026-04-04T14:45:00Z", "2026-04-05T15:15:00Z"},
	} {
		after, err := time.Parse(time.RFC3339, f.after)
		if err != nil {
			t.Fatal(err)
		}
		got, err := NextAutomationOccurrence(trigger, after)
		if err != nil || got.Format(time.RFC3339) != f.want {
			t.Fatalf("after %s: %s %v; want %s", f.after, got, err, f.want)
		}
	}
	k, e := testKernel(t)
	page := createPage(t, k, e, "DST rule")
	rule := scheduleRule(page)
	rule.Trigger.Timezone = trigger.Timezone
	rule.Trigger.Hour, rule.Trigger.Minute = trigger.Hour, trigger.Minute
	saveRule(t, k, e, page, rule)
	stored, err := loadAutomation(context.Background(), k.db, rule.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, instant := range []string{"2026-04-04T14:45:00Z", "2026-04-04T15:15:00Z", "2026-04-04T14:45:00.000000001Z"} {
		at, err := time.Parse(time.RFC3339Nano, instant)
		if err != nil {
			t.Fatal(err)
		}
		if err = k.executeAutomationOccurrence(context.Background(), e, stored, "", "schedule:"+instant, at); err != nil {
			t.Fatal(err)
		}
	}
	var count int
	if err = k.db.QueryRow("SELECT count(*) FROM pages WHERE parent_page_id=?", page).Scan(&count); err != nil || count != 1 {
		t.Fatalf("repeated or fractional wall time duplicated a local effect: %d %v", count, err)
	}
}

func TestPhase5AutomationSkippedConditionNeverReplaysAfterRestart(t *testing.T) {
	k, e := testKernel(t)
	f := newDatabase(t, k, e)
	record, recordPage := newRecord(t, k, e, f, "Condition source")
	rulePage := createPage(t, k, e, "Rules")
	rule := scheduleRule(rulePage)
	rule.Condition = &AutomationCondition{RecordID: record, PropertyID: f.status, Op: "eq", Value: f.done}
	saveRule(t, k, e, rulePage, rule)
	stored, err := loadAutomation(context.Background(), k.db, rule.ID)
	if err != nil {
		t.Fatal(err)
	}
	at := time.Date(2026, 10, 2, 4, 0, 0, 0, time.UTC)
	key := "schedule:" + at.Format(time.RFC3339Nano)
	if err = k.executeAutomationOccurrence(context.Background(), e, stored, "", key, at); err != nil {
		t.Fatal(err)
	}
	testOK(t, setValue(t, k, e, f, record, recordPage, f.status, "status", f.done))
	path := k.Path()
	k.Close()
	k, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer k.Close()
	if err = k.executeAutomationOccurrence(context.Background(), k.OwnerContext(), stored, "", key, at); err != nil {
		t.Fatal(err)
	}
	var status string
	if err = k.db.QueryRow("SELECT status FROM automation_runs WHERE rule_id=? AND occurrence_key=?", rule.ID, key).Scan(&status); err != nil || status != "skipped" {
		t.Fatalf("skipped receipt changed: %s %v", status, err)
	}
	var count int
	if err = k.db.QueryRow("SELECT count(*) FROM pages WHERE parent_page_id=?", rulePage).Scan(&count); err != nil || count != 0 {
		t.Fatalf("skipped occurrence produced effects: %d %v", count, err)
	}
}
