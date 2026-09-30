# Family Assistant identity plugin

This is a standalone Hermes plugin. It does not modify the Hermes installation.

For an image-only VPS, follow the [production deployment guide](../../../README.md)
and copy the plugin files; the application repository is not required there.
For later releases, follow the [VPS update checklist](../../../README.md#updating-an-existing-vps):
deploy the backend first, update the copied plugin files in both profiles,
then restart the single gateway. An application image update does not update
this plugin.
For a local checkout, install it as a symlink so project updates are picked up:

```sh
mkdir -p "${HERMES_HOME:-$HOME/.hermes}/plugins"
ln -sfn "$PWD/integrations/hermes/family_assistant_identity" \
  "${HERMES_HOME:-$HOME/.hermes}/plugins/family-assistant-identity"
```

Set the same random secret in both processes:

```text
Family Assistant .env: MCP_IDENTITY_SECRET=<secret>
Hermes environment:    FAMILY_ASSISTANT_IDENTITY_SECRET=<secret>
```

Enable `family-assistant-identity` in Hermes `plugins.enabled`. Keep the
existing MCP bearer key separate from this HMAC secret.

The plugin uses Hermes' `tools.override` capability to replace model-supplied
identity arguments with the signed messaging sender envelope. Grant that
capability once when enabling the plugin; it persists across gateway restarts.

### Platform support and compatibility

One plugin supports WhatsApp, WhatsApp Cloud, Telegram, Discord, Slack, Signal,
Matrix, and Mattermost through Hermes' normalized gateway events. Unknown
platforms, local CLI sessions, and API/webhook sources are not accepted.
Gateway allowlists and platform authentication must still be configured;
this plugin does not authenticate an arbitrary inbound webhook.

WhatsApp/WhatsApp Cloud retain their existing `provider=hermes`, raw sender ID,
chat ID, and signature format. Other platforms use a namespaced external ID,
such as `telegram:12345` or `discord:12345`, with `provider=hermes` and their
own channel. The same numeric ID on different platforms is not the same
account. No database migration or relinking of existing WhatsApp users is
required. Both gateway capture and the session fallback use this mapping.

Registration can create an account for a namespaced identity. WhatsApp phone
auto-link (`account_link`) remains WhatsApp-only; other platforms must use
the existing verified `identity_link` code flow to link an existing account.
Accounts are not merged based on a display name or a matching numeric ID.
Automatic scheduler delivery is still WhatsApp-only: this plugin does not
add Telegram/Discord/etc. notification delivery or enable their adapters.

For WhatsApp, the original bridge `raw_message.timestamp` (Unix seconds,
including Baileys Long values) is preferred over the gateway receipt timestamp.
Slack uses its original fractional `raw_message.ts`, truncated to Unix seconds.
Other platforms use Hermes' normalized `event.timestamp`: Telegram/Discord/
Signal adapters populate it from message time; adapters without original time
use gateway receipt time instead. If the platform-specific raw timestamp is
absent, the gateway event time is used; malformed raw timestamps are not
silently replaced. The timestamp is captured before dispatch and signed as
`message_at` (Unix seconds) in a `v2` envelope. It stays fixed throughout the
turn even if model processing is delayed. `issued_at` is refreshed for each
tool call and is only the signature freshness timestamp, not the event time.
Without a gateway timestamp the plugin keeps the legacy `v1` format.
The `pre_llm_call` hook exposes the captured original message time so the model
can resolve time-only and relative event dates in the configured timezone,
including when a queued message is processed on another day.

Deploy a backend accepting both versions before updating this plugin on the
server. For activity creation, omit `occurred_at` for "now"; the backend uses
the verified message time. Explicit RFC3339 event times remain supported.
Missing trusted time is rejected, never replaced with processing time. Rolling
back to the old plugin is safe while retaining the new backend; explicit times
remain available. Rolling back the backend requires restoring the old plugin
first so it no longer sends `v2` envelopes.

Run the plugin regression tests from the repository root:

```sh
python3 -B -m unittest discover -s integrations/hermes/family_assistant_identity -p 'test_*.py'
```

For non-interactive deployments, keep the grant in Hermes configuration:

```yaml
plugins:
  enabled:
    - family-assistant-identity
  entries:
    family-assistant-identity:
      allow_tool_override: true
```
