package workspace

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
	"trove.local/phase0/contracts"
)

func recurringTemplateAction(t *testing.T, k *Kernel, e contracts.ServiceExecutionContext, id, col string) map[string]any {
	t.Helper()
	_, hash, err := loadUnifiedTemplate(context.Background(), k, k.db, e, id)
	if err != nil {
		t.Fatal(err)
	}
	a := map[string]any{"kind": "template.instantiate", "templateId": id, "templateHash": hash, "title": "Weekly {{occurrence}}"}
	if col != "" {
		a["collectionId"] = col
	}
	return a
}
func automationPreviewAt(t *testing.T, k *Kernel, e contracts.ServiceExecutionContext, page, rule, at string) map[string]any {
	t.Helper()
	return testOK(t, testDispatch(t, k, e, "automationPreview", advancedRequest(k, map[string]any{"pageId": page, "ruleId": rule, "at": at})))
}
func TestAutomationUnifiedTemplatePreviewDeterminismAndReopen(t *testing.T) {
	k, e := testKernel(t)
	f := newDatabase(t, k, e)
	record, page := newRecord(t, k, e, f, "Rules and source")
	testOK(t, setValue(t, k, e, f, record, page, f.status, "status", f.open))
	table, cell := NewID(), NewID()
	data := map[string]any{"rowCount": 1, "columnCount": 1, "headerRows": 1, "headerColumns": 0, "columnWidths": []any{245}, "cells": []any{map[string]any{"id": cell, "row": 0, "column": 0, "rowSpan": 1, "colSpan": 1, "richText": []any{map[string]any{"text": "Header", "marks": []string{"bold"}}}, "preMergeCells": []any{}}}}
	testOK(t, dbBatch(t, k, e, []any{map[string]any{"kind": "block.insert", "block": map[string]any{"blockId": table, "pageId": page, "type": "table", "schemaVersion": 1, "data": data}}}, "page", page, "record", record, "collection", f.collection, "block", table))
	template := savedTemplate(t, k, e, page)
	r := scheduleRule(page)
	r.Trigger.Frequency = "weekly"
	r.Trigger.Weekday = 1
	r.Actions = []map[string]any{recurringTemplateAction(t, k, e, template, f.collection)}
	testOK(t, dbBatch(t, k, e, []any{map[string]any{"kind": "automation.save", "pageId": page, "rule": r}}, "page", page, "record", record, "collection", f.collection))
	at := "2026-10-05T04:00:00Z"
	p1, p2 := automationPreviewAt(t, k, e, page, r.ID, at), automationPreviewAt(t, k, e, page, r.ID, at)
	if string(marshal(p1["commands"])) != string(marshal(p2["commands"])) || string(marshal(p1["expectedRevisions"])) != string(marshal(p2["expectedRevisions"])) {
		t.Fatal("same occurrence allocated different graph")
	}
	if p1["eligible"] != true {
		t.Fatal(p1)
	}
	commands := p1["commands"].([]any)
	newPage, newRecord, copyCell := "", "", ""
	for _, raw := range commands {
		a := raw.(map[string]any)
		if a["kind"] == "record.create" {
			newPage = str(a["pageId"])
			newRecord = str(a["recordId"])
		}
		if a["kind"] == "block.insert" {
			copyCell = str(a["block"].(map[string]any)["data"].(map[string]any)["cells"].([]any)[0].(map[string]any)["id"])
		}
	}
	if newPage == page || newRecord == record || copyCell == cell || copyCell == "" {
		t.Fatal("not independently remapped", commands)
	}
	var count int
	k.db.QueryRow("SELECT count(*) FROM pages WHERE id=?", newPage).Scan(&count)
	if count != 0 {
		t.Fatal("preview left mutation")
	}
	k.db.QueryRow("SELECT count(*) FROM automation_runs WHERE rule_id=?", r.ID).Scan(&count)
	if count != 0 {
		t.Fatal("preview wrote receipt")
	}
	request := advancedRequest(k, map[string]any{"pageId": page, "ruleId": r.ID, "at": p1["at"], "occurrenceKey": p1["occurrenceKey"], "expectedRevisions": p1["expectedRevisions"]})
	testOK(t, testDispatch(t, k, e, "automation.execute", request))
	var raw string
	k.db.QueryRow("SELECT value_json FROM property_values WHERE record_id=? AND property_id=?", newRecord, f.status).Scan(&raw)
	if raw != string(marshal(f.open)) {
		t.Fatal("defaults lost", raw)
	}
	body := readPage(t, k, e, newPage)["blocks"].([]any)
	if len(body) != 1 || !strings.Contains(string(marshal(body)), copyCell) || !strings.Contains(string(marshal(body)), "Header") {
		t.Fatal(body)
	}
	path := k.Path()
	k.Close()
	var err error
	k, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer k.Close()
	e = k.OwnerContext()
	s, _ := loadAutomation(context.Background(), k.db, r.ID)
	instant, _ := time.Parse(time.RFC3339, at)
	if err = k.executeAutomationOccurrence(context.Background(), e, s, "", str(p1["occurrenceKey"]), instant); err != nil {
		t.Fatal(err)
	}
	k.db.QueryRow("SELECT count(*) FROM records WHERE collection_id=?", f.collection).Scan(&count)
	if count != 2 {
		t.Fatal("replay duplicated", count)
	}
	done := automationPreviewAt(t, k, e, page, r.ID, at)
	if done["eligible"] != false || done["diagnostic"] != "This occurrence is already succeeded" {
		t.Fatal(done)
	}
	p3 := automationPreviewAt(t, k, e, page, r.ID, "2026-10-12T04:00:00Z")
	if strings.Contains(string(marshal(p3["commands"])), copyCell) {
		t.Fatal("next occurrence reused local cell")
	}
}
func TestAutomationTemplateHashSchemaLocksAndDeniedOrigins(t *testing.T) {
	k, e := testKernel(t)
	f := newDatabase(t, k, e)
	record, source := newRecord(t, k, e, f, "Source")
	testOK(t, setValue(t, k, e, f, record, source, f.status, "status", f.open))
	template := savedTemplate(t, k, e, source)
	page := createPage(t, k, e, "Rules")
	r := scheduleRule(page)
	r.Actions = []map[string]any{recurringTemplateAction(t, k, e, template, f.collection)}
	saveRule(t, k, e, page, r)
	at := "2026-10-02T04:00:00Z"
	preview := automationPreviewAt(t, k, e, page, r.ID, at)
	testOK(t, dbBatch(t, k, e, []any{map[string]any{"kind": "collection.configure", "collectionId": f.collection, "locked": true}}, "collection", f.collection))
	testCode(t, testDispatch(t, k, e, "automationPreview", advancedRequest(k, map[string]any{"pageId": page, "ruleId": r.ID, "at": at})), "SCOPE_DENIED")
	testCode(t, testDispatch(t, k, e, "automation.execute", advancedRequest(k, map[string]any{"pageId": page, "ruleId": r.ID, "at": at, "occurrenceKey": preview["occurrenceKey"], "expectedRevisions": preview["expectedRevisions"]})), "CONFLICT")
	testOK(t, dbBatch(t, k, e, []any{map[string]any{"kind": "collection.configure", "collectionId": f.collection, "locked": false}}, "collection", f.collection))
	if err := k.RetryAutomation(context.Background(), e, r.ID, str(preview["occurrenceKey"])); err != nil {
		t.Fatal(err)
	}
	var status string
	k.db.QueryRow("SELECT status FROM automation_runs WHERE rule_id=?", r.ID).Scan(&status)
	if status != "succeeded" {
		t.Fatal("failed template did not retry", status)
	}
	// The table has no update API; simulate a restored/replaced capture to ensure
	// the saved content hash is checked rather than trusting source-page revision.
	var original string
	k.db.QueryRow("SELECT snapshot_json FROM advanced_templates WHERE id=?", template).Scan(&original)
	k.db.Exec("UPDATE advanced_templates SET snapshot_json=snapshot_json||' ' WHERE id=?", template)
	testCode(t, testDispatch(t, k, e, "automationPreview", advancedRequest(k, map[string]any{"pageId": page, "ruleId": r.ID, "at": "2026-10-03T04:00:00Z"})), "CONFLICT")
	testCode(t, dbBatch(t, k, e, []any{map[string]any{"kind": "automation.save", "pageId": page, "rule": r}}, "page", page), "CONFLICT")
	k.db.Exec("UPDATE advanced_templates SET snapshot_json=? WHERE id=?", original, template)
	other := newDatabase(t, k, e)
	r.Actions[0]["collectionId"] = other.collection
	saveRule(t, k, e, page, r)
	testCode(t, testDispatch(t, k, e, "automationPreview", advancedRequest(k, map[string]any{"pageId": page, "ruleId": r.ID, "at": at})), "CONFLICT")
	scoped := k.Grant(e.Actor, "user", contracts.DomainScope{Value: contracts.DomainScopeVariant3{Kind: "pages", WorkspaceId: contracts.DomainId(k.ID()), PageIds: []contracts.DomainId{contracts.DomainId(page)}}})
	testCode(t, testDispatch(t, k, scoped, "automationOptions", advancedRequest(k, map[string]any{"pageId": page})), "SCOPE_DENIED")
	ai := k.Grant(contracts.ServiceActor{Kind: "local_ai", ActorId: contracts.DomainId(NewID())}, "ai", e.Scope)
	testCode(t, testDispatch(t, k, ai, "automationOptions", advancedRequest(k, map[string]any{"pageId": page})), "SCOPE_DENIED")
	testCode(t, dbBatch(t, k, ai, []any{map[string]any{"kind": "automation.save", "pageId": page, "rule": r}}, "page", page), "APPROVAL_REQUIRED")
}
func TestAutomationPreviewChecksTypedValuesAndCondition(t *testing.T) {
	k, e := testKernel(t)
	f := newDatabase(t, k, e)
	record, recordPage := newRecord(t, k, e, f, "Target")
	page := createPage(t, k, e, "Rules")
	r := scheduleRule(page)
	testOK(t, setValue(t, k, e, f, record, recordPage, f.status, "status", f.open))
	r.Condition = &AutomationCondition{RecordID: record, PropertyID: f.status, Op: "eq", Value: f.open}
	r.Actions = append(r.Actions, map[string]any{"kind": "record.setValue", "recordId": record, "propertyId": f.status, "value": map[string]any{"type": "status", "schemaVersion": 1, "value": NewID()}})
	saveRule(t, k, e, page, r)
	at := "2026-10-02T04:00:00Z"
	testCode(t, testDispatch(t, k, e, "automationPreview", advancedRequest(k, map[string]any{"pageId": page, "ruleId": r.ID, "at": at})), "INVALID_REQUEST")
	var count int
	k.db.QueryRow("SELECT count(*) FROM pages WHERE parent_page_id=?", page).Scan(&count)
	if count != 0 {
		t.Fatal("failed preview leaked first action")
	}
	testOK(t, setValue(t, k, e, f, record, recordPage, f.status, "status", f.done))
	p := automationPreviewAt(t, k, e, page, r.ID, at)
	if p["eligible"] != false || p["diagnostic"] != "Condition did not match" {
		t.Fatal(p)
	}
}
func TestAutomationPublishedSchemaBoundsAndOptions(t *testing.T) {
	k, e := testKernel(t)
	page := createPage(t, k, e, "Source")
	template := savedTemplate(t, k, e, page)
	r := scheduleRule(page)
	r.Actions = []map[string]any{recurringTemplateAction(t, k, e, template, "")}
	if err := contracts.Validate("service", "AutomationRule", marshal(r)); err != nil {
		t.Fatal(err)
	}
	r.Actions[0]["propertyDecisions"] = []any{}
	if contracts.Validate("service", "AutomationRule", marshal(r)) == nil {
		t.Fatal("accepted unsupported recurring mapping")
	}
	delete(r.Actions[0], "propertyDecisions")
	r.Actions[0]["kind"] = "template.apply"
	if contracts.Validate("service", "AutomationRule", marshal(r)) == nil {
		t.Fatal("accepted temporary approval plan as persistent authority")
	}
	result := testOK(t, testDispatch(t, k, e, "automationOptions", advancedRequest(k, map[string]any{"pageId": page})))
	var choices struct {
		Templates []struct{ ID, Hash, CollectionID string }
		Events    []json.RawMessage
	}
	json.Unmarshal(marshal(result), &choices)
	if len(choices.Templates) != 1 || choices.Templates[0].ID != template || len(choices.Templates[0].Hash) != 64 || len(choices.Events) == 0 {
		t.Fatal(result)
	}
}
