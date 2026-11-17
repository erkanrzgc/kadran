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
FONKS=(calisan_ayni_mi vekil_parmak_izi istemci_olarak ak_yaz_istemci yonetici_satiri_yaz ssh_dizini_hazirla kisitsiz_satir_sayisi docker_havuzu_sabitle ikili_kur offsite_cozucu_kotu git_resolved_uyarisi git_onkosul_resolved git_onkosul_eski_kimlik git_kullanicisi)
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
echo "== yonetici_satiri_yaz: kadran-client'ın dizininde bağ denetimi (K-137) =="
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
echo "== ikili_kur: aynı ikili yeniden YAZILMIYOR (K-158) =="
# `install` her koşuda yeni bir dosya açar. Yeniden başlatılmayan servis
# silinmiş dosyada kalır ve needrestart onu ilk paket yükseltmesinde
# yeniden başlatır (Hetzner, 29 Ekim: ters vekil). Dosyanın yenilenip
# yenilenmediğini ikinci bir sabit bağ gösteriyor: yenilenirse bağ eski
# dosyada kalır, çalışan sürecin tuttuğu gibi, ve `-ef` yanlış olur.
#
# root:root sahipliği root ister. CI bu betiği bir kez de sudo ile
# koşturuyor ve orada bu bölüm ZORUNLU (KADRAN_TEST_REQUIRE_ROOT=1).
if [ "$(id -u)" -eq 0 ]; then
    B="$(mktemp -d)"
    printf 'ikili-v2\n' > "$B/kaynak"
    cp "$B/kaynak" "$B/ayni"
    chmod 0700 "$B/ayni"
    chown 65534:65534 "$B/ayni"
    ln "$B/ayni" "$B/ayni.ilk"
    dene "aynı içerik: dosya YENİLENMİYOR, sahip ve izin root:root 0755'e dönüyor" \
        "$(ys "ikili_kur '$B/kaynak' '$B/ayni'; [ '$B/ayni' -ef '$B/ayni.ilk' ]; [ \"\$(stat -c '%u:%g %a' '$B/ayni')\" = '0:0 755' ]; cmp -s '$B/kaynak' '$B/ayni'")"
    printf 'ikili-v1\n' > "$B/farkli"
    dene "farklı içerik: yenisi root:root 0755 yazılıyor" \
        "$(ys "ikili_kur '$B/kaynak' '$B/farkli'; cmp -s '$B/kaynak' '$B/farkli'; [ \"\$(stat -c '%u:%g %a' '$B/farkli')\" = '0:0 755' ]")"
    dene "hedef yokken kuruluyor" \
        "$(ys "ikili_kur '$B/kaynak' '$B/yeni'; cmp -s '$B/kaynak' '$B/yeni'; [ \"\$(stat -c '%u:%g %a' '$B/yeni')\" = '0:0 755' ]")"
    # Bağın hedefi kaynakla AYNI içerikte: karşılaştırma bağı izleseydi
    # sahip ve izin düzeltmesi bağın hedefini değiştirirdi.
    cp "$B/kaynak" "$B/kurban"
    chmod 0600 "$B/kurban"
    chown 65534:65534 "$B/kurban"
    ln -s "$B/kurban" "$B/bag"
    dene "hedef bağsa izlenmiyor: bağın hedefi aynı kalıyor, yerine düz dosya" \
        "$(ys "ikili_kur '$B/kaynak' '$B/bag'; [ ! -L '$B/bag' ]; cmp -s '$B/kaynak' '$B/bag'; [ \"\$(stat -c '%u:%g %a' '$B/kurban')\" = '65534:65534 600' ]")"
    # Kontrol grubu: düz `install` aynı içerikte de dosyayı yeniliyor —
    # yoksa ilk ölçüm bir şey kanıtlamıyor olurdu.
    cp "$B/kaynak" "$B/kontrol"
    ln "$B/kontrol" "$B/kontrol.ilk"
    dene "KONTROL: düz install aynı içerikte de dosyayı yeniliyor" \
        "install -m 0755 -o root -g root '$B/kaynak' '$B/kontrol'; [ ! '$B/kontrol' -ef '$B/kontrol.ilk' ]"
    rm -rf "$B"
