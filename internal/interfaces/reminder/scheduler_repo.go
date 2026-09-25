package interfacereminder

import (
	"context"
	"time"

	domainreminder "family-assistant/internal/domain/reminder"
)

type SchedulerRepository interface {
	ClaimDueForNotification(context.Context, time.Time, time.Time, int) ([]domainreminder.Reminder, error)
	MarkNotificationSent(context.Context, string, time.Time, time.Time) error
	ReleaseNotificationClaim(context.Context, string, time.Time) error
}
