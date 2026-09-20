package serviceidentity

import (
	"context"
	"errors"
	"testing"
	"time"

	domainidentity "family-assistant/internal/domain/identity"
	domainpermission "family-assistant/internal/domain/permission"
	domainspace "family-assistant/internal/domain/space"

	"gorm.io/gorm"
)

type identityRepositoryStub struct {
	identity *domainidentity.ExternalIdentity
	err      error
}

func (s *identityRepositoryStub) CreateLinkToken(context.Context, *domainidentity.LinkToken) error {
	return errors.New("not implemented")
}

func (s *identityRepositoryStub) ConsumeAndLink(context.Context, string, string, string, time.Time) (*domainidentity.ExternalIdentity, error) {
	return nil, errors.New("not implemented")
}

func (s *identityRepositoryStub) FindActive(_ context.Context, _, _ string) (*domainidentity.ExternalIdentity, error) {
	return s.identity, s.err
}

func (s *identityRepositoryStub) Revoke(context.Context, string, string, string) error {
	return errors.New("not implemented")
}

type spaceRepositoryStub struct {
	memberships []domainspace.ResolvedMembership
	err         error
	userID      string
}

func (s *spaceRepositoryStub) CreateWithOwner(context.Context, *domainspace.Space, *domainspace.Member) error {
	return errors.New("not implemented")
}

func (s *spaceRepositoryStub) ListActiveByUserID(_ context.Context, userID string) ([]domainspace.ResolvedMembership, error) {
	s.userID = userID
	return append([]domainspace.ResolvedMembership(nil), s.memberships...), s.err
}

func (s *spaceRepositoryStub) FindActiveMembership(context.Context, string, string) (*domainspace.ResolvedMembership, error) {
	return nil, errors.New("not implemented")
}

func (s *spaceRepositoryStub) ListActiveMembers(context.Context, string) ([]domainspace.ResolvedMembership, error) {
	return nil, errors.New("not implemented")
}

type permissionServiceStub struct {
	permissions []domainpermission.Permission
	roleID      string
	calls       int
	err         error
}

func (s *permissionServiceStub) GetRolePermissions(_ context.Context, roleID string) ([]domainpermission.Permission, error) {
	s.calls++
	s.roleID = roleID
	if s.err != nil {
		return nil, s.err
	}
	return s.permissions, nil
}

func newResolver(identity *domainidentity.ExternalIdentity, memberships []domainspace.ResolvedMembership, permissions *permissionServiceStub) *ResolverService {
	return NewResolver(
		&identityRepositoryStub{identity: identity},
		&spaceRepositoryStub{memberships: memberships},
		permissions,
	)
}

func activeMembership(id, spaceID, name, spaceType, userID, roleID, roleName string) domainspace.ResolvedMembership {
	return domainspace.ResolvedMembership{
		ID: id, SpaceID: spaceID, SpaceName: name, SpaceType: spaceType,
		UserID: userID, RoleID: roleID, RoleName: roleName, Status: domainspace.StatusActive,
	}
}

func TestResolverResolveExternalReturnsUserAndAllActiveMemberships(t *testing.T) {
	permissions := &permissionServiceStub{}
	memberships := []domainspace.ResolvedMembership{
		activeMembership("member-personal", "space-personal", "Jane", domainspace.TypePersonal, "user-1", "role-owner", "space_owner"),
		activeMembership("member-shared", "space-shared", "Trading", domainspace.TypeShared, "user-1", "role-member", "space_member"),
		{ID: "member-inactive", SpaceID: "space-old", UserID: "user-1", RoleID: "role-old", RoleName: "space_member", Status: domainspace.StatusInactive},
	}
	resolver := newResolver(&domainidentity.ExternalIdentity{
		UserID: "user-1", Provider: domainidentity.ProviderHermes, ExternalID: "profile-1", Status: domainidentity.StatusActive,
	}, memberships, permissions)

	got, err := resolver.ResolveExternal(context.Background(), " HERMES ", " profile-1 ", " whatsapp ")
	if err != nil {
		t.Fatalf("resolve external identity: %v", err)
	}
	if got.UserID != "user-1" || len(got.Memberships) != 2 {
		t.Fatalf("unexpected actor: %+v", got)
	}
	if got.Memberships[0].ID != "member-personal" || got.Memberships[1].ID != "member-shared" {
		t.Fatalf("unexpected active memberships: %#v", got.Memberships)
	}
	if got.ExternalProvider != domainidentity.ProviderHermes || got.ExternalID != "profile-1" || got.Source != "mcp" || got.Channel != "whatsapp" {
		t.Fatalf("unexpected external provenance: %+v", got)
	}
	if permissions.calls != 0 || got.Permissions != nil {
		t.Fatalf("permissions must load only after Space selection: calls=%d permissions=%#v", permissions.calls, got.Permissions)
	}
}

