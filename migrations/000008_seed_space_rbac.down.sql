DO $$
BEGIN
    IF EXISTS (
        SELECT 1
        FROM space_members
        WHERE role_id IN (
            '11111111-1111-4111-8111-000000000001',
            '11111111-1111-4111-8111-000000000002',
            '11111111-1111-4111-8111-000000000003',
            '11111111-1111-4111-8111-000000000004'
        )
    ) OR EXISTS (
        SELECT 1
        FROM space_invitations
        WHERE role_id IN (
            '11111111-1111-4111-8111-000000000001',
            '11111111-1111-4111-8111-000000000002',
            '11111111-1111-4111-8111-000000000003',
            '11111111-1111-4111-8111-000000000004'
        )
    ) THEN
        RAISE EXCEPTION 'cannot roll back Space RBAC while seeded roles are referenced';
    END IF;
END$$;

DELETE FROM role_permissions rp
USING roles r, permissions p
WHERE rp.id = md5('space-rbac:' || r.name || ':' || p.name)::uuid
  AND rp.role_id = r.id
  AND rp.permission_id = p.id
  AND (
      (r.name = 'space_owner' AND p.name IN ('list_spaces', 'view_spaces', 'create_spaces', 'list_members', 'view_members', 'create_invitations', 'list_reminders', 'view_reminders', 'create_reminders', 'update_reminders', 'list_activities', 'create_activities'))
      OR (r.name = 'space_admin' AND p.name IN ('list_spaces', 'view_spaces', 'list_members', 'view_members', 'create_invitations', 'list_reminders', 'view_reminders', 'create_reminders', 'update_reminders', 'list_activities', 'create_activities'))
      OR (r.name = 'space_member' AND p.name IN ('list_spaces', 'view_spaces', 'list_members', 'view_members', 'list_reminders', 'view_reminders', 'create_reminders', 'update_reminders', 'list_activities', 'create_activities'))
      OR (r.name = 'space_viewer' AND p.name IN ('list_spaces', 'view_spaces', 'list_members', 'view_members', 'list_reminders', 'view_reminders', 'list_activities'))
  );

DELETE FROM permissions
WHERE id IN (
    '33333333-3333-4333-8333-000000000001',
    '33333333-3333-4333-8333-000000000002',
    '33333333-3333-4333-8333-000000000003',
    '33333333-3333-4333-8333-000000000004',
    '33333333-3333-4333-8333-000000000005',
    '33333333-3333-4333-8333-000000000006',
    '33333333-3333-4333-8333-000000000007',
    '33333333-3333-4333-8333-000000000008',
    '33333333-3333-4333-8333-000000000009',
    '33333333-3333-4333-8333-000000000010',
    '33333333-3333-4333-8333-000000000011',
    '33333333-3333-4333-8333-000000000012'
);

DELETE FROM menu_items
WHERE id IN (
    '22222222-2222-4222-8222-000000000001',
    '22222222-2222-4222-8222-000000000002',
    '22222222-2222-4222-8222-000000000003',
    '22222222-2222-4222-8222-000000000004',
    '22222222-2222-4222-8222-000000000005'
);

DELETE FROM roles
WHERE id IN (
    '11111111-1111-4111-8111-000000000001',
    '11111111-1111-4111-8111-000000000002',
    '11111111-1111-4111-8111-000000000003',
    '11111111-1111-4111-8111-000000000004'
);
