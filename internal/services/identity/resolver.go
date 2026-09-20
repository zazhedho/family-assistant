package serviceidentity

import (
	"context"
	"errors"
	"strings"

	"family-assistant/internal/authscope"
	domainfamilymember "family-assistant/internal/domain/familymember"
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

	// FamilyMemberRepo is retained only while the unreleased reminder path
	// migrates. New callers must provide IdentityRepo and SpaceRepo.
	FamilyMemberRepo domainfamilymember.Repository
}

// NewResolver accepts the new identity/Space repositories. Its variadic shape
// keeps the old two-argument startup wiring compiling until Task 11 removes
// the family resolver path.
func NewResolver(identityRepo any, dependencies ...any) *ResolverService {
	resolver := &ResolverService{}
	switch repo := identityRepo.(type) {
	case domainidentity.Repository:
		resolver.IdentityRepo = repo
	case domainfamilymember.Repository:
		resolver.FamilyMemberRepo = repo
	}
	for _, dependency := range dependencies {
		switch value := dependency.(type) {
		case domainspace.Repository:
			resolver.SpaceRepo = value
		case PermissionLoader:
			resolver.PermissionService = value
		}
	}
	return resolver
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
	if identity == nil || strings.TrimSpace(identity.UserID) == "" {
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
	if r.SpaceRepo != nil {
		if !r.configuredForUser() {
			return ActorContext{}, ErrResolverMisconfigured
		}
		userID = strings.TrimSpace(userID)
		if userID == "" {
			return ActorContext{}, ErrUnauthenticated
		}
		return r.actorForUser(ctx, userID, channel, false)
	}
	return r.resolveLegacyUser(ctx, userID, channel)
}

// Resolve is a compatibility alias for the old Hermes-only middleware. New
// code should call ResolveExternal with an explicit provider.
func (r *ResolverService) Resolve(ctx context.Context, externalID, channel string) (ActorContext, error) {
	if r != nil && r.IdentityRepo != nil {
		return r.ResolveExternal(ctx, domainidentity.ProviderHermes, externalID, channel)
	}
	return r.resolveLegacyExternal(ctx, externalID, channel)
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
	active := make([]domainspace.ResolvedMembership, 0, len(memberships))
	for _, membership := range memberships {
		if strings.EqualFold(strings.TrimSpace(membership.Status), domainspace.StatusActive) {
			active = append(active, membership)
		}
	}
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

func (r *ResolverService) resolveLegacyExternal(ctx context.Context, externalID, channel string) (ActorContext, error) {
	if r == nil || r.FamilyMemberRepo == nil || r.PermissionService == nil {
		return ActorContext{}, ErrResolverMisconfigured
	}
	externalID = strings.TrimSpace(externalID)
	if externalID == "" {
		return ActorContext{}, ErrUnauthenticated
	}
	member, err := r.FamilyMemberRepo.FindActiveByHermesProfile(ctx, externalID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ActorContext{}, ErrUnauthenticated
		}
		return ActorContext{}, err
	}
	return r.actorFromLegacyMember(ctx, member, externalID, channel, true)
}

func (r *ResolverService) resolveLegacyUser(ctx context.Context, userID, channel string) (ActorContext, error) {
	if r == nil || r.FamilyMemberRepo == nil || r.PermissionService == nil {
		return ActorContext{}, ErrResolverMisconfigured
	}
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return ActorContext{}, ErrUnauthenticated
	}
	member, err := r.FamilyMemberRepo.FindActiveByUserID(ctx, userID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ActorContext{}, ErrUnauthenticated
		}
		return ActorContext{}, err
	}
	actor, err := r.actorFromLegacyMember(ctx, member, "", channel, false)
	if err != nil {
		return ActorContext{}, err
	}
	if actor.UserID != userID {
		return ActorContext{}, ErrUnauthenticated
	}
	return actor, nil
}

func (r *ResolverService) actorFromLegacyMember(ctx context.Context, member *domainfamilymember.ResolvedMember, externalID, channel string, external bool) (ActorContext, error) {
	if member == nil {
		return ActorContext{}, ErrUnauthenticated
	}
	userID := strings.TrimSpace(member.UserID)
	memberID := strings.TrimSpace(member.MemberID)
	familyID := strings.TrimSpace(member.FamilyID)
	roleID := strings.TrimSpace(member.RoleID)
	roleName := strings.TrimSpace(member.RoleName)
	resolvedProfileID := strings.TrimSpace(member.HermesProfileID)
	if userID == "" || memberID == "" || familyID == "" || roleID == "" || roleName == "" {
		return ActorContext{}, ErrUnauthenticated
	}
	if external && (resolvedProfileID == "" || resolvedProfileID != externalID) {
		return ActorContext{}, ErrUnauthenticated
	}
	permissions, err := r.permissionsForRole(ctx, roleID)
	if err != nil {
		return ActorContext{}, err
	}
	actor := ActorContext{
		UserID:          userID,
		MemberID:        memberID,
		FamilyID:        familyID,
		RoleID:          roleID,
		RoleName:        roleName,
		Permissions:     permissions,
		Source:          sourceForResolution(external),
		Channel:         strings.TrimSpace(channel),
		HermesProfileID: resolvedProfileID,
	}
	if !external {
		actor.HermesProfileID = ""
	}
	return actor, nil
}

func (r *ResolverService) permissionsForRole(ctx context.Context, roleID string) (map[string]struct{}, error) {
	permissions, err := r.PermissionService.GetRolePermissions(ctx, roleID)
	if err != nil {
		return nil, err
	}
	set := make(map[string]struct{}, len(permissions))
	for _, permission := range permissions {
		if key := authscope.PermissionKey(permission.Resource, permission.Action); key != "" {
			set[key] = struct{}{}
		}
	}
	return set, nil
}

func sourceForResolution(external bool) string {
	if external {
		return "mcp"
	}
	return "http"
}

var _ Resolver = (*ResolverService)(nil)
var _ UserResolver = (*ResolverService)(nil)
