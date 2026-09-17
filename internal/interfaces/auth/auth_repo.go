package interfaceauth

import (
	"context"
	domainauth "starter-kit/internal/domain/auth"
	"time"
)

type RepoAuthInterface interface {
	Store(ctx context.Context, m domainauth.Blacklist) error
	GetByToken(ctx context.Context, token string) (domainauth.Blacklist, error)
	ExistsByToken(ctx context.Context, token string) (bool, error)
	DeleteExpired(ctx context.Context, now time.Time) error
}
