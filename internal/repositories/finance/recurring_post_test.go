package repositoryfinance

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	domainfinance "family-assistant/internal/domain/finance"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/jackc/pgx/v5/pgconn"
)

const (
	recurring2ID = "00000000-0000-4000-8000-000000000902"
	day          = 24 * time.Hour
)

// dailyRow adds an expense template row whose next occurrence is next, with
// the given frequency (for the invalid-template case) and failure count.
func dailyRow(rows *sqlmock.Rows, id string, next time.Time, endAt any, frequency string, failures int) *sqlmock.Rows {
	return rows.AddRow(id, spaceID, "expense", 50000, accountID, nil, categoryID, "PLN", nil,
		frequency, 1, nil, next, next, endAt, false, memberID, fixedNow, fixedNow, nil, nil, failures, nil)
}

func oneDaily(next time.Time, failures int) *sqlmock.Rows {
	return dailyRow(sqlmock.NewRows(recurringCols), recurringID, next, nil, "daily", failures)
}

// expectLockedDue expects the locking select. Templates that keep failing
// sort last (failure_count first), so they cannot starve healthy ones.
func expectLockedDue(mock sqlmock.Sqlmock, limit int, rows *sqlmock.Rows) {
	mock.ExpectBegin()
	mock.ExpectQuery(sqlRe(`SELECT * FROM "finance_recurring"`,
		`paused = $1 AND next_run_at <= $2 AND (end_at IS NULL OR next_run_at <= end_at)`,
		`"finance_recurring"."deleted_at" IS NULL`, `ORDER BY failure_count, next_run_at, id LIMIT $3`, `FOR UPDATE SKIP LOCKED`)).
		WithArgs(false, fixedNow, limit).WillReturnRows(rows)
}

func expectSavepoint(mock sqlmock.Sqlmock) {
	mock.ExpectExec(sqlRe(`SAVEPOINT recurring_post`)).WillReturnResult(sqlmock.NewResult(0, 0))
}

func expectRelease(mock sqlmock.Sqlmock) {
	mock.ExpectExec(sqlRe(`RELEASE SAVEPOINT recurring_post`)).WillReturnResult(sqlmock.NewResult(0, 0))
}

func expectRollbackToSavepoint(mock sqlmock.Sqlmock) {
	mock.ExpectExec(sqlRe(`ROLLBACK TO SAVEPOINT recurring_post`)).WillReturnResult(sqlmock.NewResult(0, 0))
}

// expectReferences answers the account and category lookups of an expense
// template; archivedAccount marks the account archived.
func expectReferences(mock sqlmock.Sqlmock, archivedAccount any) {
	mock.ExpectQuery(sqlRe(`SELECT * FROM "finance_accounts"`, `space_id = $1 AND id IN ($2)`, `"finance_accounts"."deleted_at" IS NULL`)).
		WithArgs(spaceID, accountID).
		WillReturnRows(sqlmock.NewRows(accountCols).AddRow(accountID, spaceID, "Kas", "cash", 0, archivedAccount, fixedNow, fixedNow, nil))
	mock.ExpectQuery(sqlRe(`SELECT * FROM "finance_categories"`, `space_id = $1 AND id = $2`, `"finance_categories"."deleted_at" IS NULL`)).
		WithArgs(spaceID, categoryID).
		WillReturnRows(sqlmock.NewRows(categoryCols).AddRow(categoryID, spaceID, "Tagihan", "expense", nil, fixedNow, fixedNow, nil))
}

// expectInsertFor expects the posting of one occurrence of template id,
// tagged with recurring_id and idempotent on (recurring_id, occurred_at).
func expectInsertFor(mock sqlmock.Sqlmock, id string, occurredAt time.Time, rows int64) {
	mock.ExpectExec(sqlRe(`INSERT INTO "finance_transactions"`, `"recurring_id"`,
		`ON CONFLICT ("recurring_id","occurred_at") WHERE recurring_id IS NOT NULL DO NOTHING`)).
		WithArgs(sqlmock.AnyArg(), spaceID, "expense", int64(50000), accountID, nilArg{}, categoryID, "PLN", "", occurredAt, memberID,
			sqlmock.AnyArg(), sqlmock.AnyArg(), nilArg{}, id).
		WillReturnResult(sqlmock.NewResult(0, rows))
}

