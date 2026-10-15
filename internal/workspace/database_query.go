package workspace

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/mattn/go-sqlite3"
	"trove.local/phase0/contracts"
)

// No driver-wide hooks: these functions exist only on a kernel's query pool.
// JSON comparison intentionally uses the same exact decimal, date, Unicode and
// array semantics as the canonical evaluator, not SQLite's numeric affinity.
func recordQueryConnection(ctx context.Context, k *Kernel) (*sql.Conn, error) {
	conn, err := k.recordQueryDB.Conn(ctx)
	if err != nil {
		return nil, err
	}
	err = conn.Raw(func(driver any) error {
		c, ok := driver.(*sqlite3.SQLiteConn)
		if !ok {
			return fmt.Errorf("unsupported record query driver")
		}
		decode := func(raw string) any { var value any; _ = json.Unmarshal([]byte(raw), &value); return value }
		if err := c.RegisterFunc("trove_query_value", func(raw, title, page string) string {
			value := decode(raw)
			if object, ok := value.(map[string]any); ok && object["pageId"] == page {
				value = title
			}
			return string(marshal(derivedQueryValue(value)))
		}, true); err != nil {
			return err
		}
		if err := c.RegisterFunc("trove_query_filter", func(raw, target, op string) bool {
			return matchesFilter(map[string]any{"propertyId": "value", "op": op, "value": decode(target)}, map[string]any{"value": decode(raw)})
		}, true); err != nil {
			return err
		}
		if err := c.RegisterFunc("trove_query_compare", func(a, b string) int { return compareValue(decode(a), decode(b)) }, true); err != nil {
			return err
		}
		return c.RegisterCollation("TROVE_QUERY_VALUE", func(a, b string) int { return compareValue(decode(a), decode(b)) })
	})
	if err == nil {
		_, err = conn.ExecContext(ctx, "PRAGMA temp_store=FILE")
	}
	if err != nil {
		conn.Close()
		return nil, err
	}
	return conn, nil
}

type recordSQL struct {
	args    []any
	types   map[string]string
	derived bool
	needed  map[string]bool
}

func (q *recordSQL) bind(value any) string {
	name := fmt.Sprintf("q%d", len(q.args))
	q.args = append(q.args, sql.Named(name, value))
	return ":" + name
}
func (q *recordSQL) value(property string) string {
	q.needed[property] = true
	switch q.types[property] {
	case "title":
		return "json_quote(p.title)"
	case "created_time":
		return "json_quote(p.created_at)"
	case "last_edited_time":
		return "json_quote(p.updated_at)"
	case "unique_id":
		return "json_quote(r.id)"
	case "formula", "rollup":
		q.derived = true
	}
	return "trove_query_value(COALESCE((SELECT value_json FROM property_values WHERE record_id=r.id AND property_id=" + q.bind(property) + "),'null'),p.title,r.page_id)"
}
func (q *recordSQL) filter(raw any) string {
	if raw == nil {
		return "1"
	}
	f := raw.(map[string]any)
	op := str(f["op"])
	if op == "and" || op == "or" {
		parts := []string{}
		for _, term := range f["terms"].([]any) {
			parts = append(parts, q.filter(term))
		}
		if len(parts) == 0 {
			if op == "and" {
				return "1"
			}
			return "0"
		}
		return "(" + strings.Join(parts, " "+strings.ToUpper(op)+" ") + ")"
	}
	return "trove_query_filter(" + q.value(str(f["propertyId"])) + "," + q.bind(string(marshal(f["value"]))) + "," + q.bind(op) + ")"
}
func (q *recordSQL) scope(e contracts.ServiceExecutionContext) string {
	switch scope := e.Scope.Value.(type) {
	case contracts.DomainScopeVariant1:
		return "1"
	case contracts.DomainScopeVariant3:
		ids := []string{}
		for _, id := range scope.PageIds {
			ids = append(ids, q.bind(string(id)))
		}
		if len(ids) == 0 {
			return "0"
		}
		return "p.id IN (" + strings.Join(ids, ",") + ")"
	case contracts.DomainScopeVariant2:
		project := q.bind(string(scope.ProjectId))
		return "p.id IN (SELECT owning_page_id FROM containers WHERE id=" + project + " AND kind='project' UNION SELECT page_id FROM project_pages WHERE project_id=" + project + " AND relationship IN('member','pin')) AND p.id NOT IN (SELECT page_id FROM project_pages WHERE project_id=" + project + " AND relationship='exclude')"
	}
	return "0"
}

