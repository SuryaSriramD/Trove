package workspace

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"trove.local/phase0/contracts"
	"trove.local/phase0/internal/reminders"
)

func reminderFixture(t *testing.T, k *Kernel, e contracts.ServiceExecutionContext, page string) string {
	t.Helper()
	id := NewID()
	testOK(t, dbBatch(t, k, e, []any{map[string]any{"kind": "reminder.put", "reminderId": id, "pageId": page, "expectedRevision": "0", "startDate": "2026-10-01", "localTime": "09:00", "timezone": "Asia/Kolkata", "frequency": "daily", "enabled": true, "private": true}}, "page", page))
	return id
}
func TestReminderCanonicalHistoryPrivacyInboxAndReceiptEpoch(t *testing.T) {
	k, e := testKernel(t)
	ctx := context.Background()
	page := createPage(t, k, e, "Private appointment")
	id := reminderFixture(t, k, e, page)
	through := time.Date(2026, 10, 3, 12, 0, 0, 0, time.UTC)
	state, err := k.Reminders(ctx, e, through)
	if err != nil || len(state.Inbox) != 3 {
		t.Fatal(state, err)
	}
	if err = contracts.Validate("service", "ReminderState", marshal(state)); err != nil {
		t.Fatal(err)
	}
	state, err = k.Reminders(ctx, e, through)
	if err != nil || len(state.Inbox) != 3 {
		t.Fatal("duplicate occurrences", state, err)
	}
	s, err := k.ReminderSchedule(ctx, e)
	if err != nil || len(s.Rules) != 1 || s.Rules[0].Title != "Trove reminder" {
		t.Fatal(s, err)
	}
	s.Epoch = NewID()
	s.Generation = 1
	if err = contracts.Validate("service", "ReminderSchedule", marshal(s)); err != nil {
		t.Fatal(err)
	}
	r := reminders.Receipt{Version: 1, Epoch: NewID(), Generation: 1, Deliveries: []reminders.Delivery{state.Inbox[0].Delivery}}
	r.Deliveries[0].Outcome = "submitted"
	if err = k.ImportReminderReceipt(ctx, e, s, r); err == nil {
		t.Fatal("old registration accepted")
	}
	r.Epoch = s.Epoch
	if err = k.ImportReminderReceipt(ctx, e, s, r); err != nil {
		t.Fatal(err)
	}
	item := state.Inbox[0]
	testOK(t, dbBatch(t, k, e, []any{map[string]any{"kind": "reminder.inboxAction", "occurrenceId": item.ID, "expectedAction": "", "expectedActionRevision": 0, "action": "dismissed"}}, "page", page))
	r.Deliveries[0].Action = "snoozed"
	r.Deliveries[0].SnoozedUntil = time.Now().Add(time.Hour).UTC().Format(time.RFC3339)
	if err = k.ImportReminderReceipt(ctx, e, s, r); err != nil {
		t.Fatal(err)
	}
	state, err = k.Reminders(ctx, e, through)
	if err != nil || state.Inbox[0].Action != "dismissed" || state.Inbox[0].Outcome != "submitted" {
		t.Fatal(state, err)
	}
	commands := []any{map[string]any{"kind": "reminder.put", "reminderId": id, "pageId": page, "expectedRevision": "0", "startDate": "2026-10-01", "localTime": "09:00", "timezone": "UTC", "frequency": "once", "enabled": false, "private": true}}
	testCode(t, dbBatch(t, k, e, commands, "page", page), "CONFLICT")
	commands[0].(map[string]any)["expectedRevision"] = "1"
	testOK(t, dbBatch(t, k, e, commands, "page", page))
	var history int
	k.db.QueryRow("SELECT count(*) FROM reminder_history_v1 WHERE reminder_id=?", id).Scan(&history)
	if history != 2 {
		t.Fatal(history)
	}
	other := k.Grant(contracts.ServiceActor{Kind: "local_ai", ActorId: contracts.DomainId(NewID())}, "ai", e.Scope)
	if _, err = k.ReminderSchedule(ctx, other); err == nil {
		t.Fatal("model obtained derived notification state")
	}
}
func TestReminderCanonicalTaskCompletionSuppressesSchedule(t *testing.T) {
	k, e := testKernel(t)
	ctx := context.Background()
	f := newDatabase(t, k, e)
	record, page := newRecord(t, k, e, f, "Task")
	testOK(t, dbBatch(t, k, e, []any{map[string]any{"kind": "task.promote", "recordId": record, "statusPropertyId": f.status}}, "page", page, "record", record, "collection", f.collection, "property", f.status))
	id := NewID()
	testOK(t, dbBatch(t, k, e, []any{map[string]any{"kind": "reminder.put", "reminderId": id, "pageId": page, "expectedRevision": "0", "startDate": "2026-10-01", "localTime": "09:00", "timezone": "UTC", "frequency": "daily", "enabled": true, "private": true}}, "page", page, "record", record, "collection", f.collection))
	testOK(t, dbBatch(t, k, e, []any{map[string]any{"kind": "task.setStatus", "recordId": record, "statusOptionId": f.done}}, "page", page, "record", record, "collection", f.collection, "property", f.status))
	s, err := k.ReminderSchedule(ctx, e)
	if err != nil || len(s.Rules) != 1 || s.Rules[0].Enabled {
		t.Fatal(s, err)
	}
}

