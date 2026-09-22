package mcp

import (
	"context"
	"errors"
	"testing"

	domainidentity "family-assistant/internal/domain/identity"
	domainpermission "family-assistant/internal/domain/permission"
	domainspace "family-assistant/internal/domain/space"
	"family-assistant/internal/dto"
	interfacespace "family-assistant/internal/interfaces/space"
	serviceidentity "family-assistant/internal/services/identity"
)

type mcpSpaceServiceStub struct {
	spaces       []domainspace.ResolvedMembership
	members      []domainspace.ResolvedMembership
	created      *domainspace.Space
	space        *domainspace.Space
	member       *domainspace.ResolvedMembership
	createErr    error
	createUserID string
	createInput  dto.SpaceCreateInput
	createCalls  int
	listErr      error
	membersErr   error
	listUserID   string
	membersUser  string
	membersSpace string
	listCalls    int
	membersCalls int
	updateCalls  int
	updateUser   string
	updateSpace  string
	updateInput  dto.SpaceUpdateInput
	archiveCalls int
	archiveUser  string
	archiveSpace string
	roleCalls    int
	roleUser     string
	roleSpace    string
	roleMember   string
	roleName     string
	removeCalls  int
	removeUser   string
	removeSpace  string
	removeMember string
}

func (s *mcpSpaceServiceStub) List(_ context.Context, userID string) ([]domainspace.ResolvedMembership, error) {
	s.listCalls++
	s.listUserID = userID
	return s.spaces, s.listErr
}

func (s *mcpSpaceServiceStub) Create(_ context.Context, userID string, input dto.SpaceCreateInput) (*domainspace.Space, error) {
	s.createCalls++
	s.createUserID = userID
	s.createInput = input
	return s.created, s.createErr
}

func (s *mcpSpaceServiceStub) Members(_ context.Context, userID, spaceID string) ([]domainspace.ResolvedMembership, error) {
	s.membersCalls++
	s.membersUser, s.membersSpace = userID, spaceID
	return s.members, s.membersErr
}

func (s *mcpSpaceServiceStub) Update(_ context.Context, userID, spaceID string, input dto.SpaceUpdateInput) (*domainspace.Space, error) {
	s.updateCalls++
	s.updateUser, s.updateSpace, s.updateInput = userID, spaceID, input
	return s.space, nil
}

func (s *mcpSpaceServiceStub) Archive(_ context.Context, userID, spaceID string) (*domainspace.Space, error) {
	s.archiveCalls++
	s.archiveUser, s.archiveSpace = userID, spaceID
	return s.space, nil
}

func (s *mcpSpaceServiceStub) UpdateMemberRole(_ context.Context, userID, spaceID, memberID string, input dto.MemberRoleUpdateInput) (*domainspace.ResolvedMembership, error) {
	s.roleCalls++
	s.roleUser, s.roleSpace, s.roleMember, s.roleName = userID, spaceID, memberID, input.Role
	return s.member, nil
}

func (s *mcpSpaceServiceStub) RemoveMember(_ context.Context, userID, spaceID, memberID string) (*domainspace.ResolvedMembership, error) {
	s.removeCalls++
	s.removeUser, s.removeSpace, s.removeMember = userID, spaceID, memberID
	return s.member, nil
}

type mcpExternalResolverStub struct {
	actor       domainidentity.ActorContext
	permissions []domainpermission.Permission
	err         error
	roleID      string
}

func (s *mcpExternalResolverStub) ResolveExternal(context.Context, string, string, string) (domainidentity.ActorContext, error) {
	return s.actor, s.err
}

func (s *mcpExternalResolverStub) GetRolePermissions(_ context.Context, roleID string) ([]domainpermission.Permission, error) {
	s.roleID = roleID
	return s.permissions, nil
}

func mcpMembership(id, spaceID, name, spaceType, roleID string) domainspace.ResolvedMembership {
	return domainspace.ResolvedMembership{
		ID: id, SpaceID: spaceID, SpaceName: name, SpaceType: spaceType,
		UserID: "user-1", RoleID: roleID, RoleName: "space_member", Status: domainspace.StatusActive,
	}
}

func mcpExternalContext() context.Context {
	return WithExternalRequest(context.Background(), ExternalRequest{Provider: "hermes", ExternalID: "profile-1", Channel: "whatsapp"})
}

func TestSpaceListUsesResolvedActorUserID(t *testing.T) {
	service := &mcpSpaceServiceStub{spaces: []domainspace.ResolvedMembership{{
		SpaceID: "00000000-0000-0000-0000-000000000001", SpaceName: "Personal", UserID: "user-1",
	}}}
	resolver := &mcpExternalResolverStub{actor: domainidentity.ActorContext{UserID: "user-1"}}

	got, err := SpaceList(mcpExternalContext(), resolver, service)
	if err != nil {
		t.Fatalf("space_list: %v", err)
	}
	if len(got) != 1 || got[0].SpaceID != "00000000-0000-0000-0000-000000000001" || service.listCalls != 1 || service.listUserID != "user-1" {
		t.Fatalf("unexpected space_list: got=%+v calls=%d user=%q", got, service.listCalls, service.listUserID)
	}
}

