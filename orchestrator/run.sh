#!/usr/bin/env bash
# ============================================================================
#  NOC Sentinel — Phase Orchestrator
#
#  Menjalankan 11 phase (0–10) dari AI_AGENT_HERMES_MASTER_SPEC.md §47 secara
#  BERURUTAN, satu phase per session Hermes baru (isolated context), dipandu
#  oleh state file `state.json`.
#
#  Alur:
#    1. Baca phase berikutnya dari state.json
#    2. Spawn `hermes chat --oneshot` dengan model phase
#    3. Agent kerjakan phase (tulis kode + jalankan test exit-gate)
#    4. Validator deterministik cek exit-gate: `go test ./...`
#    5. PASS → update state.json → lanjut phase berikutnya
#       FAIL → berhenti + tulis laporan (TIDAK lanjut otomatis)
#
#  Pemakaian:
#    ./run.sh                 # jalankan phase berikutnya (satu phase)
#    ./run.sh --all           # jalankan semua phase berurutan sampai selesai/gagal
#    ./run.sh --reset         # set ulang state ke phase 0
#    ./run.sh --status        # tampilkan state saat ini
#    ./run.sh --check         # validasi saja (tanpa spawn agent), non-mutating
# ============================================================================
set -uo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"

# Python/Go adalah binary NATIVE Windows: mereka butuh path Windows (C:/...),
# bukan path MSYS (/c/...). Konversi sekali di awal.
to_win() { pwd -W 2>/dev/null || cygpath -m "$1" 2>/dev/null || echo "$1"; }
SCRIPT_DIR_WIN="$(cd "$SCRIPT_DIR" && to_win)"
REPO_DIR_WIN="$(cd "$REPO_DIR" && to_win)"

STATE="$SCRIPT_DIR_WIN/state.json"
PHASES="$SCRIPT_DIR_WIN/phases.json"
REPORTS_DIR_MSYS="$SCRIPT_DIR/reports"
REPORTS="$SCRIPT_DIR_WIN/reports"
mkdir -p "$REPORTS_DIR_MSYS"

# model + provider (pemilihan lewat PROVIDER dulu, lalu model di dalamnya).
# Provider = griphub-router-gpt (config.yaml); model di dalam provider itu.
PROVIDER="${NOC_PHASE_PROVIDER:-griphub-router-gpt}"
MODEL="${NOC_PHASE_MODEL:-gpt-5.6-terra}"
FALLBACK_MODELS=("gpt-5.6-sol" "gpt-5.6-luna")

info()  { printf '\033[36m→\033[0m %s\n' "$1"; }
ok()    { printf '\033[32m✔\033[0m %s\n' "$1"; }
bad()   { printf '\033[31m✘\033[0m %s\n' "$1"; }

# ---------- util: baca field json tanpa jq (pakai python) ----------
jget() { python - "$@" <<'PY'
import json,sys
path=sys.argv[1]; key=sys.argv[2]
d=json.load(open(path,encoding="utf-8"))
def walk(o,k):
    if isinstance(o,dict): return o.get(k)
    return None
# support dotted keys
cur=d
for part in key.split("."):
    if isinstance(cur,dict): cur=cur.get(part)
    else: cur=None; break
print(json.dumps(cur, ensure_ascii=False) if cur is not None else "null")
PY
}

jset() { python - "$@" <<'PY'
import json,sys,datetime
path=sys.argv[1]; updates=json.loads(sys.argv[2])
d=json.load(open(path,encoding="utf-8"))
def deep_set(o,key,val):
    parts=key.split(".")
    for p in parts[:-1]:
        o=o.setdefault(p,{})
    o[parts[-1]]=val
for k,v in updates.items():
    deep_set(d,k,v)
d["updated_at"]=datetime.datetime.now(datetime.timezone.utc).isoformat()
json.dump(d,open(path,"w",encoding="utf-8"),indent=2,ensure_ascii=False)
PY
}

status() {
  local cur name st
  cur=$(jget "$STATE" current_phase)
  st=$(jget "$STATE" status)
  name=$(python - "$PHASES" "$cur" <<'PY'
import json,sys
d=json.load(open(sys.argv[1],encoding="utf-8"))
c=int(sys.argv[2])
for p in d["phases"]:
    if p["id"]==c: print(p["name"]); break
else: print("?")
PY
)
  printf 'Phase saat ini : %s — %s\n' "$cur" "$name"
  printf 'Status         : %s\n' "$st"
  printf 'Provider       : %s\n' "$PROVIDER"
  printf 'Model          : %s\n' "$MODEL"
}

