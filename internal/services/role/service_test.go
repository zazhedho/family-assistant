package servicerole

import (
	"context"
	"errors"
	"starter-kit/internal/authscope"
	domainmenu "starter-kit/internal/domain/menu"
	domainpermission "starter-kit/internal/domain/permission"
	domainrole "starter-kit/internal/domain/role"
	"starter-kit/internal/dto"
	"starter-kit/pkg/filter"
	"starter-kit/utils"
	"testing"
)

func roleAuthContext(userID, username, role string, permissions ...string) context.Context {
	return authscope.WithContext(context.Background(), authscope.New(userID, username, role, permissions))
}

type roleRepoMock struct {
	role                domainrole.Role
	roles               []domainrole.Role
	existingByName      domainrole.Role
	rolePermissions     []string
	assignedPermissions []string
	stored              domainrole.Role
	updated             domainrole.Role
	deletedID           string
}

func (m *roleRepoMock) Store(ctx context.Context, data domainrole.Role) error {
	m.stored = data
	return nil
}
func (m *roleRepoMock) GetByID(ctx context.Context, id string) (domainrole.Role, error) {
	return m.role, nil
}
func (m *roleRepoMock) GetAll(ctx context.Context, params filter.BaseParams) ([]domainrole.Role, int64, error) {
	return append([]domainrole.Role{}, m.roles...), int64(len(m.roles)), nil
}
func (m *roleRepoMock) Update(ctx context.Context, data domainrole.Role) error {
	m.updated = data
	return nil
}
func (m *roleRepoMock) Delete(ctx context.Context, id string) error {
	m.deletedID = id
	return nil
}
func (m *roleRepoMock) SoftDelete(ctx context.Context, id, deletedBy string) error {
	m.deletedID = id
	return nil
}
func (m *roleRepoMock) GetByName(ctx context.Context, name string) (domainrole.Role, error) {
	return m.existingByName, nil
}
func (m *roleRepoMock) AssignPermissions(ctx context.Context, roleId string, permissionIds []string) error {
	m.assignedPermissions = append([]string{}, permissionIds...)
	return nil
}
func (m *roleRepoMock) RemovePermissions(ctx context.Context, roleId string, permissionIds []string) error {
	return nil
}
func (m *roleRepoMock) GetRolePermissions(ctx context.Context, roleId string) ([]string, error) {
	return m.rolePermissions, nil
}
func (m *roleRepoMock) AssignMenus(ctx context.Context, roleId string, menuIds []string) error {
	return nil
}
func (m *roleRepoMock) RemoveMenus(ctx context.Context, roleId string, menuIds []string) error {
	return nil
}
func (m *roleRepoMock) GetRoleMenus(ctx context.Context, roleId string) ([]string, error) {
	return nil, nil
}

type permissionRepoMock struct {
	permissionsByID map[string]domainpermission.Permission
	userPermissions []domainpermission.Permission
}

func (m *permissionRepoMock) Store(ctx context.Context, data domainpermission.Permission) error {
	return nil
}
func (m *permissionRepoMock) GetByID(ctx context.Context, id string) (domainpermission.Permission, error) {
	permission, ok := m.permissionsByID[id]
	if !ok {
		return domainpermission.Permission{}, errors.New("not found")
	}
	return permission, nil
}
func (m *permissionRepoMock) GetAll(ctx context.Context, params filter.BaseParams) ([]domainpermission.Permission, int64, error) {
	return nil, 0, nil
}
func (m *permissionRepoMock) Update(ctx context.Context, data domainpermission.Permission) error {
	return nil
}
func (m *permissionRepoMock) Delete(ctx context.Context, id string) error { return nil }
func (m *permissionRepoMock) SoftDelete(ctx context.Context, id, deletedBy string) error {
	return nil
}
func (m *permissionRepoMock) GetByName(ctx context.Context, name string) (domainpermission.Permission, error) {
	return domainpermission.Permission{}, errors.New("not implemented")
}
func (m *permissionRepoMock) GetByResource(ctx context.Context, resource string) ([]domainpermission.Permission, error) {
	return nil, nil
}
func (m *permissionRepoMock) GetUserPermissions(ctx context.Context, userId string) ([]domainpermission.Permission, error) {
	return append([]domainpermission.Permission{}, m.userPermissions...), nil
}

type menuRepoMock struct {
	activeMenus []domainmenu.MenuItem
}

