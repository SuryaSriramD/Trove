package workspace

import (
	"context"
	"database/sql"
	"encoding/json"
	"testing"
	"time"

	"trove.local/phase0/contracts"
)

func scheduleTestDate(start, end string, timed bool, zone string) scheduleDate {
	d := scheduleDate{Start: start, HasTime: timed, Timezone: zone}
	if end != "" {
		d.End = &end
	}
	return d
}
func scheduleTestPolicy(calendar, zone string) schedulePolicy {
	return schedulePolicy{Version: 1, Calendar: calendar, Timezone: zone, Weekdays: []int{1, 2, 3, 4, 5}}
}
func TestSchedulingCalendarInclusiveWeekdaysAndElapsedDST(t *testing.T) {
	c, err := newScheduleCalendar(scheduleTestPolicy("working_week", "America/New_York"))
	if err != nil {
		t.Fatal(err)
	}
	d := scheduleTestDate("2026-03-06", "2026-03-10", false, "America/New_York")
	duration, err := c.duration(d)
	if err != nil || duration != 3 {
		t.Fatal(duration, err)
	}
	start, _, _ := c.parse(scheduleTestDate("2026-03-12", "", false, "America/New_York"))
	shifted, err := c.shifted(d, start, duration)
	if err != nil || shifted.Start != "2026-03-12" || *shifted.End != "2026-03-16" {
		t.Fatal(shifted, err)
	}
	constraint, err := c.finishConstraint(scheduleTestDate("2026-03-13", "", false, "America/New_York"))
	if err != nil || constraint.Format("2006-01-02") != "2026-03-16" {
		t.Fatal(constraint, err)
	}
	timed := scheduleTestDate("2026-03-06T12:00:00-05:00", "2026-03-09T12:00:00-04:00", true, "America/New_York")
	duration, err = c.duration(timed)
	if err != nil || time.Duration(duration) != 24*time.Hour {
		t.Fatal(time.Duration(duration), err)
	}
	start, _, _ = c.parse(scheduleTestDate("2026-03-13T12:00:00-04:00", "", true, "America/New_York"))
	shifted, err = c.shifted(timed, start, duration)
	if err != nil || *shifted.End != "2026-03-16T12:00:00-04:00" {
		t.Fatal(shifted, err)
	}
	c, _ = newScheduleCalendar(scheduleTestPolicy("calendar_days", "America/New_York"))
	timed = scheduleTestDate("2026-03-08T00:00:00-05:00", "2026-03-09T00:00:00-04:00", true, "America/New_York")
	duration, err = c.duration(timed)
	if err != nil || time.Duration(duration) != 23*time.Hour {
		t.Fatal(time.Duration(duration), err)
	}
	start, _, _ = c.parse(scheduleTestDate("2026-03-09T00:00:00-04:00", "", true, "America/New_York"))
	shifted, err = c.shifted(timed, start, duration)
	if err != nil || *shifted.End != "2026-03-09T23:00:00-04:00" || len(c.dstNotes(timed, shifted)) != 1 {
		t.Fatal(shifted, err)
	}
	// A selected Sunday is a whole local day: DST makes it 23 elapsed hours.
	p := scheduleTestPolicy("working_week", "America/New_York")
	p.Weekdays = []int{0}
	c, _ = newScheduleCalendar(p)
	duration, err = c.duration(timed)
	if err != nil || time.Duration(duration) != 23*time.Hour {
		t.Fatal(time.Duration(duration), err)
	}
}
func TestSchedulingLocalDayBoundariesAndFallDST(t *testing.T) {
	p := scheduleTestPolicy("working_week", "America/New_York")
	p.Weekdays = []int{0}
	c, _ := newScheduleCalendar(p)
	d := scheduleTestDate("2026-11-01T00:00:00-04:00", "2026-11-02T00:00:00-05:00", true, p.Timezone)
	duration, err := c.duration(d)
	if err != nil || time.Duration(duration) != 25*time.Hour {
		t.Fatal(time.Duration(duration), err)
	}
	start, _, _ := c.parse(scheduleTestDate("2026-11-08T00:00:00-05:00", "", true, p.Timezone))
	shifted, err := c.shifted(d, start, duration)
	if err != nil || *shifted.End != "2026-11-15T01:00:00-05:00" || len(c.dstNotes(d, shifted)) != 1 {
		t.Fatal(shifted, err)
	}
	// Brazil advanced at midnight: the whole Sunday began at 01:00.
	p.Timezone = "America/Sao_Paulo"
	c, _ = newScheduleCalendar(p)
	d = scheduleTestDate("2018-11-04T01:00:00-02:00", "2018-11-05T00:00:00-02:00", true, p.Timezone)
	duration, err = c.duration(d)
	if err != nil || time.Duration(duration) != 23*time.Hour {
		t.Fatal(time.Duration(duration), err)
	}
	dateOnly := scheduleTestDate("2018-11-04", "2018-11-04", false, p.Timezone)
	if dates, err := c.duration(dateOnly); err != nil || dates != 1 {
		t.Fatal("midnight gap must preserve date-only Sunday", dates, err)
	}
	// Samoa skipped an entire date. Traversal must advance and explicit
	// nonexistent dates must not silently become another date.
	c, _ = newScheduleCalendar(scheduleTestPolicy("calendar_days", "Pacific/Apia"))
	start, _, err = c.parse(scheduleTestDate("2011-12-29", "", false, "Pacific/Apia"))
	if err != nil || c.nextDay(start).Format("2006-01-02") != "2011-12-31" {
		t.Fatal(start, err)
	}
	if _, _, err = c.parse(scheduleTestDate("2011-12-30", "", false, "Pacific/Apia")); err == nil {
		t.Fatal("nonexistent civil date was normalized silently")
	}
}

