package servicefinance

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	domainaudit "family-assistant/internal/domain/audit"
	domainfinance "family-assistant/internal/domain/finance"
	domainspace "family-assistant/internal/domain/space"
	"family-assistant/internal/dto"
	"family-assistant/internal/services/authorization"
	"gorm.io/gorm"
)

const (
	recID      = "f0f0f0f0-f0f0-4f0f-8f0f-f0f0f0f0f0f0"
	recForeign = "f1f1f1f1-f1f1-4f1f-8f1f-f1f1f1f1f1f1"
	acctOld    = "a0a0a0a0-a0a0-4a0a-8a0a-a0a0a0a0a0a0"
)

// recRepo adds recurring storage to txRepo. UpdateRecurring enforces the
// updated_at version like the real repository.
type recRepo struct {
	*txRepo
	mu3          sync.Mutex
	recs         map[string]*domainfinance.Recurring
	beforeUpdate func() // runs between the service's load and its write
	updates      int
}

func (r *recRepo) CreateRecurring(_ context.Context, rec *domainfinance.Recurring) error {
	r.mu3.Lock()
	defer r.mu3.Unlock()
	c := *rec
	r.recs[rec.ID] = &c
	return nil
}

func (r *recRepo) FindRecurring(_ context.Context, space, id string) (*domainfinance.Recurring, error) {
	r.mu3.Lock()
	defer r.mu3.Unlock()
	rec, ok := r.recs[id]
	if !ok || rec.SpaceID != space {
		return nil, gorm.ErrRecordNotFound
	}
	c := *rec
	return &c, nil
}

func (r *recRepo) ListRecurring(_ context.Context, space string) ([]domainfinance.Recurring, error) {
	r.mu3.Lock()
	defer r.mu3.Unlock()
	var out []domainfinance.Recurring
	for _, rec := range r.recs {
		if rec.SpaceID == space {
			out = append(out, *rec)
		}
	}
	return out, nil
}

func (r *recRepo) UpdateRecurring(_ context.Context, rec *domainfinance.Recurring, expected time.Time) error {
	if r.beforeUpdate != nil {
		r.beforeUpdate()
	}
	r.mu3.Lock()
	defer r.mu3.Unlock()
	cur, ok := r.recs[rec.ID]
	if !ok || cur.SpaceID != rec.SpaceID || !cur.UpdatedAt.Equal(expected) {
		return gorm.ErrRecordNotFound
	}
	c := *rec
	c.CreatedByMemberID, c.CreatedAt = cur.CreatedByMemberID, cur.CreatedAt
	c.UpdatedAt = expected.Add(time.Second)
	r.recs[rec.ID] = &c
	r.updates++
	return nil
}

func (r *recRepo) DeleteRecurring(_ context.Context, space, id string, _ time.Time) error {
	r.mu3.Lock()
	defer r.mu3.Unlock()
	rec, ok := r.recs[id]
	if !ok || rec.SpaceID != space {
		return gorm.ErrRecordNotFound
	}
	delete(r.recs, id)
	return nil
}

type recEnv struct {
	*txEnv
	recs *recRepo
	now  time.Time
}

func newRecEnv(t *testing.T) *recEnv {
	t.Helper()
	base := newTxEnv(t)
	repo := &recRepo{txRepo: base.repo, recs: map[string]*domainfinance.Recurring{}}
	now := time.Date(2026, 11, 10, 3, 0, 0, 0, time.UTC)
	svc := NewService(repo, financeSpaces(domainspace.CategoryFinance), authorization.NewAuthorizer(), base.audit).(*service)
	svc.now = func() time.Time { return now }
	base.svc = svc
	return &recEnv{txEnv: base, recs: repo, now: now}
}

func monthlyBill(start time.Time) dto.RecurringCreateInput {
	return dto.RecurringCreateInput{
		Type: "expense", Amount: 350000, Account: "kas", Category: "makan", Merchant: " PLN ", Note: "listrik",
		Frequency: " Monthly ", StartAt: start,
	}
}

