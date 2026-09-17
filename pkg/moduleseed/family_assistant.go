package moduleseed

import "strings"

// RenderFamilyAssistantSQL returns the idempotent role, menu, permission, and
// role-permission seed for the Family Assistant modules.
func RenderFamilyAssistantSQL() (string, error) {
	sections := []string{renderFamilyAssistantRoles()}
	for _, definition := range familyAssistantDefinitions() {
		sql, err := RenderSQL(definition)
		if err != nil {
			return "", err
		}
		sections = append(sections, sql)
	}

	return strings.Join(sections, "\n\n"), nil
}

func familyAssistantDefinitions() []Definition {
	return []Definition{
		{
			Name:        "families",
			DisplayName: "Families",
			Path:        "/families",
			Icon:        "bi-people",
			OrderIndex:  905,
			Actions:     []string{"list", "view", "update"},
			GrantRoleActions: map[string][]string{
				"child":  {"view"},
				"parent": {"list", "view", "update"},
			},
		},
		{
			Name:        "members",
			DisplayName: "Members",
			Path:        "/members",
			Icon:        "bi-person-lines-fill",
			OrderIndex:  906,
			Actions:     []string{"list", "view", "manage"},
			GrantRoleActions: map[string][]string{
				"child":  {"list", "view"},
				"parent": {"list", "view", "manage"},
			},
		},
		{
			Name:        "reminders",
			DisplayName: "Reminders",
			Path:        "/reminders",
			Icon:        "bi-bell",
			OrderIndex:  907,
			Actions:     []string{"list", "view", "create", "update", "delete"},
			GrantRoleActions: map[string][]string{
				"child":  {"list", "view", "create", "update", "delete"},
				"parent": {"list", "view", "create", "update", "delete"},
			},
		},
	}
}

func renderFamilyAssistantRoles() string {
	return `INSERT INTO roles (id, name, display_name, description, is_system)
VALUES
    (gen_random_uuid(), 'parent', 'Parent', 'Family parent access', TRUE),
    (gen_random_uuid(), 'child', 'Child', 'Family child access', TRUE)
ON CONFLICT (name) DO UPDATE SET
    display_name = EXCLUDED.display_name,
    description = EXCLUDED.description,
    is_system = EXCLUDED.is_system,
    deleted_at = NULL,
    updated_at = CURRENT_TIMESTAMP;`
}
