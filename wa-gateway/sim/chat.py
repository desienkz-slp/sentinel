#!/usr/bin/env python3
"""Kirim pesan WA lewat gateway lokal ke agent AI, tampilkan balasannya.
  python3 wa-gateway/sim/chat.py <noc|admin|super|NOMOR> "pesan"
Peran uji: super=628111000001  noc=628111000002  admin=628111000003  (lainnya = pelanggan)"""
import json, sys, urllib.request
GW = "http://127.0.0.1:3011"
WHO = {"super": "628111000001", "noc": "628111000002", "admin": "628111000003"}
def post(path, body):
    r = urllib.request.Request(GW + path, json.dumps(body).encode(), {"Content-Type": "application/json"})
    return json.loads(urllib.request.urlopen(r, timeout=200).read())
who, text = sys.argv[1], " ".join(sys.argv[2:])
num = WHO.get(who, who)
before = len(json.loads(urllib.request.urlopen(f"{GW}/sim/outbox?to={num}").read()))
r = post("/sim/inbound", {"from": num, "name": who, "text": text})
out = json.loads(urllib.request.urlopen(f"{GW}/sim/outbox?to={num}").read())[before:]
print(f"[{who} {num}] >> {text}")
print(f"   engine={r.get('engine')} accepted={r.get('accepted')} {r.get('ms')}ms note={r.get('note')}")
for m in out: print(f"   << ({m['part']}/{m['parts']}) {m['text']}")
if not out: print("   << (tidak ada balasan ke WhatsApp)")
