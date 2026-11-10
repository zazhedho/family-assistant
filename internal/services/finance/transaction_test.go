package servicefinance

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	domainaudit "family-assistant/internal/domain/audit"
	domainfinance "family-assistant/internal/domain/finance"
	domainidentity "family-assistant/internal/domain/identity"
	domainspace "family-assistant/internal/domain/space"
	"family-assistant/internal/dto"
	interfacefinance "family-assistant/internal/interfaces/finance"
	"family-assistant/internal/services/authorization"
	"gorm.io/gorm"
)

const (
	acct2       = "55555555-5555-4555-8555-555555555555"
	catFood     = "66666666-6666-4666-8666-666666666666"
	catSalary   = "77777777-7777-4777-8777-777777777777"
	foreignAcct = "88888888-8888-4888-8888-888888888888"
	foreignCat  = "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
	otherMember = "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb"
	txOther     = "cccccccc-cccc-4ccc-8ccc-cccccccccccc"
	txMine      = "dddddddd-dddd-4ddd-8ddd-dddddddddddd"
	txOldOct    = "eeeeeeee-eeee-4eee-8eee-eeeeeeeeeeee"
	txOctLate   = "ffffffff-ffff-4fff-8fff-ffffffffffff"
)

// txRepo adds transaction and budget storage to repoFake. SpentByCategory and
// FindRecentSimilar follow the real repository semantics.
type txRepo struct {
	*repoFake
	mu2         sync.Mutex
	txs         map[string]*domainfinance.Transaction
	budgets     []domainfinance.Budget
	lastFilter  domainfinance.ListFilter
	budgetMonth time.Time
	budgetErr   error
	spentErr    error
	similarErr  error
}

func (r *txRepo) CreateTransaction(_ context.Context, tx *domainfinance.Transaction) error {
	r.mu2.Lock()
	defer r.mu2.Unlock()
	c := *tx
	r.txs[tx.ID] = &c
	return nil
}

func (r *txRepo) FindTransaction(_ context.Context, space, id string) (*domainfinance.Transaction, error) {
	tx, ok := r.txs[id]
	if !ok || tx.SpaceID != space {
		return nil, gorm.ErrRecordNotFound
	}
	c := *tx
	return &c, nil
}

func (r *txRepo) UpdateTransaction(_ context.Context, tx *domainfinance.Transaction) error {
	cur, ok := r.txs[tx.ID]
	if !ok || cur.SpaceID != tx.SpaceID {
		return gorm.ErrRecordNotFound
	}
	c := *tx
	c.CreatedByMemberID, c.CreatedAt = cur.CreatedByMemberID, cur.CreatedAt
	r.txs[tx.ID] = &c
	return nil
}

func (r *txRepo) SoftDeleteTransaction(_ context.Context, space, id string, _ time.Time) error {
	tx, ok := r.txs[id]
	if !ok || tx.SpaceID != space {
		return gorm.ErrRecordNotFound
	}
	delete(r.txs, id)
	return nil
}

func (r *txRepo) ListTransactions(_ context.Context, f domainfinance.ListFilter) ([]domainfinance.Transaction, error) {
	r.lastFilter = f
	var out []domainfinance.Transaction
	for _, tx := range r.txs {
		if tx.SpaceID == f.SpaceID {
			out = append(out, *tx)
		}
	}
	return out, nil
}

func eqPtr(a, b *string) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

func (r *txRepo) FindRecentSimilar(_ context.Context, space string, t domainfinance.Transaction, window time.Duration) (*domainfinance.Transaction, error) {
	if r.similarErr != nil {
		return nil, r.similarErr
	}
	for _, o := range r.txs {
		if o.SpaceID == space && o.ID != t.ID && o.Type == t.Type && o.Amount == t.Amount && o.AccountID == t.AccountID &&
			eqPtr(o.ToAccountID, t.ToAccountID) && eqPtr(o.CategoryID, t.CategoryID) && o.Merchant == t.Merchant &&
			!o.CreatedAt.Before(t.CreatedAt.Add(-window)) && !o.CreatedAt.After(t.CreatedAt) {
			c := *o
			return &c, nil
		}
	}
	return nil, nil
}

func (r *txRepo) ListBudgets(_ context.Context, space string, month time.Time) ([]domainfinance.Budget, error) {
	r.budgetMonth = month
	if r.budgetErr != nil {
		return nil, r.budgetErr
	}
	var out []domainfinance.Budget
	for _, b := range r.budgets {
		if b.SpaceID == space && b.Month.Equal(month) {
			out = append(out, b)
		}
	}
	return out, nil
}