func TestSpaceCreateUsesResolvedActorUserIDAndInput(t *testing.T) {
	service := &mcpSpaceServiceStub{created: &domainspace.Space{ID: "space-1", Name: "Family", Category: domainspace.CategoryFamily}}
	resolver := &mcpExternalResolverStub{actor: domainidentity.ActorContext{UserID: "user-1"}}

	got, err := SpaceCreate(mcpExternalContext(), resolver, service, SpaceCreateInput{Name: " Family ", Category: "family"})
	if err != nil {
		t.Fatalf("space_create: %v", err)
	}
	if got == nil || got.ID != "space-1" || service.createCalls != 1 || service.createUserID != "user-1" || service.createInput.Name != " Family " || service.createInput.Category != "family" {
		t.Fatalf("unexpected space_create: got=%+v service=%+v", got, service)
	}
}

func TestSpaceCreateRequiresLinkedActor(t *testing.T) {
	service := &mcpSpaceServiceStub{}
	resolver := &mcpExternalResolverStub{err: serviceidentity.ErrUnauthenticated}

	_, err := SpaceCreate(mcpExternalContext(), resolver, service, SpaceCreateInput{Name: "Family", Category: "family"})
	var mapped *MCPError
	if !errors.As(err, &mapped) || mapped.Code != "unauthenticated" {
		t.Fatalf("error = %T %v, want unauthenticated MCP error", err, err)
	}
	if service.createCalls != 0 {
		t.Fatal("unlinked actor reached space service")
	}
}

func TestSpaceGetMembersSelectsUUIDOrNameFromActorMemberships(t *testing.T) {
	spaceID := "00000000-0000-0000-0000-000000000001"
	resolver := &mcpExternalResolverStub{
		actor: domainidentity.ActorContext{UserID: "user-1", Memberships: []domainspace.ResolvedMembership{
			mcpMembership("member-personal", "00000000-0000-0000-0000-000000000002", "Jane", domainspace.TypePersonal, "role-owner"),
			mcpMembership("member-shared", spaceID, "Trading", domainspace.TypeShared, "role-member"),
		}},
		permissions: []domainpermission.Permission{{Resource: "members", Action: "list"}},
	}
	service := &mcpSpaceServiceStub{members: []domainspace.ResolvedMembership{{SpaceID: spaceID, UserID: "user-1"}}}

	got, err := SpaceGetMembers(mcpExternalContext(), resolver, service, SpaceGetMembersInput{Space: " trading "})
	if err != nil {
		t.Fatalf("space_get_members: %v", err)
	}
	if len(got) != 1 || service.membersCalls != 1 || service.membersUser != "user-1" || service.membersSpace != spaceID || resolver.roleID != "role-member" {
		t.Fatalf("unexpected member lookup: got=%+v service=%+v role=%q", got, service, resolver.roleID)
	}
}

func TestSpaceGetMembersMapsAmbiguousNameToInvalidInput(t *testing.T) {
	resolver := &mcpExternalResolverStub{
		actor: domainidentity.ActorContext{UserID: "user-1", Memberships: []domainspace.ResolvedMembership{
			mcpMembership("member-1", "space-1", "Trading", domainspace.TypeShared, "role-1"),
			mcpMembership("member-2", "space-2", " trading ", domainspace.TypeShared, "role-2"),
		}},
		permissions: []domainpermission.Permission{{Resource: "members", Action: "list"}},
	}
	service := &mcpSpaceServiceStub{}

	_, err := SpaceGetMembers(mcpExternalContext(), resolver, service, SpaceGetMembersInput{Space: "TRADING"})
	var mapped *MCPError
	if !errors.As(err, &mapped) || mapped.Code != "invalid_input" || mapped.Message != "invalid input" {
		t.Fatalf("error = %T %v, want safe invalid input", err, err)
	}
	if service.membersCalls != 0 {
		t.Fatal("ambiguous Space name reached service")
	}
}

func TestSpaceUpdateSelectsSpaceAndPassesPatch(t *testing.T) {
	spaceID := "00000000-0000-0000-0000-000000000001"
	name := "Household"
	resolver := &mcpExternalResolverStub{
		actor: domainidentity.ActorContext{UserID: "user-1", Memberships: []domainspace.ResolvedMembership{
			mcpMembership("member-1", spaceID, "Trading", domainspace.TypeShared, "role-admin"),
		}},
		permissions: []domainpermission.Permission{{Resource: "spaces", Action: "update"}},
	}
	service := &mcpSpaceServiceStub{space: &domainspace.Space{ID: spaceID}}

	updated, err := SpaceUpdate(mcpExternalContext(), resolver, service, SpaceUpdateInput{Space: spaceID, Name: &name})
	if err != nil {
		t.Fatalf("space_update: %v", err)
	}
	if updated == nil || service.updateCalls != 1 || service.updateUser != "user-1" || service.updateSpace != spaceID || service.updateInput.Name == nil || *service.updateInput.Name != name {
		t.Fatalf("unexpected update: result=%+v service=%+v", updated, service)
	}
}