func (e *recEnv) createRec(t *testing.T, in dto.RecurringCreateInput) *domainfinance.Recurring {
	t.Helper()
	rec, err := e.svc.RecurringCreate(context.Background(), ownerActor(), in)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	return rec
}

func TestRecurringCreateStoresTemplateWithDefaults(t *testing.T) {
	e := newRecEnv(t)
	start := time.Date(2026, 11, 30, 17, 30, 0, 0, time.UTC) // 1 Dec 00:30 WIB
	rec := e.createRec(t, monthlyBill(start))
	stored := e.recs.recs[rec.ID]
	if stored == nil {
		t.Fatal("not stored")
	}
	if stored.SpaceID != spaceID || stored.Type != "expense" || stored.Amount != 350000 || stored.AccountID != acctID ||
		stored.CategoryID == nil || *stored.CategoryID != catFood || stored.Merchant != "PLN" || stored.Note != "listrik" ||
		stored.Frequency != "monthly" || stored.IntervalN != 1 || stored.DayOfMonth == nil || *stored.DayOfMonth != 1 ||
		!stored.StartAt.Equal(start) || !stored.NextRunAt.Equal(start) || stored.Paused || stored.CreatedByMemberID != memberID {
		t.Fatalf("stored = %+v", stored)
	}
	var found bool
	for _, ev := range e.audit.events {
		if ev.Resource == "finance_recurring" && ev.Action == domainaudit.ActionCreate && ev.Status == domainaudit.StatusSuccess && ev.ResourceID == rec.ID {
			found = true
		}
	}
	if !found {
		t.Fatalf("no create audit: %+v", e.audit.events)
	}
}

func TestRecurringCreateDailyHasNoDayOfMonth(t *testing.T) {
	e := newRecEnv(t)
	in := monthlyBill(e.now)
	in.Frequency, in.IntervalN = "daily", 3
	rec := e.createRec(t, in)
	if rec.DayOfMonth != nil || rec.IntervalN != 3 {
		t.Fatalf("rec = %+v", rec)
	}
}

func TestRecurringCreateValidatesSchedule(t *testing.T) {
	e := newRecEnv(t)
	before := e.now.Add(-time.Hour)
	cases := map[string]struct {
		mut   func(in *dto.RecurringCreateInput)
		field string
	}{
		"frequency":         {func(in *dto.RecurringCreateInput) { in.Frequency = "yearly" }, "frequency"},
		"interval monthly":  {func(in *dto.RecurringCreateInput) { in.IntervalN = 13 }, "interval_n"},
		"interval negative": {func(in *dto.RecurringCreateInput) { in.IntervalN = -1 }, "interval_n"},
		"day 32":            {func(in *dto.RecurringCreateInput) { in.DayOfMonth = ptr(32) }, "day_of_month"},
		"day on daily":      {func(in *dto.RecurringCreateInput) { in.Frequency, in.DayOfMonth = "daily", ptr(5) }, "day_of_month"},
		"no start":          {func(in *dto.RecurringCreateInput) { in.StartAt = time.Time{} }, "start_at"},
		"end before start":  {func(in *dto.RecurringCreateInput) { in.EndAt = &before }, "end_at"},
		"amount":            {func(in *dto.RecurringCreateInput) { in.Amount = 0 }, "amount"},
		"type":              {func(in *dto.RecurringCreateInput) { in.Type = "gift" }, "type"},
		"transfer category": {func(in *dto.RecurringCreateInput) { in.Type, in.ToAccount = "transfer", "Bank BCA" }, "category"},
	}
	for name, c := range cases {
		in := monthlyBill(e.now)
		c.mut(&in)
		_, err := e.svc.RecurringCreate(context.Background(), ownerActor(), in)
		var ve *authorization.ValidationError
		if !errors.As(err, &ve) || ve.Field != c.field {
			t.Errorf("%s: err = %v, want field %q", name, err, c.field)
		}
	}
	if len(e.recs.recs) != 0 {
		t.Fatalf("rejected creates wrote %d rows", len(e.recs.recs))
	}
}