func (r *txRepo) SpentByCategory(_ context.Context, space, category string, from, to time.Time) (int64, error) {
	if r.spentErr != nil {
		return 0, r.spentErr
	}
	var sum int64
	for _, tx := range r.txs {
		if tx.SpaceID == space && tx.Type == domainfinance.TypeExpense && tx.CategoryID != nil && *tx.CategoryID == category &&
			!tx.OccurredAt.Before(from) && tx.OccurredAt.Before(to) {
			sum += tx.Amount
		}
	}
	return sum, nil
}

type txEnv struct {
	repo  *txRepo
	svc   interfacefinance.ServiceFinanceInterface
	audit *auditFake
}

func newTxEnvWith(t *testing.T, category string) *txEnv {
	t.Helper()
	base := newRepo()
	base.accounts[acctID] = &domainfinance.Account{ID: acctID, SpaceID: spaceID, Name: "Kas", Type: "cash"}
	base.accounts[acct2] = &domainfinance.Account{ID: acct2, SpaceID: spaceID, Name: "Bank BCA", Type: "bank"}
	base.accounts[foreignAcct] = &domainfinance.Account{ID: foreignAcct, SpaceID: foreign, Name: "Orang Lain", Type: "cash"}
	base.categories[catFood] = &domainfinance.Category{ID: catFood, SpaceID: spaceID, Name: "Makan", Kind: "expense"}
	base.categories[catSalary] = &domainfinance.Category{ID: catSalary, SpaceID: spaceID, Name: "Gaji", Kind: "income"}
	base.categories[foreignCat] = &domainfinance.Category{ID: foreignCat, SpaceID: foreign, Name: "Rahasia", Kind: "expense"}
	repo := &txRepo{repoFake: base, txs: map[string]*domainfinance.Transaction{}}
	audit := &auditFake{}
	return &txEnv{repo: repo, audit: audit, svc: NewService(repo, financeSpaces(category), authorization.NewAuthorizer(), audit)}
}

func newTxEnv(t *testing.T) *txEnv { return newTxEnvWith(t, domainspace.CategoryFinance) }

func ownerActor() domainidentity.ActorContext {
	return actorWith("finance:list", "finance:create", "finance:update", "finance:delete", "finance:manage")
}

func memberActor() domainidentity.ActorContext {
	return actorWith("finance:list", "finance:create", "finance:update")
}

func ptr[T any](v T) *T { return &v }

func (e *txEnv) create(t *testing.T, in dto.TransactionCreateInput) *dto.TransactionResult {
	t.Helper()
	res, err := e.svc.TransactionCreate(context.Background(), memberActor(), in)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	return res
}

func expense(amount int64) dto.TransactionCreateInput {
	return dto.TransactionCreateInput{Type: "expense", Amount: amount, Account: "kas", Category: "MAKAN", Merchant: " Warung "}
}

func (e *txEnv) seed(id, member string, tx domainfinance.Transaction) {
	tx.ID, tx.SpaceID, tx.CreatedByMemberID = id, spaceID, member
	if tx.CreatedAt.IsZero() {
		tx.CreatedAt = time.Now().UTC()
	}
	e.repo.txs[id] = &tx
}

func reasonOf(t *testing.T, err error, field string) string {
	t.Helper()
	vErr(t, err, field)
	var ve *authorization.ValidationError
	errors.As(err, &ve)
	return ve.Reason
}

func TestCreateStoresNormalizedTransaction(t *testing.T) {
	e := newTxEnv(t)
	res := e.create(t, expense(25_000))
	tx := res.Transaction
	if tx.SpaceID != spaceID || tx.AccountID != acctID || tx.CategoryID == nil || *tx.CategoryID != catFood ||
		tx.ToAccountID != nil || tx.Merchant != "Warung" || tx.CreatedByMemberID != memberID || tx.Type != "expense" || tx.ID == "" {
		t.Fatalf("tx = %+v", tx)
	}
	if len(e.repo.txs) != 1 || e.repo.txs[tx.ID].Amount != 25_000 {
		t.Fatal("not stored")
	}
	if res.PossibleDuplicate != nil || res.BudgetStatus != nil {
		t.Fatalf("unexpected extras: %+v", res)
	}
}

func TestCreateRejectsInvalidAmount(t *testing.T) {
	for _, amount := range []int64{0, -5, domainfinance.MaxAmount + 1, 1<<63 - 1} {
		e := newTxEnv(t)
		_, err := e.svc.TransactionCreate(context.Background(), memberActor(), expense(amount))
		vErr(t, err, "amount")
		if len(e.repo.txs) != 0 {
			t.Fatalf("amount %d: nothing may be written", amount)
		}
	}
	e := newTxEnv(t)
	if _, err := e.svc.TransactionCreate(context.Background(), memberActor(), expense(domainfinance.MaxAmount)); err != nil {
		t.Fatalf("max amount must be accepted: %v", err)
	}
}

