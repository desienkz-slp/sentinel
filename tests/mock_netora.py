"""Mock NETORA billing untuk verifikasi end-to-end BillingAdapter.

Simulasi kontrak NOC_AGENT_API.md:
- Auth: header X-NOC-API-Key == "secret-noc-key-123"
- GET /api/noc/v1/customers?search=&per_page=1
"""
import json
from http.server import BaseHTTPRequestHandler, HTTPServer

KEY = "secret-noc-key-123"


class H(BaseHTTPRequestHandler):
    def log_message(self, *a):
        pass

    def do_GET(self):
        if self.headers.get("X-NOC-API-Key") != KEY:
            body = json.dumps({"status": "error", "message": "Unauthenticated."}).encode()
            self.send_response(401)
            self.send_header("Content-Type", "application/json")
            self.send_header("Content-Length", str(len(body)))
            self.end_headers()
            self.wfile.write(body)
            return
        path = self.path
        if path.startswith("/api/noc/v1/customers"):
            # Hitung jumlah data: selalu kembalikan total 1234 pelanggan.
            body = json.dumps({
                "status": "success",
                "data": [{
                    "id": 1, "username": "demo-001", "name": "Demo User",
                    "phone": "6281333678765",
                    "address": "Jl. Contoh No. 1",
                    "coordinate": {"latitude": -7.98, "longitude": 112.63},
                    "area": {"id": 1, "name": "Malang"}, "odp": {"id": 2, "name": "ODP-A"},
                    "router": {"id": 3, "name": "RTR-01", "host": "10.0.0.1"},
                    "radius": {"id": 4, "name": "RADIUS-01"},
                    "package": {"id": 5, "name": "Paket 20M", "price": 150000},
                    "sales": {"id": 6, "name": "Sales A"},
                    "status": "active", "is_isolated": False,
                    "created_at": "2025-01-01T00:00:00Z", "updated_at": "2025-01-01T00:00:00Z",
                }],
                "meta": {"current_page": 1, "per_page": 1, "total": 1234, "last_page": 1234},
            }).encode()
        else:
            body = json.dumps({"status": "error", "message": "not found"}).encode()
            self.send_response(404)
            self.send_header("Content-Type", "application/json")
            self.send_header("Content-Length", str(len(body)))
            self.end_headers()
            self.wfile.write(body)
            return
        self.send_response(200)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)


if __name__ == "__main__":
    srv = HTTPServer(("127.0.0.1", 9321), H)
    print("mock-netora on :9321", flush=True)
    srv.serve_forever()
