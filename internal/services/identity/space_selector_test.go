package serviceidentity

import (
	"context"
	"errors"
	"testing"

	domainpermission "family-assistant/internal/domain/permission"
	domainspace "family-assistant/internal/domain/space"
)

func selectorActor(memberships ...domainspace.ResolvedMembership) ActorContext {
	return ActorContext{UserID: "user-1", Memberships: memberships}
}

func selectorPermissions() *permissionServiceStub {
	return &permissionServiceStub{permissions: []domainpermission.Permission{
		{Name: "view_reminders", Resource: "reminders", Action: "view"},
	}}
}

func TestSelectSpaceDefaultsToExactlyOnePersonalSpace(t *testing.T) {
	permissions := selectorPermissions()
	actor := selectorActor(
		activeMembership("member-personal", "personal-1", "Jane", domainspace.TypePersonal, "user-1", "role-owner", "space_owner"),
		activeMembership("member-shared", "shared-1", "Trading", domainspace.TypeShared, "user-1", "role-member", "space_member"),
	)

	got, err := SelectSpace(context.Background(), actor, "", permissions)
	if err != nil {
		t.Fatalf("select default Space: %v", err)
	}
	if got.SpaceID != "personal-1" || got.SpaceName != "Jane" || got.SpaceType != domainspace.TypePersonal || got.MemberID != "member-personal" || got.RoleID != "role-owner" || got.RoleName != "space_owner" {
		t.Fatalf("unexpected selected actor: %+v", got)
	}
	if !got.HasPermission("reminders:view") || permissions.calls != 1 || permissions.roleID != "role-owner" {
		t.Fatalf("unexpected loaded permissions: actor=%+v calls=%d role=%q", got, permissions.calls, permissions.roleID)
	}
	if len(got.Memberships) != 2 {
		t.Fatalf("selection must preserve memberships: %#v", got.Memberships)
	}
}

func TestSelectSpaceUsesExactUUIDBeforeName(t *testing.T) {
	permissions := selectorPermissions()
	actor := selectorActor(
		activeMembership("member-1", "00000000-0000-0000-0000-000000000001", "Home", domainspace.TypeShared, "user-1", "role-1", "space_member"),
		activeMembership("member-2", "00000000-0000-0000-0000-000000000002", "Home", domainspace.TypeShared, "user-1", "role-2", "space_admin"),
	)

	got, err := SelectSpace(context.Background(), actor, "00000000-0000-0000-0000-000000000002", permissions)
	if err != nil {
		t.Fatalf("select UUID Space: %v", err)
	}
	if got.SpaceID != "00000000-0000-0000-0000-000000000002" || got.MemberID != "member-2" || permissions.roleID != "role-2" {
		t.Fatalf("unexpected UUID selection: %+v role=%q", got, permissions.roleID)
	}
}

func TestSelectSpaceUsesTrimmedCaseInsensitiveName(t *testing.T) {
	permissions := selectorPermissions()
	actor := selectorActor(activeMembership("member-1", "space-1", "  Trading  ", domainspace.TypeShared, "user-1", "role-1", "space_member"))

	got, err := SelectSpace(context.Background(), actor, "  tRaDiNg ", permissions)
	if err != nil {
		t.Fatalf("select by name: %v", err)
	}
	if got.SpaceID != "space-1" || got.SpaceName != "Trading" {
		t.Fatalf("unexpected name selection: %+v", got)
	}
}

func TestSelectSpaceReturnsNotFoundForNoMatchOrInactiveMembership(t *testing.T) {
	permissions := selectorPermissions()
	actor := selectorActor(
		activeMembership("member-1", "space-1", "Trading", domainspace.TypeShared, "user-1", "role-1", "space_member"),
		domainspace.ResolvedMembership{ID: "member-inactive", SpaceID: "space-2", SpaceName: "Old", SpaceType: domainspace.TypeShared, UserID: "user-1", RoleID: "role-2", RoleName: "space_member", Status: domainspace.StatusInactive},
	)

	for _, selector := range []string{"missing", "space-2"} {
		_, err := SelectSpace(context.Background(), actor, selector, permissions)
		if !errors.Is(err, ErrNotFound) {
			t.Fatalf("selector %q error = %v, want ErrNotFound", selector, err)
		}
	}
	if permissions.calls != 0 {
		t.Fatalf("not-found selection must not load permissions: %d calls", permissions.calls)
	}
}

func TestSelectSpaceRejectsAmbiguousNormalizedName(t *testing.T) {
	actor := selectorActor(
		activeMembership("member-1", "space-1", "Trading", domainspace.TypeShared, "user-1", "role-1", "space_member"),
		activeMembership("member-2", "space-2", " trading ", domainspace.TypeShared, "user-1", "role-2", "space_member"),
	)

	_, err := SelectSpace(context.Background(), actor, "TRADING", selectorPermissions())
	assertValidationField(t, err, "space")
}

func TestSelectSpaceRejectsMalformedMultiplePersonalSpacesAsInternal(t *testing.T) {
	permissions := selectorPermissions()
	actor := selectorActor(
		activeMembership("member-1", "personal-1", "Jane", domainspace.TypePersonal, "user-1", "role-1", "space_owner"),
		activeMembership("member-2", "personal-2", "Jane 2", domainspace.TypePersonal, "user-1", "role-2", "space_owner"),
	)

	_, err := SelectSpace(context.Background(), actor, "", permissions)
	if !errors.Is(err, ErrSpaceSelectionMisconfigured) {
		t.Fatalf("error = %v, want malformed Personal Space error", err)
	}
	if permissions.calls != 0 {
		t.Fatalf("malformed default selection must not load permissions: %d calls", permissions.calls)
	}
}

func assertValidationField(t *testing.T, err error, field string) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected validation error for field %q", field)
	}
	var validationErr *ValidationError
	if !errors.As(err, &validationErr) {
		t.Fatalf("error = %T %v, want ValidationError", err, err)
	}
	if validationErr.Field != field {
		t.Fatalf("validation field = %q, want %q", validationErr.Field, field)
	}
}
