import hashlib
import hmac
import http.client
import json
import os
import secrets
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path

MAX_BODY = 65536


def make_server(host, port, secret, token_path, bridge_port):
    class Handler(BaseHTTPRequestHandler):
        def setup(self):
            super().setup()
            self.connection.settimeout(5)

        def log_message(self, *_args):
            pass

        def reject(self, status):
            self.send_response(status)
            self.send_header("Content-Length", "0")
            self.end_headers()
            self.close_connection = True

        def do_POST(self):
            if self.path != "/send":
                return self.reject(404)
            if not hmac.compare_digest(self.headers.get("Authorization", "").encode(), ("Bearer " + secret).encode()):
                return self.reject(401)
            try:
                size = int(self.headers.get("Content-Length", "0"))
                if size <= 0 or size > MAX_BODY:
                    return self.reject(400)
                payload = self.rfile.read(size)
                data = json.loads(payload)
                if not isinstance(data, dict) or not all(isinstance(data.get(k), str) and data[k].strip() for k in ("chatId", "message")):
                    return self.reject(400)
                token = token_path.read_text(encoding="utf-8-sig").strip()
                if not token:
                    return self.reject(502)
                challenge = secrets.token_urlsafe(24)
                conn = http.client.HTTPConnection("127.0.0.1", bridge_port, timeout=4)
                try:
                    conn.request("GET", "/auth-proof", headers={"X-Hermes-Bridge-Challenge": challenge})
                    response = conn.getresponse()
                    proof_body = json.loads(response.read(MAX_BODY))
                    proof = proof_body.get("authProof") if isinstance(proof_body, dict) else None
                    expected = hmac.new(token.encode(), challenge.encode(), hashlib.sha256).hexdigest()
                    if response.status != 200 or response.will_close or conn.sock is None or not isinstance(proof, str) or not hmac.compare_digest(proof.encode(), expected.encode()):
                        return self.reject(502)
                    conn.request("POST", "/send", body=payload, headers={"Content-Type": "application/json", "Authorization": "Bearer " + token})
                    response = conn.getresponse()
                    response.read(MAX_BODY)
                    self.reject(204 if 200 <= response.status < 300 else 502)
                finally:
                    conn.close()
            except (OSError, ValueError, TypeError, json.JSONDecodeError, http.client.HTTPException):
                self.reject(502)

    return ThreadingHTTPServer((host, port), Handler)


if __name__ == "__main__":
    relay_secret = os.environ["WHATSAPP_RELAY_SECRET"].strip()
    if not relay_secret:
        raise SystemExit("WHATSAPP_RELAY_SECRET must be set")
    make_server(
        os.environ["WHATSAPP_RELAY_LISTEN_ADDRESS"],
        int(os.environ["WHATSAPP_RELAY_PORT"]),
        relay_secret,
        Path(os.environ["WHATSAPP_BRIDGE_TOKEN_FILE"]),
        int(os.environ.get("WHATSAPP_BRIDGE_PORT", "2000")),
    ).serve_forever()
