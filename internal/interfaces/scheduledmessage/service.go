package interfacescheduledmessage

import (
	"context"

	domainidentity "family-assistant/internal/domain/identity"
	domainscheduledmessage "family-assistant/internal/domain/scheduledmessage"
	"family-assistant/internal/dto"
)

type Service interface {
	Create(context.Context, domainidentity.ActorContext, dto.ScheduledMessageCreateInput) (*domainscheduledmessage.Schedule, error)
	List(context.Context, domainidentity.ActorContext, dto.ScheduledMessageListInput) ([]dto.ScheduledMessageOutput, error)
	Update(context.Context, domainidentity.ActorContext, string, string, dto.ScheduledMessageUpdateInput) (*domainscheduledmessage.Schedule, error)
	Cancel(context.Context, domainidentity.ActorContext, string, string) (*domainscheduledmessage.Schedule, error)
	Unsubscribe(context.Context, domainidentity.ActorContext, string, string) (*domainscheduledmessage.Schedule, error)
	Targets(context.Context, domainidentity.ActorContext, string) ([]dto.ScheduledMessageTargetOutput, error)
	BindCurrentGroup(context.Context, domainidentity.ActorContext, dto.SpaceChannelBindInput) (*domainscheduledmessage.SpaceChannel, error)
}
