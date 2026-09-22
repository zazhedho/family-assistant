package repositoryactivity

import (
	"context"
	"errors"
	"regexp"
	"testing"
	"time"

	domainactivity "family-assistant/internal/domain/activity"
	interfaceactivity "family-assistant/internal/interfaces/activity"

	"github.com/DATA-DOG/go-sqlmock"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

const (
	activityID = "00000000-0000-4000-8000-000000000401"
	spaceID    = "00000000-0000-4000-8000-000000000201"
	memberID   = "00000000-0000-4000-8000-000000000301"
)

func newActivityMockDB(t *testing.T) (*gorm.DB, sqlmock.Sqlmock) {
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

func activityFixture() *domainactivity.Activity {
	now := time.Date(2026, 9, 22, 8, 30, 0, 0, time.UTC)
	return &domainactivity.Activity{
		ID: activityID, SpaceID: spaceID, CreatedByMemberID: memberID, Kind: "breastfeeding", Note: "10 minutes",
		OccurredAt: now, CreatedAt: now, UpdatedAt: now,
	}
}

func activityColumns() []string {
	return []string{"id", "space_id", "created_by_member_id", "kind", "note", "occurred_at", "created_at", "updated_at", "deleted_at"}
}

func TestCreatePersistsActivity(t *testing.T) {
	db, mock := newActivityMockDB(t)
	repo := NewRepository(db)
	activity := activityFixture()

	mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO "space_activities" ("id","space_id","created_by_member_id","kind","note","occurred_at","created_at","updated_at") VALUES ($1,$2,$3,$4,$5,$6,$7,$8)`)).
		WithArgs(activity.ID, activity.SpaceID, activity.CreatedByMemberID, activity.Kind, activity.Note, activity.OccurredAt, activity.CreatedAt, activity.UpdatedAt).
		WillReturnResult(sqlmock.NewResult(0, 1))

	if err := repo.Create(context.Background(), activity); err != nil {
		t.Fatalf("create activity: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func TestCreateRejectsNilActivity(t *testing.T) {
	db, mock := newActivityMockDB(t)
	repo := NewRepository(db)
	if err := repo.Create(context.Background(), nil); !errors.Is(err, domainactivity.ErrActivityRequired) {
		t.Fatalf("error = %v, want activity required", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unexpected database query: %v", err)
	}
}

func TestListScopesBySpaceKindAndOccurredAt(t *testing.T) {
	db, mock := newActivityMockDB(t)
	repo := NewRepository(db)
	from := time.Date(2026, 9, 22, 0, 0, 0, 0, time.UTC)
	to := from.Add(24 * time.Hour)
	mock.ExpectQuery(`SELECT .* FROM "space_activities" WHERE space_id = \$1 AND kind = \$2 AND occurred_at >= \$3 AND occurred_at <= \$4 AND .*deleted_at.*IS NULL ORDER BY occurred_at DESC LIMIT \$5`).
		WithArgs(spaceID, "diaper", from, to, 20).
		WillReturnRows(sqlmock.NewRows(activityColumns()).AddRow(activityID, spaceID, memberID, "diaper", "wet", from.Add(time.Hour), from, from, nil))

	got, err := repo.List(context.Background(), domainactivity.ListFilter{SpaceID: spaceID, Kind: "diaper", From: &from, To: &to, Limit: 20})
	if err != nil {
		t.Fatalf("list activity: %v", err)
	}
	if len(got) != 1 || got[0].Kind != "diaper" {
		t.Fatalf("activities = %+v", got)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

var _ interfaceactivity.RepoActivityInterface = (*Repository)(nil)
