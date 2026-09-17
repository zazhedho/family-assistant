package interfacepermission

import (
	"context"
	domainpermission "starter-kit/internal/domain/permission"
	interfacegeneric "starter-kit/internal/interfaces/generic"
)

type RepoPermissionInterface interface {
	interfacegeneric.GenericRepository[domainpermission.Permission]

	GetByName(ctx context.Context, name string) (domainpermission.Permission, error)
	GetByResource(ctx context.Context, resource string) ([]domainpermission.Permission, error)
	GetUserPermissions(ctx context.Context, userId string) ([]domainpermission.Permission, error)
}
