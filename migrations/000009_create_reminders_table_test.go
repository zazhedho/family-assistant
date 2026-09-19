package migrations

import (
	"os"
	"strings"
	"testing"
)

func TestSpaceFoundationMigration(t *testing.T) {
	up := readMigration(t, "000007_create_space_foundation.up.sql")
	for _, required := range []string{
		"ADD COLUMN IF NOT EXISTS birth_date DATE",
		"CREATE TABLE IF NOT EXISTS spaces",
		"CREATE UNIQUE INDEX ux_spaces_personal_created_by",
		"CREATE TABLE IF NOT EXISTS space_members",
		"CREATE TABLE IF NOT EXISTS space_invitations",
		"CREATE TABLE IF NOT EXISTS external_identities",
		"CREATE TABLE IF NOT EXISTS identity_link_tokens",
		"CHECK (type IN ('PERSONAL','SHARED'))",
	} {
		if !strings.Contains(up, required) {
			t.Errorf("missing %q", required)
		}
	}
	for _, forbidden := range []string{"families", "family_members", "hermes_profile_id"} {
		if strings.Contains(up, forbidden) {
			t.Errorf("retains %q", forbidden)
		}
	}
}

func TestReminderMigrationIsSpaceScoped(t *testing.T) {
	up := readMigration(t, "000009_create_reminders_table.up.sql")
	for _, required := range []string{
		"space_id UUID NOT NULL",
		"assignee_member_id UUID",
		"fk_reminders_creator_space",
		"fk_reminders_assignee_space",
	} {
		if !strings.Contains(up, required) {
			t.Errorf("missing %q", required)
		}
	}
	for _, forbidden := range []string{"family_id", "owner_member_id", "scope VARCHAR"} {
		if strings.Contains(up, forbidden) {
			t.Errorf("retains %q", forbidden)
		}
	}
}

func readMigration(t *testing.T, name string) string {
	t.Helper()
	content, err := os.ReadFile(name)
	if err != nil {
		t.Fatal(err)
	}
	return string(content)
}
