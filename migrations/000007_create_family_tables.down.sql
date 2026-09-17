DROP TABLE IF EXISTS family_members;
DROP TABLE IF EXISTS families;

DROP INDEX IF EXISTS ux_users_hermes_profile_id;
ALTER TABLE users
    DROP COLUMN IF EXISTS hermes_profile_id;
