package mcp

import (
	"context"
	"errors"
	"strings"
	"time"

	domainactivity "family-assistant/internal/domain/activity"
	"family-assistant/internal/dto"
	interfaceactivity "family-assistant/internal/interfaces/activity"
	interfaceidentity "family-assistant/internal/interfaces/identity"
	interfacereminder "family-assistant/internal/interfaces/reminder"
	serviceauthorization "family-assistant/internal/services/authorization"
	"family-assistant/utils"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

type ActivityCreateInput struct {
	Space      string `json:"space,omitempty" jsonschema:"Space UUID or exact user-facing name; blank selects Personal Space"`
	Kind       string `json:"kind" jsonschema:"short event type such as breastfeeding, diaper, sleep, or note"`
	Note       string `json:"note" jsonschema:"human-readable details"`
	OccurredAt string `json:"occurred_at,omitempty" jsonschema:"RFC3339 event time ONLY when the user specifies a time. Omit for now or no stated time: backend uses the trusted WhatsApp message timestamp, not processing time. Never use another record's created_at or updated_at."`
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
	actor, err := selectSpaceActor(ctx, resolver, input.Space)
	if err != nil {
		return nil, MapToolError(err)
	}
	occurredAt, err := activityOccurredAt(ctx, input.OccurredAt)
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

func activityOccurredAt(ctx context.Context, value string) (*time.Time, error) {
	if strings.TrimSpace(value) != "" {
		return utils.ParseRFC3339(value, "occurred_at", true)
	}
	request, ok := ExternalRequestFromContext(ctx)
	if !ok || request.MessageAt.IsZero() {
		return nil, &serviceauthorization.ValidationError{Field: "occurred_at", Reason: "requires a trusted message timestamp or an explicit event time"}
	}
	return &request.MessageAt, nil
}

func ActivityList(ctx context.Context, resolver interfaceidentity.Resolver, service interfaceactivity.ServiceActivityInterface, input ActivityListInput) ([]domainactivity.Activity, error) {
	actor, err := selectSpaceActor(ctx, resolver, input.Space)
	if err != nil {
		return nil, MapToolError(err)
	}
	if service == nil {
		return nil, MapToolError(errors.New("activity service is not configured"))
	}
	listInput := dto.ActivityListInput{Space: actor.SpaceID, Kind: input.Kind, Limit: input.Limit}
	if listInput.From, err = utils.ParseRFC3339(input.From, "from", false); err != nil {
		return nil, MapToolError(err)
	}
	if listInput.To, err = utils.ParseRFC3339(input.To, "to", false); err != nil {
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
	actor, err := selectSpaceActor(ctx, resolver, input.Space)
	if err != nil {
		return nil, MapToolError(err)
	}
	activityID, err := utils.ParseUUID(input.ActivityID, "activity_id", true)
	if err != nil {
		return nil, MapToolError(err)
	}
	if input.Kind == nil && input.Note == nil && strings.TrimSpace(input.OccurredAt) == "" {
		return nil, MapToolError(&serviceauthorization.ValidationError{Field: "update", Reason: "at least one field is required"})
	}
	occurredAt, err := utils.ParseRFC3339(input.OccurredAt, "occurred_at", false)
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
	actor, err := selectSpaceActor(ctx, resolver, input.Space)
	if err != nil {
		return nil, MapToolError(err)
	}
	activityID, err := utils.ParseUUID(input.ActivityID, "activity_id", true)
	if err != nil {
		return nil, MapToolError(err)
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

func registerActivityTools(server *mcpsdk.Server, resolver interfaceidentity.Resolver, service interfaceactivity.ServiceActivityInterface, reminderService interfacereminder.ServiceReminderInterface) {
	addTool(server, &mcpsdk.Tool{
		Name: "activity_create_with_reminder", Description: "Create an activity and a reminder together in an authorized Space. Use this when both are requested; use activity_create for activity only.",
	}, func(ctx context.Context, input ActivityCreateWithReminderInput) (ActivityCreateWithReminderOutput, error) {
		return ActivityCreateWithReminder(ctx, resolver, service, reminderService, input)
	})
	addTool(server, &mcpsdk.Tool{
		Name: "activity_create", Description: "Record a standalone timestamped note or care event in an authorized Space. Use activity_create when only an activity is requested; use activity_create_with_reminder when a reminder is also requested.",
	}, func(ctx context.Context, input ActivityCreateInput) (*domainactivity.Activity, error) {
		return ActivityCreate(ctx, resolver, service, input)
	})
	addTool(server, &mcpsdk.Tool{
		Name: "activity_list", Description: "List timestamped notes and care events from an authorized Space.",
	}, func(ctx context.Context, input ActivityListInput) (ActivityListOutput, error) {
		output, err := ActivityList(ctx, resolver, service, input)
		return ActivityListOutput{Activities: output}, err
	})
	addTool(server, &mcpsdk.Tool{
		Name: "activity_update", Description: "Update a timestamped note or care event in an authorized Space.",
	}, func(ctx context.Context, input ActivityUpdateInput) (*domainactivity.Activity, error) {
		return ActivityUpdate(ctx, resolver, service, input)
	})
	addTool(server, &mcpsdk.Tool{
		Name: "activity_delete", Description: "Remove a timestamped note or care event from an authorized Space.",
	}, func(ctx context.Context, input ActivityDeleteInput) (*domainactivity.Activity, error) {
		return ActivityDelete(ctx, resolver, service, input)
	})
}
