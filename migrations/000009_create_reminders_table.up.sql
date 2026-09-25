CREATE TABLE IF NOT EXISTS reminders (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    space_id UUID NOT NULL REFERENCES spaces(id) ON DELETE CASCADE,
    created_by_member_id UUID NOT NULL,
    assignee_member_id UUID,
    title VARCHAR(255) NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    scheduled_at TIMESTAMPTZ NOT NULL,
    status VARCHAR(32) NOT NULL,
    delivery_provider VARCHAR(64) NOT NULL DEFAULT '',
    delivery_target VARCHAR(255) NOT NULL DEFAULT '',
    notification_claimed_at TIMESTAMPTZ NULL,
    notified_at TIMESTAMPTZ NULL,
    completed_at TIMESTAMPTZ NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    deleted_at TIMESTAMPTZ,
    CONSTRAINT ck_reminders_status CHECK (status IN ('PENDING','COMPLETED','CANCELLED')),
    CONSTRAINT fk_reminders_creator_space FOREIGN KEY (space_id, created_by_member_id)
        REFERENCES space_members(space_id, id),
    CONSTRAINT fk_reminders_assignee_space FOREIGN KEY (space_id, assignee_member_id)
        REFERENCES space_members(space_id, id)
);

CREATE INDEX IF NOT EXISTS ix_reminders_space_status_schedule
    ON reminders (space_id, status, scheduled_at);
CREATE INDEX IF NOT EXISTS ix_reminders_assignee_status_schedule
    ON reminders (assignee_member_id, status, scheduled_at);
CREATE INDEX IF NOT EXISTS ix_reminders_notification_due
    ON reminders (status, scheduled_at, notification_claimed_at)
    WHERE notified_at IS NULL AND deleted_at IS NULL;
