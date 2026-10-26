package domainreminder

import (
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"gorm.io/gorm"
)

var (
	ErrSpaceIDRequired        = errors.New("space_id is required")
	ErrReminderRequired       = errors.New("reminder is required")
	ErrReminderIDRequired     = errors.New("reminder_id is required")
	ErrReminderUpdateRequired = errors.New("reminder update is required")
	ErrStatusConflict         = errors.New("reminder status conflict")
	ErrInvalidStatus          = errors.New("status is invalid")
	ErrInvalidReplaceKey      = errors.New("replace_key is invalid")
	ErrReplaceKeyConflict     = errors.New("replace_key conflict")
)

type Status string

type DeliveryTarget struct {
	Provider string
	Target   string
}

const (
	StatusPending   Status = "PENDING"
	StatusSent      Status = "SENT"
	StatusCompleted Status = "COMPLETED"
	StatusCancelled Status = "CANCELLED" //nolint:misspell // persisted API enum; preserve spelling.
)

func ParseStatus(value string) (*Status, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil, nil
	}
	status := Status(value)
	switch status {
	case StatusPending, StatusSent, StatusCompleted, StatusCancelled:
		return &status, nil
	default:
		return nil, ErrInvalidStatus
	}
}

// MaxReplaceKeyLength is the maximum rune length of a normalized replace key.
const MaxReplaceKeyLength = 128

type ReplaceOutcome string

const (
	ReplaceOutcomeNone         ReplaceOutcome = "none"
	ReplaceOutcomeCreated      ReplaceOutcome = "created"
	ReplaceOutcomeReplaced     ReplaceOutcome = "replaced"
	ReplaceOutcomeKeptExisting ReplaceOutcome = "kept_existing"
)

// ReplaceResult describes the outcome of creating a reminder that may replace
// older pending reminders sharing its replace key.
type ReplaceResult struct {
	Outcome ReplaceOutcome
	// Reminder is the newly created reminder, or the kept later one.
	Reminder *Reminder
	Replaced []Reminder
}

// NormalizeReplaceKey trims and lower-cases a replace key. Blank input yields
// nil; keys longer than MaxReplaceKeyLength runes are rejected, never truncated.
func NormalizeReplaceKey(raw string) (*string, error) {
	key := strings.ToLower(strings.TrimSpace(raw))
	if key == "" {
		return nil, nil
	}
	if utf8.RuneCountInString(key) > MaxReplaceKeyLength {
		return nil, ErrInvalidReplaceKey
	}
	return &key, nil
}

type ListFilter struct {
	SpaceID string
	Status  *Status
	From    *time.Time
	To      *time.Time
}

type UpdateFields struct {
	Title            *string
	Description      *string
	ScheduledAt      *time.Time
	AssigneeMemberID *string
	ClearAssignee    bool
}

func (Reminder) TableName() string {
	return "reminders"
}

type Reminder struct {
	ID                    string         `json:"id" gorm:"column:id;primaryKey"`
	SpaceID               string         `json:"space_id" gorm:"column:space_id"`
	CreatedByMemberID     string         `json:"created_by_member_id" gorm:"column:created_by_member_id"`
	AssigneeMemberID      *string        `json:"assignee_member_id,omitempty" gorm:"column:assignee_member_id"`
	Title                 string         `json:"title" gorm:"column:title"`
	Description           string         `json:"description" gorm:"column:description"`
	ScheduledAt           time.Time      `json:"scheduled_at" gorm:"column:scheduled_at"`
	Status                Status         `json:"status" gorm:"column:status"`
	DeliveryProvider      string         `json:"delivery_provider,omitempty" gorm:"column:delivery_provider"`
	DeliveryTarget        string         `json:"delivery_target,omitempty" gorm:"column:delivery_target"`
	NotificationClaimedAt *time.Time     `json:"-" gorm:"column:notification_claimed_at"`
	NotifiedAt            *time.Time     `json:"notified_at,omitempty" gorm:"column:notified_at"`
	CompletedAt           *time.Time     `json:"completed_at,omitempty" gorm:"column:completed_at"`
	CreatedAt             time.Time      `json:"created_at" gorm:"column:created_at"`
	UpdatedAt             time.Time      `json:"updated_at" gorm:"column:updated_at"`
	DeletedAt             gorm.DeletedAt `json:"deleted_at,omitempty" gorm:"column:deleted_at"`
	ReplaceKey            *string        `json:"replace_key,omitempty" gorm:"column:replace_key"`
}
