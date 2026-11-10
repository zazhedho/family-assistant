package repositoryfinance

import (
	"context"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestBalancesSumsInSQLScopedToSpace(t *testing.T) {
	db, mock := newMockDB(t)
	repo := NewRepository(db)

	mock.ExpectQuery(sqlRe(`opening_balance + COALESCE(SUM(CASE`, `t.deleted_at IS NULL`, `WHERE a.space_id = $1 AND a.deleted_at IS NULL`, `GROUP BY`)).
		WithArgs(spaceID).
		WillReturnRows(sqlmock.NewRows([]string{"account_id", "name", "type", "balance"}).
			AddRow(accountID, "BCA", "bank", int64(975000)).AddRow(account2ID, "Cash", "cash", int64(-5)))
	got, err := repo.Balances(context.Background(), spaceID)
	if err != nil || len(got) != 2 || got[0].Balance != 975000 || got[1].Balance != -5 {
		t.Fatalf("balances: %v %+v", err, got)
	}
	met(t, mock)
}

func TestSummaryAggregatesInSQLWithHalfOpenRange(t *testing.T) {
	db, mock := newMockDB(t)
	repo := NewRepository(db)
	from, to := time.Date(2026, 10, 31, 17, 0, 0, 0, time.UTC), time.Date(2026, 11, 30, 17, 0, 0, 0, time.UTC)

	// Totals: transfers are not in income or expense.
	mock.ExpectQuery(sqlRe(`SUM(amount) FILTER (WHERE type = 'income')`, `SUM(amount) FILTER (WHERE type = 'expense')`,
		`FROM finance_transactions WHERE space_id = $1 AND deleted_at IS NULL AND occurred_at >= $2 AND occurred_at < $3`)).
		WithArgs(spaceID, from, to).
		WillReturnRows(sqlmock.NewRows([]string{"income", "expense"}).AddRow(int64(900), int64(400)))
	// By category: expenses only.
	mock.ExpectQuery(sqlRe(`SUM(t.amount)`, `t.space_id = $1 AND t.deleted_at IS NULL AND t.type = 'expense'`, `t.occurred_at >= $2 AND t.occurred_at < $3`, `GROUP BY`, `ORDER BY total DESC, name`)).
		WithArgs(spaceID, from, to).
		WillReturnRows(sqlmock.NewRows([]string{"category_id", "name", "total"}).AddRow(categoryID, "Food", int64(300)).AddRow("", "", int64(100)))
	// By account: income and expense, never transfers.
	mock.ExpectQuery(sqlRe(`t.type IN ('income', 'expense')`, `t.space_id = $1 AND t.deleted_at IS NULL`, `t.occurred_at >= $2 AND t.occurred_at < $3`, `GROUP BY`)).
		WithArgs(spaceID, from, to).
		WillReturnRows(sqlmock.NewRows([]string{"account_id", "name", "income", "expense"}).AddRow(accountID, "BCA", int64(900), int64(400)))
	// Top merchants: expenses, non-empty merchant, top 5.
	mock.ExpectQuery(sqlRe(`space_id = $1 AND deleted_at IS NULL AND type = 'expense' AND COALESCE(merchant, '') <> ''`, `occurred_at >= $2 AND occurred_at < $3`,
		`GROUP BY merchant ORDER BY total DESC, merchant LIMIT 5`)).
		WithArgs(spaceID, from, to).
		WillReturnRows(sqlmock.NewRows([]string{"merchant", "total", "count"}).AddRow("Kopi", int64(250), 3))

	got, err := repo.Summary(context.Background(), spaceID, from, to)
	if err != nil {
		t.Fatalf("summary: %v", err)
	}
	if got.Income != 900 || got.Expense != 400 || len(got.ByCategory) != 2 || got.ByCategory[0].Total != 300 ||
		len(got.ByAccount) != 1 || got.ByAccount[0].Expense != 400 || len(got.TopMerchants) != 1 || got.TopMerchants[0].Count != 3 {
		t.Fatalf("summary = %+v", got)
	}
	met(t, mock)
}

func TestSummaryEmptyResultHasEmptySlices(t *testing.T) {
	db, mock := newMockDB(t)
	repo := NewRepository(db)
	mock.ExpectQuery(sqlRe(`FROM finance_transactions`)).WillReturnRows(sqlmock.NewRows([]string{"income", "expense"}).AddRow(int64(0), int64(0)))
	mock.ExpectQuery(sqlRe(`GROUP BY`)).WillReturnRows(sqlmock.NewRows([]string{"category_id", "name", "total"}))
	mock.ExpectQuery(sqlRe(`GROUP BY`)).WillReturnRows(sqlmock.NewRows([]string{"account_id", "name", "income", "expense"}))
	mock.ExpectQuery(sqlRe(`GROUP BY`)).WillReturnRows(sqlmock.NewRows([]string{"merchant", "total", "count"}))
	got, err := repo.Summary(context.Background(), spaceID, fixedNow, fixedNow.Add(time.Hour))
	if err != nil || got.ByCategory == nil || got.ByAccount == nil || got.TopMerchants == nil {
		t.Fatalf("summary: %v %+v", err, got)
	}
	met(t, mock)
}

func TestSpentByCategoryCountsOnlyLiveExpensesInSpace(t *testing.T) {
	db, mock := newMockDB(t)
	repo := NewRepository(db)
	from, to := fixedNow, fixedNow.Add(time.Hour)

	mock.ExpectQuery(sqlRe(`SUM(amount)`, `space_id = $1 AND deleted_at IS NULL AND type = 'expense' AND category_id = $2`, `occurred_at >= $3 AND occurred_at < $4`)).
		WithArgs(spaceID, categoryID, from, to).
		WillReturnRows(sqlmock.NewRows([]string{"total"}).AddRow(int64(123)))
	got, err := repo.SpentByCategory(context.Background(), spaceID, categoryID, from, to)
	if err != nil || got != 123 {
		t.Fatalf("spent: %v %d", err, got)
	}
	met(t, mock)
}

func TestReportsRequireSpace(t *testing.T) {
	db, _ := newMockDB(t)
	repo := NewRepository(db)
	if _, err := repo.Balances(context.Background(), " "); err == nil {
		t.Error("Balances: expected error")
	}
	if _, err := repo.Summary(context.Background(), "", fixedNow, fixedNow); err == nil {
		t.Error("Summary: expected error")
	}
	if _, err := repo.SpentByCategory(context.Background(), "", categoryID, fixedNow, fixedNow); err == nil {
		t.Error("SpentByCategory: expected error")
	}
}
