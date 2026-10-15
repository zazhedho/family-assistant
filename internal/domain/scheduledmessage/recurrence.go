package scheduledmessage

import (
	"errors"
	"fmt"
	"time"
)

var (
	ErrInvalidRecurrence    = errors.New("invalid recurrence")
	ErrInvalidTimezone      = errors.New("invalid timezone")
	ErrAmbiguousLocalTime   = errors.New("ambiguous local time")
	ErrNonexistentLocalTime = errors.New("nonexistent local time")
)

// NextOccurrence returns nil for ONCE and the first future calendar occurrence otherwise.
func NextOccurrence(rule Recurrence, start, after time.Time, timezone string) (*time.Time, error) {
	if timezone == "Local" {
		return nil, fmt.Errorf("%w: %q", ErrInvalidTimezone, timezone)
	}
	loc, err := time.LoadLocation(timezone)
	if err != nil {
		return nil, fmt.Errorf("%w: %q", ErrInvalidTimezone, timezone)
	}
	if rule == RecurrenceOnce {
		return nil, nil
	}
	var periodDays int64
	switch rule {
	case RecurrenceDaily:
		periodDays = 1
	case RecurrenceWeekly:
		periodDays = 7
	default:
		return nil, fmt.Errorf("%w: %q", ErrInvalidRecurrence, rule)
	}

	startLocal := start.In(loc)
	afterLocal := after.In(loc)
	startDate := civilDate(startLocal)
	afterDate := civilDate(afterLocal)
	daysAfterStart := int64(afterDate.Sub(startDate) / (24 * time.Hour))
	step := int64(0)
	if daysAfterStart > 0 {
		step = daysAfterStart / periodDays
		if daysAfterStart%periodDays != 0 {
			step++
		}
	}

	for {
		date := startDate.AddDate(0, 0, int(step*periodDays))
		matches := wallTimeMatches(date, startLocal, loc)
		switch len(matches) {
		case 0:
			if sameCivilDate(date, afterLocal) && wallClockPassed(startLocal, afterLocal) {
				step++
				continue
			}
			return nil, ErrNonexistentLocalTime
		case 1:
			if matches[0].After(after) {
				return &matches[0], nil
			}
		case 2:
			future := false
			for _, match := range matches {
				future = future || match.After(after)
			}
			if future {
				return nil, ErrAmbiguousLocalTime
			}
		default:
			return nil, ErrAmbiguousLocalTime
		}
		step++
	}
}

func civilDate(local time.Time) time.Time {
	year, month, day := local.Date()
	return time.Date(year, month, day, 0, 0, 0, 0, time.UTC)
}

func sameCivilDate(date time.Time, local time.Time) bool {
	year, month, day := local.Date()
	dy, dm, dd := date.Date()
	return year == dy && month == dm && day == dd
}

func wallClockPassed(wall, local time.Time) bool {
	if local.Hour() != wall.Hour() {
		return local.Hour() > wall.Hour()
	}
	if local.Minute() != wall.Minute() {
		return local.Minute() > wall.Minute()
	}
	if local.Second() != wall.Second() {
		return local.Second() > wall.Second()
	}
	return local.Nanosecond() >= wall.Nanosecond()
}

func wallTimeMatches(date time.Time, wall time.Time, loc *time.Location) []time.Time {
	year, month, day := date.Date()
	wallUTC := time.Date(year, month, day, wall.Hour(), wall.Minute(), wall.Second(), wall.Nanosecond(), time.UTC)
	offsets := make(map[int]struct{})
	for delta := -48 * time.Hour; delta <= 48*time.Hour; delta += 30 * time.Minute {
		_, offset := wallUTC.Add(delta).In(loc).Zone()
		offsets[offset] = struct{}{}
	}

	matches := make([]time.Time, 0, 2)
	for offset := range offsets {
		candidate := wallUTC.Add(-time.Duration(offset) * time.Second)
		local := candidate.In(loc)
		ly, lm, ld := local.Date()
		if ly == year && lm == month && ld == day &&
			local.Hour() == wall.Hour() && local.Minute() == wall.Minute() &&
			local.Second() == wall.Second() && local.Nanosecond() == wall.Nanosecond() {
			matches = append(matches, candidate)
		}
	}
	return matches
}
