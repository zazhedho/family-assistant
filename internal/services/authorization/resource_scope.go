package authorization

import "strings"

type Resource struct {
	SpaceID           string
	CreatedByMemberID string
	AssigneeMemberID  string
}

func ValidateResource(resource Resource) error {
	if strings.TrimSpace(resource.SpaceID) == "" {
		return &ValidationError{Field: "space_id", Reason: "is required"}
	}
	return nil
}
