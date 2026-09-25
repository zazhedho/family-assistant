# Family Assistant identity plugin

This is a standalone Hermes plugin. It does not modify the Hermes installation.

For an image-only VPS, follow the [production deployment guide](../../../README.md)
and copy the plugin files; the application repository is not required there.
For a local checkout, install it as a symlink so project updates are picked up:

```sh
mkdir -p "$HERMES_HOME/plugins"
ln -sfn "$PWD/integrations/hermes/family_assistant_identity" \
  "$HERMES_HOME/plugins/family-assistant-identity"
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

For non-interactive deployments, keep the grant in Hermes configuration:

```yaml
plugins:
  enabled:
    - family-assistant-identity
  entries:
    family-assistant-identity:
      allow_tool_override: true
```
