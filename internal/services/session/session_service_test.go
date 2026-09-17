package servicesession

import (
	"context"
	"errors"
	domainsession "starter-kit/internal/domain/session"
	domainuser "starter-kit/internal/domain/user"
	"testing"
	"time"
)

type sessionRepoTestDouble struct {
	session        *domainsession.Session
	userSessions   []*domainsession.Session
	deletedIDs     []string
	activityID     string
	rotatedID      string
	deleteByUserID string
}

func (m *sessionRepoTestDouble) Create(ctx context.Context, session *domainsession.Session) error {
	m.session = new(*session)
	return nil
}
func (m *sessionRepoTestDouble) GetBySessionID(ctx context.Context, sessionID string) (*domainsession.Session, error) {
	if m.session == nil {
		return nil, errors.New("not found")
	}
	return m.session, nil
}
func (m *sessionRepoTestDouble) GetByUserID(ctx context.Context, userID string) ([]*domainsession.Session, error) {
	return append([]*domainsession.Session{}, m.userSessions...), nil
}
func (m *sessionRepoTestDouble) GetByToken(ctx context.Context, token string) (*domainsession.Session, error) {
	if m.session == nil {
		return nil, errors.New("not found")
	}
	return m.session, nil
}
func (m *sessionRepoTestDouble) GetByRefreshToken(ctx context.Context, refreshToken string) (*domainsession.Session, error) {
	if m.session == nil {
		return nil, errors.New("not found")
	}
	return m.session, nil
}
func (m *sessionRepoTestDouble) UpdateActivity(ctx context.Context, sessionID string) error {
	m.activityID = sessionID
	return nil
}
func (m *sessionRepoTestDouble) RotateTokens(ctx context.Context, sessionID string, accessToken string, refreshToken string, expiresAt time.Time) error {
	m.rotatedID = sessionID
	return nil
}
func (m *sessionRepoTestDouble) Delete(ctx context.Context, sessionID string) error {
	m.deletedIDs = append(m.deletedIDs, sessionID)
	return nil
}
func (m *sessionRepoTestDouble) DeleteByUserID(ctx context.Context, userID string) error {
	m.deleteByUserID = userID
	return nil
}
func (m *sessionRepoTestDouble) DeleteExpired(ctx context.Context) error { return nil }
func (m *sessionRepoTestDouble) SetExpiration(ctx context.Context, sessionID string, expiration time.Duration) error {
	return nil
}

func TestCreateSessionStoresSessionWithDerivedDeviceInfo(t *testing.T) {
	t.Setenv("REFRESH_TOKEN_EXP_HOURS", "1")
	repo := &sessionRepoTestDouble{}
	svc := NewSessionService(repo)

	session, err := svc.CreateSession(context.Background(), &domainuser.Users{
		Id:    "user-1",
		Name:  "Jane",
		Email: "jane@example.com",
		Role:  "viewer",
	}, "access", "refresh", domainsession.RequestMeta{
		IP:        "127.0.0.1",
		UserAgent: "Mozilla/5.0 (Windows NT 10.0)",
	})
	if err != nil {
		t.Fatalf("expected success, got %v", err)
	}

	if session.SessionID == "" || repo.session == nil {
		t.Fatalf("expected session to be stored, got %+v", session)
	}
	if session.DeviceInfo != "Windows PC" {
		t.Fatalf("expected derived Windows device info, got %q", session.DeviceInfo)
	}
	if !session.ExpiresAt.After(time.Now()) {
		t.Fatalf("expected future expiry, got %v", session.ExpiresAt)
	}
}

func TestExtractDeviceInfoVariants(t *testing.T) {
	tests := map[string]string{
		"Mozilla/5.0 (Linux; Android 13; Pixel)":       "Android Mobile",
		"Mozilla/5.0 (iPhone; CPU iPhone OS 17_0)":     "iOS Mobile",
		"Mozilla/5.0 (Mobile; rv:109.0)":               "Mobile Device",
		"Mozilla/5.0 (iPad; CPU OS 17_0 like Mac OS)":  "Tablet",
		"Mozilla/5.0 (Macintosh; Intel Mac OS X 14_0)": "Mac",
		"Mozilla/5.0 (X11; Linux x86_64)":              "Linux",
		"unknown":                                      "Unknown Device",
	}

	for userAgent, want := range tests {
		if got := extractDeviceInfo(userAgent); got != want {
			t.Fatalf("user agent %q: expected %q, got %q", userAgent, want, got)
		}
	}
}

