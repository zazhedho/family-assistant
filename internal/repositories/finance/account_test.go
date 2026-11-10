package repositoryfinance

import (
	"context"
	"errors"
	"testing"

	domainfinance "family-assistant/internal/domain/finance"

	"github.com/DATA-DOG/go-sqlmock"
	"gorm.io/gorm"
)

var accountCols = []string{"id", "space_id", "name", "type", "opening_balance", "archived_at", "created_at", "updated_at", "deleted_at"}

func TestCreateAccountInsertsRow(t *testing.T) {
	db, mock := newMockDB(t)
	repo := NewRepository(db)
	acc := &domainfinance.Account{SpaceID: spaceID, Name: "BCA", Type: "bank", OpeningBalance: 1000}

	mock.ExpectExec(sqlRe(`INSERT INTO "finance_accounts"`)).WillReturnResult(sqlmock.NewResult(0, 1))
	if err := repo.CreateAccount(context.Background(), acc); err != nil {
		t.Fatalf("create: %v", err)
	}
	if acc.ID == "" {
		t.Fatal("expected generated id")
	}
	met(t, mock)
}

func TestListAccountsScopesSpaceAndHidesArchived(t *testing.T) {
	db, mock := newMockDB(t)
	repo := NewRepository(db)

	mock.ExpectQuery(sqlRe(`FROM "finance_accounts" WHERE space_id = $1`, `archived_at IS NULL`, `"finance_accounts"."deleted_at" IS NULL`)).
		WithArgs(spaceID).WillReturnRows(sqlmock.NewRows(accountCols).AddRow(accountID, spaceID, "BCA", "bank", 0, nil, fixedNow, fixedNow, nil))
	got, err := repo.ListAccounts(context.Background(), spaceID, false)
	if err != nil || len(got) != 1 || got[0].ID != accountID {
		t.Fatalf("list: %v %+v", err, got)
	}

	// includeArchived drops the archived filter but keeps space and soft delete.
	mock.ExpectQuery(sqlRe(`FROM "finance_accounts" WHERE space_id = $1`, `"finance_accounts"."deleted_at" IS NULL`)).
		WithArgs(spaceID).WillReturnRows(sqlmock.NewRows(accountCols))
	if _, err := repo.ListAccounts(context.Background(), spaceID, true); err != nil {
		t.Fatalf("list archived: %v", err)
	}
	met(t, mock)
}

func TestFindAccountNotFoundIsRecordNotFound(t *testing.T) {
	db, mock := newMockDB(t)
	repo := NewRepository(db)

	mock.ExpectQuery(sqlRe(`WHERE (space_id = $1 AND id = $2)`, `"finance_accounts"."deleted_at" IS NULL`)).
		WithArgs(spaceID, accountID, 1).WillReturnRows(sqlmock.NewRows(accountCols))
	if _, err := repo.FindAccount(context.Background(), spaceID, accountID); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("err = %v, want ErrRecordNotFound", err)
	}
	met(t, mock)
}

func TestUpdateAccountWritesEditableColumnsInSpace(t *testing.T) {
	db, mock := newMockDB(t)
	repo := NewRepository(db)
	acc := &domainfinance.Account{ID: accountID, SpaceID: spaceID, Name: "BCA", Type: "bank", OpeningBalance: 0}

	mock.ExpectExec(sqlRe(`UPDATE "finance_accounts" SET`, `"opening_balance"`, `WHERE (id = $`, `AND space_id = $`, `"deleted_at" IS NULL`)).
		WillReturnResult(sqlmock.NewResult(0, 1))
	if err := repo.UpdateAccount(context.Background(), acc); err != nil {
		t.Fatalf("update: %v", err)
	}

	mock.ExpectExec(sqlRe(`UPDATE "finance_accounts"`)).WillReturnResult(sqlmock.NewResult(0, 0))
	if err := repo.UpdateAccount(context.Background(), acc); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("err = %v, want ErrRecordNotFound", err)
	}
	met(t, mock)
}

func TestArchiveAccountSetsArchivedAtOnlyForLiveRowInSpace(t *testing.T) {
	db, mock := newMockDB(t)
	repo := NewRepository(db)

	mock.ExpectExec(sqlRe(`UPDATE "finance_accounts" SET`, `"archived_at"`, `WHERE (id = $`, `AND space_id = $`, `"deleted_at" IS NULL`)).
		WithArgs(fixedNow, fixedNow, accountID, spaceID).WillReturnResult(sqlmock.NewResult(0, 1))
	if err := repo.ArchiveAccount(context.Background(), spaceID, accountID, fixedNow); err != nil {
		t.Fatalf("archive: %v", err)
	}
	mock.ExpectExec(sqlRe(`UPDATE "finance_accounts"`)).WillReturnResult(sqlmock.NewResult(0, 0))
	if err := repo.ArchiveAccount(context.Background(), spaceID, accountID, fixedNow); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("err = %v, want ErrRecordNotFound", err)
	}
	met(t, mock)
}
