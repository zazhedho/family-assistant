package mcp

import (
	"context"
	"errors"
	"strings"
	"time"

	domainidentity "family-assistant/internal/domain/identity"
	domainreminder "family-assistant/internal/domain/reminder"
	"family-assistant/internal/dto"
	interfaceidentity "family-assistant/internal/interfaces/identity"
	interfacereminder "family-assistant/internal/interfaces/reminder"
	serviceauthorization "family-assistant/internal/services/authorization"
	serviceidentity "family-assistant/internal/services/identity"

	"github.com/google/uuid"
	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

type ReminderCreateInput struct {
	Space            string `json:"space,omitempty" jsonschema:"Space UUID or exact user-facing name; blank selects Personal Space"`
	Title            string `json:"title" jsonschema:"the reminder title"`
	Description      string `json:"description,omitempty" jsonschema:"optional details"`
	ScheduledAt      string `json:"scheduled_at" jsonschema:"RFC3339 scheduled time"`
	AssigneeMemberID string `json:"assignee_member_id,omitempty" jsonschema:"optional assignee member UUID"`
}

type ReminderListInput struct {
	Space  string `json:"space,omitempty" jsonschema:"Space UUID or exact user-facing name; blank selects Personal Space"`
	Status string `json:"status,omitempty" jsonschema:"PENDING, COMPLETED, or CANCELLED"` //nolint:misspell // persisted API enum; preserve spelling.
	From   string `json:"from,omitempty" jsonschema:"RFC3339 lower bound"`
	To     string `json:"to,omitempty" jsonschema:"RFC3339 upper bound"`
}

type ReminderCompleteInput struct {
	Space      string `json:"space,omitempty" jsonschema:"Space UUID or exact user-facing name; blank selects Personal Space"`
	ReminderID string `json:"reminder_id" jsonschema:"reminder UUID"`
}

type ReminderOutput struct {
	ID               string  `json:"id"`
	SpaceID          string  `json:"space_id"`
	Title            string  `json:"title"`
	Description      string  `json:"description,omitempty"`
	AssigneeMemberID *string `json:"assignee_member_id,omitempty"`
	Status           string  `json:"status"`
	ScheduledAt      string  `json:"scheduled_at"`
}

type ReminderListOutput struct {
	Reminders []ReminderOutput `json:"reminders"`
}

func ReminderCreate(ctx context.Context, resolver interfaceidentity.Resolver, service interfacereminder.ServiceReminderInterface, input ReminderCreateInput) (ReminderOutput, error) {
	actor, err := reminderActor(ctx, resolver, input.Space)
	if err != nil {
		return ReminderOutput{}, MapToolError(err)
	}
	if strings.TrimSpace(input.Title) == "" {
		return ReminderOutput{}, MapToolError(&serviceauthorization.ValidationError{Field: "title", Reason: "is required"})
	}
	scheduledAt, err := parseReminderTime(input.ScheduledAt, "scheduled_at", true)
	if err != nil {
		return ReminderOutput{}, MapToolError(err)
	}
	assignee, err := optionalReminderUUID(input.AssigneeMemberID, "assignee_member_id")
	if err != nil {
		return ReminderOutput{}, MapToolError(err)
	}
	if service == nil {
		return ReminderOutput{}, MapToolError(errors.New("reminder service is not configured"))
	}
	reminder, err := service.Create(ctx, actor, dto.ReminderCreateInput{
		Space: actor.SpaceID, Title: input.Title, Description: input.Description, ScheduledAt: *scheduledAt, AssigneeMemberID: assignee,
	})
	if err != nil {
		return ReminderOutput{}, MapToolError(err)
	}
	return reminderOutput(reminder), nil
}

func ReminderList(ctx context.Context, resolver interfaceidentity.Resolver, service interfacereminder.ServiceReminderInterface, input ReminderListInput) ([]ReminderOutput, error) {
	actor, err := reminderActor(ctx, resolver, input.Space)
	if err != nil {
		return nil, MapToolError(err)
	}
	if service == nil {
		return nil, MapToolError(errors.New("reminder service is not configured"))
	}
	status, err := statusFilter(input.Status)
	if err != nil {
		return nil, MapToolError(err)
	}
	listInput := dto.ReminderListInput{Space: actor.SpaceID, Status: status}
	if listInput.From, err = parseReminderTime(input.From, "from", false); err != nil {
		return nil, MapToolError(err)
	}
	if listInput.To, err = parseReminderTime(input.To, "to", false); err != nil {
		return nil, MapToolError(err)
	}
	reminders, err := service.List(ctx, actor, listInput)
	if err != nil {
		return nil, MapToolError(err)
	}
	result := make([]ReminderOutput, 0, len(reminders))
	for i := range reminders {
		result = append(result, reminderOutput(&reminders[i]))
	}
	return result, nil
}

func ReminderComplete(ctx context.Context, resolver interfaceidentity.Resolver, service interfacereminder.ServiceReminderInterface, input ReminderCompleteInput) (ReminderOutput, error) {
	actor, err := reminderActor(ctx, resolver, input.Space)
	if err != nil {
		return ReminderOutput{}, MapToolError(err)
	}
	reminderID := strings.TrimSpace(input.ReminderID)
	if _, err := uuid.Parse(reminderID); err != nil {
		return ReminderOutput{}, MapToolError(&serviceauthorization.ValidationError{Field: "reminder_id", Reason: "must be a valid UUID"})
	}
	if service == nil {
		return ReminderOutput{}, MapToolError(errors.New("reminder service is not configured"))
	}
	reminder, err := service.Complete(ctx, actor, actor.SpaceID, reminderID)
	if err != nil {
		return ReminderOutput{}, MapToolError(err)
	}
	return reminderOutput(reminder), nil
}

func reminderActor(ctx context.Context, resolver interfaceidentity.Resolver, selector string) (domainidentity.ActorContext, error) {
	actor, err := RequireActor(ctx, resolver)
	if err != nil {
		return domainidentity.ActorContext{}, err
	}
	return serviceidentity.SelectSpace(ctx, actor, selector, selectorPermissions(resolver))
}

func optionalReminderUUID(value, field string) (*string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil, nil
	}
	if _, err := uuid.Parse(value); err != nil {
		return nil, &serviceauthorization.ValidationError{Field: field, Reason: "must be a valid UUID"}
	}
	return &value, nil
}

