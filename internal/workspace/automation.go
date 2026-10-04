package workspace

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
	"trove.local/phase0/contracts"
)

type AutomationTrigger struct {
	Type         string `json:"type"`
	CollectionID string `json:"collectionId,omitempty"`
	EntityType   string `json:"entityType,omitempty"`
	Timezone     string `json:"timezone,omitempty"`
	Frequency    string `json:"frequency,omitempty"`
	Hour         int    `json:"hour,omitempty"`
	Minute       int    `json:"minute,omitempty"`
	Weekday      int    `json:"weekday,omitempty"`
	MissedPolicy string `json:"missedPolicy,omitempty"`
	CatchUpLimit int    `json:"catchUpLimit,omitempty"`
}
type AutomationCondition struct {
	RecordID   string `json:"recordId"`
	PropertyID string `json:"propertyId"`
	Op         string `json:"op"`
	Value      any    `json:"value"`
}
type AutomationRule struct {
	ID             string               `json:"id"`
	Name           string               `json:"name"`
	Enabled        bool                 `json:"enabled"`
	Paused         bool                 `json:"paused"`
	AllowedOrigins []string             `json:"allowedOrigins"`
	Trigger        AutomationTrigger    `json:"trigger"`
	Condition      *AutomationCondition `json:"condition,omitempty"`
	Actions        []map[string]any     `json:"actions"`
}
type automationStored struct {
	Rule     AutomationRule
	Page     string
	Version  int
	Cursor   int64
	Next     string
	Failures int
}

