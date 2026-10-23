package mcp

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"sort"
	"strings"
	"testing"
	"time"

	domainidentity "family-assistant/internal/domain/identity"
	domainscheduledmessage "family-assistant/internal/domain/scheduledmessage"
	domainspace "family-assistant/internal/domain/space"
	"family-assistant/internal/dto"
	interfacescheduledmessage "family-assistant/internal/interfaces/scheduledmessage"
	serviceauthorization "family-assistant/internal/services/authorization"
	"family-assistant/pkg/config"
)

const (
	scheduledTestSpaceID    = "00000000-0000-0000-0000-000000000201"
	scheduledTestMemberID   = "00000000-0000-0000-0000-000000000202"
	scheduledTestChannelID  = "00000000-0000-0000-0000-000000000203"
	scheduledTestScheduleID = "00000000-0000-0000-0000-000000000204"
	scheduledTestSecret     = "identity-secret"
)

type scheduledMessageServiceStub struct {
	createInput dto.ScheduledMessageCreateInput
	createActor domainidentity.ActorContext
	createCalls int
	createErr   error
	listInput   dto.ScheduledMessageListInput
	listActor   domainidentity.ActorContext
	listCalls   int
	listOutput  []dto.ScheduledMessageOutput
	listErr     error
	updateInput dto.ScheduledMessageUpdateInput
	updateActor domainidentity.ActorContext
	updateSpace string
	updateID    string
	updateCalls int
	updateErr   error
	cancelActor domainidentity.ActorContext
	cancelSpace string
	cancelID    string
	cancelCalls int
	cancelErr   error
	unsubActor  domainidentity.ActorContext
	unsubSpace  string
	unsubID     string
	unsubCalls  int
	unsubErr    error
	targetActor domainidentity.ActorContext
	targetSpace string
	targetCalls int
	targets     []dto.ScheduledMessageTargetOutput
	targetsErr  error
	bindInput   dto.SpaceChannelBindInput
	bindActor   domainidentity.ActorContext
	bindCalls   int
	bindErr     error
}

var _ interfacescheduledmessage.Service = (*scheduledMessageServiceStub)(nil)

func (s *scheduledMessageServiceStub) Create(_ context.Context, actor domainidentity.ActorContext, input dto.ScheduledMessageCreateInput) (*domainscheduledmessage.Schedule, error) {
	s.createCalls++
	s.createActor, s.createInput = actor, input
	return scheduledMessageTestSchedule(), s.createErr
}

func (s *scheduledMessageServiceStub) List(_ context.Context, actor domainidentity.ActorContext, input dto.ScheduledMessageListInput) ([]dto.ScheduledMessageOutput, error) {
	s.listCalls++
	s.listActor, s.listInput = actor, input
	return s.listOutput, s.listErr
}

func (s *scheduledMessageServiceStub) Update(_ context.Context, actor domainidentity.ActorContext, spaceID, scheduleID string, input dto.ScheduledMessageUpdateInput) (*domainscheduledmessage.Schedule, error) {
	s.updateCalls++
	s.updateActor, s.updateSpace, s.updateID, s.updateInput = actor, spaceID, scheduleID, input
	return scheduledMessageTestSchedule(), s.updateErr
}

func (s *scheduledMessageServiceStub) Cancel(_ context.Context, actor domainidentity.ActorContext, spaceID, scheduleID string) (*domainscheduledmessage.Schedule, error) {
	s.cancelCalls++
	s.cancelActor, s.cancelSpace, s.cancelID = actor, spaceID, scheduleID
	return scheduledMessageTestSchedule(), s.cancelErr
}

func (s *scheduledMessageServiceStub) Unsubscribe(_ context.Context, actor domainidentity.ActorContext, spaceID, scheduleID string) (*domainscheduledmessage.Schedule, error) {
	s.unsubCalls++
	s.unsubActor, s.unsubSpace, s.unsubID = actor, spaceID, scheduleID
	return scheduledMessageTestSchedule(), s.unsubErr
}