func parseReminderTime(value, field string, required bool) (*time.Time, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		if required {
			return nil, &serviceauthorization.ValidationError{Field: field, Reason: "is required"}
		}
		return nil, nil
	}
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return nil, &serviceauthorization.ValidationError{Field: field, Reason: "must be RFC3339"}
	}
	return &parsed, nil
}

func statusFilter(value string) (*domainreminder.Status, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil, nil
	}
	status := domainreminder.Status(value)
	switch status {
	case domainreminder.StatusPending, domainreminder.StatusCompleted, domainreminder.StatusCancelled:
		return &status, nil
	default:
		return nil, &serviceauthorization.ValidationError{Field: "status", Reason: "is invalid"}
	}
}

func reminderOutput(reminder *domainreminder.Reminder) ReminderOutput {
	if reminder == nil {
		return ReminderOutput{}
	}
	return ReminderOutput{
		ID: reminder.ID, SpaceID: reminder.SpaceID, Title: reminder.Title, Description: reminder.Description,
		AssigneeMemberID: reminder.AssigneeMemberID, Status: string(reminder.Status), ScheduledAt: reminder.ScheduledAt.Format(time.RFC3339Nano),
	}
}

func registerReminderTools(server *mcpsdk.Server, service interfacereminder.ServiceReminderInterface, resolver interfaceidentity.Resolver) {
	mcpsdk.AddTool(server, &mcpsdk.Tool{
		Name: "reminder_create", Description: "Create a reminder in an authorized Space.",
	}, func(ctx context.Context, _ *mcpsdk.CallToolRequest, input ReminderCreateInput) (*mcpsdk.CallToolResult, ReminderOutput, error) {
		output, err := ReminderCreate(ctx, resolver, service, input)
		return nil, output, err
	})
	mcpsdk.AddTool(server, &mcpsdk.Tool{
		Name: "reminder_list", Description: "List reminders in an authorized Space.",
	}, func(ctx context.Context, _ *mcpsdk.CallToolRequest, input ReminderListInput) (*mcpsdk.CallToolResult, ReminderListOutput, error) {
		output, err := ReminderList(ctx, resolver, service, input)
		return nil, ReminderListOutput{Reminders: output}, err
	})
	mcpsdk.AddTool(server, &mcpsdk.Tool{
		Name: "reminder_complete", Description: "Complete an authorized Space reminder.",
	}, func(ctx context.Context, _ *mcpsdk.CallToolRequest, input ReminderCompleteInput) (*mcpsdk.CallToolResult, ReminderOutput, error) {
		output, err := ReminderComplete(ctx, resolver, service, input)
		return nil, output, err
	})
}
