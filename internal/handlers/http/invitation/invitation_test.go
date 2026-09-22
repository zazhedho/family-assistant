package handlerinvitation

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"family-assistant/internal/authscope"
	domainaudit "family-assistant/internal/domain/audit"
	domaininvitation "family-assistant/internal/domain/invitation"
	domainspace "family-assistant/internal/domain/space"
	domainuser "family-assistant/internal/domain/user"
	"family-assistant/internal/dto"
	interfaceinvitation "family-assistant/internal/interfaces/invitation"
	serviceinvitation "family-assistant/internal/services/invitation"
	"family-assistant/pkg/filter"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type invitationServiceStub struct {
	created     *domaininvitation.Invitation
	rawToken    string
	createErr   error
	accepted    *domainspace.Member
	acceptErr   error
	createUser  string
	createInput dto.InvitationCreateInput
	acceptToken string
	acceptUser  domainuser.Users
	acceptErrs  map[string]error
}

func (s *invitationServiceStub) Create(_ context.Context, userID string, input dto.InvitationCreateInput) (*domaininvitation.Invitation, string, error) {
	s.createUser, s.createInput = userID, input
	return s.created, s.rawToken, s.createErr
}
func (s *invitationServiceStub) Accept(_ context.Context, token string, user domainuser.Users) (*domainspace.Member, error) {
	s.acceptToken, s.acceptUser = token, user
	if err := s.acceptErrs[token]; err != nil {
		return nil, err
	}
	return s.accepted, s.acceptErr
}
func (s *invitationServiceStub) List(context.Context, string, string) ([]domaininvitation.Invitation, error) {
	return nil, nil
}
func (s *invitationServiceStub) Revoke(context.Context, string, string, string) (*domaininvitation.Invitation, error) {
	return nil, nil
}

type invitationUserRepositoryStub struct {
	user       domainuser.Users
	err        error
	lookupUser string
}

func (s *invitationUserRepositoryStub) GetByID(_ context.Context, userID string) (domainuser.Users, error) {
	s.lookupUser = userID
	return s.user, s.err
}
func (s *invitationUserRepositoryStub) GetAll(context.Context, filter.BaseParams) ([]domainuser.Users, int64, error) {
	return nil, 0, nil
}
func (s *invitationUserRepositoryStub) Store(context.Context, domainuser.Users) error { return nil }
func (s *invitationUserRepositoryStub) StoreWithPersonalSpace(context.Context, domainuser.Users, domainspace.Space, domainspace.Member) error {
	return nil
}
func (s *invitationUserRepositoryStub) Update(context.Context, domainuser.Users) error   { return nil }
func (s *invitationUserRepositoryStub) Delete(context.Context, string) error             { return nil }
func (s *invitationUserRepositoryStub) SoftDelete(context.Context, string, string) error { return nil }
func (s *invitationUserRepositoryStub) GetByEmail(context.Context, string) (domainuser.Users, error) {
	return domainuser.Users{}, gorm.ErrRecordNotFound
}
func (s *invitationUserRepositoryStub) GetByPhone(context.Context, string) (domainuser.Users, error) {
	return domainuser.Users{}, gorm.ErrRecordNotFound
}

type invitationAuditServiceStub struct {
	events []domainaudit.AuditEvent
}

func (s *invitationAuditServiceStub) Store(_ context.Context, event domainaudit.AuditEvent) error {
	s.events = append(s.events, event)
	return nil
}
func (s *invitationAuditServiceStub) GetAll(context.Context, filter.BaseParams) ([]dto.AuditTrailResponse, int64, error) {
	return nil, 0, nil
}
func (s *invitationAuditServiceStub) GetByID(context.Context, string) (dto.AuditTrailResponse, error) {
	return dto.AuditTrailResponse{}, nil
}

func performInvitationRequest(method, route, path, body string, scope authscope.Scope, handler gin.HandlerFunc) *httptest.ResponseRecorder {
	return performInvitationRequestWithMeta(method, route, path, body, scope, "", "", handler)
}