func expectInsert(mock sqlmock.Sqlmock, occurredAt time.Time) {
	expectInsertFor(mock, recurringID, occurredAt, 1)
}

// expectAdvance expects the advance of a successful run, which also resets
// the failure state.
func expectAdvance(mock sqlmock.Sqlmock, id string, next time.Time) {
	mock.ExpectExec(sqlRe(`UPDATE "finance_recurring" SET "failure_count"=$1,"last_failure_code"=$2,"next_run_at"=$3,"updated_at"=$4`,
		`WHERE (id = $5 AND space_id = $6)`, `"deleted_at" IS NULL`)).
		WithArgs(0, nilArg{}, next, fixedNow, id, spaceID).WillReturnResult(sqlmock.NewResult(0, 1))
}

func expectPause(mock sqlmock.Sqlmock, id, reason string) {
	mock.ExpectExec(sqlRe(`UPDATE "finance_recurring" SET "pause_reason"=$1,"paused"=$2,"updated_at"=$3`, `WHERE (id = $4 AND space_id = $5)`, `"deleted_at" IS NULL`)).
		WithArgs(reason, true, fixedNow, id, spaceID).WillReturnResult(sqlmock.NewResult(0, 1))
}

// templateBuild is the scheduler's build without the Space/member checks.
func templateBuild(r domainfinance.Recurring, occurrence time.Time) (domainfinance.Transaction, error) {
	return r.TransactionAt(occurrence, "", fixedNow), nil
}

func transientBuild(domainfinance.Recurring, time.Time) (domainfinance.Transaction, error) {
	return domainfinance.Transaction{}, errors.New("members lookup failed for password=hunter2")
}

func TestPostDueRecurringPostsEveryDueOccurrenceAndAdvancesInOneTransaction(t *testing.T) {
	db, mock := newMockDB(t)
	repo := &Repository{DB: db}
	first := fixedNow.Add(-2 * day)

	// Lock, post and advance all happen between one BEGIN and COMMIT, and the
	// select takes the row locks with SKIP LOCKED: a second scheduler sees no
	// row until this one commits, and then sees the advanced next_run_at. A
	// template that had failed before (failure_count 3) is reset on success.
	expectLockedDue(mock, 100, oneDaily(first, 3))
	expectSavepoint(mock)
	expectReferences(mock, nil)
	expectInsert(mock, first)
	expectInsert(mock, first.Add(day))
	expectInsert(mock, fixedNow)
	expectAdvance(mock, recurringID, fixedNow.Add(day))
	expectRelease(mock)
	mock.ExpectCommit()

	res, err := repo.PostDueRecurring(context.Background(), fixedNow, 100, 12, templateBuild)
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	if len(res.Posted) != 3 || len(res.Paused) != 0 || len(res.Skipped) != 0 || res.Dropped[recurringID] != 0 {
		t.Fatalf("result = %+v", res)
	}
	for i, p := range res.Posted {
		if want := first.Add(time.Duration(i) * day); !p.OccurredAt.Equal(want) || p.RecurringID != recurringID ||
			p.SpaceID != spaceID || p.MemberID != memberID || p.TransactionID == "" {
			t.Errorf("posted[%d] = %+v", i, p)
		}
	}
	met(t, mock)
}

func TestPostDueRecurringCapsCatchUpAndFastForwards(t *testing.T) {
	db, mock := newMockDB(t)
	repo := &Repository{DB: db}
	first := fixedNow.Add(-19 * day) // 20 occurrences due, the last one at now

	expectLockedDue(mock, 100, oneDaily(first, 0))
	expectSavepoint(mock)
	expectReferences(mock, nil)
	for i := 0; i < 12; i++ {
		expectInsert(mock, first.Add(time.Duration(i)*day))
	}
	// The 8 missed occurrences after the cap are dropped, not posted later.
	expectAdvance(mock, recurringID, fixedNow.Add(day))
	expectRelease(mock)
	mock.ExpectCommit()

	res, err := repo.PostDueRecurring(context.Background(), fixedNow, 100, 12, templateBuild)
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	if len(res.Posted) != 12 || res.Dropped[recurringID] != 8 {
		t.Fatalf("posted=%d dropped=%d", len(res.Posted), res.Dropped[recurringID])
	}
	met(t, mock)
}