func TestCreateRejectsInvalidType(t *testing.T) {
	e := newTxEnv(t)
	in := expense(10)
	in.Type = "refund"
	_, err := e.svc.TransactionCreate(context.Background(), memberActor(), in)
	vErr(t, err, "type")
	in.Type = " EXPENSE "
	if _, err := e.svc.TransactionCreate(context.Background(), memberActor(), in); err != nil {
		t.Fatalf("type is case-insensitive: %v", err)
	}
}

func TestCreateRejectsForeignSpaceAccount(t *testing.T) {
	for _, ref := range []string{foreignAcct, "Orang Lain", "99999999-0000-4000-8000-000000000000", "nope"} {
		e := newTxEnv(t)
		in := expense(10)
		in.Account = ref
		_, err := e.svc.TransactionCreate(context.Background(), memberActor(), in)
		if r := reasonOf(t, err, "account"); r != "not found" {
			t.Fatalf("%s: reason = %q", ref, r)
		}
		tr := dto.TransactionCreateInput{Type: "transfer", Amount: 10, Account: "Kas", ToAccount: ref}
		_, err = e.svc.TransactionCreate(context.Background(), memberActor(), tr)
		if r := reasonOf(t, err, "to_account"); r != "not found" {
			t.Fatalf("%s: to_account reason = %q", ref, r)
		}
		if len(e.repo.txs) != 0 {
			t.Fatal("nothing may be written")
		}
	}
}

func TestCreateRejectsForeignSpaceCategory(t *testing.T) {
	for _, ref := range []string{foreignCat, "Rahasia", "unknown"} {
		e := newTxEnv(t)
		in := expense(10)
		in.Category = ref
		_, err := e.svc.TransactionCreate(context.Background(), memberActor(), in)
		if r := reasonOf(t, err, "category"); r != "not found" {
			t.Fatalf("%s: reason = %q", ref, r)
		}
		if len(e.repo.txs) != 0 {
			t.Fatal("nothing may be written")
		}
	}
}

func TestCreateRejectsArchivedAccountAndCategory(t *testing.T) {
	e := newTxEnv(t)
	now := time.Now()
	e.repo.accounts[acctID].ArchivedAt = &now
	e.repo.categories[catFood].ArchivedAt = &now
	in := expense(10)
	_, err := e.svc.TransactionCreate(context.Background(), memberActor(), in)
	if r := reasonOf(t, err, "account"); r != "is archived" {
		t.Fatalf("reason = %q", r)
	}
	in.Account = acctID
	_, err = e.svc.TransactionCreate(context.Background(), memberActor(), in)
	if r := reasonOf(t, err, "account"); r != "is archived" {
		t.Fatalf("by id reason = %q", r)
	}
	in.Account = "Bank BCA"
	_, err = e.svc.TransactionCreate(context.Background(), memberActor(), in)
	if r := reasonOf(t, err, "category"); r != "is archived" {
		t.Fatalf("category reason = %q", r)
	}
	tr := dto.TransactionCreateInput{Type: "transfer", Amount: 5, Account: "Bank BCA", ToAccount: "Kas"}
	_, err = e.svc.TransactionCreate(context.Background(), memberActor(), tr)
	if r := reasonOf(t, err, "to_account"); r != "is archived" {
		t.Fatalf("to_account reason = %q", r)
	}
	if len(e.repo.txs) != 0 {
		t.Fatal("nothing may be written")
	}
}

func TestTransferRules(t *testing.T) {
	ctx := context.Background()
	e := newTxEnv(t)
	base := dto.TransactionCreateInput{Type: "transfer", Amount: 100_000, Account: "Kas", ToAccount: "bank bca"}

	res, err := e.svc.TransactionCreate(ctx, memberActor(), base)
	if err != nil {
		t.Fatal(err)
	}
	if tx := res.Transaction; tx.AccountID != acctID || tx.ToAccountID == nil || *tx.ToAccountID != acct2 || tx.CategoryID != nil {
		t.Fatalf("transfer = %+v", tx)
	}

	same := base
	same.ToAccount = acctID
	_, err = e.svc.TransactionCreate(ctx, memberActor(), same)
	vErr(t, err, "to_account_id")
	same.ToAccount = " kas "
	_, err = e.svc.TransactionCreate(ctx, memberActor(), same)
	vErr(t, err, "to_account_id")

	withCat := base
	withCat.Category = "Makan"
	_, err = e.svc.TransactionCreate(ctx, memberActor(), withCat)
	vErr(t, err, "category_id")

	noTarget := base
	noTarget.ToAccount = ""
	_, err = e.svc.TransactionCreate(ctx, memberActor(), noTarget)
	vErr(t, err, "to_account_id")

	exp := expense(10)
	exp.ToAccount = "Bank BCA"
	_, err = e.svc.TransactionCreate(ctx, memberActor(), exp)
	vErr(t, err, "to_account_id")

	if len(e.repo.txs) != 1 {
		t.Fatalf("only the valid transfer may be stored, have %d", len(e.repo.txs))
	}
}

