package handlerreminder

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"family-assistant/internal/authscope"
	domainidentity "family-assistant/internal/domain/identity"
	domainpermission "family-assistant/internal/domain/permission"
	domainreminder "family-assistant/internal/domain/reminder"
	domainspace "family-assistant/internal/domain/space"
	"family-assistant/internal/dto"
	interfacereminder "family-assistant/internal/interfaces/reminder"
	serviceauthorization "family-assistant/internal/services/authorization"
	servicereminder "family-assistant/internal/services/reminder"

	"github.com/gin-gonic/gin"
)

const (
	httpReminderID = "00000000-0000-0000-0000-000000000101"
	httpSpaceID    = "00000000-0000-0000-0000-000000000201"
	httpMemberID   = "00000000-0000-0000-0000-000000000301"
)

type reminderHTTPServiceStub struct {
	createActor   domainidentity.ActorContext
	createInput   dto.ReminderCreateInput
	listActor     domainidentity.ActorContext
	listInput     dto.ReminderListInput
	completeActor domainidentity.ActorContext
	completeSpace string
	completeID    string
	created       *domainreminder.Reminder
	listed        []domainreminder.Reminder
	completed     *domainreminder.Reminder
	createErr     error
	listErr       error
	completeErr   error
}

func (s *reminderHTTPServiceStub) Create(_ context.Context, actor domainidentity.ActorContext, input dto.ReminderCreateInput) (*domainreminder.Reminder, error) {
	s.createActor, s.createInput = actor, input
	if s.createErr != nil {
		return nil, s.createErr
	}
	if s.created != nil {
		return s.created, nil
	}
	return &domainreminder.Reminder{ID: httpReminderID, SpaceID: input.Space, Title: input.Title, AssigneeMemberID: input.AssigneeMemberID, Status: domainreminder.StatusPending, ScheduledAt: input.ScheduledAt}, nil
}

func (s *reminderHTTPServiceStub) List(_ context.Context, actor domainidentity.ActorContext, input dto.ReminderListInput) ([]domainreminder.Reminder, error) {
	s.listActor, s.listInput = actor, input
	if s.listErr != nil {
		return nil, s.listErr
	}
	return s.listed, nil
}

func (s *reminderHTTPServiceStub) Complete(_ context.Context, actor domainidentity.ActorContext, spaceID, reminderID string) (*domainreminder.Reminder, error) {
	s.completeActor, s.completeSpace, s.completeID = actor, spaceID, reminderID
	if s.completeErr != nil {
		return nil, s.completeErr
	}
	if s.completed != nil {
		return s.completed, nil
	}
	return &domainreminder.Reminder{ID: reminderID, SpaceID: spaceID, Title: "Pay bill", Status: domainreminder.StatusCompleted, ScheduledAt: time.Date(2026, 9, 20, 1, 0, 0, 0, time.UTC)}, nil
}

func (s *reminderHTTPServiceStub) Update(context.Context, domainidentity.ActorContext, string, string, dto.ReminderUpdateInput) (*domainreminder.Reminder, error) {
	return nil, nil
}

func (s *reminderHTTPServiceStub) Delete(context.Context, domainidentity.ActorContext, string, string) (*domainreminder.Reminder, error) {
	return nil, nil
}

type userResolverStub struct {
	actor   domainidentity.ActorContext
	err     error
	userID  string
	channel string
	calls   int
}

func (s *userResolverStub) ResolveUser(_ context.Context, userID, channel string) (domainidentity.ActorContext, error) {
	s.calls++
	s.userID, s.channel = userID, channel
	return s.actor, s.err
}

type permissionLoaderStub struct {
	permissions []domainpermission.Permission
	roleID      string
}

func (s *permissionLoaderStub) GetRolePermissions(_ context.Context, roleID string) ([]domainpermission.Permission, error) {
	s.roleID = roleID
	return s.permissions, nil
}

