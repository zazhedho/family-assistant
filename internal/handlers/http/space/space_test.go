package handlerspace

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"family-assistant/internal/authscope"
	domainaudit "family-assistant/internal/domain/audit"
	domainspace "family-assistant/internal/domain/space"
	"family-assistant/internal/dto"
	serviceauthorization "family-assistant/internal/services/authorization"
	servicespace "family-assistant/internal/services/space"
	"family-assistant/pkg/filter"

	"github.com/gin-gonic/gin"
)

type spaceServiceStub struct {
	spaces       []domainspace.ResolvedMembership
	members      []domainspace.ResolvedMembership
	space        *domainspace.Space
	listErr      error
	createErr    error
	membersErr   error
	createUserID string
	createInput  servicespace.CreateInput
	listUserID   string
	membersUser  string
	membersSpace string
	listCalls    int
	createCalls  int
	membersCalls int
}

type spaceAuditServiceStub struct {
	events []domainaudit.AuditEvent
}

func (s *spaceAuditServiceStub) Store(_ context.Context, event domainaudit.AuditEvent) error {
	s.events = append(s.events, event)
	return nil
}

func (s *spaceAuditServiceStub) GetAll(context.Context, filter.BaseParams) ([]dto.AuditTrailResponse, int64, error) {
	return nil, 0, nil
}

func (s *spaceAuditServiceStub) GetByID(context.Context, string) (dto.AuditTrailResponse, error) {
	return dto.AuditTrailResponse{}, nil
}

func (s *spaceServiceStub) List(_ context.Context, userID string) ([]domainspace.ResolvedMembership, error) {
	s.listCalls++
	s.listUserID = userID
	return s.spaces, s.listErr
}

func (s *spaceServiceStub) Create(_ context.Context, userID string, input servicespace.CreateInput) (*domainspace.Space, error) {
	s.createCalls++
	s.createUserID = userID
	s.createInput = input
	return s.space, s.createErr
}

func (s *spaceServiceStub) Members(_ context.Context, userID, spaceID string) ([]domainspace.ResolvedMembership, error) {
	s.membersCalls++
	s.membersUser, s.membersSpace = userID, spaceID
	return s.members, s.membersErr
}

func performSpaceRequest(method, routePath, requestPath, body string, scope authscope.Scope, handler gin.HandlerFunc) *httptest.ResponseRecorder {
	return performSpaceRequestWithMeta(method, routePath, requestPath, body, scope, "", "", handler)
}

func performSpaceRequestWithMeta(method, routePath, requestPath, body string, scope authscope.Scope, requestID, userAgent string, handler gin.HandlerFunc) *httptest.ResponseRecorder {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Handle(method, routePath, func(ctx *gin.Context) {
		if scope.UserID != "" {
			ctx.Request = ctx.Request.WithContext(authscope.WithContext(ctx.Request.Context(), scope))
		}
		handler(ctx)
	})
	req := httptest.NewRequest(method, requestPath, bytes.NewBufferString(body))
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

func decodeSpaceResponse(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var payload map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v; body=%s", err, rec.Body.String())
	}
	return payload
}

