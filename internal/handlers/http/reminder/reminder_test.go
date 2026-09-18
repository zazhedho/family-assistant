package handlerreminder

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/zazhedho/family-assistant/internal/authscope"
	domainreminder "github.com/zazhedho/family-assistant/internal/domain/reminder"
	serviceauthorization "github.com/zazhedho/family-assistant/internal/services/authorization"
	serviceidentity "github.com/zazhedho/family-assistant/internal/services/identity"
	servicereminder "github.com/zazhedho/family-assistant/internal/services/reminder"
)

const httpReminderID = "00000000-0000-0000-0000-000000000101"

type reminderHTTPServiceStub struct {
	createActor   serviceidentity.ActorContext
	createInput   servicereminder.CreateInput
	listActor     serviceidentity.ActorContext
	listInput     servicereminder.ListInput
	completeActor serviceidentity.ActorContext
	completeID    string
	created       *domainreminder.Reminder
	listed        []domainreminder.Reminder
	completed     *domainreminder.Reminder
	createErr     error
	listErr       error
	completeErr   error
}

func (s *reminderHTTPServiceStub) Create(_ context.Context, actor serviceidentity.ActorContext, input servicereminder.CreateInput) (*domainreminder.Reminder, error) {
	s.createActor, s.createInput = actor, input
	if s.createErr != nil {
		return nil, s.createErr
	}
	if s.created != nil {
		return s.created, nil
	}
	return &domainreminder.Reminder{ID: httpReminderID, Title: input.Title, Scope: input.Scope, Status: domainreminder.StatusPending, ScheduledAt: input.ScheduledAt}, nil
}
func (s *reminderHTTPServiceStub) List(_ context.Context, actor serviceidentity.ActorContext, input servicereminder.ListInput) ([]domainreminder.Reminder, error) {
	s.listActor, s.listInput = actor, input
	if s.listErr != nil {
		return nil, s.listErr
	}
	return s.listed, nil
}
func (s *reminderHTTPServiceStub) Complete(_ context.Context, actor serviceidentity.ActorContext, id string) (*domainreminder.Reminder, error) {
	s.completeActor, s.completeID = actor, id
	if s.completeErr != nil {
		return nil, s.completeErr
	}
	if s.completed != nil {
		return s.completed, nil
	}
	return &domainreminder.Reminder{ID: id, Title: "Pay bill", Scope: domainreminder.ScopePersonal, Status: domainreminder.StatusCompleted, ScheduledAt: time.Date(2026, 9, 20, 1, 0, 0, 0, time.UTC)}, nil
}

type userResolverStub struct {
	actor   serviceidentity.ActorContext
	err     error
	userID  string
	channel string
	calls   int
}

func (s *userResolverStub) ResolveUser(_ context.Context, userID, channel string) (serviceidentity.ActorContext, error) {
	s.calls++
	s.userID, s.channel = userID, channel
	return s.actor, s.err
}