func (m *menuRepoMock) Store(ctx context.Context, data domainmenu.MenuItem) error { return nil }
func (m *menuRepoMock) GetByID(ctx context.Context, id string) (domainmenu.MenuItem, error) {
	return domainmenu.MenuItem{}, errors.New("not implemented")
}
func (m *menuRepoMock) GetAll(ctx context.Context, params filter.BaseParams) ([]domainmenu.MenuItem, int64, error) {
	return nil, 0, nil
}
func (m *menuRepoMock) Update(ctx context.Context, data domainmenu.MenuItem) error { return nil }
func (m *menuRepoMock) Delete(ctx context.Context, id string) error                { return nil }
func (m *menuRepoMock) SoftDelete(ctx context.Context, id, deletedBy string) error {
	return nil
}
func (m *menuRepoMock) GetByName(ctx context.Context, name string) (domainmenu.MenuItem, error) {
	return domainmenu.MenuItem{}, errors.New("not implemented")
}
func (m *menuRepoMock) GetActiveMenus(ctx context.Context) ([]domainmenu.MenuItem, error) {
	return append([]domainmenu.MenuItem{}, m.activeMenus...), nil
}
func (m *menuRepoMock) GetUserMenus(ctx context.Context, userId string) ([]domainmenu.MenuItem, error) {
	return nil, nil
}

func TestAssignPermissionsRequiresManageSystemPermissionForSystemRole(t *testing.T) {
	service := &RoleService{
		RoleRepo: &roleRepoMock{
			role: domainrole.Role{Id: "role-1", Name: utils.RoleAdmin, IsSystem: true},
		},
		PermissionRepo: &permissionRepoMock{},
		MenuRepo:       &menuRepoMock{},
	}

	err := service.AssignPermissions(roleAuthContext("user-1", "Staff User", utils.RoleStaff), "role-1", dto.AssignPermissions{PermissionIds: []string{"perm-1"}})
	if err == nil || err.Error() != "access denied: missing permission roles:manage_system" {
		t.Fatalf("expected manage_system access error, got %v", err)
	}
}

func TestAssignPermissionsRejectsSuperadminRoleForNonSuperadmin(t *testing.T) {
	service := &RoleService{
		RoleRepo: &roleRepoMock{
			role: domainrole.Role{Id: "role-1", Name: utils.RoleSuperAdmin, IsSystem: true},
		},
		PermissionRepo: &permissionRepoMock{
			userPermissions: []domainpermission.Permission{{Resource: "roles", Action: "manage_system"}},
		},
		MenuRepo: &menuRepoMock{},
	}

	err := service.AssignPermissions(roleAuthContext("user-1", "Admin User", utils.RoleAdmin, "roles:manage_system"), "role-1", dto.AssignPermissions{PermissionIds: []string{"perm-1"}})
	if err == nil || err.Error() != "access denied: cannot modify superadmin role" {
		t.Fatalf("expected superadmin protection error, got %v", err)
	}
}

func TestAssignPermissionsAllowsSystemRoleWhenPermissionPresent(t *testing.T) {
	roleRepo := &roleRepoMock{
		role: domainrole.Role{Id: "role-1", Name: utils.RoleAdmin, IsSystem: true},
	}
	service := &RoleService{
		RoleRepo: roleRepo,
		PermissionRepo: &permissionRepoMock{
			userPermissions: []domainpermission.Permission{{Resource: "roles", Action: "manage_system"}},
			permissionsByID: map[string]domainpermission.Permission{
				"perm-1": {Id: "perm-1", Resource: "users", Action: "view"},
			},
		},
		MenuRepo: &menuRepoMock{},
	}

	err := service.AssignPermissions(roleAuthContext("user-1", "Admin User", utils.RoleAdmin, "roles:manage_system"), "role-1", dto.AssignPermissions{PermissionIds: []string{"perm-1"}})
	if err != nil {
		t.Fatalf("expected success, got %v", err)
	}

	if len(roleRepo.assignedPermissions) != 1 || roleRepo.assignedPermissions[0] != "perm-1" {
		t.Fatalf("expected assigned permission to be stored, got %v", roleRepo.assignedPermissions)
	}
}

