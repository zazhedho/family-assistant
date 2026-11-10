package servicefinance

import (
	"context"
	"errors"
	"fmt"
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
	interfacespace "family-assistant/internal/interfaces/space"
	"family-assistant/internal/services/authorization"
	"github.com/jackc/pgx/v5/pgconn"
	"gorm.io/gorm"
)

const (
	spaceID  = "11111111-1111-4111-8111-111111111111"
	otherID  = "22222222-2222-4222-8222-222222222222"
	memberID = "33333333-3333-4333-8333-333333333333"
	acctID   = "44444444-4444-4444-8444-444444444444"
	foreign  = "99999999-9999-4999-8999-999999999999"
)

type repoFake struct {
	interfacefinance.RepoFinanceInterface
	mu           sync.Mutex
	accounts     map[string]*domainfinance.Account
	categories   map[string]*domainfinance.Category
	archiveCnt   int
	unarchiveCnt int
	seedCalls    int
	createErr    error
	updateErr    error
}

func newRepo() *repoFake {
	return &repoFake{accounts: map[string]*domainfinance.Account{}, categories: map[string]*domainfinance.Category{}}
}

func (r *repoFake) CreateAccount(_ context.Context, a *domainfinance.Account) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.createErr != nil {
		return r.createErr
	}
	c := *a
	r.accounts[a.ID] = &c
	return nil
}

func (r *repoFake) ListAccounts(_ context.Context, space string, includeArchived bool) ([]domainfinance.Account, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []domainfinance.Account
	for _, a := range r.accounts {
		if a.SpaceID == space && (includeArchived || a.ArchivedAt == nil) {
			out = append(out, *a)
		}
	}
	return out, nil
}

func (r *repoFake) FindAccount(_ context.Context, space, id string) (*domainfinance.Account, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	a, ok := r.accounts[id]
	if !ok || a.SpaceID != space {
		return nil, gorm.ErrRecordNotFound
	}
	c := *a
	return &c, nil
}

func (r *repoFake) UpdateAccount(_ context.Context, a *domainfinance.Account) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.updateErr != nil {
		return r.updateErr
	}
	cur, ok := r.accounts[a.ID]
	if !ok || cur.SpaceID != a.SpaceID {
		return gorm.ErrRecordNotFound
	}
	cur.Name, cur.Type, cur.OpeningBalance = a.Name, a.Type, a.OpeningBalance
	return nil
}

func (r *repoFake) ArchiveAccount(_ context.Context, space, id string, at time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	a, ok := r.accounts[id]
	if !ok || a.SpaceID != space {
		return gorm.ErrRecordNotFound
	}
	a.ArchivedAt = &at
	r.archiveCnt++
	return nil
}

func (r *repoFake) CreateCategory(_ context.Context, c *domainfinance.Category) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.createErr != nil {
		return r.createErr
	}
	cp := *c
	r.categories[c.ID] = &cp
	return nil
}

func (r *repoFake) ListCategories(_ context.Context, space string, includeArchived bool) ([]domainfinance.Category, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []domainfinance.Category
	for _, c := range r.categories {
		if c.SpaceID == space && (includeArchived || c.ArchivedAt == nil) {
			out = append(out, *c)
		}
	}
	return out, nil
}

func (r *repoFake) UnarchiveAccount(_ context.Context, space, id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	a, ok := r.accounts[id]
	if !ok || a.SpaceID != space {
		return gorm.ErrRecordNotFound
	}
	a.ArchivedAt = nil
	r.unarchiveCnt++
	return nil
}

func (r *repoFake) UnarchiveCategory(_ context.Context, space, id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	c, ok := r.categories[id]
	if !ok || c.SpaceID != space {
		return gorm.ErrRecordNotFound
	}
	c.ArchivedAt = nil
	r.unarchiveCnt++
	return nil
}

// SeedDefaultCategories models the repository: atomic, and a no-op when the
// Space already has a category row of any state.
func (r *repoFake) SeedDefaultCategories(_ context.Context, space string, defaults []domainfinance.Category) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.seedCalls++
	for _, c := range r.categories {
		if c.SpaceID == space {
			return nil
		}
	}
	for i, d := range defaults {
		d.SpaceID = space
		d.ID = fmt.Sprintf("seed-%s-%d", space, i)
		cp := d
		r.categories[d.ID] = &cp
	}
	return nil
}

