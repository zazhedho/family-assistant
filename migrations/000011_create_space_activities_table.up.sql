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
    details JSONB NOT NULL DEFAULT '{}',
    subject VARCHAR(128),
    CONSTRAINT ck_space_activities_details_object CHECK (jsonb_typeof(details) = 'object'),
    CONSTRAINT ck_space_activities_details_size CHECK (octet_length(details::text) <= 4096),
    -- volume_ml must be positive; leftover_ml and expired_ml may be 0.
    CONSTRAINT ck_space_activities_feeding_details CHECK (
        CASE WHEN kind <> 'feeding' THEN TRUE ELSE (
            CASE
                WHEN details->'volume_ml' IS NULL THEN TRUE
                WHEN jsonb_typeof(details->'volume_ml') <> 'number' THEN FALSE
                ELSE (details->>'volume_ml')::numeric > 0
            END
            AND
            CASE
                WHEN details->'leftover_ml' IS NULL THEN TRUE
                WHEN jsonb_typeof(details->'leftover_ml') <> 'number' THEN FALSE
                ELSE (details->>'leftover_ml')::numeric >= 0
            END
            AND
            CASE
                WHEN details->'leftover_reported_at' IS NULL THEN TRUE
                ELSE jsonb_typeof(details->'leftover_reported_at') = 'string'
            END
            AND
            CASE
                WHEN details->'expired_ml' IS NULL THEN TRUE
                WHEN jsonb_typeof(details->'expired_ml') <> 'number' THEN FALSE
                ELSE (details->>'expired_ml')::numeric >= 0
            END
            AND
            CASE
                WHEN details->'outcome' IS NULL THEN TRUE
                WHEN jsonb_typeof(details->'outcome') <> 'string' THEN FALSE
                ELSE details->>'outcome' IN ('finished', 'expired')
            END
            AND
            CASE
                WHEN details->'leftover_ml' IS NULL OR details->'volume_ml' IS NULL THEN TRUE
                WHEN jsonb_typeof(details->'leftover_ml') <> 'number' OR jsonb_typeof(details->'volume_ml') <> 'number' THEN TRUE
                ELSE (details->>'leftover_ml')::numeric <= (details->>'volume_ml')::numeric
            END
            AND
            CASE
                WHEN details->'expired_ml' IS NULL OR details->'volume_ml' IS NULL THEN TRUE
                WHEN jsonb_typeof(details->'expired_ml') <> 'number' OR jsonb_typeof(details->'volume_ml') <> 'number' THEN TRUE
                ELSE (details->>'expired_ml')::numeric <= (details->>'volume_ml')::numeric
            END
            AND
            CASE
                WHEN details->>'outcome' = 'expired' THEN details->'expired_ml' IS NOT NULL
                ELSE TRUE
            END
        ) END
    ),
    CONSTRAINT fk_space_activities_creator_space FOREIGN KEY (space_id, created_by_member_id)
        REFERENCES space_members(space_id, id)
);

CREATE INDEX IF NOT EXISTS ix_space_activities_space_kind_occurred
    ON space_activities (space_id, kind, occurred_at DESC)
    WHERE deleted_at IS NULL;

INSERT INTO permissions (id, name, display_name, resource, action) VALUES
    ('33333333-3333-4333-8333-000000000020', 'update_activities', 'Update Activities', 'activities', 'update'),
    ('33333333-3333-4333-8333-000000000021', 'delete_activities', 'Delete Activities', 'activities', 'delete')
ON CONFLICT (name) DO UPDATE SET
    display_name = EXCLUDED.display_name,
    resource = EXCLUDED.resource,
    action = EXCLUDED.action,
    deleted_at = NULL,
    updated_at = CURRENT_TIMESTAMP;

INSERT INTO role_permissions (id, role_id, permission_id)
SELECT md5('space-rbac:' || r.name || ':' || p.name)::uuid, r.id, p.id
FROM roles r
JOIN permissions p ON p.resource = 'activities' AND p.name IN ('update_activities', 'delete_activities') AND p.deleted_at IS NULL
WHERE r.name IN ('space_owner', 'space_admin', 'space_member') AND r.deleted_at IS NULL
ON CONFLICT DO NOTHING;