func (s *scheduledMessageServiceStub) Targets(_ context.Context, actor domainidentity.ActorContext, spaceID string) ([]dto.ScheduledMessageTargetOutput, error) {
	s.targetCalls++
	s.targetActor, s.targetSpace = actor, spaceID
	return s.targets, s.targetsErr
}

func (s *scheduledMessageServiceStub) BindCurrentGroup(_ context.Context, actor domainidentity.ActorContext, input dto.SpaceChannelBindInput) (*domainscheduledmessage.SpaceChannel, error) {
	s.bindCalls++
	s.bindActor, s.bindInput = actor, input
	return &domainscheduledmessage.SpaceChannel{
		ID: scheduledTestChannelID, SpaceID: input.Space, Channel: input.CurrentChannel,
		Status: domainscheduledmessage.SpaceChannelStatusActive, BoundByMemberID: actor.MemberID,
	}, s.bindErr
}

func TestScheduledMessageToolsExposeSixOnlyWhenServiceConfigured(t *testing.T) {
	disabled := newScheduledMessageMCPServer(t, nil, scheduledResolver(scheduledTestSpaceID), "")
	disabledTools := scheduledToolNames(t, disabled.URL)
	if len(disabledTools) != 26 || !containsString(disabledTools, "space_channel_bind") {
		t.Fatalf("disabled tools/list = %v, want 26 tools including space_channel_bind", disabledTools)
	}
	for _, name := range disabledTools {
		if strings.HasPrefix(name, "scheduled_message_") {
			t.Fatalf("disabled tools/list exposes %s without a scheduled message service", name)
		}
	}

	enabledService := &scheduledMessageServiceStub{}
	enabled := newScheduledMessageMCPServer(t, enabledService, scheduledResolver(scheduledTestSpaceID), scheduledTestSecret)
	got := scheduledToolNames(t, enabled.URL)
	want := []string{
		"account_link", "account_register", "activity_create", "activity_create_with_reminder", "activity_delete", "activity_list", "activity_update",
		"identity_link", "identity_revoke", "invitation_accept", "invitation_create", "invitation_list", "invitation_revoke",
		"member_remove", "member_update_role", "reminder_complete", "reminder_create", "reminder_delete", "reminder_list", "reminder_update",
		"scheduled_message_cancel", "scheduled_message_create", "scheduled_message_list", "scheduled_message_targets", "scheduled_message_unsubscribe", "scheduled_message_update",
		"space_archive", "space_channel_bind", "space_create", "space_get_members", "space_list", "space_update",
	}
	if len(got) != 32 || strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("enabled tools/list = %v, want exactly %v", got, want)
	}
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func TestSpaceChannelBindWorksWithoutScheduledMessageService(t *testing.T) {
	binder := &scheduledMessageServiceStub{}
	resolver := scheduledResolver(scheduledTestSpaceID)
	handler := NewHTTPHandler(config.MCPConfig{ServerKey: "secret", IdentitySecret: scheduledTestSecret}, resolver, nil, nil, nil, nil, ToolServices{GroupBinder: binder})
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	initializeMCPServer(t, server.URL)

	names := scheduledToolNames(t, server.URL)
	if len(names) != 26 || !containsString(names, "space_channel_bind") || containsString(names, "scheduled_message_create") {
		t.Fatalf("tools/list = %v, want 26 tools with space_channel_bind and no scheduled_message tools", names)
	}
	identity := scheduledIdentity("nonce-bind-no-lane")
	response := callScheduledTool(t, server.URL, "space_channel_bind", map[string]any{"space": scheduledTestSpaceID}, &identity)
	if response.Result == nil || response.Result.IsError {
		t.Fatalf("space_channel_bind failed without scheduled message service: %+v", response)
	}
	if binder.bindCalls != 1 || binder.bindInput.Space != scheduledTestSpaceID || binder.bindInput.CurrentChatID != "120363@g.us" {
		t.Fatalf("bind input = %+v, calls=%d; want one verified group bind", binder.bindInput, binder.bindCalls)
	}
}

