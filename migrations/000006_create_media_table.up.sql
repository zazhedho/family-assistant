CREATE TABLE IF NOT EXISTS media (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    owner_user_id UUID NOT NULL,
    object_key TEXT NOT NULL UNIQUE,
    url TEXT NOT NULL,
    original_name TEXT NOT NULL,
    content_type VARCHAR(150) NOT NULL,
    size BIGINT NOT NULL CHECK (size > 0),
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    deleted_at TIMESTAMP,
    CONSTRAINT fk_media_owner
        FOREIGN KEY (owner_user_id) REFERENCES users(id) ON DELETE RESTRICT
);

CREATE INDEX IF NOT EXISTS idx_media_owner_user_id ON media(owner_user_id);
CREATE INDEX IF NOT EXISTS idx_media_deleted_at ON media(deleted_at);
