package servicespace

import (
	"context"
	"errors"
	"sort"
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
	createPermission  = "spaces:create"
	listPermission    = "spaces:list"
	membersPermission = "members:list"
	spaceOwnerRole    = "space_owner"
)

var (
	ErrForbidden                       = serviceauthorization.ErrForbidden
	ErrNotFound                        = serviceauthorization.ErrNotFound
	ErrPermissionRepositoryUnavailable = errors.New("membership permission repository is not configured")
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

type AuditProvenance struct {
	RequestID string
	IPAddress string
	UserAgent string
	Metadata  map[string]any
}

type auditProvenanceKey struct{}

func WithAuditProvenance(ctx context.Context, provenance AuditProvenance) context.Context {
	return context.WithValue(ctx, auditProvenanceKey{}, provenance)
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

func NewService(spaces domainspace.Repository, roles roleRepository, permissions permissionRepository, audit auditStore) Service {
	return &service{spaces: spaces, roles: roles, permissions: permissions, audit: audit}
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
	denied := make([]domainspace.ResolvedMembership, 0, len(memberships))
	for _, membership := range memberships {
		ok, err := s.hasPermission(ctx, membership.RoleID, listPermission)
		if err != nil {
			s.writeFailure(ctx, "list", membership.SpaceID, membership.ID, userID, err)
			return nil, err
		}
		if !ok {
			denied = append(denied, membership)
			continue
		}
		allowed = append(allowed, membership)
	}
	if len(memberships) > 0 && len(allowed) == 0 {
		err := ErrForbidden
		spaceID, memberID, metadata := auditCandidateDetails(denied)
		s.writeFailureWithDetails(ctx, "list", spaceID, memberID, "", userID, metadata, err)
		return nil, err
	}
	spaceID, memberID := "", ""
	spaceIDs := make([]string, 0, len(allowed))
	if len(allowed) > 0 {
		spaceID = allowed[0].SpaceID
		memberID = allowed[0].ID
		for _, membership := range allowed {
			spaceIDs = append(spaceIDs, membership.SpaceID)
		}
	}
	event := s.newAuditEvent(ctx, "list", spaceID, memberID, "", userID)
	event.Status = domainaudit.StatusSuccess
	event.Message = "Listed space memberships"
	event.Metadata = utils.MergeMetadata(event.Metadata, map[string]any{"space_ids": spaceIDs})
	s.writeAudit(ctx, event)

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
	denied := make([]domainspace.ResolvedMembership, 0, len(memberships))
	for i := range memberships {
		ok, permissionErr := s.hasPermission(ctx, memberships[i].RoleID, createPermission)
		if permissionErr != nil {
			denied = append(denied, memberships[i])
			spaceID, memberID, metadata := auditCandidateDetails(denied)
			s.writeFailureWithDetails(ctx, domainaudit.ActionCreate, spaceID, memberID, "", userID, metadata, permissionErr)
			return nil, permissionErr
		}
		if ok {
			actorMembership = &memberships[i]
			break
		}
		denied = append(denied, memberships[i])
	}
	if actorMembership == nil {
		err = ErrForbidden
		spaceID, memberID, metadata := auditCandidateDetails(denied)
		s.writeFailureWithDetails(ctx, domainaudit.ActionCreate, spaceID, memberID, "", userID, metadata, err)
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
		s.writeFailureWithDetails(ctx, domainaudit.ActionCreate, created.ID, actorMembership.ID, owner.ID, userID, nil, err)
		return nil, err
	}

	event := s.newAuditEvent(ctx, domainaudit.ActionCreate, created.ID, actorMembership.ID, owner.ID, userID)
	event.Status = domainaudit.StatusSuccess
	event.Message = "Created shared space"
	s.writeAudit(ctx, event)
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

	ok, err := s.hasPermission(ctx, actorMembership.RoleID, membersPermission)
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

func (s *service) hasPermission(ctx context.Context, roleID, permission string) (bool, error) {
	if s.permissions == nil {
		return false, ErrPermissionRepositoryUnavailable
	}
	permissions, err := s.permissions.GetRolePermissions(ctx, roleID)
	if err != nil {
		return false, err
	}
	parts := strings.SplitN(permission, ":", 2)
	if len(parts) != 2 {
		return false, &ValidationError{Field: "permission", Reason: "is invalid"}
	}
	want := authscope.PermissionKey(parts[0], parts[1])
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
	event := s.newAuditEvent(ctx, action, spaceID, memberID, "", userID)
	event.Status = domainaudit.StatusSuccess
	event.Message = message
	s.writeAudit(ctx, event)
}

func (s *service) writeFailure(ctx context.Context, action, spaceID, memberID, userID string, err error) {
	s.writeFailureWithDetails(ctx, action, spaceID, memberID, "", userID, nil, err)
}

func (s *service) writeFailureWithDetails(ctx context.Context, action, spaceID, memberID, ownerMemberID, userID string, metadata map[string]any, err error) {
	event := s.newAuditEvent(ctx, action, spaceID, memberID, ownerMemberID, userID)
	event.Status = domainaudit.StatusFailed
	event.Metadata = utils.MergeMetadata(event.Metadata, metadata)
	event.ErrorMessage = FailureCategory(err)
	s.writeAudit(ctx, event)
}

func auditCandidateDetails(memberships []domainspace.ResolvedMembership) (spaceID, memberID string, metadata map[string]any) {
	candidates := append([]domainspace.ResolvedMembership(nil), memberships...)
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].SpaceID == candidates[j].SpaceID {
			return candidates[i].ID < candidates[j].ID
		}
		return candidates[i].SpaceID < candidates[j].SpaceID
	})

	spaceIDs := make([]string, 0, len(candidates))
	memberIDs := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		spaceIDs = append(spaceIDs, candidate.SpaceID)
		memberIDs = append(memberIDs, candidate.ID)
	}
	metadata = map[string]any{"space_ids": spaceIDs, "member_ids": memberIDs}
	if len(candidates) == 1 {
		return candidates[0].SpaceID, candidates[0].ID, metadata
	}
	return "", "", metadata
}

