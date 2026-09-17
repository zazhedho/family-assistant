CREATE TABLE IF NOT EXISTS provinces (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    code VARCHAR(20) NOT NULL UNIQUE,
    name VARCHAR(255) NOT NULL,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP,
    deleted_at TIMESTAMP
);

CREATE TABLE IF NOT EXISTS cities (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    code VARCHAR(20) NOT NULL UNIQUE,
    province_code VARCHAR(20) NOT NULL,
    name VARCHAR(255) NOT NULL,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP,
    deleted_at TIMESTAMP
);

CREATE TABLE IF NOT EXISTS districts (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    code VARCHAR(20) NOT NULL UNIQUE,
    city_code VARCHAR(20) NOT NULL,
    name VARCHAR(255) NOT NULL,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP,
    deleted_at TIMESTAMP
);

CREATE TABLE IF NOT EXISTS villages (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    code VARCHAR(20) NOT NULL UNIQUE,
    district_code VARCHAR(20) NOT NULL,
    name VARCHAR(255) NOT NULL,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP,
    deleted_at TIMESTAMP
);

CREATE TABLE IF NOT EXISTS location_sync_jobs (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    status VARCHAR(20) NOT NULL,
    level VARCHAR(20) NOT NULL,
    year VARCHAR(10) NOT NULL,
    province_code VARCHAR(20),
    city_code VARCHAR(20),
    district_code VARCHAR(20),
    requested_by_user_id UUID,
    message TEXT,
    error_message TEXT,
    province_count INTEGER NOT NULL DEFAULT 0,
    city_count INTEGER NOT NULL DEFAULT 0,
    district_count INTEGER NOT NULL DEFAULT 0,
    village_count INTEGER NOT NULL DEFAULT 0,
    started_at TIMESTAMP,
    finished_at TIMESTAMP,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_provinces_deleted_at ON provinces(deleted_at);
CREATE INDEX IF NOT EXISTS idx_cities_province_code ON cities(province_code);
CREATE INDEX IF NOT EXISTS idx_cities_deleted_at ON cities(deleted_at);
CREATE INDEX IF NOT EXISTS idx_districts_city_code ON districts(city_code);
CREATE INDEX IF NOT EXISTS idx_districts_deleted_at ON districts(deleted_at);
CREATE INDEX IF NOT EXISTS idx_villages_district_code ON villages(district_code);
CREATE INDEX IF NOT EXISTS idx_villages_deleted_at ON villages(deleted_at);
CREATE INDEX IF NOT EXISTS idx_location_sync_jobs_status ON location_sync_jobs(status);
CREATE INDEX IF NOT EXISTS idx_location_sync_jobs_created_at ON location_sync_jobs(created_at DESC);

INSERT INTO permissions (id, name, display_name, resource, action) VALUES
    (gen_random_uuid(), 'sync_locations', 'Sync Locations', 'locations', 'sync')
ON CONFLICT (name) DO NOTHING;

INSERT INTO role_permissions (role_id, permission_id)
SELECT r.id, p.id
FROM roles r
JOIN permissions p ON p.resource = 'locations' AND p.action = 'sync'
WHERE r.name IN ('admin', 'superadmin')
ON CONFLICT DO NOTHING;
