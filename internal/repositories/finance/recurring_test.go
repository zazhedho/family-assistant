package repositoryfinance

import (
	"context"
	"errors"
	"testing"
	"time"

	domainfinance "family-assistant/internal/domain/finance"

	"github.com/DATA-DOG/go-sqlmock"
	"gorm.io/gorm"
)

const recurringID = "00000000-0000-4000-8000-000000000901"

var recurringCols = []string{"id", "space_id", "type", "amount", "account_id", "to_account_id", "category_id", "merchant", "note",
	"frequency", "interval_n", "day_of_month", "start_at", "next_run_at", "end_at", "paused", "created_by_member_id", "created_at", "updated_at", "deleted_at"}

func recurringFixture() *domainfinance.Recurring {
	return &domainfinance.Recurring{
		ID: recurringID, SpaceID: spaceID, Type: domainfinance.TypeExpense, Amount: 100000, AccountID: accountID,
		CategoryID: strp(categoryID), Frequency: domainfinance.FreqMonthly, IntervalN: 1,
		StartAt: fixedNow, NextRunAt: fixedNow, CreatedByMemberID: memberID, CreatedAt: fixedNow, UpdatedAt: fixedNow,
	}
}

func TestCreateRecurringInsertsRow(t *testing.T) {
	db, mock := newMockDB(t)
	repo := NewRepository(db)
	r := recurringFixture()
	r.ID = ""

	mock.ExpectExec(sqlRe(`INSERT INTO "finance_recurring"`)).WillReturnResult(sqlmock.NewResult(0, 1))
	if err := repo.CreateRecurring(context.Background(), r); err != nil {
		t.Fatalf("create: %v", err)
	}
	if r.ID == "" {
		t.Fatal("expected generated id")
	}
	met(t, mock)
}

func TestFindAndListRecurringScopeSpace(t *testing.T) {
	db, mock := newMockDB(t)
	repo := NewRepository(db)

	mock.ExpectQuery(sqlRe(`WHERE (space_id = $1 AND id = $2)`, `"finance_recurring"."deleted_at" IS NULL`)).
		WithArgs(spaceID, recurringID, 1).WillReturnRows(sqlmock.NewRows(recurringCols))
	if _, err := repo.FindRecurring(context.Background(), spaceID, recurringID); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("err = %v, want ErrRecordNotFound", err)
	}
	mock.ExpectQuery(sqlRe(`WHERE space_id = $1`, `"finance_recurring"."deleted_at" IS NULL`, `ORDER BY next_run_at`)).
		WithArgs(spaceID).WillReturnRows(sqlmock.NewRows(recurringCols))
	if _, err := repo.ListRecurring(context.Background(), spaceID); err != nil {
		t.Fatalf("list: %v", err)
	}
	met(t, mock)
}

func TestUpdateRecurringClearsNullableColumns(t *testing.T) {
	db, mock := newMockDB(t)
	repo := NewRepository(db)
	r := recurringFixture()
	r.CategoryID = nil

	mock.ExpectExec(sqlRe(`UPDATE "finance_recurring" SET`, `"category_id"=$`, `"day_of_month"=$`, `"end_at"=$`, `WHERE (id = $`, `AND space_id = $`, `"deleted_at" IS NULL`)).
		WillReturnResult(sqlmock.NewResult(0, 1))
	if err := repo.UpdateRecurring(context.Background(), r); err != nil {
		t.Fatalf("update: %v", err)
	}
	mock.ExpectExec(sqlRe(`UPDATE "finance_recurring"`)).WillReturnResult(sqlmock.NewResult(0, 0))
	if err := repo.UpdateRecurring(context.Background(), r); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("err = %v, want ErrRecordNotFound", err)
	}
	met(t, mock)
}

