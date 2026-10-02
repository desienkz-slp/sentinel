#!/usr/bin/env bash
# ============================================================================
#  NOC Sentinel — installer untuk CT Proxmox (Ubuntu/Debian, glibc).
#
#  Cara mengambil skrip ini sendiri TANPA scp/git (curl dari GitHub):
#     curl -fsSL https://raw.githubusercontent.com/desienkz-slp/sentinel/main/deploy/install-ct.sh -o install-ct.sh
#     chmod +x install-ct.sh
#
#  Pemakaian:
#     ./install-ct.sh <versi>              # unduh + pasang + jalankan
#     ./install-ct.sh <versi> --check      # hanya cek prasyarat, tidak mengubah apa pun
#
#  Idempoten: aman dijalankan ulang (konfigurasi & data tidak ditimpa).
#  Sumber artefak: GitHub Releases via curl — TIDAK git pull, TIDAK scp.
# ============================================================================
set -euo pipefail

OWNER="desienkz-slp"
REPO="sentinel"
APP_DIR="/opt/noc-sentinel"
SVC="noc-sentinel"

VERSION=""
CHECK_ONLY=0
for a in "$@"; do
  case "$a" in
    --check) CHECK_ONLY=1 ;;
    *) VERSION="$a" ;;
  esac
done
[ -n "$VERSION" ] || { echo "usage: install-ct.sh <versi> [--check]"; exit 2; }

URL="https://github.com/$OWNER/$REPO/releases/download/v$VERSION"
PKG="noc-sentinel-$VERSION-linux-amd64"
TAR="$PKG.tar.gz"; SUM="$PKG.tar.gz.sha256"

need() { command -v "$1" >/dev/null 2>&1 || { echo "  ✘ hilang: $1"; return 1; }; }

echo "== NOC Sentinel install (v$VERSION) =="
echo "  folder   : $APP_DIR"
echo "  mode     : $([ $CHECK_ONLY -eq 1 ] && echo check || echo install)"
echo "  sumber   : $URL  (curl — tanpa git pull / scp)"

fail=0
for c in curl tar sha256sum node; do need "$c" || fail=1; done
if [ $fail -ne 0 ]; then
  echo "  → pasang dulu:  apt-get update && apt-get install -y curl nodejs"
  exit 1
fi
echo "  ✔ semua prasyarat tersedia"

[ $CHECK_ONLY -eq 1 ] && { echo "  ✔ --check selesai (tidak ada yang diubah)"; exit 0; }

TMP="$(mktemp -d)"; trap 'rm -rf "$TMP"' EXIT
cd "$TMP"

echo "[1/5] unduh rilis..."
curl -fL --retry 3 --retry-delay 2 -o "$TAR" "$URL/$TAR"
curl -fL --retry 3 --retry-delay 2 -o "$SUM" "$URL/$SUM"

echo "[2/5] verifikasi checksum..."
echo "$(awk '{print $1}' "$SUM")  $TAR" | sha256sum -c -

echo "[3/5] pasang ke $APP_DIR..."
mkdir -p "$APP_DIR"
tar -xzf "$TAR" -C "$APP_DIR" --strip-components=1
# Folder writable saat runtime (data JSON + sesi WhatsApp).
mkdir -p "$APP_DIR/data" "$APP_DIR/wa-gateway/data/auth"

# User service tanpa shell.
id -u noc >/dev/null 2>&1 || useradd --system --no-create-home --home "$APP_DIR" noc
chown -R noc:noc "$APP_DIR"

# .env — jangan timpa yang sudah ada.
if [ ! -f "$APP_DIR/.env" ]; then
  cp "$APP_DIR/deploy/.env.production.example" "$APP_DIR/.env"
  echo "  ✔ $APP_DIR/.env dibuat dari template — ISI SECRET-NYA SEKARANG"
else
  echo "  ✔ $APP_DIR/.env sudah ada (tidak diubah)"
fi
chmod 600 "$APP_DIR/.env"

echo "[4/5] pasang unit systemd..."
cp "$APP_DIR/deploy/noc-sentinel.service" "/etc/systemd/system/$SVC.service"
systemctl daemon-reload
systemctl enable "$SVC"

echo "[5/5] jalankan..."
systemctl restart "$SVC"

echo
echo "  ✔ Selesai. Cek:"
echo "      systemctl status $SVC"
echo "      journalctl -u $SVC -f"
echo "      dashboard : http://<ip-ct>:8090   (scan QR WhatsApp pertama kali)"
