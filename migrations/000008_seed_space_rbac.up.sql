-- Space roles and module permissions are rendered through pkg/moduleseed.
INSERT INTO roles (id, name, display_name, description, is_system)
VALUES
    ('11111111-1111-4111-8111-000000000001', 'space_owner', 'Space Owner', 'Full access to a Space', TRUE),
    ('11111111-1111-4111-8111-000000000002', 'space_admin', 'Space Admin', 'Administrative access to a Space', TRUE),
    ('11111111-1111-4111-8111-000000000003', 'space_member', 'Space Member', 'Member access to a Space', TRUE),
    ('11111111-1111-4111-8111-000000000004', 'space_viewer', 'Space Viewer', 'Read-only access to a Space', TRUE)
ON CONFLICT (name) DO UPDATE SET
    display_name = EXCLUDED.display_name,
    description = EXCLUDED.description,
    is_system = EXCLUDED.is_system,
    deleted_at = NULL,
    updated_at = CURRENT_TIMESTAMP;

INSERT INTO menu_items (id, name, display_name, path, icon, order_index, is_active)
VALUES
    ('22222222-2222-4222-8222-000000000001', 'spaces', 'Spaces', '/spaces', 'bi-grid', 905, TRUE)
ON CONFLICT (name) DO UPDATE SET
    display_name = EXCLUDED.display_name,
    path = EXCLUDED.path,
    icon = EXCLUDED.icon,
    order_index = EXCLUDED.order_index,
    is_active = EXCLUDED.is_active,
    deleted_at = NULL,
    updated_at = CURRENT_TIMESTAMP;

INSERT INTO permissions (id, name, display_name, resource, action) VALUES
    ('33333333-3333-4333-8333-000000000001', 'list_spaces', 'List Spaces', 'spaces', 'list'),
    ('33333333-3333-4333-8333-000000000002', 'view_spaces', 'View Spaces', 'spaces', 'view'),
    ('33333333-3333-4333-8333-000000000003', 'create_spaces', 'Create Spaces', 'spaces', 'create')
ON CONFLICT (name) DO UPDATE SET
    display_name = EXCLUDED.display_name,
    resource = EXCLUDED.resource,
    action = EXCLUDED.action,
    deleted_at = NULL,
    updated_at = CURRENT_TIMESTAMP;

INSERT INTO menu_items (id, name, display_name, path, icon, order_index, is_active)
VALUES
    ('22222222-2222-4222-8222-000000000002', 'members', 'Members', '/members', 'bi-person-lines-fill', 906, TRUE)
ON CONFLICT (name) DO UPDATE SET
    display_name = EXCLUDED.display_name,
    path = EXCLUDED.path,
    icon = EXCLUDED.icon,
    order_index = EXCLUDED.order_index,
    is_active = EXCLUDED.is_active,
    deleted_at = NULL,
    updated_at = CURRENT_TIMESTAMP;

INSERT INTO permissions (id, name, display_name, resource, action) VALUES
    ('33333333-3333-4333-8333-000000000004', 'list_members', 'List Members', 'members', 'list'),
    ('33333333-3333-4333-8333-000000000005', 'view_members', 'View Members', 'members', 'view')
ON CONFLICT (name) DO UPDATE SET
    display_name = EXCLUDED.display_name,
    resource = EXCLUDED.resource,
    action = EXCLUDED.action,
    deleted_at = NULL,
    updated_at = CURRENT_TIMESTAMP;

INSERT INTO menu_items (id, name, display_name, path, icon, order_index, is_active)
VALUES
    ('22222222-2222-4222-8222-000000000003', 'invitations', 'Invitations', '/invitations', 'bi-envelope', 907, TRUE)
ON CONFLICT (name) DO UPDATE SET
    display_name = EXCLUDED.display_name,
    path = EXCLUDED.path,
    icon = EXCLUDED.icon,
    order_index = EXCLUDED.order_index,
    is_active = EXCLUDED.is_active,
    deleted_at = NULL,
    updated_at = CURRENT_TIMESTAMP;

