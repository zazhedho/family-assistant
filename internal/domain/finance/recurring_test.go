package domainfinance

import (
	"errors"
	"testing"
	"time"
)

func ip(v int) *int { return &v }

func validRecurring() Recurring {
	start := utc("2026-11-10T03:00:00Z")
	return Recurring{
		ID: "r1", SpaceID: "s1", Type: TypeExpense, Amount: 100000, AccountID: "a", CategoryID: sp("c"),
		Frequency: FreqMonthly, IntervalN: 1, DayOfMonth: ip(10), StartAt: start, NextRunAt: start, CreatedByMemberID: "m1",
	}
}

func TestValidateRecurringAcceptsValidSchedules(t *testing.T) {
	if err := ValidateRecurring(validRecurring()); err != nil {
		t.Fatalf("monthly: %v", err)
	}
	daily := validRecurring()
	daily.Frequency, daily.DayOfMonth, daily.IntervalN = FreqDaily, nil, 365
	if err := ValidateRecurring(daily); err != nil {
		t.Fatalf("daily: %v", err)
	}
	weekly := daily
	weekly.Frequency = FreqWeekly
	end := weekly.StartAt
	weekly.EndAt = &end // end == start is allowed
	if err := ValidateRecurring(weekly); err != nil {
		t.Fatalf("weekly: %v", err)
	}
	transfer := validRecurring()
	transfer.Type, transfer.CategoryID, transfer.ToAccountID = TypeTransfer, nil, sp("b")
	if err := ValidateRecurring(transfer); err != nil {
		t.Fatalf("transfer: %v", err)
	}
}

func TestValidateRecurringRejects(t *testing.T) {
	before := utc("2026-11-09T03:00:00Z")
	cases := map[string]struct {
		mut  func(r *Recurring)
		want error
	}{
		"unknown frequency":   {func(r *Recurring) { r.Frequency = "hourly" }, ErrInvalidFrequency},
		"yearly not offered":  {func(r *Recurring) { r.Frequency, r.DayOfMonth = FreqYearly, nil }, ErrInvalidFrequency},
		"interval zero":       {func(r *Recurring) { r.IntervalN = 0 }, ErrIntervalRange},
		"monthly interval 13": {func(r *Recurring) { r.IntervalN = 13 }, ErrIntervalRange},
		"daily interval 366": {func(r *Recurring) {
			r.Frequency, r.DayOfMonth, r.IntervalN = FreqDaily, nil, 366
		}, ErrIntervalRange},
		"day 0":                 {func(r *Recurring) { r.DayOfMonth = ip(0) }, ErrDayOfMonthRange},
		"day 32":                {func(r *Recurring) { r.DayOfMonth = ip(32) }, ErrDayOfMonthRange},
		"day on weekly":         {func(r *Recurring) { r.Frequency = FreqWeekly }, ErrDayOfMonthNotMonthly},
		"no start":              {func(r *Recurring) { r.StartAt = time.Time{} }, ErrStartRequired},
		"end before start":      {func(r *Recurring) { r.EndAt = &before }, ErrEndBeforeStart},
		"zero amount":           {func(r *Recurring) { r.Amount = 0 }, ErrInvalidAmount},
		"over limit":            {func(r *Recurring) { r.Amount = MaxAmount + 1 }, ErrInvalidAmount},
		"bad type":              {func(r *Recurring) { r.Type = "gift" }, ErrInvalidType},
		"no account":            {func(r *Recurring) { r.AccountID = "" }, ErrAccountRequired},
		"to on expense":         {func(r *Recurring) { r.ToAccountID = sp("b") }, ErrToAccountForbidden},
		"transfer missing to":   {func(r *Recurring) { r.Type, r.CategoryID = TypeTransfer, nil }, ErrToAccountRequired},
		"transfer same account": {func(r *Recurring) { r.Type, r.CategoryID, r.ToAccountID = TypeTransfer, nil, sp("a") }, ErrTransferSameAcct},
		"transfer category":     {func(r *Recurring) { r.Type, r.ToAccountID = TypeTransfer, sp("b") }, ErrTransferCategory},
	}
	for name, c := range cases {
		r := validRecurring()
		c.mut(&r)
		if err := ValidateRecurring(r); !errors.Is(err, c.want) {
			t.Errorf("%s: err = %v, want %v", name, err, c.want)
		}
	}
}

