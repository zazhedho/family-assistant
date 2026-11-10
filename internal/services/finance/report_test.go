package servicefinance

import (
	"context"
	"errors"
	"math"
	"testing"
	"time"

	domainaudit "family-assistant/internal/domain/audit"
	domainfinance "family-assistant/internal/domain/finance"
	"family-assistant/internal/dto"
	"family-assistant/internal/services/authorization"
)

func utc(y int, m time.Month, d, h, min int) time.Time {
	return time.Date(y, m, d, h, min, 0, 0, time.UTC)
}

func TestSummaryPeriodsResolveInWIB(t *testing.T) {
	cases := []struct {
		name, period, wantPeriod string
		now, from, to, prevFrom  time.Time
	}{
		{"today", "today", "today", utc(2026, 11, 15, 10, 0), utc(2026, 11, 14, 17, 0), utc(2026, 11, 15, 17, 0), utc(2026, 11, 13, 17, 0)},
		{"today just before WIB midnight", "today", "today", utc(2026, 11, 15, 16, 59), utc(2026, 11, 14, 17, 0), utc(2026, 11, 15, 17, 0), utc(2026, 11, 13, 17, 0)},
		{"today rolls at 17:00Z", "today", "today", utc(2026, 11, 15, 17, 0), utc(2026, 11, 15, 17, 0), utc(2026, 11, 16, 17, 0), utc(2026, 11, 14, 17, 0)},
		{"week on WIB Sunday night", "week", "week", utc(2026, 11, 15, 16, 30), utc(2026, 11, 8, 17, 0), utc(2026, 11, 15, 17, 0), utc(2026, 11, 1, 17, 0)},
		{"week on WIB Monday just after midnight", "week", "week", utc(2026, 11, 15, 17, 30), utc(2026, 11, 15, 17, 0), utc(2026, 11, 22, 17, 0), utc(2026, 11, 8, 17, 0)},
		{"week on Wednesday", "week", "week", utc(2026, 11, 11, 3, 0), utc(2026, 11, 8, 17, 0), utc(2026, 11, 15, 17, 0), utc(2026, 11, 1, 17, 0)},
		{"month late November", "month", "month", utc(2026, 11, 30, 16, 30), utc(2026, 10, 31, 17, 0), utc(2026, 11, 30, 17, 0), utc(2026, 9, 30, 17, 0)},
		{"month rolls to December", "month", "month", utc(2026, 11, 30, 17, 30), utc(2026, 11, 30, 17, 0), utc(2026, 12, 31, 17, 0), utc(2026, 10, 31, 17, 0)},
		{"empty defaults to month", "", "month", utc(2026, 11, 15, 10, 0), utc(2026, 10, 31, 17, 0), utc(2026, 11, 30, 17, 0), utc(2026, 9, 30, 17, 0)},
		{"mixed case", " Month ", "month", utc(2026, 11, 15, 10, 0), utc(2026, 10, 31, 17, 0), utc(2026, 11, 30, 17, 0), utc(2026, 9, 30, 17, 0)},
		{"last_month", "last_month", "last_month", utc(2026, 11, 15, 10, 0), utc(2026, 9, 30, 17, 0), utc(2026, 10, 31, 17, 0), utc(2026, 8, 31, 17, 0)},
		{"last_month in January", "last_month", "last_month", utc(2026, 1, 10, 10, 0), utc(2025, 11, 30, 17, 0), utc(2025, 12, 31, 17, 0), utc(2025, 10, 31, 17, 0)},
		{"last_month at WIB New Year", "last_month", "last_month", utc(2025, 12, 31, 17, 30), utc(2025, 11, 30, 17, 0), utc(2025, 12, 31, 17, 0), utc(2025, 10, 31, 17, 0)},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			e := newRptEnv(t)
			e.impl.now = func() time.Time { return c.now }
			rep, err := e.svc.Summary(context.Background(), memberActor(), dto.SummaryInput{Period: c.period})
			if err != nil {
				t.Fatal(err)
			}
			if rep.Period != c.wantPeriod || !rep.From.Equal(c.from) || !rep.To.Equal(c.to) || rep.From.Location() != time.UTC || rep.To.Location() != time.UTC {
				t.Fatalf("got %s [%v, %v), want [%v, %v)", rep.Period, rep.From, rep.To, c.from, c.to)
			}
			if len(e.repo.summaryCalls) != 2 || e.repo.summaryCalls[0] != (span{c.from, c.to}) || e.repo.summaryCalls[1] != (span{c.prevFrom, c.from}) {
				t.Fatalf("repo calls = %v, want [%v,%v) then [%v,%v)", e.repo.summaryCalls, c.from, c.to, c.prevFrom, c.from)
			}
		})
	}
}

