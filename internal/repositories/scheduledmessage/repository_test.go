package repositoryscheduledmessage

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	domainidentity "family-assistant/internal/domain/identity"
	domainscheduledmessage "family-assistant/internal/domain/scheduledmessage"
	"family-assistant/internal/dto"
	interfacescheduledmessage "family-assistant/internal/interfaces/scheduledmessage"

	"github.com/DATA-DOG/go-sqlmock"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func newScheduledMessageMockDB(t *testing.T) (*gorm.DB, sqlmock.Sqlmock) {
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

func scheduledMessageFixture() (*domainscheduledmessage.Schedule, *domainscheduledmessage.Delivery) {
	start := time.Date(2026, 10, 15, 8, 0, 0, 0, time.UTC)
	schedule := &domainscheduledmessage.Schedule{
		ID: "00000000-0000-4000-8000-000000000101", SpaceID: "00000000-0000-4000-8000-000000000201",
		CreatedByMemberID: "00000000-0000-4000-8000-000000000301", Target: domainscheduledmessage.TargetDM,
		ContentMode: domainscheduledmessage.ContentModeFixed, Content: "Hello", Recurrence: domainscheduledmessage.RecurrenceOnce,
		Timezone: "UTC", StartAt: start, Status: domainscheduledmessage.ScheduleStatusActive, CreatedAt: start, UpdatedAt: start,
	}
	delivery := &domainscheduledmessage.Delivery{
		ID: "00000000-0000-4000-8000-000000000401", ScheduledMessageID: schedule.ID, ScheduledAt: start,
		Status: domainscheduledmessage.DeliveryStatusPending, CreatedAt: start, UpdatedAt: start,
	}
	return schedule, delivery
}

func TestCreatePersistsScheduleAndInitialOccurrenceInOneTransaction(t *testing.T) {
	db, mock := newScheduledMessageMockDB(t)
	repo := NewRepository(db)
	schedule, delivery := scheduledMessageFixture()

	mock.ExpectBegin()
	mock.ExpectExec(`INSERT INTO "scheduled_messages"`).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(`INSERT INTO "scheduled_message_deliveries"`).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()
	if err := repo.Create(context.Background(), schedule, delivery); err != nil {
		t.Fatalf("create schedule and first occurrence: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("SQL expectations: %v", err)
	}
}

func TestCreateRollsBackScheduleWhenInitialOccurrenceInsertFails(t *testing.T) {
	db, mock := newScheduledMessageMockDB(t)
	repo := NewRepository(db)
	schedule, delivery := scheduledMessageFixture()
	insertErr := errors.New("delivery insert failed")

	mock.ExpectBegin()
	mock.ExpectExec(`INSERT INTO "scheduled_messages"`).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(`INSERT INTO "scheduled_message_deliveries"`).WillReturnError(insertErr)
	mock.ExpectRollback()
	if err := repo.Create(context.Background(), schedule, delivery); !errors.Is(err, insertErr) {
		t.Fatalf("create error = %v, want insert error", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("SQL expectations: %v", err)
	}
}

func TestClaimDueRequiresActiveSpaceScheduleAndPersistsLeaseToken(t *testing.T) {
	db, mock := newScheduledMessageMockDB(t)
	repo := NewSchedulerRepository(db)
	now := time.Date(2026, 10, 15, 8, 1, 0, 0, time.UTC)

	mock.ExpectBegin()
	mock.ExpectQuery(`SELECT .*scheduled_messages.*FOR UPDATE OF .*SKIP LOCKED`).
		WithArgs(domainscheduledmessage.ScheduleStatusActive, "ACTIVE", domainscheduledmessage.DeliveryStatusPending, now, now, 10).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow("00000000-0000-4000-8000-000000000101"))
	mock.ExpectQuery(`SELECT .*scheduled_message_deliveries.*FOR UPDATE.*SKIP LOCKED`).
		WithArgs("00000000-0000-4000-8000-000000000101", domainscheduledmessage.DeliveryStatusPending, now, now, 10).
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "scheduled_message_id", "scheduled_at", "content_snapshot", "generation_attempts", "delivery_attempts",
			"next_attempt_at", "lease_token", "lease_until", "status", "failure_category", "sent_at", "created_at", "updated_at",
		}).AddRow("00000000-0000-4000-8000-000000000401", "00000000-0000-4000-8000-000000000101", now,
			nil, 0, 0, now, nil, nil, domainscheduledmessage.DeliveryStatusPending, nil, nil, now, now))
	mock.ExpectExec(`UPDATE "scheduled_message_deliveries" SET .*lease_token.*lease_until.*WHERE .*status.*next_attempt_at.*`).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	claimed, err := repo.ClaimDue(context.Background(), now, time.Minute, 10)
	if err != nil {
		t.Fatalf("claim due deliveries: %v", err)
	}
	if len(claimed) != 1 || claimed[0].LeaseToken == nil || claimed[0].LeaseExpiresAt == nil {
		t.Fatalf("claimed lease = %#v, want persisted token and expiry", claimed)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("SQL expectations: %v", err)
	}
}

func TestDispatchCommitFailureReturnsUncertainOutcome(t *testing.T) {
	db, mock := newScheduledMessageMockDB(t)
	repo := NewSchedulerRepository(db)
	now := time.Date(2026, 10, 15, 8, 1, 0, 0, time.UTC)
	callbackCalled := false

	mock.ExpectBegin()
	mock.ExpectQuery(`SELECT scheduled_messages.space_id FROM "scheduled_message_deliveries" JOIN scheduled_messages ON scheduled_messages.id = scheduled_message_deliveries.scheduled_message_id WHERE scheduled_message_deliveries.id = .*status.*lease_token.*lease_until.*`).
		WillReturnRows(sqlmock.NewRows([]string{"space_id"}).AddRow("00000000-0000-4000-8000-000000000201"))
	mock.ExpectQuery(`SELECT id, status FROM "spaces" WHERE id = .*deleted_at IS NULL.*FOR SHARE`).
		WillReturnRows(sqlmock.NewRows([]string{"id", "status"}).AddRow("00000000-0000-4000-8000-000000000201", "ACTIVE"))
	mock.ExpectQuery(`SELECT .*FROM "scheduled_messages".*FOR UPDATE`).
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "space_id", "created_by_member_id", "target", "recipient_member_id", "space_channel_id", "content_mode", "content",
			"recurrence", "timezone", "start_at", "next_scheduled_at", "status", "unsubscribed_by_member_id", "unsubscribed_at", "created_at", "updated_at",
		}).AddRow("00000000-0000-4000-8000-000000000101", "00000000-0000-4000-8000-000000000201",
			"00000000-0000-4000-8000-000000000301", domainscheduledmessage.TargetDM,
			"00000000-0000-4000-8000-000000000501", nil, domainscheduledmessage.ContentModeFixed, "Hello",
			domainscheduledmessage.RecurrenceOnce, "UTC", now, nil, domainscheduledmessage.ScheduleStatusActive, nil, nil, now, now))
	mock.ExpectQuery(`SELECT .*FROM "scheduled_message_deliveries".*FOR UPDATE`).
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "scheduled_message_id", "scheduled_at", "content_snapshot", "generation_attempts", "delivery_attempts",
			"next_attempt_at", "lease_token", "lease_until", "status", "failure_category", "sent_at", "created_at", "updated_at",
		}).AddRow("00000000-0000-4000-8000-000000000401", "00000000-0000-4000-8000-000000000101", now,
			"Hello", 0, 0, now, "claim-token", now.Add(time.Minute), domainscheduledmessage.DeliveryStatusPending, nil, nil, now, now))
	mock.ExpectQuery(`SELECT \* FROM "space_members" WHERE .*id = .*space_id.*status.*deleted_at IS NULL.*FOR SHARE`).
		WillReturnRows(sqlmock.NewRows([]string{"id", "space_id", "status", "role_id"}).AddRow("00000000-0000-4000-8000-000000000301", "00000000-0000-4000-8000-000000000201", "ACTIVE", "role-1"))
	mock.ExpectQuery(`SELECT p.id FROM permissions p JOIN role_permissions rp ON rp.permission_id = p.id JOIN roles r ON r.id = rp.role_id WHERE rp.role_id = .*p.resource = .*p.action = .*FOR SHARE OF r, rp, p`).
		WithArgs("role-1", "scheduled_messages", "create").
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow("permission-1"))
	mock.ExpectQuery(`SELECT .*external_identities.*`).
		WillReturnRows(sqlmock.NewRows([]string{"id", "user_id", "provider", "external_id", "status", "verified_at", "created_at", "updated_at", "deleted_at"}).
			AddRow("identity-1", "user-1", "hermes", "628123456789@s.whatsapp.net", "ACTIVE", now, now, now, nil))
	mock.ExpectExec(`UPDATE "scheduled_message_deliveries" SET .* WHERE .*lease_token.*`).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(`UPDATE "scheduled_messages" SET .*next_scheduled_at.*status.*WHERE .*status.*`).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit().WillReturnError(errors.New("connection lost after send"))

	err := repo.DispatchAndFinalize(context.Background(), "00000000-0000-4000-8000-000000000401", "claim-token", now, nil,
		func(context.Context, *domainscheduledmessage.Schedule, *domainscheduledmessage.Delivery, *domainscheduledmessage.SpaceChannel, *domainidentity.ExternalIdentity) (time.Time, error) {
			callbackCalled = true
			return now, nil
		})
	if !callbackCalled {
		t.Fatal("dispatch callback was not called")
	}
	if !errors.Is(err, interfacescheduledmessage.ErrDispatchOutcomeUncertain) {
		t.Fatalf("dispatch error = %v, want uncertain outcome", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("SQL expectations: %v", err)
	}
}

