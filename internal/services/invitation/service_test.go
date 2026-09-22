package serviceinvitation

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"family-assistant/internal/authscope"
	domainaudit "family-assistant/internal/domain/audit"
	domaininvitation "family-assistant/internal/domain/invitation"
	domainpermission "family-assistant/internal/domain/permission"
	domainrole "family-assistant/internal/domain/role"
	domainspace "family-assistant/internal/domain/space"
	domainuser "family-assistant/internal/domain/user"
	"family-assistant/internal/dto"
	interfaceinvitation "family-assistant/internal/interfaces/invitation"
	serviceauthorization "family-assistant/internal/services/authorization"
	"family-assistant/pkg/config"
)

type invitationRepositoryStub struct {
	created     *domaininvitation.Invitation
	createErr   error
	accepted    *domaininvitation.Acceptance
	acceptErr   error
	tokenHash   string
	userID      string
	email       string
	pending     []domaininvitation.Invitation
	listErr     error
	listSpace   string
	revokeErr   error
	revokeID    string
	revokeSpace string
	revokeAt    time.Time
}

var _ interfaceinvitation.ServiceInvitationInterface = (*service)(nil)

func TestNewAuditEventKeepsParameterListWithinLimit(t *testing.T) {
	if got := reflect.TypeOf((&service{}).newAuditEvent).NumIn(); got > 7 {
		t.Fatalf("newAuditEvent parameters = %d, want <= 7", got)
	}
}

func (s *invitationRepositoryStub) Create(_ context.Context, invitation *domaininvitation.Invitation) error {
	if s.createErr != nil {
		return s.createErr
	}
	copyInvitation := *invitation
	s.created = &copyInvitation
	return nil
}

func (s *invitationRepositoryStub) Accept(_ context.Context, tokenHash, userID, normalizedEmail string, _ time.Time) (*domaininvitation.Acceptance, error) {
	s.tokenHash, s.userID, s.email = tokenHash, userID, normalizedEmail
	if s.acceptErr != nil {
		return nil, s.acceptErr
	}
	return s.accepted, nil
}

func (s *invitationRepositoryStub) ListPending(_ context.Context, spaceID string) ([]domaininvitation.Invitation, error) {
	s.listSpace = spaceID
	if s.listErr != nil {
		return nil, s.listErr
	}
	return append([]domaininvitation.Invitation(nil), s.pending...), nil
}

func (s *invitationRepositoryStub) RevokePending(_ context.Context, spaceID, invitationID string, revokedAt time.Time) error {
	s.revokeSpace, s.revokeID, s.revokeAt = spaceID, invitationID, revokedAt
	return s.revokeErr
}

type invitationSpaceRepositoryStub struct {
	membership *domainspace.ResolvedMembership
	err        error
	userID     string
	spaceID    string
}

func (s *invitationSpaceRepositoryStub) CreateWithOwner(context.Context, *domainspace.Space, *domainspace.Member) error {
	return nil
}
func (s *invitationSpaceRepositoryStub) ListActiveByUserID(context.Context, string) ([]domainspace.ResolvedMembership, error) {
	return nil, nil
}
func (s *invitationSpaceRepositoryStub) FindActiveMembership(_ context.Context, userID, spaceID string) (*domainspace.ResolvedMembership, error) {
	s.userID, s.spaceID = userID, spaceID
	if s.err != nil {
		return nil, s.err
	}
	return s.membership, nil
}
func (s *invitationSpaceRepositoryStub) ListActiveMembers(context.Context, string) ([]domainspace.ResolvedMembership, error) {
	return nil, nil
}
func (s *invitationSpaceRepositoryStub) Update(context.Context, string, domainspace.SpaceUpdateFields, time.Time) error {
	return nil
}
func (s *invitationSpaceRepositoryStub) Archive(context.Context, string, time.Time) error {
	return nil
}
func (s *invitationSpaceRepositoryStub) UpdateMemberRole(context.Context, string, string, string, time.Time) error {
	return nil
}
func (s *invitationSpaceRepositoryStub) RemoveMember(context.Context, string, string, time.Time) error {
	return nil
}
func (s *invitationSpaceRepositoryStub) CountActiveOwners(context.Context, string) (int64, error) {
	return 1, nil
}

