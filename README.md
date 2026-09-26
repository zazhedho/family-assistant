# Family Assistant

Backend service for a private assistant with personal and shared Spaces:
- Gin HTTP router
- PostgreSQL via GORM
- JWT authentication
- permission-first RBAC
- runtime application configurations from database
- optional Redis-based session management and rate limiting

This repository is intended to be the foundation for future projects. The current structure is generic on purpose and should be extended by adding new business modules on top of the existing patterns.

## Core Principles

### Permission-First RBAC

RBAC in this starter kit is designed with these rules:
- `permission` is the runtime source of truth for access control
- `role` is a label and a grouping mechanism for permissions
- `superadmin` is the only exception and bypasses permission checks
- menu visibility is derived from permissions, not from manual menu assignment

Practical implications:
- endpoint access is checked by `PermissionMiddleware(resource, action)`
- `/api/menus/me` is built from the permissions owned by the current user
- if a role has at least one permission for a module resource, the menu for that module can appear automatically
- parent menus are included automatically when a permitted child menu exists

### Runtime Configuration

Application configuration values can be stored in `app_configs` and changed without restarting the service.

Use this for values such as:
- external URLs
- feature toggles
- integration settings
- module-specific runtime configuration

Built-in auth feature flags:
- `auth.public_registration_enabled`: enable or disable public self-registration endpoints
- `auth.register_otp_enabled`: require OTP verification for public registration
- `auth.password_reset_email_enabled`: send password reset tokens through the email sender instead of returning a development token in the API response

The starter kit now includes a typed helper on top of `app_configs`, so services do not need to parse raw strings manually for common cases such as:
- `GetString`
- `GetBool`
- `GetInt`
- `GetDuration`
- `IsEnabled`
- `DecodeJSON`

Behavior:
- if a config key does not exist, the helper returns the provided fallback
- if a config exists but `is_active = false`, the helper also returns the fallback
- parsing errors are returned only when an active config exists but contains an invalid value

For feature flags, `is_active` controls whether the stored config overrides the code fallback. The actual on/off value is stored in `value`.

Public registration example:
- missing config: allowed, because code fallback is `true`
- `is_active = false`: allowed, because the config is ignored and fallback `true` is used
- `is_active = true`, `value = true`: allowed
- `is_active = true`, `value = false`: disabled

Default auth config rows are seeded by the existing app config migration:
- `auth.public_registration_enabled`: active, value `true`
- `auth.register_otp_enabled`: active, value `false`
- `auth.password_reset_email_enabled`: active, value `false`

## Current Modules

System modules currently included:
- Authentication and user profile
- Users
- Roles
- Permissions
- Menus
- Configurations
- Locations
- Sessions when Redis is enabled
- Media upload and owner-controlled deletion when `MEDIA_ENABLED=true`

## Project Structure

Main backend layout:

```text
family-assistant/
├── infrastructure/
├── internal/
│   ├── domain/
│   ├── dto/
│   ├── handlers/http/
│   ├── interfaces/
│   ├── repositories/
│   ├── router/
│   └── services/
├── middlewares/
├── migrations/
├── pkg/
├── utils/
└── main.go
```

Pattern for each module:

```text
route -> handler -> service -> repository -> database
```

Repository layer convention:
- use the generic repository in `internal/repositories/generic` for common CRUD and list query behavior
- keep module repository files focused on custom query cases only, such as joins, aggregates, or transactional assignment logic

Router and audit convention:
- keep route wiring in `internal/router/router.go`
- reuse the router helpers for common dependencies such as permission repository, middleware, and audit service
- for mutation handlers, embed `handlercommon.AuditWriter` instead of creating per-handler audit wrapper methods

## Environment

Copy `.env.example` to `.env` and adjust the values as needed.

Minimum required variables:
- `APP_NAME`
- `APP_ENV`
- `PORT`
- `DATABASE_URL`, or these database parts when `DATABASE_URL` is empty:
  - `DB_HOST`
  - `DB_PORT`
  - `DB_USERNAME`
  - `DB_NAME`
  - `DB_PASS`
  - `DB_SSLMODE`
