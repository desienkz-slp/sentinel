#!/usr/bin/env python3
"""
Gateway WhatsApp TIRUAN untuk menguji integrasi ai-noc-go <-> WhatsApp Gateway.

Meniru kontrak whatsapp/app/server.js + app/webhook.js dari project ai-noc:
  GET  /api/whatsapp/status   -> {"status": "connected", ...}
  GET  /api/whatsapp/qr       -> {"status": ..., "qr": ...}
  POST /api/whatsapp/connect  -> {"success": true}
  POST /api/whatsapp/logout   -> {"success": true}
  POST /api/whatsapp/send     -> {"success": true, "message_id": ...}

Plus: `--simulate "<pesan>"` mengirim payload MASUK ke ai-noc-go persis seperti
yang dilakukan webhook.js, lalu mencetak balasan yang akan dikirim ke WhatsApp.
"""
import argparse
import json
import sys
import threading
import time
import urllib.request
from http.server import BaseHTTPRequestHandler, HTTPServer

SENT = []  # pesan yang dikirim ai-noc-go -> WhatsApp (untuk verifikasi)


class Handler(BaseHTTPRequestHandler):
    def log_message(self, *a):
        pass

    def _json(self, code, obj):
        body = json.dumps(obj).encode()
        self.send_response(code)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def do_GET(self):
        if self.path == "/api/whatsapp/status":
            self._json(200, {"status": "connected", "connected": True,
                             "user": {"id": "628123456789@s.whatsapp.net", "name": "Mock Gateway"}})
        elif self.path == "/api/whatsapp/qr":
            self._json(200, {"status": "connected", "qr": None,
                             "note": "mock: sesi sudah tertaut"})
        elif self.path == "/api/whatsapp/health":
            self._json(200, {"healthy": True})
        else:
            self._json(404, {"error": "not found"})

    def do_POST(self):
        n = int(self.headers.get("Content-Length") or 0)
        raw = self.rfile.read(n) if n else b"{}"
        try:
            data = json.loads(raw or b"{}")
        except Exception:
            data = {}

        if self.path == "/api/whatsapp/send":
            SENT.append(data)
            print(f"[MOCK-GW] >>> TERKIRIM KE WHATSAPP {data.get('to')}:")
            print("           " + (data.get("message") or "").replace("\n", "\n           "))
            self._json(200, {"success": True, "message_id": f"mock_{int(time.time()*1000)}"})
        elif self.path in ("/api/whatsapp/connect", "/api/whatsapp/reconnect"):
            self._json(200, {"success": True, "status": "connected"})
        elif self.path == "/api/whatsapp/logout":
            self._json(200, {"success": True, "status": "disconnected"})
        else:
            self._json(404, {"error": "not found"})


def simulate(target: str, payload: dict):
    """Tiru webhook.js: POST payload ke N8N_WEBHOOK_URL lalu balas 'reply' ke WA."""
    req = urllib.request.Request(
        target, data=json.dumps(payload).encode(),
        headers={"Content-Type": "application/json", "X-AI-NOC-Source": "whatsapp-gateway"},
        method="POST")
    with urllib.request.urlopen(req, timeout=300) as r:
        resp = json.loads(r.read())

    print(f"[MOCK-GW] <<< RESPONS ai-noc-go: accepted={resp.get('accepted')} "
          f"verdict={resp.get('verdict')} conf={resp.get('confidence')} "
          f"engine={resp.get('engine')} {resp.get('elapsed_ms')}ms")
    if resp.get("note"):
        print(f"[MOCK-GW]     note: {resp['note']}")
    reply = resp.get("reply")
    if reply:
        # Inilah yang dilakukan webhook.js: sessionManager.sendMessage(chat_id, reply)
        print(f"[MOCK-GW] >>> TERKIRIM KE WHATSAPP {payload.get('chat_id')}:")
        print("           " + reply.replace("\n", "\n           "))
    else:
        print("[MOCK-GW]     (tidak ada 'reply' -> gateway tidak mengirim apa pun)")
    return resp


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--port", type=int, default=3001)
    ap.add_argument("--target", default="http://127.0.0.1:8090/api/wa/webhook")
    ap.add_argument("--simulate", help="kirim pesan WhatsApp masuk ke ai-noc-go")
    ap.add_argument("--sender", default="628123456789@s.whatsapp.net")
    args = ap.parse_args()

    if args.simulate:
        simulate(args.target, {
            "message_id": f"sim_{int(time.time()*1000)}",
            "chat_id": args.sender, "sender": args.sender,
            "sender_name": "Pelanggan Uji", "message": args.simulate,
            "timestamp": time.strftime("%Y-%m-%dT%H:%M:%S%z"), "type": "text",
        })
        return

    srv = HTTPServer(("127.0.0.1", args.port), Handler)
    print(f"[MOCK-GW] gateway tiruan di http://127.0.0.1:{args.port} "
          f"(status=connected, siap menerima /api/whatsapp/send)")
    threading.Thread(target=srv.serve_forever, daemon=True).start()
    try:
        while True:
            time.sleep(1)
    except KeyboardInterrupt:
        print(f"\n[MOCK-GW] total pesan keluar: {len(SENT)}")


if __name__ == "__main__":
    main()
