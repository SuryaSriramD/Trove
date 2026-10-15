package workspace

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"trove.local/phase0/contracts"
)

func autofillFixture(t *testing.T, k *Kernel, e contracts.ServiceExecutionContext, f databaseFixture, records []string, targets []string, overwrites bool) AutofillBatch {
	t.Helper()
	r := testOK(t, testDispatch(t, k, e, "autofill.create", map[string]any{"contractVersion": 1, "requestId": NewID(), "workspaceId": k.ID(), "input": AutofillInput{CollectionID: f.collection, RecordIDs: records, SourcePropertyIDs: []string{f.title, f.status}, TargetPropertyIDs: targets, Instructions: "Extract only the stated due date", ModelPackID: NewID(), IncludeNonempty: overwrites}}))
	if err := contracts.Validate("service", "AutofillBatch", marshal(r)); err != nil {
		t.Fatal(err)
	}
	var b AutofillBatch
	if err := json.Unmarshal(marshal(r), &b); err != nil {
		t.Fatal(err)
	}
	return b
}
func autofillGenerate(t *testing.T, k *Kernel, e contracts.ServiceExecutionContext, id string, g AIGenerator) AutofillBatch {
	t.Helper()
	b, err := k.StartAutofill(context.Background(), e, id)
	if err != nil {
		t.Fatal(err)
	}
	if err = k.GenerateAutofill(context.Background(), e, b, g, nil); err != nil {
		t.Fatal(err)
	}
	b, err = k.Autofill(context.Background(), e, id)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
func autofillApplyFixture(t *testing.T, k *Kernel, e contracts.ServiceExecutionContext, b AutofillBatch, ids, overwrite []string) map[string]any {
	t.Helper()
	return testDispatch(t, k, e, "autofill.apply", map[string]any{"contractVersion": 1, "requestId": NewID(), "workspaceId": k.ID(), "batchId": b.ID, "cellIds": ids, "overwriteCellIds": overwrite})
}
func autofillDateGenerator(ctx context.Context, system, prompt string, n int, grammar string, stream func(string)) (string, error) {
	return `{"outcome":"value","value":{"start":"2026-10-20","end":null,"hasTime":false,"timezone":"UTC"},"reason":"Stated in selected title"}`, nil
}
func TestAutofillReviewedTypedApplyRetryUndoPreservesUnrelated(t *testing.T) {
	k, e := testKernel(t)
	f := newDatabase(t, k, e)
	r, p := newRecord(t, k, e, f, "Due 2026-10-20")
	b := autofillFixture(t, k, e, f, []string{r}, []string{f.date}, false)
	if !strings.Contains(string(marshal(b.Cells[0].Sources)), "Open") {
		t.Fatal("selected option labels absent from source evidence")
	}
	b = autofillGenerate(t, k, e, b.ID, autofillDateGenerator)
	if b.State != "review" || b.Cells[0].State != "value" {
		t.Fatal(b)
	}
	testOK(t, autofillApplyFixture(t, k, e, b, []string{b.Cells[0].ID}, []string{}))
	testOK(t, autofillApplyFixture(t, k, e, b, []string{b.Cells[0].ID}, []string{}))
	var n int
	k.db.QueryRow("SELECT count(*) FROM operation_journal WHERE id=?", b.ID).Scan(&n)
	if n != 1 {
		t.Fatal("duplicate journal", n)
	}
	testOK(t, dbBatch(t, k, e, []any{map[string]any{"kind": "page.update", "pageId": p, "title": "Later unrelated title"}}, "page", p, "record", r, "collection", f.collection))
	testOK(t, dbBatch(t, k, e, []any{map[string]any{"kind": "operation.undo", "operationId": b.ID}}, "page", p, "record", r, "collection", f.collection, "property", f.date))
	var title string
	k.db.QueryRow("SELECT title FROM pages WHERE id=?", p).Scan(&title)
	if title != "Later unrelated title" {
		t.Fatal("undo rewound unrelated edit")
	}
	k.db.QueryRow("SELECT count(*) FROM property_values WHERE record_id=? AND property_id=?", r, f.date).Scan(&n)
	if n != 0 {
		t.Fatal("undo did not restore missing")
	}
	saved, err := k.Autofill(context.Background(), e, b.ID)
	if err != nil || len(saved.AppliedIDs) != 1 || saved.Cells[0].GeneratedAt == "" {
		t.Fatal(saved, err)
	}
}
func TestAutofillOverwriteReviewAndFalseZeroAreNonempty(t *testing.T) {
	k, e := testKernel(t)
	f := newDatabase(t, k, e)
	r, p := newRecord(t, k, e, f, "Due 2026-10-20")
	old := map[string]any{"start": "2026-10-19", "end": nil, "hasTime": false, "timezone": "UTC"}
	testOK(t, setValue(t, k, e, f, r, p, f.date, "date", old))
	b := autofillFixture(t, k, e, f, []string{r}, []string{f.date}, false)
	if !b.Cells[0].Overwrite || b.Cells[0].State != "skipped" {
		t.Fatal(b)
	}
	for _, v := range []any{false, map[string]any{"decimal": "0"}, "0"} {
		if p2Empty(v) {
			t.Fatal("manual false/zero considered empty", v)
		}
	}
	b = autofillFixture(t, k, e, f, []string{r}, []string{f.date}, true)
	b = autofillGenerate(t, k, e, b.ID, autofillDateGenerator)
	testCode(t, autofillApplyFixture(t, k, e, b, []string{b.Cells[0].ID}, []string{}), "APPROVAL_REQUIRED")
	testOK(t, autofillApplyFixture(t, k, e, b, []string{b.Cells[0].ID}, []string{b.Cells[0].ID}))
	testOK(t, dbBatch(t, k, e, []any{map[string]any{"kind": "operation.undo", "operationId": b.ID}}, "page", p, "record", r, "collection", f.collection, "property", f.date))
	var raw string
	k.db.QueryRow("SELECT value_json FROM property_values WHERE record_id=? AND property_id=?", r, f.date).Scan(&raw)
	if raw != string(marshal(old)) {
		t.Fatal("previous manual date lost", raw)
	}
}
func TestAutofillStaleRowsSchemaAndAtomicFailure(t *testing.T) {
	for _, change := range []string{"row", "schema", "locked"} {
		t.Run(change, func(t *testing.T) {
			k, e := testKernel(t)
			f := newDatabase(t, k, e)
			r, p := newRecord(t, k, e, f, "Due 2026-10-20")
			r2, _ := newRecord(t, k, e, f, "Due 2026-10-20")
			b := autofillFixture(t, k, e, f, []string{r, r2}, []string{f.date}, false)
			b = autofillGenerate(t, k, e, b.ID, autofillDateGenerator)
			switch change {
			case "row":
				testOK(t, dbBatch(t, k, e, []any{map[string]any{"kind": "page.update", "pageId": p, "title": "New manual source"}}, "page", p, "record", r, "collection", f.collection))
			case "schema":
				extra := NewID()
				testOK(t, dbBatch(t, k, e, []any{map[string]any{"kind": "collection.addProperty", "collectionId": f.collection, "propertyId": extra, "name": "Another", "type": "text", "schemaVersion": 1, "config": map[string]any{}}}, "collection", f.collection, "property", extra))
			case "locked":
				testOK(t, dbBatch(t, k, e, []any{map[string]any{"kind": "collection.configure", "collectionId": f.collection, "locked": true}}, "collection", f.collection))
			}
			result := autofillApplyFixture(t, k, e, b, []string{b.Cells[0].ID, b.Cells[1].ID}, []string{})
			if result["ok"] == true {
				t.Fatal("stale batch applied")
			}
			var n int
			k.db.QueryRow("SELECT count(*) FROM property_values WHERE property_id=?", f.date).Scan(&n)
			if n != 0 {
				t.Fatal("partial canonical writes")
			}
		})
	}
}
func TestAutofillCancellationResumeRestartAndNativeBackup(t *testing.T) {
	k, e := testKernel(t)
	f := newDatabase(t, k, e)
	r, _ := newRecord(t, k, e, f, "Due 2026-10-20")
	r2, _ := newRecord(t, k, e, f, "Due 2026-10-20")
	b := autofillFixture(t, k, e, f, []string{r, r2}, []string{f.date}, false)
	b, err := k.StartAutofill(context.Background(), e, b.ID)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	calls := 0
	err = k.GenerateAutofill(ctx, e, b, func(ctx context.Context, s, p string, n int, g string, stream func(string)) (string, error) {
		calls++
		return autofillDateGenerator(ctx, s, p, n, g, stream)
	}, func(b AutofillBatch) {
		if b.Cells[0].State == "value" {
			cancel()
		}
	})
	if !errors.Is(err, context.Canceled) || calls != 1 {
		t.Fatal("cancel did not bound generation", err, calls)
	}
	// An interrupted generation remains inert in restart and native snapshots.
	path := k.Path()
	k.Close()
	k, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer k.Close()
	e = k.OwnerContext()
	b, err = k.Autofill(context.Background(), e, b.ID)
	if err != nil || b.State != "interrupted" || b.Cells[0].State != "value" || b.Cells[1].State != "pending" {
		t.Fatal(b, err)
	}
	calls = 0
	b = autofillGenerate(t, k, e, b.ID, func(ctx context.Context, s, p string, n int, g string, stream func(string)) (string, error) {
		calls++
		return autofillDateGenerator(ctx, s, p, n, g, stream)
	})
	if calls != 1 {
		t.Fatal("resume regenerated completed effects")
	}
	backup := filepath.Join(t.TempDir(), "backup")
	if _, err = k.CreateBackup(context.Background(), e, backup); err != nil {
		t.Fatal(err)
	}
	if _, err = VerifyBackup(context.Background(), backup); err != nil {
		t.Fatal(err)
	}
}
func TestAutofillMalformedUnsupportedAndNoValueOutcomes(t *testing.T) {
	k, e := testKernel(t)
	f := newDatabase(t, k, e)
	r, _ := newRecord(t, k, e, f, "No facts")
	for _, raw := range []string{`{"outcome":"value","value":"invented-option","reason":"guess"}`, `{"outcome":"no_value","value":null,"reason":"Missing source evidence"}`, `{"outcome":"value","value":"x","reason":"x","tool":"execute"}`, `not JSON`} {
		b := autofillFixture(t, k, e, f, []string{r}, []string{f.status}, true)
		b = autofillGenerate(t, k, e, b.ID, func(context.Context, string, string, int, string, func(string)) (string, error) { return raw, nil })
		want := "error"
		if strings.Contains(raw, "no_value") {
			want = "no_value"
		}
		if b.Cells[0].State != want {
			t.Fatal(b)
		}
		if autofillApplyFixture(t, k, e, b, []string{b.Cells[0].ID}, []string{b.Cells[0].ID})["ok"] == true {
			t.Fatal("invalid value applied")
		}
	}
}
func TestAutofillStopsOnSourceChangeBetweenCells(t *testing.T) {
	k, e := testKernel(t)
	f := newDatabase(t, k, e)
	r, p := newRecord(t, k, e, f, "Due 2026-10-20")
	r2, _ := newRecord(t, k, e, f, "Due 2026-10-20")
	b := autofillFixture(t, k, e, f, []string{r, r2}, []string{f.date}, false)
	b, err := k.StartAutofill(context.Background(), e, b.ID)
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	err = k.GenerateAutofill(context.Background(), e, b, func(ctx context.Context, s, p string, n int, g string, stream func(string)) (string, error) {
		calls++
		return autofillDateGenerator(ctx, s, p, n, g, stream)
	}, func(b AutofillBatch) {
		if b.State == "running" {
			testOK(t, dbBatch(t, k, e, []any{map[string]any{"kind": "page.update", "pageId": p, "title": "Changed source"}}, "page", p, "record", r, "collection", f.collection))
		}
	})
	if err == nil || calls != 1 {
		t.Fatal("stale sources continued generating", err, calls)
	}
}

func TestAutofillRevocationStopsFlightAndRetainsResumableState(t *testing.T) {
	k, e := testKernel(t)
	f := newDatabase(t, k, e)
	r, _ := newRecord(t, k, e, f, "Due 2026-10-20")
	r2, _ := newRecord(t, k, e, f, "Due 2026-10-20")
	scoped := k.Grant(e.Actor, "user", e.Scope)
	b := autofillFixture(t, k, scoped, f, []string{r, r2}, []string{f.date}, false)
	b, err := k.StartAutofill(context.Background(), scoped, b.ID)
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	err = k.GenerateAutofill(context.Background(), scoped, b, func(ctx context.Context, s, p string, n int, g string, stream func(string)) (string, error) {
		calls++
		return autofillDateGenerator(ctx, s, p, n, g, stream)
	}, func(b AutofillBatch) {
		if b.State == "running" {
			k.RevokeGrant(string(scoped.ScopeGrantId))
		}
	})
	if err == nil || calls != 1 {
		t.Fatal("revoked batch continued", err, calls)
	}
	saved, err := k.Autofill(context.Background(), e, b.ID)
	if err != nil || saved.State != "interrupted" || saved.Cells[1].State != "pending" {
		t.Fatal("revocation stranded running state", saved, err)
	}
	b = autofillGenerate(t, k, e, b.ID, autofillDateGenerator)
	if b.State != "review" {
		t.Fatal(b)
	}
}
func TestAutofillRejectsModelChangeAndSourceChangeDuringGeneration(t *testing.T) {
	k, e := testKernel(t)
	f := newDatabase(t, k, e)
	r, p := newRecord(t, k, e, f, "Due 2026-10-20")
	b := autofillFixture(t, k, e, f, []string{r}, []string{f.date}, false)
	model := AutofillModel{PackID: b.Input.ModelPackID, SHA256: strings.Repeat("a", 64), SourceRevision: "fixture-source-v1", RuntimeCommit: "fixture-runtime-v1"}
	if err := k.BindAutofillModel(context.Background(), e, b.ID, model); err != nil {
		t.Fatal(err)
	}
	model.SHA256 = strings.Repeat("b", 64)
	if err := k.BindAutofillModel(context.Background(), e, b.ID, model); err == nil {
		t.Fatal("model provenance replaced")
	}
	b, err := k.StartAutofill(context.Background(), e, b.ID)
	if err != nil {
		t.Fatal(err)
	}
	err = k.GenerateAutofill(context.Background(), e, b, func(ctx context.Context, s, pr string, n int, g string, stream func(string)) (string, error) {
		testOK(t, dbBatch(t, k, e, []any{map[string]any{"kind": "page.update", "pageId": p, "title": "Newer manual source"}}, "page", p, "record", r, "collection", f.collection))
		return autofillDateGenerator(ctx, s, pr, n, g, stream)
	}, nil)
	if err == nil {
		t.Fatal("stale generation result saved")
	}
	b, err = k.Autofill(context.Background(), e, b.ID)
	if err != nil || b.Cells[0].State != "pending" || b.State != "interrupted" {
		t.Fatal(b, err)
	}
}

func TestAutofillStrictOutputRejectsDuplicateKeys(t *testing.T) {
	for _, raw := range []string{`{"outcome":"value","value":false,"reason":"x","value":true}`, `{"outcome":"no_value","value":null,"reason":"missing"} trailing`, `{"outcome":"value","value":true,"reason":"x","reason":"y"}`} {
		state, _, _ := decodeAutofill(raw)
		if state != "error" {
			t.Fatal("malformed transport accepted", raw)
		}
	}
}