- `JWT_KEY` (minimum 32 characters; use a random secret for production)
- `JWT_EXP`
- `PATH_MIGRATE`

Optional but recommended:
- Redis settings for sessions and rate limiting. These stay optional; when any Redis env is set, `REDIS_URL`, `REDIS_PORT`, and `REDIS_DB` format is validated.
- Permission cache settings such as `PERMISSION_CACHE_TTL` or `PERMISSION_CACHE_TTL_SECONDS` (default `5m`). These only apply when Redis is available; otherwise permission checks read from the database. Cache entries are invalidated after role-permission, permission, user-role, and user-delete mutations; TTL remains the fallback when Redis invalidation fails.
- Location Service settings: `LOCATION_SERVICE_BASE_URL` (default `https://location-service-y7si.onrender.com`) and `LOCATION_SERVICE_TIMEOUT_SECONDS` (default `20`). Location sync imports from this shared service.
- media/storage settings for file upload use cases. Set `MEDIA_ENABLED=true` to register media routes. Storage remains optional while disabled; enabling media requires valid MinIO or Cloudflare R2 credentials and an explicit `STORAGE_BASE_URL`.
- `MEDIA_MAX_FILE_SIZE_MB` controls maximum file size (default `10`). `MEDIA_ALLOWED_CONTENT_TYPES` controls accepted MIME types after server-side byte sniffing; client extensions and `Content-Type` headers are not trusted.
- media upload rate limiting uses Redis when available. Configure `MEDIA_UPLOAD_RATE_LIMIT` and `MEDIA_UPLOAD_RATE_WINDOW_SECONDS`; without Redis it degrades safely to no distributed rate limit.
- `GOOGLE_CLIENT_ID` or `GOOGLE_CLIENT_IDS` for Google login
- SMTP settings for register OTP and password reset email flows. These stay optional; when SMTP connection env is set, `SMTP_HOST`, `SMTP_PASS`, `SMTP_FROM`, and `SMTP_PORT` format are validated.

Personal and Shared Space settings:
- `MIN_INDEPENDENT_ACCOUNT_AGE` sets the minimum registration age (default `18`).
- `SPACE_INVITATION_TTL_SECONDS` controls pending invitation expiry (default `259200`).
- `IDENTITY_LINK_TTL_SECONDS` controls one-time Hermes identity-link expiry (default `600`).

Reminder delivery:
- Set `REMINDER_SCHEDULER_ENABLED=true` and point `REMINDER_WHATSAPP_BRIDGE_URL` at the
  Hermes WhatsApp bridge. The backend checks due reminders every 30 seconds by default.
- `REMINDER_SCHEDULER_INTERVAL`, `REMINDER_SCHEDULER_BATCH_SIZE`, and
  `REMINDER_SCHEDULER_LEASE` tune the poll interval, batch size, and retry lease.
- A reminder created from a WhatsApp DM or group stores that chat target. Older reminders
  without a target fall back to the active Hermes identity of the assignee, then creator.
- Reminder status is `PENDING` before delivery, `SENT` after the bridge accepts the
  notification, and `COMPLETED` only after a user confirms the task is done.

If `reminders` was already created before delivery scheduling was added, apply this once
before enabling the scheduler (fresh installs get these columns from migration `000009`):

```sql
ALTER TABLE reminders
    ADD COLUMN IF NOT EXISTS delivery_provider VARCHAR(64) NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS delivery_target VARCHAR(255) NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS notification_claimed_at TIMESTAMPTZ NULL,
    ADD COLUMN IF NOT EXISTS notified_at TIMESTAMPTZ NULL;

CREATE INDEX IF NOT EXISTS ix_reminders_notification_due
    ON reminders (status, scheduled_at, notification_claimed_at)
    WHERE notified_at IS NULL AND deleted_at IS NULL;
```

Before deploying a backend with `SENT` support to an existing database, run this
once. Editing migration `000009` only affects fresh databases; it does not rerun
against a live database:

```sql
BEGIN;
ALTER TABLE reminders DROP CONSTRAINT ck_reminders_status;
ALTER TABLE reminders ADD CONSTRAINT ck_reminders_status
    CHECK (status IN ('PENDING','SENT','COMPLETED','CANCELLED'));
UPDATE reminders SET status = 'SENT'
    WHERE status = 'PENDING' AND notified_at IS NOT NULL AND deleted_at IS NULL;
COMMIT;
```

### Local PostgreSQL databases

Development uses the exact database name `family_assistant`. Integration tests use a
separate exact database named `family_assistant_test`; create both databases with
the credentials from the ignored `.env`, then run the normal migration command
against `family_assistant`.

The PostgreSQL integration test only accepts a local host and the exact test
database path. Run it with an explicit URL ending exactly in:

```text
FAMILY_ASSISTANT_TEST_DATABASE_URL=postgres://<user>@127.0.0.1:5432/family_assistant_test?sslmode=disable
```

It refuses non-local hosts or any database other than `family_assistant_test`.
Keep this URL local and never use it for a development or production database.

## Run Locally

Install dependencies and prepare `.env`, then:

```bash
go run . -migrate
```

Or run migration and server separately:

```bash
go run . -migrate
go run .
```

### Local Hermes MCP wiring

When Hermes runs on the same private network, enable the internal MCP endpoint
with `MCP_ENABLED=true` and set a local-only `MCP_SERVER_KEY`. For production
WhatsApp identity, also set `MCP_IDENTITY_SECRET` and enable the standalone
`integrations/hermes/family_assistant_identity` plugin. The plugin signs the
sender identity; the MCP boundary rejects tool calls without a valid signature.

Hermes calls `/mcp` with:

```text
Authorization: Bearer <MCP_SERVER_KEY>
X-Hermes-Profile: <profile fallback; signed plugin identity takes precedence>
X-Hermes-Channel: whatsapp
```

The endpoint is `http://127.0.0.1:8081/mcp` by default. The exact tool allowlist
is:

- `account_register`
- `account_link`
- `identity_link`
- `identity_revoke`
- `space_create`
- `space_list`
- `space_get_members`
- `space_update`
- `space_archive`
- `member_update_role`
- `member_remove`
- `invitation_create`
- `invitation_accept`
- `invitation_list`
- `invitation_revoke`
- `activity_create`
- `activity_list`
- `activity_update`
- `activity_delete`
- `reminder_create`
- `reminder_list`
- `reminder_complete`
- `reminder_update`
- `reminder_delete`

Mutation permissions are enforced against the selected Space membership. Owners
and admins can manage shared Space membership and invitations; members can only
update or remove reminders and activities they created. Viewers are read-only.
Space archive, member removal, invitation revoke, identity revoke, and reminder
or activity deletion use lifecycle-safe soft-delete/revoke state changes, so
historical audit records remain available while normal list tools hide removed
rows.

For an existing account whose phone number matches the current WhatsApp sender,
Hermes calls `account_link` first. The account is linked without a password or
one-time code. If no matching account exists, Hermes asks for the user's name and birth date,
explains that the birth date is used for minimum-age validation, shows a
confirmation summary, and then calls `account_register` once with
`consent: true`. Email and password are not requested. Replaying the same
profile is idempotent and returns the existing account; underage independent
registration is rejected. `identity_link` remains available for pre-existing
HTTP or Google accounts.

`identity_link` remains the migration fallback for a different phone number and
consumes a one-time code issued by an authenticated account owner. The signed
WhatsApp sender identity is resolved dynamically to the linked user, and
`X-Hermes-Channel` is retained for audit provenance. Space selectors accept an
authorized Space UUID or exact user-facing name; a blank selector uses the
user's Personal Space. Every reminder operation re-authorizes the selected
Space and resource.

### Production deployment: Docker image and Hermes on one VPS

The VPS needs Docker Compose, Nginx, Hermes, access to PostgreSQL (Neon is
supported), and optionally Redis. The application repository does not need to
be cloned on the VPS. GitHub Actions builds `ghcr.io/<owner>/family-assistant:latest`
from changes on `main`, then deploys the `backend` service from
`/opt/apps/family-assistant/docker-compose.yml`. Set the GitHub Actions
`Production` secrets `VPS_HOST`, `VPS_USER`, and `VPS_SSH_KEY`; grant the VPS
access to the GHCR package if it is private. README and Hermes plugin changes
alone do not trigger an image build or deploy.