func TestResolverResolveUserReturnsAllActiveMembershipsWithoutPermissions(t *testing.T) {
	permissions := &permissionServiceStub{}
	resolver := newResolver(nil, []domainspace.ResolvedMembership{
		activeMembership("member-personal", "space-personal", "Jane", domainspace.TypePersonal, "user-1", "role-owner", "space_owner"),
	}, permissions)

	got, err := resolver.ResolveUser(context.Background(), " user-1 ", " http ")
	if err != nil {
		t.Fatalf("resolve HTTP user: %v", err)
	}
	if got.UserID != "user-1" || got.Source != "http" || got.Channel != "http" || len(got.Memberships) != 1 {
		t.Fatalf("unexpected HTTP actor: %+v", got)
	}
	if got.ExternalProvider != "" || got.ExternalID != "" || permissions.calls != 0 {
		t.Fatalf("unexpected external identity or permission state: %+v calls=%d", got, permissions.calls)
	}
}

func TestResolverResolveUserDoesNotRequireExternalIdentityRepository(t *testing.T) {
	resolver := NewResolver(
		nil,
		&spaceRepositoryStub{memberships: []domainspace.ResolvedMembership{
			activeMembership("member-personal", "space-personal", "Jane", domainspace.TypePersonal, "user-1", "role-owner", "space_owner"),
		}},
		&permissionServiceStub{},
	)

	got, err := resolver.ResolveUser(context.Background(), "user-1", "http")
	if err != nil {
		t.Fatalf("resolve HTTP user without external repository: %v", err)
	}
	if got.UserID != "user-1" || len(got.Memberships) != 1 {
		t.Fatalf("unexpected actor: %+v", got)
	}
}

func TestResolverRejectsUnknownExternalIdentityAndMissingMembership(t *testing.T) {
	for _, tt := range []struct {
		name        string
		identity    *domainidentity.ExternalIdentity
		memberships []domainspace.ResolvedMembership
		err         error
	}{
		{name: "unknown identity", err: domainidentity.ErrIdentityNotFound},
		{name: "no active membership", identity: &domainidentity.ExternalIdentity{UserID: "user-1"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			resolver := newResolver(tt.identity, tt.memberships, &permissionServiceStub{})
			if tt.err != nil {
				resolver.IdentityRepo = &identityRepositoryStub{err: tt.err}
			}
			_, err := resolver.ResolveExternal(context.Background(), "hermes", "profile-1", "whatsapp")
			if !errors.Is(err, ErrUnauthenticated) {
				t.Fatalf("error = %v, want ErrUnauthenticated", err)
			}
		})
	}
}

func TestResolverMapsRecordNotFoundToUnauthenticated(t *testing.T) {
	resolver := NewResolver(
		&identityRepositoryStub{err: gorm.ErrRecordNotFound},
		&spaceRepositoryStub{},
		&permissionServiceStub{},
	)

	_, err := resolver.ResolveExternal(context.Background(), "hermes", "profile-1", "whatsapp")
	if !errors.Is(err, ErrUnauthenticated) {
		t.Fatalf("error = %v, want ErrUnauthenticated", err)
	}
}

func TestResolverReturnsControlledErrorWhenDependenciesMissing(t *testing.T) {
	for _, tt := range []struct {
		name     string
		resolver *ResolverService
	}{
		{name: "nil resolver"},
		{name: "nil repositories", resolver: NewResolver(nil, nil, nil)},
	} {
		t.Run(tt.name, func(t *testing.T) {
			_, err := tt.resolver.ResolveExternal(context.Background(), "hermes", "profile-1", "whatsapp")
			if !errors.Is(err, ErrResolverMisconfigured) {
				t.Fatalf("ResolveExternal error = %v, want ErrResolverMisconfigured", err)
			}
			_, err = tt.resolver.ResolveUser(context.Background(), "user-1", "http")
			if !errors.Is(err, ErrResolverMisconfigured) {
				t.Fatalf("ResolveUser error = %v, want ErrResolverMisconfigured", err)
			}
		})
	}
}

func TestActorContextHasPermission(t *testing.T) {
	actor := ActorContext{Permissions: map[string]struct{}{"reminders:create": {}}}

	if !actor.HasPermission("reminders:create") {
		t.Fatal("expected permission")
	}
	if actor.HasPermission("reminders:delete") {
		t.Fatal("did not expect permission")
	}
}