func performInvitationRequestWithMeta(method, route, path, body string, scope authscope.Scope, requestID, userAgent string, handler gin.HandlerFunc) *httptest.ResponseRecorder {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Handle(method, route, func(ctx *gin.Context) {
		ctx.Request = ctx.Request.WithContext(authscope.WithContext(ctx.Request.Context(), scope))
		handler(ctx)
	})
	req := httptest.NewRequest(method, path, bytes.NewBufferString(body))
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if requestID != "" {
		req.Header.Set("X-Request-ID", requestID)
	}
	if userAgent != "" {
		req.Header.Set("User-Agent", userAgent)
	}
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

func decodeInvitationResponse(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var payload map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v; body=%s", err, rec.Body.String())
	}
	return payload
}

func TestCreateInvitationUsesAuthenticatedUserAndReturnsRawToken(t *testing.T) {
	service := &invitationServiceStub{created: &domaininvitation.Invitation{ID: "invitation-1", SpaceID: "space-1", InvitedEmail: "jane@example.com"}, rawToken: "raw-token"}
	h := NewInvitationHandler(service, &invitationUserRepositoryStub{}, &invitationAuditServiceStub{})
	scope := authscope.New("user-1", "Owner", "space_owner", nil)

	rec := performInvitationRequest(http.MethodPost, "/api/spaces/:space_id/invitations", "/api/spaces/00000000-0000-0000-0000-000000000001/invitations", `{"invited_email":"Jane@Example.COM","role_name":"space_member"}`, scope, h.Create)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
	if service.createUser != "user-1" || service.createInput.SpaceID == "" || service.createInput.InvitedEmail != "Jane@Example.COM" || service.createInput.RoleName != "space_member" {
		t.Fatalf("unexpected service input: user=%q input=%+v", service.createUser, service.createInput)
	}
	payload := decodeInvitationResponse(t, rec)
	data, ok := payload["data"].(map[string]any)
	if !ok || data["token"] != "raw-token" {
		t.Fatalf("raw token missing from response: %#v", payload)
	}
}

func TestAcceptInvitationUsesAuthenticatedUserAndBodyToken(t *testing.T) {
	service := &invitationServiceStub{accepted: &domainspace.Member{ID: "member-2", SpaceID: "space-1", UserID: "user-2"}}
	users := &invitationUserRepositoryStub{user: domainuser.Users{Id: "user-2", Email: "jane@example.com"}}
	h := NewInvitationHandler(service, users, &invitationAuditServiceStub{})
	scope := authscope.New("user-2", "Jane", "viewer", nil)

	rec := performInvitationRequest(http.MethodPost, "/api/invitations/accept", "/api/invitations/accept", `{"token":"raw-token"}`, scope, h.Accept)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
	if users.lookupUser != "user-2" || service.acceptToken != "raw-token" || service.acceptUser.Id != "user-2" {
		t.Fatalf("unexpected accept args: token=%q user=%+v", service.acceptToken, service.acceptUser)
	}
}

func TestAcceptInvitationMapsAcceptedReplayAndExpirySafely(t *testing.T) {
	service := &invitationServiceStub{
		accepted: &domainspace.Member{ID: "member-2", SpaceID: "space-1", UserID: "user-2"},
		acceptErrs: map[string]error{
			"replayed": serviceinvitation.ErrInvalidInvitation,
			"expired":  serviceinvitation.ErrInvalidInvitation,
			"guessed":  serviceinvitation.ErrInvalidInvitation,
		},
	}
	users := &invitationUserRepositoryStub{user: domainuser.Users{Id: "user-2", Email: "jane@example.com"}}
	h := NewInvitationHandler(service, users, &invitationAuditServiceStub{})
	scope := authscope.New("user-2", "Jane", "viewer", nil)

	for _, tt := range []struct {
		token string
		want  int
	}{
		{token: "valid", want: http.StatusOK},
		{token: "replayed", want: http.StatusBadRequest},
		{token: "expired", want: http.StatusBadRequest},
		{token: "guessed", want: http.StatusBadRequest},
	} {
		t.Run(tt.token, func(t *testing.T) {
			rec := performInvitationRequest(http.MethodPost, "/api/invitations/accept", "/api/invitations/accept", `{"token":"`+tt.token+`"}`, scope, h.Accept)
			if rec.Code != tt.want {
				t.Fatalf("status = %d, want %d: %s", rec.Code, tt.want, rec.Body.String())
			}
		})
	}
}

