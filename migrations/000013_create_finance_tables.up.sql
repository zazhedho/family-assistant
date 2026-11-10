-- Finance tracking. Money is whole rupiah (IDR) stored as BIGINT; balances and
-- reports are computed in SQL and never stored. Every table is Space scoped and
-- child rows reference accounts/categories through (space_id, id) composite
-- foreign keys, so a cross-Space reference is impossible at the database level.

CREATE TABLE IF NOT EXISTS finance_accounts (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    space_id UUID NOT NULL REFERENCES spaces(id) ON DELETE CASCADE,
    name VARCHAR(128) NOT NULL,
    type VARCHAR(32) NOT NULL,
    opening_balance BIGINT NOT NULL DEFAULT 0,
    archived_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    deleted_at TIMESTAMPTZ,
    CONSTRAINT uq_finance_accounts_space_id UNIQUE (space_id, id),
    CONSTRAINT ck_finance_accounts_name CHECK (length(btrim(name)) > 0),
    CONSTRAINT ck_finance_accounts_type CHECK (type IN ('cash', 'bank', 'ewallet', 'savings', 'credit_card')),
    CONSTRAINT ck_finance_accounts_opening_balance CHECK (opening_balance BETWEEN -1000000000000 AND 1000000000000)
);

CREATE INDEX IF NOT EXISTS ix_finance_accounts_space
    ON finance_accounts (space_id, created_at)
    WHERE deleted_at IS NULL;

-- Account names are unique per Space (case and padding insensitive) so that
-- resolving an account by name is never ambiguous.
CREATE UNIQUE INDEX IF NOT EXISTS uq_finance_accounts_space_name
    ON finance_accounts (space_id, lower(btrim(name)))
    WHERE deleted_at IS NULL;

CREATE TABLE IF NOT EXISTS finance_categories (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    space_id UUID NOT NULL REFERENCES spaces(id) ON DELETE CASCADE,
    name VARCHAR(128) NOT NULL,
    kind VARCHAR(16) NOT NULL,
    archived_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    deleted_at TIMESTAMPTZ,
    CONSTRAINT uq_finance_categories_space_id UNIQUE (space_id, id),
    CONSTRAINT ck_finance_categories_name CHECK (length(btrim(name)) > 0),
    CONSTRAINT ck_finance_categories_kind CHECK (kind IN ('income', 'expense'))
);

CREATE INDEX IF NOT EXISTS ix_finance_categories_space
    ON finance_categories (space_id, kind)
    WHERE deleted_at IS NULL;

CREATE UNIQUE INDEX IF NOT EXISTS uq_finance_categories_space_kind_name
    ON finance_categories (space_id, kind, lower(btrim(name)))
    WHERE deleted_at IS NULL;

CREATE TABLE IF NOT EXISTS finance_recurring (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    space_id UUID NOT NULL REFERENCES spaces(id) ON DELETE CASCADE,
    -- Transaction template.
    type VARCHAR(16) NOT NULL,
    amount BIGINT NOT NULL,
    account_id UUID NOT NULL,
    to_account_id UUID,
    category_id UUID,
    merchant VARCHAR(255),
    note TEXT,
    -- Schedule.
    frequency VARCHAR(16) NOT NULL,
    interval_n INTEGER NOT NULL DEFAULT 1,
    day_of_month INTEGER,
    start_at TIMESTAMPTZ NOT NULL,
    next_run_at TIMESTAMPTZ NOT NULL,
    end_at TIMESTAMPTZ,
    paused BOOLEAN NOT NULL DEFAULT FALSE,
    -- Why the scheduler paused the template (a fixed reason code; NULL when
    -- running or paused by a user), and its consecutive unclassified
    -- failures with the last sanitized failure code (an SQLSTATE or error
    -- class, never error text).
    pause_reason VARCHAR(32),
    failure_count INTEGER NOT NULL DEFAULT 0,
    last_failure_code VARCHAR(32),
    created_by_member_id UUID NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    deleted_at TIMESTAMPTZ,
    CONSTRAINT ck_finance_recurring_amount CHECK (amount > 0 AND amount <= 1000000000000),
    CONSTRAINT ck_finance_recurring_type CHECK (type IN ('income', 'expense', 'transfer')),
    CONSTRAINT ck_finance_recurring_transfer_shape CHECK (
        (type = 'transfer' AND to_account_id IS NOT NULL AND category_id IS NULL AND account_id <> to_account_id)
        OR (type <> 'transfer' AND to_account_id IS NULL)
    ),
    CONSTRAINT ck_finance_recurring_frequency CHECK (frequency IN ('daily', 'weekly', 'monthly', 'yearly')),
    CONSTRAINT ck_finance_recurring_interval CHECK (interval_n BETWEEN 1 AND 365),
    CONSTRAINT ck_finance_recurring_day_of_month CHECK (day_of_month IS NULL OR day_of_month BETWEEN 1 AND 31),
    CONSTRAINT ck_finance_recurring_end_after_start CHECK (end_at IS NULL OR end_at >= start_at),
    CONSTRAINT ck_finance_recurring_failure_count CHECK (failure_count >= 0),
    CONSTRAINT fk_finance_recurring_account_space FOREIGN KEY (space_id, account_id)
        REFERENCES finance_accounts(space_id, id),
    CONSTRAINT fk_finance_recurring_to_account_space FOREIGN KEY (space_id, to_account_id)
        REFERENCES finance_accounts(space_id, id),
    CONSTRAINT fk_finance_recurring_category_space FOREIGN KEY (space_id, category_id)
        REFERENCES finance_categories(space_id, id),
    CONSTRAINT fk_finance_recurring_creator_space FOREIGN KEY (space_id, created_by_member_id)
        REFERENCES space_members(space_id, id)
);

