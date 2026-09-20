package domaininvitation

import (
	"errors"
	"time"

	"gorm.io/gorm"
)

const (
	StatusPending  = "PENDING"
	StatusAccepted = "ACCEPTED"
	StatusExpired  = "EXPIRED"
	StatusRevoked  = "REVOKED"
)

var ErrInvalidInvitation = errors.New("invalid invitation")

func (Invitation) TableName() string {
	return "space_invitations"
}

type Invitation struct {
	ID                string         `json:"id" gorm:"column:id;primaryKey"`
	SpaceID           string         `json:"space_id" gorm:"column:space_id"`
	InvitedEmail      string         `json:"invited_email,omitempty" gorm:"column:invited_email"`
	RoleID            string         `json:"role_id" gorm:"column:role_id"`
	InvitedByMemberID string         `json:"invited_by_member_id" gorm:"column:invited_by_member_id"`
	TokenHash         string         `json:"-" gorm:"column:token_hash"`
	Status            string         `json:"status" gorm:"column:status"`
	ExpiresAt         time.Time      `json:"expires_at" gorm:"column:expires_at"`
	AcceptedAt        *time.Time     `json:"accepted_at,omitempty" gorm:"column:accepted_at"`
	AcceptedByUserID  string         `json:"accepted_by_user_id,omitempty" gorm:"column:accepted_by_user_id"`
	CreatedAt         time.Time      `json:"created_at" gorm:"column:created_at"`
	UpdatedAt         time.Time      `json:"updated_at" gorm:"column:updated_at"`
	DeletedAt         gorm.DeletedAt `json:"-" gorm:"column:deleted_at"`
}
