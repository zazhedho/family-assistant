package repositoryfinance

import (
	"context"
	"errors"
	"testing"

	domainfinance "family-assistant/internal/domain/finance"

	"github.com/DATA-DOG/go-sqlmock"
	"gorm.io/gorm"
)

func TestUnarchiveAccountAndCategoryWriteNullScopedBySpace(t *testing.T) {
	db, mock := newMockDB(t)
	repo := NewRepository(db)

	mock.ExpectExec(sqlRe(`UPDATE "finance_accounts" SET "archived_at"=$1`, `WHERE (id = $`, `AND space_id = $`, `"deleted_at" IS NULL`)).
		WithArgs(nilArg{}, sqlmock.AnyArg(), accountID, spaceID).WillReturnResult(sqlmock.NewResult(0, 1))
	if err := repo.UnarchiveAccount(context.Background(), spaceID, accountID); err != nil {
		t.Fatalf("unarchive account: %v", err)
	}
	mock.ExpectExec(sqlRe(`UPDATE "finance_categories" SET "archived_at"=$1`, `WHERE (id = $`, `AND space_id = $`, `"deleted_at" IS NULL`)).
		WithArgs(nilArg{}, sqlmock.AnyArg(), categoryID, spaceID).WillReturnResult(sqlmock.NewResult(0, 1))
	if err := repo.UnarchiveCategory(context.Background(), spaceID, categoryID); err != nil {
		t.Fatalf("unarchive category: %v", err)
	}
	mock.ExpectExec(sqlRe(`UPDATE "finance_categories"`)).WillReturnResult(sqlmock.NewResult(0, 0))
	if err := repo.UnarchiveCategory(context.Background(), spaceID, categoryID); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("err = %v, want ErrRecordNotFound", err)
	}
	if err := repo.UnarchiveAccount(context.Background(), "", accountID); !errors.Is(err, ErrSpaceRequired) {
		t.Fatalf("err = %v", err)
	}
	met(t, mock)
}

func seedDefaults() []domainfinance.Category {
	return []domainfinance.Category{{Name: "Makan", Kind: "expense"}, {Name: "Gaji", Kind: "income"}}
}

func TestSeedDefaultCategoriesLocksCountsInsertsAndCommits(t *testing.T) {
	db, mock := newMockDB(t)
	repo := NewRepository(db)

	mock.ExpectBegin()
	mock.ExpectExec(sqlRe(`SELECT pg_advisory_xact_lock(hashtext($1))`)).WithArgs(spaceID).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery(sqlRe(`SELECT count(*) FROM "finance_categories" WHERE space_id = $1`)).
		WithArgs(spaceID).WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))
	mock.ExpectExec(sqlRe(`INSERT INTO "finance_categories"`, `ON CONFLICT DO NOTHING`)).WillReturnResult(sqlmock.NewResult(0, 2))
	mock.ExpectCommit()
	if err := repo.SeedDefaultCategories(context.Background(), spaceID, seedDefaults()); err != nil {
		t.Fatalf("seed: %v", err)
	}
	met(t, mock)
}

func TestSeedDefaultCategoriesSkipsWhenAnyRowExists(t *testing.T) {
	db, mock := newMockDB(t)
	repo := NewRepository(db)

	mock.ExpectBegin()
	mock.ExpectExec(sqlRe(`pg_advisory_xact_lock`)).WithArgs(spaceID).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery(sqlRe(`SELECT count(*) FROM "finance_categories" WHERE space_id = $1`)).
		WithArgs(spaceID).WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(3))
	mock.ExpectCommit()
	if err := repo.SeedDefaultCategories(context.Background(), spaceID, seedDefaults()); err != nil {
		t.Fatalf("seed: %v", err)
	}
	met(t, mock)
}

// The count must see archived and soft-deleted rows: any row of any state
// means the Space was already seeded, so the statement has no deleted_at filter.
func TestSeedDefaultCategoriesCountsRowsOfAnyState(t *testing.T) {
	db, mock := newMockDB(t)
	repo := NewRepository(db)

	mock.ExpectBegin()
	mock.ExpectExec(sqlRe(`pg_advisory_xact_lock`)).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery(`^SELECT count\(\*\) FROM "finance_categories" WHERE space_id = \$1$`).
		WithArgs(spaceID).WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))
	mock.ExpectCommit()
	if err := repo.SeedDefaultCategories(context.Background(), spaceID, seedDefaults()); err != nil {
		t.Fatalf("seed: %v", err)
	}
	met(t, mock)
}

func TestSeedDefaultCategoriesRollsBackOnInsertError(t *testing.T) {
	db, mock := newMockDB(t)
	repo := NewRepository(db)
	boom := errors.New("boom")

	mock.ExpectBegin()
	mock.ExpectExec(sqlRe(`pg_advisory_xact_lock`)).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery(sqlRe(`SELECT count(*)`)).WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))
	mock.ExpectExec(sqlRe(`INSERT INTO "finance_categories"`)).WillReturnError(boom)
	mock.ExpectRollback()
	if err := repo.SeedDefaultCategories(context.Background(), spaceID, seedDefaults()); !errors.Is(err, boom) {
		t.Fatalf("err = %v, want boom", err)
	}
	met(t, mock)
}

func TestSeedDefaultCategoriesRollsBackOnLockError(t *testing.T) {
	db, mock := newMockDB(t)
	repo := NewRepository(db)
	boom := errors.New("lock failed")

	mock.ExpectBegin()
	mock.ExpectExec(sqlRe(`pg_advisory_xact_lock`)).WillReturnError(boom)
	mock.ExpectRollback()
	if err := repo.SeedDefaultCategories(context.Background(), spaceID, seedDefaults()); !errors.Is(err, boom) {
		t.Fatalf("err = %v, want lock error", err)
	}
	met(t, mock)
}

func TestSeedDefaultCategoriesRequiresSpace(t *testing.T) {
	db, _ := newMockDB(t)
	if err := NewRepository(db).SeedDefaultCategories(context.Background(), " ", seedDefaults()); !errors.Is(err, ErrSpaceRequired) {
		t.Fatalf("err = %v", err)
	}
}
