#!/usr/bin/env bash
# ============================================================================
#  NOC Sentinel — self-update (dipicu tombol "Update" di dashboard).
#
#  Dipanggil oleh binary (POST /api/update/apply) secara DETACHED dengan satu
#  argumen: versi target. Men-download artefak rilis GitHub, verifikasi
#  checksum, backup biner lama, pasang, lalu restart service systemd.
#
#  Pemakaian:
#     ./self-update.sh <versi>         # mis. ./self-update.sh 0.2.0
#     ./self-update.sh <versi> --check # hanya cek prasyarat, tidak mengubah
#
#  Idempoten & aman: kalau download/checksum gagal, biner lama TIDAK disentuh.
#  Sumber artefak: GitHub Releases via curl — TANPA git pull / scp.
#
#  Catatan portabilitas: skrip ini menargetkan server Linux (systemd). Untuk
#  host Windows .exe nanti, ganti config update_script ke installer .ps1/.bat.
# ============================================================================
set -euo pipefail

OWNER="${NOC_UPDATE_OWNER:-desienkz-slp}"
REPO="${NOC_UPDATE_REPO:-sentinel}"
APP_DIR="${NOC_APP_DIR:-/opt/noc-sentinel}"
SVC="${NOC_SERVICE:-noc-sentinel}"
ARCH="${NOC_ARCH:-amd64}"

VERSION=""
CHECK_ONLY=0
for a in "$@"; do
  case "$a" in
    --check) CHECK_ONLY=1 ;;
    *) VERSION="$a" ;;
  esac
done
[ -n "$VERSION" ] || { echo "usage: self-update.sh <versi> [--check]"; exit 2; }
VERSION="${VERSION#v}"  # buang prefix v bila ada

URL="https://github.com/$OWNER/$REPO/releases/download/v$VERSION"
PKG="noc-sentinel-$VERSION-linux-$ARCH"
TAR="$PKG.tar.gz"; SUM="$PKG.tar.gz.sha256"
TS="$(date +%Y%m%d-%H%M%S)"
LOG="${NOC_UPDATE_LOG:-$APP_DIR/data/update.log}"

log() { echo "[$(date '+%F %T')] $*" | tee -a "$LOG" 2>/dev/null || echo "[$(date '+%F %T')] $*"; }

mkdir -p "$(dirname "$LOG")" 2>/dev/null || true
log "== self-update ke v$VERSION (repo $OWNER/$REPO, arch $ARCH) =="

fail=0
for c in curl tar sha256sum; do
  command -v "$c" >/dev/null 2>&1 || { log "  ✘ hilang: $c"; fail=1; }
done
[ $fail -eq 0 ] || { log "  → pasang prasyarat dulu (curl tar coreutils)"; exit 1; }

if [ $CHECK_ONLY -eq 1 ]; then
  log "  ✔ --check selesai (prasyarat OK, tidak ada yang diubah)"
  exit 0
fi

TMP="$(mktemp -d)"; trap 'rm -rf "$TMP"' EXIT
cd "$TMP"

log "[1/5] unduh rilis v$VERSION..."
curl -fL --retry 3 --retry-delay 2 -o "$TAR" "$URL/$TAR"
curl -fL --retry 3 --retry-delay 2 -o "$SUM" "$URL/$SUM"

log "[2/5] verifikasi checksum..."
echo "$(awk '{print $1}' "$SUM")  $TAR" | sha256sum -c -

log "[3/5] backup biner & aset lama..."
if [ -f "$APP_DIR/bin/ai-noc-go" ]; then
  cp -a "$APP_DIR/bin/ai-noc-go" "$APP_DIR/bin/ai-noc-go.bak-$TS"
  # simpan hanya 3 backup terbaru.
  ls -1t "$APP_DIR/bin/"ai-noc-go.bak-* 2>/dev/null | tail -n +4 | xargs -r rm -f
fi

log "[4/5] pasang artefak baru (data & .env TIDAK disentuh)..."
# Ekstrak ke staging lalu salin file runtime; data/ & wa-gateway/data/ dijaga.
mkdir -p stage
tar -xzf "$TAR" -C stage --strip-components=1
# Salin biner + aset yang dibaca dari disk (policies/tools/workflows/migrations,
# wa-gateway/app, public). JANGAN timpa: data/, wa-gateway/data/, .env, config.json,
# tools/registry.local.yaml (overlay operator).
cp -a stage/bin/ai-noc-go "$APP_DIR/bin/ai-noc-go"
for d in policies workflows migrations; do
  [ -d "stage/$d" ] && cp -a "stage/$d/." "$APP_DIR/$d/"
done
# tools/: salin registry.yaml bawaan TAPI jangan hapus registry.local.yaml.
[ -f stage/tools/registry.yaml ] && cp -a stage/tools/registry.yaml "$APP_DIR/tools/registry.yaml"
[ -d stage/wa-gateway/app ] && cp -a stage/wa-gateway/app/. "$APP_DIR/wa-gateway/app/"
[ -d stage/wa-gateway/public ] && cp -a stage/wa-gateway/public/. "$APP_DIR/wa-gateway/public/"
[ -f stage/VERSION ] && cp -a stage/VERSION "$APP_DIR/VERSION"
# node_modules: npm ci bila package-lock berubah (native binding glibc).
if [ -f stage/wa-gateway/package-lock.json ]; then
  cp -a stage/wa-gateway/package.json stage/wa-gateway/package-lock.json "$APP_DIR/wa-gateway/" 2>/dev/null || true
  if command -v npm >/dev/null 2>&1; then
    ( cd "$APP_DIR/wa-gateway" && npm ci --omit=dev --no-audit --no-fund ) >>"$LOG" 2>&1 || log "  ! npm ci gagal — gateway mungkin perlu perbaikan manual"
  fi
fi
chown -R noc:noc "$APP_DIR" 2>/dev/null || true

log "[5/5] restart service $SVC..."
if command -v systemctl >/dev/null 2>&1; then
  systemctl restart "$SVC"
  sleep 3
  if systemctl is-active --quiet "$SVC"; then
    log "  ✔ $SVC aktif kembali di versi v$VERSION"
  else
    log "  ✘ $SVC GAGAL start — rollback ke biner lama"
    if [ -f "$APP_DIR/bin/ai-noc-go.bak-$TS" ]; then
      cp -a "$APP_DIR/bin/ai-noc-go.bak-$TS" "$APP_DIR/bin/ai-noc-go"
      systemctl restart "$SVC" || true
      log "  ↩ rollback selesai; cek: journalctl -u $SVC -n 50"
    fi
    exit 1
  fi
else
  log "  ! systemctl tidak ada — restart service secara manual"
fi

log "== self-update selesai: v$VERSION =="
