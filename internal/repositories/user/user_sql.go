package repositoryuser

import (
	"context"
	domainuser "starter-kit/internal/domain/user"
	interfaceuser "starter-kit/internal/interfaces/user"
	repositorygeneric "starter-kit/internal/repositories/generic"
	"starter-kit/pkg/filter"

	"gorm.io/gorm"
)

type repo struct {
	*repositorygeneric.GenericRepository[domainuser.Users]
}

func NewUserRepo(db *gorm.DB) interfaceuser.RepoUserInterface {
	return &repo{GenericRepository: repositorygeneric.New[domainuser.Users](db)}
}

func (r *repo) Store(ctx context.Context, user domainuser.Users) error {
	if user.Phone == "" {
		return r.DB.WithContext(ctx).Omit("phone").Create(&user).Error
	}
	return r.GenericRepository.Store(ctx, user)
}

func (r *repo) GetByEmail(ctx context.Context, email string) (ret domainuser.Users, err error) {
	return r.GetOneByField(ctx, "email", email)
}

func (r *repo) GetByPhone(ctx context.Context, phone string) (ret domainuser.Users, err error) {
	return r.GetOneByField(ctx, "phone", phone)
}

func (r *repo) GetAll(ctx context.Context, params filter.BaseParams) (ret []domainuser.Users, totalData int64, err error) {
	return r.GenericRepository.GetAll(ctx, params, repositorygeneric.QueryOptions{
		Search:         repositorygeneric.BuildSearchFunc("name", "email", "phone"),
		AllowedFilters: []string{"id", "name", "email", "phone", "role", "role_id", "created_at", "updated_at"},
		AllowedOrderColumns: []string{
			"name",
			"email",
			"phone",
			"role",
			"last_login_at",
			"login_provider",
			"created_at",
			"updated_at",
		},
	})
}
