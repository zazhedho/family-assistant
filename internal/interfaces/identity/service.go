package interfaceidentity

import (
	"context"

	domainidentity "family-assistant/internal/domain/identity"
	domainpermission "family-assistant/internal/domain/permission"
)

type LinkService interface {
	Issue(context.Context, string, string) (string, error)
	Link(context.Context, string, string, string) (*domainidentity.ExternalIdentity, error)
	Revoke(context.Context, string, string, string) error
}

type Resolver interface {
	ResolveExternal(context.Context, string, string, string) (domainidentity.ActorContext, error)
}

type UserResolver interface {
	ResolveUser(context.Context, string, string) (domainidentity.ActorContext, error)
}

type PermissionLoader interface {
	GetRolePermissions(context.Context, string) ([]domainpermission.Permission, error)
}
