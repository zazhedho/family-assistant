package serviceidentity

import (
	"context"
	"errors"
	"testing"

	domainfamilymember "github.com/zazhedho/family-assistant/internal/domain/familymember"
	domainpermission "github.com/zazhedho/family-assistant/internal/domain/permission"
	"gorm.io/gorm"
)

type familyMemberRepositoryStub struct {
	member *domainfamilymember.ResolvedMember
	err    error
}

func (s *familyMemberRepositoryStub) FindActiveByHermesProfile(context.Context, string) (*domainfamilymember.ResolvedMember, error) {
	return s.member, s.err
}

func (s *familyMemberRepositoryStub) FindActiveByID(context.Context, string, string) (*domainfamilymember.FamilyMember, error) {
	return nil, errors.New("not implemented")
}

type permissionServiceStub struct {
	permissions []domainpermission.Permission
	roleID      string
}

func (s *permissionServiceStub) GetRolePermissions(_ context.Context, roleID string) ([]domainpermission.Permission, error) {
	s.roleID = roleID
	return s.permissions, nil
}

func TestResolverResolve_ReturnsActorWithPermissions(t *testing.T) {
	permissionService := &permissionServiceStub{permissions: []domainpermission.Permission{
		{Name: "create_reminders", Resource: "reminders", Action: "create"},
		{Name: "view_members", Resource: "members", Action: "view"},
	}}
	resolver := NewResolver(
		&familyMemberRepositoryStub{member: &domainfamilymember.ResolvedMember{
			UserID:          "user-1",
			MemberID:        "member-1",
			FamilyID:        "family-1",
			RoleID:          "role-parent",
			RoleName:        "parent",
			HermesProfileID: "profile-parent",
		}},
		permissionService,
	)

	got, err := resolver.Resolve(context.Background(), "profile-parent", "whatsapp")
	if err != nil {
		t.Fatalf("resolve actor: %v", err)
	}

	if got.UserID != "user-1" || got.MemberID != "member-1" || got.FamilyID != "family-1" || got.RoleID != "role-parent" || got.RoleName != "parent" {
		t.Fatalf("unexpected actor: %+v", got)
	}
	if got.HermesProfileID != "profile-parent" || got.Channel != "whatsapp" {
		t.Fatalf("unexpected trusted metadata: %+v", got)
	}
	if !got.HasPermission("reminders:create") || !got.HasPermission("members:view") {
		t.Fatalf("expected trusted permissions, got %#v", got.Permissions)
	}
	if got.HasPermission("reminders:delete") {
		t.Fatal("unexpected permission from caller input")
	}
	if permissionService.roleID != "role-parent" {
		t.Fatalf("expected trusted role lookup, got %q", permissionService.roleID)
	}
}

func TestResolverResolve_RejectsEmptyProfile(t *testing.T) {
	resolver := NewResolver(&familyMemberRepositoryStub{}, &permissionServiceStub{})

	_, err := resolver.Resolve(context.Background(), " \t", "whatsapp")
	if !errors.Is(err, ErrUnauthenticated) {
		t.Fatalf("expected unauthenticated error, got %v", err)
	}
}

func TestResolverResolve_RejectsUnknownProfile(t *testing.T) {
	resolver := NewResolver(&familyMemberRepositoryStub{err: gorm.ErrRecordNotFound}, &permissionServiceStub{})

	_, err := resolver.Resolve(context.Background(), "profile-unknown", "whatsapp")
	if !errors.Is(err, ErrUnauthenticated) {
		t.Fatalf("expected unauthenticated error, got %v", err)
	}
}

func TestResolverResolve_RejectsBlankRoleName(t *testing.T) {
	assertMalformedMemberRejected(t, resolvedMemberForTest(func(member *domainfamilymember.ResolvedMember) {
		member.RoleName = ""
	}), "profile-parent")
}

func TestResolverResolve_RejectsBlankReturnedProfile(t *testing.T) {
	assertMalformedMemberRejected(t, resolvedMemberForTest(func(member *domainfamilymember.ResolvedMember) {
		member.HermesProfileID = ""
	}), " profile-parent ")
}

func TestResolverResolve_RejectsMismatchedReturnedProfile(t *testing.T) {
	assertMalformedMemberRejected(t, resolvedMemberForTest(func(member *domainfamilymember.ResolvedMember) {
		member.HermesProfileID = "profile-other"
	}), " profile-parent ")
}

func resolvedMemberForTest(update func(*domainfamilymember.ResolvedMember)) *domainfamilymember.ResolvedMember {
	member := &domainfamilymember.ResolvedMember{
		UserID:          "user-1",
		MemberID:        "member-1",
		FamilyID:        "family-1",
		RoleID:          "role-parent",
		RoleName:        "parent",
		HermesProfileID: "profile-parent",
	}
	update(member)
	return member
}

func assertMalformedMemberRejected(t *testing.T, member *domainfamilymember.ResolvedMember, profile string) {
	t.Helper()
	permissionService := &permissionServiceStub{}
	resolver := NewResolver(&familyMemberRepositoryStub{member: member}, permissionService)

	_, err := resolver.Resolve(context.Background(), profile, "whatsapp")
	if !errors.Is(err, ErrUnauthenticated) {
		t.Fatalf("expected unauthenticated error, got %v", err)
	}
	if permissionService.roleID != "" {
		t.Fatalf("expected malformed membership to skip permission lookup, got %q", permissionService.roleID)
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
