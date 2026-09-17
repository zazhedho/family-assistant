package handlersession

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"starter-kit/internal/authscope"
	domainaudit "starter-kit/internal/domain/audit"
	domainsession "starter-kit/internal/domain/session"
	domainuser "starter-kit/internal/domain/user"
	"starter-kit/internal/dto"
	"starter-kit/pkg/filter"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

type sessionServiceHandlerTestDouble struct {
	session      *domainsession.Session
	infos        []*domainsession.SessionInfo
	destroyedID  string
	destroyOther string
	err          error
	destroyErr   error
	otherErr     error
}

func (m *sessionServiceHandlerTestDouble) CreateSession(ctx context.Context, user *domainuser.Users, accessToken string, refreshToken string, requestMeta domainsession.RequestMeta) (*domainsession.Session, error) {
	return nil, nil
}
func (m *sessionServiceHandlerTestDouble) ValidateSession(ctx context.Context, token string) (*domainsession.Session, error) {
	return nil, nil
}
func (m *sessionServiceHandlerTestDouble) GetUserSessions(ctx context.Context, userID string, currentSessionID string) ([]*domainsession.SessionInfo, error) {
	return m.infos, m.err
}
func (m *sessionServiceHandlerTestDouble) DestroySession(ctx context.Context, sessionID string) error {
	m.destroyedID = sessionID
	if m.destroyErr != nil {
		return m.destroyErr
	}
	return m.err
}
func (m *sessionServiceHandlerTestDouble) DestroySessionByToken(ctx context.Context, token string) error {
	return nil
}
func (m *sessionServiceHandlerTestDouble) DestroyAllUserSessions(ctx context.Context, userID string) error {
	return nil
}
func (m *sessionServiceHandlerTestDouble) DestroyOtherSessions(ctx context.Context, userID string, currentSessionID string) error {
	m.destroyOther = currentSessionID
	if m.otherErr != nil {
		return m.otherErr
	}
	return m.err
}
func (m *sessionServiceHandlerTestDouble) GetSessionByToken(ctx context.Context, token string) (*domainsession.Session, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.session, nil
}
func (m *sessionServiceHandlerTestDouble) GetSessionByRefreshToken(ctx context.Context, refreshToken string) (*domainsession.Session, error) {
	return nil, nil
}
func (m *sessionServiceHandlerTestDouble) GetSessionBySessionID(ctx context.Context, sessionID string) (*domainsession.Session, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.session, nil
}
func (m *sessionServiceHandlerTestDouble) RotateSessionTokens(ctx context.Context, sessionID string, accessToken string, refreshToken string, expiresAt time.Time) error {
	return nil
}

type auditServiceSessionTestDouble struct{}

func (m *auditServiceSessionTestDouble) Store(ctx context.Context, req domainaudit.AuditEvent) error {
	return nil
}
func (m *auditServiceSessionTestDouble) GetAll(ctx context.Context, params filter.BaseParams) ([]dto.AuditTrailResponse, int64, error) {
	return nil, 0, nil
}
func (m *auditServiceSessionTestDouble) GetByID(ctx context.Context, id string) (dto.AuditTrailResponse, error) {
	return dto.AuditTrailResponse{}, nil
}