else
    if [ "${KADRAN_TEST_REQUIRE_ROOT:-}" = 1 ]; then
        echo "  ✗ K-158 bölümü root ister ve KADRAN_TEST_REQUIRE_ROOT=1 — ölçüm YAPILMADI"
        fail=1
    else
        echo "  - K-158 bölümü root ister (chown root:root); CI bunu sudo ile koşturuyor"
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
echo "== docker_havuzu_sabitle: dockerd adres havuzu (K-150) =="
H="$(mktemp -d)"
trap 'rm -f "$FN"; rm -rf "$D" "$H"' EXIT
mkdir -p "$H/var" "$H/elle" "$H/bag"
printf '%s\n' '{"default-address-pools":[{"base":"10.10.0.0/16","size":24}]}' > "$H/var/daemon.json"
printf '%s\n' '{"log-driver":"journald"}' > "$H/elle/daemon.json"
cp "$H/var/daemon.json" "$H/var/once"; cp "$H/elle/daemon.json" "$H/elle/once"
ln -s "$H/yok/hedef.json" "$H/bag/daemon.json"
dene "dosya yoksa dizin dahil yazılıyor, sonuç 'yazildi'" \
    "$(ys "[ \"\$(docker_havuzu_sabitle '$H/yeni/daemon.json')\" = yazildi ]")"
printf '{
  "default-address-pools": [
    {"base": "172.16.0.0/12", "size": 16}
  ]
}
' > "$H/beklenen.json"
dene "yazılan dosya: 172.16.0.0/12, ağ başına /16 (bayt bayt)" "cmp -s '$H/yeni/daemon.json' '$H/beklenen.json'"
dene "geçici dosya geride kalmıyor" "[ ! -e '$H/yeni/daemon.json.kadran-yeni' ]"
dene "ikinci koşu 'var' der ve dosyaya dokunmaz" \
    "$(ys "cp '$H/yeni/daemon.json' '$H/yeni/once'; [ \"\$(docker_havuzu_sabitle '$H/yeni/daemon.json')\" = var ]; cmp -s '$H/yeni/daemon.json' '$H/yeni/once'")"
dene "operatörün havuzu 'var' ve DEĞİŞMİYOR" \
    "$(ys "[ \"\$(docker_havuzu_sabitle '$H/var/daemon.json')\" = var ]; cmp -s '$H/var/daemon.json' '$H/var/once'")"
dene "havuzsuz daemon.json 'elle' ve DEĞİŞMİYOR" \
    "$(ys "[ \"\$(docker_havuzu_sabitle '$H/elle/daemon.json')\" = elle ]; cmp -s '$H/elle/daemon.json' '$H/elle/once'")"
dene "kırık bağ var sayılıyor, hedefine YAZILMIYOR" \
    "$(ys "[ \"\$(docker_havuzu_sabitle '$H/bag/daemon.json')\" = elle ]; [ -L '$H/bag/daemon.json' ]; [ ! -e '$H/yok/hedef.json' ]")"
# Kontrol grubu: yalnız `-e` bakan bir denetim kırık bağı "yok" sayar ve
# bağın hedefine yazardı; `-L` bu yüzden var.
dene "KONTROL: kırık bağ -e ile görünmez, -L ile görünür"     "[ ! -e '$H/bag/daemon.json' ] && [ -L '$H/bag/daemon.json' ]"

