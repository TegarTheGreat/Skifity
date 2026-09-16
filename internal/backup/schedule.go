package backup

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// A small cron parser.
//
// Backup schedules are the only thing in the panel that needs one, and the
// subset used here is the five-field form with numbers, ranges, lists and
// steps. Pulling in a scheduling library for that would be more code to audit
// than this file, and a wrong schedule here means a missing backup, which is
// exactly the kind of thing worth being able to read end to end.

// Schedule is a parsed cron expression.
type Schedule struct {
	Minutes     []int
	Hours       []int
	DaysOfWeek  []int
	DaysOfMonth []int
	Months      []int
	// dayOfMonthRestricted and dayOfWeekRestricted record whether each field
	// was "*", because cron matches either when both are set.
	dayOfMonthRestricted bool
	dayOfWeekRestricted  bool
}

// ParseSchedule reads a five-field cron expression.
func ParseSchedule(expression string) (Schedule, error) {
	fields := strings.Fields(expression)
	if len(fields) != 5 {
		return Schedule{}, fmt.Errorf("a schedule needs five fields (minute hour day month weekday), got %d", len(fields))
	}

	minutes, err := parseField(fields[0], 0, 59)
	if err != nil {
		return Schedule{}, fmt.Errorf("minute: %w", err)
	}
	hours, err := parseField(fields[1], 0, 23)
	if err != nil {
		return Schedule{}, fmt.Errorf("hour: %w", err)
	}
	daysOfMonth, err := parseField(fields[2], 1, 31)
	if err != nil {
		return Schedule{}, fmt.Errorf("day of month: %w", err)
	}
	months, err := parseField(fields[3], 1, 12)
	if err != nil {
		return Schedule{}, fmt.Errorf("month: %w", err)
	}
	daysOfWeek, err := parseField(fields[4], 0, 7)
	if err != nil {
		return Schedule{}, fmt.Errorf("day of week: %w", err)
	}
	// Cron accepts both 0 and 7 for Sunday.
	for i, d := range daysOfWeek {
		if d == 7 {
			daysOfWeek[i] = 0
		}
	}

	return Schedule{
		Minutes: minutes, Hours: hours,
		DaysOfMonth: daysOfMonth, Months: months, DaysOfWeek: daysOfWeek,
		dayOfMonthRestricted: fields[2] != "*",
		dayOfWeekRestricted:  fields[4] != "*",
	}, nil
}

// Matches reports whether a time falls on the schedule, to the minute.
func (s Schedule) Matches(t time.Time) bool {
	if !contains(s.Minutes, t.Minute()) || !contains(s.Hours, t.Hour()) ||
		!contains(s.Months, int(t.Month())) {
		return false
	}

	dayOfMonth := contains(s.DaysOfMonth, t.Day())
	dayOfWeek := contains(s.DaysOfWeek, int(t.Weekday()))

	switch {
	case s.dayOfMonthRestricted && s.dayOfWeekRestricted:
		// Cron's one genuine oddity: with both fields restricted, either
		// matching is enough.
		return dayOfMonth || dayOfWeek
	case s.dayOfMonthRestricted:
		return dayOfMonth
	case s.dayOfWeekRestricted:
		return dayOfWeek
	default:
		return true
	}
}

// Describe renders a schedule in words, for the UI.
func (s Schedule) Describe() string {
	if len(s.Minutes) == 1 && len(s.Hours) == 1 && !s.dayOfMonthRestricted && !s.dayOfWeekRestricted {
		return fmt.Sprintf("every day at %02d:%02d UTC", s.Hours[0], s.Minutes[0])
	}
	if len(s.Minutes) == 1 && len(s.Hours) == 1 && s.dayOfWeekRestricted && len(s.DaysOfWeek) == 1 {
		return fmt.Sprintf("every %s at %02d:%02d UTC",
			time.Weekday(s.DaysOfWeek[0]).String(), s.Hours[0], s.Minutes[0])
	}
	if len(s.Hours) == 24 && len(s.Minutes) == 1 {
		return fmt.Sprintf("every hour at %02d minutes past", s.Minutes[0])
	}
	return "on a custom schedule"
}

// parseField reads one cron field.
func parseField(field string, low, high int) ([]int, error) {
	if field == "*" {
		return rangeOf(low, high, 1), nil
	}

	var out []int
	for _, part := range strings.Split(field, ",") {
		step := 1
		if base, stepText, hasStep := strings.Cut(part, "/"); hasStep {
			parsed, err := strconv.Atoi(stepText)
			if err != nil || parsed < 1 {
				return nil, fmt.Errorf("%q is not a valid step", stepText)
			}
			step = parsed
			part = base
		}

		switch {
		case part == "*":
			out = append(out, rangeOf(low, high, step)...)
		case strings.Contains(part, "-"):
			startText, endText, _ := strings.Cut(part, "-")
			start, err := strconv.Atoi(startText)
			if err != nil {
				return nil, fmt.Errorf("%q is not a number", startText)
			}
			end, err := strconv.Atoi(endText)
			if err != nil {
				return nil, fmt.Errorf("%q is not a number", endText)
			}
			if start < low || end > high || start > end {
				return nil, fmt.Errorf("the range %s is outside %d-%d", part, low, high)
			}
			out = append(out, rangeOf(start, end, step)...)
		default:
			value, err := strconv.Atoi(part)
			if err != nil {
				return nil, fmt.Errorf("%q is not a number", part)
			}
			if value < low || value > high {
				return nil, fmt.Errorf("%d is outside %d-%d", value, low, high)
			}
			out = append(out, value)
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("no values")
	}
	return out, nil
}

func rangeOf(low, high, step int) []int {
	var out []int
	for v := low; v <= high; v += step {
		out = append(out, v)
	}
	return out
}

func contains(values []int, v int) bool {
	for _, item := range values {
		if item == v {
			return true
		}
	}
	return false
}

// dueNow reports whether a schedule fires in the current minute.
//
// The scheduler runs once a minute and compares against the minute, so a backup
// runs exactly once even if the tick is a few seconds late.
func dueNow(expression string, now time.Time) bool {
	schedule, err := ParseSchedule(expression)
	if err != nil {
		return false
	}
	return schedule.Matches(now)
}

// NextRun returns the next time a schedule fires after a given time.
func NextRun(expression string, after time.Time) (time.Time, error) {
	schedule, err := ParseSchedule(expression)
	if err != nil {
		return time.Time{}, err
	}
	// Scanning minute by minute for up to a year is simple and fast enough:
	// this runs when a user opens a settings page, not in a hot loop.
	candidate := after.UTC().Truncate(time.Minute).Add(time.Minute)
	limit := candidate.AddDate(1, 0, 0)
	for candidate.Before(limit) {
		if schedule.Matches(candidate) {
			return candidate, nil
		}
		candidate = candidate.Add(time.Minute)
	}
	return time.Time{}, fmt.Errorf("this schedule never fires")
}
