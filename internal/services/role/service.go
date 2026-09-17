package servicerole

import (
	"context"
	"errors"
	"starter-kit/internal/authscope"
	permissioncache "starter-kit/internal/cache/permission"
	domainrole "starter-kit/internal/domain/role"
	"starter-kit/internal/dto"
	interfacemenu "starter-kit/internal/interfaces/menu"
	interfacepermission "starter-kit/internal/interfaces/permission"
	interfacerole "starter-kit/internal/interfaces/role"
	serviceshared "starter-kit/internal/services/shared"
	"starter-kit/pkg/filter"
	"starter-kit/utils"
	"time"
)

type RoleService struct {
	RoleRepo        interfacerole.RepoRoleInterface
	PermissionRepo  interfacepermission.RepoPermissionInterface
	MenuRepo        interfacemenu.RepoMenuInterface
	PermissionCache permissioncache.Invalidator
}

func NewRoleService(
	roleRepo interfacerole.RepoRoleInterface,
	permissionRepo interfacepermission.RepoPermissionInterface,
	menuRepo interfacemenu.RepoMenuInterface,
	invalidators ...permissioncache.Invalidator,
) *RoleService {
	service := &RoleService{
		RoleRepo:       roleRepo,
		PermissionRepo: permissionRepo,
		MenuRepo:       menuRepo,
	}
	if len(invalidators) > 0 {
		service.PermissionCache = invalidators[0]
	}
	return service
}

func (s *RoleService) Create(ctx context.Context, req dto.RoleCreate) (domainrole.Role, error) {
	existing, _ := s.RoleRepo.GetByName(ctx, req.Name)
	if existing.Id != "" {
		return domainrole.Role{}, errors.New("role with this name already exists")
	}

	data := domainrole.Role{
		Id:          utils.CreateUUID(),
		Name:        utils.StripHTML(req.Name),
		DisplayName: utils.StripHTML(req.DisplayName),
		Description: utils.StripHTML(req.Description),
		IsSystem:    false,
		CreatedAt:   time.Now(),
	}

	if err := s.RoleRepo.Store(ctx, data); err != nil {
		return domainrole.Role{}, err
	}

	return data, nil
}

func (s *RoleService) GetByID(ctx context.Context, id string) (domainrole.Role, error) {
	return s.RoleRepo.GetByID(ctx, id)
}

func (s *RoleService) GetByIDWithDetails(ctx context.Context, id string) (dto.RoleWithDetails, error) {
	role, err := s.RoleRepo.GetByID(ctx, id)
	if err != nil {
		return dto.RoleWithDetails{}, err
	}

	permissionIds, err := s.RoleRepo.GetRolePermissions(ctx, id)
	if err != nil {
		return dto.RoleWithDetails{}, err
	}

	menuIds, err := s.deriveMenuIDsFromPermissions(ctx, permissionIds)
	if err != nil {
		return dto.RoleWithDetails{}, err
	}

	updatedAt := ""
	if role.UpdatedAt != nil {
		updatedAt = role.UpdatedAt.Format(time.RFC3339)
	}

	return dto.RoleWithDetails{
		Id:            role.Id,
		Name:          role.Name,
		DisplayName:   role.DisplayName,
		Description:   role.Description,
		IsSystem:      role.IsSystem,
		PermissionIds: permissionIds,
		MenuIds:       menuIds,
		CreatedAt:     role.CreatedAt.Format(time.RFC3339),
		UpdatedAt:     updatedAt,
	}, nil
}

func (s *RoleService) GetAll(ctx context.Context, params filter.BaseParams) ([]domainrole.Role, int64, error) {
	scope := authscope.FromContext(ctx)
	roles, total, err := s.RoleRepo.GetAll(ctx, params)
	if err != nil {
		return nil, 0, err
	}

	if scope.Role != utils.RoleSuperAdmin {
		filteredRoles := make([]domainrole.Role, 0)
		for _, role := range roles {
			if role.Name != utils.RoleSuperAdmin {
				filteredRoles = append(filteredRoles, role)
			}
		}
		superadminCount := int64(len(roles) - len(filteredRoles))
		return filteredRoles, total - superadminCount, nil
	}

	return roles, total, nil
}

