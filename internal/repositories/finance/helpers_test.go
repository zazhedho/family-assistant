package repositoryfinance

import (
	"database/sql/driver"
	"regexp"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

const (
	spaceID    = "00000000-0000-4000-8000-000000000201"
	memberID   = "00000000-0000-4000-8000-000000000301"
	accountID  = "00000000-0000-4000-8000-000000000501"
	account2ID = "00000000-0000-4000-8000-000000000502"
	categoryID = "00000000-0000-4000-8000-000000000601"
	txID       = "00000000-0000-4000-8000-000000000701"
)

var fixedNow = time.Date(2026, 11, 10, 3, 0, 0, 0, time.UTC)

func newMockDB(t *testing.T) (*gorm.DB, sqlmock.Sqlmock) {
	t.Helper()
	sqlDB, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
	if err != nil {
		t.Fatalf("sqlmock: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	db, err := gorm.Open(postgres.New(postgres.Config{Conn: sqlDB, PreferSimpleProtocol: true}), &gorm.Config{SkipDefaultTransaction: true})
	if err != nil {
		t.Fatalf("open gorm: %v", err)
	}
	return db, mock
}

// sqlRe matches a query that contains every fragment, in order, with any text
// between them. Fragments are quoted literally.
func sqlRe(fragments ...string) string {
	out := ""
	for i, f := range fragments {
		if i > 0 {
			out += ".*"
		}
		out += regexp.QuoteMeta(f)
	}
	return out
}

// nilArg matches a SQL NULL argument.
type nilArg struct{}

func (nilArg) Match(v driver.Value) bool { return v == nil }

func met(t *testing.T, mock sqlmock.Sqlmock) {
	t.Helper()
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func strp(s string) *string { return &s }