func queryRecords(ctx context.Context, k *Kernel, e contracts.ServiceExecutionContext, request map[string]any) (any, error) {
	conn, err := recordQueryConnection(ctx, k)
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	tx, err := conn.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	collection, view := str(request["collectionId"]), str(request["viewId"])
	if err = k.authorizeEntity(ctx, tx, e, "collection", collection); err != nil {
		return nil, err
	}
	if err = requireActiveCollection(ctx, tx, collection); err != nil {
		return nil, err
	}
	identity := recordQueryCursor{Version: 2, Collection: collection, View: view, ClockMinute: time.Now().UTC().Truncate(time.Minute).Format(time.RFC3339)}
	if err = tx.QueryRowContext(ctx, "SELECT revision FROM collections WHERE id=?", collection).Scan(&identity.CollectionRevision); err != nil {
		return nil, err
	}
	query := map[string]any{"filter": nil, "sorts": []any{}}
	if view != "" {
		if err = k.authorizeEntity(ctx, tx, e, "view", view); err != nil {
			return nil, err
		}
		var actual, raw string
		if err = tx.QueryRowContext(ctx, "SELECT collection_id,query_json,revision FROM views WHERE id=?", view).Scan(&actual, &raw, &identity.ViewRevision); err != nil {
			return nil, err
		}
		if actual != collection {
			return nil, Fail("INVALID_REQUEST", "View collection mismatch")
		}
		if err = json.Unmarshal([]byte(raw), &query); err != nil {
			return nil, err
		}
	}
	if err = tx.QueryRowContext(ctx, "SELECT CAST(COALESCE(MAX(cursor),0) AS TEXT) FROM committed_events").Scan(&identity.EventCursor); err != nil {
		return nil, err
	}
	hash := sha256.Sum256(marshal(map[string]any{"actor": e.Actor, "origin": e.Origin, "scope": e.Scope, "grant": e.ScopeGrantId, "policy": e.PolicyRevision}))
	identity.Scope = hex.EncodeToString(hash[:])
	hash = sha256.Sum256(marshal(query))
	identity.Query = hex.EncodeToString(hash[:])
	cursor := identity
	if raw := str(request["cursor"]); raw != "" {
		cursor, err = k.recordQueries.open(raw)
		if err != nil {
			return nil, err
		}
		if cursor.Collection != collection || cursor.View != view || cursor.Scope != identity.Scope || cursor.Query != identity.Query {
			return nil, Fail("INVALID_REQUEST", "Cursor query or scope mismatch; restart the query")
		}
		if !sameQuerySnapshot(cursor, identity) {
			return nil, Fail("CONFLICT", "Records, view or clock changed; restart the query")
		}
	}
	plan := &recordSQL{types: map[string]string{}, needed: map[string]bool{}}
	properties, err := tx.QueryContext(ctx, "SELECT id,type FROM properties WHERE collection_id=?", collection)
	if err != nil {
		return nil, err
	}
	for properties.Next() {
		var id, kind string
		if err = properties.Scan(&id, &kind); err != nil {
			properties.Close()
			return nil, err
		}
		plan.types[id] = kind
	}
	err = properties.Err()
	properties.Close()
	if err != nil {
		return nil, err
	}
	base := "r.collection_id=" + plan.bind(collection) + " AND p.deleted_at IS NULL AND (" + plan.scope(e) + ")"
	filter := plan.filter(query["filter"])
	sorts, _ := query["sorts"].([]any)
	expressions, directions := []string{}, []string{}
	for _, raw := range sorts {
		item := raw.(map[string]any)
		expressions = append(expressions, plan.value(str(item["propertyId"])))
		direction := "ASC"
		if item["direction"] == "desc" {
			direction = "DESC"
		}
		directions = append(directions, direction)
	}
	clock, _ := time.Parse(time.RFC3339, identity.ClockMinute)
	evaluator := newDerivedEvaluator(ctx, k, tx, e, clock, "UTC")
	evaluator.workLimit, evaluator.byteLimit = queryDerivedWork, querySnapshotBytes
	limit := int(number(request["limit"]))
	if limit < 1 || limit > 200 {
		return nil, Fail("INVALID_REQUEST", "Record limit must be between 1 and 200")
	}
	var ids []string
	var next recordQueryCursor
	if plan.derived {
		var entry *recordQuerySnapshot
		if cursor.Snapshot != "" {
			entry, err = k.recordQueries.get(cursor.Snapshot, identity)
		} else {
			entry, err = buildRecordQuerySnapshot(ctx, tx, plan, base, query, identity, evaluator)
			if err == nil {
				err = k.recordQueries.put(entry)
				k.recordQueries.work.Add(int64(evaluator.work))
			}
		}
		if err != nil {
			return nil, err
		}
		if cursor.Offset > len(entry.IDs) {
			return nil, Fail("INVALID_REQUEST", "Cursor offset out of range")
		}
		end := min(cursor.Offset+limit, len(entry.IDs))
		ids = entry.IDs[cursor.Offset:end]
		evaluator = newDerivedEvaluator(ctx, k, tx, e, clock, "UTC")
		evaluator.baseCache = entry.Memo
		evaluator.workLimit, evaluator.byteLimit = queryDerivedWork, querySnapshotBytes
		if end < len(entry.IDs) {
			next = identity
			next.Snapshot, next.Offset = entry.Token, end
		}
	} else {
		ids, err = ordinaryRecordPage(ctx, tx, plan, base, filter, expressions, directions, cursor.LastID, limit+1)
		if err != nil {
			return nil, err
		}
		if len(ids) > limit {
			ids = ids[:limit]
			next = identity
			next.LastID = ids[len(ids)-1]
		}
	}
	records, _, err := hydrateRecordPage(ctx, tx, ids)
	if err != nil {
		return nil, err
	}
	for i := range records {
		if err = enrichDerivedRecord(evaluator, &records[i]); err != nil {
			return nil, err
		}
	}
	if err = k.authorize(e); err != nil {
		return nil, err
	}
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	result := map[string]any{"recordIds": append([]string{}, ids...), "records": records, "snapshotCursor": identity.EventCursor}
	if next.Version != 0 {
		sealed, err := k.recordQueries.seal(next)
		if err != nil {
			return nil, err
		}
		result["nextCursor"] = sealed
	}
	return result, nil
}

