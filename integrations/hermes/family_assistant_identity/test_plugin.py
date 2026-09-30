import os
import unittest
from datetime import datetime, timezone
from types import SimpleNamespace
from unittest.mock import patch

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

    def test_message_time_is_captured_signed_and_cannot_be_overridden(self):
        message_at = datetime(2026, 9, 29, 23, 59, tzinfo=timezone.utc)
        event = SimpleNamespace(timestamp=message_at, source=SimpleNamespace(
            platform="whatsapp", user_id="sender", chat_id="group", chat_type="group",
        ))
        plugin.capture_gateway_identity(event)
        with patch.dict(os.environ, {"FAMILY_ASSISTANT_IDENTITY_SECRET": "identity-secret"}):
            result = plugin.inject_identity("mcp_family_assistant_activity_create", {
                "kind": "note", "note": "now", "__hermes_identity": {"message_at": 1},
            })
        envelope = result["args"][plugin.IDENTITY_ARGUMENT_NAME]
        self.assertEqual(envelope["version"], "v2")
        self.assertEqual(envelope["message_at"], int(message_at.timestamp()))
        self.assertTrue(plugin.verify_signature("identity-secret", envelope))
        envelope["message_at"] -= 1
        self.assertFalse(plugin.verify_signature("identity-secret", envelope))

    def test_prefers_original_bridge_timestamp_over_gateway_receipt_time(self):
        expected = 1799999400
        for timestamp in (expected, str(expected), {"low": expected, "high": 0, "unsigned": True}):
            with self.subTest(timestamp=timestamp):
                event = SimpleNamespace(
                    timestamp=datetime(2026, 9, 30, 8, 26, tzinfo=timezone.utc),
                    raw_message={"timestamp": timestamp},
                    source=SimpleNamespace(platform="whatsapp", user_id="sender", chat_id="group", chat_type="group"),
                )
                plugin.capture_gateway_identity(event)
                self.assertEqual(plugin._current_identity.get()["message_at"], str(expected))

    def test_invalid_bridge_timestamp_is_not_replaced_with_processing_time(self):
        for timestamp in ("invalid", True, -1, 0, 10**30, {"low": "bad", "high": 0}):
            with self.subTest(timestamp=timestamp):
                event = SimpleNamespace(
                    timestamp=datetime(2026, 9, 30, 8, 26, tzinfo=timezone.utc),
                    raw_message={"timestamp": timestamp},
                    source=SimpleNamespace(platform="whatsapp", user_id="sender", chat_id="group", chat_type="group"),
                )
                plugin.capture_gateway_identity(event)
                self.assertNotIn("message_at", plugin._current_identity.get())

    def test_injects_message_time_context_without_using_processing_clock(self):
        self.assertIsNone(plugin.inject_message_time())
        message_at = datetime(2026, 9, 29, 23, 59, tzinfo=timezone.utc)
        event = SimpleNamespace(timestamp=message_at, source=SimpleNamespace(
            platform="whatsapp", user_id="sender", chat_id="group", chat_type="group",
        ))
        plugin.capture_gateway_identity(event)
        context = plugin.inject_message_time()["context"]
        self.assertIn("2026-09-29T23:59:00+00:00", context)
        self.assertIn("message time", context)
        self.assertIn("configured timezone", context)

    def test_v2_signature_matches_backend_wire_format(self):
        envelope = {
            "version": "v2", "provider": "hermes", "external_id": "6285333320090@s.whatsapp.net",
            "channel": "whatsapp", "chat_id": "120363@g.us", "chat_type": "group",
            "issued_at": 1800000000, "nonce": "nonce-1", "message_at": 1799999400,
        }
        self.assertEqual(plugin._sign("identity-secret", envelope),
                         "f531954fea544f2ba8aa50901e448e837182ac4f68ef6ea5c4952c69836d5606")

    def test_registers_only_documented_extension_points(self):
        calls = []

        class Context:
            def register_hook(self, name, callback):
                calls.append(("hook", name, callback))

            def register_middleware(self, name, callback):
                calls.append(("middleware", name, callback))

        plugin.register(Context())

        self.assertEqual([kind for kind, _, _ in calls], ["hook", "hook", "middleware"])
        self.assertEqual([name for _, name, _ in calls], ["pre_gateway_dispatch", "pre_llm_call", "tool_request"])

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

    def test_matches_both_prefixes_only_for_configured_server(self):
        with patch.dict(os.environ, {"FAMILY_ASSISTANT_MCP_SERVER": "custom"}):
            for name, expected in (
                ("mcp_custom_activity_create", True),
                ("mcp__custom__activity_create", True),
                ("mcp_family_assistant_activity_create", False),
                ("terminal", False),
            ):
                with self.subTest(name=name):
                    self.assertEqual(plugin._is_target_tool(name), expected)

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
