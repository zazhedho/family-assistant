package domainactivity

import (
	"errors"
	"time"

	"gorm.io/gorm"
)

var (
	ErrActivityRequired       = errors.New("activity is required")
	ErrActivitySpaceID        = errors.New("activity space_id is required")
	ErrActivityIDRequired     = errors.New("activity_id is required")
	ErrActivityUpdateRequired = errors.New("activity update is required")
)

func (Activity) TableName() string { return "space_activities" }

type Activity struct {
	ID                string         `json:"id" gorm:"column:id;primaryKey"`
	SpaceID           string         `json:"space_id" gorm:"column:space_id"`
	CreatedByMemberID string         `json:"created_by_member_id" gorm:"column:created_by_member_id"`
	Kind              string         `json:"kind" gorm:"column:kind"`
	Note              string         `json:"note" gorm:"column:note"`
	OccurredAt        time.Time      `json:"occurred_at" gorm:"column:occurred_at"`
	CreatedAt         time.Time      `json:"created_at" gorm:"column:created_at"`
	UpdatedAt         time.Time      `json:"updated_at" gorm:"column:updated_at"`
	DeletedAt         gorm.DeletedAt `json:"-" gorm:"column:deleted_at"`
}

type ListFilter struct {
	SpaceID string
	Kind    string
	From    *time.Time
	To      *time.Time
	Limit   int
}

type UpdateFields struct {
	Kind       *string
	Note       *string
	OccurredAt *time.Time
}