func performSessionRequest(method, routePath, requestPath string, handler gin.HandlerFunc, scope authscope.Scope) *httptest.ResponseRecorder {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Handle(method, routePath, func(ctx *gin.Context) {
		ctx.Set("token", "access-token")
		if scope.UserID != "" {
			ctx.Request = ctx.Request.WithContext(authscope.WithContext(ctx.Request.Context(), scope))
		}
		handler(ctx)
	})
	req := httptest.NewRequest(method, requestPath, nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

func TestGetActiveSessionsRequiresAuthAndReturnsSessions(t *testing.T) {
	handler := NewSessionHandler(&sessionServiceHandlerTestDouble{}, &auditServiceSessionTestDouble{})
	rec := performSessionRequest(http.MethodGet, "/sessions", "/sessions", handler.GetActiveSessions, authscope.Scope{})
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rec.Code)
	}

	handler = NewSessionHandler(&sessionServiceHandlerTestDouble{
		session: &domainsession.Session{SessionID: "current"},
		infos:   []*domainsession.SessionInfo{{SessionID: "current", IsCurrentSession: true}},
	}, &auditServiceSessionTestDouble{})
	rec = performSessionRequest(http.MethodGet, "/sessions", "/sessions", handler.GetActiveSessions, authscope.New("user-1", "Jane", "viewer", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	handler = NewSessionHandler(&sessionServiceHandlerTestDouble{
		session: &domainsession.Session{SessionID: "current"},
		err:     errors.New("database down"),
	}, &auditServiceSessionTestDouble{})
	rec = performSessionRequest(http.MethodGet, "/sessions", "/sessions", handler.GetActiveSessions, authscope.New("user-1", "Jane", "viewer", nil))
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestRevokeSessionValidatesOwnership(t *testing.T) {
	handler := NewSessionHandler(&sessionServiceHandlerTestDouble{}, &auditServiceSessionTestDouble{})
	rec := performSessionRequest(http.MethodDelete, "/session/:session_id", "/session/session-1", handler.RevokeSession, authscope.Scope{})
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d: %s", rec.Code, rec.Body.String())
	}

	handler = NewSessionHandler(&sessionServiceHandlerTestDouble{
		session: &domainsession.Session{SessionID: "session-1", UserID: "other-user"},
	}, &auditServiceSessionTestDouble{})
	rec = performSessionRequest(http.MethodDelete, "/session/:session_id", "/session/session-1", handler.RevokeSession, authscope.New("user-1", "Jane", "viewer", nil))
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d: %s", rec.Code, rec.Body.String())
	}

	handler = NewSessionHandler(&sessionServiceHandlerTestDouble{err: errors.New("not found")}, &auditServiceSessionTestDouble{})
	rec = performSessionRequest(http.MethodDelete, "/session/:session_id", "/session/session-1", handler.RevokeSession, authscope.New("user-1", "Jane", "viewer", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestRevokeSessionDeletesOwnedSession(t *testing.T) {
	service := &sessionServiceHandlerTestDouble{session: &domainsession.Session{SessionID: "session-1", UserID: "user-1"}}
	handler := NewSessionHandler(service, &auditServiceSessionTestDouble{})
	rec := performSessionRequest(http.MethodDelete, "/session/:session_id", "/session/session-1", handler.RevokeSession, authscope.New("user-1", "Jane", "viewer", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if service.destroyedID != "session-1" {
		t.Fatalf("expected session delete, got %q", service.destroyedID)
	}

	service = &sessionServiceHandlerTestDouble{
		session:    &domainsession.Session{SessionID: "session-1", UserID: "user-1"},
		destroyErr: errors.New("redis down"),
	}
	handler = NewSessionHandler(service, &auditServiceSessionTestDouble{})
	rec = performSessionRequest(http.MethodDelete, "/session/:session_id", "/session/session-1", handler.RevokeSession, authscope.New("user-1", "Jane", "viewer", nil))
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestRevokeAllOtherSessionsHandlesCurrentSessionLookup(t *testing.T) {
	handler := NewSessionHandler(&sessionServiceHandlerTestDouble{}, &auditServiceSessionTestDouble{})
	rec := performSessionRequest(http.MethodPost, "/sessions/revoke-others", "/sessions/revoke-others", handler.RevokeAllOtherSessions, authscope.Scope{})
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rec.Code)
	}

	handler = NewSessionHandler(&sessionServiceHandlerTestDouble{err: errors.New("not found")}, &auditServiceSessionTestDouble{})
	rec = performSessionRequest(http.MethodPost, "/sessions/revoke-others", "/sessions/revoke-others", handler.RevokeAllOtherSessions, authscope.New("user-1", "Jane", "viewer", nil))
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", rec.Code)
	}

	service := &sessionServiceHandlerTestDouble{session: &domainsession.Session{SessionID: "current", UserID: "user-1"}}
	handler = NewSessionHandler(service, &auditServiceSessionTestDouble{})
	rec = performSessionRequest(http.MethodPost, "/sessions/revoke-others", "/sessions/revoke-others", handler.RevokeAllOtherSessions, authscope.New("user-1", "Jane", "viewer", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if service.destroyOther != "current" {
		t.Fatalf("expected current session id, got %q", service.destroyOther)
	}

	service = &sessionServiceHandlerTestDouble{
		session:  &domainsession.Session{SessionID: "current", UserID: "user-1"},
		otherErr: errors.New("redis down"),
	}
	handler = NewSessionHandler(service, &auditServiceSessionTestDouble{})
	rec = performSessionRequest(http.MethodPost, "/sessions/revoke-others", "/sessions/revoke-others", handler.RevokeAllOtherSessions, authscope.New("user-1", "Jane", "viewer", nil))
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected destroy others 500, got %d: %s", rec.Code, rec.Body.String())
	}
}
