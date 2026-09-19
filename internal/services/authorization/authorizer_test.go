package authorization

import (
	"context"
	"errors"
	"testing"

	identity "github.com/zazhedho/family-assistant/internal/services/identity"
)

func TestAuthorizerChecksValidationBeforePermission(t *testing.T) {
	actor := identity.ActorContext{Permissions: map[string]struct{}{}}
	err := NewAuthorizer().Authorize(context.Background(), actor, "reminders:view", Resource{
		Scope: ScopePersonal,
	})

	var validationErr *ValidationError
	if !errors.As(err, &validationErr) {
		t.Fatalf("Authorize() error = %T %v, want *ValidationError", err, err)
	}
}

func TestAuthorizerChecksPermissionBeforeFamily(t *testing.T) {
	actor := task5Actor("child", "child-1", "family-1", "reminders:list")
	err := NewAuthorizer().Authorize(context.Background(), actor, "reminders:view", Resource{
		FamilyID: "family-2",
		Scope:    ScopeFamily,
	})

	if !errors.Is(err, ErrForbidden) {
		t.Fatalf("Authorize() error = %v, want ErrForbidden before cross-family check", err)
	}
}

func TestAuthorizerRejectsBlankPermission(t *testing.T) {
	permission := " \t"
	actor := identity.ActorContext{
		FamilyID:    "family-1",
		Permissions: map[string]struct{}{permission: {}},
	}
	err := NewAuthorizer().Authorize(context.Background(), actor, permission, Resource{
		FamilyID: "family-1",
		Scope:    ScopeFamily,
	})

	if !errors.Is(err, ErrForbidden) {
		t.Fatalf("Authorize() error = %v, want ErrForbidden", err)
	}
}

func TestAuthorizerRequiresPermissionForFamilyResource(t *testing.T) {
	actor := identity.ActorContext{
		MemberID:    "member-1",
		FamilyID:    "family-1",
		Permissions: map[string]struct{}{},
	}
	err := NewAuthorizer().Authorize(context.Background(), actor, "reminders:view", Resource{
		FamilyID: "family-1",
		Scope:    ScopeFamily,
	})

	if !errors.Is(err, ErrForbidden) {
		t.Fatalf("Authorize() error = %v, want ErrForbidden", err)
	}
}

func TestAuthorizerImplementsInterface(t *testing.T) {
	var _ Authorizer = authorizer{}
}
