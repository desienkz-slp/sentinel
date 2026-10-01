"""Mock RouterOS REST untuk verifikasi end-to-end MikrotikAdapter.

Meniru RouterOS v7 REST API:
- Auth: HTTP Basic Auth (noc:rahasia)
- GET /rest/system/resource -> identitas router
- GET /rest/ppp/active -> daftar sesi PPPoE aktif
"""
import json
from http.server import BaseHTTPRequestHandler, HTTPServer

USER, PASS = "noc", "rahasia"


class H(BaseHTTPRequestHandler):
    def log_message(self, *a):
        pass

    def _auth_ok(self):
        import base64
        auth = self.headers.get("Authorization", "")
        if auth.startswith("Basic "):
            try:
                decoded = base64.b64decode(auth[6:]).decode()
                u, _, p = decoded.partition(":")
                return u == USER and p == PASS
            except Exception:
                return False
        return False

    def do_GET(self):
        if not self._auth_ok():
            body = b'{"error":401,"message":"invalid user name or password"}'
            self.send_response(401)
            self.send_header("Content-Type", "application/json")
            self.send_header("Content-Length", str(len(body)))
            self.end_headers()
            self.wfile.write(body)
            return

        path = self.path
        if path.startswith("/rest/system/resource"):
            data = [{"version": "7.14.2", "board-name": "CCR1036-8G-2S+",
                     "uptime": "3d2h10m", "cpu-load": "2"}]
        elif path.startswith("/rest/ppp/active"):
            data = [
                {"name": "628123456789@netlayer", "service": "pppoe",
                 "caller-id": "AA:BB:CC:DD:EE:FF", "address": "10.0.0.5", "uptime": "1h2m"},
                {"name": "other-user", "service": "pppoe",
                 "caller-id": "11:22:33:44:55:66", "address": "10.0.0.6", "uptime": "2h"},
            ]
        else:
            body = b'{"error":404}'
            self.send_response(404)
            self.send_header("Content-Length", str(len(body)))
            self.end_headers()
            self.wfile.write(body)
            return

        body = json.dumps(data).encode()
        self.send_response(200)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)


if __name__ == "__main__":
    srv = HTTPServer(("127.0.0.1", 9322), H)
    print("mock-routeros on :9322", flush=True)
    srv.serve_forever()
