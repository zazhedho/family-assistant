package authorization

import (
	"strings"

	domainauthorization "family-assistant/internal/domain/authorization"
)

func ValidateResource(resource domainauthorization.Resource) error {
	if strings.TrimSpace(resource.SpaceID) == "" {
		return &ValidationError{Field: "space_id", Reason: "is required"}
	}
	return nil
}
