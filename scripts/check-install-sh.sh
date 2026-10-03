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
FONKS=(calisan_ayni_mi vekil_parmak_izi yonetici_satiri_yaz kisitsiz_satir_sayisi)
for f in "${FONKS[@]}"; do
    sed -n "/^$f() {/,/^}/p" internal/bootstrap/install.sh >> "$FN"
done
for f in "${FONKS[@]}"; do
    grep -q "^$f() {" "$FN" || {
        echo "$f install.sh'tan çıkarılamadı — ölçüm geçersiz" >&2
        exit 1
    }
done

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
echo "== yonetici_satiri_yaz: authorized_keys (K-131) =="
D="$(mktemp -d)"
trap 'rm -f "$FN"; rm -rf "$D"' EXIT
YON='ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIYonetici erkan@dizustu'
CI_SATIRI='command="/usr/local/lib/kadran/kadran-connect -deploy=site",restrict ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIDagitim ci'
printf '%s\n' "$YON" > "$D/yon.pub"
printf '%s\n%s\n' "$YON" 'ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIIkinci baska' > "$D/iki.pub"
printf '%s\r\n' "$YON" > "$D/cr.pub"
# Her senaryo kendi dosyasıyla: önce eski bir yönetici satırı ve bir
# dağıtım satırı var (yükseltme anı).
hazirla() {
    printf '%s\n%s\n' "command=\"/eski/kadran-connect\",restrict $YON" "$CI_SATIRI" > "$D/$1"
}
ys() { printf "set -euo pipefail; source '%s'; %s" "$FN" "$1"; }

hazirla ak1
dene "dağıtım satırı yeniden kurulumda KORUNUYOR" \
    "$(ys "yonetici_satiri_yaz '$D/ak1' '$D/yon.pub' /usr/local/lib/kadran; grep -qxF '$CI_SATIRI' '$D/ak1'")"
hazirla ak2
dene "yönetici satırı DEĞİŞİYOR, iki kez koşunca da tek satır" \
    "$(ys "yonetici_satiri_yaz '$D/ak2' '$D/yon.pub' /usr/local/lib/kadran; yonetici_satiri_yaz '$D/ak2' '$D/yon.pub' /usr/local/lib/kadran; [ \"\$(grep -c AAAAIYonetici '$D/ak2')\" -eq 1 ]; grep -qxF 'command=\"/usr/local/lib/kadran/kadran-connect\",restrict $YON' '$D/ak2'; ! grep -q /eski/ '$D/ak2'")"
hazirla ak3
cp "$D/ak3" "$D/ak3.once"
dene "iki satırlı anahtar REDDEDİLİYOR ve dosyaya dokunulmuyor" \
    "$(ys "if yonetici_satiri_yaz '$D/ak3' '$D/iki.pub' /usr/local/lib/kadran; then exit 1; fi; cmp -s '$D/ak3' '$D/ak3.once'")"
hazirla ak4
dene "CR taşıyan anahtar REDDEDİLİYOR" \
    "$(ys "if yonetici_satiri_yaz '$D/ak4' '$D/cr.pub' /usr/local/lib/kadran; then exit 1; fi; true")"
# Kontrol grubu: tek-satır denetimi olmasaydı iki satırlı dosya
# authorized_keys'e kısıtsız ikinci bir satır yazardı.
dene "KONTROL: denetimsiz yazım kısıtsız satır üretir" \
    "f='$D/kontrol'; : > \"\$f\"; printf '%s\n' \"command=\\\"x\\\",restrict \$(cat '$D/iki.pub')\" >> \"\$f\"; grep -q '^ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIIkinci' \"\$f\""

echo
echo "== kisitsiz_satir_sayisi: kurulum sonrası denetim (K-131) =="
L=/usr/local/lib/kadran
printf '%s\n%s\n\n# yorum\n' "command=\"$L/kadran-connect\",restrict $YON" "$CI_SATIRI" > "$D/temiz"
printf '%s\n%s\n' "command=\"$L/kadran-connect\",restrict $YON" 'ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIIkinci baska' > "$D/kirli"
printf '%s\n' "command=\"$L/kadran-connect\" $YON" > "$D/restrictsiz"
printf '%s\n' "command=\"/bin/sh\",restrict $YON" > "$D/baska-komut"
: > "$D/bos"
dene "yönetici + dağıtım + yorum + boş satır → 0" \
    "$(ys "[ \"\$(kisitsiz_satir_sayisi '$D/temiz' $L)\" = 0 ]")"
dene "kısıtsız ikinci satır → 1" \
    "$(ys "[ \"\$(kisitsiz_satir_sayisi '$D/kirli' $L)\" = 1 ]")"
dene "restrict'siz satır → 1" \
    "$(ys "[ \"\$(kisitsiz_satir_sayisi '$D/restrictsiz' $L)\" = 1 ]")"
dene "başka komuta zorlanmış satır → 1" \
    "$(ys "[ \"\$(kisitsiz_satir_sayisi '$D/baska-komut' $L)\" = 1 ]")"
dene "boş dosya → 0 ve kurulum DURMAZ" \
    "$(ys "[ \"\$(kisitsiz_satir_sayisi '$D/bos' $L)\" = 0 ]")"
# Kontrol grubu: eski denetim kirli dosyayı GEÇİRİYORDU.
dene "KONTROL: eski denetim kısıtsız satırı geçirirdi" \
    "grep -q 'command=\"' '$D/kirli'"

echo
(( fail == 0 )) || { echo "BAŞARISIZ"; exit 1; }
echo "Kurulum betiği yardımcıları doğru."