func schedulePreviewFixture(t *testing.T, k *Kernel, e contracts.ServiceExecutionContext, input scheduleInput) schedulingPlan {
	t.Helper()
	raw := testOK(t, testDispatch(t, k, e, "scheduling.preview", map[string]any{"contractVersion": 1, "requestId": NewID(), "workspaceId": k.ID(), "input": input}))
	if err := contracts.Validate("service", "SchedulingPlan", marshal(raw)); err != nil {
		t.Fatal(err)
	}
	var plan schedulingPlan
	if err := json.Unmarshal(marshal(raw), &plan); err != nil {
		t.Fatal(err)
	}
	return plan
}
func scheduleApplyFixture(k *Kernel, e contracts.ServiceExecutionContext, p schedulingPlan) map[string]any {
	return testRequest(k, e, []any{map[string]any{"kind": "scheduling.apply", "plan": p}}, p.RequiredRevisions...)
}
func TestSchedulingReviewedBranchSlackAtomicUndoAndRestart(t *testing.T) {
	k, e := testKernel(t)
	f := newDatabase(t, k, e)
	a, ap := newRecord(t, k, e, f, "Predecessor")
	b, bp := newRecord(t, k, e, f, "Dependent")
	c, cp := newRecord(t, k, e, f, "Slack")
	for _, row := range []struct{ id, page, start, end string }{{a, ap, "2026-10-12", "2026-10-13"}, {b, bp, "2026-10-14", "2026-10-15"}, {c, cp, "2026-10-25", "2026-10-26"}} {
		testOK(t, setValue(t, k, e, f, row.id, row.page, f.date, "date", scheduleTestDate(row.start, row.end, false, "UTC")))
	}
	for _, r := range [][2]string{{b, bp}, {c, cp}} {
		testOK(t, dbBatch(t, k, e, []any{map[string]any{"kind": "record.configure", "recordId": r[0], "parentRecordId": nil, "dependencyRecordIds": []string{a}}}, "record", a, "record", r[0], "page", r[1], "collection", f.collection))
	}
	var beforeCursor, afterCursor int64
	k.db.QueryRow("SELECT COALESCE(MAX(cursor),0) FROM committed_events").Scan(&beforeCursor)
	input := scheduleInput{f.collection, f.date, a, "2026-10-16", scheduleTestPolicy("working_week", "UTC")}
	plan := schedulePreviewFixture(t, k, e, input)
	k.db.QueryRow("SELECT COALESCE(MAX(cursor),0) FROM committed_events").Scan(&afterCursor)
	if beforeCursor != afterCursor {
		t.Fatal("Preview/cancellation wrote canonical state")
	}
	got := map[string]scheduleChange{}
	for _, r := range plan.Records {
		got[r.RecordID] = r
	}
	if got[a].After.Start != "2026-10-16" || *got[a].After.End != "2026-10-19" || got[b].After.Start != "2026-10-20" || *got[b].After.End != "2026-10-21" || got[c].Changed || got[c].Reason != "slack_preserved" {
		t.Fatal(got)
	}
	req := scheduleApplyFixture(k, e, plan)
	testOK(t, testDispatch(t, k, e, "batch", req))
	testOK(t, testDispatch(t, k, e, "batch", req))
	var receipts int
	k.db.QueryRow("SELECT count(*) FROM scheduling_plans").Scan(&receipts)
	if receipts != 1 {
		t.Fatal("retry duplicated receipt")
	}
	testOK(t, dbBatch(t, k, e, []any{map[string]any{"kind": "page.update", "pageId": bp, "title": "Later independent title"}}, "page", bp, "record", b, "collection", f.collection))
	path := k.Path()
	k.Close()
	reopened, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	k = reopened
	e = k.OwnerContext()
	undo := testOK(t, testDispatch(t, k, e, "operations.previewUndo", map[string]any{"contractVersion": 1, "requestId": NewID(), "workspaceId": k.ID(), "operationId": plan.ID}))
	var guards []contracts.ServiceExpectedRevision
	json.Unmarshal(marshal(undo["expectedRevisions"]), &guards)
	testOK(t, testDispatch(t, k, e, "batch", testRequest(k, e, []any{undo["command"]}, guards...)))
	var raw, title string
	k.db.QueryRow("SELECT value_json FROM property_values WHERE record_id=? AND property_id=?", b, f.date).Scan(&raw)
	var restored scheduleDate
	json.Unmarshal([]byte(raw), &restored)
	k.db.QueryRow("SELECT title FROM pages WHERE id=?", bp).Scan(&title)
	if restored.Start != "2026-10-14" || title != "Later independent title" {
		t.Fatal(restored, title)
	}
}
func TestSchedulingRejectsStaleGraphDatesMissingMixedAndCycles(t *testing.T) {
	k, e := testKernel(t)
	f := newDatabase(t, k, e)
	a, ap := newRecord(t, k, e, f, "A")
	b, bp := newRecord(t, k, e, f, "B")
	set := func(id, page string, d scheduleDate) { testOK(t, setValue(t, k, e, f, id, page, f.date, "date", d)) }
	set(a, ap, scheduleTestDate("2026-10-12", "", false, "UTC"))
	set(b, bp, scheduleTestDate("2026-10-13", "", false, "UTC"))
	testOK(t, dbBatch(t, k, e, []any{map[string]any{"kind": "record.configure", "recordId": b, "parentRecordId": nil, "dependencyRecordIds": []string{a}}}, "record", a, "record", b, "page", bp, "collection", f.collection))
	input := scheduleInput{f.collection, f.date, a, "2026-10-15", scheduleTestPolicy("calendar_days", "UTC")}
	plan := schedulePreviewFixture(t, k, e, input)
	set(b, bp, scheduleTestDate("2026-10-20", "", false, "UTC"))
	testCode(t, testDispatch(t, k, e, "batch", scheduleApplyFixture(k, e, plan)), "CONFLICT")
	// Even with newly supplied transport guards, the immutable preview is stale.
	testCode(t, dbBatch(t, k, e, []any{map[string]any{"kind": "scheduling.apply", "plan": plan}}, "record", a, "record", b, "page", ap, "page", bp, "collection", f.collection, "property", f.date), "CONFLICT")
	plan = schedulePreviewFixture(t, k, e, input)
	testOK(t, dbBatch(t, k, e, []any{map[string]any{"kind": "record.configure", "recordId": b, "parentRecordId": nil, "dependencyRecordIds": []string{}}}, "record", b, "page", bp, "collection", f.collection))
	testCode(t, testDispatch(t, k, e, "batch", scheduleApplyFixture(k, e, plan)), "CONFLICT")
	testOK(t, dbBatch(t, k, e, []any{map[string]any{"kind": "record.configure", "recordId": b, "parentRecordId": nil, "dependencyRecordIds": []string{a}}}, "record", a, "record", b, "page", bp, "collection", f.collection))
	preview := func() map[string]any {
		return testDispatch(t, k, e, "scheduling.preview", map[string]any{"contractVersion": 1, "requestId": NewID(), "workspaceId": k.ID(), "input": input})
	}
	set(b, bp, scheduleTestDate("2026-10-20T12:00:00Z", "", true, "UTC"))
	testCode(t, preview(), "CONFLICT")
	testOK(t, dbBatch(t, k, e, []any{map[string]any{"kind": "record.clearValue", "recordId": b, "propertyId": f.date}}, "record", b, "page", bp, "collection", f.collection, "property", f.date))
	testCode(t, preview(), "CONFLICT")
	set(b, bp, scheduleTestDate("2026-10-20", "", false, "UTC"))
	// Corrupt/imported graph evidence must also be rejected before any writes.
	if err := k.write(context.Background(), func(tx *sql.Tx) error {
		_, err := tx.Exec("INSERT INTO record_dependencies VALUES(?,?)", a, b)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	testCode(t, preview(), "CONFLICT")
	var applied int
	k.db.QueryRow("SELECT count(*) FROM scheduling_plans").Scan(&applied)
	if applied != 0 {
		t.Fatal("Invalid schedule partly applied")
	}
}

func TestSchedulingScopeTamperingLockedAtomicityAndTimedGraph(t *testing.T) {
	k, e := testKernel(t)
	f := newDatabase(t, k, e)
	a, ap := newRecord(t, k, e, f, "A")
	b, bp := newRecord(t, k, e, f, "B")
	testOK(t, setValue(t, k, e, f, a, ap, f.date, "date", scheduleTestDate("2026-03-07T12:00:00-05:00", "2026-03-08T12:00:00-04:00", true, "America/New_York")))
	testOK(t, setValue(t, k, e, f, b, bp, f.date, "date", scheduleTestDate("2026-03-08T12:00:00-04:00", "2026-03-08T14:00:00-04:00", true, "America/New_York")))
	testOK(t, dbBatch(t, k, e, []any{map[string]any{"kind": "record.configure", "recordId": b, "parentRecordId": nil, "dependencyRecordIds": []string{a}}}, "record", a, "record", b, "page", bp, "collection", f.collection))
	input := scheduleInput{f.collection, f.date, a, "2026-03-08T12:00:00-04:00", scheduleTestPolicy("calendar_days", "America/New_York")}
	plan := schedulePreviewFixture(t, k, e, input)
	for _, r := range plan.Records {
		if r.RecordID == b && (r.After.Start != "2026-03-09T11:00:00-04:00" || *r.After.End != "2026-03-09T13:00:00-04:00") {
			t.Fatal(r)
		}
	}
	scoped := k.Grant(e.Actor, "user", contracts.DomainScope{Value: contracts.DomainScopeVariant3{Kind: "pages", WorkspaceId: contracts.DomainId(k.ID()), PageIds: []contracts.DomainId{contracts.DomainId(f.page), contracts.DomainId(ap)}}})
	testCode(t, testDispatch(t, k, scoped, "scheduling.preview", map[string]any{"contractVersion": 1, "requestId": NewID(), "workspaceId": k.ID(), "input": input}), "SCOPE_DENIED")
	tampered := plan
	tampered.Records = append([]scheduleChange{}, plan.Records...)
	tampered.Records[0].After.Start = "2026-12-01T00:00:00Z"
	testCode(t, testDispatch(t, k, e, "batch", scheduleApplyFixture(k, e, tampered)), "CONFLICT")
	// A lock can be present at preview; the canonical writer still rejects the
	// entire reviewed change without a receipt or a partial date mutation.
	testOK(t, dbBatch(t, k, e, []any{map[string]any{"kind": "collection.configure", "collectionId": f.collection, "locked": true}}, "collection", f.collection))
	plan = schedulePreviewFixture(t, k, e, input)
	testCode(t, testDispatch(t, k, e, "batch", scheduleApplyFixture(k, e, plan)), "SCOPE_DENIED")
	var count int
	k.db.QueryRow("SELECT count(*) FROM scheduling_plans").Scan(&count)
	if count != 0 {
		t.Fatal("rejected plan retained as applied")
	}
	var raw string
	k.db.QueryRow("SELECT value_json FROM property_values WHERE record_id=? AND property_id=?", a, f.date).Scan(&raw)
	var d scheduleDate
	json.Unmarshal([]byte(raw), &d)
	if d.Start != "2026-03-07T12:00:00-05:00" {
		t.Fatal("partial schedule mutation")
	}
}