func TestReminderPagingRetainsEveryOccurrenceAndLongTitles(t *testing.T) {
	k, e := testKernel(t)
	ctx := context.Background()
	page := createPage(t, k, e, strings.Repeat("📅", 180))
	id := NewID()
	testOK(t, dbBatch(t, k, e, []any{map[string]any{"kind": "reminder.put", "reminderId": id, "pageId": page, "expectedRevision": "0", "startDate": "2020-01-01", "localTime": "09:00", "timezone": "UTC", "frequency": "daily", "enabled": true, "private": true}}, "page", page))
	through := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	for i := 0; i < 4; i++ {
		if _, err := k.Reminders(ctx, e, through); err != nil {
			t.Fatal(err)
		}
	}
	seen := map[string]bool{}
	cursor := ""
	for {
		page, err := k.remindersPage(ctx, e, through, cursor)
		if err != nil {
			t.Fatal(err)
		}
		for _, item := range page.Inbox {
			if seen[item.ID] {
				t.Fatal("duplicate cursor result", item.ID)
			}
			seen[item.ID] = true
		}
		if page.NextCursor == "" {
			break
		}
		cursor = page.NextCursor
	}
	if len(seen) != 2193 {
		t.Fatal("lost retained occurrences", len(seen))
	}
}
func TestReminderRepeatedNativeSnoozeAndStaleAction(t *testing.T) {
	k, e := testKernel(t)
	ctx := context.Background()
	page := createPage(t, k, e, "Reminder")
	reminderFixture(t, k, e, page)
	through := time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC)
	state, err := k.Reminders(ctx, e, through)
	if err != nil {
		t.Fatal(err)
	}
	s, err := k.ReminderSchedule(ctx, e)
	if err != nil {
		t.Fatal(err)
	}
	s.Epoch = NewID()
	s.Generation = 1
	d := state.Inbox[0].Delivery
	d.Outcome = "submitted"
	d.Action = "snoozed"
	d.ActionRevision = 1
	d.SnoozedUntil = time.Now().Add(time.Hour).UTC().Format(time.RFC3339)
	r := reminders.Receipt{Version: 1, Epoch: s.Epoch, Generation: 1, Deliveries: []reminders.Delivery{d}}
	if err = k.ImportReminderReceipt(ctx, e, s, r); err != nil {
		t.Fatal(err)
	}
	d.ActionRevision = 2
	d.SnoozedUntil = time.Now().Add(2 * time.Hour).UTC().Format(time.RFC3339)
	r.Deliveries[0] = d
	if err = k.ImportReminderReceipt(ctx, e, s, r); err != nil {
		t.Fatal(err)
	}
	r.Deliveries[0].ActionRevision = 1
	r.Deliveries[0].SnoozedUntil = time.Now().Add(time.Hour).UTC().Format(time.RFC3339)
	if err = k.ImportReminderReceipt(ctx, e, s, r); err != nil {
		t.Fatal(err)
	}
	state, err = k.Reminders(ctx, e, through)
	if err != nil || state.Inbox[0].ActionRevision != 2 || state.Inbox[0].SnoozedUntil != d.SnoozedUntil {
		t.Fatal(state, err)
	}
	testCode(t, dbBatch(t, k, e, []any{map[string]any{"kind": "reminder.inboxAction", "occurrenceId": d.ID, "expectedAction": "snoozed", "expectedActionRevision": 1, "action": "dismissed"}}, "page", page), "CONFLICT")
	r.Deliveries[0].SnoozedUntil = "invalid"
	if err = k.ImportReminderReceipt(ctx, e, s, r); err == nil {
		t.Fatal("invalid snooze receipt accepted")
	}
}
func TestReminderBackupRestoresEvidenceWithoutActivationIdentity(t *testing.T) {
	k, e := testKernel(t)
	ctx := context.Background()
	page := createPage(t, k, e, "Retained reminder")
	reminderFixture(t, k, e, page)
	key, err := k.ReminderRegistrationKey(ctx, e)
	if err != nil {
		t.Fatal(err)
	}
	state, err := k.Reminders(ctx, e, time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	backup := filepath.Join(t.TempDir(), "snapshot")
	if _, err = k.CreateBackup(ctx, e, backup); err != nil {
		t.Fatal(err)
	}
	destination := filepath.Join(t.TempDir(), "restored")
	if _, err = RestoreBackup(ctx, backup, destination); err != nil {
		t.Fatal(err)
	}
	restored, err := Open(destination)
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()
	next, err := restored.ReminderRegistrationKey(ctx, restored.OwnerContext())
	if err != nil || key == next {
		t.Fatal("portable restore reused activation identity", next, err)
	}
	got, err := restored.Reminders(ctx, restored.OwnerContext(), time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC))
	if err != nil || len(got.Inbox) != len(state.Inbox) || len(got.Rules) != 1 {
		t.Fatal(got, err)
	}
}
