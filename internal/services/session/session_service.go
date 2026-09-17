package servicesession

import (
	"context"
	"fmt"
	domainsession "starter-kit/internal/domain/session"
	domainuser "starter-kit/internal/domain/user"
	interfacesession "starter-kit/internal/interfaces/session"
	"starter-kit/utils"
	"time"

	"github.com/google/uuid"
)

type ServiceSession struct {
	SessionRepo interfacesession.RepoSessionInterface
}

func NewSessionService(sessionRepo interfacesession.RepoSessionInterface) *ServiceSession {
	return &ServiceSession{
		SessionRepo: sessionRepo,
	}
}

func (s *ServiceSession) CreateSession(ctx context.Context, user *domainuser.Users, accessToken string, refreshToken string, requestMeta domainsession.RequestMeta) (*domainsession.Session, error) {
	sessionID := uuid.New().String()

	refreshExpHours := utils.GetEnv("REFRESH_TOKEN_EXP_HOURS", 168)
	expiresAt := time.Now().Add(time.Hour * time.Duration(refreshExpHours))

	userAgent := requestMeta.UserAgent
	deviceInfo := requestMeta.DeviceInfo
	if deviceInfo == "" {
		deviceInfo = extractDeviceInfo(userAgent)
	}

	session := &domainsession.Session{
		SessionID:    sessionID,
		UserID:       user.Id,
		Username:     user.Name,
		Email:        user.Email,
		Role:         user.Role,
		AccessToken:  accessToken,
		RefreshToken: refreshToken,
		DeviceInfo:   deviceInfo,
		IP:           requestMeta.IP,
		UserAgent:    userAgent,
		LoginAt:      time.Now(),
		LastActivity: time.Now(),
		ExpiresAt:    expiresAt,
	}

	if err := s.SessionRepo.Create(ctx, session); err != nil {
		return nil, fmt.Errorf("failed to create session: %w", err)
	}

	return session, nil
}

func (s *ServiceSession) ValidateSession(ctx context.Context, token string) (*domainsession.Session, error) {
	session, err := s.SessionRepo.GetByToken(ctx, token)
	if err != nil {
		return nil, fmt.Errorf("session not found or expired")
	}

	if time.Now().After(session.ExpiresAt) {
		if delErr := s.SessionRepo.Delete(ctx, session.SessionID); delErr != nil {
			fmt.Printf("Failed to delete expired session: %v\n", delErr)
		}
		return nil, fmt.Errorf("session expired")
	}

	if err := s.SessionRepo.UpdateActivity(ctx, session.SessionID); err != nil {
		fmt.Printf("Failed to update session activity: %v\n", err)
	}

	return session, nil
}

func (s *ServiceSession) GetUserSessions(ctx context.Context, userID string, currentSessionID string) ([]*domainsession.SessionInfo, error) {
	sessions, err := s.SessionRepo.GetByUserID(ctx, userID)
	if err != nil {
		return nil, err
	}

	sessionInfos := make([]*domainsession.SessionInfo, 0, len(sessions))
	for _, session := range sessions {
		info := &domainsession.SessionInfo{
			SessionID:        session.SessionID,
			DeviceInfo:       session.DeviceInfo,
			IP:               session.IP,
			LoginAt:          session.LoginAt,
			LastActivity:     session.LastActivity,
			IsCurrentSession: session.SessionID == currentSessionID,
		}
		sessionInfos = append(sessionInfos, info)
	}

	return sessionInfos, nil
}

func (s *ServiceSession) DestroySession(ctx context.Context, sessionID string) error {
	return s.SessionRepo.Delete(ctx, sessionID)
}

func (s *ServiceSession) DestroySessionByToken(ctx context.Context, token string) error {
	session, err := s.SessionRepo.GetByToken(ctx, token)
	if err != nil {
		return err
	}
	return s.SessionRepo.Delete(ctx, session.SessionID)
}

func (s *ServiceSession) GetSessionByToken(ctx context.Context, token string) (*domainsession.Session, error) {
	return s.SessionRepo.GetByToken(ctx, token)
}

func (s *ServiceSession) GetSessionByRefreshToken(ctx context.Context, refreshToken string) (*domainsession.Session, error) {
	return s.SessionRepo.GetByRefreshToken(ctx, refreshToken)
}

func (s *ServiceSession) GetSessionBySessionID(ctx context.Context, sessionID string) (*domainsession.Session, error) {
	return s.SessionRepo.GetBySessionID(ctx, sessionID)
}

func (s *ServiceSession) RotateSessionTokens(ctx context.Context, sessionID string, accessToken string, refreshToken string, expiresAt time.Time) error {
	return s.SessionRepo.RotateTokens(ctx, sessionID, accessToken, refreshToken, expiresAt)
}

func (s *ServiceSession) DestroyAllUserSessions(ctx context.Context, userID string) error {
	return s.SessionRepo.DeleteByUserID(ctx, userID)
}

func (s *ServiceSession) DestroyOtherSessions(ctx context.Context, userID string, currentSessionID string) error {
	sessions, err := s.SessionRepo.GetByUserID(ctx, userID)
	if err != nil {
		return err
	}

	for _, session := range sessions {
		if session.SessionID != currentSessionID {
			if err := s.SessionRepo.Delete(ctx, session.SessionID); err != nil {
				return err
			}
		}
	}

	return nil
}

var _ interfacesession.ServiceSessionInterface = (*ServiceSession)(nil)