echo
echo "== kadran-git: ön koşullar ve kullanıcı (K-167) =="
# Fonksiyonlar GERÇEKTEN koşuyor (set -euo pipefail altında). Kök dizin
# KOK önekiyle sahtelenir; üretimde KOK boş. Beklenen hata iletisi birebir
# aranıyor: ileti değişirse ya da kontrol düşerse kırmızı.
G="$(mktemp -d)"
trap 'rm -f "$FN"; rm -rf "$D" "$H" "$G"' EXIT
hata_bekle() {
    local ad="$1" ileti="$2" cikti rc=0
    shift 2
    cikti="$(bash -c "set -euo pipefail; source '$FN'; $*" 2>&1)" || rc=$?
    if [ "$rc" -ne 0 ] && grep -qF -- "$ileti" <<<"$cikti"; then echo "  ✓ $ad"; else echo "  ✗ $ad (çıkış $rc: $cikti)"; fail=1; fi
}
gecer() {
    local ad="$1" cikti rc=0
    shift 1
    cikti="$(bash -c "set -euo pipefail; source '$FN'; $*" 2>&1)" || rc=$?
    if [ "$rc" -eq 0 ]; then echo "  ✓ $ad"; else echo "  ✗ $ad (çıkış $rc: $cikti)"; fail=1; fi
}
# git_onkosul_resolved'ın üç yolu (son inceleme I1). Her fikstür KOK'u ve
# systemctl'i kendisi kurar: boş KOK gerçek /etc/systemd/system'i okurdu.
# kurar: çıkış 0, GIT_ARACI=1, uyarı yok. atlar: çıkış 0, GIT_ARACI=0, uyarı var.
RESOLVED_ILETI='kadran-git needs systemd-resolved (stub resolver at 127.0.0.53)'
KURULU_ILETI='and kadran-git is already set up on this server'
UYARI_ILETI='private GitHub'
git_karari() {
    local ad="$1" bekle="$2" cikti rc=0
    shift 2
    cikti="$(bash -c "set -euo pipefail; source '$FN'; $*; echo \"GIT_ARACI=\$GIT_ARACI\"" 2>&1)" || rc=$?
    if [ "$rc" -ne 0 ] || ! grep -qx "GIT_ARACI=$bekle" <<<"$cikti"; then
        echo "  ✗ $ad (çıkış $rc: $cikti)"; fail=1; return
    fi
    if [ "$bekle" = 0 ] && ! grep -qF -- "$UYARI_ILETI" <<<"$cikti"; then
        echo "  ✗ $ad (uyarı basılmadı: $cikti)"; fail=1; return
    fi
    if [ "$bekle" = 1 ] && grep -qF -- "$UYARI_ILETI" <<<"$cikti"; then
        echo "  ✗ $ad (resolved varken uyarı basıldı: $cikti)"; fail=1; return
    fi
    echo "  ✓ $ad"
}
mkdir -p "$G/iyi/run/systemd/resolve" "$G/gcp/run/systemd/resolve" "$G/bos" \
    "$G/kurulu/etc/systemd/system" "$G/soketli/etc/systemd/system" \
    "$G/iyi-kurulu/run/systemd/resolve" "$G/iyi-kurulu/etc/systemd/system"
printf 'nameserver 127.0.0.53\noptions edns0 trust-ad\n' > "$G/iyi/run/systemd/resolve/stub-resolv.conf"
cp "$G/iyi/run/systemd/resolve/stub-resolv.conf" "$G/iyi-kurulu/run/systemd/resolve/stub-resolv.conf"
printf 'nameserver 169.254.169.254\n' > "$G/gcp/run/systemd/resolve/stub-resolv.conf"
: > "$G/kurulu/etc/systemd/system/kadran-git.service"
: > "$G/iyi-kurulu/etc/systemd/system/kadran-git.service"
: > "$G/soketli/etc/systemd/system/kadran-git-fetch.socket"
AKTIF="systemctl() { return 0; }"
PASIF="systemctl() { return 3; }"
git_karari "resolved etkin + saplama 127.0.0.53 → aracı kurulur, uyarı yok" 1 \
    "$AKTIF; KOK='$G/iyi'; git_onkosul_resolved"
git_karari "resolved etkin, kadran-git zaten kurulu → aracı kurulur (yükseltme)" 1 \
    "$AKTIF; KOK='$G/iyi-kurulu'; git_onkosul_resolved"
git_karari "resolved etkin değil, kadran-git hiç kurulmamış → uyarır ve atlar" 0 \
    "$PASIF; KOK='$G/iyi'; git_onkosul_resolved"
