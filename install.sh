#!/usr/bin/env bash
# ============================================================================
#  NOC Sentinel — installer SEMUA kebutuhan sistem, satu perintah.
#
#  Pemakaian:
#     ./install.sh              # cek + pasang semua, lalu build
#     ./install.sh --check      # hanya cek, tidak memasang apa pun
#     ./install.sh --no-build   # pasang dependensi, lewati build
#
#  Skrip ini idempoten: aman dijalankan berulang kali.
# ============================================================================
set -uo pipefail

# ---------- warna & util ----------
if [ -t 1 ]; then
  B=$'\033[1m'; DIM=$'\033[2m'; R=$'\033[0m'
  OK=$'\033[32m'; WARN=$'\033[33m'; ERR=$'\033[31m'; INFO=$'\033[36m'
else
  B=""; DIM=""; R=""; OK=""; WARN=""; ERR=""; INFO=""
fi
ok()   { printf "  ${OK}✔${R} %s\n" "$1"; }
bad()  { printf "  ${ERR}✘${R} %s\n" "$1"; }
warn() { printf "  ${WARN}!${R} %s\n" "$1"; }
info() { printf "  ${INFO}→${R} %s\n" "$1"; }
head1(){ printf "\n${B}%s${R}\n" "$1"; }

CHECK_ONLY=0
DO_BUILD=1
for a in "$@"; do
  case "$a" in
    --check)    CHECK_ONLY=1 ;;
    --no-build) DO_BUILD=0 ;;
    -h|--help)  sed -n '2,12p' "$0" | sed 's/^# \{0,1\}//'; exit 0 ;;
  esac
done

# ---------- lokasi skrip ----------
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$SCRIPT_DIR"

# ---------- deteksi platform ----------
UNAME="$(uname -s 2>/dev/null || echo unknown)"
case "$UNAME" in
  MINGW*|MSYS*|CYGWIN*) PLATFORM=windows ;;
  Darwin)               PLATFORM=macos ;;
  Linux)                PLATFORM=linux ;;
  *)                    PLATFORM=unknown ;;
esac

PKG=""
command -v winget >/dev/null 2>&1 && PKG=winget
[ -z "$PKG" ] && command -v choco >/dev/null 2>&1 && PKG=choco
[ -z "$PKG" ] && command -v brew  >/dev/null 2>&1 && PKG=brew
[ -z "$PKG" ] && command -v apt-get >/dev/null 2>&1 && PKG=apt
[ -z "$PKG" ] && command -v dnf >/dev/null 2>&1 && PKG=dnf
[ -z "$PKG" ] && command -v pacman >/dev/null 2>&1 && PKG=pacman

printf "${B}\n╔══════════════════════════════════════════════════════════════╗\n"
printf "║   NOC Sentinel — AI NOC + WhatsApp Gateway (Go)              ║\n"
printf "║   Installer semua kebutuhan sistem                           ║\n"
printf "╚══════════════════════════════════════════════════════════════╝${R}\n"
printf "${DIM}  Platform : %s\n  Folder   : %s\n  Paket mgr: %s${R}\n" \
  "$PLATFORM" "$SCRIPT_DIR" "${PKG:-tidak terdeteksi}"

FAIL=0        # kebutuhan wajib yang belum siap
MISSING=()    # nama tool yang perlu dipasang

# ---------- helper pemasangan ----------
pkg_install() {
  # $1 = nama paket per-OS: "winget_id|brew|apt|dnf|pacman"
  local spec="$1" name="$2"
  IFS='|' read -r w b a d p <<< "$spec"
  case "$PKG" in
    winget) info "winget install $w"; winget install --id "$w" -e --accept-source-agreements --accept-package-agreements --silent ;;
    choco)  info "choco install $name"; choco install "$name" -y --no-progress ;;
    brew)   info "brew install $b"; brew install "$b" ;;
    apt)    info "apt-get install $a"; sudo apt-get update -qq && sudo apt-get install -y "$a" ;;
    dnf)    info "dnf install $d"; sudo dnf install -y "$d" ;;
    pacman) info "pacman -S $p"; sudo pacman -S --noconfirm "$p" ;;
    *)      return 1 ;;
  esac
}

