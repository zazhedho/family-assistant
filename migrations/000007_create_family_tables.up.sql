ALTER TABLE users
    ADD COLUMN IF NOT EXISTS hermes_profile_id VARCHAR(191);

CREATE UNIQUE INDEX IF NOT EXISTS ux_users_hermes_profile_id
    ON users (hermes_profile_id)
    WHERE hermes_profile_id IS NOT NULL;

CREATE TABLE IF NOT EXISTS families (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name VARCHAR(191) NOT NULL,
    status VARCHAR(32) NOT NULL,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS family_members (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    family_id UUID NOT NULL REFERENCES families(id),
    user_id UUID NOT NULL REFERENCES users(id),
    role_id UUID NOT NULL REFERENCES roles(id),
    status VARCHAR(32) NOT NULL,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT ux_family_members_family_user UNIQUE (family_id, user_id)
);

CREATE INDEX IF NOT EXISTS ix_family_members_user_status
    ON family_members (user_id, status);
CREATE INDEX IF NOT EXISTS ix_family_members_family_status
    ON family_members (family_id, status);
