package reminders

import (
	"testing"
	"time"
)

func TestReminderExplicitTimezoneDSTAndOccurrenceIdentity(t *testing.T) {
	for _, c := range [][4]string{{"2026-03-08", "02:30", "America/New_York", "2026-03-08T07:00:00Z"}, {"2026-11-01", "01:30", "America/New_York", "2026-11-01T05:30:00Z"}, {"2026-10-15", "09:00", "Asia/Kolkata", "2026-10-15T03:30:00Z"}} {
		got, ok := LocalInstant(c[0], c[1], c[2])
		if !ok || got.Format(time.RFC3339) != c[3] {
			t.Fatal(c, got, ok)
		}
	}
	r := Rule{ID: "id", Revision: "1", PageID: "page", StartDate: "2026-03-07", LocalTime: "09:00", Timezone: "America/New_York", Frequency: "daily", Enabled: true, Private: true}
	through, _ := time.Parse(time.RFC3339, "2026-03-10T00:00:00Z")
	items, more, err := Occurrences(r, time.Time{}, through, 2)
	if err != nil || !more || len(items) != 2 {
		t.Fatal(items, more, err)
	}
	after, _ := time.Parse(time.RFC3339, items[1].DueAt)
	rest, more, err := Occurrences(r, after, through, 2)
	if err != nil || more || len(rest) != 1 {
		t.Fatal(rest, more, err)
	}
	r.Revision = "2"
	again, _, _ := Occurrences(r, time.Time{}, through, 2)
	if again[0].ID != items[0].ID {
		t.Fatal("an edit duplicated a date's occurrence")
	}
	r.Frequency = "weekly"
	weekly, _, err := Occurrences(r, time.Time{}, through, 2)
	if err != nil || len(weekly) != 1 {
		t.Fatal(weekly, err)
	}
}
func TestReminderMissedOccurrencesPageWithoutLoss(t *testing.T) {
	r := Rule{ID: "id", Revision: "1", PageID: "page", StartDate: "2020-01-01", LocalTime: "09:00", Timezone: "UTC", Frequency: "daily", Enabled: true}
	through := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	after := time.Time{}
	seen := map[string]bool{}
	for {
		items, more, err := Occurrences(r, after, through, 1000)
		if err != nil {
			t.Fatal(err)
		}
		for _, i := range items {
			if seen[i.ID] {
				t.Fatal("duplicate", i)
			}
			seen[i.ID] = true
			after, _ = time.Parse(time.RFC3339, i.DueAt)
		}
		if !more {
			break
		}
	}
	if len(seen) != 2193 {
		t.Fatal(len(seen))
	}
}
