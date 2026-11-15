package interfaceonboarding

import (
	"context"
	"time"

	domainidentity "family-assistant/internal/domain/identity"
	domainonboarding "family-assistant/internal/domain/onboarding"
)

type RepoOnboardingInterface interface {
	FindByExternalIdentity(context.Context, string, string) (domainonboarding.AccountRef, error)
	FindByPhone(context.Context, string) (domainonboarding.AccountRef, error)
	LinkExternalIdentity(context.Context, domainidentity.ExternalIdentity) error
	Create(context.Context, domainonboarding.Registration) error
	FindAccountForRename(ctx context.Context, provider, externalID string) (domainonboarding.UserRef, error)
	NameTakenInSharedSpaces(ctx context.Context, userID, normalizedName string) (bool, error)
	UpdateUserName(ctx context.Context, userID, name string, now time.Time) error
}
