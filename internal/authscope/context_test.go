package authscope

import (
	"context"
	"testing"
)

func TestNewFromClaimsNormalizesPermissionsAndImpersonation(t *testing.T) {
	scope := NewFromClaims(map[string]any{
		"user_id":           " user-1 ",
		"username":          " Jane ",
		"role":              "admin",
		"is_impersonated":   true,
		"original_user_id":  "admin-1",
		"original_username": "Admin",
		"original_role":     "superadmin",
	}, []string{" Users:List ", "invalid", "roles:update"})

	if scope.UserID != "user-1" {
		t.Fatalf("expected trimmed user id, got %q", scope.UserID)
	}
	if !scope.Has("users", "list") {
		t.Fatal("expected users:list permission")
	}
	if !scope.Has("ROLES", "UPDATE") {
		t.Fatal("expected case-insensitive roles:update permission")
	}
	if scope.Has("invalid", "") {
		t.Fatal("expected invalid permission key to be ignored")
	}
	if !scope.IsImpersonated || scope.OriginalUserID != "admin-1" {
		t.Fatalf("expected impersonation claims, got %+v", scope)
	}
}

func TestFromContextReturnsEmptyScopeWhenMissing(t *testing.T) {
	scope := FromContext(context.Background())
	if scope.UserID != "" {
		t.Fatalf("expected empty user id, got %q", scope.UserID)
	}
	if scope.Permissions == nil {
		t.Fatal("expected non-nil permissions map")
	}
}

func TestWithContextStoresScope(t *testing.T) {
	want := New(" user-1 ", " Jane ", "admin", []string{"users:read"})
	ctx := WithContext(context.Background(), want)

	got := FromContext(ctx)
	if got.UserID != "user-1" || got.Username != "Jane" || got.Role != "admin" {
		t.Fatalf("unexpected scope from context: %+v", got)
	}
	if !got.Has("users", "read") {
		t.Fatalf("expected permission from context, got %+v", got.Permissions)
	}
}

func TestSuperadminHasEveryPermission(t *testing.T) {
	scope := New("user-1", "Root", "superadmin", nil)
	if !scope.Has("anything", "delete") {
		t.Fatal("expected superadmin to bypass permission map")
	}
}

func TestActorUserIDAndRolePreferOriginalImpersonator(t *testing.T) {
	scope := NewFromClaims(map[string]any{
		"user_id":          "member-1",
		"role":             "member",
		"is_impersonated":  true,
		"original_user_id": "admin-1",
		"original_role":    "superadmin",
	}, nil)

	if scope.ActorUserID() != "admin-1" {
		t.Fatalf("expected original actor user id, got %q", scope.ActorUserID())
	}
	if scope.ActorRole() != "superadmin" {
		t.Fatalf("expected original actor role, got %q", scope.ActorRole())
	}
}