func TestRecurringCreateRejectsArchivedForeignAndMismatchedRefs(t *testing.T) {
	e := newRecEnv(t)
	archived := e.now
	e.repo.accounts[acctOld] = &domainfinance.Account{ID: acctOld, SpaceID: spaceID, Name: "Lama", Type: "cash", ArchivedAt: &archived}
	in := monthlyBill(e.now)
	in.Account = acctOld
	if r := reasonOf(t, errOf(e.svc.RecurringCreate(context.Background(), ownerActor(), in)), "account"); r != "is archived" {
		t.Fatalf("archived reason %q", r)
	}
	in.Account = foreignAcct
	if r := reasonOf(t, errOf(e.svc.RecurringCreate(context.Background(), ownerActor(), in)), "account"); r != "not found" {
		t.Fatalf("foreign reason %q", r)
	}
	in = monthlyBill(e.now)
	in.Category = foreignCat
	reasonOf(t, errOf(e.svc.RecurringCreate(context.Background(), ownerActor(), in)), "category")
	in.Category = catSalary // income category on an expense
	reasonOf(t, errOf(e.svc.RecurringCreate(context.Background(), ownerActor(), in)), "category")
	if len(e.recs.recs) != 0 {
		t.Fatal("nothing may be written")
	}
}

func errOf[T any](_ T, err error) error { return err }

func TestRecurringPermissions(t *testing.T) {
	e := newRecEnv(t)
	ctx := context.Background()
	rec := e.createRec(t, monthlyBill(e.now))
	member := memberActor()
	if _, err := e.svc.RecurringCreate(ctx, member, monthlyBill(e.now)); !errors.Is(err, authorization.ErrForbidden) {
		t.Fatalf("member create err = %v", err)
	}
	if _, err := e.svc.RecurringUpdate(ctx, member, dto.RecurringUpdateInput{ID: rec.ID, Paused: ptr(true)}); !errors.Is(err, authorization.ErrForbidden) {
		t.Fatalf("member update err = %v", err)
	}
	if err := e.svc.RecurringDelete(ctx, member, dto.RecurringDeleteInput{ID: rec.ID}); !errors.Is(err, authorization.ErrForbidden) {
		t.Fatalf("member delete err = %v", err)
	}
	if got, err := e.svc.RecurringList(ctx, actorWith("finance:list"), dto.RecurringListInput{}); err != nil || len(got) != 1 {
		t.Fatalf("viewer list: %v %v", got, err)
	}
	if _, err := e.svc.RecurringList(ctx, actorWith("finance:manage"), dto.RecurringListInput{}); !errors.Is(err, authorization.ErrForbidden) {
		t.Fatalf("list without finance:list err = %v", err)
	}
	if len(e.recs.recs) != 1 || e.recs.updates != 0 {
		t.Fatal("forbidden calls changed the store")
	}

	family := newRecEnv(t)
	svc := NewService(family.recs, financeSpaces(domainspace.CategoryFamily), authorization.NewAuthorizer(), family.audit)
	if _, err := svc.RecurringCreate(ctx, ownerActor(), monthlyBill(e.now)); !errors.Is(err, authorization.ErrNotFound) {
		t.Fatalf("non-finance space err = %v", err)
	}
}

func TestRecurringPauseKeepsNextRunAndFutureResumeKeepsIt(t *testing.T) {
	e := newRecEnv(t)
	ctx := context.Background()
	in := monthlyBill(e.now)
	in.DayOfMonth = ptr(25) // next run 25 Nov, in the future
	rec := e.createRec(t, in)
	got, err := e.svc.RecurringUpdate(ctx, ownerActor(), dto.RecurringUpdateInput{ID: rec.ID, Paused: ptr(true)})
	if err != nil || !got.Paused || !e.recs.recs[rec.ID].Paused || !got.NextRunAt.Equal(rec.NextRunAt) {
		t.Fatalf("pause: %+v %v", got, err)
	}
	got, err = e.svc.RecurringUpdate(ctx, ownerActor(), dto.RecurringUpdateInput{ID: rec.ID, Paused: ptr(false)})
	if err != nil || got.Paused || !got.NextRunAt.Equal(rec.NextRunAt) {
		t.Fatalf("resume: %+v %v", got, err)
	}
}