func (s *service) newAuditEvent(ctx context.Context, action, spaceID, memberID, ownerMemberID, userID string) domainaudit.AuditEvent {
	scope := authscope.FromContext(ctx)
	actorUserID := scope.ActorUserID()
	if actorUserID == "" {
		actorUserID = userID
	}
	provenance, _ := ctx.Value(auditProvenanceKey{}).(AuditProvenance)
	metadata := utils.MergeMetadata(provenance.Metadata, map[string]any{"space_id": spaceID})
	if scope.IsImpersonated && strings.TrimSpace(scope.UserID) != actorUserID {
		metadata = utils.MergeMetadata(metadata, map[string]any{"subject_user_id": userID})
	}
	return domainaudit.AuditEvent{
		Action:                action,
		Resource:              "space",
		ResourceID:            spaceID,
		ActorUserID:           actorUserID,
		ActorMemberID:         memberID,
		ResourceOwnerMemberID: ownerMemberID,
		Source:                "http",
		ActorRole:             scope.ActorRole(),
		RequestID:             provenance.RequestID,
		IPAddress:             provenance.IPAddress,
		UserAgent:             provenance.UserAgent,
		Metadata:              metadata,
	}
}

func FailureCategory(err error) string {
	var validationErr *ValidationError
	switch {
	case errors.As(err, &validationErr), errors.Is(err, serviceauthorization.ErrInvalidResource):
		return "validation"
	case errors.Is(err, ErrForbidden):
		return "forbidden"
	case errors.Is(err, ErrNotFound), errors.Is(err, gorm.ErrRecordNotFound):
		return "not_found"
	default:
		return "internal"
	}
}

func (s *service) writeAudit(ctx context.Context, event domainaudit.AuditEvent) {
	if s.audit != nil {
		_ = s.audit.Store(ctx, event)
	}
}

var _ Service = (*service)(nil)
