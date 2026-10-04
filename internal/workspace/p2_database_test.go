package workspace

import (
	"archive/zip"
	"context"
	"encoding/json"
	"io"
	"path/filepath"
	"strings"
	"testing"
	"trove.local/phase0/contracts"
)

func p2Configure(t *testing.T, k *Kernel, e contracts.ServiceExecutionContext, f databaseFixture, c P2DatabaseConfig) map[string]any {
	t.Helper()
	return dbBatch(t, k, e, []any{map[string]any{"kind": "database.p2Configure", "collectionId": f.collection, "config": c}}, "collection", f.collection)
}
func p2Query(t *testing.T, k *Kernel, e contracts.ServiceExecutionContext, method, collection string) map[string]any {
	t.Helper()
	return testDispatch(t, k, e, method, map[string]any{"contractVersion": 1, "requestId": NewID(), "workspaceId": k.ID(), "collectionId": collection})
}
func TestP2ChartsExactDecimalsAuthorizedSourcesAndRestart(t *testing.T) {
	k, e := testKernel(t)
	f := newDatabase(t, k, e)
	number := NewID()
	testOK(t, dbBatch(t, k, e, []any{map[string]any{"kind": "collection.addProperty", "collectionId": f.collection, "propertyId": number, "name": "Amount", "type": "number", "schemaVersion": 1, "config": map[string]any{"format": "decimal"}}}, "collection", f.collection, "property", number))
	a, ap := newRecord(t, k, e, f, "Alpha")
	b, bp := newRecord(t, k, e, f, "Beta")
	c, _ := newRecord(t, k, e, f, "No amount")
	for _, v := range []struct{ r, p, value string }{{a, ap, "9007199254740993.000000000000001"}, {b, bp, "-0.000000000000002"}} {
		testOK(t, setValue(t, k, e, f, v.r, v.p, number, "number", map[string]any{"decimal": v.value, "format": "decimal"}))
		testOK(t, setValue(t, k, e, f, v.r, v.p, f.status, "status", f.open))
	}
	config := defaultP2Config()
	config.Chart.Aggregation = "sum"
	config.Chart.ValuePropertyID = &number
	testOK(t, p2Configure(t, k, e, f, config))
	result := testOK(t, p2Query(t, k, e, "databaseChart", f.collection))
	group := result["groups"].([]any)[0].(map[string]any)
	if group["decimal"] != "9007199254740992.999999999999999" || group["numericCount"] != float64(2) || len(group["recordIds"].([]any)) != 3 {
		t.Fatal(result)
	}
	scoped := k.Grant(e.Actor, "user", contracts.DomainScope{Value: contracts.DomainScopeVariant3{Kind: "pages", WorkspaceId: contracts.DomainId(k.ID()), PageIds: []contracts.DomainId{contracts.DomainId(f.page), contracts.DomainId(ap)}}})
	limited := testOK(t, p2Query(t, k, scoped, "databaseChart", f.collection))
	if strings.Contains(string(marshal(limited)), b) || strings.Contains(string(marshal(limited)), c) || limited["recordCount"] != float64(1) {
		t.Fatal("Scoped chart leaked a record", limited)
	}
	foreign := createPage(t, k, e, "Outside")
	denied := k.Grant(e.Actor, "user", contracts.DomainScope{Value: contracts.DomainScopeVariant3{Kind: "pages", WorkspaceId: contracts.DomainId(k.ID()), PageIds: []contracts.DomainId{contracts.DomainId(foreign)}}})
	testCode(t, p2Query(t, k, denied, "databaseChart", f.collection), "SCOPE_DENIED")
	config.Chart.GroupPropertyID = &f.status
	config.Chart.Aggregation = "count"
	testOK(t, p2Configure(t, k, e, f, config))
	grouped := testOK(t, p2Query(t, k, e, "databaseChart", f.collection))
	if len(grouped["groups"].([]any)) != 2 {
		t.Fatal(grouped)
	}
	path := k.Path()
	k.Close()
	reopened, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	stored := testOK(t, p2Query(t, reopened, reopened.OwnerContext(), "databaseP2", f.collection))
	if !strings.Contains(string(marshal(stored)), "groupPropertyId") || !strings.Contains(string(marshal(stored)), f.status) {
		t.Fatal(stored)
	}
}
func TestP2ChartsNullAverageAndResourceLimits(t *testing.T) {
	k, e := testKernel(t)
	f := newDatabase(t, k, e)
	number := NewID()
	testOK(t, dbBatch(t, k, e, []any{map[string]any{"kind": "collection.addProperty", "collectionId": f.collection, "propertyId": number, "name": "Value", "type": "number", "schemaVersion": 1, "config": map[string]any{"format": "decimal"}}}, "collection", f.collection, "property", number))
	config := defaultP2Config()
	config.Chart.Aggregation = "average"
	config.Chart.ValuePropertyID = &number
	testOK(t, p2Configure(t, k, e, f, config))
	for _, value := range []string{"1", "0", "0"} {
		r, p := newRecord(t, k, e, f, value)
		testOK(t, setValue(t, k, e, f, r, p, number, "number", map[string]any{"decimal": value, "format": "decimal"}))
	}
	newRecord(t, k, e, f, "Missing")
	result := testOK(t, p2Query(t, k, e, "databaseChart", f.collection))
	g := result["groups"].([]any)[0].(map[string]any)
	if g["decimal"] != "0.333333333333" || g["exactFraction"] != "1/3" || g["numericCount"] != float64(3) {
		t.Fatal(result)
	}
	// More than 100 groups is a visible failure, never a silently truncated chart.
	commands := []any{}
	pairs := []string{"collection", f.collection}
	for i := 0; i < 101; i++ {
		r, p := NewID(), NewID()
		commands = append(commands, map[string]any{"kind": "record.create", "recordId": r, "pageId": p, "collectionId": f.collection, "title": r})
		pairs = append(pairs, "record", r, "page", p)
	}
	testOK(t, dbBatch(t, k, e, commands, pairs...))
	config.Chart.GroupPropertyID = &f.title
	config.Chart.Aggregation = "count"
	testOK(t, p2Configure(t, k, e, f, config))
	testCode(t, p2Query(t, k, e, "databaseChart", f.collection), "RESOURCE_EXHAUSTED")
}
func TestP2LocalFormsValidationAtomicityAndExtraction(t *testing.T) {
	k, e := testKernel(t)
	f := newDatabase(t, k, e)
	config := defaultP2Config()
	config.Form.Enabled = true
	config.Form.RequiredPropertyIDs = []string{f.status}
	testOK(t, p2Configure(t, k, e, f, config))
	submit := func(title string, values []any) (string, string, map[string]any) {
		r, p := NewID(), NewID()
		return r, p, dbBatch(t, k, e, []any{map[string]any{"kind": "database.formSubmit", "collectionId": f.collection, "recordId": r, "pageId": p, "title": title, "values": values}}, "collection", f.collection, "record", r, "page", p, "property", f.status)
	}
	_, missing, result := submit("Missing", []any{})
	testCode(t, result, "INVALID_REQUEST")
	_, invalid, result := submit("Bad", []any{map[string]any{"propertyId": f.status, "value": map[string]any{"type": "status", "schemaVersion": 1, "value": NewID()}}})
	testCode(t, result, "INVALID_REQUEST")
	for _, p := range []string{missing, invalid} {
		var n int
		k.db.QueryRow("SELECT count(*) FROM pages WHERE id=?", p).Scan(&n)
		if n != 0 {
			t.Fatal("Invalid form partially committed")
		}
	}
	r, p, result := submit("Local submission", []any{map[string]any{"propertyId": f.status, "value": map[string]any{"type": "status", "schemaVersion": 1, "value": f.open}}})
	testOK(t, result)
	var provenance string
	if err := k.db.QueryRow("SELECT collection_revision FROM p2_form_submissions WHERE record_id=?", r).Scan(&provenance); err != nil || provenance == "" {
		t.Fatal(err, provenance)
	}
	if err := k.FlushSearch(context.Background()); err != nil {
		t.Fatal(err)
	}
	var chunks int
	if err := k.db.QueryRow("SELECT count(*) FROM search_chunks WHERE page_id=? AND body LIKE '%Open%'", p).Scan(&chunks); err != nil || chunks == 0 {
		t.Fatal("Canonical form record did not reach search extraction", err, chunks)
	}
	config.Form.Enabled = false
	testOK(t, p2Configure(t, k, e, f, config))
	_, _, result = submit("Disabled", []any{})
	testCode(t, result, "SCOPE_DENIED")
	other := newDatabase(t, k, e)
	config.Form.RequiredPropertyIDs = []string{other.status}
	testCode(t, p2Configure(t, k, e, f, config), "INVALID_REQUEST")
}
func TestP2RowButtonsConfirmScopeRevisionsAndAIApproval(t *testing.T) {
	k, e := testKernel(t)
	f := newDatabase(t, k, e)
	r, p := newRecord(t, k, e, f, "Review row")
	config := defaultP2Config()
	button := P2RowButton{ID: NewID(), Label: "Complete", PropertyID: f.status, Value: map[string]any{"type": "status", "schemaVersion": 1, "value": f.done}, ConfirmationRequired: true}
	config.Buttons = []P2RowButton{button}
	testOK(t, p2Configure(t, k, e, f, config))
	command := map[string]any{"kind": "database.rowButtonRun", "collectionId": f.collection, "recordId": r, "buttonId": button.ID, "confirmed": false}
	pairs := []string{"collection", f.collection, "record", r, "page", p, "property", f.status}
	testCode(t, dbBatch(t, k, e, []any{command}, pairs...), "APPROVAL_REQUIRED")
	command["confirmed"] = true
	request := testRequest(k, e, []any{command}, dbGuards(t, k, pairs...)...)
	testOK(t, testDispatch(t, k, e, "batch", request))
	replay := testOK(t, testDispatch(t, k, e, "batch", request))
	if replay["replayed"] != true {
		t.Fatal(replay)
	}
	stale := testRequest(k, e, []any{command}, dbGuards(t, k, pairs...)...)
	config.Buttons[0].Value["value"] = f.open
	testOK(t, p2Configure(t, k, e, f, config))
	testCode(t, testDispatch(t, k, e, "batch", stale), "CONFLICT")
	other := newDatabase(t, k, e)
	outside, op := newRecord(t, k, e, other, "Out of scope")
	command["recordId"] = outside
	testCode(t, dbBatch(t, k, e, []any{command}, "collection", f.collection, "record", outside, "page", op, "property", f.status), "SCOPE_DENIED")
	command["recordId"] = r
	ai := k.Grant(contracts.ServiceActor{Kind: "local_ai", ActorId: contracts.DomainId(NewID())}, "ai", e.Scope)
	testCode(t, dbBatch(t, k, ai, []any{command}, pairs...), "APPROVAL_REQUIRED")
	testOK(t, dbBatch(t, k, e, []any{map[string]any{"kind": "collection.configure", "collectionId": f.collection, "locked": true}}, "collection", f.collection))
	testCode(t, dbBatch(t, k, e, []any{command}, pairs...), "SCOPE_DENIED")
}
func TestP2PortableExportRetainsInertConfiguration(t *testing.T) {
	k, e := testKernel(t)
	f := newDatabase(t, k, e)
	config := defaultP2Config()
	config.Form.Enabled = true
	config.Buttons = []P2RowButton{{ID: NewID(), Label: "Reviewed only", PropertyID: f.status, Value: map[string]any{"type": "status", "schemaVersion": 1, "value": f.done}, ConfirmationRequired: true}}
	testOK(t, p2Configure(t, k, e, f, config))
	tx, err := k.db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	metadata, err := exportPhase4Metadata(context.Background(), tx)
	tx.Rollback()
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(metadata)
	if !strings.Contains(string(raw), "Reviewed only") || !strings.Contains(string(raw), "p2DatabaseConfigs") {
		t.Fatal(string(raw))
	}
	destination := filepath.Join(t.TempDir(), "p2.zip")
	if _, err = k.ExportPortable(context.Background(), e, destination); err != nil {
		t.Fatal(err)
	}
	archive, err := zip.OpenReader(destination)
	if err != nil {
		t.Fatal(err)
	}
	defer archive.Close()
	entries := map[string]string{}
	for _, f := range archive.File {
		reader, err := f.Open()
		if err != nil {
			t.Fatal(err)
		}
		data, err := io.ReadAll(reader)
		reader.Close()
		if err != nil {
			t.Fatal(err)
		}
		entries[f.Name] = string(data)
	}
	restored, owner := testKernel(t)
	plan := importFixture(t, restored, owner, entries)
	if !strings.Contains(string(marshal(plan)), "Reviewed only") {
		t.Fatal("Portable import dropped row action evidence")
	}
	completeImportFixture(t, restored, owner, plan)
	var active int
	if err := restored.db.QueryRow("SELECT count(*) FROM p2_database_config").Scan(&active); err != nil || active != 0 {
		t.Fatal("Imported actions were activated", err, active)
	}
}
