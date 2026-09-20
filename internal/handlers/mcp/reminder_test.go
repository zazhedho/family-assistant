package mcp

import (
	"context"
	"errors"
	"testing"
	"time"

	domainpermission "family-assistant/internal/domain/permission"
	domainreminder "family-assistant/internal/domain/reminder"
	domainspace "family-assistant/internal/domain/space"
	serviceidentity "family-assistant/internal/services/identity"
	servicereminder "family-assistant/internal/services/reminder"
)

const (
	mcpReminderID = "00000000-0000-0000-0000-000000000101"
	mcpSpaceID    = "00000000-0000-0000-0000-000000000201"
	mcpMemberID   = "00000000-0000-0000-0000-000000000301"
)

type reminderToolServiceStub struct {
	createActor   serviceidentity.ActorContext
	createInput   servicereminder.CreateInput
	listActor     serviceidentity.ActorContext
	listInput     servicereminder.ListInput
	completeActor serviceidentity.ActorContext
	completeSpace string
	completeID    string
	createCalls   int
	listCalls     int
	completeCalls int
	createErr     error
	listErr       error
	completeErr   error
}

func (s *reminderToolServiceStub) Create(_ context.Context, actor serviceidentity.ActorContext, input servicereminder.CreateInput) (*domainreminder.Reminder, error) {
	s.createCalls++
	s.createActor, s.createInput = actor, input
	if s.createErr != nil {
		return nil, s.createErr
	}
	return &domainreminder.Reminder{
		ID: mcpReminderID, SpaceID: input.Space, Title: input.Title, Description: input.Description,
		AssigneeMemberID: input.AssigneeMemberID, Status: domainreminder.StatusPending, ScheduledAt: input.ScheduledAt,
	}, nil
}

func (s *reminderToolServiceStub) List(_ context.Context, actor serviceidentity.ActorContext, input servicereminder.ListInput) ([]domainreminder.Reminder, error) {
	s.listCalls++
	s.listActor, s.listInput = actor, input
	if s.listErr != nil {
		return nil, s.listErr
	}
	return []domainreminder.Reminder{{ID: mcpReminderID, SpaceID: input.Space, Title: "Pay bill", Status: domainreminder.StatusPending, ScheduledAt: time.Date(2026, 9, 20, 1, 0, 0, 0, time.UTC)}}, nil
}

func (s *reminderToolServiceStub) Complete(_ context.Context, actor serviceidentity.ActorContext, spaceID, reminderID string) (*domainreminder.Reminder, error) {
	s.completeCalls++
	s.completeActor, s.completeSpace, s.completeID = actor, spaceID, reminderID
	if s.completeErr != nil {
		return nil, s.completeErr
	}
	return &domainreminder.Reminder{ID: reminderID, SpaceID: spaceID, Title: "Pay bill", Status: domainreminder.StatusCompleted, ScheduledAt: time.Date(2026, 9, 20, 1, 0, 0, 0, time.UTC)}, nil
}

func mcpReminderResolver() *mcpExternalResolverStub {
	return &mcpExternalResolverStub{
		actor: serviceidentity.ActorContext{
			UserID: "user-1",
			Memberships: []domainspace.ResolvedMembership{
				{ID: mcpMemberID, SpaceID: mcpSpaceID, SpaceName: "Jane", SpaceType: domainspace.TypePersonal, UserID: "user-1", RoleID: "role-personal", RoleName: "space_owner", Status: domainspace.StatusActive},
				{ID: "00000000-0000-0000-0000-000000000302", SpaceID: "00000000-0000-0000-0000-000000000202", SpaceName: "Shared", SpaceType: domainspace.TypeShared, UserID: "user-1", RoleID: "role-shared", RoleName: "space_member", Status: domainspace.StatusActive},
			},
		},
		permissions: []domainpermission.Permission{
			{Resource: "reminders", Action: "create"},
			{Resource: "reminders", Action: "list"},
			{Resource: "reminders", Action: "update"},
		},
	}
}

