package serviceshared

import (
	"context"
	"starter-kit/internal/authscope"
	domainmenu "starter-kit/internal/domain/menu"
	domainpermission "starter-kit/internal/domain/permission"
	interfacepermission "starter-kit/internal/interfaces/permission"
	"strings"
)

func ResolveAccessibleMenus(activeMenus []domainmenu.MenuItem, resources []string) []domainmenu.MenuItem {
	if len(activeMenus) == 0 || len(resources) == 0 {
		return []domainmenu.MenuItem{}
	}

	menuByID := make(map[string]domainmenu.MenuItem, len(activeMenus))
	menuByName := make(map[string]domainmenu.MenuItem, len(activeMenus))
	for _, menu := range activeMenus {
		menuByID[menu.Id] = menu
		menuByName[menu.Name] = menu
	}

	allowedIDs := make(map[string]struct{})
	for _, resource := range resources {
		menu, exists := menuByName[resource]
		if !exists {
			continue
		}

		allowedIDs[menu.Id] = struct{}{}
		parentID := menu.ParentId
		for parentID != nil && *parentID != "" {
			parentMenu, exists := menuByID[*parentID]
			if !exists || !parentMenu.IsActive || parentMenu.DeletedAt.Valid {
				break
			}

			allowedIDs[parentMenu.Id] = struct{}{}
			parentID = parentMenu.ParentId
		}
	}

	ret := make([]domainmenu.MenuItem, 0, len(allowedIDs))
	for _, menu := range activeMenus {
		if _, exists := allowedIDs[menu.Id]; exists {
			ret = append(ret, menu)
		}
	}

	return ret
}

func ResolveAccessibleMenuIDs(activeMenus []domainmenu.MenuItem, resources []string) []string {
	menus := ResolveAccessibleMenus(activeMenus, resources)
	ids := make([]string, 0, len(menus))
	for _, menu := range menus {
		ids = append(ids, menu.Id)
	}

	return ids
}

func HasPermission(ctx context.Context, permissionRepo interfacepermission.RepoPermissionInterface, resource, action string) (bool, error) {
	scope := authscope.FromContext(ctx)
	if scope.Has(resource, action) {
		return true, nil
	}

	if len(scope.Permissions) > 0 || strings.TrimSpace(scope.UserID) == "" {
		return false, nil
	}

	permissions, err := permissionRepo.GetUserPermissions(ctx, scope.UserID)
	if err != nil {
		return false, err
	}

	return permissionListHasAccess(permissions, resource, action), nil
}

func permissionListHasAccess(permissions []domainpermission.Permission, resource, action string) bool {
	targetKey := authscope.PermissionKey(resource, action)
	if targetKey == "" {
		return false
	}

	for _, permission := range permissions {
		if authscope.PermissionKey(permission.Resource, permission.Action) == targetKey {
			return true
		}
	}

	return false
}
