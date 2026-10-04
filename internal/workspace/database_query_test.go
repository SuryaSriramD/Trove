package workspace

import (
	"context"
	"database/sql"
	"encoding/base64"
	"fmt"
	"strings"
	"testing"
	"time"

	"trove.local/phase0/contracts"
)

func TestRecordSQLSemanticParityAndConnectionIsolation(t *testing.T) {
	k, _ := testKernel(t)
	ctx := context.Background()
	conn, err := recordQueryConnection(ctx, k)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	cases := []struct {
		a, b any
		op   string
	}{
		{map[string]any{"decimal": "10000000000000000.00001"}, map[string]any{"decimal": "10000000000000000.00002"}, "lt"},
		{map[string]any{"start": "2026-10-01T10:00:00+05:30", "hasTime": true}, map[string]any{"start": "2026-10-01T05:00:00Z", "hasTime": true}, "lt"},
		{[]any{map[string]any{"text": "ÄPFEL World"}}, "äpfel", "contains"},
		{[]any{"partial-id"}, "partial", "contains"},
		{[]any{"ab", "c"}, []any{"a", "bc"}, "eq"},
		{[]any{"one", "two"}, []any{"two", "one"}, "eq"},
		{nil, nil, "is_empty"}, {nil, "z", "lt"},
	}
	for _, c := range cases {
		var cmp int
		var matched bool
		err = conn.QueryRowContext(ctx, "SELECT trove_query_compare(?,?),trove_query_filter(?,?,?)", string(marshal(c.a)), string(marshal(c.b)), string(marshal(c.a)), string(marshal(c.b)), c.op).Scan(&cmp, &matched)
		if err != nil || cmp != compareValue(c.a, c.b) || matched != matchesFilter(map[string]any{"propertyId": "p", "op": c.op, "value": c.b}, map[string]any{"p": c.a}) {
			t.Fatalf("semantic drift %+v: %d %v %v", c, cmp, matched, err)
		}
	}
	for _, db := range []interface {
		QueryRowContext(context.Context, string, ...any) *sql.Row
	}{k.db, k.writeDB} {
		var value int
		if err = db.QueryRowContext(ctx, "SELECT trove_query_compare('1','2')").Scan(&value); err == nil {
			t.Fatal("query function leaked into canonical or writer pool")
		}
	}
	other, _ := testKernel(t)
	var value int
	if err = other.recordQueryDB.QueryRowContext(ctx, "SELECT trove_query_compare('1','2')").Scan(&value); err == nil {
		t.Fatal("query function leaked into another kernel")
	}
}

func TestRecordCursorIntegrityScopeAndKeyset(t *testing.T) {
	k, e := testKernel(t)
	f := newDatabase(t, k, e)
	a, ap := newRecord(t, k, e, f, "same")
	b, bp := newRecord(t, k, e, f, "same")
	newRecord(t, k, e, f, "private")
	scope := contracts.DomainScope{Value: contracts.DomainScopeVariant3{Kind: "pages", WorkspaceId: contracts.DomainId(k.ID()), PageIds: []contracts.DomainId{contracts.DomainId(f.page), contracts.DomainId(ap), contracts.DomainId(bp)}}}
	scoped := k.Grant(e.Actor, "user", scope)
	first := testOK(t, queryDB(t, k, scoped, f.collection, "", 1, ""))
	cursor := first["nextCursor"].(string)
	second := testOK(t, queryDB(t, k, scoped, f.collection, "", 1, cursor))
	if first["recordIds"].([]any)[0] != a || second["recordIds"].([]any)[0] != b || second["nextCursor"] != nil {
		t.Fatal(first, second)
	}
	parts := strings.Split(cursor, ".")
	raw, _ := base64.RawURLEncoding.DecodeString(parts[0])
	raw = append(raw, ' ')
	testCode(t, queryDB(t, k, scoped, f.collection, "", 1, base64.RawURLEncoding.EncodeToString(raw)+"."+parts[1]), "INVALID_REQUEST")
	otherGrant := k.Grant(e.Actor, "user", scope)
	testCode(t, queryDB(t, k, otherGrant, f.collection, "", 1, cursor), "INVALID_REQUEST")
	k.RevokeGrant(string(scoped.ScopeGrantId))
	testCode(t, queryDB(t, k, scoped, f.collection, "", 1, cursor), "SCOPE_DENIED")
}

