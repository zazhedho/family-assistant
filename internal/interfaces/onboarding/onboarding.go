package interfaceonboarding

import (
	"context"

	domainaudit "family-assistant/internal/domain/audit"
	domainrole "family-assistant/internal/domain/role"
	"family-assistant/internal/dto"
)

type ServiceOnboardingInterface interface {
	Register(context.Context, dto.AccountRegistrationInput) (dto.AccountRegistrationResult, error)
	LinkExisting(context.Context, dto.AccountLinkInput) (dto.AccountRegistrationResult, error)
}

type RoleFinder interface {
	GetByName(context.Context, string) (domainrole.Role, error)
}

type AuditStore interface {
	Store(context.Context, domainaudit.AuditEvent) error
}
