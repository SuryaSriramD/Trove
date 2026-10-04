package workspace

import (
	"context"
	"encoding/json"
	"testing"
	"time"
	"trove.local/phase0/contracts"
)

func saveRule(t *testing.T, k *Kernel, e contracts.ServiceExecutionContext, page string, rule AutomationRule) {
	t.Helper()
	testOK(t, dbBatch(t, k, e, []any{map[string]any{"kind": "automation.save", "pageId": page, "rule": rule}}, "page", page))
}
func scheduleRule(page string) AutomationRule {
	return AutomationRule{ID: NewID(), Name: "Daily log", Enabled: true, AllowedOrigins: []string{"user"}, Trigger: AutomationTrigger{Type: "schedule", Timezone: "Asia/Kolkata", Frequency: "daily", Hour: 9, Minute: 30, MissedPolicy: "skip", CatchUpLimit: 1}, Actions: []map[string]any{{"kind": "page.create", "pageId": "$id:note", "parentPageId": page, "title": "Daily {{occurrence}}"}}}
}
func TestAutomationAtomicDedupRestartAndOrigin(t *testing.T) {
	k, e := testKernel(t)
	page := createPage(t, k, e, "Rules")
	r := scheduleRule(page)
	saveRule(t, k, e, page, r)
	at, _ := time.Parse(time.RFC3339, "2026-10-02T04:00:00Z")
	s, err := loadAutomation(context.Background(), k.db, r.ID)
	if err != nil {
		t.Fatal(err)
	}
	key := "schedule:" + at.Format(time.RFC3339Nano)
	if err = k.executeAutomationOccurrence(context.Background(), e, s, "", key, at); err != nil {
		t.Fatal(err)
	}
	var count int
	k.db.QueryRow("SELECT count(*) FROM pages WHERE parent_page_id=?", page).Scan(&count)
	if count != 1 {
		var diag string
		k.db.QueryRow("SELECT diagnostic FROM automation_runs").Scan(&diag)
		t.Fatalf("effects=%d diagnostic=%s", count, diag)
	}
	var event string
	k.db.QueryRow("SELECT payload_json FROM committed_events ORDER BY cursor DESC LIMIT 1").Scan(&event)
	var ev contracts.ServiceCommittedChangeEvent
	json.Unmarshal([]byte(event), &ev)
	if ev.Context.Origin != "automation" {
		t.Fatal("automation cascade origin", ev.Context.Origin)
	}
	path := k.Path()
	k.Close()
	k, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer k.Close()
	e = k.OwnerContext()
	if err = k.executeAutomationOccurrence(context.Background(), e, s, "", key, at); err != nil {
		t.Fatal(err)
	}
	k.db.QueryRow("SELECT count(*) FROM pages WHERE parent_page_id=?", page).Scan(&count)
	if count != 1 {
		t.Fatal("duplicate after restart", count)
	}
}
func TestAutomationRollbackFailureRetryAndPause(t *testing.T) {
	k, e := testKernel(t)
	page := createPage(t, k, e, "Rules")
	r := scheduleRule(page)
	r.Actions = append(r.Actions, map[string]any{"kind": "page.update", "pageId": NewID(), "title": "Missing"})
	saveRule(t, k, e, page, r)
	s, _ := loadAutomation(context.Background(), k.db, r.ID)
	at, _ := time.Parse(time.RFC3339, "2026-10-02T04:00:00Z")
	key := "schedule:" + at.Format(time.RFC3339Nano)
	for i := 0; i < 3; i++ {
		if err := k.executeAutomationOccurrence(context.Background(), e, s, "", key, at); err != nil {
			t.Fatal(err)
		}
	}
	var count int
	k.db.QueryRow("SELECT count(*) FROM pages WHERE parent_page_id=?", page).Scan(&count)
	if count != 0 {
		t.Fatal("partial effects committed")
	}
	s, _ = loadAutomation(context.Background(), k.db, r.ID)
	if !s.Rule.Paused || s.Failures != 3 {
		t.Fatalf("not paused %#v", s)
	}
	// A corrected version can run; old failed evidence remains in run history.
	r.Actions = r.Actions[:1]
	saveRule(t, k, e, page, r)
	s, _ = loadAutomation(context.Background(), k.db, r.ID)
	if err := k.executeAutomationOccurrence(context.Background(), e, s, "", key, at); err != nil {
		t.Fatal(err)
	}
	k.db.QueryRow("SELECT count(*) FROM pages WHERE parent_page_id=?", page).Scan(&count)
	if count != 1 {
		t.Fatal("corrected rule failed")
	}
}
func TestAutomationCalendarDSTAndMissedPolicies(t *testing.T) {
	fixtures := []struct {
		zone, after, want string
		hour              int
	}{{"America/New_York", "2026-03-07T08:00:00Z", "2026-03-09T06:30:00Z", 2}, {"America/New_York", "2026-10-31T08:00:00Z", "2026-11-01T05:30:00Z", 1}, {"Asia/Kolkata", "2026-09-30T00:00:00Z", "2026-09-30T04:00:00Z", 9}}
	for _, f := range fixtures {
		after, _ := time.Parse(time.RFC3339, f.after)
		next, err := NextAutomationOccurrence(AutomationTrigger{Timezone: f.zone, Frequency: "daily", Hour: f.hour, Minute: 30}, after)
		if err != nil || next.Format(time.RFC3339) != f.want {
			t.Fatalf("%s got %s %v want %s", f.zone, next, err, f.want)
		}
	}
	k, e := testKernel(t)
	page := createPage(t, k, e, "Rules")
	now, _ := time.Parse(time.RFC3339, "2026-10-05T08:00:00Z")
	for _, policy := range []string{"skip", "catch_up"} {
		r := scheduleRule(page)
		r.Trigger.MissedPolicy = policy
		r.Trigger.CatchUpLimit = 2
		saveRule(t, k, e, page, r)
		_, err := k.db.Exec("UPDATE automation_rules SET next_at='2026-10-01T04:00:00Z' WHERE id=?", r.ID)
		if err != nil {
			t.Fatal(err)
		}
		s, _ := loadAutomation(context.Background(), k.db, r.ID)
		if err = k.tickSchedule(context.Background(), e, s, now); err != nil {
			t.Fatal(err)
		}
		var effects int
		k.db.QueryRow("SELECT count(*) FROM automation_runs WHERE rule_id=? AND status='succeeded'", r.ID).Scan(&effects)
		expected := 0
		if policy == "catch_up" {
			expected = 2
		}
		if effects != expected {
			t.Fatalf("%s effects %d", policy, effects)
		}
		s, _ = loadAutomation(context.Background(), k.db, r.ID)
		if err = k.tickSchedule(context.Background(), e, s, now); err != nil {
			t.Fatal(err)
		}
		var after int
		k.db.QueryRow("SELECT count(*) FROM automation_runs WHERE rule_id=? AND status='succeeded'", r.ID).Scan(&after)
		if after != effects {
			t.Fatal("sleep retry duplicated occurrences")
		}
	}
}
func TestAutomationExcludedOriginsAndEventDedup(t *testing.T) {
	k, e := testKernel(t)
	f := newDatabase(t, k, e)
	page := createPage(t, k, e, "Rules")
	r := scheduleRule(page)
	r.Trigger = AutomationTrigger{Type: "event", CollectionID: f.collection}
	saveRule(t, k, e, page, r)
	_, _ = newRecord(t, k, e, f, "Trigger")
	if err := k.RunAutomationTick(context.Background(), e, time.Now()); err != nil {
		t.Fatal(err)
	}
	var count int
	k.db.QueryRow("SELECT count(*) FROM automation_runs WHERE rule_id=? AND status='succeeded'", r.ID).Scan(&count)
	if count != 1 {
		t.Fatal("event did not fire", count)
	}
	if err := k.RunAutomationTick(context.Background(), e, time.Now()); err != nil {
		t.Fatal(err)
	}
	var after int
	k.db.QueryRow("SELECT count(*) FROM automation_runs WHERE rule_id=? AND status='succeeded'", r.ID).Scan(&after)
	if after != count {
		t.Fatal("cascade or replay", after)
	}
	for _, origin := range []string{"import", "restore", "automation"} {
		r.AllowedOrigins = []string{origin}
		if validateAutomationRule(&r) == nil {
			t.Fatal("accepted cascading origin", origin)
		}
	}
}
func TestAutomationFailedEventCanRetryAfterCursorAdvance(t *testing.T) {
	k, e := testKernel(t)
	f := newDatabase(t, k, e)
	record, page := newRecord(t, k, e, f, "Task")
	rulePage := createPage(t, k, e, "Rules")
	r := scheduleRule(rulePage)
	r.Trigger = AutomationTrigger{Type: "event", CollectionID: f.collection}
	r.Actions = []map[string]any{{"kind": "record.setValue", "recordId": record, "propertyId": f.status, "value": map[string]any{"type": "status", "schemaVersion": 1, "value": f.done}}}
	saveRule(t, k, e, rulePage, r)
	testOK(t, dbBatch(t, k, e, []any{map[string]any{"kind": "collection.configure", "collectionId": f.collection, "locked": true}}, "collection", f.collection))
	if err := k.RunAutomationTick(context.Background(), e, time.Now()); err != nil {
		t.Fatal(err)
	}
	var key string
	if err := k.db.QueryRow("SELECT occurrence_key FROM automation_runs WHERE rule_id=? AND status='failed'", r.ID).Scan(&key); err != nil {
		t.Fatal(err)
	}
	testOK(t, dbBatch(t, k, e, []any{map[string]any{"kind": "collection.configure", "collectionId": f.collection, "locked": false}}, "collection", f.collection))
	if err := k.RetryAutomation(context.Background(), e, r.ID, key); err != nil {
		t.Fatal(err)
	}
	var raw string
	k.db.QueryRow("SELECT value_json FROM property_values WHERE record_id=? AND property_id=?", record, f.status).Scan(&raw)
	if raw != string(marshal(f.done)) {
		var diag string
		k.db.QueryRow("SELECT diagnostic FROM automation_runs WHERE rule_id=? AND occurrence_key=?", r.ID, key).Scan(&diag)
		t.Fatal("retry failed", page, raw, diag)
	}
}
func TestAutomationRecurringRecordTemplateUsesAtomicNewIdentities(t *testing.T) {
	k, e := testKernel(t)
	f := newDatabase(t, k, e)
	page := createPage(t, k, e, "Rules")
	r := scheduleRule(page)
	r.Actions = []map[string]any{{"kind": "record.create", "recordId": "$id:record", "pageId": "$id:record-page", "collectionId": f.collection, "title": "Recurring {{occurrence}}"}, {"kind": "record.setValue", "recordId": "$id:record", "propertyId": f.status, "value": map[string]any{"type": "status", "schemaVersion": 1, "value": f.open}}}
	saveRule(t, k, e, page, r)
	s, _ := loadAutomation(context.Background(), k.db, r.ID)
	at, _ := time.Parse(time.RFC3339, "2026-10-02T04:00:00Z")
	key := "schedule:" + at.Format(time.RFC3339Nano)
	if err := k.executeAutomationOccurrence(context.Background(), e, s, "", key, at); err != nil {
		t.Fatal(err)
	}
	var count int
	k.db.QueryRow("SELECT count(*) FROM records WHERE collection_id=?", f.collection).Scan(&count)
	if count != 1 {
		var diagnostic string
		k.db.QueryRow("SELECT diagnostic FROM automation_runs WHERE rule_id=?", r.ID).Scan(&diagnostic)
		t.Fatal("record template failed", count, diagnostic)
	}
}
func TestAutomationRejectsScopedRulePlantingAndLogsPreviewFailures(t *testing.T) {
	k, e := testKernel(t)
	page := createPage(t, k, e, "Rules")
	scope := k.Grant(e.Actor, "user", contracts.DomainScope{Value: contracts.DomainScopeVariant3{Kind: "pages", WorkspaceId: contracts.DomainId(k.ID()), PageIds: []contracts.DomainId{contracts.DomainId(page)}}})
	r := scheduleRule(page)
	testCode(t, dbBatch(t, k, scope, []any{map[string]any{"kind": "automation.save", "pageId": page, "rule": r}}, "page", page), "SCOPE_DENIED")
	saveRule(t, k, e, page, r)
	preview := testOK(t, testDispatch(t, k, e, "automationPreview", recoveryRequest(k, map[string]any{"pageId": page, "ruleId": r.ID, "at": "2026-10-02T04:00:00Z"})))
	testOK(t, dbBatch(t, k, e, []any{map[string]any{"kind": "page.update", "pageId": page, "title": "Changed after preview"}}, "page", page))
	testCode(t, testDispatch(t, k, e, "automation.execute", recoveryRequest(k, map[string]any{"pageId": page, "ruleId": r.ID, "at": preview["at"], "occurrenceKey": preview["occurrenceKey"], "expectedRevisions": preview["expectedRevisions"]})), "CONFLICT")
	var count int
	k.db.QueryRow("SELECT count(*) FROM automation_runs WHERE rule_id=? AND status='failed'", r.ID).Scan(&count)
	if count != 1 {
		t.Fatal("preview failure not logged")
	}
	k.db.QueryRow("SELECT count(*) FROM pages WHERE parent_page_id=?", page).Scan(&count)
	if count != 0 {
		t.Fatal("stale preview committed")
	}
}