func TestDeleteRecurringSoftDeletesInSpace(t *testing.T) {
	db, mock := newMockDB(t)
	repo := NewRepository(db)

	mock.ExpectExec(sqlRe(`UPDATE "finance_recurring" SET`, `"deleted_at"`, `WHERE (id = $`, `AND space_id = $`, `"deleted_at" IS NULL`)).
		WithArgs(fixedNow, fixedNow, recurringID, spaceID).WillReturnResult(sqlmock.NewResult(0, 1))
	if err := repo.DeleteRecurring(context.Background(), spaceID, recurringID, fixedNow); err != nil {
		t.Fatalf("delete: %v", err)
	}
	met(t, mock)
}

func TestClaimDueRecurringUsesSkipLocked(t *testing.T) {
	db, mock := newMockDB(t)
	repo := NewRepository(db)
	r := recurringFixture()
	r.StartAt = time.Date(2026, 10, 10, 3, 0, 0, 0, time.UTC)
	r.NextRunAt = r.StartAt
	day := 10
	r.DayOfMonth = &day
	wantNext := time.Date(2026, 11, 10, 3, 0, 0, 0, time.UTC)

	mock.ExpectBegin()
	mock.ExpectQuery(sqlRe(`FROM "finance_recurring"`, `paused = $1 AND next_run_at <= $2 AND (end_at IS NULL OR next_run_at <= end_at)`,
		`"finance_recurring"."deleted_at" IS NULL`, `ORDER BY next_run_at, id LIMIT $3`, `FOR UPDATE SKIP LOCKED`)).
		WithArgs(false, fixedNow, 5).
		WillReturnRows(sqlmock.NewRows(recurringCols).AddRow(r.ID, spaceID, "expense", 100000, accountID, nil, categoryID, nil, nil,
			"monthly", 1, 10, r.StartAt, r.NextRunAt, nil, false, memberID, fixedNow, fixedNow, nil))
	mock.ExpectExec(sqlRe(`UPDATE "finance_recurring" SET`, `"next_run_at"`, `WHERE (id = $`, `AND space_id = $`)).
		WithArgs(wantNext, fixedNow, recurringID, spaceID).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	got, err := repo.ClaimDueRecurring(context.Background(), fixedNow, 5)
	if err != nil || len(got) != 1 {
		t.Fatalf("claim: %v %+v", err, got)
	}
	// The caller posts the occurrence that was due, not the advanced one.
	if !got[0].NextRunAt.Equal(r.StartAt) {
		t.Fatalf("returned NextRunAt = %v, want pre-advance %v", got[0].NextRunAt, r.StartAt)
	}
	met(t, mock)
}

func TestClaimDueRecurringRollsBackWhenAdvanceFails(t *testing.T) {
	db, mock := newMockDB(t)
	repo := NewRepository(db)
	r := recurringFixture()

	mock.ExpectBegin()
	mock.ExpectQuery(sqlRe(`FOR UPDATE SKIP LOCKED`)).
		WillReturnRows(sqlmock.NewRows(recurringCols).AddRow(r.ID, spaceID, "expense", 100000, accountID, nil, categoryID, nil, nil,
			"monthly", 1, nil, r.StartAt, r.NextRunAt, nil, false, memberID, fixedNow, fixedNow, nil))
	mock.ExpectExec(sqlRe(`UPDATE "finance_recurring"`)).WillReturnError(errors.New("boom"))
	mock.ExpectRollback()

	got, err := repo.ClaimDueRecurring(context.Background(), fixedNow, 5)
	if err == nil || got != nil {
		t.Fatalf("expected error and no rows, got %v %+v", err, got)
	}
	met(t, mock)
}

func TestClaimDueRecurringNonPositiveLimitClaimsNothing(t *testing.T) {
	db, _ := newMockDB(t)
	got, err := NewRepository(db).ClaimDueRecurring(context.Background(), fixedNow, 0)
	if err != nil || len(got) != 0 {
		t.Fatalf("got %v %+v", err, got)
	}
}
