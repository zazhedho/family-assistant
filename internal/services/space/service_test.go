package servicespace

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"family-assistant/internal/authscope"
	domainaudit "family-assistant/internal/domain/audit"
	domainpermission "family-assistant/internal/domain/permission"
	domainrole "family-assistant/internal/domain/role"
	domainspace "family-assistant/internal/domain/space"
	"family-assistant/internal/dto"
	interfacespace "family-assistant/internal/interfaces/space"
	serviceauthorization "family-assistant/internal/services/authorization"

	"gorm.io/gorm"
)

var _ interfacespace.ServiceSpaceInterface = (*service)(nil)

type spaceRepositoryStub struct {
	memberships []domainspace.ResolvedMembership
	membership  *domainspace.ResolvedMembership
	listed      []domainspace.ResolvedMembership
	createErr   error
	listErr     error
	findErr     error
	membersErr  error
	created     *domainspace.Space
	owner       *domainspace.Member
	listUserID  string
	findUserID  string
	findSpaceID string
	memberSpace string
}

func (s *spaceRepositoryStub) CreateWithOwner(_ context.Context, space *domainspace.Space, member *domainspace.Member) error {
	if s.createErr != nil {
		return s.createErr
	}
	if space.ID == "" {
		space.ID = "00000000-0000-0000-0000-000000000101"
	}
	if member.ID == "" {
		member.ID = "00000000-0000-0000-0000-000000000102"
	}
	if member.SpaceID == "" {
		member.SpaceID = space.ID
	}
	copySpace := *space
	copyMember := *member
	s.created = &copySpace
	s.owner = &copyMember
	return nil
}

func (s *spaceRepositoryStub) ListActiveByUserID(_ context.Context, userID string) ([]domainspace.ResolvedMembership, error) {
	s.listUserID = userID
	if s.listErr != nil {
		return nil, s.listErr
	}
	return append([]domainspace.ResolvedMembership(nil), s.memberships...), nil
}

func (s *spaceRepositoryStub) FindActiveMembership(_ context.Context, userID, spaceID string) (*domainspace.ResolvedMembership, error) {
	s.findUserID, s.findSpaceID = userID, spaceID
	if s.findErr != nil {
		return nil, s.findErr
	}
	if s.membership == nil {
		return nil, gorm.ErrRecordNotFound
	}
	copyMembership := *s.membership
	return &copyMembership, nil
}

func (s *spaceRepositoryStub) ListActiveMembers(_ context.Context, spaceID string) ([]domainspace.ResolvedMembership, error) {
	s.memberSpace = spaceID
	if s.membersErr != nil {
		return nil, s.membersErr
	}
	return append([]domainspace.ResolvedMembership(nil), s.listed...), nil
}

type roleRepositoryStub struct {
	role domainrole.Role
	err  error
	name string
}

func (s *roleRepositoryStub) GetByName(_ context.Context, name string) (domainrole.Role, error) {
	s.name = name
	return s.role, s.err
}

type permissionRepositoryStub struct {
	byRole map[string][]domainpermission.Permission
	err    error
	roles  []string
}

func (s *permissionRepositoryStub) GetRolePermissions(_ context.Context, roleID string) ([]domainpermission.Permission, error) {
	s.roles = append(s.roles, roleID)
	if s.err != nil {
		return nil, s.err
	}
	return append([]domainpermission.Permission(nil), s.byRole[roleID]...), nil
}

type spaceAuditStub struct {
	events []domainaudit.AuditEvent
}

func (s *spaceAuditStub) Store(_ context.Context, event domainaudit.AuditEvent) error {
	s.events = append(s.events, event)
	return nil
}

func newSpaceService(repo *spaceRepositoryStub, permissions map[string][]domainpermission.Permission, audit *spaceAuditStub) interfacespace.ServiceSpaceInterface {
	return NewService(repo, &roleRepositoryStub{role: domainrole.Role{Id: "role-owner", Name: "space_owner"}}, &permissionRepositoryStub{byRole: permissions}, audit)
}

