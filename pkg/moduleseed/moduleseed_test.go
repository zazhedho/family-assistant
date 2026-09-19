package moduleseed

import (
	"strings"
	"testing"
)

func TestRenderSQLUsesConsistentResourceAndMenuName(t *testing.T) {
	sql, err := RenderSQL(Definition{
		Name:        "projects",
		DisplayName: "Projects",
		Path:        "/projects",
		Icon:        "bi-folder",
		OrderIndex:  905,
		Actions:     []string{"list", "view", "create", "update", "delete"},
		GrantRoles:  []string{"admin", "superadmin"},
	})
	if err != nil {
		t.Fatalf("expected success, got %v", err)
	}

	expectedFragments := []string{
		"'projects', 'Projects', '/projects', 'bi-folder', 905, TRUE",
		"'list_projects', 'List Projects', 'projects', 'list'",
		"'view_projects', 'View Projects', 'projects', 'view'",
		"JOIN permissions p ON p.resource = 'projects'",
		"WHERE r.name IN ('admin', 'superadmin')",
	}
	for _, fragment := range expectedFragments {
		if !strings.Contains(sql, fragment) {
			t.Fatalf("expected SQL to contain %q\nSQL:\n%s", fragment, sql)
		}
	}
}

func TestRenderSQLSupportsParentMenu(t *testing.T) {
	sql, err := RenderSQL(Definition{
		Name:        "education_stats",
		DisplayName: "Education Stats",
		Path:        "/education/stats",
		Icon:        "bi-bar-chart",
		OrderIndex:  210,
		ParentName:  "education",
		Actions:     []string{"list", "view"},
	})
	if err != nil {
		t.Fatalf("expected success, got %v", err)
	}

	if !strings.Contains(sql, "(SELECT id FROM menu_items WHERE name = 'education' AND deleted_at IS NULL LIMIT 1)") {
		t.Fatalf("expected parent_id subquery in SQL:\n%s", sql)
	}
}

func TestRenderSQLRejectsMissingRequiredFields(t *testing.T) {
	_, err := RenderSQL(Definition{Name: "projects"})
	if err == nil || err.Error() != "display_name is required" {
		t.Fatalf("expected display_name validation error, got %v", err)
	}
}

func TestRenderSpaceSQLIsStable(t *testing.T) {
	first, err := RenderSpaceSQL()
	if err != nil {
		t.Fatalf("render first seed: %v", err)
	}
	second, err := RenderSpaceSQL()
	if err != nil {
		t.Fatalf("render second seed: %v", err)
	}
	if first != second {
		t.Fatal("Space seed must be deterministic")
	}
	if strings.Contains(first, "gen_random_uuid()") {
		t.Fatal("Space seed must not generate random seed IDs")
	}
}
