package migrations

import (
	"os"
	"strings"
	"testing"
)

func TestCRUDPermissionsAreSeededInExistingMigrations(t *testing.T) {
	spaceRBAC, err := os.ReadFile("000008_seed_space_rbac.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	activityRBAC, err := os.ReadFile("000011_create_space_activities_table.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	combined := string(spaceRBAC) + "\n" + string(activityRBAC)
	for _, permission := range []string{
		"delete_reminders",
		"update_spaces",
		"delete_spaces",
		"list_invitations",
		"delete_invitations",
		"update_members",
		"delete_members",
		"update_activities",
		"delete_activities",
	} {
		if !strings.Contains(combined, permission) {
			t.Errorf("missing CRUD permission %q", permission)
		}
	}
}

func TestCRUDPermissionsAreRemovedByExistingDownMigrations(t *testing.T) {
	spaceRBAC, err := os.ReadFile("000008_seed_space_rbac.down.sql")
	if err != nil {
		t.Fatal(err)
	}
	activityRBAC, err := os.ReadFile("000011_create_space_activities_table.down.sql")
	if err != nil {
		t.Fatal(err)
	}
	combined := string(spaceRBAC) + "\n" + string(activityRBAC)
	for _, permission := range []string{
		"delete_reminders",
		"update_spaces",
		"delete_spaces",
		"list_invitations",
		"delete_invitations",
		"update_members",
		"delete_members",
		"update_activities",
		"delete_activities",
	} {
		if !strings.Contains(combined, permission) {
			t.Errorf("down migrations do not remove CRUD permission %q", permission)
		}
	}
}
