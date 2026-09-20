package serviceidentity

import domainspace "family-assistant/internal/domain/space"

type ActorContext struct {
	UserID            string
	Memberships       []domainspace.ResolvedMembership
	SpaceID           string
	SpaceName         string
	SpaceType         string
	MemberID          string
	RoleID            string
	RoleName          string
	Permissions       map[string]struct{}
	ExternalProvider  string
	ExternalID        string
	Source            string
	Channel           string
	InitiatorUserID   string
	InitiatorRoleName string

	// Deprecated family fields stay until the reminder vertical slice moves to
	// Space-scoped storage. New authorization code must use SpaceID.
	FamilyID        string
	HermesProfileID string
}

func (a ActorContext) HasPermission(permission string) bool {
	_, ok := a.Permissions[permission]
	return ok
}
