package serviceidentity

import (
	"context"
	"errors"
	"strings"

	domainidentity "family-assistant/internal/domain/identity"
	interfaceidentity "family-assistant/internal/interfaces/identity"
	interfacespace "family-assistant/internal/interfaces/space"

	"gorm.io/gorm"
)

var (
	ErrUnauthenticated       = errors.New("unauthenticated")
	ErrResolverMisconfigured = errors.New("identity resolver is not configured")
)

type ResolverService struct {
	IdentityRepo      interfaceidentity.RepoIdentityInterface
	SpaceRepo         interfacespace.RepoSpaceInterface
	PermissionService interfaceidentity.PermissionLoader
}

func NewResolver(identityRepo interfaceidentity.RepoIdentityInterface, spaceRepo interfacespace.RepoSpaceInterface, permissions interfaceidentity.PermissionLoader) *ResolverService {
	return &ResolverService{IdentityRepo: identityRepo, SpaceRepo: spaceRepo, PermissionService: permissions}
}

func (r *ResolverService) ResolveExternal(ctx context.Context, provider, externalID, channel string) (domainidentity.ActorContext, error) {
	if !r.configuredForExternal() {
		return domainidentity.ActorContext{}, ErrResolverMisconfigured
	}
	provider = domainidentity.NormalizeProvider(provider)
	externalID = strings.TrimSpace(externalID)
	if provider == "" || externalID == "" {
		return domainidentity.ActorContext{}, ErrUnauthenticated
	}

	identity, err := r.IdentityRepo.FindActive(ctx, provider, externalID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) || errors.Is(err, domainidentity.ErrIdentityNotFound) {
			return domainidentity.ActorContext{}, ErrUnauthenticated
		}
		return domainidentity.ActorContext{}, err
	}
	if identity == nil ||
		strings.TrimSpace(identity.UserID) == "" ||
		strings.TrimSpace(identity.Status) != domainidentity.StatusActive ||
		domainidentity.NormalizeProvider(identity.Provider) != provider ||
		strings.TrimSpace(identity.ExternalID) != externalID {
		return domainidentity.ActorContext{}, ErrUnauthenticated
	}

	actor, err := r.actorForUser(ctx, identity.UserID, channel, true)
	if err != nil {
		return domainidentity.ActorContext{}, err
	}
	actor.ExternalProvider = provider
	actor.ExternalID = externalID
	return actor, nil
}

func (r *ResolverService) ResolveUser(ctx context.Context, userID, channel string) (domainidentity.ActorContext, error) {
	if r == nil {
		return domainidentity.ActorContext{}, ErrResolverMisconfigured
	}
	if !r.configuredForUser() {
		return domainidentity.ActorContext{}, ErrResolverMisconfigured
	}
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return domainidentity.ActorContext{}, ErrUnauthenticated
	}
	return r.actorForUser(ctx, userID, channel, false)
}

func (r *ResolverService) actorForUser(ctx context.Context, userID, channel string, external bool) (domainidentity.ActorContext, error) {
	userID = strings.TrimSpace(userID)
	memberships, err := r.SpaceRepo.ListActiveByUserID(ctx, userID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return domainidentity.ActorContext{}, ErrUnauthenticated
		}
		return domainidentity.ActorContext{}, err
	}
	active := activeMembershipsForUser(userID, memberships)
	if len(active) == 0 {
		return domainidentity.ActorContext{}, ErrUnauthenticated
	}

	actor := domainidentity.ActorContext{
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

var _ interfaceidentity.Resolver = (*ResolverService)(nil)
var _ interfaceidentity.UserResolver = (*ResolverService)(nil)