func TestSpaceChannelBindFailsClosedWithoutBinder(t *testing.T) {
	server := newScheduledMessageMCPServer(t, nil, scheduledResolver(scheduledTestSpaceID), scheduledTestSecret)
	identity := scheduledIdentity("nonce-bind-no-binder")
	response := callScheduledTool(t, server.URL, "space_channel_bind", map[string]any{"space": scheduledTestSpaceID}, &identity)
	if response.Result == nil || !response.Result.IsError {
		t.Fatalf("space_channel_bind without a binder = %+v, want a tool error", response)
	}
}

func TestScheduledMessageToolsRequireVerifiedEnvelope(t *testing.T) {
	service := &scheduledMessageServiceStub{}
	server := newScheduledMessageMCPServer(t, service, scheduledResolver(scheduledTestSpaceID), scheduledTestSecret)
	assertScheduledToolError(t, callScheduledTool(t, server.URL, "scheduled_message_targets", map[string]any{"space": scheduledTestSpaceID}, nil), ErrMCPUnauthenticated.Error())

	tampered := scheduledIdentity("nonce-tampered")
	tampered.Signature = strings.Repeat("0", len(tampered.Signature))
	assertScheduledToolError(t, callScheduledTool(t, server.URL, "scheduled_message_targets", map[string]any{"space": scheduledTestSpaceID}, &tampered), ErrMCPUnauthenticated.Error())
	if service.targetCalls != 0 {
		t.Fatalf("unsigned or tampered calls reached service %d times", service.targetCalls)
	}

	legacy := newScheduledMessageMCPServer(t, service, scheduledResolver(scheduledTestSpaceID), "")
	assertScheduledToolError(t, callScheduledTool(t, legacy.URL, "scheduled_message_targets", map[string]any{"space": scheduledTestSpaceID}, nil), ErrMCPUnauthenticated.Error())
	if service.targetCalls != 0 {
		t.Fatalf("plain profile header reached service %d times", service.targetCalls)
	}
}

func TestSpaceChannelBindUsesSignedCurrentGroup(t *testing.T) {
	service := &scheduledMessageServiceStub{}
	resolver := scheduledResolver(scheduledTestSpaceID)
	resolver.actor.Memberships[0].SpaceType = domainspace.TypePersonal
	resolver.actor.Memberships[0].RoleName = "space_owner"
	server := newScheduledMessageMCPServer(t, service, resolver, scheduledTestSecret)
	identity := scheduledIdentity("nonce-bind")
	response := callScheduledTool(t, server.URL, "space_channel_bind", map[string]any{
		"space": scheduledTestSpaceID, "chat_id": "999999999@g.us", "channel": "spoofed", "chat_type": "group",
	}, &identity)
	if response.Result == nil || response.Result.IsError {
		t.Fatalf("space_channel_bind failed: %+v", response)
	}
	if service.bindCalls != 1 || service.bindInput.Space != scheduledTestSpaceID || service.bindInput.CurrentChannel != "whatsapp" ||
		service.bindInput.CurrentChatID != "120363@g.us" || service.bindInput.CurrentChatType != "group" {
		t.Fatalf("bind input = %+v, calls=%d; want verified current group only", service.bindInput, service.bindCalls)
	}
	if resolver.profileID != "6285333320090@s.whatsapp.net" || service.bindActor.UserID != "user-1" ||
		service.bindActor.SpaceType != domainspace.TypePersonal || service.bindActor.RoleName != "space_owner" {
		t.Fatalf("bind actor came from untrusted profile: resolver=%q actor=%+v", resolver.profileID, service.bindActor)
	}
}

