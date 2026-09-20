package authorization

import (
	"context"
	"errors"
	"testing"

	identity "family-assistant/internal/services/identity"
)

func TestAuthorizerChecksValidationBeforePermission(t *testing.T) {
	actor := identity.ActorContext{Permissions: map[string]struct{}{}}
	err := NewAuthorizer().Authorize(context.Background(), actor, "reminders:view", Resource{})

	var validationErr *ValidationError
	if !errors.As(err, &validationErr) {
		t.Fatalf("Authorize() error = %T %v, want *ValidationError", err, err)
	}
}

func TestAuthorizerChecksPermissionBeforeCrossSpace(t *testing.T) {
	actor := spaceActor("space-1", "reminders:list")
	err := NewAuthorizer().Authorize(context.Background(), actor, "reminders:view", Resource{SpaceID: "space-2"})

	if !errors.Is(err, ErrForbidden) {
		t.Fatalf("Authorize() error = %v, want ErrForbidden before cross-Space check", err)
	}
}

func TestAuthorizerRejectsCrossSpaceResourceAsNotFound(t *testing.T) {
	actor := spaceActor("space-1", "reminders:view")
	err := NewAuthorizer().Authorize(context.Background(), actor, "reminders:view", Resource{SpaceID: "space-2"})

	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("Authorize() error = %v, want ErrNotFound", err)
	}
}

func TestAuthorizerAllowsAccessibleResourceWithPermission(t *testing.T) {
	actor := spaceActor("space-1", "reminders:view")
	err := NewAuthorizer().Authorize(context.Background(), actor, "reminders:view", Resource{
		SpaceID:           "space-1",
		CreatedByMemberID: "member-1",
		AssigneeMemberID:  "member-2",
	})
	if err != nil {
		t.Fatalf("Authorize() error = %v", err)
	}
}

func TestAuthorizerRejectsBlankPermission(t *testing.T) {
	actor := spaceActor("space-1", "")
	err := NewAuthorizer().Authorize(context.Background(), actor, " \t", Resource{SpaceID: "space-1"})
	if !errors.Is(err, ErrForbidden) {
		t.Fatalf("Authorize() error = %v, want ErrForbidden", err)
	}
}

func spaceActor(spaceID, permission string) identity.ActorContext {
	permissions := map[string]struct{}{}
	if permission != "" {
		permissions[permission] = struct{}{}
	}
	return identity.ActorContext{SpaceID: spaceID, Permissions: permissions}
}

func TestAuthorizerImplementsInterface(t *testing.T) {
	var _ Authorizer = authorizer{}
}
