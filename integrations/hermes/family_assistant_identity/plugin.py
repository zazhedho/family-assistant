"""Signed sender identity middleware for the Family Assistant MCP server.

This plugin is deliberately outside the Hermes source tree. It uses the public
gateway hook and tool-request middleware surfaces, then lets Family Assistant
verify the envelope at its own trust boundary.
"""

from __future__ import annotations

import hashlib
import hmac
import logging
import os
import secrets
import time
from contextvars import ContextVar
from datetime import datetime, timezone
from decimal import Decimal, InvalidOperation
from typing import Any


logger = logging.getLogger(__name__)

IDENTITY_ARGUMENT_NAME = "__hermes_identity"
IDENTITY_VERSION = "v1"
_IDENTITY_SECRET_ENV = "FAMILY_ASSISTANT_IDENTITY_SECRET"
_TARGET_SERVER_ENV = "FAMILY_ASSISTANT_MCP_SERVER"
_WHATSAPP_PLATFORMS = {"whatsapp", "whatsapp_cloud"}
_MESSAGE_PLATFORMS = _WHATSAPP_PLATFORMS | {
    "telegram", "discord", "slack", "signal", "matrix", "mattermost",
}
_current_identity: ContextVar[dict[str, Any] | None] = ContextVar(
    "family_assistant_identity", default=None
)


def register(ctx: Any) -> None:
    ctx.register_hook("pre_gateway_dispatch", capture_gateway_identity)
    ctx.register_hook("pre_llm_call", inject_message_time)
    ctx.register_middleware("tool_request", inject_identity)


def capture_gateway_identity(event: Any, gateway: Any = None, **_: Any) -> None:
    source = getattr(event, "source", None)
    platform = _text(getattr(source, "platform", ""))
    if platform not in _MESSAGE_PLATFORMS:
        _current_identity.set(None)
        return None

    external_id = _text(getattr(source, "user_id", "")) or _text(
        getattr(source, "user_id_alt", "")
    )
    if not external_id:
        _current_identity.set(None)
        return None
    chat_id = _text(getattr(source, "chat_id", "")) or external_id
    chat_type = _text(getattr(source, "chat_type", ""))
    identity = _identity(platform, external_id, chat_id, chat_type)
    identity["shared"] = _is_shared_session(source, chat_type, gateway)
    message_at = _message_timestamp(event)
    if message_at is not None:
        identity["message_at"] = str(message_at)
    _current_identity.set(identity)
    return None


def _shared_session_helper() -> Any:
    """Hermes' own shared-session predicate, or None on releases that lack it."""
    try:
        from gateway.session import is_shared_multi_user_session
    except ImportError:
        return None
    return is_shared_multi_user_session


def _is_shared_session(source: Any, chat_type: str, gateway: Any) -> bool:
    """True when the session for ``source`` may serve several members at once.

    Hermes steers/queues follow-up messages from other members into the running
    turn of a shared session without running ``pre_gateway_dispatch`` for them,
    so the captured sender could be the wrong person. Fails closed: anything
    that cannot be positively resolved for a non-DM chat counts as shared.
    """
    if chat_type == "dm":
        return False
    helper = _shared_session_helper()
    config = getattr(gateway, "config", None)
    if helper is None or config is None:
        return True
    try:
        return bool(helper(
            source,
            group_sessions_per_user=getattr(config, "group_sessions_per_user", True),
            thread_sessions_per_user=getattr(config, "thread_sessions_per_user", False),
        ))
    except Exception:
        return True


def inject_message_time(**_: Any) -> dict[str, str] | None:
    identity = _current_identity.get()
    if not identity or not identity.get("message_at"):
        return None
    timestamp = datetime.fromtimestamp(int(identity["message_at"]), timezone.utc).isoformat()
    return {"context": (
        f"[Family Assistant] Message reference time ({identity['channel']}): {timestamp}. "
        "This is not processing time. For event requests, resolve today/yesterday "
        "and time-only dates from this message time in the configured timezone "
        "shown by live-time, unless the user specifies a date. Omit occurred_at "
        "for now/no stated time: backend uses the signed message timestamp. "
        "Never reuse an old record's created_at/updated_at as a new event time."
    )}


def _message_timestamp(event: Any) -> int | None:
    raw = getattr(event, "raw_message", None)
    platform = _text(getattr(getattr(event, "source", None), "platform", ""))
    value = None
    if isinstance(raw, dict):
        if platform in _WHATSAPP_PLATFORMS:
            value = raw.get("timestamp")
        elif platform == "slack" and raw.get("ts") is not None:
            try:
                value = int(Decimal(str(raw["ts"])))
            except (InvalidOperation, ValueError, OverflowError):
                return None
    if value is None:
        timestamp = getattr(event, "timestamp", None)
        if not isinstance(timestamp, datetime):
            return None
        value = int(timestamp.timestamp())
    return _parse_message_timestamp(value)


