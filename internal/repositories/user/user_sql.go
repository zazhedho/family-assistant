package repositoryuser

import (
	"context"
	domainspace "family-assistant/internal/domain/space"
	domainuser "family-assistant/internal/domain/user"
	interfaceuser "family-assistant/internal/interfaces/user"
	repositorygeneric "family-assistant/internal/repositories/generic"
	"family-assistant/pkg/filter"
	"strings"

	"gorm.io/gorm"
)

type repo struct {
	*repositorygeneric.GenericRepository[domainuser.Users]
}

func NewUserRepo(db *gorm.DB) interfaceuser.RepoUserInterface {
	return &repo{GenericRepository: repositorygeneric.New[domainuser.Users](db)}
}

func (r *repo) Store(ctx context.Context, user domainuser.Users) error {
	return storeUser(r.DB, ctx, user)
}

func (r *repo) StoreWithPersonalSpace(ctx context.Context, user domainuser.Users, space domainspace.Space, member domainspace.Member) error {
	return r.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := storeUser(tx, ctx, user); err != nil {
			return err
		}
		if err := tx.WithContext(ctx).Create(&space).Error; err != nil {
			return err
		}
		return tx.WithContext(ctx).Create(&member).Error
	})
}

func storeUser(db *gorm.DB, ctx context.Context, user domainuser.Users) error {
	var omit []string
	if strings.TrimSpace(user.Phone) == "" {
		omit = append(omit, "phone")
	}
	if user.BirthDate == nil {
		omit = append(omit, "birth_date")
	}
	if strings.TrimSpace(user.AgeVerificationMethod) == "" {
		omit = append(omit, "age_verification_method")
	}
	if user.AgeVerifiedAt == nil {
		omit = append(omit, "age_verified_at")
	}
	query := db.WithContext(ctx)
	if len(omit) > 0 {
		query = query.Omit(omit...)
	}
	return query.Create(&user).Error
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
