package domainreminder

import (
	"errors"
	"time"

	"gorm.io/gorm"
)

var (
	ErrSpaceIDRequired    = errors.New("space_id is required")
	ErrReminderRequired   = errors.New("reminder is required")
	ErrReminderIDRequired = errors.New("reminder_id is required")
	ErrStatusConflict     = errors.New("reminder status conflict")
)

type Status string

const (
	StatusPending   Status = "PENDING"
	StatusCompleted Status = "COMPLETED"
	StatusCancelled Status = "CANCELLED" //nolint:misspell // persisted API enum; preserve spelling.
)

type ListFilter struct {
	SpaceID string
	Status  *Status
	From    *time.Time
	To      *time.Time
}

func (Reminder) TableName() string {
	return "reminders"
}

type Reminder struct {
	ID                string         `json:"id" gorm:"column:id;primaryKey"`
	SpaceID           string         `json:"space_id" gorm:"column:space_id"`
	CreatedByMemberID string         `json:"created_by_member_id" gorm:"column:created_by_member_id"`
	AssigneeMemberID  *string        `json:"assignee_member_id,omitempty" gorm:"column:assignee_member_id"`
	Title             string         `json:"title" gorm:"column:title"`
	Description       string         `json:"description" gorm:"column:description"`
	ScheduledAt       time.Time      `json:"scheduled_at" gorm:"column:scheduled_at"`
	Status            Status         `json:"status" gorm:"column:status"`
	CompletedAt       *time.Time     `json:"completed_at,omitempty" gorm:"column:completed_at"`
	CreatedAt         time.Time      `json:"created_at" gorm:"column:created_at"`
	UpdatedAt         time.Time      `json:"updated_at" gorm:"column:updated_at"`
	DeletedAt         gorm.DeletedAt `json:"deleted_at,omitempty" gorm:"column:deleted_at"`
}
