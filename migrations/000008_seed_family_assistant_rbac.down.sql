-- Intentionally non-destructive: this migration has no provenance marker for
-- distinguishing its rows from pre-existing/shared RBAC records.
-- Preserve roles, permissions, menus, and grants on rollback.
SELECT 1;
