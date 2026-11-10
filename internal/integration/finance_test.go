package integration

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	domainfinance "family-assistant/internal/domain/finance"
	domainidentity "family-assistant/internal/domain/identity"
	"family-assistant/internal/dto"
	interfacefinance "family-assistant/internal/interfaces/finance"
	auditRepo "family-assistant/internal/repositories/audit"
	financeRepo "family-assistant/internal/repositories/finance"
	spaceRepo "family-assistant/internal/repositories/space"
	auditService "family-assistant/internal/services/audit"
	"family-assistant/internal/services/authorization"
	financeService "family-assistant/internal/services/finance"
	"family-assistant/utils"

	"github.com/jackc/pgx/v5/pgconn"
	"gorm.io/gorm"
)

type financeEnv struct {
	db    *gorm.DB
	sqlDB *sql.DB
	svc   interfacefinance.ServiceFinanceInterface
	sched func(now time.Time) *financeService.Scheduler
}

type financeSpaceFixture struct {
	id                              string
	owner, member, viewer, outsider domainidentity.ActorContext
	ownerMemberID, memberMemberID   string
}

// financeSetup applies the real migrations to the test database and builds the
// finance service and scheduler from real repositories. It skips the test when
// the DSN variable is unset.
func financeSetup(t *testing.T) *financeEnv {
	t.Helper()
	rawDSN, dsnSet := os.LookupEnv("FAMILY_ASSISTANT_TEST_DATABASE_URL")
	dsn := strings.TrimSpace(rawDSN)
	if !dsnSet {
		t.Skip("FAMILY_ASSISTANT_TEST_DATABASE_URL is not set")
	}
	if dsn == "" {
		t.Fatal("FAMILY_ASSISTANT_TEST_DATABASE_URL must not be empty when set")
	}
	assertSafeTestDSN(t, dsn)
	db := resetIntegrationDatabase(t, dsn)
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })

	audits := auditService.NewAuditService(auditRepo.NewAuditRepo(db))
	spaces := spaceRepo.NewRepository(db)
	repo := financeRepo.NewRepository(db)
	return &financeEnv{
		db: db, sqlDB: sqlDB,
		svc: financeService.NewService(repo, spaces, authorization.NewAuthorizer(), audits),
		sched: func(now time.Time) *financeService.Scheduler {
			return financeService.NewScheduler(repo, spaces, audits, time.Minute, func() time.Time { return now })
		},
	}
}

func (e *financeEnv) mustExec(t *testing.T, query string, args ...any) {
	t.Helper()
	if err := e.db.Exec(query, args...).Error; err != nil {
		t.Fatalf("exec %q: %v", query, err)
	}
}

func (e *financeEnv) newUser(t *testing.T, label string) string {
	t.Helper()
	id := utils.CreateUUID()
	e.mustExec(t, `INSERT INTO users (id, name, email, password) VALUES (?, ?, ?, '')`, id, label, id+"@finance.example.test")
	return id
}

// join adds userID to the Space with the named role and returns the actor and member id.
func (e *financeEnv) join(t *testing.T, spaceID, userID, role string) (domainidentity.ActorContext, string) {
	t.Helper()
	memberID := utils.CreateUUID()
	e.mustExec(t, `INSERT INTO space_members (id, space_id, user_id, role_id) SELECT ?, ?, ?, id FROM roles WHERE name = ? AND deleted_at IS NULL`, memberID, spaceID, userID, role)
	return actorFor(t, e.db, spaceID, userID, memberID, role), memberID
}

