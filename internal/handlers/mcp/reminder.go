package mcp

import (
	"context"
	"errors"
	"strings"
	"time"

	domainreminder "family-assistant/internal/domain/reminder"
	serviceauthorization "family-assistant/internal/services/authorization"
	serviceidentity "family-assistant/internal/services/identity"
	servicereminder "family-assistant/internal/services/reminder"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

type ReminderCreateInput struct {
	Title          string `json:"title" jsonschema:"the reminder title"`
	Description    string `json:"description,omitempty" jsonschema:"optional details"`
	ScheduledAt    string `json:"scheduled_at" jsonschema:"RFC3339 scheduled time"`
	Scope          string `json:"scope,omitempty" jsonschema:"PERSONAL or FAMILY"`
	TargetMemberID string `json:"target_member_id,omitempty" jsonschema:"optional family member target"`
}

type ReminderListInput struct {
	Scope          string `json:"scope,omitempty" jsonschema:"PERSONAL or FAMILY"`
	Status         string `json:"status,omitempty" jsonschema:"PENDING, COMPLETED, or CANCELLED"` //nolint:misspell // persisted API enum; preserve spelling.
	From           string `json:"from,omitempty" jsonschema:"RFC3339 lower bound"`
	To             string `json:"to,omitempty" jsonschema:"RFC3339 upper bound"`
	TargetMemberID string `json:"target_member_id,omitempty" jsonschema:"optional family member target"`
}

type ReminderCompleteInput struct {
	ReminderID string `json:"reminder_id" jsonschema:"reminder UUID"`
}

type ReminderOutput struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	Scope       string `json:"scope"`
	Status      string `json:"status"`
	ScheduledAt string `json:"scheduled_at"`
}

func ReminderCreate(ctx context.Context, service servicereminder.Service, input ReminderCreateInput) (ReminderOutput, error) {
	actor, err := reminderActor(ctx)
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
	if service == nil {
		return ReminderOutput{}, MapToolError(errors.New("reminder service is not configured"))
	}
	createInput := servicereminder.CreateInput{
		Title: input.Title, Description: input.Description, ScheduledAt: *scheduledAt,
		Scope: domainreminder.Scope(strings.TrimSpace(input.Scope)), TargetMemberID: optionalReminderID(input.TargetMemberID),
	}
	reminder, err := service.Create(ctx, actor, createInput)
	if err != nil {
		return ReminderOutput{}, MapToolError(err)
	}
	return reminderOutput(reminder), nil
}

func ReminderList(ctx context.Context, service servicereminder.Service, input ReminderListInput) ([]ReminderOutput, error) {
	actor, err := reminderActor(ctx)
	if err != nil {
		return nil, MapToolError(err)
	}
	if service == nil {
		return nil, MapToolError(errors.New("reminder service is not configured"))
	}
	listInput := servicereminder.ListInput{
		Scope: scopeFilter(input.Scope), Status: statusFilter(input.Status),
		TargetMemberID: optionalReminderID(input.TargetMemberID),
	}
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

func ReminderComplete(ctx context.Context, service servicereminder.Service, input ReminderCompleteInput) (ReminderOutput, error) {
	actor, err := reminderActor(ctx)
	if err != nil {
		return ReminderOutput{}, MapToolError(err)
	}
	if service == nil {
		return ReminderOutput{}, MapToolError(errors.New("reminder service is not configured"))
	}
	reminder, err := service.Complete(ctx, actor, input.ReminderID)
	if err != nil {
		return ReminderOutput{}, MapToolError(err)
	}
	return reminderOutput(reminder), nil
}

func reminderActor(ctx context.Context) (serviceidentity.ActorContext, error) {
	actor, ok := ActorFromContext(ctx)
	if !ok || strings.TrimSpace(actor.UserID) == "" || strings.TrimSpace(actor.MemberID) == "" || strings.TrimSpace(actor.FamilyID) == "" {
		return serviceidentity.ActorContext{}, serviceidentity.ErrUnauthenticated
	}
	return actor, nil
}

func optionalReminderID(value string) *string {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	return &value
}

func scopeFilter(value string) *domainreminder.Scope {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	scope := domainreminder.Scope(value)
	return &scope
}

func statusFilter(value string) *domainreminder.Status {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	status := domainreminder.Status(value)
	return &status
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

func reminderOutput(reminder *domainreminder.Reminder) ReminderOutput {
	if reminder == nil {
		return ReminderOutput{}
	}
	return ReminderOutput{
		ID: reminder.ID, Title: reminder.Title, Scope: string(reminder.Scope), Status: string(reminder.Status),
		ScheduledAt: reminder.ScheduledAt.Format(time.RFC3339Nano),
	}
}

func registerReminderTools(server *mcpsdk.Server, service servicereminder.Service, resolver any) {
	mcpsdk.AddTool(server, &mcpsdk.Tool{
		Name: "reminder_create", Description: "Create a family reminder.",
	}, func(ctx context.Context, _ *mcpsdk.CallToolRequest, input ReminderCreateInput) (*mcpsdk.CallToolResult, ReminderOutput, error) {
		actor, err := RequireActor(ctx, resolver)
		if err != nil {
			return nil, ReminderOutput{}, MapToolError(err)
		}
		output, err := ReminderCreate(WithActorContext(ctx, actor), service, input)
		return nil, output, err
	})
	mcpsdk.AddTool(server, &mcpsdk.Tool{
		Name: "reminder_list", Description: "List authorized family reminders.",
	}, func(ctx context.Context, _ *mcpsdk.CallToolRequest, input ReminderListInput) (*mcpsdk.CallToolResult, []ReminderOutput, error) {
		actor, err := RequireActor(ctx, resolver)
		if err != nil {
			return nil, nil, MapToolError(err)
		}
		output, err := ReminderList(WithActorContext(ctx, actor), service, input)
		return nil, output, err
	})
	mcpsdk.AddTool(server, &mcpsdk.Tool{
		Name: "reminder_complete", Description: "Complete an authorized family reminder.",
	}, func(ctx context.Context, _ *mcpsdk.CallToolRequest, input ReminderCompleteInput) (*mcpsdk.CallToolResult, ReminderOutput, error) {
		actor, err := RequireActor(ctx, resolver)
		if err != nil {
			return nil, ReminderOutput{}, MapToolError(err)
		}
		output, err := ReminderComplete(WithActorContext(ctx, actor), service, input)
		return nil, output, err
	})
}
