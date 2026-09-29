package mcp

import (
	"encoding/json"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	domainactivity "family-assistant/internal/domain/activity"
	domainidentity "family-assistant/internal/domain/identity"
	domainpermission "family-assistant/internal/domain/permission"
	domainspace "family-assistant/internal/domain/space"
	interfacereminder "family-assistant/internal/interfaces/reminder"
	"family-assistant/pkg/config"
)

func TestActivityCreateWithReminderCreatesActivityAndReminder(t *testing.T) {
	for _, tt := range []struct {
		name         string
		reminder     map[string]any
		wantTime     time.Time
		wantAssignee string
	}{
		{
			name: "absolute schedule",
			reminder: map[string]any{
				"title": "Follow up", "description": "Check feeding", "scheduled_at": "2026-09-29T11:00:00+07:00",
				"assignee_member_id": "00000000-0000-0000-0000-000000000302",
			},
			wantTime:     time.Date(2026, 9, 29, 11, 0, 0, 0, time.FixedZone("UTC+07", 7*60*60)),
			wantAssignee: "00000000-0000-0000-0000-000000000302",
		},
		{
			name:     "relative schedule",
			reminder: map[string]any{"title": "Follow up", "after_minutes": 15},
			wantTime: time.Date(2026, 9, 29, 10, 15, 0, 0, time.FixedZone("UTC+07", 7*60*60)),
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			activityService := &mcpActivityServiceStub{created: &domainactivity.Activity{
				ID: "activity-1", SpaceID: mcpSpaceID, Kind: "feeding", Note: "15 minutes",
			}}
			reminderService := &reminderToolServiceStub{}
			resolver := activityReminderResolver()
			server := activityReminderHTTPServer(t, resolver, activityService, reminderService, "identity-secret")

			identity := newSignedIdentityEnvelope("identity-secret", time.Now())
			identity.Nonce = "activity-reminder-" + strings.ReplaceAll(tt.name, " ", "-")
			identity.ChatID = "120363@g.us"
			identity.ChatType = "group"
			identity.Signature = signIdentityEnvelope("identity-secret", identity)
			arguments := activityReminderArguments(tt.reminder)
			arguments[identityArgumentName] = identity
			response := callActivityReminderTool(t, server.URL, arguments)
			if response.Error != nil || response.Result == nil || response.Result.IsError {
				t.Fatalf("activity_create_with_reminder response = %+v", response)
			}

			var output struct {
				Status   string                   `json:"status"`
				Activity *domainactivity.Activity `json:"activity"`
				Reminder *ReminderOutput          `json:"reminder"`
			}
			if err := json.Unmarshal(response.Result.StructuredOutput, &output); err != nil {
				t.Fatalf("decode output %s: %v", response.Result.StructuredOutput, err)
			}
			if output.Status != "created" || output.Activity == nil || output.Activity.ID != "activity-1" || output.Reminder == nil || output.Reminder.ID != mcpReminderID {
				t.Fatalf("output = %+v", output)
			}
			if output.Activity.SpaceID != mcpSpaceID || output.Reminder.SpaceID != output.Activity.SpaceID || activityService.createInput.Space != output.Activity.SpaceID || reminderService.createInput.Space != output.Activity.SpaceID {
				t.Fatalf("activity/reminder spaces = activity:%q activity input:%q reminder:%q reminder input:%q", output.Activity.SpaceID, activityService.createInput.Space, output.Reminder.SpaceID, reminderService.createInput.Space)
			}
			if got, err := time.Parse(time.RFC3339Nano, output.Reminder.ScheduledAt); err != nil || !got.Equal(tt.wantTime) {
				t.Fatalf("reminder scheduled_at = %q, want %s (parse error %v)", output.Reminder.ScheduledAt, tt.wantTime, err)
			}
			if reminderService.createInput.AssigneeMemberID == nil && tt.wantAssignee != "" || reminderService.createInput.AssigneeMemberID != nil && *reminderService.createInput.AssigneeMemberID != tt.wantAssignee {
				t.Fatalf("reminder assignee = %v, want %q", reminderService.createInput.AssigneeMemberID, tt.wantAssignee)
			}
			if activityService.createCalls != 1 || reminderService.createCalls != 1 {
				t.Fatalf("service calls activity=%d reminder=%d, want one each", activityService.createCalls, reminderService.createCalls)
			}
			if reminderService.createInput.DeliveryProvider != "whatsapp" || reminderService.createInput.DeliveryTarget != "120363@g.us" || resolver.profileID != identity.ExternalID {
				t.Fatalf("trusted group identity not forwarded: reminder input=%+v resolved profile=%q", reminderService.createInput, resolver.profileID)
			}
		})
	}
}

