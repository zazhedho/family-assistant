package mcp

import (
	"context"
	"errors"
	"strings"
	"time"

	domainactivity "family-assistant/internal/domain/activity"
	domainidentity "family-assistant/internal/domain/identity"
	"family-assistant/internal/dto"
	interfaceactivity "family-assistant/internal/interfaces/activity"
	interfaceidentity "family-assistant/internal/interfaces/identity"
	interfacereminder "family-assistant/internal/interfaces/reminder"
	serviceauthorization "family-assistant/internal/services/authorization"
	serviceidentity "family-assistant/internal/services/identity"

	"github.com/google/uuid"
	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

type ActivityCreateInput struct {
	Space      string `json:"space,omitempty" jsonschema:"Space UUID or exact user-facing name; blank selects Personal Space"`
	Kind       string `json:"kind" jsonschema:"short event type such as breastfeeding, diaper, sleep, or note"`
	Note       string `json:"note" jsonschema:"human-readable details"`
	OccurredAt string `json:"occurred_at" jsonschema:"RFC3339 event time"`
}

type ActivityReminderInput struct {
	Title            string `json:"title"`
	Description      string `json:"description,omitempty"`
	ScheduledAt      string `json:"scheduled_at,omitempty"`
	AfterMinutes     *int64 `json:"after_minutes,omitempty"`
	AssigneeMemberID string `json:"assignee_member_id,omitempty"`
}

type ActivityCreateWithReminderInput struct {
	Activity ActivityCreateInput   `json:"activity"`
	Reminder ActivityReminderInput `json:"reminder"`
}

type ActivityCreateWithReminderOutput struct {
	Status        string                   `json:"status"`
	Activity      *domainactivity.Activity `json:"activity"`
	Reminder      *ReminderOutput          `json:"reminder,omitempty"`
	ReminderError string                   `json:"reminder_error,omitempty"`
}

type ActivityListInput struct {
	Space string `json:"space,omitempty" jsonschema:"Space UUID or exact user-facing name; blank selects Personal Space"`
	Kind  string `json:"kind,omitempty" jsonschema:"optional event type filter"`
	From  string `json:"from,omitempty" jsonschema:"optional RFC3339 lower bound"`
	To    string `json:"to,omitempty" jsonschema:"optional RFC3339 upper bound"`
	Limit int    `json:"limit,omitempty" jsonschema:"optional maximum results, capped at 100"`
}

type ActivityUpdateInput struct {
	Space      string  `json:"space,omitempty" jsonschema:"Space UUID or exact user-facing name; blank selects Personal Space"`
	ActivityID string  `json:"activity_id" jsonschema:"activity UUID"`
	Kind       *string `json:"kind,omitempty" jsonschema:"optional replacement event type"`
	Note       *string `json:"note,omitempty" jsonschema:"optional replacement details"`
	OccurredAt string  `json:"occurred_at,omitempty" jsonschema:"optional RFC3339 event time"`
}

type ActivityDeleteInput struct {
	Space      string `json:"space,omitempty" jsonschema:"Space UUID or exact user-facing name; blank selects Personal Space"`
	ActivityID string `json:"activity_id" jsonschema:"activity UUID"`
}

type ActivityListOutput struct {
	Activities []domainactivity.Activity `json:"activities"`
}

func ActivityCreate(ctx context.Context, resolver interfaceidentity.Resolver, service interfaceactivity.ServiceActivityInterface, input ActivityCreateInput) (*domainactivity.Activity, error) {
	actor, err := activityActor(ctx, resolver, input.Space)
	if err != nil {
		return nil, MapToolError(err)
	}
	occurredAt, err := parseReminderTime(input.OccurredAt, "occurred_at", true)
	if err != nil {
		return nil, MapToolError(err)
	}
	if service == nil {
		return nil, MapToolError(errors.New("activity service is not configured"))
	}
	created, err := service.Create(ctx, actor, dto.ActivityCreateInput{
		Space: actor.SpaceID, Kind: input.Kind, Note: input.Note, OccurredAt: *occurredAt,
	})
	if err != nil {
		return nil, MapToolError(err)
	}
	return created, nil
}

func ActivityCreateWithReminder(ctx context.Context, resolver interfaceidentity.Resolver, activityService interfaceactivity.ServiceActivityInterface, reminderService interfacereminder.ServiceReminderInterface, input ActivityCreateWithReminderInput) (ActivityCreateWithReminderOutput, error) {
	if reminderService == nil {
		return ActivityCreateWithReminderOutput{}, MapToolError(errors.New("reminder service is not configured"))
	}
	if strings.TrimSpace(input.Reminder.Title) == "" {
		return ActivityCreateWithReminderOutput{}, MapToolError(&serviceauthorization.ValidationError{Field: "title", Reason: "is required"})
	}

	hasScheduledAt := strings.TrimSpace(input.Reminder.ScheduledAt) != ""
	hasAfterMinutes := input.Reminder.AfterMinutes != nil
	if hasScheduledAt == hasAfterMinutes {
		return ActivityCreateWithReminderOutput{}, MapToolError(&serviceauthorization.ValidationError{Field: "schedule", Reason: "exactly one of scheduled_at or after_minutes is required"})
	}

	var scheduledAt *time.Time
	if hasScheduledAt {
		var err error
		scheduledAt, err = parseReminderTime(input.Reminder.ScheduledAt, "scheduled_at", true)
		if err != nil {
			return ActivityCreateWithReminderOutput{}, MapToolError(err)
		}
	} else {
		afterMinutes := *input.Reminder.AfterMinutes
		if afterMinutes <= 0 {
			return ActivityCreateWithReminderOutput{}, MapToolError(&serviceauthorization.ValidationError{Field: "after_minutes", Reason: "must be a positive integer"})
		}
		const maxAfterMinutes = (1<<63 - 1) / time.Minute
		if time.Duration(afterMinutes) > maxAfterMinutes {
			return ActivityCreateWithReminderOutput{}, MapToolError(&serviceauthorization.ValidationError{Field: "after_minutes", Reason: "is too large"})
		}
		occurredAt, err := parseReminderTime(input.Activity.OccurredAt, "occurred_at", true)
		if err != nil {
			return ActivityCreateWithReminderOutput{}, MapToolError(err)
		}
		computed := occurredAt.Add(time.Duration(afterMinutes) * time.Minute)
		if computed.Year() > 9999 {
			return ActivityCreateWithReminderOutput{}, MapToolError(&serviceauthorization.ValidationError{Field: "after_minutes", Reason: "produces a time outside the RFC3339 range"})
		}
		scheduledAt = &computed
	}

	assignee, err := optionalReminderUUID(input.Reminder.AssigneeMemberID, "assignee_member_id")
	if err != nil {
		return ActivityCreateWithReminderOutput{}, MapToolError(err)
	}

	activity, err := ActivityCreate(ctx, resolver, activityService, input.Activity)
	if err != nil {
		return ActivityCreateWithReminderOutput{}, err
	}
	if activity == nil {
		return ActivityCreateWithReminderOutput{}, MapToolError(errors.New("activity service returned no activity"))
	}
	assigneeMemberID := ""
	if assignee != nil {
		assigneeMemberID = *assignee
	}
	reminder, err := ReminderCreate(ctx, resolver, reminderService, ReminderCreateInput{
		Space: activity.SpaceID, Title: input.Reminder.Title, Description: input.Reminder.Description,
		ScheduledAt: scheduledAt.Format(time.RFC3339Nano), AssigneeMemberID: assigneeMemberID,
	})
	if err != nil {
		//nolint:nilerr // Reminder failure is reported as partial success after activity persistence.
		return ActivityCreateWithReminderOutput{
			Status: "partial_success", Activity: activity, ReminderError: err.Error(),
		}, nil
	}
	return ActivityCreateWithReminderOutput{Status: "created", Activity: activity, Reminder: &reminder}, nil
}

func ActivityList(ctx context.Context, resolver interfaceidentity.Resolver, service interfaceactivity.ServiceActivityInterface, input ActivityListInput) ([]domainactivity.Activity, error) {
	actor, err := activityActor(ctx, resolver, input.Space)
	if err != nil {
		return nil, MapToolError(err)
	}
	if service == nil {
		return nil, MapToolError(errors.New("activity service is not configured"))
	}
	listInput := dto.ActivityListInput{Space: actor.SpaceID, Kind: input.Kind, Limit: input.Limit}
	if listInput.From, err = parseReminderTime(input.From, "from", false); err != nil {
		return nil, MapToolError(err)
	}
	if listInput.To, err = parseReminderTime(input.To, "to", false); err != nil {
		return nil, MapToolError(err)
	}
	activities, err := service.List(ctx, actor, listInput)
	if err != nil {
		return nil, MapToolError(err)
	}
	if activities == nil {
		activities = []domainactivity.Activity{}
	}
	return activities, nil
}

func ActivityUpdate(ctx context.Context, resolver interfaceidentity.Resolver, service interfaceactivity.ServiceActivityInterface, input ActivityUpdateInput) (*domainactivity.Activity, error) {
	actor, err := activityActor(ctx, resolver, input.Space)
	if err != nil {
		return nil, MapToolError(err)
	}
	activityID := strings.TrimSpace(input.ActivityID)
	if _, err := uuid.Parse(activityID); err != nil {
		return nil, MapToolError(&serviceauthorization.ValidationError{Field: "activity_id", Reason: "must be a valid UUID"})
	}
	if input.Kind == nil && input.Note == nil && strings.TrimSpace(input.OccurredAt) == "" {
		return nil, MapToolError(&serviceauthorization.ValidationError{Field: "update", Reason: "at least one field is required"})
	}
	occurredAt, err := parseReminderTime(input.OccurredAt, "occurred_at", false)
	if err != nil {
		return nil, MapToolError(err)
	}
	if service == nil {
		return nil, MapToolError(errors.New("activity service is not configured"))
	}
	activity, err := service.Update(ctx, actor, actor.SpaceID, activityID, dto.ActivityUpdateInput{Kind: input.Kind, Note: input.Note, OccurredAt: occurredAt})
	if err != nil {
		return nil, MapToolError(err)
	}
	return activity, nil
}

func ActivityDelete(ctx context.Context, resolver interfaceidentity.Resolver, service interfaceactivity.ServiceActivityInterface, input ActivityDeleteInput) (*domainactivity.Activity, error) {
	actor, err := activityActor(ctx, resolver, input.Space)
	if err != nil {
		return nil, MapToolError(err)
	}
	activityID := strings.TrimSpace(input.ActivityID)
	if _, err := uuid.Parse(activityID); err != nil {
		return nil, MapToolError(&serviceauthorization.ValidationError{Field: "activity_id", Reason: "must be a valid UUID"})
	}
	if service == nil {
		return nil, MapToolError(errors.New("activity service is not configured"))
	}
	activity, err := service.Delete(ctx, actor, actor.SpaceID, activityID)
	if err != nil {
		return nil, MapToolError(err)
	}
	return activity, nil
}

func activityActor(ctx context.Context, resolver interfaceidentity.Resolver, selector string) (domainidentity.ActorContext, error) {
	actor, err := RequireActor(ctx, resolver)
	if err != nil {
		return domainidentity.ActorContext{}, err
	}
	return serviceidentity.SelectSpace(ctx, actor, selector, selectorPermissions(resolver))
}

func registerActivityTools(server *mcpsdk.Server, resolver interfaceidentity.Resolver, service interfaceactivity.ServiceActivityInterface, reminderService interfacereminder.ServiceReminderInterface) {
	addTool(server, &mcpsdk.Tool{
		Name: "activity_create_with_reminder", Description: "Create an activity and a reminder together in an authorized Space. Use this when both are requested; use activity_create for activity only.",
	}, func(ctx context.Context, _ *mcpsdk.CallToolRequest, input ActivityCreateWithReminderInput) (*mcpsdk.CallToolResult, ActivityCreateWithReminderOutput, error) {
		output, err := ActivityCreateWithReminder(ctx, resolver, service, reminderService, input)
		return nil, output, err
	})
	addTool(server, &mcpsdk.Tool{
		Name: "activity_create", Description: "Record a standalone timestamped note or care event in an authorized Space. Use activity_create when only an activity is requested; use activity_create_with_reminder when a reminder is also requested.",
	}, func(ctx context.Context, _ *mcpsdk.CallToolRequest, input ActivityCreateInput) (*mcpsdk.CallToolResult, *domainactivity.Activity, error) {
		output, err := ActivityCreate(ctx, resolver, service, input)
		return nil, output, err
	})
	addTool(server, &mcpsdk.Tool{
		Name: "activity_list", Description: "List timestamped notes and care events from an authorized Space.",
	}, func(ctx context.Context, _ *mcpsdk.CallToolRequest, input ActivityListInput) (*mcpsdk.CallToolResult, ActivityListOutput, error) {
		output, err := ActivityList(ctx, resolver, service, input)
		return nil, ActivityListOutput{Activities: output}, err
	})
	addTool(server, &mcpsdk.Tool{
		Name: "activity_update", Description: "Update a timestamped note or care event in an authorized Space.",
	}, func(ctx context.Context, _ *mcpsdk.CallToolRequest, input ActivityUpdateInput) (*mcpsdk.CallToolResult, *domainactivity.Activity, error) {
		output, err := ActivityUpdate(ctx, resolver, service, input)
		return nil, output, err
	})
	addTool(server, &mcpsdk.Tool{
		Name: "activity_delete", Description: "Remove a timestamped note or care event from an authorized Space.",
	}, func(ctx context.Context, _ *mcpsdk.CallToolRequest, input ActivityDeleteInput) (*mcpsdk.CallToolResult, *domainactivity.Activity, error) {
		output, err := ActivityDelete(ctx, resolver, service, input)
		return nil, output, err
	})
}
