package interfaceonboarding

import (
	"context"

	domainaudit "family-assistant/internal/domain/audit"
	domainrole "family-assistant/internal/domain/role"
)

type Input struct {
	Name       string
	BirthDate  string
	Consent    bool
	Provider   string
	ExternalID string
	Channel    string
}

type Result struct {
	Status  string
	UserID  string
	SpaceID string
}

type ServiceOnboardingInterface interface {
	Register(context.Context, Input) (Result, error)
}

type RoleFinder interface {
	GetByName(context.Context, string) (domainrole.Role, error)
}

type AuditStore interface {
	Store(context.Context, domainaudit.AuditEvent) error
}