git_karari "saplama başka adresi gösteriyor (GCP), birim yok → uyarır ve atlar" 0 \
    "$AKTIF; KOK='$G/gcp'; git_onkosul_resolved"
git_karari "saplama dosyası yok, birim yok → uyarır ve atlar (set -e altında durmaz)" 0 \
    "$AKTIF; KOK='$G/bos'; git_onkosul_resolved"
hata_bekle "resolved yok ama kadran-git.service var → durur" "$KURULU_ILETI" \
    "$PASIF; KOK='$G/kurulu'; git_onkosul_resolved"
hata_bekle "resolved yok ama yalnız bir kadran-git soketi var → durur" "$KURULU_ILETI" \
    "$PASIF; KOK='$G/soketli'; git_onkosul_resolved"
hata_bekle "durma iletisi resolved'ı ve ilk iletiyi söylüyor" "$RESOLVED_ILETI" \
    "$PASIF; KOK='$G/kurulu'; git_onkosul_resolved"
hata_bekle "durma iletisi resolved'sız yolu söylüyor (birimleri kaldır)" 'remove the kadran-git units to run without private repositories' \
    "$PASIF; KOK='$G/kurulu'; git_onkosul_resolved"
dene "uyarı rehberi gösteriyor" \
    "$(ys "git_resolved_uyarisi | grep -qF 'See docs/guides/private-git.md'")"

KIMLIK_ILETI='remove --git-credentials from the kadran-exec drop-in; private repositories now use'
AG_ILETI='IPAddressAllow= lines from'
AG_SON='kadran-exec no longer uses the network'
DI=etc/systemd/system/kadran-exec.service.d
mkdir -p "$G/dropinsiz" "$G/temiz/$DI" "$G/kirli/$DI" "$G/eski146/$DI" "$G/ag/$DI" "$G/izin/$DI" "$G/yorum/$DI"
# Canlı iki sunucudaki biçim: yalnız ExecStart ve --allow-repo. GEÇMELİ.
printf '[Service]\nExecStart=\nExecStart=/usr/local/lib/kadran/kadran-exec --allow-repo a/b\n' > "$G/temiz/$DI/10.conf"
printf '[Service]\nExecStart=\nExecStart=/usr/local/lib/kadran/kadran-exec --git-credentials /etc/kadran/git.token\n' > "$G/kirli/$DI/20-git.conf"
# K-146 rehberinin drop-in'i birebir (aa49db9:docs/guides/private-git.md).
cat > "$G/eski146/$DI/override.conf" <<'ESKI146'
[Service]
ExecStart=
ExecStart=/usr/local/lib/kadran/kadran-exec --socket /run/kadran-exec/exec.sock --journal /var/lib/kadran-exec/exec-audit.log --allow-user kadran --owner-group kadran --allow-repo YOUR_ORG/YOUR_REPO --git-credentials /etc/kadran/private-git/credentials.json
RestrictAddressFamilies=
RestrictAddressFamilies=AF_UNIX AF_INET AF_INET6
# Bulut meta veri servisi root executor'dan ERİŞİLEMEZ olmalı: GCP/AWS/Azure'da
# 169.254.169.254 örnek kimlik belirteci demektir. Link-local (IPv4 ve IPv6) ve
# AWS'nin IPv6 meta veri adresi kapatılır.
IPAddressDeny=169.254.0.0/16 fe80::/10 fd00:ec2::254/128
ESKI146
# Güncel rehberi izleyip yalnız --git-credentials'ı silen kullanıcının
# drop-in'i: ağ satırları kaldı (son inceleme I2'nin asıl senaryosu).
printf '[Service]\nExecStart=\nExecStart=/usr/local/lib/kadran/kadran-exec --allow-repo a/b\nRestrictAddressFamilies=\nRestrictAddressFamilies=AF_UNIX AF_INET AF_INET6\nIPAddressDeny=169.254.0.0/16 fe80::/10\n' > "$G/ag/$DI/30-net.conf"
printf '[Service]\nIPAddressAllow=10.0.0.2\n' > "$G/izin/$DI/40.conf"
# Yorumdaki adlar ve boş sıfırlama satırı durdurmamalı (satır başı çapası).
printf '[Service]\n# eskiden: IPAddressDeny= ve RestrictAddressFamilies=AF_INET vardı\nRestrictAddressFamilies=\nExecStart=\nExecStart=/usr/local/lib/kadran/kadran-exec --allow-repo a/b\n' > "$G/yorum/$DI/50.conf"
gecer "drop-in dizini yokken (glob eşleşmez) DURMAZ" \
    "KOK='$G/dropinsiz'; git_onkosul_eski_kimlik"
