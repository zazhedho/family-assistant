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

var budgetCols = []string{"id", "space_id", "category_id", "month", "amount", "created_at", "updated_at", "deleted_at"}

func TestUpsertBudgetUsesPartialUniqueConflictAndMonthDate(t *testing.T) {
	db, mock := newMockDB(t)
	repo := NewRepository(db)
	// 2026-12-01 00:30 WIB is already December in Jakarta (UTC 17:30 on the 30th).
	b := &domainfinance.Budget{SpaceID: spaceID, CategoryID: categoryID, Month: time.Date(2026, 11, 30, 17, 30, 0, 0, time.UTC), Amount: 500000}
	wantMonth := time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC)

	mock.ExpectQuery(sqlRe(`INSERT INTO "finance_budgets"`, `ON CONFLICT ("space_id","category_id","month")`, `WHERE deleted_at IS NULL DO UPDATE SET`, `"amount"`, `RETURNING`)).
		WithArgs(sqlmock.AnyArg(), spaceID, categoryID, wantMonth, int64(500000), sqlmock.AnyArg(), sqlmock.AnyArg(), nilArg{}).
		WillReturnRows(sqlmock.NewRows(budgetCols).AddRow("00000000-0000-4000-8000-000000000801", spaceID, categoryID, wantMonth, 500000, fixedNow, fixedNow, nil))
	if err := repo.UpsertBudget(context.Background(), b); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	if !b.Month.Equal(wantMonth) || b.ID != "00000000-0000-4000-8000-000000000801" {
		t.Fatalf("budget not normalized/returned: %+v", b)
	}
	met(t, mock)
}

func TestListBudgetsFiltersSpaceAndMonthDate(t *testing.T) {
	db, mock := newMockDB(t)
	repo := NewRepository(db)
	month := time.Date(2026, 11, 30, 17, 30, 0, 0, time.UTC)

	mock.ExpectQuery(sqlRe(`FROM "finance_budgets" WHERE (space_id = $1 AND month = $2)`, `"finance_budgets"."deleted_at" IS NULL`)).
		WithArgs(spaceID, time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC)).
		WillReturnRows(sqlmock.NewRows(budgetCols))
	if _, err := repo.ListBudgets(context.Background(), spaceID, month); err != nil {
		t.Fatalf("list: %v", err)
	}
	met(t, mock)
}

func TestDeleteBudgetSoftDeletesInSpace(t *testing.T) {
	db, mock := newMockDB(t)
	repo := NewRepository(db)

	mock.ExpectExec(sqlRe(`UPDATE "finance_budgets" SET`, `"deleted_at"`, `WHERE (id = $`, `AND space_id = $`, `"deleted_at" IS NULL`)).
		WithArgs(fixedNow, fixedNow, "b1", spaceID).WillReturnResult(sqlmock.NewResult(0, 1))
	if err := repo.DeleteBudget(context.Background(), spaceID, "b1", fixedNow); err != nil {
		t.Fatalf("delete: %v", err)
	}
	mock.ExpectExec(sqlRe(`UPDATE "finance_budgets"`)).WillReturnResult(sqlmock.NewResult(0, 0))
	if err := repo.DeleteBudget(context.Background(), spaceID, "b1", fixedNow); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("err = %v, want ErrRecordNotFound", err)
	}
	met(t, mock)
}
