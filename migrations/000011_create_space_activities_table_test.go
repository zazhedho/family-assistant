package migrations

import (
	"os"
	"strings"
	"testing"
)

func TestSpaceActivitiesMigrationIsScopedAndPermissionSeeded(t *testing.T) {
	upBytes, err := os.ReadFile("000011_create_space_activities_table.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	up := string(upBytes)
	for _, required := range []string{
		"CREATE TABLE IF NOT EXISTS space_activities",
		"space_id UUID NOT NULL",
		"created_by_member_id UUID NOT NULL",
		"fk_space_activities_creator_space",
		"occurred_at TIMESTAMPTZ NOT NULL",
		"update_activities",
		"delete_activities",
		"space_owner",
		"space_admin",
		"space_member",
	} {
		if !strings.Contains(up, required) {
			t.Errorf("missing %q", required)
		}
	}
	for _, forbidden := range []string{"family_id", "hermes_profile_id"} {
		if strings.Contains(up, forbidden) {
			t.Errorf("retains %q", forbidden)
		}
	}

	downBytes, err := os.ReadFile("000011_create_space_activities_table.down.sql")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(downBytes), "DROP TABLE IF EXISTS space_activities") {
		t.Error("down migration does not drop space_activities")
	}
}
