package servicespace

import (
	"context"
	"errors"
	"strings"
	"time"

	"family-assistant/internal/authscope"
	domainaudit "family-assistant/internal/domain/audit"
	domainpermission "family-assistant/internal/domain/permission"
	domainrole "family-assistant/internal/domain/role"
	domainspace "family-assistant/internal/domain/space"
	serviceauthorization "family-assistant/internal/services/authorization"
	"family-assistant/utils"
	"gorm.io/gorm"
)

const (
	createPermission = "spaces:create"
	listPermission   = "spaces:list"
	spaceOwnerRole   = "space_owner"
)

var (
	ErrForbidden = serviceauthorization.ErrForbidden
	ErrNotFound  = serviceauthorization.ErrNotFound
)

type ValidationError = serviceauthorization.ValidationError

type CreateInput struct {
	Name     string
	Category string
}

type Service interface {
	List(context.Context, string) ([]domainspace.ResolvedMembership, error)
	Create(context.Context, string, CreateInput) (*domainspace.Space, error)
	Members(context.Context, string, string) ([]domainspace.ResolvedMembership, error)
}

type roleRepository interface {
	GetByName(context.Context, string) (domainrole.Role, error)
}

type permissionRepository interface {
	GetRolePermissions(context.Context, string) ([]domainpermission.Permission, error)
}

type auditStore interface {
	Store(context.Context, domainaudit.AuditEvent) error
}

type service struct {
	spaces      domainspace.Repository
	roles       roleRepository
	permissions permissionRepository
	audit       auditStore
}

func NewService(spaces domainspace.Repository, roles roleRepository, dependencies ...any) Service {
	svc := &service{spaces: spaces, roles: roles}
	for _, dependency := range dependencies {
		switch value := dependency.(type) {
		case permissionRepository:
			svc.permissions = value
		case auditStore:
			svc.audit = value
		}
	}
	return svc
}

func NewSpaceService(spaces domainspace.Repository, roles roleRepository, dependencies ...any) Service {
	return NewService(spaces, roles, dependencies...)
}

func (s *service) List(ctx context.Context, userID string) ([]domainspace.ResolvedMembership, error) {
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return nil, &ValidationError{Field: "user_id", Reason: "is required"}
	}

	memberships, err := s.spaces.ListActiveByUserID(ctx, userID)
	if err != nil {
		s.writeFailure(ctx, "list", "", "", userID, err)
		return nil, err
	}

	allowed := make([]domainspace.ResolvedMembership, 0, len(memberships))
	for _, membership := range memberships {
		ok, err := s.hasPermission(ctx, membership.RoleID, "spaces", strings.TrimPrefix(listPermission, "spaces:"))
		if err != nil {
			s.writeFailure(ctx, "list", membership.SpaceID, membership.ID, userID, err)
			return nil, err
		}
		if !ok {
			continue
		}
		allowed = append(allowed, membership)
		s.writeSuccess(ctx, "list", membership.SpaceID, membership.ID, userID, "Listed space membership")
	}
	if len(memberships) > 0 && len(allowed) == 0 {
		err := ErrForbidden
		s.writeFailure(ctx, "list", "", "", userID, err)
		return nil, err
	}

	return allowed, nil
}

func (s *service) Create(ctx context.Context, userID string, input CreateInput) (created *domainspace.Space, err error) {
	userID = strings.TrimSpace(userID)
	if userID == "" {
		err = &ValidationError{Field: "user_id", Reason: "is required"}
		s.writeFailure(ctx, domainaudit.ActionCreate, "", "", userID, err)
		return nil, err
	}
	name := strings.TrimSpace(input.Name)
	if name == "" {
		err = &ValidationError{Field: "name", Reason: "is required"}
		s.writeFailure(ctx, domainaudit.ActionCreate, "", "", userID, err)
		return nil, err
	}
	category := strings.TrimSpace(input.Category)
	if !validSharedCategory(category) {
		err = &ValidationError{Field: "category", Reason: "must be one of family, friends, community, work, finance, custom"}
		s.writeFailure(ctx, domainaudit.ActionCreate, "", "", userID, err)
		return nil, err
	}

	memberships, err := s.spaces.ListActiveByUserID(ctx, userID)
	if err != nil {
		s.writeFailure(ctx, domainaudit.ActionCreate, "", "", userID, err)
		return nil, err
	}
	var actorMembership *domainspace.ResolvedMembership
	for i := range memberships {
		ok, permissionErr := s.hasPermission(ctx, memberships[i].RoleID, "spaces", strings.TrimPrefix(createPermission, "spaces:"))
		if permissionErr != nil {
			s.writeFailure(ctx, domainaudit.ActionCreate, memberships[i].SpaceID, memberships[i].ID, userID, permissionErr)
			return nil, permissionErr
		}
		if ok {
			actorMembership = &memberships[i]
			break
		}
	}
	if actorMembership == nil {
		err = ErrForbidden
		s.writeFailure(ctx, domainaudit.ActionCreate, "", "", userID, err)
		return nil, err
	}

	if s.roles == nil {
		err = errors.New("space role repository is not configured")
		s.writeFailure(ctx, domainaudit.ActionCreate, "", actorMembership.ID, userID, err)
		return nil, err
	}
	ownerRole, err := s.roles.GetByName(ctx, spaceOwnerRole)
	if err != nil {
		s.writeFailure(ctx, domainaudit.ActionCreate, "", actorMembership.ID, userID, err)
		return nil, err
	}
	if strings.TrimSpace(ownerRole.Id) == "" {
		err = errors.New("role space_owner is not configured")
		s.writeFailure(ctx, domainaudit.ActionCreate, "", actorMembership.ID, userID, err)
		return nil, err
	}

	now := time.Now().UTC()
	created = &domainspace.Space{
		ID:              utils.CreateUUID(),
		Name:            name,
		Type:            domainspace.TypeShared,
		Category:        category,
		Status:          domainspace.StatusActive,
		CreatedByUserID: userID,
		CreatedAt:       now,
	}
	owner := &domainspace.Member{
		ID:        utils.CreateUUID(),
		SpaceID:   created.ID,
		UserID:    userID,
		RoleID:    ownerRole.Id,
		Status:    domainspace.StatusActive,
		CreatedAt: now,
	}
	if err = s.spaces.CreateWithOwner(ctx, created, owner); err != nil {
		s.writeFailure(ctx, domainaudit.ActionCreate, created.ID, actorMembership.ID, userID, err)
		return nil, err
	}

	s.writeAudit(ctx, domainaudit.AuditEvent{
		Action:                domainaudit.ActionCreate,
		Resource:              "space",
		ResourceID:            created.ID,
		ActorUserID:           userID,
		ActorMemberID:         actorMembership.ID,
		ResourceOwnerMemberID: owner.ID,
		Source:                "http",
		Status:                domainaudit.StatusSuccess,
		Message:               "Created shared space",
		Metadata:              map[string]any{"space_id": created.ID},
	})
	return created, nil
}

