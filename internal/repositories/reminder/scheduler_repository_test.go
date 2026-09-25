package repositoryreminder

import (
	"context"
	"testing"
	"time"

	domainreminder "family-assistant/internal/domain/reminder"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestClaimDueForNotificationClaimsPendingRows(t *testing.T) {
	db, mock := newReminderMockDB(t)
	repo := NewSchedulerRepository(db)
	now := time.Date(2026, 9, 25, 8, 0, 0, 0, time.UTC)
	staleBefore := now.Add(-2 * time.Minute)

	mock.ExpectBegin()
	mock.ExpectQuery(`SELECT .* FROM "reminders" WHERE .*status.*scheduled_at.*notified_at.*notification_claimed_at.*`).
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "space_id", "created_by_member_id", "assignee_member_id", "title", "description",
			"scheduled_at", "status", "completed_at", "created_at", "updated_at", "deleted_at",
			"delivery_provider", "delivery_target", "notification_claimed_at", "notified_at",
		}).AddRow(
			"reminder-1", "space-1", "member-1", nil, "Drink milk", "Finish the bottle",
			now.Add(-time.Minute), domainreminder.StatusPending, nil, now.Add(-time.Hour), now.Add(-time.Hour), nil,
			"whatsapp", "120363@g.us", nil, nil,
		))
	mock.ExpectExec(`UPDATE "reminders" SET .*notification_claimed_at.*`).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	got, err := repo.ClaimDueForNotification(context.Background(), now, staleBefore, 10)
	if err != nil {
		t.Fatalf("claim due reminders: %v", err)
	}
	if len(got) != 1 || got[0].ID != "reminder-1" || got[0].DeliveryTarget != "120363@g.us" {
		t.Fatalf("unexpected claimed reminders: %#v", got)
	}
	if got[0].NotificationClaimedAt == nil || !got[0].NotificationClaimedAt.Equal(now) {
		t.Fatalf("claim timestamp = %v, want %v", got[0].NotificationClaimedAt, now)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func TestMarkNotificationSentUpdatesOnlyPendingClaim(t *testing.T) {
	db, mock := newReminderMockDB(t)
	repo := NewSchedulerRepository(db)
	sentAt := time.Date(2026, 9, 25, 8, 1, 0, 0, time.UTC)
	claimedAt := time.Date(2026, 9, 25, 8, 0, 0, 0, time.UTC)

	mock.ExpectExec(`UPDATE "reminders" SET .*notification_claimed_at.*notified_at.* WHERE .*id.*status.*notification_claimed_at.*`).
		WillReturnResult(sqlmock.NewResult(0, 1))

	if err := repo.MarkNotificationSent(context.Background(), "reminder-1", claimedAt, sentAt); err != nil {
		t.Fatalf("mark notification sent: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func TestReleaseNotificationClaimClearsClaim(t *testing.T) {
	db, mock := newReminderMockDB(t)
	repo := NewSchedulerRepository(db)
	claimedAt := time.Date(2026, 9, 25, 8, 0, 0, 0, time.UTC)

	mock.ExpectExec(`UPDATE "reminders" SET .*notification_claimed_at.* WHERE .*id.*status.*notification_claimed_at.*`).
		WillReturnResult(sqlmock.NewResult(0, 1))

	if err := repo.ReleaseNotificationClaim(context.Background(), "reminder-1", claimedAt); err != nil {
		t.Fatalf("release notification claim: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}
