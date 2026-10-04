package workspace

import (
	"context"
	"encoding/json"
	"io"
	"strings"
	"testing"
	"trove.local/phase0/contracts"
)

func savedTemplate(t *testing.T, k *Kernel, e contracts.ServiceExecutionContext, page string) string {
	t.Helper()
	var rev string
	if err := k.db.QueryRow("SELECT revision FROM pages WHERE id=?", page).Scan(&rev); err != nil {
		t.Fatal(err)
	}
	return str(testOK(t, testDispatch(t, k, e, "editor.saveTemplate", advancedRequest(k, map[string]any{"pageId": page, "name": "Combined defaults", "expectedPageRevision": rev})))["id"])
}
func templatePreviewTest(t *testing.T, k *Kernel, e contracts.ServiceExecutionContext, id, collection string, decisions ...any) map[string]any {
	t.Helper()
	if decisions == nil {
		decisions = []any{}
	}
	r := advancedRequest(k, map[string]any{"templateId": id, "title": "Instantiated", "propertyDecisions": decisions})
	if collection != "" {
		r["collectionId"] = collection
	}
	result, err := previewTemplate(context.Background(), k, e, r)
	if err != nil {
		t.Fatalf("template preview: %v", err)
	}
	var decoded map[string]any
	json.Unmarshal(marshal(result), &decoded)
	return decoded
}
func templateApplyRequest(t *testing.T, k *Kernel, e contracts.ServiceExecutionContext, p map[string]any) map[string]any {
	t.Helper()
	var guards []contracts.ServiceExpectedRevision
	if err := json.Unmarshal(marshal(p["expectedRevisions"]), &guards); err != nil {
		t.Fatal(err)
	}
	return testRequest(k, e, []any{p["command"]}, guards...)
}