INSERT INTO permissions (id, name, display_name, resource, action) VALUES
    ('33333333-3333-4333-8333-000000000006', 'create_invitations', 'Create Invitations', 'invitations', 'create')
ON CONFLICT (name) DO UPDATE SET
    display_name = EXCLUDED.display_name,
    resource = EXCLUDED.resource,
    action = EXCLUDED.action,
    deleted_at = NULL,
    updated_at = CURRENT_TIMESTAMP;

INSERT INTO menu_items (id, name, display_name, path, icon, order_index, is_active)
VALUES
    ('22222222-2222-4222-8222-000000000004', 'reminders', 'Reminders', '/reminders', 'bi-bell', 908, TRUE)
ON CONFLICT (name) DO UPDATE SET
    display_name = EXCLUDED.display_name,
    path = EXCLUDED.path,
    icon = EXCLUDED.icon,
    order_index = EXCLUDED.order_index,
    is_active = EXCLUDED.is_active,
    deleted_at = NULL,
    updated_at = CURRENT_TIMESTAMP;

INSERT INTO permissions (id, name, display_name, resource, action) VALUES
    ('33333333-3333-4333-8333-000000000007', 'list_reminders', 'List Reminders', 'reminders', 'list'),
    ('33333333-3333-4333-8333-000000000008', 'view_reminders', 'View Reminders', 'reminders', 'view'),
    ('33333333-3333-4333-8333-000000000009', 'create_reminders', 'Create Reminders', 'reminders', 'create'),
    ('33333333-3333-4333-8333-000000000010', 'update_reminders', 'Update Reminders', 'reminders', 'update')
ON CONFLICT (name) DO UPDATE SET
    display_name = EXCLUDED.display_name,
    resource = EXCLUDED.resource,
    action = EXCLUDED.action,
    deleted_at = NULL,
    updated_at = CURRENT_TIMESTAMP;

INSERT INTO role_permissions (id, role_id, permission_id)
SELECT md5('space-rbac:' || r.name || ':' || p.name)::uuid, r.id, p.id
FROM roles r
JOIN permissions p ON p.resource = 'spaces' AND p.name IN ('list_spaces', 'view_spaces', 'create_spaces') AND p.deleted_at IS NULL
WHERE r.name IN ('space_owner') AND r.deleted_at IS NULL
ON CONFLICT DO NOTHING;

INSERT INTO role_permissions (id, role_id, permission_id)
SELECT md5('space-rbac:' || r.name || ':' || p.name)::uuid, r.id, p.id
FROM roles r
JOIN permissions p ON p.resource = 'members' AND p.name IN ('list_members', 'view_members') AND p.deleted_at IS NULL
WHERE r.name IN ('space_owner') AND r.deleted_at IS NULL
ON CONFLICT DO NOTHING;

INSERT INTO role_permissions (id, role_id, permission_id)
SELECT md5('space-rbac:' || r.name || ':' || p.name)::uuid, r.id, p.id
FROM roles r
JOIN permissions p ON p.resource = 'invitations' AND p.name IN ('create_invitations') AND p.deleted_at IS NULL
WHERE r.name IN ('space_owner') AND r.deleted_at IS NULL
ON CONFLICT DO NOTHING;

INSERT INTO role_permissions (id, role_id, permission_id)
SELECT md5('space-rbac:' || r.name || ':' || p.name)::uuid, r.id, p.id
FROM roles r
JOIN permissions p ON p.resource = 'reminders' AND p.name IN ('list_reminders', 'view_reminders', 'create_reminders', 'update_reminders') AND p.deleted_at IS NULL
WHERE r.name IN ('space_owner') AND r.deleted_at IS NULL
ON CONFLICT DO NOTHING;

INSERT INTO role_permissions (id, role_id, permission_id)
SELECT md5('space-rbac:' || r.name || ':' || p.name)::uuid, r.id, p.id
FROM roles r
JOIN permissions p ON p.resource = 'spaces' AND p.name IN ('list_spaces', 'view_spaces') AND p.deleted_at IS NULL
WHERE r.name IN ('space_admin') AND r.deleted_at IS NULL
ON CONFLICT DO NOTHING;