func membership(roleID, spaceID, userID string) domainspace.ResolvedMembership {
	return domainspace.ResolvedMembership{ID: "member-" + spaceID, SpaceID: spaceID, UserID: userID, RoleID: roleID, Status: domainspace.StatusActive}
}

func permission(resource, action string) domainpermission.Permission {
	return domainpermission.Permission{Resource: resource, Action: action}
}

func TestCreateSharedSpaceRejectsPersonalCategory(t *testing.T) {
	repo := &spaceRepositoryStub{memberships: []domainspace.ResolvedMembership{membership("role-1", "personal", "user-1")}}
	service := newSpaceService(repo, map[string][]domainpermission.Permission{
		"role-1": {permission("spaces", "create")},
	}, &spaceAuditStub{})

	_, err := service.Create(context.Background(), "user-1", dto.SpaceCreateInput{Name: "Mine", Category: domainspace.CategoryPersonal})
	var validationErr *serviceauthorization.ValidationError
	if !errors.As(err, &validationErr) || validationErr.Field != "category" {
		t.Fatalf("expected category validation error, got %v", err)
	}
}

func TestCreateSharedSpaceAcceptsAllowlistedCategories(t *testing.T) {
	for _, category := range []string{domainspace.CategoryFamily, domainspace.CategoryFriends, domainspace.CategoryCommunity, domainspace.CategoryWork, domainspace.CategoryFinance, domainspace.CategoryCustom} {
		t.Run(category, func(t *testing.T) {
			repo := &spaceRepositoryStub{memberships: []domainspace.ResolvedMembership{membership("role-1", "personal", "user-1")}}
			service := newSpaceService(repo, map[string][]domainpermission.Permission{
				"role-1": {permission("spaces", "create")},
			}, &spaceAuditStub{})

			created, err := service.Create(context.Background(), "user-1", dto.SpaceCreateInput{Name: " Shared ", Category: category})
			if err != nil {
				t.Fatalf("create shared space: %v", err)
			}
			if created.Category != category || created.Name != "Shared" || created.Type != domainspace.TypeShared {
				t.Fatalf("unexpected space: %+v", created)
			}
		})
	}
}

func TestCreateSharedSpaceTrimsButDoesNotUniquifyNames(t *testing.T) {
	repo := &spaceRepositoryStub{memberships: []domainspace.ResolvedMembership{membership("role-1", "personal", "user-1")}}
	service := newSpaceService(repo, map[string][]domainpermission.Permission{
		"role-1": {permission("spaces", "create")},
	}, &spaceAuditStub{})

	first, err := service.Create(context.Background(), "user-1", dto.SpaceCreateInput{Name: "  Shared  ", Category: domainspace.CategoryFamily})
	if err != nil {
		t.Fatalf("first create: %v", err)
	}
	second, err := service.Create(context.Background(), "user-1", dto.SpaceCreateInput{Name: "Shared", Category: domainspace.CategoryFamily})
	if err != nil {
		t.Fatalf("second create: %v", err)
	}
	if first.Name != "Shared" || second.Name != "Shared" || first.ID == second.ID {
		t.Fatalf("expected duplicate names with distinct spaces: first=%+v second=%+v", first, second)
	}
}

func TestCreateSharedSpaceCreatesOwnerMembershipTransactionally(t *testing.T) {
	repo := &spaceRepositoryStub{memberships: []domainspace.ResolvedMembership{membership("role-1", "personal", "user-1")}}
	audit := &spaceAuditStub{}
	roleRepo := &roleRepositoryStub{role: domainrole.Role{Id: "role-owner", Name: "space_owner"}}
	service := NewService(repo, roleRepo, &permissionRepositoryStub{byRole: map[string][]domainpermission.Permission{
		"role-1": {permission("spaces", "create")},
	}}, audit)

	created, err := service.Create(context.Background(), "user-1", dto.SpaceCreateInput{Name: "Family", Category: domainspace.CategoryFamily})
	if err != nil {
		t.Fatalf("create shared space: %v", err)
	}
	if repo.created == nil || repo.owner == nil {
		t.Fatal("expected repository to receive both space and owner")
	}
	if roleRepo.name != "space_owner" {
		t.Fatalf("owner role lookup = %q, want space_owner", roleRepo.name)
	}
	if repo.owner.SpaceID != created.ID || repo.owner.UserID != "user-1" || repo.owner.RoleID != "role-owner" || repo.owner.Status != domainspace.StatusActive {
		t.Fatalf("unexpected owner: %+v", repo.owner)
	}
	if len(audit.events) != 1 || audit.events[0].Status != domainaudit.StatusSuccess || audit.events[0].ResourceID != created.ID || audit.events[0].ActorMemberID != "member-personal" || audit.events[0].ResourceOwnerMemberID != repo.owner.ID || audit.events[0].Source != "http" {
		t.Fatalf("unexpected audit: %+v", audit.events)
	}
	if audit.events[0].AfterData != nil || audit.events[0].BeforeData != nil {
		t.Fatalf("request/body data must not be audited: %+v", audit.events[0])
	}
}

