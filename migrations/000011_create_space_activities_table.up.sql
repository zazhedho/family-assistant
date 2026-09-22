CREATE TABLE IF NOT EXISTS space_activities (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    space_id UUID NOT NULL REFERENCES spaces(id) ON DELETE CASCADE,
    created_by_member_id UUID NOT NULL,
    kind VARCHAR(64) NOT NULL,
    note TEXT NOT NULL,
    occurred_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    deleted_at TIMESTAMPTZ,
    CONSTRAINT fk_space_activities_creator_space FOREIGN KEY (space_id, created_by_member_id)
        REFERENCES space_members(space_id, id)
);

CREATE INDEX IF NOT EXISTS ix_space_activities_space_kind_occurred
    ON space_activities (space_id, kind, occurred_at DESC)
    WHERE deleted_at IS NULL;

INSERT INTO menu_items (id, name, display_name, path, icon, order_index, is_active)
VALUES ('22222222-2222-4222-8222-000000000005', 'activities', 'Activities', '/activities', 'bi-journal-text', 909, TRUE)
ON CONFLICT (name) DO UPDATE SET
    display_name = EXCLUDED.display_name,
    path = EXCLUDED.path,
    icon = EXCLUDED.icon,
    order_index = EXCLUDED.order_index,
    is_active = EXCLUDED.is_active,
    deleted_at = NULL,
    updated_at = CURRENT_TIMESTAMP;

INSERT INTO permissions (id, name, display_name, resource, action) VALUES
    ('33333333-3333-4333-8333-000000000011', 'list_activities', 'List Activities', 'activities', 'list'),
    ('33333333-3333-4333-8333-000000000012', 'create_activities', 'Create Activities', 'activities', 'create')
ON CONFLICT (name) DO UPDATE SET
    display_name = EXCLUDED.display_name,
    resource = EXCLUDED.resource,
    action = EXCLUDED.action,
    deleted_at = NULL,
    updated_at = CURRENT_TIMESTAMP;

INSERT INTO role_permissions (id, role_id, permission_id)
SELECT md5('space-rbac:' || r.name || ':' || p.name)::uuid, r.id, p.id
FROM roles r
JOIN permissions p ON p.resource = 'activities' AND p.name IN ('list_activities', 'create_activities') AND p.deleted_at IS NULL
WHERE r.name IN ('space_owner', 'space_admin', 'space_member') AND r.deleted_at IS NULL
ON CONFLICT DO NOTHING;

INSERT INTO role_permissions (id, role_id, permission_id)
SELECT md5('space-rbac:' || r.name || ':' || p.name)::uuid, r.id, p.id
FROM roles r
JOIN permissions p ON p.resource = 'activities' AND p.name IN ('list_activities') AND p.deleted_at IS NULL
WHERE r.name IN ('space_viewer') AND r.deleted_at IS NULL
ON CONFLICT DO NOTHING;
