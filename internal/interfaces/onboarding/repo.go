package interfaceonboarding

import (
	"context"

	domainonboarding "family-assistant/internal/domain/onboarding"
)

type RepoOnboardingInterface interface {
	FindByExternalIdentity(context.Context, string, string) (domainonboarding.AccountRef, error)
	Create(context.Context, domainonboarding.Registration) error
}
