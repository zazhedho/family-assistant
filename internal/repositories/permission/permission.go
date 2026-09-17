package repositorypermission

import (
	"context"
	domainpermission "starter-kit/internal/domain/permission"
	interfacepermission "starter-kit/internal/interfaces/permission"
	repositorygeneric "starter-kit/internal/repositories/generic"
	"starter-kit/pkg/filter"
	"starter-kit/utils"

	"gorm.io/gorm"
)

type repo struct {
	*repositorygeneric.GenericRepository[domainpermission.Permission]
}

func NewPermissionRepo(db *gorm.DB) interfacepermission.RepoPermissionInterface {
	return &repo{GenericRepository: repositorygeneric.New[domainpermission.Permission](db)}
}

func (r *repo) GetByName(ctx context.Context, name string) (ret domainpermission.Permission, err error) {
	return r.GetOneByField(ctx, "name", name)
}

func (r *repo) GetAll(ctx context.Context, params filter.BaseParams) (ret []domainpermission.Permission, totalData int64, err error) {
	return r.GenericRepository.GetAll(ctx, params, repositorygeneric.QueryOptions{
		Search:         repositorygeneric.BuildSearchFunc("name", "display_name", "description", "resource"),
		AllowedFilters: []string{"id", "name", "display_name", "resource", "action", "created_at", "updated_at"},
		AllowedOrderColumns: []string{
			"name",
			"display_name",
			"resource",
			"action",
			"created_at",
			"updated_at",
		},
	})
}

func (r *repo) GetByResource(ctx context.Context, resource string) (ret []domainpermission.Permission, err error) {
	return r.GetManyByField(ctx, "resource", resource)
}

func (r *repo) GetUserPermissions(ctx context.Context, userId string) (ret []domainpermission.Permission, err error) {
	var user struct {
		RoleId *string
		Role   string
	}
	if err = r.DB.WithContext(ctx).Table("users").Select("role_id, role").Where("id = ?", userId).First(&user).Error; err != nil {
		return nil, err
	}

	if user.Role == utils.RoleSuperAdmin {
		if err = r.DB.WithContext(ctx).Where("deleted_at IS NULL").Order("resource, action").Find(&ret).Error; err != nil {
			return nil, err
		}
		return ret, nil
	}

	if user.RoleId == nil || *user.RoleId == "" {
		return []domainpermission.Permission{}, nil
	}

	query := `
		SELECT DISTINCT p.*
		FROM permissions p
		INNER JOIN role_permissions rp ON p.id = rp.permission_id
		WHERE rp.role_id = ? AND p.deleted_at IS NULL
		ORDER BY p.resource, p.action
	`
	if err = r.DB.WithContext(ctx).Raw(query, *user.RoleId).Scan(&ret).Error; err != nil {
		return nil, err
	}

	return ret, nil
}
