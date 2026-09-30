package mcp

import (
	"context"
	"errors"
	"strings"
	"time"

	domainreminder "family-assistant/internal/domain/reminder"
	"family-assistant/internal/dto"
	interfaceidentity "family-assistant/internal/interfaces/identity"
	interfacereminder "family-assistant/internal/interfaces/reminder"
	serviceauthorization "family-assistant/internal/services/authorization"
	"family-assistant/utils"

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
	Status string `json:"status,omitempty" jsonschema:"PENDING, SENT, COMPLETED, or CANCELLED"` //nolint:misspell // persisted API enum; preserve spelling.
	From   string `json:"from,omitempty" jsonschema:"RFC3339 lower bound"`
	To     string `json:"to,omitempty" jsonschema:"RFC3339 upper bound"`
}

type ReminderCompleteInput struct {
	Space      string `json:"space,omitempty" jsonschema:"Space UUID or exact user-facing name; blank selects Personal Space"`
	ReminderID string `json:"reminder_id" jsonschema:"reminder UUID"`
}

type ReminderUpdateInput struct {
	Space            string  `json:"space,omitempty" jsonschema:"Space UUID or exact user-facing name; blank selects Personal Space"`
	ReminderID       string  `json:"reminder_id" jsonschema:"reminder UUID"`
	Title            *string `json:"title,omitempty" jsonschema:"optional replacement title"`
	Description      *string `json:"description,omitempty" jsonschema:"optional replacement details"`
	ScheduledAt      string  `json:"scheduled_at,omitempty" jsonschema:"optional RFC3339 scheduled time"`
	AssigneeMemberID *string `json:"assignee_member_id,omitempty" jsonschema:"optional assignee member UUID"`
	ClearAssignee    bool    `json:"clear_assignee,omitempty" jsonschema:"remove the current assignee"`
}

type ReminderDeleteInput struct {
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
	actor, err := selectSpaceActor(ctx, resolver, input.Space)
	if err != nil {
		return ReminderOutput{}, MapToolError(err)
	}
	if strings.TrimSpace(input.Title) == "" {
		return ReminderOutput{}, MapToolError(&serviceauthorization.ValidationError{Field: "title", Reason: "is required"})
	}
	scheduledAt, err := utils.ParseRFC3339(input.ScheduledAt, "scheduled_at", true)
	if err != nil {
		return ReminderOutput{}, MapToolError(err)
	}
	assignee, err := utils.ParseOptionalUUID(input.AssigneeMemberID, "assignee_member_id")
	if err != nil {
		return ReminderOutput{}, MapToolError(err)
	}
	if service == nil {
		return ReminderOutput{}, MapToolError(errors.New("reminder service is not configured"))
	}
	request, _ := ExternalRequestFromContext(ctx)
	deliveryProvider := strings.ToLower(strings.TrimSpace(request.Channel))
	deliveryTarget := strings.TrimSpace(request.ChatID)
	if deliveryTarget == "" {
		deliveryTarget = strings.TrimSpace(request.ExternalID)
	}
	reminder, err := service.Create(ctx, actor, dto.ReminderCreateInput{
		Space: actor.SpaceID, Title: input.Title, Description: input.Description, ScheduledAt: *scheduledAt, AssigneeMemberID: assignee,
		DeliveryProvider: deliveryProvider, DeliveryTarget: deliveryTarget,
	})
	if err != nil {
		return ReminderOutput{}, MapToolError(err)
	}
	return reminderOutput(reminder), nil
}

