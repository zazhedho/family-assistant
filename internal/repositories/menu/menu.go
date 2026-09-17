package repositorymenu

import (
	"context"
	"sort"
	domainmenu "starter-kit/internal/domain/menu"
	interfacemenu "starter-kit/internal/interfaces/menu"
	repositorygeneric "starter-kit/internal/repositories/generic"
	"starter-kit/pkg/filter"
	"starter-kit/utils"

	"gorm.io/gorm"
)

type repo struct {
	*repositorygeneric.GenericRepository[domainmenu.MenuItem]
}

func NewMenuRepo(db *gorm.DB) interfacemenu.RepoMenuInterface {
	return &repo{GenericRepository: repositorygeneric.New[domainmenu.MenuItem](db)}
}

func (r *repo) GetByName(ctx context.Context, name string) (ret domainmenu.MenuItem, err error) {
	return r.GetOneByField(ctx, "name", name)
}

func (r *repo) GetAll(ctx context.Context, params filter.BaseParams) (ret []domainmenu.MenuItem, totalData int64, err error) {
	return r.GenericRepository.GetAll(ctx, params, repositorygeneric.QueryOptions{
		Search:         repositorygeneric.BuildSearchFunc("name", "display_name", "path"),
		AllowedFilters: []string{"id", "name", "display_name", "path", "parent_id", "order_index", "is_active", "created_at", "updated_at"},
		AllowedOrderColumns: []string{
			"name",
			"display_name",
			"path",
			"order_index",
			"created_at",
			"updated_at",
		},
	})
}

func (r *repo) GetActiveMenus(ctx context.Context) (ret []domainmenu.MenuItem, err error) {
	if err = r.DB.WithContext(ctx).Where("is_active = ?", true).Order("order_index ASC").Find(&ret).Error; err != nil {
		return nil, err
	}
	return ret, nil
}

func (r *repo) GetUserMenus(ctx context.Context, userId string) (ret []domainmenu.MenuItem, err error) {
	var user struct {
		RoleId *string
		Role   string
	}
	if err = r.DB.WithContext(ctx).Table("users").Select("role_id, role").Where("id = ?", userId).First(&user).Error; err != nil {
		return nil, err
	}

	if user.Role == utils.RoleSuperAdmin {
		return r.GetActiveMenus(ctx)
	}

	if user.RoleId == nil || *user.RoleId == "" {
		return []domainmenu.MenuItem{}, nil
	}

	query := `
		SELECT DISTINCT m.*
		FROM menu_items m
		INNER JOIN permissions p ON p.resource = m.name
		INNER JOIN role_permissions rp ON rp.permission_id = p.id
		WHERE rp.role_id = ?
		  AND m.is_active = true
		  AND m.deleted_at IS NULL
		  AND p.deleted_at IS NULL
		ORDER BY m.order_index ASC
	`
	if err = r.DB.WithContext(ctx).Raw(query, *user.RoleId).Scan(&ret).Error; err != nil {
		return nil, err
	}

	menuByID := make(map[string]domainmenu.MenuItem, len(ret))
	pendingParentIDs := make([]string, 0)
	for _, menu := range ret {
		menuByID[menu.Id] = menu
		if menu.ParentId != nil && *menu.ParentId != "" {
			if _, exists := menuByID[*menu.ParentId]; !exists {
				pendingParentIDs = append(pendingParentIDs, *menu.ParentId)
			}
		}
	}

	for len(pendingParentIDs) > 0 {
		var parentMenus []domainmenu.MenuItem
		if err = r.DB.WithContext(ctx).
			Where("id IN ? AND is_active = ? AND deleted_at IS NULL", pendingParentIDs, true).
			Find(&parentMenus).Error; err != nil {
			return nil, err
		}

		nextParentIDs := make([]string, 0)
		for _, menu := range parentMenus {
			if _, exists := menuByID[menu.Id]; exists {
				continue
			}

			menuByID[menu.Id] = menu
			ret = append(ret, menu)

			if menu.ParentId != nil && *menu.ParentId != "" {
				if _, exists := menuByID[*menu.ParentId]; !exists {
					nextParentIDs = append(nextParentIDs, *menu.ParentId)
				}
			}
		}

		pendingParentIDs = nextParentIDs
	}

	sort.SliceStable(ret, func(i, j int) bool {
		if ret[i].OrderIndex == ret[j].OrderIndex {
			return ret[i].DisplayName < ret[j].DisplayName
		}
		return ret[i].OrderIndex < ret[j].OrderIndex
	})

	return ret, nil
}
