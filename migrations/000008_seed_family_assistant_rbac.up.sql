-- Family Assistant roles and module permissions are rendered through pkg/moduleseed.
INSERT INTO roles (id, name, display_name, description, is_system)
VALUES
    (gen_random_uuid(), 'parent', 'Parent', 'Family parent access', TRUE),
    (gen_random_uuid(), 'child', 'Child', 'Family child access', TRUE)
ON CONFLICT (name) DO UPDATE SET
    display_name = EXCLUDED.display_name,
    description = EXCLUDED.description,
    is_system = EXCLUDED.is_system,
    deleted_at = NULL,
    updated_at = CURRENT_TIMESTAMP;

INSERT INTO menu_items (id, name, display_name, path, icon, order_index, is_active)
VALUES
    (gen_random_uuid(), 'families', 'Families', '/families', 'bi-people', 905, TRUE)
ON CONFLICT (name) DO UPDATE SET
    display_name = EXCLUDED.display_name,
    path = EXCLUDED.path,
    icon = EXCLUDED.icon,
    order_index = EXCLUDED.order_index,
    is_active = EXCLUDED.is_active,
    deleted_at = NULL,
    updated_at = CURRENT_TIMESTAMP;

INSERT INTO permissions (id, name, display_name, resource, action) VALUES
    (gen_random_uuid(), 'list_families', 'List Families', 'families', 'list'),
    (gen_random_uuid(), 'view_families', 'View Families', 'families', 'view'),
    (gen_random_uuid(), 'update_families', 'Update Families', 'families', 'update')
ON CONFLICT (name) DO UPDATE SET
    display_name = EXCLUDED.display_name,
    resource = EXCLUDED.resource,
    action = EXCLUDED.action,
    deleted_at = NULL,
    updated_at = CURRENT_TIMESTAMP;

INSERT INTO role_permissions (role_id, permission_id)
SELECT r.id, p.id
FROM roles r
JOIN permissions p ON p.resource = 'families' AND p.name IN ('view_families') AND p.deleted_at IS NULL
WHERE r.name IN ('child') AND r.deleted_at IS NULL
ON CONFLICT DO NOTHING;

INSERT INTO role_permissions (role_id, permission_id)
SELECT r.id, p.id
FROM roles r
JOIN permissions p ON p.resource = 'families' AND p.name IN ('list_families', 'view_families', 'update_families') AND p.deleted_at IS NULL
WHERE r.name IN ('parent') AND r.deleted_at IS NULL
ON CONFLICT DO NOTHING;

INSERT INTO menu_items (id, name, display_name, path, icon, order_index, is_active)
VALUES
    (gen_random_uuid(), 'members', 'Members', '/members', 'bi-person-lines-fill', 906, TRUE)
ON CONFLICT (name) DO UPDATE SET
    display_name = EXCLUDED.display_name,
    path = EXCLUDED.path,
    icon = EXCLUDED.icon,
    order_index = EXCLUDED.order_index,
    is_active = EXCLUDED.is_active,
    deleted_at = NULL,
    updated_at = CURRENT_TIMESTAMP;

INSERT INTO permissions (id, name, display_name, resource, action) VALUES
    (gen_random_uuid(), 'list_members', 'List Members', 'members', 'list'),
    (gen_random_uuid(), 'view_members', 'View Members', 'members', 'view'),
    (gen_random_uuid(), 'manage_members', 'Manage Members', 'members', 'manage')
ON CONFLICT (name) DO UPDATE SET
    display_name = EXCLUDED.display_name,
    resource = EXCLUDED.resource,
    action = EXCLUDED.action,
    deleted_at = NULL,
    updated_at = CURRENT_TIMESTAMP;

INSERT INTO role_permissions (role_id, permission_id)
SELECT r.id, p.id
FROM roles r
JOIN permissions p ON p.resource = 'members' AND p.name IN ('list_members', 'view_members') AND p.deleted_at IS NULL
WHERE r.name IN ('child') AND r.deleted_at IS NULL
ON CONFLICT DO NOTHING;

INSERT INTO role_permissions (role_id, permission_id)
SELECT r.id, p.id
FROM roles r
JOIN permissions p ON p.resource = 'members' AND p.name IN ('list_members', 'view_members', 'manage_members') AND p.deleted_at IS NULL
WHERE r.name IN ('parent') AND r.deleted_at IS NULL
ON CONFLICT DO NOTHING;

INSERT INTO menu_items (id, name, display_name, path, icon, order_index, is_active)
VALUES
    (gen_random_uuid(), 'reminders', 'Reminders', '/reminders', 'bi-bell', 907, TRUE)
ON CONFLICT (name) DO UPDATE SET
    display_name = EXCLUDED.display_name,
    path = EXCLUDED.path,
    icon = EXCLUDED.icon,
    order_index = EXCLUDED.order_index,
    is_active = EXCLUDED.is_active,
    deleted_at = NULL,
    updated_at = CURRENT_TIMESTAMP;

INSERT INTO permissions (id, name, display_name, resource, action) VALUES
    (gen_random_uuid(), 'list_reminders', 'List Reminders', 'reminders', 'list'),
    (gen_random_uuid(), 'view_reminders', 'View Reminders', 'reminders', 'view'),
    (gen_random_uuid(), 'create_reminders', 'Create Reminders', 'reminders', 'create'),
    (gen_random_uuid(), 'update_reminders', 'Update Reminders', 'reminders', 'update'),
    (gen_random_uuid(), 'delete_reminders', 'Delete Reminders', 'reminders', 'delete')
ON CONFLICT (name) DO UPDATE SET
    display_name = EXCLUDED.display_name,
    resource = EXCLUDED.resource,
    action = EXCLUDED.action,
    deleted_at = NULL,
    updated_at = CURRENT_TIMESTAMP;

INSERT INTO role_permissions (role_id, permission_id)
SELECT r.id, p.id
FROM roles r
JOIN permissions p ON p.resource = 'reminders' AND p.name IN ('list_reminders', 'view_reminders', 'create_reminders', 'update_reminders', 'delete_reminders') AND p.deleted_at IS NULL
WHERE r.name IN ('child') AND r.deleted_at IS NULL
ON CONFLICT DO NOTHING;

INSERT INTO role_permissions (role_id, permission_id)
SELECT r.id, p.id
FROM roles r
JOIN permissions p ON p.resource = 'reminders' AND p.name IN ('list_reminders', 'view_reminders', 'create_reminders', 'update_reminders', 'delete_reminders') AND p.deleted_at IS NULL
WHERE r.name IN ('parent') AND r.deleted_at IS NULL
ON CONFLICT DO NOTHING;
