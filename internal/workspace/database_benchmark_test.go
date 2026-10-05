package workspace

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"trove.local/phase0/contracts"
)

// This opt-in benchmark extends TestRecordQueryTenThousandRows. It is not a
// release SLA test: the runner records source/host binding and raw samples.
// The expectation oracle below uses source-row integers, never the production
// comparator, filter, query builder, SQL, or formula evaluator.
type dbBenchRow struct {
	Index, Precision, Effort, Day, Order, Related int
	ID, Page, Text                                string
	Deleted, MissingNumber, MissingDate           bool
}
type dbBenchCase struct {
	Name, View string
	Query      map[string]any
	Accept     func(dbBenchRow) bool
	Less       func(dbBenchRow, dbBenchRow) bool
	Derived    bool
}
type dbBenchSample struct {
	Milliseconds   float64 `json:"ms"`
	Bytes          int     `json:"responseBytes"`
	AllocatedBytes uint64  `json:"goAllocatedBytes"`
	Allocations    uint64  `json:"goAllocations"`
}
type dbBenchEnvelope struct {
	OK     bool                                `json:"ok"`
	Result contracts.ServiceQueryRecordsResult `json:"result"`
	Error  struct{ Code, Message string }      `json:"error"`
}

func dbBenchID(group, n int) string { return fmt.Sprintf("019a0000-%04x-7000-8000-%012d", group, n+1) }
func dbBenchDigest(ids []string) string {
	sum := sha256.Sum256([]byte(strings.Join(ids, "\n")))
	return hex.EncodeToString(sum[:])
}
func dbBenchStats(samples []dbBenchSample) map[string]any {
	values := make([]float64, len(samples))
	sum := 0.
	var allocated uint64
	for i, s := range samples {
		values[i] = s.Milliseconds
		sum += s.Milliseconds
		allocated += s.AllocatedBytes
	}
	sort.Float64s(values)
	quantile := func(q float64) float64 { return values[int(math.Ceil(q*float64(len(values))))-1] }
	return map[string]any{"samples": samples, "count": len(samples), "meanMs": sum / float64(len(samples)), "p50Ms": quantile(.5), "p95Ms": quantile(.95), "maxMs": values[len(values)-1], "meanGoAllocatedBytes": float64(allocated) / float64(len(samples))}
}
func dbBenchMeasure(t *testing.T, call func() []byte) (dbBenchEnvelope, dbBenchSample) {
	t.Helper()
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	start := time.Now()
	body := call()
	elapsed := time.Since(start)
	runtime.ReadMemStats(&after)
	var out dbBenchEnvelope
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatal(err)
	}
	return out, dbBenchSample{Milliseconds: float64(elapsed) / float64(time.Millisecond), Bytes: len(body), AllocatedBytes: after.TotalAlloc - before.TotalAlloc, Allocations: after.Mallocs - before.Mallocs}
}
func TestDatabasePerformance10K(t *testing.T) {
	output := os.Getenv("TROVE_DATABASE_BENCHMARK_OUTPUT")
	if output == "" {
		t.Skip("opt in with scripts/benchmark-database.py")
	}
	iterations := 7
	if value := os.Getenv("TROVE_DATABASE_BENCHMARK_ITERATIONS"); value != "" {
		var err error
		iterations, err = strconv.Atoi(value)
		if err != nil || iterations < 3 || iterations > 30 {
			t.Fatal("benchmark iterations must be 3..30")
		}
	}
	const count = 10000
	started := time.Now()
	k, e := testKernel(t)
	f := newDatabase(t, k, e)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	amount, effort, text, choice, tags, check, relation, formula, rollup := NewID(), NewID(), NewID(), NewID(), NewID(), NewID(), NewID(), NewID(), NewID()
	options := []string{NewID(), NewID(), NewID()}
	tagOptions := []string{NewID(), NewID()}
	optionConfig := func(ids []string) map[string]any {
		out := []any{}
		for i, id := range ids {
			out = append(out, map[string]any{"id": id, "label": fmt.Sprintf("Option %d", i)})
		}
		return map[string]any{"options": out}
	}
	defs := []struct {
		id, kind, name string
		config         any
	}{{amount, "number", "Exact amount", map[string]any{"format": "decimal"}}, {effort, "number", "Effort", map[string]any{"format": "decimal"}}, {text, "text", "Notes", map[string]any{}}, {choice, "select", "Team", optionConfig(options)}, {tags, "multi_select", "Tags", optionConfig(tagOptions)}, {check, "checkbox", "Flagged", map[string]any{}}, {relation, "relation", "Related work", map[string]any{"targetCollectionId": f.collection, "bidirectionalPropertyId": nil}}, {formula, "formula", "Effort score", derivedConfig("formula", map[string]any{"translatedExpression": fmt.Sprintf("prop(%q) * 2 + 1", effort)})}, {rollup, "rollup", "Related effort", derivedConfig("rollup", map[string]any{"relationPropertyId": relation, "targetPropertyId": effort, "aggregation": "sum"})}}
	for _, p := range defs {
		testOK(t, dbBatch(t, k, e, []any{map[string]any{"kind": "collection.addProperty", "collectionId": f.collection, "propertyId": p.id, "name": p.name, "type": p.kind, "schemaVersion": 1, "config": p.config}}, "collection", f.collection, "property", p.id))
	}
	rows := make([]dbBenchRow, count)
	byID := map[string]dbBenchRow{}
	live := 0
	for i := range rows {
		row := dbBenchRow{Index: i, ID: dbBenchID(1, i), Page: dbBenchID(2, i), Precision: i * 37 % 997, Effort: i % 50, Day: i % 60, Order: i * 73 % count, Related: 1 + i%50, Deleted: i >= 100 && i%91 == 0, MissingNumber: i%11 == 0, MissingDate: i%13 == 0}
		row.Text = fmt.Sprintf("Planning memo %05d. ", i) + strings.Repeat("Typed evidence and delivery notes. ", 8)
		if i%7 == 0 {
			row.Text += "Café తెలుగు 世界 research"
		}
		if i == 1337 {
			row.Text += " Literal x' OR 1=1 -- marker"
		}
		rows[i] = row
		byID[row.ID] = row
		if !row.Deleted {
			live++
		}
	}
	// Canonical schema/property/view creation uses guarded services. Bulk record
	// setup follows the existing 10k fixture and excludes mutation/event costs.
	tx, err := k.writeDB.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	prepare := func(query string) *sql.Stmt {
		statement, err := tx.PrepareContext(ctx, query)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { statement.Close() })
		return statement
	}
	pages := prepare("INSERT INTO pages(id,workspace_id,title,revision,created_at,updated_at,deleted_at) VALUES(?,?,?,'1','2026-10-01T00:00:00Z','2026-10-01T00:00:00Z',?)")
	records := prepare("INSERT INTO records(id,collection_id,page_id,sort_key,revision) VALUES(?,?,?,?,'1')")
	values := prepare("INSERT INTO property_values(record_id,collection_id,property_id,property_type,schema_version,value_json) VALUES(?,?,?,?,1,?)")
	blocks := prepare("INSERT INTO blocks(id,page_id,sort_key,type,schema_version,data_json,revision) VALUES(?,?,'000000000001','paragraph',1,?,'1')")
	insert := func(statement *sql.Stmt, args ...any) {
		if _, err := statement.ExecContext(ctx, args...); err != nil {
			t.Fatal(err)
		}
	}
	valueCount, bodyBytes := 0, 0
	for _, row := range rows {
		var deleted any
		if row.Deleted {
			deleted = "2026-10-02T00:00:00Z"
		}
		insert(pages, row.Page, k.ID(), fmt.Sprintf("Research work %05d", row.Index), deleted)
		insert(records, row.ID, f.collection, row.Page, fmt.Sprintf("%012d", row.Order))
		body := strings.Repeat(fmt.Sprintf("Research note %d — structured tasks, evidence, and project decisions. ", row.Index), 6)
		bodyBytes += len(body)
		insert(blocks, dbBenchID(3, row.Index), row.Page, string(marshal(map[string]any{"richText": []any{map[string]any{"text": body}}})))
		set := func(id, kind string, value any) {
			if err := contracts.Validate("domain", "property_"+kind+"_v1", marshal(value)); err != nil {
				t.Fatalf("invalid fixture %s: %v", kind, err)
			}
			insert(values, row.ID, f.collection, id, kind, string(marshal(value)))
			valueCount++
		}
		status := f.open
		if row.Index%3 == 2 {
			status = f.done
		}
		set(f.status, "status", status)
		set(effort, "number", map[string]any{"decimal": strconv.Itoa(row.Effort), "format": "decimal"})
		set(text, "text", []any{map[string]any{"text": row.Text, "marks": []string{"bold"}}})
		set(choice, "select", options[row.Index%3])
		set(check, "checkbox", row.Index%3 == 0)
		selected := []string{}
		if row.Index%2 == 0 {
			selected = append(selected, tagOptions[0])
		}
		if row.Index%5 == 0 {
			selected = append(selected, tagOptions[1])
		}
		set(tags, "multi_select", selected)
		set(relation, "relation", []string{dbBenchID(1, row.Related)})
		if !row.MissingNumber {
			set(amount, "number", map[string]any{"decimal": fmt.Sprintf("9007199254740993.%06d", row.Precision), "format": "decimal"})
		}
		if !row.MissingDate {
			instant := time.Date(2026, 1, 1, 8, 0, 0, 0, time.UTC).AddDate(0, 0, row.Day)
			if row.Index%2 == 0 {
				instant = instant.In(time.FixedZone("+0530", 19800))
			}
			set(f.date, "date", map[string]any{"start": instant.Format(time.RFC3339), "end": nil, "hasTime": true, "timezone": "Asia/Kolkata"})
		}
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO relation_edges(property_id,record_id,target_record_id) SELECT v.property_id,v.record_id,j.value FROM property_values v,json_each(v.value_json) j WHERE v.property_type='relation'`); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	var integrity string
	if err = k.db.QueryRow("PRAGMA integrity_check").Scan(&integrity); err != nil || integrity != "ok" {
		t.Fatal(integrity, err)
	}
	fk, err := k.db.Query("PRAGMA foreign_key_check")
	if err != nil {
		t.Fatal(err)
	}
	if fk.Next() {
		t.Fatal("fixture foreign-key violation")
	}
	fk.Close()
	filter := func(property, op string, value any) map[string]any {
		return map[string]any{"propertyId": property, "op": op, "value": value}
	}
	and := func(terms ...any) map[string]any { return map[string]any{"op": "and", "terms": terms} }
	query := func(condition any, property, direction string) map[string]any {
		q := emptyQuery(f.title, f.status, f.date, amount, effort, text, choice, tags, check, relation, formula, rollup)
		q["filter"] = condition
		if property != "" {
			q["sorts"] = []any{map[string]any{"propertyId": property, "direction": direction}}
		}
		return q
	}
	order := func(a, b dbBenchRow) bool { return a.Order < b.Order }
	precision := func(a, b dbBenchRow) bool {
		if a.Precision != b.Precision {
			return a.Precision > b.Precision
		}
		return order(a, b)
	}
	dateOrder := func(a, b dbBenchRow) bool {
		if a.Day != b.Day {
			return a.Day < b.Day
		}
		return order(a, b)
	}
	cases := []dbBenchCase{
		{Name: "all_live_default_order", Query: query(nil, "", ""), Accept: func(dbBenchRow) bool { return true }, Less: order},
		{Name: "exact_decimal_and_status", Query: query(and(filter(amount, "gte", map[string]any{"decimal": "9007199254740993.000700", "format": "decimal"}), filter(f.status, "eq", f.open)), amount, "desc"), Accept: func(r dbBenchRow) bool { return !r.MissingNumber && r.Precision >= 700 && r.Index%3 != 2 }, Less: precision},
		{Name: "timed_date_and_checkbox", Query: query(and(filter(f.date, "gte", map[string]any{"start": "2026-01-31T08:00:00Z", "end": nil, "hasTime": true, "timezone": "UTC"}), filter(check, "eq", true)), f.date, "asc"), Accept: func(r dbBenchRow) bool { return !r.MissingDate && r.Day >= 30 && r.Index%3 == 0 }, Less: dateOrder},
		{Name: "unicode_text_contains", Query: query(filter(text, "contains", "CAFÉ"), "", ""), Accept: func(r dbBenchRow) bool { return r.Index%7 == 0 }, Less: order},
		{Name: "bound_literal_not_sql", Query: query(filter(text, "contains", "x' OR 1=1 --"), "", ""), Accept: func(r dbBenchRow) bool { return r.Index == 1337 }, Less: order},
		{Name: "multi_select_and_missing_number", Query: query(and(filter(tags, "contains", tagOptions[1]), filter(amount, "is_empty", nil)), "", ""), Accept: func(r dbBenchRow) bool { return r.Index%5 == 0 && r.MissingNumber }, Less: order},
		{Name: "formula_filter_sort", Query: query(filter(formula, "gte", map[string]any{"decimal": "75", "format": "decimal"}), formula, "desc"), Accept: func(r dbBenchRow) bool { return r.Effort*2+1 >= 75 }, Less: func(a, b dbBenchRow) bool {
			if a.Effort != b.Effort {
				return a.Effort > b.Effort
			}
			return order(a, b)
		}, Derived: true},
		{Name: "relation_rollup_filter_sort", Query: query(filter(rollup, "gte", map[string]any{"decimal": "40", "format": "decimal"}), rollup, "asc"), Accept: func(r dbBenchRow) bool { return r.Related%50 >= 40 }, Less: func(a, b dbBenchRow) bool {
			aValue, bValue := a.Related%50, b.Related%50
			if aValue != bValue {
				return aValue < bValue
			}
			return order(a, b)
		}, Derived: true},
	}
	for i := range cases {
		c := &cases[i]
		c.View = NewID()
		testOK(t, dbBatch(t, k, e, []any{map[string]any{"kind": "view.save", "viewId": c.View, "collectionId": f.collection, "owningPageId": f.page, "name": c.Name, "type": "table", "schemaVersion": 1, "config": map[string]any{"columnWidths": []any{}}, "query": c.Query}}, "view", c.View, "collection", f.collection, "page", f.page))
	}
	// Bind a permitted provider subset without widening the 100-page pairing cap.
	bridge := NewProviderBridge(k, e)
	defer bridge.Close()
	selectedPages := []string{f.page}
	selected := map[string]bool{}
	for i := 1; len(selectedPages) < 100; i += 97 {
		r := rows[i%count]
		if !r.Deleted && !selected[r.ID] {
			selectedPages = append(selectedPages, r.Page)
			selected[r.ID] = true
		}
	}
	paired, err := bridge.Connect(ctx, "mcp", selectedPages, []string{"queryRecords"})
	if err != nil {
		t.Fatal(err)
	}
	makeCall := func(view, cursor string, provider bool) func() []byte {
		request := map[string]any{"collectionId": f.collection, "viewId": view, "limit": 200}
		if cursor != "" {
			request["cursor"] = cursor
		}
		if provider {
			return func() []byte {
				raw, err := bridge.Call(ctx, paired.ID, paired.Token, "queryRecords", request)
				if err != nil {
					t.Fatal(err)
				}
				return raw
			}
		}
		request["contractVersion"] = 1
		request["requestId"] = NewID()
		request["workspaceId"] = k.ID()
		raw := marshal(request)
		return func() []byte { return k.Dispatch(ctx, e, "queryRecords", raw) }
	}
	expected := func(c dbBenchCase, provider bool) []string {
		found := []dbBenchRow{}
		for _, row := range rows {
			if !row.Deleted && c.Accept(row) && (!provider || selected[row.ID]) {
				found = append(found, row)
			}
		}
		sort.Slice(found, func(i, j int) bool { return c.Less(found[i], found[j]) })
		out := []string{}
		for _, r := range found {
			out = append(out, r.ID)
		}
		return out
	}
	validate := func(out dbBenchEnvelope, want []string, provider bool) {
		t.Helper()
		if !out.OK {
			t.Fatalf("query failed %s: %s", out.Error.Code, out.Error.Message)
		}
		got := []string{}
		for _, id := range out.Result.RecordIds {
			got = append(got, string(id))
		}
		if !slices.Equal(got, want) {
			t.Fatalf("independent IDs differ: got %d %v, wanted %d %v", len(got), got[:min(4, len(got))], len(want), want[:min(4, len(want))])
		}
		if len(out.Result.Records) != len(want) || len(want) > 200 {
			t.Fatal("hydration count mismatch")
		}
		for i, record := range out.Result.Records {
			r := byID[want[i]]
			if string(record.Id) != r.ID || string(record.PageId) != r.Page {
				t.Fatal("canonical identity mismatch")
			}
			values := map[string]json.RawMessage{}
			for _, v := range record.Values {
				values[string(v.PropertyId)] = v.Value
			}
			if r.MissingNumber {
				if len(values[amount]) > 0 {
					t.Fatal("missing numeric value appeared")
				}
			} else {
				var amountValue struct {
					Decimal string `json:"decimal"`
				}
				json.Unmarshal(values[amount], &amountValue)
				if amountValue.Decimal != fmt.Sprintf("9007199254740993.%06d", r.Precision) {
					t.Fatal("precision lost")
				}
			}
			for _, derived := range []struct {
				id   string
				want int
			}{{formula, r.Effort*2 + 1}, {rollup, r.Related % 50}} {

				var value struct {
					Raw struct {
						Status     string `json:"status"`
						Diagnostic string `json:"diagnostic"`
						Value      struct {
							Decimal string `json:"decimal"`
						} `json:"value"`
					} `json:"raw"`
				}
				json.Unmarshal(values[derived.id], &value)
				if provider && derived.id == rollup && !selected[dbBenchID(1, r.Related)] {
					if value.Raw.Status != "error" || value.Raw.Value.Decimal != "" || value.Raw.Diagnostic != "related record is unavailable in this scope" {
						t.Fatalf("out-of-scope rollup leaked: %s", values[derived.id])
					}
					continue
				}
				if value.Raw.Status != "computed" || value.Raw.Value.Decimal != strconv.Itoa(derived.want) {
					t.Fatalf("independent derived value differs: row %d property %s got %s want %d", r.Index, derived.id, string(values[derived.id]), derived.want)
				}
			}
		}
	}
	report := map[string]any{"schemaVersion": 1, "completed": false, "startedAt": started.UTC().Format(time.RFC3339), "workload": map[string]any{"records": count, "liveRecords": live, "deletedRecords": count - live, "properties": 12, "storedTypedValues": valueCount, "paragraphBodies": count, "bodyUTF8Bytes": bodyBytes, "precisionBase": "9007199254740993", "formula": "effort * 2 + 1", "formulaEvaluatorVersion": 1, "relationTargets": 50, "pageSize": 200, "providerSelectedRecordPages": 99, "setup": "Canonical schema and view services; prepared direct SQL bulk record fixture, integrity/FK checked; mutation/import/event cost excluded", "queryCases": len(cases)}, "iterations": iterations, "scope": "Go Kernel.Dispatch validation/query/response JSON serialization; caller JSON decode and independent assertions excluded. ProviderBridge.Call is in-process, not Wails IPC/network/paint. OS and SQLite caches are not flushed; no cold-start claim.", "profileWarmupQueriesPerCase": 1, "cases": []any{}}
	writeReport := func() {
		body, err := json.MarshalIndent(report, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		if err = os.MkdirAll(filepath.Dir(output), 0700); err != nil {
			t.Fatal(err)
		}
		if err = os.WriteFile(output, append(body, '\n'), 0600); err != nil {
			t.Fatal(err)
		}
	}
	report["setupSeconds"] = time.Since(started).Seconds()
	writeReport()
	profiles := append(append([]dbBenchCase{}, cases...), dbBenchCase{Name: "provider_bridge_99_selected_rows", View: cases[0].View, Query: cases[0].Query, Accept: cases[0].Accept, Less: cases[0].Less})
	for _, c := range profiles {
		provider := c.Name == "provider_bridge_99_selected_rows"
		want := expected(c, provider)
		caseStart := time.Now()
		restarts := 0
		// Full result walk proves membership, order and no gaps/duplicates, independent
		// of pagination. Minute-bound cursors are restarted at most twice, explicitly.
		walkPages := 0
		for attempt := 0; attempt < 3; attempt++ {
			cursor := ""
			offset := 0
			restart := false
			walkPages = 0
			for {
				out, _ := dbBenchMeasure(t, makeCall(c.View, cursor, provider))
				if !out.OK && out.Error.Code == "CONFLICT" {
					restarts++
					restart = true
					break
				}
				end := min(offset+200, len(want))
				validate(out, want[offset:end], provider)
				walkPages++
				offset = end
				if out.Result.NextCursor == nil {
					if offset != len(want) {
						t.Fatal("premature end of result")
					}
					break
				}
				if offset >= len(want) {
					t.Fatal("spurious next cursor")
				}
				cursor = *out.Result.NextCursor
			}
			if !restart {
				break
			}
			if attempt == 2 {
				t.Fatal("cursor kept expiring before complete correctness walk")
			}
		}
		first, firstSample := dbBenchMeasure(t, makeCall(c.View, "", provider))
		validate(first, want[:min(200, len(want))], provider)
		firstSamples := []dbBenchSample{}
		for i := 0; i < iterations; i++ {
			out, sample := dbBenchMeasure(t, makeCall(c.View, "", provider))
			validate(out, want[:min(200, len(want))], provider)
			firstSamples = append(firstSamples, sample)
		}
		row := map[string]any{"name": c.Name, "query": c.Query, "derivedOrderingSnapshot": c.Derived, "expectedRows": len(want), "expectedRecordIdsSha256": dbBenchDigest(want), "fullResultWalkPages": walkPages, "cursorRestarts": restarts, "firstObservationAfterCorrectnessWalk": firstSample, "firstPage": dbBenchStats(firstSamples), "measurementBoundary": "Kernel.Dispatch (caller request already serialized)"}
		if provider {
			row["measurementBoundary"] = "ProviderBridge.Call authentication/scope + Kernel.Dispatch + response serialization"
		}
		if len(want) > 200 {
			out, _ := dbBenchMeasure(t, makeCall(c.View, "", provider))
			validate(out, want[:200], provider)
			cursor := *out.Result.NextCursor
			nextSamples := []dbBenchSample{}
			for i := 0; i < iterations; i++ {
				out, sample := dbBenchMeasure(t, makeCall(c.View, cursor, provider))
				if !out.OK && out.Error.Code == "CONFLICT" {
					restarts++
					fresh, _ := dbBenchMeasure(t, makeCall(c.View, "", provider))
					validate(fresh, want[:200], provider)
					cursor = *fresh.Result.NextCursor
					out, sample = dbBenchMeasure(t, makeCall(c.View, cursor, provider))
				}
				validate(out, want[200:min(400, len(want))], provider)
				nextSamples = append(nextSamples, sample)
			}
			row["secondPage"] = dbBenchStats(nextSamples)
			row["cursorRestarts"] = restarts
		}
		row["caseWallSeconds"] = time.Since(caseStart).Seconds()
		report["cases"] = append(report["cases"].([]any), row)
		writeReport()
		t.Logf("DATABASE_BENCHMARK %s rows=%d first p50=%.2fms p95=%.2fms", c.Name, len(want), row["firstPage"].(map[string]any)["p50Ms"], row["firstPage"].(map[string]any)["p95Ms"])
	}
	// A canonical mutation invalidates the saved cursor, as a UI refresh must.
	initial, _ := dbBenchMeasure(t, makeCall(cases[0].View, "", false))
	validate(initial, expected(cases[0], false)[:200], false)
	testOK(t, dbBatch(t, k, e, []any{map[string]any{"kind": "page.update", "pageId": f.page, "title": "Changed after benchmark"}}, "page", f.page))
	stale, _ := dbBenchMeasure(t, makeCall(cases[0].View, *initial.Result.NextCursor, false))
	if stale.OK || stale.Error.Code != "CONFLICT" {
		t.Fatal("canonical mutation did not invalidate cursor")
	}
	if err = bridge.Disconnect(ctx, paired.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = bridge.Call(ctx, paired.ID, paired.Token, "queryRecords", map[string]any{"collectionId": f.collection, "limit": 200}); err == nil {
		t.Fatal("revoked bridge remained usable")
	}
	var peak runtime.MemStats
	runtime.ReadMemStats(&peak)
	report["endingGoHeapAllocBytes"] = peak.HeapAlloc
	report["endingGoSysBytes"] = peak.Sys
	report["canonicalMutationCursorInvalidation"] = true
	report["providerRevocationChecked"] = true
	report["completed"] = true
	report["wallSeconds"] = time.Since(started).Seconds()
	report["finishedAt"] = time.Now().UTC().Format(time.RFC3339)
	writeReport()
}