func (e *financeEnv) newSpace(t *testing.T, category string) financeSpaceFixture {
	t.Helper()
	spaceID, ownerUser := utils.CreateUUID(), e.newUser(t, "owner")
	e.mustExec(t, `INSERT INTO spaces (id, name, type, category, created_by_user_id) VALUES (?, ?, 'SHARED', ?, ?)`, spaceID, "Space "+spaceID, category, ownerUser)
	f := financeSpaceFixture{id: spaceID}
	f.owner, f.ownerMemberID = e.join(t, spaceID, ownerUser, "space_owner")
	f.member, f.memberMemberID = e.join(t, spaceID, e.newUser(t, "member"), "space_member")
	f.viewer, _ = e.join(t, spaceID, e.newUser(t, "viewer"), "space_viewer")
	f.outsider = actorFor(t, e.db, spaceID, e.newUser(t, "outsider"), "", "space_member")
	return f
}

// result carries a service call's two return values so must can take them
// together with the *testing.T (Go forbids f(t, g()) for a two-value g).
type result[T any] struct {
	v   T
	err error
}

func got[T any](v T, err error) result[T] { return result[T]{v, err} }

func must[T any](t *testing.T, r result[T]) T {
	t.Helper()
	if r.err != nil {
		t.Fatalf("unexpected error: %v", r.err)
	}
	return r.v
}

func wantInt[T ~int64](t *testing.T, what string, got, want T) {
	t.Helper()
	if got != want {
		t.Fatalf("%s = %d, want %d", what, got, want)
	}
}

// wantErr requires err to match target (a sentinel).
func wantErr(t *testing.T, what string, err, target error) {
	t.Helper()
	if !errors.Is(err, target) {
		t.Fatalf("%s: error = %v, want %v", what, err, target)
	}
}

// wantFieldErr requires a ValidationError on field with a reason containing reason.
func wantFieldErr(t *testing.T, what string, err error, field, reason string) {
	t.Helper()
	var ve *authorization.ValidationError
	if !errors.As(err, &ve) || ve.Field != field || !strings.Contains(ve.Reason, reason) {
		t.Fatalf("%s: error = %v, want validation %s %q", what, err, field, reason)
	}
}