func TestUnifiedTemplateValuesNestedBodyTasksAndIndependentInstances(t *testing.T) {
	k, e := testKernel(t)
	f := newDatabase(t, k, e)
	record, page := newRecord(t, k, e, f, "Source default")
	testOK(t, setValue(t, k, e, f, record, page, f.status, "status", f.open))
	testOK(t, setValue(t, k, e, f, record, page, f.date, "date", map[string]any{"start": "2026-10-04", "end": "2026-10-05", "hasTime": false, "timezone": "Asia/Kolkata"}))
	task, taskPage := newRecord(t, k, e, f, "Checklist step")
	testOK(t, dbBatch(t, k, e, []any{map[string]any{"kind": "task.promote", "recordId": task, "statusPropertyId": f.status}}, "record", task, "page", taskPage, "collection", f.collection, "property", f.status))
	toggle, child, todo, todo2, synced := NewID(), NewID(), NewID(), NewID(), NewID()
	block := func(id, kind, parent string, data any) any {
		b := map[string]any{"blockId": id, "pageId": page, "type": kind, "schemaVersion": 1, "data": data}
		if parent != "" {
			b["parentBlockId"] = parent
		}
		return map[string]any{"kind": "block.insert", "block": b}
	}
	testOK(t, dbBatch(t, k, e, []any{
		block(toggle, "toggle", "", map[string]any{"richText": []any{map[string]any{"text": "Section", "marks": []string{"bold"}}}, "collapsed": true}),
		block(child, "paragraph", toggle, map[string]any{"richText": []any{map[string]any{"text": "Rich child", "color": "blue", "marks": []string{"italic"}, "link": canonicalPageLink(page, toggle)}}}),
		block(todo, "todo", toggle, map[string]any{"taskRecordId": task}), block(todo2, "todo", "", map[string]any{"taskRecordId": task}),
		block(synced, "synced_block", "", map[string]any{"contentSourceId": child}),
	}, "page", page, "record", record, "collection", f.collection, "block", toggle, "block", child, "block", todo, "block", todo2, "block", synced))
	template := savedTemplate(t, k, e, page)
	otherDestination := newDatabase(t, k, e)
	limited := k.Grant(contracts.ServiceActor{Kind: "user", ActorId: contracts.DomainId(NewID())}, "user", contracts.DomainScope{Value: contracts.DomainScopeVariant3{Kind: "pages", WorkspaceId: contracts.DomainId(k.ID()), PageIds: []contracts.DomainId{contracts.DomainId(page), contracts.DomainId(otherDestination.page)}}})
	bodyOnly := testOK(t, testDispatch(t, k, limited, "editor.template", advancedRequest(k, map[string]any{"templateId": template})))
	if _, leaked := bodyOnly["ownedTasks"]; leaked {
		t.Fatal("compatibility read exposed separately scoped task contents")
	}
	testCode(t, testDispatch(t, k, limited, "template.preview", advancedRequest(k, map[string]any{"templateId": template, "collectionId": otherDestination.collection, "title": "Hidden task denied"})), "SCOPE_DENIED")
	// Capture is immutable: later edits to the source defaults are not adopted.
	testOK(t, setValue(t, k, e, f, record, page, f.status, "status", f.done))
	pages, tasks := []string{}, []string{}
	for range 2 {
		preview := templatePreviewTest(t, k, e, template, f.collection)
		if preview["blockCount"] != float64(5) || preview["valueCount"] != float64(3) || preview["taskCount"] != float64(1) || preview["canApply"] != true {
			t.Fatal(preview)
		}
		if err := contracts.Validate("service", "TemplatePreviewResult", marshal(preview)); err != nil {
			t.Fatal(err)
		}
		var count int
		k.db.QueryRow("SELECT count(*) FROM pages WHERE id=?", preview["pageId"]).Scan(&count)
		if count != 0 {
			t.Fatal("preview created a page")
		}
		request := templateApplyRequest(t, k, e, preview)
		testOK(t, testDispatch(t, k, e, "batch", request))
		replayed := testOK(t, testDispatch(t, k, e, "batch", request))
		if replayed["replayed"] != true {
			t.Fatal("retry did not replay")
		}
		newPage := str(preview["pageId"])
		pages = append(pages, newPage)
		newRecord := str(preview["recordId"])
		var status string
		if err := k.db.QueryRow("SELECT value_json FROM property_values WHERE record_id=? AND property_id=?", newRecord, f.status).Scan(&status); err != nil || status != "\""+f.open+"\"" {
			t.Fatal(status, err)
		}
		body := readPage(t, k, e, newPage)["blocks"].([]any)
		ids := map[string]string{}
		taskRefs := []string{}
		for _, raw := range body {
			b := raw.(map[string]any)
			if b["id"] == toggle || b["id"] == child || b["id"] == todo {
				t.Fatal("reused source block")
			}
			ids[str(b["type"])] = str(b["id"])
			if b["type"] == "todo" {
				taskRefs = append(taskRefs, str(b["data"].(map[string]any)["taskRecordId"]))
			}
		}
		if len(taskRefs) != 2 || taskRefs[0] != taskRefs[1] || taskRefs[0] == task {
			t.Fatal(taskRefs)
		}
		tasks = append(tasks, taskRefs[0])
		for _, raw := range body {
			b := raw.(map[string]any)
			data := b["data"].(map[string]any)
			if b["type"] == "paragraph" {
				if b["parentBlockId"] != ids["toggle"] || !strings.Contains(string(marshal(data)), canonicalPageLink(newPage, ids["toggle"])) || !strings.Contains(string(marshal(data)), "blue") {
					t.Fatal(b)
				}
			}
			if b["type"] == "synced_block" && data["contentSourceId"] != ids["paragraph"] {
				t.Fatal(b)
			}
		}
	}
	if tasks[0] == tasks[1] || pages[0] == pages[1] {
		t.Fatal("instances share mutable identity")
	}
	var clonedTaskPage string
	k.db.QueryRow("SELECT page_id FROM records WHERE id=?", tasks[0]).Scan(&clonedTaskPage)
	testOK(t, dbBatch(t, k, e, []any{map[string]any{"kind": "task.setStatus", "recordId": tasks[0], "statusOptionId": f.done}}, "record", tasks[0], "page", clonedTaskPage, "collection", f.collection, "property", f.status))
	for _, id := range []string{task, tasks[1]} {
		var value string
		k.db.QueryRow("SELECT value_json FROM property_values WHERE record_id=? AND property_id=?", id, f.status).Scan(&value)
		if value != "\""+f.open+"\"" {
			t.Fatal("task edit leaked", id, value)
		}
	}
	path := k.Path()
	k.Close()
	reopened, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	for _, page := range pages {
		if len(readPage(t, reopened, reopened.OwnerContext(), page)["blocks"].([]any)) != 5 {
			t.Fatal("body missing after reopen")
		}
	}
}

