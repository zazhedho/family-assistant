# Family Assistant identity plugin

This is a standalone Hermes plugin. It does not modify the Hermes installation.

For an image-only VPS, follow the [production deployment guide](../../../README.md)
and copy the plugin files; the application repository is not required there.
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
identity arguments with the signed WhatsApp sender envelope. Grant that
capability once when enabling the plugin; it persists across gateway restarts.

The original bridge `raw_message.timestamp` (Unix seconds, including Baileys
Long values) is preferred over the gateway receipt timestamp. If it is absent,
the gateway event's receipt time is used; a malformed bridge timestamp is not
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

For non-interactive deployments, keep the grant in Hermes configuration:

```yaml
plugins:
  enabled:
    - family-assistant-identity
  entries:
    family-assistant-identity:
      allow_tool_override: true
```
