package interfacespace

import (
	"context"
	"time"

	domainspace "family-assistant/internal/domain/space"
)

type RepoSpaceInterface interface {
	CreateWithOwner(context.Context, *domainspace.Space, *domainspace.Member) error
	ListActiveByUserID(context.Context, string) ([]domainspace.ResolvedMembership, error)
	FindActiveMembership(context.Context, string, string) (*domainspace.ResolvedMembership, error)
	ListActiveMembers(context.Context, string) ([]domainspace.ResolvedMembership, error)
	Update(context.Context, string, domainspace.SpaceUpdateFields, time.Time) error
	Archive(context.Context, string, time.Time) error
	UpdateMemberRole(context.Context, string, string, string, time.Time) error
	RemoveMember(context.Context, string, string, time.Time) error
	CountActiveOwners(context.Context, string) (int64, error)
}