func (r *repoFake) FindCategory(_ context.Context, space, id string) (*domainfinance.Category, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	c, ok := r.categories[id]
	if !ok || c.SpaceID != space {
		return nil, gorm.ErrRecordNotFound
	}
	cp := *c
	return &cp, nil
}

func (r *repoFake) UpdateCategory(_ context.Context, c *domainfinance.Category) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.updateErr != nil {
		return r.updateErr
	}
	cur, ok := r.categories[c.ID]
	if !ok || cur.SpaceID != c.SpaceID {
		return gorm.ErrRecordNotFound
	}
	cur.Name, cur.Kind = c.Name, c.Kind
	return nil
}

func (r *repoFake) ArchiveCategory(_ context.Context, space, id string, at time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	c, ok := r.categories[id]
	if !ok || c.SpaceID != space {
		return gorm.ErrRecordNotFound
	}
	c.ArchivedAt = &at
	return nil
}

type spacesFake struct {
	interfacespace.RepoSpaceInterface
	members []domainspace.ResolvedMembership
}

func (s *spacesFake) ListActiveMembers(_ context.Context, id string) ([]domainspace.ResolvedMembership, error) {
	var out []domainspace.ResolvedMembership
	for _, m := range s.members {
		if m.SpaceID == id && m.Status == domainspace.StatusActive {
			out = append(out, m)
		}
	}
	return out, nil
}

type auditFake struct {
	mu     sync.Mutex
	events []domainaudit.AuditEvent
}

func (a *auditFake) Store(_ context.Context, e domainaudit.AuditEvent) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.events = append(a.events, e)
	return nil
}

func financeSpaces(category string) *spacesFake {
	return &spacesFake{members: []domainspace.ResolvedMembership{
		{ID: memberID, SpaceID: spaceID, UserID: "user-1", SpaceCategory: category, Status: domainspace.StatusActive},
	}}
}

func actorWith(perms ...string) domainidentity.ActorContext {
	set := map[string]struct{}{}
	for _, p := range perms {
		set[p] = struct{}{}
	}
	return domainidentity.ActorContext{UserID: "user-1", SpaceID: spaceID, MemberID: memberID, Permissions: set, Source: "mcp"}
}

func newSvc(repo *repoFake, spaces *spacesFake, audit *auditFake) interfacefinance.ServiceFinanceInterface {
	return NewService(repo, spaces, authorization.NewAuthorizer(), audit)
}

func vErr(t *testing.T, err error, field string) {
	t.Helper()
	var ve *authorization.ValidationError
	if !errors.As(err, &ve) || ve.Field != field {
		t.Fatalf("err = %v, want ValidationError on %q", err, field)
	}
}

