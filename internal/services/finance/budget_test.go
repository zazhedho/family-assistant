package servicefinance

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	domainaudit "family-assistant/internal/domain/audit"
	domainfinance "family-assistant/internal/domain/finance"
	domainspace "family-assistant/internal/domain/space"
	"family-assistant/internal/dto"
	interfacefinance "family-assistant/internal/interfaces/finance"
	"family-assistant/internal/services/authorization"
	"family-assistant/utils"
	"gorm.io/gorm"
)

var fixedNow = time.Date(2026, 11, 15, 10, 0, 0, 0, time.UTC) // Sunday 17:00 WIB

type span struct{ from, to time.Time }

// rptRepo adds budgets, balances and summaries to txRepo. Balances and Summary
// follow the real repository contract: transfers move money between accounts
// but are never income or expense, and Summary reads the half-open [from, to).
type rptRepo struct {
	*txRepo
	summaryCalls []span
	summaryErr   error
	balanceErr   error
	balances     []domainfinance.AccountBalance // when set, returned as is
	summary      *domainfinance.SummaryResult   // when set, returned as is
	upsertErr    error
}

func (r *rptRepo) UpsertBudget(_ context.Context, b *domainfinance.Budget) error {
	if r.upsertErr != nil {
		return r.upsertErr
	}
	b.Month = domainfinance.MonthDate(b.Month)
	for i := range r.budgets {
		cur := &r.budgets[i]
		if cur.SpaceID == b.SpaceID && cur.CategoryID == b.CategoryID && cur.Month.Equal(b.Month) {
			cur.Amount = b.Amount
			b.ID = cur.ID
			return nil
		}
	}
	r.budgets = append(r.budgets, *b)
	return nil
}

func (r *rptRepo) DeleteBudget(_ context.Context, space, id string, _ time.Time) error {
	for i, b := range r.budgets {
		if b.SpaceID == space && b.ID == id {
			r.budgets = append(r.budgets[:i], r.budgets[i+1:]...)
			return nil
		}
	}
	return gorm.ErrRecordNotFound
}

func (r *rptRepo) Balances(_ context.Context, space string) ([]domainfinance.AccountBalance, error) {
	if r.balanceErr != nil {
		return nil, r.balanceErr
	}
	if r.balances != nil {
		return r.balances, nil
	}
	out := []domainfinance.AccountBalance{}
	for _, a := range r.accounts {
		if a.SpaceID != space {
			continue
		}
		bal := a.OpeningBalance
		for _, tx := range r.txs {
			switch {
			case tx.SpaceID != space:
			case tx.Type == domainfinance.TypeIncome && tx.AccountID == a.ID:
				bal += tx.Amount
			case tx.Type == domainfinance.TypeExpense && tx.AccountID == a.ID:
				bal -= tx.Amount
			case tx.Type == domainfinance.TypeTransfer && tx.AccountID == a.ID:
				bal -= tx.Amount
			case tx.Type == domainfinance.TypeTransfer && tx.ToAccountID != nil && *tx.ToAccountID == a.ID:
				bal += tx.Amount
			}
		}
		out = append(out, domainfinance.AccountBalance{AccountID: a.ID, Name: a.Name, Type: a.Type, Balance: bal})
	}
	return out, nil
}

func (r *rptRepo) Summary(_ context.Context, space string, from, to time.Time) (*domainfinance.SummaryResult, error) {
	r.summaryCalls = append(r.summaryCalls, span{from, to})
	if r.summaryErr != nil {
		return nil, r.summaryErr
	}
	if r.summary != nil {
		return r.summary, nil
	}
	res := &domainfinance.SummaryResult{}
	byCat := map[string]domainfinance.Money{}
	for _, tx := range r.txs {
		if tx.SpaceID != space || tx.OccurredAt.Before(from) || !tx.OccurredAt.Before(to) {
			continue
		}
		switch tx.Type {
		case domainfinance.TypeIncome:
			res.Income += tx.Amount
		case domainfinance.TypeExpense:
			res.Expense += tx.Amount
			if tx.CategoryID != nil {
				byCat[*tx.CategoryID] += tx.Amount
			}
		}
	}
	for id, total := range byCat {
		res.ByCategory = append(res.ByCategory, domainfinance.CategoryTotal{CategoryID: id, Name: r.categories[id].Name, Total: total})
	}
	return res, nil
}