-- The scheduler claims due rows with FOR UPDATE SKIP LOCKED, ordered by
-- failure_count first so templates that keep failing go last.
CREATE INDEX IF NOT EXISTS ix_finance_recurring_due
    ON finance_recurring (failure_count, next_run_at)
    WHERE deleted_at IS NULL AND paused = FALSE;

CREATE TABLE IF NOT EXISTS finance_transactions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    space_id UUID NOT NULL REFERENCES spaces(id) ON DELETE CASCADE,
    type VARCHAR(16) NOT NULL,
    amount BIGINT NOT NULL,
    account_id UUID NOT NULL,
    to_account_id UUID,
    category_id UUID,
    merchant VARCHAR(255),
    note TEXT,
    occurred_at TIMESTAMPTZ NOT NULL,
    created_by_member_id UUID NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    deleted_at TIMESTAMPTZ,
    -- The recurring template that posted this row (NULL for manual entries).
    recurring_id UUID,
    CONSTRAINT ck_finance_transactions_amount CHECK (amount > 0 AND amount <= 1000000000000),
    CONSTRAINT ck_finance_transactions_type CHECK (type IN ('income', 'expense', 'transfer')),
    -- A transfer moves money between two different accounts and has no category;
    -- income and expense touch exactly one account.
    CONSTRAINT ck_finance_transactions_transfer_shape CHECK (
        (type = 'transfer' AND to_account_id IS NOT NULL AND category_id IS NULL AND account_id <> to_account_id)
        OR (type <> 'transfer' AND to_account_id IS NULL)
    ),
    CONSTRAINT fk_finance_transactions_account_space FOREIGN KEY (space_id, account_id)
        REFERENCES finance_accounts(space_id, id),
    CONSTRAINT fk_finance_transactions_to_account_space FOREIGN KEY (space_id, to_account_id)
        REFERENCES finance_accounts(space_id, id),
    CONSTRAINT fk_finance_transactions_category_space FOREIGN KEY (space_id, category_id)
        REFERENCES finance_categories(space_id, id),
    CONSTRAINT fk_finance_transactions_creator_space FOREIGN KEY (space_id, created_by_member_id)
        REFERENCES space_members(space_id, id),
    CONSTRAINT fk_finance_transactions_recurring FOREIGN KEY (recurring_id)
        REFERENCES finance_recurring(id)
);

-- One posting per template occurrence, ever: no deleted_at filter, so a
-- soft-deleted posted occurrence is never posted again.
CREATE UNIQUE INDEX IF NOT EXISTS uq_finance_transactions_recurring_occurrence
    ON finance_transactions (recurring_id, occurred_at)
    WHERE recurring_id IS NOT NULL;

CREATE INDEX IF NOT EXISTS ix_finance_transactions_space_occurred
    ON finance_transactions (space_id, occurred_at DESC)
    WHERE deleted_at IS NULL;

CREATE INDEX IF NOT EXISTS ix_finance_transactions_space_account
    ON finance_transactions (space_id, account_id)
    WHERE deleted_at IS NULL;