gecer "canlı biçim: yalnız ExecStart + --allow-repo → geçer" \
    "KOK='$G/temiz'; git_onkosul_eski_kimlik"
hata_bekle "drop-in'te --git-credentials → durur (eski ileti)" "$KIMLIK_ILETI" \
    "KOK='$G/kirli'; git_onkosul_eski_kimlik"
hata_bekle "ileti kadran github connect yolunu söylüyor" 'kadran github connect' \
    "KOK='$G/kirli'; git_onkosul_eski_kimlik"
hata_bekle "K-146 rehberinin drop-in'i → ağ satırları için durur" "$AG_SON" \
    "KOK='$G/eski146'; git_onkosul_eski_kimlik"
hata_bekle "K-146 rehberinin drop-in'i → --git-credentials'ı da söyler" "$KIMLIK_ILETI" \
    "KOK='$G/eski146'; git_onkosul_eski_kimlik"
hata_bekle "yalnız ağ satırları kalmış drop-in → durur" "$AG_ILETI" \
    "KOK='$G/ag'; git_onkosul_eski_kimlik"
hata_bekle "ağ iletisi dosyayı adlandırıyor" "$DI/30-net.conf" \
    "KOK='$G/ag'; git_onkosul_eski_kimlik"
hata_bekle "yalnız IPAddressAllow= satırı → durur" "$AG_ILETI" \
    "KOK='$G/izin'; git_onkosul_eski_kimlik"
gecer "yorumdaki ad ve boş RestrictAddressFamilies= → geçer" \
    "KOK='$G/yorum'; git_onkosul_eski_kimlik"

# kadran-git kullanıcısı: sahte id/useradd; çağrılar günlüğe yazılıyor.
KUL_SAHTE='getent() { return 1; }; groupadd() { :; }; NOLOGIN=/usr/sbin/nologin
useradd() { echo "useradd $*" >> "$GUNLUK"; }
id() { case "$*" in "-u kadran-git") return 1;; "-nG kadran-git") echo "kadran-git $GRUP_GIT";; "-nG kadran") echo "kadran $GRUP_KADRAN";; esac; }'
GUNLUK_G="$G/kul-gunluk"
for g in docker kadran kadran-client kadran-proxy; do
    hata_bekle "kadran-git $g grubundaysa durur" "kadran-git must not belong to $g" \
        "GUNLUK='$GUNLUK_G'; GRUP_GIT='$g'; GRUP_KADRAN=''; $KUL_SAHTE; git_kullanicisi"
done
hata_bekle "kadran, kadran-git grubundaysa durur" "kadran must not belong to the kadran-git group" \
    "GUNLUK='$GUNLUK_G'; GRUP_GIT=''; GRUP_KADRAN='kadran-git'; $KUL_SAHTE; git_kullanicisi"
: > "$GUNLUK_G"
gecer "temiz üyelikte geçer ve kullanıcı sistem hesabı olarak açılır" \
    "GUNLUK='$GUNLUK_G'; GRUP_GIT=''; GRUP_KADRAN=''; $KUL_SAHTE; git_kullanicisi; grep -q -- '--system --gid kadran-git --home-dir /nonexistent --no-create-home --shell /usr/sbin/nologin --comment Kadran GitHub access broker kadran-git' '$GUNLUK_G'"