func (e *financeEnv) countRows(t *testing.T, table, spaceID string) int {
	t.Helper()
	var n int
	if err := e.db.Raw(`SELECT COUNT(*) FROM `+table+` WHERE space_id = ?`, spaceID).Row().Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func (e *financeEnv) account(t *testing.T, f financeSpaceFixture, name, kind string, opening domainfinance.Money) string {
	t.Helper()
	acc := must(t, got(e.svc.AccountCreate(context.Background(), f.owner, dto.AccountCreateInput{Space: f.id, Name: name, Type: kind, OpeningBalance: opening})))
	return acc.ID
}

func (e *financeEnv) balance(t *testing.T, f financeSpaceFixture) (map[string]domainfinance.Money, domainfinance.Money) {
	t.Helper()
	rep := must(t, got(e.svc.Balances(context.Background(), f.owner, f.id)))
	byName := map[string]domainfinance.Money{}
	for _, a := range rep.Accounts {
		byName[a.Name] = a.Balance
	}
	return byName, rep.Total
}

// currentMonthOrSkip returns now and the WIB "YYYY-MM"; it skips within ten
// minutes of a WIB month edge so the month-relative assertions cannot flake.
func currentMonthOrSkip(t *testing.T) (time.Time, string) {
	t.Helper()
	now := time.Now()
	w := now.In(domainfinance.WIB())
	start := time.Date(w.Year(), w.Month(), 1, 0, 0, 0, 0, domainfinance.WIB())
	next := start.AddDate(0, 1, 0)
	if now.Sub(start) < 10*time.Minute || next.Sub(now) < 10*time.Minute {
		t.Skip("too close to a WIB month boundary for month-relative assertions")
	}
	return now, w.Format("2006-01")
}

func TestFinanceEndToEnd(t *testing.T) {
	env := financeSetup(t)
	ctx := context.Background()

	t.Run("core flow: accounts, categories, budget, transactions, balances, summary", func(t *testing.T) {
		now, month := currentMonthOrSkip(t)
		f := env.newSpace(t, "finance")
		env.account(t, f, "Kas", "cash", 0)
		env.account(t, f, "BCA", "bank", 1_000_000)

		cats := must(t, got(env.svc.CategoryList(ctx, f.owner, dto.CategoryListInput{Space: f.id})))
		names := map[string]bool{}
		for _, c := range cats {
			names[c.Kind+":"+c.Name] = true
		}
		for _, want := range []string{"expense:Makan", "expense:Transport", "income:Gaji", "income:Bonus", "expense:Lainnya", "income:Lainnya"} {
			if !names[want] {
				t.Fatalf("default category %s missing: %v", want, names)
			}
		}
		must(t, got(env.svc.BudgetSet(ctx, f.owner, dto.BudgetSetInput{Space: f.id, Category: "Makan", Month: month, Amount: 500_000})))

		exp := must(t, got(env.svc.TransactionCreate(ctx, f.owner, dto.TransactionCreateInput{
			Space: f.id, Type: "expense", Amount: 125_000, Account: "Kas", Category: "Makan", Merchant: "Warung Bu Tini", OccurredAt: now,
		})))
		if exp.BudgetStatus == nil {
			t.Fatal("expense returned no budget_status")
		}
		wantInt(t, "budget spent", exp.BudgetStatus.Spent, 125_000)
		wantInt(t, "budget percent", int64(exp.BudgetStatus.PercentUsed), 25)
		wantInt(t, "budget remaining", exp.BudgetStatus.Remaining, 375_000)
		if exp.PossibleDuplicate != nil {
			t.Fatal("first expense flagged as duplicate")
		}
		must(t, got(env.svc.TransactionCreate(ctx, f.owner, dto.TransactionCreateInput{
			Space: f.id, Type: "income", Amount: 8_000_000, Account: "BCA", Category: "Gaji", OccurredAt: now,
		})))
		must(t, got(env.svc.TransactionCreate(ctx, f.owner, dto.TransactionCreateInput{
			Space: f.id, Type: "transfer", Amount: 500_000, Account: "BCA", ToAccount: "Kas", OccurredAt: now,
		})))

		balances, total := env.balance(t, f)
		wantInt(t, "Kas balance", balances["Kas"], 375_000)
		wantInt(t, "BCA balance", balances["BCA"], 8_500_000)
		wantInt(t, "total", total, 8_875_000)

		sum := must(t, got(env.svc.Summary(ctx, f.owner, dto.SummaryInput{Space: f.id, Period: "month"})))
		wantInt(t, "income", sum.Income, 8_000_000)
		wantInt(t, "expense", sum.Expense, 125_000)
		wantInt(t, "net", sum.Net, 7_875_000)
		wantInt(t, "previous income", sum.PreviousIncome, 0)
		wantInt(t, "previous expense", sum.PreviousExpense, 0)
		if len(sum.ByCategory) != 1 || sum.ByCategory[0].Name != "Makan" {
			t.Fatalf("by_category = %+v, want only Makan", sum.ByCategory)
		}
		wantInt(t, "Makan total", sum.ByCategory[0].Total, 125_000)
		if len(sum.TopMerchants) != 1 || sum.TopMerchants[0].Merchant != "Warung Bu Tini" {
			t.Fatalf("top_merchants = %+v, want Warung Bu Tini", sum.TopMerchants)
		}
		wantInt(t, "merchant total", sum.TopMerchants[0].Total, 125_000)
	})

	t.Run("duplicate within two minutes warns and stores both", func(t *testing.T) {
		f := env.newSpace(t, "finance")
		env.account(t, f, "Kas", "cash", 1_000_000)
		in := dto.TransactionCreateInput{Space: f.id, Type: "expense", Amount: 40_000, Account: "Kas", Category: "Makan", Merchant: "Kopi"}
		first := must(t, got(env.svc.TransactionCreate(ctx, f.owner, in)))
		second := must(t, got(env.svc.TransactionCreate(ctx, f.owner, in)))
		if first.PossibleDuplicate != nil {
			t.Fatal("first create flagged as duplicate")
		}
		if second.PossibleDuplicate == nil || second.PossibleDuplicate.ID != first.Transaction.ID {
			t.Fatalf("second possible_duplicate = %+v, want the first transaction", second.PossibleDuplicate)
		}
		if n := env.countRows(t, "finance_transactions", f.id); n != 2 {
			t.Fatalf("stored rows = %d, want 2", n)
		}
		balances, _ := env.balance(t, f)
		wantInt(t, "Kas balance", balances["Kas"], 920_000)
	})

	t.Run("same category name for both kinds resolves by transaction kind", func(t *testing.T) {
		f := env.newSpace(t, "finance")
		env.account(t, f, "Kas", "cash", 0)
		kindOf := map[string]string{}
		for _, c := range must(t, got(env.svc.CategoryList(ctx, f.owner, dto.CategoryListInput{Space: f.id}))) {
			if c.Name == "Lainnya" {
				kindOf[c.ID] = c.Kind
			}
		}
		if len(kindOf) != 2 {
			t.Fatalf("Lainnya categories = %v, want one per kind", kindOf)
		}
		for _, kind := range []string{"income", "expense"} {
			res := must(t, got(env.svc.TransactionCreate(ctx, f.owner, dto.TransactionCreateInput{
				Space: f.id, Type: kind, Amount: 1_000, Account: "Kas", Category: "Lainnya",
			})))
			if resolved := kindOf[*res.Transaction.CategoryID]; resolved != kind {
				t.Fatalf("%s transaction resolved Lainnya of kind %q", kind, resolved)
			}
		}
		_, err := env.svc.TransactionList(ctx, f.owner, dto.TransactionListInput{Space: f.id, Category: "Lainnya"})
		wantFieldErr(t, "list by ambiguous name", err, "category", "ambiguous")
		listed := must(t, got(env.svc.TransactionList(ctx, f.owner, dto.TransactionListInput{Space: f.id, Category: "Lainnya", Type: "expense"})))
		if len(listed) != 1 || listed[0].Type != "expense" {
			t.Fatalf("list Lainnya+expense = %+v, want the one expense", listed)
		}
	})

	t.Run("authorization", func(t *testing.T) { testFinanceAuthorization(t, env) })
	t.Run("database constraints", func(t *testing.T) { testFinanceConstraints(t, env) })
	t.Run("WIB month boundary", func(t *testing.T) { testFinanceWIBBoundary(t, env) })
	t.Run("recurring posting", func(t *testing.T) { testFinanceRecurring(t, env) })
	t.Run("soft delete leaves balances and summaries", func(t *testing.T) { testFinanceSoftDelete(t, env) })
}

func testFinanceAuthorization(t *testing.T, env *financeEnv) {
	ctx := context.Background()
	f := env.newSpace(t, "finance")
	other := env.newSpace(t, "finance")
	family := env.newSpace(t, "family")
	env.account(t, f, "Kas", "cash", 100_000)
	otherAccount := env.account(t, other, "Luar", "cash", 0)

	create := dto.TransactionCreateInput{Space: f.id, Type: "expense", Amount: 5_000, Account: "Kas", Category: "Makan"}
	before := env.countRows(t, "finance_transactions", f.id)

	// A viewer lists but cannot write.
	_, err := env.svc.TransactionCreate(ctx, f.viewer, create)
	wantErr(t, "viewer create", err, authorization.ErrForbidden)
	must(t, got(env.svc.AccountList(ctx, f.viewer, dto.AccountListInput{Space: f.id})))
	must(t, got(env.svc.TransactionList(ctx, f.viewer, dto.TransactionListInput{Space: f.id})))
	if n := env.countRows(t, "finance_transactions", f.id); n != before {
		t.Fatalf("viewer create wrote rows: %d -> %d", before, n)
	}

	// A member creates, lists and edits their own rows only.
	own := must(t, got(env.svc.TransactionCreate(ctx, f.member, create))).Transaction
	ownerTx := must(t, got(env.svc.TransactionCreate(ctx, f.owner, dto.TransactionCreateInput{Space: f.id, Type: "expense", Amount: 7_000, Account: "Kas", Category: "Makan"}))).Transaction
	if rows := must(t, got(env.svc.TransactionList(ctx, f.member, dto.TransactionListInput{Space: f.id}))); len(rows) != 2 {
		t.Fatalf("member list = %d rows, want 2", len(rows))
	}
	note := "edited"
	must(t, got(env.svc.TransactionUpdate(ctx, f.member, f.id, own.ID, dto.TransactionUpdateInput{Note: &note})))
	_, err = env.svc.TransactionUpdate(ctx, f.member, f.id, ownerTx.ID, dto.TransactionUpdateInput{Note: &note})
	wantErr(t, "member updates owner's transaction", err, authorization.ErrForbidden)
	wantErr(t, "member delete", env.svc.TransactionDelete(ctx, f.member, f.id, own.ID), authorization.ErrForbidden)
	if n := env.countRows(t, "finance_transactions", f.id); n != 2 {
		t.Fatalf("rows after forbidden calls = %d, want 2", n)
	}

	// A member cannot manage accounts, budgets or recurring templates.
	_, err = env.svc.AccountCreate(ctx, f.member, dto.AccountCreateInput{Space: f.id, Name: "Baru", Type: "cash"})
	wantErr(t, "member account create", err, authorization.ErrForbidden)
	_, err = env.svc.BudgetSet(ctx, f.member, dto.BudgetSetInput{Space: f.id, Category: "Makan", Month: time.Now().In(domainfinance.WIB()).Format("2006-01"), Amount: 1_000})
	wantErr(t, "member budget set", err, authorization.ErrForbidden)
	_, err = env.svc.RecurringCreate(ctx, f.member, dto.RecurringCreateInput{
		Space: f.id, Type: "expense", Amount: 1_000, Account: "Kas", Category: "Makan", Frequency: "daily", StartAt: time.Now(),
	})
	wantErr(t, "member recurring create", err, authorization.ErrForbidden)
	for table, want := range map[string]int{"finance_budgets": 0, "finance_recurring": 0, "finance_accounts": 1} {
		if n := env.countRows(t, table, f.id); n != want {
			t.Fatalf("%s rows = %d, want %d", table, n, want)
		}
	}

	// Outsiders see the Space as missing: non-members, members of a non-finance
	// Space, owners of another finance Space, and a family Space used as finance.
	for name, actor := range map[string]domainidentity.ActorContext{
		"non-member": f.outsider, "family member": family.owner, "other finance owner": other.owner,
	} {
		_, err := env.svc.TransactionList(ctx, actor, dto.TransactionListInput{Space: f.id})
		wantErr(t, name+" list", err, authorization.ErrNotFound)
		_, err = env.svc.TransactionCreate(ctx, actor, create)
		wantErr(t, name+" create", err, authorization.ErrNotFound)
	}
	_, err = env.svc.AccountList(ctx, family.owner, dto.AccountListInput{Space: family.id})
	wantErr(t, "non-finance Space used as finance", err, authorization.ErrNotFound)

	// An account of another finance Space is rejected and nothing is written.
	cross := create
	cross.Account = otherAccount
	_, err = env.svc.TransactionCreate(ctx, f.owner, cross)
	wantFieldErr(t, "foreign account id", err, "account", "not found")
	if n := env.countRows(t, "finance_transactions", f.id); n != 2 {
		t.Fatalf("rows after foreign account = %d, want 2", n)
	}
	if n := env.countRows(t, "finance_transactions", other.id); n != 0 {
		t.Fatalf("foreign Space got %d rows", n)
	}
}

// pgCode returns the SQLSTATE of err, or "".
func pgCode(err error) string {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code
	}
	return ""
}

