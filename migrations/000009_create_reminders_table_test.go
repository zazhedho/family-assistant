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
		"CREATE UNIQUE INDEX IF NOT EXISTS ux_spaces_personal_created_by",
		"CREATE TABLE IF NOT EXISTS space_members",
		"CREATE TABLE IF NOT EXISTS space_invitations",
		"CREATE TABLE IF NOT EXISTS external_identities",
		"CREATE TABLE IF NOT EXISTS identity_link_tokens",
		"CHECK (type IN ('PERSONAL','SHARED'))",
		"CONSTRAINT ck_spaces_category CHECK (category IN ('personal','family','friends','community','work','finance','custom'))",
		"CONSTRAINT ck_spaces_type_category CHECK",
		"invited_email VARCHAR(255),",
		"accepted_by_user_id UUID",
		"fk_space_invitations_acceptor",
		"verified_at TIMESTAMPTZ NOT NULL",
		"metadata JSONB NOT NULL DEFAULT '{}'::jsonb",
		"provider VARCHAR(64) NOT NULL",
	} {
		if !strings.Contains(up, required) {
			t.Errorf("missing %q", required)
		}
	}
	if strings.Count(up, "provider VARCHAR(64) NOT NULL") != 2 {
		t.Errorf("provider must be persisted on external identities and link tokens")
	}
	for _, forbidden := range []string{"families", "family_members", "hermes_profile_id"} {
		if strings.Contains(up, forbidden) {
			t.Errorf("retains %q", forbidden)
		}
	}
	if strings.Contains(up, "REMOVED") {
		t.Error("space member status retains unsupported REMOVED value")
	}

	down := readMigration(t, "000008_seed_space_rbac.down.sql")
	for _, required := range []string{
		"RAISE EXCEPTION",
		"FROM space_members",
		"FROM space_invitations",
	} {
		if !strings.Contains(down, required) {
			t.Errorf("seed rollback missing dependent-row guard %q", required)
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
		"deleted_at TIMESTAMPTZ",
		"'PENDING','SENT','COMPLETED','CANCELLED'",
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

func TestExternalOnlyUserMigration(t *testing.T) {
	up := readMigration(t, "000010_allow_external_only_users.up.sql")
	for _, required := range []string{
		"ALTER COLUMN email DROP NOT NULL",
		"ALTER COLUMN password DROP NOT NULL",
	} {
		if !strings.Contains(up, required) {
			t.Errorf("up migration missing %q", required)
		}
	}

	down := readMigration(t, "000010_allow_external_only_users.down.sql")
	for _, required := range []string{
		"@account.invalid",
		"!external-only:",
		"ALTER COLUMN email SET NOT NULL",
		"ALTER COLUMN password SET NOT NULL",
	} {
		if !strings.Contains(down, required) {
			t.Errorf("down migration missing safe rollback %q", required)
		}
	}
	if strings.Contains(strings.ToUpper(down), "DELETE FROM USERS") {
		t.Fatal("rollback must not delete credentialless accounts")
	}
}

func readMigration(t *testing.T, name string) string {
	t.Helper()
	var (
		content []byte
		err     error
	)
	switch name {
	case "000007_create_space_foundation.up.sql":
		content, err = os.ReadFile("000007_create_space_foundation.up.sql")
	case "000008_seed_space_rbac.down.sql":
		content, err = os.ReadFile("000008_seed_space_rbac.down.sql")
	case "000009_create_reminders_table.up.sql":
		content, err = os.ReadFile("000009_create_reminders_table.up.sql")
	case "000010_allow_external_only_users.up.sql":
		content, err = os.ReadFile("000010_allow_external_only_users.up.sql")
	case "000010_allow_external_only_users.down.sql":
		content, err = os.ReadFile("000010_allow_external_only_users.down.sql")
	default:
		t.Fatalf("unsupported migration %q", name)
	}
	if err != nil {
		t.Fatal(err)
	}
	return string(content)
}