type invitationRoleRepositoryStub struct {
	roles map[string]domainrole.Role
}

func (s *invitationRoleRepositoryStub) GetByName(_ context.Context, name string) (domainrole.Role, error) {
	role, ok := s.roles[name]
	if !ok {
		return domainrole.Role{}, errors.New("role not found")
	}
	return role, nil
}

type invitationPermissionRepositoryStub struct {
	permissions map[string][]domainpermission.Permission
}

func (s *invitationPermissionRepositoryStub) GetRolePermissions(_ context.Context, roleID string) ([]domainpermission.Permission, error) {
	return s.permissions[roleID], nil
}

type invitationAuditStub struct {
	events []domainaudit.AuditEvent
}

func (s *invitationAuditStub) Store(_ context.Context, event domainaudit.AuditEvent) error {
	s.events = append(s.events, event)
	return nil
}

func invitationService(repo *invitationRepositoryStub, audit *invitationAuditStub) interfaceinvitation.ServiceInvitationInterface {
	return NewService(
		repo,
		&invitationSpaceRepositoryStub{membership: &domainspace.ResolvedMembership{ID: "member-owner", SpaceID: "space-1", SpaceType: domainspace.TypeShared, RoleID: "role-owner", Status: domainspace.StatusActive}},
		&invitationRoleRepositoryStub{roles: map[string]domainrole.Role{
			"space_admin":  {Id: "role-admin", Name: "space_admin"},
			"space_member": {Id: "role-member", Name: "space_member"},
			"space_viewer": {Id: "role-viewer", Name: "space_viewer"},
		}},
		&invitationPermissionRepositoryStub{permissions: map[string][]domainpermission.Permission{
			"role-owner": {{Resource: "invitations", Action: "create"}},
		}},
		audit,
		config.InvitationConfig{TTL: 72 * time.Hour},
	)
}

func TestCreateInvitationHashesRandomTokenAndNormalizesEmail(t *testing.T) {
	repo := &invitationRepositoryStub{}
	audit := &invitationAuditStub{}
	service := invitationService(repo, audit)

	created, rawToken, err := service.Create(authscope.WithContext(context.Background(), authscope.New("user-1", "Owner", "space_owner", nil)), "user-1", dto.InvitationCreateInput{
		SpaceID: "space-1", InvitedEmail: " Jane@Example.COM ", RoleName: "space_member",
	})
	if err != nil {
		t.Fatalf("create invitation: %v", err)
	}
	if created == nil || repo.created == nil || rawToken == "" {
		t.Fatalf("missing invitation/token: invitation=%#v token=%q", created, rawToken)
	}
	if _, err := base64.RawURLEncoding.DecodeString(rawToken); err != nil {
		t.Fatalf("token is not unpadded URL-safe base64: %q: %v", rawToken, err)
	}
	if strings.Contains(repo.created.TokenHash, rawToken) || repo.created.TokenHash == rawToken {
		t.Fatalf("raw token persisted: hash=%q raw=%q", repo.created.TokenHash, rawToken)
	}
	hash := sha256.Sum256([]byte(rawToken))
	if repo.created.TokenHash != hex.EncodeToString(hash[:]) {
		t.Fatalf("token hash = %q, want SHA-256 hex", repo.created.TokenHash)
	}
	if repo.created.InvitedEmail != "jane@example.com" || repo.created.RoleID != "role-member" || repo.created.InvitedByMemberID != "member-owner" {
		t.Fatalf("unexpected invitation: %+v", repo.created)
	}
	if got := repo.created.ExpiresAt.Sub(repo.created.CreatedAt); got != 72*time.Hour {
		t.Fatalf("TTL = %s, want 72h", got)
	}
	if len(audit.events) != 1 || audit.events[0].ResourceID != repo.created.ID {
		t.Fatalf("missing create audit: %+v", audit.events)
	}
	if audit.events[0].Metadata["email_bound"] != true || audit.events[0].Metadata["space_id"] != "space-1" || audit.events[0].Metadata["role"] != "space_member" {
		t.Fatalf("unexpected audit metadata: %#v", audit.events[0].Metadata)
	}
}

