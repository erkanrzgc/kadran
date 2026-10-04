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
    [ "$(id -u)" -eq 0 ] || die "root olarak çalıştırın"
    if systemctl is-active --quiet kadrand.service; then
        die "kadrand çalışıyor; önce: systemctl stop kadrand"
    fi
fi
command -v sqlite3 >/dev/null || die "sqlite3 yok: apt-get install -y sqlite3"
command -v age >/dev/null || die "age yok: apt-get install -y age"
[ -r "$KEY" ] || die "kasa anahtarı okunamıyor: $KEY"

isaret="$(kdb "$DB" "SELECT count(*) FROM env_seal")" \
    || die "kasa işareti okunamadı (v0.5.0 öncesi bir veritabanı mı?)"
if [ "$isaret" = 0 ]; then
    echo "kasa işareti yok: değerler zaten düz, yapılacak bir şey yok"
    exit 0
fi

yedek="$DB.kasa-oncesi-$(date -u +%Y%m%dT%H%M%SZ)"
kdb "$DB" "VACUUM INTO '$yedek'" || die "yedek alınamadı: $yedek"
echo "yedek: $yedek"

# Döngünün beslendiği sorgu başarısız olursa döngü sessizce boş geçer; işaret
# silinir, değerler mühürlü kalırdı. Beklenen sayı önceden okunuyor.
toplam="$(kdb "$DB" "SELECT count(*) FROM apps a, json_each(a.env_json) e")" \
    || die "değerler sayılamadı"

sql="$(mktemp)"
trap 'rm -f "$sql"' EXIT
printf 'PRAGMA busy_timeout = 5000;\nBEGIN IMMEDIATE;\n' > "$sql"
n=0
while IFS=$'\t' read -r app key val; do
    [[ "$app" =~ ^[a-z][a-z0-9-]{0,31}$ ]] || die "geçersiz uygulama adı veritabanında: $app"
    [[ "$key" =~ ^[A-Za-z_][A-Za-z0-9_]*$ ]] || die "$app: geçersiz değişken adı veritabanında"
    [[ "$val" =~ ^age:[A-Za-z0-9+/]+=*$ ]] || die "$app/$key mühürlü değil; işaretle çelişiyor, durdu"
    acik="$(printf '%s' "${val#age:}" | base64 -d | age -d -i "$KEY" | hex)" \
        || die "$app/$key açılamadı (anahtar bu değil mi?)"
    bag="$(printf '%s' "$app" | hex)00$(printf '%s' "$key" | hex)00"
    [[ "$acik" == "$bag"* ]] || die "$app/$key başka bir uygulamaya ya da ada mühürlü"
    printf "UPDATE apps SET env_json = json_set(env_json, '\$.\"%s\"', CAST(X'%s' AS TEXT)) WHERE id = '%s';\n" \
        "$key" "${acik#"$bag"}" "$app" >> "$sql"
    n=$((n + 1))
done < <(kdb -separator $'\t' "$DB" "SELECT a.id, e.key, e.value FROM apps a, json_each(a.env_json) e")
[ "$n" = "$toplam" ] || die "$toplam değerden $n tanesi okundu; hiçbir şey yazılmadı"
printf 'DELETE FROM env_seal;\nCOMMIT;\n' >> "$sql"

# Standart çıktı atılıyor: PRAGMA sonucunu ("5000") basıyordu. Hatalar
# standart hataya gidiyor.
kdb "$DB" < "$sql" >/dev/null || die "yazılamadı; değişiklik geri alındı (yedek: $yedek)"

# Sonuç ölçülüyor, varsayılmıyor: mühürlü değer kalmamalı, işaret gitmeli.
kalan="$(kdb "$DB" "SELECT count(*) FROM apps a, json_each(a.env_json) e WHERE e.value LIKE '$MUHUR_BASI%'")"
isaret="$(kdb "$DB" "SELECT count(*) FROM env_seal")"
[ "$kalan" = 0 ] && [ "$isaret" = 0 ] \
    || die "TUTARSIZ: $kalan mühürlü değer kaldı, işaret $isaret — v0.4.x KURMAYIN (yedek: $yedek)"
echo "$n değer düz metne çevrildi, kasa işareti silindi."
echo "Şimdi v0.4.x kurulabilir. Yedek düz değer içermiyor ama veritabanı artık içeriyor."