func TestCreateRejectsCategoryKindMismatch(t *testing.T) {
	e := newTxEnv(t)
	in := expense(10)
	in.Type = "income"
	_, err := e.svc.TransactionCreate(context.Background(), memberActor(), in)
	if r := reasonOf(t, err, "category"); r != "kind does not match type" {
		t.Fatalf("reason = %q", r)
	}
	in = expense(10)
	in.Category = "Gaji"
	_, err = e.svc.TransactionCreate(context.Background(), memberActor(), in)
	vErr(t, err, "category")
	in.Type = "income"
	if _, err := e.svc.TransactionCreate(context.Background(), memberActor(), in); err != nil {
		t.Fatalf("income category on income: %v", err)
	}
	if len(e.repo.txs) != 1 {
		t.Fatal("only the valid one may be stored")
	}
}

func TestCategoryByNameSeedsDefaults(t *testing.T) {
	e := newTxEnv(t)
	e.repo.categories = map[string]*domainfinance.Category{}
	res := e.create(t, expense(10))
	if res.Transaction.CategoryID == nil {
		t.Fatal("default category Makan must resolve by name")
	}
}

func TestOccurredAtDefaultsToNow(t *testing.T) {
	e := newTxEnv(t)
	before := time.Now().UTC().Add(-time.Second)
	res := e.create(t, expense(10))
	got := res.Transaction.OccurredAt
	if got.Before(before) || got.After(time.Now().UTC().Add(time.Second)) || got.Location() != time.UTC {
		t.Fatalf("occurred_at = %v", got)
	}
	when := time.Date(2026, 5, 1, 12, 0, 0, 0, time.FixedZone("x", 7*3600))
	in := expense(11)
	in.OccurredAt = when
	res = e.create(t, in)
	if !res.Transaction.OccurredAt.Equal(when) || res.Transaction.OccurredAt.Location() != time.UTC {
		t.Fatalf("explicit occurred_at = %v", res.Transaction.OccurredAt)
	}
}

func TestMerchantAndNoteLimits(t *testing.T) {
	e := newTxEnv(t)
	in := expense(10)
	in.Merchant = strings.Repeat("é", 129)
	_, err := e.svc.TransactionCreate(context.Background(), memberActor(), in)
	vErr(t, err, "merchant")
	in = expense(10)
	in.Note = strings.Repeat("n", 1001)
	_, err = e.svc.TransactionCreate(context.Background(), memberActor(), in)
	vErr(t, err, "note")
	in = expense(10)
	in.Merchant, in.Note = strings.Repeat("é", 128), " "+strings.Repeat("n", 1000)+" "
	res, err := e.svc.TransactionCreate(context.Background(), memberActor(), in)
	if err != nil || len(res.Transaction.Note) != 1000 {
		t.Fatalf("limits must be inclusive: %v", err)
	}
}

func TestPossibleDuplicateWithinTwoMinutes(t *testing.T) {
	e := newTxEnv(t)
	first := e.create(t, expense(50_000))
	if first.PossibleDuplicate != nil {
		t.Fatal("first create has no duplicate")
	}
	second := e.create(t, expense(50_000))
	if second.PossibleDuplicate == nil || second.PossibleDuplicate.ID != first.Transaction.ID {
		t.Fatalf("duplicate = %+v", second.PossibleDuplicate)
	}
	if len(e.repo.txs) != 2 {
		t.Fatalf("both must be stored, have %d", len(e.repo.txs))
	}
	if other := e.create(t, expense(50_001)); other.PossibleDuplicate != nil {
		t.Fatal("different amount is not a duplicate")
	}
	for _, tx := range e.repo.txs {
		tx.CreatedAt = tx.CreatedAt.Add(-3 * time.Minute)
	}
	if late := e.create(t, expense(50_000)); late.PossibleDuplicate != nil {
		t.Fatal("older than two minutes is not a duplicate")
	}
}

func TestDuplicateLookupFailureDoesNotFailCreate(t *testing.T) {
	e := newTxEnv(t)
	e.repo.similarErr = errors.New("db down")
	res, err := e.svc.TransactionCreate(context.Background(), memberActor(), expense(10))
	if err != nil || res.PossibleDuplicate != nil || len(e.repo.txs) != 1 {
		t.Fatalf("err=%v res=%+v", err, res)
	}
}