type rptEnv struct {
	repo  *rptRepo
	svc   interfacefinance.ServiceFinanceInterface
	audit *auditFake
	impl  *service
}

func newRptEnvWith(t *testing.T, category string) *rptEnv {
	t.Helper()
	base := newTxEnv(t)
	repo := &rptRepo{txRepo: base.repo}
	audit := &auditFake{}
	impl, ok := NewService(repo, financeSpaces(category), authorization.NewAuthorizer(), audit).(*service)
	if !ok {
		t.Fatal("NewService must return *service")
	}
	impl.now = func() time.Time { return fixedNow }
	return &rptEnv{repo: repo, svc: impl, audit: audit, impl: impl}
}

func newRptEnv(t *testing.T) *rptEnv { return newRptEnvWith(t, domainspace.CategoryFinance) }

func (e *rptEnv) seed(id, member string, tx domainfinance.Transaction) {
	tx.ID, tx.SpaceID, tx.CreatedByMemberID = id, spaceID, member
	tx.CreatedAt = time.Now().UTC()
	e.repo.txs[id] = &tx
}

func (e *rptEnv) lastAudit(t *testing.T) domainaudit.AuditEvent {
	t.Helper()
	if len(e.audit.events) == 0 {
		t.Fatal("no audit event")
	}
	return e.audit.events[len(e.audit.events)-1]
}

func budgetIn(month string, amount domainfinance.Money, category string) dto.BudgetSetInput {
	return dto.BudgetSetInput{Category: category, Month: month, Amount: amount}
}

func TestBudgetSetRequiresManage(t *testing.T) {
	e := newRptEnv(t)
	ctx := context.Background()
	for _, perms := range [][]string{{"finance:list", "finance:create", "finance:update"}, {"finance:list"}, {}} {
		if _, err := e.svc.BudgetSet(ctx, actorWith(perms...), budgetIn("2026-11", 1000, "Makan")); !errors.Is(err, authorization.ErrForbidden) {
			t.Fatalf("set with %v err = %v, want ErrForbidden", perms, err)
		}
		if err := e.svc.BudgetDelete(ctx, actorWith(perms...), dto.BudgetDeleteInput{Category: "Makan", Month: "2026-11"}); !errors.Is(err, authorization.ErrForbidden) {
			t.Fatalf("delete with %v err = %v, want ErrForbidden", perms, err)
		}
	}
	if len(e.repo.budgets) != 0 {
		t.Fatal("nothing may be written")
	}
}

func TestBudgetGuardsSpaceAndMembership(t *testing.T) {
	e := newRptEnvWith(t, domainspace.CategoryFamily)
	ctx := context.Background()
	if _, err := e.svc.BudgetSet(ctx, ownerActor(), budgetIn("2026-11", 1000, "Makan")); !errors.Is(err, authorization.ErrNotFound) {
		t.Fatalf("set err = %v, want ErrNotFound", err)
	}
	if err := e.svc.BudgetDelete(ctx, ownerActor(), dto.BudgetDeleteInput{Category: "Makan", Month: "2026-11"}); !errors.Is(err, authorization.ErrNotFound) {
		t.Fatalf("delete err = %v", err)
	}
	if _, err := e.svc.BudgetList(ctx, ownerActor(), "", ""); !errors.Is(err, authorization.ErrNotFound) {
		t.Fatalf("list err = %v", err)
	}
	if _, err := e.svc.Balances(ctx, ownerActor(), ""); !errors.Is(err, authorization.ErrNotFound) {
		t.Fatalf("balances err = %v", err)
	}
	if _, err := e.svc.Summary(ctx, ownerActor(), dto.SummaryInput{}); !errors.Is(err, authorization.ErrNotFound) {
		t.Fatalf("summary err = %v", err)
	}
	if len(e.repo.budgets) != 0 || len(e.repo.summaryCalls) != 0 {
		t.Fatal("nothing may be read or written")
	}
}