func TestSpaceArchiveSelectsSpace(t *testing.T) {
	spaceID := "00000000-0000-0000-0000-000000000001"
	resolver := &mcpExternalResolverStub{
		actor: domainidentity.ActorContext{UserID: "user-1", Memberships: []domainspace.ResolvedMembership{
			mcpMembership("member-1", spaceID, "Trading", domainspace.TypeShared, "role-owner"),
		}},
		permissions: []domainpermission.Permission{{Resource: "spaces", Action: "delete"}},
	}
	service := &mcpSpaceServiceStub{space: &domainspace.Space{ID: spaceID}}

	archived, err := SpaceArchive(mcpExternalContext(), resolver, service, SpaceArchiveInput{Space: spaceID})
	if err != nil {
		t.Fatalf("space_archive: %v", err)
	}
	if archived == nil || service.archiveCalls != 1 || service.archiveUser != "user-1" || service.archiveSpace != spaceID {
		t.Fatalf("unexpected archive: result=%+v service=%+v", archived, service)
	}
}

func TestMemberUpdateRoleValidatesMemberUUIDAndPassesRole(t *testing.T) {
	spaceID := "00000000-0000-0000-0000-000000000001"
	memberID := "00000000-0000-0000-0000-000000000002"
	resolver := &mcpExternalResolverStub{
		actor: domainidentity.ActorContext{UserID: "user-1", Memberships: []domainspace.ResolvedMembership{
			mcpMembership("actor-member", spaceID, "Trading", domainspace.TypeShared, "role-admin"),
		}},
		permissions: []domainpermission.Permission{{Resource: "members", Action: "update"}},
	}
	service := &mcpSpaceServiceStub{member: &domainspace.ResolvedMembership{ID: memberID}}

	updated, err := MemberUpdateRole(mcpExternalContext(), resolver, service, MemberUpdateRoleInput{Space: spaceID, MemberID: memberID, Role: "space_viewer"})
	if err != nil {
		t.Fatalf("member_update_role: %v", err)
	}
	if updated == nil || service.roleCalls != 1 || service.roleUser != "user-1" || service.roleSpace != spaceID || service.roleMember != memberID || service.roleName != "space_viewer" {
		t.Fatalf("unexpected role update: result=%+v service=%+v", updated, service)
	}
}

func TestMemberRemovePassesSelectedSpaceAndMember(t *testing.T) {
	spaceID := "00000000-0000-0000-0000-000000000001"
	memberID := "00000000-0000-0000-0000-000000000002"
	resolver := &mcpExternalResolverStub{
		actor: domainidentity.ActorContext{UserID: "user-1", Memberships: []domainspace.ResolvedMembership{
			mcpMembership("actor-member", spaceID, "Trading", domainspace.TypeShared, "role-admin"),
		}},
		permissions: []domainpermission.Permission{{Resource: "members", Action: "delete"}},
	}
	service := &mcpSpaceServiceStub{member: &domainspace.ResolvedMembership{ID: memberID}}

	removed, err := MemberRemove(mcpExternalContext(), resolver, service, MemberRemoveInput{Space: spaceID, MemberID: memberID})
	if err != nil {
		t.Fatalf("member_remove: %v", err)
	}
	if removed == nil || service.removeCalls != 1 || service.removeUser != "user-1" || service.removeSpace != spaceID || service.removeMember != memberID {
		t.Fatalf("unexpected member removal: result=%+v service=%+v", removed, service)
	}
}

func TestSpaceToolsRequireLinkedActor(t *testing.T) {
	service := &mcpSpaceServiceStub{}
	resolver := &mcpExternalResolverStub{err: serviceidentity.ErrUnauthenticated}
	for _, call := range []func() error{
		func() error { _, err := SpaceList(mcpExternalContext(), resolver, service); return err },
		func() error {
			_, err := SpaceGetMembers(mcpExternalContext(), resolver, service, SpaceGetMembersInput{})
			return err
		},
	} {
		err := call()
		if err == nil {
			t.Fatal("expected unlinked actor error")
		}
		var mapped *MCPError
		if !errors.As(err, &mapped) || mapped.Code != "unauthenticated" {
			t.Fatalf("error = %T %v, want safe unauthenticated", err, err)
		}
	}
}

var _ interfacespace.ServiceSpaceInterface = (*mcpSpaceServiceStub)(nil)