echo
echo "== offsite_cozucu_kotu: uzak yedeğin DNS'i süzgecine takılmamalı (K-168) =="
# Dosyalar GCP ve Hetzner'de okunan hâlleriyle (12 Kasım).
R="$(mktemp -d)"
trap 'rm -f "$FN"; rm -rf "$D" "$H" "$G" "$R"' EXIT
printf 'nameserver 127.0.0.53\noptions edns0 trust-ad\nsearch .\n' > "$R/stub"
printf 'nameserver 169.254.169.254\nsearch c.ornek.internal google.internal\n' > "$R/stub-eski"
printf 'nameserver 169.254.169.254\nsearch c.ornek.internal google.internal\n' > "$R/gcp"
printf 'nameserver 127.0.0.1\n' > "$R/yerel"
printf 'nameserver fe80::1%%eth0\n' > "$R/fe80"
printf 'nameserver 185.12.64.1\nnameserver 2a01:4ff:ff00::add:1\n' > "$R/olagan"
printf 'nameserver 127.0.0.53\n' > "$R/stub-gosteren"
: > "$R/zaman"
ln -s /dev/null "$R/maske"
dene "uzak yedek kurulu değil → sorun yok" \
    "$(ys "if offsite_cozucu_kotu '$R/yok' '$R/gcp' inactive '$R/yok-zaman'; then exit 1; fi")"
dene "zamanlayıcı maskeli → sorun yok" \
    "$(ys "if offsite_cozucu_kotu '$R/yok' '$R/gcp' inactive '$R/maske'; then exit 1; fi")"
dene "GCP, resolved çalışıyor, stub 127.0.0.53 → sorun yok (bağ stub'ı gösterir)" \
    "$(ys "if offsite_cozucu_kotu '$R/stub' '$R/gcp' active '$R/zaman'; then exit 1; fi")"
dene "GCP, resolved yok → durur ve 169.254.169.254'ü basar" \
    "$(ys "ns=\"\$(offsite_cozucu_kotu '$R/yok' '$R/gcp' inactive '$R/zaman')\"; [ \"\$ns\" = 169.254.169.254 ]")"
dene "resolved kapalı, eski stub dosyası duruyor → yine durur" \
    "$(ys "ns=\"\$(offsite_cozucu_kotu '$R/stub' '$R/gcp' inactive '$R/zaman')\"; [ \"\$ns\" = 169.254.169.254 ]")"
dene "resolved çalışıyor ama stub dosyası yok → durur" \
    "$(ys "ns=\"\$(offsite_cozucu_kotu '$R/yok' '$R/gcp' active '$R/zaman')\"; [ \"\$ns\" = 169.254.169.254 ]")"
dene "stub 127.0.0.53'ü göstermiyor → durur" \
    "$(ys "ns=\"\$(offsite_cozucu_kotu '$R/stub-eski' '$R/gcp' active '$R/zaman')\"; [ \"\$ns\" = 169.254.169.254 ]")"
dene "yerel çözücü 127.0.0.1 → durur" \
    "$(ys "ns=\"\$(offsite_cozucu_kotu '$R/yok' '$R/yerel' inactive '$R/zaman')\"; [ \"\$ns\" = 127.0.0.1 ]")"
dene "IPv6 link-local çözücü → durur" \
    "$(ys "ns=\"\$(offsite_cozucu_kotu '$R/yok' '$R/fe80' inactive '$R/zaman')\"; [ \"\$ns\" = 'fe80::1%eth0' ]")"
dene "resolved'sız, olağan çözücü → sorun yok (bugün çalışan kurulum durmaz)" \
    "$(ys "if offsite_cozucu_kotu '$R/yok' '$R/olagan' inactive '$R/zaman'; then exit 1; fi")"
dene "resolv.conf 127.0.0.53 → sorun yok (süzgeç izin veriyor)" \
    "$(ys "if offsite_cozucu_kotu '$R/yok' '$R/stub-gosteren' inactive '$R/zaman'; then exit 1; fi")"
dene "eski adlı zamanlayıcı da sayılır (göç)" \
    "$(ys "ns=\"\$(offsite_cozucu_kotu '$R/yok' '$R/gcp' inactive '$R/yok-zaman' '$R/zaman')\"; [ \"\$ns\" = 169.254.169.254 ]")"
