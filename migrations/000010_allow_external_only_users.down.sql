UPDATE users
SET email = 'rollback+' || id::text || '@account.invalid'
WHERE email IS NULL;

UPDATE users
SET password = '!external-only:' || id::text
WHERE password IS NULL;

ALTER TABLE users
    ALTER COLUMN email SET NOT NULL,
    ALTER COLUMN password SET NOT NULL;
