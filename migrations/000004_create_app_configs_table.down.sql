DELETE FROM role_permissions
WHERE permission_id IN (
    SELECT id FROM permissions WHERE resource = 'configs'
);

DELETE FROM permissions WHERE resource = 'configs';
DELETE FROM menu_items WHERE name = 'configs';

DROP TABLE IF EXISTS app_configs;
