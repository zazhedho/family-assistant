package repositorysession

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	domainsession "starter-kit/internal/domain/session"
	"starter-kit/pkg/logger"
	"time"

	"github.com/redis/go-redis/v9"
)

type SessionRepository struct {
	Redis *redis.Client
}

func NewSessionRepository(redisClient *redis.Client) *SessionRepository {
	return &SessionRepository{
		Redis: redisClient,
	}
}

const (
	sessionKeyPrefix       = "session:"
	userSessionsKey        = "user_sessions:"
	accessTokenSessionKey  = "access_token_session:"
	refreshTokenSessionKey = "refresh_token_session:"
)

func (r *SessionRepository) Create(ctx context.Context, session *domainsession.Session) error {
	sessionKey := fmt.Sprintf("%s%s", sessionKeyPrefix, session.SessionID)
	userSessionKey := fmt.Sprintf("%s%s", userSessionsKey, session.UserID)
	accessTokenKey := fmt.Sprintf("%s%s", accessTokenSessionKey, session.AccessToken)
	refreshTokenKey := fmt.Sprintf("%s%s", refreshTokenSessionKey, session.RefreshToken)

	sessionData, err := json.Marshal(session)
	if err != nil {
		return fmt.Errorf("failed to marshal session: %w", err)
	}

	ttl := time.Until(session.ExpiresAt)
	if ttl <= 0 {
		return fmt.Errorf("session already expired")
	}

	pipe := r.Redis.Pipeline()
	pipe.Set(ctx, sessionKey, sessionData, ttl)
	pipe.SAdd(ctx, userSessionKey, session.SessionID)
	pipe.Expire(ctx, userSessionKey, ttl)
	pipe.Set(ctx, accessTokenKey, session.SessionID, ttl)
	pipe.Set(ctx, refreshTokenKey, session.SessionID, ttl)

	_, err = pipe.Exec(ctx)
	if err != nil {
		return fmt.Errorf("failed to create session: %w", err)
	}

	logger.WriteLog(logger.LogLevelDebug, fmt.Sprintf("Session created: %s for user: %s", session.SessionID, session.UserID))
	return nil
}

func (r *SessionRepository) GetBySessionID(ctx context.Context, sessionID string) (*domainsession.Session, error) {
	sessionKey := fmt.Sprintf("%s%s", sessionKeyPrefix, sessionID)

	data, err := r.Redis.Get(ctx, sessionKey).Result()
	if errors.Is(err, redis.Nil) {
		return nil, fmt.Errorf("session not found")
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get session: %w", err)
	}

	var session domainsession.Session
	if err := json.Unmarshal([]byte(data), &session); err != nil {
		return nil, fmt.Errorf("failed to unmarshal session: %w", err)
	}

	return &session, nil
}

func (r *SessionRepository) GetByUserID(ctx context.Context, userID string) ([]*domainsession.Session, error) {
	userSessionKey := fmt.Sprintf("%s%s", userSessionsKey, userID)

	sessionIDs, err := r.Redis.SMembers(ctx, userSessionKey).Result()
	if err != nil {
		return nil, fmt.Errorf("failed to get user sessions: %w", err)
	}

	sessions := make([]*domainsession.Session, 0, len(sessionIDs))
	for _, sessionID := range sessionIDs {
		session, err := r.GetBySessionID(ctx, sessionID)
		if err != nil {
			logger.WriteLog(logger.LogLevelDebug, fmt.Sprintf("Skipping invalid session %s: %v", sessionID, err))
			r.Redis.SRem(ctx, userSessionKey, sessionID)
			continue
		}
		sessions = append(sessions, session)
	}

	return sessions, nil
}

func (r *SessionRepository) GetByToken(ctx context.Context, token string) (*domainsession.Session, error) {
	tokenKey := fmt.Sprintf("%s%s", accessTokenSessionKey, token)

	sessionID, err := r.Redis.Get(ctx, tokenKey).Result()
	if errors.Is(err, redis.Nil) {
		return nil, fmt.Errorf("session not found for token")
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get session by token: %w", err)
	}

	return r.GetBySessionID(ctx, sessionID)
}

func (r *SessionRepository) GetByRefreshToken(ctx context.Context, refreshToken string) (*domainsession.Session, error) {
	tokenKey := fmt.Sprintf("%s%s", refreshTokenSessionKey, refreshToken)

	sessionID, err := r.Redis.Get(ctx, tokenKey).Result()
	if errors.Is(err, redis.Nil) {
		return nil, fmt.Errorf("session not found for refresh token")
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get session by refresh token: %w", err)
	}

	return r.GetBySessionID(ctx, sessionID)
}

