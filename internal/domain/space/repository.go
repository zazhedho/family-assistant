package domainspace

import "context"

type Repository interface {
	CreateWithOwner(context.Context, *Space, *Member) error
	ListActiveByUserID(context.Context, string) ([]ResolvedMembership, error)
	FindActiveMembership(context.Context, string, string) (*ResolvedMembership, error)
	ListActiveMembers(context.Context, string) ([]ResolvedMembership, error)
}