func TestCreateInvitationTokensAreDistinct(t *testing.T) {
	firstRepo := &invitationRepositoryStub{}
	secondRepo := &invitationRepositoryStub{}
	service := invitationService(firstRepo, &invitationAuditStub{})
	_, first, err := service.Create(context.Background(), "user-1", dto.InvitationCreateInput{SpaceID: "space-1", RoleName: "space_member"})
	if err != nil {
		t.Fatalf("first create: %v", err)
	}
	service = invitationService(secondRepo, &invitationAuditStub{})
	_, second, err := service.Create(context.Background(), "user-1", dto.InvitationCreateInput{SpaceID: "space-1", RoleName: "space_member"})
	if err != nil {
		t.Fatalf("second create: %v", err)
	}
	if first == second {
		t.Fatalf("tokens repeated: %q", first)
	}
}

func TestCreateInvitationRejectsPersonalSpace(t *testing.T) {
	repo := &invitationRepositoryStub{}
	spaces := &invitationSpaceRepositoryStub{membership: &domainspace.ResolvedMembership{
		ID: "member-personal", SpaceID: "space-personal", SpaceType: domainspace.TypePersonal, RoleID: "role-owner", Status: domainspace.StatusActive,
	}}
	service := NewService(
		repo,
		spaces,
		&invitationRoleRepositoryStub{roles: map[string]domainrole.Role{"space_member": {Id: "role-member", Name: "space_member"}}},
		&invitationPermissionRepositoryStub{permissions: map[string][]domainpermission.Permission{"role-owner": {{Resource: "invitations", Action: "create"}}}},
		&invitationAuditStub{},
		config.InvitationConfig{TTL: time.Hour},
	)

	_, _, err := service.Create(context.Background(), "user-1", dto.InvitationCreateInput{SpaceID: "space-personal", RoleName: "space_member"})
	if !errors.Is(err, ErrForbidden) {
		t.Fatalf("error = %v, want forbidden for Personal Space", err)
	}
	if repo.created != nil {
		t.Fatal("Personal Space invitation was persisted")
	}
}