func TestValidateSessionDeletesExpiredSession(t *testing.T) {
	repo := &sessionRepoTestDouble{session: &domainsession.Session{
		SessionID: "session-1",
		ExpiresAt: time.Now().Add(-time.Minute),
	}}
	svc := NewSessionService(repo)

	_, err := svc.ValidateSession(context.Background(), "access")
	if err == nil || err.Error() != "session expired" {
		t.Fatalf("expected expired session error, got %v", err)
	}
	if len(repo.deletedIDs) != 1 || repo.deletedIDs[0] != "session-1" {
		t.Fatalf("expected expired session delete, got %+v", repo.deletedIDs)
	}
}

func TestValidateSessionUpdatesActivityForValidSession(t *testing.T) {
	repo := &sessionRepoTestDouble{session: &domainsession.Session{
		SessionID: "session-1",
		ExpiresAt: time.Now().Add(time.Hour),
	}}
	svc := NewSessionService(repo)

	session, err := svc.ValidateSession(context.Background(), "access")
	if err != nil {
		t.Fatalf("expected success, got %v", err)
	}
	if session.SessionID != "session-1" || repo.activityID != "session-1" {
		t.Fatalf("expected activity update, session=%+v activity=%q", session, repo.activityID)
	}
}

func TestDestroyOtherSessionsKeepsCurrentSession(t *testing.T) {
	repo := &sessionRepoTestDouble{userSessions: []*domainsession.Session{
		{SessionID: "current"},
		{SessionID: "old-1"},
		{SessionID: "old-2"},
	}}
	svc := NewSessionService(repo)

	if err := svc.DestroyOtherSessions(context.Background(), "user-1", "current"); err != nil {
		t.Fatalf("expected success, got %v", err)
	}
	if len(repo.deletedIDs) != 2 || repo.deletedIDs[0] != "old-1" || repo.deletedIDs[1] != "old-2" {
		t.Fatalf("expected only old sessions deleted, got %+v", repo.deletedIDs)
	}
}

func TestSessionServicePassThroughMethods(t *testing.T) {
	repo := &sessionRepoTestDouble{
		session: &domainsession.Session{SessionID: "session-1", ExpiresAt: time.Now().Add(time.Hour)},
		userSessions: []*domainsession.Session{
			{SessionID: "session-1", DeviceInfo: "Browser", IP: "127.0.0.1"},
		},
	}
	svc := NewSessionService(repo)
	ctx := context.Background()

	infos, err := svc.GetUserSessions(ctx, "user-1", "session-1")
	if err != nil || len(infos) != 1 || !infos[0].IsCurrentSession {
		t.Fatalf("get user sessions: infos=%+v err=%v", infos, err)
	}
	if err := svc.DestroySession(ctx, "session-1"); err != nil {
		t.Fatalf("destroy session: %v", err)
	}
	if err := svc.DestroySessionByToken(ctx, "access"); err != nil {
		t.Fatalf("destroy by token: %v", err)
	}
	if got, err := svc.GetSessionByToken(ctx, "access"); err != nil || got.SessionID != "session-1" {
		t.Fatalf("get by token: session=%+v err=%v", got, err)
	}
	if got, err := svc.GetSessionByRefreshToken(ctx, "refresh"); err != nil || got.SessionID != "session-1" {
		t.Fatalf("get by refresh: session=%+v err=%v", got, err)
	}
	if got, err := svc.GetSessionBySessionID(ctx, "session-1"); err != nil || got.SessionID != "session-1" {
		t.Fatalf("get by session id: session=%+v err=%v", got, err)
	}
	if err := svc.RotateSessionTokens(ctx, "session-1", "access-2", "refresh-2", time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("rotate tokens: %v", err)
	}
	if repo.rotatedID != "session-1" {
		t.Fatalf("expected rotation to be delegated, got %q", repo.rotatedID)
	}
	if err := svc.DestroyAllUserSessions(ctx, "user-1"); err != nil {
		t.Fatalf("destroy all user sessions: %v", err)
	}
	if repo.deleteByUserID != "user-1" {
		t.Fatalf("expected delete by user id, got %q", repo.deleteByUserID)
	}
}
