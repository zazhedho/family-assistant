package interfacespace

import (
	"context"

	domainspace "family-assistant/internal/domain/space"
)

type RepoSpaceInterface interface {
	CreateWithOwner(context.Context, *domainspace.Space, *domainspace.Member) error
	ListActiveByUserID(context.Context, string) ([]domainspace.ResolvedMembership, error)
	FindActiveMembership(context.Context, string, string) (*domainspace.ResolvedMembership, error)
	ListActiveMembers(context.Context, string) ([]domainspace.ResolvedMembership, error)
}