# ---------- exit-gate validator (deterministik) ----------
validate_phase() {
  local phase_id="$1"
  # Gate universal: semua test harus lulus.
  (cd "$REPO_DIR" && go test ./... -count=1 >/tmp/noc-phase-test.log 2>&1)
  if [ $? -ne 0 ]; then
    bad "exit-gate GAGAL: go test ./... tidak lulus"
    grep -E '^(--- FAIL|FAIL|panic)' /tmp/noc-phase-test.log | head -20 | sed 's/^/      /'
    return 1
  fi
  ok "exit-gate lulus: go test ./... hijau"
  return 0
}

# ---------- jalankan satu phase lewat Hermes session baru ----------
run_phase() {
  local phase_id="$1"
  local phase_name goal gate prompt report
  phase_name=$(python - "$PHASES" "$phase_id" <<'PY'
import json,sys
d=json.load(open(sys.argv[1],encoding="utf-8")); c=int(sys.argv[2])
for p in d["phases"]:
    if p["id"]==c: print(p["name"]); break
else: print("UNKNOWN")
PY
)
  goal=$(python - "$PHASES" "$phase_id" <<'PY'
import json,sys
d=json.load(open(sys.argv[1],encoding="utf-8")); c=int(sys.argv[2])
for p in d["phases"]:
    if p["id"]==c: print(p["goal"]); break
else: print("")
PY
)
  gate=$(python - "$PHASES" "$phase_id" <<'PY'
import json,sys
d=json.load(open(sys.argv[1],encoding="utf-8")); c=int(sys.argv[2])
for p in d["phases"]:
    if p["id"]==c: print(p["exit_gate"]); break
else: print("")
PY
)
  report="$REPORTS/phase_${phase_id}.md"

  info "=== Phase $phase_id: $phase_name ==="
  info "Goal: $goal"

  # Prompt mandiri untuk agent di session baru. Agent TIDAK tahu percakapan ini;
  # semua konteks diberikan lewat file & prompt di bawah.
  prompt="Kamu adalah agent pengembangan NOC Sentinel (Go, project di repo ini).

TUGAS SEKARANG: implementasikan PHASE $phase_id — $phase_name.

Konteks project (baca file ini dulu):
- Master spec: AI_AGENT_HERMES_MASTER_SPEC.md (baca §47 untuk daftar phase)
- Rencana deliver: docs/MASTER_SPEC_DELIVERY_PLAN.md
- Matriks penerimaan: docs/MASTER_SPEC_ACCEPTANCE_MATRIX.md

GOAL phase ini:
$goal

EXIT-GATE yang WAJIB dipenuhi (validator deterministik memeriksanya):
$gate

ATURAN:
1. Baca repository & architecture dulu (prinsip 'inspect before changing').
2. Jangan rewrite besar — kembangkan incrementally, reuse komponen existing.
3. Jangan mengarang API endpoint eksternal; tandai assumption bila belum jelas.
4. Tulis kode + test untuk phase ini, lalu JALANKAN `go test ./...` sampai hijau.
5. Tulis ringkasan hasil ke file: $report
   Format: daftar file dibuat/diubah, test yang lulus, asumsi, risiko.
6. Gunakan bahasa Indonesia untuk dokumentasi/komentar.
7. Commit hasil dengan git (pesan: 'feat(phase-N): <ringkasan>').

Jangan berhenti sebelum go test ./... hijau dan laporan tertulis."

  # --- spawn hermes (session baru, isolated) ---
  # prompt ditulis ke file temp native-path (process substitution /dev/fd tidak
  # bisa dibaca oleh binary Python native di Windows)
  local promptfile
  promptfile="$LOCALAPPDATA/Temp/noc_phase_${phase_id}_prompt.txt"
  printf '%s' "$prompt" > "$promptfile"

  local out m
  for m in "$MODEL" "${FALLBACK_MODELS[@]}"; do
    info "Spawn hermes — provider: $PROVIDER, model: $m"
    out=$(cd "$REPO_DIR" && timeout 1200 hermes chat --oneshot -Q \
      --in "$REPO_DIR_WIN" \
      --provider "$PROVIDER" \
      -m "$m" \
      --query-file "$promptfile" 2>&1)
    # cek apakah model benar-benar menjawab (bukan error dari Hermes/gateway).
    # Deteksi pakai frasa spesifik Hermes, bukan kode HTTP mentah (agar tidak
    # false-positive pada teks laporan yang kebetulan memuat "401" dsb).
    if echo "$out" | grep -qiE 'Custom endpoint rejected|Provider said:|Missing Authentication|internal_model_route|usage limit has been reached|model rejected this request|temporarily unavailable'; then
      bad "Model $m tidak tersedia: $(echo "$out" | head -1)"
      continue
    fi
    # sukses
    printf '%s\n' "$out" > "$REPORTS/phase_${phase_id}.log"
    return 0
  done
  bad "Semua model gagal untuk phase $phase_id"
  return 1
}