# ---------- 1. Git ----------
head1 "1. Git"
if command -v git >/dev/null 2>&1; then
  ok "git $(git --version | awk '{print $3}')"
else
  bad "git belum terpasang"
  MISSING+=("git")
  [ "$CHECK_ONLY" -eq 0 ] && { pkg_install "Git.Git|git|git|git|git" git && command -v git >/dev/null 2>&1 && ok "git terpasang" || FAIL=1; }
fi

# ---------- 2. Go (wajib, >= 1.24) ----------
head1 "2. Go (wajib — untuk membangun binary)"
GO_MIN="1.24"
if command -v go >/dev/null 2>&1; then
  GO_VER="$(go env GOVERSION 2>/dev/null | sed 's/^go//')"
  ok "Go $GO_VER"
  # bandingkan versi
  if [ "$(printf '%s\n%s\n' "$GO_MIN" "$GO_VER" | sort -V | head -1)" != "$GO_MIN" ]; then
    warn "Go $GO_VER < $GO_MIN yang dibutuhkan — silakan perbarui"
    FAIL=1
  fi
else
  bad "Go belum terpasang (butuh >= $GO_MIN)"
  MISSING+=("go")
  if [ "$CHECK_ONLY" -eq 0 ]; then
    if [ "$PLATFORM" = windows ]; then
      pkg_install "GoLang.Go|go|golang-go|golang|go" go
    else
      pkg_install "GoLang.Go|go|golang-go|golang|go" go
    fi
    if command -v go >/dev/null 2>&1; then
      ok "Go terpasang"
    else
      bad "Gagal memasang Go otomatis"
      info "Pasang manual: https://go.dev/dl/"
      FAIL=1
    fi
  fi
fi

# ---------- 3. Node.js (wajib, >= 18 — untuk WhatsApp Gateway) ----------
head1 "3. Node.js (wajib — untuk WhatsApp Gateway)"
NODE_MIN="18"
if command -v node >/dev/null 2>&1; then
  NODE_VER="$(node --version | sed 's/^v//')"
  NODE_MAJ="${NODE_VER%%.*}"
  if [ "$NODE_MAJ" -ge "$NODE_MIN" ] 2>/dev/null; then
    ok "Node.js v$NODE_VER"
  else
    warn "Node.js v$NODE_VER < v$NODE_MIN — silakan perbarui"
    FAIL=1
  fi
  command -v npm >/dev/null 2>&1 && ok "npm $(npm --version)" || { bad "npm tidak ada"; FAIL=1; }
else
  bad "Node.js belum terpasang (butuh >= v$NODE_MIN)"
  MISSING+=("node")
  if [ "$CHECK_ONLY" -eq 0 ]; then
    pkg_install "OpenJS.NodeJS.LTS|node|nodejs|nodejs|nodejs" nodejs
    if command -v node >/dev/null 2>&1; then ok "Node.js terpasang"; else
      bad "Gagal memasang Node.js otomatis"; info "Pasang manual: https://nodejs.org/ (LTS)"; FAIL=1
    fi
  fi
fi

# ---------- 4. Codex CLI (opsional — untuk eskalasi analisis) ----------
head1 "4. Codex CLI ${DIM}(opsional — eskalasi analisis)${R}"
if command -v codex >/dev/null 2>&1; then
  ok "codex $(codex --version 2>/dev/null | head -1)"
  CODEX_HOME_DIR="${HOME}/.codex"
  if [ -f "$CODEX_HOME_DIR/auth.json" ]; then
    if grep -q '"auth_mode"[[:space:]]*:[[:space:]]*"apikey"' "$CODEX_HOME_DIR/auth.json" 2>/dev/null; then
      ok "Codex memakai mode API key (siap untuk provider kustom)"
    else
      warn "Codex masih mode OAuth — akan gagal 401 dengan provider kustom (9Router)"
      info "Perbaikan: set auth.json ke {\"auth_mode\":\"apikey\",\"OPENAI_API_KEY\":\"<key>\",\"tokens\":null}"
    fi
  else
    warn "~/.codex/auth.json belum ada — Codex belum dikonfigurasi"
  fi
