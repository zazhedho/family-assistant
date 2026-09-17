package interfacerole

import (
	"context"
	domainrole "starter-kit/internal/domain/role"
	"starter-kit/internal/dto"
	"starter-kit/pkg/filter"
)

type ServiceRoleInterface interface {
	Create(ctx context.Context, req dto.RoleCreate) (domainrole.Role, error)
	GetByID(ctx context.Context, id string) (domainrole.Role, error)
	GetByIDWithDetails(ctx context.Context, id string) (dto.RoleWithDetails, error)
	GetAll(ctx context.Context, params filter.BaseParams) ([]domainrole.Role, int64, error)
	Update(ctx context.Context, id string, req dto.RoleUpdate) (domainrole.Role, error)
	Delete(ctx context.Context, id string) error
	AssignPermissions(ctx context.Context, roleId string, req dto.AssignPermissions) error
	AssignMenus(ctx context.Context, roleId string, req dto.AssignMenus) error
	GetRolePermissions(ctx context.Context, roleId string) ([]string, error)
	GetRoleMenus(ctx context.Context, roleId string) ([]string, error)
}
