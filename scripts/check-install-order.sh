#!/usr/bin/env bash
# Göç sırası: aracı (kadran-proxy) soketi kadrand'dan ÖNCE hazır olmalı
# (B2/I4). kadrand.service `Requires=kadran-proxy.socket` taşıyor.
#
# Ayrı bir betik: scripts/check-install-sh.sh'ın bir bölümü Windows'ta
# (sembolik bağ/izin) kırmızı; bu sıra denetimi platformdan bağımsız.
# scripts/mutate-install.sh bunu da koşturuyor.
set -uo pipefail
cd "$(dirname "$0")/.."

fail=0
dene() {
    local ad="$1" kod="$2"
    if bash -c "$kod" >/dev/null 2>&1; then echo "  ✓ $ad"; else echo "  ✗ $ad"; fail=1; fi
}
D="$(mktemp -d)"
trap 'rm -rf "$D"' EXIT

echo
echo "== Göçte aracı soketi kadrand'dan ÖNCE (B2/I4) =="
# kadrand.service Requires=kadran-proxy.socket: göçte birim/kullanıcı/soket
# kadrand yeniden başlamadan önce hazır olmalı. İşlevler sahte komutlarla
# koşuyor; çağrı SIRASI günlüğe yazılıyor.
AFN="$D/arac-fonks"
: > "$AFN"
for f in vekil_araci_kullanicisi vekil_araci_tmpfiles vekil_araci_birimleri vekil_araci_onhazirla; do
    sed -n "/^$f() {/,/^}/p" internal/bootstrap/install.sh >> "$AFN"
    grep -q "^$f() {" "$AFN" || { echo "$f install.sh'tan çıkarılamadı — ölçüm geçersiz" >&2; exit 1; }
done
ARAC_GUNLUK="$D/arac-gunluk"
arac_kos() {
    : > "$ARAC_GUNLUK"
    bash -c "
        set -euo pipefail
        STAGE=/stage; NOLOGIN=/usr/sbin/nologin
        step() { :; }; say() { :; }
        die() { echo HATA \"\$*\" >&2; exit 1; }
        log() { echo \"\$*\" >> '$ARAC_GUNLUK'; }
        getent() { return 1; }
        id() { case \"\$*\" in \"-u \"*) return 1;; \"-nG kadran-proxy\") echo kadran-proxy;; *) echo kadran;; esac; }
        groupadd() { log groupadd \"\$@\"; }
        useradd() { log useradd \"\$@\"; }
        install() { log install \"\$@\"; }
        systemd-tmpfiles() { log tmpfiles \"\$@\"; }
        systemctl() { log systemctl \"\$@\"; }
        source '$AFN'
        vekil_araci_onhazirla
    "
}
# sira <desen1> <desen2>: günlükte desen1 ilk kez desen2'den ÖNCE geçiyor mu
sira() {
    local a b
    a="$(grep -n -- "$1" "$ARAC_GUNLUK" | head -1 | cut -d: -f1)"
    b="$(grep -n -- "$2" "$ARAC_GUNLUK" | head -1 | cut -d: -f1)"
    [ -n "$a" ] && [ -n "$b" ] && [ "$a" -lt "$b" ]
}
arac_kos >/dev/null 2>&1
dene "önhazırlık hatasız koşuyor" "[ -s '$ARAC_GUNLUK' ]"
SIRA_FN="$(declare -f sira)"
dene "broker kullanıcısı tmpfiles'tan ÖNCE" \
    "$SIRA_FN; ARAC_GUNLUK='$ARAC_GUNLUK'; sira 'useradd.*kadran-proxy' 'tmpfiles.*kadran-proxy.conf'"
dene "tmpfiles birim dosyalarından ÖNCE" \
    "$SIRA_FN; ARAC_GUNLUK='$ARAC_GUNLUK'; sira 'tmpfiles.*kadran-proxy.conf' 'install.*kadran-proxy.socket'"
dene "birimler daemon-reload'dan ÖNCE" \
    "$SIRA_FN; ARAC_GUNLUK='$ARAC_GUNLUK'; sira 'install.*kadran-proxy.socket' 'systemctl daemon-reload'"
dene "daemon-reload soketin başlatılmasından ÖNCE" \
    "$SIRA_FN; ARAC_GUNLUK='$ARAC_GUNLUK'; sira 'systemctl daemon-reload' 'systemctl restart kadran-proxy.socket'"
dene "soket başlatılıyor" "grep -q 'systemctl restart kadran-proxy.socket' '$ARAC_GUNLUK'"
dene "aracı SERVİSİ başlatılmıyor (eski vekil portları tutuyor)" \
    "! grep -Eq 'systemctl (start|restart) kadran-proxy.service|kadran-caddy' '$ARAC_GUNLUK'"
# Ana akış: göç dalındaki çağrı kadrand'ın ilk yeniden başlatmasından önce.
cagri="$(grep -n '^    vekil_araci_onhazirla$' internal/bootstrap/install.sh | head -1 | cut -d: -f1)"
yeniden="$(grep -n '^systemctl restart kadrand.service' internal/bootstrap/install.sh | head -1 | cut -d: -f1)"
dene "ana akış: vekil_araci_onhazirla çağrısı var" "[ -n '$cagri' ]"
dene "ana akış: çağrı kadrand yeniden başlatmasından ÖNCE" \
    "[ -n '$cagri' ] && [ -n '$yeniden' ] && [ '$cagri' -lt '$yeniden' ]"

echo
(( fail == 0 )) || { echo "BAŞARISIZ"; exit 1; }
echo "Göç sırası doğru."
