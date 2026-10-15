CREATE TABLE IF NOT EXISTS space_channels (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    space_id UUID NOT NULL REFERENCES spaces(id) ON DELETE CASCADE,
    channel VARCHAR(32) NOT NULL,
    external_chat_id VARCHAR(255) NOT NULL,
    status VARCHAR(16) NOT NULL DEFAULT 'ACTIVE',
    bound_by_member_id UUID NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT ck_space_channels_channel CHECK (channel = 'whatsapp'),
    CONSTRAINT ck_space_channels_group_jid CHECK (external_chat_id ~ '^[0-9]*[1-9][0-9]*@g[.]us$'),
    CONSTRAINT ck_space_channels_status CHECK (status IN ('ACTIVE','INACTIVE')),
    CONSTRAINT uq_space_channels_space_id_id UNIQUE (space_id, id),
    CONSTRAINT fk_space_channels_binder_space FOREIGN KEY (space_id, bound_by_member_id)
        REFERENCES space_members(space_id, id)
);

CREATE UNIQUE INDEX IF NOT EXISTS ux_space_channels_active_chat
    ON space_channels (channel, external_chat_id)
    WHERE status = 'ACTIVE';
CREATE INDEX IF NOT EXISTS ix_space_channels_space_status
    ON space_channels (space_id, status);

CREATE TABLE IF NOT EXISTS scheduled_messages (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    space_id UUID NOT NULL REFERENCES spaces(id) ON DELETE CASCADE,
    created_by_member_id UUID NOT NULL,
    target VARCHAR(16) NOT NULL,
    recipient_member_id UUID,
    space_channel_id UUID,
    content_mode VARCHAR(16) NOT NULL,
    content TEXT NOT NULL,
    recurrence VARCHAR(16) NOT NULL,
    timezone VARCHAR(64) NOT NULL,
    start_at TIMESTAMPTZ NOT NULL,
    next_scheduled_at TIMESTAMPTZ,
    status VARCHAR(20) NOT NULL DEFAULT 'ACTIVE',
    unsubscribed_by_member_id UUID,
    unsubscribed_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT ck_scheduled_messages_target CHECK (target IN ('DM','GROUP')),
    CONSTRAINT ck_scheduled_messages_destination CHECK (
        (target = 'DM' AND recipient_member_id IS NOT NULL AND space_channel_id IS NULL)
        OR (target = 'GROUP' AND recipient_member_id IS NULL AND space_channel_id IS NOT NULL)
    ),
    CONSTRAINT ck_scheduled_messages_content_mode CHECK (content_mode IN ('FIXED','AI')),
    CONSTRAINT ck_scheduled_messages_content CHECK (length(btrim(content)) > 0),
    CONSTRAINT ck_scheduled_messages_recurrence CHECK (recurrence IN ('ONCE','DAILY','WEEKLY')),
    CONSTRAINT ck_scheduled_messages_timezone CHECK (length(btrim(timezone)) > 0),
    CONSTRAINT ck_scheduled_messages_status CHECK (status IN ('ACTIVE','SENT','FAILED','CANCELLED','UNSUBSCRIBED')),
    CONSTRAINT ck_scheduled_messages_next CHECK (status <> 'ACTIVE' OR next_scheduled_at IS NOT NULL),
    CONSTRAINT ck_scheduled_messages_unsubscribe CHECK (
        (status = 'UNSUBSCRIBED' AND target = 'DM' AND unsubscribed_by_member_id = recipient_member_id AND unsubscribed_at IS NOT NULL)
        OR (status <> 'UNSUBSCRIBED' AND unsubscribed_by_member_id IS NULL AND unsubscribed_at IS NULL)
    ),
    CONSTRAINT uq_scheduled_messages_space_id_id UNIQUE (space_id, id),
    CONSTRAINT fk_scheduled_messages_creator_space FOREIGN KEY (space_id, created_by_member_id)
        REFERENCES space_members(space_id, id),
    CONSTRAINT fk_scheduled_messages_recipient_space FOREIGN KEY (space_id, recipient_member_id)
        REFERENCES space_members(space_id, id),
    CONSTRAINT fk_scheduled_messages_channel_space FOREIGN KEY (space_id, space_channel_id)
        REFERENCES space_channels(space_id, id),
    CONSTRAINT fk_scheduled_messages_unsubscriber_space FOREIGN KEY (space_id, unsubscribed_by_member_id)
        REFERENCES space_members(space_id, id)
);

CREATE INDEX IF NOT EXISTS ix_scheduled_messages_space_status_next
    ON scheduled_messages (space_id, status, next_scheduled_at);
CREATE INDEX IF NOT EXISTS ix_scheduled_messages_creator
    ON scheduled_messages (space_id, created_by_member_id, status);

