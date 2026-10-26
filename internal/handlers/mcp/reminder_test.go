package mcp

import (
	"context"
	"errors"
	"testing"
	"time"

	domainidentity "family-assistant/internal/domain/identity"
	domainpermission "family-assistant/internal/domain/permission"
	domainreminder "family-assistant/internal/domain/reminder"
	domainspace "family-assistant/internal/domain/space"
	"family-assistant/internal/dto"
	interfacereminder "family-assistant/internal/interfaces/reminder"
	servicereminder "family-assistant/internal/services/reminder"
)

const (
	mcpReminderID = "00000000-0000-0000-0000-000000000101"
	mcpSpaceID    = "00000000-0000-0000-0000-000000000201"
	mcpMemberID   = "00000000-0000-0000-0000-000000000301"
)

type reminderToolServiceStub struct {
	createActor   domainidentity.ActorContext
	createInput   dto.ReminderCreateInput
	listActor     domainidentity.ActorContext
	listInput     dto.ReminderListInput
	completeActor domainidentity.ActorContext
	completeSpace string
	completeID    string
	createCalls   int
	listCalls     int
	completeCalls int
	createErr     error
	listErr       error
	completeErr   error
	updateActor   domainidentity.ActorContext
	updateInput   dto.ReminderUpdateInput
	updateSpace   string
	updateID      string
	updateCalls   int
	updateErr     error
	deleteActor   domainidentity.ActorContext
	deleteSpace   string
	deleteID      string
	deleteCalls   int
	deleteErr     error
	replaceResult *domainreminder.ReplaceResult
}

