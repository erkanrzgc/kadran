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
# Veritabanından okunan uygulama ve değişken adları SQL'e girmeden önce
# kurulumun kabul ettiği desenle sınanıyor: daemon'un yazdığı veriye root
# olarak güvenilmiyor. Değer SQL'e onaltılık (X'..') olarak giriyor; kabuk ya
# da SQL kaçışı yok, sondaki satır sonları da korunuyor.
set -euo pipefail

DB="${KADRAN_DB:-/var/lib/kadran/kadran.db}"
KEY="${KADRAN_VAULT_KEY:-/var/lib/kadran-exec/vault.key}"

die() { echo "kadran-kasa-coz: $*" >&2; exit 1; }
hex() { od -An -v -tx1 | tr -d ' \n'; }

if [ -z "${KADRAN_KASA_TEST:-}" ]; then
    [ "$(id -u)" -eq 0 ] || die "root olarak çalıştırın"
    if systemctl is-active --quiet kadrand.service; then
        die "kadrand çalışıyor; önce: systemctl stop kadrand"
    fi
fi
command -v sqlite3 >/dev/null || die "sqlite3 yok: apt-get install -y sqlite3"
command -v age >/dev/null || die "age yok: apt-get install -y age"
[ -f "$DB" ] || die "veritabanı yok: $DB"
[ -r "$KEY" ] || die "kasa anahtarı okunamıyor: $KEY"

isaret="$(sqlite3 "$DB" "SELECT count(*) FROM env_seal")" \
    || die "kasa işareti okunamadı (v0.5.0 öncesi bir veritabanı mı?)"
if [ "$isaret" = 0 ]; then
    echo "kasa işareti yok: değerler zaten düz, yapılacak bir şey yok"
    exit 0
fi

yedek="$DB.kasa-oncesi-$(date -u +%Y%m%dT%H%M%SZ)"
sqlite3 "$DB" "VACUUM INTO '$yedek'" || die "yedek alınamadı: $yedek"
chmod 0600 "$yedek"
echo "yedek: $yedek"

# Döngünün beslendiği sorgu başarısız olursa döngü sessizce boş geçer; işaret
# silinir, değerler mühürlü kalırdı. Beklenen sayı önceden okunuyor.
toplam="$(sqlite3 "$DB" "SELECT count(*) FROM apps a, json_each(a.env_json) e")" \
    || die "değerler sayılamadı"

sql="$(mktemp)"
trap 'rm -f "$sql"' EXIT
echo "BEGIN IMMEDIATE;" > "$sql"
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
done < <(sqlite3 -separator $'\t' "$DB" "SELECT a.id, e.key, e.value FROM apps a, json_each(a.env_json) e")
[ "$n" = "$toplam" ] || die "$toplam değerden $n tanesi okundu; hiçbir şey yazılmadı"
echo "DELETE FROM env_seal;" >> "$sql"
echo "COMMIT;" >> "$sql"

sqlite3 "$DB" < "$sql" || die "yazılamadı; veritabanı değişmedi (yedek: $yedek)"
echo "$n değer düz metne çevrildi, kasa işareti silindi."
echo "Şimdi v0.4.x kurulabilir. Yedek düz değer içermiyor ama veritabanı artık içeriyor."
