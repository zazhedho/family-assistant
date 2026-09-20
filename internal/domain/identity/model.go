package domainidentity

import (
	"errors"
	"strings"
	"time"

	"gorm.io/gorm"
)

const (
	ProviderHermes = "hermes"
	StatusActive   = "ACTIVE"
	StatusRevoked  = "REVOKED"
)

var (
	ErrInvalidLinkToken = errors.New("invalid link token")
	ErrIdentityConflict = errors.New("external identity is already linked")
	ErrIdentityNotFound = errors.New("external identity not found")
)

func NormalizeProvider(provider string) string {
	return strings.ToLower(strings.TrimSpace(provider))
}

func (ExternalIdentity) TableName() string {
	return "external_identities"
}

func (LinkToken) TableName() string {
	return "identity_link_tokens"
}

type ExternalIdentity struct {
	ID         string         `json:"id" gorm:"column:id;primaryKey"`
	UserID     string         `json:"user_id" gorm:"column:user_id"`
	Provider   string         `json:"provider" gorm:"column:provider"`
	ExternalID string         `json:"external_id" gorm:"column:external_id"`
	Status     string         `json:"status" gorm:"column:status"`
	VerifiedAt time.Time      `json:"verified_at" gorm:"column:verified_at"`
	Metadata   map[string]any `json:"metadata,omitempty" gorm:"column:metadata;type:jsonb;serializer:json"`
	CreatedAt  time.Time      `json:"created_at" gorm:"column:created_at"`
	UpdatedAt  time.Time      `json:"updated_at" gorm:"column:updated_at"`
	DeletedAt  gorm.DeletedAt `json:"-" gorm:"column:deleted_at"`
}

type LinkToken struct {
	ID         string         `json:"id" gorm:"column:id;primaryKey"`
	UserID     string         `json:"user_id" gorm:"column:user_id"`
	Provider   string         `json:"provider" gorm:"column:provider"`
	TokenHash  string         `json:"-" gorm:"column:token_hash"`
	ExpiresAt  time.Time      `json:"expires_at" gorm:"column:expires_at"`
	ConsumedAt *time.Time     `json:"consumed_at,omitempty" gorm:"column:consumed_at"`
	CreatedAt  time.Time      `json:"created_at" gorm:"column:created_at"`
	UpdatedAt  time.Time      `json:"updated_at" gorm:"column:updated_at"`
	DeletedAt  gorm.DeletedAt `json:"-" gorm:"column:deleted_at"`
}
