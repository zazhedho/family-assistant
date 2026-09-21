package authorization

import (
	"context"
	"strings"

	domainauthorization "family-assistant/internal/domain/authorization"
	domainidentity "family-assistant/internal/domain/identity"
	interfaceauthorization "family-assistant/internal/interfaces/authorization"
)

type authorizer struct{}

func NewAuthorizer() interfaceauthorization.Authorizer {
	return authorizer{}
}

func (authorizer) Authorize(_ context.Context, actor domainidentity.ActorContext, permission string, resource domainauthorization.Resource) error {
	if err := ValidateResource(resource); err != nil {
		return err
	}
	if strings.TrimSpace(permission) == "" {
		return ErrForbidden
	}
	if !actor.HasPermission(permission) {
		return ErrForbidden
	}
	if strings.TrimSpace(resource.SpaceID) != "" {
		if strings.TrimSpace(actor.SpaceID) != strings.TrimSpace(resource.SpaceID) {
			return ErrNotFound
		}
		return nil
	}
	return ErrNotFound
}

var _ interfaceauthorization.Authorizer = authorizer{}