func ReminderList(ctx context.Context, resolver interfaceidentity.Resolver, service interfacereminder.ServiceReminderInterface, input ReminderListInput) ([]ReminderOutput, error) {
	actor, err := selectSpaceActor(ctx, resolver, input.Space)
	if err != nil {
		return nil, MapToolError(err)
	}
	if service == nil {
		return nil, MapToolError(errors.New("reminder service is not configured"))
	}
	status, err := domainreminder.ParseStatus(input.Status)
	if err != nil {
		return nil, MapToolError(err)
	}
	listInput := dto.ReminderListInput{Space: actor.SpaceID, Status: status}
	if listInput.From, err = utils.ParseRFC3339(input.From, "from", false); err != nil {
		return nil, MapToolError(err)
	}
	if listInput.To, err = utils.ParseRFC3339(input.To, "to", false); err != nil {
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
	actor, err := selectSpaceActor(ctx, resolver, input.Space)
	if err != nil {
		return ReminderOutput{}, MapToolError(err)
	}
	reminderID, err := utils.ParseUUID(input.ReminderID, "reminder_id", true)
	if err != nil {
		return ReminderOutput{}, MapToolError(err)
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

func ReminderUpdate(ctx context.Context, resolver interfaceidentity.Resolver, service interfacereminder.ServiceReminderInterface, input ReminderUpdateInput) (ReminderOutput, error) {
	actor, err := selectSpaceActor(ctx, resolver, input.Space)
	if err != nil {
		return ReminderOutput{}, MapToolError(err)
	}
	reminderID, err := utils.ParseUUID(input.ReminderID, "reminder_id", true)
	if err != nil {
		return ReminderOutput{}, MapToolError(err)
	}
	if input.Title == nil && input.Description == nil && strings.TrimSpace(input.ScheduledAt) == "" && input.AssigneeMemberID == nil && !input.ClearAssignee {
		return ReminderOutput{}, MapToolError(&serviceauthorization.ValidationError{Field: "update", Reason: "at least one field is required"})
	}
	scheduledAt, err := utils.ParseRFC3339(input.ScheduledAt, "scheduled_at", false)
	if err != nil {
		return ReminderOutput{}, MapToolError(err)
	}
	var assignee *string
	if input.AssigneeMemberID != nil {
		assignee, err = utils.ParseOptionalUUID(*input.AssigneeMemberID, "assignee_member_id")
		if err != nil {
			return ReminderOutput{}, MapToolError(err)
		}
	}
	if service == nil {
		return ReminderOutput{}, MapToolError(errors.New("reminder service is not configured"))
	}
	reminder, err := service.Update(ctx, actor, actor.SpaceID, reminderID, dto.ReminderUpdateInput{
		Title: input.Title, Description: input.Description, ScheduledAt: scheduledAt,
		AssigneeMemberID: assignee, ClearAssignee: input.ClearAssignee,
	})
	if err != nil {
		return ReminderOutput{}, MapToolError(err)
	}
	return reminderOutput(reminder), nil
}

func ReminderDelete(ctx context.Context, resolver interfaceidentity.Resolver, service interfacereminder.ServiceReminderInterface, input ReminderDeleteInput) (ReminderOutput, error) {
	actor, err := selectSpaceActor(ctx, resolver, input.Space)
	if err != nil {
		return ReminderOutput{}, MapToolError(err)
	}
	reminderID, err := utils.ParseUUID(input.ReminderID, "reminder_id", true)
	if err != nil {
		return ReminderOutput{}, MapToolError(err)
	}
	if service == nil {
		return ReminderOutput{}, MapToolError(errors.New("reminder service is not configured"))
	}
	reminder, err := service.Delete(ctx, actor, actor.SpaceID, reminderID)
	if err != nil {
		return ReminderOutput{}, MapToolError(err)
	}
	return reminderOutput(reminder), nil
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
	addTool(server, &mcpsdk.Tool{
		Name: "reminder_create", Description: "Create a reminder in an authorized Space.",
	}, func(ctx context.Context, input ReminderCreateInput) (ReminderOutput, error) {
		return ReminderCreate(ctx, resolver, service, input)
	})
	addTool(server, &mcpsdk.Tool{
		Name: "reminder_list", Description: "List reminders in an authorized Space.",
	}, func(ctx context.Context, input ReminderListInput) (ReminderListOutput, error) {
		output, err := ReminderList(ctx, resolver, service, input)
		return ReminderListOutput{Reminders: output}, err
	})
	addTool(server, &mcpsdk.Tool{
		Name: "reminder_complete", Description: "Complete an authorized Space reminder.",
	}, func(ctx context.Context, input ReminderCompleteInput) (ReminderOutput, error) {
		return ReminderComplete(ctx, resolver, service, input)
	})
	addTool(server, &mcpsdk.Tool{
		Name: "reminder_update", Description: "Update a pending reminder in an authorized Space.",
	}, func(ctx context.Context, input ReminderUpdateInput) (ReminderOutput, error) {
		return ReminderUpdate(ctx, resolver, service, input)
	})
	addTool(server, &mcpsdk.Tool{
		Name: "reminder_delete", Description: "Cancel and remove a reminder from an authorized Space.",
	}, func(ctx context.Context, input ReminderDeleteInput) (ReminderOutput, error) {
		return ReminderDelete(ctx, resolver, service, input)
	})
}