func TestBudgetMonthMustBeValid(t *testing.T) {
	bad := []string{"", " ", "2026-13", "2026-00", "2026-1", "2026-11-01", "2026/11", "nov 2026", "26-11", "2026-11 ", "+2026-11", "2020-01", "2030-01", "2026-11\n"}
	for _, month := range bad {
		t.Run(fmt.Sprintf("%q", month), func(t *testing.T) {
			e := newRptEnv(t)
			_, err := e.svc.BudgetSet(context.Background(), ownerActor(), budgetIn(month, 1000, "Makan"))
			vErr(t, err, "month")
			vErr(t, e.svc.BudgetDelete(context.Background(), ownerActor(), dto.BudgetDeleteInput{Category: "Makan", Month: month}), "month")
			if len(e.repo.budgets) != 0 {
				t.Fatal("nothing may be written")
			}
		})
	}
}

func TestBudgetMonthWindowIs24Months(t *testing.T) {
	cases := []struct {
		month string
		ok    bool
	}{{"2024-11", true}, {"2024-10", false}, {"2028-11", true}, {"2028-12", false}, {"2026-11", true}}
	for _, c := range cases {
		e := newRptEnv(t)
		_, err := e.svc.BudgetSet(context.Background(), ownerActor(), budgetIn(c.month, 1000, "Makan"))
		if c.ok && err != nil {
			t.Fatalf("%s: %v", c.month, err)
		}
		if !c.ok {
			vErr(t, err, "month")
		}
	}
}

func TestBudgetMonthWindowUsesWIBNow(t *testing.T) {
	e := newRptEnv(t)
	// 2026-11-30T17:30Z is already December in WIB, so December is "current".
	e.impl.now = func() time.Time { return time.Date(2026, 11, 30, 17, 30, 0, 0, time.UTC) }
	if _, err := e.svc.BudgetSet(context.Background(), ownerActor(), budgetIn("2028-12", 1000, "Makan")); err != nil {
		t.Fatalf("2028-12 is 24 months after WIB December: %v", err)
	}
	_, err := e.svc.BudgetSet(context.Background(), ownerActor(), budgetIn("2029-01", 1000, "Makan"))
	vErr(t, err, "month")
}

func TestBudgetAmountMustBeValid(t *testing.T) {
	for _, amount := range []domainfinance.Money{0, -1, domainfinance.MaxAmount + 1} {
		e := newRptEnv(t)
		_, err := e.svc.BudgetSet(context.Background(), ownerActor(), budgetIn("2026-11", amount, "Makan"))
		vErr(t, err, "amount")
		if len(e.repo.budgets) != 0 {
			t.Fatal("nothing may be written")
		}
	}
	e := newRptEnv(t)
	if _, err := e.svc.BudgetSet(context.Background(), ownerActor(), budgetIn("2026-11", domainfinance.MaxAmount, "Makan")); err != nil {
		t.Fatalf("max amount: %v", err)
	}
}

func TestBudgetCategoryRules(t *testing.T) {
	archived := "99999999-0000-4000-8000-000000000001"
	cases := []struct {
		name, category, reason string
	}{
		{"income by name", "Gaji", "kind does not match type"},
		{"income by id", catSalary, "kind does not match type"},
		{"archived", archived, reasonArchived},
		{"foreign id", foreignCat, reasonNotFound},
		{"unknown name", "Nope", reasonNotFound},
		{"blank", "  ", requiredReason},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			e := newRptEnv(t)
			at := time.Now()
			e.repo.categories[archived] = &domainfinance.Category{ID: archived, SpaceID: spaceID, Name: "Lama", Kind: "expense", ArchivedAt: &at}
			_, err := e.svc.BudgetSet(context.Background(), ownerActor(), budgetIn("2026-11", 1000, c.category))
			if got := reasonOf(t, err, "category"); got != c.reason {
				t.Fatalf("reason = %q, want %q", got, c.reason)
			}
			if len(e.repo.budgets) != 0 {
				t.Fatal("nothing may be written")
			}
		})
	}
}

