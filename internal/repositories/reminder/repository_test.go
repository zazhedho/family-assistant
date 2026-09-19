package repositoryreminder

import (
	"context"
	"database/sql/driver"
	"errors"
	"regexp"
	"testing"
	"time"

	domainreminder "family-assistant/internal/domain/reminder"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
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
	return &domainreminder.Reminder{
		ID:                "reminder-1",
		FamilyID:          "family-1",
		OwnerMemberID:     "member-1",
		CreatedByMemberID: "member-2",
		Scope:             domainreminder.ScopePersonal,
		Title:             "Pay electricity bill",
		Description:       "Before the due date",
		ScheduledAt:       time.Date(2026, 9, 20, 8, 0, 0, 0, time.UTC),
		Status:            domainreminder.StatusPending,
		CreatedAt:         time.Date(2026, 9, 17, 8, 0, 0, 0, time.UTC),
		UpdatedAt:         time.Date(2026, 9, 17, 8, 0, 0, 0, time.UTC),
	}
}

type updatedAtMatcher struct {
	got *time.Time
	min time.Time
	max time.Time
}

func (m updatedAtMatcher) Match(value driver.Value) bool {
	actual, ok := value.(time.Time)
	if !ok {
		return false
	}
	if m.got != nil {
		*m.got = actual
	}
	return !actual.Before(m.min) && !actual.After(m.max)
}

