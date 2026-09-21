package interfaceauthorization

import (
	"context"

	domainauthorization "family-assistant/internal/domain/authorization"
	domainidentity "family-assistant/internal/domain/identity"
)

type Authorizer interface {
	Authorize(context.Context, domainidentity.ActorContext, string, domainauthorization.Resource) error
}
