package interfaceonboarding

import (
	"context"

	domainidentity "family-assistant/internal/domain/identity"
	domainonboarding "family-assistant/internal/domain/onboarding"
)

type RepoOnboardingInterface interface {
	FindByExternalIdentity(context.Context, string, string) (domainonboarding.AccountRef, error)
	FindByPhone(context.Context, string) (domainonboarding.AccountRef, error)
	LinkExternalIdentity(context.Context, domainidentity.ExternalIdentity) error
	Create(context.Context, domainonboarding.Registration) error
}