func ordinaryRecordPage(ctx context.Context, tx *sql.Tx, plan *recordSQL, base, filter string, expressions, directions []string, last string, limit int) ([]string, error) {
	order := []string{}
	for i, expression := range expressions {
		order = append(order, expression+" COLLATE TROVE_QUERY_VALUE "+directions[i])
	}
	order = append(order, "r.sort_key ASC", "r.id ASC")
	after := "1"
	if last != "" {
		fields := append(append([]string{}, expressions...), "r.sort_key", "r.id")
		values := make([]string, len(fields))
		dest := make([]any, len(fields))
		for i := range values {
			dest[i] = &values[i]
		}
		if err := tx.QueryRowContext(ctx, "SELECT "+strings.Join(fields, ",")+" FROM records r JOIN pages p ON p.id=r.page_id WHERE ("+base+") AND r.id="+plan.bind(last), plan.args...).Scan(dest...); err != nil {
			return nil, Fail("CONFLICT", "Cursor record is no longer available; restart the query")
		}
		equal, branches := []string{}, []string{}
		for i, expression := range expressions {
			cmp := "trove_query_compare(" + expression + "," + plan.bind(values[i]) + ")"
			op := ">0"
			if directions[i] == "DESC" {
				op = "<0"
			}
			branches = append(branches, "("+strings.Join(append(append([]string{}, equal...), cmp+op), " AND ")+")")
			equal = append(equal, cmp+"=0")
		}
		sortKey, id := plan.bind(values[len(expressions)]), plan.bind(last)
		branches = append(branches, "("+strings.Join(append(equal, "(r.sort_key>"+sortKey+" OR (r.sort_key="+sortKey+" AND r.id>"+id+"))"), " AND ")+")")
		after = "(" + strings.Join(branches, " OR ") + ")"
	}
	return queryRecordIDs(ctx, tx, "SELECT r.id FROM records r JOIN pages p ON p.id=r.page_id WHERE ("+base+") AND ("+filter+") AND ("+after+") ORDER BY "+strings.Join(order, ",")+" LIMIT "+plan.bind(limit), plan.args...)
}

