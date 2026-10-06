#!/usr/bin/env bash
# kadran-kasa-coz.sh — kasayı geri alır (K-123).
#
# v0.5.0'dan v0.4.x'e dönmeden ÖNCE root olarak çalıştırılır. Ortam
# değişkeni değerlerini executor'ın anahtarıyla açıp veritabanına DÜZ
# yazar ve kasa işaretini (env_seal) siler. Sonra v0.4.x kurulabilir.
# Yeniden v0.5.0'a geçilirse daemon değerleri yine mühürler.
#
#   systemctl stop kadrand
#   /usr/local/lib/kadran/kadran-kasa-coz.sh
#
# Gerekenler: sqlite3 ve age (Debian/Ubuntu: apt-get install -y sqlite3 age).
# Önce veritabanının bir kopyasını alır (<db>.kasa-oncesi-<zaman>).
#
# ── Root, daemon'un dosyalarına dokunmuyor ───────────────────────────
#
# Veritabanı, yan dosyaları ve yedeği daemon'un dizininde (kadran 0750).
# Root orada dosya açsaydı, ele geçirilmiş bir daemon'un önceden koyduğu
# sembolik bağ root'a seçtiği bir yere veritabanı içeriği yazdırırdı
# (güvenlik incelemesi). sqlite3 bu yüzden kadran olarak koşuyor; root
# yalnız kendi anahtarıyla çözüyor ve SQL'i kendi geçici dosyasından
# standart girdiye veriyor.
#
# Veritabanından okunan uygulama ve değişken adları SQL'e girmeden önce
# kurulumun kabul ettiği desenle sınanıyor; değer SQL'e onaltılık (X'..')
# olarak giriyor. `-bail`: bir UPDATE düşerse işaret silinmeden duruluyor,
# transaction geri alınıyor.
set -euo pipefail
umask 077

DB="${KADRAN_DB:-/var/lib/kadran/kadran.db}"
KEY="${KADRAN_VAULT_KEY:-/var/lib/kadran-exec/vault.key}"
MUHUR_BASI='age:YWdlLWVuY3J5cHRpb24ub3JnL3Yx' # önek + base64("age-encryption.org/v1")

die() { echo "kadran-kasa-coz: $*" >&2; exit 1; }
hex() { od -An -v -tx1 | tr -d ' \n'; }

# kdb, sqlite3'ü kadran olarak çalıştırır (testte çağıranın kendisi olarak).
kdb() {
    if [ -n "${KADRAN_KASA_TEST:-}" ]; then
        sqlite3 -bail "$@"
    else
        setpriv --reuid kadran --regid kadran --clear-groups \
            env -i PATH="$PATH" sqlite3 -bail "$@"
    fi
}

if [ -z "${KADRAN_KASA_TEST:-}" ]; then
    [ "$(id -u)" -eq 0 ] || die "run as root"
    if systemctl is-active --quiet kadrand.service; then
        die "kadrand is running; first: systemctl stop kadrand"
    fi
fi
command -v sqlite3 >/dev/null || die "sqlite3 missing: apt-get install -y sqlite3"
command -v age >/dev/null || die "age missing: apt-get install -y age"
[ -r "$KEY" ] || die "cannot read the vault key: $KEY"

isaret="$(kdb "$DB" "SELECT count(*) FROM env_seal")" \
    || die "could not read the vault marker (a database from before v0.5.0?)"
if [ "$isaret" = 0 ]; then
    echo "no vault marker: values are already plain, nothing to do"
    exit 0
fi

yedek="$DB.kasa-oncesi-$(date -u +%Y%m%dT%H%M%SZ)"
kdb "$DB" "VACUUM INTO '$yedek'" || die "could not take a backup: $yedek"
echo "backup: $yedek"

# Döngünün beslendiği sorgu başarısız olursa döngü sessizce boş geçer; işaret
# silinir, değerler mühürlü kalırdı. Beklenen sayı önceden okunuyor.
toplam="$(kdb "$DB" "SELECT count(*) FROM apps a, json_each(a.env_json) e")" \
    || die "could not count the values"

sql="$(mktemp)"
trap 'rm -f "$sql"' EXIT
printf 'PRAGMA busy_timeout = 5000;\nBEGIN IMMEDIATE;\n' > "$sql"
n=0
while IFS=$'\t' read -r app key val; do
    [[ "$app" =~ ^[a-z][a-z0-9-]{0,31}$ ]] || die "invalid app name in the database: $app"
    [[ "$key" =~ ^[A-Za-z_][A-Za-z0-9_]*$ ]] || die "$app: invalid variable name in the database"
    [[ "$val" =~ ^age:[A-Za-z0-9+/]+=*$ ]] || die "$app/$key is not sealed; that contradicts the marker, stopping"
    acik="$(printf '%s' "${val#age:}" | base64 -d | age -d -i "$KEY" | hex)" \
        || die "could not open $app/$key (is this the right key?)"
    bag="$(printf '%s' "$app" | hex)00$(printf '%s' "$key" | hex)00"
    [[ "$acik" == "$bag"* ]] || die "$app/$key is sealed to another app or name"
    printf "UPDATE apps SET env_json = json_set(env_json, '\$.\"%s\"', CAST(X'%s' AS TEXT)) WHERE id = '%s';\n" \
        "$key" "${acik#"$bag"}" "$app" >> "$sql"
    n=$((n + 1))
done < <(kdb -separator $'\t' "$DB" "SELECT a.id, e.key, e.value FROM apps a, json_each(a.env_json) e")
[ "$n" = "$toplam" ] || die "read $n of $toplam values; nothing was written"
printf 'DELETE FROM env_seal;\nCOMMIT;\n' >> "$sql"

# Standart çıktı atılıyor: PRAGMA sonucunu ("5000") basıyordu. Hatalar
# standart hataya gidiyor.
kdb "$DB" < "$sql" >/dev/null || die "could not write; the change was rolled back (backup: $yedek)"

# Sonuç ölçülüyor, varsayılmıyor: mühürlü değer kalmamalı, işaret gitmeli.
kalan="$(kdb "$DB" "SELECT count(*) FROM apps a, json_each(a.env_json) e WHERE e.value LIKE '$MUHUR_BASI%'")"
isaret="$(kdb "$DB" "SELECT count(*) FROM env_seal")"
[ "$kalan" = 0 ] && [ "$isaret" = 0 ] \
    || die "INCONSISTENT: $kalan sealed values left, marker $isaret — do NOT install v0.4.x (backup: $yedek)"
echo "$n values turned back into plain text, vault marker removed."
echo "v0.4.x can be installed now. The backup holds no plain values, but the database now does."