func TestUnifiedTemplateMappingsStaleGuardsAndTemplateHash(t *testing.T) {
	k, e := testKernel(t)
	source := newDatabase(t, k, e)
	record, page := newRecord(t, k, e, source, "Defaults")
	testOK(t, setValue(t, k, e, source, record, page, source.status, "status", source.open))
	id := savedTemplate(t, k, e, page)
	dest := newDatabase(t, k, e)
	preview := templatePreviewTest(t, k, e, id, dest.collection)
	if preview["canApply"] != false || len(preview["propertyIssues"].([]any)) != 1 {
		t.Fatal(preview)
	}
	decision := map[string]any{"sourcePropertyId": source.status, "action": "map", "targetPropertyId": dest.status, "value": dest.open}
	request := advancedRequest(k, map[string]any{"templateId": id, "collectionId": dest.collection, "title": "Mapped", "propertyDecisions": []any{decision, decision}})
	testCode(t, testDispatch(t, k, e, "template.preview", request), "INVALID_REQUEST")
	request["propertyDecisions"] = []any{map[string]any{"sourcePropertyId": NewID(), "action": "omit"}}
	testCode(t, testDispatch(t, k, e, "template.preview", request), "INVALID_REQUEST")
	preview = templatePreviewTest(t, k, e, id, dest.collection, decision)
	testOK(t, testDispatch(t, k, e, "batch", templateApplyRequest(t, k, e, preview)))
	var actual string
	k.db.QueryRow("SELECT value_json FROM property_values WHERE record_id=? AND property_id=?", preview["recordId"], dest.status).Scan(&actual)
	if actual != "\""+dest.open+"\"" {
		t.Fatal(actual)
	}
	stale := templatePreviewTest(t, k, e, id, source.collection)
	newRecord(t, k, e, source, "Racing write")
	testCode(t, testDispatch(t, k, e, "batch", templateApplyRequest(t, k, e, stale)), "CONFLICT")
	hashPreview := templatePreviewTest(t, k, e, id, source.collection)
	if _, err := k.db.Exec("UPDATE advanced_templates SET snapshot_json=json_set(snapshot_json,'$.page.title','Changed') WHERE id=?", id); err != nil {
		t.Fatal(err)
	}
	testCode(t, testDispatch(t, k, e, "batch", templateApplyRequest(t, k, e, hashPreview)), "CONFLICT")
}