func testFinanceConstraints(t *testing.T, env *financeEnv) {
	ctx := context.Background()
	a := env.newSpace(t, "finance")
	b := env.newSpace(t, "finance")
	accA := env.account(t, a, "Kas", "cash", 0)
	accB := env.account(t, b, "Kas", "cash", 0)
	insert := `INSERT INTO finance_transactions (id, space_id, type, amount, account_id, occurred_at, created_by_member_id, recurring_id)
		VALUES ($1, $2, 'expense', 1000, $3, $4, $5, $6)`
	at := time.Date(2026, 11, 1, 3, 0, 0, 0, time.UTC)

	_, err := env.sqlDB.ExecContext(ctx, insert, utils.CreateUUID(), a.id, accB, at, a.ownerMemberID, nil)
	if pgCode(err) != "23503" {
		t.Fatalf("insert with another Space's account: error = %v, want foreign key violation 23503", err)
	}
	if n := env.countRows(t, "finance_transactions", a.id); n != 0 {
		t.Fatalf("rejected insert left %d rows", n)
	}

	rec := must(t, got(env.svc.RecurringCreate(ctx, a.owner, dto.RecurringCreateInput{
		Space: a.id, Type: "expense", Amount: 1_000, Account: "Kas", Category: "Makan", Frequency: "daily", StartAt: time.Now(),
	})))
	if _, err := env.sqlDB.ExecContext(ctx, insert, utils.CreateUUID(), a.id, accA, at, a.ownerMemberID, rec.ID); err != nil {
		t.Fatalf("first occurrence insert: %v", err)
	}
	_, err = env.sqlDB.ExecContext(ctx, insert, utils.CreateUUID(), a.id, accA, at, a.ownerMemberID, rec.ID)
	if pgCode(err) != "23505" {
		t.Fatalf("duplicate (recurring_id, occurred_at): error = %v, want unique violation 23505", err)
	}
}