func TestSummaryCustomPeriod(t *testing.T) {
	from, to := utc(2026, 11, 1, 0, 0), utc(2026, 11, 11, 0, 0)
	e := newRptEnv(t)
	rep, err := e.svc.Summary(context.Background(), memberActor(), dto.SummaryInput{Period: "custom", From: &from, To: &to})
	if err != nil || rep.Period != "custom" || !rep.From.Equal(from) || !rep.To.Equal(to) {
		t.Fatalf("%v %+v", err, rep)
	}
	if want := (span{utc(2026, 10, 22, 0, 0), from}); e.repo.summaryCalls[1] != want {
		t.Fatalf("previous = %v, want %v", e.repo.summaryCalls[1], want)
	}
	maxTo := from.AddDate(0, 0, 366)
	if _, err := e.svc.Summary(context.Background(), memberActor(), dto.SummaryInput{Period: "custom", From: &from, To: &maxTo}); err != nil {
		t.Fatalf("366 days: %v", err)
	}
	// Non-UTC input is normalized to UTC instants.
	wib := time.FixedZone("x", 7*3600)
	f2, t2 := from.In(wib), to.In(wib)
	if rep, err := e.svc.Summary(context.Background(), memberActor(), dto.SummaryInput{Period: "custom", From: &f2, To: &t2}); err != nil || rep.From.Location() != time.UTC {
		t.Fatalf("%v %v", err, rep)
	}
}

func TestSummaryBlankPeriodWithRangeImpliesCustom(t *testing.T) {
	from, to := utc(2026, 11, 1, 0, 0), utc(2026, 11, 11, 0, 0)
	e := newRptEnv(t)
	e.impl.now = func() time.Time { return utc(2026, 11, 15, 10, 0) }
	rep, err := e.svc.Summary(context.Background(), memberActor(), dto.SummaryInput{From: &from, To: &to})
	if err != nil || rep.Period != "custom" || !rep.From.Equal(from) || !rep.To.Equal(to) {
		t.Fatalf("%v %+v", err, rep)
	}
	if e.repo.summaryCalls[0] != (span{from, to}) {
		t.Fatalf("calls = %v", e.repo.summaryCalls)
	}
	// A plain blank period is still the current month.
	e = newRptEnv(t)
	e.impl.now = func() time.Time { return utc(2026, 11, 15, 10, 0) }
	rep, err = e.svc.Summary(context.Background(), memberActor(), dto.SummaryInput{})
	if err != nil || rep.Period != "month" {
		t.Fatalf("%v %+v", err, rep)
	}
}

func TestSummaryRangeWithExplicitPeriodIsRejected(t *testing.T) {
	from, to := utc(2026, 11, 1, 0, 0), utc(2026, 11, 11, 0, 0)
	_, err := newRptEnv(t).svc.Summary(context.Background(), memberActor(), dto.SummaryInput{Period: "month", From: &from, To: &to})
	if r := reasonOf(t, err, "from"); r != "is only allowed with period custom" {
		t.Fatalf("reason = %q", r)
	}
}