func TestPostDueRecurringStopsAtEndAt(t *testing.T) {
	db, mock := newMockDB(t)
	repo := &Repository{DB: db}
	first := fixedNow.Add(-3 * day)
	end := fixedNow.Add(-day)

	expectLockedDue(mock, 100, dailyRow(sqlmock.NewRows(recurringCols), recurringID, first, end, "daily", 0))
	expectSavepoint(mock)
	expectReferences(mock, nil)
	expectInsert(mock, first)
	expectInsert(mock, first.Add(day))
	expectInsert(mock, end)
	// next_run_at moves past end_at, so the template is never due again.
	expectAdvance(mock, recurringID, fixedNow)
	expectRelease(mock)
	mock.ExpectCommit()

	res, err := repo.PostDueRecurring(context.Background(), fixedNow, 100, 12, templateBuild)
	if err != nil || len(res.Posted) != 3 || res.Dropped[recurringID] != 0 {
		t.Fatalf("res=%+v err=%v", res, err)
	}
	met(t, mock)
}

func TestPostDueRecurringSkipsAnOccurrenceThatWasAlreadyPosted(t *testing.T) {
	db, mock := newMockDB(t)
	repo := &Repository{DB: db}
	first := fixedNow.Add(-day)

	// The unique (recurring_id, occurred_at) index already holds the first
	// occurrence: the insert affects no row, the occurrence is passed over
	// and the template is neither paused nor rolled back.
	expectLockedDue(mock, 100, oneDaily(first, 0))
	expectSavepoint(mock)
	expectReferences(mock, nil)
	expectInsertFor(mock, recurringID, first, 0)
	expectInsert(mock, fixedNow)
	expectAdvance(mock, recurringID, fixedNow.Add(day))
	expectRelease(mock)
	mock.ExpectCommit()

	res, err := repo.PostDueRecurring(context.Background(), fixedNow, 100, 12, templateBuild)
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	if len(res.Posted) != 1 || !res.Posted[0].OccurredAt.Equal(fixedNow) || res.AlreadyPosted != 1 || len(res.Paused) != 0 {
		t.Fatalf("res = %+v", res)
	}
	met(t, mock)
}

func TestPostDueRecurringPausesFailedTemplateWithoutAbortingOthers(t *testing.T) {
	db, mock := newMockDB(t)
	repo := &Repository{DB: db}
	rows := sqlmock.NewRows(recurringCols)
	dailyRow(rows, recurringID, fixedNow, nil, "daily", 0)
	dailyRow(rows, recurring2ID, fixedNow, nil, "daily", 0)

	expectLockedDue(mock, 100, rows)
	expectSavepoint(mock)
	expectReferences(mock, nil)
	expectRollbackToSavepoint(mock)
	expectPause(mock, recurringID, "member_invalid")
	expectSavepoint(mock)
	expectReferences(mock, nil)
	expectInsertFor(mock, recurring2ID, fixedNow, 1)
	expectAdvance(mock, recurring2ID, fixedNow.Add(day))
	expectRelease(mock)
	mock.ExpectCommit()

	build := func(r domainfinance.Recurring, occ time.Time) (domainfinance.Transaction, error) {
		if r.ID == recurringID {
			return domainfinance.Transaction{}, domainfinance.Pause(domainfinance.PauseMemberInvalid)
		}
		return templateBuild(r, occ)
	}
	res, err := repo.PostDueRecurring(context.Background(), fixedNow, 100, 12, build)
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	if len(res.Paused) != 1 || res.Paused[0].RecurringID != recurringID || res.Paused[0].Reason != "member_invalid" ||
		res.Paused[0].MemberID != memberID || res.Paused[0].SpaceID != spaceID {
		t.Fatalf("paused = %+v", res.Paused)
	}
	if len(res.Posted) != 1 || res.Posted[0].RecurringID != recurring2ID {
		t.Fatalf("posted = %+v", res.Posted)
	}
	met(t, mock)
}

// expectPausedRun runs a single-template tick that ends in a pause with reason.
func expectPausedRun(t *testing.T, build domainfinance.RecurringBuildFunc, frequency string, archived any, lookups bool, reason string) {
	t.Helper()
	db, mock := newMockDB(t)
	repo := &Repository{DB: db}
	expectLockedDue(mock, 100, dailyRow(sqlmock.NewRows(recurringCols), recurringID, fixedNow, nil, frequency, 0))
	expectSavepoint(mock)
	if lookups {
		expectReferences(mock, archived)
	}
	expectRollbackToSavepoint(mock)
	expectPause(mock, recurringID, reason)
	mock.ExpectCommit()

	res, err := repo.PostDueRecurring(context.Background(), fixedNow, 100, 12, build)
	if err != nil || len(res.Posted) != 0 || len(res.Paused) != 1 || res.Paused[0].Reason != reason {
		t.Fatalf("res=%+v err=%v", res, err)
	}
	met(t, mock)
}

