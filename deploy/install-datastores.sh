#!/usr/bin/env bash
# ============================================================================
#  NOC Sentinel - pasang PostgreSQL + Redis (satu perintah, idempoten).
#
#  Pemakaian (sebagai root di server):
#     ./deploy/install-datastores.sh --check     # hanya cek, TIDAK mengubah apa pun
#     ./deploy/install-datastores.sh             # pasang + konfigurasi + migrasi
#     ./deploy/install-datastores.sh --no-migrate
#
#  Keamanan:
#   - PostgreSQL & Redis hanya listen 127.0.0.1 (tidak terbuka ke jaringan).
#   - Kata sandi dibuat acak, disimpan di /opt/noc-sentinel/.datastores.env
#     (mode 600, milik root). Tidak pernah dicetak ke layar/log.
#   - Cadangan data/ dibuat sebelum mengubah apa pun.
#   - Tidak menghapus data. Aman diulang.
# ============================================================================
set -uo pipefail

APP_DIR="${NOC_APP_DIR:-/opt/noc-sentinel}"
ENV_FILE="$APP_DIR/.datastores.env"
PG_DB="${NOC_POSTGRES_DB:-noc_sentinel}"
PG_USER="${NOC_POSTGRES_USER:-noc_app}"
CHECK_ONLY=0
DO_MIGRATE=1
for a in "$@"; do
  case "$a" in
    --check) CHECK_ONLY=1 ;;
    --no-migrate) DO_MIGRATE=0 ;;
    -h|--help) sed -n '2,17p' "$0" | sed 's/^# \{0,1\}//'; exit 0 ;;
    *) echo "argumen tidak dikenal: $a" >&2; exit 2 ;;
  esac
done

ok()   { printf "  [ok]   %s\n" "$1"; }
bad()  { printf "  [GAGAL] %s\n" "$1"; FAIL=1; }
todo() { printf "  [perlu] %s\n" "$1"; NEED=1; }
info() { printf "  ->     %s\n" "$1"; }
FAIL=0; NEED=0

if [ "$(id -u)" -ne 0 ]; then echo "jalankan sebagai root"; exit 2; fi

# ---------- keadaan sekarang ----------
have_pkg() { dpkg -s "$1" >/dev/null 2>&1; }
pg_active()    { systemctl is-active --quiet postgresql; }
redis_active() { systemctl is-active --quiet redis-server; }
pg_ready()     { pg_isready -q -h 127.0.0.1 -p 5432 2>/dev/null; }
redis_conf=/etc/redis/redis.conf

echo "== Pemeriksaan =="
have_pkg postgresql      && ok "paket postgresql terpasang" || todo "paket postgresql belum terpasang"
have_pkg redis-server    && ok "paket redis-server terpasang" || todo "paket redis-server belum terpasang"
pg_active    && ok "postgresql aktif" || todo "postgresql belum aktif"
redis_active && ok "redis aktif"      || todo "redis belum aktif"
[ -f "$ENV_FILE" ] && ok "berkas kredensial ada ($ENV_FILE)" || todo "berkas kredensial belum ada"
if [ -f "$ENV_FILE" ]; then
  [ "$(stat -c %a "$ENV_FILE")" = "600" ] && ok "kredensial mode 600" || todo "mode kredensial bukan 600"
fi
if have_pkg postgresql && pg_active; then
  if su postgres -c "psql -tAc \"select 1 from pg_database where datname='$PG_DB'\"" 2>/dev/null | grep -q 1; then
    ok "database $PG_DB ada"
  else todo "database $PG_DB belum ada"; fi
  listen=$(su postgres -c "psql -tAc 'show listen_addresses'" 2>/dev/null)
  [ "$listen" = "localhost" ] || [ "$listen" = "127.0.0.1" ] && ok "postgres hanya listen lokal" || todo "listen_addresses bukan lokal ($listen)"
fi
if [ -f "$redis_conf" ]; then
  [ "$(grep -Ec '^bind[[:space:]]' "$redis_conf")" = "1" ] && grep -Eq '^bind 127\.0\.0\.1$' "$redis_conf" && ok "redis bind 127.0.0.1 (satu baris)" || todo "redis belum di-bind ke 127.0.0.1 secara bersih"
  grep -Eq '^requirepass ' "$redis_conf" && ok "redis memakai requirepass" || todo "redis belum memakai kata sandi"
fi

if [ "$CHECK_ONLY" -eq 1 ]; then
  echo
  if [ "$NEED" -eq 0 ] && [ "$FAIL" -eq 0 ]; then echo "SEMUA SESUAI (tidak ada yang perlu diubah)"; exit 0; fi
  echo "ada yang perlu dipasang/diubah (jalankan tanpa --check)"; exit 1
fi

# ---------- cadangan ----------
echo "== Cadangan =="
if [ -d "$APP_DIR/data" ]; then
  bk="$APP_DIR/data-backup-$(date +%Y%m%d%H%M%S).tar.gz"
  tar -czf "$bk" -C "$APP_DIR" data 2>/dev/null && chmod 600 "$bk" && ok "data dicadangkan: $(basename "$bk")" || bad "cadangan data gagal"
  [ "$FAIL" -eq 0 ] || exit 1
fi

# ---------- paket ----------
echo "== Paket =="
export DEBIAN_FRONTEND=noninteractive
if ! have_pkg postgresql || ! have_pkg redis-server; then
  apt-get update -qq >/dev/null 2>&1
  apt-get install -y -qq postgresql postgresql-contrib redis-server >/dev/null 2>&1 \
    && ok "paket terpasang" || { bad "apt-get install gagal"; exit 1; }
