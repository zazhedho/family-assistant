import os
import unittest
from datetime import datetime, timezone
from types import SimpleNamespace
from unittest.mock import patch

from integrations.hermes.family_assistant_identity import plugin


def _hermes_is_shared(source, *, group_sessions_per_user=True, thread_sessions_per_user=False):
    """Stand-in mirroring gateway.session.is_shared_multi_user_session."""
    if source.chat_type == "dm":
        return False
    return not (thread_sessions_per_user if source.thread_id else group_sessions_per_user)


def _gateway(group_sessions_per_user=True, thread_sessions_per_user=False):
    return SimpleNamespace(config=SimpleNamespace(
        group_sessions_per_user=group_sessions_per_user,
        thread_sessions_per_user=thread_sessions_per_user,
    ))


PER_USER = _gateway(group_sessions_per_user=True)
SHARED = _gateway(group_sessions_per_user=False)
SECRET_ENV = {"FAMILY_ASSISTANT_IDENTITY_SECRET": "identity-secret"}


class FamilyAssistantIdentityPluginTest(unittest.TestCase):
    def setUp(self):
        plugin._current_identity.set(None)
        self.addCleanup(plugin._current_identity.set, None)
        patcher = patch.object(plugin, "_shared_session_helper", return_value=_hermes_is_shared)
        patcher.start()
        self.addCleanup(patcher.stop)

    def _group_event(self, chat_type="group", thread_id=None):
        return SimpleNamespace(source=SimpleNamespace(
            platform="telegram", user_id="12345", chat_id="group-chat-9876",
            chat_type=chat_type, thread_id=thread_id,
        ))

    def _sign_after_capture(self, event, **kwargs):
        plugin.capture_gateway_identity(event, **kwargs)
        with patch.dict(os.environ, SECRET_ENV):
            return plugin.inject_identity("mcp_family_assistant_space_list", {})

    def test_shared_group_session_is_not_signed_and_warns(self):
        with self.assertLogs(plugin.logger, level="WARNING") as logs:
            result = self._sign_after_capture(self._group_event(), gateway=SHARED)
        self.assertIsNone(result)
        self.assertTrue(plugin._current_identity.get()["shared"])
        self.assertEqual(len(logs.records), 1)
        message = logs.records[0].getMessage()
        self.assertIn("group_sessions_per_user", message)
        self.assertNotIn("12345", message)
        self.assertNotIn("group-chat-9876", message)

    def test_per_user_group_session_is_signed(self):
        result = self._sign_after_capture(self._group_event(), gateway=PER_USER)
        self.assertIsNotNone(result)
        self.assertFalse(plugin._current_identity.get()["shared"])
        envelope = result["args"][plugin.IDENTITY_ARGUMENT_NAME]
        self.assertEqual(envelope["external_id"], "telegram:12345")
        self.assertTrue(plugin.verify_signature("identity-secret", envelope))

    def test_dm_is_signed_even_when_group_sessions_are_shared(self):
        result = self._sign_after_capture(self._group_event(chat_type="dm"), gateway=SHARED)
        self.assertIsNotNone(result)
        self.assertFalse(plugin._current_identity.get()["shared"])

    def test_thread_in_per_user_groups_is_shared_unless_threads_are_per_user(self):
        event = self._group_event(chat_type="thread", thread_id="t1")
        with self.assertLogs(plugin.logger, level="WARNING"):
            self.assertIsNone(self._sign_after_capture(event, gateway=PER_USER))
        gateway = _gateway(group_sessions_per_user=True, thread_sessions_per_user=True)
        self.assertIsNotNone(self._sign_after_capture(event, gateway=gateway))

    def test_unresolvable_shared_state_for_group_fails_closed(self):
        def raising_helper(*args, **kwargs):
            raise RuntimeError("boom")

        cases = {
            "helper unavailable": (None, PER_USER),
            "no gateway": (_hermes_is_shared, None),
            "gateway without config": (_hermes_is_shared, SimpleNamespace()),
            "helper raises": (raising_helper, PER_USER),
        }
        for name, (helper, gateway) in cases.items():
            with self.subTest(name=name):
                with patch.object(plugin, "_shared_session_helper", return_value=helper), \
                        self.assertLogs(plugin.logger, level="WARNING"):
                    self.assertIsNone(self._sign_after_capture(self._group_event(), gateway=gateway))

    def test_unavailable_helper_does_not_affect_dm(self):
        with patch.object(plugin, "_shared_session_helper", return_value=None):
            self.assertIsNotNone(self._sign_after_capture(self._group_event(chat_type="dm")))

    def test_session_fallback_refuses_group_chat_but_signs_dm(self):
        base = {
            "HERMES_SESSION_PLATFORM": "telegram", "HERMES_SESSION_USER_ID": "12345",
            "HERMES_SESSION_CHAT_ID": "chat", **SECRET_ENV,
        }
        for chat_type in ("group", "channel", "thread", ""):
            with self.subTest(chat_type=chat_type):
                with patch.dict(os.environ, {**base, "HERMES_SESSION_CHAT_TYPE": chat_type}):
                    self.assertIsNone(plugin._session_identity())
                    self.assertIsNone(plugin.inject_identity("mcp_family_assistant_space_list", {}))
        with patch.dict(os.environ, {**base, "HERMES_SESSION_CHAT_TYPE": "dm"}):
            self.assertIsNotNone(plugin.inject_identity("mcp_family_assistant_space_list", {}))

    def test_captures_whatsapp_sender_from_gateway_event(self):
        event = SimpleNamespace(
            source=SimpleNamespace(
                platform=SimpleNamespace(value="whatsapp"),
                user_id="6285333320090@s.whatsapp.net",
                user_id_alt="",
                chat_id="120363@g.us",
                chat_type="group",
                thread_id=None,
            )
        )

        result = plugin.capture_gateway_identity(event, gateway=PER_USER, session_store=None)

        self.assertIsNone(result)
        self.assertEqual(
            plugin._current_identity.get(),
            {
                "provider": "hermes",
                "external_id": "6285333320090@s.whatsapp.net",
                "channel": "whatsapp",
                "chat_id": "120363@g.us",
                "chat_type": "group",
                "shared": False,
            },
        )

    def test_message_time_is_captured_signed_and_cannot_be_overridden(self):
        message_at = datetime(2026, 9, 29, 23, 59, tzinfo=timezone.utc)
        event = SimpleNamespace(timestamp=message_at, source=SimpleNamespace(
            platform="whatsapp", user_id="sender", chat_id="group", chat_type="group", thread_id=None,
        ))
        plugin.capture_gateway_identity(event, gateway=PER_USER)
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

    def test_messaging_platforms_sign_distinct_identities_and_message_time(self):
        message_at = datetime(2026, 9, 29, 23, 59, tzinfo=timezone.utc)
        for platform in ("telegram", "discord", "slack", "signal", "matrix", "mattermost"):
            with self.subTest(platform=platform):
                event = SimpleNamespace(timestamp=message_at, source=SimpleNamespace(
                    platform=SimpleNamespace(value=platform), user_id="12345",
                    chat_id="group", chat_type="group", thread_id=None,
                ))
                plugin.capture_gateway_identity(event, gateway=PER_USER)
                with patch.dict(os.environ, {"FAMILY_ASSISTANT_IDENTITY_SECRET": "identity-secret"}):
                    result = plugin.inject_identity("mcp_family_assistant_activity_create", {})
                self.assertIsNotNone(result)
                envelope = result["args"][plugin.IDENTITY_ARGUMENT_NAME]
                self.assertEqual(envelope["external_id"], platform + ":12345")
                self.assertEqual(envelope["channel"], platform)
                self.assertEqual(envelope["chat_id"], "group")
                self.assertEqual(envelope["chat_type"], "group")
                self.assertEqual(envelope["message_at"], 1790726340)
                self.assertTrue(plugin.verify_signature("identity-secret", envelope))
                self.assertNotIn("WhatsApp", plugin.inject_message_time()["context"])

    def test_non_whatsapp_session_fallback_uses_the_same_namespace(self):
        with patch.dict(os.environ, {
            "HERMES_SESSION_PLATFORM": "telegram", "HERMES_SESSION_USER_ID": "12345",
            "HERMES_SESSION_CHAT_ID": "chat", "HERMES_SESSION_CHAT_TYPE": "dm",
            "FAMILY_ASSISTANT_IDENTITY_SECRET": "identity-secret",
        }):
            result = plugin.inject_identity("mcp_family_assistant_space_list", {})
        self.assertIsNotNone(result)
        envelope = result["args"][plugin.IDENTITY_ARGUMENT_NAME]
        self.assertEqual(envelope["external_id"], "telegram:12345")
        self.assertEqual(envelope["version"], "v1")
        self.assertNotIn("message_at", envelope)

    def test_unsupported_platform_or_missing_sender_clears_previous_identity(self):
        for platform, sender in (("local", "12345"), ("api_server", "12345"),
                                 ("unknown", "12345"), ("", "12345"), ("telegram", "")):
            with self.subTest(platform=platform, sender=sender):
                plugin._current_identity.set({"provider": "hermes", "external_id": "previous"})
                plugin.capture_gateway_identity(SimpleNamespace(source=SimpleNamespace(
                    platform=platform, user_id=sender,
                )))
                self.assertIsNone(plugin._current_identity.get())

    def test_slack_prefers_original_fractional_timestamp(self):
        event = SimpleNamespace(
            timestamp=datetime(2026, 9, 30, tzinfo=timezone.utc),
            raw_message={"ts": "1790726340.123456"},
            source=SimpleNamespace(platform="slack", user_id="U123", chat_id="C123", chat_type="group"),
        )
        plugin.capture_gateway_identity(event)
        self.assertIsNotNone(plugin._current_identity.get())
        self.assertEqual(plugin._current_identity.get()["message_at"], "1790726340")

    def test_whatsapp_cloud_retains_existing_raw_identity(self):
        plugin.capture_gateway_identity(SimpleNamespace(source=SimpleNamespace(
            platform="whatsapp_cloud", user_id="6285333320090",
            chat_id="6285333320090", chat_type="dm",
        )))
        self.assertEqual(plugin._current_identity.get()["external_id"], "6285333320090")

    def test_invalid_slack_timestamp_does_not_become_receipt_time(self):
        for timestamp in ("invalid", "NaN", "Infinity", True, "-1", "0"):
            with self.subTest(timestamp=timestamp):
                plugin.capture_gateway_identity(SimpleNamespace(
                    timestamp=datetime(2026, 9, 30, tzinfo=timezone.utc),
                    raw_message={"ts": timestamp},
                    source=SimpleNamespace(platform="slack", user_id="U123"),
                ))
                self.assertNotIn("message_at", plugin._current_identity.get())

    def test_non_whatsapp_raw_timestamp_does_not_override_normalized_time(self):
        plugin.capture_gateway_identity(SimpleNamespace(
            timestamp=datetime(2026, 9, 29, 23, 59, tzinfo=timezone.utc),
            raw_message={"timestamp": 1},
            source=SimpleNamespace(platform="telegram", user_id="12345"),
        ))
        self.assertEqual(plugin._current_identity.get()["message_at"], "1790726340")

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