func TestCreatePersistsOwnershipScopeAndStatus(t *testing.T) {
	db, mock := newReminderMockDB(t)
	repo := NewRepository(db)
	reminder := reminderFixture()

	mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO "reminders" ("id","family_id","owner_member_id","created_by_member_id","scope","title","description","scheduled_at","status","completed_at","created_at","updated_at") VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)`)).
		WithArgs(
			reminder.ID,
			reminder.FamilyID,
			reminder.OwnerMemberID,
			reminder.CreatedByMemberID,
			reminder.Scope,
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

	mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO "reminders" ("id","family_id","owner_member_id","created_by_member_id","scope","title","description","scheduled_at","status","completed_at","created_at","updated_at") VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)`)).
		WithArgs(
			sqlmock.AnyArg(),
			reminder.FamilyID,
			reminder.OwnerMemberID,
			reminder.CreatedByMemberID,
			reminder.Scope,
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

func TestFindByIDInFamilyCannotReturnAnotherFamily(t *testing.T) {
	db, mock := newReminderMockDB(t)
	repo := NewRepository(db)

	mock.ExpectQuery(`SELECT .* FROM "reminders" WHERE family_id = \$1 AND id = \$2.*LIMIT \$3`).
		WithArgs("family-a", "reminder-in-family-b", 1).
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "family_id", "owner_member_id", "created_by_member_id", "scope", "title", "description", "scheduled_at", "status", "completed_at", "created_at", "updated_at",
		}))

	_, err := repo.FindByIDInFamily(context.Background(), "family-a", "reminder-in-family-b")
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("expected record not found, got %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func TestListWithOwnerMemberIDReturnsOnlyThatOwner(t *testing.T) {
	db, mock := newReminderMockDB(t)
	repo := NewRepository(db)
	owner := "member-1"

	mock.ExpectQuery(`SELECT .* FROM "reminders" WHERE family_id = \$1 AND owner_member_id = \$2`).
		WithArgs("family-1", owner).
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "family_id", "owner_member_id", "created_by_member_id", "scope", "title", "description", "scheduled_at", "status", "completed_at", "created_at", "updated_at",
		}).AddRow("reminder-1", "family-1", owner, "member-2", "PERSONAL", "Own reminder", "", time.Now(), "PENDING", nil, time.Now(), time.Now()))

	got, err := repo.List(context.Background(), domainreminder.ListFilter{FamilyID: "family-1", OwnerMemberID: &owner})
	if err != nil {
		t.Fatalf("list reminders: %v", err)
	}
	if len(got) != 1 || got[0].OwnerMemberID != owner {
		t.Fatalf("unexpected reminders: %#v", got)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func TestListWithFamilyScopeReturnsOnlyFamilyRows(t *testing.T) {
	db, mock := newReminderMockDB(t)
	repo := NewRepository(db)
	scope := domainreminder.ScopeFamily

	mock.ExpectQuery(`SELECT .* FROM "reminders" WHERE family_id = \$1 AND scope = \$2`).
		WithArgs("family-1", scope).
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "family_id", "owner_member_id", "created_by_member_id", "scope", "title", "description", "scheduled_at", "status", "completed_at", "created_at", "updated_at",
		}).AddRow("reminder-1", "family-1", "member-1", "member-1", "FAMILY", "Family reminder", "", time.Now(), "PENDING", nil, time.Now(), time.Now()))

	got, err := repo.List(context.Background(), domainreminder.ListFilter{FamilyID: "family-1", Scope: &scope})
	if err != nil {
		t.Fatalf("list reminders: %v", err)
	}
	if len(got) != 1 || got[0].Scope != scope || got[0].FamilyID != "family-1" {
		t.Fatalf("unexpected reminders: %#v", got)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func TestListAppliesStatusAndDateRangeTogether(t *testing.T) {
	db, mock := newReminderMockDB(t)
	repo := NewRepository(db)
	status := domainreminder.StatusPending
	from := time.Date(2026, 9, 17, 0, 0, 0, 0, time.UTC)
	to := time.Date(2026, 9, 30, 23, 59, 59, 0, time.UTC)

	mock.ExpectQuery(`SELECT .* FROM "reminders" WHERE family_id = \$1 AND status = \$2 AND scheduled_at >= \$3 AND scheduled_at <= \$4`).
		WithArgs("family-1", status, from, to).
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "family_id", "owner_member_id", "created_by_member_id", "scope", "title", "description", "scheduled_at", "status", "completed_at", "created_at", "updated_at",
		}).AddRow("reminder-1", "family-1", "member-1", "member-1", "PERSONAL", "Pay bill", "", time.Date(2026, 9, 20, 8, 0, 0, 0, time.UTC), "PENDING", nil, from, from))

	got, err := repo.List(context.Background(), domainreminder.ListFilter{FamilyID: "family-1", Status: &status, From: &from, To: &to})
	if err != nil {
		t.Fatalf("list reminders: %v", err)
	}
	if len(got) != 1 || got[0].Status != status {
		t.Fatalf("unexpected reminders: %#v", got)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func TestListRejectsUnscopedFamilyIDBeforeQuery(t *testing.T) {
	db, mock := newReminderMockDB(t)
	repo := NewRepository(db)

	_, err := repo.List(context.Background(), domainreminder.ListFilter{FamilyID: " \t"})
	if err == nil {
		t.Fatal("expected blank family ID to be rejected")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unexpected database query: %v", err)
	}
}

func TestUpdateUsesMutableAllowlistAndAllowsNullCompletedAt(t *testing.T) {
	db, mock := newReminderMockDB(t)
	repo := NewRepository(db)
	reminder := reminderFixture()
	reminder.Status = domainreminder.StatusCompleted
	reminder.CompletedAt = nil
	reminder.UpdatedAt = time.Date(2026, 9, 18, 8, 0, 0, 0, time.UTC)
	reminder.Title = "must not be updated"
	reminder.OwnerMemberID = "must-not-mutate-owner"

	mock.ExpectExec(regexp.QuoteMeta(`UPDATE "reminders" SET "completed_at"=$1,"status"=$2,"updated_at"=$3 WHERE id = $4 AND family_id = $5 AND status = $6`)).
		WithArgs(nil, reminder.Status, sqlmock.AnyArg(), reminder.ID, reminder.FamilyID, domainreminder.StatusPending).
		WillReturnResult(sqlmock.NewResult(0, 1))

	if err := repo.Update(context.Background(), reminder); err != nil {
		t.Fatalf("update reminder: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func TestUpdateOwnsFreshUpdatedAtAndPersistsItOnModel(t *testing.T) {
	db, mock := newReminderMockDB(t)
	repo := NewRepository(db)
	reminder := reminderFixture()
	stale := time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)
	reminder.UpdatedAt = stale
	before := time.Now().UTC()
	var persisted time.Time

	mock.ExpectExec(regexp.QuoteMeta(`UPDATE "reminders" SET "completed_at"=$1,"status"=$2,"updated_at"=$3 WHERE id = $4 AND family_id = $5 AND status = $6`)).
		WithArgs(nil, reminder.Status, updatedAtMatcher{got: &persisted, min: before, max: before.Add(5 * time.Second)}, reminder.ID, reminder.FamilyID, domainreminder.StatusPending).
		WillReturnResult(sqlmock.NewResult(0, 1))

	if err := repo.Update(context.Background(), reminder); err != nil {
		t.Fatalf("update reminder: %v", err)
	}
	after := time.Now().UTC()
	if reminder.UpdatedAt.IsZero() || reminder.UpdatedAt.Equal(stale) || reminder.UpdatedAt.Before(before) || reminder.UpdatedAt.After(after) {
		t.Fatalf("expected fresh repository timestamp, got %v (bounds %v..%v)", reminder.UpdatedAt, before, after)
	}
	if !persisted.Equal(reminder.UpdatedAt) {
		t.Fatalf("persisted timestamp %v differs from model %v", persisted, reminder.UpdatedAt)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func TestUpdateReturnsStatusConflictWhenPendingFamilyScopedRowIsMissing(t *testing.T) {
	db, mock := newReminderMockDB(t)
	repo := NewRepository(db)
	reminder := reminderFixture()

	mock.ExpectExec(regexp.QuoteMeta(`UPDATE "reminders" SET "completed_at"=$1,"status"=$2,"updated_at"=$3 WHERE id = $4 AND family_id = $5 AND status = $6`)).
		WithArgs(nil, reminder.Status, sqlmock.AnyArg(), reminder.ID, reminder.FamilyID, domainreminder.StatusPending).
		WillReturnResult(sqlmock.NewResult(0, 0))

	err := repo.Update(context.Background(), reminder)
	if !errors.Is(err, domainreminder.ErrStatusConflict) {
		t.Fatalf("expected status conflict, got %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func TestUpdateRejectsNilReminderBeforeQuery(t *testing.T) {
	db, mock := newReminderMockDB(t)
	repo := NewRepository(db)

	err := repo.Update(context.Background(), nil)
	if !errors.Is(err, domainreminder.ErrReminderRequired) {
		t.Fatalf("expected reminder required, got %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unexpected database query: %v", err)
	}
}

func TestUpdateRejectsBlankReminderIDBeforeQuery(t *testing.T) {
	db, mock := newReminderMockDB(t)
	repo := NewRepository(db)
	reminder := reminderFixture()
	reminder.ID = " \t"

	err := repo.Update(context.Background(), reminder)
	if !errors.Is(err, domainreminder.ErrReminderIDRequired) {
		t.Fatalf("expected reminder ID required, got %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unexpected database query: %v", err)
	}
}
