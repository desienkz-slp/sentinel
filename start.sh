#!/usr/bin/env bash
# ============================================================================
#  NOC Sentinel — jalankan aplikasi (build bila perlu).
#  Untuk pertama kali: ./install.sh
# ============================================================================
set -uo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$SCRIPT_DIR"

BIN="bin/ai-noc-go"
PORT="${NOC_PORT:-8090}"

# ---- build bila sumber lebih baru dari binary ----
need_build=0
if [ ! -x "$BIN" ]; then
  need_build=1
elif [ -n "$(find . -name '*.go' -newer "$BIN" -not -path './bin/*' -print -quit 2>/dev/null)" ]; then
  need_build=1
elif [ -n "$(find web -name '*.html' -newer "$BIN" -print -quit 2>/dev/null)" ]; then
  need_build=1
fi

if [ "$need_build" -eq 1 ]; then
  if command -v go >/dev/null 2>&1; then
    echo "[*] Membangun ai-noc-go…"
    mkdir -p bin
    go build -o "$BIN" . || { echo "[X] Build gagal."; exit 1; }
    echo "[+] Build selesai."
  else
    echo "[X] Go tidak ditemukan dan binary belum ada."
    echo "    Jalankan ./install.sh lebih dulu."
    exit 1
  fi
fi

# ---- peringatan bila Node.js tidak ada ----
if ! command -v node >/dev/null 2>&1; then
  echo "[!] Node.js tidak ditemukan — WhatsApp Gateway tidak akan jalan."
  echo "    Jalankan ./install.sh untuk memasangnya."
  echo
fi

echo
echo "  Dashboard  : http://127.0.0.1:$PORT"
echo "  WA Gateway : http://127.0.0.1:$PORT/whatsapp"
echo "  Hentikan   : Ctrl+C"
echo

# Buka browser bila memungkinkan (desktop).
if command -v xdg-open >/dev/null 2>&1; then
  (xdg-open "http://127.0.0.1:$PORT" >/dev/null 2>&1 &)
elif command -v open >/dev/null 2>&1; then
  (open "http://127.0.0.1:$PORT" >/dev/null 2>&1 &)
fi

exec "$BIN" -addr ":$PORT"
