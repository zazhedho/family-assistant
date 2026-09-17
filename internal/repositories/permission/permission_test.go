package repositorypermission

import (
	"context"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"

	"starter-kit/pkg/filter"
	"starter-kit/utils"
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

func TestPermissionRepositoryDryRun(t *testing.T) {
	repo := NewPermissionRepo(newDryRunDB(t))
	ctx := context.Background()

	if _, err := repo.GetByName(ctx, "users.read"); err != nil {
		t.Fatalf("get by name: %v", err)
	}
	if _, err := repo.GetByResource(ctx, "users"); err != nil {
		t.Fatalf("get by resource: %v", err)
	}
	if _, _, err := repo.GetAll(ctx, filter.BaseParams{
		Search:         "users",
		Filters:        map[string]any{"resource": "users", "action": "read"},
		OrderBy:        "resource",
		OrderDirection: "ASC",
		Limit:          10,
	}); err != nil {
		t.Fatalf("get all: %v", err)
	}
	if permissions, err := repo.GetUserPermissions(ctx, "user-1"); err != nil || len(permissions) != 0 {
		t.Fatalf("get user permissions: permissions=%v err=%v", permissions, err)
	}
}

func newPermissionMockDB(t *testing.T) (*gorm.DB, sqlmock.Sqlmock) {
	t.Helper()
	sqlDB, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
	if err != nil {
		t.Fatalf("sqlmock: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })

	db, err := gorm.Open(postgres.New(postgres.Config{Conn: sqlDB, PreferSimpleProtocol: true}), &gorm.Config{
		SkipDefaultTransaction: true,
	})
	if err != nil {
		t.Fatalf("open gorm: %v", err)
	}
	return db, mock
}

func TestPermissionRepositoryGetUserPermissions(t *testing.T) {
	t.Run("superadmin returns all permissions", func(t *testing.T) {
		db, mock := newPermissionMockDB(t)
		repo := NewPermissionRepo(db)
		now := time.Now()

		mock.ExpectQuery(`SELECT role_id, role FROM "users" WHERE id = \$1 ORDER BY .* LIMIT \$2`).
			WithArgs("user-1", 1).
			WillReturnRows(sqlmock.NewRows([]string{"role_id", "role"}).AddRow(nil, utils.RoleSuperAdmin))
		mock.ExpectQuery(`SELECT \* FROM "permissions" WHERE deleted_at IS NULL AND "permissions"\."deleted_at" IS NULL ORDER BY resource, action`).
			WillReturnRows(sqlmock.NewRows([]string{
				"id", "name", "display_name", "description", "resource", "action", "created_at", "updated_at", "deleted_at",
			}).AddRow("perm-1", "users.read", "Read users", "", "users", "read", now, nil, nil))

		permissions, err := repo.GetUserPermissions(context.Background(), "user-1")
		if err != nil {
			t.Fatalf("get user permissions: %v", err)
		}
		if len(permissions) != 1 || permissions[0].Id != "perm-1" {
			t.Fatalf("unexpected permissions: %#v", permissions)
		}
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatalf("sql expectations: %v", err)
		}
	})

	t.Run("empty role id returns empty permissions", func(t *testing.T) {
		db, mock := newPermissionMockDB(t)
		repo := NewPermissionRepo(db)

		mock.ExpectQuery(`SELECT role_id, role FROM "users" WHERE id = \$1 ORDER BY .* LIMIT \$2`).
			WithArgs("user-1", 1).
			WillReturnRows(sqlmock.NewRows([]string{"role_id", "role"}).AddRow(nil, utils.RoleViewer))

		permissions, err := repo.GetUserPermissions(context.Background(), "user-1")
		if err != nil {
			t.Fatalf("get user permissions: %v", err)
		}
		if len(permissions) != 0 {
			t.Fatalf("expected empty permissions, got %#v", permissions)
		}
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatalf("sql expectations: %v", err)
		}
	})

	t.Run("role permissions are loaded by role id", func(t *testing.T) {
		db, mock := newPermissionMockDB(t)
		repo := NewPermissionRepo(db)
		roleID := "role-1"
		now := time.Now()

		mock.ExpectQuery(`SELECT role_id, role FROM "users" WHERE id = \$1 ORDER BY .* LIMIT \$2`).
			WithArgs("user-1", 1).
			WillReturnRows(sqlmock.NewRows([]string{"role_id", "role"}).AddRow(roleID, utils.RoleViewer))
		mock.ExpectQuery(`SELECT DISTINCT p\.\*[\s\S]+FROM permissions p[\s\S]+INNER JOIN role_permissions rp[\s\S]+WHERE rp\.role_id = \$1 AND p\.deleted_at IS NULL[\s\S]+ORDER BY p\.resource, p\.action`).
			WithArgs(roleID).
			WillReturnRows(sqlmock.NewRows([]string{
				"id", "name", "display_name", "description", "resource", "action", "created_at", "updated_at", "deleted_at",
			}).AddRow("perm-1", "users.read", "Read users", "", "users", "read", now, nil, nil))

		permissions, err := repo.GetUserPermissions(context.Background(), "user-1")
		if err != nil {
			t.Fatalf("get user permissions: %v", err)
		}
		if len(permissions) != 1 || permissions[0].Resource != "users" || permissions[0].Action != "read" {
			t.Fatalf("unexpected permissions: %#v", permissions)
		}
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatalf("sql expectations: %v", err)
		}
	})
}