dene "dosyalar yokken kurulum DURMAZ (set -e)" \
    "$(ys "if offsite_cozucu_kotu '$R/yok' '$R/yok-resolv' inactive '$R/zaman'; then exit 1; fi; true")"
# Kontrol grubu: maske denetimi bağı izleyen bir sınamadan geçmeli; -e
# /dev/null'a giden bağı "var" sayar.
dene "KONTROL: maskeli zamanlayıcı -e ile var görünür" "[ -e '$R/maske' ] && [ -L '$R/maske' ]"

# Çağrı yeri: ön koşullarda, kurulum hiçbir şeyi değiştirmeden önce. Uzak
# yedek seçimli birimlerle en sonda güncelleniyor; orada durmak yarım
# kurulum bırakırdı.
sed -n '/^step "Prerequisites"$/,/^step "Docker address pool"$/p' internal/bootstrap/install.sh > "$R/on"
[ -s "$R/on" ] || { echo "ön koşul bölümü install.sh'tan çıkarılamadı — ölçüm geçersiz" >&2; exit 1; }
on_kosulda() {
    if grep -qF -- "$2" "$R/on"; then echo "  ✓ $1"; else echo "  ✗ $1"; fail=1; fi
}
on_kosulda "ön koşullarda çağrılıyor" \
    'if offsite_ns="$(offsite_cozucu_kotu /run/systemd/resolve/stub-resolv.conf /etc/resolv.conf'
on_kosulda "resolved'ın durumu systemctl'den" \
    '"$(systemctl is-active systemd-resolved 2>/dev/null || true)"'
on_kosulda "kurulu zamanlayıcıya bakıyor" '/etc/systemd/system/kadran-offsite.timer'
on_kosulda "eski adlı zamanlayıcıya da bakıyor (göç)" '/etc/systemd/system/panely-offsite.timer'
on_kosulda "sonuç kurulumu durduruyor" '    die "kadran-offsite needs systemd-resolved'

echo
echo "== kadran-git atlandığında ana akış (K-167, son inceleme I1) =="
# Bu satırlar ana akışta; fonksiyon olarak koşturulamıyor. Ardışık satır
# çifti aranıyor: koşul ters dönerse ya da gövde değişirse kırmızı.
ardisik() { # ardisik <ad> <satır1> <satır2>
    # ENVIRON, -v değil: -v ters eğik çizgi kaçışlarını çözer (printf
    # satırındaki kaçış gerçek satır sonuna döner ve eşleşme hiç olmaz).
    if A="$2" B="$3" awk 'onceki == ENVIRON["A"] && $0 == ENVIRON["B"] { f = 1 } { onceki = $0 } END { exit !f }' internal/bootstrap/install.sh; then
        echo "  ✓ $1"
    else
        echo "  ✗ $1"; fail=1
    fi
}
ardisik "etkinlik denetimi kadran-git soketlerini yalnız aracı kurulduysa sınıyor" \
    'if [ "$GIT_ARACI" = 1 ]; then' '    izlenen_birimler+=(kadran-git-admin.socket kadran-git-fetch.socket)'
ardisik "etkinlik denetimi listeden koşuyor" \
    'fi' 'for unit in "${izlenen_birimler[@]}"; do'
ardisik "imaj denetimi atlanan aracı için hata olmayan tek satır basıyor" \
    'if [ "$GIT_ARACI" != 1 ]; then' '    printf '"'"'  - %s\n'"'"' "kadran-git not set up: systemd-resolved is not running (private GitHub repositories are disabled)"'
ardisik "özet uyarıyı yineliyor" \
    'if [ "$GIT_ARACI" != 1 ]; then' '    git_resolved_uyarisi'
if grep -qE '^for unit in .*kadran-git' internal/bootstrap/install.sh; then
    echo "  ✗ etkinlik döngüsü kadran-git soketlerini koşulsuz sayıyor"; fail=1
else
    echo "  ✓ etkinlik döngüsü kadran-git soketlerini koşulsuz saymıyor"
fi

echo
(( fail == 0 )) || { echo "BAŞARISIZ"; exit 1; }
echo "Kurulum betiği yardımcıları doğru."
