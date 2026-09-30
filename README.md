# Family Assistant

Backend service for a private assistant with personal and shared Spaces:
- Gin HTTP router
- PostgreSQL via GORM
- JWT authentication
- permission-first RBAC
- runtime application configurations from database
- optional Redis-based session management and rate limiting
- WhatsApp onboarding and signed Hermes MCP identity
- personal/shared Spaces, activities, and backend-delivered reminders

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
- WhatsApp account registration, phone-based account linking, and external identity management
- Personal and shared Spaces, memberships, and invitations
- Activities (generic notes and event records), including creation with a reminder
- Reminders and optional WhatsApp delivery scheduler, with a Redis due-time index

## Project Structure

Main backend layout:

```text
family-assistant/
├── infrastructure/
├── internal/
│   ├── domain/
│   ├── dto/
│   ├── cache/
│   ├── handlers/
│   │   ├── http/
│   │   └── mcp/
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
- `MEDIA_MAX_FILE_SIZE_MB` controls maximum file size (code default `5`; an explicit env value overrides it). `MEDIA_ALLOWED_CONTENT_TYPES` controls accepted MIME types after server-side byte sniffing; client extensions and `Content-Type` headers are not trusted.
- media upload rate limiting uses Redis when available. Configure `MEDIA_UPLOAD_RATE_LIMIT` and `MEDIA_UPLOAD_RATE_WINDOW_SECONDS`; without Redis it degrades safely to no distributed rate limit.
- `GOOGLE_CLIENT_ID` or `GOOGLE_CLIENT_IDS` for Google login
- SMTP settings for register OTP and password reset email flows. These stay optional; when SMTP connection env is set, `SMTP_HOST`, `SMTP_PASS`, `SMTP_FROM`, and `SMTP_PORT` format are validated.

Personal and Shared Space settings:
- `MIN_INDEPENDENT_ACCOUNT_AGE` sets the minimum registration age (default `18`).
- `SPACE_INVITATION_TTL_SECONDS` controls pending invitation expiry (default `259200`).
- `IDENTITY_LINK_TTL_SECONDS` controls one-time Hermes identity-link expiry (default `600`).

MCP settings:

- `MCP_ENABLED` defaults to `false`; enable it for Hermes.
- `MCP_ADDR` defaults to `127.0.0.1:8081`; use `0.0.0.0:8081` inside Docker with a loopback-only published port.
- `MCP_SERVER_KEY` is required when MCP is enabled. It authenticates the Hermes service, not individual users.
- `MCP_IDENTITY_SECRET` verifies signed WhatsApp sender/chat/time context. Configure it for WhatsApp deployments; use a different secret from the bearer key.
- `MCP_PROFILE_HEADER` defaults to `X-Hermes-Profile`; signed sender identity takes precedence over this fallback.

Reminder delivery:
- Set `REMINDER_SCHEDULER_ENABLED=true` and point `REMINDER_WHATSAPP_BRIDGE_URL` at the
  Hermes WhatsApp bridge. The backend checks due reminders every 2 minutes by default.
- `REMINDER_SCHEDULER_INTERVAL`, `REMINDER_SCHEDULER_BATCH_SIZE`, and
  `REMINDER_SCHEDULER_LEASE` tune the poll interval, batch size, and retry lease.
- `REMINDER_SCHEDULER_DATABASE_FALLBACK_INTERVAL` controls PostgreSQL polling during Redis
  failures (default `1m`); `REMINDER_SCHEDULER_INDEX_RECONCILE_INTERVAL` controls Redis index
  reconciliation (default `1h`); `REMINDER_SCHEDULER_INDEX_BATCH_SIZE` controls Redis write
  batches (default `1000`).
- A reminder created from a WhatsApp DM or group stores that chat target. Reminders
  created outside WhatsApp without a target fall back to the active Hermes identity of
  the assignee, then creator.
- Reminder status is `PENDING` before delivery, `SENT` after the bridge accepts the
  notification, and `COMPLETED` only after a user confirms the task is done.

This guide assumes a fresh database. Migration `000009` already creates the delivery
columns, due-reminder index, and `PENDING`, `SENT`, `COMPLETED`, `CANCELLED` status constraint;
no manual schema patch is needed.

| Scheduler variable | Default |
| --- | --- |
| `REMINDER_SCHEDULER_ENABLED` | `false` |
| `REMINDER_WHATSAPP_BRIDGE_URL` | unset; required when enabled |
| `REMINDER_SCHEDULER_INTERVAL` | `2m` |
| `REMINDER_SCHEDULER_BATCH_SIZE` | `50` |
| `REMINDER_SCHEDULER_LEASE` | `2m` |
| `REMINDER_SCHEDULER_DATABASE_FALLBACK_INTERVAL` | `1m` |
| `REMINDER_SCHEDULER_INDEX_RECONCILE_INTERVAL` | `1h` |
| `REMINDER_SCHEDULER_INDEX_BATCH_SIZE` | `1000` |

PostgreSQL remains the source of truth. When Redis is available, the scheduler checks
the sorted-set index `family-assistant:reminders:due` first and skips PostgreSQL claims
when nothing is due. Entries contain reminder IDs and due-time scores, not message bodies.
They have no TTL: delivery/cancellation/completion removes entries explicitly, while
startup and hourly reconciliation rebuild pending entries and remove stale ones.
Use a native `redis://` or TLS `rediss://` connection URL (not an Upstash REST URL).

