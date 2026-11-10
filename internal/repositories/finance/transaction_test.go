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

var txCols = []string{"id", "space_id", "type", "amount", "account_id", "to_account_id", "category_id", "merchant", "note", "occurred_at", "created_by_member_id", "created_at", "updated_at", "deleted_at"}

func txFixture() *domainfinance.Transaction {
	return &domainfinance.Transaction{
		ID: txID, SpaceID: spaceID, Type: domainfinance.TypeExpense, Amount: 25000, AccountID: accountID,
		CategoryID: strp(categoryID), Merchant: "Kopi", OccurredAt: fixedNow, CreatedByMemberID: memberID,
		CreatedAt: fixedNow, UpdatedAt: fixedNow,
	}
}

func TestCreateTransactionInsertsRowAndNullableIDs(t *testing.T) {
	db, mock := newMockDB(t)
	repo := NewRepository(db)
	tx := txFixture()
	tx.ID = ""

	// nil ToAccountID is written as NULL (the 6th column).
	mock.ExpectExec(sqlRe(`INSERT INTO "finance_transactions"`)).
		WithArgs(sqlmock.AnyArg(), spaceID, "expense", int64(25000), accountID, nilArg{}, categoryID, "Kopi", "", fixedNow, memberID, fixedNow, fixedNow, nilArg{}, nilArg{}).
		WillReturnResult(sqlmock.NewResult(0, 1))
	if err := repo.CreateTransaction(context.Background(), tx); err != nil {
		t.Fatalf("create: %v", err)
	}
	if tx.ID == "" {
		t.Fatal("expected generated id")
	}
	met(t, mock)
}

func TestFindTransactionScopesSpaceAndSoftDelete(t *testing.T) {
	db, mock := newMockDB(t)
	repo := NewRepository(db)

	mock.ExpectQuery(sqlRe(`WHERE (space_id = $1 AND id = $2)`, `"finance_transactions"."deleted_at" IS NULL`)).
		WithArgs(spaceID, txID, 1).WillReturnRows(sqlmock.NewRows(txCols))
	if _, err := repo.FindTransaction(context.Background(), spaceID, txID); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("err = %v, want ErrRecordNotFound", err)
	}
	met(t, mock)
}

func TestListTransactionsAppliesEveryFilter(t *testing.T) {
	db, mock := newMockDB(t)
	repo := NewRepository(db)
	from, to := fixedNow.Add(-24*time.Hour), fixedNow

	mock.ExpectQuery(sqlRe(`FROM "finance_transactions" WHERE space_id = $1`, `type = $2`, `category_id = $3`,
		`(account_id = $4 OR to_account_id = $5)`, `occurred_at >= $6`, `occurred_at < $7`, `"finance_transactions"."deleted_at" IS NULL`,
		`ORDER BY occurred_at DESC, id DESC LIMIT $`)).
		WillReturnRows(sqlmock.NewRows(txCols))
	_, err := repo.ListTransactions(context.Background(), domainfinance.ListFilter{
		SpaceID: spaceID, Type: "expense", CategoryID: categoryID, AccountID: accountID, From: &from, To: &to, Limit: 10,
	})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	met(t, mock)
}

func TestListTransactionsSearchEscapesWildcards(t *testing.T) {
	db, mock := newMockDB(t)
	repo := NewRepository(db)

	mock.ExpectQuery(sqlRe(`WHERE space_id = $1`, `LOWER(merchant) LIKE LOWER($2) OR LOWER(note) LIKE LOWER($3)`)).
		WithArgs(spaceID, `%50\%%`, `%50\%%`).WillReturnRows(sqlmock.NewRows(txCols))
	if _, err := repo.ListTransactions(context.Background(), domainfinance.ListFilter{SpaceID: spaceID, Search: "50%"}); err != nil {
		t.Fatalf("list: %v", err)
	}
	met(t, mock)
}

func TestListTransactionsRequiresSpace(t *testing.T) {
	db, _ := newMockDB(t)
	if _, err := NewRepository(db).ListTransactions(context.Background(), domainfinance.ListFilter{}); err == nil {
		t.Fatal("expected error for empty space id")
	}
}