func TestCreateInvitationScopesMembershipLookupToAuthenticatedUserAndSpace(t *testing.T) {
	spaces := &invitationSpaceRepositoryStub{membership: &domainspace.ResolvedMembership{
		ID: "member-owner", SpaceID: "space-1", SpaceType: domainspace.TypeShared, RoleID: "role-owner", Status: domainspace.StatusActive,
	}}
	repo := &invitationRepositoryStub{}
	service := NewService(
		repo,
		spaces,
		&invitationRoleRepositoryStub{roles: map[string]domainrole.Role{"space_member": {Id: "role-member", Name: "space_member"}}},
		&invitationPermissionRepositoryStub{permissions: map[string][]domainpermission.Permission{"role-owner": {{Resource: "invitations", Action: "create"}}}},
		&invitationAuditStub{},
		config.InvitationConfig{TTL: time.Hour},
	)

	_, _, err := service.Create(context.Background(), "authenticated-user", dto.InvitationCreateInput{SpaceID: "space-1", RoleName: "space_member"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if spaces.userID != "authenticated-user" || spaces.spaceID != "space-1" {
		t.Fatalf("lookup scope = user %q space %q", spaces.userID, spaces.spaceID)
	}
}

func TestCreateInvitationAllowsOnlyNonOwnerTargetRoles(t *testing.T) {
	for _, role := range []string{"space_admin", "space_member", "space_viewer"} {
		t.Run(role, func(t *testing.T) {
			repo := &invitationRepositoryStub{}
			service := invitationService(repo, &invitationAuditStub{})
			_, _, err := service.Create(context.Background(), "user-1", dto.InvitationCreateInput{SpaceID: "space-1", RoleName: role})
			if err != nil {
				t.Fatalf("role %s rejected: %v", role, err)
			}
		})
	}
	for _, role := range []string{"space_owner", "parent", ""} {
		t.Run("reject-"+role, func(t *testing.T) {
			repo := &invitationRepositoryStub{}
			service := invitationService(repo, &invitationAuditStub{})
			_, _, err := service.Create(context.Background(), "user-1", dto.InvitationCreateInput{SpaceID: "space-1", RoleName: role})
			var validationErr *serviceauthorization.ValidationError
			if !errors.As(err, &validationErr) || validationErr.Field != "role_name" {
				t.Fatalf("error = %v, want role_name validation", err)
			}
		})
	}
}

func TestCreateInvitationRequiresInvitationPermission(t *testing.T) {
	repo := &invitationRepositoryStub{}
	service := NewService(
		repo,
		&invitationSpaceRepositoryStub{membership: &domainspace.ResolvedMembership{ID: "member-viewer", SpaceID: "space-1", RoleID: "role-viewer"}},
		&invitationRoleRepositoryStub{roles: map[string]domainrole.Role{"space_member": {Id: "role-member", Name: "space_member"}}},
		&invitationPermissionRepositoryStub{permissions: map[string][]domainpermission.Permission{"role-viewer": {{Resource: "spaces", Action: "view"}}}},
		&invitationAuditStub{},
		config.InvitationConfig{TTL: time.Hour},
	)

	_, _, err := service.Create(context.Background(), "user-1", dto.InvitationCreateInput{SpaceID: "space-1", RoleName: "space_member"})
	if !errors.Is(err, ErrForbidden) {
		t.Fatalf("error = %v, want forbidden", err)
	}
}

func TestAcceptInvitationHashesTokenAndNormalizesAuthenticatedEmail(t *testing.T) {
	repo := &invitationRepositoryStub{accepted: &domaininvitation.Acceptance{InvitationID: "invitation-1", SpaceID: "space-1", RoleID: "role-member", RoleName: "space_member", EmailBound: true, Member: &domainspace.Member{ID: "member-2", SpaceID: "space-1", UserID: "user-2", RoleID: "role-member", Status: domainspace.StatusActive}}}
	service := invitationService(repo, &invitationAuditStub{})
	user := domainuser.Users{Id: "user-2", Email: " Jane@Example.COM "}

	member, err := service.Accept(context.Background(), "raw-token", user)
	if err != nil {
		t.Fatalf("accept: %v", err)
	}
	if member == nil || repo.userID != user.Id || repo.email != "jane@example.com" {
		t.Fatalf("unexpected accept args: member=%#v user=%q email=%q", member, repo.userID, repo.email)
	}
	hash := sha256.Sum256([]byte("raw-token"))
	if repo.tokenHash != hex.EncodeToString(hash[:]) {
		t.Fatalf("token hash = %q, want SHA-256 hex", repo.tokenHash)
	}
}

func TestAcceptInvitationAuditsCommittedInvitationMetadataAndProvenance(t *testing.T) {
	audit := &invitationAuditStub{}
	repo := &invitationRepositoryStub{accepted: &domaininvitation.Acceptance{
		InvitationID: "invitation-1", SpaceID: "space-1", RoleID: "role-member", RoleName: "space_member", EmailBound: true,
		Member: &domainspace.Member{ID: "member-2", SpaceID: "space-1", UserID: "subject-2", RoleID: "role-member", Status: domainspace.StatusActive},
	}}
	service := invitationService(repo, audit)
	scope := authscope.New("subject-2", "Subject", "space_member", nil)
	scope.IsImpersonated = true
	scope.OriginalUserID = "actor-1"
	scope.OriginalRole = "space_owner"
	ctx := authscope.WithContext(WithAuditProvenance(context.Background(), AuditProvenance{RequestID: "request-1", IPAddress: "192.0.2.1", UserAgent: "agent-1"}), scope)

	if _, err := service.Accept(ctx, "raw-token", domainuser.Users{Id: "subject-2", Email: "jane@example.com"}); err != nil {
		t.Fatalf("accept: %v", err)
	}
	if len(audit.events) != 1 {
		t.Fatalf("audit events = %+v", audit.events)
	}
	event := audit.events[0]
	if event.Resource != "space_invitation" || event.ResourceID != "invitation-1" || event.ActorUserID != "actor-1" || event.ActorMemberID != "member-2" || event.RequestID != "request-1" || event.IPAddress != "192.0.2.1" || event.UserAgent != "agent-1" {
		t.Fatalf("unexpected acceptance audit: %+v", event)
	}
	if event.Metadata["space_id"] != "space-1" || event.Metadata["role"] != "space_member" || event.Metadata["email_bound"] != true || event.Metadata["subject_user_id"] != "subject-2" {
		t.Fatalf("unexpected acceptance metadata: %#v", event.Metadata)
	}
	encoded, _ := json.Marshal(event)
	if strings.Contains(string(encoded), "raw-token") || strings.Contains(string(encoded), "token_hash") || strings.Contains(string(encoded), "request_body") {
		t.Fatalf("sensitive acceptance audit: %s", encoded)
	}
}

func TestAcceptInvitationAuditsUnboundEmailAsFalse(t *testing.T) {
	audit := &invitationAuditStub{}
	repo := &invitationRepositoryStub{accepted: &domaininvitation.Acceptance{
		InvitationID: "invitation-2", SpaceID: "space-1", RoleID: "role-viewer", RoleName: "space_viewer", EmailBound: false,
		Member: &domainspace.Member{ID: "member-3", SpaceID: "space-1", UserID: "user-3", RoleID: "role-viewer", Status: domainspace.StatusActive},
	}}
	service := invitationService(repo, audit)
	if _, err := service.Accept(context.Background(), "raw-token", domainuser.Users{Id: "user-3", Email: "jane@example.com"}); err != nil {
		t.Fatalf("accept: %v", err)
	}
	if got := audit.events[0].Metadata["email_bound"]; got != false {
		t.Fatalf("email_bound = %v, want false", got)
	}
}

func TestAcceptUnboundInvitationAllowsEmaillessWhatsAppUser(t *testing.T) {
	repo := &invitationRepositoryStub{accepted: &domaininvitation.Acceptance{
		InvitationID: "invitation-whatsapp", SpaceID: "space-1", RoleID: "role-member", RoleName: "space_member",
		Member: &domainspace.Member{ID: "member-whatsapp", SpaceID: "space-1", UserID: "user-whatsapp", RoleID: "role-member", Status: domainspace.StatusActive},
	}}
	service := invitationService(repo, &invitationAuditStub{})

	member, err := service.Accept(context.Background(), "raw-token", domainuser.Users{Id: "user-whatsapp"})
	if err != nil {
		t.Fatalf("accept unbound WhatsApp invitation: %v", err)
	}
	if member == nil || repo.userID != "user-whatsapp" || repo.email != "" {
		t.Fatalf("unexpected accept args: member=%#v user=%q email=%q", member, repo.userID, repo.email)
	}
}

func TestAcceptInvitationInvalidTokensShareSafeError(t *testing.T) {
	for _, token := range []string{"replayed", "expired", "guessed"} {
		t.Run(token, func(t *testing.T) {
			repo := &invitationRepositoryStub{acceptErr: domaininvitation.ErrInvalidInvitation}
			service := invitationService(repo, &invitationAuditStub{})
			_, err := service.Accept(context.Background(), token, domainuser.Users{Id: "user-2", Email: "jane@example.com"})
			if !errors.Is(err, ErrInvalidInvitation) {
				t.Fatalf("error = %v, want ErrInvalidInvitation", err)
			}
		})
	}
}

func TestListPendingInvitationsRequiresPermissionAndStripsTokenHash(t *testing.T) {
	repo := &invitationRepositoryStub{pending: []domaininvitation.Invitation{{ID: "invitation-1", SpaceID: "space-1", TokenHash: "secret", Status: domaininvitation.StatusPending}}}
	spaces := &invitationSpaceRepositoryStub{membership: &domainspace.ResolvedMembership{ID: "member-owner", SpaceID: "space-1", SpaceType: domainspace.TypeShared, RoleID: "role-owner", Status: domainspace.StatusActive}}
	service := NewService(repo, spaces, nil, &invitationPermissionRepositoryStub{permissions: map[string][]domainpermission.Permission{
		"role-owner": {{Resource: "invitations", Action: "list"}},
	}}, &invitationAuditStub{}).(*service)

	got, err := service.List(context.Background(), "user-1", "space-1")
	if err != nil {
		t.Fatalf("list invitations: %v", err)
	}
	if len(got) != 1 || got[0].TokenHash != "" || repo.listSpace != "space-1" {
		t.Fatalf("unexpected list: invitations=%+v repo=%+v", got, repo)
	}
}

func TestListPendingInvitationsDeniesViewer(t *testing.T) {
	repo := &invitationRepositoryStub{}
	spaces := &invitationSpaceRepositoryStub{membership: &domainspace.ResolvedMembership{ID: "member-viewer", SpaceID: "space-1", SpaceType: domainspace.TypeShared, RoleID: "role-viewer", Status: domainspace.StatusActive}}
	service := NewService(repo, spaces, nil, &invitationPermissionRepositoryStub{permissions: map[string][]domainpermission.Permission{
		"role-viewer": {{Resource: "spaces", Action: "view"}},
	}}, &invitationAuditStub{}).(*service)

	_, err := service.List(context.Background(), "user-1", "space-1")
	if !errors.Is(err, ErrForbidden) {
		t.Fatalf("error = %v, want forbidden", err)
	}
	if repo.listSpace != "" {
		t.Fatal("viewer reached invitation repository")
	}
}

func TestRevokePendingInvitationUpdatesStatusAndAudits(t *testing.T) {
	repo := &invitationRepositoryStub{pending: []domaininvitation.Invitation{{ID: "invitation-1", SpaceID: "space-1", TokenHash: "secret", Status: domaininvitation.StatusPending}}}
	spaces := &invitationSpaceRepositoryStub{membership: &domainspace.ResolvedMembership{ID: "member-admin", SpaceID: "space-1", SpaceType: domainspace.TypeShared, RoleID: "role-admin", Status: domainspace.StatusActive}}
	audit := &invitationAuditStub{}
	service := NewService(repo, spaces, nil, &invitationPermissionRepositoryStub{permissions: map[string][]domainpermission.Permission{
		"role-admin": {{Resource: "invitations", Action: "delete"}},
	}}, audit).(*service)

	got, err := service.Revoke(context.Background(), "user-1", "space-1", "invitation-1")
	if err != nil {
		t.Fatalf("revoke invitation: %v", err)
	}
	if got == nil || got.Status != domaininvitation.StatusRevoked || got.TokenHash != "" || repo.revokeID != "invitation-1" || len(audit.events) != 1 || audit.events[0].Action != "delete" {
		t.Fatalf("unexpected revoke: invitation=%+v repo=%+v audit=%+v", got, repo, audit.events)
	}
}

func TestRevokePendingInvitationRejectsUnknownOrCompletedInvitation(t *testing.T) {
	repo := &invitationRepositoryStub{pending: []domaininvitation.Invitation{{ID: "other", SpaceID: "space-1", Status: domaininvitation.StatusPending}}}
	spaces := &invitationSpaceRepositoryStub{membership: &domainspace.ResolvedMembership{ID: "member-admin", SpaceID: "space-1", SpaceType: domainspace.TypeShared, RoleID: "role-admin", Status: domainspace.StatusActive}}
	service := NewService(repo, spaces, nil, &invitationPermissionRepositoryStub{permissions: map[string][]domainpermission.Permission{
		"role-admin": {{Resource: "invitations", Action: "delete"}},
	}}, &invitationAuditStub{}).(*service)

	_, err := service.Revoke(context.Background(), "user-1", "space-1", "invitation-1")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("error = %v, want not found", err)
	}
	if repo.revokeID != "" {
		t.Fatal("unknown invitation reached revoke repository")
	}
}

var _ interfaceinvitation.RepoInvitationInterface = (*invitationRepositoryStub)(nil)
