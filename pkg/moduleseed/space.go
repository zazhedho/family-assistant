package moduleseed

import (
	"fmt"
	"strings"
)

var spaceGrants = map[string]map[string][]string{
	"space_owner": {
		"spaces":      {"list", "view", "create"},
		"members":     {"list", "view"},
		"invitations": {"create"},
		"reminders":   {"list", "view", "create", "update"},
		"activities":  {"list", "create"},
	},
	"space_admin": {
		"spaces":      {"list", "view"},
		"members":     {"list", "view"},
		"invitations": {"create"},
		"reminders":   {"list", "view", "create", "update"},
		"activities":  {"list", "create"},
	},
	"space_member": {
		"spaces":     {"list", "view"},
		"members":    {"list", "view"},
		"reminders":  {"list", "view", "create", "update"},
		"activities": {"list", "create"},
	},
	"space_viewer": {
		"spaces":     {"list", "view"},
		"members":    {"list", "view"},
		"reminders":  {"list", "view"},
		"activities": {"list"},
	},
}

var spaceRoleOrder = []string{"space_owner", "space_admin", "space_member", "space_viewer"}
var spaceResourceOrder = []string{"spaces", "members", "invitations", "reminders", "activities"}

type spaceRole struct {
	ID, Name, DisplayName, Description string
}

var spaceRoles = []spaceRole{
	{ID: "11111111-1111-4111-8111-000000000001", Name: "space_owner", DisplayName: "Space Owner", Description: "Full access to a Space"},
	{ID: "11111111-1111-4111-8111-000000000002", Name: "space_admin", DisplayName: "Space Admin", Description: "Administrative access to a Space"},
	{ID: "11111111-1111-4111-8111-000000000003", Name: "space_member", DisplayName: "Space Member", Description: "Member access to a Space"},
	{ID: "11111111-1111-4111-8111-000000000004", Name: "space_viewer", DisplayName: "Space Viewer", Description: "Read-only access to a Space"},
}

type spaceModule struct {
	ID, Name, DisplayName, Path, Icon string
	OrderIndex                        int
	Actions                           []string
	PermissionIDs                     map[string]string
}

var spaceModules = []spaceModule{
	{
		ID: "22222222-2222-4222-8222-000000000001", Name: "spaces", DisplayName: "Spaces", Path: "/spaces", Icon: "bi-grid", OrderIndex: 905,
		Actions: []string{"list", "view", "create"},
		PermissionIDs: map[string]string{
			"list":   "33333333-3333-4333-8333-000000000001",
			"view":   "33333333-3333-4333-8333-000000000002",
			"create": "33333333-3333-4333-8333-000000000003",
		},
	},
	{
		ID: "22222222-2222-4222-8222-000000000002", Name: "members", DisplayName: "Members", Path: "/members", Icon: "bi-person-lines-fill", OrderIndex: 906,
		Actions: []string{"list", "view"},
		PermissionIDs: map[string]string{
			"list": "33333333-3333-4333-8333-000000000004",
			"view": "33333333-3333-4333-8333-000000000005",
		},
	},
	{
		ID: "22222222-2222-4222-8222-000000000003", Name: "invitations", DisplayName: "Invitations", Path: "/invitations", Icon: "bi-envelope", OrderIndex: 907,
		Actions: []string{"create"},
		PermissionIDs: map[string]string{
			"create": "33333333-3333-4333-8333-000000000006",
		},
	},
	{
		ID: "22222222-2222-4222-8222-000000000004", Name: "reminders", DisplayName: "Reminders", Path: "/reminders", Icon: "bi-bell", OrderIndex: 908,
		Actions: []string{"list", "view", "create", "update"},
		PermissionIDs: map[string]string{
			"list":   "33333333-3333-4333-8333-000000000007",
			"view":   "33333333-3333-4333-8333-000000000008",
			"create": "33333333-3333-4333-8333-000000000009",
			"update": "33333333-3333-4333-8333-000000000010",
		},
	},
	{
		ID: "22222222-2222-4222-8222-000000000005", Name: "activities", DisplayName: "Activities", Path: "/activities", Icon: "bi-journal-text", OrderIndex: 909,
		Actions: []string{"list", "create"},
		PermissionIDs: map[string]string{
			"list":   "33333333-3333-4333-8333-000000000011",
			"create": "33333333-3333-4333-8333-000000000012",
		},
	},
}