func TestUpdateTransactionClearsCategoryToNull(t *testing.T) {
	db, mock := newMockDB(t)
	repo := NewRepository(db)
	tx := txFixture()
	tx.CategoryID = nil
	tx.Type = domainfinance.TypeTransfer
	tx.ToAccountID = strp(account2ID)

	mock.ExpectExec(sqlRe(`UPDATE "finance_transactions" SET`, `"category_id"=$`, `WHERE (id = $`, `AND space_id = $`, `"deleted_at" IS NULL`)).
		WithArgs("transfer", int64(25000), accountID, account2ID, nilArg{}, "Kopi", "", fixedNow, sqlmock.AnyArg(), txID, spaceID).
		WillReturnResult(sqlmock.NewResult(0, 1))
	if err := repo.UpdateTransaction(context.Background(), tx); err != nil {
		t.Fatalf("update: %v", err)
	}

	// Clearing the transfer target on an expense also writes NULL.
	tx2 := txFixture()
	mock.ExpectExec(sqlRe(`UPDATE "finance_transactions" SET`)).
		WithArgs("expense", int64(25000), accountID, nilArg{}, categoryID, "Kopi", "", fixedNow, sqlmock.AnyArg(), txID, spaceID).
		WillReturnResult(sqlmock.NewResult(0, 1))
	if err := repo.UpdateTransaction(context.Background(), tx2); err != nil {
		t.Fatalf("update 2: %v", err)
	}

	mock.ExpectExec(sqlRe(`UPDATE "finance_transactions"`)).WillReturnResult(sqlmock.NewResult(0, 0))
	if err := repo.UpdateTransaction(context.Background(), tx2); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("err = %v, want ErrRecordNotFound", err)
	}
	met(t, mock)
}

func TestSoftDeleteTransactionIsSpaceScoped(t *testing.T) {
	db, mock := newMockDB(t)
	repo := NewRepository(db)

	mock.ExpectExec(sqlRe(`UPDATE "finance_transactions" SET`, `"deleted_at"`, `WHERE (id = $`, `AND space_id = $`, `"deleted_at" IS NULL`)).
		WithArgs(fixedNow, fixedNow, txID, spaceID).WillReturnResult(sqlmock.NewResult(0, 1))
	if err := repo.SoftDeleteTransaction(context.Background(), spaceID, txID, fixedNow); err != nil {
		t.Fatalf("delete: %v", err)
	}
	mock.ExpectExec(sqlRe(`UPDATE "finance_transactions"`)).WillReturnResult(sqlmock.NewResult(0, 0))
	if err := repo.SoftDeleteTransaction(context.Background(), spaceID, txID, fixedNow); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("err = %v, want ErrRecordNotFound", err)
	}
	met(t, mock)
}

func TestFindRecentSimilarFiltersWindow(t *testing.T) {
	db, mock := newMockDB(t)
	repo := NewRepository(db)
	cand := *txFixture()
	cand.ID = ""
	cand.CreatedAt = fixedNow

	mock.ExpectQuery(sqlRe(`WHERE (space_id = $1`, `type = $2`, `amount = $3`, `account_id = $4`,
		`to_account_id IS NOT DISTINCT FROM $5`, `category_id IS NOT DISTINCT FROM $6`, `COALESCE(merchant, '') = $7`,
		`created_at >= $8`, `created_at <= $9`, `"finance_transactions"."deleted_at" IS NULL`, `ORDER BY created_at DESC`)).
		WithArgs(spaceID, "expense", int64(25000), accountID, nilArg{}, categoryID, "Kopi", fixedNow.Add(-2*time.Minute), fixedNow, 1).
		WillReturnRows(sqlmock.NewRows(txCols).AddRow(txID, spaceID, "expense", 25000, accountID, nil, categoryID, "Kopi", nil, fixedNow, memberID, fixedNow, fixedNow, nil))
	got, err := repo.FindRecentSimilar(context.Background(), spaceID, cand, 2*time.Minute)
	if err != nil || got == nil || got.ID != txID {
		t.Fatalf("similar: %v %+v", err, got)
	}

	// No match is nil, nil rather than an error.
	mock.ExpectQuery(sqlRe(`FROM "finance_transactions"`)).WillReturnRows(sqlmock.NewRows(txCols))
	got, err = repo.FindRecentSimilar(context.Background(), spaceID, cand, 2*time.Minute)
	if err != nil || got != nil {
		t.Fatalf("no match: %v %+v", err, got)
	}
	met(t, mock)
}
