# Family Assistant identity plugin

This is a standalone Hermes plugin. It does not modify the Hermes installation.

Install it as a symlink so project updates are picked up without copying files:

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
