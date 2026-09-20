package serviceinvitation

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"strings"
	"time"

	"family-assistant/internal/authscope"
	domainaudit "family-assistant/internal/domain/audit"
	domaininvitation "family-assistant/internal/domain/invitation"
	domainpermission "family-assistant/internal/domain/permission"
	domainrole "family-assistant/internal/domain/role"
	domainspace "family-assistant/internal/domain/space"
	domainuser "family-assistant/internal/domain/user"
	serviceauthorization "family-assistant/internal/services/authorization"
	"family-assistant/pkg/config"
	"family-assistant/utils"
	"gorm.io/gorm"
)

const createPermission = "invitations:create"

var (
	ErrForbidden                       = serviceauthorization.ErrForbidden
	ErrNotFound                        = serviceauthorization.ErrNotFound
	ErrInvalidInvitation               = domaininvitation.ErrInvalidInvitation
	ErrMembershipConflict              = domaininvitation.ErrMembershipConflict
	ErrPermissionRepositoryUnavailable = errors.New("membership permission repository is not configured")
)

type ValidationError = serviceauthorization.ValidationError

type CreateInput struct {
	SpaceID      string
	InvitedEmail string
	RoleName     string
}

type Service interface {
	Create(context.Context, string, CreateInput) (*domaininvitation.Invitation, string, error)
	Accept(context.Context, string, domainuser.Users) (*domainspace.Member, error)
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

type service struct {
	invitations domaininvitation.Repository
	spaces      domainspace.Repository
	roles       roleRepository
	permissions permissionRepository
	audit       auditStore
	config      config.InvitationConfig
	now         func() time.Time
}

func NewService(invitations domaininvitation.Repository, spaces domainspace.Repository, roles roleRepository, permissions permissionRepository, audit auditStore, configs ...config.InvitationConfig) Service {
	cfg := config.LoadInvitationConfig()
	if len(configs) > 0 {
		cfg = configs[0]
	}
	if cfg.TTL <= 0 {
		cfg.TTL = config.DefaultInvitationTTL
	}
	return &service{
		invitations: invitations,
		spaces:      spaces,
		roles:       roles,
		permissions: permissions,
		audit:       audit,
		config:      cfg,
		now:         func() time.Time { return time.Now().UTC() },
	}
}

func (s *service) Create(ctx context.Context, userID string, input CreateInput) (created *domaininvitation.Invitation, rawToken string, err error) {
	userID = strings.TrimSpace(userID)
	spaceID := strings.TrimSpace(input.SpaceID)
	roleName := utils.NormalizeKey(input.RoleName)
	if userID == "" {
		err = &ValidationError{Field: "user_id", Reason: "is required"}
		s.writeFailure(ctx, "", "", userID, err)
		return nil, "", err
	}
	if spaceID == "" {
		err = &ValidationError{Field: "space_id", Reason: "is required"}
		s.writeFailure(ctx, spaceID, "", userID, err)
		return nil, "", err
	}
	if !validTargetRole(roleName) {
		err = &ValidationError{Field: "role_name", Reason: "must be one of space_admin, space_member, space_viewer"}
		s.writeFailure(ctx, spaceID, roleName, userID, err)
		return nil, "", err
	}

	normalizedEmail := ""
	if strings.TrimSpace(input.InvitedEmail) != "" {
		normalizedEmail = utils.SanitizeEmail(input.InvitedEmail)
		if normalizedEmail == "" {
			err = &ValidationError{Field: "invited_email", Reason: "must be a valid email"}
			s.writeFailure(ctx, spaceID, roleName, userID, err)
			return nil, "", err
		}
	}
	if s.spaces == nil {
		err = errors.New("space repository is not configured")
		s.writeFailure(ctx, spaceID, roleName, userID, err)
		return nil, "", err
	}
	actorMembership, lookupErr := s.spaces.FindActiveMembership(ctx, userID, spaceID)
	if errors.Is(lookupErr, gorm.ErrRecordNotFound) {
		lookupErr = ErrNotFound
	}
	if lookupErr != nil {
		s.writeFailure(ctx, spaceID, roleName, userID, lookupErr)
		return nil, "", lookupErr
	}
	if actorMembership == nil {
		err = ErrNotFound
		s.writeFailure(ctx, spaceID, roleName, userID, err)
		return nil, "", err
	}
	if strings.TrimSpace(actorMembership.SpaceID) != spaceID {
		err = ErrNotFound
		s.writeFailure(ctx, spaceID, roleName, userID, err)
		return nil, "", err
	}
	if actorMembership.SpaceType != domainspace.TypeShared || actorMembership.Status != domainspace.StatusActive {
		err = ErrForbidden
		s.writeFailure(ctx, spaceID, roleName, userID, err)
		return nil, "", err
	}
	allowed, permissionErr := s.hasPermission(ctx, actorMembership.RoleID, createPermission)
	if permissionErr != nil {
		s.writeFailure(ctx, spaceID, roleName, userID, permissionErr)
		return nil, "", permissionErr
	}
	if !allowed {
		err = ErrForbidden
		s.writeFailure(ctx, spaceID, roleName, userID, err)
		return nil, "", err
	}
	if s.roles == nil {
		err = errors.New("space role repository is not configured")
		s.writeFailure(ctx, spaceID, roleName, userID, err)
		return nil, "", err
	}
	role, roleErr := s.roles.GetByName(ctx, roleName)
	if roleErr != nil {
		s.writeFailure(ctx, spaceID, roleName, userID, roleErr)
		return nil, "", roleErr
	}
	if strings.TrimSpace(role.Id) == "" {
		err = errors.New("invitation role is not configured")
		s.writeFailure(ctx, spaceID, roleName, userID, err)
		return nil, "", err
	}
	if s.invitations == nil {
		err = errors.New("invitation repository is not configured")
		s.writeFailure(ctx, spaceID, roleName, userID, err)
		return nil, "", err
	}

	rawToken, err = newToken()
	if err != nil {
		s.writeFailure(ctx, spaceID, roleName, userID, err)
		return nil, "", err
	}
	hash := sha256.Sum256([]byte(rawToken))
	now := s.now().UTC()
	created = &domaininvitation.Invitation{
		ID:                utils.CreateUUID(),
		SpaceID:           spaceID,
		InvitedEmail:      normalizedEmail,
		RoleID:            role.Id,
		InvitedByMemberID: actorMembership.ID,
		TokenHash:         hex.EncodeToString(hash[:]),
		Status:            domaininvitation.StatusPending,
		ExpiresAt:         now.Add(s.config.TTL),
		CreatedAt:         now,
		UpdatedAt:         now,
	}
	if err = s.invitations.Create(ctx, created); err != nil {
		s.writeFailure(ctx, spaceID, roleName, userID, err)
		return nil, "", err
	}
	event := s.newAuditEvent(ctx, domainaudit.ActionCreate, created.ID, spaceID, actorMembership.ID, userID, roleName, normalizedEmail != "")
	event.Status = domainaudit.StatusSuccess
	event.Message = "Created space invitation"
	s.writeAudit(ctx, event)
	return created, rawToken, nil
}

func (s *service) Accept(ctx context.Context, rawToken string, user domainuser.Users) (*domainspace.Member, error) {
	rawToken = strings.TrimSpace(rawToken)
	userID := strings.TrimSpace(user.Id)
	normalizedEmail := utils.SanitizeEmail(user.Email)
	if rawToken == "" || userID == "" || normalizedEmail == "" {
		s.writeFailure(ctx, "", "", userID, ErrInvalidInvitation)
		return nil, ErrInvalidInvitation
	}
	if s.invitations == nil {
		err := errors.New("invitation repository is not configured")
		s.writeFailure(ctx, "", "", userID, err)
		return nil, err
	}
	hash := sha256.Sum256([]byte(rawToken))
	acceptance, err := s.invitations.Accept(ctx, hex.EncodeToString(hash[:]), userID, normalizedEmail, s.now().UTC())
	if err != nil {
		s.writeFailure(ctx, "", "", userID, err)
		return nil, err
	}
	if acceptance == nil || acceptance.Member == nil {
		s.writeFailure(ctx, "", "", userID, ErrInvalidInvitation)
		return nil, ErrInvalidInvitation
	}
	roleName := strings.TrimSpace(acceptance.RoleName)
	if roleName == "" {
		roleName = acceptance.RoleID
	}
	event := s.newAuditEvent(ctx, domainaudit.ActionAccept, acceptance.InvitationID, acceptance.SpaceID, acceptance.Member.ID, userID, roleName, acceptance.EmailBound)
	event.Status = domainaudit.StatusSuccess
	event.Message = "Accepted space invitation"
	s.writeAudit(ctx, event)
	return acceptance.Member, nil
}

func validTargetRole(roleName string) bool {
	switch roleName {
	case "space_admin", "space_member", "space_viewer":
		return true
	default:
		return false
	}
}

func newToken() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
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
	for _, candidate := range permissions {
		if authscope.PermissionKey(candidate.Resource, candidate.Action) == want {
			return true, nil
		}
	}
	return false, nil
}

