CREATE TABLE IF NOT EXISTS reminders (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    space_id UUID NOT NULL REFERENCES spaces(id) ON DELETE CASCADE,
    created_by_member_id UUID NOT NULL,
    assignee_member_id UUID,
    title VARCHAR(255) NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    scheduled_at TIMESTAMPTZ NOT NULL,
    status VARCHAR(32) NOT NULL,
    completed_at TIMESTAMPTZ NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
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
