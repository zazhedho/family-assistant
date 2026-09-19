ALTER TABLE users
    ADD COLUMN IF NOT EXISTS birth_date DATE,
    ADD COLUMN IF NOT EXISTS age_verification_method VARCHAR(32),
    ADD COLUMN IF NOT EXISTS age_verified_at TIMESTAMP;

CREATE TABLE IF NOT EXISTS spaces (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name VARCHAR(191) NOT NULL,
    type VARCHAR(16) NOT NULL,
    category VARCHAR(32) NOT NULL,
    status VARCHAR(32) NOT NULL DEFAULT 'ACTIVE',
    created_by_user_id UUID NOT NULL REFERENCES users(id),
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    deleted_at TIMESTAMP,
    CONSTRAINT ck_spaces_type CHECK (type IN ('PERSONAL','SHARED')),
    CONSTRAINT ck_spaces_category CHECK (category IN ('personal','family','friends','community','work','finance','custom')),
    CONSTRAINT ck_spaces_type_category CHECK (
        (type = 'PERSONAL' AND category = 'personal')
        OR (type = 'SHARED' AND category <> 'personal')
    ),
    CONSTRAINT ck_spaces_status CHECK (status IN ('ACTIVE','ARCHIVED'))
);

CREATE UNIQUE INDEX IF NOT EXISTS ux_spaces_personal_created_by
    ON spaces (created_by_user_id)
    WHERE type = 'PERSONAL' AND deleted_at IS NULL;

CREATE INDEX IF NOT EXISTS ix_spaces_created_by_status
    ON spaces (created_by_user_id, status)
    WHERE deleted_at IS NULL;

CREATE TABLE IF NOT EXISTS space_members (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    space_id UUID NOT NULL REFERENCES spaces(id) ON DELETE CASCADE,
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    role_id UUID NOT NULL REFERENCES roles(id),
    status VARCHAR(32) NOT NULL DEFAULT 'ACTIVE',
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    deleted_at TIMESTAMP,
    CONSTRAINT ck_space_members_status CHECK (status IN ('ACTIVE','INACTIVE')),
    CONSTRAINT uq_space_members_space_id_id UNIQUE (space_id, id)
);

CREATE UNIQUE INDEX IF NOT EXISTS ux_space_members_space_user
    ON space_members (space_id, user_id)
    WHERE deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS ix_space_members_user_status
    ON space_members (user_id, status)
    WHERE deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS ix_space_members_space_status
    ON space_members (space_id, status)
    WHERE deleted_at IS NULL;

CREATE TABLE IF NOT EXISTS space_invitations (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    space_id UUID NOT NULL REFERENCES spaces(id) ON DELETE CASCADE,
    invited_email VARCHAR(255),
    role_id UUID NOT NULL REFERENCES roles(id),
    invited_by_member_id UUID NOT NULL,
    token_hash VARCHAR(128) NOT NULL UNIQUE,
    status VARCHAR(32) NOT NULL DEFAULT 'PENDING',
    expires_at TIMESTAMPTZ NOT NULL,
    accepted_at TIMESTAMPTZ,
    accepted_by_user_id UUID,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    deleted_at TIMESTAMPTZ,
    CONSTRAINT ck_space_invitations_status CHECK (status IN ('PENDING','ACCEPTED','EXPIRED','REVOKED')),
    CONSTRAINT fk_space_invitations_acceptor FOREIGN KEY (accepted_by_user_id)
        REFERENCES users(id),
    CONSTRAINT fk_space_invitations_inviter_space FOREIGN KEY (space_id, invited_by_member_id)
        REFERENCES space_members(space_id, id)
);

CREATE INDEX IF NOT EXISTS ix_space_invitations_space_status
    ON space_invitations (space_id, status)
    WHERE deleted_at IS NULL;

CREATE TABLE IF NOT EXISTS external_identities (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    provider VARCHAR(64) NOT NULL,
    external_id VARCHAR(255) NOT NULL,
    status VARCHAR(32) NOT NULL DEFAULT 'ACTIVE',
    verified_at TIMESTAMPTZ,
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    deleted_at TIMESTAMPTZ,
    CONSTRAINT ck_external_identities_status CHECK (status IN ('ACTIVE','REVOKED'))
);

CREATE UNIQUE INDEX IF NOT EXISTS ux_external_identities_active_provider_id
    ON external_identities (provider, external_id)
    WHERE status = 'ACTIVE' AND deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS ix_external_identities_user_status
    ON external_identities (user_id, status)
    WHERE deleted_at IS NULL;

CREATE TABLE IF NOT EXISTS identity_link_tokens (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    provider VARCHAR(64) NOT NULL,
    token_hash VARCHAR(128) NOT NULL UNIQUE,
    expires_at TIMESTAMPTZ NOT NULL,
    consumed_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    deleted_at TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS ix_identity_link_tokens_user_expires
    ON identity_link_tokens (user_id, expires_at)
    WHERE deleted_at IS NULL;