else
  warn "codex tidak ada — fitur eskalasi ke Codex akan nonaktif (sistem tetap jalan)"
  if [ "$CHECK_ONLY" -eq 0 ]; then
    info "Pasang dengan: npm install -g @openai/codex"
  fi
fi

# ---------- 5. 9Router / penyedia LLM ----------
head1 "5. Penyedia LLM (9Router) ${DIM}(wajib — otak agen)${R}"
LLM_URL="${NOC_LLM_BASE_URL:-http://127.0.0.1:20128/v1}"
if curl -s -m 6 -o /dev/null "${LLM_URL}/models" 2>/dev/null; then
  ok "endpoint LLM menjawab: $LLM_URL"
else
  warn "endpoint LLM tidak menjawab di $LLM_URL"
  info "Jalankan 9Router Anda, atau set NOC_LLM_BASE_URL ke endpoint OpenAI-compatible lain"
fi

# ---------- 6. API key ----------
head1 "6. API key LLM"
KEY_FOUND=""
[ -n "${NOC_LLM_API_KEY:-}" ] && KEY_FOUND="NOC_LLM_API_KEY (env)"
[ -z "$KEY_FOUND" ] && [ -n "${HERMES_CUSTOM_9ROUTER_API_KEY:-}" ] && KEY_FOUND="HERMES_CUSTOM_9ROUTER_API_KEY (env)"
[ -z "$KEY_FOUND" ] && [ -n "${OPENAI_API_KEY:-}" ] && KEY_FOUND="OPENAI_API_KEY (env)"
if [ -z "$KEY_FOUND" ] && [ -f "${HOME}/.codex/auth.json" ]; then
  grep -q '"OPENAI_API_KEY"[[:space:]]*:[[:space:]]*"[^"]' "${HOME}/.codex/auth.json" 2>/dev/null \
    && KEY_FOUND="~/.codex/auth.json"
fi
if [ -n "$KEY_FOUND" ]; then
  ok "API key ditemukan dari: $KEY_FOUND"
else
  warn "API key belum ada"
  info "Isi salah satu: env NOC_LLM_API_KEY, atau config.json (\"llm_api_key\"),"
  info "atau lewat panel ⚙ Pengaturan di dashboard setelah aplikasi jalan"
fi

# ---------- 7. Konfigurasi lokal ----------
head1 "7. Konfigurasi"
if [ -f config.json ]; then
  ok "config.json sudah ada (tidak diubah)"
else
  if [ "$CHECK_ONLY" -eq 0 ] && [ -f config.example.json ]; then
    cp config.example.json config.json
    ok "config.json dibuat dari config.example.json"
    info "Edit bila perlu — atau ubah dari dashboard tanpa restart"
  else
    info "config.json belum ada (opsional — nilai bawaan dipakai)"
  fi
fi

# ---------- 8. Dependensi WhatsApp Gateway ----------
head1 "8. Dependensi WhatsApp Gateway (npm)"
if [ -d wa-gateway/node_modules ] && [ -d wa-gateway/node_modules/@whiskeysockets ]; then
  ok "node_modules sudah terpasang (dilewati)"
else
  if [ "$CHECK_ONLY" -eq 1 ]; then
    info "node_modules belum ada — akan dipasang saat aplikasi pertama dijalankan"
  elif command -v npm >/dev/null 2>&1; then
    info "npm install (196 paket, ±30 detik)…"
    if (cd wa-gateway && npm install --omit=dev --no-audit --no-fund >/tmp/ainoc-npm.log 2>&1); then
      ok "dependensi gateway terpasang ($(du -sh wa-gateway/node_modules 2>/dev/null | cut -f1))"
    else
      bad "npm install gagal — lihat /tmp/ainoc-npm.log"
      tail -5 /tmp/ainoc-npm.log 2>/dev/null | sed 's/^/      /'
      FAIL=1
    fi
  else
    bad "npm tidak tersedia — tidak bisa memasang dependensi gateway"
    FAIL=1
  fi