func (s *reminderToolServiceStub) Create(_ context.Context, actor domainidentity.ActorContext, input dto.ReminderCreateInput) (*domainreminder.Reminder, error) {
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

func (s *reminderToolServiceStub) CreateReplacing(ctx context.Context, actor domainidentity.ActorContext, input dto.ReminderCreateInput) (domainreminder.ReplaceResult, error) {
	reminder, err := s.Create(ctx, actor, input)
	if err != nil {
		return domainreminder.ReplaceResult{}, err
	}
	if s.replaceResult != nil {
		result := *s.replaceResult
		if result.Reminder == nil {
			result.Reminder = reminder
		}
		return result, nil
	}
	return domainreminder.ReplaceResult{Outcome: domainreminder.ReplaceOutcomeNone, Reminder: reminder}, nil
}

func (s *reminderToolServiceStub) List(_ context.Context, actor domainidentity.ActorContext, input dto.ReminderListInput) ([]domainreminder.Reminder, error) {
	s.listCalls++
	s.listActor, s.listInput = actor, input
	if s.listErr != nil {
		return nil, s.listErr
	}
	return []domainreminder.Reminder{{ID: mcpReminderID, SpaceID: input.Space, Title: "Pay bill", Status: domainreminder.StatusPending, ScheduledAt: time.Date(2026, 9, 20, 1, 0, 0, 0, time.UTC)}}, nil
}

func (s *reminderToolServiceStub) Complete(_ context.Context, actor domainidentity.ActorContext, spaceID, reminderID string) (*domainreminder.Reminder, error) {
	s.completeCalls++
	s.completeActor, s.completeSpace, s.completeID = actor, spaceID, reminderID
	if s.completeErr != nil {
		return nil, s.completeErr
	}
	return &domainreminder.Reminder{ID: reminderID, SpaceID: spaceID, Title: "Pay bill", Status: domainreminder.StatusCompleted, ScheduledAt: time.Date(2026, 9, 20, 1, 0, 0, 0, time.UTC)}, nil
}

func (s *reminderToolServiceStub) Update(_ context.Context, actor domainidentity.ActorContext, spaceID, reminderID string, input dto.ReminderUpdateInput) (*domainreminder.Reminder, error) {
	s.updateCalls++
	s.updateActor, s.updateSpace, s.updateID, s.updateInput = actor, spaceID, reminderID, input
	if s.updateErr != nil {
		return nil, s.updateErr
	}
	title := "Updated title"
	if input.Title != nil {
		title = *input.Title
	}
	return &domainreminder.Reminder{ID: reminderID, SpaceID: spaceID, Title: title, Status: domainreminder.StatusPending, ScheduledAt: time.Date(2026, 9, 20, 1, 0, 0, 0, time.UTC)}, nil
}

func (s *reminderToolServiceStub) Delete(_ context.Context, actor domainidentity.ActorContext, spaceID, reminderID string) (*domainreminder.Reminder, error) {
	s.deleteCalls++
	s.deleteActor, s.deleteSpace, s.deleteID = actor, spaceID, reminderID
	if s.deleteErr != nil {
		return nil, s.deleteErr
	}
	return &domainreminder.Reminder{ID: reminderID, SpaceID: spaceID, Status: domainreminder.StatusCancelled, ScheduledAt: time.Date(2026, 9, 20, 1, 0, 0, 0, time.UTC)}, nil
}

func mcpReminderResolver() *mcpExternalResolverStub {
	return &mcpExternalResolverStub{
		actor: domainidentity.ActorContext{
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

func TestReminderCreateCapturesTrustedChatTarget(t *testing.T) {
	service := &reminderToolServiceStub{}
	ctx := WithExternalRequest(context.Background(), ExternalRequest{
		Provider: "hermes", ExternalID: "sender@s.whatsapp.net", Channel: "whatsapp",
		ChatID: "120363@g.us", ChatType: "group",
	})

	if _, err := ReminderCreate(ctx, mcpReminderResolver(), service, ReminderCreateInput{
		Title: "Baby feeding", ScheduledAt: "2026-09-20T08:00:00Z",
	}); err != nil {
		t.Fatalf("create reminder: %v", err)
	}
	if service.createInput.DeliveryProvider != "whatsapp" || service.createInput.DeliveryTarget != "120363@g.us" {
		t.Fatalf("delivery target = %+v, want trusted WhatsApp group", service.createInput)
	}
}

func TestReminderCreateFallsBackToTrustedExternalIDWithoutChatID(t *testing.T) {
	service := &reminderToolServiceStub{}
	ctx := WithExternalRequest(context.Background(), ExternalRequest{
		Provider: "hermes", ExternalID: "6285333320090@s.whatsapp.net", Channel: "whatsapp",
	})

	if _, err := ReminderCreate(ctx, mcpReminderResolver(), service, ReminderCreateInput{
		Title: "Personal reminder", ScheduledAt: "2026-09-20T08:00:00Z",
	}); err != nil {
		t.Fatalf("create reminder: %v", err)
	}
	if service.createInput.DeliveryProvider != "whatsapp" || service.createInput.DeliveryTarget != "6285333320090@s.whatsapp.net" {
		t.Fatalf("delivery target = %+v, want trusted external ID fallback", service.createInput)
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

func TestReminderListDefaultsWindowWithoutBounds(t *testing.T) {
	service := &reminderToolServiceStub{}
	before := time.Now()
	if _, err := ReminderList(mcpExternalContext(), mcpReminderResolver(), service, ReminderListInput{Status: "PENDING"}); err != nil {
		t.Fatalf("list reminders: %v", err)
	}
	after := time.Now()
	from, to := service.listInput.From, service.listInput.To
	if from == nil || to == nil {
		t.Fatalf("window = %v..%v, want both bounds", from, to)
	}
	if from.Before(before.Add(-reminderListDefaultLookback)) || from.After(after.Add(-reminderListDefaultLookback)) {
		t.Fatalf("from = %v, want about 24h ago", from)
	}
	if to.Before(before.Add(reminderListDefaultLookahead)) || to.After(after.Add(reminderListDefaultLookahead)) {
		t.Fatalf("to = %v, want about 30 days ahead", to)
	}
}

func TestReminderListBoundsLowerWhenOnlyUpperGiven(t *testing.T) {
	service := &reminderToolServiceStub{}
	if _, err := ReminderList(mcpExternalContext(), mcpReminderResolver(), service, ReminderListInput{To: "2026-09-21T00:00:00Z"}); err != nil {
		t.Fatalf("list reminders: %v", err)
	}
	want := time.Date(2026, 9, 21, 0, 0, 0, 0, time.UTC).Add(-reminderListDefaultLookahead)
	if service.listInput.From == nil || !service.listInput.From.Equal(want) {
		t.Fatalf("from = %v, want %v", service.listInput.From, want)
	}
}

func TestReminderListKeepsExplicitLowerBoundOnly(t *testing.T) {
	service := &reminderToolServiceStub{}
	if _, err := ReminderList(mcpExternalContext(), mcpReminderResolver(), service, ReminderListInput{From: "2026-09-01T00:00:00Z"}); err != nil {
		t.Fatalf("list reminders: %v", err)
	}
	if service.listInput.From == nil || !service.listInput.From.Equal(time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)) || service.listInput.To != nil {
		t.Fatalf("window = %v..%v, want explicit from and open end", service.listInput.From, service.listInput.To)
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

func TestReminderUpdateMapsPatchAndSelectedSpace(t *testing.T) {
	service := &reminderToolServiceStub{}
	title := "Updated title"
	got, err := ReminderUpdate(mcpExternalContext(), mcpReminderResolver(), service, ReminderUpdateInput{
		Space: mcpSpaceID, ReminderID: mcpReminderID, Title: &title, ScheduledAt: "2026-09-22T08:00:00+07:00",
	})
	if err != nil {
		t.Fatalf("update reminder: %v", err)
	}
	if got.ID != mcpReminderID || got.SpaceID != mcpSpaceID || service.updateCalls != 1 || service.updateSpace != mcpSpaceID || service.updateID != mcpReminderID {
		t.Fatalf("unexpected update: got=%+v service=%+v", got, service)
	}
	if service.updateInput.Title == nil || *service.updateInput.Title != title || service.updateInput.ScheduledAt == nil {
		t.Fatalf("unexpected update input: %+v", service.updateInput)
	}
}

func TestReminderUpdateRejectsEmptyPatchBeforeService(t *testing.T) {
	service := &reminderToolServiceStub{}
	_, err := ReminderUpdate(mcpExternalContext(), mcpReminderResolver(), service, ReminderUpdateInput{Space: mcpSpaceID, ReminderID: mcpReminderID})
	var mapped *MCPError
	if !errors.As(err, &mapped) || mapped.Code != "invalid_input" {
		t.Fatalf("error = %T %v, want invalid_input", err, err)
	}
	if service.updateCalls != 0 {
		t.Fatal("empty update reached service")
	}
}

func TestReminderDeletePassesSelectedSpaceAndID(t *testing.T) {
	service := &reminderToolServiceStub{}
	got, err := ReminderDelete(mcpExternalContext(), mcpReminderResolver(), service, ReminderDeleteInput{Space: mcpSpaceID, ReminderID: mcpReminderID})
	if err != nil {
		t.Fatalf("delete reminder: %v", err)
	}
	if got.ID != mcpReminderID || got.Status != string(domainreminder.StatusCancelled) || service.deleteCalls != 1 || service.deleteSpace != mcpSpaceID || service.deleteID != mcpReminderID {
		t.Fatalf("unexpected delete: got=%+v service=%+v", got, service)
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

var _ interfacereminder.ServiceReminderInterface = (*reminderToolServiceStub)(nil)

func TestReminderCreateReturnsCompletedReminders(t *testing.T) {
	key := "diaper"
	completedAt := time.Date(2026, 9, 20, 1, 0, 0, 0, time.UTC)
	service := &reminderToolServiceStub{replaceResult: &domainreminder.ReplaceResult{
		Outcome: domainreminder.ReplaceOutcomeCreated,
		Completed: []domainreminder.Reminder{{
			ID: "sent-1", SpaceID: mcpSpaceID, Title: "Sent", Status: domainreminder.StatusCompleted,
			ScheduledAt: time.Date(2026, 9, 20, 0, 0, 0, 0, time.UTC), CompletedAt: &completedAt, ReplaceKey: &key,
		}},
	}}
	got, err := ReminderCreate(mcpExternalContext(), mcpReminderResolver(), service, ReminderCreateInput{
		Title: "Diaper", ScheduledAt: "2026-09-20T08:00:00+07:00", ReplaceKey: "diaper",
	})
	if err != nil {
		t.Fatalf("create reminder: %v", err)
	}
	if got.ReplaceOutcome != "created" || len(got.ReplacedReminders) != 0 || len(got.CompletedReminders) != 1 ||
		got.CompletedReminders[0].ID != "sent-1" || got.CompletedReminders[0].Status != "COMPLETED" {
		t.Fatalf("output = %+v", got)
	}
}

func TestReminderCreatePassesReplaceKeyAndReturnsOutcome(t *testing.T) {
	key := "diaper"
	service := &reminderToolServiceStub{replaceResult: &domainreminder.ReplaceResult{
		Outcome: domainreminder.ReplaceOutcomeReplaced,
		Replaced: []domainreminder.Reminder{{
			ID: "old-1", SpaceID: mcpSpaceID, Title: "Old", Status: domainreminder.StatusCancelled,
			ScheduledAt: time.Date(2026, 9, 20, 4, 0, 0, 0, time.UTC), ReplaceKey: &key,
		}},
	}}
	got, err := ReminderCreate(mcpExternalContext(), mcpReminderResolver(), service, ReminderCreateInput{
		Title: "Diaper", ScheduledAt: "2026-09-20T08:00:00+07:00", ReplaceKey: " Diaper ",
	})
	if err != nil {
		t.Fatalf("create reminder: %v", err)
	}
	if service.createInput.ReplaceKey != " Diaper " {
		t.Fatalf("service replace key = %q", service.createInput.ReplaceKey)
	}
	if got.ReplaceOutcome != "replaced" || len(got.ReplacedReminders) != 1 || got.ReplacedReminders[0].ID != "old-1" || got.ReplacedReminders[0].Status != "CANCELLED" {
		t.Fatalf("output = %+v", got)
	}
}

func TestReminderCreateKeptExistingReturnsKeptReminder(t *testing.T) {
	service := &reminderToolServiceStub{replaceResult: &domainreminder.ReplaceResult{
		Outcome: domainreminder.ReplaceOutcomeKeptExisting,
		Reminder: &domainreminder.Reminder{
			ID: "kept-1", SpaceID: mcpSpaceID, Title: "Later", Status: domainreminder.StatusPending,
			ScheduledAt: time.Date(2026, 9, 21, 8, 0, 0, 0, time.UTC),
		},
	}}
	got, err := ReminderCreate(mcpExternalContext(), mcpReminderResolver(), service, ReminderCreateInput{
		Title: "Diaper", ScheduledAt: "2026-09-20T08:00:00+07:00", ReplaceKey: "diaper",
	})
	if err != nil {
		t.Fatalf("create reminder: %v", err)
	}
	if got.ID != "kept-1" || got.ReplaceOutcome != "kept_existing" || len(got.ReplacedReminders) != 0 {
		t.Fatalf("output = %+v", got)
	}
}
