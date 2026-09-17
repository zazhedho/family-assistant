package interfacesession

import (
	"context"
	"time"

	domainsession "starter-kit/internal/domain/session"
	domainuser "starter-kit/internal/domain/user"
)

type ServiceSessionInterface interface {
	CreateSession(ctx context.Context, user *domainuser.Users, accessToken string, refreshToken string, requestMeta domainsession.RequestMeta) (*domainsession.Session, error)
	ValidateSession(ctx context.Context, token string) (*domainsession.Session, error)
	GetUserSessions(ctx context.Context, userID string, currentSessionID string) ([]*domainsession.SessionInfo, error)
	DestroySession(ctx context.Context, sessionID string) error
	DestroySessionByToken(ctx context.Context, token string) error
	DestroyAllUserSessions(ctx context.Context, userID string) error
	DestroyOtherSessions(ctx context.Context, userID string, currentSessionID string) error
	GetSessionByToken(ctx context.Context, token string) (*domainsession.Session, error)
	GetSessionByRefreshToken(ctx context.Context, refreshToken string) (*domainsession.Session, error)
	GetSessionBySessionID(ctx context.Context, sessionID string) (*domainsession.Session, error)
	RotateSessionTokens(ctx context.Context, sessionID string, accessToken string, refreshToken string, expiresAt time.Time) error
}
