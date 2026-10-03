#!/usr/bin/env python3
"""Uji komunikasi end-to-end: gateway WA lokal (simulasi) -> NOC Sentinel lokal.
Semua nomor sintetis. Tidak menyentuh WhatsApp/server produksi."""
import json, sys, time, urllib.request

GW = "http://127.0.0.1:3011"
SENT = "http://127.0.0.1:8190"
TOK = {"Authorization": "Bearer uji-lokal", "Content-Type": "application/json"}
SUPER, NOC, ADMIN, CUST = "628111000001", "628111000002", "628111000003", "628999000001"

def call(url, body=None, headers=None, t=200):
    req = urllib.request.Request(url, data=json.dumps(body).encode() if body is not None else None,
                                 headers=headers or {"Content-Type": "application/json"})
    try:
        with urllib.request.urlopen(req, timeout=t) as r: return json.loads(r.read())
    except urllib.error.HTTPError as e: return {"_http": e.code, "_body": e.read().decode()[:200]}

def wa(frm, text, name="Sim"):
    return call(GW + "/sim/inbound", {"from": frm, "name": name, "text": text})

def outbox(to): return call(GW + f"/sim/outbox?to={to}")

RUN = "".join(chr(97 + int(c)) for c in str(int(time.time()))[-6:])  # bikin pesan unik per putaran agar dedup tidak mengganggu
results = []
def check(name, ok, detail=""):
    results.append(ok); print(("PASS " if ok else "FAIL ") + name + (f"  [{detail}]" if detail else ""))

def text_of(r):  # balasan yang sampai ke "WhatsApp"
    return " ".join(m["text"] for m in (outbox(r_to[0]) if False else []))

call(GW + "/sim/reset", {})

# 1. Pelanggan tak dikenal -> dilayani sebagai pelanggan, balasan <=500 per pesan
r = wa(CUST, f"halo, internet saya lambat ({RUN})")
ob = outbox(CUST)
check("1 pelanggan: ada balasan", r.get("had_reply") and len(ob) >= 1, f"engine={r.get('engine')} ms={r.get('ms')}")
check("1 pelanggan: tiap pesan <=500 char", all(len(m["text"]) <= 500 for m in ob), f"{[len(m['text']) for m in ob]}")
low = " ".join(m["text"] for m in ob).lower()
check("1 pelanggan: tidak menyebut nama/nomor staf", not any(x in low for x in ("super uji", "noc uji", "admin uji", "628111")))

# 2. Super Admin tanpa target -> HARUS ditanya pelanggan mana, bukan didiagnosis sbg pelanggan
call(GW + "/sim/reset", {})
r = wa(SUPER, f"cek internet sekarang {RUN}", "Super Uji")
ob = outbox(SUPER); t = " ".join(m["text"] for m in ob).lower()
check("2 staf tanpa target: ditanya pelanggan mana", "pelanggan mana" in t, f"engine={r.get('engine')} :: {t[:110]!r}")
check("2 staf tanpa target: tidak dianggap pelanggan", "halo bapak/ibu" not in t and "status langganan anda" not in t)

# 3. NOC & Admin: juga tidak diperlakukan sebagai pelanggan
for who, num in (("noc", NOC), ("admin", ADMIN)):
    call(GW + "/sim/reset", {})
    r = wa(num, f"cek internet sekarang {RUN}")
    t = " ".join(m["text"] for m in outbox(num)).lower()
    check(f"3 {who}: tidak diperlakukan sebagai pelanggan", "status langganan anda" not in t and "halo bapak/ibu" not in t, t[:90])

# 4. Dedup: pesan identik cepat -> hanya satu diproses
call(GW + "/sim/reset", {})
a = wa(CUST, f"tes dedup unik {RUN}"); b = wa(CUST, f"tes dedup unik {RUN}")
check("4 dedup: kedua ditolak sbg duplikat", b.get("accepted") is False and "duplikat" in (b.get("note") or ""), f"a={a.get('accepted')} b={b.get('note')}")

