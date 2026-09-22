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
