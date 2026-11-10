package domainfinance

import (
	"errors"
	"sync"
	"time"
)

// MaxAmount is the largest allowed amount in whole rupiah (IDR).
const MaxAmount int64 = 1_000_000_000_000

var ErrInvalidAmount = errors.New("amount must be a whole number between 1 and 1000000000000")

// ValidateAmount checks 1 <= v <= MaxAmount.
func ValidateAmount(v int64) error {
	if v < 1 || v > MaxAmount {
		return ErrInvalidAmount
	}
	return nil
}

var (
	wibOnce sync.Once
	wibLoc  *time.Location
)

// WIB returns the Asia/Jakarta location, falling back to a fixed UTC+7 zone
// when tzdata is unavailable (Jakarta has no DST).
func WIB() *time.Location {
	wibOnce.Do(func() {
		loc, err := time.LoadLocation("Asia/Jakarta")
		if err != nil {
			loc = time.FixedZone("WIB", 7*60*60)
		}
		wibLoc = loc
	})
	return wibLoc
}

// MonthStart returns the start of the WIB calendar month containing t, in UTC.
func MonthStart(t time.Time) time.Time {
	w := t.In(WIB())
	return time.Date(w.Year(), w.Month(), 1, 0, 0, 0, 0, WIB()).UTC()
}

// MonthDate returns the first day of the WIB calendar month containing t as a
// date at 00:00 UTC. This is the value to store in finance_budgets.month.
func MonthDate(t time.Time) time.Time {
	w := t.In(WIB())
	return time.Date(w.Year(), w.Month(), 1, 0, 0, 0, 0, time.UTC)
}

func daysIn(year int, month time.Month) int {
	return time.Date(year, month+1, 0, 0, 0, 0, 0, time.UTC).Day()
}

// NextOccurrence returns the first scheduled time strictly after `after`,
// computed on the WIB calendar and keeping after's WIB clock time. Monthly and
// yearly schedules use DayOfMonth (or StartAt's WIB day) clamped to the length
// of the target month.
func NextOccurrence(r Recurring, after time.Time) time.Time {
	w := after.In(WIB())
	n := r.IntervalN
	if n < 1 {
		n = 1
	}
	switch r.Frequency {
	case FreqDaily:
		return w.AddDate(0, 0, n).UTC()
	case FreqWeekly:
		return w.AddDate(0, 0, 7*n).UTC()
	case FreqMonthly, FreqYearly:
		months := n
		if r.Frequency == FreqYearly {
			months = 12 * n
		}
		day := r.StartAt.In(WIB()).Day()
		if r.StartAt.IsZero() {
			day = w.Day()
		}
		if r.DayOfMonth != nil {
			day = *r.DayOfMonth
		}
		total := w.Year()*12 + int(w.Month()) - 1 + months
		year, month := total/12, time.Month(total%12+1)
		if last := daysIn(year, month); day > last {
			day = last
		}
		return time.Date(year, month, day, w.Hour(), w.Minute(), w.Second(), w.Nanosecond(), WIB()).UTC()
	}
	return w.AddDate(0, 0, n).UTC()
}
