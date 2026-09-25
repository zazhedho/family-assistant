package migrations

import (
	"os"
	"strings"
	"testing"
)

func TestCRUDPermissionsRemainSeededAndReversible(t *testing.T) {
	permissions := []string{
		"delete_reminders",
		"update_spaces",
		"delete_spaces",
		"list_invitations",
		"delete_invitations",
		"update_members",
		"delete_members",
		"update_activities",
		"delete_activities",
	}
	cases := []struct {
		name       string
		migrations []string
		focus      string
		required   []string
		forbidden  []string
		missing    string
	}{
		{
			name:       "up",
			migrations: []string{"000008_seed_space_rbac.up.sql", "000011_create_space_activities_table.up.sql"},
			focus:      "000011_create_space_activities_table.up.sql",
			required: []string{
				"('33333333-3333-4333-8333-000000000020', 'update_activities'",
				"('33333333-3333-4333-8333-000000000021', 'delete_activities'",
				"p.name IN ('update_activities', 'delete_activities')",
				"r.name IN ('space_owner', 'space_admin', 'space_member')",
			},
			forbidden: []string{
				"22222222-2222-4222-8222-000000000005",
				"33333333-3333-4333-8333-000000000011",
				"33333333-3333-4333-8333-000000000012",
			},
			missing: "missing CRUD permission %q",
		},
		{
			name:       "down",
			migrations: []string{"000008_seed_space_rbac.down.sql", "000011_create_space_activities_table.down.sql"},
			focus:      "000011_create_space_activities_table.down.sql",
			required: []string{
				"33333333-3333-4333-8333-000000000020",
				"33333333-3333-4333-8333-000000000021",
			},
			forbidden: []string{
				"22222222-2222-4222-8222-000000000005",
				"33333333-3333-4333-8333-000000000011",
				"33333333-3333-4333-8333-000000000012",
			},
			missing: "down migrations do not remove CRUD permission %q",
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			var combined strings.Builder
			var focused string
			for _, migration := range testCase.migrations {
				content, err := os.ReadFile(migration) // #nosec G304 -- paths are fixed migration test fixtures.
				if err != nil {
					t.Fatal(err)
				}
				combined.Write(content)
				if migration == testCase.focus {
					focused = string(content)
				}
			}
			migrationContent := combined.String()
			for _, permission := range permissions {
				if !strings.Contains(migrationContent, permission) {
					t.Errorf(testCase.missing, permission)
				}
			}
			for _, fragment := range testCase.required {
				if !strings.Contains(focused, fragment) {
					t.Errorf("%s missing %q", testCase.name, fragment)
				}
			}
			for _, fragment := range testCase.forbidden {
				if strings.Contains(focused, fragment) {
					t.Errorf("%s unexpectedly owns %q", testCase.name, fragment)
				}
			}
		})
	}
}
