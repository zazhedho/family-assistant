package moduleseed

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func TestRenderFamilyAssistantSQLSeedsExactRBAC(t *testing.T) {
	sql, err := RenderFamilyAssistantSQL()
	if err != nil {
		t.Fatalf("render family assistant seed: %v", err)
	}

	wantPermissions := map[string]struct {
		resource string
		action   string
	}{
		"families:list":   {resource: "families", action: "list"},
		"families:view":   {resource: "families", action: "view"},
		"families:update": {resource: "families", action: "update"},
		"members:list":    {resource: "members", action: "list"},
		"members:view":    {resource: "members", action: "view"},
		"members:manage":  {resource: "members", action: "manage"},
		"reminders:list":  {resource: "reminders", action: "list"},
		"reminders:view":  {resource: "reminders", action: "view"},
		"reminders:create": {
			resource: "reminders",
			action:   "create",
		},
		"reminders:update": {
			resource: "reminders",
			action:   "update",
		},
		"reminders:delete": {
			resource: "reminders",
			action:   "delete",
		},
	}
	permissionRow := regexp.MustCompile(`(?m)^\s+\(gen_random_uuid\(\), '([^']+)', '[^']*', '(families|members|reminders)', '[^']+'\),?$`)
	permissionNames := make(map[string]int)
	for _, match := range permissionRow.FindAllStringSubmatch(sql, -1) {
		permissionNames[match[1]]++
	}

	for key, permission := range wantPermissions {
		name := permission.action + "_" + permission.resource
		if got := permissionNames[name]; got != 1 {
			t.Errorf("permission %s rendered %d times, want once", key, got)
		}
		fragment := fmt.Sprintf("'%s', '%s'", permission.resource, permission.action)
		if !strings.Contains(sql, fragment) {
			t.Errorf("permission %s missing resource/action fragment %q", key, fragment)
		}
	}

	for _, role := range []string{"parent", "child"} {
		if !strings.Contains(sql, "'"+role+"'") {
			t.Errorf("role %q missing", role)
		}
	}

	for _, fragment := range []string{
		"ON CONFLICT (name) DO UPDATE",
		"ON CONFLICT DO NOTHING;",
		"JOIN permissions p ON p.resource = 'families' AND p.name IN ('list_families', 'view_families', 'update_families') AND p.deleted_at IS NULL",
		"JOIN permissions p ON p.resource = 'families' AND p.name IN ('view_families') AND p.deleted_at IS NULL",
	} {
		if !strings.Contains(sql, fragment) {
			t.Errorf("seed missing idempotent/grant fragment %q", fragment)
		}
	}
}

func TestFamilyAssistantMigrationUsesRenderedSeed(t *testing.T) {
	want, err := RenderFamilyAssistantSQL()
	if err != nil {
		t.Fatalf("render family assistant seed: %v", err)
	}

	migrationPath := filepath.Join("..", "..", "migrations", "000008_seed_family_assistant_rbac.up.sql")
	contents, err := os.ReadFile(migrationPath) // #nosec G304 -- path is fixed to the repository migration fixture.
	if err != nil {
		t.Fatalf("read family assistant migration: %v", err)
	}

	got := strings.TrimSpace(string(contents))
	got = strings.TrimPrefix(got, "-- Family Assistant roles and module permissions are rendered through pkg/moduleseed.\n")
	if got != want {
		t.Fatalf("migration differs from rendered seed")
	}
}

func TestFamilyAssistantSeedUsesExactCanonicalGrantsAndSafeRollback(t *testing.T) {
	sql, err := RenderFamilyAssistantSQL()
	if err != nil {
		t.Fatalf("render family assistant seed: %v", err)
	}

	wantNames := map[string]struct{}{
		"list_families":    {},
		"view_families":    {},
		"update_families":  {},
		"list_members":     {},
		"view_members":     {},
		"manage_members":   {},
		"list_reminders":   {},
		"view_reminders":   {},
		"create_reminders": {},
		"update_reminders": {},
		"delete_reminders": {},
	}
	permissionRow := regexp.MustCompile(`(?m)^\s+\(gen_random_uuid\(\), '([^']+)', '[^']*', '(families|members|reminders)', '[^']+'\),?$`)
	matches := permissionRow.FindAllStringSubmatch(sql, -1)
	if len(matches) != len(wantNames) {
		t.Fatalf("permission rows: got %d, want exactly %d", len(matches), len(wantNames))
	}
	gotNames := make(map[string]struct{}, len(matches))
	for _, match := range matches {
		gotNames[match[1]] = struct{}{}
	}
	if len(gotNames) != len(wantNames) {
		t.Fatalf("permission names: got %v, want exactly %v", gotNames, wantNames)
	}
	for name := range wantNames {
		if _, ok := gotNames[name]; !ok {
			t.Errorf("missing canonical permission name %q", name)
		}
	}
	roleGrant := regexp.MustCompile(`(?m)^INSERT INTO role_permissions \(role_id, permission_id\)`)
	if got := len(roleGrant.FindAllString(sql, -1)); got != 6 {
		t.Fatalf("role grant statements: got %d, want exactly 6", got)
	}

	for _, fragment := range []string{
		"JOIN permissions p ON p.resource = 'families' AND p.name IN ('view_families') AND p.deleted_at IS NULL\nWHERE r.name IN ('child') AND r.deleted_at IS NULL",
		"JOIN permissions p ON p.resource = 'families' AND p.name IN ('list_families', 'view_families', 'update_families') AND p.deleted_at IS NULL\nWHERE r.name IN ('parent') AND r.deleted_at IS NULL",
		"JOIN permissions p ON p.resource = 'members' AND p.name IN ('list_members', 'view_members') AND p.deleted_at IS NULL\nWHERE r.name IN ('child') AND r.deleted_at IS NULL",
		"JOIN permissions p ON p.resource = 'members' AND p.name IN ('list_members', 'view_members', 'manage_members') AND p.deleted_at IS NULL\nWHERE r.name IN ('parent') AND r.deleted_at IS NULL",
		"JOIN permissions p ON p.resource = 'reminders' AND p.name IN ('list_reminders', 'view_reminders', 'create_reminders', 'update_reminders', 'delete_reminders') AND p.deleted_at IS NULL\nWHERE r.name IN ('child') AND r.deleted_at IS NULL",
		"JOIN permissions p ON p.resource = 'reminders' AND p.name IN ('list_reminders', 'view_reminders', 'create_reminders', 'update_reminders', 'delete_reminders') AND p.deleted_at IS NULL\nWHERE r.name IN ('parent') AND r.deleted_at IS NULL",
		"ON CONFLICT (name) DO UPDATE",
		"display_name = EXCLUDED.display_name",
		"resource = EXCLUDED.resource",
		"action = EXCLUDED.action",
		"deleted_at = NULL",
	} {
		if !strings.Contains(sql, fragment) {
			t.Errorf("seed missing canonical/upsert fragment %q", fragment)
		}
	}

	downPath := filepath.Join("..", "..", "migrations", "000008_seed_family_assistant_rbac.down.sql")
	down, err := os.ReadFile(downPath) // #nosec G304 -- path is fixed to the repository migration fixture.
	if err != nil {
		t.Fatalf("read family assistant down migration: %v", err)
	}
	if strings.Contains(strings.ToUpper(string(down)), "DELETE FROM") {
		t.Fatalf("down migration must not delete rows without provenance")
	}
}
