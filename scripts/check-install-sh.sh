#!/usr/bin/env bash
# Kurulum betiğinin yardımcı fonksiyonlarını GERÇEKTEN çalıştırır (K-112).
#
# install.sh `set -euo pipefail` ile koşuyor. Bu kipte bir dosya eksik
# diye düşen bir boru hattı, korumasız bir `x="$(…)"` atamasında BÜTÜN
# kurulumu sessizce durdurur. Go testleri betiği metin olarak okuyor ve
# bunu göremez: taze kurulumda (dosyalar henüz yokken) çalışan ilk
# vekil_parmak_izi taslağı tam bu yüzden kurulumu bozacaktı.
set -uo pipefail
cd "$(dirname "$0")/.."

fail=0
dene() {
    local ad="$1" kod="$2"
    if bash -c "$kod" >/dev/null 2>&1; then echo "  ✓ $ad"; else echo "  ✗ $ad"; fail=1; fi
}

FN="$(mktemp)"
trap 'rm -f "$FN"' EXIT
for f in calisan_ayni_mi vekil_parmak_izi; do
    sed -n "/^$f() {/,/^}/p" internal/bootstrap/install.sh >> "$FN"
done
grep -q '^vekil_parmak_izi() {' "$FN" && grep -q '^calisan_ayni_mi() {' "$FN" || {
    echo "fonksiyonlar install.sh'tan çıkarılamadı — ölçüm geçersiz" >&2
    exit 1
}

echo "== install.sh yardımcıları, set -euo pipefail altında =="
dene "vekil_parmak_izi: dosyalar yokken kurulum DURMAZ" \
    "set -euo pipefail; source '$FN'; x=\"\$(vekil_parmak_izi)\"; [ -n \"\$x\" ]"
dene "calisan_ayni_mi: olmayan birim → yanlış, kurulum DURMAZ" \
    "set -euo pipefail; source '$FN'; LIB_DIR=/yok; if calisan_ayni_mi yok-boyle.service /yok/ikili; then exit 1; fi; true"
# Kontrol grubu: korumasız hâl GERÇEKTEN durdurur — yoksa yukarıdaki
# ölçüm bir şey kanıtlamıyor olurdu.
dene "KONTROL: korumasız boru hattı set -e altında durdurur" \
    "! bash -c 'set -euo pipefail; f() { cat /yok/a 2>/dev/null | md5sum; }; x=\"\$(f)\"; true'"

echo
(( fail == 0 )) || { echo "BAŞARISIZ"; exit 1; }
echo "Kurulum betiği yardımcıları doğru."