func TestReminderCreateSelectsPersonalSpaceAndMapsAssignee(t *testing.T) {
	service := &reminderToolServiceStub{}
	resolver := mcpReminderResolver()
	got, err := ReminderCreate(mcpExternalContext(), resolver, service, ReminderCreateInput{
		Title: "Pay bill", Description: "Before Friday", ScheduledAt: "2026-09-20T08:00:00+07:00",
		AssigneeMemberID: "00000000-0000-0000-0000-000000000302",
	})
	if err != nil {
		t.Fatalf("create reminder: %v", err)
	}
	if got.ID != mcpReminderID || got.SpaceID != mcpSpaceID || got.Status != "PENDING" || got.ScheduledAt != "2026-09-20T08:00:00+07:00" {
		t.Fatalf("unexpected safe output: %+v", got)
	}
	if service.createCalls != 1 || service.createInput.Space != mcpSpaceID || service.createInput.AssigneeMemberID == nil || *service.createInput.AssigneeMemberID != "00000000-0000-0000-0000-000000000302" {
		t.Fatalf("unexpected service input: %+v", service.createInput)
	}
}

func TestReminderCreateRejectsInvalidScheduleSafely(t *testing.T) {
	service := &reminderToolServiceStub{}
	_, err := ReminderCreate(mcpExternalContext(), mcpReminderResolver(), service, ReminderCreateInput{Title: "Pay bill", ScheduledAt: "tomorrow"})
	var mapped *MCPError
	if !errors.As(err, &mapped) || mapped.Code != "invalid_input" {
		t.Fatalf("error = %T %v, want safe invalid_input", err, err)
	}
	if service.createCalls != 0 {
		t.Fatal("invalid input reached service")
	}
}

func TestReminderListSelectsSpaceByNameAndMapsFilters(t *testing.T) {
	service := &reminderToolServiceStub{}
	got, err := ReminderList(mcpExternalContext(), mcpReminderResolver(), service, ReminderListInput{
		Space: " shared ", Status: "COMPLETED", From: "2026-09-20T00:00:00Z", To: "2026-09-21T00:00:00Z",
	})
	if err != nil {
		t.Fatalf("list reminders: %v", err)
	}
	if len(got) != 1 || got[0].SpaceID != "00000000-0000-0000-0000-000000000202" || service.listInput.Space != "00000000-0000-0000-0000-000000000202" {
		t.Fatalf("unexpected list mapping: got=%+v input=%+v", got, service.listInput)
	}
	if service.listInput.Status == nil || *service.listInput.Status != domainreminder.StatusCompleted || service.listInput.From == nil || service.listInput.To == nil {
		t.Fatalf("unexpected list filters: %+v", service.listInput)
	}
}

func TestReminderCompletePassesSelectedSpaceAndReminderID(t *testing.T) {
	service := &reminderToolServiceStub{}
	got, err := ReminderComplete(mcpExternalContext(), mcpReminderResolver(), service, ReminderCompleteInput{Space: mcpSpaceID, ReminderID: mcpReminderID})
	if err != nil {
		t.Fatalf("complete reminder: %v", err)
	}
	if got.ID != mcpReminderID || got.SpaceID != mcpSpaceID || service.completeSpace != mcpSpaceID || service.completeID != mcpReminderID {
		t.Fatalf("unexpected complete call/output: got=%+v service=%+v", got, service)
	}
}

func TestReminderToolsMapServiceErrorsSafely(t *testing.T) {
	service := &reminderToolServiceStub{createErr: servicereminder.ErrConflict}
	_, err := ReminderCreate(mcpExternalContext(), mcpReminderResolver(), service, ReminderCreateInput{Title: "Pay bill", ScheduledAt: "2026-09-20T08:00:00Z"})
	var mapped *MCPError
	if !errors.As(err, &mapped) || mapped.Code != "conflict" {
		t.Fatalf("error = %T %v, want conflict", err, err)
	}
}

var _ servicereminder.Service = (*reminderToolServiceStub)(nil)