func init() {
	RegisterInitializer(func(ctx context.Context, k *Kernel) error {
		return k.write(ctx, func(tx *sql.Tx) error {
			_, e := tx.ExecContext(ctx, `
 CREATE TABLE IF NOT EXISTS automation_rules(id TEXT PRIMARY KEY,page_id TEXT NOT NULL REFERENCES pages(id),version INTEGER NOT NULL,rule_json TEXT NOT NULL,event_cursor INTEGER NOT NULL,next_at TEXT NOT NULL DEFAULT '',failures INTEGER NOT NULL DEFAULT 0);
 CREATE TABLE IF NOT EXISTS automation_runs(id TEXT PRIMARY KEY,rule_id TEXT NOT NULL REFERENCES automation_rules(id),rule_version INTEGER NOT NULL,occurrence_key TEXT NOT NULL,status TEXT NOT NULL,diagnostic TEXT NOT NULL DEFAULT '',created_at TEXT NOT NULL,commands_json TEXT NOT NULL DEFAULT '[]',UNIQUE(rule_id,rule_version,occurrence_key));
 `)
			return e
		})
	})
	for _, kind := range []string{"automation.save", "automation.state", "automation.run"} {
		RegisterCommand(kind, automationCommand)
	}
	RegisterQuery("automationPreview", automationPreview)
	RegisterQuery("automation.execute", executePreviewedAutomation)
	RegisterQuery("automationHistory", automationHistory)
}
func validateAutomationRule(r *AutomationRule) error {
	if contracts.Validate("domain", "Id", marshal(r.ID)) != nil || strings.TrimSpace(r.Name) == "" || len(r.Name) > 200 {
		return Fail("INVALID_REQUEST", "Rule needs a stable ID and a name of 1–200 characters")
	}
	if len(r.AllowedOrigins) == 0 {
		r.AllowedOrigins = []string{"user"}
	}
	for _, o := range r.AllowedOrigins {
		if o != "user" && o != "ai" {
			return Fail("INVALID_REQUEST", "Eligible rule origins are user and explicitly opted-in AI; import, restore and automation cannot cascade")
		}
	}
	if r.Trigger.Type != "event" && r.Trigger.Type != "schedule" {
		return Fail("INVALID_REQUEST", "Trigger must be event or schedule")
	}
	if r.Trigger.Type == "schedule" {
		t := &r.Trigger
		if _, e := time.LoadLocation(t.Timezone); e != nil {
			return Fail("INVALID_REQUEST", "A valid IANA schedule timezone is required")
		}
		if t.Frequency != "daily" && t.Frequency != "weekly" || t.Hour < 0 || t.Hour > 23 || t.Minute < 0 || t.Minute > 59 || t.Weekday < 0 || t.Weekday > 6 {
			return Fail("INVALID_REQUEST", "Invalid daily/weekly wall-clock recurrence")
		}
		if t.MissedPolicy == "" {
			t.MissedPolicy = "skip"
		}
		if t.MissedPolicy != "skip" && t.MissedPolicy != "catch_up" {
			return Fail("INVALID_REQUEST", "Missed-run policy must be skip or catch_up")
		}
		if t.CatchUpLimit == 0 {
			t.CatchUpLimit = 1
		}
		if t.CatchUpLimit < 1 || t.CatchUpLimit > 20 {
			return Fail("INVALID_REQUEST", "Catch-up limit must be 1–20")
		}
	}
	if len(r.Actions) < 1 || len(r.Actions) > 32 {
		return Fail("INVALID_REQUEST", "Rules require 1–32 actions")
	}
	if len(marshal(r)) > 256*1024 {
		return Fail("RESOURCE_EXHAUSTED", "Rule exceeds 256 KiB")
	}
	allowed := map[string]bool{"page.create": true, "page.update": true, "record.create": true, "record.setValue": true, "record.clearValue": true, "task.setStatus": true}
	for _, a := range r.Actions {
		if !allowed[str(a["kind"])] {
			return Fail("INVALID_REQUEST", "Automation action is not an allowed local command")
		}
	}
	if c := r.Condition; c != nil {
		if c.RecordID == "" || c.PropertyID == "" || c.Op != "eq" && c.Op != "ne" && c.Op != "is_empty" {
			return Fail("INVALID_REQUEST", "Condition requires record/property IDs and eq, ne, or is_empty")
		}
	}
	return nil
}
func loadAutomation(ctx context.Context, q querier, id string) (automationStored, error) {
	var s automationStored
	var raw string
	e := q.QueryRowContext(ctx, "SELECT page_id,version,rule_json,event_cursor,next_at,failures FROM automation_rules WHERE id=?", id).Scan(&s.Page, &s.Version, &raw, &s.Cursor, &s.Next, &s.Failures)
	if e == nil {
		e = json.Unmarshal([]byte(raw), &s.Rule)
	}
	return s, e
}
func automationCommand(m *Mutation, c map[string]any) error {
	if err := requireOwnerScope(m.Kernel, m.Execution); err != nil {
		return err
	}
	if m.Execution.Origin != "user" {
		return Fail("SCOPE_DENIED", "Only the local owner may configure and run saved local rules")
	}
	page := str(c["pageId"])
	kind := str(c["kind"])
	if kind == "automation.save" {
		if err := m.TouchPage(page); err != nil {
			return err
		}
		var r AutomationRule
		if err := json.Unmarshal(marshal(c["rule"]), &r); err != nil {
			return Fail("INVALID_REQUEST", err.Error())
		}
		if err := validateAutomationRule(&r); err != nil {
			return err
		}
		old, err := loadAutomation(m.Context, m.Tx, r.ID)
		if err == nil && old.Page != page {
			return Fail("INVALID_REQUEST", "A rule cannot change its owning page")
		}
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		var cursor int64
		if err = m.Tx.QueryRowContext(m.Context, "SELECT COALESCE(MAX(cursor),0) FROM committed_events").Scan(&cursor); err != nil {
			return err
		}
		next := ""
		if r.Trigger.Type == "schedule" {
			n, e := NextAutomationOccurrence(r.Trigger, time.Now())
			if e != nil {
				return e
			}
			next = n.Format(time.RFC3339Nano)
		}
		_, err = m.Exec("INSERT INTO automation_rules(id,page_id,version,rule_json,event_cursor,next_at) VALUES(?,?,1,?,?,?) ON CONFLICT(id) DO UPDATE SET version=version+1,rule_json=excluded.rule_json,event_cursor=excluded.event_cursor,next_at=excluded.next_at,failures=0", r.ID, page, string(marshal(r)), cursor, next)
		return err
	}
	s, err := loadAutomation(m.Context, m.Tx, str(c["ruleId"]))
	if err != nil {
		return err
	}
	if s.Page != page {
		return Fail("SCOPE_DENIED", "Rule does not belong to the requested page")
	}
	if err = m.AuthorizePage(page); err != nil {
		return err
	}
	if kind == "automation.state" {
		if err = m.TouchPage(page); err != nil {
			return err
		}
		s.Rule.Enabled = c["enabled"] == true
		s.Rule.Paused = c["paused"] == true
		_, err = m.Exec("UPDATE automation_rules SET rule_json=?,failures=0 WHERE id=?", string(marshal(s.Rule)), s.Rule.ID)
		return err
	}
	key := str(c["occurrenceKey"])
	at, err := time.Parse(time.RFC3339Nano, str(c["at"]))
	if err != nil || len(key) > 300 || key == "" {
		return Fail("INVALID_REQUEST", "Run requires an occurrence key and valid instant")
	}
	var status string
	err = m.Tx.QueryRowContext(m.Context, "SELECT status FROM automation_runs WHERE rule_id=? AND rule_version=? AND occurrence_key=?", s.Rule.ID, s.Version, key).Scan(&status)
	if err == nil && status != "failed" {
		return nil
	}
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if !s.Rule.Enabled || s.Rule.Paused {
		return Fail("INVALID_REQUEST", "Rule is disabled or paused")
	}
	commands, eligible, why, err := prepareAutomation(m.Context, m.Kernel, m.Tx, m.Execution, s, str(c["eventId"]), key, at)
	if err != nil {
		return err
	}
	if !eligible {
		return Fail("INVALID_REQUEST", why)
	}
	if err = m.TouchPage(page); err != nil {
		return err
	}
	for _, action := range commands {
		handler := commandHandlers[str(action["kind"])]
		if handler == nil {
			return Fail("UNSUPPORTED_SCHEMA", "Unknown local action")
		}
		if err = handler(m, action); err != nil {
			return err
		}
	}
	_, err = m.Exec("INSERT INTO automation_runs(id,rule_id,rule_version,occurrence_key,status,created_at,commands_json) VALUES(?,?,?,?,'succeeded',?,?) ON CONFLICT(rule_id,rule_version,occurrence_key) DO UPDATE SET status='succeeded',diagnostic='',created_at=excluded.created_at,commands_json=excluded.commands_json", NewID(), s.Rule.ID, s.Version, key, now(), string(marshal(commands)))
	if err != nil {
		return err
	}
	_, err = m.Exec("UPDATE automation_rules SET failures=0 WHERE id=?", s.Rule.ID)
	return err
}

