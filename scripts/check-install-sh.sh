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
FONKS=(calisan_ayni_mi vekil_parmak_izi istemci_olarak ak_yaz_istemci yonetici_satiri_yaz ssh_dizini_hazirla kisitsiz_satir_sayisi)
for f in "${FONKS[@]}"; do
    sed -n "/^$f() {/,/^}/p" internal/bootstrap/install.sh >> "$FN"
done
for f in "${FONKS[@]}"; do
    grep -q "^$f() {" "$FN" || {
        echo "$f install.sh'tan çıkarılamadı — ölçüm geçersiz" >&2
        exit 1
    }
done
# install.sh'ın `die`'ı tek satırlık; sed aralığı onu çıkaramaz. Aynı
# sözleşme: iletiyi bas, 1 ile çık.
printf '%s\n' 'die() { printf "HATA: %s\n" "$*" >&2; exit 1; }' >> "$FN"

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
# Yazıcı authorized_keys'i bu kimlikle yazıyor (K-139). Root'suz koşuda
# betiği koşturan kullanıcının kendisi; sayısal: konteynerde uid'in adı
# olmayabilir.
U="$(id -u)"
G="$(id -g)"
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
    "$(ys "yonetici_satiri_yaz '$D/ak1' '$D/yon.pub' /usr/local/lib/kadran '$U' '$G'; grep -qxF '$CI_SATIRI' '$D/ak1'")"
hazirla ak2
dene "yönetici satırı DEĞİŞİYOR, iki kez koşunca da tek satır" \
    "$(ys "yonetici_satiri_yaz '$D/ak2' '$D/yon.pub' /usr/local/lib/kadran '$U' '$G'; yonetici_satiri_yaz '$D/ak2' '$D/yon.pub' /usr/local/lib/kadran '$U' '$G'; [ \"\$(grep -c AAAAIYonetici '$D/ak2')\" -eq 1 ]; grep -qxF 'command=\"/usr/local/lib/kadran/kadran-connect\",restrict $YON' '$D/ak2'; ! grep -q /eski/ '$D/ak2'")"
hazirla ak3
cp "$D/ak3" "$D/ak3.once"
dene "iki satırlı anahtar REDDEDİLİYOR ve dosyaya dokunulmuyor" \
    "$(ys "if yonetici_satiri_yaz '$D/ak3' '$D/iki.pub' /usr/local/lib/kadran '$U' '$G'; then exit 1; fi; cmp -s '$D/ak3' '$D/ak3.once'")"
hazirla ak4
dene "CR taşıyan anahtar REDDEDİLİYOR" \
    "$(ys "if yonetici_satiri_yaz '$D/ak4' '$D/cr.pub' /usr/local/lib/kadran '$U' '$G'; then exit 1; fi; true")"
# Kontrol grubu: tek-satır denetimi olmasaydı iki satırlı dosya
# authorized_keys'e kısıtsız ikinci bir satır yazardı.
dene "KONTROL: denetimsiz yazım kısıtsız satır üretir" \
    "f='$D/kontrol'; : > \"\$f\"; printf '%s\n' \"command=\\\"x\\\",restrict \$(cat '$D/iki.pub')\" >> \"\$f\"; grep -q '^ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIIkinci' \"\$f\""

echo
echo "== yonetici_satiri_yaz: root, kadran-client'ın dizinine yazıyor (K-137) =="
# .ssh kadran-client'ın; o kullanıcı oraya bağ koyabilir. Root bir bağı
# izlerse istemcinin seçtiği dosyanın üstüne yazar. Hata senaryoları
# `if (…)` içinde: kurulum fonksiyonu `|| die` bağlamında çağırıyor ve
# orada `set -e` KAPALI; doğrudan çağrı bu farkı gizlerdi.
YENI_SATIR="command=\"/usr/local/lib/kadran/kadran-connect\",restrict $YON"
hazirla ak5
printf 'kurban\n' > "$D/kurban5"
cp "$D/kurban5" "$D/kurban5.once"
ln -s "$D/kurban5" "$D/ak5.yeni"
dene "önceden konmuş ak.yeni bağı İZLENMİYOR, kurban aynı kalıyor" \
    "$(ys "yonetici_satiri_yaz '$D/ak5' '$D/yon.pub' /usr/local/lib/kadran '$U' '$G'; cmp -s '$D/kurban5' '$D/kurban5.once'; [ ! -L '$D/ak5' ]; grep -qxF '$YENI_SATIR' '$D/ak5'; grep -qxF '$CI_SATIRI' '$D/ak5'")"
