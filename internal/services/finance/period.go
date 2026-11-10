package servicefinance

import (
	"strings"
	"time"

	domainfinance "family-assistant/internal/domain/finance"
	"family-assistant/internal/dto"
)

const (
	periodToday     = "today"
	periodWeek      = "week"
	periodMonth     = "month"
	periodLastMonth = "last_month"
	periodCustom    = "custom"

	maxCustomDays   = 366
	budgetMonthSpan = 24 // months a budget month may lie from the current WIB month
	monthLayout     = "2006-01"
)

// periodRange is a report period as half-open UTC instants [from, to) plus the
// start of the immediately preceding period, which ends at from.
type periodRange struct {
	name           string
	from, to, prev time.Time
}

// parseBudgetMonth strictly parses "YYYY-MM" as a WIB calendar month and
// rejects months more than budgetMonthSpan months from now's WIB month.
func parseBudgetMonth(raw string, now time.Time) (time.Time, error) {
	if len(raw) != len(monthLayout) {
		return time.Time{}, validationError("month", "must be YYYY-MM")
	}
	month, err := time.ParseInLocation(monthLayout, raw, domainfinance.WIB())
	if err != nil {
		return time.Time{}, validationError("month", "must be YYYY-MM")
	}
	w := now.In(domainfinance.WIB())
	diff := (month.Year()-w.Year())*12 + int(month.Month()) - int(w.Month())
	if diff < -budgetMonthSpan || diff > budgetMonthSpan {
		return time.Time{}, validationError("month", "must be within 24 months of the current month")
	}
	return month, nil
}

// wibDay returns midnight WIB of the date t falls on, shifted by days, in UTC.
func wibDay(t time.Time, days int) time.Time {
	w := t.In(domainfinance.WIB())
	return time.Date(w.Year(), w.Month(), w.Day()+days, 0, 0, 0, 0, domainfinance.WIB()).UTC()
}

// wibMonthStart returns the start of the WIB month containing t, shifted by
// months, in UTC.
func wibMonthStart(t time.Time, months int) time.Time {
	w := t.In(domainfinance.WIB())
	return time.Date(w.Year(), w.Month()+time.Month(months), 1, 0, 0, 0, 0, domainfinance.WIB()).UTC()
}

// resolvePeriod resolves the report period in WIB (never UTC or local time).
func resolvePeriod(input dto.SummaryInput, now time.Time) (periodRange, error) {
	name := strings.ToLower(strings.TrimSpace(input.Period))
	switch name {
	case "", periodMonth:
		from, to := monthRange(now)
		return periodRange{periodMonth, from, to, wibMonthStart(now, -1)}, nil
	case periodLastMonth:
		return periodRange{name, wibMonthStart(now, -1), wibMonthStart(now, 0), wibMonthStart(now, -2)}, nil
	case periodToday:
		return periodRange{name, wibDay(now, 0), wibDay(now, 1), wibDay(now, -1)}, nil
	case periodWeek:
		// ISO week: Monday is day 0.
		offset := (int(now.In(domainfinance.WIB()).Weekday()) + 6) % 7
		return periodRange{name, wibDay(now, -offset), wibDay(now, 7-offset), wibDay(now, -offset-7)}, nil
	case periodCustom:
		return resolveCustom(input.From, input.To)
	}
	return periodRange{}, validationError("period", "must be today, week, month, last_month or custom")
}

func resolveCustom(from, to *time.Time) (periodRange, error) {
	if from == nil || from.IsZero() {
		return periodRange{}, validationError("from", requiredReason)
	}
	if to == nil || to.IsZero() {
		return periodRange{}, validationError("to", requiredReason)
	}
	f, t := from.UTC(), to.UTC()
	if !f.Before(t) {
		return periodRange{}, validationError("to", "must be after from")
	}
	if t.Sub(f) > maxCustomDays*24*time.Hour {
		return periodRange{}, validationError("to", "range is at most 366 days")
	}
	return periodRange{periodCustom, f, t, f.Add(-t.Sub(f))}, nil
}
