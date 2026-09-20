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
	if strings.TrimSpace(resource.SpaceID) != "" {
		return nil
	}
	if strings.TrimSpace(resource.FamilyID) != "" {
		return validateLegacyResource(resource)
	}
	return &ValidationError{Field: "space_id", Reason: "is required"}
}

func validateLegacyResource(resource Resource) error {
	switch resource.Scope {
	case ScopePersonal:
		if strings.TrimSpace(resource.OwnerMemberID) == "" {
			return &ValidationError{Field: "owner_member_id", Reason: "is required for PERSONAL scope"}
		}
	case ScopeFamily:
		return nil
	default:
		return &ValidationError{Field: "scope", Reason: "must be PERSONAL or FAMILY"}
	}
	return nil
}
