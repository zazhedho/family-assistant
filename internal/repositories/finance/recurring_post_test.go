package repositoryfinance

import (
	"context"
	"errors"
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

// dailyRow adds an expense template row whose next occurrence is next;
// frequency is a parameter for the invalid-template case.
func dailyRow(rows *sqlmock.Rows, id string, next time.Time, endAt any, frequency string) *sqlmock.Rows {
	return rows.AddRow(id, spaceID, "expense", 50000, accountID, nil, categoryID, "PLN", nil,
		frequency, 1, nil, next, next, endAt, false, memberID, fixedNow, fixedNow, nil)
}

func expectLockedDue(mock sqlmock.Sqlmock, limit int, rows *sqlmock.Rows) {
	mock.ExpectBegin()
	mock.ExpectQuery(sqlRe(`SELECT * FROM "finance_recurring"`,
		`paused = $1 AND next_run_at <= $2 AND (end_at IS NULL OR next_run_at <= end_at)`,
		`"finance_recurring"."deleted_at" IS NULL`, `ORDER BY next_run_at, id LIMIT $3`, `FOR UPDATE SKIP LOCKED`)).
		WithArgs(false, fixedNow, limit).WillReturnRows(rows)
}

func expectSavepoint(mock sqlmock.Sqlmock) {
	mock.ExpectExec(sqlRe(`SAVEPOINT recurring_post`)).WillReturnResult(sqlmock.NewResult(0, 0))
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

func expectInsert(mock sqlmock.Sqlmock, occurredAt time.Time) {
	mock.ExpectExec(sqlRe(`INSERT INTO "finance_transactions"`)).
		WithArgs(sqlmock.AnyArg(), spaceID, "expense", int64(50000), accountID, nilArg{}, categoryID, "PLN", "", occurredAt, memberID,
			sqlmock.AnyArg(), sqlmock.AnyArg(), nilArg{}).
		WillReturnResult(sqlmock.NewResult(0, 1))
}

func expectAdvance(mock sqlmock.Sqlmock, id string, next time.Time) {
	mock.ExpectExec(sqlRe(`UPDATE "finance_recurring" SET "next_run_at"=$1,"updated_at"=$2`, `WHERE (id = $3 AND space_id = $4)`, `"deleted_at" IS NULL`)).
		WithArgs(next, fixedNow, id, spaceID).WillReturnResult(sqlmock.NewResult(0, 1))
}

func expectPause(mock sqlmock.Sqlmock, id string) {
	mock.ExpectExec(sqlRe(`UPDATE "finance_recurring" SET "paused"=$1,"updated_at"=$2`, `WHERE (id = $3 AND space_id = $4)`, `"deleted_at" IS NULL`)).
		WithArgs(true, fixedNow, id, spaceID).WillReturnResult(sqlmock.NewResult(0, 1))
}

// templateBuild is the scheduler's build without the Space/member checks.
func templateBuild(r domainfinance.Recurring, occurrence time.Time) (domainfinance.Transaction, error) {
	return r.TransactionAt(occurrence, "", fixedNow), nil
}

func TestPostDueRecurringPostsEveryDueOccurrenceAndAdvancesInOneTransaction(t *testing.T) {
	db, mock := newMockDB(t)
	repo := &Repository{DB: db}
	first := fixedNow.Add(-2 * day)

	// Lock, post and advance all happen between one BEGIN and COMMIT, and the
	// select takes the row locks with SKIP LOCKED: a second scheduler sees no
	// row until this one commits, and then sees the advanced next_run_at.
	expectLockedDue(mock, 100, dailyRow(sqlmock.NewRows(recurringCols), recurringID, first, nil, "daily"))
	expectSavepoint(mock)
	expectReferences(mock, nil)
	expectInsert(mock, first)
	expectInsert(mock, first.Add(day))
	expectInsert(mock, fixedNow)
	expectAdvance(mock, recurringID, fixedNow.Add(day))
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

	expectLockedDue(mock, 100, dailyRow(sqlmock.NewRows(recurringCols), recurringID, first, nil, "daily"))
	expectSavepoint(mock)
	expectReferences(mock, nil)
	for i := 0; i < 12; i++ {
		expectInsert(mock, first.Add(time.Duration(i)*day))
	}
	// The 8 missed occurrences after the cap are dropped, not posted later.
	expectAdvance(mock, recurringID, fixedNow.Add(day))
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

	expectLockedDue(mock, 100, dailyRow(sqlmock.NewRows(recurringCols), recurringID, first, end, "daily"))
	expectSavepoint(mock)
	expectReferences(mock, nil)
	expectInsert(mock, first)
	expectInsert(mock, first.Add(day))
	expectInsert(mock, end)
	// next_run_at moves past end_at, so the template is never due again.
	expectAdvance(mock, recurringID, fixedNow)
	mock.ExpectCommit()

	res, err := repo.PostDueRecurring(context.Background(), fixedNow, 100, 12, templateBuild)
	if err != nil || len(res.Posted) != 3 || res.Dropped[recurringID] != 0 {
		t.Fatalf("res=%+v err=%v", res, err)
	}
	met(t, mock)
}

func TestPostDueRecurringPausesFailedTemplateWithoutAbortingOthers(t *testing.T) {
	db, mock := newMockDB(t)
	repo := &Repository{DB: db}
	rows := sqlmock.NewRows(recurringCols)
	dailyRow(rows, recurringID, fixedNow, nil, "daily")
	dailyRow(rows, recurring2ID, fixedNow, nil, "daily")

	expectLockedDue(mock, 100, rows)
	expectSavepoint(mock)
	expectReferences(mock, nil)
	expectRollbackToSavepoint(mock)
	expectPause(mock, recurringID)
	expectSavepoint(mock)
	expectReferences(mock, nil)
	expectInsert(mock, fixedNow)
	expectAdvance(mock, recurring2ID, fixedNow.Add(day))
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

func TestPostDueRecurringPausesArchivedReference(t *testing.T) {
	db, mock := newMockDB(t)
	repo := &Repository{DB: db}

	expectLockedDue(mock, 100, dailyRow(sqlmock.NewRows(recurringCols), recurringID, fixedNow, nil, "daily"))
	expectSavepoint(mock)
	expectReferences(mock, fixedNow.Add(-day))
	expectRollbackToSavepoint(mock)
	expectPause(mock, recurringID)
	mock.ExpectCommit()

	res, err := repo.PostDueRecurring(context.Background(), fixedNow, 100, 12, templateBuild)
	if err != nil || len(res.Posted) != 0 || len(res.Paused) != 1 || res.Paused[0].Reason != domainfinance.PauseReferenceInvalid {
		t.Fatalf("res=%+v err=%v", res, err)
	}
	met(t, mock)
}

func TestPostDueRecurringPausesInvalidTemplateBeforeAnyLookup(t *testing.T) {
	db, mock := newMockDB(t)
	repo := &Repository{DB: db}

	// yearly passes the table CHECK but is not a schedule the service offers.
	expectLockedDue(mock, 100, dailyRow(sqlmock.NewRows(recurringCols), recurringID, fixedNow, nil, "yearly"))
	expectSavepoint(mock)
	expectRollbackToSavepoint(mock)
	expectPause(mock, recurringID)
	mock.ExpectCommit()

	res, err := repo.PostDueRecurring(context.Background(), fixedNow, 100, 12, templateBuild)
	if err != nil || len(res.Paused) != 1 || res.Paused[0].Reason != domainfinance.PauseTemplateInvalid {
		t.Fatalf("res=%+v err=%v", res, err)
	}
	met(t, mock)
}

func TestPostDueRecurringPausesWhenBuiltTransactionIsInvalid(t *testing.T) {
	db, mock := newMockDB(t)
	repo := &Repository{DB: db}

	expectLockedDue(mock, 100, dailyRow(sqlmock.NewRows(recurringCols), recurringID, fixedNow, nil, "daily"))
	expectSavepoint(mock)
	expectReferences(mock, nil)
	expectRollbackToSavepoint(mock)
	expectPause(mock, recurringID)
	mock.ExpectCommit()

	foreignSpace := func(r domainfinance.Recurring, occ time.Time) (domainfinance.Transaction, error) {
		tx := r.TransactionAt(occ, "", fixedNow)
		tx.SpaceID = "00000000-0000-4000-8000-000000000999"
		return tx, nil
	}
	res, err := repo.PostDueRecurring(context.Background(), fixedNow, 100, 12, foreignSpace)
	if err != nil || len(res.Paused) != 1 || res.Paused[0].Reason != domainfinance.PauseValidationFailed {
		t.Fatalf("res=%+v err=%v", res, err)
	}
	met(t, mock)
}

func TestPostDueRecurringPausesOnRejectedInsertAndUndoesItsEarlierOccurrences(t *testing.T) {
	db, mock := newMockDB(t)
	repo := &Repository{DB: db}
	first := fixedNow.Add(-day)

	expectLockedDue(mock, 100, dailyRow(sqlmock.NewRows(recurringCols), recurringID, first, nil, "daily"))
	expectSavepoint(mock)
	expectReferences(mock, nil)
	expectInsert(mock, first)
	mock.ExpectExec(sqlRe(`INSERT INTO "finance_transactions"`)).WillReturnError(&pgconn.PgError{Code: "23503"})
	// The savepoint rollback also undoes the first occurrence of the template.
	expectRollbackToSavepoint(mock)
	expectPause(mock, recurringID)
	mock.ExpectCommit()

	res, err := repo.PostDueRecurring(context.Background(), fixedNow, 100, 12, templateBuild)
	if err != nil || len(res.Posted) != 0 || len(res.Paused) != 1 || res.Paused[0].Reason != domainfinance.PauseInsertRejected {
		t.Fatalf("res=%+v err=%v", res, err)
	}
	met(t, mock)
}

func TestPostDueRecurringSkipsTransientBuildErrorWithoutPausing(t *testing.T) {
	db, mock := newMockDB(t)
	repo := &Repository{DB: db}

	expectLockedDue(mock, 100, dailyRow(sqlmock.NewRows(recurringCols), recurringID, fixedNow, nil, "daily"))
	expectSavepoint(mock)
	expectReferences(mock, nil)
	expectRollbackToSavepoint(mock)
	mock.ExpectCommit()

	transient := func(domainfinance.Recurring, time.Time) (domainfinance.Transaction, error) {
		return domainfinance.Transaction{}, errors.New("members lookup timed out")
	}
	res, err := repo.PostDueRecurring(context.Background(), fixedNow, 100, 12, transient)
	if err != nil || len(res.Paused) != 0 || len(res.Skipped) != 1 || res.Skipped[0].Reason != domainfinance.SkipTransient {
		t.Fatalf("res=%+v err=%v", res, err)
	}
	met(t, mock)
}

func TestPostDueRecurringRollsBackEverythingOnDatabaseFailure(t *testing.T) {
	db, mock := newMockDB(t)
	repo := &Repository{DB: db}

	expectLockedDue(mock, 100, dailyRow(sqlmock.NewRows(recurringCols), recurringID, fixedNow, nil, "daily"))
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