func TestScheduledMessageCreateParsesUUIDsAndRFC3339(t *testing.T) {
	service := &scheduledMessageServiceStub{}
	server := newScheduledMessageMCPServer(t, service, scheduledResolver(scheduledTestSpaceID), scheduledTestSecret)
	identity := scheduledIdentity("nonce-create")
	startAt := "2035-03-12T09:30:00Z"
	response := callScheduledTool(t, server.URL, "scheduled_message_create", map[string]any{
		"space": scheduledTestSpaceID, "target": "DM", "recipient_member_id": scheduledTestMemberID,
		"content_mode": "FIXED", "content": "A family note", "recurrence": "ONCE", "timezone": "UTC", "start_at": startAt,
	}, &identity)
	if response.Result == nil || response.Result.IsError {
		t.Fatalf("scheduled_message_create failed: %+v", response)
	}
	input := service.createInput
	if service.createCalls != 1 || service.createActor.UserID != "user-1" || input.Space != scheduledTestSpaceID || input.Target != domainscheduledmessage.TargetDM ||
		input.RecipientMemberID == nil || *input.RecipientMemberID != scheduledTestMemberID || input.ContentMode != domainscheduledmessage.ContentModeFixed ||
		input.Content != "A family note" || input.Recurrence != domainscheduledmessage.RecurrenceOnce || input.Timezone != "UTC" ||
		input.StartAt.Format(time.RFC3339) != startAt {
		t.Fatalf("create actor/input = %+v/%+v; calls=%d", service.createActor, input, service.createCalls)
	}
	var output dto.ScheduledMessageOutput
	if err := json.Unmarshal(response.Result.StructuredOutput, &output); err != nil || output.Content != "A family note" {
		t.Fatalf("create output = %+v, decode error=%v; expected DTO content", output, err)
	}
}

func TestScheduledMessageCreateRejectsMalformedTargetUUID(t *testing.T) {
	service := &scheduledMessageServiceStub{}
	server := newScheduledMessageMCPServer(t, service, scheduledResolver(scheduledTestSpaceID), scheduledTestSecret)
	identity := scheduledIdentity("nonce-create-invalid")
	response := callScheduledTool(t, server.URL, "scheduled_message_create", map[string]any{
		"space": scheduledTestSpaceID, "target": "DM", "recipient_member_id": "not-a-uuid",
		"content_mode": "FIXED", "content": "A family note", "recurrence": "ONCE", "timezone": "UTC", "start_at": "2035-03-12T09:30:00Z",
	}, &identity)
	assertScheduledToolError(t, response, ErrMCPInvalidInput.Error())
	if service.createCalls != 0 {
		t.Fatalf("malformed UUID reached service %d times", service.createCalls)
	}
}

func TestScheduledMessageListRejectsSpaceOutsideAuthenticatedMembership(t *testing.T) {
	service := &scheduledMessageServiceStub{}
	server := newScheduledMessageMCPServer(t, service, scheduledResolver(scheduledTestSpaceID), scheduledTestSecret)
	identity := scheduledIdentity("nonce-wrong-space")
	response := callScheduledTool(t, server.URL, "scheduled_message_list", map[string]any{
		"space": "00000000-0000-0000-0000-000000000299",
	}, &identity)
	assertScheduledToolError(t, response, ErrMCPNotFound.Error())
	if service.listCalls != 0 {
		t.Fatalf("unauthorized Space reached service %d times", service.listCalls)
	}
}

