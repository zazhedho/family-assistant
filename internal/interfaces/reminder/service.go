package interfacereminder

import (
	"context"

	domainidentity "family-assistant/internal/domain/identity"
	domainreminder "family-assistant/internal/domain/reminder"
	"family-assistant/internal/dto"
)

type ServiceReminderInterface interface {
	Create(context.Context, domainidentity.ActorContext, dto.ReminderCreateInput) (*domainreminder.Reminder, error)
	List(context.Context, domainidentity.ActorContext, dto.ReminderListInput) ([]domainreminder.Reminder, error)
	Complete(context.Context, domainidentity.ActorContext, string, string) (*domainreminder.Reminder, error)
	Update(context.Context, domainidentity.ActorContext, string, string, dto.ReminderUpdateInput) (*domainreminder.Reminder, error)
	Delete(context.Context, domainidentity.ActorContext, string, string) (*domainreminder.Reminder, error)
}