func TestSummaryRejectsBadInput(t *testing.T) {
	from, to := utc(2026, 11, 1, 0, 0), utc(2026, 11, 11, 0, 0)
	tooLong := from.AddDate(0, 0, 367)
	zero := time.Time{}
	cases := []struct {
		name  string
		in    dto.SummaryInput
		field string
	}{
		{"unknown period", dto.SummaryInput{Period: "year"}, "period"},
		{"month with from and to", dto.SummaryInput{Period: "month", From: &from, To: &to}, "from"},
		{"today with to only", dto.SummaryInput{Period: "today", To: &to}, "to"},
		{"last_month with from only", dto.SummaryInput{Period: " Last_Month ", From: &from}, "from"},
		{"blank with from only still custom", dto.SummaryInput{From: &from}, "to"},
		{"blank with to only still custom", dto.SummaryInput{To: &to}, "from"},
		{"custom without from", dto.SummaryInput{Period: "custom", To: &to}, "from"},
		{"custom without to", dto.SummaryInput{Period: "custom", From: &from}, "to"},
		{"custom zero from", dto.SummaryInput{Period: "custom", From: &zero, To: &to}, "from"},
		{"custom from equals to", dto.SummaryInput{Period: "custom", From: &from, To: &from}, "to"},
		{"custom from after to", dto.SummaryInput{Period: "custom", From: &to, To: &from}, "to"},
		{"custom over 366 days", dto.SummaryInput{Period: "custom", From: &from, To: &tooLong}, "to"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			e := newRptEnv(t)
			_, err := e.svc.Summary(context.Background(), memberActor(), c.in)
			vErr(t, err, c.field)
			if len(e.repo.summaryCalls) != 0 {
				t.Fatal("repo must not be called")
			}
		})
	}
}

func TestSummaryRequiresList(t *testing.T) {
	e := newRptEnv(t)
	if _, err := e.svc.Summary(context.Background(), actorWith("finance:manage", "finance:create"), dto.SummaryInput{}); !errors.Is(err, authorization.ErrForbidden) {
		t.Fatalf("err = %v", err)
	}
	if _, err := e.svc.Balances(context.Background(), actorWith("finance:manage"), ""); !errors.Is(err, authorization.ErrForbidden) {
		t.Fatalf("balances err = %v", err)
	}
	if len(e.repo.summaryCalls) != 0 {
		t.Fatal("repo must not be called")
	}
}

func TestSummaryNetIsIncomeMinusExpense(t *testing.T) {
	e := newRptEnv(t)
	e.seed(txMine, memberID, domainfinance.Transaction{Type: "income", Amount: 5_000_000, AccountID: acctID, CategoryID: ptr(catSalary), OccurredAt: utc(2026, 11, 10, 3, 0)})
	e.seed(txOther, memberID, domainfinance.Transaction{Type: "expense", Amount: 1_200_000, AccountID: acctID, CategoryID: ptr(catFood), OccurredAt: utc(2026, 11, 11, 3, 0)})
	e.seed(txOldOct, memberID, domainfinance.Transaction{Type: "expense", Amount: 700_000, AccountID: acctID, CategoryID: ptr(catFood), OccurredAt: utc(2026, 10, 11, 3, 0)})
	e.seed(txOctLate, memberID, domainfinance.Transaction{Type: "income", Amount: 300_000, AccountID: acctID, CategoryID: ptr(catSalary), OccurredAt: utc(2026, 10, 12, 3, 0)})
	rep, err := e.svc.Summary(context.Background(), memberActor(), dto.SummaryInput{Period: "month"})
	if err != nil {
		t.Fatal(err)
	}
	if rep.Income != 5_000_000 || rep.Expense != 1_200_000 || rep.Net != 3_800_000 || rep.PreviousIncome != 300_000 || rep.PreviousExpense != 700_000 {
		t.Fatalf("report = %+v", rep)
	}
	if len(rep.ByCategory) != 1 || rep.ByCategory[0].Name != "Makan" || rep.ByCategory[0].Total != 1_200_000 {
		t.Fatalf("by category = %+v", rep.ByCategory)
	}
	if rep.ByAccount == nil || rep.TopMerchants == nil {
		t.Fatal("empty lists must be non-nil so they encode as []")
	}
}

