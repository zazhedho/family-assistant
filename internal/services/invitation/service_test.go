package serviceinvitation

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
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
	"family-assistant/pkg/config"
)

type invitationRepositoryStub struct {
	created   *domaininvitation.Invitation
	createErr error
	accepted  *domainspace.Member
	acceptErr error
	tokenHash string
	userID    string
	email     string
}

func (s *invitationRepositoryStub) Create(_ context.Context, invitation *domaininvitation.Invitation) error {
	if s.createErr != nil {
		return s.createErr
	}
	copyInvitation := *invitation
	s.created = &copyInvitation
	return nil
}

func (s *invitationRepositoryStub) Accept(_ context.Context, tokenHash, userID, normalizedEmail string, _ time.Time) (*domainspace.Member, error) {
	s.tokenHash, s.userID, s.email = tokenHash, userID, normalizedEmail
	if s.acceptErr != nil {
		return nil, s.acceptErr
	}
	return s.accepted, nil
}

type invitationSpaceRepositoryStub struct {
	membership *domainspace.ResolvedMembership
	err        error
}

func (s *invitationSpaceRepositoryStub) CreateWithOwner(context.Context, *domainspace.Space, *domainspace.Member) error {
	return nil
}
func (s *invitationSpaceRepositoryStub) ListActiveByUserID(context.Context, string) ([]domainspace.ResolvedMembership, error) {
	return nil, nil
}
func (s *invitationSpaceRepositoryStub) FindActiveMembership(context.Context, string, string) (*domainspace.ResolvedMembership, error) {
	if s.err != nil {
		return nil, s.err
	}
	return s.membership, nil
}
func (s *invitationSpaceRepositoryStub) ListActiveMembers(context.Context, string) ([]domainspace.ResolvedMembership, error) {
	return nil, nil
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

func invitationService(repo *invitationRepositoryStub, audit *invitationAuditStub) Service {
	return NewService(
		repo,
		&invitationSpaceRepositoryStub{membership: &domainspace.ResolvedMembership{ID: "member-owner", SpaceID: "space-1", RoleID: "role-owner", Status: domainspace.StatusActive}},
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

	created, rawToken, err := service.Create(authscope.WithContext(context.Background(), authscope.New("user-1", "Owner", "space_owner", nil)), "user-1", CreateInput{
		SpaceID: "space-1", InvitedEmail: " Jane@Example.COM ", RoleName: "space_member",
	})
	if err != nil {
		t.Fatalf("create invitation: %v", err)
	}
	if created == nil || repo.created == nil || rawToken == "" {
		t.Fatalf("missing invitation/token: invitation=%#v token=%q", created, rawToken)
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

func TestCreateInvitationAllowsOnlyNonOwnerTargetRoles(t *testing.T) {
	for _, role := range []string{"space_admin", "space_member", "space_viewer"} {
		t.Run(role, func(t *testing.T) {
			repo := &invitationRepositoryStub{}
			service := invitationService(repo, &invitationAuditStub{})
			_, _, err := service.Create(context.Background(), "user-1", CreateInput{SpaceID: "space-1", RoleName: role})
			if err != nil {
				t.Fatalf("role %s rejected: %v", role, err)
			}
		})
	}
	for _, role := range []string{"space_owner", "parent", ""} {
		t.Run("reject-"+role, func(t *testing.T) {
			repo := &invitationRepositoryStub{}
			service := invitationService(repo, &invitationAuditStub{})
			_, _, err := service.Create(context.Background(), "user-1", CreateInput{SpaceID: "space-1", RoleName: role})
			var validationErr *ValidationError
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

	_, _, err := service.Create(context.Background(), "user-1", CreateInput{SpaceID: "space-1", RoleName: "space_member"})
	if !errors.Is(err, ErrForbidden) {
		t.Fatalf("error = %v, want forbidden", err)
	}
}

func TestAcceptInvitationHashesTokenAndNormalizesAuthenticatedEmail(t *testing.T) {
	repo := &invitationRepositoryStub{accepted: &domainspace.Member{ID: "member-2", SpaceID: "space-1", UserID: "user-2", RoleID: "role-member", Status: domainspace.StatusActive}}
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

var _ domaininvitation.Repository = (*invitationRepositoryStub)(nil)