func (r *SessionRepository) UpdateActivity(ctx context.Context, sessionID string) error {
	session, err := r.GetBySessionID(ctx, sessionID)
	if err != nil {
		return err
	}

	session.LastActivity = time.Now()

	sessionKey := fmt.Sprintf("%s%s", sessionKeyPrefix, sessionID)
	sessionData, err := json.Marshal(session)
	if err != nil {
		return fmt.Errorf("failed to marshal session: %w", err)
	}

	ttl, err := r.Redis.TTL(ctx, sessionKey).Result()
	if err != nil {
		return fmt.Errorf("failed to get session TTL: %w", err)
	}

	err = r.Redis.Set(ctx, sessionKey, sessionData, ttl).Err()
	if err != nil {
		return fmt.Errorf("failed to update session activity: %w", err)
	}

	return nil
}

func (r *SessionRepository) Delete(ctx context.Context, sessionID string) error {
	session, err := r.GetBySessionID(ctx, sessionID)
	if err != nil {
		return err
	}

	sessionKey := fmt.Sprintf("%s%s", sessionKeyPrefix, sessionID)
	userSessionKey := fmt.Sprintf("%s%s", userSessionsKey, session.UserID)
	accessTokenKey := fmt.Sprintf("%s%s", accessTokenSessionKey, session.AccessToken)
	refreshTokenKey := fmt.Sprintf("%s%s", refreshTokenSessionKey, session.RefreshToken)

	pipe := r.Redis.Pipeline()
	pipe.Del(ctx, sessionKey)
	pipe.SRem(ctx, userSessionKey, sessionID)
	pipe.Del(ctx, accessTokenKey)
	pipe.Del(ctx, refreshTokenKey)

	_, err = pipe.Exec(ctx)
	if err != nil {
		return fmt.Errorf("failed to delete session: %w", err)
	}

	logger.WriteLog(logger.LogLevelDebug, fmt.Sprintf("Session deleted: %s", sessionID))
	return nil
}

func (r *SessionRepository) DeleteByUserID(ctx context.Context, userID string) error {
	sessions, err := r.GetByUserID(ctx, userID)
	if err != nil {
		return err
	}

	for _, session := range sessions {
		if err := r.Delete(ctx, session.SessionID); err != nil {
			logger.WriteLog(logger.LogLevelError, fmt.Sprintf("Failed to delete session %s: %v", session.SessionID, err))
		}
	}

	return nil
}

func (r *SessionRepository) RotateTokens(ctx context.Context, sessionID string, accessToken string, refreshToken string, expiresAt time.Time) error {
	session, err := r.GetBySessionID(ctx, sessionID)
	if err != nil {
		return err
	}

	sessionKey := fmt.Sprintf("%s%s", sessionKeyPrefix, sessionID)
	oldAccessTokenKey := fmt.Sprintf("%s%s", accessTokenSessionKey, session.AccessToken)
	oldRefreshTokenKey := fmt.Sprintf("%s%s", refreshTokenSessionKey, session.RefreshToken)
	newAccessTokenKey := fmt.Sprintf("%s%s", accessTokenSessionKey, accessToken)
	newRefreshTokenKey := fmt.Sprintf("%s%s", refreshTokenSessionKey, refreshToken)

	session.AccessToken = accessToken
	session.RefreshToken = refreshToken
	session.LastActivity = time.Now()
	session.ExpiresAt = expiresAt

	sessionData, err := json.Marshal(session)
	if err != nil {
		return fmt.Errorf("failed to marshal session: %w", err)
	}

	ttl := time.Until(expiresAt)
	if ttl <= 0 {
		return fmt.Errorf("session already expired")
	}

	pipe := r.Redis.Pipeline()
	pipe.Del(ctx, oldAccessTokenKey)
	pipe.Del(ctx, oldRefreshTokenKey)
	pipe.Set(ctx, sessionKey, sessionData, ttl)
	pipe.Set(ctx, newAccessTokenKey, sessionID, ttl)
	pipe.Set(ctx, newRefreshTokenKey, sessionID, ttl)
	userSessionKey := fmt.Sprintf("%s%s", userSessionsKey, session.UserID)
	pipe.Expire(ctx, userSessionKey, ttl)

	_, err = pipe.Exec(ctx)
	if err != nil {
		return fmt.Errorf("failed to rotate session tokens: %w", err)
	}

	return nil
}

func (r *SessionRepository) DeleteExpired(ctx context.Context) error {
	logger.WriteLog(logger.LogLevelDebug, "Expired sessions are automatically removed by Redis TTL")
	return nil
}

func (r *SessionRepository) SetExpiration(ctx context.Context, sessionID string, expiration time.Duration) error {
	sessionKey := fmt.Sprintf("%s%s", sessionKeyPrefix, sessionID)

	err := r.Redis.Expire(ctx, sessionKey, expiration).Err()
	if err != nil {
		return fmt.Errorf("failed to set session expiration: %w", err)
	}

	return nil
}
