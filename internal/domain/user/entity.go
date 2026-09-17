package domainuser

import (
	"time"

	"gorm.io/gorm"
)

func (Users) TableName() string {
	return "users"
}

type Users struct {
	Id                 string         `json:"id" gorm:"column:id;primaryKey"`
	Name               string         `json:"name" gorm:"column:name"`
	Email              string         `json:"email,omitempty" gorm:"column:email"`
	Phone              string         `json:"phone,omitempty" gorm:"column:phone"`
	Password           string         `json:"-" gorm:"column:password"`
	Role               string         `json:"role,omitempty" gorm:"column:role"`
	RoleId             *string        `json:"role_id,omitempty" gorm:"column:role_id"`
	EmailVerifiedAt    *time.Time     `json:"email_verified_at,omitempty" gorm:"column:email_verified_at"`
	PhoneVerifiedAt    *time.Time     `json:"phone_verified_at,omitempty" gorm:"column:phone_verified_at"`
	LastLoginAt        *time.Time     `json:"last_login_at,omitempty" gorm:"column:last_login_at"`
	LastLoginIP        string         `json:"last_login_ip,omitempty" gorm:"column:last_login_ip"`
	LastLoginUserAgent string         `json:"last_login_user_agent,omitempty" gorm:"column:last_login_user_agent"`
	LockedUntil        *time.Time     `json:"locked_until,omitempty" gorm:"column:locked_until"`
	PasswordChangedAt  *time.Time     `json:"password_changed_at,omitempty" gorm:"column:password_changed_at"`
	LoginProvider      string         `json:"login_provider,omitempty" gorm:"column:login_provider"`
	AvatarURL          string         `json:"avatar_url,omitempty" gorm:"column:avatar_url"`
	Metadata           map[string]any `json:"metadata,omitempty" gorm:"column:metadata;type:jsonb;serializer:json"`
	CreatedAt          time.Time      `json:"created_at" gorm:"column:created_at"`
	UpdatedAt          *time.Time     `json:"updated_at,omitempty" gorm:"column:updated_at"`
	DeletedAt          gorm.DeletedAt `json:"-" gorm:"index"`
}
