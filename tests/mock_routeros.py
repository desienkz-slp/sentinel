"""Mock RouterOS API NATIVE (protokol biner) untuk verifikasi end-to-end.

Meniru API native RouterOS di port TCP:
- Login plaintext (6.43+): /login =name=... =password=...
- Command: /system/resource/print dan /ppp/active/print
"""
import socket
import threading

USER, PASS = "noc", "rahasia"
HOST, PORT = "127.0.0.1", 9323


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
            data += sock.recv(ln - len(data))
        words.append(data.decode(errors="replace"))


def handle(conn):
    try:
        # Login
        s = read_sentence(conn)
        user = passw = ""
        for w in s or []:
            if w.startswith("=name="):
                user = w[6:]
            if w.startswith("=password="):
                passw = w[9:]
        if user != USER or passw != PASS:
            conn.sendall(sentence(["!trap", "=message=invalid user name or password (6)"]))
            return
        conn.sendall(sentence(["!done"]))
        # Command
        s = read_sentence(conn)
        if not s:
            return
        cmd = s[0]
        rows = []
        if cmd == "/system/resource/print":
            rows = [["=version=7.14.2", "=board-name=CCR1036-8G-2S+", "=uptime=3d2h10m", "=cpu-load=2"]]
        elif cmd == "/ppp/active/print":
            rows = [
                ["=name=628123456789@netlayer", "=service=pppoe", "=caller-id=AA:BB", "=address=10.0.0.5", "=uptime=1h2m"],
                ["=name=other-user", "=service=pppoe", "=address=10.0.0.6", "=uptime=2h"],
            ]
        for row in rows:
            conn.sendall(sentence(["!re"] + row))
        conn.sendall(sentence(["!done"]))
    except Exception:
        pass
    finally:
        conn.close()


def main():
    srv = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
    srv.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
    srv.bind((HOST, PORT))
    srv.listen(5)
    print(f"mock-routeros-native on :{PORT}", flush=True)
    while True:
        conn, _ = srv.accept()
        threading.Thread(target=handle, args=(conn,), daemon=True).start()


if __name__ == "__main__":
    main()
