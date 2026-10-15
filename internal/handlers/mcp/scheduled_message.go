package mcp

import (
	"context"
	"errors"

	domainidentity "family-assistant/internal/domain/identity"
	domainscheduledmessage "family-assistant/internal/domain/scheduledmessage"
	"family-assistant/internal/dto"
	interfaceidentity "family-assistant/internal/interfaces/identity"
	interfacescheduledmessage "family-assistant/internal/interfaces/scheduledmessage"
	serviceidentity "family-assistant/internal/services/identity"
	"family-assistant/utils"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

type spaceChannelBindToolInput struct {
	Space string `json:"space,omitempty" jsonschema:"Space UUID or exact name; blank selects Personal Space"`
}

type scheduledMessageCreateToolInput struct {
	Space             string `json:"space,omitempty" jsonschema:"Space UUID or exact name; blank selects Personal Space"`
	Target            string `json:"target" jsonschema:"DM or GROUP"`
	RecipientMemberID string `json:"recipient_member_id,omitempty" jsonschema:"recipient member UUID for a DM target"`
	SpaceChannelID    string `json:"space_channel_id,omitempty" jsonschema:"bound group channel UUID for a GROUP target"`
	ContentMode       string `json:"content_mode" jsonschema:"FIXED or AI"`
	Content           string `json:"content" jsonschema:"fixed text for FIXED, or a theme for AI"`
	Recurrence        string `json:"recurrence" jsonschema:"ONCE, DAILY, or WEEKLY"`
	Timezone          string `json:"timezone" jsonschema:"IANA timezone"`
	StartAt           string `json:"start_at" jsonschema:"RFC3339 timestamp"`
}

type scheduledMessageListToolInput struct {
	Space      string `json:"space,omitempty" jsonschema:"Space UUID or exact name; blank selects Personal Space"`
	ScheduleID string `json:"schedule_id,omitempty" jsonschema:"optional schedule UUID for a single schedule and delivery history"`
	Status     string `json:"status,omitempty" jsonschema:"optional ACTIVE, SENT, FAILED, CANCELLED, or UNSUBSCRIBED filter"`
}

type scheduledMessageUpdateToolInput struct {
	Space       string  `json:"space,omitempty" jsonschema:"Space UUID or exact name; blank selects Personal Space"`
	ScheduleID  string  `json:"schedule_id" jsonschema:"schedule UUID"`
	ContentMode *string `json:"content_mode,omitempty" jsonschema:"optional FIXED or AI"`
	Content     *string `json:"content,omitempty" jsonschema:"optional fixed text or AI theme"`
	Recurrence  *string `json:"recurrence,omitempty" jsonschema:"optional ONCE, DAILY, or WEEKLY"`
	Timezone    *string `json:"timezone,omitempty" jsonschema:"optional IANA timezone"`
	StartAt     *string `json:"start_at,omitempty" jsonschema:"optional RFC3339 timestamp"`
}

type scheduledMessageMutationToolInput struct {
	Space      string `json:"space,omitempty" jsonschema:"Space UUID or exact name; blank selects Personal Space"`
	ScheduleID string `json:"schedule_id" jsonschema:"schedule UUID"`
}

func scheduledMessageCreate(ctx context.Context, resolver interfaceidentity.Resolver, service interfacescheduledmessage.Service, input scheduledMessageCreateToolInput) (dto.ScheduledMessageOutput, error) {
	actor, _, err := selectVerifiedScheduledActor(ctx, resolver, input.Space)
	if err != nil {
		return dto.ScheduledMessageOutput{}, MapToolError(err)
	}
	if service == nil {
		return dto.ScheduledMessageOutput{}, MapToolError(errors.New("scheduled message service is not configured"))
	}
	recipientID, err := utils.ParseOptionalUUID(input.RecipientMemberID, "recipient_member_id")
	if err != nil {
		return dto.ScheduledMessageOutput{}, MapToolError(err)
	}
	channelID, err := utils.ParseOptionalUUID(input.SpaceChannelID, "space_channel_id")
	if err != nil {
		return dto.ScheduledMessageOutput{}, MapToolError(err)
	}
	startAt, err := utils.ParseRFC3339(input.StartAt, "start_at", true)
	if err != nil {
		return dto.ScheduledMessageOutput{}, MapToolError(err)
	}
	created, err := service.Create(ctx, actor, dto.ScheduledMessageCreateInput{
		Space: actor.SpaceID, Target: domainscheduledmessage.TargetKind(input.Target),
		RecipientMemberID: recipientID, SpaceChannelID: channelID,
		ContentMode: domainscheduledmessage.ContentMode(input.ContentMode), Content: input.Content,
		Recurrence: domainscheduledmessage.Recurrence(input.Recurrence), Timezone: input.Timezone, StartAt: *startAt,
	})
	if err != nil {
		return dto.ScheduledMessageOutput{}, MapToolError(err)
	}
	if created == nil {
		return dto.ScheduledMessageOutput{}, MapToolError(errors.New("scheduled message service returned no schedule"))
	}
	return scheduledMessageOutput(created), nil
}

func scheduledMessageList(ctx context.Context, resolver interfaceidentity.Resolver, service interfacescheduledmessage.Service, input scheduledMessageListToolInput) ([]dto.ScheduledMessageOutput, error) {
	actor, _, err := selectVerifiedScheduledActor(ctx, resolver, input.Space)
	if err != nil {
		return nil, MapToolError(err)
	}
	if service == nil {
		return nil, MapToolError(errors.New("scheduled message service is not configured"))
	}
	scheduleID, err := utils.ParseOptionalUUID(input.ScheduleID, "schedule_id")
	if err != nil {
		return nil, MapToolError(err)
	}
	var status *domainscheduledmessage.ScheduleStatus
	if input.Status != "" {
		value := domainscheduledmessage.ScheduleStatus(input.Status)
		status = &value
	}
	output, err := service.List(ctx, actor, dto.ScheduledMessageListInput{Space: actor.SpaceID, ScheduleID: scheduleID, Status: status})
	if err != nil {
		return nil, MapToolError(err)
	}
	if output == nil {
		output = []dto.ScheduledMessageOutput{}
	}
	return output, nil
}

func scheduledMessageTargets(ctx context.Context, resolver interfaceidentity.Resolver, service interfacescheduledmessage.Service, space string) ([]dto.ScheduledMessageTargetOutput, error) {
	actor, _, err := selectVerifiedScheduledActor(ctx, resolver, space)
	if err != nil {
		return nil, MapToolError(err)
	}
	if service == nil {
		return nil, MapToolError(errors.New("scheduled message service is not configured"))
	}
	output, err := service.Targets(ctx, actor, actor.SpaceID)
	if err != nil {
		return nil, MapToolError(err)
	}
	if output == nil {
		output = []dto.ScheduledMessageTargetOutput{}
	}
	return output, nil
}

func scheduledMessageUpdate(ctx context.Context, resolver interfaceidentity.Resolver, service interfacescheduledmessage.Service, input scheduledMessageUpdateToolInput) (dto.ScheduledMessageOutput, error) {
	actor, _, err := selectVerifiedScheduledActor(ctx, resolver, input.Space)
	if err != nil {
		return dto.ScheduledMessageOutput{}, MapToolError(err)
	}
	if service == nil {
		return dto.ScheduledMessageOutput{}, MapToolError(errors.New("scheduled message service is not configured"))
	}
	scheduleID, err := utils.ParseUUID(input.ScheduleID, "schedule_id", true)
	if err != nil {
		return dto.ScheduledMessageOutput{}, MapToolError(err)
	}
	update, err := scheduledMessageUpdateInput(input)
	if err != nil {
		return dto.ScheduledMessageOutput{}, MapToolError(err)
	}
	updated, err := service.Update(ctx, actor, actor.SpaceID, scheduleID, update)
	if err != nil {
		return dto.ScheduledMessageOutput{}, MapToolError(err)
	}
	if updated == nil {
		return dto.ScheduledMessageOutput{}, MapToolError(errors.New("scheduled message service returned no schedule"))
	}
	return scheduledMessageOutput(updated), nil
}

func scheduledMessageUpdateInput(input scheduledMessageUpdateToolInput) (dto.ScheduledMessageUpdateInput, error) {
	var update dto.ScheduledMessageUpdateInput
	if input.ContentMode != nil {
		value := domainscheduledmessage.ContentMode(*input.ContentMode)
		update.ContentMode = &value
	}
	update.Content = input.Content
	if input.Recurrence != nil {
		value := domainscheduledmessage.Recurrence(*input.Recurrence)
		update.Recurrence = &value
	}
	update.Timezone = input.Timezone
	if input.StartAt != nil {
		parsed, err := utils.ParseRFC3339(*input.StartAt, "start_at", true)
		if err != nil {
			return dto.ScheduledMessageUpdateInput{}, err
		}
		update.StartAt = parsed
	}
	return update, nil
}

func scheduledMessageCancel(ctx context.Context, resolver interfaceidentity.Resolver, service interfacescheduledmessage.Service, input scheduledMessageMutationToolInput) (dto.ScheduledMessageOutput, error) {
	actor, _, err := selectVerifiedScheduledActor(ctx, resolver, input.Space)
	if err != nil {
		return dto.ScheduledMessageOutput{}, MapToolError(err)
	}
	if service == nil {
		return dto.ScheduledMessageOutput{}, MapToolError(errors.New("scheduled message service is not configured"))
	}
	scheduleID, err := utils.ParseUUID(input.ScheduleID, "schedule_id", true)
	if err != nil {
		return dto.ScheduledMessageOutput{}, MapToolError(err)
	}
	cancelled, err := service.Cancel(ctx, actor, actor.SpaceID, scheduleID)
	if err != nil {
		return dto.ScheduledMessageOutput{}, MapToolError(err)
	}
	if cancelled == nil {
		return dto.ScheduledMessageOutput{}, MapToolError(errors.New("scheduled message service returned no schedule"))
	}
	return scheduledMessageOutput(cancelled), nil
}

func scheduledMessageUnsubscribe(ctx context.Context, resolver interfaceidentity.Resolver, service interfacescheduledmessage.Service, input scheduledMessageMutationToolInput) (dto.ScheduledMessageOutput, error) {
	actor, _, err := selectVerifiedScheduledActor(ctx, resolver, input.Space)
	if err != nil {
		return dto.ScheduledMessageOutput{}, MapToolError(err)
	}
	if service == nil {
		return dto.ScheduledMessageOutput{}, MapToolError(errors.New("scheduled message service is not configured"))
	}
	scheduleID, err := utils.ParseUUID(input.ScheduleID, "schedule_id", true)
	if err != nil {
		return dto.ScheduledMessageOutput{}, MapToolError(err)
	}
	unsubscribed, err := service.Unsubscribe(ctx, actor, actor.SpaceID, scheduleID)
	if err != nil {
		return dto.ScheduledMessageOutput{}, MapToolError(err)
	}
	if unsubscribed == nil {
		return dto.ScheduledMessageOutput{}, MapToolError(errors.New("scheduled message service returned no schedule"))
	}
	return scheduledMessageOutput(unsubscribed), nil
}

func spaceChannelBind(ctx context.Context, resolver interfaceidentity.Resolver, service interfacescheduledmessage.Service, input spaceChannelBindToolInput) (dto.SpaceChannelOutput, error) {
	actor, request, err := selectVerifiedScheduledActor(ctx, resolver, input.Space)
	if err != nil {
		return dto.SpaceChannelOutput{}, MapToolError(err)
	}
	if request.Channel != "whatsapp" || request.ChatType != "group" || request.ChatID == "" {
		return dto.SpaceChannelOutput{}, MapToolError(interfacescheduledmessage.ErrInvalidWhatsAppIdentity)
	}
	if service == nil {
		return dto.SpaceChannelOutput{}, MapToolError(errors.New("scheduled message service is not configured"))
	}
	bound, err := service.BindCurrentGroup(ctx, actor, dto.SpaceChannelBindInput{
		Space: actor.SpaceID, CurrentChannel: request.Channel,
		CurrentChatID: request.ChatID, CurrentChatType: request.ChatType,
	})
	if err != nil {
		return dto.SpaceChannelOutput{}, MapToolError(err)
	}
	if bound == nil {
		return dto.SpaceChannelOutput{}, MapToolError(errors.New("scheduled message service returned no channel"))
	}
	return dto.SpaceChannelOutput{
		ID: bound.ID, SpaceID: bound.SpaceID, Channel: bound.Channel,
		Status: bound.Status, BoundByMemberID: bound.BoundByMemberID,
		CreatedAt: bound.CreatedAt, UpdatedAt: bound.UpdatedAt,
	}, nil
}

func selectVerifiedScheduledActor(ctx context.Context, resolver interfaceidentity.Resolver, space string) (domainidentity.ActorContext, ExternalRequest, error) {
	request, ok := verifiedExternalRequestFromContext(ctx)
	if !ok {
		return domainidentity.ActorContext{}, ExternalRequest{}, serviceidentity.ErrUnauthenticated
	}
	actorContext := withVerifiedExternalRequest(ctx, request)
	actor, err := selectSpaceActor(actorContext, resolver, space)
	if err != nil {
		return domainidentity.ActorContext{}, ExternalRequest{}, err
	}
	return actor, request, nil
}

func scheduledMessageOutput(schedule *domainscheduledmessage.Schedule) dto.ScheduledMessageOutput {
	return dto.ScheduledMessageOutput{
		ID: schedule.ID, SpaceID: schedule.SpaceID, CreatedByMemberID: schedule.CreatedByMemberID,
		Target: schedule.Target, RecipientMemberID: schedule.RecipientMemberID, SpaceChannelID: schedule.SpaceChannelID,
		ContentMode: schedule.ContentMode, Content: schedule.Content, Recurrence: schedule.Recurrence,
		Timezone: schedule.Timezone, StartAt: schedule.StartAt, NextScheduledAt: schedule.NextScheduledAt,
		Status: schedule.Status, UnsubscribedByMemberID: schedule.UnsubscribedByMemberID,
		UnsubscribedAt: schedule.UnsubscribedAt, CreatedAt: schedule.CreatedAt, UpdatedAt: schedule.UpdatedAt,
	}
}

func registerScheduledMessageTools(server *mcpsdk.Server, resolver interfaceidentity.Resolver, service interfacescheduledmessage.Service) {
	addTool(server, &mcpsdk.Tool{Name: "space_channel_bind", Description: "Bind the current verified WhatsApp group to an authorized Space."}, func(ctx context.Context, input spaceChannelBindToolInput) (dto.SpaceChannelOutput, error) {
		return spaceChannelBind(ctx, resolver, service, input)
	})
	addTool(server, &mcpsdk.Tool{Name: "scheduled_message_targets", Description: "List recipients and bound groups available in a Space."}, func(ctx context.Context, input struct {
		Space string `json:"space,omitempty" jsonschema:"Space UUID or exact name; blank selects Personal Space"`
	}) ([]dto.ScheduledMessageTargetOutput, error) {
		return scheduledMessageTargets(ctx, resolver, service, input.Space)
	})
	addTool(server, &mcpsdk.Tool{Name: "scheduled_message_create", Description: "Schedule a fixed WhatsApp message or AI-generated message from a theme."}, func(ctx context.Context, input scheduledMessageCreateToolInput) (dto.ScheduledMessageOutput, error) {
		return scheduledMessageCreate(ctx, resolver, service, input)
	})
	addTool(server, &mcpsdk.Tool{Name: "scheduled_message_list", Description: "List schedules in an authorized Space, optionally including one schedule's delivery history."}, func(ctx context.Context, input scheduledMessageListToolInput) ([]dto.ScheduledMessageOutput, error) {
		return scheduledMessageList(ctx, resolver, service, input)
	})
	addTool(server, &mcpsdk.Tool{Name: "scheduled_message_update", Description: "Update schedule content, recurrence, timezone, or next start time."}, func(ctx context.Context, input scheduledMessageUpdateToolInput) (dto.ScheduledMessageOutput, error) {
		return scheduledMessageUpdate(ctx, resolver, service, input)
	})
	addTool(server, &mcpsdk.Tool{Name: "scheduled_message_cancel", Description: "Cancel a schedule you created or are authorized to administer."}, func(ctx context.Context, input scheduledMessageMutationToolInput) (dto.ScheduledMessageOutput, error) {
		return scheduledMessageCancel(ctx, resolver, service, input)
	})
	addTool(server, &mcpsdk.Tool{Name: "scheduled_message_unsubscribe", Description: "Unsubscribe your authenticated recipient membership from a schedule."}, func(ctx context.Context, input scheduledMessageMutationToolInput) (dto.ScheduledMessageOutput, error) {
		return scheduledMessageUnsubscribe(ctx, resolver, service, input)
	})
}