Without Redis, or while the index is degraded, PostgreSQL fallback is rate-limited by
`REMINDER_SCHEDULER_DATABASE_FALLBACK_INTERVAL`. It still runs on scheduler ticks:
with a `2m` poll and `1m` fallback setting, checks occur every 2 minutes, not on a separate
1-minute timer. Delivery can wait up to a poll interval plus processing/retries.
The scheduler runs inside the backend process; it needs no separate service, port,
Hermes cronjob, or user authentication session. Bridge acceptance marks `SENT`, not
proof that the recipient read the message. A send followed by a failed database update
can be retried after the lease expires; delivery is not guaranteed exactly once.

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

This applies migrations **and starts the server**; it is not a migration-only command.
Once the schema is current, start without applying migrations using:

```bash
go run . -migrate=false
```

### Local verification

Run from the repository root:

```sh
go test ./...
make lint
PYTHONDONTWRITEBYTECODE=1 python3 -m unittest discover -s integrations/hermes/family_assistant_identity -p 'test_*.py'
```

Install the linter with `make lint-install` if needed. PostgreSQL integration tests
require the dedicated local `FAMILY_ASSISTANT_TEST_DATABASE_URL` described above;
never point them at the development or production database.

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

The endpoint is `http://127.0.0.1:8081/mcp` by default. It exposes these **25 tools**;
Hermes adds its MCP server prefix to tool names:

| Tool | Purpose |
| --- | --- |
| `account_register` | Register with name, birth date, and explicit consent; create the Personal Space. |
| `account_link` | Link an existing account matching the signed WhatsApp phone number. |
| `identity_link` | Link using an account-owner-issued one-time code. |
| `identity_revoke` | Revoke the current external identity link. |
| `space_create` | Create a shared Space. |
| `space_list` | List accessible personal/shared Spaces. |
| `space_get_members` | List active members of a Space. |
| `space_update` | Update shared Space details. |
| `space_archive` | Archive a shared Space. |
| `member_update_role` | Change a shared Space member's role. |
| `member_remove` | Remove a shared Space member. |
| `invitation_create` | Issue an expiring invitation. |
| `invitation_accept` | Accept an invitation using its token. |
| `invitation_list` | List pending invitations without exposing their tokens. |
| `invitation_revoke` | Revoke a pending invitation. |
| `activity_create` | Record an activity/note without creating a reminder. |
| `activity_create_with_reminder` | Record an activity and create its reminder in one tool call. |
| `activity_list` | List activities with kind/time filters. |
| `activity_update` | Update an activity's kind, note, or event time. |
| `activity_delete` | Soft-delete an activity. |
| `reminder_create` | Create a reminder, optionally assigned to a member. |
| `reminder_list` | List reminders with status/time filters. |
| `reminder_complete` | Mark the task completed. |
| `reminder_update` | Update a pending reminder's details, schedule, or assignee. |
| `reminder_delete` | Cancel and soft-delete a reminder. |