func TestDerivedQueryMemoReuseExpiryAndCancellation(t *testing.T) {
	k, e := testKernel(t)
	f := newDatabase(t, k, e)
	a, _ := newRecord(t, k, e, f, "A")
	b, _ := newRecord(t, k, e, f, "B")
	formula := NewID()
	testOK(t, dbBatch(t, k, e, []any{map[string]any{"kind": "collection.addProperty", "collectionId": f.collection, "propertyId": formula, "name": "Constant", "type": "formula", "schemaVersion": 1, "config": derivedConfig("formula", map[string]any{"translatedExpression": "1 + 2"})}}, "collection", f.collection, "property", formula))
	view := NewID()
	q := emptyQuery(f.title, formula)
	q["sorts"] = []any{map[string]any{"propertyId": formula, "direction": "desc"}}
	testOK(t, dbBatch(t, k, e, []any{map[string]any{"kind": "view.save", "viewId": view, "collectionId": f.collection, "owningPageId": f.page, "name": "Derived", "type": "table", "schemaVersion": 1, "config": map[string]any{"columnWidths": []any{}}, "query": q}}, "view", view, "collection", f.collection, "page", f.page))
	first := testOK(t, queryDB(t, k, e, f.collection, view, 1, ""))
	cursor := first["nextCursor"].(string)
	work := k.recordQueries.work.Load()
	second := testOK(t, queryDB(t, k, e, f.collection, view, 1, cursor))
	if first["recordIds"].([]any)[0] != a || second["recordIds"].([]any)[0] != b || k.recordQueries.builds.Load() != 1 || k.recordQueries.hits.Load() != 1 || work != 2 || k.recordQueries.work.Load() != work {
		t.Fatalf("memo not reused: %v %v work=%d", first, second, work)
	}
	k.recordQueries.mu.Lock()
	for _, entry := range k.recordQueries.entries {
		entry.Expires = time.Now().Add(-time.Minute)
	}
	k.recordQueries.mu.Unlock()
	testCode(t, queryDB(t, k, e, f.collection, view, 1, cursor), "CONFLICT")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := queryRecords(ctx, k, e, map[string]any{"collectionId": f.collection, "viewId": view, "limit": 1}); err == nil {
		t.Fatal("cancelled query succeeded")
	}
}

func TestRecordQueryCacheBoundsAndDerivedBudget(t *testing.T) {
	cache := recordQueryCache{}
	if err := cache.put(&recordQuerySnapshot{Bytes: querySnapshotBytes + 1}); err == nil {
		t.Fatal("unbounded snapshot")
	}
	for i := 0; i < 12; i++ {
		if err := cache.put(&recordQuerySnapshot{Token: fmt.Sprint(i), Bytes: querySnapshotBytes}); err != nil {
			t.Fatal(err)
		}
	}
	if len(cache.entries) != 4 || cache.bytes != queryCacheBytes {
		t.Fatal("global cache exceeds budget", len(cache.entries), cache.bytes)
	}
	if _, err := cache.get("0", recordQueryCursor{}); err == nil {
		t.Fatal("evicted snapshot remained available")
	}
	cache.close()
	if _, err := cache.seal(recordQueryCursor{}); err == nil {
		t.Fatal("closed cache sealed cursor")
	}
	k, e := testKernel(t)
	f := newDatabase(t, k, e)
	r, _ := newRecord(t, k, e, f, "record")
	tx, err := k.db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	d := newDerivedEvaluator(context.Background(), k, tx, e, time.Now(), "UTC")
	d.workLimit = 1
	if _, err = d.value(r, f.title); err != nil {
		t.Fatal(err)
	}
	if _, err = d.value(r, f.status); err == nil || !d.exhausted {
		t.Fatal("derived budget failed closed", err)
	}
}