func TestCreateSharedSpaceAuditsFailureWithoutRequestBody(t *testing.T) {
	repo := &spaceRepositoryStub{
		memberships: []domainspace.ResolvedMembership{membership("role-1", "personal", "user-1")},
		createErr:   errors.New("insert failed: secret=raw-body password=top-secret"),
	}
	audit := &spaceAuditStub{}
	service := newSpaceService(repo, map[string][]domainpermission.Permission{
		"role-1": {permission("spaces", "create")},
	}, audit)

	_, err := service.Create(context.Background(), "user-1", dto.SpaceCreateInput{Name: "Family", Category: domainspace.CategoryFamily})
	if err == nil {
		t.Fatal("expected repository error")
	}
	if len(audit.events) != 1 || audit.events[0].Status != domainaudit.StatusFailed || audit.events[0].ErrorMessage != "internal" || audit.events[0].ResourceID == "" || audit.events[0].ActorUserID != "user-1" || audit.events[0].ActorMemberID != "member-personal" || audit.events[0].ResourceOwnerMemberID == "" || audit.events[0].Source != "http" || audit.events[0].AfterData != nil {
		t.Fatalf("unexpected failure audit: %+v", audit.events)
	}
	if audit.events[0].ErrorMessage == "insert failed: secret=raw-body password=top-secret" || audit.events[0].ErrorMessage == "sql" {
		t.Fatalf("failure audit leaked raw error: %+v", audit.events[0])
	}
}

func TestListReturnsOnlyCallerActiveMemberships(t *testing.T) {
	repo := &spaceRepositoryStub{memberships: []domainspace.ResolvedMembership{
		membership("role-1", "space-1", "user-1"),
		membership("role-1", "space-2", "user-1"),
	}}
	svc := newSpaceService(repo, map[string][]domainpermission.Permission{
		"role-1": {permission("spaces", "list")},
	}, &spaceAuditStub{})

	got, err := svc.List(context.Background(), "user-1")
	if err != nil {
		t.Fatalf("list spaces: %v", err)
	}
	audit := svc.(*service).audit.(*spaceAuditStub)
	if repo.listUserID != "user-1" || len(got) != 2 || got[0].UserID != "user-1" || got[1].UserID != "user-1" || len(audit.events) != 1 || audit.events[0].Status != domainaudit.StatusSuccess {
		t.Fatalf("unexpected memberships: user=%q got=%+v", repo.listUserID, got)
	}
}

func TestListAuditsEmptySuccess(t *testing.T) {
	audit := &spaceAuditStub{}
	service := newSpaceService(&spaceRepositoryStub{}, nil, audit)

	got, err := service.List(context.Background(), "user-1")
	if err != nil || len(got) != 0 {
		t.Fatalf("list empty: got=%v err=%v", got, err)
	}
	if len(audit.events) != 1 || audit.events[0].Status != domainaudit.StatusSuccess || audit.events[0].Source != "http" || audit.events[0].ActorUserID != "user-1" {
		t.Fatalf("expected one empty-list audit, got %+v", audit.events)
	}
}

