package workspace

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"sort"
	"time"

	"trove.local/phase0/contracts"
)

type scheduleInput struct {
	CollectionID   string         `json:"collectionId"`
	DatePropertyID string         `json:"datePropertyId"`
	RecordID       string         `json:"recordId"`
	NewStart       string         `json:"newStart"`
	Policy         schedulePolicy `json:"policy"`
}
type scheduleChange struct {
	RecordID            string       `json:"recordId"`
	PageID              string       `json:"pageId"`
	Title               string       `json:"title"`
	Before              scheduleDate `json:"before"`
	After               scheduleDate `json:"after"`
	DependencyRecordIDs []string     `json:"dependencyRecordIds"`
	Reason              string       `json:"reason"`
	Changed             bool         `json:"changed"`
	Duration            string       `json:"duration"`
	DSTEffects          []string     `json:"dstEffects"`
}
type schedulingPlan struct {
	Version           int                                 `json:"version"`
	ID                string                              `json:"id"`
	Hash              string                              `json:"hash"`
	Input             scheduleInput                       `json:"input"`
	RequiredRevisions []contracts.ServiceExpectedRevision `json:"requiredRevisions"`
	Records           []scheduleChange                    `json:"records"`
}

func init() {
	RegisterInitializer(func(ctx context.Context, k *Kernel) error {
		return k.write(ctx, func(tx *sql.Tx) error {
			_, err := tx.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS scheduling_plans(id TEXT PRIMARY KEY,version INTEGER NOT NULL CHECK(version=1),collection_id TEXT NOT NULL REFERENCES collections(id),plan_json TEXT NOT NULL CHECK(json_valid(plan_json)),applied_at TEXT NOT NULL);`)
			return err
		})
	})
	RegisterQuery("scheduling.preview", previewScheduling)
	RegisterCommand("scheduling.apply", applyScheduling)
}
func previewScheduling(ctx context.Context, k *Kernel, e contracts.ServiceExecutionContext, r map[string]any) (any, error) {
	var input scheduleInput
	if err := json.Unmarshal(marshal(r["input"]), &input); err != nil {
		return nil, err
	}
	tx, err := k.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	return buildSchedulingPlan(ctx, k, tx, e, input, NewID())
}
func buildSchedulingPlan(ctx context.Context, k *Kernel, q querier, e contracts.ServiceExecutionContext, input scheduleInput, id string) (schedulingPlan, error) {
	out := schedulingPlan{Version: 1, ID: id, Input: input, RequiredRevisions: []contracts.ServiceExpectedRevision{}, Records: []scheduleChange{}}
	calendar, err := newScheduleCalendar(input.Policy)
	if err != nil {
		return out, err
	}
	if err = k.authorizeEntity(ctx, q, e, "collection", input.CollectionID); err != nil {
		return out, err
	}
	property, err := propertyDefinition(ctx, q, input.DatePropertyID)
	if err != nil {
		return out, err
	}
	if string(property.CollectionId) != input.CollectionID || property.Type != "date" {
		return out, Fail("INVALID_REQUEST", "Choose a date property from this collection")
	}
	type node struct {
		page, revision, pageRevision, title string
		deleted                             sql.NullString
		dependencies, children              []string
	}
	nodes := map[string]*node{}
	rows, err := q.QueryContext(ctx, "SELECT r.id,r.page_id,r.revision,p.revision,p.title,p.deleted_at FROM records r JOIN pages p ON p.id=r.page_id WHERE r.collection_id=? ORDER BY r.id LIMIT 10001", input.CollectionID)
	if err != nil {
		return out, err
	}
	for rows.Next() {
		var rid string
		n := &node{dependencies: []string{}, children: []string{}}
		if err = rows.Scan(&rid, &n.page, &n.revision, &n.pageRevision, &n.title, &n.deleted); err != nil {
			rows.Close()
			return out, err
		}
		nodes[rid] = n
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return out, err
	}
	if len(nodes) > 10000 {
		return out, Fail("RESOURCE_EXHAUSTED", "Scheduling supports collections with at most 10000 records")
	}
	if nodes[input.RecordID] == nil {
		return out, Fail("NOT_FOUND", "Selected record is unavailable in this collection")
	}
	rows, err = q.QueryContext(ctx, "SELECT d.record_id,d.depends_on_record_id FROM record_dependencies d JOIN records r ON r.id=d.record_id WHERE r.collection_id=? ORDER BY d.record_id,d.depends_on_record_id", input.CollectionID)
	if err != nil {
		return out, err
	}
	count := 0
	for rows.Next() {
		var a, b string
		if err = rows.Scan(&a, &b); err != nil {
			rows.Close()
			return out, err
		}
		count++
		if count > 100000 {
			rows.Close()
			return out, Fail("RESOURCE_EXHAUSTED", "Scheduling graph exceeds 100000 edges")
		}
		if nodes[a] != nil {
			nodes[a].dependencies = append(nodes[a].dependencies, b)
		}
		if nodes[b] != nil {
			nodes[b].children = append(nodes[b].children, a)
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return out, err
	}
	affected := map[string]bool{}
	pending := []string{input.RecordID}
	for len(pending) > 0 {
		rid := pending[0]
		pending = pending[1:]
		if affected[rid] {
			continue
		}
		affected[rid] = true
		pending = append(pending, nodes[rid].children...)
	}
	needed := map[string]bool{}
	pending = []string{}
	for rid := range affected {
		pending = append(pending, rid)
	}
	for len(pending) > 0 {
		rid := pending[0]
		pending = pending[1:]
		if needed[rid] {
			continue
		}
		n := nodes[rid]
		if n == nil || n.deleted.Valid {
			return out, Fail("CONFLICT", "The affected graph contains a missing, foreign or trashed dependency")
		}
		needed[rid] = true
		pending = append(pending, n.dependencies...)
	}
	if len(needed) > 450 {
		return out, Fail("RESOURCE_EXHAUSTED", "Narrow the schedule to at most 450 affected and prerequisite records")
	}
	keys := []string{}
	for rid := range needed {
		keys = append(keys, rid)
	}
	sort.Strings(keys)
	states := map[string]int{}
	ordered := []string{}
	var visit func(string) error
	visit = func(rid string) error {
		if states[rid] == 1 {
			return Fail("CONFLICT", "The affected dependency graph contains a cycle")
		}
		if states[rid] == 2 {
			return nil
		}
		states[rid] = 1
		for _, d := range nodes[rid].dependencies {
			if err := visit(d); err != nil {
				return err
			}
		}
		states[rid] = 2
		ordered = append(ordered, rid)
		return nil
	}
	for _, rid := range keys {
		if err = visit(rid); err != nil {
			return out, err
		}
	}
	addGuard := func(kind, id string) error {
		if err := k.authorizeEntity(ctx, q, e, kind, id); err != nil {
			return err
		}
		var rev string
		if err := q.QueryRowContext(ctx, "SELECT revision FROM "+entityTables[kind]+" WHERE id=?", id).Scan(&rev); err != nil {
			return err
		}
		out.RequiredRevisions = append(out.RequiredRevisions, required(kind, id, rev))
		return nil
	}
	if err = addGuard("collection", input.CollectionID); err != nil {
		return out, err
	}
	if err = addGuard("property", input.DatePropertyID); err != nil {
		return out, err
	}
	dates := map[string]scheduleDate{}
	durations := map[string]int64{}
	var hasTime bool
	for i, rid := range keys {
		n := nodes[rid]
		if err = addGuard("record", rid); err != nil {
			return out, err
		}
		if err = addGuard("page", n.page); err != nil {
			return out, err
		}
		var raw string
		err = q.QueryRowContext(ctx, "SELECT value_json FROM property_values WHERE record_id=? AND property_id=? AND property_type='date'", rid, input.DatePropertyID).Scan(&raw)
		if errors.Is(err, sql.ErrNoRows) {
			return out, Fail("CONFLICT", "Every affected record and prerequisite needs a date in the selected property")
		}
		if err != nil {
			return out, err
		}
		var d scheduleDate
		if err = json.Unmarshal([]byte(raw), &d); err != nil {
			return out, err
		}
		if i == 0 {
			hasTime = d.HasTime
		} else if d.HasTime != hasTime {
			return out, Fail("CONFLICT", "Date-only and timed values cannot share a scheduling graph")
		}
		duration, err := calendar.duration(d)
		if err != nil {
			return out, err
		}
		dates[rid] = d
		durations[rid] = duration
	}
	requested := dates[input.RecordID]
	requested.Start = input.NewStart
	requested.End = nil
	requestedStart, _, err := calendar.parse(requested)
	if err != nil {
		return out, err
	}
	if !calendar.eligible(requestedStart) {
		return out, Fail("INVALID_REQUEST", "Choose a requested start on an eligible weekday")
	}
	after := map[string]scheduleDate{}
	changes := map[string]scheduleChange{}
	changeCount := 0
	for _, rid := range ordered {
		d := dates[rid]
		next := d
		reason := "prerequisite"
		start, _, err := calendar.parse(d)
		if err != nil {
			return out, err
		}
		constraint := time.Time{}
		for _, dep := range nodes[rid].dependencies {
			finish, err := calendar.finishConstraint(after[dep])
			if err != nil {
				return out, err
			}
			if finish.After(constraint) {
				constraint = finish
			}
		}
		if rid == input.RecordID {
			if requestedStart.Before(constraint) {
				return out, Fail("CONFLICT", "Requested start violates a predecessor finish-to-start constraint")
			}
			next, err = calendar.shifted(d, requestedStart, durations[rid])
			reason = "requested"
		} else if affected[rid] {
			reason = "slack_preserved"
			if start.Before(constraint) {
				next, err = calendar.shifted(d, constraint, durations[rid])
				reason = "dependency"
			}
		}
		if err != nil {
			return out, err
		}
		after[rid] = next
		changed := string(marshal(d)) != string(marshal(next))
		if changed {
			changeCount++
		}
		changes[rid] = scheduleChange{RecordID: rid, PageID: nodes[rid].page, Title: nodes[rid].title, Before: d, After: next, DependencyRecordIDs: nodes[rid].dependencies, Reason: reason, Changed: changed, Duration: scheduleDurationLabel(d, durations[rid]), DSTEffects: calendar.dstNotes(d, next)}
	}
	if changeCount > 128 {
		return out, Fail("RESOURCE_EXHAUSTED", "One reviewed schedule can change at most 128 records")
	}
	for _, rid := range keys {
		out.Records = append(out.Records, changes[rid])
	}
	out.Hash = snapshotDigest(string(marshal(out)))
	return out, nil
}
func applyScheduling(m *Mutation, c map[string]any) error {
	if len(m.Request.Commands) != 1 {
		return Fail("INVALID_REQUEST", "A reviewed schedule requires an isolated atomic batch")
	}
	if m.Execution.Actor.Kind != "user" || m.Execution.Origin != "user" {
		return Fail("SCOPE_DENIED", "The user must approve the complete scheduling preview")
	}
	var plan schedulingPlan
	if err := json.Unmarshal(marshal(c["plan"]), &plan); err != nil {
		return err
	}
	if plan.Version != 1 {
		return Fail("UNSUPPORTED_VERSION", "Unsupported scheduling plan")
	}
	current, err := buildSchedulingPlan(m.Context, m.Kernel, m.Tx, m.Execution, plan.Input, plan.ID)
	if err != nil {
		return err
	}
	if current.Hash != plan.Hash || string(marshal(current)) != string(marshal(plan)) {
		return Fail("CONFLICT", "Dates, schema or dependency graph changed after preview; create a new plan")
	}
	for _, g := range current.RequiredRevisions {
		if err = m.Require(g.Target.EntityType, string(g.Target.EntityId)); err != nil {
			return err
		}
	}
	actions := []map[string]any{}
	for _, change := range current.Records {
		if change.Changed {
			actions = append(actions, map[string]any{"kind": "record.setValue", "recordId": change.RecordID, "propertyId": plan.Input.DatePropertyID, "value": map[string]any{"type": "date", "schemaVersion": 1, "value": change.After}})
		}
	}
	if len(actions) == 0 {
		return Fail("INVALID_REQUEST", "This schedule changes no dates")
	}
	if err = applyJournal(m, plan.ID, "Reschedule dependencies", actions); err != nil {
		return err
	}
	_, err = m.Exec("INSERT INTO scheduling_plans VALUES(?,1,?,?,?)", plan.ID, plan.Input.CollectionID, string(marshal(plan)), now())
	return err
}
