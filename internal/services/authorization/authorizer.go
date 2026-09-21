package authorization

import (
	"context"
	"strings"

	domainidentity "family-assistant/internal/domain/identity"
)

type Authorizer interface {
	Authorize(ctx context.Context, actor domainidentity.ActorContext, permission string, resource Resource) error
}

type authorizer struct{}

func NewAuthorizer() Authorizer {
	return authorizer{}
}

func (authorizer) Authorize(_ context.Context, actor domainidentity.ActorContext, permission string, resource Resource) error {
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

var _ Authorizer = authorizer{}
