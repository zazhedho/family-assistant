package moduleseed

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRenderSpaceSQLSeedsSpaceRBAC(t *testing.T) {
	sql, err := RenderSpaceSQL()
	if err != nil {
		t.Fatalf("render space seed: %v", err)
	}

	for _, role := range []string{"space_owner", "space_admin", "space_member", "space_viewer"} {
		if !strings.Contains(sql, "'"+role+"'") {
			t.Errorf("role %q missing", role)
		}
	}
	for _, resource := range []string{"spaces", "members", "invitations", "reminders"} {
		if !strings.Contains(sql, "'"+resource+"'") {
			t.Errorf("resource %q missing", resource)
		}
	}
	for _, forbidden := range []string{"'parent'", "'child'", "'families'"} {
		if strings.Contains(sql, forbidden) {
			t.Errorf("seed retains forbidden value %q", forbidden)
		}
	}

	for _, fragment := range []string{
		"'list_spaces'",
		"'view_spaces'",
		"'create_spaces'",
		"'list_members'",
		"'view_members'",
		"'create_invitations'",
		"'list_reminders'",
		"'view_reminders'",
		"'create_reminders'",
		"'update_reminders'",
		"ON CONFLICT (name) DO UPDATE",
		"ON CONFLICT DO NOTHING;",
	} {
		if !strings.Contains(sql, fragment) {
			t.Errorf("seed missing %q", fragment)
		}
	}
}

func TestRenderSpaceSQLUsesExactGrantMatrix(t *testing.T) {
	sql, err := RenderSpaceSQL()
	if err != nil {
		t.Fatalf("render space seed: %v", err)
	}

	want := map[string]map[string][]string{
		"space_owner": {
			"spaces":      {"list", "view", "create"},
			"members":     {"list", "view"},
			"invitations": {"create"},
			"reminders":   {"list", "view", "create", "update"},
		},
		"space_admin": {
			"spaces":      {"list", "view"},
			"members":     {"list", "view"},
			"invitations": {"create"},
			"reminders":   {"list", "view", "create", "update"},
		},
		"space_member": {
			"spaces":    {"list", "view"},
			"members":   {"list", "view"},
			"reminders": {"list", "view", "create", "update"},
		},
		"space_viewer": {
			"spaces":    {"list", "view"},
			"members":   {"list", "view"},
			"reminders": {"list", "view"},
		},
	}
	if got := strings.Count(sql, "INSERT INTO role_permissions"); got != 14 {
		t.Fatalf("grant statements: got %d, want 14", got)
	}

	for role, resources := range want {
		for resource, actions := range resources {
			names := make([]string, len(actions))
			for i, action := range actions {
				names[i] = "'" + action + "_" + resource + "'"
			}
			fragment := fmt.Sprintf(
				"JOIN permissions p ON p.resource = '%s' AND p.name IN (%s) AND p.deleted_at IS NULL\nWHERE r.name IN ('%s')",
				resource, strings.Join(names, ", "), role,
			)
			if !strings.Contains(sql, fragment) {
				t.Errorf("missing exact grant for %s/%s", role, resource)
			}
		}
	}
}

func TestSpaceSeedMigrationUsesRenderedSeed(t *testing.T) {
	want, err := RenderSpaceSQL()
	if err != nil {
		t.Fatalf("render space seed: %v", err)
	}

	migrationPath := filepath.Join("..", "..", "migrations", "000008_seed_space_rbac.up.sql")
	contents, err := os.ReadFile(migrationPath) // #nosec G304 -- path is fixed to the repository migration fixture.
	if err != nil {
		t.Fatalf("read space seed migration: %v", err)
	}

	got := strings.TrimSpace(string(contents))
	got = strings.TrimPrefix(got, "-- Space roles and module permissions are rendered through pkg/moduleseed.\n")
	if got != want {
		t.Fatalf("migration differs from rendered seed")
	}
}
