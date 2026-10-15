package workspace

import (
	"fmt"
	"time"
)

// Scheduling v1 uses whole local days. It deliberately has no holiday or
// business-hour interpretation. Timed durations count elapsed eligible time.
type schedulePolicy struct {
	Version  int    `json:"version"`
	Calendar string `json:"calendar"`
	Timezone string `json:"timezone"`
	Weekdays []int  `json:"weekdays"`
}
type scheduleDate struct {
	Start    string  `json:"start"`
	End      *string `json:"end"`
	Timezone string  `json:"timezone"`
	HasTime  bool    `json:"hasTime"`
}
type scheduleCalendar struct {
	policy   schedulePolicy
	zone     *time.Location
	weekdays [7]bool
}

const scheduleDayLimit = 36600

func newScheduleCalendar(p schedulePolicy) (scheduleCalendar, error) {
	c := scheduleCalendar{policy: p}
	if p.Version != 1 || (p.Calendar != "calendar_days" && p.Calendar != "working_week") {
		return c, Fail("INVALID_REQUEST", "Unsupported scheduling calendar")
	}
	var err error
	c.zone, err = time.LoadLocation(p.Timezone)
	if err != nil || p.Timezone == "" {
		return c, Fail("INVALID_REQUEST", "Choose an explicit valid scheduling timezone")
	}
	for _, day := range p.Weekdays {
		if day < 0 || day > 6 || c.weekdays[day] {
			return c, Fail("INVALID_REQUEST", "Working weekdays must be unique values from Sunday (0) to Saturday (6)")
		}
		c.weekdays[day] = true
	}
	if p.Calendar == "calendar_days" {
		for i := range c.weekdays {
			c.weekdays[i] = true
		}
	} else if len(p.Weekdays) == 0 {
		return c, Fail("INVALID_REQUEST", "Select at least one working weekday")
	}
	return c, nil
}
func (c scheduleCalendar) eligible(t time.Time) bool { return c.weekdays[t.In(c.zone).Weekday()] }

