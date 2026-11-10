package repositoryfinance

import (
	"context"
	"errors"
	"testing"

	domainfinance "family-assistant/internal/domain/finance"

	"github.com/DATA-DOG/go-sqlmock"
	"gorm.io/gorm"
)

var categoryCols = []string{"id", "space_id", "name", "kind", "archived_at", "created_at", "updated_at", "deleted_at"}

func TestCreateCategoryInsertsRow(t *testing.T) {
	db, mock := newMockDB(t)
	repo := NewRepository(db)
	cat := &domainfinance.Category{SpaceID: spaceID, Name: "Food", Kind: "expense"}

	mock.ExpectExec(sqlRe(`INSERT INTO "finance_categories"`)).WillReturnResult(sqlmock.NewResult(0, 1))
	if err := repo.CreateCategory(context.Background(), cat); err != nil {
		t.Fatalf("create: %v", err)
	}
	if cat.ID == "" {
		t.Fatal("expected generated id")
	}
	met(t, mock)
}

func TestListCategoriesScopesSpaceAndHidesArchived(t *testing.T) {
	db, mock := newMockDB(t)
	repo := NewRepository(db)

	mock.ExpectQuery(sqlRe(`FROM "finance_categories" WHERE space_id = $1`, `archived_at IS NULL`, `"finance_categories"."deleted_at" IS NULL`)).
		WithArgs(spaceID).WillReturnRows(sqlmock.NewRows(categoryCols).AddRow(categoryID, spaceID, "Food", "expense", nil, fixedNow, fixedNow, nil))
	got, err := repo.ListCategories(context.Background(), spaceID, false)
	if err != nil || len(got) != 1 {
		t.Fatalf("list: %v %+v", err, got)
	}
	mock.ExpectQuery(sqlRe(`FROM "finance_categories" WHERE space_id = $1`, `"finance_categories"."deleted_at" IS NULL`)).
		WithArgs(spaceID).WillReturnRows(sqlmock.NewRows(categoryCols))
	if _, err := repo.ListCategories(context.Background(), spaceID, true); err != nil {
		t.Fatalf("list archived: %v", err)
	}
	met(t, mock)
}

func TestFindCategoryNotFoundIsRecordNotFound(t *testing.T) {
	db, mock := newMockDB(t)
	repo := NewRepository(db)

	mock.ExpectQuery(sqlRe(`WHERE (space_id = $1 AND id = $2)`, `"finance_categories"."deleted_at" IS NULL`)).
		WithArgs(spaceID, categoryID, 1).WillReturnRows(sqlmock.NewRows(categoryCols))
	if _, err := repo.FindCategory(context.Background(), spaceID, categoryID); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("err = %v, want ErrRecordNotFound", err)
	}
	met(t, mock)
}

func TestUpdateAndArchiveCategoryAreSpaceScoped(t *testing.T) {
	db, mock := newMockDB(t)
	repo := NewRepository(db)

	mock.ExpectExec(sqlRe(`UPDATE "finance_categories" SET`, `"kind"`, `WHERE (id = $`, `AND space_id = $`, `"deleted_at" IS NULL`)).
		WillReturnResult(sqlmock.NewResult(0, 1))
	if err := repo.UpdateCategory(context.Background(), &domainfinance.Category{ID: categoryID, SpaceID: spaceID, Name: "Food", Kind: "expense"}); err != nil {
		t.Fatalf("update: %v", err)
	}
	mock.ExpectExec(sqlRe(`UPDATE "finance_categories" SET`, `"archived_at"`, `WHERE (id = $`, `AND space_id = $`, `"deleted_at" IS NULL`)).
		WithArgs(fixedNow, fixedNow, categoryID, spaceID).WillReturnResult(sqlmock.NewResult(0, 1))
	if err := repo.ArchiveCategory(context.Background(), spaceID, categoryID, fixedNow); err != nil {
		t.Fatalf("archive: %v", err)
	}
	mock.ExpectExec(sqlRe(`UPDATE "finance_categories"`)).WillReturnResult(sqlmock.NewResult(0, 0))
	if err := repo.ArchiveCategory(context.Background(), spaceID, categoryID, fixedNow); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("err = %v, want ErrRecordNotFound", err)
	}
	met(t, mock)
}
