package workspace

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"trove.local/phase0/contracts"
)

type valueRecoveryFixture struct {
	db                                                     databaseFixture
	record, page, other, otherPage, amount, note, revision string
}

func schemaRecoveryFixture(t *testing.T, k *Kernel, e contracts.ServiceExecutionContext) valueRecoveryFixture {
	t.Helper()
	f := valueRecoveryFixture{db: newDatabase(t, k, e), amount: NewID(), note: NewID()}
	f.record, f.page = newRecord(t, k, e, f.db, "Historical record")
	f.other, f.otherPage = newRecord(t, k, e, f.db, "Unrelated record")
	testOK(t, dbBatch(t, k, e, []any{
		map[string]any{"kind": "collection.addProperty", "collectionId": f.db.collection, "propertyId": f.amount, "name": "Amount", "type": "number", "schemaVersion": 1, "config": map[string]any{"format": "number"}},
		map[string]any{"kind": "collection.addProperty", "collectionId": f.db.collection, "propertyId": f.note, "name": "Note", "type": "text", "schemaVersion": 1, "config": map[string]any{}},
	}, "collection", f.db.collection, "property", f.amount, "property", f.note))
	testOK(t, setValue(t, k, e, f.db, f.record, f.page, f.note, "text", richImport("Historical compatible note")))
	testOK(t, setValue(t, k, e, f.db, f.record, f.page, f.amount, "number", map[string]any{"decimal": "12", "format": "number"}))
	if err := k.db.QueryRow("SELECT revision FROM pages WHERE id=?", f.page).Scan(&f.revision); err != nil {
		t.Fatal(err)
	}
	testOK(t, setValue(t, k, e, f.db, f.record, f.page, f.amount, "number", map[string]any{"decimal": "99", "format": "number"}))
	testOK(t, setValue(t, k, e, f.db, f.record, f.page, f.note, "text", richImport("Current compatible note")))
	testOK(t, setValue(t, k, e, f.db, f.other, f.otherPage, f.amount, "number", map[string]any{"decimal": "77", "format": "number"}))
	// Simulate a completed configuration migration within the supported registry version.
	if _, err := k.db.Exec("UPDATE properties SET config_json=?,revision=CAST(revision AS INTEGER)+1 WHERE id=?", `{"format":"percent"}`, f.amount); err != nil {
		t.Fatal(err)
	}
	if _, err := k.db.Exec("UPDATE property_values SET value_json=json_set(value_json,'$.format','percent') WHERE property_id=?", f.amount); err != nil {
		t.Fatal(err)
	}
	return f
}

func valuePreview(t *testing.T, k *Kernel, e contracts.ServiceExecutionContext, f valueRecoveryFixture, decisions []any) map[string]any {
	t.Helper()
	r := map[string]any{"pageId": f.page, "restoreRevision": f.revision}
	if decisions != nil {
		r["propertyDecisions"] = decisions
	}
	return testDispatch(t, k, e, "history.previewRestore", recoveryRequest(k, r))
}

func applyValuePreview(t *testing.T, k *Kernel, e contracts.ServiceExecutionContext, p map[string]any) map[string]any {
	t.Helper()
	var guards []contracts.ServiceExpectedRevision
	if err := json.Unmarshal(marshal(p["expectedRevisions"]), &guards); err != nil {
		t.Fatal(err)
	}
	return testDispatch(t, k, e, "batch", testRequest(k, e, []any{map[string]any{"kind": "history.restore", "pageId": p["pageId"], "restoreRevision": p["restoreRevision"], "previewId": p["previewId"]}}, guards...))
}