func TestBudgetSetUpsertsByCategoryAndMonth(t *testing.T) {
	e := newRptEnv(t)
	ctx := context.Background()
	first, err := e.svc.BudgetSet(ctx, ownerActor(), budgetIn("2026-11", 1_000_000, " makan "))
	if err != nil {
		t.Fatal(err)
	}
	if first.CategoryID != catFood || first.SpaceID != spaceID || first.Amount != 1_000_000 || !first.Month.Equal(time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("budget = %+v", first)
	}
	if _, err := utils.ParseUUID(first.ID, "id", true); err != nil {
		t.Fatalf("id = %q", first.ID)
	}
	second, err := e.svc.BudgetSet(ctx, ownerActor(), budgetIn("2026-11", 2_000_000, catFood))
	if err != nil || len(e.repo.budgets) != 1 || e.repo.budgets[0].Amount != 2_000_000 || second.ID != first.ID {
		t.Fatalf("upsert: %v %+v", err, e.repo.budgets)
	}
	if _, err := e.svc.BudgetSet(ctx, ownerActor(), budgetIn("2026-12", 5, catFood)); err != nil || len(e.repo.budgets) != 2 {
		t.Fatalf("other month must be separate: %v", err)
	}
}

func TestBudgetDelete(t *testing.T) {
	e := newRptEnv(t)
	ctx := context.Background()
	nov, err := e.svc.BudgetSet(ctx, ownerActor(), budgetIn("2026-11", 100, catFood))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.BudgetSet(ctx, ownerActor(), budgetIn("2026-12", 200, catFood)); err != nil {
		t.Fatal(err)
	}
	if err := e.svc.BudgetDelete(ctx, ownerActor(), dto.BudgetDeleteInput{Category: "Makan", Month: "2026-10"}); !errors.Is(err, authorization.ErrNotFound) {
		t.Fatalf("missing budget err = %v, want ErrNotFound", err)
	}
	if err := e.svc.BudgetDelete(ctx, ownerActor(), dto.BudgetDeleteInput{Category: foreignCat, Month: "2026-11"}); err == nil {
		t.Fatal("foreign category must fail")
	}
	if err := e.svc.BudgetDelete(ctx, ownerActor(), dto.BudgetDeleteInput{Category: "Makan", Month: "2026-11"}); err != nil {
		t.Fatal(err)
	}
	if len(e.repo.budgets) != 1 || e.repo.budgets[0].Month.Month() != 12 {
		t.Fatalf("budgets = %+v", e.repo.budgets)
	}
	if ev := e.lastAudit(t); ev.Action != domainaudit.ActionDelete || ev.Status != domainaudit.StatusSuccess || ev.ResourceID != nov.ID {
		t.Fatalf("audit = %+v", ev)
	}
}

func TestBudgetDeleteAllowsArchivedCategory(t *testing.T) {
	e := newRptEnv(t)
	if _, err := e.svc.BudgetSet(context.Background(), ownerActor(), budgetIn("2026-11", 100, catFood)); err != nil {
		t.Fatal(err)
	}
	at := time.Now()
	e.repo.categories[catFood].ArchivedAt = &at
	if err := e.svc.BudgetDelete(context.Background(), ownerActor(), dto.BudgetDeleteInput{Category: catFood, Month: "2026-11"}); err != nil || len(e.repo.budgets) != 0 {
		t.Fatalf("err = %v budgets = %d", err, len(e.repo.budgets))
	}
}