printf 'kurban\n' > "$D/kurban6"
cp "$D/kurban6" "$D/kurban6.once"
ln -s "$D/kurban6" "$D/ak6"
dene "authorized_keys bağsa kurulum DURUYOR, kurbana dokunulmuyor" \
    "$(ys "if (yonetici_satiri_yaz '$D/ak6' '$D/yon.pub' /usr/local/lib/kadran '$U' '$G'); then exit 1; fi; cmp -s '$D/kurban6' '$D/kurban6.once'; [ -L '$D/ak6' ]")"
mkfifo "$D/ak7"
dene "authorized_keys düzenli dosya değilse (FIFO) DURUYOR, takılmıyor" \
    "set -e; rc=0; timeout 5 bash -c \"source '$FN'; yonetici_satiri_yaz '$D/ak7' '$D/yon.pub' /usr/local/lib/kadran '$U' '$G'\" || rc=\$?; [ \"\$rc\" -eq 1 ]; [ -p '$D/ak7' ]"
printf '%s\n' "command=\"/eski/kadran-connect\",restrict $YON" > "$D/ak8"
dene "yalnız yönetici satırı varken de DEĞİŞİYOR (grep'in 1'i hata değil)" \
    "$(ys "yonetici_satiri_yaz '$D/ak8' '$D/yon.pub' /usr/local/lib/kadran '$U' '$G'; [ \"\$(wc -l < '$D/ak8')\" -eq 1 ]; grep -qxF '$YENI_SATIR' '$D/ak8'")"
dene "authorized_keys yokken 0600 doğuyor" \
    "$(ys "yonetici_satiri_yaz '$D/ak9' '$D/yon.pub' /usr/local/lib/kadran '$U' '$G'; [ \"\$(stat -c %a '$D/ak9')\" = 600 ]; [ \"\$(wc -l < '$D/ak9')\" -eq 1 ]; grep -qxF '$YENI_SATIR' '$D/ak9'")"
printf '%s' "$CI_SATIRI" > "$D/ak10"
dene "son satırı satır sonu taşımayan dosyada satırlar YAPIŞMIYOR" \
    "$(ys "yonetici_satiri_yaz '$D/ak10' '$D/yon.pub' /usr/local/lib/kadran '$U' '$G'; grep -qxF '$CI_SATIRI' '$D/ak10'; grep -qxF '$YENI_SATIR' '$D/ak10'")"
# Okuma hatası root'ta kurulamaz (root 000 dosyayı da okur). CI bu betiği
# root'suz koşturuyor; orada her seferinde ölçülüyor.
if [ "$(id -u)" -ne 0 ]; then
    hazirla ak11
    chmod 000 "$D/ak11"
    dene "okunamayan authorized_keys'te DURUYOR, dağıtım satırı düşmüyor" \
        "$(ys "if (yonetici_satiri_yaz '$D/ak11' '$D/yon.pub' /usr/local/lib/kadran '$U' '$G'); then exit 1; fi; chmod 600 '$D/ak11'; grep -qxF '$CI_SATIRI' '$D/ak11'")"
else
    echo "  - okunamayan dosya senaryosu root'ta kurulamaz; root'suz koşturun (CI öyle)"
fi

echo
echo "== ssh_dizini_hazirla: bağlı .ssh (K-137) =="
mkdir "$D/ev1"
dene ".ssh yokken 0700 kuruluyor" \
    "$(ys "ssh_dizini_hazirla '$D/ev1/.ssh' '$U' '$G'; [ -d '$D/ev1/.ssh' ]; [ \"\$(stat -c %a '$D/ev1/.ssh')\" = 700 ]")"