func TestListDeniesMembershipRoleWithoutListPermission(t *testing.T) {
	audit := &spaceAuditStub{}
	repo := &spaceRepositoryStub{memberships: []domainspace.ResolvedMembership{
		membership("role-1", "space-b", "user-1"),
		membership("role-1", "space-a", "user-1"),
	}}
	service := newSpaceService(repo, map[string][]domainpermission.Permission{"role-1": {}}, audit)

	_, err := service.List(context.Background(), "user-1")
	if !errors.Is(err, ErrForbidden) {
		t.Fatalf("expected forbidden, got %v", err)
	}
	if len(audit.events) != 1 || audit.events[0].Status != domainaudit.StatusFailed || audit.events[0].ErrorMessage != "forbidden" || audit.events[0].ResourceID != "" || audit.events[0].ActorMemberID != "" {
		t.Fatalf("expected one sanitized denial audit, got %+v", audit.events)
	}
	if got := audit.events[0].Metadata["space_ids"]; !reflect.DeepEqual(got, []string{"space-a", "space-b"}) {
		t.Fatalf("denied space IDs = %#v, want deterministic candidates", got)
	}
	if got := audit.events[0].Metadata["member_ids"]; !reflect.DeepEqual(got, []string{"member-space-a", "member-space-b"}) {
		t.Fatalf("denied member IDs = %#v, want deterministic candidates", got)
	}
}

func TestCreateDeniesMembershipRoleWithoutCreatePermission(t *testing.T) {
	audit := &spaceAuditStub{}
	repo := &spaceRepositoryStub{memberships: []domainspace.ResolvedMembership{membership("role-1", "space-1", "user-1")}}
	service := newSpaceService(repo, map[string][]domainpermission.Permission{"role-1": {}}, audit)

	_, err := service.Create(context.Background(), "user-1", dto.SpaceCreateInput{Name: "Family", Category: domainspace.CategoryFamily})
	if !errors.Is(err, ErrForbidden) {
		t.Fatalf("expected forbidden, got %v", err)
	}
	if len(audit.events) != 1 || audit.events[0].Status != domainaudit.StatusFailed || audit.events[0].ErrorMessage != "forbidden" || audit.events[0].ResourceID != "space-1" || audit.events[0].ActorMemberID != "member-space-1" {
		t.Fatalf("expected one sanitized denial audit, got %+v", audit.events)
	}
	if got := audit.events[0].Metadata["space_ids"]; !reflect.DeepEqual(got, []string{"space-1"}) {
		t.Fatalf("denied space IDs = %#v, want candidate", got)
	}
	if got := audit.events[0].Metadata["member_ids"]; !reflect.DeepEqual(got, []string{"member-space-1"}) {
		t.Fatalf("denied member IDs = %#v, want candidate", got)
	}
}

func TestAuthorizationFailsClosedWithoutMembershipPermissionRepository(t *testing.T) {
	repo := &spaceRepositoryStub{
		memberships: []domainspace.ResolvedMembership{membership("role-1", "space-1", "user-1")},
		membership: func() *domainspace.ResolvedMembership {
			m := membership("role-1", "space-1", "user-1")
			return &m
		}(),
	}
	ctx := authscope.WithContext(context.Background(), authscope.New("user-1", "Jane", "superadmin", []string{"spaces:list", "spaces:create", "members:list"}))
	service := NewService(repo, &roleRepositoryStub{role: domainrole.Role{Id: "role-owner", Name: "space_owner"}}, nil, &spaceAuditStub{})

	if _, err := service.List(ctx, "user-1"); err == nil {
		t.Fatal("list authorized from global authscope without membership permission repository")
	}
	if _, err := service.Create(ctx, "user-1", dto.SpaceCreateInput{Name: "Family", Category: domainspace.CategoryFamily}); err == nil {
		t.Fatal("create authorized from global authscope without membership permission repository")
	}
	if _, err := service.Members(ctx, "user-1", "space-1"); err == nil {
		t.Fatal("members authorized from global authscope without membership permission repository")
	}
}

func TestMembersReturnsNotFoundWithoutActiveCallerMembership(t *testing.T) {
	repo := &spaceRepositoryStub{}
	audit := &spaceAuditStub{}
	service := newSpaceService(repo, nil, audit)

	_, err := service.Members(context.Background(), "user-1", "00000000-0000-0000-0000-000000000001")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected not found, got %v", err)
	}
	if len(audit.events) != 1 || audit.events[0].Status != domainaudit.StatusFailed || audit.events[0].ErrorMessage != "not_found" {
		t.Fatalf("expected one not-found audit, got %+v", audit.events)
	}
}