func TestPostDueRecurringPausesArchivedReference(t *testing.T) {
	expectPausedRun(t, templateBuild, "daily", fixedNow.Add(-day), true, domainfinance.PauseReferenceInvalid)
}

func TestPostDueRecurringPausesInvalidTemplateBeforeAnyLookup(t *testing.T) {
	// yearly passes the table CHECK but is not a schedule the service offers.
	expectPausedRun(t, templateBuild, "yearly", nil, false, domainfinance.PauseTemplateInvalid)
}

func TestPostDueRecurringPausesWhenBuiltTransactionIsInvalid(t *testing.T) {
	foreignSpace := func(r domainfinance.Recurring, occ time.Time) (domainfinance.Transaction, error) {
		tx := r.TransactionAt(occ, "", fixedNow)
		tx.SpaceID = "00000000-0000-4000-8000-000000000999"
		return tx, nil
	}
	expectPausedRun(t, foreignSpace, "daily", nil, true, domainfinance.PauseValidationFailed)
}

func TestPostDueRecurringPausesOnRejectedInsertAndUndoesItsEarlierOccurrences(t *testing.T) {
	db, mock := newMockDB(t)
	repo := &Repository{DB: db}
	first := fixedNow.Add(-day)

	expectLockedDue(mock, 100, oneDaily(first, 0))
	expectSavepoint(mock)
	expectReferences(mock, nil)
	expectInsert(mock, first)
	mock.ExpectExec(sqlRe(`INSERT INTO "finance_transactions"`)).WillReturnError(&pgconn.PgError{Code: "23503"})
	// The savepoint rollback also undoes the first occurrence of the template.
	expectRollbackToSavepoint(mock)
	expectPause(mock, recurringID, domainfinance.PauseInsertRejected)
	mock.ExpectCommit()

	res, err := repo.PostDueRecurring(context.Background(), fixedNow, 100, 12, templateBuild)
	if err != nil || len(res.Posted) != 0 || len(res.Paused) != 1 || res.Paused[0].Reason != domainfinance.PauseInsertRejected ||
		res.Paused[0].Code != "SQLSTATE_23503" {
		t.Fatalf("res=%+v err=%v", res, err)
	}
	met(t, mock)
}

func TestPostDueRecurringSkipsTransientFailureAndRecordsSanitizedCode(t *testing.T) {
	db, mock := newMockDB(t)
	repo := &Repository{DB: db}

	expectLockedDue(mock, 100, oneDaily(fixedNow, 1))
	expectSavepoint(mock)
	expectReferences(mock, nil)
	expectRollbackToSavepoint(mock)
	// Recorded in the outer transaction after the rollback: the count goes
	// up and only an error class is kept, never the message.
	mock.ExpectExec(sqlRe(`UPDATE "finance_recurring" SET "failure_count"=$1,"last_failure_code"=$2,"updated_at"=$3`, `WHERE (id = $4 AND space_id = $5)`)).
		WithArgs(2, "GO_errors.errorString", fixedNow, recurringID, spaceID).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	res, err := repo.PostDueRecurring(context.Background(), fixedNow, 100, 12, transientBuild)
	if err != nil || len(res.Paused) != 0 || len(res.Skipped) != 1 || res.Skipped[0].Reason != domainfinance.SkipTransient ||
		res.Skipped[0].Code != "GO_errors.errorString" {
		t.Fatalf("res=%+v err=%v", res, err)
	}
	met(t, mock)
}