func TestActivityCreateWithReminderRejectsInvalidSchedulingBeforeWrites(t *testing.T) {
	validTime := "2026-09-29T11:00:00Z"
	for _, tt := range []struct {
		name       string
		reminder   map[string]any
		occurredAt string
	}{
		{name: "missing", reminder: map[string]any{"title": "Follow up"}},
		{name: "both", reminder: map[string]any{"title": "Follow up", "scheduled_at": validTime, "after_minutes": 1}},
		{name: "zero", reminder: map[string]any{"title": "Follow up", "after_minutes": 0}},
		{name: "negative", reminder: map[string]any{"title": "Follow up", "after_minutes": -1}},
		{name: "duration overflow", reminder: map[string]any{"title": "Follow up", "after_minutes": int64(1<<63 - 1)}},
		{name: "integer overflow", reminder: map[string]any{"title": "Follow up", "after_minutes": json.Number("9223372036854775808")}},
		{name: "RFC3339 range overflow", reminder: map[string]any{"title": "Follow up", "after_minutes": 1}, occurredAt: "9999-12-31T23:59:59Z"},
		{name: "malformed absolute time", reminder: map[string]any{"title": "Follow up", "scheduled_at": "tomorrow"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			activityService := &mcpActivityServiceStub{created: &domainactivity.Activity{ID: "activity-1", SpaceID: mcpSpaceID}}
			reminderService := &reminderToolServiceStub{}
			server := activityReminderHTTPServer(t, activityReminderResolver(), activityService, reminderService, "")

			arguments := activityReminderArguments(tt.reminder)
			if tt.occurredAt != "" {
				arguments["activity"].(map[string]any)["occurred_at"] = tt.occurredAt
			}
			response := callActivityReminderTool(t, server.URL, arguments)
			requireMCPToolError(t, response)
			if activityService.createCalls != 0 || reminderService.createCalls != 0 {
				t.Fatalf("invalid schedule reached services: activity=%d reminder=%d", activityService.createCalls, reminderService.createCalls)
			}
		})
	}
}

func TestActivityCreateWithReminderRejectsInvalidReminderBeforeActivity(t *testing.T) {
	validSchedule := "2026-09-29T11:00:00Z"
	for _, reminder := range []struct {
		name  string
		input map[string]any
	}{
		{name: "title", input: map[string]any{"title": " ", "scheduled_at": validSchedule}},
		{name: "assignee UUID", input: map[string]any{"title": "Follow up", "scheduled_at": validSchedule, "assignee_member_id": "not-a-uuid"}},
	} {
		t.Run(reminder.name, func(t *testing.T) {
			activityService := &mcpActivityServiceStub{created: &domainactivity.Activity{ID: "activity-1", SpaceID: mcpSpaceID}}
			reminderService := &reminderToolServiceStub{}
			server := activityReminderHTTPServer(t, activityReminderResolver(), activityService, reminderService, "")

			response := callActivityReminderTool(t, server.URL, activityReminderArguments(reminder.input))
			requireMCPToolError(t, response)
			if activityService.createCalls != 0 || reminderService.createCalls != 0 {
				t.Fatalf("invalid reminder reached services: activity=%d reminder=%d", activityService.createCalls, reminderService.createCalls)
			}
		})
	}
}

