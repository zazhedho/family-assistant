package domainreminder

import (
	"context"
	"errors"
	"time"
)

var (
	ErrFamilyIDRequired   = errors.New("family_id is required")
	ErrReminderRequired   = errors.New("reminder is required")
	ErrReminderIDRequired = errors.New("reminder_id is required")
	ErrStatusConflict     = errors.New("reminder status conflict")
)

type ListFilter struct {
	FamilyID      string
	OwnerMemberID *string
	Scope         *Scope
	Status        *Status
	From          *time.Time
	To            *time.Time
}

type Repository interface {
	Create(ctx context.Context, reminder *Reminder) error
	FindByIDInFamily(ctx context.Context, familyID, reminderID string) (*Reminder, error)
	List(ctx context.Context, filter ListFilter) ([]Reminder, error)
	Update(ctx context.Context, reminder *Reminder) error
}