func (s *RoleService) Update(ctx context.Context, id string, req dto.RoleUpdate) (domainrole.Role, error) {
	role, err := s.RoleRepo.GetByID(ctx, id)
	if err != nil {
		return domainrole.Role{}, err
	}

	if role.IsSystem {
		return domainrole.Role{}, errors.New("cannot update system roles")
	}

	if req.DisplayName != "" {
		role.DisplayName = utils.StripHTML(req.DisplayName)
	}
	role.Description = utils.StripHTML(req.Description)
	role.UpdatedAt = new(time.Now())

	if err := s.RoleRepo.Update(ctx, role); err != nil {
		return domainrole.Role{}, err
	}

	return role, nil
}

func (s *RoleService) Delete(ctx context.Context, id string) error {
	role, err := s.RoleRepo.GetByID(ctx, id)
	if err != nil {
		return err
	}

	if role.IsSystem {
		return errors.New("cannot delete system roles")
	}

	if err := s.RoleRepo.Delete(ctx, id); err != nil {
		return err
	}
	s.invalidatePermissionCache(ctx)
	return nil
}

func (s *RoleService) AssignPermissions(ctx context.Context, roleId string, req dto.AssignPermissions) error {
	scope := authscope.FromContext(ctx)
	role, err := s.RoleRepo.GetByID(ctx, roleId)
	if err != nil {
		return err
	}

	if role.IsSystem {
		canManageSystem, err := serviceshared.HasPermission(ctx, s.PermissionRepo, "roles", "manage_system")
		if err != nil {
			return err
		}
		if !canManageSystem {
			return errors.New("access denied: missing permission roles:manage_system")
		}
		if role.Name == utils.RoleSuperAdmin && scope.Role != utils.RoleSuperAdmin {
			return errors.New("access denied: cannot modify superadmin role")
		}
	}

	for _, permId := range req.PermissionIds {
		if _, err := s.PermissionRepo.GetByID(ctx, permId); err != nil {
			return errors.New("invalid permission ID: " + permId)
		}
	}

	if err := s.RoleRepo.AssignPermissions(ctx, roleId, req.PermissionIds); err != nil {
		return err
	}
	s.invalidatePermissionCache(ctx)
	return nil
}

func (s *RoleService) invalidatePermissionCache(ctx context.Context) {
	if s.PermissionCache != nil {
		s.PermissionCache.DeleteAll(ctx)
	}
}

func (s *RoleService) AssignMenus(ctx context.Context, roleId string, req dto.AssignMenus) error {
	return errors.New("menu access is derived from permissions; assign permissions instead")
}

func (s *RoleService) GetRolePermissions(ctx context.Context, roleId string) ([]string, error) {
	return s.RoleRepo.GetRolePermissions(ctx, roleId)
}

func (s *RoleService) GetRoleMenus(ctx context.Context, roleId string) ([]string, error) {
	permissionIds, err := s.RoleRepo.GetRolePermissions(ctx, roleId)
	if err != nil {
		return nil, err
	}

	return s.deriveMenuIDsFromPermissions(ctx, permissionIds)
}

func (s *RoleService) deriveMenuIDsFromPermissions(ctx context.Context, permissionIds []string) ([]string, error) {
	resources := make([]string, 0, len(permissionIds))

	for _, permissionId := range permissionIds {
		permission, err := s.PermissionRepo.GetByID(ctx, permissionId)
		if err != nil {
			return nil, err
		}

		if permission.Resource == "" {
			continue
		}

		resources = append(resources, permission.Resource)
	}

	activeMenus, err := s.MenuRepo.GetActiveMenus(ctx)
	if err != nil {
		return nil, err
	}

	return serviceshared.ResolveAccessibleMenuIDs(activeMenus, resources), nil
}

var _ interfacerole.ServiceRoleInterface = (*RoleService)(nil)