func TestActivityCreateWithReminderReportsPartialSuccess(t *testing.T) {
	activityService := &mcpActivityServiceStub{created: &domainactivity.Activity{ID: "activity-1", SpaceID: mcpSpaceID}}
	reminderService := &reminderToolServiceStub{createErr: errors.New("private persistence detail")}
	server := activityReminderHTTPServer(t, activityReminderResolver(), activityService, reminderService, "")
	response := callActivityReminderTool(t, server.URL, activityReminderArguments(map[string]any{
		"title": "Follow up", "scheduled_at": "2026-09-29T11:00:00Z",
	}))
	if response.Error != nil || response.Result == nil || response.Result.IsError {
		t.Fatalf("partial-success response = %+v", response)
	}
	var output struct {
		Status        string                   `json:"status"`
		Activity      *domainactivity.Activity `json:"activity"`
		Reminder      *ReminderOutput          `json:"reminder"`
		ReminderError string                   `json:"reminder_error"`
	}
	if err := json.Unmarshal(response.Result.StructuredOutput, &output); err != nil {
		t.Fatalf("decode output: %v", err)
	}
	if output.Status != "partial_success" || output.Activity == nil || output.Activity.ID != "activity-1" || output.Reminder != nil || output.ReminderError != ErrMCPInternal.Error() {
		t.Fatalf("partial-success output = %+v", output)
	}
	if strings.Contains(output.ReminderError, "private persistence detail") || activityService.createCalls != 1 || reminderService.createCalls != 1 {
		t.Fatalf("unsafe or unexpected partial success: output=%+v activity calls=%d reminder calls=%d", output, activityService.createCalls, reminderService.createCalls)
	}
}

func TestActivityCreateWithReminderSkipsReminderWhenActivityFails(t *testing.T) {
	activityService := &mcpActivityServiceStub{createErr: errors.New("private activity detail")}
	reminderService := &reminderToolServiceStub{}
	server := activityReminderHTTPServer(t, activityReminderResolver(), activityService, reminderService, "")
	response := callActivityReminderTool(t, server.URL, activityReminderArguments(map[string]any{
		"title": "Follow up", "scheduled_at": "2026-09-29T11:00:00Z",
	}))
	requireMCPToolError(t, response)
	if activityService.createCalls != 1 || reminderService.createCalls != 0 {
		t.Fatalf("service calls activity=%d reminder=%d, want one activity and no reminder", activityService.createCalls, reminderService.createCalls)
	}
}

func TestActivityCreateWithReminderRequiresReminderServiceBeforeActivity(t *testing.T) {
	activityService := &mcpActivityServiceStub{created: &domainactivity.Activity{ID: "activity-1", SpaceID: mcpSpaceID}}
	server := activityReminderHTTPServer(t, activityReminderResolver(), activityService, nil, "")
	response := callActivityReminderTool(t, server.URL, activityReminderArguments(map[string]any{
		"title": "Follow up", "scheduled_at": "2026-09-29T11:00:00Z",
	}))
	requireMCPToolError(t, response)
	if activityService.createCalls != 0 {
		t.Fatalf("activity service called %d times without reminder service", activityService.createCalls)
	}
}

func activityReminderHTTPServer(t *testing.T, resolver *resolverStub, activityService *mcpActivityServiceStub, reminderService interfacereminder.ServiceReminderInterface, identitySecret string) *httptest.Server {
	t.Helper()
	handler := NewHTTPHandler(config.MCPConfig{ServerKey: "secret", IdentitySecret: identitySecret}, resolver, nil, nil, nil, reminderService, ToolServices{Activity: activityService})
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	initializeMCPServer(t, server.URL)
	return server
}

func activityReminderResolver() *resolverStub {
	return &resolverStub{
		actor: domainidentity.ActorContext{UserID: "user-1", Memberships: []domainspace.ResolvedMembership{{
			ID: mcpMemberID, SpaceID: mcpSpaceID, SpaceName: "Jane", SpaceType: domainspace.TypePersonal,
			UserID: "user-1", RoleID: "role-personal", RoleName: "space_owner", Status: domainspace.StatusActive,
		}}},
		permissions: []domainpermission.Permission{
			{Resource: "activities", Action: "create"}, {Resource: "reminders", Action: "create"},
		},
	}
}

func activityReminderArguments(reminder map[string]any) map[string]any {
	return map[string]any{
		"activity": map[string]any{
			"kind": "feeding", "note": "15 minutes", "occurred_at": "2026-09-29T10:00:00+07:00",
		},
		"reminder": reminder,
	}
}

func callActivityReminderTool(t *testing.T, serverURL string, arguments map[string]any) mcpServerResponse {
	t.Helper()
	return callMCPServer(t, serverURL, "profile", "secret", "tools/call", map[string]any{
		"name": "activity_create_with_reminder", "arguments": arguments,
	})
}

func requireMCPToolError(t *testing.T, response mcpServerResponse) {
	t.Helper()
	if response.Error != nil || response.Result == nil || !response.Result.IsError {
		t.Fatalf("response = %+v, want MCP tool error", response)
	}
}
