package servicefinance

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	domainaudit "family-assistant/internal/domain/audit"
	domainfinance "family-assistant/internal/domain/finance"
	domainspace "family-assistant/internal/domain/space"
	interfacefinance "family-assistant/internal/interfaces/finance"
)

var schedNow = time.Date(2026, 11, 10, 3, 0, 0, 0, time.UTC)

// postFake models PostDueRecurring: one mutex stands in for the row locks, so
// overlapping calls are serialized like two transactions on the same rows,
// and each call posts and advances before releasing it.
type postFake struct {
	interfacefinance.RepoFinanceInterface
	mu    sync.Mutex
	recs  map[string]*domainfinance.Recurring
	txs   []domainfinance.Transaction
	calls atomic.Int32
	panic bool
}

func (f *postFake) PostDueRecurring(_ context.Context, now time.Time, limit, maxPer int, build domainfinance.RecurringBuildFunc) (domainfinance.PostResult, error) {
	f.calls.Add(1)
	if f.panic {
		panic("boom")
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	res := domainfinance.PostResult{Dropped: map[string]int{}}
	for _, rec := range f.recs {
		if limit == 0 {
			break
		}
		if rec.Paused || rec.NextRunAt.After(now) {
			continue
		}
		limit--
		var staged []domainfinance.Transaction
		occ, err := rec.NextRunAt, error(nil)
		for len(staged) < maxPer && !occ.After(now) && err == nil {
			var tx domainfinance.Transaction
			if tx, err = build(*rec, occ); err == nil {
				staged = append(staged, tx)
				occ = domainfinance.NextOccurrence(*rec, occ)
			}
		}
		issue := domainfinance.RecurringIssue{RecurringID: rec.ID, SpaceID: rec.SpaceID, MemberID: rec.CreatedByMemberID}
		var pauseErr *domainfinance.PauseError
		switch {
		case errors.As(err, &pauseErr):
			rec.Paused, issue.Reason = true, pauseErr.Reason
			res.Paused = append(res.Paused, issue)
			continue
		case err != nil:
			issue.Reason = domainfinance.SkipTransient
			res.Skipped = append(res.Skipped, issue)
			continue
		}
		for !occ.After(now) {
			res.Dropped[rec.ID]++
			occ = domainfinance.NextOccurrence(*rec, occ)
		}
		rec.NextRunAt = occ
		for _, tx := range staged {
			f.txs = append(f.txs, tx)
			res.Posted = append(res.Posted, domainfinance.PostedOccurrence{
				RecurringID: rec.ID, SpaceID: rec.SpaceID, TransactionID: tx.ID, MemberID: tx.CreatedByMemberID, OccurredAt: tx.OccurredAt,
			})
		}
	}
	return res, nil
}

type membersFake struct {
	members []domainspace.ResolvedMembership
	err     error
}

func (m *membersFake) ListActiveMembers(_ context.Context, id string) ([]domainspace.ResolvedMembership, error) {
	if m.err != nil {
		return nil, m.err
	}
	var out []domainspace.ResolvedMembership
	for _, mem := range m.members {
		if mem.SpaceID == id {
			out = append(out, mem)
		}
	}
	return out, nil
}

type failingAudit struct{ calls atomic.Int32 }

func (a *failingAudit) Store(context.Context, domainaudit.AuditEvent) error {
	a.calls.Add(1)
	return errors.New("audit down")
}

func financeMembers(category string) *membersFake {
	return &membersFake{members: []domainspace.ResolvedMembership{
		{ID: memberID, SpaceID: spaceID, UserID: "user-1", SpaceCategory: category, Status: domainspace.StatusActive},
	}}
}

func dailyTemplate(id string, next time.Time) *domainfinance.Recurring {
	return &domainfinance.Recurring{
		ID: id, SpaceID: spaceID, Type: domainfinance.TypeExpense, Amount: 50000, AccountID: acctID, CategoryID: ptr(catFood),
		Merchant: "PLN", Frequency: domainfinance.FreqDaily, IntervalN: 1, StartAt: next, NextRunAt: next, CreatedByMemberID: memberID,
	}
}

func newSched(repo *postFake, members *membersFake, audit auditStore) *Scheduler {
	return NewScheduler(repo, members, audit, time.Minute, func() time.Time { return schedNow })
}

func TestTickPostsDueOccurrence(t *testing.T) {
	repo := &postFake{recs: map[string]*domainfinance.Recurring{recID: dailyTemplate(recID, schedNow)}}
	audit := &auditFake{}
	if err := newSched(repo, financeMembers(domainspace.CategoryFinance), audit).Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(repo.txs) != 1 {
		t.Fatalf("txs = %d", len(repo.txs))
	}
	tx := repo.txs[0]
	if tx.ID == "" || tx.SpaceID != spaceID || tx.Type != "expense" || tx.Amount != 50000 || tx.AccountID != acctID ||
		tx.CategoryID == nil || *tx.CategoryID != catFood || tx.Merchant != "PLN" || !tx.OccurredAt.Equal(schedNow) ||
		tx.CreatedByMemberID != memberID {
		t.Fatalf("tx = %+v", tx)
	}
	if next := repo.recs[recID].NextRunAt; !next.Equal(schedNow.Add(24 * time.Hour)) {
		t.Fatalf("next = %v", next)
	}
	if len(audit.events) != 1 {
		t.Fatalf("events = %+v", audit.events)
	}
	ev := audit.events[0]
	if ev.Source != "scheduler" || ev.ActorMemberID != memberID || ev.ActorUserID != "user-1" || ev.Action != domainaudit.ActionCreate ||
		ev.Resource != "finance_transaction" || ev.ResourceID != tx.ID || ev.Status != domainaudit.StatusSuccess ||
		ev.Metadata["space_id"] != spaceID || ev.Metadata["recurring_id"] != recID {
		t.Fatalf("event = %+v", ev)
	}
	for k := range ev.Metadata {
		if k != "space_id" && k != "recurring_id" {
			t.Fatalf("audit must carry ids only, got %q", k)
		}
	}
}

func TestTickCatchesUpAtMostTwelve(t *testing.T) {
	repo := &postFake{recs: map[string]*domainfinance.Recurring{recID: dailyTemplate(recID, schedNow.Add(-19*24*time.Hour))}}
	audit := &auditFake{}
	if err := newSched(repo, financeMembers(domainspace.CategoryFinance), audit).Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(repo.txs) != MaxCatchUp || MaxCatchUp != 12 {
		t.Fatalf("posted %d, cap %d", len(repo.txs), MaxCatchUp)
	}
	if next := repo.recs[recID].NextRunAt; !next.After(schedNow) {
		t.Fatalf("not fast-forwarded: %v", next)
	}
	var dropped any
	for _, ev := range audit.events {
		if ev.Resource == "finance_recurring" && ev.ResourceID == recID {
			dropped = ev.Metadata["dropped"]
		}
	}
	if dropped != 8 {
		t.Fatalf("dropped audit = %v, events %+v", dropped, audit.events)
	}
}

func TestTickSkipsPaused(t *testing.T) {
	rec := dailyTemplate(recID, schedNow.Add(-time.Hour))
	rec.Paused = true
	repo := &postFake{recs: map[string]*domainfinance.Recurring{recID: rec}}
	if err := newSched(repo, financeMembers(domainspace.CategoryFinance), &auditFake{}).Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(repo.txs) != 0 || !repo.recs[recID].NextRunAt.Equal(schedNow.Add(-time.Hour)) {
		t.Fatalf("paused template posted: %+v", repo.txs)
	}
}

func TestOverlappingTicksDoNotDoublePost(t *testing.T) {
	repo := &postFake{recs: map[string]*domainfinance.Recurring{
		recID:      dailyTemplate(recID, schedNow.Add(-2*24*time.Hour)),
		recForeign: dailyTemplate(recForeign, schedNow),
	}}
	members := financeMembers(domainspace.CategoryFinance)
	a, b := newSched(repo, members, &auditFake{}), newSched(repo, members, &auditFake{})
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		s := a
		if i%2 == 1 {
			s = b
		}
		go func() {
			defer wg.Done()
			if err := s.Tick(context.Background()); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if len(repo.txs) != 4 {
		t.Fatalf("posted %d, want 4 (3 + 1)", len(repo.txs))
	}
	ids := map[string]bool{}
	for _, tx := range repo.txs {
		if ids[tx.ID] {
			t.Fatalf("duplicate transaction id %s", tx.ID)
		}
		ids[tx.ID] = true
	}
	// Each template's occurrence is posted exactly once across both schedulers.
	perOccurrence := map[time.Time]int{}
	for _, tx := range repo.txs {
		perOccurrence[tx.OccurredAt]++
	}
	if perOccurrence[schedNow] != 2 || perOccurrence[schedNow.Add(-24*time.Hour)] != 1 || perOccurrence[schedNow.Add(-48*time.Hour)] != 1 {
		t.Fatalf("occurrences = %v", perOccurrence)
	}
}

func TestTickPausesWhenCreatorOrSpaceIsNoLongerValid(t *testing.T) {
	cases := map[string]struct {
		members *membersFake
		reason  string
	}{
		"creator left":     {&membersFake{members: []domainspace.ResolvedMembership{{ID: otherMember, SpaceID: spaceID, SpaceCategory: domainspace.CategoryFinance, Status: domainspace.StatusActive}}}, "member_invalid"},
		"not finance":      {financeMembers(domainspace.CategoryFamily), "space_invalid"},
		"space gone/empty": {&membersFake{}, "space_invalid"},
	}
	for name, c := range cases {
		repo := &postFake{recs: map[string]*domainfinance.Recurring{recID: dailyTemplate(recID, schedNow)}}
		audit := &auditFake{}
		if err := newSched(repo, c.members, audit).Tick(context.Background()); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if len(repo.txs) != 0 || !repo.recs[recID].Paused {
			t.Fatalf("%s: txs=%d paused=%v", name, len(repo.txs), repo.recs[recID].Paused)
		}
		if len(audit.events) != 1 || audit.events[0].Resource != "finance_recurring" || audit.events[0].ResourceID != recID ||
			audit.events[0].Status != domainaudit.StatusFailed || audit.events[0].ErrorMessage != c.reason || audit.events[0].Source != "scheduler" {
			t.Fatalf("%s: events = %+v", name, audit.events)
		}
	}
}

func TestTickTransientMembersErrorRetriesLater(t *testing.T) {
	repo := &postFake{recs: map[string]*domainfinance.Recurring{recID: dailyTemplate(recID, schedNow)}}
	if err := newSched(repo, &membersFake{err: errors.New("db timeout")}, &auditFake{}).Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(repo.txs) != 0 || repo.recs[recID].Paused || !repo.recs[recID].NextRunAt.Equal(schedNow) {
		t.Fatal("a transient failure must neither post nor pause nor advance")
	}
}

func TestTickAuditFailureDoesNotUndoPosting(t *testing.T) {
	repo := &postFake{recs: map[string]*domainfinance.Recurring{recID: dailyTemplate(recID, schedNow)}}
	audit := &failingAudit{}
	if err := newSched(repo, financeMembers(domainspace.CategoryFinance), audit).Tick(context.Background()); err != nil {
		t.Fatalf("audit failure must not fail the tick: %v", err)
	}
	if len(repo.txs) != 1 || audit.calls.Load() != 1 {
		t.Fatalf("txs=%d audit calls=%d", len(repo.txs), audit.calls.Load())
	}
}

func TestTickUnconfiguredIsAnError(t *testing.T) {
	if err := (*Scheduler)(nil).Tick(context.Background()); err == nil {
		t.Fatal("nil scheduler")
	}
	if err := NewScheduler(nil, nil, nil, 0, nil).Tick(context.Background()); err == nil {
		t.Fatal("missing dependencies")
	}
}

func TestRunRecoversFromPanicAndStopsOnCancel(t *testing.T) {
	repo := &postFake{panic: true, recs: map[string]*domainfinance.Recurring{}}
	s := NewScheduler(repo, financeMembers(domainspace.CategoryFinance), &auditFake{}, 5*time.Millisecond, nil)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		s.Run(ctx)
		close(done)
	}()
	deadline := time.After(2 * time.Second)
	for repo.calls.Load() < 3 {
		select {
		case <-deadline:
			t.Fatalf("ticks after panic: %d", repo.calls.Load())
		case <-time.After(time.Millisecond):
		}
	}
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not stop on cancel")
	}
}

func TestRunReturnsAtOnceOnCanceledContext(t *testing.T) {
	repo := &postFake{recs: map[string]*domainfinance.Recurring{}}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	NewScheduler(repo, financeMembers(domainspace.CategoryFinance), &auditFake{}, time.Hour, nil).Run(ctx)
	if repo.calls.Load() != 0 {
		t.Fatal("a canceled scheduler must not tick")
	}
}