func budgetFor(category string, when time.Time, amount int64) domainfinance.Budget {
	return domainfinance.Budget{ID: "b-" + category, SpaceID: spaceID, CategoryID: category, Month: domainfinance.MonthDate(when), Amount: amount}
}

func TestExpenseReturnsBudgetStatus(t *testing.T) {
	e := newTxEnv(t)
	now := time.Now().UTC()
	e.repo.budgets = []domainfinance.Budget{budgetFor(catFood, now, 1_000_000)}
	e.seed(txOther, otherMember, domainfinance.Transaction{Type: "expense", Amount: 800_000, AccountID: acctID, CategoryID: ptr(catFood), OccurredAt: now})
	res := e.create(t, expense(50_000))
	bs := res.BudgetStatus
	if bs == nil || bs.CategoryID != catFood || bs.Budget != 1_000_000 || bs.Spent != 850_000 || bs.Remaining != 150_000 || bs.PercentUsed != 85 {
		t.Fatalf("status = %+v", bs)
	}
	income := dto.TransactionCreateInput{Type: "income", Amount: 5, Account: "Kas", Category: "Gaji"}
	if got := e.create(t, income); got.BudgetStatus != nil {
		t.Fatal("income has no budget status")
	}
	noCat := dto.TransactionCreateInput{Type: "expense", Amount: 5, Account: "Kas"}
	if got := e.create(t, noCat); got.BudgetStatus != nil {
		t.Fatal("no category, no budget status")
	}
}

func TestBudgetStatusOverBudgetIsNegativeRemaining(t *testing.T) {
	e := newTxEnv(t)
	e.repo.budgets = []domainfinance.Budget{budgetFor(catFood, time.Now(), 100)}
	bs := e.create(t, expense(150)).BudgetStatus
	if bs == nil || bs.Remaining != -50 || bs.PercentUsed != 150 {
		t.Fatalf("status = %+v", bs)
	}
}

func TestBudgetStatusUsesWIBMonth(t *testing.T) {
	e := newTxEnv(t)
	nov := time.Date(2026, 11, 15, 0, 0, 0, 0, time.UTC)
	e.repo.budgets = []domainfinance.Budget{budgetFor(catFood, nov, 1_000_000)}
	// 2026-11-30 16:30 UTC is 23:30 WIB on Nov 30; 17:30 UTC is Dec 1 00:30 WIB.
	e.seed(txOther, otherMember, domainfinance.Transaction{Type: "expense", Amount: 100_000, AccountID: acctID, CategoryID: ptr(catFood), OccurredAt: time.Date(2026, 11, 30, 16, 30, 0, 0, time.UTC)})
	e.seed(txMine, memberID, domainfinance.Transaction{Type: "expense", Amount: 999, AccountID: acctID, CategoryID: ptr(catFood), OccurredAt: time.Date(2026, 11, 30, 17, 30, 0, 0, time.UTC)})
	// 2026-10-31 17:30 UTC is Nov 1 00:30 WIB (counted); 16:30 UTC is Oct 31 23:30 WIB (not counted).
	e.seed(txOldOct, memberID, domainfinance.Transaction{Type: "expense", Amount: 7, AccountID: acctID, CategoryID: ptr(catFood), OccurredAt: time.Date(2026, 10, 31, 17, 30, 0, 0, time.UTC)})
	e.seed(txOctLate, memberID, domainfinance.Transaction{Type: "expense", Amount: 5_000, AccountID: acctID, CategoryID: ptr(catFood), OccurredAt: time.Date(2026, 10, 31, 16, 30, 0, 0, time.UTC)})

	in := expense(200_000)
	in.OccurredAt = time.Date(2026, 11, 30, 16, 45, 0, 0, time.UTC)
	bs := e.create(t, in).BudgetStatus
	if bs == nil || bs.Spent != 300_007 || bs.PercentUsed != 30 {
		t.Fatalf("november status = %+v", bs)
	}
	if !e.repo.budgetMonth.Equal(time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("budget month = %v", e.repo.budgetMonth)
	}
	in.OccurredAt = time.Date(2026, 11, 30, 17, 30, 0, 0, time.UTC)
	if bs := e.create(t, in).BudgetStatus; bs != nil {
		t.Fatalf("a Dec 1 WIB transaction must not use the November budget: %+v", bs)
	}
	if !e.repo.budgetMonth.Equal(time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("budget month = %v", e.repo.budgetMonth)
	}
}

