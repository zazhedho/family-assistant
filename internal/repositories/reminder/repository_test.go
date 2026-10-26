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
	"github.com/jackc/pgx/v5/pgconn"
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
	mock.ExpectExec(`UPDATE "reminders" SET .* WHERE \(id = \$4 AND space_id = \$5 AND status IN \(\$6,\$7\)\) AND .*deleted_at.*IS NULL`).
		WithArgs(completedAt, domainreminder.StatusCompleted, completedAt, reminderID, spaceID, domainreminder.StatusPending, domainreminder.StatusSent).
		WillReturnResult(sqlmock.NewResult(0, rows))
}

func TestCreatePersistsSpaceAndOptionalAssignee(t *testing.T) {
	db, mock := newReminderMockDB(t)
	repo := NewRepository(db)
	reminder := reminderFixture()

	mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO "reminders" ("id","space_id","created_by_member_id","assignee_member_id","title","description","scheduled_at","status","delivery_provider","delivery_target","completed_at","created_at","updated_at","replace_key") VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14)`)).
		WithArgs(
			reminder.ID,
			reminder.SpaceID,
			reminder.CreatedByMemberID,
			reminder.AssigneeMemberID,
			reminder.Title,
			reminder.Description,
			reminder.ScheduledAt,
			reminder.Status,
			reminder.DeliveryProvider,
			reminder.DeliveryTarget,
			reminder.CompletedAt,
			reminder.CreatedAt,
			reminder.UpdatedAt,
			reminder.ReplaceKey,
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

	mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO "reminders" ("id","space_id","created_by_member_id","assignee_member_id","title","description","scheduled_at","status","delivery_provider","delivery_target","completed_at","created_at","updated_at","replace_key") VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14)`)).
		WithArgs(
			sqlmock.AnyArg(),
			reminder.SpaceID,
			reminder.CreatedByMemberID,
			reminder.AssigneeMemberID,
			reminder.Title,
			reminder.Description,
			reminder.ScheduledAt,
			reminder.Status,
			reminder.DeliveryProvider,
			reminder.DeliveryTarget,
			reminder.CompletedAt,
			reminder.CreatedAt,
			reminder.UpdatedAt,
			reminder.ReplaceKey,
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

func TestCompleteUnfinishedScopesBySpaceAndStatus(t *testing.T) {
	db, mock := newReminderMockDB(t)
	repo := NewRepository(db)
	now := time.Date(2026, 9, 20, 8, 0, 0, 0, time.UTC)

	mock.ExpectExec(`UPDATE "reminders" SET .* WHERE \(id = \$4 AND space_id = \$5 AND status IN \(\$6,\$7\)\) AND .*deleted_at.*IS NULL`).
		WithArgs(now, domainreminder.StatusCompleted, now, reminderID, spaceID, domainreminder.StatusPending, domainreminder.StatusSent).
		WillReturnResult(sqlmock.NewResult(0, 1))

	if err := repo.CompleteUnfinished(context.Background(), spaceID, reminderID, now); err != nil {
		t.Fatalf("complete reminder: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func TestCompleteUnfinishedReturnsStatusConflictWhenNoOpenRow(t *testing.T) {
	db, mock := newReminderMockDB(t)
	repo := NewRepository(db)
	now := time.Date(2026, 9, 20, 8, 0, 0, 0, time.UTC)
	expectComplete(mock, now, 0)

	err := repo.CompleteUnfinished(context.Background(), spaceID, reminderID, now)
	if !errors.Is(err, domainreminder.ErrStatusConflict) {
		t.Fatalf("expected status conflict, got %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func TestCompleteUnfinishedRejectsBlankScopeOrReminderIDBeforeQuery(t *testing.T) {
	db, mock := newReminderMockDB(t)
	repo := NewRepository(db)
	now := time.Now().UTC()

	if err := repo.CompleteUnfinished(context.Background(), " \t", reminderID, now); !errors.Is(err, domainreminder.ErrSpaceIDRequired) {
		t.Fatalf("expected space ID required, got %v", err)
	}
	if err := repo.CompleteUnfinished(context.Background(), spaceID, " \t", now); !errors.Is(err, domainreminder.ErrReminderIDRequired) {
		t.Fatalf("expected reminder ID required, got %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unexpected database query: %v", err)
	}
}

func TestUpdatePendingScopesBySpaceAndPendingStatus(t *testing.T) {
	db, mock := newReminderMockDB(t)
	repo := NewRepository(db)
	now := time.Date(2026, 9, 22, 8, 0, 0, 0, time.UTC)
	title := "Updated title"

	mock.ExpectExec(`UPDATE "reminders" SET .* WHERE .*id = .* AND .*space_id = .* AND .*status = .* AND .*deleted_at.*IS NULL`).
		WillReturnResult(sqlmock.NewResult(0, 1))

	err := repo.UpdatePending(context.Background(), spaceID, reminderID, domainreminder.UpdateFields{Title: &title}, now)
	if err != nil {
		t.Fatalf("update reminder: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func TestSoftDeleteScopesBySpaceAndHidesReminder(t *testing.T) {
	db, mock := newReminderMockDB(t)
	repo := NewRepository(db)
	now := time.Date(2026, 9, 22, 8, 0, 0, 0, time.UTC)

	mock.ExpectExec(`UPDATE "reminders" SET .*deleted_at.*status.*updated_at.* WHERE .*id = .* AND .*space_id = .* AND .*deleted_at.*IS NULL`).
		WillReturnResult(sqlmock.NewResult(0, 1))

	err := repo.SoftDelete(context.Background(), spaceID, reminderID, domainreminder.StatusCancelled, now)
	if err != nil {
		t.Fatalf("delete reminder: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func TestRepositorySatisfiesInterface(t *testing.T) {
	var _ interfacereminder.RepoReminderInterface = (*Repository)(nil)
}

const replaceKeyIndex = "ux_reminders_pending_replace_key"

var replaceInsertSQL = regexp.QuoteMeta(`INSERT INTO "reminders" ("id","space_id","created_by_member_id","assignee_member_id","title","description","scheduled_at","status","delivery_provider","delivery_target","completed_at","created_at","updated_at","replace_key") VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14)`)

const replaceSelectSQL = `SELECT \* FROM "reminders" WHERE \(space_id = \$1 AND replace_key = \$2 AND status IN \(\$3,\$4\)\) AND "reminders"\."deleted_at" IS NULL ORDER BY scheduled_at DESC FOR UPDATE`

func replaceFixture(key string) *domainreminder.Reminder {
	r := reminderFixture()
	r.ReplaceKey = &key
	return r
}

func existingRow(id string, scheduledAt time.Time) *sqlmock.Rows {
	return sqlmock.NewRows(reminderColumns()).AddRow(id, spaceID, memberID, nil, "old", "", scheduledAt, "PENDING", nil, scheduledAt, scheduledAt, nil)
}

func expectReplaceInsert(mock sqlmock.Sqlmock, r *domainreminder.Reminder) *sqlmock.ExpectedExec {
	return mock.ExpectExec(replaceInsertSQL).
		WithArgs(r.ID, r.SpaceID, r.CreatedByMemberID, r.AssigneeMemberID, r.Title, r.Description, r.ScheduledAt, r.Status, r.DeliveryProvider, r.DeliveryTarget, r.CompletedAt, r.CreatedAt, r.UpdatedAt, r.ReplaceKey)
}

func TestCreateIncludesReplaceKeyColumn(t *testing.T) {
	db, mock := newReminderMockDB(t)
	repo := NewRepository(db)
	r := replaceFixture("bill")
	expectReplaceInsert(mock, r).WillReturnResult(sqlmock.NewResult(0, 1))
	if err := repo.Create(context.Background(), r); err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func TestCreateReplacingWithoutKeyInsertsOnly(t *testing.T) {
	db, mock := newReminderMockDB(t)
	repo := NewRepository(db)
	r := reminderFixture()
	expectReplaceInsert(mock, r).WillReturnResult(sqlmock.NewResult(0, 1))

	res, err := repo.CreateReplacing(context.Background(), r, time.Now())
	if err != nil {
		t.Fatalf("create replacing: %v", err)
	}
	if res.Outcome != domainreminder.ReplaceOutcomeNone || res.Reminder != r {
		t.Fatalf("unexpected result %+v", res)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func TestCreateReplacingCreatesWhenNoPending(t *testing.T) {
	db, mock := newReminderMockDB(t)
	repo := NewRepository(db)
	r := replaceFixture("bill")
	mock.ExpectBegin()
	mock.ExpectQuery(replaceSelectSQL).WithArgs(spaceID, "bill", domainreminder.StatusPending, domainreminder.StatusSent).WillReturnRows(sqlmock.NewRows(reminderColumns()))
	expectReplaceInsert(mock, r).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	res, err := repo.CreateReplacing(context.Background(), r, time.Now())
	if err != nil {
		t.Fatalf("create replacing: %v", err)
	}
	if res.Outcome != domainreminder.ReplaceOutcomeCreated || res.Reminder != r || len(res.Replaced) != 0 {
		t.Fatalf("unexpected result %+v", res)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func TestCreateReplacingCancelsEarlierPending(t *testing.T) {
	db, mock := newReminderMockDB(t)
	repo := NewRepository(db)
	r := replaceFixture("bill")
	now := time.Date(2026, 9, 18, 9, 0, 0, 0, time.UTC)
	id1, id2 := "00000000-0000-4000-8000-000000000111", "00000000-0000-4000-8000-000000000112"
	rows := sqlmock.NewRows(reminderColumns()).
		AddRow(id1, spaceID, memberID, nil, "a", "", r.ScheduledAt.Add(-time.Hour), "PENDING", nil, now, now, nil).
		AddRow(id2, spaceID, memberID, nil, "b", "", r.ScheduledAt.Add(-2*time.Hour), "PENDING", nil, now, now, nil)
	mock.ExpectBegin()
	mock.ExpectQuery(replaceSelectSQL).WithArgs(spaceID, "bill", domainreminder.StatusPending, domainreminder.StatusSent).WillReturnRows(rows)
	mock.ExpectExec(`UPDATE "reminders" SET .* WHERE id IN \(\$4,\$5\)`).
		WithArgs(now, domainreminder.StatusCancelled, now, id1, id2).
		WillReturnResult(sqlmock.NewResult(0, 2))
	expectReplaceInsert(mock, r).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	res, err := repo.CreateReplacing(context.Background(), r, now)
	if err != nil {
		t.Fatalf("create replacing: %v", err)
	}
	if res.Outcome != domainreminder.ReplaceOutcomeReplaced || len(res.Replaced) != 2 {
		t.Fatalf("unexpected result %+v", res)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func TestCreateReplacingTieReplaces(t *testing.T) {
	db, mock := newReminderMockDB(t)
	repo := NewRepository(db)
	r := replaceFixture("bill")
	now := time.Date(2026, 9, 18, 9, 0, 0, 0, time.UTC)
	id1 := "00000000-0000-4000-8000-000000000111"
	mock.ExpectBegin()
	mock.ExpectQuery(replaceSelectSQL).WithArgs(spaceID, "bill", domainreminder.StatusPending, domainreminder.StatusSent).WillReturnRows(existingRow(id1, r.ScheduledAt))
	mock.ExpectExec(`UPDATE "reminders" SET .* WHERE id IN \(\$4\)`).
		WithArgs(now, domainreminder.StatusCancelled, now, id1).
		WillReturnResult(sqlmock.NewResult(0, 1))
	expectReplaceInsert(mock, r).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	res, err := repo.CreateReplacing(context.Background(), r, now)
	if err != nil {
		t.Fatalf("create replacing: %v", err)
	}
	if res.Outcome != domainreminder.ReplaceOutcomeReplaced || len(res.Replaced) != 1 {
		t.Fatalf("unexpected result %+v", res)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func TestCreateReplacingKeepsLaterPendingReminder(t *testing.T) {
	db, mock := newReminderMockDB(t)
	repo := NewRepository(db)
	r := replaceFixture("bill")
	laterID := "00000000-0000-4000-8000-000000000113"
	mock.ExpectBegin()
	mock.ExpectQuery(replaceSelectSQL).WithArgs(spaceID, "bill", domainreminder.StatusPending, domainreminder.StatusSent).WillReturnRows(existingRow(laterID, r.ScheduledAt.Add(time.Hour)))
	mock.ExpectCommit()

	res, err := repo.CreateReplacing(context.Background(), r, time.Now())
	if err != nil {
		t.Fatalf("create replacing: %v", err)
	}
	if res.Outcome != domainreminder.ReplaceOutcomeKeptExisting || res.Reminder == nil || res.Reminder.ID != laterID {
		t.Fatalf("unexpected result %+v", res)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func TestCreateReplacingOnlyTouchesSameSpaceAndKey(t *testing.T) {
	db, mock := newReminderMockDB(t)
	repo := NewRepository(db)
	r := replaceFixture("bill")
	now := time.Date(2026, 9, 18, 9, 0, 0, 0, time.UTC)
	id1 := "00000000-0000-4000-8000-000000000111"
	mock.ExpectBegin()
	mock.ExpectQuery(replaceSelectSQL).WithArgs(spaceID, "bill", "PENDING", "SENT").WillReturnRows(existingRow(id1, r.ScheduledAt.Add(-time.Hour)))
	mock.ExpectExec(`UPDATE "reminders" SET .* WHERE id IN \(\$4\)`).
		WithArgs(now, domainreminder.StatusCancelled, now, id1).
		WillReturnResult(sqlmock.NewResult(0, 1))
	expectReplaceInsert(mock, r).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	if _, err := repo.CreateReplacing(context.Background(), r, now); err != nil {
		t.Fatalf("create replacing: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func TestCreateReplacingCompletesEarlierSentReminder(t *testing.T) {
	db, mock := newReminderMockDB(t)
	repo := NewRepository(db)
	r := replaceFixture("bill")
	now := time.Date(2026, 9, 18, 9, 0, 0, 0, time.UTC)
	sentID := "00000000-0000-4000-8000-000000000121"
	rows := sqlmock.NewRows(reminderColumns()).
		AddRow(sentID, spaceID, memberID, nil, "old", "", r.ScheduledAt.Add(-4*time.Hour), "SENT", nil, now, now, nil)
	mock.ExpectBegin()
	mock.ExpectQuery(replaceSelectSQL).WithArgs(spaceID, "bill", domainreminder.StatusPending, domainreminder.StatusSent).WillReturnRows(rows)
	mock.ExpectExec(`UPDATE "reminders" SET .*completed_at.*status.*updated_at.* WHERE id IN \(\$4\)`).
		WithArgs(now, domainreminder.StatusCompleted, now, sentID).
		WillReturnResult(sqlmock.NewResult(0, 1))
	expectReplaceInsert(mock, r).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	res, err := repo.CreateReplacing(context.Background(), r, now)
	if err != nil {
		t.Fatalf("create replacing: %v", err)
	}
	if res.Outcome != domainreminder.ReplaceOutcomeCreated || len(res.Replaced) != 0 || len(res.Completed) != 1 || res.Completed[0].ID != sentID {
		t.Fatalf("unexpected result %+v", res)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func TestCreateReplacingCancelsPendingAndCompletesSent(t *testing.T) {
	db, mock := newReminderMockDB(t)
	repo := NewRepository(db)
	r := replaceFixture("bill")
	now := time.Date(2026, 9, 18, 9, 0, 0, 0, time.UTC)
	pendingID, sentID := "00000000-0000-4000-8000-000000000122", "00000000-0000-4000-8000-000000000123"
	rows := sqlmock.NewRows(reminderColumns()).
		AddRow(pendingID, spaceID, memberID, nil, "a", "", r.ScheduledAt.Add(-time.Hour), "PENDING", nil, now, now, nil).
		AddRow(sentID, spaceID, memberID, nil, "b", "", r.ScheduledAt.Add(-5*time.Hour), "SENT", nil, now, now, nil)
	mock.ExpectBegin()
	mock.ExpectQuery(replaceSelectSQL).WithArgs(spaceID, "bill", domainreminder.StatusPending, domainreminder.StatusSent).WillReturnRows(rows)
	mock.ExpectExec(`UPDATE "reminders" SET .*deleted_at.*status.*updated_at.* WHERE id IN \(\$4\)`).
		WithArgs(now, domainreminder.StatusCancelled, now, pendingID).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(`UPDATE "reminders" SET .*completed_at.*status.*updated_at.* WHERE id IN \(\$4\)`).
		WithArgs(now, domainreminder.StatusCompleted, now, sentID).
		WillReturnResult(sqlmock.NewResult(0, 1))
	expectReplaceInsert(mock, r).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	res, err := repo.CreateReplacing(context.Background(), r, now)
	if err != nil {
		t.Fatalf("create replacing: %v", err)
	}
	if res.Outcome != domainreminder.ReplaceOutcomeReplaced || len(res.Replaced) != 1 || res.Replaced[0].ID != pendingID || len(res.Completed) != 1 || res.Completed[0].ID != sentID {
		t.Fatalf("unexpected result %+v", res)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func TestCreateReplacingKeptExistingLeavesSentUntouched(t *testing.T) {
	db, mock := newReminderMockDB(t)
	repo := NewRepository(db)
	r := replaceFixture("bill")
	laterID, sentID := "00000000-0000-4000-8000-000000000124", "00000000-0000-4000-8000-000000000125"
	rows := sqlmock.NewRows(reminderColumns()).
		AddRow(laterID, spaceID, memberID, nil, "later", "", r.ScheduledAt.Add(time.Hour), "PENDING", nil, r.ScheduledAt, r.ScheduledAt, nil).
		AddRow(sentID, spaceID, memberID, nil, "sent", "", r.ScheduledAt.Add(-5*time.Hour), "SENT", nil, r.ScheduledAt, r.ScheduledAt, nil)
	mock.ExpectBegin()
	mock.ExpectQuery(replaceSelectSQL).WithArgs(spaceID, "bill", domainreminder.StatusPending, domainreminder.StatusSent).WillReturnRows(rows)
	mock.ExpectCommit()

	res, err := repo.CreateReplacing(context.Background(), r, time.Now())
	if err != nil {
		t.Fatalf("create replacing: %v", err)
	}
	if res.Outcome != domainreminder.ReplaceOutcomeKeptExisting || res.Reminder == nil || res.Reminder.ID != laterID || len(res.Completed) != 0 {
		t.Fatalf("unexpected result %+v", res)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func TestCreateReplacingIgnoresSentScheduledAfterNewReminder(t *testing.T) {
	db, mock := newReminderMockDB(t)
	repo := NewRepository(db)
	r := replaceFixture("bill")
	sentID := "00000000-0000-4000-8000-000000000126"
	rows := sqlmock.NewRows(reminderColumns()).
		AddRow(sentID, spaceID, memberID, nil, "sent", "", r.ScheduledAt.Add(time.Hour), "SENT", nil, r.ScheduledAt, r.ScheduledAt, nil)
	mock.ExpectBegin()
	mock.ExpectQuery(replaceSelectSQL).WithArgs(spaceID, "bill", domainreminder.StatusPending, domainreminder.StatusSent).WillReturnRows(rows)
	expectReplaceInsert(mock, r).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	res, err := repo.CreateReplacing(context.Background(), r, time.Now())
	if err != nil {
		t.Fatalf("create replacing: %v", err)
	}
	if res.Outcome != domainreminder.ReplaceOutcomeCreated || len(res.Completed) != 0 {
		t.Fatalf("unexpected result %+v", res)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func uniqueViolation() error {
	return &pgconn.PgError{Code: "23505", ConstraintName: replaceKeyIndex}
}

func TestCreateReplacingRetriesOnceOnUniqueViolation(t *testing.T) {
	db, mock := newReminderMockDB(t)
	repo := NewRepository(db)
	r := replaceFixture("bill")
	mock.ExpectBegin()
	mock.ExpectQuery(replaceSelectSQL).WillReturnRows(sqlmock.NewRows(reminderColumns()))
	expectReplaceInsert(mock, r).WillReturnError(uniqueViolation())
	mock.ExpectRollback()
	mock.ExpectBegin()
	mock.ExpectQuery(replaceSelectSQL).WillReturnRows(sqlmock.NewRows(reminderColumns()))
	expectReplaceInsert(mock, r).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	res, err := repo.CreateReplacing(context.Background(), r, time.Now())
	if err != nil {
		t.Fatalf("create replacing: %v", err)
	}
	if res.Outcome != domainreminder.ReplaceOutcomeCreated {
		t.Fatalf("unexpected result %+v", res)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func TestCreateReplacingReturnsConflictAfterSecondViolation(t *testing.T) {
	db, mock := newReminderMockDB(t)
	repo := NewRepository(db)
	r := replaceFixture("bill")
	for i := 0; i < 2; i++ {
		mock.ExpectBegin()
		mock.ExpectQuery(replaceSelectSQL).WillReturnRows(sqlmock.NewRows(reminderColumns()))
		expectReplaceInsert(mock, r).WillReturnError(uniqueViolation())
		mock.ExpectRollback()
	}

	_, err := repo.CreateReplacing(context.Background(), r, time.Now())
	if !errors.Is(err, domainreminder.ErrReplaceKeyConflict) {
		t.Fatalf("expected ErrReplaceKeyConflict, got %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}
