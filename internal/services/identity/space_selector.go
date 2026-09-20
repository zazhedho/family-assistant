package serviceidentity

import (
	"context"
	"errors"
	"strings"

	"family-assistant/internal/authscope"
	domainspace "family-assistant/internal/domain/space"

	"github.com/google/uuid"
)

var (
	ErrNotFound                    = errors.New("not found")
	ErrInvalidResource             = errors.New("invalid resource")
	ErrSpaceSelectionMisconfigured = errors.New("space selection is misconfigured")
)

type ValidationError struct {
	Field  string
	Reason string
}

func (e *ValidationError) Error() string {
	if e == nil {
		return ErrInvalidResource.Error()
	}
	if e.Field == "" {
		return ErrInvalidResource.Error() + ": " + e.Reason
	}
	return ErrInvalidResource.Error() + ": " + e.Field + " " + e.Reason
}

func (*ValidationError) Unwrap() error {
	return ErrInvalidResource
}

func SelectSpace(ctx context.Context, actor ActorContext, selector string, permissions PermissionLoader) (ActorContext, error) {
	if strings.TrimSpace(actor.UserID) == "" {
		return ActorContext{}, ErrUnauthenticated
	}

	active := activeMembershipsForUser(actor.UserID, actor.Memberships)
	if len(active) == 0 {
		return ActorContext{}, ErrNotFound
	}

	selector = strings.TrimSpace(selector)
	var selected *domainspace.ResolvedMembership
	if selector == "" {
		for i := range active {
			if strings.EqualFold(strings.TrimSpace(active[i].SpaceType), domainspace.TypePersonal) {
				if selected != nil {
					return ActorContext{}, ErrSpaceSelectionMisconfigured
				}
				selected = &active[i]
			}
		}
		if selected == nil {
			return ActorContext{}, ErrNotFound
		}
	} else {
		for i := range active {
			if sameSpaceID(active[i].SpaceID, selector) {
				selected = &active[i]
				break
			}
		}
		if selected == nil {
			matches := make([]*domainspace.ResolvedMembership, 0, 1)
			for i := range active {
				if sameSpaceName(active[i].SpaceName, selector) {
					matches = append(matches, &active[i])
				}
			}
			switch len(matches) {
			case 0:
				return ActorContext{}, ErrNotFound
			case 1:
				selected = matches[0]
			default:
				return ActorContext{}, &ValidationError{Field: "space", Reason: "matches multiple active Spaces"}
			}
		}
	}

	if selected == nil || strings.TrimSpace(selected.SpaceID) == "" || strings.TrimSpace(selected.ID) == "" || strings.TrimSpace(selected.RoleID) == "" || strings.TrimSpace(selected.RoleName) == "" {
		return ActorContext{}, ErrSpaceSelectionMisconfigured
	}
	if permissions == nil {
		return ActorContext{}, ErrResolverMisconfigured
	}
	permissionSet, err := permissionsForRole(ctx, permissions, selected.RoleID)
	if err != nil {
		return ActorContext{}, err
	}

	selectedActor := actor
	selectedActor.Memberships = active
	selectedActor.SpaceID = strings.TrimSpace(selected.SpaceID)
	selectedActor.SpaceName = strings.TrimSpace(selected.SpaceName)
	selectedActor.SpaceType = strings.TrimSpace(selected.SpaceType)
	selectedActor.MemberID = strings.TrimSpace(selected.ID)
	selectedActor.RoleID = strings.TrimSpace(selected.RoleID)
	selectedActor.RoleName = strings.TrimSpace(selected.RoleName)
	selectedActor.Permissions = permissionSet
	return selectedActor, nil
}

func activeMembershipsForUser(userID string, memberships []domainspace.ResolvedMembership) []domainspace.ResolvedMembership {
	active := make([]domainspace.ResolvedMembership, 0, len(memberships))
	userID = strings.TrimSpace(userID)
	for _, membership := range memberships {
		if strings.EqualFold(strings.TrimSpace(membership.Status), domainspace.StatusActive) && strings.TrimSpace(membership.UserID) == userID {
			active = append(active, membership)
		}
	}
	return active
}

func sameSpaceID(left, right string) bool {
	left = strings.TrimSpace(left)
	right = strings.TrimSpace(right)
	leftUUID, leftErr := uuid.Parse(left)
	rightUUID, rightErr := uuid.Parse(right)
	if leftErr == nil && rightErr == nil {
		return leftUUID == rightUUID
	}
	return left == right
}

func sameSpaceName(left, right string) bool {
	return strings.EqualFold(strings.TrimSpace(left), strings.TrimSpace(right))
}

func permissionsForRole(ctx context.Context, loader PermissionLoader, roleID string) (map[string]struct{}, error) {
	permissions, err := loader.GetRolePermissions(ctx, strings.TrimSpace(roleID))
	if err != nil {
		return nil, err
	}
	permissionSet := make(map[string]struct{}, len(permissions))
	for _, permission := range permissions {
		if key := authscope.PermissionKey(permission.Resource, permission.Action); key != "" {
			permissionSet[key] = struct{}{}
		}
	}
	return permissionSet, nil
}