func TestScheduledMessageListAndTargetsUseSelectedSpace(t *testing.T) {
	service := &scheduledMessageServiceStub{
		listOutput: []dto.ScheduledMessageOutput{{
			ID:      scheduledTestScheduleID,
			History: []dto.ScheduledMessageDeliveryOutput{{ID: "delivery-1"}},
		}},
		targets: []dto.ScheduledMessageTargetOutput{{Kind: domainscheduledmessage.TargetDM, MemberID: stringPointer(scheduledTestMemberID), DisplayName: "Sam"}},
	}
	server := newScheduledMessageMCPServer(t, service, scheduledResolver(scheduledTestSpaceID), scheduledTestSecret)
	identity := scheduledIdentity("nonce-list")
	list := callScheduledTool(t, server.URL, "scheduled_message_list", map[string]any{
		"space": scheduledTestSpaceID, "schedule_id": scheduledTestScheduleID, "status": "ACTIVE",
	}, &identity)
	if list.Result == nil || list.Result.IsError || service.listCalls != 1 || service.listActor.SpaceID != scheduledTestSpaceID ||
		service.listInput.Space != scheduledTestSpaceID || service.listInput.ScheduleID == nil || *service.listInput.ScheduleID != scheduledTestScheduleID ||
		service.listInput.Status == nil || *service.listInput.Status != domainscheduledmessage.ScheduleStatusActive {
		t.Fatalf("list response/input = %+v/%+v, calls=%d", list.Result, service.listInput, service.listCalls)
	}
	var listOutput struct {
		Schedules []dto.ScheduledMessageOutput `json:"schedules"`
	}
	if err := json.Unmarshal(list.Result.StructuredOutput, &listOutput); err != nil || len(listOutput.Schedules) != 1 ||
		listOutput.Schedules[0].ID != scheduledTestScheduleID || len(listOutput.Schedules[0].History) != 1 ||
		listOutput.Schedules[0].History[0].ID != "delivery-1" {
		t.Fatalf("scheduled_message_list structured output = %s, decode error=%v", list.Result.StructuredOutput, err)
	}

	targets := callScheduledTool(t, server.URL, "scheduled_message_targets", map[string]any{"space": scheduledTestSpaceID}, ptrScheduledIdentity(scheduledIdentity("nonce-targets")))
	if targets.Result == nil || targets.Result.IsError || service.targetCalls != 1 || service.targetActor.SpaceID != scheduledTestSpaceID || service.targetSpace != scheduledTestSpaceID {
		t.Fatalf("targets response/space = %+v/%q, calls=%d", targets.Result, service.targetSpace, service.targetCalls)
	}
	var targetsOutput struct {
		Targets []dto.ScheduledMessageTargetOutput `json:"targets"`
	}
	if err := json.Unmarshal(targets.Result.StructuredOutput, &targetsOutput); err != nil || len(targetsOutput.Targets) != 1 ||
		targetsOutput.Targets[0].DisplayName != "Sam" {
		t.Fatalf("scheduled_message_targets structured output = %s, decode error=%v", targets.Result.StructuredOutput, err)
	}
}

func TestScheduledMessageListAndTargetsReturnEmptyArrays(t *testing.T) {
	server := newScheduledMessageMCPServer(t, &scheduledMessageServiceStub{}, scheduledResolver(scheduledTestSpaceID), scheduledTestSecret)
	list := callScheduledTool(t, server.URL, "scheduled_message_list", map[string]any{"space": scheduledTestSpaceID}, ptrScheduledIdentity(scheduledIdentity("nonce-empty-list")))
	targets := callScheduledTool(t, server.URL, "scheduled_message_targets", map[string]any{"space": scheduledTestSpaceID}, ptrScheduledIdentity(scheduledIdentity("nonce-empty-targets")))
	if list.Result == nil || list.Result.IsError || targets.Result == nil || targets.Result.IsError {
		t.Fatalf("empty list responses failed: list=%+v targets=%+v", list.Result, targets.Result)
	}
	var listOutput struct {
		Schedules []json.RawMessage `json:"schedules"`
	}
	if err := json.Unmarshal(list.Result.StructuredOutput, &listOutput); err != nil || listOutput.Schedules == nil || len(listOutput.Schedules) != 0 {
		t.Fatalf("empty scheduled_message_list output = %s, decode error=%v", list.Result.StructuredOutput, err)
	}
	var targetsOutput struct {
		Targets []json.RawMessage `json:"targets"`
	}
	if err := json.Unmarshal(targets.Result.StructuredOutput, &targetsOutput); err != nil || targetsOutput.Targets == nil || len(targetsOutput.Targets) != 0 {
		t.Fatalf("empty scheduled_message_targets output = %s, decode error=%v", targets.Result.StructuredOutput, err)
	}
}