func httpTestActor() domainidentity.ActorContext {
	return domainidentity.ActorContext{
		UserID: "user-trusted",
		Memberships: []domainspace.ResolvedMembership{{
			ID: httpMemberID, SpaceID: httpSpaceID, SpaceName: "Jane", SpaceType: domainspace.TypePersonal,
			UserID: "user-trusted", RoleID: "role-owner", RoleName: "space_owner", Status: domainspace.StatusActive,
		}},
		Source: "http",
	}
}

func httpPermissions() *permissionLoaderStub {
	return &permissionLoaderStub{permissions: []domainpermission.Permission{
		{Resource: "reminders", Action: "create"},
		{Resource: "reminders", Action: "list"},
		{Resource: "reminders", Action: "update"},
	}}
}

func performReminderHTTPRequest(method, path, body string, scope authscope.Scope, handler gin.HandlerFunc) *httptest.ResponseRecorder {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Handle(method, "/api/reminders", func(ctx *gin.Context) {
		ctx.Request = ctx.Request.WithContext(authscope.WithContext(ctx.Request.Context(), scope))
		handler(ctx)
	})
	router.Handle(method, "/api/reminders/:reminder_id/complete", func(ctx *gin.Context) {
		ctx.Request = ctx.Request.WithContext(authscope.WithContext(ctx.Request.Context(), scope))
		handler(ctx)
	})
	req := httptest.NewRequest(method, path, bytes.NewBufferString(body))
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

func TestReminderCreateSelectsPersonalSpaceAndMapsAssignee(t *testing.T) {
	service := &reminderHTTPServiceStub{}
	resolver := &userResolverStub{actor: httpTestActor()}
	h := NewReminderHandler(service, resolver, httpPermissions())
	rec := performReminderHTTPRequest(http.MethodPost, "/api/reminders", `{"title":"Pay bill","scheduled_at":"2026-09-20T08:00:00+07:00","assignee_member_id":"00000000-0000-0000-0000-000000000302"}`, authscope.New("user-trusted", "Jane", "user", nil), h.Create)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create status = %d, want 201: %s", rec.Code, rec.Body.String())
	}
	if service.createInput.Space != httpSpaceID || service.createInput.AssigneeMemberID == nil || *service.createInput.AssigneeMemberID != "00000000-0000-0000-0000-000000000302" {
		t.Fatalf("service input = %+v, want selected Personal Space and assignee", service.createInput)
	}
	if resolver.userID != "user-trusted" || resolver.channel != "http" || resolver.calls != 1 {
		t.Fatalf("resolver call = user %q channel %q count %d", resolver.userID, resolver.channel, resolver.calls)
	}
}

func TestReminderCreateRejectsNonRFC3339(t *testing.T) {
	service := &reminderHTTPServiceStub{}
	h := NewReminderHandler(service, &userResolverStub{actor: httpTestActor()}, httpPermissions())
	for _, body := range []string{`{"title":"Pay bill","scheduled_at":"tomorrow"}`} {
		rec := performReminderHTTPRequest(http.MethodPost, "/api/reminders", body, authscope.New("user-trusted", "Jane", "user", nil), h.Create)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("body %s status = %d, want 400", body, rec.Code)
		}
	}
	if service.createInput.Space != "" {
		t.Fatal("invalid create request reached service")
	}
}

func TestReminderStatusFilterAcceptsSent(t *testing.T) {
	status, err := statusFilter("SENT")
	if err != nil || status == nil || *status != domainreminder.StatusSent {
		t.Fatalf("statusFilter(SENT) = %v, %v", status, err)
	}
}

