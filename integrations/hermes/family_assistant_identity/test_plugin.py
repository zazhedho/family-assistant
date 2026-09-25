import os
import unittest
from types import SimpleNamespace

from integrations.hermes.family_assistant_identity import plugin


class FamilyAssistantIdentityPluginTest(unittest.TestCase):
    def setUp(self):
        plugin._current_identity.set(None)
        self.addCleanup(plugin._current_identity.set, None)

    def test_captures_whatsapp_sender_from_gateway_event(self):
        event = SimpleNamespace(
            source=SimpleNamespace(
                platform=SimpleNamespace(value="whatsapp"),
                user_id="6285333320090@s.whatsapp.net",
                user_id_alt="",
                chat_id="120363@g.us",
                chat_type="group",
            )
        )

        result = plugin.capture_gateway_identity(event)

        self.assertIsNone(result)
        self.assertEqual(
            plugin._current_identity.get(),
            {
                "provider": "hermes",
                "external_id": "6285333320090@s.whatsapp.net",
                "channel": "whatsapp",
                "chat_id": "120363@g.us",
                "chat_type": "group",
            },
        )

    def test_registers_only_documented_extension_points(self):
        calls = []

        class Context:
            def register_hook(self, name, callback):
                calls.append(("hook", name, callback))

            def register_middleware(self, name, callback):
                calls.append(("middleware", name, callback))

        plugin.register(Context())

        self.assertEqual([kind for kind, _, _ in calls], ["hook", "middleware"])
        self.assertEqual([name for _, name, _ in calls], ["pre_gateway_dispatch", "tool_request"])

    def test_injects_signed_identity_only_for_family_assistant_tools(self):
        plugin._current_identity.set(
            {
                "provider": "hermes",
                "external_id": "6285333320090@s.whatsapp.net",
                "channel": "whatsapp",
                "chat_id": "120363@g.us",
                "chat_type": "group",
            }
        )
        old_secret = os.environ.get("FAMILY_ASSISTANT_IDENTITY_SECRET")
        os.environ["FAMILY_ASSISTANT_IDENTITY_SECRET"] = "identity-secret"
        self.addCleanup(self._restore_secret, old_secret)

        result = plugin.inject_identity(
            tool_name="mcp_family_assistant_account_register",
            args={"name": "Zaiduszh", "consent": True},
        )

        self.assertIsNotNone(result)
        identity = result["args"][plugin.IDENTITY_ARGUMENT_NAME]
        self.assertEqual(identity["provider"], "hermes")
        self.assertEqual(identity["external_id"], "6285333320090@s.whatsapp.net")
        self.assertEqual(identity["chat_id"], "120363@g.us")
        self.assertEqual(identity["chat_type"], "group")
        self.assertTrue(plugin.verify_signature("identity-secret", identity))

    def test_does_not_modify_other_tools(self):
        plugin._current_identity.set(
            {"provider": "hermes", "external_id": "sender", "channel": "whatsapp"}
        )
        os.environ["FAMILY_ASSISTANT_IDENTITY_SECRET"] = "identity-secret"
        self.addCleanup(os.environ.pop, "FAMILY_ASSISTANT_IDENTITY_SECRET", None)

        self.assertIsNone(plugin.inject_identity(tool_name="terminal", args={"command": "pwd"}))

    def test_missing_secret_fails_closed_by_omitting_envelope(self):
        plugin._current_identity.set(
            {"provider": "hermes", "external_id": "sender", "channel": "whatsapp"}
        )
        os.environ.pop("FAMILY_ASSISTANT_IDENTITY_SECRET", None)

        self.assertIsNone(
            plugin.inject_identity(
                tool_name="mcp_family_assistant_space_list",
                args={},
            )
        )

    def _restore_secret(self, old_secret):
        if old_secret is None:
            os.environ.pop("FAMILY_ASSISTANT_IDENTITY_SECRET", None)
        else:
            os.environ["FAMILY_ASSISTANT_IDENTITY_SECRET"] = old_secret


if __name__ == "__main__":
    unittest.main()
