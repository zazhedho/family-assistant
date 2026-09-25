"""Signed sender identity middleware for the Family Assistant MCP server.

This plugin is deliberately outside the Hermes source tree. It uses the public
gateway hook and tool-request middleware surfaces, then lets Family Assistant
verify the envelope at its own trust boundary.
"""

from __future__ import annotations

import hashlib
import hmac
import os
import secrets
import time
from contextvars import ContextVar
from typing import Any


IDENTITY_ARGUMENT_NAME = "__hermes_identity"
IDENTITY_VERSION = "v1"
_IDENTITY_SECRET_ENV = "FAMILY_ASSISTANT_IDENTITY_SECRET"
_TARGET_SERVER_ENV = "FAMILY_ASSISTANT_MCP_SERVER"
_current_identity: ContextVar[dict[str, str] | None] = ContextVar(
    "family_assistant_identity", default=None
)


def register(ctx: Any) -> None:
    ctx.register_hook("pre_gateway_dispatch", capture_gateway_identity)
    ctx.register_middleware("tool_request", inject_identity)


def capture_gateway_identity(event: Any, **_: Any) -> None:
    source = getattr(event, "source", None)
    platform = _text(getattr(source, "platform", ""))
    if platform not in {"whatsapp", "whatsapp_cloud"}:
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
    _current_identity.set(_identity(platform, external_id, chat_id, chat_type))
    return None


def inject_identity(tool_name: str = "", args: dict[str, Any] | None = None, **_: Any) -> dict[str, Any] | None:
    if not _is_target_tool(tool_name):
        return None

    secret = os.environ.get(_IDENTITY_SECRET_ENV, "").strip()
    identity = _session_identity()
    if not secret or not identity:
        # Family Assistant is configured to fail closed when this envelope is required.
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


def _session_identity() -> dict[str, str] | None:
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
    if platform not in {"whatsapp", "whatsapp_cloud"} or not external_id:
        return None
    return _identity(platform, external_id, chat_id or external_id, chat_type)


def _is_target_tool(tool_name: str) -> bool:
    server = os.environ.get(_TARGET_SERVER_ENV, "family_assistant").strip() or "family_assistant"
    return tool_name.startswith(f"mcp_{server}_") or tool_name.startswith(f"mcp__{server}__")


def _sign(secret: str, envelope: dict[str, Any]) -> str:
    payload = "\x00".join(
        (
            IDENTITY_VERSION,
            str(envelope.get("provider") or ""),
            str(envelope.get("external_id") or ""),
            str(envelope.get("channel") or ""),
            str(envelope.get("chat_id") or ""),
            str(envelope.get("chat_type") or ""),
            str(envelope.get("issued_at") or ""),
            str(envelope.get("nonce") or ""),
        )
    )
    return hmac.new(secret.encode(), payload.encode(), hashlib.sha256).hexdigest()


def _text(value: Any) -> str:
    value = getattr(value, "value", value)
    return str(value or "").strip()


def _identity(platform: str, external_id: str, chat_id: str, chat_type: str) -> dict[str, str]:
    return {
        "provider": "hermes",
        "external_id": external_id,
        "channel": platform,
        "chat_id": chat_id,
        "chat_type": chat_type,
    }
