package domainreminder

import "time"

type Scope string

const (
	ScopePersonal Scope = "PERSONAL"
	ScopeFamily   Scope = "FAMILY"
)

type Status string

const (
	StatusPending   Status = "PENDING"
	StatusCompleted Status = "COMPLETED"
	StatusCancelled Status = "CANCELLED" //nolint:misspell // persisted API enum; preserve spelling.
)

func (Reminder) TableName() string {
	return "reminders"
}

type Reminder struct {
	ID                string     `json:"id" gorm:"column:id;primaryKey"`
	FamilyID          string     `json:"family_id" gorm:"column:family_id"`
	OwnerMemberID     string     `json:"owner_member_id" gorm:"column:owner_member_id"`
	CreatedByMemberID string     `json:"created_by_member_id" gorm:"column:created_by_member_id"`
	Scope             Scope      `json:"scope" gorm:"column:scope"`
	Title             string     `json:"title" gorm:"column:title"`
	Description       string     `json:"description" gorm:"column:description"`
	ScheduledAt       time.Time  `json:"scheduled_at" gorm:"column:scheduled_at"`
	Status            Status     `json:"status" gorm:"column:status"`
	CompletedAt       *time.Time `json:"completed_at,omitempty" gorm:"column:completed_at"`
	CreatedAt         time.Time  `json:"created_at" gorm:"column:created_at"`
	UpdatedAt         time.Time  `json:"updated_at" gorm:"column:updated_at"`
}
