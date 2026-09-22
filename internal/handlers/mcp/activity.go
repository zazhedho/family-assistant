package mcp

import (
	"context"
	"errors"
	"strings"

	domainactivity "family-assistant/internal/domain/activity"
	domainidentity "family-assistant/internal/domain/identity"
	"family-assistant/internal/dto"
	interfaceactivity "family-assistant/internal/interfaces/activity"
	interfaceidentity "family-assistant/internal/interfaces/identity"
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

func registerActivityTools(server *mcpsdk.Server, resolver interfaceidentity.Resolver, service interfaceactivity.ServiceActivityInterface) {
	addTool(server, &mcpsdk.Tool{
		Name: "activity_create", Description: "Record a timestamped note or care event in an authorized Space, such as breastfeeding, diaper changes, sleep, medication, or any custom activity.",
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
