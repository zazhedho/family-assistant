package authorization

import "strings"

type Scope string

const (
	// Deprecated: Space type replaces family resource scope.
	ScopePersonal Scope = "PERSONAL"
	ScopeFamily   Scope = "FAMILY"
)

type Resource struct {
	SpaceID           string
	CreatedByMemberID string
	AssigneeMemberID  string

	// Deprecated family fields remain only for consumers migrating in Tasks 9-11.
	FamilyID      string
	OwnerMemberID string
	OwnerRoleName string
	Scope         Scope
}

func ValidateResource(resource Resource) error {
	if strings.TrimSpace(resource.SpaceID) == "" {
		return &ValidationError{Field: "space_id", Reason: "is required"}
	}
	return nil
}