For an activity that also needs a reminder, use `activity_create_with_reminder`
instead of separate `activity_create` and `reminder_create` calls. Generic example:

```json
{
  "activity": {"space": "Family", "kind": "diaper", "note": "Changed diaper"},
  "reminder": {"title": "Check diaper", "after_minutes": 240}
}
```

Omitting `activity.occurred_at` uses the trusted signed WhatsApp message time.
For a user-specified event time, supply an explicit RFC3339 timestamp with its
timezone offset. Supply exactly one of `reminder.after_minutes` (positive integer)
or `reminder.scheduled_at` (RFC3339). Relative minutes are calculated by the backend
from the resolved activity time, not the model's session date.

The result is `created` or `partial_success`. This is not an all-or-nothing
transaction: if reminder creation fails after the activity is saved, the result
contains the activity and `reminder_error`. Do not repeat the whole call blindly;
create/retry only the missing reminder after checking the result.
Browser/terminal tools belong to Hermes, not this MCP endpoint. Media upload is
currently HTTP-only; automatic WhatsApp media ingestion is not implemented.

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
registration is rejected.

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

Follow steps 1–8 in order for a fresh server. Commands run on the VPS as the
service administrator (`root` in this guide), except the explicitly labelled
plugin copy commands, which run from a checkout on your deployment workstation.
Do not copy this server's credentials or WhatsApp session to another server.

Before step 1:

- Install Docker Engine with the Compose plugin using the
  [official OS-specific instructions](https://docs.docker.com/engine/install/),
  and install Nginx, curl, and OpenSSL with the VPS package manager.
- Verify `docker compose version` and `nginx -v`. Create the application and
  log directories with `mkdir -p /opt/apps/family-assistant /var/log/apps/family-assistant`.
- Provision a PostgreSQL database and obtain its TLS connection URL. Redis is
  optional; provision it only if you want the cache/index features.
- Ensure the backend image has been published. For a private GHCR package,
  run `docker login ghcr.io -u <github-user>` on the VPS and enter a token with
  package-read permission at the password prompt, never in a committed file.
- Generate separate values for `JWT_KEY`, `MCP_SERVER_KEY`, and
  `MCP_IDENTITY_SECRET` (for example, run `openssl rand -hex 32` separately for
  each). The MCP bearer key and identity HMAC secret must not be the same.

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
   REMINDER_SCHEDULER_INTERVAL=2m
   ```

   Set Redis variables only when Redis is available. Keep `MCP_SERVER_KEY`,
   `MCP_IDENTITY_SECRET`, `JWT_KEY`, and database credentials out of Git.
   Fresh databases migrate on first container start; no manual SQL patch is needed.

3. Install and configure Hermes on the host, not inside the backend container:

   ```sh
   curl -fsSL https://hermes-agent.nousresearch.com/install.sh | bash
   ```

   Follow the installer's PATH instructions or open a new login shell, then run:

   ```sh
   hermes --version
   hermes setup
   hermes model
   ```

   In the model picker, select **ChatGPT or Codex Subscription** for the
   `openai-codex` provider. Open its device-login URL on your own browser,
   enter the displayed code, and authorize your account. No browser or Codex
   CLI installation on the VPS is required. Select a tool-capable model from
   your account's live catalog. The current VPS uses `gpt-6-luna`; availability
   depends on the account/provider, so do not force an unavailable model ID.
   If using another provider, configure its credentials through the picker.
   See [Hermes provider setup](https://hermes-agent.nousresearch.com/docs/integrations/providers).

   Verify the selection and start one CLI conversation before adding WhatsApp:

   ```sh
   hermes config get model.provider
   hermes config get model.default
   hermes
   ```

   Inside that CLI conversation, use `/reasoning max --global` for the current
   project's preferred effort, then `/reasoning` to verify it and `/exit` to
   leave. Higher effort can increase latency; use a supported lower setting
   if preferred. A stored config value alone does not prove runtime effort:
   some Hermes versions warn that `agent.reasoning_effort` is unrecognized.
   Verify `/reasoning` again from WhatsApp after gateway setup.

   If web/browser tools are needed, configure their providers with
   `hermes setup tools`; enabling a toolset does not provide missing API keys
   or a browser runtime. Do not enable terminal/file tools on the default
   WhatsApp profile during the wizard. The explicit allowlist in step 6 is the
   intended policy. The official [installation guide](https://hermes-agent.nousresearch.com/docs/getting-started/installation)
   covers installer and PATH troubleshooting.

   Pair a **dedicated bot number** using `hermes whatsapp`: scan the QR from
   that number's WhatsApp Linked Devices screen. Only the default profile owns
   this session. Do not start a second gateway for the same bot number.
   Install/start the background gateway only after step 7.
   Its WhatsApp bridge listens on host loopback port `3010`.
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

4. Start the backend **before activating the new identity plugin**:

   ```sh
   cd /opt/apps/family-assistant
   docker compose pull backend
   docker compose up -d backend
   curl -f http://127.0.0.1:8086/healthcheck
   ```

   Check container logs if startup/migrations fail. A fresh database uses the
   migrations shipped in the image. Hermes is not started yet, so reminder
   delivery is tested only after step 8.

5. Install the standalone identity plugin without modifying Hermes source.
   From a checkout on the deployment machine, copy `plugin.py`, `plugin.yaml`,
   and `__init__.py` from
   [`integrations/hermes/family_assistant_identity/`](integrations/hermes/family_assistant_identity/)
   to `/root/.hermes/plugins/family-assistant-identity/` on the VPS:

   Run these commands on the **deployment workstation**, from the repository root:

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

6. Merge these blocks into `/root/.hermes/config.yaml` (do not replace the
   rest of Hermes' configuration). Substitute the actual group JID and admin
   number; use the bridge logs to discover the group JID. `group_allow_from`
   limits groups, `WHATSAPP_ALLOWED_USERS` limits senders (including group
   members), and the admin lists limit privileged WhatsApp commands. Add a new
   user's number to the sender allowlist before expecting onboarding replies:

   ```yaml
   timezone: Asia/Jakarta

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
   Distinguish event time from database created_at/updated_at and processing time.
   Never reuse a previous record's timestamp for a new event. Use the current
   live-time context for today's date, not the session-start date.
   If activity_create or activity_create_with_reminder allows occurred_at to be
   omitted, omit it for "now" or no stated time: the backend uses the signed
   WhatsApp message timestamp. If the user specifies a time, send that explicit
   RFC3339 event time with its timezone; ask when the date/time is ambiguous.
   For relative reminders, use after_minutes so the backend calculates the
   schedule from the event time. Report occurred_at from the tool response,
   not created_at/updated_at. Do not change event time without a user request.
   Resolve today/yesterday and time-only event dates from the original message
   time in the configured timezone; an explicit user date takes priority.
   ```

   Keep the existing identity/personality text in `SOUL.md`; append these
   rules instead of replacing the entire file. If creating it for the first
   time, include the Family Assistant identity and these rules. The admin
   profile is cloned after this step so it starts with the same rules.

   Enable current-time context without modifying Hermes source:

   ```sh
   hermes config set timezone Asia/Jakarta
   hermes plugins install live-time --enable
   ```

   Preserve other enabled plugins when merging `plugins.enabled`; the
   `live-time` installer enables its own entry. Configure the admin profile
   only after it is created in step 7. Do not restart/start the gateway yet.

   `live-time` is a community plugin in the Hermes catalog. It refreshes the
   current time before each model call; it is not the event timestamp. For
   queued/delayed messages, the event time comes from the WhatsApp gateway event.
   The identity plugin prefers the original bridge message timestamp; when it
   is absent it uses gateway receipt time, never the model's completion time.
   It also injects that original message time before each model call. Resolve
   "today", "yesterday", and time-only event dates from the message's date in
   the configured timezone, not the later processing date. Explicit dates win.
   The identity plugin signs that message time in a `v2` envelope. Deploy the
   backend first (it accepts both `v1` and `v2`), then copy the updated identity
   plugin files and restart Hermes. An older backend cannot verify `v2`.
   Legacy `v1` calls still work with explicit event times; an omitted event time
   without a signed message timestamp is rejected rather than guessed.

   Review old `/root/.hermes/memories/MEMORY.md` and `USER.md` before copying
   them to another server: stale link-code or Hermes cron instructions can
   override the intended flow.

7. Create and configure the administrator profile before starting the gateway.

   Keep group chats on the shared `default` profile, with web/browser and
   Family Assistant MCP only. Route only the administrator's direct WhatsApp
   chat to `admin`. On the VPS:

   ```sh
   hermes profile create admin --clone
   mkdir -p /root/.hermes/profiles/admin/plugins/family-assistant-identity
   cp /root/.hermes/plugins/family-assistant-identity/plugin.py \
      /root/.hermes/plugins/family-assistant-identity/plugin.yaml \
      /root/.hermes/plugins/family-assistant-identity/__init__.py \
      /root/.hermes/profiles/admin/plugins/family-assistant-identity/
   hermes -p admin plugins enable family-assistant-identity
   hermes -p admin config set timezone Asia/Jakarta
   hermes -p admin plugins install live-time --enable
   hermes -p admin config set terminal.backend local
   ```

   Cloning config is not a substitute for installing standalone plugin files
   in the new profile. Do not copy the WhatsApp session, use `--clone-channels`,
   or run `hermes -p admin gateway install`. The admin profile is served by the
   default profile's multiplexed gateway. OAuth logins are shared through
   Hermes' root auth store; do not manually copy single-use refresh tokens.
   See [Hermes profile cloning](https://hermes-agent.nousresearch.com/docs/user-guide/profiles).

   Verify `hermes -p admin config get model.provider` and
   `hermes -p admin config get model.default`. On a fresh setup they inherit
   the default model. If changing providers later, run `hermes -p admin model`
   too; changing the default profile does not keep the admin config in sync.
   Review the admin `.env` for the MCP bearer/HMAC keys and `SOUL.md` for the
   same onboarding/time rules. Do not expose those values in logs.

   Merge this **only into the default profile's** `/root/.hermes/config.yaml`,
   substituting the admin number/JID:

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

   In `/root/.hermes/profiles/admin/config.yaml`, merge the admin WhatsApp
   toolsets. The following matches the capabilities enabled on the current
   VPS; optional tool providers still require their own setup:

   ```yaml
   platform_toolsets:
     whatsapp:
       - a2a
       - browser
       - clarify
       - code_execution
       - computer_use
       - connections
       - context_engine
       - cronjob
       - delegation
       - file
       - homeassistant
       - image_gen
       - kanban
       - mcp-family_assistant
       - memory
       - session_search
       - skills
       - spotify
       - stt
       - terminal
       - todo
       - tts
       - video
       - video_gen
       - vision
       - web
       - x_search
       - yuanbao
   ```

   A group—including the admin's group messages—stays on `default`. Never add
   a group route to `admin`. Sender allowlists still apply to group members.
   `terminal.backend: local` executes on the host; when Hermes runs as `root`,
   admin DM terminal access is **root access to the VPS**, not Docker isolation.

8. Install/start the single gateway and verify the complete flow:

   ```sh
   curl -f http://127.0.0.1:8086/healthcheck
   hermes gateway install
   hermes gateway restart
   hermes gateway status
   hermes status
   hermes plugins list --plain --no-bundled
   hermes -p admin plugins list --plain --no-bundled
   hermes plugins doctor /root/.hermes/plugins/family-assistant-identity
   hermes plugins doctor /root/.hermes/plugins/live-time
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

   Verify both profiles appear in `hermes status`; the private plugins must
   be enabled and the WhatsApp bridge connected. Then test from WhatsApp:

   - An allowlisted unregistered sender says `halo`: offer onboarding, accept
     a natural birth date, and register only after consent.
   - An existing sender links through `account_link` without a web login or
     externally generated link code.
   - Send an activity without a time: its `occurred_at` matches the original
     message time, not completion time or another record's `updated_at`.
   - Send an explicit earlier time: it is preserved; an ambiguous date prompts
     a question. Add a relative reminder and verify it is calculated from the
     event time and delivered to the originating DM/group.
   - Group participants share one session; terminal/file/code-execution tools
     are unavailable there, including to the admin. Only the admin DM targets
     `admin`. A sender outside the allowlist receives no bot reply.
   - WhatsApp replies show neither reasoning nor tool-progress messages.
     `/reasoning` reports the intended effort in the admin DM.

   A fresh install has no old conversation to reset. After changing toolsets
   on an existing installation, `/new` reloads the session's schema but starts
   a new conversation; use it deliberately, not as a daily time fix. Refreshing
   time is handled by `live-time` and the signed message context.

   On later updates, deploy the backend first, copy the identity plugin files
   to **both profiles**, and restart the single gateway. The application image
   update does not update Hermes plugins. `SOUL.md`/model settings in the admin
   clone also do not automatically follow later default-profile edits.

Default health check:

```text
GET /healthcheck
```

## Main Routes

The current route set includes:

- `GET /healthcheck`
- `GET /api/user/register/status`
- `POST /api/user/register`
- `POST /api/user/register/otp/send`
- `POST /api/user/login`
- `POST /api/user/google/login`
- `POST /api/user/refresh-token`
- `POST /api/user/forgot-password`
- `POST /api/user/reset-password`
- `POST /api/user/logout`
- `GET /api/user`
- `POST /api/user`
- `PUT /api/user`
- `DELETE /api/user`
- `GET /api/user/:id`
- `PUT /api/user/:id`
- `DELETE /api/user/:id`
- `PUT /api/user/change/password`
- `POST /api/user/:id/impersonate`
- `POST /api/user/stop-impersonation`
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

Activity CRUD (including combined activity/reminder creation), Space update/archive,
member role changes/removal, invitation list/revoke, and reminder update/delete
are currently MCP-only. Do not assume every MCP tool has a matching HTTP route.
Resource routes enforce JWT authentication and applicable permissions/membership;
public authentication and location-read routes have their own access rules.

Roles:

- `GET /api/roles`
- `POST /api/role`
- `GET /api/role/:id`
- `PUT /api/role/:id`
- `DELETE /api/role/:id`
- `POST /api/role/:id/permissions`

Permissions:

- `GET /api/permissions`
- `GET /api/permissions/me`
- `POST /api/permission`
- `GET /api/permission/:id`
- `PUT /api/permission/:id`
- `DELETE /api/permission/:id`

Menus:

- `GET /api/menus/active`
- `GET /api/menus/me`
- `GET /api/menus`
- `GET /api/menu/:id`
- `PUT /api/menu/:id`

Configurations:

- `GET /api/configs`
- `GET /api/config/:id`
- `PUT /api/config/:id`

Locations:

- `GET /api/location/province`
- `GET /api/location/city?province_code=11`
- `GET /api/location/district?city_code=1101`
- `GET /api/location/village?district_code=110101`
- `POST /api/location/sync`
- `GET /api/location/sync/:id`

Audits:

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
