package interfacereminder

import (
	"context"
	"time"

	domainreminder "family-assistant/internal/domain/reminder"
)

type RepoReminderInterface interface {
	Create(context.Context, *domainreminder.Reminder) error
	FindByIDInSpace(context.Context, string, string) (*domainreminder.Reminder, error)
	List(context.Context, domainreminder.ListFilter) ([]domainreminder.Reminder, error)
	CompletePending(context.Context, string, string, time.Time) error
	UpdatePending(context.Context, string, string, domainreminder.UpdateFields, time.Time) error
	SoftDelete(context.Context, string, string, domainreminder.Status, time.Time) error
}