func queryRecordIDs(ctx context.Context, tx *sql.Tx, statement string, args ...any) ([]string, error) {
	rows, err := tx.QueryContext(ctx, statement, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []string{}
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			return nil, err
		}
		result = append(result, id)
	}
	return result, rows.Err()
}

// Hydration is bounded by one page/batch and uses two queries, not N+1 reads.
func hydrateRecordPage(ctx context.Context, tx *sql.Tx, ids []string) ([]contracts.DomainRecord, map[string]string, error) {
	return hydrateRecordProjection(ctx, tx, ids, nil)
}

// A derived ordering scan needs only filter/sort cells, not every visible cell.
// A nil projection retains full response hydration; an empty one needs metadata only.
func hydrateRecordProjection(ctx context.Context, tx *sql.Tx, ids []string, needed map[string]bool) ([]contracts.DomainRecord, map[string]string, error) {
	result := []contracts.DomainRecord{}
	titles := map[string]string{}
	if len(ids) == 0 {
		return result, titles, nil
	}
	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(ids)), ",")
	args := make([]any, len(ids))
	for i, id := range ids {
		args[i] = id
	}
	rows, err := tx.QueryContext(ctx, "SELECT r.id,r.collection_id,r.page_id,r.revision,r.sort_key,c.title_property_id,p.title FROM records r JOIN collections c ON c.id=r.collection_id JOIN pages p ON p.id=r.page_id WHERE r.id IN ("+placeholders+")", args...)
	if err != nil {
		return nil, nil, err
	}
	records := map[string]*contracts.DomainRecord{}
	for rows.Next() {
		r := &contracts.DomainRecord{}
		var property contracts.DomainId
		var title string
		if err = rows.Scan(&r.Id, &r.CollectionId, &r.PageId, &r.Revision, &r.SortKey, &property, &title); err != nil {
			rows.Close()
			return nil, nil, err
		}
		r.Values = []contracts.DomainPropertyValue{{PropertyId: property, SchemaVersion: 1, Value: marshal(map[string]any{"pageId": r.PageId})}}
		records[string(r.Id)] = r
		titles[string(r.Id)] = title
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, nil, err
	}
	valueCondition := ""
	if needed != nil {
		propertyIDs := make([]string, 0, len(needed))
		for id := range needed {
			propertyIDs = append(propertyIDs, id)
		}
		sort.Strings(propertyIDs)
		if len(propertyIDs) == 0 {
			valueCondition = " AND 0"
		} else {
			valueCondition = " AND property_id IN (" + strings.TrimSuffix(strings.Repeat("?,", len(propertyIDs)), ",") + ")"
			for _, id := range propertyIDs {
				args = append(args, id)
			}
		}
	}
	rows, err = tx.QueryContext(ctx, "SELECT record_id,property_id,schema_version,value_json FROM property_values WHERE record_id IN ("+placeholders+")"+valueCondition+" ORDER BY record_id,property_id", args...)
	if err != nil {
		return nil, nil, err
	}
	for rows.Next() {
		var id, raw string
		var value contracts.DomainPropertyValue
		if err = rows.Scan(&id, &value.PropertyId, &value.SchemaVersion, &raw); err != nil {
			rows.Close()
			return nil, nil, err
		}
		value.Value = json.RawMessage(raw)
		records[id].Values = append(records[id].Values, value)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, nil, err
	}
	for _, id := range ids {
		r := records[id]
		if r == nil {
			return nil, nil, Fail("CONFLICT", "Record changed during query")
		}
		result = append(result, *r)
	}
	return result, titles, nil
}