// Resuming a template paused long ago does not back-post the missed months:
// next_run_at moves to the first occurrence at or after now, and the
// scheduler's failure state is cleared.
func TestRecurringResumeFastForwardsAndClearsFailureState(t *testing.T) {
	e := newRecEnv(t)
	ctx := context.Background()
	in := monthlyBill(e.now)
	in.DayOfMonth = ptr(10)
	rec := e.createRec(t, in)
	stored := e.recs.recs[rec.ID]
	stored.Paused = true
	stored.NextRunAt = time.Date(2026, 5, 10, 3, 0, 0, 0, time.UTC) // paused since May
	stored.PauseReason = ptr(domainfinance.PauseRetryExhausted)
	stored.FailureCount = 5
	stored.LastFailureCode = ptr("SQLSTATE_40001")

	got, err := e.svc.RecurringUpdate(ctx, ownerActor(), dto.RecurringUpdateInput{ID: rec.ID, Paused: ptr(false)})
	if err != nil {
		t.Fatal(err)
	}
	// 10 Nov 10:00 WIB is exactly now: due now, not again in December.
	if !got.NextRunAt.Equal(e.now) || got.Paused || got.PauseReason != nil || got.FailureCount != 0 || got.LastFailureCode != nil {
		t.Fatalf("resumed = %+v", got)
	}
	// An edit that does not unpause keeps the failure state.
	stored = e.recs.recs[rec.ID]
	stored.FailureCount, stored.LastFailureCode = 2, ptr("GO_x")
	got, err = e.svc.RecurringUpdate(ctx, ownerActor(), dto.RecurringUpdateInput{ID: rec.ID, Note: ptr("x")})
	if err != nil || got.FailureCount != 2 || got.LastFailureCode == nil {
		t.Fatalf("plain edit: %+v %v", got, err)
	}
}

func TestRecurringCreateMonthlyFirstRunIsOnDayOfMonth(t *testing.T) {
	e := newRecEnv(t)
	in := monthlyBill(e.now) // 10 Nov 10:00 WIB
	in.DayOfMonth = ptr(25)
	rec := e.createRec(t, in)
	if want := time.Date(2026, 11, 25, 3, 0, 0, 0, time.UTC); !rec.NextRunAt.Equal(want) || !rec.StartAt.Equal(e.now) {
		t.Fatalf("next = %v want %v", rec.NextRunAt, want)
	}
}

func TestRecurringCreateRejectsStartMoreThanADayInThePast(t *testing.T) {
	e := newRecEnv(t)
	if _, err := e.svc.RecurringCreate(context.Background(), ownerActor(), monthlyBill(e.now.Add(-24*time.Hour))); err != nil {
		t.Fatalf("exactly 24h back must be accepted: %v", err)
	}
	_, err := e.svc.RecurringCreate(context.Background(), ownerActor(), monthlyBill(e.now.Add(-24*time.Hour-time.Second)))
	if r := reasonOf(t, err, "start_at"); r != "is too far in the past" {
		t.Fatalf("reason %q", r)
	}
	if len(e.recs.recs) != 1 {
		t.Fatalf("rows = %d", len(e.recs.recs))
	}
}

