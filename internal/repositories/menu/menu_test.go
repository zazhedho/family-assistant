package repositorymenu

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

func TestMenuRepositoryDryRun(t *testing.T) {
	repo := NewMenuRepo(newDryRunDB(t))
	ctx := context.Background()

	if _, err := repo.GetByName(ctx, "dashboard"); err != nil {
		t.Fatalf("get by name: %v", err)
	}
	if _, _, err := repo.GetAll(ctx, filter.BaseParams{
		Search:         "dash",
		Filters:        map[string]any{"name": "dashboard", "is_active": true},
		OrderBy:        "order_index",
		OrderDirection: "ASC",
		Limit:          10,
	}); err != nil {
		t.Fatalf("get all: %v", err)
	}
	if _, err := repo.GetActiveMenus(ctx); err != nil {
		t.Fatalf("get active menus: %v", err)
	}
	if menus, err := repo.GetUserMenus(ctx, "user-1"); err != nil || len(menus) != 0 {
		t.Fatalf("get user menus: menus=%v err=%v", menus, err)
	}
}

func newMenuMockDB(t *testing.T) (*gorm.DB, sqlmock.Sqlmock) {
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

func TestMenuRepositoryGetUserMenus(t *testing.T) {
	t.Run("superadmin returns active menus", func(t *testing.T) {
		db, mock := newMenuMockDB(t)
		repo := NewMenuRepo(db)
		now := time.Now()

		mock.ExpectQuery(`SELECT role_id, role FROM "users" WHERE id = \$1 ORDER BY .* LIMIT \$2`).
			WithArgs("user-1", 1).
			WillReturnRows(sqlmock.NewRows([]string{"role_id", "role"}).AddRow(nil, utils.RoleSuperAdmin))
		mock.ExpectQuery(`SELECT \* FROM "menu_items" WHERE is_active = \$1 AND "menu_items"\."deleted_at" IS NULL ORDER BY order_index ASC`).
			WithArgs(true).
			WillReturnRows(sqlmock.NewRows([]string{
				"id", "name", "display_name", "path", "icon", "parent_id", "order_index", "is_active", "created_at", "updated_at", "deleted_at",
			}).AddRow("menu-1", "dashboard", "Dashboard", "/dashboard", "home", nil, 1, true, now, nil, nil))

		menus, err := repo.GetUserMenus(context.Background(), "user-1")
		if err != nil {
			t.Fatalf("get user menus: %v", err)
		}
		if len(menus) != 1 || menus[0].Id != "menu-1" {
			t.Fatalf("unexpected menus: %#v", menus)
		}
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatalf("sql expectations: %v", err)
		}
	})

	t.Run("empty role id returns empty menus", func(t *testing.T) {
		db, mock := newMenuMockDB(t)
		repo := NewMenuRepo(db)

		mock.ExpectQuery(`SELECT role_id, role FROM "users" WHERE id = \$1 ORDER BY .* LIMIT \$2`).
			WithArgs("user-1", 1).
			WillReturnRows(sqlmock.NewRows([]string{"role_id", "role"}).AddRow(nil, utils.RoleViewer))

		menus, err := repo.GetUserMenus(context.Background(), "user-1")
		if err != nil {
			t.Fatalf("get user menus: %v", err)
		}
		if len(menus) != 0 {
			t.Fatalf("expected empty menus, got %#v", menus)
		}
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatalf("sql expectations: %v", err)
		}
	})

	t.Run("role menus include missing parent menus and are sorted", func(t *testing.T) {
		db, mock := newMenuMockDB(t)
		repo := NewMenuRepo(db)
		roleID := "role-1"
		parentID := "parent-1"
		rootID := "root-1"
		now := time.Now()

		mock.ExpectQuery(`SELECT role_id, role FROM "users" WHERE id = \$1 ORDER BY .* LIMIT \$2`).
			WithArgs("user-1", 1).
			WillReturnRows(sqlmock.NewRows([]string{"role_id", "role"}).AddRow(roleID, utils.RoleViewer))
		mock.ExpectQuery(`SELECT DISTINCT m\.\*[\s\S]+FROM menu_items m[\s\S]+INNER JOIN permissions p[\s\S]+INNER JOIN role_permissions rp[\s\S]+WHERE rp\.role_id = \$1[\s\S]+ORDER BY m\.order_index ASC`).
			WithArgs(roleID).
			WillReturnRows(sqlmock.NewRows([]string{
				"id", "name", "display_name", "path", "icon", "parent_id", "order_index", "is_active", "created_at", "updated_at", "deleted_at",
			}).AddRow("child-1", "users", "Users", "/users", "users", parentID, 30, true, now, nil, nil))
		mock.ExpectQuery(`SELECT \* FROM "menu_items" WHERE \(id IN \(\$1\) AND is_active = \$2 AND deleted_at IS NULL\) AND "menu_items"\."deleted_at" IS NULL`).
			WithArgs(parentID, true).
			WillReturnRows(sqlmock.NewRows([]string{
				"id", "name", "display_name", "path", "icon", "parent_id", "order_index", "is_active", "created_at", "updated_at", "deleted_at",
			}).AddRow(parentID, "settings", "Settings", "/settings", "settings", rootID, 20, true, now, nil, nil))
		mock.ExpectQuery(`SELECT \* FROM "menu_items" WHERE \(id IN \(\$1\) AND is_active = \$2 AND deleted_at IS NULL\) AND "menu_items"\."deleted_at" IS NULL`).
			WithArgs(rootID, true).
			WillReturnRows(sqlmock.NewRows([]string{
				"id", "name", "display_name", "path", "icon", "parent_id", "order_index", "is_active", "created_at", "updated_at", "deleted_at",
			}).AddRow(rootID, "root", "Root", "/", "home", nil, 10, true, now, nil, nil))

		menus, err := repo.GetUserMenus(context.Background(), "user-1")
		if err != nil {
			t.Fatalf("get user menus: %v", err)
		}
		if len(menus) != 3 {
			t.Fatalf("expected 3 menus, got %#v", menus)
		}
		if menus[0].Id != rootID || menus[1].Id != parentID || menus[2].Id != "child-1" {
			t.Fatalf("menus were not sorted with parents: %#v", menus)
		}
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatalf("sql expectations: %v", err)
		}
	})
}
