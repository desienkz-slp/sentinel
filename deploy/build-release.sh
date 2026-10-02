#!/usr/bin/env bash
# ============================================================================
#  NOC Sentinel — build artefak rilis (Linux).
#
#  Dijalankan di build box LINUX / CI, BUKAN di Windows dan BUKAN di CT target.
#  Alasannya: node_modules gateway WA berisi native binding per-platform
#  (sharp-*, whatsapp-rust-bridge). node_modules yang di-install di Windows
#  TIDAK boleh disalin ke Linux — harus di-install ulang di sini via npm ci.
#
#  Hasil: dist/noc-sentinel-<versi>-linux-<arch>.tar.gz + .sha256
#  CT target memakai artefak ini lewat curl (lihat install-ct.sh) — TANPA
#  git pull dan TANPA scp.
#
#  Pemakaian:
#     ./deploy/build-release.sh <versi> [arch]     (default arch=amd64)
#     ./deploy/build-release.sh 0.1.0 amd64
# ============================================================================
set -euo pipefail

VERSION="${1:?usage: build-release.sh <versi> [arch]}"
ARCH="${2:-amd64}"
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
DIST="$ROOT/dist"
STAGE="$DIST/noc-sentinel"
PKG="noc-sentinel-$VERSION-linux-$ARCH"

for c in go node npm tar sha256sum; do
  command -v "$c" >/dev/null 2>&1 || { echo "[X] $c tidak ditemukan"; exit 1; }
done

rm -rf "$DIST"
mkdir -p "$STAGE/bin" "$STAGE/wa-gateway"

echo "[1/5] build binary ai-noc-go (linux/$ARCH, static)..."
( cd "$ROOT" && CGO_ENABLED=0 GOOS=linux GOARCH="$ARCH" \
  go build -trimpath -ldflags "-s -w" -o "$STAGE/bin/ai-noc-go" . )

echo "[2/5] salin aset runtime (dibaca dari disk saat jalan)..."
# web/* SUDAH di-embed ke binary — tidak perlu disalin.
cp -r "$ROOT/policies" "$ROOT/tools" "$ROOT/workflows" "$ROOT/migrations" "$STAGE/"
cp -r "$ROOT/wa-gateway/app" "$ROOT/wa-gateway/public" "$STAGE/wa-gateway/"
cp "$ROOT/wa-gateway/package.json" "$ROOT/wa-gateway/package-lock.json" "$STAGE/wa-gateway/"
# Skrip deploy + unit systemd + template env ikut dalam artefak.
cp -r "$ROOT/deploy" "$STAGE/"

echo "[3/5] npm ci (linux) — native binding yang benar untuk glibc..."
( cd "$STAGE/wa-gateway" && npm ci --omit=dev --no-audit --no-fund )

echo "$VERSION" > "$STAGE/VERSION"

echo "[4/5] paket tarball + checksum..."
tar -C "$DIST" -czf "$DIST/$PKG.tar.gz" "noc-sentinel"
( cd "$DIST" && sha256sum "$PKG.tar.gz" > "$PKG.tar.gz.sha256" )

echo "[5/5] selesai:"
ls -lh "$DIST/$PKG.tar.gz" "$DIST/$PKG.tar.gz.sha256"