func TestRecurringUpdateRevalidatesMergedTemplate(t *testing.T) {
	e := newRecEnv(t)
	ctx := context.Background()
	end := e.now.Add(90 * 24 * time.Hour)
	in := monthlyBill(e.now)
	in.EndAt = &end
	rec := e.createRec(t, in)
	a := ownerActor()

	_, err := e.svc.RecurringUpdate(ctx, a, dto.RecurringUpdateInput{ID: rec.ID, IntervalN: ptr(13)})
	vErr(t, err, "interval_n")
	_, err = e.svc.RecurringUpdate(ctx, a, dto.RecurringUpdateInput{ID: rec.ID, Frequency: ptr("weekly"), DayOfMonth: ptr(3)})
	vErr(t, err, "day_of_month")
	early := e.now.Add(-time.Hour)
	_, err = e.svc.RecurringUpdate(ctx, a, dto.RecurringUpdateInput{ID: rec.ID, EndAt: &early})
	vErr(t, err, "end_at")
	_, err = e.svc.RecurringUpdate(ctx, a, dto.RecurringUpdateInput{ID: rec.ID, Type: ptr("income")})
	vErr(t, err, "category")
	_, err = e.svc.RecurringUpdate(ctx, a, dto.RecurringUpdateInput{ID: rec.ID})
	vErr(t, err, "update")
	if e.recs.updates != 0 {
		t.Fatal("rejected updates wrote")
	}

	// Switching to weekly drops the monthly day; ClearEndAt removes the end.
	got, err := e.svc.RecurringUpdate(ctx, a, dto.RecurringUpdateInput{ID: rec.ID, Frequency: ptr("weekly"), ClearEndAt: true, Amount: ptr(int64(400000))})
	if err != nil || got.Frequency != "weekly" || got.DayOfMonth != nil || got.EndAt != nil || got.Amount != 400000 {
		t.Fatalf("update: %+v %v", got, err)
	}
	// Type and category change together, re-resolved by kind.
	got, err = e.svc.RecurringUpdate(ctx, a, dto.RecurringUpdateInput{ID: rec.ID, Type: ptr("income"), Category: ptr("gaji")})
	if err != nil || got.Type != "income" || got.CategoryID == nil || *got.CategoryID != catSalary {
		t.Fatalf("type change: %+v %v", got, err)
	}
}

// A schedule change never moves next_run_at before the occurrence that was
// due next, so nothing already posted is posted again.
func TestRecurringScheduleChangeNeverRepostsBeforeNextRun(t *testing.T) {
	e := newRecEnv(t)
	ctx := context.Background()
	day := 24 * time.Hour
	start := e.now.Add(-10 * day)
	in := monthlyBill(e.now)
	in.Frequency = "daily"
	rec := e.createRec(t, in)
	// A template started 10 days ago; the scheduler has posted through yesterday.
	stored := e.recs.recs[rec.ID]
	stored.StartAt, stored.NextRunAt = start, e.now

	got, err := e.svc.RecurringUpdate(ctx, ownerActor(), dto.RecurringUpdateInput{ID: rec.ID, IntervalN: ptr(3)})
	if err != nil {
		t.Fatal(err)
	}
	// Chained from start every 3 days: start+9d < now, start+12d is next.
	if want := start.Add(12 * day); !got.NextRunAt.Equal(want) {
		t.Fatalf("next = %v want %v", got.NextRunAt, want)
	}
	later := e.now.Add(30 * day)
	got, err = e.svc.RecurringUpdate(ctx, ownerActor(), dto.RecurringUpdateInput{ID: rec.ID, StartAt: &later})
	if err != nil || !got.NextRunAt.Equal(later) {
		t.Fatalf("future start: %v %v", got.NextRunAt, err)
	}
	// Moving the start back into the past does not reach behind the old next run.
	got, err = e.svc.RecurringUpdate(ctx, ownerActor(), dto.RecurringUpdateInput{ID: rec.ID, StartAt: &start})
	if err != nil || got.NextRunAt.Before(later) {
		t.Fatalf("past start: %v %v", got.NextRunAt, err)
	}
	// A non-schedule edit keeps next_run_at.
	got, err = e.svc.RecurringUpdate(ctx, ownerActor(), dto.RecurringUpdateInput{ID: rec.ID, Note: ptr("baru")})
	if err != nil || !got.NextRunAt.Equal(e.recs.recs[rec.ID].NextRunAt) || got.Note != "baru" {
		t.Fatalf("note: %+v %v", got, err)
	}
}

// Moving a monthly template to another day re-chains from the first
// occurrence on that day, not from StartAt's own date.
func TestRecurringDayOfMonthChangeUsesFirstOccurrence(t *testing.T) {
	e := newRecEnv(t)
	rec := e.createRec(t, monthlyBill(e.now)) // day 10, next run now
	got, err := e.svc.RecurringUpdate(context.Background(), ownerActor(), dto.RecurringUpdateInput{ID: rec.ID, DayOfMonth: ptr(25)})
	if err != nil {
		t.Fatal(err)
	}
	if want := time.Date(2026, 11, 25, 3, 0, 0, 0, time.UTC); !got.NextRunAt.Equal(want) {
		t.Fatalf("next = %v want %v", got.NextRunAt, want)
	}
}

