package handleridentity

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
	domainidentity "family-assistant/internal/domain/identity"
	"family-assistant/internal/dto"
	serviceidentity "family-assistant/internal/services/identity"
	"family-assistant/pkg/filter"

	"github.com/gin-gonic/gin"
)

type identityServiceStub struct {
	code       string
	issueErr   error
	identity   *domainidentity.ExternalIdentity
	linkErr    error
	revokeErr  error
	issueUser  string
	issueProv  string
	revokeUser string
	revokeProv string
	revokeID   string
}

func (s *identityServiceStub) Issue(_ context.Context, userID, provider string) (string, error) {
	s.issueUser, s.issueProv = userID, provider
	return s.code, s.issueErr
}
func (s *identityServiceStub) Link(context.Context, string, string, string) (*domainidentity.ExternalIdentity, error) {
	return s.identity, s.linkErr
}
func (s *identityServiceStub) Revoke(_ context.Context, userID, provider, externalID string) error {
	s.revokeUser, s.revokeProv, s.revokeID = userID, provider, externalID
	return s.revokeErr
}

type identityAuditStub struct{ events []domainaudit.AuditEvent }

func (s *identityAuditStub) Store(_ context.Context, event domainaudit.AuditEvent) error {
	s.events = append(s.events, event)
	return nil
}
func (s *identityAuditStub) GetAll(context.Context, filter.BaseParams) ([]dto.AuditTrailResponse, int64, error) {
	return nil, 0, nil
}
func (s *identityAuditStub) GetByID(context.Context, string) (dto.AuditTrailResponse, error) {
	return dto.AuditTrailResponse{}, nil
}

func performIdentityRequest(method, route, path, body string, scope authscope.Scope, handler gin.HandlerFunc) *httptest.ResponseRecorder {
	return performIdentityRequestWithMeta(method, route, path, body, scope, "", "", handler)
}

func performIdentityRequestWithMeta(method, route, path, body string, scope authscope.Scope, requestID, userAgent string, handler gin.HandlerFunc) *httptest.ResponseRecorder {
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

func decodeIdentityResponse(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var payload map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v; body=%s", err, rec.Body.String())
	}
	return payload
}

func TestIssueReturnsRawCodeOnlyAndUsesAuthenticatedUser(t *testing.T) {
	service := &identityServiceStub{code: "raw-link-code"}
	h := NewIdentityHandler(service)
	rec := performIdentityRequest(http.MethodPost, "/api/hermes/link-codes", "/api/hermes/link-codes", "", authscope.New("user-1", "Jane", "user", nil), h.Issue)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
	if service.issueUser != "user-1" || service.issueProv != domainidentity.ProviderHermes {
		t.Fatalf("unexpected issue args: user=%q provider=%q", service.issueUser, service.issueProv)
	}
	payload := decodeIdentityResponse(t, rec)
	data, ok := payload["data"].(map[string]any)
	if !ok || data["code"] != "raw-link-code" || len(data) != 1 {
		t.Fatalf("unexpected issue response: %#v", payload)
	}
}

func TestRevokeRequiresStrictExternalIDBodyAndAuthenticatedUser(t *testing.T) {
	service := &identityServiceStub{}
	h := NewIdentityHandler(service)
	rec := performIdentityRequest(http.MethodDelete, "/api/hermes/link", "/api/hermes/link", `{"external_id":"profile-a"}`, authscope.New("user-1", "Jane", "user", nil), h.Revoke)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
	if service.revokeUser != "user-1" || service.revokeProv != domainidentity.ProviderHermes || service.revokeID != "profile-a" {
		t.Fatalf("unexpected revoke args: user=%q provider=%q external=%q", service.revokeUser, service.revokeProv, service.revokeID)
	}
}

func TestIdentityHandlerRejectsUnknownOrTrailingJSON(t *testing.T) {
	h := NewIdentityHandler(&identityServiceStub{})
	for _, body := range []string{`{"external_id":"profile-a","token":"secret"}`, `{"external_id":"profile-a"}{"extra":true}`} {
		rec := performIdentityRequest(http.MethodDelete, "/api/hermes/link", "/api/hermes/link", body, authscope.New("user-1", "Jane", "user", nil), h.Revoke)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400: %s", rec.Code, rec.Body.String())
		}
	}
}

func TestIdentityHandlerMapsSafeErrorsAndRequiresAuthentication(t *testing.T) {
	for _, tt := range []struct {
		name string
		err  error
		want int
	}{
		{name: "invalid token", err: domainidentity.ErrInvalidLinkToken, want: http.StatusBadRequest},
		{name: "conflict", err: domainidentity.ErrIdentityConflict, want: http.StatusConflict},
		{name: "missing identity", err: domainidentity.ErrIdentityNotFound, want: http.StatusNotFound},
		{name: "internal", err: errors.New("database down"), want: http.StatusInternalServerError},
	} {
		t.Run(tt.name, func(t *testing.T) {
			h := NewIdentityHandler(&identityServiceStub{revokeErr: tt.err})
			rec := performIdentityRequest(http.MethodDelete, "/api/hermes/link", "/api/hermes/link", `{"external_id":"profile-a"}`, authscope.New("user-1", "Jane", "user", nil), h.Revoke)
			if rec.Code != tt.want {
				t.Fatalf("status = %d, want %d: %s", rec.Code, tt.want, rec.Body.String())
			}
		})
	}
	rec := performIdentityRequest(http.MethodPost, "/api/hermes/link-codes", "/api/hermes/link-codes", "", authscope.Scope{}, hIssueForTest())
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated issue = %d, want 401: %s", rec.Code, rec.Body.String())
	}
}

func hIssueForTest() gin.HandlerFunc {
	h := NewIdentityHandler(&identityServiceStub{code: "code"})
	return h.Issue
}

func TestIdentityHandlerAuditsEarlyFailureWithoutBodyOrSecrets(t *testing.T) {
	audit := &identityAuditStub{}
	h := NewIdentityHandler(&identityServiceStub{}, audit)
	rec := performIdentityRequestWithMeta(http.MethodDelete, "/api/hermes/link", "/api/hermes/link", `{"external_id":"profile-a","token":"bearer-secret"}`, authscope.New("user-1", "Jane", "user", nil), "request-1", "identity-agent", h.Revoke)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400: %s", rec.Code, rec.Body.String())
	}
	if len(audit.events) != 1 {
		t.Fatalf("audit events = %#v", audit.events)
	}
	event := audit.events[0]
	if event.Status != domainaudit.StatusFailed || event.Source != "http" || event.RequestID == "" || event.UserAgent != "identity-agent" || event.BeforeData != nil || event.AfterData != nil {
		t.Fatalf("unexpected audit: %#v", event)
	}
	encoded, _ := json.Marshal(event)
	if strings.Contains(string(encoded), "bearer-secret") || strings.Contains(string(encoded), "request_body") {
		t.Fatalf("secret/body leaked into audit: %s", encoded)
	}
}

func TestIdentityServiceStubSatisfiesInterface(t *testing.T) {
	var _ serviceidentity.LinkService = (*identityServiceStub)(nil)
}
