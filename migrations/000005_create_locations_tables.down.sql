DELETE FROM role_permissions
WHERE permission_id IN (
    SELECT id FROM permissions WHERE resource = 'locations'
);

DELETE FROM permissions WHERE resource = 'locations';

DROP TABLE IF EXISTS location_sync_jobs;
DROP TABLE IF EXISTS villages;
DROP TABLE IF EXISTS districts;
DROP TABLE IF EXISTS cities;
DROP TABLE IF EXISTS provinces;