func TestSpaceHandlersUseAuthScopeAndResponseEnvelope(t *testing.T) {
	service := &spaceServiceStub{
		spaces: []domainspace.ResolvedMembership{{SpaceID: "00000000-0000-0000-0000-000000000001", UserID: "user-1"}},
		space:  &domainspace.Space{ID: "00000000-0000-0000-0000-000000000002", Name: "Family", Type: domainspace.TypeShared, Category: domainspace.CategoryFamily, Status: domainspace.StatusActive},
	}
	h := NewSpaceHandler(service, &spaceAuditServiceStub{})
	scope := authscope.New("user-1", "Jane", "viewer", nil)

	rec := performSpaceRequest(http.MethodGet, "/api/spaces", "/api/spaces", "", scope, h.List)
	if rec.Code != http.StatusOK {
		t.Fatalf("list status = %d: %s", rec.Code, rec.Body.String())
	}
	payload := decodeSpaceResponse(t, rec)
	if payload["status"] != true || service.listUserID != "user-1" {
		t.Fatalf("unexpected list response=%v user=%q", payload, service.listUserID)
	}

	rec = performSpaceRequest(http.MethodPost, "/api/spaces", "/api/spaces", `{"name":" Family ","category":"family"}`, scope, h.Create)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create status = %d: %s", rec.Code, rec.Body.String())
	}
	if service.createUserID != "user-1" || service.createInput.Name != " Family " || service.createInput.Category != "family" {
		t.Fatalf("unexpected create input: user=%q input=%+v", service.createUserID, service.createInput)
	}
	payload = decodeSpaceResponse(t, rec)
	if payload["status"] != true {
		t.Fatalf("unexpected create envelope=%v", payload)
	}
}

func TestSpaceMembersHandlerValidatesUUIDAndPassesAuthenticatedUser(t *testing.T) {
	service := &spaceServiceStub{members: []domainspace.ResolvedMembership{{SpaceID: "00000000-0000-0000-0000-000000000001", UserID: "user-1"}}}
	h := NewSpaceHandler(service, &spaceAuditServiceStub{})
	scope := authscope.New("user-1", "Jane", "viewer", nil)

	rec := performSpaceRequest(http.MethodGet, "/api/spaces/:space_id/members", "/api/spaces/00000000-0000-0000-0000-000000000001/members", "", scope, h.Members)
	if rec.Code != http.StatusOK || service.membersUser != "user-1" || service.membersSpace != "00000000-0000-0000-0000-000000000001" {
		t.Fatalf("unexpected members response: code=%d body=%s user=%q space=%q", rec.Code, rec.Body.String(), service.membersUser, service.membersSpace)
	}

	rec = performSpaceRequest(http.MethodGet, "/api/spaces/:space_id/members", "/api/spaces/not-a-uuid/members", "", scope, h.Members)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("invalid UUID status = %d: %s", rec.Code, rec.Body.String())
	}
}

func TestSpaceCreateRejectsUnknownAndTrailingJSON(t *testing.T) {
	service := &spaceServiceStub{space: &domainspace.Space{ID: "00000000-0000-0000-0000-000000000001"}}
	h := NewSpaceHandler(service, &spaceAuditServiceStub{})
	scope := authscope.New("user-1", "Jane", "viewer", nil)

	for _, body := range []string{`{"name":"Family","category":"family","extra":true}`, `{"name":"Family","category":"family"}{"name":"Other"}`} {
		rec := performSpaceRequest(http.MethodPost, "/api/spaces", "/api/spaces", body, scope, h.Create)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("invalid JSON status = %d: %s", rec.Code, rec.Body.String())
		}
	}
	if service.createCalls != 0 {
		t.Fatalf("service called for invalid JSON %d times", service.createCalls)
	}
}

func TestSpaceHandlersMapServiceErrors(t *testing.T) {
	scope := authscope.New("user-1", "Jane", "viewer", nil)
	tests := []struct {
		name string
		err  error
		want int
		call func(*SpaceHandler, *httptest.ResponseRecorder)
	}{
		{name: "forbidden", err: servicespace.ErrForbidden, want: http.StatusForbidden, call: func(h *SpaceHandler, rec *httptest.ResponseRecorder) {
			*rec = *performSpaceRequest(http.MethodGet, "/api/spaces", "/api/spaces", "", scope, h.List)
		}},
		{name: "not found", err: servicespace.ErrNotFound, want: http.StatusNotFound, call: func(h *SpaceHandler, rec *httptest.ResponseRecorder) {
			*rec = *performSpaceRequest(http.MethodGet, "/api/spaces/:space_id/members", "/api/spaces/00000000-0000-0000-0000-000000000001/members", "", scope, h.Members)
		}},
		{name: "validation", err: &serviceauthorization.ValidationError{Field: "category", Reason: "is invalid"}, want: http.StatusBadRequest, call: func(h *SpaceHandler, rec *httptest.ResponseRecorder) {
			*rec = *performSpaceRequest(http.MethodPost, "/api/spaces", "/api/spaces", `{"name":"Mine","category":"personal"}`, scope, h.Create)
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			service := &spaceServiceStub{listErr: tt.err, createErr: tt.err, membersErr: tt.err}
			h := NewSpaceHandler(service, &spaceAuditServiceStub{})
			var rec httptest.ResponseRecorder
			tt.call(h, &rec)
			if rec.Code != tt.want {
				t.Fatalf("status = %d, want %d: %s", rec.Code, tt.want, rec.Body.String())
			}
		})
	}
}

