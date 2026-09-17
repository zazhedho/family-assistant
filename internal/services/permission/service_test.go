package servicepermission

import (
	"context"
	"errors"
	domainpermission "starter-kit/internal/domain/permission"
	"starter-kit/internal/dto"
	"starter-kit/pkg/filter"
	"testing"
)

type permissionRepoTestDouble struct {
	permission      domainpermission.Permission
	permissionByKey map[string]domainpermission.Permission
	stored          domainpermission.Permission
	updated         domainpermission.Permission
	deletedID       string
}

func (m *permissionRepoTestDouble) Store(ctx context.Context, data domainpermission.Permission) error {
	m.stored = data
	return nil
}
func (m *permissionRepoTestDouble) GetByID(ctx context.Context, id string) (domainpermission.Permission, error) {
	if m.permission.Id == "" {
		return domainpermission.Permission{}, errors.New("not found")
	}
	return m.permission, nil
}
func (m *permissionRepoTestDouble) GetAll(ctx context.Context, params filter.BaseParams) ([]domainpermission.Permission, int64, error) {
	return []domainpermission.Permission{m.permission}, 1, nil
}
func (m *permissionRepoTestDouble) Update(ctx context.Context, data domainpermission.Permission) error {
	m.updated = data
	m.permission = data
	return nil
}
func (m *permissionRepoTestDouble) Delete(ctx context.Context, id string) error {
	m.deletedID = id
	return nil
}
func (m *permissionRepoTestDouble) SoftDelete(ctx context.Context, id, deletedBy string) error {
	m.deletedID = id
	return nil
}
func (m *permissionRepoTestDouble) GetByName(ctx context.Context, name string) (domainpermission.Permission, error) {
	if m.permissionByKey != nil {
		return m.permissionByKey[name], nil
	}
	return domainpermission.Permission{}, nil
}
func (m *permissionRepoTestDouble) GetByResource(ctx context.Context, resource string) ([]domainpermission.Permission, error) {
	return []domainpermission.Permission{m.permission}, nil
}
func (m *permissionRepoTestDouble) GetUserPermissions(ctx context.Context, userId string) ([]domainpermission.Permission, error) {
	return []domainpermission.Permission{m.permission}, nil
}

func TestCreateRejectsDuplicatePermissionName(t *testing.T) {
	svc := NewPermissionService(&permissionRepoTestDouble{
		permissionByKey: map[string]domainpermission.Permission{
			"list_users": {Id: "perm-1", Name: "list_users"},
		},
	})

	_, err := svc.Create(context.Background(), dto.PermissionCreate{Name: "list_users"})
	if err == nil || err.Error() != "permission with this name already exists" {
		t.Fatalf("expected duplicate error, got %v", err)
	}
}

func TestCreateStoresPermission(t *testing.T) {
	repo := &permissionRepoTestDouble{}
	svc := NewPermissionService(repo)

	got, err := svc.Create(context.Background(), dto.PermissionCreate{
		Name:        "list_users",
		DisplayName: "List Users",
		Description: "Can list users",
		Resource:    "users",
		Action:      "list",
	})
	if err != nil {
		t.Fatalf("expected success, got %v", err)
	}

	if got.Id == "" || repo.stored.Id != got.Id {
		t.Fatalf("expected generated id to be stored, got result=%+v stored=%+v", got, repo.stored)
	}
	if repo.stored.Resource != "users" || repo.stored.Action != "list" {
		t.Fatalf("unexpected stored permission: %+v", repo.stored)
	}
}

func TestUpdatePreservesEmptyFieldsAndSetsUpdatedAt(t *testing.T) {
	repo := &permissionRepoTestDouble{permission: domainpermission.Permission{
		Id:          "perm-1",
		Name:        "list_users",
		DisplayName: "List Users",
		Description: "old",
		Resource:    "users",
		Action:      "list",
	}}
	svc := NewPermissionService(repo)

	got, err := svc.Update(context.Background(), "perm-1", dto.PermissionUpdate{
		Description: "new",
		Action:      "view",
	})
	if err != nil {
		t.Fatalf("expected success, got %v", err)
	}

	if got.DisplayName != "List Users" || got.Resource != "users" {
		t.Fatalf("expected empty fields to preserve existing values, got %+v", got)
	}
	if got.Description != "new" || got.Action != "view" || got.UpdatedAt == nil {
		t.Fatalf("expected mutable fields to update, got %+v", got)
	}
}

func TestDeleteDelegatesToRepository(t *testing.T) {
	repo := &permissionRepoTestDouble{}
	svc := NewPermissionService(repo)

	if err := svc.Delete(context.Background(), "perm-1"); err != nil {
		t.Fatalf("expected success, got %v", err)
	}
	if repo.deletedID != "perm-1" {
		t.Fatalf("expected deleted id to be recorded, got %q", repo.deletedID)
	}
}

func TestPermissionServicePassThroughMethods(t *testing.T) {
	repo := &permissionRepoTestDouble{permission: domainpermission.Permission{
		Id:       "perm-1",
		Name:     "list_users",
		Resource: "users",
		Action:   "list",
	}}
	svc := NewPermissionService(repo)

	if got, err := svc.GetByID(context.Background(), "perm-1"); err != nil || got.Id != "perm-1" {
		t.Fatalf("get by id: permission=%+v err=%v", got, err)
	}
	if got, total, err := svc.GetAll(context.Background(), filter.BaseParams{}); err != nil || total != 1 || len(got) != 1 {
		t.Fatalf("get all: permissions=%+v total=%d err=%v", got, total, err)
	}
	if got, err := svc.GetByResource(context.Background(), "users"); err != nil || len(got) != 1 {
		t.Fatalf("get by resource: permissions=%+v err=%v", got, err)
	}
	if got, err := svc.GetUserPermissions(context.Background(), "user-1"); err != nil || len(got) != 1 {
		t.Fatalf("get user permissions: permissions=%+v err=%v", got, err)
	}
}