// NextAutomationOccurrence uses local calendar days, never a 24-hour duration.
// Missing wall times at a DST jump are skipped; ambiguous times use the first
// UTC occurrence, so the repeated hour cannot execute twice.
func NextAutomationOccurrence(t AutomationTrigger, after time.Time) (time.Time, error) {
	loc, err := time.LoadLocation(t.Timezone)
	if err != nil {
		return time.Time{}, err
	}
	local := after.In(loc)
	for day := 0; day < 370; day++ {
		date := time.Date(local.Year(), local.Month(), local.Day()+day, 12, 0, 0, 0, loc)
		if t.Frequency == "weekly" && int(date.Weekday()) != t.Weekday {
			continue
		}
		candidate := time.Date(date.Year(), date.Month(), date.Day(), t.Hour, t.Minute, 0, 0, loc)
		if candidate.Day() != date.Day() || candidate.Hour() != t.Hour || candidate.Minute() != t.Minute {
			continue
		}
		// A timezone transition can repeat thirty minutes (Lord Howe), one hour,
		// or more. Derive the repeated interval from the actual offset change.
		start, _ := candidate.ZoneBounds()
		if !start.IsZero() {
			_, offset := candidate.Zone()
			_, previousOffset := start.Add(-time.Second).Zone()
			if previousOffset > offset {
				earlier := candidate.Add(time.Duration(offset-previousOffset) * time.Second)
				if earlier.In(loc).Format("2006-01-02 15:04") == candidate.In(loc).Format("2006-01-02 15:04") {
					candidate = earlier
				}
			}
		}
		if candidate.After(after) {
			return candidate.UTC(), nil
		}
	}
	return time.Time{}, fmt.Errorf("no schedule occurrence within one year")
}
func deterministicAutomationID(rule, key, name string) string {
	sum := sha256.Sum256([]byte(rule + "\x00" + key + "\x00" + name))
	b := sum[:16]
	b[6] = (b[6] & 15) | 0x70
	b[8] = (b[8] & 63) | 0x80
	h := hex.EncodeToString(b)
	return h[:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:]
}
func expandAutomationValue(v any, rule, key string, at time.Time) any {
	switch x := v.(type) {
	case string:
		if strings.HasPrefix(x, "$id:") {
			return deterministicAutomationID(rule, key, x[4:])
		}
		return strings.ReplaceAll(x, "{{occurrence}}", at.UTC().Format(time.RFC3339))
	case []any:
		a := []any{}
		for _, v := range x {
			a = append(a, expandAutomationValue(v, rule, key, at))
		}
		return a
	case map[string]any:
		a := map[string]any{}
		for k, v := range x {
			a[k] = expandAutomationValue(v, rule, key, at)
		}
		return a
	}
	return v
}
func prepareAutomation(ctx context.Context, k *Kernel, tx *sql.Tx, e contracts.ServiceExecutionContext, s automationStored, eventID, key string, at time.Time) ([]map[string]any, bool, string, error) {
	r := s.Rule
	if err := k.authorizePage(ctx, tx, e, s.Page); err != nil {
		return nil, false, "", err
	}
	if r.Trigger.Type == "event" {
		var raw string
		var cursor int64
		if err := tx.QueryRowContext(ctx, "SELECT cursor,payload_json FROM committed_events WHERE event_id=?", eventID).Scan(&cursor, &raw); err != nil {
			return nil, false, "Trigger event is unavailable", nil
		}
		var event contracts.ServiceCommittedChangeEvent
		json.Unmarshal([]byte(raw), &event)
		allowed := false
		for _, o := range r.AllowedOrigins {
			if o == event.Context.Origin {
				allowed = true
			}
		}
		if !allowed {
			return nil, false, "Event origin is excluded", nil
		}
		if key != "event:"+eventID {
			return nil, false, "Event occurrence key mismatch", nil
		}
		if cursor <= s.Cursor {
			var prior string
			_ = tx.QueryRowContext(ctx, "SELECT status FROM automation_runs WHERE rule_id=? AND rule_version=? AND occurrence_key=?", r.ID, s.Version, key).Scan(&prior)
			if prior != "failed" {
				return nil, false, "Event was already inspected or predates this rule", nil
			}
		}
		matched := r.Trigger.CollectionID == "" && r.Trigger.EntityType == ""
		for _, change := range event.Revisions {
			if r.Trigger.CollectionID != "" && change.Target.EntityType == "collection" && string(change.Target.EntityId) == r.Trigger.CollectionID {
				matched = true
			}
			if r.Trigger.CollectionID == "" && r.Trigger.EntityType == change.Target.EntityType {
				matched = true
			}
		}
		if !matched {
			return nil, false, "Event does not match the trigger", nil
		}
	} else {
		if key != "schedule:"+at.UTC().Format(time.RFC3339Nano) {
			return nil, false, "Schedule occurrence key mismatch", nil
		}
		loc, _ := time.LoadLocation(r.Trigger.Timezone)
		local := at.In(loc)
		if local.Hour() != r.Trigger.Hour || local.Minute() != r.Trigger.Minute || local.Second() != 0 || r.Trigger.Frequency == "weekly" && int(local.Weekday()) != r.Trigger.Weekday {
			return nil, false, "Instant does not match the configured recurrence", nil
		}
		first, err := NextAutomationOccurrence(r.Trigger, at.Add(-time.Nanosecond))
		if err != nil || !first.Equal(at) {
			return nil, false, "Instant is not the canonical recurrence occurrence", nil
		}
	}
	if c := r.Condition; c != nil {
		d := newDerivedEvaluator(ctx, k, tx, e, at, "UTC")
		v, err := d.value(c.RecordID, c.PropertyID)
		if err != nil {
			return nil, false, "", err
		}
		if v.Status != "computed" {
			return nil, false, "Condition has an unavailable derived value", nil
		}
		row := map[string]any{c.PropertyID: v.Value}
		if !matchesFilter(map[string]any{"propertyId": c.PropertyID, "op": c.Op, "value": c.Value}, row) {
			return nil, false, "Condition did not match", nil
		}
	}
	out := []map[string]any{}
	for _, action := range r.Actions {
		a := expandAutomationValue(action, r.ID+fmt.Sprint(s.Version), key, at).(map[string]any)
		if err := contracts.Validate("service", "Command", marshal(a)); err != nil {
			return nil, false, "", Fail("INVALID_REQUEST", "Invalid expanded action: "+err.Error())
		}
		out = append(out, a)
	}
	return out, true, "", nil
}
func automationGuards(ctx context.Context, tx *sql.Tx, page string, actions []map[string]any) ([]contracts.ServiceExpectedRevision, error) {
	refs := map[string][2]string{}
	add := func(kind, id string) {
		if id != "" {
			refs[kind+":"+id] = [2]string{kind, id}
		}
	}
	add("page", page)
	var addPage func(string) error
	addPage = func(id string) error {
		add("page", id)
		var record, col string
		err := tx.QueryRowContext(ctx, "SELECT id,collection_id FROM records WHERE page_id=?", id).Scan(&record, &col)
		if err == nil {
			add("record", record)
			add("collection", col)
		} else if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		return nil
	}
	newRecords := map[string][2]string{}
	for _, a := range actions {
		if a["kind"] == "record.create" {
			newRecords[str(a["recordId"])] = [2]string{str(a["pageId"]), str(a["collectionId"])}
		}
	}
	for _, a := range actions {
		switch str(a["kind"]) {
		case "page.create":
			add("page", str(a["pageId"]))
			if err := addPage(str(a["parentPageId"])); err != nil {
				return nil, err
			}
		case "page.update":
			if err := addPage(str(a["pageId"])); err != nil {
				return nil, err
			}
		case "record.create":
			add("record", str(a["recordId"]))
			add("page", str(a["pageId"]))
			add("collection", str(a["collectionId"]))
		case "record.setValue", "record.clearValue", "task.setStatus":
			record := str(a["recordId"])
			add("record", record)
			var p, col string
			if fresh, ok := newRecords[record]; ok {
				p, col = fresh[0], fresh[1]
			} else {
				if err := tx.QueryRowContext(ctx, "SELECT page_id,collection_id FROM records WHERE id=?", record).Scan(&p, &col); err != nil {
					return nil, err
				}
			}
			add("page", p)
			add("collection", col)
			property := str(a["propertyId"])
			if property == "" {
				if err := tx.QueryRowContext(ctx, "SELECT status_property_id FROM task_facets WHERE record_id=?", record).Scan(&property); err != nil {
					return nil, err
				}
			}
			add("property", property)
			pd, pdErr := propertyDefinition(ctx, tx, property)
			if pdErr == nil && pd.Type == "relation" {
				var cfg map[string]any
				json.Unmarshal(pd.Config, &cfg)
				back := str(cfg["bidirectionalPropertyId"])
				var previous string
				_ = tx.QueryRowContext(ctx, "SELECT value_json FROM property_values WHERE record_id=? AND property_id=?", record, property).Scan(&previous)
				var old []string
				json.Unmarshal([]byte(previous), &old)
				if value, ok := a["value"].(map[string]any); ok {
					if fresh, ok := value["value"].([]any); ok {
						for _, id := range fresh {
							old = append(old, str(id))
						}
					}
				}
				for _, target := range old {
					add("record", target)
					if back != "" {
						add("property", back)
						var page, collection string
						if fresh, ok := newRecords[target]; ok {
							page, collection = fresh[0], fresh[1]
						} else if err := tx.QueryRowContext(ctx, "SELECT page_id,collection_id FROM records WHERE id=?", target).Scan(&page, &collection); err != nil {
							return nil, err
						}
						add("page", page)
						add("collection", collection)
					}
				}
			}
			if v, ok := a["value"].(map[string]any); ok && v["type"] == "relation" {
				if ids, ok := v["value"].([]any); ok {
					for _, id := range ids {
						add("record", str(id))
					}
				}
			}
		}
	}
	keys := []string{}
	for key := range refs {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	guards := []contracts.ServiceExpectedRevision{}
	for _, key := range keys {
		r := refs[key]
		rev := "0"
		err := tx.QueryRowContext(ctx, "SELECT revision FROM "+entityTables[r[0]]+" WHERE id=?", r[1]).Scan(&rev)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return nil, err
		}
		guards = append(guards, required(r[0], r[1], rev))
	}
	return guards, nil
}
func automationPreview(ctx context.Context, k *Kernel, e contracts.ServiceExecutionContext, req map[string]any) (any, error) {
	tx, err := k.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	s, err := loadAutomation(ctx, tx, str(req["ruleId"]))
	if err != nil {
		return nil, err
	}
	if s.Page != str(req["pageId"]) {
		return nil, Fail("SCOPE_DENIED", "Rule page mismatch")
	}
	at := time.Now().UTC()
	if raw := str(req["at"]); raw != "" {
		at, err = time.Parse(time.RFC3339Nano, raw)
		if err != nil {
			return nil, err
		}
	} else if s.Rule.Trigger.Type == "schedule" {
		at, err = NextAutomationOccurrence(s.Rule.Trigger, at)
		if err != nil {
			return nil, err
		}
	}
	key := "event:" + str(req["eventId"])
	if s.Rule.Trigger.Type == "schedule" {
		key = "schedule:" + at.UTC().Format(time.RFC3339Nano)
	}
	actions, eligible, why, err := prepareAutomation(ctx, k, tx, e, s, str(req["eventId"]), key, at)
	if err != nil {
		return nil, err
	}
	guards, err := automationGuards(ctx, tx, s.Page, actions)
	if err != nil {
		return nil, err
	}
	for _, g := range guards {
		if g.Revision != "0" {
			if err = k.authorizeEntity(ctx, tx, e, g.Target.EntityType, string(g.Target.EntityId)); err != nil {
				return nil, err
			}
		}
	}
	return map[string]any{"rule": s.Rule, "version": s.Version, "eligible": eligible, "diagnostic": why, "commands": actions, "expectedRevisions": guards, "occurrenceKey": key, "at": at.UTC().Format(time.RFC3339Nano)}, nil
}
func automationHistory(ctx context.Context, k *Kernel, e contracts.ServiceExecutionContext, req map[string]any) (any, error) {
	tx, err := k.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	page := str(req["pageId"])
	if err = k.authorizePage(ctx, tx, e, page); err != nil {
		return nil, err
	}
	rows, err := tx.QueryContext(ctx, "SELECT id,version,rule_json,next_at,failures FROM automation_rules WHERE page_id=? ORDER BY id", page)
	if err != nil {
		return nil, err
	}
	rules := []any{}
	for rows.Next() {
		var id, raw, next string
		var version, failures int
		if err = rows.Scan(&id, &version, &raw, &next, &failures); err != nil {
			rows.Close()
			return nil, err
		}
		var r AutomationRule
		json.Unmarshal([]byte(raw), &r)
		rules = append(rules, map[string]any{"rule": r, "version": version, "nextAt": next, "failures": failures})
	}
	rows.Close()
	runs := []any{}
	rows, err = tx.QueryContext(ctx, "SELECT r.id,r.rule_id,r.rule_version,r.occurrence_key,r.status,r.diagnostic,r.created_at,r.commands_json FROM automation_runs r JOIN automation_rules a ON a.id=r.rule_id WHERE a.page_id=? ORDER BY r.created_at DESC,r.id DESC LIMIT 200", page)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id, rule, key, status, diagnostic, at, commands string
		var version int
		if err = rows.Scan(&id, &rule, &version, &key, &status, &diagnostic, &at, &commands); err != nil {
			return nil, err
		}
		var cmds any
		json.Unmarshal([]byte(commands), &cmds)
		runs = append(runs, map[string]any{"id": id, "ruleId": rule, "ruleVersion": version, "occurrenceKey": key, "status": status, "diagnostic": diagnostic, "at": at, "commands": cmds})
	}
	return map[string]any{"rules": rules, "runs": runs, "schedulePolicy": "Schedules run only while Trove is running. Missed occurrences skip and log by default."}, rows.Err()
}
