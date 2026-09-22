package mcp

import (
	"context"
	"errors"
	"testing"

	domainidentity "family-assistant/internal/domain/identity"
	domaininvitation "family-assistant/internal/domain/invitation"
	domainpermission "family-assistant/internal/domain/permission"
	domainspace "family-assistant/internal/domain/space"
	domainuser "family-assistant/internal/domain/user"
	"family-assistant/internal/dto"
	interfaceinvitation "family-assistant/internal/interfaces/invitation"
	serviceidentity "family-assistant/internal/services/identity"
)

type mcpInvitationServiceStub struct {
	created      *domaininvitation.Invitation
	createToken  string
	createErr    error
	createUserID string
	createInput  dto.InvitationCreateInput
	createCalls  int
	accepted     *domainspace.Member
	acceptErr    error
	acceptToken  string
	acceptUser   domainuser.Users
	acceptCalls  int
	pending      []domaininvitation.Invitation
	listCalls    int
	listUser     string
	listSpace    string
	listErr      error
	revoked      *domaininvitation.Invitation
	revokeCalls  int
	revokeUser   string
	revokeSpace  string
	revokeID     string
	revokeErr    error
}

func (s *mcpInvitationServiceStub) Create(_ context.Context, userID string, input dto.InvitationCreateInput) (*domaininvitation.Invitation, string, error) {
	s.createCalls++
	s.createUserID, s.createInput = userID, input
	return s.created, s.createToken, s.createErr
}

func (s *mcpInvitationServiceStub) Accept(_ context.Context, token string, user domainuser.Users) (*domainspace.Member, error) {
	s.acceptCalls++
	s.acceptToken, s.acceptUser = token, user
	return s.accepted, s.acceptErr
}

func (s *mcpInvitationServiceStub) List(_ context.Context, userID, spaceID string) ([]domaininvitation.Invitation, error) {
	s.listCalls++
	s.listUser, s.listSpace = userID, spaceID
	return s.pending, s.listErr
}

func (s *mcpInvitationServiceStub) Revoke(_ context.Context, userID, spaceID, invitationID string) (*domaininvitation.Invitation, error) {
	s.revokeCalls++
	s.revokeUser, s.revokeSpace, s.revokeID = userID, spaceID, invitationID
	return s.revoked, s.revokeErr
}

func TestInvitationCreateSelectsAuthorizedSpaceAndUsesActor(t *testing.T) {
	spaceID := "00000000-0000-0000-0000-000000000101"
	resolver := &mcpExternalResolverStub{
		actor: domainidentity.ActorContext{UserID: "user-1", Memberships: []domainspace.ResolvedMembership{
			mcpMembership("member-shared", spaceID, "Trading", domainspace.TypeShared, "role-member"),
		}},
		permissions: []domainpermission.Permission{{Resource: "invitations", Action: "create"}},
	}
	service := &mcpInvitationServiceStub{
		created:     &domaininvitation.Invitation{ID: "invitation-1", SpaceID: spaceID, RoleID: "role-member"},
		createToken: "invite-token",
	}

	got, err := InvitationCreate(mcpExternalContext(), resolver, service, InvitationCreateInput{
		Space: " trading ", RoleName: "space_member",
	})
	if err != nil {
		t.Fatalf("invitation_create: %v", err)
	}
	if got.Token != "invite-token" || got.Invitation == nil || got.Invitation.ID != "invitation-1" {
		t.Fatalf("output = %+v", got)
	}
	if service.createCalls != 1 || service.createUserID != "user-1" || service.createInput.SpaceID != spaceID || service.createInput.RoleName != "space_member" || service.createInput.InvitedEmail != "" {
		t.Fatalf("service args = %+v", service)
	}
	if resolver.roleID != "role-member" {
		t.Fatalf("selected role = %q, want role-member", resolver.roleID)
	}
}

func TestInvitationAcceptUsesResolvedWhatsAppUserWithoutEmail(t *testing.T) {
	service := &mcpInvitationServiceStub{accepted: &domainspace.Member{ID: "member-2", UserID: "user-2"}}
	resolver := &mcpExternalResolverStub{actor: domainidentity.ActorContext{UserID: "user-2"}}

	got, err := InvitationAccept(mcpExternalContext(), resolver, service, InvitationAcceptInput{Token: " invite-token "})
	if err != nil {
		t.Fatalf("invitation_accept: %v", err)
	}
	if got == nil || got.UserID != "user-2" || service.acceptCalls != 1 || service.acceptToken != "invite-token" || service.acceptUser.Id != "user-2" || service.acceptUser.Email != "" {
		t.Fatalf("service/output = member:%+v service:%+v", got, service)
	}
}

