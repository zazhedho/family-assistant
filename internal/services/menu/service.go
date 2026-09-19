package servicemenu

import (
	"context"
	domainmenu "family-assistant/internal/domain/menu"
	"family-assistant/internal/dto"
	interfacemenu "family-assistant/internal/interfaces/menu"
	interfacepermission "family-assistant/internal/interfaces/permission"
	serviceshared "family-assistant/internal/services/shared"
	"family-assistant/pkg/filter"
	"time"
)

type MenuService struct {
	MenuRepo       interfacemenu.RepoMenuInterface
	PermissionRepo interfacepermission.RepoPermissionInterface
}

func NewMenuService(menuRepo interfacemenu.RepoMenuInterface, permissionRepo interfacepermission.RepoPermissionInterface) *MenuService {
	return &MenuService{
		MenuRepo:       menuRepo,
		PermissionRepo: permissionRepo,
	}
}

func (s *MenuService) GetByID(ctx context.Context, id string) (domainmenu.MenuItem, error) {
	return s.MenuRepo.GetByID(ctx, id)
}

func (s *MenuService) GetAll(ctx context.Context, params filter.BaseParams) ([]domainmenu.MenuItem, int64, error) {
	return s.MenuRepo.GetAll(ctx, params)
}

func (s *MenuService) GetActiveMenus(ctx context.Context) ([]domainmenu.MenuItem, error) {
	return s.MenuRepo.GetActiveMenus(ctx)
}

func (s *MenuService) GetUserMenus(ctx context.Context, userId string) ([]domainmenu.MenuItem, error) {
	activeMenus, err := s.MenuRepo.GetActiveMenus(ctx)
	if err != nil {
		return nil, err
	}

	permissions, err := s.PermissionRepo.GetUserPermissions(ctx, userId)
	if err != nil {
		return nil, err
	}

	resources := make([]string, 0, len(permissions))
	for _, permission := range permissions {
		if permission.Resource == "" {
			continue
		}
		resources = append(resources, permission.Resource)
	}

	return serviceshared.ResolveAccessibleMenus(activeMenus, resources), nil
}

func (s *MenuService) Update(ctx context.Context, id string, req dto.MenuUpdate) (domainmenu.MenuItem, error) {
	menu, err := s.MenuRepo.GetByID(ctx, id)
	if err != nil {
		return domainmenu.MenuItem{}, err
	}

	if req.DisplayName != "" {
		menu.DisplayName = req.DisplayName
	}
	if req.Path != "" {
		menu.Path = req.Path
	}
	if req.Icon != "" {
		menu.Icon = req.Icon
	}
	if req.ParentId != nil {
		menu.ParentId = req.ParentId
	}
	if req.OrderIndex != nil {
		menu.OrderIndex = *req.OrderIndex
	}
	if req.IsActive != nil {
		menu.IsActive = *req.IsActive
	}
	menu.UpdatedAt = new(time.Now())

	if err := s.MenuRepo.Update(ctx, menu); err != nil {
		return domainmenu.MenuItem{}, err
	}

	return menu, nil
}

var _ interfacemenu.ServiceMenuInterface = (*MenuService)(nil)