func TestBudgetStatusFailureDoesNotFailCreate(t *testing.T) {
	for name, set := range map[string]func(*txRepo){
		"budget lookup": func(r *txRepo) { r.budgetErr = errors.New("db down") },
		"spent lookup":  func(r *txRepo) { r.spentErr = errors.New("db down") },
	} {
		t.Run(name, func(t *testing.T) {
			e := newTxEnv(t)
			e.repo.budgets = []domainfinance.Budget{budgetFor(catFood, time.Now(), 1000)}
			set(e.repo)
			res, err := e.svc.TransactionCreate(context.Background(), memberActor(), expense(10))
			if err != nil || res.BudgetStatus != nil || len(e.repo.txs) != 1 {
				t.Fatalf("err=%v res=%+v stored=%d", err, res, len(e.repo.txs))
			}
		})
	}
}

func TestCreateGuard(t *testing.T) {
	e := newTxEnv(t)
	if _, err := e.svc.TransactionCreate(context.Background(), actorWith("finance:list"), expense(10)); !errors.Is(err, authorization.ErrForbidden) {
		t.Fatalf("viewer err = %v", err)
	}
	nonFinance := newTxEnvWith(t, domainspace.CategoryFamily)
	if _, err := nonFinance.svc.TransactionCreate(context.Background(), memberActor(), expense(10)); !errors.Is(err, authorization.ErrNotFound) {
		t.Fatalf("non-finance err = %v", err)
	}
	if len(e.repo.txs) != 0 || len(nonFinance.repo.txs) != 0 {
		t.Fatal("nothing may be written")
	}
}

func TestMemberCannotDelete(t *testing.T) {
	e := newTxEnv(t)
	e.seed(txMine, memberID, domainfinance.Transaction{Type: "expense", Amount: 5, AccountID: acctID})
	err := e.svc.TransactionDelete(context.Background(), memberActor(), spaceID, txMine)
	if !errors.Is(err, authorization.ErrForbidden) || len(e.repo.txs) != 1 {
		t.Fatalf("err=%v stored=%d", err, len(e.repo.txs))
	}
	if err := e.svc.TransactionDelete(context.Background(), ownerActor(), spaceID, txMine); err != nil || len(e.repo.txs) != 0 {
		t.Fatalf("owner delete: %v", err)
	}
	if err := e.svc.TransactionDelete(context.Background(), ownerActor(), spaceID, txMine); !errors.Is(err, authorization.ErrNotFound) {
		t.Fatalf("second delete err = %v", err)
	}
	err = e.svc.TransactionDelete(context.Background(), ownerActor(), spaceID, "nope")
	vErr(t, err, "id")
}

func TestMemberUpdatesOnlyOwnTransaction(t *testing.T) {
	e := newTxEnv(t)
	ctx := context.Background()
	e.seed(txOther, otherMember, domainfinance.Transaction{Type: "expense", Amount: 5, AccountID: acctID})
	e.seed(txMine, memberID, domainfinance.Transaction{Type: "expense", Amount: 5, AccountID: acctID})
	up := dto.TransactionUpdateInput{Amount: ptr(int64(900))}

	if _, err := e.svc.TransactionUpdate(ctx, memberActor(), spaceID, txOther, up); !errors.Is(err, authorization.ErrForbidden) {
		t.Fatalf("other's tx err = %v", err)
	}
	if e.repo.txs[txOther].Amount != 5 {
		t.Fatal("other's transaction changed")
	}
	if _, err := e.svc.TransactionUpdate(ctx, memberActor(), spaceID, txMine, up); err != nil || e.repo.txs[txMine].Amount != 900 {
		t.Fatalf("own tx: %v", err)
	}
	if _, err := e.svc.TransactionUpdate(ctx, ownerActor(), spaceID, txOther, up); err != nil || e.repo.txs[txOther].Amount != 900 {
		t.Fatalf("owner updates any: %v", err)
	}
	// A row that does not exist in the caller's Space is NotFound, never Forbidden.
	e.repo.txs[txOther].SpaceID = foreign
	if _, err := e.svc.TransactionUpdate(ctx, memberActor(), spaceID, txOther, up); !errors.Is(err, authorization.ErrNotFound) {
		t.Fatalf("foreign row err = %v", err)
	}
	if _, err := e.svc.TransactionUpdate(ctx, actorWith("finance:list"), spaceID, txMine, up); !errors.Is(err, authorization.ErrForbidden) {
		t.Fatalf("no update permission err = %v", err)
	}
}