func buildRecordQuerySnapshot(ctx context.Context, tx *sql.Tx, plan *recordSQL, base string, query map[string]any, identity recordQueryCursor, d *derivedEvaluator) (*recordQuerySnapshot, error) {
	type candidate struct {
		id, sortKey string
		values      map[string]any
	}
	candidates := []candidate{}
	lastKey, lastID := "", ""
	scanned, bytes := 0, 0
	for {
		args := append([]any{}, plan.args...)
		args = append(args, sql.Named("lastKey", lastKey), sql.Named("lastID", lastID))
		ids, err := queryRecordIDs(ctx, tx, "SELECT r.id FROM records r JOIN pages p ON p.id=r.page_id WHERE ("+base+") AND (r.sort_key>:lastKey OR (r.sort_key=:lastKey AND r.id>:lastID)) ORDER BY r.sort_key,r.id LIMIT 256", args...)
		if err != nil {
			return nil, err
		}
		if len(ids) == 0 {
			break
		}
		scanned += len(ids)
		if scanned > querySnapshotRows {
			return nil, Fail("RESOURCE_EXHAUSTED", "Derived query exceeds 100000 scanned records; narrow the view")
		}
		records, titles, err := hydrateRecordProjection(ctx, tx, ids, plan.needed)
		if err != nil {
			return nil, err
		}
		for _, r := range records {
			if err = ctx.Err(); err != nil {
				return nil, err
			}
			values := map[string]any{}
			for _, cell := range r.Values {
				property := string(cell.PropertyId)
				if !plan.needed[property] {
					continue
				}
				var value any
				_ = json.Unmarshal(cell.Value, &value)
				if object, ok := value.(map[string]any); ok && object["pageId"] == string(r.PageId) {
					value = titles[string(r.Id)]
				}
				values[property] = derivedQueryValue(value)
			}
			for property := range plan.needed {
				switch plan.types[property] {
				case "formula", "rollup", "created_time", "last_edited_time", "unique_id":
					v, err := d.value(string(r.Id), property)
					if err != nil {
						return nil, err
					}
					if v.Status == "computed" {
						values[property] = v.Value
					} else {
						values[property] = nil
					}
				}
			}
			if matchesFilter(query["filter"], values) {
				bytes += len(r.Id) + len(r.SortKey) + len(marshal(values))
				candidates = append(candidates, candidate{string(r.Id), string(r.SortKey), values})
			}
			if bytes+d.memoBytes > querySnapshotBytes {
				return nil, Fail("RESOURCE_EXHAUSTED", "Derived query exceeds its 16 MiB working-data budget; narrow the view")
			}
			lastKey, lastID = string(r.SortKey), string(r.Id)
		}
	}
	sorts, _ := query["sorts"].([]any)
	sort.SliceStable(candidates, func(i, j int) bool {
		for _, raw := range sorts {
			s := raw.(map[string]any)
			cmp := compareValue(candidates[i].values[str(s["propertyId"])], candidates[j].values[str(s["propertyId"])])
			if cmp != 0 {
				if s["direction"] == "desc" {
					return cmp > 0
				}
				return cmp < 0
			}
		}
		if candidates[i].sortKey == candidates[j].sortKey {
			return candidates[i].id < candidates[j].id
		}
		return candidates[i].sortKey < candidates[j].sortKey
	})
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	entry := &recordQuerySnapshot{Token: NewID(), Identity: identity, IDs: []string{}, Memo: d.cache, Bytes: d.memoBytes}
	for _, candidate := range candidates {
		entry.IDs = append(entry.IDs, candidate.id)
		entry.Bytes += len(candidate.id)
	}
	return entry, nil
}
