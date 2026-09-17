package interfacesession

import (
	"context"
	domainsession "starter-kit/internal/domain/session"
	"time"
)

type RepoSessionInterface interface {
	Create(ctx context.Context, session *domainsession.Session) error
	GetBySessionID(ctx context.Context, sessionID string) (*domainsession.Session, error)
	GetByUserID(ctx context.Context, userID string) ([]*domainsession.Session, error)
	GetByToken(ctx context.Context, token string) (*domainsession.Session, error)
	GetByRefreshToken(ctx context.Context, refreshToken string) (*domainsession.Session, error)
	UpdateActivity(ctx context.Context, sessionID string) error
	RotateTokens(ctx context.Context, sessionID string, accessToken string, refreshToken string, expiresAt time.Time) error
	Delete(ctx context.Context, sessionID string) error
	DeleteByUserID(ctx context.Context, userID string) error
	DeleteExpired(ctx context.Context) error
	SetExpiration(ctx context.Context, sessionID string, expiration time.Duration) error
}