1. Create `/opt/apps/family-assistant/docker-compose.yml` on the VPS. This is
   the current image-only layout; replace the Docker gateway address if the new
   server uses a different network:

   ```yaml
   services:
     backend:
       image: ghcr.io/zazhedho/family-assistant:latest
       container_name: family-assistant-backend
       env_file:
         - .env
       ports:
         - "127.0.0.1:8086:8080"
         - "127.0.0.1:8087:8081"
       extra_hosts:
         - "host.docker.internal:172.24.0.1"
       restart: unless-stopped
       logging:
         driver: json-file
         options:
           max-size: 10m
           max-file: "3"
       environment:
         TZ: Asia/Jakarta
       volumes:
         - /var/log/apps/family-assistant:/var/log/family-assistant
   ```

2. Create `/opt/apps/family-assistant/.env` from [`.env.example`](.env.example)
   and restrict it to the service administrator (`chmod 600`). Set at least
   `APP_NAME`, `APP_ENV=production`, `PORT=8080`, `DATABASE_URL` (Neon requires
   TLS), `JWT_KEY`, `JWT_EXP`, `PATH_MIGRATE=file://migrations`, and
   `RUN_MIGRATION=true`. The image contains the migrations. For Hermes, set:

   ```dotenv
   MCP_ENABLED=true
   MCP_ADDR=0.0.0.0:8081
   MCP_SERVER_KEY=<random bearer key>
   MCP_IDENTITY_SECRET=<different random HMAC secret>
   REMINDER_SCHEDULER_ENABLED=true
   REMINDER_WHATSAPP_BRIDGE_URL=http://host.docker.internal:3011
   REMINDER_SCHEDULER_INTERVAL=30s
   ```

   Set Redis variables only when Redis is available. Keep `MCP_SERVER_KEY`,
   `MCP_IDENTITY_SECRET`, `JWT_KEY`, and database credentials out of Git.
   Fresh databases migrate on first container start; the SQL patch above is
   only for databases created before reminder delivery was added.

3. Install Hermes as a host service and pair the dedicated bot number with
   `hermes whatsapp`. Install its background gateway with
   `hermes gateway install`. Its WhatsApp bridge listens on host loopback port `3010`.
   The backend container reaches it through a host-only Nginx proxy on `3011`.
   Create the Compose network with `docker compose create backend`, then find
   its gateway with `docker network inspect family-assistant_default`. Use that
   address in both `extra_hosts` above and the Nginx `listen` directive. Save
   this server block as `/etc/nginx/conf.d/family-assistant-hermes-bridge.conf`:

   ```nginx
   server {
       listen 172.24.0.1:3011;
       server_name _;
       location / {
           proxy_http_version 1.1;
           proxy_set_header Host 127.0.0.1:3010;
           proxy_pass http://127.0.0.1:3010;
       }
   }
   ```

   Keep `3011` bound to the Docker gateway, and keep `3010`, `8086`, and `8087`
   off public interfaces. Test Nginx with `nginx -t`, then reload it with
   `systemctl reload nginx`.

4. Install the standalone identity plugin without modifying Hermes source.
   From a checkout on the deployment machine, copy `plugin.py`, `plugin.yaml`,
   and `__init__.py` from
   [`integrations/hermes/family_assistant_identity/`](integrations/hermes/family_assistant_identity/)
   to `/root/.hermes/plugins/family-assistant-identity/` on the VPS:

   ```sh
   ssh root@<server> 'mkdir -p /root/.hermes/plugins/family-assistant-identity'
   scp integrations/hermes/family_assistant_identity/plugin.py \
     integrations/hermes/family_assistant_identity/plugin.yaml \
     integrations/hermes/family_assistant_identity/__init__.py \
     root@<server>:/root/.hermes/plugins/family-assistant-identity/
   ```

   The plugin must be updated whenever its signed envelope changes; updating the backend
   image or Hermes does not copy it. In `/root/.hermes/.env`, set:

   ```dotenv
   WHATSAPP_ENABLED=true
   WHATSAPP_MODE=bot
   WHATSAPP_ALLOWED_USERS=<comma-separated allowed phone numbers>
   MCP_FAMILY_ASSISTANT_API_KEY=<same value as MCP_SERVER_KEY>
   FAMILY_ASSISTANT_IDENTITY_SECRET=<same value as MCP_IDENTITY_SECRET>
   ```

