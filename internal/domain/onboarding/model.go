package domainonboarding

import (
	"context"

	domainidentity "family-assistant/internal/domain/identity"
	domainspace "family-assistant/internal/domain/space"
	domainuser "family-assistant/internal/domain/user"
)

type AccountRef struct {
	UserID  string
	SpaceID string
}

type Registration struct {
	User     domainuser.Users
	Space    domainspace.Space
	Member   domainspace.Member
	Identity domainidentity.ExternalIdentity
}

type Repository interface {
	FindByExternalIdentity(context.Context, string, string) (AccountRef, error)
	Create(context.Context, Registration) error
}
