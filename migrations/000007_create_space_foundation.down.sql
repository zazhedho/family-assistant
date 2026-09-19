DROP TABLE IF EXISTS identity_link_tokens;
DROP TABLE IF EXISTS external_identities;
DROP TABLE IF EXISTS space_invitations;
DROP TABLE IF EXISTS space_members;
DROP TABLE IF EXISTS spaces;

ALTER TABLE users
    DROP COLUMN IF EXISTS age_verified_at,
    DROP COLUMN IF EXISTS age_verification_method,
    DROP COLUMN IF EXISTS birth_date;