// RenderSpaceSQL returns the idempotent role, menu, permission, and grant seed
// for the Space modules. Seed-owned rows use stable IDs for exact rollback.
func RenderSpaceSQL() (string, error) {
	sections := []string{renderSpaceRoles()}
	for _, module := range spaceModules {
		sections = append(sections, renderSpaceMenu(module), renderSpacePermissions(module))
	}

	for _, roleName := range spaceRoleOrder {
		for _, resource := range spaceResourceOrder {
			actions := spaceGrants[roleName][resource]
			if len(actions) == 0 {
				continue
			}
			sections = append(sections, renderSpaceGrant(roleName, resource, actions))
		}
	}
	return strings.Join(sections, "\n\n"), nil
}

func renderSpaceRoles() string {
	values := make([]string, 0, len(spaceRoles))
	for _, role := range spaceRoles {
		values = append(values, fmt.Sprintf("    (%s, %s, %s, %s, TRUE)", quote(role.ID), quote(role.Name), quote(role.DisplayName), quote(role.Description)))
	}
	return fmt.Sprintf(`INSERT INTO roles (id, name, display_name, description, is_system)
VALUES
%s
ON CONFLICT (name) DO UPDATE SET
    display_name = EXCLUDED.display_name,
    description = EXCLUDED.description,
    is_system = EXCLUDED.is_system,
    deleted_at = NULL,
    updated_at = CURRENT_TIMESTAMP;`, strings.Join(values, ",\n"))
}

func renderSpaceMenu(module spaceModule) string {
	return fmt.Sprintf(`INSERT INTO menu_items (id, name, display_name, path, icon, order_index, is_active)
VALUES
    (%s, %s, %s, %s, %s, %d, TRUE)
ON CONFLICT (name) DO UPDATE SET
    display_name = EXCLUDED.display_name,
    path = EXCLUDED.path,
    icon = EXCLUDED.icon,
    order_index = EXCLUDED.order_index,
    is_active = EXCLUDED.is_active,
    deleted_at = NULL,
    updated_at = CURRENT_TIMESTAMP;`, quote(module.ID), quote(module.Name), quote(module.DisplayName), quote(module.Path), quote(module.Icon), module.OrderIndex)
}

func renderSpacePermissions(module spaceModule) string {
	values := make([]string, 0, len(module.Actions))
	for _, action := range module.Actions {
		values = append(values, fmt.Sprintf("    (%s, %s, %s, %s, %s)",
			quote(module.PermissionIDs[action]),
			quote(permissionName(action, module.Name)),
			quote(permissionDisplayName(action, module.DisplayName)),
			quote(module.Name),
			quote(action),
		))
	}
	return fmt.Sprintf(`INSERT INTO permissions (id, name, display_name, resource, action) VALUES
%s
ON CONFLICT (name) DO UPDATE SET
    display_name = EXCLUDED.display_name,
    resource = EXCLUDED.resource,
    action = EXCLUDED.action,
    deleted_at = NULL,
    updated_at = CURRENT_TIMESTAMP;`, strings.Join(values, ",\n"))
}

func renderSpaceGrant(roleName, resource string, actions []string) string {
	quotedNames := make([]string, 0, len(actions))
	for _, action := range actions {
		quotedNames = append(quotedNames, quote(permissionName(action, resource)))
	}
	return fmt.Sprintf(`INSERT INTO role_permissions (id, role_id, permission_id)
SELECT md5('space-rbac:' || r.name || ':' || p.name)::uuid, r.id, p.id
FROM roles r
JOIN permissions p ON p.resource = %s AND p.name IN (%s) AND p.deleted_at IS NULL
WHERE r.name IN (%s) AND r.deleted_at IS NULL
ON CONFLICT DO NOTHING;`, quote(resource), strings.Join(quotedNames, ", "), quote(roleName))
}