func TestBudgetListComputesSpentInWIBMonth(t *testing.T) {
	e := newRptEnv(t)
	ctx := context.Background()
	e.repo.categories[catOther] = &domainfinance.Category{ID: catOther, SpaceID: spaceID, Name: "Anak", Kind: "expense"}
	e.repo.budgets = []domainfinance.Budget{budgetFor(catFood, fixedNow, 1_000_000), budgetFor(catOther, fixedNow, 400_000),
		budgetFor(catFood, time.Date(2026, 12, 15, 0, 0, 0, 0, time.UTC), 7)}
	e.seed(txMine, memberID, domainfinance.Transaction{Type: "expense", Amount: 250_000, AccountID: acctID, CategoryID: ptr(catFood),
		OccurredAt: time.Date(2026, 11, 30, 16, 30, 0, 0, time.UTC)}) // 23:30 WIB Nov 30: in November
	e.seed(txOther, memberID, domainfinance.Transaction{Type: "expense", Amount: 999, AccountID: acctID, CategoryID: ptr(catFood),
		OccurredAt: time.Date(2026, 11, 30, 17, 30, 0, 0, time.UTC)}) // 00:30 WIB Dec 1: December
	e.seed(txOldOct, memberID, domainfinance.Transaction{Type: "expense", Amount: 5, AccountID: acctID, CategoryID: ptr(catFood),
		OccurredAt: time.Date(2026, 10, 31, 16, 59, 0, 0, time.UTC)}) // 23:59 WIB Oct 31: October

	for _, month := range []string{"", "2026-11"} {
		got, err := e.svc.BudgetList(ctx, memberActor(), "", month)
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != 2 || got[0].CategoryName != "Anak" || got[1].CategoryName != "Makan" {
			t.Fatalf("month %q: %+v", month, got)
		}
		if got[0].Spent != 0 || got[0].Remaining != 400_000 || got[0].PercentUsed != 0 {
			t.Fatalf("anak = %+v", got[0])
		}
		want := domainfinance.NewBudgetStatus(catFood, 1_000_000, 250_000)
		if got[1].BudgetStatus != want || got[1].CategoryID != catFood {
			t.Fatalf("makan = %+v want %+v", got[1], want)
		}
	}
	dec, err := e.svc.BudgetList(ctx, memberActor(), "", "2026-12")
	if err != nil || len(dec) != 1 || dec[0].Spent != 999 || dec[0].Budget != 7 || dec[0].Remaining != -992 {
		t.Fatalf("december: %v %+v", err, dec)
	}
}

func TestBudgetListValidatesAndFailsLoudly(t *testing.T) {
	e := newRptEnv(t)
	ctx := context.Background()
	_, err := e.svc.BudgetList(ctx, memberActor(), "", "2026-13")
	vErr(t, err, "month")
	if _, err := e.svc.BudgetList(ctx, actorWith("finance:manage"), "", ""); !errors.Is(err, authorization.ErrForbidden) {
		t.Fatalf("err = %v, want ErrForbidden", err)
	}
	e.repo.budgets = []domainfinance.Budget{budgetFor(catFood, fixedNow, 100)}
	e.repo.spentErr = errors.New("db down")
	if got, err := e.svc.BudgetList(ctx, memberActor(), "", ""); err == nil || got != nil {
		t.Fatalf("a spent failure must fail the list, got %v %v", got, err)
	}
}

func TestBudgetAuditCoversSuccessAndFailureWithIDsOnly(t *testing.T) {
	e := newRptEnv(t)
	ctx := context.Background()
	if _, err := e.svc.BudgetSet(ctx, ownerActor(), budgetIn("2026-11", 100, "Makan")); err != nil {
		t.Fatal(err)
	}
	ok := e.lastAudit(t)
	if ok.Status != domainaudit.StatusSuccess || ok.Resource != resourceBudget || ok.ResourceID != e.repo.budgets[0].ID {
		t.Fatalf("success audit = %+v", ok)
	}
	if _, err := e.svc.BudgetSet(ctx, ownerActor(), budgetIn("not-a-month", 100, "Makan")); err == nil {
		t.Fatal("want error")
	}
	bad := e.lastAudit(t)
	if bad.Status != domainaudit.StatusFailed || bad.ErrorMessage != "validation" || bad.ResourceID != "" || len(bad.Metadata) != 1 || bad.Metadata["space_id"] != spaceID {
		t.Fatalf("failure audit = %+v", bad)
	}
	if _, err := e.svc.BudgetList(ctx, actorWith(), "", ""); err == nil {
		t.Fatal("want forbidden")
	}
	if ev := e.lastAudit(t); ev.Status != domainaudit.StatusFailed || ev.ErrorMessage != "forbidden" || ev.Action != actionList {
		t.Fatalf("list audit = %+v", ev)
	}
}