func (s *service) Members(ctx context.Context, userID, spaceID string) ([]domainspace.ResolvedMembership, error) {
	userID = strings.TrimSpace(userID)
	spaceID = strings.TrimSpace(spaceID)
	if userID == "" {
		err := &ValidationError{Field: "user_id", Reason: "is required"}
		s.writeFailure(ctx, "list", spaceID, "", userID, err)
		return nil, err
	}
	if spaceID == "" {
		err := &ValidationError{Field: "space_id", Reason: "is required"}
		s.writeFailure(ctx, "list", spaceID, "", userID, err)
		return nil, err
	}

	actorMembership, err := s.spaces.FindActiveMembership(ctx, userID, spaceID)
	if err != nil {
		if errors.Is(err, ErrNotFound) || errors.Is(err, gorm.ErrRecordNotFound) {
			err = ErrNotFound
		}
		s.writeFailure(ctx, "list", spaceID, "", userID, err)
		return nil, err
	}
	if actorMembership == nil {
		err = ErrNotFound
		s.writeFailure(ctx, "list", spaceID, "", userID, err)
		return nil, err
	}

	ok, err := s.hasPermission(ctx, actorMembership.RoleID, "members", "list")
	if err != nil {
		s.writeFailure(ctx, "list", spaceID, actorMembership.ID, userID, err)
		return nil, err
	}
	if !ok {
		err = ErrForbidden
		s.writeFailure(ctx, "list", spaceID, actorMembership.ID, userID, err)
		return nil, err
	}

	members, err := s.spaces.ListActiveMembers(ctx, spaceID)
	if err != nil {
		s.writeFailure(ctx, "list", spaceID, actorMembership.ID, userID, err)
		return nil, err
	}
	s.writeSuccess(ctx, "list", spaceID, actorMembership.ID, userID, "Listed space members")
	return members, nil
}

func (s *service) hasPermission(ctx context.Context, roleID, resource, action string) (bool, error) {
	if s.permissions == nil {
		return authscope.FromContext(ctx).Has(resource, action), nil
	}
	permissions, err := s.permissions.GetRolePermissions(ctx, roleID)
	if err != nil {
		return false, err
	}
	want := authscope.PermissionKey(resource, action)
	for _, permission := range permissions {
		if authscope.PermissionKey(permission.Resource, permission.Action) == want {
			return true, nil
		}
	}
	return false, nil
}

func validSharedCategory(category string) bool {
	switch category {
	case domainspace.CategoryFamily, domainspace.CategoryFriends, domainspace.CategoryCommunity, domainspace.CategoryWork, domainspace.CategoryFinance, domainspace.CategoryCustom:
		return true
	default:
		return false
	}
}

func (s *service) writeSuccess(ctx context.Context, action, spaceID, memberID, userID, message string) {
	s.writeAudit(ctx, domainaudit.AuditEvent{
		Action:        action,
		Resource:      "space",
		ResourceID:    spaceID,
		ActorUserID:   userID,
		ActorMemberID: memberID,
		Source:        "http",
		Status:        domainaudit.StatusSuccess,
		Message:       message,
		Metadata:      map[string]any{"space_id": spaceID},
	})
}

func (s *service) writeFailure(ctx context.Context, action, spaceID, memberID, userID string, err error) {
	s.writeAudit(ctx, domainaudit.AuditEvent{
		Action:        action,
		Resource:      "space",
		ResourceID:    spaceID,
		ActorUserID:   userID,
		ActorMemberID: memberID,
		Source:        "http",
		Status:        domainaudit.StatusFailed,
		ErrorMessage:  err.Error(),
		Metadata:      map[string]any{"space_id": spaceID},
	})
}

func (s *service) writeAudit(ctx context.Context, event domainaudit.AuditEvent) {
	if s.audit != nil {
		_ = s.audit.Store(ctx, event)
	}
}

var _ Service = (*service)(nil)