func TestSpaceHandlersRequireAuthentication(t *testing.T) {
	h := NewSpaceHandler(&spaceServiceStub{}, &spaceAuditServiceStub{})
	for _, tt := range []struct {
		name string
		call gin.HandlerFunc
	}{
		{name: "list", call: h.List},
		{name: "create", call: h.Create},
		{name: "members", call: h.Members},
	} {
		t.Run(tt.name, func(t *testing.T) {
			rec := performSpaceRequest(http.MethodGet, "/api/spaces/:space_id/members", "/api/spaces/00000000-0000-0000-0000-000000000001/members", `{"name":"x","category":"family"}`, authscope.Scope{}, tt.call)
			if rec.Code != http.StatusUnauthorized {
				t.Fatalf("status = %d, want 401: %s", rec.Code, rec.Body.String())
			}
		})
	}
}

func TestSpaceServiceStubSatisfiesService(t *testing.T) {
	var _ servicespace.Service = (*spaceServiceStub)(nil)
}

func TestSpaceHandlerAuditsEarlyFailuresWithHTTPProvenance(t *testing.T) {
	scope := authscope.New("user-1", "Jane", "viewer", nil)
	tests := []struct {
		name string
		want int
		call func(*SpaceHandler) *httptest.ResponseRecorder
	}{
		{name: "unauthenticated", want: http.StatusUnauthorized, call: func(h *SpaceHandler) *httptest.ResponseRecorder {
			return performSpaceRequestWithMeta(http.MethodGet, "/api/spaces", "/api/spaces", "", authscope.Scope{}, "00000000-0000-0000-0000-000000000091", "space-agent", h.List)
		}},
		{name: "invalid json", want: http.StatusBadRequest, call: func(h *SpaceHandler) *httptest.ResponseRecorder {
			return performSpaceRequestWithMeta(http.MethodPost, "/api/spaces", "/api/spaces", `{"name":"Family","secret":"raw-body"}`, scope, "00000000-0000-0000-0000-000000000092", "space-agent", h.Create)
		}},
		{name: "invalid uuid", want: http.StatusBadRequest, call: func(h *SpaceHandler) *httptest.ResponseRecorder {
			return performSpaceRequestWithMeta(http.MethodGet, "/api/spaces/:space_id/members", "/api/spaces/not-a-uuid/members", "", scope, "00000000-0000-0000-0000-000000000093", "space-agent", h.Members)
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			audit := &spaceAuditServiceStub{}
			h := NewSpaceHandler(&spaceServiceStub{}, audit)
			rec := tt.call(h)
			if rec.Code != tt.want {
				t.Fatalf("status = %d, want %d: %s", rec.Code, tt.want, rec.Body.String())
			}
			if len(audit.events) != 1 {
				t.Fatalf("expected one early-outcome audit, got %+v", audit.events)
			}
			event := audit.events[0]
			if event.Status != domainaudit.StatusFailed || event.Source != "http" || event.RequestID == "" || event.UserAgent != "space-agent" || event.IPAddress == "" || event.AfterData != nil || event.BeforeData != nil {
				t.Fatalf("unexpected HTTP audit provenance: %+v", event)
			}
		})
	}
}
