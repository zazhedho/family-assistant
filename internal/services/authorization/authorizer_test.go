package authorization

import (
	"context"
	"errors"
	"testing"

	domainauthorization "family-assistant/internal/domain/authorization"
	domainidentity "family-assistant/internal/domain/identity"
	interfaceauthorization "family-assistant/internal/interfaces/authorization"
)

func TestAuthorizerChecksValidationBeforePermission(t *testing.T) {
	actor := domainidentity.ActorContext{Permissions: map[string]struct{}{}}
	err := NewAuthorizer().Authorize(context.Background(), actor, "reminders:view", domainauthorization.Resource{})

	if _, ok := errors.AsType[*ValidationError](err); !ok {
		t.Fatalf("Authorize() error = %T %v, want *ValidationError", err, err)
	}
}

func TestAuthorizerDoesNotAuthorizeLegacyFamilyResource(t *testing.T) {
	actor := domainidentity.ActorContext{
		Permissions: map[string]struct{}{"reminders:view": {}},
	}
	err := NewAuthorizer().Authorize(context.Background(), actor, "reminders:view", domainauthorization.Resource{})

	var validationErr *ValidationError
	if !errors.As(err, &validationErr) || validationErr.Field != "space_id" {
		t.Fatalf("Authorize() error = %T %v, want space_id ValidationError", err, err)
	}
}

func TestAuthorizerChecksPermissionBeforeCrossSpace(t *testing.T) {
	actor := spaceActor("space-1", "reminders:list")
	err := NewAuthorizer().Authorize(context.Background(), actor, "reminders:view", domainauthorization.Resource{SpaceID: "space-2"})

	if !errors.Is(err, ErrForbidden) {
		t.Fatalf("Authorize() error = %v, want ErrForbidden before cross-Space check", err)
	}
}

func TestAuthorizerRejectsCrossSpaceResourceAsNotFound(t *testing.T) {
	actor := spaceActor("space-1", "reminders:view")
	err := NewAuthorizer().Authorize(context.Background(), actor, "reminders:view", domainauthorization.Resource{SpaceID: "space-2"})

	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("Authorize() error = %v, want ErrNotFound", err)
	}
}

func TestAuthorizerAllowsAccessibleResourceWithPermission(t *testing.T) {
	actor := spaceActor("space-1", "reminders:view")
	err := NewAuthorizer().Authorize(context.Background(), actor, "reminders:view", domainauthorization.Resource{SpaceID: "space-1"})
	if err != nil {
		t.Fatalf("Authorize() error = %v", err)
	}
}

func TestAuthorizerRejectsBlankPermission(t *testing.T) {
	actor := spaceActor("space-1", "")
	err := NewAuthorizer().Authorize(context.Background(), actor, " \t", domainauthorization.Resource{SpaceID: "space-1"})
	if !errors.Is(err, ErrForbidden) {
		t.Fatalf("Authorize() error = %v, want ErrForbidden", err)
	}
}

func spaceActor(spaceID, permission string) domainidentity.ActorContext {
	permissions := map[string]struct{}{}
	if permission != "" {
		permissions[permission] = struct{}{}
	}
	return domainidentity.ActorContext{SpaceID: spaceID, Permissions: permissions}
}

func TestAuthorizerImplementsInterface(t *testing.T) {
	var _ interfaceauthorization.Authorizer = authorizer{}
}
