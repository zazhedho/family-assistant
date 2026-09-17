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

var ErrUnauthenticated = errors.New("unauthenticated")

type Resolver interface {
	Resolve(ctx context.Context, hermesProfileID, channel string) (ActorContext, error)
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
	userID := ""
	memberID := ""
	familyID := ""
	roleID := ""
	roleName := ""
	resolvedProfileID := ""
	if member != nil {
		userID = strings.TrimSpace(member.UserID)
		memberID = strings.TrimSpace(member.MemberID)
		familyID = strings.TrimSpace(member.FamilyID)
		roleID = strings.TrimSpace(member.RoleID)
		roleName = strings.TrimSpace(member.RoleName)
		resolvedProfileID = strings.TrimSpace(member.HermesProfileID)
	}
	if userID == "" || memberID == "" || familyID == "" || roleID == "" || roleName == "" || resolvedProfileID == "" || resolvedProfileID != profileID {
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

	return ActorContext{
		UserID:          userID,
		MemberID:        memberID,
		FamilyID:        familyID,
		RoleID:          roleID,
		RoleName:        roleName,
		Permissions:     permissionSet,
		HermesProfileID: resolvedProfileID,
		Channel:         strings.TrimSpace(channel),
	}, nil
}

var _ Resolver = (*ResolverService)(nil)
