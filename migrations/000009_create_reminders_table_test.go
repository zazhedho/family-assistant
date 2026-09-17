package migrations

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReminderMigrationUsesSameFamilyCompositeForeignKeys(t *testing.T) {
	up, err := os.ReadFile(filepath.Join("000009_create_reminders_table.up.sql"))
	if err != nil {
		t.Fatalf("read up migration: %v", err)
	}
	down, err := os.ReadFile(filepath.Join("000009_create_reminders_table.down.sql"))
	if err != nil {
		t.Fatalf("read down migration: %v", err)
	}

	upSQL := string(up)
	for _, required := range []string{
		"CREATE UNIQUE INDEX IF NOT EXISTS ux_family_members_family_id_id",
		"ON family_members (family_id, id);",
		"CONSTRAINT fk_reminders_owner_family FOREIGN KEY (family_id, owner_member_id)",
		"CONSTRAINT fk_reminders_creator_family FOREIGN KEY (family_id, created_by_member_id)",
		"REFERENCES family_members(family_id, id)",
	} {
		if !strings.Contains(upSQL, required) {
			t.Errorf("up migration missing %q", required)
		}
	}
	for _, forbidden := range []string{
		"owner_member_id UUID NOT NULL REFERENCES family_members(id)",
		"created_by_member_id UUID NOT NULL REFERENCES family_members(id)",
	} {
		if strings.Contains(upSQL, forbidden) {
			t.Errorf("up migration retains redundant foreign key %q", forbidden)
		}
	}

	downSQL := string(down)
	tablePos := strings.Index(downSQL, "DROP TABLE IF EXISTS reminders;")
	indexPos := strings.Index(downSQL, "DROP INDEX IF EXISTS ux_family_members_family_id_id;")
	if tablePos < 0 || indexPos < 0 || tablePos > indexPos {
		t.Fatalf("down migration must drop reminders before family-member index")
	}
}
