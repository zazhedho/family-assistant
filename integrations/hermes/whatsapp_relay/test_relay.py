import hashlib
import hmac
import json
import tempfile
import threading
import unittest
import urllib.error
import urllib.request
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path

import relay


class Bridge(BaseHTTPRequestHandler):
    protocol_version = "HTTP/1.1"
    token = "first-token"
    sends = []
    valid_proof = True

    def log_message(self, *_args):
        pass

    def do_GET(self):
        challenge = self.headers["X-Hermes-Bridge-Challenge"]
        proof = hmac.new(self.token.encode(), challenge.encode(), hashlib.sha256).hexdigest()
        body = json.dumps({"authProof": proof if self.valid_proof else "invalid"}).encode()
        self.send_response(200)
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def do_POST(self):
        self.sends.append((self.headers.get("Authorization"), self.rfile.read(int(self.headers["Content-Length"]))))
        self.send_response(204)
        self.send_header("Content-Length", "0")
        self.end_headers()


class RelayTest(unittest.TestCase):
    def setUp(self):
        Bridge.token, Bridge.sends, Bridge.valid_proof = "first-token", [], True
        self.tmp = tempfile.TemporaryDirectory()
        self.token = Path(self.tmp.name) / "token"
        self.token.write_text(Bridge.token)
        self.bridge = ThreadingHTTPServer(("127.0.0.1", 0), Bridge)
        self.server = relay.make_server("127.0.0.1", 0, "relay-secret", self.token, self.bridge.server_port)
        for server in (self.bridge, self.server):
            threading.Thread(target=server.serve_forever, daemon=True).start()

    def tearDown(self):
        for server in (self.server, self.bridge):
            server.shutdown()
            server.server_close()
        self.tmp.cleanup()

    def request(self, secret="relay-secret", path="/send"):
        req = urllib.request.Request(
            f"http://127.0.0.1:{self.server.server_port}{path}",
            data=json.dumps({"chatId": "chat-1", "message": "hello"}).encode(),
            headers={"Authorization": "Bearer " + secret, "Content-Type": "application/json"},
        )
        try:
            with urllib.request.urlopen(req) as response:
                return response.status
        except urllib.error.HTTPError as exc:
            status = exc.code
            exc.close()
            return status

    def test_valid_send_and_rotated_token(self):
        self.assertEqual(self.request(), 204)
        Bridge.token = "second-token"
        self.token.write_text(Bridge.token)
        self.assertEqual(self.request(), 204)
        self.assertEqual([x[0] for x in Bridge.sends], ["Bearer first-token", "Bearer second-token"])

    def test_wrong_secret_and_wrong_route_do_not_send(self):
        self.assertEqual(self.request("bad"), 401)
        self.assertEqual(self.request(path="/other"), 404)
        self.assertEqual(Bridge.sends, [])

    def test_wrong_bridge_proof_never_sends_token(self):
        Bridge.valid_proof = False
        self.assertEqual(self.request(), 502)
        self.assertEqual(Bridge.sends, [])


if __name__ == "__main__":
    unittest.main()