func TestPostDueRecurringTransientInsertErrorRecordsSQLState(t *testing.T) {
	db, mock := newMockDB(t)
	repo := &Repository{DB: db}

	expectLockedDue(mock, 100, oneDaily(fixedNow, 0))
	expectSavepoint(mock)
	expectReferences(mock, nil)
	mock.ExpectExec(sqlRe(`INSERT INTO "finance_transactions"`)).WillReturnError(&pgconn.PgError{Code: "40001", Message: "secret detail"})
	expectRollbackToSavepoint(mock)
	mock.ExpectExec(sqlRe(`UPDATE "finance_recurring" SET "failure_count"=$1,"last_failure_code"=$2,"updated_at"=$3`)).
		WithArgs(1, "SQLSTATE_40001", fixedNow, recurringID, spaceID).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	res, err := repo.PostDueRecurring(context.Background(), fixedNow, 100, 12, templateBuild)
	if err != nil || len(res.Skipped) != 1 || res.Skipped[0].Code != "SQLSTATE_40001" {
		t.Fatalf("res=%+v err=%v", res, err)
	}
	met(t, mock)
}

func TestPostDueRecurringLastAllowedFailurePausesRetryExhausted(t *testing.T) {
	db, mock := newMockDB(t)
	repo := &Repository{DB: db}

	expectLockedDue(mock, 100, oneDaily(fixedNow, domainfinance.MaxConsecutiveFailures-1))
	expectSavepoint(mock)
	expectReferences(mock, nil)
	expectRollbackToSavepoint(mock)
	mock.ExpectExec(sqlRe(`UPDATE "finance_recurring" SET "failure_count"=$1,"last_failure_code"=$2,"pause_reason"=$3,"paused"=$4,"updated_at"=$5`,
		`WHERE (id = $6 AND space_id = $7)`)).
		WithArgs(domainfinance.MaxConsecutiveFailures, "GO_errors.errorString", "retry_exhausted", true, fixedNow, recurringID, spaceID).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	res, err := repo.PostDueRecurring(context.Background(), fixedNow, 100, 12, transientBuild)
	if err != nil || len(res.Skipped) != 0 || len(res.Paused) != 1 || res.Paused[0].Reason != domainfinance.PauseRetryExhausted ||
		res.Paused[0].Code != "GO_errors.errorString" {
		t.Fatalf("res=%+v err=%v", res, err)
	}
	met(t, mock)
}

func TestPostDueRecurringRollsBackEverythingOnDatabaseFailure(t *testing.T) {
	db, mock := newMockDB(t)
	repo := &Repository{DB: db}

	expectLockedDue(mock, 100, oneDaily(fixedNow, 0))
	expectSavepoint(mock)
	expectReferences(mock, nil)
	expectInsert(mock, fixedNow)
	mock.ExpectExec(sqlRe(`UPDATE "finance_recurring"`)).WillReturnError(errors.New("connection reset"))
	mock.ExpectRollback()

	res, err := repo.PostDueRecurring(context.Background(), fixedNow, 100, 12, templateBuild)
	if err == nil || len(res.Posted) != 0 {
		t.Fatalf("expected error and nothing reported as posted, got %+v %v", res, err)
	}
	met(t, mock)
}

func TestPostDueRecurringNonPositiveArgumentsDoNothing(t *testing.T) {
	db, mock := newMockDB(t)
	repo := &Repository{DB: db}
	for _, c := range []struct{ limit, max int }{{0, 12}, {100, 0}} {
		res, err := repo.PostDueRecurring(context.Background(), fixedNow, c.limit, c.max, templateBuild)
		if err != nil || len(res.Posted) != 0 {
			t.Fatalf("%+v: %+v %v", c, res, err)
		}
	}
	if _, err := repo.PostDueRecurring(context.Background(), fixedNow, 100, 12, nil); err == nil {
		t.Fatal("nil build must be an error")
	}
	met(t, mock)
}

type secretError struct{ dsn string }

func (e *secretError) Error() string { return "dial " + e.dsn }

func TestFailureCodeIsSanitized(t *testing.T) {
	cases := map[string]error{
		"SQLSTATE_42P01":        fmt.Errorf("wrapped: %w", &pgconn.PgError{Code: "42P01", Message: "relation x does not exist"}),
		"CONTEXT_CANCELED":      fmt.Errorf("query: %w", context.Canceled),
		"CONTEXT_DEADLINE":      context.DeadlineExceeded,
		"GO_errors.errorString": errors.New("password=hunter2"),
		"GO_repositoryfinance.": &secretError{dsn: "db.internal token=xyzzy"},
	}
	for want, err := range cases {
		got := failureCode(err)
		if !strings.HasPrefix(got, want) || len(got) > 32 || strings.Contains(got, "hunter2") || strings.Contains(got, "xyzzy") {
			t.Errorf("failureCode(%v) = %q, want prefix %q", err, got, want)
		}
	}
}
