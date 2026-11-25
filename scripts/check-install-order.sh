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
echo "== kadran-git soketleri kadrand'dan ÖNCE (K-167) =="
# kadrand.service Wants=kadran-git-admin.socket: soketler kadrand yeniden
# başlamadan önce etkin olmalı. Fonksiyon sahte komutlarla koşuyor; çağrı
# SIRASI günlüğe yazılıyor.
GFN="$D/git-fonks"
: > "$GFN"
for f in git_kullanicisi git_birimleri git_araci_kur; do
    sed -n "/^$f() {/,/^}/p" internal/bootstrap/install.sh >> "$GFN"
    grep -q "^$f() {" "$GFN" || { echo "$f install.sh'tan çıkarılamadı — ölçüm geçersiz" >&2; exit 1; }
done
GIT_GUNLUK="$D/git-gunluk"
# git_araci_kur_sahte <GIT_ARACI> <günlük>: fonksiyonu sahte komutlarla koşturur.
git_araci_kur_sahte() {
    : > "$2"
    bash -c "
        set -euo pipefail
        STAGE=/stage; NOLOGIN=/usr/sbin/nologin; GIT_ARACI='$1'
        step() { :; }; say() { :; }
        die() { echo HATA \"\$*\" >&2; exit 1; }
        log() { echo \"\$*\" >> '$2'; }
        getent() { return 1; }
        id() { case \"\$*\" in \"-u \"*) return 1;; \"-nG kadran-git\") echo kadran-git;; *) echo kadran;; esac; }
        groupadd() { log groupadd \"\$@\"; }
        useradd() { log useradd \"\$@\"; }
        install() { log install \"\$@\"; }
        systemctl() { log systemctl \"\$@\"; }
        source '$GFN'
        git_araci_kur
    " >/dev/null 2>&1
}
git_araci_kur_sahte 1 "$GIT_GUNLUK"
dene "git_araci_kur hatasız koşuyor" "[ -s '$GIT_GUNLUK' ]"
# systemd-resolved yokken (GIT_ARACI=0) aracı ATLANIR: kullanıcı, birim ve
# soket kurulmaz, hiçbir systemctl çağrısı yapılmaz. Çıkış 0 olmalı.
GIT_ATLA="$D/git-atla"
atla_rc=0
git_araci_kur_sahte 0 "$GIT_ATLA" || atla_rc=$?
dene "GIT_ARACI=0: git_araci_kur hatasız döner" "[ '$atla_rc' -eq 0 ]"
dene "GIT_ARACI=0: hiçbir şey kurulmuyor (kullanıcı, birim, soket yok)" \
    "[ -e '$GIT_ATLA' ] && [ ! -s '$GIT_ATLA' ]"
SIRA_GIT="$(declare -f sira)"
gsira() { # gsira <ad> <desen1> <desen2>
    dene "$1" "$SIRA_GIT; ARAC_GUNLUK='$GIT_GUNLUK'; sira '$2' '$3'"
}
gsira "kullanıcı birim dosyalarından ÖNCE" 'useradd.*kadran-git' 'install.*kadran-git-admin.socket'
gsira "birim dosyaları daemon-reload'dan ÖNCE" 'install.*kadran-git-fetch.socket' 'systemctl daemon-reload'
gsira "daemon-reload soketlerin enable'ından ÖNCE" 'systemctl daemon-reload' 'systemctl enable kadran-git-admin.socket kadran-git-fetch.socket'
gsira "enable soketlerin yeniden başlatılmasından ÖNCE" 'systemctl enable kadran-git-admin.socket' 'systemctl restart kadran-git-admin.socket kadran-git-fetch.socket'
gsira "soketler yeniden başlatıldıktan SONRA servis try-restart" 'systemctl restart kadran-git-admin.socket' 'systemctl try-restart kadran-git.service'
dene "git servisi başlatılmıyor (soketle etkinleşir)" \
    "! grep -Eq 'systemctl (start|restart) kadran-git.service' '$GIT_GUNLUK'"
# Ana akış: git_araci_kur kadrand'ın yeniden başlatmasından önce.
gcagri="$(grep -n '^git_araci_kur$' internal/bootstrap/install.sh | head -1 | cut -d: -f1)"
dene "ana akış: git_araci_kur çağrısı var" "[ -n '$gcagri' ]"
dene "ana akış: git_araci_kur kadrand yeniden başlatmasından ÖNCE" \
    "[ -n '$gcagri' ] && [ -n '$yeniden' ] && [ '$gcagri' -lt '$yeniden' ]"
# Ön koşullar hiçbir değişiklikten ÖNCE: Docker adres havuzu adımı ilk
# yazan adım, ikili kurulumu ondan sonra.
oncagri="$(grep -n '^git_onkosul_resolved$' internal/bootstrap/install.sh | head -1 | cut -d: -f1)"
onceki="$(grep -n '^git_onkosul_eski_kimlik$' internal/bootstrap/install.sh | head -1 | cut -d: -f1)"
havuz="$(grep -n '^step "Docker address pool"' internal/bootstrap/install.sh | head -1 | cut -d: -f1)"
ikili="$(grep -n '^ikili_kur "\$STAGE/kadrand"' internal/bootstrap/install.sh | head -1 | cut -d: -f1)"
dene "ön koşul (resolved) ilk değişiklikten ÖNCE" \
    "[ -n '$oncagri' ] && [ -n '$havuz' ] && [ '$oncagri' -lt '$havuz' ] && [ '$oncagri' -lt '$ikili' ]"
dene "ön koşul (eski kimlik) ilk değişiklikten ÖNCE" \
    "[ -n '$onceki' ] && [ -n '$havuz' ] && [ '$onceki' -lt '$havuz' ] && [ '$onceki' -lt '$ikili' ]"

echo
echo "== Ters vekil kadrand'dan hemen ÖNCE yeniden başlıyor (K-169) =="
# Caddy değişince ters_vekil_baslat kadrand'ı durdurup Caddy'yi rotasız açar;
# rotaları kadrand yükler. Arada başka adım koşarsa site o kadar kapalı kalır
# (v0.14.0, GCP: SSH, git aracı ve executor yeniden başlatmaları → 7,8 sn).
vbas="$(grep -n '^    ters_vekil_baslat$' internal/bootstrap/install.sh | head -1 | cut -d: -f1)"
sshadim="$(grep -n '^step "SSH configuration"' internal/bootstrap/install.sh | head -1 | cut -d: -f1)"
execr="$(grep -n '^systemctl restart kadran-exec.service$' internal/bootstrap/install.sh | head -1 | cut -d: -f1)"
dene "ana akış: ters_vekil_baslat çağrısı var" "[ -n '$vbas' ]"
dene "vekil başlatması SSH adımından SONRA" \
    "[ -n '$vbas' ] && [ -n '$sshadim' ] && [ '$vbas' -gt '$sshadim' ]"
dene "vekil başlatması git_araci_kur'dan SONRA" \
    "[ -n '$vbas' ] && [ -n '$gcagri' ] && [ '$vbas' -gt '$gcagri' ]"
dene "vekil başlatması executor yeniden başlatmasından SONRA" \
    "[ -n '$vbas' ] && [ -n '$execr' ] && [ '$vbas' -gt '$execr' ]"
dene "vekil başlatması kadrand yeniden başlatmasından ÖNCE" \
    "[ -n '$vbas' ] && [ -n '$yeniden' ] && [ '$vbas' -lt '$yeniden' ]"
# Aradaki satırlar yalnız if kapanışı, yorum ya da boş satır olabilir.
dene "vekil başlatmasıyla kadrand arasında başka komut yok" \
    "[ -n '$vbas' ] && [ -n '$yeniden' ] && ! sed -n \"\$(( $vbas + 1 )),\$(( $yeniden - 1 ))p\" internal/bootstrap/install.sh | grep -Ev '^[[:space:]]*(#|\$)|^fi\$' | grep -q ."
dene "ters vekil hazırlığı SSH adımından ÖNCE (K-050 denetimi erken düşer)" \
    "h=\"\$(grep -n '^    ters_vekil_hazirla\$' internal/bootstrap/install.sh | head -1 | cut -d: -f1)\"; [ -n \"\$h\" ] && [ \"\$h\" -lt '$sshadim' ]"

echo
(( fail == 0 )) || { echo "BAŞARISIZ"; exit 1; }
echo "Göç sırası doğru."