func TestAcceptInvitationMapsExistingMembershipToConflict(t *testing.T) {
	h := NewInvitationHandler(
		&invitationServiceStub{acceptErr: serviceinvitation.ErrMembershipConflict},
		&invitationUserRepositoryStub{user: domainuser.Users{Id: "user-2", Email: "jane@example.com"}},
		&invitationAuditServiceStub{},
	)
	rec := performInvitationRequest(http.MethodPost, "/api/invitations/accept", "/api/invitations/accept", `{"token":"raw-token"}`, authscope.New("user-2", "Jane", "viewer", nil), h.Accept)
	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409: %s", rec.Code, rec.Body.String())
	}
}

func TestInvitationHandlersRejectUnknownOrTrailingJSON(t *testing.T) {
	service := &invitationServiceStub{created: &domaininvitation.Invitation{ID: "invitation-1"}, rawToken: "raw-token"}
	h := NewInvitationHandler(service, &invitationUserRepositoryStub{}, &invitationAuditServiceStub{})
	scope := authscope.New("user-1", "Owner", "space_owner", nil)
	for _, body := range []string{`{"invited_email":"jane@example.com","role_name":"space_member","token":"bad"}`, `{"invited_email":"jane@example.com","role_name":"space_member"}{"extra":true}`} {
		rec := performInvitationRequest(http.MethodPost, "/api/spaces/:space_id/invitations", "/api/spaces/00000000-0000-0000-0000-000000000001/invitations", body, scope, h.Create)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400: %s", rec.Code, rec.Body.String())
		}
	}
}

func TestInvitationHandlersMapSafeErrorsAndRequireAuthentication(t *testing.T) {
	h := NewInvitationHandler(&invitationServiceStub{createErr: serviceinvitation.ErrInvalidInvitation}, &invitationUserRepositoryStub{}, &invitationAuditServiceStub{})
	validScope := authscope.New("user-1", "Owner", "space_owner", nil)
	rec := performInvitationRequest(http.MethodPost, "/api/spaces/:space_id/invitations", "/api/spaces/00000000-0000-0000-0000-000000000001/invitations", `{"role_name":"space_member"}`, validScope, h.Create)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("invalid invitation status = %d: %s", rec.Code, rec.Body.String())
	}
	rec = performInvitationRequest(http.MethodPost, "/api/invitations/accept", "/api/invitations/accept", `{"token":"raw-token"}`, authscope.Scope{}, h.Accept)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated status = %d: %s", rec.Code, rec.Body.String())
	}
}

func TestInvitationHandlerAuditsEarlyFailureWithHTTPProvenanceAndNoBody(t *testing.T) {
	audit := &invitationAuditServiceStub{}
	h := NewInvitationHandler(&invitationServiceStub{}, &invitationUserRepositoryStub{}, audit)
	rec := performInvitationRequestWithMeta(http.MethodPost, "/api/invitations/accept", "/api/invitations/accept", `{"token":"raw-secret-token","extra":"secret"}`, authscope.New("user-2", "Jane", "viewer", nil), "request-1", "invite-agent", h.Accept)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400: %s", rec.Code, rec.Body.String())
	}
	if len(audit.events) != 1 {
		t.Fatalf("audit events = %+v", audit.events)
	}
	event := audit.events[0]
	if event.Status != domainaudit.StatusFailed || event.Source != "http" || event.RequestID == "" || event.UserAgent != "invite-agent" || event.BeforeData != nil || event.AfterData != nil {
		t.Fatalf("unexpected early audit: %+v", event)
	}
	encoded, _ := json.Marshal(event)
	if strings.Contains(string(encoded), "raw-secret-token") || strings.Contains(string(encoded), "request_body") {
		t.Fatalf("request body leaked into audit: %s", encoded)
	}
}

func TestInvitationServiceStubSatisfiesService(t *testing.T) {
	var _ interfaceinvitation.ServiceInvitationInterface = (*invitationServiceStub)(nil)
	_ = errors.New
}