// testFinanceWIBBoundary pins the service's own month derivation: budgets are
// keyed by the WIB month of the transaction, so a service that used UTC months
// would attach the 23:30 WIB expense to December (or the 00:30 WIB one to
// November) and the exact budget_status values below would differ.
func testFinanceWIBBoundary(t *testing.T, env *financeEnv) {
	ctx := context.Background()
	f := env.newSpace(t, "finance")
	env.account(t, f, "Kas", "cash", 0)
	for _, month := range []string{"2026-11", "2026-12"} {
		must(t, got(env.svc.BudgetSet(ctx, f.owner, dto.BudgetSetInput{Space: f.id, Category: "Makan", Month: month, Amount: 100_000})))
	}
	novLast := time.Date(2026, 11, 30, 16, 30, 0, 0, time.UTC)  // 23:30 WIB on 30 Nov
	decFirst := time.Date(2026, 11, 30, 17, 30, 0, 0, time.UTC) // 00:30 WIB on 1 Dec
	spent := func(month string) domainfinance.Money {
		list := must(t, got(env.svc.BudgetList(ctx, f.owner, f.id, month)))
		if len(list) != 1 {
			t.Fatalf("budgets of %s = %+v, want one", month, list)
		}
		return list[0].Spent
	}
	expense := func(amount domainfinance.Money, at time.Time) *dto.TransactionResult {
		return must(t, got(env.svc.TransactionCreate(ctx, f.owner, dto.TransactionCreateInput{
			Space: f.id, Type: "expense", Amount: amount, Account: "Kas", Category: "Makan", OccurredAt: at,
		})))
	}

	first := expense(1_000, novLast)
	if first.BudgetStatus == nil {
		t.Fatal("23:30 WIB expense returned no budget_status")
	}
	wantInt(t, "November budget spent after the 23:30 WIB expense", first.BudgetStatus.Spent, 1_000)
	wantInt(t, "November budget remaining", first.BudgetStatus.Remaining, 99_000)
	wantInt(t, "December budget spent before any December expense", spent("2026-12"), 0)

	second := expense(2_000, decFirst)
	if second.BudgetStatus == nil {
		t.Fatal("00:30 WIB expense returned no budget_status")
	}
	wantInt(t, "December budget spent by the 00:30 WIB expense alone", second.BudgetStatus.Spent, 2_000)
	wantInt(t, "December budget remaining", second.BudgetStatus.Remaining, 98_000)
	wantInt(t, "November budget spent", spent("2026-11"), 1_000)
	wantInt(t, "December budget spent", spent("2026-12"), 2_000)

	// Report ranges built from the same instants (SQL range filtering).
	novFrom, decFrom := time.Date(2026, 10, 31, 17, 0, 0, 0, time.UTC), time.Date(2026, 11, 30, 17, 0, 0, 0, time.UTC)
	janFrom := time.Date(2026, 12, 31, 17, 0, 0, 0, time.UTC)
	nov := must(t, got(env.svc.Summary(ctx, f.owner, dto.SummaryInput{Space: f.id, Period: "custom", From: &novFrom, To: &decFrom})))
	dec := must(t, got(env.svc.Summary(ctx, f.owner, dto.SummaryInput{Space: f.id, Period: "custom", From: &decFrom, To: &janFrom})))
	wantInt(t, "November expense (WIB range)", nov.Expense, 1_000)
	wantInt(t, "December expense (WIB range)", dec.Expense, 2_000)
}

