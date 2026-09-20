package domainreminder

import (
	"context"
	"errors"
	"time"
)

var (
	ErrSpaceIDRequired    = errors.New("space_id is required")
	ErrReminderRequired   = errors.New("reminder is required")
	ErrReminderIDRequired = errors.New("reminder_id is required")
	ErrStatusConflict     = errors.New("reminder status conflict")
)

type ListFilter struct {
	SpaceID string
	Status  *Status
	From    *time.Time
	To      *time.Time
}

type Repository interface {
	Create(ctx context.Context, reminder *Reminder) error
	FindByIDInSpace(ctx context.Context, spaceID, reminderID string) (*Reminder, error)
	List(ctx context.Context, filter ListFilter) ([]Reminder, error)
	CompletePending(ctx context.Context, spaceID, reminderID string, completedAt time.Time) error
}
