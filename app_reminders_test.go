package main

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"
	"trove.local/phase0/internal/reminders"
	"trove.local/phase0/internal/workspace"
)

func TestReminderDesktopOptInPublicationAndWorkspaceIsolation(t *testing.T) {
	a := supportTestApp(t)
	store, err := reminders.OpenStore(filepath.Join(t.TempDir(), "derived"))
	if err != nil {
		t.Fatal(err)
	}
	registration := "notRegistered"
	actions := []int{}
	s := &reminderDesktop{store: store, data: reminderDesktopFile{Version: 1, Bindings: map[string]reminderActivation{}}, stop: make(chan struct{}), done: make(chan struct{})}
	s.native = func(action int) (string, error) {
		if action != 0 {
			actions = append(actions, action)
		}
		if action == 1 {
			registration = "enabled"
		}
		if action == 2 {
			registration = "notRegistered"
		}
		return registration, nil
	}
	a.reminders = s
	stop, done := make(chan struct{}), make(chan struct{})
	defer func() { close(stop); <-done; store.Close() }()
	go func() {
		defer close(done)
		ticker := time.NewTicker(5 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-stop:
				return
			case <-ticker.C:
				var schedule reminders.Schedule
				if store.Read("schedule.json", &schedule) == nil {
					_ = store.Write("receipt-"+schedule.Epoch+".json", reminders.Receipt{Version: 1, Epoch: schedule.Epoch, Generation: schedule.Generation, Permission: "authorized", AcknowledgedAt: time.Now().UTC().Format(time.RFC3339), Deliveries: []reminders.Delivery{}})
				}
			}
		}
	}()
	status, err := a.ReminderStatus()
	if err != nil || status.Enabled || s.data.Published.Active || len(actions) != 0 {
		t.Fatal(status, actions, err)
	}
	status, err = a.SetReminderBackgroundEnabled(true)
	if err != nil || !status.Enabled || status.Publication != "acknowledged" || !s.data.Published.Active || len(actions) != 1 || actions[0] != 1 {
		t.Fatal(status, actions, err)
	}
	priorGeneration := s.data.Published.Generation
	status, err = a.RefreshReminders()
	if err != nil || status.Publication != "acknowledged" || status.Generation <= priorGeneration {
		t.Fatal("reconcile reused old publication", status, err)
	}
	firstEpoch := s.data.Published.Epoch
	next, err := workspace.Create(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer next.Close()
	a.mu.Lock()
	a.kernel = next
	a.execution = next.OwnerContext()
	a.mu.Unlock()
	status, err = a.ReminderStatus()
	if err != nil || status.Enabled || s.data.Published.Active || s.data.Published.Epoch == firstEpoch {
		t.Fatal("workspace inherited activation", status, err)
	}
	status, err = a.SetReminderBackgroundEnabled(false)
	if err != nil || status.Enabled || s.data.Published.Active || actions[len(actions)-1] != 2 {
		t.Fatal(status, actions, err)
	}
	for _, binding := range s.data.Bindings {
		if binding.Enabled {
			t.Fatal("disable left another workspace active")
		}
	}
}
func TestReminderDesktopRejectsStaleAcknowledgement(t *testing.T) {
	a := supportTestApp(t)
	store, err := reminders.OpenStore(filepath.Join(t.TempDir(), "derived"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	key, err := a.kernel.ReminderRegistrationKey(context.Background(), a.execution)
	if err != nil {
		t.Fatal(err)
	}
	s := &reminderDesktop{store: store, data: reminderDesktopFile{Version: 1, Bindings: map[string]reminderActivation{key: {Enabled: true, Epoch: workspace.NewID()}}}, native: func(int) (string, error) { return "enabled", nil }}
	if _, err = a.syncReminderPublication(s, ""); err != nil {
		t.Fatal(err)
	}
	if err = store.Write("receipt-"+s.data.Published.Epoch+".json", reminders.Receipt{Version: 1, Epoch: s.data.Published.Epoch, Generation: s.data.Published.Generation, Permission: "denied", AcknowledgedAt: time.Now().Add(-time.Minute).UTC().Format(time.RFC3339), Deliveries: []reminders.Delivery{}}); err != nil {
		t.Fatal(err)
	}
	status, err := a.syncReminderPublication(s, "")
	if err != nil || status.Publication != "unknown" || status.Permission != "denied" {
		t.Fatal(status, err)
	}
	status, err = a.waitReminderAcknowledgementUntil(s, 20*time.Millisecond)
	if err != nil || status.Publication != "unknown" {
		t.Fatal("stale acknowledgement revived an absent helper", status, err)
	}
	if reminderAcknowledgementFresh(time.Now().Add(time.Hour).UTC().Format(time.RFC3339)) {
		t.Fatal("future acknowledgement accepted")
	}
}

func TestReminderRevocationAttemptsOSEvenWhenPublicationIsCorrupt(t *testing.T) {
	a := supportTestApp(t)
	store, err := reminders.OpenStore(filepath.Join(t.TempDir(), "derived"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	calls := []int{}
	s := &reminderDesktop{store: store, data: reminderDesktopFile{Version: 1, Bindings: map[string]reminderActivation{"old": {Enabled: true, Epoch: workspace.NewID()}}, Published: reminders.Schedule{Epoch: workspace.NewID(), WorkspaceID: a.kernel.ID()}}, native: func(action int) (string, error) { calls = append(calls, action); return "notRegistered", nil }}
	a.reminders = s
	if err = store.Write("schedule.json", map[string]any{"generation": "corrupt"}); err != nil {
		t.Fatal(err)
	}
	status, err := a.SetReminderBackgroundEnabled(false)
	if err == nil || status.Enabled || len(calls) != 1 || calls[0] != 2 {
		t.Fatal(status, calls, err)
	}
}

func TestReminderReceiptCacheReconcilesLaterRecoveryBatches(t *testing.T) {
	a := supportTestApp(t)
	ctx := context.Background()
	page, id := workspace.NewID(), workspace.NewID()
	request := map[string]any{"contractVersion": 1, "requestId": workspace.NewID(), "workspaceId": a.kernel.ID(), "mutation": map[string]any{"scopeGrantId": a.execution.ScopeGrantId, "idempotencyKey": workspace.NewID(), "expectedRevisions": []any{map[string]any{"target": map[string]any{"entityType": "page", "entityId": page}, "revision": "0"}}}, "commands": []any{map[string]any{"kind": "page.create", "pageId": page, "title": "Recovery"}, map[string]any{"kind": "reminder.put", "reminderId": id, "pageId": page, "expectedRevision": "0", "startDate": "2020-01-01", "localTime": "09:00", "timezone": "UTC", "frequency": "daily", "enabled": true, "private": true}}}
	raw, _ := json.Marshal(request)
	var response struct {
		OK bool `json:"ok"`
	}
	reply := a.kernel.Dispatch(ctx, a.execution, "batch", raw)
	if err := json.Unmarshal(reply, &response); err != nil || !response.OK {
		t.Fatal(string(reply), err)
	}
	store, err := reminders.OpenStore(filepath.Join(t.TempDir(), "derived"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	s := &reminderDesktop{store: store, data: reminderDesktopFile{Version: 1, Bindings: map[string]reminderActivation{}}, native: func(int) (string, error) { return "notRegistered", nil }}
	if _, err = a.syncReminderPublication(s, ""); err != nil {
		t.Fatal(err)
	}
	occurrences, _, err := reminders.Occurrences(s.data.Published.Rules[0], time.Time{}, time.Now(), 10000)
	if err != nil || len(occurrences) <= 2000 {
		t.Fatal(len(occurrences), err)
	}
	deliveries := make([]reminders.Delivery, len(occurrences))
	for i, item := range occurrences {
		deliveries[i] = reminders.Delivery{Occurrence: item, Outcome: "unknown"}
	}
	receipt := reminders.Receipt{Version: 1, Epoch: s.data.Published.Epoch, Generation: s.data.Published.Generation, Permission: "authorized", AcknowledgedAt: time.Now().UTC().Format(time.RFC3339), Deliveries: deliveries}
	if err = a.kernel.ImportReminderReceipt(ctx, a.execution, s.data.Published, receipt); err != nil {
		t.Fatal(err)
	}
	cached, _ := json.Marshal(deliveries)
	s.lastReceipt = string(cached)
	for i := 0; i < 3; i++ {
		receipt.Generation = s.data.Published.Generation
		if err = store.Write("receipt-"+receipt.Epoch+".json", receipt); err != nil {
			t.Fatal(err)
		}
		if _, err = a.syncReminderPublication(s, ""); err != nil {
			t.Fatal(err)
		}
	}
	state, err := a.kernel.Reminders(ctx, a.execution, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range state.Inbox {
		if item.Outcome != "unknown" {
			t.Fatal("cached receipt lost delivery evidence for a later recovered occurrence", item)
		}
	}
}