def _parse_message_timestamp(value: Any) -> int | None:
    if isinstance(value, dict):
        low, high = value.get("low"), value.get("high")
        if type(low) is not int or type(high) is not int:
            return None
        value = (high << 32) | (low & 0xFFFFFFFF)
    if isinstance(value, bool) or not isinstance(value, (int, str)):
        return None
    try:
        parsed = int(value)
        datetime.fromtimestamp(parsed, timezone.utc)
    except (ValueError, OverflowError, OSError):
        return None
    return parsed if parsed > 0 else None


def inject_identity(tool_name: str = "", args: dict[str, Any] | None = None, **_: Any) -> dict[str, Any] | None:
    if not _is_target_tool(tool_name):
        return None

    secret = os.environ.get(_IDENTITY_SECRET_ENV, "").strip()
    identity = _session_identity()
    if not secret or not identity:
        # Family Assistant is configured to fail closed when this envelope is required.
        return None
    if identity.get("shared"):
        logger.warning(
            "family-assistant-identity: refusing to sign %s for a shared multi-user "
            "session (platform=%s chat=...%s). Set group_sessions_per_user: true so "
            "each member gets their own session; the backend will reject this call.",
            tool_name, identity.get("channel", ""), str(identity.get("chat_id", ""))[-4:],
        )
        return None

    envelope = {
        "version": IDENTITY_VERSION,
        "provider": identity["provider"],
        "external_id": identity["external_id"],
        "channel": identity["channel"],
        "chat_id": identity.get("chat_id", ""),
        "chat_type": identity.get("chat_type", ""),
        "issued_at": int(time.time()),
        "nonce": secrets.token_urlsafe(18),
    }
    if identity.get("message_at"):
        envelope["version"] = "v2"
        envelope["message_at"] = int(identity["message_at"])
    envelope["signature"] = _sign(secret, envelope)
    rewritten = dict(args or {})
    # Overwrite any model-supplied value; the model never chooses its identity.
    rewritten[IDENTITY_ARGUMENT_NAME] = envelope
    return {
        "args": rewritten,
        "source": "family-assistant-identity",
        "reason": "signed sender identity",
    }


def verify_signature(secret: str, envelope: dict[str, Any]) -> bool:
    signature = str(envelope.get("signature") or "")
    if not signature:
        return False
    expected = _sign(secret, envelope)
    return hmac.compare_digest(signature, expected)


def _session_identity() -> dict[str, Any] | None:
    current = _current_identity.get()
    if current:
        return current

    # Current Hermes exposes the session context through this helper. The fallback keeps the
    # plugin compatible with older releases that still bridge session values to the environment.
    try:
        from gateway.session_context import get_session_env

        platform = _text(get_session_env("HERMES_SESSION_PLATFORM", ""))
        external_id = _text(get_session_env("HERMES_SESSION_USER_ID", "")) or _text(
            get_session_env("HERMES_SESSION_USER_ID_ALT", "")
        )
        chat_id = _text(get_session_env("HERMES_SESSION_CHAT_ID", ""))
        chat_type = _text(get_session_env("HERMES_SESSION_CHAT_TYPE", ""))
    except Exception:
        platform = _text(os.environ.get("HERMES_SESSION_PLATFORM", ""))
        external_id = _text(os.environ.get("HERMES_SESSION_USER_ID", "")) or _text(
            os.environ.get("HERMES_SESSION_USER_ID_ALT", "")
        )
        chat_id = _text(os.environ.get("HERMES_SESSION_CHAT_ID", ""))
        chat_type = _text(os.environ.get("HERMES_SESSION_CHAT_TYPE", ""))
    if platform not in _MESSAGE_PLATFORMS or not external_id:
        return None
    if chat_type != "dm":
        # This fallback can be a stale session creator (internal/queued events), which is
        # not necessarily the member who asked. Only a DM has exactly one possible sender.
        return None
    return _identity(platform, external_id, chat_id or external_id, chat_type)


def _is_target_tool(tool_name: str) -> bool:
    server = os.environ.get(_TARGET_SERVER_ENV, "family_assistant").strip() or "family_assistant"
    return tool_name.startswith((f"mcp_{server}_", f"mcp__{server}__"))


def _sign(secret: str, envelope: dict[str, Any]) -> str:
    payload = "\x00".join(
        (
            str(envelope.get("version") or IDENTITY_VERSION),
            str(envelope.get("provider") or ""),
            str(envelope.get("external_id") or ""),
            str(envelope.get("channel") or ""),
            str(envelope.get("chat_id") or ""),
            str(envelope.get("chat_type") or ""),
            str(envelope.get("issued_at") or ""),
            str(envelope.get("nonce") or ""),
        )
    )
    if envelope.get("version") == "v2":
        payload += "\x00" + str(envelope.get("message_at") or "")
    return hmac.new(secret.encode(), payload.encode(), hashlib.sha256).hexdigest()


def _text(value: Any) -> str:
    value = getattr(value, "value", value)
    return str(value or "").strip()


def _identity(platform: str, external_id: str, chat_id: str, chat_type: str) -> dict[str, Any]:
    return {
        "provider": "hermes",
        "external_id": external_id if platform in _WHATSAPP_PLATFORMS else f"{platform}:{external_id}",
        "channel": platform,
        "chat_id": chat_id,
        "chat_type": chat_type,
    }
