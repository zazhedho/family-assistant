package repositoryuser

import (
	"context"
	"database/sql/driver"
	"regexp"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	domainuser "starter-kit/internal/domain/user"

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

func TestUserRepositoryDryRun(t *testing.T) {
	repo := NewUserRepo(newDryRunDB(t))
	ctx := context.Background()

	if _, err := repo.GetByEmail(ctx, "jane@example.com"); err != nil {
		t.Fatalf("get by email: %v", err)
	}
	if _, err := repo.GetByPhone(ctx, "08123456789"); err != nil {
		t.Fatalf("get by phone: %v", err)
	}
	if _, _, err := repo.GetAll(ctx, filter.BaseParams{
		Search:         "jane",
		Filters:        map[string]any{"role": "admin", "email": "jane@example.com"},
		OrderBy:        "email",
		OrderDirection: "ASC",
		Limit:          10,
	}); err != nil {
		t.Fatalf("get all: %v", err)
	}
}

func TestUserRepositoryStoreOmitsEmptyPhone(t *testing.T) {
	sqlDB, mock, err := sqlmock.New()
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

	repo := NewUserRepo(db)
	mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO "users" ("id","name","email","password","role","role_id","email_verified_at","phone_verified_at","last_login_at","last_login_ip","last_login_user_agent","locked_until","password_changed_at","login_provider","avatar_url","metadata","created_at","updated_at","deleted_at") VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19)`)).
		WithArgs(anyArgs(19)...).
		WillReturnResult(sqlmock.NewResult(0, 1))

	err = repo.Store(context.Background(), domainuser.Users{
		Id:       "user-1",
		Name:     "Google User",
		Email:    "google@example.com",
		Phone:    "",
		Password: "hashed",
		Role:     "member",
	})
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectations: %v", err)
	}
}

func anyArgs(total int) []driver.Value {
	args := make([]driver.Value, 0, total)
	for i := 0; i < total; i++ {
		args = append(args, sqlmock.AnyArg())
	}
	return args
}
