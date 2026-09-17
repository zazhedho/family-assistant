-- Create roles table
CREATE TABLE IF NOT EXISTS roles (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name VARCHAR(50) UNIQUE NOT NULL,
    display_name VARCHAR(100) NOT NULL,
    description TEXT,
    is_system BOOLEAN DEFAULT FALSE,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP,
    deleted_at TIMESTAMP
);

-- Create permissions table
CREATE TABLE IF NOT EXISTS permissions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name VARCHAR(100) UNIQUE NOT NULL,
    display_name VARCHAR(150) NOT NULL,
    description TEXT,
    resource VARCHAR(50) NOT NULL,
    action VARCHAR(50) NOT NULL,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP,
    deleted_at TIMESTAMP
);

-- Create menu_items table
CREATE TABLE IF NOT EXISTS menu_items (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name VARCHAR(50) UNIQUE NOT NULL,
    display_name VARCHAR(100) NOT NULL,
    path VARCHAR(255) NOT NULL,
    icon VARCHAR(50),
    parent_id UUID,
    order_index INT DEFAULT 0,
    is_active BOOLEAN DEFAULT TRUE,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP,
    deleted_at TIMESTAMP,
    FOREIGN KEY (parent_id) REFERENCES menu_items(id) ON DELETE SET NULL
);

-- Create role_permissions junction table
CREATE TABLE IF NOT EXISTS role_permissions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    role_id UUID NOT NULL,
    permission_id UUID NOT NULL,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (role_id) REFERENCES roles(id) ON DELETE CASCADE,
    FOREIGN KEY (permission_id) REFERENCES permissions(id) ON DELETE CASCADE,
    UNIQUE(role_id, permission_id)
);

-- Create role_menus junction table
CREATE TABLE IF NOT EXISTS role_menus (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    role_id UUID NOT NULL,
    menu_item_id UUID NOT NULL,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (role_id) REFERENCES roles(id) ON DELETE CASCADE,
    FOREIGN KEY (menu_item_id) REFERENCES menu_items(id) ON DELETE CASCADE,
    UNIQUE(role_id, menu_item_id)
);

-- Create indexes
CREATE INDEX IF NOT EXISTS idx_roles_name ON roles(name);
CREATE INDEX IF NOT EXISTS idx_permissions_resource_action ON permissions(resource, action);
CREATE INDEX IF NOT EXISTS idx_menu_items_parent_id ON menu_items(parent_id);
CREATE INDEX IF NOT EXISTS idx_menu_items_order ON menu_items(order_index);
CREATE INDEX IF NOT EXISTS idx_role_permissions_role_id ON role_permissions(role_id);
CREATE INDEX IF NOT EXISTS idx_role_permissions_permission_id ON role_permissions(permission_id);
CREATE INDEX IF NOT EXISTS idx_role_menus_role_id ON role_menus(role_id);
CREATE INDEX IF NOT EXISTS idx_role_menus_menu_item_id ON role_menus(menu_item_id);

-- Add foreign key to users table
DO $$
BEGIN
  IF NOT EXISTS (
    SELECT 1
    FROM pg_constraint
    WHERE conname = 'fk_users_role'
      AND conrelid = 'users'::regclass
  ) THEN
    ALTER TABLE users
    ADD CONSTRAINT fk_users_role
    FOREIGN KEY (role_id) REFERENCES roles(id) ON DELETE SET NULL;
  END IF;
END$$;
CREATE INDEX IF NOT EXISTS idx_users_role_id ON users(role_id);

-- Insert system roles
INSERT INTO roles (id, name, display_name, description, is_system) VALUES
    (gen_random_uuid(), 'superadmin', 'Super Administrator', 'Full system access with highest privileges', TRUE),
    (gen_random_uuid(), 'admin', 'Administrator', 'Full system access', TRUE),
    (gen_random_uuid(), 'staff', 'Staff', 'Staff access with limited permissions', TRUE),
    (gen_random_uuid(), 'viewer', 'Viewer', 'Read-only access', TRUE)
ON CONFLICT (name) DO NOTHING;

-- Insert menu items
INSERT INTO menu_items (id, name, display_name, path, icon, order_index) VALUES
    (gen_random_uuid(), 'dashboard', 'Dashboard', '/dashboard', 'bi-speedometer2', 1),
    (gen_random_uuid(), 'profile', 'Profile', '/profile', 'bi-person-circle', 2),
    (gen_random_uuid(), 'users', 'Users', '/users', 'bi-people', 900),
    (gen_random_uuid(), 'roles', 'Roles', '/roles', 'bi-shield-lock', 901),
    (gen_random_uuid(), 'menus', 'Menus', '/menus', 'bi-list-ul', 902),
    (gen_random_uuid(), 'audits', 'Audit Trails', '/audits', 'bi-journal-text', 904)
ON CONFLICT (name) DO NOTHING;