// Local days can start after 00:00 or repeat midnight. Find the first
// instant of the requested civil date, or the next existing date when a
// timezone skipped a whole date. Ordinary dates take the constant-time path.
func (c scheduleCalendar) dayStart(year int, month time.Month, day int) time.Time {
	civil := time.Date(year, month, day, 0, 0, 0, 0, time.UTC)
	target := civil.Format("2006-01-02")
	candidate := time.Date(civil.Year(), civil.Month(), civil.Day(), 0, 0, 0, 0, c.zone)
	if candidate.Format("2006-01-02") == target && candidate.Add(-time.Nanosecond).In(c.zone).Format("2006-01-02") != target {
		return candidate
	}
	noon := time.Date(civil.Year(), civil.Month(), civil.Day(), 12, 0, 0, 0, c.zone)
	lo, hi := noon.Add(-48*time.Hour), noon.Add(48*time.Hour)
	for hi.Sub(lo) > time.Nanosecond {
		mid := lo.Add(hi.Sub(lo) / 2)
		if mid.In(c.zone).Format("2006-01-02") < target {
			lo = mid
		} else {
			hi = mid
		}
	}
	return hi.In(c.zone)
}
func (c scheduleCalendar) midnight(t time.Time) time.Time {
	t = t.In(c.zone)
	return c.dayStart(t.Year(), t.Month(), t.Day())
}
func (c scheduleCalendar) nextDay(t time.Time) time.Time {
	t = t.In(c.zone)
	return c.dayStart(t.Year(), t.Month(), t.Day()+1)
}
func (c scheduleCalendar) nextEligible(t time.Time) (time.Time, error) {
	for i := 0; i < 8; i++ {
		if c.eligible(t) {
			return t, nil
		}
		t = c.nextDay(t)
	}
	return t, Fail("INVALID_REQUEST", "No eligible weekday")
}
func (c scheduleCalendar) parse(d scheduleDate) (time.Time, time.Time, error) {
	var start, end time.Time
	var err error
	if d.HasTime {
		start, err = time.Parse(time.RFC3339Nano, d.Start)
	} else {
		var civil time.Time
		civil, err = time.Parse("2006-01-02", d.Start)
		if err == nil {
			start = c.dayStart(civil.Year(), civil.Month(), civil.Day())
		}
	}
	if err != nil || (!d.HasTime && start.Format("2006-01-02") != d.Start) {
		return start, end, Fail("INVALID_REQUEST", "A scheduled record has an invalid or nonexistent start date in this timezone")
	}
	if !d.HasTime {
		start = c.midnight(start)
	}
	start = start.In(c.zone)
	end = start
	if d.End != nil {
		if d.HasTime {
			end, err = time.Parse(time.RFC3339Nano, *d.End)
		} else {
			var civil time.Time
			civil, err = time.Parse("2006-01-02", *d.End)
			if err == nil {
				end = c.dayStart(civil.Year(), civil.Month(), civil.Day())
			}
		}
		end = end.In(c.zone)
		if err != nil || (!d.HasTime && end.Format("2006-01-02") != *d.End) || end.Before(start) {
			return start, end, Fail("INVALID_REQUEST", "A scheduled record has an invalid or nonexistent date range in this timezone")
		}
		if !d.HasTime {
			end = c.midnight(end)
		}
	}
	if end.Sub(start) > time.Duration(scheduleDayLimit)*24*time.Hour {
		return start, end, Fail("RESOURCE_EXHAUSTED", "Scheduling ranges must be at most 100 years")
	}
	return start, end, nil
}
func (c scheduleCalendar) duration(d scheduleDate) (int64, error) {
	start, end, err := c.parse(d)
	if err != nil {
		return 0, err
	}
	if d.HasTime && c.policy.Calendar == "calendar_days" {
		return int64(end.Sub(start)), nil
	}
	var duration int64
	for day, n := c.midnight(start), 0; !day.After(end); day, n = c.nextDay(day), n+1 {
		if n > scheduleDayLimit {
			return 0, Fail("RESOURCE_EXHAUSTED", "Scheduling range exceeds traversal bounds")
		}
		if !c.eligible(day) {
			continue
		}
		if !d.HasTime {
			duration++
			continue
		}
		lo, hi := day, c.nextDay(day)
		if lo.Before(start) {
			lo = start
		}
		if hi.After(end) {
			hi = end
		}
		if hi.After(lo) {
			duration += int64(hi.Sub(lo))
		}
	}
	if !d.HasTime && duration == 0 {
		return 0, Fail("INVALID_REQUEST", "A date-only range has no eligible days in the selected calendar")
	}
	return duration, nil
}
func (c scheduleCalendar) shifted(d scheduleDate, start time.Time, duration int64) (scheduleDate, error) {
	start = start.In(c.zone)
	if !c.eligible(start) {
		return d, Fail("INVALID_REQUEST", "The requested start is not an eligible weekday")
	}
	end := start
	if !d.HasTime {
		remaining := duration - 1
		for n := 0; remaining > 0; n++ {
			if n > scheduleDayLimit {
				return d, Fail("RESOURCE_EXHAUSTED", "Shifted range exceeds traversal bounds")
			}
			end = c.nextDay(end)
			if c.eligible(end) {
				remaining--
			}
		}
	} else if c.policy.Calendar == "calendar_days" {
		end = start.Add(time.Duration(duration))
	} else {
		remaining := time.Duration(duration)
		for n := 0; remaining > 0; n++ {
			if n > scheduleDayLimit {
				return d, Fail("RESOURCE_EXHAUSTED", "Shifted range exceeds traversal bounds")
			}
			var err error
			end, err = c.nextEligible(end)
			if err != nil {
				return d, err
			}
			available := c.nextDay(end).Sub(end)
			if remaining <= available {
				end = end.Add(remaining)
				remaining = 0
			} else {
				remaining -= available
				end = c.nextDay(end)
			}
		}
	}
	format := "2006-01-02"
	if d.HasTime {
		format = time.RFC3339Nano
	}
	d.Start = start.Format(format)
	d.Timezone = c.policy.Timezone
	if d.End != nil {
		value := end.Format(format)
		d.End = &value
	}
	return d, nil
}
func (c scheduleCalendar) finishConstraint(d scheduleDate) (time.Time, error) {
	_, end, err := c.parse(d)
	if err != nil {
		return end, err
	}
	if !d.HasTime {
		end = c.nextDay(end)
	}
	return c.nextEligible(end)
}
func scheduleDurationLabel(d scheduleDate, duration int64) string {
	if !d.HasTime {
		return fmt.Sprintf("%d inclusive eligible dates", duration)
	}
	return fmt.Sprintf("%s elapsed eligible time", time.Duration(duration))
}
func (c scheduleCalendar) dstNotes(before, after scheduleDate) []string {
	notes := []string{}
	if !before.HasTime {
		return notes
	}
	a, b, e1 := c.parse(before)
	x, y, e2 := c.parse(after)
	if e1 != nil || e2 != nil {
		return notes
	}
	_, ao := a.Zone()
	_, bo := b.Zone()
	_, xo := x.Zone()
	_, yo := y.Zone()
	if ao != bo || xo != yo || ao != xo || bo != yo {
		notes = append(notes, fmt.Sprintf("UTC offsets: before %s → %s; after %s → %s. Duration uses elapsed eligible time, including DST effects.", a.Format("Z07:00"), b.Format("Z07:00"), x.Format("Z07:00"), y.Format("Z07:00")))
	}
	return notes
}