5. Merge these blocks into `/root/.hermes/config.yaml` (do not replace the
   rest of Hermes' configuration). Substitute the actual group JID and admin
   number; use the bridge logs to discover the group JID. `group_allow_from`
   limits groups, `WHATSAPP_ALLOWED_USERS` limits senders (including group
   members), and the admin lists limit privileged WhatsApp commands. Add a new
   user's number to the sender allowlist before expecting onboarding replies:

   ```yaml
   mcp_servers:
     family_assistant:
       url: http://127.0.0.1:8087/mcp
       connect_timeout: 15.0
       headers:
         Authorization: Bearer ${MCP_FAMILY_ASSISTANT_API_KEY}
         X-Hermes-Channel: whatsapp
       identity_header:
         name: X-Hermes-Profile
         value_from: profile
       enabled: true

   platform_toolsets:
     whatsapp: [web, browser, mcp-family_assistant]

   platforms:
     whatsapp:
       extra:
         bridge_port: 3010
         allow_admin_from: ['<admin-number>@s.whatsapp.net', '<admin-number>']
         group_allow_admin_from: ['<admin-number>@s.whatsapp.net', '<admin-number>']
         user_allowed_commands: []
         group_user_allowed_commands: []

   whatsapp:
     group_policy: allowlist
     group_allow_from: <group-jid>@g.us
     require_mention: true

   plugins:
     enabled: [family-assistant-identity]
     entries:
       family-assistant-identity:
         allow_tool_override: true

   display:
     platforms:
       whatsapp:
         tool_progress: false
         show_reasoning: false
         interim_assistant_messages: false
   ```

   The plugin manifest requests `tools.override`; Hermes must grant it. Check
   gateway logs for `capability=tools.override decision=allow` after restart.
   Append these durable rules to `/root/.hermes/SOUL.md` so new sessions follow
   the same onboarding flow:

   ```text
   Reply in Indonesian for Family Assistant. At the start of a new WhatsApp
   session, call account_link before registration or protected operations.
   If the number has no matching account, offer registration on greetings.
   Ask naturally for name and birth date, normalize the date to YYYY-MM-DD,
   explain the age check, summarize the data, and call account_register only
   after explicit consent. Do not ask for a one-time code when account_link
   succeeds; identity_link is only for a different-number migration.
   Never claim a tool action succeeded if it failed.
   Use Family Assistant MCP for data; backend scheduler delivers reminders.
   Do not create Hermes cron jobs for Family Assistant reminders.
   ```

   Review old `/root/.hermes/memories/MEMORY.md` and `USER.md` before copying
   them to another server: stale link-code or Hermes cron instructions can
   override the intended flow.

6. Start and check services:

   ```sh
   cd /opt/apps/family-assistant
   docker compose pull backend
   docker compose up -d backend
   curl -f http://127.0.0.1:8086/healthcheck
   hermes gateway restart
   hermes gateway status
   hermes mcp test family_assistant
   ```

   The MCP test must list `account_link`. Then send `tautkan akun saya` from a
   WhatsApp number already stored in `users.phone`: the tool should return
   `status=existing`. Send a reminder from a permitted chat and verify it is
   delivered there after the due time. A successful `tools/list` alone does not
   prove that signed WhatsApp identity works. If the agent still sees an old
   tool list, use `/new` in that WhatsApp chat. If a fresh session returns
   `authentication required`, compare the deployed plugin files with this
   repository and check both shared-secret pairs and the capability grant.

### Hermes WhatsApp profile and tool access

Keep group chats on the shared `default` profile, with web/browser and Family
Assistant MCP only. Route only the administrator's direct WhatsApp chat to a
separate `admin` profile. Do not copy WhatsApp credentials into that profile.

In `/root/.hermes/config.yaml`, merge this into the default profile config and
replace the placeholder with the administrator's phone number or JID:

```yaml
group_sessions_per_user: false

gateway:
  multiplex_profiles: true
  profile_routes:
    - name: admin-whatsapp-dm
      platform: whatsapp
      chat_id: "<admin-number>@s.whatsapp.net"
      profile: admin
```

Create the isolated admin profile from the default profile, then set its
WhatsApp tools and terminal backend:

```sh
hermes profile create admin --clone
hermes -p admin tools enable hermes-whatsapp --platform whatsapp
hermes -p admin tools enable mcp-family_assistant --platform whatsapp
hermes -p admin tools enable a2a --platform whatsapp
hermes -p admin config set terminal.backend local
hermes gateway restart
```

The admin profile must be served by the default profile's multiplexed gateway.
After restarting, verify `hermes status` lists both `default` and `admin`, and
that only the admin DM route targets `admin`. A group chat—including messages
sent by the administrator—stays on `default`; therefore terminal, file, and
code-execution tools remain unavailable in groups. All sender allowlists still
apply, including to group participants. Send `/new` in the admin DM and group
after changing toolsets so each session reloads its tool schema.

`terminal.backend: local` executes on the Hermes host without container
isolation. If the gateway runs as `root`, terminal access from the administrator
DM is root access to the VPS. Keep this route private and never add a group
route to the admin profile.

Default health check:

```text
GET /healthcheck
```

## Main Routes

The current route set includes:

- `POST /api/user/register`
- `POST /api/user/register/otp/send`
- `POST /api/user/login`
- `POST /api/user/google/login`
- `POST /api/user/refresh-token`
- `POST /api/user/forgot-password`
- `POST /api/user/reset-password`
- `POST /api/user/logout`
- `GET /api/user`
- `GET /api/users`

`POST /api/user/register` requires `birth_date` in `YYYY-MM-DD` format and
enforces `MIN_INDEPENDENT_ACCOUNT_AGE`.

Personal and Shared Space routes:
- `GET /api/spaces`
- `POST /api/spaces`
- `GET /api/spaces/:space_id/members`
- `POST /api/spaces/:space_id/invitations`
- `POST /api/invitations/accept`
- `POST /api/hermes/link-codes`
- `DELETE /api/hermes/link`

Space-scoped reminder routes:
- `POST /api/reminders`
- `GET /api/reminders?space_id=<space-id>`
- `POST /api/reminders/:reminder_id/complete?space_id=<space-id>`

- `GET /api/roles`
- `POST /api/role`
- `GET /api/role/:id`
- `PUT /api/role/:id`
- `DELETE /api/role/:id`
- `POST /api/role/:id/permissions`

- `GET /api/permissions`
- `GET /api/permissions/me`
- `POST /api/permission`
- `GET /api/permission/:id`
- `PUT /api/permission/:id`
- `DELETE /api/permission/:id`

- `GET /api/menus/active`
- `GET /api/menus/me`
- `GET /api/menus`
- `GET /api/menu/:id`
- `PUT /api/menu/:id`

- `GET /api/configs`
- `GET /api/config/:id`
- `PUT /api/config/:id`

- `GET /api/location/province`
- `GET /api/location/city?province_code=11`
- `GET /api/location/district?city_code=1101`
- `GET /api/location/village?district_code=110101`
- `POST /api/location/sync`
- `GET /api/location/sync/:id`

- `GET /api/audits`
- `GET /api/audit/:id`

Media routes are registered only when `MEDIA_ENABLED=true`:
- `POST /api/media` using multipart field `file`
- `DELETE /api/media/:id`

Uploaded media metadata is stored in PostgreSQL. Object names are generated by the server. Delete accepts only a media UUID; the owner or `superadmin` may delete it. A failed metadata insert triggers storage cleanup, and upload/delete attempts are written to the audit trail.

Additional session routes are registered only when Redis is available:
- `GET /api/user/sessions`
- `DELETE /api/user/session/:session_id`
- `POST /api/user/sessions/revoke-others`

Location architecture:
- PostgreSQL is the source of truth for provinces, cities, districts, and villages
- Redis is used only as runtime cache
- shared Location Service is used only for sync/import to the database
- location sync runs asynchronously; start the job with `POST /api/location/sync` and poll its status via `GET /api/location/sync/:id`
- use scoped sync for regular updates; `level=all` is intended for initial bootstrap because it performs a full hierarchical import

## Module Seed Helper

To avoid writing menu and permission seed SQL manually for every new module, the starter kit includes a helper command:

```bash
go run ./cmd/module-seed \
  --name projects \
  --display-name "Projects" \
  --path /projects \
  --icon bi-folder \
  --order-index 905
```

The command prints SQL for:
- one `menu_items` row
- matching `permissions` rows for the same resource
- optional default `role_permissions` grants

This helps prevent mismatch bugs such as:
- `menu_items.name = projects`
- `permissions.resource = project`

Optional flags:
- `--parent-name education`
- `--resource reports`
- `--actions list,view,export`
- `--grant-roles admin,superadmin`

For nested menus, `--parent-name` generates a `parent_id` subquery so the migration stays declarative and consistent.

## How To Add A New Module

When adding a new module, keep it aligned with the permission-first design.

### 1. Add the backend layers

Create these parts:
- `internal/domain/<module>`
- `internal/dto`
- `internal/interfaces/<module>`
- `internal/repositories/<module>`
- `internal/services/<module>`
- `internal/handlers/http/<module>`
- route registration in `internal/router/router.go`

For route registration:
- use the existing router helpers for shared dependencies instead of recreating the same wiring in every route group
- protect endpoints with `PermissionMiddleware(resource, action)`
- pass `r.auditService()` to handlers that write audit trails

For handler audit trails:
- embed `handlercommon.AuditWriter` in handlers that write audit events
- call `h.WriteAudit(ctx, event)` for mutation success and failure paths

For repository implementation:
- reuse `internal/repositories/generic.GenericRepository[T]` for `Store`, `GetByID`, `GetAll`, `Update`, and `Delete`
- embed `interfacegeneric.GenericRepository[T]` in module repository interfaces for the common contract
- configure searchable columns, allowed filters, and sortable columns through `repositorygeneric.QueryOptions`
- add custom methods in the module repo only when the query is business-specific

### 2. Add migration

For a new business module, create:
- the business table(s)
- one `menu_items` row for the module
- the required `permissions` rows for the same resource name
- optional default `role_permissions` seed if needed

Important:
- use the same resource name across menu and permissions
- example:
  - menu name: `projects`
  - permission resource: `projects`

This is what allows menus to be derived automatically from permissions.

Tip:
- use `go run ./cmd/module-seed ...` to generate the menu and permission seed block before pasting it into the migration

### 3. Protect routes with permissions

Use:

```go
mdw.PermissionMiddleware("projects", "list")
mdw.PermissionMiddleware("projects", "view")
mdw.PermissionMiddleware("projects", "create")
mdw.PermissionMiddleware("projects", "update")
mdw.PermissionMiddleware("projects", "delete")
```

Avoid using role-name checks for module access unless the case is explicitly special like `superadmin`.

## Role Management Flow

Recommended admin flow:

1. Create a role.
2. Assign permissions to the role.
3. Do not assign menus manually.
4. Let menu visibility be derived from permissions automatically.

Menu management note:
- `menus` is code-defined, but selected presentation fields may still be updated at runtime
- do not create or delete menus through admin API
- structural changes such as adding new menus should still go through code and migration

This prevents drift between:
- what a user can see
- what a user can actually access

## Notes

- `role_menus` still exists in the base schema for compatibility, but runtime access control does not depend on it.
- For new modules, prefer permission-based design from the start.
- If you introduce nested menus, parent menu visibility will be resolved automatically when the child menu is permitted.
