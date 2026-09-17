package repositoryauth

import (
	"context"
	"errors"
	domainauth "starter-kit/internal/domain/auth"
	interfaceauth "starter-kit/internal/interfaces/auth"
	repositorygeneric "starter-kit/internal/repositories/generic"
	"time"

	"gorm.io/gorm"
)

var ErrBlacklistExpiryRequired = errors.New("blacklist expiry is required")

type blacklistRepo struct {
	*repositorygeneric.GenericRepository[domainauth.Blacklist]
}

func NewBlacklistRepo(db *gorm.DB) interfaceauth.RepoAuthInterface {
	return &blacklistRepo{
		GenericRepository: repositorygeneric.New[domainauth.Blacklist](db),
	}
}

func (r *blacklistRepo) Store(ctx context.Context, blacklist domainauth.Blacklist) error {
	if blacklist.ExpiresAt.IsZero() {
		return ErrBlacklistExpiryRequired
	}

	if err := r.DeleteExpired(ctx, time.Now()); err != nil {
		return err
	}

	return r.GenericRepository.Store(ctx, blacklist)
}

func (r *blacklistRepo) GetByToken(ctx context.Context, token string) (domainauth.Blacklist, error) {
	return r.GetOneByField(ctx, "token", token)
}

func (r *blacklistRepo) ExistsByToken(ctx context.Context, token string) (bool, error) {
	return r.ExistsByField(ctx, "token", token)
}

func (r *blacklistRepo) DeleteExpired(ctx context.Context, now time.Time) error {
	return r.DB.WithContext(ctx).
		Where("expires_at IS NULL OR expires_at <= ?", now).
		Delete(&domainauth.Blacklist{}).Error
}