func TestDispatchAndFinalizeTreatsPostSendNextOccurrenceFailureAsUncertain(t *testing.T) {
	db, mock := newScheduledMessageMockDB(t)
	repo := NewSchedulerRepository(db)
	now := time.Date(2026, 10, 15, 8, 1, 0, 0, time.UTC)
	leaseUntil := now.Add(time.Minute)
	factoryErr := errors.New("next calendar occurrence failed")
	callbackCalled := false
	factoryCalled := false

	mock.ExpectBegin()
	mock.ExpectQuery(`SELECT scheduled_messages.space_id FROM "scheduled_message_deliveries" JOIN scheduled_messages ON scheduled_messages.id = scheduled_message_deliveries.scheduled_message_id WHERE scheduled_message_deliveries.id = .*status.*lease_token.*lease_until.*`).
		WillReturnRows(sqlmock.NewRows([]string{"space_id"}).AddRow("00000000-0000-4000-8000-000000000201"))
	mock.ExpectQuery(`SELECT id, status FROM "spaces" WHERE id = .*deleted_at IS NULL.*FOR SHARE`).
		WillReturnRows(sqlmock.NewRows([]string{"id", "status"}).AddRow("00000000-0000-4000-8000-000000000201", "ACTIVE"))
	mock.ExpectQuery(`SELECT .*FROM "scheduled_messages".*FOR UPDATE OF "scheduled_messages"`).
		WillReturnRows(sqlmock.NewRows([]string{"id", "space_id", "created_by_member_id", "target", "recipient_member_id", "content_mode", "content", "recurrence", "timezone", "start_at", "status"}).
			AddRow("00000000-0000-4000-8000-000000000101", "00000000-0000-4000-8000-000000000201", "00000000-0000-4000-8000-000000000301",
				domainscheduledmessage.TargetDM, "00000000-0000-4000-8000-000000000501", domainscheduledmessage.ContentModeFixed, "Hello", domainscheduledmessage.RecurrenceOnce, "UTC", now, domainscheduledmessage.ScheduleStatusActive))
	mock.ExpectQuery(`SELECT \* FROM "scheduled_message_deliveries" WHERE id = .*status.*lease_token.*lease_until.*FOR UPDATE`).
		WillReturnRows(sqlmock.NewRows([]string{"id", "scheduled_message_id", "scheduled_at", "status", "lease_token", "lease_until"}).
			AddRow("00000000-0000-4000-8000-000000000401", "00000000-0000-4000-8000-000000000101", now, domainscheduledmessage.DeliveryStatusPending, "claim-token", leaseUntil))
	mock.ExpectQuery(`SELECT \* FROM "space_members" WHERE .*id = .*space_id.*status.*deleted_at IS NULL.*FOR SHARE`).
		WillReturnRows(sqlmock.NewRows([]string{"id", "space_id", "status", "role_id"}).AddRow("00000000-0000-4000-8000-000000000301", "00000000-0000-4000-8000-000000000201", "ACTIVE", "role-1"))
	mock.ExpectQuery(`SELECT p.id FROM permissions p JOIN role_permissions rp ON rp.permission_id = p.id JOIN roles r ON r.id = rp.role_id WHERE rp.role_id = .*p.resource = .*p.action = .*FOR SHARE OF r, rp, p`).
		WithArgs("role-1", "scheduled_messages", "create").
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow("permission-1"))
	mock.ExpectQuery(`SELECT .*external_identities.*`).
		WillReturnRows(sqlmock.NewRows([]string{"id", "user_id", "provider", "external_id", "status", "verified_at", "created_at", "updated_at", "deleted_at"}).
			AddRow("identity-1", "user-1", "hermes", "628123456789@s.whatsapp.net", "ACTIVE", now, now, now, nil))
	mock.ExpectRollback()

	err := repo.DispatchAndFinalize(context.Background(), "00000000-0000-4000-8000-000000000401", "claim-token", now,
		func(_ *domainscheduledmessage.Schedule, finalizedAt time.Time) (*domainscheduledmessage.Delivery, error) {
			factoryCalled = true
			if !finalizedAt.Equal(now.Add(2 * time.Minute)) {
				t.Fatalf("factory time = %s, want post-send time", finalizedAt)
			}
			return nil, factoryErr
		},
		func(context.Context, *domainscheduledmessage.Schedule, *domainscheduledmessage.Delivery, *domainscheduledmessage.SpaceChannel, *domainidentity.ExternalIdentity) (time.Time, error) {
			callbackCalled = true
			return now.Add(2 * time.Minute), nil
		})
	if !callbackCalled || !factoryCalled || !errors.Is(err, interfacescheduledmessage.ErrDispatchOutcomeUncertain) || !errors.Is(err, factoryErr) {
		t.Fatalf("dispatch/factory/error = %t/%t/%v, want uncertain outcome matching the sentinel and factory error", callbackCalled, factoryCalled, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("SQL expectations: %v", err)
	}
}

