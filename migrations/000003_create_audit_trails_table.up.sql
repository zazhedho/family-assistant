CREATE TABLE IF NOT EXISTS audit_trails (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    occurred_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    actor_user_id UUID,
    actor_role VARCHAR(50),
    action VARCHAR(50) NOT NULL,
    resource VARCHAR(100) NOT NULL,
    resource_id VARCHAR(100),
    status VARCHAR(20) NOT NULL,
    message TEXT,
    error_message TEXT,
    request_id VARCHAR(100),
    ip_address VARCHAR(64),
    user_agent TEXT,
    before_data TEXT,
    after_data TEXT,
    metadata TEXT,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_audit_trails_occurred_at ON audit_trails(occurred_at);
CREATE INDEX IF NOT EXISTS idx_audit_trails_actor_user_id ON audit_trails(actor_user_id);
CREATE INDEX IF NOT EXISTS idx_audit_trails_action_resource ON audit_trails(action, resource);
