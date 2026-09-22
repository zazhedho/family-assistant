package interfaceactivity

import (
	"context"

	domainactivity "family-assistant/internal/domain/activity"
	domainidentity "family-assistant/internal/domain/identity"
	"family-assistant/internal/dto"
)

type ServiceActivityInterface interface {
	Create(context.Context, domainidentity.ActorContext, dto.ActivityCreateInput) (*domainactivity.Activity, error)
	List(context.Context, domainidentity.ActorContext, dto.ActivityListInput) ([]domainactivity.Activity, error)
	Update(context.Context, domainidentity.ActorContext, string, string, dto.ActivityUpdateInput) (*domainactivity.Activity, error)
	Delete(context.Context, domainidentity.ActorContext, string, string) (*domainactivity.Activity, error)
}