-- Insert permissions
INSERT INTO permissions (id, name, display_name, resource, action) VALUES
    -- Dashboard permissions
    (gen_random_uuid(), 'view_dashboard', 'View Dashboard', 'dashboard', 'view'),

    -- User permissions
    (gen_random_uuid(), 'list_users', 'List Users', 'users', 'list'),
    (gen_random_uuid(), 'view_users', 'View User Detail', 'users', 'view'),
    (gen_random_uuid(), 'create_users', 'Create Users', 'users', 'create'),
    (gen_random_uuid(), 'impersonate_users', 'Impersonate Users', 'users', 'impersonate'),
    (gen_random_uuid(), 'update_users', 'Update Users', 'users', 'update'),
    (gen_random_uuid(), 'assign_role_users', 'Assign User Roles', 'users', 'assign_role'),
    (gen_random_uuid(), 'update_password_users', 'Update Password Users', 'users', 'update_password'),
    (gen_random_uuid(), 'delete_users', 'Delete Users', 'users', 'delete'),

    -- Role permissions
    (gen_random_uuid(), 'list_roles', 'List Roles', 'roles', 'list'),
    (gen_random_uuid(), 'view_roles', 'View Role Detail', 'roles', 'view'),
    (gen_random_uuid(), 'create_roles', 'Create Roles', 'roles', 'create'),
    (gen_random_uuid(), 'update_roles', 'Update Roles', 'roles', 'update'),
    (gen_random_uuid(), 'delete_roles', 'Delete Roles', 'roles', 'delete'),
    (gen_random_uuid(), 'manage_system_roles', 'Manage System Roles', 'roles', 'manage_system'),
    (gen_random_uuid(), 'assign_permissions', 'Assign Permissions', 'roles', 'assign_permissions'),

    -- Menu permissions
    (gen_random_uuid(), 'list_menus', 'List Menus', 'menus', 'list'),
    (gen_random_uuid(), 'view_menu', 'View Menu Detail', 'menus', 'view'),
    (gen_random_uuid(), 'update_menu', 'Update Menu', 'menus', 'update'),

    -- Permission management permissions
    (gen_random_uuid(), 'list_permissions', 'List Permissions', 'permissions', 'list'),
    (gen_random_uuid(), 'view_permissions', 'View Permission Detail', 'permissions', 'view'),
    (gen_random_uuid(), 'create_permissions', 'Create Permissions', 'permissions', 'create'),
    (gen_random_uuid(), 'update_permissions', 'Update Permissions', 'permissions', 'update'),
    (gen_random_uuid(), 'delete_permissions', 'Delete Permissions', 'permissions', 'delete'),

    -- Audit permissions
    (gen_random_uuid(), 'list_audits', 'List Audit Trails', 'audits', 'list'),
    (gen_random_uuid(), 'view_audits', 'View Audit Trail Detail', 'audits', 'view'),

    -- Profile permissions
    (gen_random_uuid(), 'view_profile', 'View Profile', 'profile', 'view'),
    (gen_random_uuid(), 'update_profile', 'Update Profile', 'profile', 'update'),
    (gen_random_uuid(), 'update_password_profile', 'Update Password Profile', 'profile', 'update_password'),
    (gen_random_uuid(), 'delete_profile', 'Delete Profile', 'profile', 'delete')
ON CONFLICT (name) DO NOTHING;

-- Assign all permissions to admin role
INSERT INTO role_permissions (role_id, permission_id)
SELECT r.id, p.id
FROM roles r
CROSS JOIN permissions p
WHERE r.name = 'admin'
ON CONFLICT DO NOTHING;

-- Assign read and write permissions to staff role
INSERT INTO role_permissions (role_id, permission_id)
SELECT r.id, p.id
FROM roles r
CROSS JOIN permissions p
WHERE r.name = 'staff'
AND p.action IN ('list', 'view', 'create', 'update')
AND p.resource NOT IN ('users', 'roles', 'permissions', 'audits')
ON CONFLICT DO NOTHING;

-- Assign view profile permission to staff
INSERT INTO role_permissions (role_id, permission_id)
SELECT r.id, p.id
FROM roles r
CROSS JOIN permissions p
WHERE r.name = 'staff'
AND p.name IN ('view_profile', 'update_profile', 'view_dashboard')
ON CONFLICT DO NOTHING;

-- Assign only view permissions to viewer role
INSERT INTO role_permissions (role_id, permission_id)
SELECT r.id, p.id
FROM roles r
CROSS JOIN permissions p
WHERE r.name = 'viewer'
AND p.action IN ('list', 'view')
AND p.resource NOT IN ('users', 'roles', 'permissions', 'audits')
ON CONFLICT DO NOTHING;

-- Assign view profile permission to viewer
INSERT INTO role_permissions (role_id, permission_id)
SELECT r.id, p.id
FROM roles r
CROSS JOIN permissions p
WHERE r.name = 'viewer'
AND p.name IN ('view_profile', 'view_dashboard')
ON CONFLICT DO NOTHING;

-- Assign all permissions to superadmin role
INSERT INTO role_permissions (role_id, permission_id)
SELECT r.id, p.id
FROM roles r
CROSS JOIN permissions p
WHERE r.name = 'superadmin'
ON CONFLICT DO NOTHING;