func storedProperty(t *testing.T, k *Kernel, record, property string) string {
	t.Helper()
	var raw string
	if err := k.db.QueryRow("SELECT value_json FROM property_values WHERE record_id=? AND property_id=?", record, property).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestHistoricalValueRetentionIsReviewedAndKeepsCurrentTypedValue(t *testing.T) {
	k, e := testKernel(t)
	f := schemaRecoveryFixture(t, k, e)
	preview := testOK(t, valuePreview(t, k, e, f, nil))
	if err := contracts.Validate("service", "HistoryPreviewRestoreResult", marshal(preview)); err != nil {
		t.Fatal("recovery preview violates the public contract", err)
	}
	if preview["canRestore"] != false || len(preview["incompatibleValues"].([]any)) != 1 {
		t.Fatal(preview)
	}
	testCode(t, applyValuePreview(t, k, e, preview), "CONFLICT")
	beforeOther := storedProperty(t, k, f.other, f.amount)
	current := readPage(t, k, e, f.page)["page"].(map[string]any)["revision"]
	preview = testOK(t, valuePreview(t, k, e, f, []any{map[string]any{"sourcePropertyId": f.amount, "action": "retain"}}))
	if err := contracts.Validate("service", "HistoryPreviewRestoreResult", marshal(preview)); err != nil {
		t.Fatal("reviewed preview violates the public contract", err)
	}
	if preview["canRestore"] != true {
		t.Fatal("reviewed retention is not applicable", preview)
	}
	testOK(t, applyValuePreview(t, k, e, preview))
	if !strings.Contains(storedProperty(t, k, f.record, f.amount), `"99"`) {
		t.Fatal("retention overwrote current typed value")
	}
	if !strings.Contains(storedProperty(t, k, f.record, f.note), "Historical compatible note") {
		t.Fatal("compatible historical value did not restore")
	}
	page := readPage(t, k, e, f.page)["page"].(map[string]any)
	if page["revision"] == current || !strings.Contains(string(marshal(page["provenance"])), `"12"`) || !strings.Contains(string(marshal(page["provenance"])), "retained evidence only") {
		t.Fatal("historical evidence/new revision missing", page)
	}
	var prior string
	if err := k.db.QueryRow("SELECT snapshot_json FROM page_snapshots WHERE page_id=? AND revision=?", f.page, current).Scan(&prior); err != nil || !strings.Contains(prior, "Current compatible note") {
		t.Fatal("pre-restore recovery point lost", err)
	}
	if got := storedProperty(t, k, f.other, f.amount); got != beforeOther {
		t.Fatal("unrelated record changed", got)
	}
	var version int
	var config string
	if err := k.db.QueryRow("SELECT schema_version,config_json FROM properties WHERE id=?", f.amount).Scan(&version, &config); err != nil || version != 1 || config != `{"format":"percent"}` {
		t.Fatal("shared schema rewound", version, config, err)
	}
}

func TestHistoricalUnknownVersionRemainsInertReviewableEvidence(t *testing.T) {
	k, e := testKernel(t)
	f := schemaRecoveryFixture(t, k, e)
	snap, _, err := loadRecoverySnapshot(context.Background(), k.db, f.page, f.revision)
	if err != nil {
		t.Fatal(err)
	}
	opaqueFile := NewID() // Historical opaque evidence may reference a removed source.
	for i := range snap.Record.Values {
		if string(snap.Record.Values[i].PropertyId) == f.amount {
			snap.Record.Values[i].SchemaVersion = 9
			snap.Record.Values[i].Value = marshal(map[string]any{"sourcePayload": "legacy opaque value", "operation": "never execute", "fileId": opaqueFile})
		}
	}
	for i := range snap.Properties {
		if string(snap.Properties[i].Id) == f.amount {
			snap.Properties[i].SchemaVersion = 9
			snap.Properties[i].Type = "future_vendor_type"
			snap.Properties[i].Config = json.RawMessage(`{"legacyConfig":true}`)
		}
	}
	if _, err = k.db.Exec("UPDATE page_snapshots SET snapshot_json=? WHERE page_id=? AND revision=?", string(marshal(snap)), f.page, f.revision); err != nil {
		t.Fatal(err)
	}
	preview := testOK(t, valuePreview(t, k, e, f, nil))
	if err = contracts.Validate("service", "HistoryPreviewRestoreResult", marshal(preview)); err != nil {
		t.Fatal("opaque historical metadata violates service evidence contract", err)
	}
	issue := preview["incompatibleValues"].([]any)[0].(map[string]any)
	if preview["canRestore"] != false || issue["historicalProperty"].(map[string]any)["schemaVersion"] != float64(9) || !strings.Contains(string(marshal(issue)), "legacy opaque value") {
		t.Fatal("opaque historical evidence unavailable", preview)
	}
	preview = testOK(t, valuePreview(t, k, e, f, []any{map[string]any{"sourcePropertyId": f.amount, "action": "retain"}}))
	if strings.Contains(string(marshal(preview["affectedFileIds"])), opaqueFile) {
		t.Fatal("opaque evidence became an attachment reference", preview)
	}
	testOK(t, applyValuePreview(t, k, e, preview))
	if got := storedProperty(t, k, f.record, f.amount); !strings.Contains(got, `"99"`) || strings.Contains(got, "sourcePayload") {
		t.Fatal("opaque evidence became an active typed value", got)
	}
	if !strings.Contains(string(marshal(readPage(t, k, e, f.page))), "legacy opaque value") {
		t.Fatal("opaque historical source was lost")
	}
}

func TestHistoricalValueMappingValidatesReviewAndFreshness(t *testing.T) {
	k, e := testKernel(t)
	f := schemaRecoveryFixture(t, k, e)
	target := NewID()
	testOK(t, dbBatch(t, k, e, []any{map[string]any{"kind": "collection.addProperty", "collectionId": f.db.collection, "propertyId": target, "name": "Mapped amount", "type": "number", "schemaVersion": 1, "config": map[string]any{"format": "number"}}}, "collection", f.db.collection, "property", target))
	decision := map[string]any{"sourcePropertyId": f.amount, "action": "map", "targetPropertyId": target, "value": map[string]any{"decimal": "34", "format": "number"}}
	testCode(t, valuePreview(t, k, e, f, []any{decision, decision}), "INVALID_REQUEST")
	testCode(t, valuePreview(t, k, e, f, []any{map[string]any{"sourcePropertyId": NewID(), "action": "retain"}}), "INVALID_REQUEST")
	testCode(t, valuePreview(t, k, e, f, []any{map[string]any{"sourcePropertyId": f.amount, "action": "map", "targetPropertyId": target, "value": "guessed conversion"}}), "INVALID_REQUEST")
	testCode(t, valuePreview(t, k, e, f, []any{map[string]any{"sourcePropertyId": f.amount, "action": "map", "targetPropertyId": f.note, "value": richImport("overwrite unrelated")}}), "INVALID_REQUEST")
	foreign := newDatabase(t, k, e)
	testCode(t, valuePreview(t, k, e, f, []any{map[string]any{"sourcePropertyId": f.amount, "action": "map", "targetPropertyId": foreign.date, "value": map[string]any{"start": "2026-10-04", "hasTime": false}}}), "INVALID_REQUEST")
	preview := testOK(t, valuePreview(t, k, e, f, []any{decision}))
	if _, err := k.db.Exec("UPDATE properties SET config_json=? WHERE id=?", `{"format":"percent"}`, target); err != nil {
		t.Fatal(err)
	}
	testCode(t, applyValuePreview(t, k, e, preview), "CONFLICT")
	if !strings.Contains(storedProperty(t, k, f.record, f.note), "Current compatible note") {
		t.Fatal("stale mapping partly restored page")
	}
	preview = testOK(t, valuePreview(t, k, e, f, []any{decision}))
	testOK(t, applyValuePreview(t, k, e, preview))
	if !strings.Contains(storedProperty(t, k, f.record, target), `"34"`) || !strings.Contains(storedProperty(t, k, f.record, f.amount), `"99"`) {
		t.Fatal("mapping did not preserve intended values")
	}
	testCode(t, applyValuePreview(t, k, e, preview), "CONFLICT")
}

func TestRemovedHistoricalPropertyCanBeRetainedOrMappedWithScopeChecks(t *testing.T) {
	k, e := testKernel(t)
	f := schemaRecoveryFixture(t, k, e)
	if _, err := k.db.Exec("DELETE FROM property_values WHERE property_id=?", f.amount); err != nil {
		t.Fatal(err)
	}
	if _, err := k.db.Exec("DELETE FROM properties WHERE id=?", f.amount); err != nil {
		t.Fatal(err)
	}
	preview := testOK(t, valuePreview(t, k, e, f, nil))
	issue := preview["incompatibleValues"].([]any)[0].(map[string]any)
	if issue["currentProperty"] != nil || !strings.Contains(string(marshal(issue["historicalValue"])), `"12"`) {
		t.Fatal("removed raw value unavailable", issue)
	}
	fileProperty := NewID()
	testOK(t, dbBatch(t, k, e, []any{map[string]any{"kind": "collection.addProperty", "collectionId": f.db.collection, "propertyId": fileProperty, "name": "Mapped file", "type": "files", "schemaVersion": 1, "config": map[string]any{}}}, "collection", f.db.collection, "property", fileProperty))
	privatePage := createPage(t, k, e, "Private file")
	file, _ := attachTestFile(t, k, e, privatePage, stageTestFile(t, k, e, "PRIVATE_FILE"), false)
	scoped := k.Grant(e.Actor, "user", contracts.DomainScope{Value: contracts.DomainScopeVariant3{Kind: "pages", WorkspaceId: contracts.DomainId(k.ID()), PageIds: []contracts.DomainId{contracts.DomainId(f.page), contracts.DomainId(f.db.page)}}})
	testCode(t, valuePreview(t, k, scoped, f, []any{map[string]any{"sourcePropertyId": f.amount, "action": "map", "targetPropertyId": fileProperty, "value": []any{map[string]any{"fileId": file, "displayName": "private.txt"}}}}), "SCOPE_DENIED")
	testCode(t, valuePreview(t, k, e, f, []any{map[string]any{"sourcePropertyId": f.amount, "action": "map", "targetPropertyId": f.db.status, "value": NewID()}}), "CONFLICT")
	preview = testOK(t, valuePreview(t, k, scoped, f, []any{map[string]any{"sourcePropertyId": f.amount, "action": "retain"}}))
	testOK(t, applyValuePreview(t, k, scoped, preview))
	if !strings.Contains(string(marshal(readPage(t, k, e, f.page))), "retainedHistoricalValues") {
		t.Fatal("removed property evidence was lost")
	}
	if err := k.authorizeEntity(context.Background(), k.db, scoped, "file", file); err == nil {
		t.Fatal("recovery granted unrelated file access")
	}
}