func TestDispatchRejectsInactiveCreatorAsInvalidTarget(t *testing.T) {
	db, mock := newScheduledMessageMockDB(t)
	repo := NewSchedulerRepository(db)
	now := time.Date(2026, 10, 15, 8, 1, 0, 0, time.UTC)
	callbackCalled := false

	mock.ExpectBegin()
	mock.ExpectQuery(`SELECT scheduled_messages.space_id FROM "scheduled_message_deliveries" JOIN scheduled_messages ON scheduled_messages.id = scheduled_message_deliveries.scheduled_message_id WHERE scheduled_message_deliveries.id = .*status.*lease_token.*lease_until.*`).
		WillReturnRows(sqlmock.NewRows([]string{"space_id"}).AddRow("00000000-0000-4000-8000-000000000201"))
	mock.ExpectQuery(`SELECT id, status FROM "spaces" WHERE id = .*deleted_at IS NULL.*FOR SHARE`).
		WillReturnRows(sqlmock.NewRows([]string{"id", "status"}).AddRow("00000000-0000-4000-8000-000000000201", "ACTIVE"))
	mock.ExpectQuery(`SELECT .*FROM "scheduled_messages".*FOR UPDATE OF "scheduled_messages"`).
		WillReturnRows(sqlmock.NewRows([]string{"id", "space_id", "created_by_member_id", "target", "content_mode", "content", "recurrence", "timezone", "start_at", "status"}).
			AddRow("00000000-0000-4000-8000-000000000101", "00000000-0000-4000-8000-000000000201", "00000000-0000-4000-8000-000000000301", domainscheduledmessage.TargetDM, domainscheduledmessage.ContentModeFixed, "Hello", domainscheduledmessage.RecurrenceOnce, "UTC", now, domainscheduledmessage.ScheduleStatusActive))
	mock.ExpectQuery(`SELECT \* FROM "scheduled_message_deliveries" WHERE id = .*status.*lease_token.*lease_until.*FOR UPDATE`).
		WillReturnRows(sqlmock.NewRows([]string{"id", "scheduled_message_id", "scheduled_at", "status", "lease_token", "lease_until"}).
			AddRow("00000000-0000-4000-8000-000000000401", "00000000-0000-4000-8000-000000000101", now, domainscheduledmessage.DeliveryStatusPending, "claim-token", now.Add(time.Minute)))
	mock.ExpectQuery(`SELECT \* FROM "space_members" WHERE .*id = .*space_id.*status.*deleted_at IS NULL.*FOR SHARE`).
		WillReturnRows(sqlmock.NewRows([]string{"id", "space_id", "status"}))
	mock.ExpectRollback()

	err := repo.DispatchAndFinalize(context.Background(), "00000000-0000-4000-8000-000000000401", "claim-token", now, nil,
		func(context.Context, *domainscheduledmessage.Schedule, *domainscheduledmessage.Delivery, *domainscheduledmessage.SpaceChannel, *domainidentity.ExternalIdentity) (time.Time, error) {
			callbackCalled = true
			return now, nil
		})
	if callbackCalled || !errors.Is(err, interfacescheduledmessage.ErrInvalidTarget) {
		t.Fatalf("callback called=%v, error=%v; want invalid current creator before bridge", callbackCalled, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("SQL expectations: %v", err)
	}
}

func TestDispatchRejectsCreatorAfterCurrentCreatePermissionIsRevoked(t *testing.T) {
	db, mock := newScheduledMessageMockDB(t)
	repo := NewSchedulerRepository(db)
	now := time.Date(2026, 10, 15, 8, 1, 0, 0, time.UTC)
	callbackCalled := false

	mock.ExpectBegin()
	mock.ExpectQuery(`SELECT scheduled_messages.space_id FROM "scheduled_message_deliveries" JOIN scheduled_messages ON scheduled_messages.id = scheduled_message_deliveries.scheduled_message_id WHERE scheduled_message_deliveries.id = .*status.*lease_token.*lease_until.*`).
		WillReturnRows(sqlmock.NewRows([]string{"space_id"}).AddRow("00000000-0000-4000-8000-000000000201"))
	mock.ExpectQuery(`SELECT id, status FROM "spaces" WHERE id = .*deleted_at IS NULL.*FOR SHARE`).
		WillReturnRows(sqlmock.NewRows([]string{"id", "status"}).AddRow("00000000-0000-4000-8000-000000000201", "ACTIVE"))
	mock.ExpectQuery(`SELECT .*FROM "scheduled_messages".*FOR UPDATE OF "scheduled_messages"`).
		WillReturnRows(sqlmock.NewRows([]string{"id", "space_id", "created_by_member_id", "target", "content_mode", "content", "recurrence", "timezone", "start_at", "status"}).
			AddRow("00000000-0000-4000-8000-000000000101", "00000000-0000-4000-8000-000000000201", "00000000-0000-4000-8000-000000000301", domainscheduledmessage.TargetDM, domainscheduledmessage.ContentModeFixed, "Hello", domainscheduledmessage.RecurrenceOnce, "UTC", now, domainscheduledmessage.ScheduleStatusActive))
	mock.ExpectQuery(`SELECT \* FROM "scheduled_message_deliveries" WHERE id = .*status.*lease_token.*lease_until.*FOR UPDATE`).
		WillReturnRows(sqlmock.NewRows([]string{"id", "scheduled_message_id", "scheduled_at", "status", "lease_token", "lease_until"}).
			AddRow("00000000-0000-4000-8000-000000000401", "00000000-0000-4000-8000-000000000101", now, domainscheduledmessage.DeliveryStatusPending, "claim-token", now.Add(time.Minute)))
	mock.ExpectQuery(`SELECT \* FROM "space_members" WHERE .*id = .*space_id.*status.*deleted_at IS NULL.*FOR SHARE`).
		WillReturnRows(sqlmock.NewRows([]string{"id", "space_id", "status", "role_id"}).AddRow("00000000-0000-4000-8000-000000000301", "00000000-0000-4000-8000-000000000201", "ACTIVE", "role-1"))
	mock.ExpectQuery(`SELECT p.id FROM permissions p JOIN role_permissions rp ON rp.permission_id = p.id JOIN roles r ON r.id = rp.role_id WHERE rp.role_id = .*p.resource = .*p.action = .*FOR SHARE OF r, rp, p`).
		WithArgs("role-1", "scheduled_messages", "create").
		WillReturnRows(sqlmock.NewRows([]string{"id"}))
	mock.ExpectRollback()

	err := repo.DispatchAndFinalize(context.Background(), "00000000-0000-4000-8000-000000000401", "claim-token", now, nil,
		func(context.Context, *domainscheduledmessage.Schedule, *domainscheduledmessage.Delivery, *domainscheduledmessage.SpaceChannel, *domainidentity.ExternalIdentity) (time.Time, error) {
			callbackCalled = true
			return now, nil
		})
	if callbackCalled || !errors.Is(err, interfacescheduledmessage.ErrInvalidTarget) {
		t.Fatalf("callback called=%v, error=%v; want current permission denial before bridge", callbackCalled, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("SQL expectations: %v", err)
	}
}

func TestLoadClaimedRejectsCreatorWithoutCurrentCreatePermission(t *testing.T) {
	db, mock := newScheduledMessageMockDB(t)
	repo := NewSchedulerRepository(db)
	now := time.Date(2026, 10, 15, 8, 1, 0, 0, time.UTC)
	const deliveryID = "00000000-0000-4000-8000-000000000401"
	const scheduleID = "00000000-0000-4000-8000-000000000101"
	const spaceID = "00000000-0000-4000-8000-000000000201"
	const creatorID = "00000000-0000-4000-8000-000000000301"

	mock.ExpectQuery(`SELECT \* FROM "scheduled_message_deliveries" WHERE id = .*status.*lease_token.*lease_until.*`).
		WithArgs(deliveryID, domainscheduledmessage.DeliveryStatusPending, "claim-token", now, 1).
		WillReturnRows(sqlmock.NewRows([]string{"id", "scheduled_message_id", "status", "lease_token", "lease_until"}).
			AddRow(deliveryID, scheduleID, domainscheduledmessage.DeliveryStatusPending, "claim-token", now.Add(time.Minute)))
	mock.ExpectQuery(`SELECT \* FROM "scheduled_messages" WHERE scheduled_messages.id = .*status.*`).
		WithArgs(scheduleID, domainscheduledmessage.ScheduleStatusActive, 1).
		WillReturnRows(sqlmock.NewRows([]string{"id", "space_id", "created_by_member_id", "target", "content_mode", "content", "recurrence", "timezone", "start_at", "status"}).
			AddRow(scheduleID, spaceID, creatorID, domainscheduledmessage.TargetDM, domainscheduledmessage.ContentModeFixed, "Hello", domainscheduledmessage.RecurrenceOnce, "UTC", now, domainscheduledmessage.ScheduleStatusActive))
	mock.ExpectQuery(`SELECT "status" FROM "spaces" WHERE id = .*deleted_at IS NULL.*`).
		WithArgs(spaceID, 1).
		WillReturnRows(sqlmock.NewRows([]string{"status"}).AddRow(spaceActiveStatus))
	mock.ExpectQuery(`SELECT EXISTS \(SELECT 1 FROM space_members`).
		WithArgs(creatorID, spaceID, memberActiveStatus, "scheduled_messages", "create").
		WillReturnRows(sqlmock.NewRows([]string{"allowed"}).AddRow(false))

	_, _, _, _, _, err := repo.LoadClaimed(context.Background(), deliveryID, "claim-token", now)
	if !errors.Is(err, interfacescheduledmessage.ErrInvalidTarget) {
		t.Fatalf("LoadClaimed() error = %v, want current permission denial", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("SQL expectations: %v", err)
	}
}

func TestLoadClaimedReturnsLiveClaimForInvalidTargetWhenSpaceIsInactive(t *testing.T) {
	db, mock := newScheduledMessageMockDB(t)
	repo := NewSchedulerRepository(db)
	now := time.Date(2026, 10, 15, 8, 1, 0, 0, time.UTC)
	const deliveryID = "00000000-0000-4000-8000-000000000401"
	const scheduleID = "00000000-0000-4000-8000-000000000101"
	const spaceID = "00000000-0000-4000-8000-000000000201"
	const creatorID = "00000000-0000-4000-8000-000000000301"

	mock.ExpectQuery(`SELECT \* FROM "scheduled_message_deliveries" WHERE id = .*status.*lease_token.*lease_until.*`).
		WithArgs(deliveryID, domainscheduledmessage.DeliveryStatusPending, "claim-token", now, 1).
		WillReturnRows(sqlmock.NewRows([]string{"id", "scheduled_message_id", "status", "lease_token", "lease_until"}).
			AddRow(deliveryID, scheduleID, domainscheduledmessage.DeliveryStatusPending, "claim-token", now.Add(time.Minute)))
	mock.ExpectQuery(`SELECT \* FROM "scheduled_messages" WHERE scheduled_messages.id = .*status.*`).
		WithArgs(scheduleID, domainscheduledmessage.ScheduleStatusActive, 1).
		WillReturnRows(sqlmock.NewRows([]string{"id", "space_id", "created_by_member_id", "target", "content_mode", "content", "recurrence", "timezone", "start_at", "status"}).
			AddRow(scheduleID, spaceID, creatorID, domainscheduledmessage.TargetDM, domainscheduledmessage.ContentModeFixed, "Hello", domainscheduledmessage.RecurrenceOnce, "UTC", now, domainscheduledmessage.ScheduleStatusActive))
	mock.ExpectQuery(`SELECT "status" FROM "spaces" WHERE id = .*deleted_at IS NULL.*`).
		WithArgs(spaceID, 1).
		WillReturnRows(sqlmock.NewRows([]string{"status"}).AddRow("INACTIVE"))

	schedule, delivery, _, _, _, err := repo.LoadClaimed(context.Background(), deliveryID, "claim-token", now)
	if !errors.Is(err, interfacescheduledmessage.ErrInvalidTarget) || schedule == nil || delivery == nil || delivery.LeaseToken == nil || *delivery.LeaseToken != "claim-token" {
		t.Fatalf("LoadClaimed() = schedule %v, delivery %v, error %v; want token-bearing rows and invalid target", schedule, delivery, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("SQL expectations: %v", err)
	}
}

func TestDispatchRejectsInactiveSpaceAsInvalidTargetBeforeBridge(t *testing.T) {
	db, mock := newScheduledMessageMockDB(t)
	repo := NewSchedulerRepository(db)
	now := time.Date(2026, 10, 15, 8, 1, 0, 0, time.UTC)
	callbackCalled := false

	mock.ExpectBegin()
	mock.ExpectQuery(`SELECT scheduled_messages.space_id FROM "scheduled_message_deliveries" JOIN scheduled_messages ON scheduled_messages.id = scheduled_message_deliveries.scheduled_message_id WHERE scheduled_message_deliveries.id = .*status.*lease_token.*lease_until.*`).
		WillReturnRows(sqlmock.NewRows([]string{"space_id"}).AddRow("00000000-0000-4000-8000-000000000201"))
	mock.ExpectQuery(`SELECT id, status FROM "spaces" WHERE id = .*deleted_at IS NULL.*FOR SHARE`).
		WillReturnRows(sqlmock.NewRows([]string{"id", "status"}).AddRow("00000000-0000-4000-8000-000000000201", "INACTIVE"))
	mock.ExpectRollback()

	err := repo.DispatchAndFinalize(context.Background(), "00000000-0000-4000-8000-000000000401", "claim-token", now, nil,
		func(context.Context, *domainscheduledmessage.Schedule, *domainscheduledmessage.Delivery, *domainscheduledmessage.SpaceChannel, *domainidentity.ExternalIdentity) (time.Time, error) {
			callbackCalled = true
			return now, nil
		})
	if callbackCalled || !errors.Is(err, interfacescheduledmessage.ErrInvalidTarget) {
		t.Fatalf("callback called=%v, error=%v; want inactive Space invalid-target before bridge", callbackCalled, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("SQL expectations: %v", err)
	}
}

func TestFinalizeInvalidTargetCancellationUnderInactiveSpaceKeepsTokenGuard(t *testing.T) {
	db, mock := newScheduledMessageMockDB(t)
	repo := NewSchedulerRepository(db)
	schedule, delivery := scheduledMessageFixture()
	now := time.Date(2026, 10, 15, 8, 1, 0, 0, time.UTC)
	token, expires := "claim-token", now.Add(time.Minute)
	delivery.LeaseToken, delivery.LeaseExpiresAt, delivery.UpdatedAt = &token, &expires, now
	delivery.Status = domainscheduledmessage.DeliveryStatusCancelled
	category := "invalid_target"
	delivery.FailureCategory = &category
	schedule.Status, schedule.NextScheduledAt, schedule.UpdatedAt = domainscheduledmessage.ScheduleStatusCancelled, nil, now

	mock.ExpectBegin()
	mock.ExpectQuery(`SELECT scheduled_messages.space_id FROM "scheduled_message_deliveries" JOIN scheduled_messages ON scheduled_messages.id = scheduled_message_deliveries.scheduled_message_id WHERE scheduled_message_deliveries.id = .*status.*lease_token.*lease_until.*`).
		WithArgs(delivery.ID, domainscheduledmessage.DeliveryStatusPending, token, now, 1).
		WillReturnRows(sqlmock.NewRows([]string{"space_id"}).AddRow(schedule.SpaceID))
	mock.ExpectQuery(`SELECT id, status FROM "spaces" WHERE id = .*deleted_at IS NULL.*FOR SHARE`).
		WillReturnRows(sqlmock.NewRows([]string{"id", "status"}).AddRow(schedule.SpaceID, "INACTIVE"))
	mock.ExpectQuery(`SELECT .*FROM "scheduled_messages" WHERE id = .*space_id.*status.*FOR UPDATE OF "scheduled_messages"`).
		WithArgs(schedule.ID, schedule.SpaceID, domainscheduledmessage.ScheduleStatusActive, 1).
		WillReturnRows(sqlmock.NewRows([]string{"id", "space_id", "created_by_member_id", "status"}).
			AddRow(schedule.ID, schedule.SpaceID, schedule.CreatedByMemberID, domainscheduledmessage.ScheduleStatusActive))
	mock.ExpectQuery(`SELECT \* FROM "scheduled_message_deliveries" WHERE id = .*scheduled_message_id.*status.*lease_token.*lease_until.*FOR UPDATE`).
		WithArgs(delivery.ID, schedule.ID, domainscheduledmessage.DeliveryStatusPending, token, expires, now, 1).
		WillReturnRows(sqlmock.NewRows([]string{"id", "scheduled_message_id", "status", "lease_token", "lease_until"}).
			AddRow(delivery.ID, schedule.ID, domainscheduledmessage.DeliveryStatusPending, token, expires))
	mock.ExpectExec(`UPDATE "scheduled_message_deliveries" SET .* WHERE .*lease_token.*lease_until.*`).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(`UPDATE "scheduled_messages" SET .*WHERE id = .*status.*`).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	if err := repo.FinalizeOccurrence(context.Background(), delivery, schedule, nil); err != nil {
		t.Fatalf("FinalizeOccurrence() error = %v; want token-guarded cancellation during inactive Space", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("SQL expectations: %v", err)
	}
}

func TestDispatchLocksSpaceBeforeScheduleAndDeliveryAndFinalizesAtomically(t *testing.T) {
	db, mock := newScheduledMessageMockDB(t)
	repo := NewSchedulerRepository(db)
	now := time.Date(2026, 10, 15, 8, 1, 0, 0, time.UTC)
	leaseUntil := now.Add(time.Minute)
	channelID := "00000000-0000-4000-8000-000000000701"
	creatorID := "00000000-0000-4000-8000-000000000301"
	callbackCalled := false
	next := &domainscheduledmessage.Delivery{
		ID: "00000000-0000-4000-8000-000000000402", ScheduledAt: now.Add(24 * time.Hour),
		Status: domainscheduledmessage.DeliveryStatusPending,
	}

	mock.ExpectBegin()
	mock.ExpectQuery(`SELECT scheduled_messages.space_id FROM "scheduled_message_deliveries" JOIN scheduled_messages ON scheduled_messages.id = scheduled_message_deliveries.scheduled_message_id WHERE scheduled_message_deliveries.id = .*status.*lease_token.*lease_until.*`).
		WillReturnRows(sqlmock.NewRows([]string{"space_id"}).AddRow("00000000-0000-4000-8000-000000000201"))
	mock.ExpectQuery(`SELECT id, status FROM "spaces" WHERE id = .*deleted_at IS NULL.*FOR SHARE`).
		WillReturnRows(sqlmock.NewRows([]string{"id", "status"}).AddRow("00000000-0000-4000-8000-000000000201", "ACTIVE"))
	mock.ExpectQuery(`SELECT .*FROM "scheduled_messages".*FOR UPDATE OF "scheduled_messages"`).
		WillReturnRows(sqlmock.NewRows([]string{"id", "space_id", "created_by_member_id", "target", "space_channel_id", "status"}).
			AddRow("00000000-0000-4000-8000-000000000101", "00000000-0000-4000-8000-000000000201", creatorID,
				domainscheduledmessage.TargetGroup, channelID, domainscheduledmessage.ScheduleStatusActive))
	mock.ExpectQuery(`SELECT \* FROM "scheduled_message_deliveries" WHERE id = .*status.*lease_token.*lease_until.*FOR UPDATE`).
		WillReturnRows(sqlmock.NewRows([]string{"id", "scheduled_message_id", "scheduled_at", "status", "lease_token", "lease_until"}).
			AddRow("00000000-0000-4000-8000-000000000401", "00000000-0000-4000-8000-000000000101", now,
				domainscheduledmessage.DeliveryStatusPending, "claim-token", leaseUntil))
	mock.ExpectQuery(`SELECT \* FROM "space_members" WHERE .*id = .*space_id.*status.*FOR SHARE`).
		WillReturnRows(sqlmock.NewRows([]string{"id", "space_id", "status", "role_id"}).AddRow(creatorID, "00000000-0000-4000-8000-000000000201", "ACTIVE", "role-1"))
	mock.ExpectQuery(`SELECT p.id FROM permissions p JOIN role_permissions rp ON rp.permission_id = p.id JOIN roles r ON r.id = rp.role_id WHERE rp.role_id = .*p.resource = .*p.action = .*FOR SHARE OF r, rp, p`).
		WithArgs("role-1", "scheduled_messages", "create").
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow("permission-1"))
	mock.ExpectQuery(`SELECT \* FROM "space_channels" WHERE .*id = .*space_id.*status.*channel.*external_chat_id.*FOR SHARE`).
		WillReturnRows(sqlmock.NewRows([]string{"id", "space_id", "channel", "external_chat_id", "status", "bound_by_member_id"}).
			AddRow(channelID, "00000000-0000-4000-8000-000000000201", "whatsapp", "120363123456@g.us", domainscheduledmessage.SpaceChannelStatusActive, creatorID))
	mock.ExpectExec(`UPDATE "scheduled_message_deliveries" SET .* WHERE .*lease_token.*lease_until.*`).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(`INSERT INTO "scheduled_message_deliveries"`).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(`UPDATE "scheduled_messages" SET .*next_scheduled_at.*status.*WHERE .*status.*`).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	finalizedAt := now.Add(2 * time.Minute)
	err := repo.DispatchAndFinalize(context.Background(), "00000000-0000-4000-8000-000000000401", "claim-token", now,
		func(_ *domainscheduledmessage.Schedule, at time.Time) (*domainscheduledmessage.Delivery, error) {
			if !at.Equal(finalizedAt) {
				t.Errorf("finalization time = %s, want %s", at, finalizedAt)
			}
			next.ScheduledAt = at.Add(24 * time.Hour)
			return next, nil
		},
		func(_ context.Context, _ *domainscheduledmessage.Schedule, _ *domainscheduledmessage.Delivery, channel *domainscheduledmessage.SpaceChannel, identity *domainidentity.ExternalIdentity) (time.Time, error) {
			callbackCalled = true
			if channel == nil || identity != nil || channel.ExternalChatID != "120363123456@g.us" {
				t.Errorf("dispatch target = channel %#v, identity %#v", channel, identity)
			}
			return finalizedAt, nil
		})
	if err != nil {
		t.Fatalf("dispatch and finalize recurring occurrence: %v", err)
	}
	if !callbackCalled {
		t.Fatal("dispatch callback was not called")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("SQL expectations: %v", err)
	}
}

func TestListTargetsSelectsVerifiedWhatsAppIdentityAmongHermesNamespaces(t *testing.T) {
	db, mock := newScheduledMessageMockDB(t)
	repo := NewRepository(db)
	memberID := "00000000-0000-4000-8000-000000000501"

	mock.ExpectQuery(`(?s)SELECT 'DM' AS kind.*external_id ~.*UNION ALL.*FROM space_channels`).
		WithArgs("00000000-0000-4000-8000-000000000201", whatsappIdentifier, "00000000-0000-4000-8000-000000000201").
		WillReturnRows(sqlmock.NewRows([]string{"kind", "member_id", "space_channel_id", "display_name"}).
			AddRow(domainscheduledmessage.TargetDM, memberID, nil, "Alex"))

	targets, err := repo.ListTargets(context.Background(), "00000000-0000-4000-8000-000000000201")
	if err != nil {
		t.Fatalf("list eligible targets: %v", err)
	}
	if len(targets) != 1 || targets[0].MemberID == nil || *targets[0].MemberID != memberID {
		t.Fatalf("targets = %#v, want eligible WhatsApp member", targets)
	}
	serialized, err := json.Marshal(targets)
	if err != nil {
		t.Fatalf("marshal safe targets: %v", err)
	}
	if strings.Contains(string(serialized), "@") {
		t.Fatalf("target response exposed a raw address: %s", serialized)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("SQL expectations: %v", err)
	}
}

func TestListAppliesDMVisibilityBeforeLimit(t *testing.T) {
	db, mock := newScheduledMessageMockDB(t)
	repo := NewRepository(db)
	spaceID := "00000000-0000-4000-8000-000000000201"
	viewerID := "00000000-0000-4000-8000-000000000301"

	mock.ExpectQuery(`(?s)SELECT .*scheduled_messages.space_id = .*scheduled_messages.target <> .*created_by_member_id = .*recipient_member_id = .*LIMIT \$`).
		WithArgs(spaceID, "ACTIVE", domainscheduledmessage.TargetDM, false, viewerID, viewerID, 100).
		WillReturnRows(sqlmock.NewRows([]string{"id", "space_id", "created_by_member_id", "target"}))

	_, err := repo.List(context.Background(), dto.ScheduledMessageListInput{
		Space: spaceID, ViewerMemberID: viewerID, CanViewAllDMs: false,
	})
	if err != nil {
		t.Fatalf("list visible schedules: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("SQL expectations: %v", err)
	}
}

func TestListCanFilterOneScheduleForHistoryView(t *testing.T) {
	db, mock := newScheduledMessageMockDB(t)
	repo := NewRepository(db)
	spaceID := "00000000-0000-4000-8000-000000000201"
	scheduleID := "00000000-0000-4000-8000-000000000101"
	viewerID := "00000000-0000-4000-8000-000000000301"

	mock.ExpectQuery(`(?s)SELECT .*scheduled_messages.space_id = .*scheduled_messages.id = .*scheduled_messages.target <> .*LIMIT \$`).
		WithArgs(spaceID, "ACTIVE", scheduleID, domainscheduledmessage.TargetDM, true, viewerID, viewerID, 100).
		WillReturnRows(sqlmock.NewRows([]string{"id", "space_id", "created_by_member_id", "target"}))

	_, err := repo.List(context.Background(), dto.ScheduledMessageListInput{
		Space: spaceID, ScheduleID: &scheduleID, ViewerMemberID: viewerID, CanViewAllDMs: true,
	})
	if err != nil {
		t.Fatalf("list one visible schedule: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("SQL expectations: %v", err)
	}
}

func TestWhatsAppIdentityRejectsAmbiguousNumericAndTelegramNamespaces(t *testing.T) {
	for _, test := range []struct {
		identity string
		want     bool
	}{
		{identity: "628123456789", want: false},
		{identity: "0@s.whatsapp.net", want: false},
		{identity: "000@lid", want: false},
		{identity: "0001@lid", want: true},
		{identity: "628123456789@s.whatsapp.net", want: true},
		{identity: "628123456789@lid", want: true},
		{identity: "628123456789@telegram", want: false},
		{identity: "628123456789:4@s.whatsapp.net", want: false},
	} {
		if got := isValidWhatsAppIdentity(test.identity); got != test.want {
			t.Errorf("isValidWhatsAppIdentity(%q) = %v, want %v", test.identity, got, test.want)
		}
	}
}

func TestBindCurrentGroupRejectsUntrustedIdentifiersBeforeDatabaseAccess(t *testing.T) {
	db, _ := newScheduledMessageMockDB(t)
	repo := NewRepository(db)
	validAt := time.Date(2026, 10, 15, 8, 0, 0, 0, time.UTC)
	for _, test := range []struct {
		channel  string
		chatID   string
		chatType string
	}{
		{channel: "telegram", chatID: "123@g.us", chatType: "group"},
		{channel: "whatsapp", chatID: "0@g.us", chatType: "group"},
		{channel: "whatsapp", chatID: "000@g.us", chatType: "group"},
		{channel: "whatsapp", chatID: "sender@s.whatsapp.net", chatType: "group"},
		{channel: "whatsapp", chatID: "123@g.us", chatType: "direct"},
	} {
		_, err := repo.BindCurrentGroup(context.Background(), "space", "member", test.channel, test.chatID, test.chatType, validAt)
		if !errors.Is(err, interfacescheduledmessage.ErrInvalidWhatsAppIdentity) {
			t.Errorf("bind (%q, %q, %q) error = %v, want invalid WhatsApp identity", test.channel, test.chatID, test.chatType, err)
		}
	}
}

func TestCancelSerializesOnScheduleBeforePendingDeliveries(t *testing.T) {
	db, mock := newScheduledMessageMockDB(t)
	repo := NewRepository(db)
	at := time.Date(2026, 10, 15, 8, 0, 0, 0, time.UTC)

	mock.ExpectBegin()
	mock.ExpectQuery(`SELECT .*FROM "scheduled_messages" JOIN spaces .*FOR UPDATE OF "scheduled_messages"`).
		WillReturnRows(sqlmock.NewRows([]string{"id", "space_id", "status"}).AddRow(
			"00000000-0000-4000-8000-000000000101", "00000000-0000-4000-8000-000000000201", domainscheduledmessage.ScheduleStatusActive))
	mock.ExpectQuery(`SELECT \* FROM "scheduled_message_deliveries" WHERE scheduled_message_id = .*status.*ORDER BY scheduled_at ASC, id ASC FOR UPDATE`).
		WillReturnRows(sqlmock.NewRows([]string{"id"}))
	mock.ExpectExec(`UPDATE "scheduled_messages" SET .*next_scheduled_at.*status.*updated_at.*WHERE id = .*status = .*`).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	cancelled, err := repo.Cancel(context.Background(), "00000000-0000-4000-8000-000000000201", "00000000-0000-4000-8000-000000000101", at)
	if err != nil {
		t.Fatalf("cancel active schedule: %v", err)
	}
	if len(cancelled) != 0 {
		t.Fatalf("cancelled delivery IDs = %v, want none", cancelled)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("SQL expectations: %v", err)
	}
}

func TestCancelReturnsCancelledDeliveryUUIDs(t *testing.T) {
	db, mock := newScheduledMessageMockDB(t)
	repo := NewRepository(db)
	at := time.Date(2026, 10, 15, 8, 0, 0, 0, time.UTC)
	const scheduleID = "00000000-0000-4000-8000-000000000101"
	const deliveryID = "00000000-0000-4000-8000-000000000401"

	mock.ExpectBegin()
	mock.ExpectQuery(`SELECT .*FROM "scheduled_messages" JOIN spaces .*FOR UPDATE OF "scheduled_messages"`).
		WillReturnRows(sqlmock.NewRows([]string{"id", "space_id", "status"}).AddRow(
			scheduleID, "00000000-0000-4000-8000-000000000201", domainscheduledmessage.ScheduleStatusActive))
	mock.ExpectQuery(`SELECT \* FROM "scheduled_message_deliveries" WHERE scheduled_message_id = .*status.*ORDER BY scheduled_at ASC, id ASC FOR UPDATE`).
		WillReturnRows(sqlmock.NewRows([]string{"id", "scheduled_message_id", "status"}).AddRow(
			deliveryID, scheduleID, domainscheduledmessage.DeliveryStatusPending))
	mock.ExpectExec(`UPDATE "scheduled_message_deliveries" SET .*lease_token.*lease_until.*status.*WHERE .*`).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(`UPDATE "scheduled_messages" SET .*next_scheduled_at.*status.*updated_at.*WHERE id = .*status = .*`).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	cancelled, err := repo.Cancel(context.Background(), "00000000-0000-4000-8000-000000000201", scheduleID, at)
	if err != nil {
		t.Fatalf("cancel active schedule: %v", err)
	}
	if len(cancelled) != 1 || cancelled[0] != deliveryID || cancelled[0] == scheduleID {
		t.Fatalf("cancelled delivery IDs = %v, want delivery UUID %s distinct from schedule UUID %s", cancelled, deliveryID, scheduleID)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("SQL expectations: %v", err)
	}
}

func TestDispatchRejectsExpiredOrWrongTokenBeforeCallingBridge(t *testing.T) {
	db, mock := newScheduledMessageMockDB(t)
	repo := NewSchedulerRepository(db)
	now := time.Date(2026, 10, 15, 8, 1, 0, 0, time.UTC)
	callbackCalled := false

	mock.ExpectBegin()
	mock.ExpectQuery(`SELECT scheduled_messages.space_id FROM "scheduled_message_deliveries" JOIN scheduled_messages ON scheduled_messages.id = scheduled_message_deliveries.scheduled_message_id WHERE scheduled_message_deliveries.id = .*status.*lease_token.*lease_until.*`).
		WillReturnRows(sqlmock.NewRows([]string{"space_id"}))
	mock.ExpectRollback()
	err := repo.DispatchAndFinalize(context.Background(), "delivery", "stale-token", now, nil,
		func(context.Context, *domainscheduledmessage.Schedule, *domainscheduledmessage.Delivery, *domainscheduledmessage.SpaceChannel, *domainidentity.ExternalIdentity) (time.Time, error) {
			callbackCalled = true
			return now, nil
		})
	if callbackCalled || !errors.Is(err, interfacescheduledmessage.ErrClaimConflict) {
		t.Fatalf("callback called=%v, error=%v; want rejected stale token before bridge", callbackCalled, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("SQL expectations: %v", err)
	}
}

func TestUpdateActiveKeepsOldOccurrenceAsHistoryAndInsertsReplacement(t *testing.T) {
	db, mock := newScheduledMessageMockDB(t)
	repo := NewRepository(db)
	at := time.Date(2026, 10, 15, 8, 0, 0, 0, time.UTC)
	newAt := at.Add(24 * time.Hour)
	content := "Updated text"
	delivery := &domainscheduledmessage.Delivery{
		ID: "00000000-0000-4000-8000-000000000402", ScheduledAt: newAt,
		Status: domainscheduledmessage.DeliveryStatusPending,
	}

	mock.ExpectBegin()
	mock.ExpectQuery(`SELECT .*FROM "scheduled_messages" JOIN spaces .*FOR UPDATE OF "scheduled_messages"`).
		WillReturnRows(sqlmock.NewRows([]string{"id", "space_id", "status"}).AddRow(
			"00000000-0000-4000-8000-000000000101", "00000000-0000-4000-8000-000000000201", domainscheduledmessage.ScheduleStatusActive))
	mock.ExpectQuery(`SELECT \* FROM "scheduled_message_deliveries" WHERE scheduled_message_id = .*status.*ORDER BY scheduled_at ASC, id ASC FOR UPDATE`).
		WillReturnRows(sqlmock.NewRows([]string{"id", "scheduled_message_id", "status"}).AddRow(
			"00000000-0000-4000-8000-000000000401", "00000000-0000-4000-8000-000000000101", domainscheduledmessage.DeliveryStatusPending))
	mock.ExpectQuery(`SELECT \* FROM "scheduled_message_deliveries" WHERE scheduled_message_id = .*scheduled_at = .*LIMIT .*FOR UPDATE`).
		WillReturnRows(sqlmock.NewRows([]string{"id"}))
	mock.ExpectExec(`UPDATE "scheduled_message_deliveries" SET .*status.* WHERE id = .*status.*`).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(`INSERT INTO "scheduled_message_deliveries"`).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(`UPDATE "scheduled_messages" SET .*content.*next_scheduled_at.*WHERE id = .*status.*`).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	cancelled, err := repo.UpdateActive(context.Background(), "00000000-0000-4000-8000-000000000201", "00000000-0000-4000-8000-000000000101",
		dto.ScheduledMessageUpdateInput{Content: &content}, delivery, at)
	if err != nil {
		t.Fatalf("update schedule and pending occurrence: %v", err)
	}
	if len(cancelled) != 1 || cancelled[0] != "00000000-0000-4000-8000-000000000401" || delivery.ID == cancelled[0] {
		t.Fatalf("cancelled delivery IDs = %v and replacement ID = %s, want old delivery UUID distinct from replacement", cancelled, delivery.ID)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("SQL expectations: %v", err)
	}
}

func TestUpdateActiveReusesPendingOccurrenceAndInvalidatesItsLease(t *testing.T) {
	db, mock := newScheduledMessageMockDB(t)
	repo := NewRepository(db)
	at := time.Date(2026, 10, 15, 8, 0, 0, 0, time.UTC)
	scheduledAt := at.Add(24 * time.Hour)
	createdAt := at.Add(-24 * time.Hour)
	const spaceID = "00000000-0000-4000-8000-000000000201"
	const scheduleID = "00000000-0000-4000-8000-000000000101"
	const existingID = "00000000-0000-4000-8000-000000000401"
	content := "updated pending occurrence"
	leaseToken := "00000000-0000-4000-8000-000000000901"
	leaseUntil := scheduledAt.Add(time.Hour)
	next := &domainscheduledmessage.Delivery{
		ID: "00000000-0000-4000-8000-000000000402", ScheduledAt: scheduledAt,
		ContentSnapshot: &content,
	}

	mock.ExpectBegin()
	mock.ExpectQuery(`SELECT .*FROM "scheduled_messages" JOIN spaces .*FOR UPDATE OF "scheduled_messages"`).
		WillReturnRows(sqlmock.NewRows([]string{"id", "space_id", "status"}).AddRow(
			scheduleID, spaceID, domainscheduledmessage.ScheduleStatusActive))
	rows := func() *sqlmock.Rows {
		return sqlmock.NewRows([]string{"id", "scheduled_message_id", "scheduled_at", "status", "generation_attempts", "delivery_attempts", "created_at", "lease_token", "lease_until"}).AddRow(
			existingID, scheduleID, scheduledAt, domainscheduledmessage.DeliveryStatusPending, 2, 3, createdAt, leaseToken, leaseUntil)
	}
	mock.ExpectQuery(`SELECT \* FROM "scheduled_message_deliveries" WHERE scheduled_message_id = .*status.*ORDER BY scheduled_at ASC, id ASC FOR UPDATE`).
		WillReturnRows(rows())
	mock.ExpectQuery(`SELECT \* FROM "scheduled_message_deliveries" WHERE scheduled_message_id = .*scheduled_at = .*LIMIT .*FOR UPDATE`).
		WillReturnRows(rows())
	mock.ExpectExec(`UPDATE "scheduled_message_deliveries" SET .* WHERE id = .*`).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(`UPDATE "scheduled_messages" SET .*content.*next_scheduled_at.*WHERE id = .*status.*`).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	invalidated, err := repo.UpdateActive(context.Background(), spaceID, scheduleID,
		dto.ScheduledMessageUpdateInput{Content: &content}, next, at)
	if err != nil {
		t.Fatalf("update active schedule and reuse pending occurrence: %v", err)
	}
	if len(invalidated) != 1 || invalidated[0] != existingID {
		t.Fatalf("invalidated delivery IDs = %v, want reused pending UUID %s", invalidated, existingID)
	}
	if next.ID != existingID || next.Status != domainscheduledmessage.DeliveryStatusPending || next.GenerationAttempts != 2 || next.DeliveryAttempts != 3 || !next.CreatedAt.Equal(createdAt) {
		t.Fatalf("next occurrence after reuse = %+v, want existing UUID, attempts, and creation time", next)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("SQL expectations: %v", err)
	}
}

func TestUpdateActiveReusesSupersededCancelledOccurrence(t *testing.T) {
	db, mock := newScheduledMessageMockDB(t)
	repo := NewRepository(db)
	at := time.Date(2026, 10, 15, 8, 0, 0, 0, time.UTC)
	scheduledAt := at.Add(24 * time.Hour)
	createdAt := at.Add(-24 * time.Hour)
	const spaceID = "00000000-0000-4000-8000-000000000201"
	const scheduleID = "00000000-0000-4000-8000-000000000101"
	const cancelledID = "00000000-0000-4000-8000-000000000401"
	const pendingID = "00000000-0000-4000-8000-000000000402"
	content := "restored cancelled occurrence"
	next := &domainscheduledmessage.Delivery{ScheduledAt: scheduledAt, ContentSnapshot: &content}

	mock.ExpectBegin()
	mock.ExpectQuery(`SELECT .*FROM "scheduled_messages" JOIN spaces .*FOR UPDATE OF "scheduled_messages"`).
		WillReturnRows(sqlmock.NewRows([]string{"id", "space_id", "status"}).AddRow(
			scheduleID, spaceID, domainscheduledmessage.ScheduleStatusActive))
	mock.ExpectQuery(`SELECT \* FROM "scheduled_message_deliveries" WHERE scheduled_message_id = .*status.*ORDER BY scheduled_at ASC, id ASC FOR UPDATE`).
		WillReturnRows(sqlmock.NewRows([]string{"id", "scheduled_message_id", "scheduled_at", "status"}).AddRow(
			pendingID, scheduleID, scheduledAt.Add(time.Hour), domainscheduledmessage.DeliveryStatusPending))
	mock.ExpectQuery(`SELECT \* FROM "scheduled_message_deliveries" WHERE scheduled_message_id = .*scheduled_at = .*LIMIT .*FOR UPDATE`).
		WillReturnRows(sqlmock.NewRows([]string{"id", "scheduled_message_id", "scheduled_at", "status", "generation_attempts", "delivery_attempts", "created_at"}).AddRow(
			cancelledID, scheduleID, scheduledAt, domainscheduledmessage.DeliveryStatusCancelled, 1, 2, createdAt))
	mock.ExpectExec(`UPDATE "scheduled_message_deliveries" SET .*status.* WHERE id = .*status.*`).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(`UPDATE "scheduled_message_deliveries" SET .* WHERE id = .*`).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(`UPDATE "scheduled_messages" SET .*content.*next_scheduled_at.*WHERE id = .*status.*`).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	invalidated, err := repo.UpdateActive(context.Background(), spaceID, scheduleID,
		dto.ScheduledMessageUpdateInput{Content: &content}, next, at)
	if err != nil {
		t.Fatalf("update active schedule and reuse superseded occurrence: %v", err)
	}
	if len(invalidated) != 1 || invalidated[0] != pendingID {
		t.Fatalf("invalidated delivery IDs = %v, want other pending UUID %s only", invalidated, pendingID)
	}
	if next.ID != cancelledID || next.Status != domainscheduledmessage.DeliveryStatusPending || next.GenerationAttempts != 1 || next.DeliveryAttempts != 2 || !next.CreatedAt.Equal(createdAt) {
		t.Fatalf("next occurrence after restoring superseded row = %+v, want retained UUID, attempts, and creation time", next)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("SQL expectations: %v", err)
	}
}

func TestUnsubscribePersistsAttributionCancelsPendingAndPreservesSentHistory(t *testing.T) {
	db, mock := newScheduledMessageMockDB(t)
	repo := NewRepository(db)
	at := time.Date(2026, 10, 15, 8, 0, 0, 0, time.UTC)
	memberID := "00000000-0000-4000-8000-000000000501"

	mock.ExpectBegin()
	mock.ExpectQuery(`SELECT .*FROM "scheduled_messages" JOIN spaces .*FOR UPDATE OF "scheduled_messages"`).
		WillReturnRows(sqlmock.NewRows([]string{"id", "space_id", "status"}).AddRow(
			"00000000-0000-4000-8000-000000000101", "00000000-0000-4000-8000-000000000201", domainscheduledmessage.ScheduleStatusActive))
	mock.ExpectQuery(`SELECT \* FROM "scheduled_message_deliveries" WHERE scheduled_message_id = .*status.*ORDER BY scheduled_at ASC, id ASC FOR UPDATE`).
		WithArgs("00000000-0000-4000-8000-000000000101", domainscheduledmessage.DeliveryStatusPending).
		WillReturnRows(sqlmock.NewRows([]string{"id", "scheduled_message_id", "status"}).AddRow(
			"00000000-0000-4000-8000-000000000401", "00000000-0000-4000-8000-000000000101", domainscheduledmessage.DeliveryStatusPending))
	mock.ExpectExec(`UPDATE "scheduled_message_deliveries" SET .*lease_token.*lease_until.*status.*WHERE .*`).
		WithArgs(sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), "00000000-0000-4000-8000-000000000101", domainscheduledmessage.DeliveryStatusPending).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(`UPDATE "scheduled_messages" SET .*unsubscribed_at.*unsubscribed_by_member_id.*WHERE .*`).
		WithArgs(nil, domainscheduledmessage.ScheduleStatusUnsubscribed, at, memberID, at, "00000000-0000-4000-8000-000000000101", domainscheduledmessage.ScheduleStatusActive).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	cancelled, err := repo.Unsubscribe(context.Background(), "00000000-0000-4000-8000-000000000201", "00000000-0000-4000-8000-000000000101", memberID, at)
	if err != nil {
		t.Fatalf("unsubscribe schedule: %v", err)
	}
	if len(cancelled) != 1 || cancelled[0] != "00000000-0000-4000-8000-000000000401" {
		t.Fatalf("unsubscribed delivery IDs = %v, want cancelled pending delivery UUID", cancelled)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("SQL expectations: %v", err)
	}
}

func TestTerminalUnsubscribeCannotBeRepeatedUpdatedOrCancelled(t *testing.T) {
	for _, operation := range []struct {
		name string
		run  func(*Repository, time.Time) error
	}{
		{name: "repeat unsubscribe", run: func(repo *Repository, at time.Time) error {
			_, err := repo.Unsubscribe(context.Background(), "space", "schedule", "member", at)
			return err
		}},
		{name: "update", run: func(repo *Repository, at time.Time) error {
			start := at.Add(time.Hour)
			next := &domainscheduledmessage.Delivery{ScheduledAt: start}
			_, err := repo.UpdateActive(context.Background(), "space", "schedule", dto.ScheduledMessageUpdateInput{StartAt: &start}, next, at)
			return err
		}},
		{name: "cancel", run: func(repo *Repository, at time.Time) error {
			_, err := repo.Cancel(context.Background(), "space", "schedule", at)
			return err
		}},
	} {
		t.Run(operation.name, func(t *testing.T) {
			db, mock := newScheduledMessageMockDB(t)
			repo := NewRepository(db)
			at := time.Date(2026, 10, 15, 8, 0, 0, 0, time.UTC)

			mock.ExpectBegin()
			mock.ExpectQuery(`SELECT .*FROM "scheduled_messages" JOIN spaces .*status.*FOR UPDATE OF "scheduled_messages"`).
				WillReturnRows(sqlmock.NewRows([]string{"id"}))
			mock.ExpectRollback()

			if err := operation.run(repo, at); !errors.Is(err, interfacescheduledmessage.ErrConflict) {
				t.Fatalf("terminal operation error = %v, want conflict", err)
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatalf("SQL expectations: %v", err)
			}
		})
	}
}

func TestUpdateRequiresReplacementForOccurrenceAffectingFields(t *testing.T) {
	contentMode := domainscheduledmessage.ContentModeAI
	content := "Changed content"
	recurrence := domainscheduledmessage.RecurrenceDaily
	timezone := "Asia/Jakarta"
	startAt := time.Date(2026, 10, 16, 8, 0, 0, 0, time.UTC)
	for _, test := range []struct {
		name   string
		fields dto.ScheduledMessageUpdateInput
	}{
		{name: "content mode", fields: dto.ScheduledMessageUpdateInput{ContentMode: &contentMode}},
		{name: "content", fields: dto.ScheduledMessageUpdateInput{Content: &content}},
		{name: "recurrence", fields: dto.ScheduledMessageUpdateInput{Recurrence: &recurrence}},
		{name: "timezone", fields: dto.ScheduledMessageUpdateInput{Timezone: &timezone}},
		{name: "start time", fields: dto.ScheduledMessageUpdateInput{StartAt: &startAt}},
	} {
		t.Run(test.name, func(t *testing.T) {
			db, mock := newScheduledMessageMockDB(t)
			repo := NewRepository(db)
			at := time.Date(2026, 10, 15, 8, 0, 0, 0, time.UTC)

			_, err := repo.UpdateActive(context.Background(), "space", "schedule", test.fields, nil, at)
			if !errors.Is(err, interfacescheduledmessage.ErrConflict) {
				t.Fatalf("update without replacement error = %v, want conflict", err)
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatalf("unexpected database access without replacement: %v", err)
			}
		})
	}
}

func TestHistoryIsSpaceScopedAndOccurrenceOrdered(t *testing.T) {
	db, mock := newScheduledMessageMockDB(t)
	repo := NewRepository(db)
	secondAt := time.Date(2026, 10, 16, 8, 0, 0, 0, time.UTC)
	firstAt := secondAt.Add(-24 * time.Hour)

	mock.ExpectQuery(`(?s)SELECT .*scheduled_message_deliveries.*JOIN scheduled_messages.*JOIN spaces.*scheduled_messages.space_id = .*scheduled_messages.id = .*spaces.status.*LIMIT \$`).
		WithArgs("00000000-0000-4000-8000-000000000201", "00000000-0000-4000-8000-000000000101", "ACTIVE", 5).
		WillReturnRows(sqlmock.NewRows([]string{"id", "scheduled_message_id", "scheduled_at", "status"}).
			AddRow("delivery-2", "00000000-0000-4000-8000-000000000101", secondAt, domainscheduledmessage.DeliveryStatusSent).
			AddRow("delivery-1", "00000000-0000-4000-8000-000000000101", firstAt, domainscheduledmessage.DeliveryStatusFailed))

	history, err := repo.ListHistory(context.Background(), "00000000-0000-4000-8000-000000000201", "00000000-0000-4000-8000-000000000101", 5)
	if err != nil {
		t.Fatalf("list occurrence history: %v", err)
	}
	if len(history) != 2 || !history[0].ScheduledAt.Equal(secondAt) || history[1].Status != domainscheduledmessage.DeliveryStatusFailed {
		t.Fatalf("history = %#v, want descending occurrence order and retained failure", history)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("SQL expectations: %v", err)
	}
}

func TestListHistoryCapsRequestedRowsAtOneHundred(t *testing.T) {
	db, mock := newScheduledMessageMockDB(t)
	repo := NewRepository(db)
	spaceID := "00000000-0000-4000-8000-000000000201"
	scheduleID := "00000000-0000-4000-8000-000000000101"

	mock.ExpectQuery(`(?s)SELECT .*scheduled_messages.space_id = .*scheduled_messages.id = .*spaces.status.*LIMIT \$`).
		WithArgs(spaceID, scheduleID, "ACTIVE", 100).
		WillReturnRows(sqlmock.NewRows([]string{"id", "scheduled_message_id", "scheduled_at", "status"}))

	if _, err := repo.ListHistory(context.Background(), spaceID, scheduleID, 500); err != nil {
		t.Fatalf("list bounded history: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("SQL expectations: %v", err)
	}
}

func TestBindCurrentGroupRequiresOwnerOrAdminAndStoresOnlyTrustedChat(t *testing.T) {
	db, mock := newScheduledMessageMockDB(t)
	repo := NewRepository(db)
	at := time.Date(2026, 10, 15, 8, 0, 0, 0, time.UTC)
	chatID := "120363123456@g.us"

	mock.ExpectBegin()
	mock.ExpectQuery(`(?s)SELECT count\(\*\).*space_members.*roles.*spaces.*roles.deleted_at IS NULL.*roles.name IN`).
		WithArgs("00000000-0000-4000-8000-000000000301", "00000000-0000-4000-8000-000000000201", "ACTIVE", "ACTIVE", "space_owner", "space_admin").
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))
	mock.ExpectQuery(`SELECT \* FROM "space_channels" WHERE external_chat_id = .*status.*FOR UPDATE`).
		WillReturnRows(sqlmock.NewRows([]string{"id"}))
	mock.ExpectExec(`INSERT INTO "space_channels"`).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	channel, err := repo.BindCurrentGroup(context.Background(), "00000000-0000-4000-8000-000000000201", "00000000-0000-4000-8000-000000000301", "whatsapp", chatID, "group", at)
	if err != nil {
		t.Fatalf("bind verified current group: %v", err)
	}
	if channel.ExternalChatID != chatID || channel.BoundByMemberID != "00000000-0000-4000-8000-000000000301" {
		t.Fatalf("bound channel = %#v, want trusted chat and actor attribution", channel)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("SQL expectations: %v", err)
	}
}

func TestBindCurrentGroupRejectsSoftDeletedAdminRole(t *testing.T) {
	db, mock := newScheduledMessageMockDB(t)
	repo := NewRepository(db)
	at := time.Date(2026, 10, 15, 8, 0, 0, 0, time.UTC)

	mock.ExpectBegin()
	mock.ExpectQuery(`(?s)SELECT count\(\*\).*space_members.*roles.*spaces.*roles.deleted_at IS NULL.*roles.name IN`).
		WithArgs("member", "space", "ACTIVE", "ACTIVE", "space_owner", "space_admin").
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))
	mock.ExpectRollback()

	_, err := repo.BindCurrentGroup(context.Background(), "space", "member", "whatsapp", "120363123456@g.us", "group", at)
	if !errors.Is(err, interfacescheduledmessage.ErrConflict) {
		t.Fatalf("bind with deleted role error = %v, want authorization conflict", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("SQL expectations: %v", err)
	}
}

func TestPersistGeneratedContentUsesLiveLeaseAndActiveSchedule(t *testing.T) {
	db, mock := newScheduledMessageMockDB(t)
	repo := NewSchedulerRepository(db)
	now := time.Date(2026, 10, 15, 8, 1, 0, 0, time.UTC)
	leaseUntil := now.Add(time.Minute)
	token := "00000000-0000-4000-8000-000000000601"
	delivery := &domainscheduledmessage.Delivery{
		ID: "00000000-0000-4000-8000-000000000401", ScheduledMessageID: "00000000-0000-4000-8000-000000000101",
		Status: domainscheduledmessage.DeliveryStatusPending, LeaseToken: &token, LeaseExpiresAt: &leaseUntil, UpdatedAt: now,
	}

	mock.ExpectBegin()
	mock.ExpectQuery(`SELECT .*FROM "scheduled_messages" JOIN spaces .*FOR UPDATE OF "scheduled_messages"`).
		WillReturnRows(sqlmock.NewRows([]string{"id", "status"}).AddRow(delivery.ScheduledMessageID, domainscheduledmessage.ScheduleStatusActive))
	mock.ExpectQuery(`SELECT \* FROM "scheduled_message_deliveries" WHERE id = .*lease_token.*lease_until.*FOR UPDATE`).
		WillReturnRows(sqlmock.NewRows([]string{"id", "scheduled_message_id", "status", "lease_token", "lease_until", "updated_at"}).
			AddRow(delivery.ID, delivery.ScheduledMessageID, domainscheduledmessage.DeliveryStatusPending, token, leaseUntil, now))
	mock.ExpectExec(`UPDATE "scheduled_message_deliveries" SET .*content_snapshot.*updated_at.*WHERE .*lease_token.*lease_until.*`).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	if err := repo.PersistGeneratedContent(context.Background(), delivery, "Generated text", now); err != nil {
		t.Fatalf("persist generated content: %v", err)
	}
	if delivery.ContentSnapshot == nil || *delivery.ContentSnapshot != "Generated text" {
		t.Fatalf("content snapshot = %v, want persisted generated text", delivery.ContentSnapshot)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("SQL expectations: %v", err)
	}
}

func TestRecordRetryUpdatesAttemptsAndBackoffUnderClaimToken(t *testing.T) {
	db, mock := newScheduledMessageMockDB(t)
	repo := NewSchedulerRepository(db)
	now := time.Date(2026, 10, 15, 8, 1, 0, 0, time.UTC)
	leaseUntil := now.Add(time.Minute)
	token := "00000000-0000-4000-8000-000000000601"
	nextAttempt := now.Add(5 * time.Minute)
	nextOccurrence := now.Add(24 * time.Hour)
	delivery := &domainscheduledmessage.Delivery{
		ID: "00000000-0000-4000-8000-000000000401", ScheduledMessageID: "00000000-0000-4000-8000-000000000101",
		Status: domainscheduledmessage.DeliveryStatusPending, DeliveryAttempts: 1, NextAttemptAt: &nextAttempt,
		LeaseToken: &token, LeaseExpiresAt: &leaseUntil, UpdatedAt: now,
	}
	schedule := &domainscheduledmessage.Schedule{
		ID: delivery.ScheduledMessageID, Status: domainscheduledmessage.ScheduleStatusActive, NextScheduledAt: &nextOccurrence, UpdatedAt: now,
	}

	mock.ExpectBegin()
	mock.ExpectQuery(`SELECT .*FROM "scheduled_messages" JOIN spaces .*FOR UPDATE OF "scheduled_messages"`).
		WillReturnRows(sqlmock.NewRows([]string{"id", "status"}).AddRow(schedule.ID, domainscheduledmessage.ScheduleStatusActive))
	mock.ExpectQuery(`SELECT \* FROM "scheduled_message_deliveries" WHERE id = .*lease_token.*lease_until.*FOR UPDATE`).
		WillReturnRows(sqlmock.NewRows([]string{"id", "scheduled_message_id", "status", "lease_token", "lease_until", "updated_at"}).
			AddRow(delivery.ID, schedule.ID, domainscheduledmessage.DeliveryStatusPending, token, leaseUntil, now))
	mock.ExpectExec(`UPDATE "scheduled_message_deliveries" SET .* WHERE .*lease_token.*lease_until.*`).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(`UPDATE "scheduled_messages" SET .*WHERE id = .*status.*`).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	if err := repo.RecordRetry(context.Background(), delivery, schedule); err != nil {
		t.Fatalf("record retry: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("SQL expectations: %v", err)
	}
}

func TestReserveExternalAttemptPersistsCounterAndKeepsLiveLease(t *testing.T) {
	for _, test := range []struct {
		name               string
		call               func(*Repository, *domainscheduledmessage.Delivery, time.Time) error
		col                string
		generationAttempts int
		deliveryAttempts   int
	}{
		{name: "generation", col: "generation_attempts", generationAttempts: 1, call: func(repo *Repository, delivery *domainscheduledmessage.Delivery, now time.Time) error {
			return repo.ReserveGenerationAttempt(context.Background(), delivery, now, 4)
		}},
		{name: "delivery", col: "delivery_attempts", deliveryAttempts: 1, call: func(repo *Repository, delivery *domainscheduledmessage.Delivery, now time.Time) error {
			return repo.ReserveDeliveryAttempt(context.Background(), delivery, now, 4)
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			db, mock := newScheduledMessageMockDB(t)
			repo := NewSchedulerRepository(db)
			now := time.Date(2026, 10, 15, 8, 1, 0, 0, time.UTC)
			leaseUntil := now.Add(time.Minute)
			token := "00000000-0000-4000-8000-000000000601"
			delivery := &domainscheduledmessage.Delivery{
				ID: "00000000-0000-4000-8000-000000000401", ScheduledMessageID: "00000000-0000-4000-8000-000000000101",
				Status: domainscheduledmessage.DeliveryStatusPending, LeaseToken: &token, LeaseExpiresAt: &leaseUntil, UpdatedAt: now,
			}

			mock.ExpectBegin()
			mock.ExpectQuery(`SELECT .*FROM "scheduled_messages" JOIN spaces .*FOR UPDATE OF "scheduled_messages"`).
				WillReturnRows(sqlmock.NewRows([]string{"id", "status"}).AddRow(delivery.ScheduledMessageID, domainscheduledmessage.ScheduleStatusActive))
			mock.ExpectQuery(`SELECT \* FROM "scheduled_message_deliveries" WHERE id = .*lease_token.*lease_until.*FOR UPDATE`).
				WillReturnRows(sqlmock.NewRows([]string{"id", "scheduled_message_id", "status", "lease_token", "lease_until", "generation_attempts", "delivery_attempts"}).
					AddRow(delivery.ID, delivery.ScheduledMessageID, domainscheduledmessage.DeliveryStatusPending, token, leaseUntil, 0, 0))
			mock.ExpectExec(`UPDATE "scheduled_message_deliveries" SET .*` + test.col + `.*updated_at.*WHERE .*lease_token.*lease_until.*` + test.col + `.*`).
				WillReturnResult(sqlmock.NewResult(0, 1))
			mock.ExpectCommit()

			if err := test.call(repo, delivery, now); err != nil {
				t.Fatalf("reserve external attempt: %v", err)
			}
			assertReservedExternalAttempt(t, delivery, test.generationAttempts, test.deliveryAttempts, token, leaseUntil)
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatalf("SQL expectations: %v", err)
			}
		})
	}
}

func assertReservedExternalAttempt(t *testing.T, delivery *domainscheduledmessage.Delivery, generationAttempts, deliveryAttempts int, token string, leaseUntil time.Time) {
	t.Helper()
	if delivery.GenerationAttempts != generationAttempts || delivery.DeliveryAttempts != deliveryAttempts {
		t.Fatalf("reserved counters = generation %d, delivery %d", delivery.GenerationAttempts, delivery.DeliveryAttempts)
	}
	if delivery.Status != domainscheduledmessage.DeliveryStatusPending || delivery.LeaseToken == nil || *delivery.LeaseToken != token || delivery.LeaseExpiresAt == nil || !delivery.LeaseExpiresAt.Equal(leaseUntil) {
		t.Fatalf("reservation changed live claim: %#v", delivery)
	}
}

func TestReserveAttemptRejectsExhaustedBudgetWithoutAdvancingCounter(t *testing.T) {
	db, mock := newScheduledMessageMockDB(t)
	repo := NewSchedulerRepository(db)
	now := time.Date(2026, 10, 15, 8, 1, 0, 0, time.UTC)
	leaseUntil := now.Add(time.Minute)
	token := "00000000-0000-4000-8000-000000000601"
	delivery := &domainscheduledmessage.Delivery{
		ID: "00000000-0000-4000-8000-000000000401", ScheduledMessageID: "00000000-0000-4000-8000-000000000101",
		GenerationAttempts: 1, Status: domainscheduledmessage.DeliveryStatusPending, LeaseToken: &token, LeaseExpiresAt: &leaseUntil,
	}

	mock.ExpectBegin()
	mock.ExpectQuery(`SELECT .*FROM "scheduled_messages" JOIN spaces .*FOR UPDATE OF "scheduled_messages"`).
		WillReturnRows(sqlmock.NewRows([]string{"id", "status"}).AddRow(delivery.ScheduledMessageID, domainscheduledmessage.ScheduleStatusActive))
	mock.ExpectQuery(`SELECT \* FROM "scheduled_message_deliveries" WHERE id = .*lease_token.*lease_until.*FOR UPDATE`).
		WillReturnRows(sqlmock.NewRows([]string{"id", "scheduled_message_id", "status", "lease_token", "lease_until"}).
			AddRow(delivery.ID, delivery.ScheduledMessageID, domainscheduledmessage.DeliveryStatusPending, token, leaseUntil))
	mock.ExpectExec(`UPDATE "scheduled_message_deliveries" SET .*generation_attempts.*WHERE .*generation_attempts.*`).
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectRollback()

	err := repo.ReserveGenerationAttempt(context.Background(), delivery, now, 1)
	if !errors.Is(err, interfacescheduledmessage.ErrAttemptLimitReached) || delivery.GenerationAttempts != 1 {
		t.Fatalf("reservation error/count = %v/%d, want exhausted budget at count 1", err, delivery.GenerationAttempts)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("SQL expectations: %v", err)
	}
}

func TestReserveAttemptRejectsStaleLeaseBeforeIncrement(t *testing.T) {
	db, mock := newScheduledMessageMockDB(t)
	repo := NewSchedulerRepository(db)
	now := time.Date(2026, 10, 15, 8, 1, 0, 0, time.UTC)
	leaseUntil := now.Add(-time.Second)
	token := "stale-token"
	delivery := &domainscheduledmessage.Delivery{
		ID: "00000000-0000-4000-8000-000000000401", ScheduledMessageID: "00000000-0000-4000-8000-000000000101",
		Status: domainscheduledmessage.DeliveryStatusPending, LeaseToken: &token, LeaseExpiresAt: &leaseUntil,
	}

	mock.ExpectBegin()
	mock.ExpectQuery(`SELECT .*FROM "scheduled_messages" JOIN spaces .*FOR UPDATE OF "scheduled_messages"`).
		WillReturnRows(sqlmock.NewRows([]string{"id", "status"}).AddRow(delivery.ScheduledMessageID, domainscheduledmessage.ScheduleStatusActive))
	mock.ExpectQuery(`SELECT \* FROM "scheduled_message_deliveries" WHERE id = .*lease_token.*lease_until.*FOR UPDATE`).
		WillReturnRows(sqlmock.NewRows([]string{"id", "scheduled_message_id", "status", "lease_token", "lease_until"}))
	mock.ExpectRollback()

	err := repo.ReserveDeliveryAttempt(context.Background(), delivery, now, 4)
	if !errors.Is(err, interfacescheduledmessage.ErrClaimConflict) {
		t.Fatalf("stale reservation error = %v, want claim conflict", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("SQL expectations: %v", err)
	}
}

func TestListGeneratedHistoryFiltersSentAIContentBeforeApplyingLimit(t *testing.T) {
	db, mock := newScheduledMessageMockDB(t)
	repo := NewSchedulerRepository(db)
	spaceID := "00000000-0000-4000-8000-000000000201"
	scheduleID := "00000000-0000-4000-8000-000000000101"

	mock.ExpectQuery(`(?s)SELECT .*content_snapshot.*FROM scheduled_message_deliveries.*JOIN scheduled_messages.*content_mode = .*status = .*content_snapshot IS NOT NULL.*s.space_id = .*s.id = .*ORDER BY .*scheduled_at DESC.*LIMIT \$`).
		WithArgs(domainscheduledmessage.ContentModeAI, domainscheduledmessage.DeliveryStatusSent, spaceActiveStatus, spaceID, scheduleID, 5).
		WillReturnRows(sqlmock.NewRows([]string{"content_snapshot"}).AddRow("sent generated text").AddRow("older sent text"))

	got, err := repo.ListGeneratedHistory(context.Background(), spaceID, scheduleID, 50)
	if err != nil {
		t.Fatalf("list generated history: %v", err)
	}
	if len(got) != 2 || got[0] != "sent generated text" || got[1] != "older sent text" {
		t.Fatalf("generated history = %#v", got)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("SQL expectations: %v", err)
	}
}