func TestScheduledMessageUpdateMapsScheduleUUIDAndOptionalFields(t *testing.T) {
	service := &scheduledMessageServiceStub{}
	server := newScheduledMessageMCPServer(t, service, scheduledResolver(scheduledTestSpaceID), scheduledTestSecret)
	identity := scheduledIdentity("nonce-update")
	startAt := "2035-04-12T09:30:00Z"
	response := callScheduledTool(t, server.URL, "scheduled_message_update", map[string]any{
		"space": scheduledTestSpaceID, "schedule_id": scheduledTestScheduleID, "content_mode": "AI", "content": "celebrate spring",
		"recurrence": "DAILY", "timezone": "Asia/Jakarta", "start_at": startAt,
	}, &identity)
	if response.Result == nil || response.Result.IsError || service.updateCalls != 1 || service.updateActor.UserID != "user-1" ||
		service.updateSpace != scheduledTestSpaceID || service.updateID != scheduledTestScheduleID ||
		service.updateInput.ContentMode == nil || *service.updateInput.ContentMode != domainscheduledmessage.ContentModeAI ||
		service.updateInput.Content == nil || *service.updateInput.Content != "celebrate spring" ||
		service.updateInput.Recurrence == nil || *service.updateInput.Recurrence != domainscheduledmessage.RecurrenceDaily ||
		service.updateInput.Timezone == nil || *service.updateInput.Timezone != "Asia/Jakarta" || service.updateInput.StartAt == nil || service.updateInput.StartAt.Format(time.RFC3339) != startAt {
		t.Fatalf("update actor/space/id/input = %+v/%q/%q/%+v, calls=%d", service.updateActor, service.updateSpace, service.updateID, service.updateInput, service.updateCalls)
	}
}

func TestScheduledMessageCancelAndUnsubscribeUseAuthenticatedActor(t *testing.T) {
	service := &scheduledMessageServiceStub{}
	resolver := scheduledResolver(scheduledTestSpaceID)
	server := newScheduledMessageMCPServer(t, service, resolver, scheduledTestSecret)
	allowed := callScheduledTool(t, server.URL, "scheduled_message_cancel", map[string]any{
		"space": scheduledTestSpaceID, "schedule_id": scheduledTestScheduleID,
	}, ptrScheduledIdentity(scheduledIdentity("nonce-cancel-allowed")))
	if allowed.Result == nil || allowed.Result.IsError || service.cancelCalls != 1 || service.cancelActor.UserID != "user-1" ||
		service.cancelSpace != scheduledTestSpaceID || service.cancelID != scheduledTestScheduleID {
		t.Fatalf("allowed cancel result/args = %+v/%+v", allowed.Result, service)
	}

	service.cancelErr = serviceauthorization.ErrForbidden
	forbidden := callScheduledTool(t, server.URL, "scheduled_message_cancel", map[string]any{
		"space": scheduledTestSpaceID, "schedule_id": scheduledTestScheduleID,
	}, ptrScheduledIdentity(scheduledIdentity("nonce-cancel-forbidden")))
	assertScheduledToolError(t, forbidden, ErrMCPForbidden.Error())

	unsubscribed := callScheduledTool(t, server.URL, "scheduled_message_unsubscribe", map[string]any{
		"space": scheduledTestSpaceID, "schedule_id": scheduledTestScheduleID,
	}, ptrScheduledIdentity(scheduledIdentity("nonce-unsubscribe")))
	if unsubscribed.Result == nil || unsubscribed.Result.IsError || service.unsubCalls != 1 || service.unsubActor.UserID != "user-1" ||
		service.unsubSpace != scheduledTestSpaceID || service.unsubID != scheduledTestScheduleID || resolver.profileID != "6285333320090@s.whatsapp.net" {
		t.Fatalf("unsubscribe result/args = %+v/%+v; resolver=%q", unsubscribed.Result, service, resolver.profileID)
	}
}

