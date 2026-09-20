package serviceidentity

import (
	"context"
	"errors"
	"strings"

	domainidentity "family-assistant/internal/domain/identity"
	domainpermission "family-assistant/internal/domain/permission"
	domainspace "family-assistant/internal/domain/space"

	"gorm.io/gorm"
)

var (
	ErrUnauthenticated       = errors.New("unauthenticated")
	ErrResolverMisconfigured = errors.New("identity resolver is not configured")
)

type Resolver interface {
	ResolveExternal(context.Context, string, string, string) (ActorContext, error)
}

type UserResolver interface {
	ResolveUser(context.Context, string, string) (ActorContext, error)
}

type PermissionLoader interface {
	GetRolePermissions(context.Context, string) ([]domainpermission.Permission, error)
}

type ResolverService struct {
	IdentityRepo      domainidentity.Repository
	SpaceRepo         domainspace.Repository
	PermissionService PermissionLoader
}

func NewResolver(identityRepo domainidentity.Repository, spaceRepo domainspace.Repository, permissions PermissionLoader) *ResolverService {
	return &ResolverService{IdentityRepo: identityRepo, SpaceRepo: spaceRepo, PermissionService: permissions}
}

func (r *ResolverService) ResolveExternal(ctx context.Context, provider, externalID, channel string) (ActorContext, error) {
	if !r.configuredForExternal() {
		return ActorContext{}, ErrResolverMisconfigured
	}
	provider = domainidentity.NormalizeProvider(provider)
	externalID = strings.TrimSpace(externalID)
	if provider == "" || externalID == "" {
		return ActorContext{}, ErrUnauthenticated
	}

	identity, err := r.IdentityRepo.FindActive(ctx, provider, externalID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) || errors.Is(err, domainidentity.ErrIdentityNotFound) {
			return ActorContext{}, ErrUnauthenticated
		}
		return ActorContext{}, err
	}
	if identity == nil ||
		strings.TrimSpace(identity.UserID) == "" ||
		strings.TrimSpace(identity.Status) != domainidentity.StatusActive ||
		domainidentity.NormalizeProvider(identity.Provider) != provider ||
		strings.TrimSpace(identity.ExternalID) != externalID {
		return ActorContext{}, ErrUnauthenticated
	}

	actor, err := r.actorForUser(ctx, identity.UserID, channel, true)
	if err != nil {
		return ActorContext{}, err
	}
	actor.ExternalProvider = provider
	actor.ExternalID = externalID
	return actor, nil
}

func (r *ResolverService) ResolveUser(ctx context.Context, userID, channel string) (ActorContext, error) {
	if r == nil {
		return ActorContext{}, ErrResolverMisconfigured
	}
	if !r.configuredForUser() {
		return ActorContext{}, ErrResolverMisconfigured
	}
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return ActorContext{}, ErrUnauthenticated
	}
	return r.actorForUser(ctx, userID, channel, false)
}

func (r *ResolverService) actorForUser(ctx context.Context, userID, channel string, external bool) (ActorContext, error) {
	userID = strings.TrimSpace(userID)
	memberships, err := r.SpaceRepo.ListActiveByUserID(ctx, userID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ActorContext{}, ErrUnauthenticated
		}
		return ActorContext{}, err
	}
	active := activeMembershipsForUser(userID, memberships)
	if len(active) == 0 {
		return ActorContext{}, ErrUnauthenticated
	}

	actor := ActorContext{
		UserID:      userID,
		Memberships: active,
		Source:      sourceForResolution(external),
		Channel:     strings.TrimSpace(channel),
	}
	return actor, nil
}

func (r *ResolverService) configuredForExternal() bool {
	return r != nil && r.IdentityRepo != nil && r.SpaceRepo != nil
}

func (r *ResolverService) configuredForUser() bool {
	return r != nil && r.SpaceRepo != nil
}

func sourceForResolution(external bool) string {
	if external {
		return "mcp"
	}
	return "http"
}

var _ Resolver = (*ResolverService)(nil)
var _ UserResolver = (*ResolverService)(nil)
