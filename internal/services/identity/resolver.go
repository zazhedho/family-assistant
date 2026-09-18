package serviceidentity

import (
	"context"
	"errors"
	"strings"

	"github.com/zazhedho/family-assistant/internal/authscope"
	domainfamilymember "github.com/zazhedho/family-assistant/internal/domain/familymember"
	domainpermission "github.com/zazhedho/family-assistant/internal/domain/permission"
	"gorm.io/gorm"
)

var (
	ErrUnauthenticated       = errors.New("unauthenticated")
	ErrResolverMisconfigured = errors.New("identity resolver is not configured")
)

type Resolver interface {
	Resolve(ctx context.Context, hermesProfileID, channel string) (ActorContext, error)
}

type UserResolver interface {
	ResolveUser(ctx context.Context, userID, channel string) (ActorContext, error)
}

type ResolverService struct {
	FamilyMemberRepo  domainfamilymember.Repository
	PermissionService interface {
		GetRolePermissions(context.Context, string) ([]domainpermission.Permission, error)
	}
}

func NewResolver(familyMemberRepo domainfamilymember.Repository, permissionService interface {
	GetRolePermissions(context.Context, string) ([]domainpermission.Permission, error)
}) *ResolverService {
	return &ResolverService{
		FamilyMemberRepo:  familyMemberRepo,
		PermissionService: permissionService,
	}
}

func (r *ResolverService) Resolve(ctx context.Context, hermesProfileID, channel string) (ActorContext, error) {
	if err := r.ensureConfigured(); err != nil {
		return ActorContext{}, err
	}
	profileID := strings.TrimSpace(hermesProfileID)
	if profileID == "" {
		return ActorContext{}, ErrUnauthenticated
	}

	member, err := r.FamilyMemberRepo.FindActiveByHermesProfile(ctx, profileID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ActorContext{}, ErrUnauthenticated
		}
		return ActorContext{}, err
	}
	return r.actorFromMember(ctx, member, profileID, channel, true)
}

func (r *ResolverService) ResolveUser(ctx context.Context, userID, channel string) (ActorContext, error) {
	if err := r.ensureConfigured(); err != nil {
		return ActorContext{}, err
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
	actor, err := r.actorFromMember(ctx, member, "", channel, false)
	if err != nil {
		return ActorContext{}, err
	}
	if actor.UserID != userID {
		return ActorContext{}, ErrUnauthenticated
	}
	return actor, nil
}

func (r *ResolverService) ensureConfigured() error {
	if r == nil || r.FamilyMemberRepo == nil || r.PermissionService == nil {
		return ErrResolverMisconfigured
	}
	return nil
}

func (r *ResolverService) actorFromMember(ctx context.Context, member *domainfamilymember.ResolvedMember, profileID, channel string, requireProfile bool) (ActorContext, error) {
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
	if requireProfile && (resolvedProfileID == "" || resolvedProfileID != profileID) {
		return ActorContext{}, ErrUnauthenticated
	}

	permissions, err := r.PermissionService.GetRolePermissions(ctx, roleID)
	if err != nil {
		return ActorContext{}, err
	}
	permissionSet := make(map[string]struct{}, len(permissions))
	for _, permission := range permissions {
		if key := authscope.PermissionKey(permission.Resource, permission.Action); key != "" {
			permissionSet[key] = struct{}{}
		}
	}

	actor := ActorContext{
		UserID:      userID,
		MemberID:    memberID,
		FamilyID:    familyID,
		RoleID:      roleID,
		RoleName:    roleName,
		Permissions: permissionSet,
		Channel:     strings.TrimSpace(channel),
	}
	if requireProfile {
		actor.HermesProfileID = resolvedProfileID
	}
	return actor, nil
}

var _ Resolver = (*ResolverService)(nil)
var _ UserResolver = (*ResolverService)(nil)