func TestUpdateMergesAndClears(t *testing.T) {
	e := newTxEnv(t)
	ctx := context.Background()
	e.seed(txMine, memberID, domainfinance.Transaction{Type: "expense", Amount: 5, AccountID: acctID, CategoryID: ptr(catFood), Merchant: "A", Note: "n"})
	res, err := e.svc.TransactionUpdate(ctx, memberActor(), spaceID, txMine, dto.TransactionUpdateInput{Category: ptr(""), Merchant: ptr(" Toko ")})
	if err != nil {
		t.Fatal(err)
	}
	got := e.repo.txs[txMine]
	if got.CategoryID != nil || got.Merchant != "Toko" || got.Note != "n" || got.Amount != 5 || got.AccountID != acctID || res.Transaction.CategoryID != nil {
		t.Fatalf("stored = %+v", got)
	}
	_, err = e.svc.TransactionUpdate(ctx, memberActor(), spaceID, txMine, dto.TransactionUpdateInput{})
	vErr(t, err, "update")
	_, err = e.svc.TransactionUpdate(ctx, memberActor(), spaceID, txMine, dto.TransactionUpdateInput{Amount: ptr(int64(0))})
	vErr(t, err, "amount")
	_, err = e.svc.TransactionUpdate(ctx, memberActor(), spaceID, txMine, dto.TransactionUpdateInput{Account: ptr(foreignAcct)})
	vErr(t, err, "account")
	_, err = e.svc.TransactionUpdate(ctx, memberActor(), spaceID, txMine, dto.TransactionUpdateInput{Account: ptr("")})
	vErr(t, err, "account_id")
	if e.repo.txs[txMine].Amount != 5 || e.repo.txs[txMine].AccountID != acctID {
		t.Fatal("rejected updates must not write")
	}
}

func TestUpdateRevalidatesKindAndTransferRules(t *testing.T) {
	e := newTxEnv(t)
	ctx := context.Background()
	e.seed(txMine, memberID, domainfinance.Transaction{Type: "expense", Amount: 5, AccountID: acctID, CategoryID: ptr(catFood)})
	_, err := e.svc.TransactionUpdate(ctx, memberActor(), spaceID, txMine, dto.TransactionUpdateInput{Type: ptr("income")})
	if r := reasonOf(t, err, "category"); r != "kind does not match type" {
		t.Fatalf("reason = %q", r)
	}
	_, err = e.svc.TransactionUpdate(ctx, memberActor(), spaceID, txMine, dto.TransactionUpdateInput{Type: ptr("transfer"), ToAccount: ptr("Bank BCA")})
	vErr(t, err, "category_id")
	res, err := e.svc.TransactionUpdate(ctx, memberActor(), spaceID, txMine, dto.TransactionUpdateInput{Type: ptr("transfer"), ToAccount: ptr("Bank BCA"), Category: ptr("")})
	if err != nil || res.Transaction.ToAccountID == nil || *e.repo.txs[txMine].ToAccountID != acct2 || e.repo.txs[txMine].CategoryID != nil {
		t.Fatalf("convert to transfer: %v %+v", err, e.repo.txs[txMine])
	}
	_, err = e.svc.TransactionUpdate(ctx, memberActor(), spaceID, txMine, dto.TransactionUpdateInput{Type: ptr("expense")})
	vErr(t, err, "to_account_id")
	if _, err = e.svc.TransactionUpdate(ctx, memberActor(), spaceID, txMine, dto.TransactionUpdateInput{Type: ptr("expense"), ToAccount: ptr("")}); err != nil || e.repo.txs[txMine].ToAccountID != nil {
		t.Fatalf("convert back: %v", err)
	}
}

func TestUpdateKeepsArchivedUnchangedRefs(t *testing.T) {
	e := newTxEnv(t)
	e.seed(txMine, memberID, domainfinance.Transaction{Type: "expense", Amount: 5, AccountID: acctID, CategoryID: ptr(catFood)})
	now := time.Now()
	e.repo.accounts[acctID].ArchivedAt = &now
	e.repo.categories[catFood].ArchivedAt = &now
	if _, err := e.svc.TransactionUpdate(context.Background(), memberActor(), spaceID, txMine, dto.TransactionUpdateInput{Amount: ptr(int64(6))}); err != nil {
		t.Fatalf("editing amount must not need live refs: %v", err)
	}
	_, err := e.svc.TransactionUpdate(context.Background(), memberActor(), spaceID, txMine, dto.TransactionUpdateInput{Account: ptr("Kas")})
	vErr(t, err, "account")
}

func TestUpdateReturnsBudgetStatus(t *testing.T) {
	e := newTxEnv(t)
	now := time.Now().UTC()
	e.repo.budgets = []domainfinance.Budget{budgetFor(catFood, now, 1_000_000)}
	e.seed(txMine, memberID, domainfinance.Transaction{Type: "expense", Amount: 5, AccountID: acctID, CategoryID: ptr(catFood), OccurredAt: now})
	res, err := e.svc.TransactionUpdate(context.Background(), memberActor(), spaceID, txMine, dto.TransactionUpdateInput{Amount: ptr(int64(500_000))})
	if err != nil || res.BudgetStatus == nil || res.BudgetStatus.PercentUsed != 50 || res.PossibleDuplicate != nil {
		t.Fatalf("err=%v res=%+v", err, res)
	}
}

