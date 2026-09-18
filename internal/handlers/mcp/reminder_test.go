package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	domainreminder "github.com/zazhedho/family-assistant/internal/domain/reminder"
	serviceidentity "github.com/zazhedho/family-assistant/internal/services/identity"
	servicereminder "github.com/zazhedho/family-assistant/internal/services/reminder"
)

const (
	mcpReminderID = "00000000-0000-0000-0000-000000000101"
	mcpTargetID   = "00000000-0000-0000-0000-000000000201"
)

type reminderToolServiceStub struct {
	createActor   serviceidentity.ActorContext
	createInput   servicereminder.CreateInput
	listActor     serviceidentity.ActorContext
	listInput     servicereminder.ListInput
	completeActor serviceidentity.ActorContext
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
		ID: mcpReminderID, Title: input.Title, Scope: input.Scope,
		Status: domainreminder.StatusPending, ScheduledAt: input.ScheduledAt,
	}, nil
}

func (s *reminderToolServiceStub) List(_ context.Context, actor serviceidentity.ActorContext, input servicereminder.ListInput) ([]domainreminder.Reminder, error) {
	s.listCalls++
	s.listActor, s.listInput = actor, input
	if s.listErr != nil {
		return nil, s.listErr
	}
	return []domainreminder.Reminder{{ID: mcpReminderID, Title: "Pay bill", Scope: domainreminder.ScopePersonal, Status: domainreminder.StatusPending, ScheduledAt: time.Date(2026, 9, 20, 1, 0, 0, 0, time.UTC)}}, nil
}

func (s *reminderToolServiceStub) Complete(_ context.Context, actor serviceidentity.ActorContext, reminderID string) (*domainreminder.Reminder, error) {
	s.completeCalls++
	s.completeActor, s.completeID = actor, reminderID
	if s.completeErr != nil {
		return nil, s.completeErr
	}
	return &domainreminder.Reminder{ID: reminderID, Title: "Pay bill", Scope: domainreminder.ScopePersonal, Status: domainreminder.StatusCompleted, ScheduledAt: time.Date(2026, 9, 20, 1, 0, 0, 0, time.UTC)}, nil
}

func mcpTestActor() serviceidentity.ActorContext {
	return serviceidentity.ActorContext{
		UserID: "user-trusted", MemberID: "member-trusted", FamilyID: "family-trusted",
		RoleID: "role-parent", RoleName: "parent", HermesProfileID: "profile-trusted",
		Source: "mcp", Channel: "whatsapp", Permissions: map[string]struct{}{"reminders:create": {}, "reminders:list": {}, "reminders:update": {}},
	}
}

func TestReminderCreateUsesContextActorAndMapsToolData(t *testing.T) {
	service := &reminderToolServiceStub{}
	actor := mcpTestActor()
	ctx := WithActorContext(context.Background(), actor)
	input := ReminderCreateInput{
		Title: "Pay bill", Description: "Before Friday", ScheduledAt: "2026-09-20T08:00:00+07:00",
		Scope: "PERSONAL", TargetMemberID: mcpTargetID,
	}
	// Unknown JSON identity fields must remain ignored by the typed input.
	var decoded ReminderCreateInput
	if err := json.Unmarshal([]byte(`{"title":"Pay bill","scheduled_at":"2026-09-20T08:00:00+07:00","user_id":"attacker","member_id":"spoofed"}`), &decoded); err != nil {
		t.Fatalf("decode input: %v", err)
	}
	if decoded.Title != "Pay bill" {
		t.Fatalf("unexpected decoded title: %+v", decoded)
	}

	got, err := ReminderCreate(ctx, service, input)
	if err != nil {
		t.Fatalf("create reminder: %v", err)
	}
	if got.ID != mcpReminderID || got.Title != input.Title || got.Scope != "PERSONAL" || got.Status != "PENDING" || got.ScheduledAt != input.ScheduledAt {
		t.Fatalf("unexpected safe output: %+v", got)
	}
	if service.createCalls != 1 || service.createActor.UserID != actor.UserID || service.createActor.MemberID != actor.MemberID {
		t.Fatalf("service did not receive context actor: %+v", service.createActor)
	}
	if service.createInput.Title != input.Title || service.createInput.Description != input.Description || service.createInput.Scope != domainreminder.ScopePersonal || service.createInput.TargetMemberID == nil || *service.createInput.TargetMemberID != mcpTargetID {
		t.Fatalf("unexpected service input: %+v", service.createInput)
	}
}

func TestReminderCreateRejectsMissingTitleAndInvalidScheduleSafely(t *testing.T) {
	for _, tt := range []struct {
		name  string
		input ReminderCreateInput
	}{
		{name: "missing title", input: ReminderCreateInput{ScheduledAt: "2026-09-20T08:00:00Z"}},
		{name: "invalid schedule", input: ReminderCreateInput{Title: "Pay bill", ScheduledAt: "tomorrow"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			service := &reminderToolServiceStub{}
			_, err := ReminderCreate(WithActorContext(context.Background(), mcpTestActor()), service, tt.input)
			var mapped *MCPError
			if !errors.As(err, &mapped) || mapped.Code != "invalid_input" {
				t.Fatalf("error = %T %v, want safe invalid_input", err, err)
			}
			if service.createCalls != 0 {
				t.Fatal("invalid input reached service")
			}
		})
	}
}

func TestReminderListMapsFiltersAndUsesContextActor(t *testing.T) {
	service := &reminderToolServiceStub{}
	from := "2026-09-20T00:00:00Z"
	to := "2026-09-21T00:00:00Z"
	got, err := ReminderList(WithActorContext(context.Background(), mcpTestActor()), service, ReminderListInput{
		Scope: "PERSONAL", Status: "COMPLETED", From: from, To: to, TargetMemberID: mcpTargetID,
	})
	if err != nil {
		t.Fatalf("list reminders: %v", err)
	}
	if len(got) != 1 || got[0].ID != mcpReminderID || got[0].Status != "PENDING" {
		t.Fatalf("unexpected list output: %+v", got)
	}
	input := service.listInput
	if service.listCalls != 1 || service.listActor.UserID != "user-trusted" || input.Scope == nil || *input.Scope != domainreminder.ScopePersonal || input.Status == nil || *input.Status != domainreminder.StatusCompleted || input.From == nil || input.To == nil || input.TargetMemberID == nil || *input.TargetMemberID != mcpTargetID {
		t.Fatalf("unexpected list mapping: actor=%+v input=%+v", service.listActor, input)
	}
	if input.From.Format(time.RFC3339) != from || input.To.Format(time.RFC3339) != to {
		t.Fatalf("unexpected date filters: %+v", input)
	}
}

func TestReminderCompletePassesOnlyIDAndContextActor(t *testing.T) {
	service := &reminderToolServiceStub{}
	actor := mcpTestActor()
	got, err := ReminderComplete(WithActorContext(context.Background(), actor), service, ReminderCompleteInput{ReminderID: mcpReminderID})
	if err != nil {
		t.Fatalf("complete reminder: %v", err)
	}
	if got.ID != mcpReminderID || got.Status != "COMPLETED" {
		t.Fatalf("unexpected complete output: %+v", got)
	}
	if service.completeCalls != 1 || service.completeID != mcpReminderID || service.completeActor.UserID != actor.UserID || service.completeActor.MemberID != actor.MemberID {
		t.Fatalf("unexpected complete call: actor=%+v id=%q", service.completeActor, service.completeID)
	}
}