func TestRecurringUpdateConflictsWhenSchedulerAdvancedMeanwhile(t *testing.T) {
	e := newRecEnv(t)
	rec := e.createRec(t, monthlyBill(e.now))
	e.recs.beforeUpdate = func() {
		e.recs.mu3.Lock()
		defer e.recs.mu3.Unlock()
		cur := e.recs.recs[rec.ID]
		cur.NextRunAt = cur.NextRunAt.Add(31 * 24 * time.Hour)
		cur.UpdatedAt = cur.UpdatedAt.Add(time.Minute)
	}
	_, err := e.svc.RecurringUpdate(context.Background(), ownerActor(), dto.RecurringUpdateInput{ID: rec.ID, Note: ptr("x")})
	if r := reasonOf(t, err, "id"); r == "" {
		t.Fatal("empty reason")
	}
	if e.recs.recs[rec.ID].Note != "listrik" {
		t.Fatal("stale write applied")
	}
}

func TestRecurringDeleteAndScope(t *testing.T) {
	e := newRecEnv(t)
	ctx := context.Background()
	rec := e.createRec(t, monthlyBill(e.now))
	e.recs.recs[recForeign] = &domainfinance.Recurring{ID: recForeign, SpaceID: foreign}
	if err := e.svc.RecurringDelete(ctx, ownerActor(), dto.RecurringDeleteInput{ID: recForeign}); !errors.Is(err, authorization.ErrNotFound) {
		t.Fatalf("foreign delete err = %v", err)
	}
	if _, err := e.svc.RecurringUpdate(ctx, ownerActor(), dto.RecurringUpdateInput{ID: recForeign, Paused: ptr(true)}); !errors.Is(err, authorization.ErrNotFound) {
		t.Fatalf("foreign update err = %v", err)
	}
	vErr(t, e.svc.RecurringDelete(ctx, ownerActor(), dto.RecurringDeleteInput{ID: "nope"}), "id")
	if err := e.svc.RecurringDelete(ctx, ownerActor(), dto.RecurringDeleteInput{ID: rec.ID}); err != nil {
		t.Fatal(err)
	}
	if _, ok := e.recs.recs[rec.ID]; ok || e.recs.recs[recForeign] == nil {
		t.Fatal("wrong row deleted")
	}
}

func TestRecurringListClampsAndScopes(t *testing.T) {
	e := newRecEnv(t)
	for i := 0; i < 120; i++ {
		id := fmt.Sprintf("r-%03d", i)
		e.recs.recs[id] = &domainfinance.Recurring{ID: id, SpaceID: spaceID, NextRunAt: e.now.Add(time.Duration(i) * time.Minute)}
	}
	e.recs.recs[recForeign] = &domainfinance.Recurring{ID: recForeign, SpaceID: foreign}
	a := actorWith("finance:list")
	for limit, want := range map[int]int{0: 50, 10: 10, 500: 100} {
		got, err := e.svc.RecurringList(context.Background(), a, dto.RecurringListInput{Limit: limit})
		if err != nil || len(got) != want {
			t.Fatalf("limit %d: %d %v", limit, len(got), err)
		}
		for _, r := range got {
			if r.SpaceID != spaceID {
				t.Fatal("foreign row listed")
			}
		}
	}
	_, err := e.svc.RecurringList(context.Background(), a, dto.RecurringListInput{Limit: -1})
	vErr(t, err, "limit")
}

func TestMapDomainErrorRecurringSentinels(t *testing.T) {
	cases := map[error]string{
		domainfinance.ErrInvalidFrequency:     "frequency",
		domainfinance.ErrIntervalRange:        "interval_n",
		domainfinance.ErrDayOfMonthRange:      "day_of_month",
		domainfinance.ErrDayOfMonthNotMonthly: "day_of_month",
		domainfinance.ErrStartRequired:        "start_at",
		domainfinance.ErrEndBeforeStart:       "end_at",
	}
	for in, field := range cases {
		vErr(t, mapDomainError(in), field)
	}
}
