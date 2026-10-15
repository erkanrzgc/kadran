#!/usr/bin/env bash
# Firma tablosundaki resmi yardim baglantilarini (Guide) acmayi dener (K-151).
# CI'da KOSMAZ: dis sitelere cikiyor ve sonuc zamanla degisiyor. Bir firma
# menusunu ya da yardim sayfasini degistirince elle kosulur.
#
# 403/429 cogu zaman bot korumasidir (or. Akamai): o baglanti tarayicida
# elle acilarak dogrulanir, betik yalniz listeler.
set -uo pipefail
cd "$(dirname "$0")/.."
fail=0
while read -r url; do
    code="$(curl -sSL -o /dev/null -m 20 -A 'Mozilla/5.0 kadran-link-check' -w '%{http_code}' "$url" 2>/dev/null)"
    printf '%s  %s\n' "${code:-000}" "$url"
    case "$code" in 2??) ;; *) fail=1 ;; esac
done < <(grep -oE 'Guide: +"https://[^"]+"' internal/domaincheck/providers.go | sed -E 's/.*"(https:[^"]+)"/\1/')
exit $fail