CREATE TABLE IF NOT EXISTS scheduled_message_deliveries (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    scheduled_message_id UUID NOT NULL REFERENCES scheduled_messages(id) ON DELETE CASCADE,
    scheduled_at TIMESTAMPTZ NOT NULL,
    content_snapshot TEXT,
    generation_attempts INTEGER NOT NULL DEFAULT 0,
    delivery_attempts INTEGER NOT NULL DEFAULT 0,
    next_attempt_at TIMESTAMPTZ,
    lease_token UUID,
    lease_until TIMESTAMPTZ,
    status VARCHAR(16) NOT NULL DEFAULT 'PENDING',
    failure_category VARCHAR(64),
    sent_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT uq_scheduled_message_deliveries_occurrence UNIQUE (scheduled_message_id, scheduled_at),
    CONSTRAINT ck_scheduled_message_deliveries_generation_attempts CHECK (generation_attempts >= 0),
    CONSTRAINT ck_scheduled_message_deliveries_delivery_attempts CHECK (delivery_attempts >= 0),
    CONSTRAINT ck_scheduled_message_deliveries_next_attempt CHECK (status <> 'PENDING' OR next_attempt_at IS NOT NULL),
    CONSTRAINT ck_scheduled_message_deliveries_lease CHECK (
        (lease_token IS NULL AND lease_until IS NULL) OR (lease_token IS NOT NULL AND lease_until IS NOT NULL)
    ),
    CONSTRAINT ck_scheduled_message_deliveries_status CHECK (status IN ('PENDING','SENT','SKIPPED','FAILED','CANCELLED')),
    CONSTRAINT ck_scheduled_message_deliveries_sent CHECK (
        (status = 'SENT' AND sent_at IS NOT NULL) OR (status <> 'SENT' AND sent_at IS NULL)
    )
);

CREATE INDEX IF NOT EXISTS ix_scheduled_message_deliveries_due
    ON scheduled_message_deliveries (next_attempt_at, id, lease_until)
    WHERE status = 'PENDING';
CREATE INDEX IF NOT EXISTS ix_scheduled_message_deliveries_history
    ON scheduled_message_deliveries (scheduled_message_id, scheduled_at DESC);

INSERT INTO permissions (id, name, display_name, resource, action) VALUES
    ('33333333-3333-4333-8333-000000000022', 'list_scheduled_messages', 'List Scheduled Messages', 'scheduled_messages', 'list'),
    ('33333333-3333-4333-8333-000000000023', 'view_scheduled_messages', 'View Scheduled Messages', 'scheduled_messages', 'view'),
    ('33333333-3333-4333-8333-000000000024', 'create_scheduled_messages', 'Create Scheduled Messages', 'scheduled_messages', 'create'),
    ('33333333-3333-4333-8333-000000000025', 'update_scheduled_messages', 'Update Scheduled Messages', 'scheduled_messages', 'update'),
    ('33333333-3333-4333-8333-000000000026', 'cancel_scheduled_messages', 'Cancel Scheduled Messages', 'scheduled_messages', 'delete'),
    ('33333333-3333-4333-8333-000000000027', 'unsubscribe_scheduled_messages', 'Unsubscribe Scheduled Messages', 'scheduled_messages', 'unsubscribe'),
    ('33333333-3333-4333-8333-000000000028', 'bind_space_channels', 'Bind Space Channels', 'space_channels', 'create')
ON CONFLICT (name) DO NOTHING;

INSERT INTO role_permissions (id, role_id, permission_id)
SELECT md5('space-rbac:' || r.name || ':' || p.name)::uuid, r.id, p.id
FROM roles r
JOIN permissions p ON p.name IN (
    'list_scheduled_messages', 'view_scheduled_messages', 'create_scheduled_messages',
    'update_scheduled_messages', 'cancel_scheduled_messages',
    'unsubscribe_scheduled_messages', 'bind_space_channels'
) AND p.deleted_at IS NULL
WHERE r.name IN ('space_owner', 'space_admin') AND r.deleted_at IS NULL
ON CONFLICT DO NOTHING;

INSERT INTO role_permissions (id, role_id, permission_id)
SELECT md5('space-rbac:' || r.name || ':' || p.name)::uuid, r.id, p.id
FROM roles r
JOIN permissions p ON p.name IN (
    'list_scheduled_messages', 'view_scheduled_messages', 'create_scheduled_messages',
    'update_scheduled_messages', 'cancel_scheduled_messages', 'unsubscribe_scheduled_messages'
) AND p.deleted_at IS NULL
WHERE r.name = 'space_member' AND r.deleted_at IS NULL
ON CONFLICT DO NOTHING;

INSERT INTO role_permissions (id, role_id, permission_id)
SELECT md5('space-rbac:' || r.name || ':' || p.name)::uuid, r.id, p.id
FROM roles r
JOIN permissions p ON p.name IN ('list_scheduled_messages', 'view_scheduled_messages', 'unsubscribe_scheduled_messages')
    AND p.deleted_at IS NULL
WHERE r.name = 'space_viewer' AND r.deleted_at IS NULL
ON CONFLICT DO NOTHING;