func TestUnifiedTemplateScopeDirectUserCaptureAndApprovalBoundary(t *testing.T) {
	k, e := testKernel(t)
	f := newDatabase(t, k, e)
	_, page := newRecord(t, k, e, f, "Source")
	id := savedTemplate(t, k, e, page)
	limited := k.Grant(contracts.ServiceActor{Kind: "user", ActorId: contracts.DomainId(NewID())}, "user", contracts.DomainScope{Value: contracts.DomainScopeVariant3{Kind: "pages", WorkspaceId: contracts.DomainId(k.ID()), PageIds: []contracts.DomainId{contracts.DomainId(page)}}})
	testCode(t, testDispatch(t, k, limited, "template.preview", advancedRequest(k, map[string]any{"templateId": id, "collectionId": f.collection, "title": "Denied"})), "SCOPE_DENIED")
	ai := k.Grant(contracts.ServiceActor{Kind: "local_ai", ActorId: contracts.DomainId(NewID())}, "ai", contracts.DomainScope{Value: contracts.DomainScopeVariant1{Kind: "workspace", WorkspaceId: contracts.DomainId(k.ID())}})
	testCode(t, testDispatch(t, k, ai, "editor.saveTemplate", advancedRequest(k, map[string]any{"pageId": page, "name": "Forbidden", "expectedPageRevision": "1"})), "SCOPE_DENIED")
	preview := templatePreviewTest(t, k, ai, id, f.collection)
	request := templateApplyRequest(t, k, ai, preview)
	testCode(t, testDispatch(t, k, ai, "batch", request), "APPROVAL_REQUIRED")
	testCode(t, testDispatch(t, k, e, "batch", templateApplyRequest(t, k, e, preview)), "SCOPE_DENIED")
	var typed contracts.ServiceBatchRequest
	if err := json.Unmarshal(marshal(request), &typed); err != nil {
		t.Fatal(err)
	}
	approval, err := k.Approve(context.Background(), e, typed, ai)
	if err != nil {
		t.Fatal(err)
	}
	request["mutation"].(map[string]any)["approvalId"] = approval
	testOK(t, testDispatch(t, k, ai, "batch", request))
}

func TestUnifiedTemplateAssetsLocalLayoutIDsAndExplicitExternalReferences(t *testing.T) {
	k, e := testKernel(t)
	source := createPage(t, k, e, "Layout source")
	external := createPage(t, k, e, "Explicit linked content")
	target := newDatabase(t, k, e)
	prepared := stageTestFile(t, k, e, "immutable template asset")
	file, _ := attachTestFile(t, k, e, source, prepared, true)
	extBlock, columns, table, synced := NewID(), NewID(), NewID(), NewID()
	column, cell := NewID(), NewID()
	testOK(t, dbBatch(t, k, e, []any{map[string]any{"kind": "block.insert", "block": map[string]any{"blockId": extBlock, "pageId": external, "type": "paragraph", "schemaVersion": 1, "data": map[string]any{"richText": []any{map[string]any{"text": "Explicit shared source"}}}}}}, "page", external, "block", extBlock))
	testOK(t, dbBatch(t, k, e, []any{
		map[string]any{"kind": "block.insert", "block": map[string]any{"blockId": columns, "pageId": source, "type": "columns", "schemaVersion": 1, "data": map[string]any{"columns": []any{map[string]any{"id": column, "widthRatio": 1, "childBlockIds": []any{table}}}}}},
		map[string]any{"kind": "block.insert", "block": map[string]any{"blockId": table, "pageId": source, "parentBlockId": columns, "type": "table", "schemaVersion": 1, "data": map[string]any{"rowCount": 1, "columnCount": 1, "headerRows": 1, "headerColumns": 0, "columnWidths": []any{245}, "cells": []any{map[string]any{"id": cell, "row": 0, "column": 0, "rowSpan": 1, "colSpan": 1, "richText": []any{map[string]any{"text": "Header", "marks": []string{"bold"}}}, "preMergeCells": []any{}}}}}},
		map[string]any{"kind": "block.insert", "block": map[string]any{"blockId": synced, "pageId": source, "type": "synced_block", "schemaVersion": 1, "data": map[string]any{"contentSourceId": extBlock}}},
	}, "page", source, "block", columns, "block", table, "block", synced))
	id := savedTemplate(t, k, e, source)
	limited := k.Grant(contracts.ServiceActor{Kind: "user", ActorId: contracts.DomainId(NewID())}, "user", contracts.DomainScope{Value: contracts.DomainScopeVariant3{Kind: "pages", WorkspaceId: contracts.DomainId(k.ID()), PageIds: []contracts.DomainId{contracts.DomainId(source), contracts.DomainId(target.page)}}})
	testCode(t, testDispatch(t, k, limited, "template.preview", advancedRequest(k, map[string]any{"templateId": id, "collectionId": target.collection, "title": "No hidden source"})), "SCOPE_DENIED")
	p := templatePreviewTest(t, k, e, id, target.collection)
	testOK(t, testDispatch(t, k, e, "batch", templateApplyRequest(t, k, e, p)))
	body := readPage(t, k, e, str(p["pageId"]))["blocks"].([]any)
	var newColumn, newTable string
	for _, raw := range body {
		b := raw.(map[string]any)
		data := b["data"].(map[string]any)
		switch b["type"] {
		case "columns":
			group := data["columns"].([]any)[0].(map[string]any)
			newColumn = str(group["id"])
			newTable = str(group["childBlockIds"].([]any)[0])
			if newColumn == column {
				t.Fatal("reused layout identity")
			}
		case "table":
			if data["cells"].([]any)[0].(map[string]any)["id"] == cell || data["headerRows"] != float64(1) || data["columnWidths"].([]any)[0] != float64(245) {
				t.Fatal(data)
			}
		case "synced_block":
			if data["contentSourceId"] != extBlock {
				t.Fatal("explicit external reference was incorrectly cloned")
			}
		}
	}
	if newTable == table || newTable == "" {
		t.Fatal("layout child reference was not remapped")
	}
	destinationScope := k.Grant(contracts.ServiceActor{Kind: "user", ActorId: contracts.DomainId(NewID())}, "user", contracts.DomainScope{Value: contracts.DomainScopeVariant3{Kind: "pages", WorkspaceId: contracts.DomainId(k.ID()), PageIds: []contracts.DomainId{contracts.DomainId(str(p["pageId"]))}}})
	opened, _, err := k.ReadFile(context.Background(), destinationScope, file)
	if err != nil {
		t.Fatal(err)
	}
	defer opened.Close()
	bytes, err := io.ReadAll(opened)
	if err != nil || string(bytes) != "immutable template asset" {
		t.Fatal(string(bytes), err)
	}
}