func TestTransactionAtCopiesTemplate(t *testing.T) {
	r := validRecurring()
	r.Merchant, r.Note = "PLN", "listrik"
	occ := utc("2026-12-10T03:00:00Z")
	now := utc("2026-12-10T03:00:05Z")
	tx := r.TransactionAt(occ, "tx1", now)
	if tx.ID != "tx1" || tx.SpaceID != "s1" || tx.Type != TypeExpense || tx.Amount != 100000 || tx.AccountID != "a" ||
		tx.CategoryID == nil || *tx.CategoryID != "c" || tx.ToAccountID != nil || tx.Merchant != "PLN" || tx.Note != "listrik" ||
		!tx.OccurredAt.Equal(occ) || tx.CreatedByMemberID != "m1" || !tx.CreatedAt.Equal(now) || !tx.UpdatedAt.Equal(now) {
		t.Fatalf("tx = %+v", tx)
	}
	// The pointers are copies: changing the transaction never changes the template.
	*tx.CategoryID = "x"
	if *r.CategoryID != "c" {
		t.Fatal("template category aliased")
	}
}

func TestCheckRecurringReferences(t *testing.T) {
	archived := utc("2026-11-01T00:00:00Z")
	live := []Account{{ID: "a"}, {ID: "b"}}
	food := &Category{ID: "c", Kind: TypeExpense}
	r := validRecurring()
	if err := CheckRecurringReferences(r, live, food); err != nil {
		t.Fatalf("valid: %v", err)
	}
	transfer := r
	transfer.Type, transfer.CategoryID, transfer.ToAccountID = TypeTransfer, nil, sp("b")
	if err := CheckRecurringReferences(transfer, live, nil); err != nil {
		t.Fatalf("transfer: %v", err)
	}
	cases := map[string]error{
		"missing account":     CheckRecurringReferences(r, []Account{{ID: "b"}}, food),
		"archived account":    CheckRecurringReferences(r, []Account{{ID: "a", ArchivedAt: &archived}}, food),
		"missing to account":  CheckRecurringReferences(transfer, []Account{{ID: "a"}}, nil),
		"archived to account": CheckRecurringReferences(transfer, []Account{{ID: "a"}, {ID: "b", ArchivedAt: &archived}}, nil),
		"missing category":    CheckRecurringReferences(r, live, nil),
		"archived category":   CheckRecurringReferences(r, live, &Category{ID: "c", Kind: TypeExpense, ArchivedAt: &archived}),
		"other category":      CheckRecurringReferences(r, live, &Category{ID: "z", Kind: TypeExpense}),
		"kind mismatch":       CheckRecurringReferences(r, live, &Category{ID: "c", Kind: TypeIncome}),
	}
	for name, err := range cases {
		var pe *PauseError
		if !errors.As(err, &pe) || pe.Reason != PauseReferenceInvalid {
			t.Errorf("%s: err = %v, want pause %s", name, err, PauseReferenceInvalid)
		}
	}
}

func TestPauseErrorCarriesReason(t *testing.T) {
	err := Pause(PauseMemberInvalid)
	var pe *PauseError
	if !errors.As(err, &pe) || pe.Reason != "member_invalid" || err.Error() == "" {
		t.Fatalf("err = %v", err)
	}
}

// NextOccurrence only chains occurrences: it does not apply EndAt and does not
// validate Frequency (an unknown one advances like daily).
func TestNextOccurrenceIgnoresEndAtAndUnknownFrequencyFallsBackToDaily(t *testing.T) {
	a := utc("2026-11-10T03:00:00Z")
	end := a
	r := Recurring{Frequency: FreqDaily, IntervalN: 1, StartAt: a, EndAt: &end}
	if got := NextOccurrence(r, a); !got.Equal(utc("2026-11-11T03:00:00Z")) {
		t.Errorf("end_at applied: %v", got)
	}
	if got := NextOccurrence(Recurring{Frequency: "hourly", IntervalN: 3}, a); !got.Equal(utc("2026-11-13T03:00:00Z")) {
		t.Errorf("unknown frequency: %v", got)
	}
	// Monthly without DayOfMonth uses StartAt's WIB day: 2026-10-31 17:30 UTC is 1 Nov WIB.
	start := utc("2026-10-31T17:30:00Z")
	m := Recurring{Frequency: FreqMonthly, IntervalN: 1, StartAt: start}
	if got := NextOccurrence(m, start); !got.Equal(utc("2026-11-30T17:30:00Z")) {
		t.Errorf("monthly WIB day: %v", got)
	}
}