func TestGuardRejectsNonFinanceSpace(t *testing.T) {
	repo := newRepo()
	svc := newSvc(repo, financeSpaces(domainspace.CategoryFamily), &auditFake{})
	_, err := svc.AccountCreate(context.Background(), actorWith("finance:manage"), dto.AccountCreateInput{Name: "Kas", Type: "cash"})
	if !errors.Is(err, authorization.ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
	if _, err := svc.CategoryList(context.Background(), actorWith("finance:list"), dto.CategoryListInput{}); !errors.Is(err, authorization.ErrNotFound) {
		t.Fatalf("list err = %v", err)
	}
	if len(repo.accounts) != 0 || len(repo.categories) != 0 {
		t.Fatalf("nothing may be written: %d %d", len(repo.accounts), len(repo.categories))
	}
}

func TestGuardRejectsNonMember(t *testing.T) {
	repo := newRepo()
	spaces := financeSpaces(domainspace.CategoryFinance)
	spaces.members[0].ID = "someone-else"
	svc := newSvc(repo, spaces, &auditFake{})
	_, err := svc.AccountCreate(context.Background(), actorWith("finance:manage"), dto.AccountCreateInput{Name: "Kas", Type: "cash"})
	if !errors.Is(err, authorization.ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
	if len(repo.accounts) != 0 {
		t.Fatal("nothing may be written")
	}
}

func TestMemberCannotManageAccounts(t *testing.T) {
	repo := newRepo()
	repo.accounts[acctID] = &domainfinance.Account{ID: acctID, SpaceID: spaceID, Name: "Kas", Type: "cash"}
	svc := newSvc(repo, financeSpaces(domainspace.CategoryFinance), &auditFake{})
	member := actorWith("finance:list", "finance:create", "finance:update")
	if _, err := svc.AccountCreate(context.Background(), member, dto.AccountCreateInput{Name: "Kas", Type: "cash"}); !errors.Is(err, authorization.ErrForbidden) {
		t.Fatalf("create err = %v", err)
	}
	name := "Baru"
	if _, err := svc.AccountUpdate(context.Background(), member, dto.AccountUpdateInput{ID: acctID, Name: &name}); !errors.Is(err, authorization.ErrForbidden) {
		t.Fatalf("update err = %v", err)
	}
	if _, err := svc.CategoryCreate(context.Background(), member, dto.CategoryCreateInput{Name: "X", Kind: "expense"}); !errors.Is(err, authorization.ErrForbidden) {
		t.Fatalf("category create err = %v", err)
	}
	if _, err := svc.CategoryUpdate(context.Background(), member, dto.CategoryUpdateInput{ID: acctID, Name: &name}); !errors.Is(err, authorization.ErrForbidden) {
		t.Fatalf("category update err = %v", err)
	}
	if _, err := svc.AccountList(context.Background(), member, dto.AccountListInput{}); err != nil {
		t.Fatalf("list should work with finance:list: %v", err)
	}
	if _, err := svc.AccountList(context.Background(), actorWith("finance:manage"), dto.AccountListInput{}); !errors.Is(err, authorization.ErrForbidden) {
		t.Fatalf("list without finance:list err = %v", err)
	}
	if repo.accounts[acctID].Name != "Kas" || len(repo.accounts) != 1 || len(repo.categories) != 0 {
		t.Fatal("repo changed")
	}
}

func TestDefaultCategoriesCreatedOnce(t *testing.T) {
	repo := newRepo()
	svc := newSvc(repo, financeSpaces(domainspace.CategoryFinance), &auditFake{})
	for i := 0; i < 3; i++ {
		got, err := svc.CategoryList(context.Background(), actorWith("finance:list"), dto.CategoryListInput{})
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != 11 {
			t.Fatalf("call %d: %d categories, want 11", i, len(got))
		}
	}
	expense, income := 0, 0
	for _, c := range repo.categories {
		switch c.Kind {
		case "expense":
			expense++
		case "income":
			income++
		}
	}
	if expense != 8 || income != 3 {
		t.Fatalf("expense=%d income=%d", expense, income)
	}
	if _, err := svc.CategoryCreate(context.Background(), actorWith("finance:manage"), dto.CategoryCreateInput{Name: " Hobi ", Kind: "expense"}); err != nil {
		t.Fatal(err)
	}
	if len(repo.categories) != 12 {
		t.Fatalf("categories = %d, want 12", len(repo.categories))
	}
}

func TestDefaultCategoriesNotReseededWhenAllArchived(t *testing.T) {
	repo := newRepo()
	svc := newSvc(repo, financeSpaces(domainspace.CategoryFinance), &auditFake{})
	if _, err := svc.CategoryList(context.Background(), actorWith("finance:list"), dto.CategoryListInput{}); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	for _, c := range repo.categories {
		c.ArchivedAt = &now
	}
	got, err := svc.CategoryList(context.Background(), actorWith("finance:list"), dto.CategoryListInput{})
	if err != nil || len(got) != 0 || len(repo.categories) != 11 {
		t.Fatalf("got=%d total=%d err=%v", len(got), len(repo.categories), err)
	}
}

func TestDefaultCategoriesConcurrentFirstCallsDoNotDuplicate(t *testing.T) {
	repo := newRepo()
	svc := newSvc(repo, financeSpaces(domainspace.CategoryFinance), &auditFake{})
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := svc.CategoryList(context.Background(), actorWith("finance:list"), dto.CategoryListInput{}); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if len(repo.categories) != 11 {
		t.Fatalf("categories = %d, want 11", len(repo.categories))
	}
	if repo.seedCalls == 0 {
		t.Fatal("seeding must go through the repository")
	}
}

func TestListLimitsAreClamped(t *testing.T) {
	repo := newRepo()
	for i := 0; i < 120; i++ {
		id := fmt.Sprintf("acc-%d", i)
		repo.accounts[id] = &domainfinance.Account{ID: id, SpaceID: spaceID, Name: "n", Type: "cash"}
	}
	svc := newSvc(repo, financeSpaces(domainspace.CategoryFinance), &auditFake{})
	a := actorWith("finance:list")
	for limit, want := range map[int]int{0: 50, 10: 10, 500: 100} {
		got, err := svc.AccountList(context.Background(), a, dto.AccountListInput{Limit: limit})
		if err != nil || len(got) != want {
			t.Fatalf("limit %d: len=%d err=%v want %d", limit, len(got), err, want)
		}
	}
	_, err := svc.AccountList(context.Background(), a, dto.AccountListInput{Limit: -1})
	vErr(t, err, "limit")
}

func TestAccountOpeningBalanceMayBeNegativeOnlyForCreditCard(t *testing.T) {
	repo := newRepo()
	svc := newSvc(repo, financeSpaces(domainspace.CategoryFinance), &auditFake{})
	a := actorWith("finance:manage")
	ctx := context.Background()
	for _, typ := range []string{"cash", "bank", "ewallet", "savings"} {
		_, err := svc.AccountCreate(ctx, a, dto.AccountCreateInput{Name: "x", Type: typ, OpeningBalance: -1})
		vErr(t, err, "opening_balance")
	}
	if len(repo.accounts) != 0 {
		t.Fatal("rejected creates must not write")
	}
	cc, err := svc.AccountCreate(ctx, a, dto.AccountCreateInput{Name: " Kartu ", Type: "credit_card", OpeningBalance: -500000})
	if err != nil || cc.OpeningBalance != -500000 || cc.Name != "Kartu" || cc.SpaceID != spaceID {
		t.Fatalf("credit card: %+v %v", cc, err)
	}
	if _, err := svc.AccountCreate(ctx, a, dto.AccountCreateInput{Name: "Kas", Type: "cash", OpeningBalance: 0}); err != nil {
		t.Fatal(err)
	}
	_, err = svc.AccountCreate(ctx, a, dto.AccountCreateInput{Name: "x", Type: "cash", OpeningBalance: domainfinance.MaxAmount + 1})
	vErr(t, err, "opening_balance")
	_, err = svc.AccountCreate(ctx, a, dto.AccountCreateInput{Name: "x", Type: "credit_card", OpeningBalance: -domainfinance.MaxAmount - 1})
	vErr(t, err, "opening_balance")
}

func TestAccountCreateValidatesNameAndType(t *testing.T) {
	svc := newSvc(newRepo(), financeSpaces(domainspace.CategoryFinance), &auditFake{})
	a := actorWith("finance:manage")
	_, err := svc.AccountCreate(context.Background(), a, dto.AccountCreateInput{Name: "   ", Type: "cash"})
	vErr(t, err, "name")
	_, err = svc.AccountCreate(context.Background(), a, dto.AccountCreateInput{Name: strings.Repeat("x", 129), Type: "cash"})
	vErr(t, err, "name")
	_, err = svc.AccountCreate(context.Background(), a, dto.AccountCreateInput{Name: "x", Type: "crypto"})
	vErr(t, err, "type")
}

func TestArchiveAccountKeepsTransactions(t *testing.T) {
	repo := newRepo()
	repo.accounts[acctID] = &domainfinance.Account{ID: acctID, SpaceID: spaceID, Name: "Kas", Type: "cash"}
	svc := newSvc(repo, financeSpaces(domainspace.CategoryFinance), &auditFake{})
	yes := true
	got, err := svc.AccountUpdate(context.Background(), actorWith("finance:manage"), dto.AccountUpdateInput{ID: acctID, Archived: &yes})
	if err != nil {
		t.Fatal(err)
	}
	if got.ArchivedAt == nil || repo.accounts[acctID].ArchivedAt == nil {
		t.Fatal("archived_at must be set")
	}
	if len(repo.accounts) != 1 || repo.archiveCnt != 1 {
		t.Fatalf("archive only: accounts=%d archives=%d", len(repo.accounts), repo.archiveCnt)
	}
	first := *repo.accounts[acctID].ArchivedAt
	if _, err := svc.AccountUpdate(context.Background(), actorWith("finance:manage"), dto.AccountUpdateInput{ID: acctID, Archived: &yes}); err != nil {
		t.Fatal(err)
	}
	if !repo.accounts[acctID].ArchivedAt.Equal(first) || repo.archiveCnt != 1 {
		t.Fatal("archiving twice must not restamp")
	}
}

func TestUnarchiveClearsArchivedAtAndKeepsRow(t *testing.T) {
	repo := newRepo()
	repo.accounts[acctID] = &domainfinance.Account{ID: acctID, SpaceID: spaceID, Name: "Kas", Type: "cash"}
	repo.categories[otherID] = &domainfinance.Category{ID: otherID, SpaceID: spaceID, Name: "Hobi", Kind: "expense"}
	svc := newSvc(repo, financeSpaces(domainspace.CategoryFinance), &auditFake{})
	a := actorWith("finance:manage")
	ctx := context.Background()
	yes, no := true, false

	if _, err := svc.AccountUpdate(ctx, a, dto.AccountUpdateInput{ID: acctID, Archived: &yes}); err != nil {
		t.Fatal(err)
	}
	got, err := svc.AccountUpdate(ctx, a, dto.AccountUpdateInput{ID: acctID, Archived: &no})
	if err != nil || got.ArchivedAt != nil || repo.accounts[acctID].ArchivedAt != nil || len(repo.accounts) != 1 {
		t.Fatalf("account unarchive: %+v %v", got, err)
	}
	if _, err := svc.CategoryUpdate(ctx, a, dto.CategoryUpdateInput{ID: otherID, Archived: &yes}); err != nil {
		t.Fatal(err)
	}
	gotCat, err := svc.CategoryUpdate(ctx, a, dto.CategoryUpdateInput{ID: otherID, Archived: &no})
	if err != nil || gotCat.ArchivedAt != nil || repo.categories[otherID].ArchivedAt != nil {
		t.Fatalf("category unarchive: %+v %v", gotCat, err)
	}
	if repo.unarchiveCnt != 2 {
		t.Fatalf("unarchive calls = %d, want 2", repo.unarchiveCnt)
	}
}

func TestUnarchiveOfNonArchivedRowIsNoOp(t *testing.T) {
	repo := newRepo()
	repo.accounts[acctID] = &domainfinance.Account{ID: acctID, SpaceID: spaceID, Name: "Kas", Type: "cash"}
	repo.categories[otherID] = &domainfinance.Category{ID: otherID, SpaceID: spaceID, Name: "Hobi", Kind: "expense"}
	svc := newSvc(repo, financeSpaces(domainspace.CategoryFinance), &auditFake{})
	a := actorWith("finance:manage")
	no := false
	if _, err := svc.AccountUpdate(context.Background(), a, dto.AccountUpdateInput{ID: acctID, Archived: &no}); err != nil {
		t.Fatalf("account: %v", err)
	}
	if _, err := svc.CategoryUpdate(context.Background(), a, dto.CategoryUpdateInput{ID: otherID, Archived: &no}); err != nil {
		t.Fatalf("category: %v", err)
	}
	if repo.unarchiveCnt != 0 || repo.archiveCnt != 0 {
		t.Fatalf("no-op must not write: unarchive=%d archive=%d", repo.unarchiveCnt, repo.archiveCnt)
	}
}

func uniqueViolation() error { return &pgconn.PgError{Code: "23505"} }

func TestUniqueViolationMapsToNameAlreadyExists(t *testing.T) {
	repo := newRepo()
	repo.accounts[acctID] = &domainfinance.Account{ID: acctID, SpaceID: spaceID, Name: "Kas", Type: "cash"}
	repo.categories[otherID] = &domainfinance.Category{ID: otherID, SpaceID: spaceID, Name: "Hobi", Kind: "expense"}
	svc := newSvc(repo, financeSpaces(domainspace.CategoryFinance), &auditFake{})
	a := actorWith("finance:manage")
	ctx := context.Background()
	name := "Baru"

	repo.updateErr = uniqueViolation()
	_, err := svc.AccountUpdate(ctx, a, dto.AccountUpdateInput{ID: acctID, Name: &name})
	vErr(t, err, "name")
	_, err = svc.CategoryUpdate(ctx, a, dto.CategoryUpdateInput{ID: otherID, Name: &name})
	vErr(t, err, "name")
	repo.updateErr = nil

	repo.createErr = uniqueViolation()
	_, err = svc.AccountCreate(ctx, a, dto.AccountCreateInput{Name: "Kas", Type: "cash"})
	vErr(t, err, "name")
	_, err = svc.CategoryCreate(ctx, a, dto.CategoryCreateInput{Name: "Hobi", Kind: "expense"})
	vErr(t, err, "name")
	var ve *authorization.ValidationError
	if errors.As(err, &ve) && ve.Reason != reasonExists {
		t.Fatalf("reason = %q", ve.Reason)
	}

	repo.createErr = errors.New("other db error")
	if _, err = svc.AccountCreate(ctx, a, dto.AccountCreateInput{Name: "Kas2", Type: "cash"}); err == nil || errors.As(err, &ve) {
		t.Fatalf("non-unique errors must pass through, got %v", err)
	}
}

func TestAuditDoesNotStoreUnvalidatedInput(t *testing.T) {
	repo := newRepo()
	audit := &auditFake{}
	svc := newSvc(repo, financeSpaces(domainspace.CategoryFinance), audit)
	a := actorWith("finance:manage")
	junk := strings.Repeat("A", 5000)
	name := "x"
	_, _ = svc.AccountCreate(context.Background(), a, dto.AccountCreateInput{Space: junk, Name: "Kas", Type: "cash"})
	_, _ = svc.AccountUpdate(context.Background(), a, dto.AccountUpdateInput{ID: junk, Name: &name})
	_, _ = svc.CategoryUpdate(context.Background(), a, dto.CategoryUpdateInput{Space: junk, ID: junk, Name: &name})
	if len(audit.events) != 3 {
		t.Fatalf("events = %d", len(audit.events))
	}
	for _, e := range audit.events {
		if len(e.ResourceID) > 36 || e.ResourceID != "" {
			t.Errorf("resource id stored: %q", e.ResourceID)
		}
		if got, _ := e.Metadata["space_id"].(string); len(got) > 36 || (got != "" && got != spaceID) {
			t.Errorf("space_id stored: %q", got)
		}
	}
	// A valid UUID is kept.
	audit.events = nil
	_, _ = svc.AccountUpdate(context.Background(), a, dto.AccountUpdateInput{ID: acctID, Name: &name})
	if len(audit.events) != 1 || audit.events[0].ResourceID != acctID {
		t.Fatalf("valid id must be audited: %+v", audit.events)
	}
}

func TestAccountUpdateRenameAndScope(t *testing.T) {
	repo := newRepo()
	repo.accounts[acctID] = &domainfinance.Account{ID: acctID, SpaceID: spaceID, Name: "Kas", Type: "cash", OpeningBalance: 5}
	repo.accounts[otherID] = &domainfinance.Account{ID: otherID, SpaceID: foreign, Name: "Orang", Type: "cash"}
	svc := newSvc(repo, financeSpaces(domainspace.CategoryFinance), &auditFake{})
	a := actorWith("finance:manage")
	name := "  Dompet "
	got, err := svc.AccountUpdate(context.Background(), a, dto.AccountUpdateInput{ID: acctID, Name: &name})
	if err != nil || got.Name != "Dompet" || repo.accounts[acctID].OpeningBalance != 5 {
		t.Fatalf("%+v %v", got, err)
	}
	if _, err := svc.AccountUpdate(context.Background(), a, dto.AccountUpdateInput{ID: otherID, Name: &name}); reasonOf(t, err, "account") != "not found" {
		t.Fatalf("foreign account err = %v", err)
	}
	if repo.accounts[otherID].Name != "Orang" {
		t.Fatal("foreign account changed")
	}
	_, err = svc.AccountUpdate(context.Background(), a, dto.AccountUpdateInput{ID: "not-a-uuid", Name: &name})
	vErr(t, err, "account")
	_, err = svc.AccountUpdate(context.Background(), a, dto.AccountUpdateInput{ID: acctID})
	vErr(t, err, "update")
	empty := " "
	_, err = svc.AccountUpdate(context.Background(), a, dto.AccountUpdateInput{ID: acctID, Name: &empty})
	vErr(t, err, "name")
}

func TestCategoryCreateAndUpdate(t *testing.T) {
	repo := newRepo()
	svc := newSvc(repo, financeSpaces(domainspace.CategoryFinance), &auditFake{})
	a := actorWith("finance:manage")
	ctx := context.Background()
	_, err := svc.CategoryCreate(ctx, a, dto.CategoryCreateInput{Name: "x", Kind: "transfer"})
	vErr(t, err, "kind")
	_, err = svc.CategoryCreate(ctx, a, dto.CategoryCreateInput{Name: " ", Kind: "expense"})
	vErr(t, err, "name")
	c, err := svc.CategoryCreate(ctx, a, dto.CategoryCreateInput{Name: " Hobi ", Kind: "expense"})
	if err != nil || c.Name != "Hobi" {
		t.Fatalf("%+v %v", c, err)
	}
	name := "Hobi Baru"
	yes := true
	got, err := svc.CategoryUpdate(ctx, a, dto.CategoryUpdateInput{ID: c.ID, Name: &name, Archived: &yes})
	if err != nil || got.Name != "Hobi Baru" || got.ArchivedAt == nil {
		t.Fatalf("%+v %v", got, err)
	}
	if _, err := svc.CategoryUpdate(ctx, a, dto.CategoryUpdateInput{ID: otherID, Name: &name}); reasonOf(t, err, "category") != "not found" {
		t.Fatalf("missing err = %v", err)
	}
}

func TestWritesAreAudited(t *testing.T) {
	repo := newRepo()
	audit := &auditFake{}
	svc := newSvc(repo, financeSpaces(domainspace.CategoryFinance), audit)
	a := actorWith("finance:manage")
	if _, err := svc.AccountCreate(context.Background(), a, dto.AccountCreateInput{Name: "Kas", Type: "cash"}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.AccountCreate(context.Background(), a, dto.AccountCreateInput{Name: "", Type: "cash"}); err == nil {
		t.Fatal("expected error")
	}
	if _, err := svc.AccountCreate(context.Background(), actorWith(), dto.AccountCreateInput{Name: "Kas", Type: "cash"}); err == nil {
		t.Fatal("expected error")
	}
	var ok, validation, forbidden int
	for _, e := range audit.events {
		if e.Resource != "finance_account" || e.Action != domainaudit.ActionCreate {
			continue
		}
		switch {
		case e.Status == domainaudit.StatusSuccess:
			ok++
		case e.ErrorMessage == "validation":
			validation++
		case e.ErrorMessage == "forbidden":
			forbidden++
		}
	}
	if ok != 1 || validation != 1 || forbidden != 1 {
		t.Fatalf("audit ok=%d validation=%d forbidden=%d events=%+v", ok, validation, forbidden, audit.events)
	}
}

func TestMapDomainError(t *testing.T) {
	cases := map[error]string{
		domainfinance.ErrInvalidType:        "type",
		domainfinance.ErrInvalidAmount:      "amount",
		domainfinance.ErrMoneyFormat:        "amount",
		domainfinance.ErrMoneyPrecision:     "amount",
		domainfinance.ErrAmountTooLarge:     "amount",
		domainfinance.ErrAccountRequired:    "account",
		domainfinance.ErrToAccountRequired:  "to_account",
		domainfinance.ErrToAccountForbidden: "to_account",
		domainfinance.ErrTransferSameAcct:   "to_account",
		domainfinance.ErrTransferCategory:   "category",
	}
	for in, field := range cases {
		vErr(t, mapDomainError(in), field)
	}
	unknown := errors.New("boom")
	if got := mapDomainError(unknown); !errors.Is(got, unknown) {
		t.Fatalf("unknown must pass through, got %v", got)
	}
	if mapDomainError(nil) != nil {
		t.Fatal("nil must stay nil")
	}
}

func TestMapMoneyErrorUsesTheGivenField(t *testing.T) {
	for _, in := range []error{domainfinance.ErrMoneyFormat, domainfinance.ErrMoneyPrecision, domainfinance.ErrAmountTooLarge, domainfinance.ErrInvalidAmount} {
		vErr(t, MapMoneyError("opening_balance", in), "opening_balance")
		vErr(t, MapMoneyError("amount", fmt.Errorf("wrapped: %w", in)), "amount")
	}
	unknown := errors.New("boom")
	if got := MapMoneyError("amount", unknown); !errors.Is(got, unknown) {
		t.Fatalf("unknown must pass through, got %v", got)
	}
	if MapMoneyError("amount", nil) != nil {
		t.Fatal("nil must stay nil")
	}
	if _, err := domainfinance.ParseMoney("5.123", false); err != nil {
		vErr(t, MapMoneyError("opening_balance", err), "opening_balance")
	}
}

func TestAccountUpdateByName(t *testing.T) {
	repo := newRepo()
	repo.accounts[acctID] = &domainfinance.Account{ID: acctID, SpaceID: spaceID, Name: "BCA", Type: "bank"}
	repo.accounts[otherID] = &domainfinance.Account{ID: otherID, SpaceID: foreign, Name: "Mandiri", Type: "bank"}
	svc := newSvc(repo, financeSpaces(domainspace.CategoryFinance), &auditFake{})
	a := actorWith("finance:manage")
	ctx := context.Background()

	name := "BCA Utama"
	ref := " bca "
	got, err := svc.AccountUpdate(ctx, a, dto.AccountUpdateInput{ID: ref, Name: &name})
	if err != nil || got.Name != "BCA Utama" || repo.accounts[acctID].Name != "BCA Utama" {
		t.Fatalf("rename by name: %+v %v", got, err)
	}
	yes := true
	got, err = svc.AccountUpdate(ctx, a, dto.AccountUpdateInput{ID: "bca utama", Archived: &yes})
	if err != nil || got.ArchivedAt == nil || repo.archiveCnt != 1 {
		t.Fatalf("archive by name: %+v %v", got, err)
	}
	no := false
	got, err = svc.AccountUpdate(ctx, a, dto.AccountUpdateInput{ID: "BCA Utama", Archived: &no})
	if err != nil || got.ArchivedAt != nil {
		t.Fatalf("restore archived by name: %+v %v", got, err)
	}
	// A name that lives in another Space is not found, and nothing changes.
	_, err = svc.AccountUpdate(ctx, a, dto.AccountUpdateInput{ID: "Mandiri", Name: &name})
	if r := reasonOf(t, err, "account"); r != "not found" {
		t.Fatalf("reason = %q", r)
	}
	if repo.accounts[otherID].Name != "Mandiri" {
		t.Fatal("foreign account changed")
	}
	// The permission guard still runs before any lookup.
	if _, err = svc.AccountUpdate(ctx, actorWith(), dto.AccountUpdateInput{ID: "BCA Utama", Name: &name}); err == nil || errors.Is(err, authorization.ErrNotFound) {
		t.Fatalf("expected a forbidden error, got %v", err)
	}
}

func TestCategoryUpdateByName(t *testing.T) {
	repo := newRepo()
	svc := newSvc(repo, financeSpaces(domainspace.CategoryFinance), &auditFake{})
	a := actorWith("finance:manage")
	ctx := context.Background()
	for _, in := range []dto.CategoryCreateInput{
		{Name: "Hobi", Kind: "expense"}, {Name: "Bonus Tahunan", Kind: "income"}, {Name: "Lain Dua", Kind: "expense"}, {Name: "Lain Dua", Kind: "income"},
	} {
		if _, err := svc.CategoryCreate(ctx, a, in); err != nil {
			t.Fatal(err)
		}
	}
	name := "Hobi Baru"
	got, err := svc.CategoryUpdate(ctx, a, dto.CategoryUpdateInput{ID: "hobi", Name: &name})
	if err != nil || got.Name != "Hobi Baru" {
		t.Fatalf("rename by name: %+v %v", got, err)
	}
	yes := true
	got, err = svc.CategoryUpdate(ctx, a, dto.CategoryUpdateInput{ID: "BONUS TAHUNAN", Archived: &yes})
	if err != nil || got.ArchivedAt == nil {
		t.Fatalf("archive by name: %+v %v", got, err)
	}
	// One name for both kinds is ambiguous and nothing is changed.
	_, err = svc.CategoryUpdate(ctx, a, dto.CategoryUpdateInput{ID: "lain dua", Archived: &yes})
	if r := reasonOf(t, err, "category"); r != "is ambiguous" {
		t.Fatalf("reason = %q", r)
	}
	_, err = svc.CategoryUpdate(ctx, a, dto.CategoryUpdateInput{ID: "no such", Archived: &yes})
	if r := reasonOf(t, err, "category"); r != "not found" {
		t.Fatalf("reason = %q", r)
	}
}