CREATE INDEX IF NOT EXISTS ix_finance_transactions_space_to_account
    ON finance_transactions (space_id, to_account_id)
    WHERE deleted_at IS NULL AND to_account_id IS NOT NULL;

CREATE INDEX IF NOT EXISTS ix_finance_transactions_space_category
    ON finance_transactions (space_id, category_id)
    WHERE deleted_at IS NULL AND category_id IS NOT NULL;

CREATE TABLE IF NOT EXISTS finance_budgets (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    space_id UUID NOT NULL REFERENCES spaces(id) ON DELETE CASCADE,
    category_id UUID NOT NULL,
    -- First day of the calendar month (Asia/Jakarta).
    month DATE NOT NULL,
    amount BIGINT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    deleted_at TIMESTAMPTZ,
    CONSTRAINT ck_finance_budgets_amount CHECK (amount > 0 AND amount <= 1000000000000),
    CONSTRAINT ck_finance_budgets_month_first_day CHECK (EXTRACT(DAY FROM month) = 1),
    CONSTRAINT fk_finance_budgets_category_space FOREIGN KEY (space_id, category_id)
        REFERENCES finance_categories(space_id, id)
);

-- Partial so a soft-deleted budget can be set again for the same month.
CREATE UNIQUE INDEX IF NOT EXISTS uq_finance_budgets_space_category_month
    ON finance_budgets (space_id, category_id, month)
    WHERE deleted_at IS NULL;

INSERT INTO menu_items (id, name, display_name, path, icon, order_index, is_active)
VALUES
    ('22222222-2222-4222-8222-000000000006', 'finance', 'Finance', '/finance', 'bi-wallet2', 910, TRUE)
ON CONFLICT (name) DO UPDATE SET
    display_name = EXCLUDED.display_name,
    path = EXCLUDED.path,
    icon = EXCLUDED.icon,
    order_index = EXCLUDED.order_index,
    is_active = EXCLUDED.is_active,
    deleted_at = NULL,
    updated_at = CURRENT_TIMESTAMP;

INSERT INTO permissions (id, name, display_name, resource, action) VALUES
    ('33333333-3333-4333-8333-000000000029', 'list_finance', 'List Finance', 'finance', 'list'),
    ('33333333-3333-4333-8333-000000000030', 'create_finance', 'Create Finance', 'finance', 'create'),
    ('33333333-3333-4333-8333-000000000031', 'update_finance', 'Update Finance', 'finance', 'update'),
    ('33333333-3333-4333-8333-000000000032', 'delete_finance', 'Delete Finance', 'finance', 'delete'),
    ('33333333-3333-4333-8333-000000000033', 'manage_finance', 'Manage Finance', 'finance', 'manage')
ON CONFLICT (name) DO UPDATE SET
    display_name = EXCLUDED.display_name,
    resource = EXCLUDED.resource,
    action = EXCLUDED.action,
    deleted_at = NULL,
    updated_at = CURRENT_TIMESTAMP;

INSERT INTO role_permissions (id, role_id, permission_id)
SELECT md5('space-rbac:' || r.name || ':' || p.name)::uuid, r.id, p.id
FROM roles r
JOIN permissions p ON p.resource = 'finance' AND p.name IN ('list_finance', 'create_finance', 'update_finance', 'delete_finance', 'manage_finance') AND p.deleted_at IS NULL
WHERE r.name IN ('space_owner', 'space_admin') AND r.deleted_at IS NULL
ON CONFLICT DO NOTHING;

INSERT INTO role_permissions (id, role_id, permission_id)
SELECT md5('space-rbac:' || r.name || ':' || p.name)::uuid, r.id, p.id
FROM roles r
JOIN permissions p ON p.resource = 'finance' AND p.name IN ('list_finance', 'create_finance', 'update_finance') AND p.deleted_at IS NULL
WHERE r.name = 'space_member' AND r.deleted_at IS NULL
ON CONFLICT DO NOTHING;

INSERT INTO role_permissions (id, role_id, permission_id)
SELECT md5('space-rbac:' || r.name || ':' || p.name)::uuid, r.id, p.id
FROM roles r
JOIN permissions p ON p.resource = 'finance' AND p.name IN ('list_finance') AND p.deleted_at IS NULL
WHERE r.name = 'space_viewer' AND r.deleted_at IS NULL
ON CONFLICT DO NOTHING;
