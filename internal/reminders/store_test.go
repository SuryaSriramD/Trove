package reminders

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func TestReminderPublicationOwnershipAndAcknowledgement(t *testing.T) {
	root := filepath.Join(t.TempDir(), "reminders")
	store, err := OpenStore(root)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if second, err := OpenStore(root); err == nil {
		second.Close()
		t.Fatal("second schedule publisher admitted")
	}
	s := Schedule{Epoch: "epoch", WorkspaceID: "workspace", Rules: []Rule{}, Overrides: []Delivery{}}
	s, err = store.Publish(s)
	if err != nil {
		t.Fatal(err)
	}
	if s.Generation != 1 {
		t.Fatal(s)
	}
	r := Receipt{Version: 1, Epoch: s.Epoch, Generation: 0, AcknowledgedAt: time.Now().UTC().Format(time.RFC3339), Deliveries: []Delivery{}}
	if err = store.Write("receipt-epoch.json", r); err != nil {
		t.Fatal(err)
	}
	if _, err = store.Receipt(s); err == nil {
		t.Fatal("old acknowledgement accepted")
	}
	r.Generation = 1
	if err = store.Write("receipt-epoch.json", r); err != nil {
		t.Fatal(err)
	}
	if _, err = store.Receipt(s); err != nil {
		t.Fatal(err)
	}
	s, err = store.Publish(s)
	if err != nil || s.Generation != 2 {
		t.Fatal(s, err)
	}
	info, err := os.Stat(filepath.Join(root, "schedule.json"))
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatal(info, err)
	}
	if err = os.WriteFile(filepath.Join(root, "schedule.json"), []byte("truncated"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = store.Publish(s); err == nil {
		t.Fatal("corrupt publication reset its generation")
	}
}
func TestReminderSwiftCalendarParity(t *testing.T) {
	helper := os.Getenv("TROVE_REMINDER_FIXTURE")
	if helper == "" {
		t.Skip("requires compiled Swift reminder helper; schedule-only mode never initializes notifications")
	}
	for _, c := range [][4]string{{"2026-03-07", "02:30", "America/New_York", "2026-03-10T12:00:00Z"}, {"2026-10-31", "01:30", "America/New_York", "2026-11-03T12:00:00Z"}, {"2011-12-30", "09:00", "Pacific/Apia", "2012-01-03T12:00:00Z"}, {"2026-10-15", "09:00", "Asia/Kolkata", "2026-10-18T12:00:00Z"}} {
		rule := Rule{ID: "019a1234-5678-7000-8000-000000000001", Revision: "1", PageID: "019a1234-5678-7000-8000-000000000002", StartDate: c[0], LocalTime: c[1], Timezone: c[2], Frequency: "daily", Enabled: true, Private: true}
		through, _ := time.Parse(time.RFC3339, c[3])
		expected, _, err := Occurrences(rule, time.Time{}, through, 1000)
		if err != nil {
			t.Fatal(err)
		}
		input, _ := json.Marshal(map[string]any{"rule": rule, "through": c[3]})
		cmd := exec.Command(helper, "--test-schedule")
		cmd.Stdin = bytes.NewReader(input)
		raw, err := cmd.Output()
		if err != nil {
			t.Fatal(err)
		}
		var deliveries []Delivery
		if err = json.Unmarshal(raw, &deliveries); err != nil {
			t.Fatal(err)
		}
		got := []Occurrence{}
		for _, d := range deliveries {
			got = append(got, d.Occurrence)
		}
		if !reflect.DeepEqual(got, expected) {
			t.Fatalf("calendar mismatch %v: got %+v expected %+v", c, got, expected)
		}
	}
}

func TestReminderHelperRecoverySummaryAndUnknownRetry(t *testing.T) {
	helper := os.Getenv("TROVE_REMINDER_FIXTURE")
	if helper == "" {
		t.Skip("requires compiled Swift reminder helper; delivery fixture never initializes OS notifications")
	}
	raw, err := exec.Command(helper, "--test-delivery").CombinedOutput()
	if err != nil {
		t.Fatalf("%v: %s", err, raw)
	}
	var got struct {
		Submissions int `json:"submissions"`
		Restart     int `json:"afterRestartSubmissions"`
		Retained    int `json:"retainedOccurrences"`
		Unknown     int `json:"unknownOccurrences"`
	}
	if err = json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	if got.Submissions != 1 || got.Restart != 1 || got.Retained != 2193 || got.Unknown != 2193 {
		t.Fatalf("recovery must retain every occurrence and avoid a blind retry: %+v", got)
	}
}