func TestRoleServiceCRUDAndDetails(t *testing.T) {
	roleRepo := &roleRepoMock{
		role: domainrole.Role{Id: "role-1", Name: "manager", DisplayName: "Manager"},
		roles: []domainrole.Role{
			{Id: "role-1", Name: "manager"},
			{Id: "role-2", Name: utils.RoleSuperAdmin},
		},
		rolePermissions: []string{"perm-1"},
	}
	parentID := "menu-root"
	service := NewRoleService(roleRepo, &permissionRepoMock{
		permissionsByID: map[string]domainpermission.Permission{
			"perm-1": {Id: "perm-1", Resource: "users", Action: "view"},
		},
	}, &menuRepoMock{activeMenus: []domainmenu.MenuItem{
		{Id: parentID, Name: "root", DisplayName: "Root", IsActive: true},
		{Id: "menu-users", Name: "users", DisplayName: "Users", ParentId: &parentID, IsActive: true},
	}})

	created, err := service.Create(context.Background(), dto.RoleCreate{Name: "manager", DisplayName: "Manager", Description: "Can manage users"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if created.Id == "" || roleRepo.stored.Name != "manager" {
		t.Fatalf("expected created role to be stored, role=%+v stored=%+v", created, roleRepo.stored)
	}

	got, err := service.GetByID(context.Background(), "role-1")
	if err != nil || got.Id != "role-1" {
		t.Fatalf("get by id: role=%+v err=%v", got, err)
	}

	details, err := service.GetByIDWithDetails(context.Background(), "role-1")
	if err != nil {
		t.Fatalf("get details: %v", err)
	}
	if len(details.PermissionIds) != 1 || len(details.MenuIds) != 2 {
		t.Fatalf("unexpected details: %+v", details)
	}

	roles, total, err := service.GetAll(roleAuthContext("user-1", "Staff", utils.RoleStaff), filter.BaseParams{})
	if err != nil {
		t.Fatalf("get all: %v", err)
	}
	if total != 1 || len(roles) != 1 || roles[0].Name == utils.RoleSuperAdmin {
		t.Fatalf("expected superadmin filtered for non-superadmin, roles=%+v total=%d", roles, total)
	}

	updated, err := service.Update(context.Background(), "role-1", dto.RoleUpdate{DisplayName: "Updated", Description: "Updated desc"})
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if updated.DisplayName != "Updated" || roleRepo.updated.DisplayName != "Updated" {
		t.Fatalf("expected updated role, got %+v stored %+v", updated, roleRepo.updated)
	}

	if err := service.Delete(context.Background(), "role-1"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if roleRepo.deletedID != "role-1" {
		t.Fatalf("expected role deletion, got %q", roleRepo.deletedID)
	}

	permissionIDs, err := service.GetRolePermissions(context.Background(), "role-1")
	if err != nil || len(permissionIDs) != 1 || permissionIDs[0] != "perm-1" {
		t.Fatalf("get role permissions: ids=%+v err=%v", permissionIDs, err)
	}
	menuIDs, err := service.GetRoleMenus(context.Background(), "role-1")
	if err != nil || len(menuIDs) != 2 {
		t.Fatalf("get role menus: ids=%+v err=%v", menuIDs, err)
	}
}

func TestRoleServiceSystemRoleProtectionsAndMenus(t *testing.T) {
	systemRepo := &roleRepoMock{role: domainrole.Role{Id: "role-1", Name: utils.RoleAdmin, IsSystem: true}}
	service := NewRoleService(systemRepo, &permissionRepoMock{}, &menuRepoMock{})

	if _, err := service.Update(context.Background(), "role-1", dto.RoleUpdate{DisplayName: "Updated"}); err == nil {
		t.Fatal("expected system role update to be rejected")
	}
	if err := service.Delete(context.Background(), "role-1"); err == nil {
		t.Fatal("expected system role delete to be rejected")
	}
	if err := service.AssignMenus(context.Background(), "role-1", dto.AssignMenus{MenuIds: []string{"menu-1"}}); err == nil {
		t.Fatal("expected direct menu assignment to be rejected")
	}
}

func TestRoleServiceCreateRejectsDuplicateAndInvalidPermission(t *testing.T) {
	duplicateService := NewRoleService(&roleRepoMock{existingByName: domainrole.Role{Id: "role-1", Name: "manager"}}, &permissionRepoMock{}, &menuRepoMock{})
	if _, err := duplicateService.Create(context.Background(), dto.RoleCreate{Name: "manager"}); err == nil {
		t.Fatal("expected duplicate role error")
	}

	service := NewRoleService(&roleRepoMock{role: domainrole.Role{Id: "role-1", Name: "manager"}}, &permissionRepoMock{}, &menuRepoMock{})
	err := service.AssignPermissions(roleAuthContext("user-1", "Admin User", utils.RoleAdmin), "role-1", dto.AssignPermissions{PermissionIds: []string{"missing"}})
	if err == nil || err.Error() != "invalid permission ID: missing" {
		t.Fatalf("expected invalid permission error, got %v", err)
	}
}
