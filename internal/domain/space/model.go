package domainspace

import (
	"time"

	"gorm.io/gorm"
)

const (
	TypePersonal = "PERSONAL"
	TypeShared   = "SHARED"

	CategoryPersonal  = "personal"
	CategoryFamily    = "family"
	CategoryFriends   = "friends"
	CategoryCommunity = "community"
	CategoryWork      = "work"
	CategoryFinance   = "finance"
	CategoryCustom    = "custom"

	StatusActive   = "ACTIVE"
	StatusInactive = "INACTIVE"
	StatusArchived = "ARCHIVED"
)

func (Space) TableName() string {
	return "spaces"
}

type Space struct {
	ID              string         `json:"id" gorm:"column:id;primaryKey"`
	Name            string         `json:"name" gorm:"column:name"`
	Type            string         `json:"type" gorm:"column:type"`
	Category        string         `json:"category" gorm:"column:category"`
	Status          string         `json:"status" gorm:"column:status"`
	CreatedByUserID string         `json:"created_by_user_id" gorm:"column:created_by_user_id"`
	CreatedAt       time.Time      `json:"created_at" gorm:"column:created_at"`
	UpdatedAt       *time.Time     `json:"updated_at,omitempty" gorm:"column:updated_at"`
	DeletedAt       gorm.DeletedAt `json:"-" gorm:"column:deleted_at"`
}

type SpaceUpdateFields struct {
	Name     *string
	Category *string
}

func (Member) TableName() string {
	return "space_members"
}

type Member struct {
	ID        string         `json:"id" gorm:"column:id;primaryKey"`
	SpaceID   string         `json:"space_id" gorm:"column:space_id"`
	UserID    string         `json:"user_id" gorm:"column:user_id"`
	RoleID    string         `json:"role_id" gorm:"column:role_id"`
	Status    string         `json:"status" gorm:"column:status"`
	CreatedAt time.Time      `json:"created_at" gorm:"column:created_at"`
	UpdatedAt *time.Time     `json:"updated_at,omitempty" gorm:"column:updated_at"`
	DeletedAt gorm.DeletedAt `json:"-" gorm:"column:deleted_at"`
}

type ResolvedMembership struct {
	ID            string `json:"id" gorm:"column:id"`
	SpaceID       string `json:"space_id" gorm:"column:space_id"`
	SpaceName     string `json:"space_name" gorm:"column:space_name"`
	SpaceType     string `json:"space_type" gorm:"column:space_type"`
	SpaceCategory string `json:"space_category" gorm:"column:space_category"`
	UserID        string `json:"user_id" gorm:"column:user_id"`
	UserName      string `json:"user_name" gorm:"column:user_name"`
	UserEmail     string `json:"user_email" gorm:"column:user_email"`
	RoleID        string `json:"role_id" gorm:"column:role_id"`
	RoleName      string `json:"role_name" gorm:"column:role_name"`
	Status        string `json:"status" gorm:"column:status"`
}