INSERT INTO role_permissions (id, role_id, permission_id)
SELECT md5('space-rbac:' || r.name || ':' || p.name)::uuid, r.id, p.id
FROM roles r
JOIN permissions p ON p.resource = 'members' AND p.name IN ('list_members', 'view_members') AND p.deleted_at IS NULL
WHERE r.name IN ('space_admin') AND r.deleted_at IS NULL
ON CONFLICT DO NOTHING;

INSERT INTO role_permissions (id, role_id, permission_id)
SELECT md5('space-rbac:' || r.name || ':' || p.name)::uuid, r.id, p.id
FROM roles r
JOIN permissions p ON p.resource = 'invitations' AND p.name IN ('create_invitations') AND p.deleted_at IS NULL
WHERE r.name IN ('space_admin') AND r.deleted_at IS NULL
ON CONFLICT DO NOTHING;

INSERT INTO role_permissions (id, role_id, permission_id)
SELECT md5('space-rbac:' || r.name || ':' || p.name)::uuid, r.id, p.id
FROM roles r
JOIN permissions p ON p.resource = 'reminders' AND p.name IN ('list_reminders', 'view_reminders', 'create_reminders', 'update_reminders') AND p.deleted_at IS NULL
WHERE r.name IN ('space_admin') AND r.deleted_at IS NULL
ON CONFLICT DO NOTHING;

INSERT INTO role_permissions (id, role_id, permission_id)
SELECT md5('space-rbac:' || r.name || ':' || p.name)::uuid, r.id, p.id
FROM roles r
JOIN permissions p ON p.resource = 'spaces' AND p.name IN ('list_spaces', 'view_spaces') AND p.deleted_at IS NULL
WHERE r.name IN ('space_member') AND r.deleted_at IS NULL
ON CONFLICT DO NOTHING;

INSERT INTO role_permissions (id, role_id, permission_id)
SELECT md5('space-rbac:' || r.name || ':' || p.name)::uuid, r.id, p.id
FROM roles r
JOIN permissions p ON p.resource = 'members' AND p.name IN ('list_members', 'view_members') AND p.deleted_at IS NULL
WHERE r.name IN ('space_member') AND r.deleted_at IS NULL
ON CONFLICT DO NOTHING;

INSERT INTO role_permissions (id, role_id, permission_id)
SELECT md5('space-rbac:' || r.name || ':' || p.name)::uuid, r.id, p.id
FROM roles r
JOIN permissions p ON p.resource = 'reminders' AND p.name IN ('list_reminders', 'view_reminders', 'create_reminders', 'update_reminders') AND p.deleted_at IS NULL
WHERE r.name IN ('space_member') AND r.deleted_at IS NULL
ON CONFLICT DO NOTHING;

INSERT INTO role_permissions (id, role_id, permission_id)
SELECT md5('space-rbac:' || r.name || ':' || p.name)::uuid, r.id, p.id
FROM roles r
JOIN permissions p ON p.resource = 'spaces' AND p.name IN ('list_spaces', 'view_spaces') AND p.deleted_at IS NULL
WHERE r.name IN ('space_viewer') AND r.deleted_at IS NULL
ON CONFLICT DO NOTHING;

INSERT INTO role_permissions (id, role_id, permission_id)
SELECT md5('space-rbac:' || r.name || ':' || p.name)::uuid, r.id, p.id
FROM roles r
JOIN permissions p ON p.resource = 'members' AND p.name IN ('list_members', 'view_members') AND p.deleted_at IS NULL
WHERE r.name IN ('space_viewer') AND r.deleted_at IS NULL
ON CONFLICT DO NOTHING;

INSERT INTO role_permissions (id, role_id, permission_id)
SELECT md5('space-rbac:' || r.name || ':' || p.name)::uuid, r.id, p.id
FROM roles r
JOIN permissions p ON p.resource = 'reminders' AND p.name IN ('list_reminders', 'view_reminders') AND p.deleted_at IS NULL
WHERE r.name IN ('space_viewer') AND r.deleted_at IS NULL
ON CONFLICT DO NOTHING;
