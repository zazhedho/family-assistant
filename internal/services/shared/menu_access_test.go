package serviceshared

import (
	"context"
	"errors"
	"starter-kit/internal/authscope"
	domainmenu "starter-kit/internal/domain/menu"
	domainpermission "starter-kit/internal/domain/permission"
	"starter-kit/pkg/filter"
	"testing"
)

func TestResolveAccessibleMenus_IncludesParentAndPreservesActiveOrder(t *testing.T) {
	parentID := "education"
	menus := []domainmenu.MenuItem{
		{Id: "dashboard", Name: "dashboard", DisplayName: "Dashboard", OrderIndex: 1, IsActive: true},
		{Id: "education-stats", Name: "education_stats", DisplayName: "Education Stats", ParentId: &parentID, OrderIndex: 10, IsActive: true},
		{Id: "education-priority", Name: "education_priority", DisplayName: "Education Priority", ParentId: &parentID, OrderIndex: 11, IsActive: true},
		{Id: "education", Name: "education", DisplayName: "Education", OrderIndex: 20, IsActive: true},
	}

	got := ResolveAccessibleMenus(menus, []string{"education_priority"})
	if len(got) != 2 {
		t.Fatalf("expected 2 menus, got %d", len(got))
	}
	if got[0].Id != "education-priority" {
		t.Fatalf("expected child menu first in active order, got %s", got[0].Id)
	}
	if got[1].Id != "education" {
		t.Fatalf("expected parent menu to be included, got %s", got[1].Id)
	}
}

func TestResolveAccessibleMenuIDs_IgnoresUnknownResources(t *testing.T) {
	menus := []domainmenu.MenuItem{
		{Id: "users", Name: "users", OrderIndex: 1, IsActive: true},
	}

	got := ResolveAccessibleMenuIDs(menus, []string{"unknown"})
	if len(got) != 0 {
		t.Fatalf("expected no menu ids, got %v", got)
	}
}

type permissionRepoSharedTestDouble struct {
	permissions []domainpermission.Permission
	err         error
}

func (m *permissionRepoSharedTestDouble) Store(ctx context.Context, data domainpermission.Permission) error {
	return nil
}
func (m *permissionRepoSharedTestDouble) GetByID(ctx context.Context, id string) (domainpermission.Permission, error) {
	return domainpermission.Permission{}, errors.New("not implemented")
}
func (m *permissionRepoSharedTestDouble) GetAll(ctx context.Context, params filter.BaseParams) ([]domainpermission.Permission, int64, error) {
	return nil, 0, nil
}
func (m *permissionRepoSharedTestDouble) Update(ctx context.Context, data domainpermission.Permission) error {
	return nil
}
func (m *permissionRepoSharedTestDouble) Delete(ctx context.Context, id string) error { return nil }
func (m *permissionRepoSharedTestDouble) SoftDelete(ctx context.Context, id, deletedBy string) error {
	return nil
}
func (m *permissionRepoSharedTestDouble) GetByName(ctx context.Context, name string) (domainpermission.Permission, error) {
	return domainpermission.Permission{}, errors.New("not implemented")
}
func (m *permissionRepoSharedTestDouble) GetByResource(ctx context.Context, resource string) ([]domainpermission.Permission, error) {
	return nil, nil
}
func (m *permissionRepoSharedTestDouble) GetUserPermissions(ctx context.Context, userId string) ([]domainpermission.Permission, error) {
	if m.err != nil {
		return nil, m.err
	}
	return append([]domainpermission.Permission{}, m.permissions...), nil
}

func TestHasPermissionUsesScopeAndRepositoryFallback(t *testing.T) {
	ctx := authscope.WithContext(context.Background(), authscope.New("user-1", "Jane", "staff", []string{"users:list"}))
	ok, err := HasPermission(ctx, &permissionRepoSharedTestDouble{}, "users", "list")
	if err != nil || !ok {
		t.Fatalf("expected scope permission to allow, ok=%v err=%v", ok, err)
	}

	ctx = authscope.WithContext(context.Background(), authscope.New("user-1", "Jane", "staff", nil))
	ok, err = HasPermission(ctx, &permissionRepoSharedTestDouble{
		permissions: []domainpermission.Permission{{Resource: "users", Action: "delete"}},
	}, "users", "delete")
	if err != nil || !ok {
		t.Fatalf("expected repository permission to allow, ok=%v err=%v", ok, err)
	}

	ok, err = HasPermission(context.Background(), &permissionRepoSharedTestDouble{}, "users", "delete")
	if err != nil || ok {
		t.Fatalf("expected anonymous context to deny without repo lookup, ok=%v err=%v", ok, err)
	}
}
