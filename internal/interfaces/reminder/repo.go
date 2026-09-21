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
}
