#!/usr/bin/env bash
# Menyalakan gateway WA simulasi + Sentinel uji. Tanpa URL yang perlu disalin.
# Pakai:  bash wa-gateway/sim/start-sim.sh        (jalan terus; Ctrl+C untuk berhenti)
set -u
cd "$(dirname "$0")/../.."
[ -f .localtest/config.json ] || { echo "[X] .localtest/config.json belum ada (lihat wa-gateway/sim/README.md)"; exit 1; }
go build -o .localtest/ai-noc-go.exe . || exit 1
for p in 8190 3011; do
  if netstat -ano | grep -qE ":$p .*LISTENING"; then echo "[X] port $p sudah dipakai. Matikan prosesnya dulu."; exit 1; fi
done
( cd .localtest && ./ai-noc-go.exe -config config.json ) &
SP=$!
( cd wa-gateway && PORT=3011 NOC_WEBHOOK_URL="http://127.0.0.1:8190/api/wa/webhook" node sim/sim-gateway.mjs ) &
GP=$!
trap 'kill $SP $GP 2>/dev/null' EXIT INT TERM
sleep 5
curl -s -m5 http://127.0.0.1:3011/api/whatsapp/status && echo
echo "[OK] siap. Uji: python3 wa-gateway/sim/e2e.py"
wait