# 5. Pesan panjang dipecah di gateway, urutan benar, tiap bagian <=500
call(GW + "/sim/reset", {})
long = ("Baris data tagihan nomor %d lunas dan sudah diverifikasi. " * 1)
big = "\n".join(long % i for i in range(1, 30))
s = call(GW + "/api/whatsapp/send", {"to": CUST, "message": big})
ob = outbox(CUST)
check("5 pecah: >1 bagian", s.get("parts", 1) > 1 and len(ob) == s.get("parts"), f"parts={s.get('parts')}")
check("5 pecah: tiap bagian <=500", all(len(m["text"]) <= 500 for m in ob), f"max={max(len(m['text']) for m in ob)}")
check("5 pecah: urutan (i/n) berurutan", [m["part"] for m in ob] == list(range(1, len(ob) + 1)))
joined = " ".join(m["text"] for m in ob)
check("5 pecah: tidak ada data hilang", all(f"nomor {i} lunas" in joined for i in (1, 15, 29)))

# 6. Grup diabaikan
call(GW + "/sim/reset", {})
r = call(GW + "/sim/inbound", {"from": "120363000000000001@g.us", "name": "Grup", "text": f"halo grup {RUN}"})
check("6 grup: tidak dibalas karena grup", not r.get("had_reply") and "grup" in (r.get("note") or ""), f"note={r.get('note')}")

# 7. Pesan kosong ditolak aman
r = call(GW + "/sim/inbound", {"from": CUST, "text": ""})
check("7 kosong: ditolak 400, tidak crash", r.get("_http") == 400)

# 8. Aturan akses: pelanggan tak boleh memerintah staf (tutup/update/perintah NOC)
call(GW + "/sim/reset", {})
r = wa(CUST, f"tutup CASE-20260101-AAAAAA {RUN}")
t = " ".join(m["text"] for m in outbox(CUST)).lower()
check("8 pelanggan tidak bisa pakai perintah staf", "ditutup" not in t and "case-20260101-aaaaaa" not in t, t[:90])

# 9. Serah-terima: eskalasi pelanggan -> staf update/tutup -> pelanggan menerima pesan tersaring
import os
LEDGER = os.path.join(".localtest", "data", "handoffs.json")
def ledger():
    try: return json.load(open(LEDGER, encoding="utf-8"))
    except Exception: return []
def status_of(cid):
    for h in ledger():
        if h["case_id"] == cid: return h["status"]
    return None

def open_case():
    for h in reversed(ledger()):
        if h["status"] != "selesai" and h["customer"] == CUST: return h["case_id"]
    return None
call(GW + "/sim/reset", {})
hid = open_case()
if not hid:
    wa(CUST, f"internet mati total sejak pagi lampu LOS merah {RUN}")
    hid = open_case()
check("9a handoff: kasus tercatat di ledger", bool(hid), str(hid))
if hid:
    call(GW + "/sim/reset", {})
    wa(ADMIN, f"tutup {hid} sudah beres")
    t = " ".join(m["text"] for m in outbox(ADMIN)).lower()
    check("9b admin ditolak utk domain network & status tak berubah", "tidak berwenang" in t and status_of(hid) != "selesai", f"status={status_of(hid)} :: {t[:70]}")
    wa(NOC, f"tutup {hid}")
    t = " ".join(m["text"] for m in outbox(NOC)).lower()
    check("9c tutup tanpa pesan pelanggan ditolak", "wajib menyertakan pesan" in t, t[:70])
    call(GW + "/sim/reset", {})
    wa(NOC, f"update {hid} info teknisi sedang menuju lokasi")
    cust = " ".join(m["text"] for m in outbox(CUST)).lower()
    check("9d update NOC -> pelanggan menerima pesan", len(cust) > 0, cust[:80])
    check("9d pesan pelanggan tanpa nama/nomor staf", not any(x in cust for x in ("noc uji", "super uji", "admin uji", "628111")))
    call(GW + "/sim/reset", {})
    wa(NOC, f"tutup {hid} Gangguan sudah diperbaiki, silakan dicoba kembali.")
    cust = " ".join(m["text"] for m in outbox(CUST)).lower()
    check("9e tutup oleh NOC: status selesai & pelanggan diberi tahu", status_of(hid) == "selesai" and "diperbaiki" in cust, f"status={status_of(hid)}")
    check("9e pesan penutupan tanpa nama/nomor staf", not any(x in cust for x in ("noc uji", "628111")))
    call(GW + "/sim/reset", {})
    wa(NOC, f"tutup {hid} Gangguan sudah diperbaiki, silakan dicoba kembali.")
    check("9f tutup ulang tidak mengirim ke pelanggan lagi", len(outbox(CUST)) == 0)


print(f"\nHASIL: {sum(results)}/{len(results)} lulus")
sys.exit(0 if all(results) else 1)