func testFinanceRecurring(t *testing.T, env *financeEnv) {
	ctx := context.Background()
	f := env.newSpace(t, "finance")
	env.account(t, f, "Kas", "cash", 0)
	env.account(t, f, "Dompet", "ewallet", 0)
	start := time.Now().Add(-23 * time.Hour).Truncate(time.Second) // inside the 24h-in-the-past window
	template := func(account string) dto.RecurringCreateInput {
		return dto.RecurringCreateInput{
			Space: f.id, Type: "expense", Amount: 10_000, Account: account, Category: "Makan", Frequency: "daily", StartAt: start,
		}
	}
	running := must(t, got(env.svc.RecurringCreate(ctx, f.owner, template("Kas"))))
	paused := must(t, got(env.svc.RecurringCreate(ctx, f.owner, template("Kas"))))
	broken := must(t, got(env.svc.RecurringCreate(ctx, f.owner, template("Dompet"))))
	yes := true
	must(t, got(env.svc.RecurringUpdate(ctx, f.owner, dto.RecurringUpdateInput{Space: f.id, ID: paused.ID, Paused: &yes})))
	must(t, got(env.svc.AccountUpdate(ctx, f.owner, dto.AccountUpdateInput{Space: f.id, ID: accountIDByName(t, env, f, "Dompet"), Archived: &yes})))

	// Three occurrences are due: start, start+1d, start+2d.
	now := start.Add(48*time.Hour + time.Hour)
	if err := env.sched(now).Tick(ctx); err != nil {
		t.Fatalf("tick: %v", err)
	}
	postedBy := func(id string) int {
		var n int
		if err := env.db.Raw(`SELECT COUNT(*) FROM finance_transactions WHERE recurring_id = ? AND deleted_at IS NULL`, id).Row().Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	if n := postedBy(running.ID); n != 3 {
		t.Fatalf("running template posted %d, want 3", n)
	}
	if n := postedBy(paused.ID) + postedBy(broken.ID); n != 0 {
		t.Fatalf("paused and broken templates posted %d, want 0", n)
	}
	balances, _ := env.balance(t, f)
	wantInt(t, "Kas balance after 3 postings", balances["Kas"], -30_000)

	byID := map[string]domainfinance.Recurring{}
	for _, r := range must(t, got(env.svc.RecurringList(ctx, f.owner, dto.RecurringListInput{Space: f.id}))) {
		byID[r.ID] = r
	}
	if r := byID[broken.ID]; !r.Paused || r.PauseReason == nil || *r.PauseReason != domainfinance.PauseReferenceInvalid {
		t.Fatalf("archived-account template = paused %v reason %v, want paused reference_invalid", r.Paused, r.PauseReason)
	}
	if r := byID[paused.ID]; !r.Paused || r.PauseReason != nil {
		t.Fatalf("user-paused template changed: paused %v reason %v", r.Paused, r.PauseReason)
	}
	if want := start.Add(72 * time.Hour); !byID[running.ID].NextRunAt.Equal(want) {
		t.Fatalf("running next_run_at = %v, want %v", byID[running.ID].NextRunAt, want)
	}

	// A second tick at the same instant posts nothing.
	if err := env.sched(now).Tick(ctx); err != nil {
		t.Fatalf("second tick: %v", err)
	}
	if n := env.countRows(t, "finance_transactions", f.id); n != 3 {
		t.Fatalf("transactions after second tick = %d, want 3", n)
	}
}

func accountIDByName(t *testing.T, env *financeEnv, f financeSpaceFixture, name string) string {
	t.Helper()
	for _, a := range must(t, got(env.svc.AccountList(context.Background(), f.owner, dto.AccountListInput{Space: f.id}))) {
		if a.Name == name {
			return a.ID
		}
	}
	t.Fatalf("account %s not found", name)
	return ""
}

func testFinanceSoftDelete(t *testing.T, env *financeEnv) {
	ctx := context.Background()
	f := env.newSpace(t, "finance")
	env.account(t, f, "Kas", "cash", 1_000_000)
	from, to := time.Date(2026, 10, 31, 17, 0, 0, 0, time.UTC), time.Date(2026, 11, 30, 17, 0, 0, 0, time.UTC)
	at := time.Date(2026, 11, 15, 5, 0, 0, 0, time.UTC)
	keep := must(t, got(env.svc.TransactionCreate(ctx, f.owner, dto.TransactionCreateInput{Space: f.id, Type: "income", Amount: 50_000, Account: "Kas", Category: "Bonus", OccurredAt: at})))
	gone := must(t, got(env.svc.TransactionCreate(ctx, f.owner, dto.TransactionCreateInput{Space: f.id, Type: "expense", Amount: 100_000, Account: "Kas", Category: "Makan", Merchant: "Sate", OccurredAt: at})))
	summary := func() *dto.SummaryReport {
		return must(t, got(env.svc.Summary(ctx, f.owner, dto.SummaryInput{Space: f.id, Period: "custom", From: &from, To: &to})))
	}
	balances, total := env.balance(t, f)
	wantInt(t, "balance before delete", balances["Kas"], 950_000)
	wantInt(t, "total before delete", total, 950_000)
	wantInt(t, "expense before delete", summary().Expense, 100_000)

	if err := env.svc.TransactionDelete(ctx, f.owner, f.id, gone.Transaction.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	balances, total = env.balance(t, f)
	wantInt(t, "balance after delete", balances["Kas"], 1_050_000)
	wantInt(t, "total after delete", total, 1_050_000)
	after := summary()
	wantInt(t, "expense after delete", after.Expense, 0)
	wantInt(t, "income after delete", after.Income, 50_000)
	if len(after.ByCategory) != 0 || len(after.TopMerchants) != 0 {
		t.Fatalf("deleted row still reported: %+v %+v", after.ByCategory, after.TopMerchants)
	}
	listed := must(t, got(env.svc.TransactionList(ctx, f.owner, dto.TransactionListInput{Space: f.id})))
	if len(listed) != 1 || listed[0].ID != keep.Transaction.ID {
		t.Fatalf("list after delete = %+v, want only the kept income", listed)
	}
	var deleted int
	if err := env.db.Raw(`SELECT COUNT(*) FROM finance_transactions WHERE id = ? AND deleted_at IS NOT NULL`, gone.Transaction.ID).Row().Scan(&deleted); err != nil || deleted != 1 {
		t.Fatalf("soft-deleted row count = %d, %v; want 1 (row kept)", deleted, err)
	}
}