func TestReminderCreateRejectsTrailingJSON(t *testing.T) {
	service := &reminderHTTPServiceStub{}
	h := NewReminderHandler(service, &userResolverStub{actor: httpTestActor()})
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.POST("/api/reminders", func(ctx *gin.Context) {
		ctx.Request = ctx.Request.WithContext(authscope.WithContext(ctx.Request.Context(), authscope.New("user-1", "Jane", "viewer", nil)))
		h.Create(ctx)
	})
	req := httptest.NewRequest(http.MethodPost, "/api/reminders", bytes.NewBufferString(`{"title":"Pay bill","scheduled_at":"2026-09-20T08:00:00Z"}{"extra":true}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("trailing JSON status = %d, want 400", rec.Code)
	}
	if service.createActor.UserID != "" {
		t.Fatal("trailing JSON reached service")
	}
}

func TestReminderFamilyPermissionMiddlewareUsesResolvedFamilyActor(t *testing.T) {
	service := &reminderHTTPServiceStub{}
	resolver := &userResolverStub{actor: httpTestActor()}
	h := NewReminderHandler(service, resolver)
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.POST("/api/reminders", FamilyPermissionMiddleware(resolver, "reminders:create"), h.Create)
	req := httptest.NewRequest(http.MethodPost, "/api/reminders", bytes.NewBufferString(`{"title":"Pay bill","scheduled_at":"2026-09-20T08:00:00Z"}`))
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(authscope.WithContext(req.Context(), authscope.New("user-effective", "Jane", "viewer", nil)))
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("family permission status = %d, want 201: %s", rec.Code, rec.Body.String())
	}
	if resolver.userID != "user-effective" || resolver.channel != "http" || resolver.calls != 1 {
		t.Fatalf("resolver call = user %q channel %q count %d", resolver.userID, resolver.channel, resolver.calls)
	}
	if service.createActor.RoleName != "parent" || service.createActor.UserID != "user-trusted" {
		t.Fatalf("service received untrusted actor: %+v", service.createActor)
	}
}

func TestReminderFamilyPermissionMiddlewareDeniesMissingFamilyPermission(t *testing.T) {
	service := &reminderHTTPServiceStub{}
	resolver := &userResolverStub{actor: serviceidentity.ActorContext{UserID: "user-trusted", MemberID: "member-trusted", FamilyID: "family-trusted", RoleName: "child"}}
	h := NewReminderHandler(service, resolver)
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.POST("/api/reminders", FamilyPermissionMiddleware(resolver, "reminders:create"), h.Create)
	req := httptest.NewRequest(http.MethodPost, "/api/reminders", bytes.NewBufferString(`{"title":"Pay bill","scheduled_at":"2026-09-20T08:00:00Z"}`))
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(authscope.WithContext(req.Context(), authscope.New("user-effective", "Jane", "superadmin", []string{"reminders:create"})))
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("missing family permission status = %d, want 403", rec.Code)
	}
	if service.createActor.UserID != "" {
		t.Fatal("missing family permission reached service")
	}
}

func TestReminderFamilyPermissionMiddlewareSetsImpersonationInitiator(t *testing.T) {
	service := &reminderHTTPServiceStub{}
	resolver := &userResolverStub{actor: httpTestActor()}
	h := NewReminderHandler(service, resolver)
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.POST("/api/reminders", FamilyPermissionMiddleware(resolver, "reminders:create"), h.Create)
	requestScope := authscope.NewFromClaims(map[string]any{
		"user_id": "user-effective", "is_impersonated": true, "original_user_id": "user-operator", "original_role": "admin",
	}, nil)
	req := httptest.NewRequest(http.MethodPost, "/api/reminders", bytes.NewBufferString(`{"title":"Pay bill","scheduled_at":"2026-09-20T08:00:00Z"}`))
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(authscope.WithContext(req.Context(), requestScope))
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("impersonation status = %d, want 201: %s", rec.Code, rec.Body.String())
	}
	if service.createActor.UserID != "user-trusted" || service.createActor.InitiatorUserID != "user-operator" || service.createActor.InitiatorRoleName != "admin" {
		t.Fatalf("unexpected impersonation actor: %+v", service.createActor)
	}
}

func httpTestActor() serviceidentity.ActorContext {
	return serviceidentity.ActorContext{UserID: "user-trusted", MemberID: "member-trusted", FamilyID: "family-trusted", RoleName: "parent", Permissions: map[string]struct{}{"reminders:create": {}, "reminders:list": {}, "reminders:update": {}}}
}

func performReminderHTTPRequest(method, path string, body any, scope authscope.Scope, handler gin.HandlerFunc) *httptest.ResponseRecorder {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Handle(method, "/api/reminders", func(ctx *gin.Context) {
		ctx.Request = ctx.Request.WithContext(authscope.WithContext(ctx.Request.Context(), scope))
		handler(ctx)
	})
	router.Handle(method, "/api/reminders/:id/complete", func(ctx *gin.Context) {
		ctx.Request = ctx.Request.WithContext(authscope.WithContext(ctx.Request.Context(), scope))
		handler(ctx)
	})
	var reader *bytes.Reader
	if body == nil {
		reader = bytes.NewReader(nil)
	} else {
		raw, _ := json.Marshal(body)
		reader = bytes.NewReader(raw)
	}
	req := httptest.NewRequest(method, path, reader)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

func TestReminderCreateResolvesHTTPUserAndCallsSharedService(t *testing.T) {
	service := &reminderHTTPServiceStub{}
	resolver := &userResolverStub{actor: httpTestActor()}
	h := NewReminderHandler(service, resolver)
	rec := performReminderHTTPRequest(http.MethodPost, "/api/reminders", map[string]any{
		"title": "Pay bill", "description": "Before Friday", "scheduled_at": "2026-09-20T08:00:00+07:00", "scope": "PERSONAL",
		"user_id": "attacker", "member_id": "spoofed",
	}, authscope.New(" user-trusted ", "Jane", "admin", nil), h.Create)
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
	if resolver.userID != "user-trusted" || resolver.channel != "http" {
		t.Fatalf("resolver got untrusted identity: user=%q channel=%q", resolver.userID, resolver.channel)
	}
	if service.createActor.UserID != "user-trusted" || service.createActor.MemberID != "member-trusted" || service.createInput.Title != "Pay bill" || service.createInput.Scope != domainreminder.ScopePersonal {
		t.Fatalf("unexpected service call: actor=%+v input=%+v", service.createActor, service.createInput)
	}
}

func TestReminderHTTPRejectsMissingIdentityAndInvalidSchedule(t *testing.T) {
	service := &reminderHTTPServiceStub{}
	resolver := &userResolverStub{actor: httpTestActor()}
	h := NewReminderHandler(service, resolver)
	if rec := performReminderHTTPRequest(http.MethodPost, "/api/reminders", map[string]any{"title": "Pay bill", "scheduled_at": "2026-09-20T08:00:00Z"}, authscope.Scope{}, h.Create); rec.Code != http.StatusUnauthorized {
		t.Fatalf("missing auth scope status = %d, want 401", rec.Code)
	}
	if rec := performReminderHTTPRequest(http.MethodPost, "/api/reminders", map[string]any{"title": "Pay bill", "scheduled_at": "tomorrow"}, authscope.New("user-1", "Jane", "admin", nil), h.Create); rec.Code != http.StatusBadRequest {
		t.Fatalf("invalid schedule status = %d, want 400", rec.Code)
	}
}

func TestReminderListMapsQueryFiltersAndUsesResolverActor(t *testing.T) {
	service := &reminderHTTPServiceStub{listed: []domainreminder.Reminder{{ID: httpReminderID, Title: "Pay bill", Scope: domainreminder.ScopePersonal, Status: domainreminder.StatusPending, ScheduledAt: time.Date(2026, 9, 20, 1, 0, 0, 0, time.UTC)}}}
	resolver := &userResolverStub{actor: httpTestActor()}
	h := NewReminderHandler(service, resolver)
	rec := performReminderHTTPRequest(http.MethodGet, "/api/reminders?scope=PERSONAL&status=COMPLETED&from=2026-09-20T00:00:00Z&to=2026-09-21T00:00:00Z&target_member_id=00000000-0000-0000-0000-000000000201", nil, authscope.New("user-1", "Jane", "admin", nil), h.List)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	input := service.listInput
	if service.listActor.UserID != "user-trusted" || input.Scope == nil || *input.Scope != domainreminder.ScopePersonal || input.Status == nil || *input.Status != domainreminder.StatusCompleted || input.From == nil || input.To == nil || input.TargetMemberID == nil {
		t.Fatalf("unexpected list mapping: actor=%+v input=%+v", service.listActor, input)
	}
}

func TestReminderCompleteUsesPathIDAndMapsErrors(t *testing.T) {
	resolver := &userResolverStub{actor: httpTestActor()}
	for _, tt := range []struct {
		name string
		err  error
		want int
	}{
		{name: "success", want: http.StatusOK},
		{name: "forbidden", err: serviceauthorization.ErrForbidden, want: http.StatusForbidden},
		{name: "not found", err: serviceauthorization.ErrNotFound, want: http.StatusNotFound},
		{name: "conflict", err: servicereminder.ErrConflict, want: http.StatusConflict},
		{name: "internal", err: errors.New("database details"), want: http.StatusInternalServerError},
	} {
		t.Run(tt.name, func(t *testing.T) {
			service := &reminderHTTPServiceStub{completeErr: tt.err}
			h := NewReminderHandler(service, resolver)
			rec := performReminderHTTPRequest(http.MethodPatch, "/api/reminders/"+httpReminderID+"/complete", map[string]any{"user_id": "attacker"}, authscope.New("user-1", "Jane", "admin", nil), h.Complete)
			if rec.Code != tt.want {
				t.Fatalf("status = %d, want %d: %s", rec.Code, tt.want, rec.Body.String())
			}
			if service.completeID != httpReminderID || service.completeActor.UserID != "user-trusted" {
				t.Fatalf("unexpected complete call: actor=%+v id=%q", service.completeActor, service.completeID)
			}
		})
	}
}

func TestReminderHTTPMapsInvalidAndUnauthenticatedResolverErrors(t *testing.T) {
	h := NewReminderHandler(&reminderHTTPServiceStub{}, &userResolverStub{err: serviceidentity.ErrUnauthenticated})
	rec := performReminderHTTPRequest(http.MethodGet, "/api/reminders", nil, authscope.New("user-1", "Jane", "admin", nil), h.List)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("resolver unauthenticated status = %d, want 401", rec.Code)
	}

	h = NewReminderHandler(&reminderHTTPServiceStub{}, &userResolverStub{err: &serviceauthorization.ValidationError{Field: "user_id", Reason: "is required"}})
	rec = performReminderHTTPRequest(http.MethodGet, "/api/reminders", nil, authscope.New("user-1", "Jane", "admin", nil), h.List)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("resolver validation status = %d, want 400", rec.Code)
	}

}