func TestReminderListUsesExplicitUUIDSpaceAndStrictTime(t *testing.T) {
	service := &reminderHTTPServiceStub{}
	resolver := &userResolverStub{actor: httpTestActor()}
	h := NewReminderHandler(service, resolver, httpPermissions())
	rec := performReminderHTTPRequest(http.MethodGet, "/api/reminders?space_id="+httpSpaceID+"&status=PENDING&from=2026-09-20T00:00:00Z&to=2026-09-21T00:00:00Z", "", authscope.New("user-trusted", "Jane", "user", nil), h.List)
	if rec.Code != http.StatusOK {
		t.Fatalf("list status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	if service.listInput.Space != httpSpaceID || service.listInput.Status == nil || *service.listInput.Status != domainreminder.StatusPending || service.listInput.From == nil || service.listInput.To == nil {
		t.Fatalf("list input = %+v", service.listInput)
	}
	bad := performReminderHTTPRequest(http.MethodGet, "/api/reminders?from=tomorrow", "", authscope.New("user-trusted", "Jane", "user", nil), h.List)
	if bad.Code != http.StatusBadRequest || service.listInput.From == nil {
		t.Fatalf("invalid time status = %d, list input = %+v", bad.Code, service.listInput)
	}
}

func TestReminderCompleteUsesSpaceAndReminderPath(t *testing.T) {
	service := &reminderHTTPServiceStub{}
	h := NewReminderHandler(service, &userResolverStub{actor: httpTestActor()}, httpPermissions())
	rec := performReminderHTTPRequest(http.MethodPost, "/api/reminders/"+httpReminderID+"/complete?space_id="+httpSpaceID, "", authscope.New("user-trusted", "Jane", "user", nil), h.Complete)
	if rec.Code != http.StatusOK {
		t.Fatalf("complete status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	if service.completeSpace != httpSpaceID || service.completeID != httpReminderID {
		t.Fatalf("complete call = space %q reminder %q", service.completeSpace, service.completeID)
	}
}

func TestReminderCompleteRejectsInvalidReminderUUIDBeforeService(t *testing.T) {
	service := &reminderHTTPServiceStub{}
	h := NewReminderHandler(service, &userResolverStub{actor: httpTestActor()}, httpPermissions())
	rec := performReminderHTTPRequest(http.MethodPost, "/api/reminders/not-a-uuid/complete?space_id="+httpSpaceID, "", authscope.New("user-trusted", "Jane", "user", nil), h.Complete)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("invalid reminder ID status = %d, want 400: %s", rec.Code, rec.Body.String())
	}
	if service.completeID != "" {
		t.Fatalf("invalid reminder ID reached service: %q", service.completeID)
	}
}

func TestReminderHTTPMapsServiceErrorsSafely(t *testing.T) {
	for _, tt := range []struct {
		name string
		err  error
		want int
	}{
		{name: "forbidden", err: serviceauthorization.ErrForbidden, want: http.StatusForbidden},
		{name: "not found", err: serviceauthorization.ErrNotFound, want: http.StatusNotFound},
		{name: "conflict", err: servicereminder.ErrConflict, want: http.StatusConflict},
	} {
		t.Run(tt.name, func(t *testing.T) {
			service := &reminderHTTPServiceStub{createErr: tt.err}
			h := NewReminderHandler(service, &userResolverStub{actor: httpTestActor()}, httpPermissions())
			rec := performReminderHTTPRequest(http.MethodPost, "/api/reminders", `{"title":"Pay bill","scheduled_at":"2026-09-20T08:00:00Z"}`, authscope.New("user-trusted", "Jane", "user", nil), h.Create)
			if rec.Code != tt.want {
				t.Fatalf("status = %d, want %d: %s", rec.Code, tt.want, rec.Body.String())
			}
		})
	}
}

func TestReminderHTTPRequiresAuthentication(t *testing.T) {
	h := NewReminderHandler(&reminderHTTPServiceStub{}, &userResolverStub{actor: httpTestActor()}, httpPermissions())
	rec := performReminderHTTPRequest(http.MethodPost, "/api/reminders", `{"title":"Pay bill","scheduled_at":"2026-09-20T08:00:00Z"}`, authscope.Scope{}, h.Create)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("missing auth status = %d, want 401", rec.Code)
	}
}

var _ interfacereminder.ServiceReminderInterface = (*reminderHTTPServiceStub)(nil)
