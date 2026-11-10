package domainfinance

import (
	"errors"
	"testing"
	"time"
)

func sp(s string) *string { return &s }

func utc(s string) time.Time {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		panic(err)
	}
	return t.UTC()
}

func TestValidateAmountRejectsZeroNegativeAndOverLimit(t *testing.T) {
	for _, v := range []Money{0, -1, MaxAmount + 1} {
		if err := ValidateAmount(v); !errors.Is(err, ErrInvalidAmount) {
			t.Errorf("ValidateAmount(%d) = %v, want ErrInvalidAmount", v, err)
		}
	}
	for _, v := range []Money{1, MaxAmount} {
		if err := ValidateAmount(v); err != nil {
			t.Errorf("ValidateAmount(%d) = %v, want nil", v, err)
		}
	}
}

func TestValidateTransactionTransferRules(t *testing.T) {
	ok := Transaction{Type: TypeTransfer, Amount: 10, AccountID: "a", ToAccountID: sp("b")}
	if err := ValidateTransaction(ok); err != nil {
		t.Fatalf("valid transfer: %v", err)
	}
	cases := map[string]Transaction{
		"same account":  {Type: TypeTransfer, Amount: 10, AccountID: "a", ToAccountID: sp("a")},
		"missing to":    {Type: TypeTransfer, Amount: 10, AccountID: "a"},
		"category":      {Type: TypeTransfer, Amount: 10, AccountID: "a", ToAccountID: sp("b"), CategoryID: sp("c")},
		"bad type":      {Type: "x", Amount: 10, AccountID: "a"},
		"no account":    {Type: TypeExpense, Amount: 10},
		"to on expense": {Type: TypeExpense, Amount: 10, AccountID: "a", ToAccountID: sp("b")},
		"zero amount":   {Type: TypeExpense, Amount: 0, AccountID: "a"},
		"over limit":    {Type: TypeIncome, Amount: MaxAmount + 1, AccountID: "a"},
	}
	for name, tx := range cases {
		if err := ValidateTransaction(tx); err == nil {
			t.Errorf("%s: expected error", name)
		}
	}
	if err := ValidateTransaction(Transaction{Type: TypeExpense, Amount: 5, AccountID: "a", CategoryID: sp("c")}); err != nil {
		t.Errorf("valid expense: %v", err)
	}
}

func TestMonthStartUsesWIB(t *testing.T) {
	got := MonthStart(utc("2026-11-30T16:30:00Z"))
	if want := utc("2026-10-31T17:00:00Z"); !got.Equal(want) || got.Location() != time.UTC {
		t.Errorf("got %v want %v", got, want)
	}
	got = MonthStart(utc("2026-11-30T17:30:00Z"))
	if want := utc("2026-11-30T17:00:00Z"); !got.Equal(want) {
		t.Errorf("got %v want %v", got, want)
	}
}

func TestNextOccurrenceMonthlyClampsDay31(t *testing.T) {
	start := utc("2027-01-31T03:00:00Z") // 10:00 WIB
	day := 31
	r := Recurring{Frequency: FreqMonthly, IntervalN: 1, DayOfMonth: &day, StartAt: start}
	got := NextOccurrence(r, start)
	if want := utc("2027-02-28T03:00:00Z"); !got.Equal(want) {
		t.Errorf("got %v want %v", got, want)
	}
	// The occurrence after the clamped one returns to the 31st.
	if got2 := NextOccurrence(r, got); !got2.Equal(utc("2027-03-31T03:00:00Z")) {
		t.Errorf("got %v", got2)
	}
}

func TestNextOccurrenceDailyWeeklyYearly(t *testing.T) {
	a := utc("2026-11-10T03:00:00Z")
	if got := NextOccurrence(Recurring{Frequency: FreqDaily, IntervalN: 2}, a); !got.Equal(utc("2026-11-12T03:00:00Z")) {
		t.Errorf("daily %v", got)
	}
	if got := NextOccurrence(Recurring{Frequency: FreqWeekly, IntervalN: 1}, a); !got.Equal(utc("2026-11-17T03:00:00Z")) {
		t.Errorf("weekly %v", got)
	}
	leap := utc("2028-02-29T03:00:00Z")
	r := Recurring{Frequency: FreqYearly, IntervalN: 1, StartAt: leap}
	if got := NextOccurrence(r, leap); !got.Equal(utc("2029-02-28T03:00:00Z")) {
		t.Errorf("yearly %v", got)
	}
}

func TestNewBudgetStatus(t *testing.T) {
	cases := []struct {
		budget, spent, remaining Money
		pct                      int
	}{
		{1000, 0, 1000, 0},
		{1000, 999, 1, 99},
		{1000, 1000, 0, 100},
		{1000, 1500, -500, 150},
		{0, 50, -50, 0},
		{3, 1, 2, 33},
		{MaxAmount, MaxAmount, 0, 100},
	}
	for _, c := range cases {
		s := NewBudgetStatus("cat", c.budget, c.spent)
		if s.Remaining != c.remaining || s.PercentUsed != c.pct || s.CategoryID != "cat" || s.Budget != c.budget || s.Spent != c.spent {
			t.Errorf("%+v -> %+v", c, s)
		}
	}
	// spent*100 must not overflow for spent up to MaxAmount.
	if s := NewBudgetStatus("c", 1, MaxAmount); s.PercentUsed != int(MaxAmount*100) {
		t.Errorf("big pct %d", s.PercentUsed)
	}
}

func TestValidateTransactionNilAndEmptyOptionalIDsAreAbsent(t *testing.T) {
	if err := ValidateTransaction(Transaction{Type: TypeExpense, Amount: 5, AccountID: "a"}); err != nil {
		t.Errorf("nil ids: %v", err)
	}
	if err := ValidateTransaction(Transaction{Type: TypeExpense, Amount: 5, AccountID: "a", ToAccountID: sp(""), CategoryID: sp("")}); err != nil {
		t.Errorf("empty ids: %v", err)
	}
	if err := ValidateTransaction(Transaction{Type: TypeTransfer, Amount: 5, AccountID: "a", ToAccountID: sp("b"), CategoryID: sp("")}); err != nil {
		t.Errorf("transfer with empty category: %v", err)
	}
}

func TestMonthDate(t *testing.T) {
	cases := map[string]string{
		"2026-11-30T16:30:00Z": "2026-11-01T00:00:00Z",
		"2026-11-30T17:30:00Z": "2026-12-01T00:00:00Z",
		"2026-12-31T17:30:00Z": "2027-01-01T00:00:00Z",
	}
	for in, want := range cases {
		if got := MonthDate(utc(in)); !got.Equal(utc(want)) || got.Location() != time.UTC {
			t.Errorf("MonthDate(%s) = %v want %s", in, got, want)
		}
	}
}
