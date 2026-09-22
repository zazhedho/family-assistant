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
	Update(context.Context, string, string, dto.SpaceUpdateInput) (*domainspace.Space, error)
	Archive(context.Context, string, string) (*domainspace.Space, error)
	UpdateMemberRole(context.Context, string, string, string, dto.MemberRoleUpdateInput) (*domainspace.ResolvedMembership, error)
	RemoveMember(context.Context, string, string, string) (*domainspace.ResolvedMembership, error)
}