func TestSummaryNetCanBeNegative(t *testing.T) {
	e := newRptEnv(t)
	e.repo.summary = &domainfinance.SummaryResult{Income: 100, Expense: 250}
	rep, err := e.svc.Summary(context.Background(), memberActor(), dto.SummaryInput{})
	if err != nil || rep.Net != -150 {
		t.Fatalf("%v %+v", err, rep)
	}
}

func TestTransfersExcludedFromIncomeAndExpense(t *testing.T) {
	e := newRptEnv(t)
	e.seed(txMine, memberID, domainfinance.Transaction{Type: "income", Amount: 1_000, AccountID: acctID, OccurredAt: utc(2026, 11, 10, 3, 0)})
	e.seed(txOther, memberID, domainfinance.Transaction{Type: "expense", Amount: 300, AccountID: acctID, OccurredAt: utc(2026, 11, 11, 3, 0)})
	e.seed(txOldOct, memberID, domainfinance.Transaction{Type: "transfer", Amount: 500_000, AccountID: acctID, ToAccountID: ptr(acct2), OccurredAt: utc(2026, 11, 12, 3, 0)})
	rep, err := e.svc.Summary(context.Background(), memberActor(), dto.SummaryInput{})
	if err != nil {
		t.Fatal(err)
	}
	if rep.Income != 1_000 || rep.Expense != 300 || rep.Net != 700 {
		t.Fatalf("transfer leaked into the report: %+v", rep)
	}
}

func TestSummaryTransactionInstantsFallInWIBMonth(t *testing.T) {
	e := newRptEnv(t)
	e.seed(txMine, memberID, domainfinance.Transaction{Type: "expense", Amount: 100, AccountID: acctID, OccurredAt: utc(2026, 11, 30, 16, 30)}) // 23:30 WIB Nov 30
	e.seed(txOther, memberID, domainfinance.Transaction{Type: "expense", Amount: 7, AccountID: acctID, OccurredAt: utc(2026, 11, 30, 17, 30)})  // 00:30 WIB Dec 1
	e.seed(txOldOct, memberID, domainfinance.Transaction{Type: "expense", Amount: 3, AccountID: acctID, OccurredAt: utc(2026, 11, 30, 17, 0)})  // exactly Dec 1 00:00 WIB
	for _, c := range []struct {
		now            time.Time
		expense, prior domainfinance.Money
	}{{utc(2026, 11, 30, 16, 30), 100, 0}, {utc(2026, 11, 30, 17, 30), 10, 100}} {
		e.impl.now = func() time.Time { return c.now }
		rep, err := e.svc.Summary(context.Background(), memberActor(), dto.SummaryInput{Period: "month"})
		if err != nil || rep.Expense != c.expense || rep.PreviousExpense != c.prior {
			t.Fatalf("now %v: %v %+v", c.now, err, rep)
		}
	}
}

func TestSummaryRepoFailureFails(t *testing.T) {
	e := newRptEnv(t)
	e.repo.summaryErr = errors.New("db down")
	rep, err := e.svc.Summary(context.Background(), memberActor(), dto.SummaryInput{})
	if err == nil || rep != nil {
		t.Fatalf("%v %v", rep, err)
	}
	if ev := e.lastAudit(t); ev.Status != domainaudit.StatusFailed || ev.ErrorMessage != "internal" {
		t.Fatalf("audit = %+v", ev)
	}
}

func TestSummaryAuditsSuccessWithoutParameters(t *testing.T) {
	e := newRptEnv(t)
	if _, err := e.svc.Summary(context.Background(), memberActor(), dto.SummaryInput{Period: "week"}); err != nil {
		t.Fatal(err)
	}
	ev := e.lastAudit(t)
	if ev.Status != domainaudit.StatusSuccess || ev.Resource != resourceReport || ev.Action != actionList || len(ev.Metadata) != 1 {
		t.Fatalf("audit = %+v", ev)
	}
}