func TestTransactionListResolvesFiltersAndClamps(t *testing.T) {
	e := newTxEnv(t)
	ctx := context.Background()
	e.seed(txMine, memberID, domainfinance.Transaction{Type: "expense", Amount: 5, AccountID: acctID})
	got, err := e.svc.TransactionList(ctx, memberActor(), dto.TransactionListInput{Type: " Expense ", Category: "makan", Account: "KAS", Search: "x", Limit: 500})
	if err != nil || len(got) != 1 {
		t.Fatalf("%v %v", got, err)
	}
	f := e.repo.lastFilter
	if f.SpaceID != spaceID || f.Type != "expense" || f.CategoryID != catFood || f.AccountID != acctID || f.Search != "x" || f.Limit != 100 {
		t.Fatalf("filter = %+v", f)
	}
	if _, err := e.svc.TransactionList(ctx, memberActor(), dto.TransactionListInput{}); err != nil || e.repo.lastFilter.Limit != 50 {
		t.Fatalf("default limit = %d (%v)", e.repo.lastFilter.Limit, err)
	}
	_, err = e.svc.TransactionList(ctx, memberActor(), dto.TransactionListInput{Limit: -1})
	vErr(t, err, "limit")
	_, err = e.svc.TransactionList(ctx, memberActor(), dto.TransactionListInput{Category: "ghost"})
	vErr(t, err, "category")
	_, err = e.svc.TransactionList(ctx, memberActor(), dto.TransactionListInput{Account: foreignAcct})
	vErr(t, err, "account")
	_, err = e.svc.TransactionList(ctx, memberActor(), dto.TransactionListInput{Type: "bogus"})
	vErr(t, err, "type")
	now := time.Now()
	e.repo.accounts[acctID].ArchivedAt = &now
	if _, err := e.svc.TransactionList(ctx, memberActor(), dto.TransactionListInput{Account: "Kas"}); err != nil {
		t.Fatalf("archived account may be used as a filter: %v", err)
	}
	if _, err := e.svc.TransactionList(ctx, actorWith("finance:create"), dto.TransactionListInput{}); !errors.Is(err, authorization.ErrForbidden) {
		t.Fatalf("list needs finance:list: %v", err)
	}
}

func TestTransactionWritesAreAuditedWithoutSensitiveData(t *testing.T) {
	e := newTxEnv(t)
	ctx := context.Background()
	in := expense(123_456)
	in.Note = "secret-note"
	res := e.create(t, in)
	_, _ = e.svc.TransactionCreate(ctx, memberActor(), expense(0))
	_, _ = e.svc.TransactionUpdate(ctx, ownerActor(), spaceID, res.Transaction.ID, dto.TransactionUpdateInput{Amount: ptr(int64(7))})
	_, _ = e.svc.TransactionUpdate(ctx, memberActor(), spaceID, "junk", dto.TransactionUpdateInput{Amount: ptr(int64(7))})
	_ = e.svc.TransactionDelete(ctx, memberActor(), spaceID, res.Transaction.ID)
	_ = e.svc.TransactionDelete(ctx, ownerActor(), spaceID, res.Transaction.ID)

	counts := map[string]int{}
	for _, ev := range e.audit.events {
		if ev.Resource != "finance_transaction" {
			t.Fatalf("resource = %q", ev.Resource)
		}
		counts[ev.Action+"/"+ev.Status+"/"+ev.ErrorMessage]++
		blob := ev.Resource + ev.ResourceID + ev.ErrorMessage
		for k, v := range ev.Metadata {
			s, _ := v.(string)
			blob += k + ":" + s
		}
		for _, bad := range []string{"123456", "Warung", "secret-note"} {
			if strings.Contains(blob, bad) {
				t.Fatalf("audit leaks %q: %+v", bad, ev)
			}
		}
	}
	want := map[string]int{
		"create/" + domainaudit.StatusSuccess + "/":          1,
		"create/" + domainaudit.StatusFailed + "/validation": 1,
		"update/" + domainaudit.StatusSuccess + "/":          1,
		"update/" + domainaudit.StatusFailed + "/validation": 1,
		"delete/" + domainaudit.StatusFailed + "/forbidden":  1,
		"delete/" + domainaudit.StatusSuccess + "/":          1,
	}
	for k, v := range want {
		if counts[k] != v {
			t.Fatalf("audit counts = %v, want %v", counts, want)
		}
	}
	if e.audit.events[0].ResourceID != res.Transaction.ID {
		t.Fatalf("create audit resource id = %q", e.audit.events[0].ResourceID)
	}
}