func newScheduledMessageMCPServer(t *testing.T, service interfacescheduledmessage.Service, resolver *resolverStub, identitySecret string) *httptest.Server {
	t.Helper()
	services := ToolServices{ScheduledMessage: service}
	if service != nil {
		services.GroupBinder = service
	}
	handler := NewHTTPHandler(config.MCPConfig{ServerKey: "secret", IdentitySecret: identitySecret}, resolver, nil, nil, nil, nil, services)
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	initializeMCPServer(t, server.URL)
	return server
}

func scheduledToolNames(t *testing.T, serverURL string) []string {
	t.Helper()
	response := callMCPServer(t, serverURL, "profile", "secret", "tools/list", map[string]any{})
	if response.Error != nil || response.Result == nil {
		t.Fatalf("tools/list failed: %+v", response)
	}
	got := make([]string, 0, len(response.Result.Tools))
	for _, tool := range response.Result.Tools {
		got = append(got, tool.Name)
	}
	sort.Strings(got)
	return got
}

func scheduledResolver(spaceID string) *resolverStub {
	return &resolverStub{actor: domainidentity.ActorContext{
		UserID: "user-1", Memberships: []domainspace.ResolvedMembership{{
			ID: scheduledTestMemberID, SpaceID: spaceID, SpaceName: "Family", SpaceType: domainspace.TypeShared,
			UserID: "user-1", RoleID: "role-1", RoleName: "space_member", Status: domainspace.StatusActive,
		}},
	}}
}

func scheduledIdentity(nonce string) IdentityEnvelope {
	envelope := newSignedIdentityEnvelope(scheduledTestSecret, time.Now())
	envelope.Nonce = nonce
	envelope.Signature = signIdentityEnvelope(scheduledTestSecret, envelope)
	return envelope
}

func ptrScheduledIdentity(envelope IdentityEnvelope) *IdentityEnvelope { return &envelope }

func callScheduledTool(t *testing.T, serverURL, tool string, arguments map[string]any, identity *IdentityEnvelope) mcpServerResponse {
	t.Helper()
	if arguments == nil {
		arguments = map[string]any{}
	}
	if identity != nil {
		arguments[identityArgumentName] = identity
	}
	return callMCPServer(t, serverURL, "stale-profile", "secret", "tools/call", map[string]any{"name": tool, "arguments": arguments})
}

func assertScheduledToolError(t *testing.T, response mcpServerResponse, message string) {
	t.Helper()
	if response.Error != nil || response.Result == nil || !response.Result.IsError || len(response.Result.Content) == 0 || !strings.Contains(response.Result.Content[0].Text, message) {
		t.Fatalf("tool response = %+v, want error containing %q", response, message)
	}
}

func scheduledMessageTestSchedule() *domainscheduledmessage.Schedule {
	return &domainscheduledmessage.Schedule{
		ID: scheduledTestScheduleID, SpaceID: scheduledTestSpaceID, CreatedByMemberID: scheduledTestMemberID,
		Target: domainscheduledmessage.TargetDM, RecipientMemberID: stringPointer(scheduledTestMemberID),
		ContentMode: domainscheduledmessage.ContentModeFixed, Content: "A family note",
		Recurrence: domainscheduledmessage.RecurrenceOnce, Timezone: "UTC", StartAt: time.Date(2035, 3, 12, 9, 30, 0, 0, time.UTC),
		Status: domainscheduledmessage.ScheduleStatusActive,
	}
}

func stringPointer(value string) *string { return &value }
