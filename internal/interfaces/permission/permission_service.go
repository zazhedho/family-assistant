package interfacepermission

import (
	"context"
	domainpermission "github.com/zazhedho/family-assistant/internal/domain/permission"
	"github.com/zazhedho/family-assistant/internal/dto"
	"github.com/zazhedho/family-assistant/pkg/filter"
)

type ServicePermissionInterface interface {
	Create(ctx context.Context, req dto.PermissionCreate) (domainpermission.Permission, error)
	GetByID(ctx context.Context, id string) (domainpermission.Permission, error)
	GetAll(ctx context.Context, params filter.BaseParams) ([]domainpermission.Permission, int64, error)
	GetByResource(ctx context.Context, resource string) ([]domainpermission.Permission, error)
	GetRolePermissions(ctx context.Context, roleID string) ([]domainpermission.Permission, error)
	GetUserPermissions(ctx context.Context, userId string) ([]domainpermission.Permission, error)
	Update(ctx context.Context, id string, req dto.PermissionUpdate) (domainpermission.Permission, error)
	Delete(ctx context.Context, id string) error
}
