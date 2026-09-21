package repositoryreminder

import (
	"context"
	"errors"
	"regexp"
	"testing"
	"time"

	domainreminder "family-assistant/internal/domain/reminder"
	interfacereminder "family-assistant/internal/interfaces/reminder"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

const (
	reminderID = "00000000-0000-4000-8000-000000000101"
	spaceID    = "00000000-0000-4000-8000-000000000201"
	memberID   = "00000000-0000-4000-8000-000000000301"
)

func newReminderMockDB(t *testing.T) (*gorm.DB, sqlmock.Sqlmock) {
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

func reminderFixture() *domainreminder.Reminder {
	assignee := memberID
	return &domainreminder.Reminder{
		ID:                reminderID,
		SpaceID:           spaceID,
		CreatedByMemberID: memberID,
		AssigneeMemberID:  &assignee,
		Title:             "Pay electricity bill",
		Description:       "Before the due date",
		ScheduledAt:       time.Date(2026, 9, 20, 8, 0, 0, 0, time.UTC),
		Status:            domainreminder.StatusPending,
		CreatedAt:         time.Date(2026, 9, 17, 8, 0, 0, 0, time.UTC),
		UpdatedAt:         time.Date(2026, 9, 17, 8, 0, 0, 0, time.UTC),
	}
}

func reminderColumns() []string {
	return []string{
		"id", "space_id", "created_by_member_id", "assignee_member_id", "title", "description",
		"scheduled_at", "status", "completed_at", "created_at", "updated_at", "deleted_at",
	}
}

func expectComplete(mock sqlmock.Sqlmock, completedAt time.Time, rows int64) {
	mock.ExpectExec(`UPDATE "reminders" SET .* WHERE \(id = \$4 AND space_id = \$5 AND status = \$6\) AND .*deleted_at.*IS NULL`).
		WithArgs(completedAt, domainreminder.StatusCompleted, completedAt, reminderID, spaceID, domainreminder.StatusPending).
		WillReturnResult(sqlmock.NewResult(0, rows))
}

func TestCreatePersistsSpaceAndOptionalAssignee(t *testing.T) {
	db, mock := newReminderMockDB(t)
	repo := NewRepository(db)
	reminder := reminderFixture()

	mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO "reminders" ("id","space_id","created_by_member_id","assignee_member_id","title","description","scheduled_at","status","completed_at","created_at","updated_at") VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`)).
		WithArgs(
			reminder.ID,
			reminder.SpaceID,
			reminder.CreatedByMemberID,
			reminder.AssigneeMemberID,
			reminder.Title,
			reminder.Description,
			reminder.ScheduledAt,
			reminder.Status,
			reminder.CompletedAt,
			reminder.CreatedAt,
			reminder.UpdatedAt,
		).
		WillReturnResult(sqlmock.NewResult(0, 1))

	if err := repo.Create(context.Background(), reminder); err != nil {
		t.Fatalf("create reminder: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func TestCreateGeneratesUUIDWhenIDIsEmpty(t *testing.T) {
	db, mock := newReminderMockDB(t)
	repo := NewRepository(db)
	reminder := reminderFixture()
	reminder.ID = ""

	mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO "reminders" ("id","space_id","created_by_member_id","assignee_member_id","title","description","scheduled_at","status","completed_at","created_at","updated_at") VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`)).
		WithArgs(
			sqlmock.AnyArg(),
			reminder.SpaceID,
			reminder.CreatedByMemberID,
			reminder.AssigneeMemberID,
			reminder.Title,
			reminder.Description,
			reminder.ScheduledAt,
			reminder.Status,
			reminder.CompletedAt,
			reminder.CreatedAt,
			reminder.UpdatedAt,
		).
		WillReturnResult(sqlmock.NewResult(0, 1))

	if err := repo.Create(context.Background(), reminder); err != nil {
		t.Fatalf("create reminder: %v", err)
	}
	if _, err := uuid.Parse(reminder.ID); err != nil {
		t.Fatalf("expected generated UUID, got %q: %v", reminder.ID, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func TestCreateRejectsNilReminderBeforeQuery(t *testing.T) {
	db, mock := newReminderMockDB(t)
	repo := NewRepository(db)

	err := repo.Create(context.Background(), nil)
	if !errors.Is(err, domainreminder.ErrReminderRequired) {
		t.Fatalf("expected reminder required, got %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unexpected database query: %v", err)
	}
}

func TestFindByIDInSpaceCannotReturnAnotherSpaceOrDeletedRow(t *testing.T) {
	db, mock := newReminderMockDB(t)
	repo := NewRepository(db)

	mock.ExpectQuery(`SELECT .* FROM "reminders" WHERE \(space_id = \$1 AND id = \$2\) AND .*deleted_at.*IS NULL.*LIMIT \$3`).
		WithArgs(spaceID, reminderID, 1).
		WillReturnRows(sqlmock.NewRows(reminderColumns()))

	_, err := repo.FindByIDInSpace(context.Background(), spaceID, reminderID)
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("expected record not found, got %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func TestListScopesBySpaceStatusAndScheduleAndExcludesDeletedRows(t *testing.T) {
	db, mock := newReminderMockDB(t)
	repo := NewRepository(db)
	status := domainreminder.StatusPending
	from := time.Date(2026, 9, 17, 0, 0, 0, 0, time.UTC)
	to := time.Date(2026, 9, 30, 23, 59, 59, 0, time.UTC)

	mock.ExpectQuery(`SELECT .* FROM "reminders" WHERE space_id = \$1 AND status = \$2 AND scheduled_at >= \$3 AND scheduled_at <= \$4 AND .*deleted_at.*IS NULL`).
		WithArgs(spaceID, status, from, to).
		WillReturnRows(sqlmock.NewRows(reminderColumns()).AddRow(
			reminderID, spaceID, memberID, nil, "Pay bill", "", time.Date(2026, 9, 20, 8, 0, 0, 0, time.UTC),
			"PENDING", nil, from, from, nil,
		))

	got, err := repo.List(context.Background(), domainreminder.ListFilter{SpaceID: spaceID, Status: &status, From: &from, To: &to})
	if err != nil {
		t.Fatalf("list reminders: %v", err)
	}
	if len(got) != 1 || got[0].SpaceID != spaceID || got[0].Status != status {
		t.Fatalf("unexpected reminders: %#v", got)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func TestListRejectsUnscopedSpaceBeforeQuery(t *testing.T) {
	db, mock := newReminderMockDB(t)
	repo := NewRepository(db)

	_, err := repo.List(context.Background(), domainreminder.ListFilter{SpaceID: " \t"})
	if !errors.Is(err, domainreminder.ErrSpaceIDRequired) {
		t.Fatalf("expected space ID required, got %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unexpected database query: %v", err)
	}
}

func TestCompletePendingScopesBySpaceAndStatus(t *testing.T) {
	db, mock := newReminderMockDB(t)
	repo := NewRepository(db)
	now := time.Date(2026, 9, 20, 8, 0, 0, 0, time.UTC)

	mock.ExpectExec(`UPDATE "reminders" SET .* WHERE \(id = \$4 AND space_id = \$5 AND status = \$6\) AND .*deleted_at.*IS NULL`).
		WithArgs(now, domainreminder.StatusCompleted, now, reminderID, spaceID, domainreminder.StatusPending).
		WillReturnResult(sqlmock.NewResult(0, 1))

	if err := repo.CompletePending(context.Background(), spaceID, reminderID, now); err != nil {
		t.Fatalf("complete reminder: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func TestCompletePendingReturnsStatusConflictWhenNoPendingRow(t *testing.T) {
	db, mock := newReminderMockDB(t)
	repo := NewRepository(db)
	now := time.Date(2026, 9, 20, 8, 0, 0, 0, time.UTC)
	expectComplete(mock, now, 0)

	err := repo.CompletePending(context.Background(), spaceID, reminderID, now)
	if !errors.Is(err, domainreminder.ErrStatusConflict) {
		t.Fatalf("expected status conflict, got %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func TestCompletePendingRejectsBlankScopeOrReminderIDBeforeQuery(t *testing.T) {
	db, mock := newReminderMockDB(t)
	repo := NewRepository(db)
	now := time.Now().UTC()

	if err := repo.CompletePending(context.Background(), " \t", reminderID, now); !errors.Is(err, domainreminder.ErrSpaceIDRequired) {
		t.Fatalf("expected space ID required, got %v", err)
	}
	if err := repo.CompletePending(context.Background(), spaceID, " \t", now); !errors.Is(err, domainreminder.ErrReminderIDRequired) {
		t.Fatalf("expected reminder ID required, got %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unexpected database query: %v", err)
	}
}

func TestRepositorySatisfiesInterface(t *testing.T) {
	var _ interfacereminder.RepoReminderInterface = (*Repository)(nil)
}
