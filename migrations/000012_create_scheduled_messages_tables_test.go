package migrations

import (
	"os"
	"strings"
	"testing"
)

func TestScheduledMessageMigrationIsSpaceScopedAndPreservesOccurrences(t *testing.T) {
	up := readScheduledMessageMigration(t, "000012_create_scheduled_messages_tables.up.sql")
	for _, required := range []string{
		"CREATE TABLE IF NOT EXISTS space_channels",
		"CREATE TABLE IF NOT EXISTS scheduled_messages",
		"CREATE TABLE IF NOT EXISTS scheduled_message_deliveries",
		"FOREIGN KEY (space_id, bound_by_member_id)",
		"FOREIGN KEY (space_id, created_by_member_id)",
		"FOREIGN KEY (space_id, recipient_member_id)",
		"FOREIGN KEY (space_id, space_channel_id)",
		"external_chat_id ~ '^[0-9]*[1-9][0-9]*@g[.]us$'",
		"UNIQUE (scheduled_message_id, scheduled_at)",
		"lease_until TIMESTAMPTZ",
		"lease_token UUID",
		"WHERE status = 'ACTIVE'",
		"WHERE status = 'PENDING'",
	} {
		if !strings.Contains(up, required) {
			t.Errorf("up migration missing %q", required)
		}
	}
	if got := strings.Count(up, "CREATE TABLE IF NOT EXISTS "); got != 3 {
		t.Errorf("up migration creates %d tables, want only the three scheduled-message tables", got)
	}
}

func TestScheduledMessageMigrationSeedsScopedRolePermissions(t *testing.T) {
	up := readScheduledMessageMigration(t, "000012_create_scheduled_messages_tables.up.sql")
	for _, required := range []string{
		"create_scheduled_messages",
		"list_scheduled_messages",
		"view_scheduled_messages",
		"update_scheduled_messages",
		"cancel_scheduled_messages",
		"unsubscribe_scheduled_messages",
		"bind_space_channels",
		"space_owner",
		"space_admin",
		"space_member",
		"space_viewer",
	} {
		if !strings.Contains(up, required) {
			t.Errorf("up migration missing scoped permission seed %q", required)
		}
	}
}

func TestScheduledMessageDownMigrationDropsOnlyNewObjects(t *testing.T) {
	down := readScheduledMessageMigration(t, "000012_create_scheduled_messages_tables.down.sql")
	for _, required := range []string{
		"scheduled_message_deliveries",
		"scheduled_messages",
		"space_channels",
		"production rollback",
	} {
		if !strings.Contains(strings.ToLower(down), strings.ToLower(required)) {
			t.Errorf("down migration missing %q", required)
		}
	}
	if got := strings.Count(strings.ToUpper(down), "DROP TABLE"); got != 3 {
		t.Errorf("down migration drops %d tables, want only the three scheduled-message tables", got)
	}
	for _, forbidden := range []string{"DROP TABLE IF EXISTS REMINDERS", "DROP TABLE IF EXISTS USERS", "DROP TABLE IF EXISTS SPACES"} {
		if strings.Contains(strings.ToUpper(down), forbidden) {
			t.Errorf("down migration must preserve existing table: %s", forbidden)
		}
	}
}

func readScheduledMessageMigration(t *testing.T, name string) string {
	t.Helper()
	var content []byte
	var err error
	switch name {
	case "000012_create_scheduled_messages_tables.up.sql":
		content, err = os.ReadFile("000012_create_scheduled_messages_tables.up.sql")
	case "000012_create_scheduled_messages_tables.down.sql":
		content, err = os.ReadFile("000012_create_scheduled_messages_tables.down.sql")
	default:
		t.Fatalf("unexpected scheduled-message migration file: %q", name)
	}
	if err != nil {
		t.Fatal(err)
	}
	return string(content)
}
