package repositoryrole

import (
	"context"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"

	"starter-kit/pkg/filter"
)

func newDryRunDB(t *testing.T) *gorm.DB {
	t.Helper()
	sqlDB, _, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })

	db, err := gorm.Open(postgres.New(postgres.Config{Conn: sqlDB, PreferSimpleProtocol: true}), &gorm.Config{
		DryRun:                 true,
		SkipDefaultTransaction: true,
	})
	if err != nil {
		t.Fatalf("open gorm: %v", err)
	}
	return db
}

func TestRoleRepositoryDryRun(t *testing.T) {
	repo := NewRoleRepo(newDryRunDB(t))
	ctx := context.Background()

	if _, err := repo.GetByName(ctx, "admin"); err != nil {
		t.Fatalf("get by name: %v", err)
	}
	if _, _, err := repo.GetAll(ctx, filter.BaseParams{
		Search:         "admin",
		Filters:        map[string]any{"name": "admin", "is_system": false},
		OrderBy:        "name",
		OrderDirection: "ASC",
		Limit:          10,
	}); err != nil {
		t.Fatalf("get all: %v", err)
	}
	if ids, err := repo.GetRolePermissions(ctx, "role-1"); err != nil || len(ids) != 0 {
		t.Fatalf("get role permissions: ids=%v err=%v", ids, err)
	}
	if ids, err := repo.GetRoleMenus(ctx, "role-1"); err != nil || len(ids) != 0 {
		t.Fatalf("get role menus: ids=%v err=%v", ids, err)
	}
}

func newRoleMockDB(t *testing.T) (*gorm.DB, sqlmock.Sqlmock) {
	t.Helper()
	sqlDB, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
	if err != nil {
		t.Fatalf("sqlmock: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })

	db, err := gorm.Open(postgres.New(postgres.Config{Conn: sqlDB, PreferSimpleProtocol: true}), &gorm.Config{})
	if err != nil {
		t.Fatalf("open gorm: %v", err)
	}
	return db, mock
}

func TestRoleRepositoryAssignAndRemoveRelations(t *testing.T) {
	db, mock := newRoleMockDB(t)
	repo := NewRoleRepo(db)
	ctx := context.Background()

	mock.ExpectBegin()
	mock.ExpectExec(`DELETE FROM "role_permissions"`).WithArgs("role-1").WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(`INSERT INTO "role_permissions"`).WithArgs(sqlmock.AnyArg(), "role-1", "perm-1", sqlmock.AnyArg()).WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()
	if err := repo.AssignPermissions(ctx, "role-1", []string{"perm-1"}); err != nil {
		t.Fatalf("assign permissions: %v", err)
	}

	mock.ExpectBegin()
	mock.ExpectExec(`DELETE FROM "role_permissions"`).WithArgs("role-1", "perm-1", "perm-2").WillReturnResult(sqlmock.NewResult(0, 2))
	mock.ExpectCommit()
	if err := repo.RemovePermissions(ctx, "role-1", []string{"perm-1", "perm-2"}); err != nil {
		t.Fatalf("remove permissions: %v", err)
	}

	mock.ExpectBegin()
	mock.ExpectExec(`DELETE FROM "role_menus"`).WithArgs("role-1").WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(`INSERT INTO "role_menus"`).WithArgs(sqlmock.AnyArg(), "role-1", "menu-1", sqlmock.AnyArg()).WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()
	if err := repo.AssignMenus(ctx, "role-1", []string{"menu-1"}); err != nil {
		t.Fatalf("assign menus: %v", err)
	}

	mock.ExpectBegin()
	mock.ExpectExec(`DELETE FROM "role_menus"`).WithArgs("role-1", "menu-1").WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()
	if err := repo.RemoveMenus(ctx, "role-1", []string{"menu-1"}); err != nil {
		t.Fatalf("remove menus: %v", err)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}
