package interfacereminder

import (
	"context"
	"time"

	domainreminder "family-assistant/internal/domain/reminder"
)

type SchedulerRepository interface {
	ClaimDueForNotification(context.Context, time.Time, time.Time, int) ([]domainreminder.Reminder, error)
	ListPendingForNotification(context.Context) ([]domainreminder.Reminder, error)
	MarkNotificationSent(context.Context, string, time.Time, time.Time) error
	ReleaseNotificationClaim(context.Context, string, time.Time) error
}

type ReminderIndexWriter interface {
	Upsert(context.Context, string, time.Time) error
	Remove(context.Context, string) error
}

type DueReminderIndex interface {
	ReminderIndexWriter
	HasDue(context.Context, time.Time) (bool, error)
	AllIDs(context.Context) ([]string, error)
	UpsertMany(context.Context, []domainreminder.Reminder) error
	RemoveMany(context.Context, []string) error
}