# ---------- aksi utama ----------
MODE="${1:---run}"
case "$MODE" in
  --status)
    status
    exit 0
    ;;
  --reset)
    jset "$STATE" '{"current_phase":0,"status":"pending","started_at":null,"history":[]}'
    ok "state di-reset ke phase 0"
    exit 0
    ;;
  --check)
    info "mode --check: hanya validasi, tidak spawn agent, tidak mengubah state"
    cur=$(jget "$STATE" current_phase)
    validate_phase "$cur"
    exit $?
    ;;
  --all)
    info "Menjalankan semua phase berurutan dari state saat ini"
    while true; do
      cur=$(jget "$STATE" current_phase)
      total=$(python - "$PHASES" -1 <<'PY'
import json,sys
print(len(json.load(open(sys.argv[1],encoding="utf-8"))["phases"]))
PY
)
      if [ "$cur" -ge "$total" ]; then
        ok "Semua phase selesai (0–$((total-1)))."
        exit 0
      fi
      jset "$STATE" "{\"status\":\"in_progress\",\"started_at\":\"now\"}"
      if run_phase "$cur"; then
        if validate_phase "$cur"; then
          python - "$STATE" "$PHASES" "$cur" <<'PY'
import json,sys,datetime
state=json.load(open(sys.argv[1],encoding="utf-8"))
c=int(sys.argv[3])
entry={"phase":c,"name":"","status":"passed","at":datetime.datetime.now(datetime.timezone.utc).isoformat()}
for p in json.load(open(sys.argv[2],encoding="utf-8"))["phases"]:
    if p["id"]==c: entry["name"]=p["name"]
state.setdefault("history",[]).append(entry)
state["current_phase"]=c+1
state["status"]="pending"
json.dump(state,open(sys.argv[1],"w",encoding="utf-8"),indent=2,ensure_ascii=False)
PY
          ok "Phase $cur LULUS — lanjut ke phase $((cur+1))"
        else
          python - "$STATE" "$cur" <<'PY'
import json,sys,datetime
state=json.load(open(sys.argv[1],encoding="utf-8"))
c=int(sys.argv[2])
state.setdefault("history",[]).append({"phase":c,"status":"failed_exit_gate","at":datetime.datetime.now(datetime.timezone.utc).isoformat()})
state["status"]="failed"
json.dump(state,open(sys.argv[1],"w",encoding="utf-8"),indent=2,ensure_ascii=False)
PY
          bad "Phase $cur GAGAL exit-gate — berhenti. Periksa /tmp/noc-phase-test.log"
          exit 1
        fi
      else
        python - "$STATE" "$cur" <<'PY'
import json,sys,datetime
state=json.load(open(sys.argv[1],encoding="utf-8"))
c=int(sys.argv[2])
state.setdefault("history",[]).append({"phase":c,"status":"failed_spawn","at":datetime.datetime.now(datetime.timezone.utc).isoformat()})
state["status"]="failed"
json.dump(state,open(sys.argv[1],"w",encoding="utf-8"),indent=2,ensure_ascii=False)
PY
        bad "Phase $cur GAGAL spawn — berhenti."
        exit 1
      fi
    done
    ;;
  *)
    # satu phase
    cur=$(jget "$STATE" current_phase)
    jset "$STATE" "{\"status\":\"in_progress\",\"started_at\":\"now\"}"
    if run_phase "$cur"; then
      if validate_phase "$cur"; then
        python - "$STATE" "$PHASES" "$cur" <<'PY'
import json,sys,datetime
state=json.load(open(sys.argv[1],encoding="utf-8")); c=int(sys.argv[3])
entry={"phase":c,"name":"","status":"passed","at":datetime.datetime.now(datetime.timezone.utc).isoformat()}
for p in json.load(open(sys.argv[2],encoding="utf-8"))["phases"]:
    if p["id"]==c: entry["name"]=p["name"]
state.setdefault("history",[]).append(entry)
state["current_phase"]=c+1
state["status"]="pending"
json.dump(state,open(sys.argv[1],"w",encoding="utf-8"),indent=2,ensure_ascii=False)
PY
        ok "Phase $cur LULUS. Jalankan ./run.sh lagi untuk phase berikutnya."
      else
        bad "Phase $cur GAGAL exit-gate. Periksa /tmp/noc-phase-test.log"
        exit 1
      fi
    else
      bad "Phase $cur GAGAL spawn."
      exit 1
    fi
    ;;
esac
