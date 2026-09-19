CREATE UNIQUE INDEX IF NOT EXISTS ux_family_members_family_id_id
    ON family_members (family_id, id);

CREATE TABLE IF NOT EXISTS reminders (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    family_id UUID NOT NULL REFERENCES families(id),
    owner_member_id UUID NOT NULL,
    created_by_member_id UUID NOT NULL,
    scope VARCHAR(16) NOT NULL,
    title VARCHAR(255) NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    scheduled_at TIMESTAMPTZ NOT NULL,
    status VARCHAR(32) NOT NULL,
    completed_at TIMESTAMPTZ NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT ck_reminders_scope CHECK (scope IN ('PERSONAL','FAMILY')),
    CONSTRAINT ck_reminders_status CHECK (status IN ('PENDING','COMPLETED','CANCELLED')),
    CONSTRAINT fk_reminders_owner_family FOREIGN KEY (family_id, owner_member_id)
        REFERENCES family_members(family_id, id),
    CONSTRAINT fk_reminders_creator_family FOREIGN KEY (family_id, created_by_member_id)
        REFERENCES family_members(family_id, id)
);

CREATE INDEX IF NOT EXISTS ix_reminders_family_status_schedule
    ON reminders (family_id, status, scheduled_at);
CREATE INDEX IF NOT EXISTS ix_reminders_owner_status_schedule
    ON reminders (owner_member_id, status, scheduled_at);
