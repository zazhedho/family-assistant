package interfacespace

import (
	"context"

	domainspace "family-assistant/internal/domain/space"
	"family-assistant/internal/dto"
)

type ServiceSpaceInterface interface {
	List(context.Context, string) ([]domainspace.ResolvedMembership, error)
	Create(context.Context, string, dto.SpaceCreateInput) (*domainspace.Space, error)
	Members(context.Context, string, string) ([]domainspace.ResolvedMembership, error)
}