func TestBalancesIncludeOpeningAndTransfers(t *testing.T) {
	e := newRptEnv(t)
	cardID, oldID := "13131313-1313-4313-8313-131313131313", "14141414-1414-4414-8414-141414141414"
	e.repo.accounts[acctID].OpeningBalance = 100_000
	e.repo.accounts[cardID] = &domainfinance.Account{ID: cardID, SpaceID: spaceID, Name: "Kartu", Type: "credit_card", OpeningBalance: -50_000}
	at := time.Now()
	e.repo.accounts[oldID] = &domainfinance.Account{ID: oldID, SpaceID: spaceID, Name: "Lama", Type: "cash", OpeningBalance: 999, ArchivedAt: &at}
	e.seed(txMine, memberID, domainfinance.Transaction{Type: "income", Amount: 1_000_000, AccountID: acctID, OccurredAt: utc(2026, 11, 1, 3, 0)})
	e.seed(txOther, memberID, domainfinance.Transaction{Type: "expense", Amount: 30_000, AccountID: acctID, OccurredAt: utc(2026, 11, 2, 3, 0)})
	e.seed(txOldOct, memberID, domainfinance.Transaction{Type: "transfer", Amount: 200_000, AccountID: acctID, ToAccountID: ptr(acct2), OccurredAt: utc(2026, 11, 3, 3, 0)})
	e.seed(txOctLate, memberID, domainfinance.Transaction{Type: "expense", Amount: 20_000, AccountID: cardID, OccurredAt: utc(2026, 11, 4, 3, 0)})

	rep, err := e.svc.Balances(context.Background(), memberActor(), "")
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]domainfinance.Money{}
	for _, a := range rep.Accounts {
		got[a.Name] = a.Balance
	}
	want := map[string]domainfinance.Money{"Kas": 870_000, "Bank BCA": 200_000, "Kartu": -70_000}
	if len(got) != 3 {
		t.Fatalf("archived account must be hidden: %v", got)
	}
	for name, bal := range want {
		if got[name] != bal {
			t.Fatalf("%s = %d, want %d (all %v)", name, got[name], bal, got)
		}
	}
	if rep.Total != 1_000_000 {
		t.Fatalf("total = %d, want 1000000 (credit card negative included, archived excluded)", rep.Total)
	}
	for i := 1; i < len(rep.Accounts); i++ {
		if rep.Accounts[i-1].Name > rep.Accounts[i].Name {
			t.Fatalf("accounts must be sorted by name: %+v", rep.Accounts)
		}
	}
}

func TestBalancesEmptyAndFailures(t *testing.T) {
	e := newRptEnv(t)
	e.repo.accounts = map[string]*domainfinance.Account{}
	rep, err := e.svc.Balances(context.Background(), memberActor(), "")
	if err != nil || rep.Accounts == nil || len(rep.Accounts) != 0 || rep.Total != 0 {
		t.Fatalf("%v %+v", err, rep)
	}
	e.repo.balanceErr = errors.New("db down")
	if rep, err := e.svc.Balances(context.Background(), memberActor(), ""); err == nil || rep != nil {
		t.Fatalf("%v %v", rep, err)
	}
}

func TestBalancesTotalOverflowIsRejected(t *testing.T) {
	e := newRptEnv(t)
	e.repo.balances = []domainfinance.AccountBalance{
		{AccountID: acctID, Name: "A", Balance: math.MaxInt64},
		{AccountID: acct2, Name: "B", Balance: 1},
	}
	rep, err := e.svc.Balances(context.Background(), memberActor(), "")
	if err == nil || rep != nil || errors.Is(err, authorization.ErrForbidden) {
		t.Fatalf("overflow must be an internal error, got %v %v", rep, err)
	}
	if ev := e.lastAudit(t); ev.ErrorMessage != "internal" {
		t.Fatalf("audit = %+v", ev)
	}
}