func TestUnifiedTemplateSchemaChangeExpiryAndLockedDestination(t *testing.T) {
	k, e := testKernel(t)
	f := newDatabase(t, k, e)
	record, page := newRecord(t, k, e, f, "Default")
	testOK(t, setValue(t, k, e, f, record, page, f.status, "status", f.open))
	id := savedTemplate(t, k, e, page)
	if _, err := k.db.Exec("UPDATE properties SET config_json=json_set(config_json,'$.options[0].label','Renamed'),revision=revision+1 WHERE id=?", f.status); err != nil {
		t.Fatal(err)
	}
	p := templatePreviewTest(t, k, e, id, f.collection)
	if p["canApply"] != false || len(p["propertyIssues"].([]any)) != 1 {
		t.Fatal("changed option schema was silently adopted", p)
	}
	d := map[string]any{"sourcePropertyId": f.status, "action": "map", "targetPropertyId": f.status, "value": f.open}
	p = templatePreviewTest(t, k, e, id, f.collection, d)
	if _, err := k.db.Exec("UPDATE template_previews SET expires_at='2000-01-01T00:00:00Z' WHERE id=?", p["previewId"]); err != nil {
		t.Fatal(err)
	}
	testCode(t, testDispatch(t, k, e, "batch", templateApplyRequest(t, k, e, p)), "CONFLICT")
	testOK(t, dbBatch(t, k, e, []any{map[string]any{"kind": "collection.configure", "collectionId": f.collection, "locked": true}}, "collection", f.collection))
	testCode(t, testDispatch(t, k, e, "template.preview", advancedRequest(k, map[string]any{"templateId": id, "collectionId": f.collection, "title": "Locked", "propertyDecisions": []any{d}})), "SCOPE_DENIED")
}
