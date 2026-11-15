package domainonboarding

import (
	domainidentity "family-assistant/internal/domain/identity"
	domainspace "family-assistant/internal/domain/space"
	domainuser "family-assistant/internal/domain/user"
)

type AccountRef struct {
	UserID  string
	SpaceID string
}

// UserRef is the caller's own account as resolved from a signed external identity.
type UserRef struct {
	ID   string
	Name string
}

type Registration struct {
	User     domainuser.Users
	Space    domainspace.Space
	Member   domainspace.Member
	Identity domainidentity.ExternalIdentity
}