mkdir "$D/ev2" "$D/kurban-dizin2"
chmod 0755 "$D/kurban-dizin2"
ln -s "$D/kurban-dizin2" "$D/ev2/.ssh"
dene ".ssh bağsa kurulum DURUYOR, hedef dizine dokunulmuyor" \
    "$(ys "if (ssh_dizini_hazirla '$D/ev2/.ssh' '$U' '$G'); then exit 1; fi; [ \"\$(stat -c %a '$D/kurban-dizin2')\" = 755 ]; [ -L '$D/ev2/.ssh' ]")"
# Kontrol grubu: korumasız `install -d` bağı GERÇEKTEN izliyor — yoksa
# yukarıdaki ölçüm bir şey kanıtlamıyor olurdu.
mkdir "$D/ev3" "$D/kurban-dizin3"
chmod 0755 "$D/kurban-dizin3"
ln -s "$D/kurban-dizin3" "$D/ev3/.ssh"
dene "KONTROL: korumasız install -d bağlı .ssh'nin HEDEFİNİ değiştirir" \
    "install -d -m 0700 '$D/ev3/.ssh'; [ \"\$(stat -c %a '$D/kurban-dizin3')\" = 700 ]"

echo
echo "== kadran-client olarak yazma (K-139) =="
# Root bağ denetimini yapıyor, yazmayı kadran-client'ın kimliğiyle
# (setpriv) yapıyor. Denetim ile yazma arasındaki pencerede konan bir bağ
# artık o kullanıcının kendi yetkisinden fazlasını açamıyor. Pencere
# deterministik kurulamaz; yerine yazıcıyı denetimsiz çağırıyoruz (denetimin
# kaçırdığı durumun aynısı) ve bağın hedefi root'un dosyası.
#
# Gerçek bir kullanıcı ve setpriv ister: yalnız root'ta. CI bu betiği bir
# kez de sudo ile koşturuyor ve orada bu bölüm ZORUNLU
# (KADRAN_TEST_REQUIRE_ROOT=1).
if [ "$(id -u)" -eq 0 ]; then
    KUL=kadran-k139-test
    if getent passwd "$KUL" >/dev/null; then userdel "$KUL" >/dev/null 2>&1; fi
    if getent group "$KUL" >/dev/null; then groupdel "$KUL" >/dev/null 2>&1; fi
    useradd --system --user-group --no-create-home --shell /usr/sbin/nologin "$KUL" \
        || { echo "  ✗ deneme kullanıcısı kurulamadı — ölçüm YAPILMADI"; exit 1; }
    K="$(mktemp -d)"
    chmod 0755 "$K"
    trap 'rm -f "$FN"; rm -rf "$D" "$K"; userdel "$KUL" >/dev/null 2>&1; groupdel "$KUL" >/dev/null 2>&1' EXIT
    for ev in ev ev-kontrol ev-dizin ev-kok ev-kokdizin; do
        install -d -o "$KUL" -g "$KUL" -m 0755 "$K/$ev"
    done
    printf 'GIZLI-K139\n' > "$K/gizli"
    chmod 0600 "$K/gizli"
    AKB='ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIYonetici'

    dene "ssh_dizini_hazirla: .ssh istemcinin, 0700" \
        "$(ys "ssh_dizini_hazirla '$K/ev/.ssh' $KUL $KUL; [ \"\$(stat -c '%U %a' '$K/ev/.ssh')\" = '$KUL 700' ]")"
    dene "yonetici_satiri_yaz: yeni authorized_keys istemcinin, 0600" \
        "$(ys "yonetici_satiri_yaz '$K/ev/.ssh/authorized_keys' '$D/yon.pub' /usr/local/lib/kadran $KUL $KUL; [ \"\$(stat -c '%U %a' '$K/ev/.ssh/authorized_keys')\" = '$KUL 600' ]; grep -qxF '$YENI_SATIR' '$K/ev/.ssh/authorized_keys'")"
    printf '%s\n' "$CI_SATIRI" >> "$K/ev/.ssh/authorized_keys"
    dene "yükseltme: dağıtım satırı istemci olarak okunup KORUNUYOR" \
        "$(ys "yonetici_satiri_yaz '$K/ev/.ssh/authorized_keys' '$D/yon.pub' /usr/local/lib/kadran $KUL $KUL; grep -qxF '$CI_SATIRI' '$K/ev/.ssh/authorized_keys'; [ \"\$(grep -c AAAAIYonetici '$K/ev/.ssh/authorized_keys')\" -eq 1 ]; [ \"\$(stat -c %U '$K/ev/.ssh/authorized_keys')\" = $KUL ]")"

    ln -s "$K/gizli" "$K/ev/.ssh/ak-pencere"
    dene "pencerede konan bağ: istemci root'un dosyasını OKUYAMIYOR, sır sızmıyor" \
        "$(ys "if istemci_olarak $KUL $KUL ak_yaz_istemci '$K/ev/.ssh/ak-pencere' '$AKB' '$YENI_SATIR'; then exit 1; fi; [ -L '$K/ev/.ssh/ak-pencere' ]; ! grep -rqs GIZLI-K139 '$K/ev'; [ \"\$(stat -c '%U %a' '$K/gizli')\" = 'root 600' ]; grep -qx GIZLI-K139 '$K/gizli'")"
    install -d -o "$KUL" -g "$KUL" -m 0700 "$K/ev-kontrol/.ssh"
    ln -s "$K/gizli" "$K/ev-kontrol/.ssh/authorized_keys"
    dene "KONTROL: aynı yazıcı root olarak koşunca bağın hedefini okuyup istemcinin dizinine yazar" \
        "$(ys "ak_yaz_istemci '$K/ev-kontrol/.ssh/authorized_keys' '$AKB' '$YENI_SATIR'; [ ! -L '$K/ev-kontrol/.ssh/authorized_keys' ]; grep -qx GIZLI-K139 '$K/ev-kontrol/.ssh/authorized_keys'")"

    install -d -m 0755 "$K/kok-dizin"
    ln -s "$K/kok-dizin" "$K/ev-dizin/.ssh"
    dene "pencerede bağa dönen .ssh: istemci olarak install -d root'un dizinini DEĞİŞTİRMİYOR" \
        "$(ys "if istemci_olarak $KUL $KUL install -d -m 0700 '$K/ev-dizin/.ssh'; then exit 1; fi; [ \"\$(stat -c '%U %a' '$K/kok-dizin')\" = 'root 755' ]")"

    install -d -o "$KUL" -g "$KUL" -m 0700 "$K/ev-kok/.ssh"
    printf '%s\n' "$CI_SATIRI" > "$K/ev-kok/.ssh/authorized_keys"
    chmod 0600 "$K/ev-kok/.ssh/authorized_keys"
    cp "$K/ev-kok/.ssh/authorized_keys" "$K/ak-kok.once"
    dene "root'a ait authorized_keys: kurulum DURUYOR, dosyaya dokunulmuyor" \
        "$(ys "if (yonetici_satiri_yaz '$K/ev-kok/.ssh/authorized_keys' '$D/yon.pub' /usr/local/lib/kadran $KUL $KUL); then exit 1; fi; cmp -s '$K/ev-kok/.ssh/authorized_keys' '$K/ak-kok.once'; [ \"\$(stat -c %U '$K/ev-kok/.ssh/authorized_keys')\" = root ]")"

    install -d -m 0755 "$K/ev-kokdizin/.ssh"
    dene "root'a ait .ssh: kurulum DURUYOR, dizine dokunulmuyor" \
        "$(ys "if (ssh_dizini_hazirla '$K/ev-kokdizin/.ssh' $KUL $KUL); then exit 1; fi; [ \"\$(stat -c '%U %a' '$K/ev-kokdizin/.ssh')\" = 'root 755' ]")"
else
    dene "istemci_olarak root'suzken başka kullanıcıya geçmeyi REDDEDİYOR" \
        "$(ys "if istemci_olarak root root true; then exit 1; fi; true")"
    if [ "${KADRAN_TEST_REQUIRE_ROOT:-}" = 1 ]; then
        echo "  ✗ K-139 bölümü root ister ve KADRAN_TEST_REQUIRE_ROOT=1 — ölçüm YAPILMADI"
        fail=1
    else
        echo "  - K-139 bölümü root ister (useradd, setpriv); CI bunu sudo ile koşturuyor"
    fi
fi

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
