package authorization

import "strings"

type Scope string

const (
	ScopePersonal Scope = "PERSONAL"
	ScopeFamily   Scope = "FAMILY"
)

type Resource struct {
	FamilyID      string
	OwnerMemberID string
	OwnerRoleName string
	Scope         Scope
}

func ValidateResource(resource Resource) error {
	switch resource.Scope {
	case ScopePersonal:
		if strings.TrimSpace(resource.OwnerMemberID) == "" {
			return &ValidationError{Field: "owner_member_id", Reason: "is required for PERSONAL scope"}
		}
		if strings.TrimSpace(resource.FamilyID) == "" {
			return &ValidationError{Field: "family_id", Reason: "is required"}
		}
	case ScopeFamily:
		if strings.TrimSpace(resource.FamilyID) == "" {
			return &ValidationError{Field: "family_id", Reason: "is required for FAMILY scope"}
		}
	default:
		return &ValidationError{Field: "scope", Reason: "must be PERSONAL or FAMILY"}
	}

	return nil
}