func TestMembersRequiresMembershipScopedPermission(t *testing.T) {
	repo := &spaceRepositoryStub{membership: func() *domainspace.ResolvedMembership {
		m := membership("role-1", "space-1", "user-1")
		return &m
	}()}
	audit := &spaceAuditStub{}
	service := newSpaceService(repo, map[string][]domainpermission.Permission{
		"role-1": {permission("members", "view")},
	}, audit)

	_, err := service.Members(context.Background(), "user-1", "space-1")
	if !errors.Is(err, ErrForbidden) {
		t.Fatalf("expected forbidden, got %v", err)
	}
	if len(audit.events) != 1 || audit.events[0].Status != domainaudit.StatusFailed || audit.events[0].ErrorMessage != "forbidden" {
		t.Fatalf("expected one forbidden audit, got %+v", audit.events)
	}
}

func TestMembersListsMembersAfterActiveCallerAuthorization(t *testing.T) {
	repo := &spaceRepositoryStub{
		membership: func() *domainspace.ResolvedMembership {
			m := membership("role-1", "space-1", "user-1")
			return &m
		}(),
		listed: []domainspace.ResolvedMembership{membership("role-1", "space-1", "user-1")},
	}
	audit := &spaceAuditStub{}
	service := newSpaceService(repo, map[string][]domainpermission.Permission{
		"role-1": {permission("members", "list")},
	}, audit)

	got, err := service.Members(context.Background(), "user-1", "space-1")
	if err != nil {
		t.Fatalf("list members: %v", err)
	}
	if repo.findUserID != "user-1" || repo.findSpaceID != "space-1" || repo.memberSpace != "space-1" || len(got) != 1 || len(audit.events) != 1 || audit.events[0].Status != domainaudit.StatusSuccess {
		t.Fatalf("unexpected member lookup: %+v", got)
	}
}

func TestAuditUsesInitiatorAndHTTPProvenanceDuringImpersonation(t *testing.T) {
	audit := &spaceAuditStub{}
	repo := &spaceRepositoryStub{memberships: []domainspace.ResolvedMembership{membership("role-1", "target-space", "target-user")}}
	scope := authscope.New("target-user", "Target", "member", nil)
	scope.IsImpersonated = true
	scope.OriginalUserID = "operator-user"
	scope.OriginalUsername = "Operator"
	scope.OriginalRole = "admin"
	ctx := authscope.WithContext(context.Background(), scope)
	ctx = WithAuditProvenance(ctx, AuditProvenance{RequestID: "request-1", IPAddress: "192.0.2.1", UserAgent: "test-agent"})
	service := newSpaceService(repo, map[string][]domainpermission.Permission{
		"role-1": {permission("spaces", "create")},
	}, audit)

	_, err := service.Create(ctx, "target-user", dto.SpaceCreateInput{Name: "Family", Category: domainspace.CategoryFamily})
	if err != nil {
		t.Fatalf("create impersonated space: %v", err)
	}
	if len(audit.events) != 1 {
		t.Fatalf("expected one audit, got %+v", audit.events)
	}
	event := audit.events[0]
	if event.ActorUserID != "operator-user" || event.ActorRole != "admin" || event.ActorMemberID != "member-target-space" || event.RequestID != "request-1" || event.IPAddress != "192.0.2.1" || event.UserAgent != "test-agent" {
		t.Fatalf("unexpected initiator/provenance: %+v", event)
	}
	if event.Metadata["subject_user_id"] != "target-user" {
		t.Fatalf("expected effective subject metadata, got %+v", event.Metadata)
	}
}

func TestSpaceRepositoryStubSatisfiesInterface(t *testing.T) {
	var _ interfacespace.RepoSpaceInterface = (*spaceRepositoryStub)(nil)
	var _ interface {
		Store(context.Context, domainaudit.AuditEvent) error
	} = (*spaceAuditStub)(nil)
}