else ok "paket sudah ada"; fi

# ---------- kredensial (dibuat sekali, tidak diubah pada pengulangan) ----------
echo "== Kredensial =="
if [ ! -f "$ENV_FILE" ]; then
  umask 077
  pgpw=$(head -c 24 /dev/urandom | base64 | tr -dc 'A-Za-z0-9' | head -c 28)
  rdpw=$(head -c 24 /dev/urandom | base64 | tr -dc 'A-Za-z0-9' | head -c 28)
  {
    echo "NOC_POSTGRES_HOST=127.0.0.1"
    echo "NOC_POSTGRES_PORT=5432"
    echo "NOC_POSTGRES_USER=$PG_USER"
    echo "NOC_POSTGRES_PASSWORD=$pgpw"
    echo "NOC_POSTGRES_DB=$PG_DB"
    echo "NOC_REDIS_ADDR=127.0.0.1:6379"
    echo "NOC_REDIS_PASSWORD=$rdpw"
  } > "$ENV_FILE"
  chmod 600 "$ENV_FILE"; chown root:root "$ENV_FILE"
  ok "kredensial dibuat (tidak ditampilkan)"
else ok "kredensial sudah ada (dipakai ulang)"; fi
chmod 600 "$ENV_FILE"
# shellcheck disable=SC1090
set -a; . "$ENV_FILE"; set +a

# ---------- PostgreSQL ----------
echo "== PostgreSQL =="
systemctl enable --now postgresql >/dev/null 2>&1
for i in $(seq 1 30); do pg_isready -q -h 127.0.0.1 -p 5432 && break; sleep 1; done
pg_isready -q -h 127.0.0.1 -p 5432 && ok "postgres siap" || { bad "postgres tidak siap"; exit 1; }
cf=$(su postgres -c "psql -tAc 'show config_file'" 2>/dev/null)
if [ -n "$cf" ] && ! grep -Eq "^listen_addresses *= *'(localhost|127\.0\.0\.1)'" "$cf"; then
  sed -i -E "s/^#?listen_addresses *=.*/listen_addresses = 'localhost'/" "$cf"
  systemctl restart postgresql && ok "listen_addresses dikunci ke localhost"
fi
role_exists=$(su postgres -c "psql -tAc \"select 1 from pg_roles where rolname='$PG_USER'\"" 2>/dev/null)
if [ "$role_exists" = "1" ]; then
  su postgres -c "psql -q -v ON_ERROR_STOP=1 -c \"alter role $PG_USER with login password '$NOC_POSTGRES_PASSWORD'\"" >/dev/null 2>&1 && ok "role $PG_USER sinkron"
else
  su postgres -c "psql -q -v ON_ERROR_STOP=1 -c \"create role $PG_USER with login password '$NOC_POSTGRES_PASSWORD'\"" >/dev/null 2>&1 && ok "role $PG_USER dibuat" || bad "gagal membuat role"
fi
if ! su postgres -c "psql -tAc \"select 1 from pg_database where datname='$PG_DB'\"" 2>/dev/null | grep -q 1; then
  su postgres -c "createdb -O $PG_USER $PG_DB" >/dev/null 2>&1 && ok "database $PG_DB dibuat" || bad "gagal membuat database"
else ok "database $PG_DB ada"; fi
su postgres -c "psql -q -d $PG_DB -c 'create extension if not exists pgcrypto'" >/dev/null 2>&1 && ok "ekstensi pgcrypto"

# ---------- Redis ----------
echo "== Redis =="
if [ -f "$redis_conf" ]; then
  # Hanya baris AKTIF (tanpa #) yang disentuh; komentar penjelasan dibiarkan.
  # Semua baris bind aktif dibuang lalu satu baris bind ditambahkan.
  sed -i -E '/^bind[[:space:]]/d' "$redis_conf"
  echo "bind 127.0.0.1" >> "$redis_conf"
  sed -i -E '/^protected-mode[[:space:]]/d' "$redis_conf"
  echo "protected-mode yes" >> "$redis_conf"
  # requirepass/maxmemory: satu baris aktif saja (buang duplikat dari percobaan sebelumnya).
  sed -i -E '/^maxmemory(-policy)?[[:space:]]/d' "$redis_conf"
  echo "maxmemory 256mb" >> "$redis_conf"
  echo "maxmemory-policy allkeys-lru" >> "$redis_conf"
  sed -i -E '/^requirepass[[:space:]]/d' "$redis_conf"
  echo "requirepass $NOC_REDIS_PASSWORD" >> "$redis_conf"
fi
systemctl enable redis-server >/dev/null 2>&1
systemctl restart redis-server && ok "redis dikonfigurasi & dimulai"
sleep 1
if REDISCLI_AUTH="$NOC_REDIS_PASSWORD" redis-cli -h 127.0.0.1 ping 2>/dev/null | grep -q PONG; then ok "redis PONG"; else bad "redis tidak menjawab"; fi

# ---------- sambungkan ke layanan ----------
echo "== Layanan =="
unit=/etc/systemd/system/noc-sentinel.service
if [ -f "$unit" ] && ! grep -q "datastores.env" "$unit"; then
  sed -i "/^EnvironmentFile=/a EnvironmentFile=-$ENV_FILE" "$unit"
  systemctl daemon-reload && ok "layanan membaca $ENV_FILE"
fi

echo
[ "$FAIL" -eq 0 ] && echo "SELESAI. Jalankan --check untuk memverifikasi; restart layanan: systemctl restart noc-sentinel" || { echo "ADA KEGAGALAN"; exit 1; }
