// Package reminders contains the derived reminder schedule contract. It has no
// workspace or database dependency; the login item receives only this data.
package reminders

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
)

type Rule struct {
	ID        string `json:"id"`
	Revision  string `json:"revision"`
	PageID    string `json:"pageId"`
	Title     string `json:"title"`
	StartDate string `json:"startDate"`
	LocalTime string `json:"localTime"`
	Timezone  string `json:"timezone"`
	Frequency string `json:"frequency"`
	Enabled   bool   `json:"enabled"`
	Private   bool   `json:"private"`
}
type Occurrence struct {
	ID         string `json:"id"`
	ReminderID string `json:"reminderId"`
	Revision   string `json:"revision"`
	DueAt      string `json:"dueAt"`
}
type Delivery struct {
	Occurrence
	Outcome        string `json:"outcome"`
	Action         string `json:"action,omitempty"`
	ActionRevision int64  `json:"actionRevision,omitempty"`
	SnoozedUntil   string `json:"snoozedUntil,omitempty"`
	Error          string `json:"error,omitempty"`
}
type Schedule struct {
	Version              int        `json:"version"`
	Epoch                string     `json:"epoch"`
	Generation           int64      `json:"generation"`
	WorkspaceID          string     `json:"workspaceId"`
	Active               bool       `json:"active"`
	Rules                []Rule     `json:"rules"`
	Overrides            []Delivery `json:"overrides"`
	AuthorizationRequest string     `json:"authorizationRequest,omitempty"`
}
type Receipt struct {
	Version        int        `json:"version"`
	Epoch          string     `json:"epoch"`
	Generation     int64      `json:"generation"`
	Permission     string     `json:"permission"`
	AcknowledgedAt string     `json:"acknowledgedAt"`
	Deliveries     []Delivery `json:"deliveries"`
	Error          string     `json:"error,omitempty"`
}

func Validate(rule Rule) error {
	if rule.ID == "" || rule.PageID == "" || len(rule.Title) > 512 {
		return fmt.Errorf("invalid reminder source")
	}
	if _, err := strconv.ParseUint(rule.Revision, 10, 64); err != nil {
		return fmt.Errorf("invalid reminder revision")
	}
	d, err := time.Parse("2006-01-02", rule.StartDate)
	if err != nil || d.Year() < 2000 || d.Year() > 2200 {
		return fmt.Errorf("choose a reminder date from 2000 through 2200")
	}
	if _, err = time.Parse("15:04", rule.LocalTime); err != nil {
		return fmt.Errorf("choose a local reminder time")
	}
	if rule.Timezone == "Local" || strings.TrimSpace(rule.Timezone) == "" {
		return fmt.Errorf("choose an explicit timezone")
	}
	if _, err = time.LoadLocation(rule.Timezone); err != nil {
		return fmt.Errorf("unknown reminder timezone")
	}
	if rule.Frequency != "once" && rule.Frequency != "daily" && rule.Frequency != "weekly" {
		return fmt.Errorf("unsupported reminder frequency")
	}
	return nil
}

// LocalInstant chooses the earlier repeated time. A DST gap moves to the first
// valid minute after the requested wall time; an entirely skipped date has no
// occurrence. The explicit timezone never changes with the system timezone.
func LocalInstant(date, clock, zone string) (time.Time, bool) {
	wall, err := time.Parse("2006-01-02T15:04", date+"T"+clock)
	if err != nil {
		return time.Time{}, false
	}
	loc, err := time.LoadLocation(zone)
	if err != nil {
		return time.Time{}, false
	}
	offsets := map[int]bool{}
	for h := -36; h <= 36; h += 12 {
		_, offset := wall.Add(time.Duration(h) * time.Hour).In(loc).Zone()
		offsets[offset] = true
	}
	for minute := 0; minute <= 180; minute++ {
		wanted := wall.Add(time.Duration(minute) * time.Minute)
		if wanted.Format("2006-01-02") != date {
			return time.Time{}, false
		}
		var found []time.Time
		for offset := range offsets {
			candidate := wanted.Add(-time.Duration(offset) * time.Second)
			if candidate.In(loc).Format("2006-01-02T15:04") == wanted.Format("2006-01-02T15:04") {
				found = append(found, candidate)
			}
		}
		if len(found) > 0 {
			sort.Slice(found, func(i, j int) bool { return found[i].Before(found[j]) })
			return found[0].UTC(), true
		}
	}
	return time.Time{}, false
}

// Occurrences is bounded and never advances past an unreturned occurrence.
func Occurrences(rule Rule, after, through time.Time, limit int) ([]Occurrence, bool, error) {
	out := []Occurrence{}
	if err := Validate(rule); err != nil {
		return nil, false, err
	}
	if !rule.Enabled {
		return out, false, nil
	}
	if limit < 1 || limit > 10000 {
		return nil, false, fmt.Errorf("invalid occurrence limit")
	}
	start, _ := time.Parse("2006-01-02", rule.StartDate)
	loc, _ := time.LoadLocation(rule.Timezone)
	date := start
	if rule.Frequency != "once" && !after.IsZero() {
		local := after.In(loc)
		candidate := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, time.UTC).AddDate(0, 0, -1)
		if candidate.After(date) {
			date = candidate
		}
	}
	lastLocal := through.In(loc)
	last := time.Date(lastLocal.Year(), lastLocal.Month(), lastLocal.Day(), 0, 0, 0, 0, time.UTC)
	for !date.After(last) {
		eligible := rule.Frequency != "weekly" || date.Weekday() == start.Weekday()
		if eligible {
			if due, ok := LocalInstant(date.Format("2006-01-02"), rule.LocalTime, rule.Timezone); ok && due.After(after) && !due.After(through) {
				if len(out) == limit {
					return out, true, nil
				}
				out = append(out, Occurrence{ID: rule.ID + ":" + strconv.FormatInt(due.Unix(), 10), ReminderID: rule.ID, Revision: rule.Revision, DueAt: due.Format(time.RFC3339)})
			}
		}
		if rule.Frequency == "once" {
			break
		}
		date = date.AddDate(0, 0, 1)
	}
	return out, false, nil
}