fi

# ---------- 9. Build binary Go ----------
head1 "9. Build binary"
if [ "$DO_BUILD" -eq 1 ] && [ "$CHECK_ONLY" -eq 0 ] && command -v go >/dev/null 2>&1; then
  info "go build -o bin/ai-noc-go(.exe) ."
  mkdir -p bin
  BIN="bin/ai-noc-go"
  [ "$PLATFORM" = windows ] && BIN="bin/ai-noc-go.exe"
  if go build -o "$BIN" . 2>/tmp/ainoc-build.log; then
    ok "build sukses: $BIN ($(du -h "$BIN" 2>/dev/null | cut -f1))"
  else
    bad "build gagal — lihat /tmp/ainoc-build.log"
    sed 's/^/      /' /tmp/ainoc-build.log | head -15
    FAIL=1
  fi
else
  info "build dilewati"
fi

# ---------- 10. Tes cepat ----------
if [ "$CHECK_ONLY" -eq 0 ] && [ "$DO_BUILD" -eq 1 ] && command -v go >/dev/null 2>&1; then
  head1 "10. Unit test"
  if go test ./... -count=1 >/tmp/ainoc-test.log 2>&1; then
    ok "semua unit test lulus ($(grep -c '^ok' /tmp/ainoc-test.log) paket)"
  else
    warn "ada test yang gagal — lihat /tmp/ainoc-test.log"
    grep -E '^(--- FAIL|FAIL)' /tmp/ainoc-test.log | head -5 | sed 's/^/      /'
  fi
fi

# ---------- ringkasan ----------
printf "\n${B}══════════════════════════════════════════════════════════════${R}\n"
if [ "$FAIL" -eq 0 ] && [ "$CHECK_ONLY" -eq 0 ]; then
  printf "${OK}${B}  SIAP. Sistem sudah bisa dijalankan.${R}\n\n"
  printf "  Jalankan:\n"
  if [ "$PLATFORM" = windows ]; then
    printf "    ${B}start.bat${R}            ${DIM}(klik dua kali juga bisa)${R}\n"
    printf "    ${DIM}atau: ./bin/ai-noc-go.exe${R}\n"
  else
    printf "    ${B}./start.sh${R}\n"
    printf "    ${DIM}atau: ./bin/ai-noc-go${R}\n"
  fi
  printf "\n  Dashboard  : ${B}http://127.0.0.1:8090${R}\n"
  printf "  WA Gateway : ${B}http://127.0.0.1:8090/whatsapp${R}  ${DIM}(scan QR dari panel dashboard)${R}\n"
  printf "\n  ${WARN}Jangan lupa: isi allowlist nomor WhatsApp sebelum dipakai sungguhan.${R}\n"
elif [ "$CHECK_ONLY" -eq 1 ]; then
  if [ "$FAIL" -eq 0 ]; then
    printf "${OK}${B}  Semua kebutuhan wajib sudah terpenuhi.${R}\n"
  else
    printf "${ERR}${B}  Ada kebutuhan wajib yang belum siap (lihat tanda ✘ di atas).${R}\n"
  fi
  printf "${DIM}  Jalankan ./install.sh tanpa --check untuk memasang.${R}\n"
else
  printf "${ERR}${B}  Instalasi belum tuntas.${R}\n"
  [ ${#MISSING[@]} -gt 0 ] && printf "  Belum terpasang: ${B}%s${R}\n" "${MISSING[*]}"
  printf "\n  Perbaiki lalu jalankan ulang: ${B}./install.sh${R}\n"
fi
printf "${B}══════════════════════════════════════════════════════════════${R}\n\n"

exit "$FAIL"
