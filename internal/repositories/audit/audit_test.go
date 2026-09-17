package repositoryaudit

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

func TestAuditRepositoryDryRunGetAll(t *testing.T) {
	repo := NewAuditRepo(newDryRunDB(t))

	if _, _, err := repo.GetAll(context.Background(), filter.BaseParams{
		Search:         "login",
		Filters:        map[string]any{"action": "login", "status": "success"},
		OrderBy:        "occurred_at",
		OrderDirection: "DESC",
		Limit:          10,
	}); err != nil {
		t.Fatalf("get all: %v", err)
	}
}
