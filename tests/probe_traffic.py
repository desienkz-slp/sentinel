"""Probe sintaks command API native RouterOS untuk traffic reading.

Membaca config.json (kredensial asli), login, lalu menjalankan beberapa command
kandidat dan mencetak respons MENTAH (field name) supaya implementasi Go akurat.
TIDAK mencetak password.
"""
import json
import socket
import sys

CFG = "config.json"


def enc_len(n):
    if n < 0x80:
        return bytes([n])
    if n < 0x4000:
        return bytes([0x80 | (n >> 8), n & 0xFF])
    if n < 0x200000:
        return bytes([0xC0 | (n >> 16), (n >> 8) & 0xFF, n & 0xFF])
    raise ValueError("too long")


def word(w):
    b = w.encode()
    return enc_len(len(b)) + b


def sentence(words):
    return b"".join(word(w) for w in words) + b"\x00"


def read_sentence(sock):
    words = []
    while True:
        c = sock.recv(1)
        if not c:
            return None
        b = c[0]
        if b & 0x80 == 0:
            ln = b
        elif b & 0xC0 == 0x80:
            ln = ((b & 0x3F) << 8) | sock.recv(1)[0]
        else:
            ln = ((b & 0x1F) << 16) | (sock.recv(1)[0] << 8) | sock.recv(1)[0]
        if ln == 0:
            return words
        data = b""
        while len(data) < ln:
            chunk = sock.recv(ln - len(data))
            if not chunk:
                return words
            data += chunk
        words.append(data.decode(errors="replace"))


class MT:
    def __init__(self, host, port, user, pwd):
        self.host, self.port, self.user, self.pwd = host, port, user, pwd
        self.s = socket.socket()
        self.s.settimeout(10)
        self.s.connect((host, port))

    def login(self):
        self.s.sendall(sentence(["/login", f"=name={self.user}", f"=password={self.pwd}"]))
        r = read_sentence(self.s)
        return r

    def call(self, *words):
        self.s.sendall(sentence(list(words)))
        rows = []
        while True:
            r = read_sentence(self.s)
            if r is None:
                break
            if r[0] in ("!done", "!trap", "!fatal"):
                if r[0] != "!done":
                    rows.append(r)
                break
            rows.append(r)
        return rows


def main():
    cfg = json.load(open(CFG, encoding="utf-8"))
    host = cfg.get("mikrotik_host")
    port = cfg.get("mikrotik_port") or 8728
    user = cfg.get("mikrotik_user")
    pwd = cfg.get("mikrotik_pass")
    if not host or not user:
        print("config.json belum berisi mikrotik_host/user", file=sys.stderr)
        return
    mt = MT(host, port, user, pwd)
    r = mt.login()
    print("login:", r)

    def show(label, rows, limit=3):
        print(f"\n===== {label} =====")
        for row in rows[:limit]:
            print(" ", row)

    # 1. interface print (cumulative stats)
    show("/interface/print", mt.call("/interface/print"))

    # 2. interface monitor-traffic once (live bps)
    show("/interface/monitor-traffic once",
         mt.call("/interface/monitor-traffic", "=interface=ether1", "=once="))

    # 3. queue simple print (config)
    show("/queue/simple/print", mt.call("/queue/simple/print"))

    # 4. queue simple print stats (rate/bytes)
    show("/queue/simple/print stats",
         mt.call("/queue/simple/print", "=stats="))

    # 5. ppp active print (cek field bytes/rate)
    show("/ppp/active/print", mt.call("/ppp/active/print"))

    mt.s.close()


if __name__ == "__main__":
    main()