func TestInvitationListSelectsAuthorizedSpaceAndReturnsSafeRows(t *testing.T) {
	spaceID := "00000000-0000-0000-0000-000000000101"
	resolver := &mcpExternalResolverStub{
		actor: domainidentity.ActorContext{UserID: "user-1", Memberships: []domainspace.ResolvedMembership{
			mcpMembership("member-owner", spaceID, "Trading", domainspace.TypeShared, "role-owner"),
		}},
		permissions: []domainpermission.Permission{{Resource: "invitations", Action: "list"}},
	}
	service := &mcpInvitationServiceStub{pending: []domaininvitation.Invitation{{ID: "invitation-1", SpaceID: spaceID, Status: domaininvitation.StatusPending}}}

	got, err := InvitationList(mcpExternalContext(), resolver, service, InvitationListInput{Space: "trading"})
	if err != nil {
		t.Fatalf("invitation_list: %v", err)
	}
	if len(got) != 1 || got[0].ID != "invitation-1" || service.listCalls != 1 || service.listUser != "user-1" || service.listSpace != spaceID {
		t.Fatalf("unexpected list: invitations=%+v service=%+v", got, service)
	}
}

func TestInvitationRevokeValidatesUUIDAndUsesTrustedActor(t *testing.T) {
	spaceID := "00000000-0000-0000-0000-000000000101"
	invitationID := "00000000-0000-0000-0000-000000000102"
	resolver := &mcpExternalResolverStub{
		actor: domainidentity.ActorContext{UserID: "user-1", Memberships: []domainspace.ResolvedMembership{
			mcpMembership("member-owner", spaceID, "Trading", domainspace.TypeShared, "role-owner"),
		}},
		permissions: []domainpermission.Permission{{Resource: "invitations", Action: "delete"}},
	}
	service := &mcpInvitationServiceStub{revoked: &domaininvitation.Invitation{ID: invitationID, Status: domaininvitation.StatusRevoked}}

	got, err := InvitationRevoke(mcpExternalContext(), resolver, service, InvitationRevokeInput{Space: spaceID, InvitationID: invitationID})
	if err != nil {
		t.Fatalf("invitation_revoke: %v", err)
	}
	if got == nil || got.ID != invitationID || service.revokeCalls != 1 || service.revokeUser != "user-1" || service.revokeSpace != spaceID || service.revokeID != invitationID {
		t.Fatalf("unexpected revoke: invitation=%+v service=%+v", got, service)
	}
}

func TestInvitationRevokeRejectsInvalidUUIDBeforeService(t *testing.T) {
	resolver := &mcpExternalResolverStub{actor: domainidentity.ActorContext{UserID: "user-1", Memberships: []domainspace.ResolvedMembership{
		mcpMembership("member-owner", "00000000-0000-0000-0000-000000000101", "Trading", domainspace.TypeShared, "role-owner"),
	}}, permissions: []domainpermission.Permission{{Resource: "invitations", Action: "delete"}}}
	service := &mcpInvitationServiceStub{}

	_, err := InvitationRevoke(mcpExternalContext(), resolver, service, InvitationRevokeInput{Space: "Trading", InvitationID: "bad"})
	var mapped *MCPError
	if !errors.As(err, &mapped) || mapped.Code != "invalid_input" || service.revokeCalls != 0 {
		t.Fatalf("error=%T %v service=%+v", err, err, service)
	}
}

func TestInvitationToolsRequireLinkedActor(t *testing.T) {
	resolver := &mcpExternalResolverStub{err: serviceidentity.ErrUnauthenticated}
	service := &mcpInvitationServiceStub{}

	if _, err := InvitationCreate(mcpExternalContext(), resolver, service, InvitationCreateInput{RoleName: "space_member"}); err == nil {
		t.Fatal("invitation_create accepted unlinked actor")
	} else {
		var mapped *MCPError
		if !errors.As(err, &mapped) || mapped.Code != "unauthenticated" {
			t.Fatalf("create error = %T %v, want unauthenticated", err, err)
		}
	}
	if _, err := InvitationAccept(mcpExternalContext(), resolver, service, InvitationAcceptInput{Token: "invite-token"}); err == nil {
		t.Fatal("invitation_accept accepted unlinked actor")
	}
	if service.createCalls != 0 || service.acceptCalls != 0 {
		t.Fatalf("unlinked actor reached service: %+v", service)
	}
}

var _ interfaceinvitation.ServiceInvitationInterface = (*mcpInvitationServiceStub)(nil)