func (s *service) newAuditEvent(ctx context.Context, action, resourceID, spaceID, memberID, userID, roleName string, emailBound bool) domainaudit.AuditEvent {
	scope := authscope.FromContext(ctx)
	actorUserID := scope.ActorUserID()
	if actorUserID == "" {
		actorUserID = userID
	}
	provenance, _ := ctx.Value(auditProvenanceKey{}).(AuditProvenance)
	metadata := utils.MergeMetadata(provenance.Metadata, map[string]any{
		"space_id":    spaceID,
		"email_bound": emailBound,
	})
	if roleName != "" {
		metadata["role"] = roleName
	}
	if scope.IsImpersonated && strings.TrimSpace(scope.UserID) != actorUserID {
		metadata = utils.MergeMetadata(metadata, map[string]any{"subject_user_id": userID})
	}
	return domainaudit.AuditEvent{
		Action:        action,
		Resource:      "space_invitation",
		ResourceID:    resourceID,
		ActorUserID:   actorUserID,
		ActorMemberID: memberID,
		ActorRole:     scope.ActorRole(),
		Source:        "http",
		RequestID:     provenance.RequestID,
		IPAddress:     provenance.IPAddress,
		UserAgent:     provenance.UserAgent,
		Metadata:      metadata,
	}
}

func (s *service) writeFailure(ctx context.Context, spaceID, roleName, userID string, err error) {
	event := s.newAuditEvent(ctx, domainaudit.ActionCreate, "", spaceID, "", userID, roleName, false)
	event.Status = domainaudit.StatusFailed
	event.ErrorMessage = FailureCategory(err)
	s.writeAudit(ctx, event)
}

func FailureCategory(err error) string {
	var validationErr *ValidationError
	switch {
	case errors.As(err, &validationErr), errors.Is(err, ErrInvalidInvitation), errors.Is(err, serviceauthorization.ErrInvalidResource):
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
