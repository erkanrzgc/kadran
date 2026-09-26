#!/usr/bin/env bash
# Alarm göndericisinin biçimlendiricisini (K-108) ve çekirdek servis
# denetimini (K-110) sınar.
#
# Girdiler panelyd'nin journal'ına GERÇEKTEN düşmüş satırların
# biçiminde: `slog` metin çıktısı, değer boşluk içeriyorsa tırnaklı.
# Kapanış satırları canlı sunucudan birebir alındı (17 Eyl).
#
# Betik ağa dokunmaz ve yapılandırma okumaz: gönderici kaynak olarak
# yüklendiğinde yalnızca fonksiyonlarını tanımlıyor.
set -uo pipefail
cd "$(dirname "$0")/.."

# shellcheck source=/dev/null
source deploy/notify/panely-notify.sh

fail=0
bekle() {
    local ad="$1" satir="$2" beklenen="$3" cikti
    cikti="$(bicimle "$satir")"
    if [[ "$cikti" == "$beklenen" ]]; then
        echo "  ✓ $ad"
    else
        echo "  ✗ $ad"
        echo "      beklenen: $(printf '%q' "$beklenen")"
        echo "      çıkan   : $(printf '%q' "$cikti")"
        fail=1
    fi
}

echo "== Alarm biçimlendirici =="

bekle "kritik açılış, tırnaklı ayrıntı" \
    'time=2026-09-17T20:15:02.1Z level=ERROR msg=ALARM alarm=backup_failed:panely.db durum=acildi ciddiyet=kritik hedef=panely.db ayrinti="zamanlı yedek alınamıyor — geri dönüş yolu YOK"' \
    $'🔴 KRİTİK — backup_failed:panely.db\n   zamanlı yedek alınamıyor — geri dönüş yolu YOK'

bekle "uyarı açılış, hedef kimlikten farklı" \
    'time=x level=WARN msg=ALARM alarm=disk_low:host durum=acildi ciddiyet=uyari hedef=/ ayrinti="%9 boş, 3.4 GiB / 38 GiB"' \
    $'🟡 UYARI — disk_low:host (/)\n   %9 boş, 3.4 GiB / 38 GiB'

bekle "kapanış (canlıdan birebir)" \
    'time=2026-09-17T20:23:03.729Z level=WARN msg=ALARM alarm=disk_low:host durum=kapandi' \
    '✅ DÜZELDİ — disk_low:host'

bekle "tırmanma" \
    'time=x level=ERROR msg=ALARM alarm=disk_low:host durum=tirmandi ciddiyet=kritik hedef=host ayrinti="%3 boş"' \
    $'🔴 KÖTÜLEŞTİ — disk_low:host\n   %3 boş'

bekle "ayrıntıda kaçışlı tırnak" \
    'time=x level=ERROR msg=ALARM alarm=heal_exhausted:web durum=acildi ciddiyet=kritik hedef=web ayrinti="\"web\" 3 kez kurtarılamadı"' \
    $'🔴 KRİTİK — heal_exhausted:web\n   "web" 3 kez kurtarılamadı'

bekle "tırnaksız tek kelimelik ayrıntı" \
    'time=x level=WARN msg=ALARM alarm=disk_low:host durum=acildi ciddiyet=uyari hedef=host ayrinti=dolmak' \
    $'🟡 UYARI — disk_low:host\n   dolmak'

# Kontrol grubu: tanınmayan durum sessizce "düzeldi" diye okunmamalı.
bekle "bilinmeyen durum düzeldi SAYILMAZ" \
    'time=x level=WARN msg=ALARM alarm=disk_low:host durum=yeni_bir_sey' \
    '❔ yeni_bir_sey — disk_low:host'

echo "== Yapılandırma ayrıştırıcı =="

# Canlıda iki kez oldu: yapıştırılan anahtarın SONUNDA boşluk kaldı ve
# biçim denetimi reddetti. Kullanıcı sebebi göremiyordu.
conf_dene() {
    local ad="$1" icerik="$2" beklenen="$3" dosya sonuc
    dosya="$(mktemp)"
    printf '%b' "$icerik" > "$dosya"
    sonuc="$( (PANELY_NOTIFY_CONF="$dosya"; unset CREDENTIALS_DIRECTORY
               yapilandirma_oku && printf '%s|%s' "$TOKEN" "$CHAT_ID") 2>/dev/null)"
    rm -f "$dosya"
    if [[ "$sonuc" == "$beklenen" ]]; then
        echo "  ✓ $ad"
    else
        echo "  ✗ $ad (beklenen $(printf '%q' "$beklenen"), çıkan $(printf '%q' "$sonuc"))"
        fail=1
    fi
}

conf_dene "düz" 'TELEGRAM_TOKEN=123:abc\nTELEGRAM_CHAT_ID=42\n' '123:abc|42'
conf_dene "değerin sonunda boşluk" 'TELEGRAM_TOKEN=123:abc \nTELEGRAM_CHAT_ID=42 \n' '123:abc|42'
conf_dene "eşittirin iki yanında boşluk" 'TELEGRAM_TOKEN = 123:abc\nTELEGRAM_CHAT_ID = -42\n' '123:abc|-42'
conf_dene "Windows satır sonu" 'TELEGRAM_TOKEN=123:abc\r\nTELEGRAM_CHAT_ID=42\r\n' '123:abc|42'
# Kontrol grubu: kırpma, bozuk anahtarı kabul edilir hâle getirmemeli.
conf_dene "ortada boşluklu anahtar REDDEDİLİR" 'TELEGRAM_TOKEN=123:ab c\n' ''

# Nabız (K-109): isteğe bağlı, ama yarım kurulum sessiz kalmamalı.
N32=0123456789abcdef0123456789abcdef
conf_dene "nabız yok → geçerli" 'TELEGRAM_TOKEN=123:abc\nTELEGRAM_CHAT_ID=42\n' '123:abc|42'
conf_dene "nabız tam → geçerli" \
    "TELEGRAM_TOKEN=123:abc\nTELEGRAM_CHAT_ID=42\nHEARTBEAT_URL=https://n.example.workers.dev/ping\nHEARTBEAT_TOKEN=$N32\n" '123:abc|42'
conf_dene "yalnız URL → REDDEDİLİR" \
    'TELEGRAM_TOKEN=123:abc\nHEARTBEAT_URL=https://n.example.workers.dev/ping\n' ''
conf_dene "yalnız anahtar → REDDEDİLİR" \
    "TELEGRAM_TOKEN=123:abc\nHEARTBEAT_TOKEN=$N32\n" ''
conf_dene "http (şifresiz) → REDDEDİLİR" \
    "TELEGRAM_TOKEN=123:abc\nHEARTBEAT_URL=http://n.example.workers.dev/ping\nHEARTBEAT_TOKEN=$N32\n" ''
conf_dene "kısa anahtar → REDDEDİLİR" \
    'TELEGRAM_TOKEN=123:abc\nHEARTBEAT_URL=https://n.example.workers.dev/ping\nHEARTBEAT_TOKEN=abc\n' ''

echo "== Günlük seviyesi önekleri =="

# Birim LogLevelMax=notice taşıyor; düz satırlar susturuluyor (ölçüldü).
# journal'a yazarken önek ŞART, terminalde OLMAMALI.
onek_dene() {
    local ad="$1" journal="$2" beklenen="$3" cikti
    cikti="$(JOURNAL_STREAM="$journal" bash -c \
        'source deploy/notify/panely-notify.sh; log x; (die y) 2>&1' 2>&1)"
    if [[ "$cikti" == "$beklenen" ]]; then
        echo "  ✓ $ad"
    else
        echo "  ✗ $ad (çıkan $(printf '%q' "$cikti"))"
        fail=1
    fi
}
onek_dene "journal altında notice/err önekli" "8:123" \
    $'<5>panely-notify: x\n<3>panely-notify: HATA: y'
onek_dene "terminalde öneksiz" "" \
    $'panely-notify: x\npanely-notify: HATA: y'

echo "== Servis kararı (K-110) =="

# karar_dene <ad> "<eski_durum> <eski_asagi> <cokus> <calisiyor>" "<eylem> <yeni_durum> <yeni_asagi>"
karar_dene() {
    local ad="$1" girdi="$2" beklenen="$3" cikti
    # shellcheck disable=SC2086
    cikti="$(servis_karar $girdi)"
    if [[ "$cikti" == "$beklenen" ]]; then
        echo "  ✓ $ad"
    else
        echo "  ✗ $ad (girdi $girdi → beklenen '$beklenen', çıkan '$cikti')"
        fail=1
    fi
}
karar_dene "sağlam kalır"                        "saglam 0 0 1"      "yok saglam 0"
karar_dene "tek çöküş, systemd geri getirdi"     "saglam 0 1 1"      "coktu coktu 0"
karar_dene "çöktü ve ayağa kalkmadı"             "saglam 0 1 0"      "coktu coktu 1"
karar_dene "çöküşten sonra bir tur temiz"        "coktu 0 0 1"       "duzeldi saglam 0"
karar_dene "ikinci turda da çöküş → döngü"       "coktu 0 3 1"       "dongu dongu 0"
# Kontrol grubu: döngü sürdükçe her dakika mesaj GİTMEMELİ (ölçüldü:
# başlatma sınırı hiç dolmuyor, döngü sonsuz).
karar_dene "döngü sürüyor → sessiz"              "dongu 0 25 1"      "yok dongu 0"
karar_dene "döngüden çıktı"                      "dongu 0 0 1"       "duzeldi saglam 0"
# Kontrol grubu: yükseltmedeki kısa yeniden başlatma alarm vermemeli.
karar_dene "bir tur kapalı (çalışmıyor)"         "saglam 0 0 0"      "yok saglam 1"
karar_dene "iki tur kapalı (exit 0, stop)"       "saglam 1 0 0"      "calismiyor calismiyor 2"
karar_dene "çöktü, sonra kalkmadı"               "coktu 1 0 0"       "calismiyor calismiyor 2"
karar_dene "çalışmıyor sürüyor → sessiz"         "calismiyor 2 0 0"  "yok calismiyor 2"
karar_dene "geri geldi"                          "calismiyor 2 0 1"  "geri_geldi saglam 0"
karar_dene "bozuk durum dosyası sağlam sayılır"  "bozuk x 0 1"       "yok saglam 0"

echo "== journal olay ayıklayıcı (K-110) =="

# Satırlar canlı sunucudan birebir (26 Eyl). `sahte`: sıradan bir
# süreç UNIT= ve MESSAGE_ID= alanlarını KENDİSİ yazabiliyor (ölçüldü);
# yalnızca _PID=1 olanlar systemd'nin kendi olayı.
GERCEK='{"UNIT_RESULT":"signal","__SEQNUM":"1539133","UNIT":"panely-olcum-coken.service","_PID":"1","MESSAGE_ID":"d9b373ed55a64feb8242e02dbe79a49c","__MONOTONIC_TIMESTAMP":"4239811578782","_BOOT_ID":"94c0a82e2a494db8b29d4e2dcd371633","__CURSOR":"s=cf474177a33247aab7ab0645a06d9e65;i=177c3d;b=94c0a82e2a494db8b29d4e2dcd371633;m=3db28768b9e;t=65c604af6d57c;x=bb85dbbdeadcf4cc","__REALTIME_TIMESTAMP":"1790418504570236","__SEQNUM_ID":"cf474177a33247aab7ab0645a06d9e65"}'
SAHTE='{"__SEQNUM":"1540102","__SEQNUM_ID":"cf474177a33247aab7ab0645a06d9e65","__REALTIME_TIMESTAMP":"1790419089987293","UNIT_RESULT":"signal","_PID":"542425","MESSAGE_ID":"d9b373ed55a64feb8242e02dbe79a49c","__CURSOR":"s=cf474177a33247aab7ab0645a06d9e65;i=178006;b=94c0a82e2a494db8b29d4e2dcd371633;m=3db4b5b4cff;t=65c606ddb96dd;x=c28734a1e20fe064","UNIT":"panely-caddy.service","__MONOTONIC_TIMESTAMP":"4240396995839","_BOOT_ID":"94c0a82e2a494db8b29d4e2dcd371633"}'
BASLADI='{"__MONOTONIC_TIMESTAMP":"4239813639252","_PID":"1","MESSAGE_ID":"39f53479d3a045ac8e11786248231fbf","__REALTIME_TIMESTAMP":"1790418506630706","UNIT":"panely-olcum-coken.service","_BOOT_ID":"94c0a82e2a494db8b29d4e2dcd371633","__CURSOR":"s=cf474177a33247aab7ab0645a06d9e65;i=177c43;b=94c0a82e2a494db8b29d4e2dcd371633;m=3db2895fc54;t=65c604b164632;x=d005e3fb79c8e472","__SEQNUM":"1539139","__SEQNUM_ID":"cf474177a33247aab7ab0645a06d9e65"}'
KENDI='{"__MONOTONIC_TIMESTAMP":"4239805720889","__SEQNUM_ID":"cf474177a33247aab7ab0645a06d9e65","_BOOT_ID":"94c0a82e2a494db8b29d4e2dcd371633","_PID":"539751","__CURSOR":"s=cf474177a33247aab7ab0645a06d9e65;i=177c30;b=94c0a82e2a494db8b29d4e2dcd371633;m=3db281d2939;t=65c604a9d7318;x=62a03c68915ec7c7","__SEQNUM":"1539120","__REALTIME_TIMESTAMP":"1790418498712344"}'

ayikla_dene() {
    local ad="$1" girdi="$2" beklenen="$3" cikti
    cikti="$(printf '%s\n' "$girdi" | servis_olay_ayikla)"
    if [[ "$cikti" == "$beklenen" ]]; then
        echo "  ✓ $ad"
    else
        echo "  ✗ $ad (beklenen $(printf '%q' "$beklenen"), çıkan $(printf '%q' "$cikti"))"
        fail=1
    fi
}
ayikla_dene "systemd'nin çöküş olayı"            "$GERCEK"  "panely-olcum-coken.service signal"
ayikla_dene "sahte olay (_PID≠1) REDDEDİLİR"     "$SAHTE"   ""
ayikla_dene "başka olay (Started) sayılmaz"      "$BASLADI" ""
ayikla_dene "servisin kendi satırı sayılmaz"     "$KENDI"   ""
ayikla_dene "karışık akış" "$KENDI"$'\n'"$GERCEK"$'\n'"$SAHTE"$'\n'"$BASLADI"$'\n'"$GERCEK" \
    $'panely-olcum-coken.service signal\npanely-olcum-coken.service signal'

echo "== Servis mesaj satırı (K-110) =="

satir_dene() {
    local ad="$1" beklenen="$2" cikti; shift 2
    cikti="$(servis_satiri "$@")"
    if [[ "$cikti" == "$beklenen" ]]; then
        echo "  ✓ $ad"
    else
        echo "  ✗ $ad (çıkan $(printf '%q' "$cikti"))"
        fail=1
    fi
}
satir_dene "çöktü, geri geldi" '🟠 ÇÖKTÜ — panelyd.service: 1 kez (signal), systemd yeniden başlattı' \
    coktu panelyd.service 1 signal 1
satir_dene "çöktü, kalkmadı" '🟠 ÇÖKTÜ — panelyd.service: 2 kez (exit-code), şu an ÇALIŞMIYOR' \
    coktu panelyd.service 2 exit-code 0
satir_dene "döngü" '🔴 ÇÖKME DÖNGÜSÜ — panely-exec.service: son kontrolden beri 26 kez daha (exit-code)' \
    dongu panely-exec.service 26 exit-code 1
satir_dene "çalışmıyor" '🔴 ÇALIŞMIYOR — panely-caddy.service' calismiyor panely-caddy.service 0 "" 0
satir_dene "toparlandı" '✅ TOPARLANDI — panelyd.service: son kontrolden beri çökmedi' duzeldi panelyd.service 0 "" 1
satir_dene "geri geldi" '✅ YENİDEN ÇALIŞIYOR — panely-caddy.service' geri_geldi panely-caddy.service 0 "" 1

echo "== Servis turu, uçtan uca sahte ortamda (K-110) =="

# journalctl, gonder ve cgroup sahte; geri kalanı gerçek kod.
# tur_kos <json-dosyası> <gönderim-sonucu 0|1> <kapalı birimler…>
tur_kos() {
    local json="$1" gonder_sonuc="$2"; shift 2
    (
        TMP="$(mktemp -d)"; SUNUCU=test
        CGROUP_KOK="$SAHTE_KOK/cg"
        rm -rf "$CGROUP_KOK"
        for b in "${CEKIRDEK_BIRIMLER[@]}"; do
            mkdir -p "$CGROUP_KOK/$b"; echo 123 > "$CGROUP_KOK/$b/cgroup.procs"
        done
        for b in "$@"; do : > "$CGROUP_KOK/$b/cgroup.procs"; done
        journalctl() {
            local a ilk=0
            for a in "$@"; do
                [[ "$a" == --cursor-file=* ]] && echo "imlec-$RANDOM" > "${a#--cursor-file=}"
                [[ "$a" == -n ]] && ilk=1   # ilk koşu: geçmiş basılmaz
            done
            (( ilk )) || cat "$json"
        }
        gonder() { cp "$1" "$SAHTE_KOK/giden"; return "$gonder_sonuc"; }
        log() { :; }
        servisler
    ) 2>/dev/null
}
SAHTE_KOK="$(mktemp -d)"
export STATE_DIRECTORY="$SAHTE_KOK/durum"; mkdir -p "$STATE_DIRECTORY"
CEKIRDEK_JSON="$SAHTE_KOK/olay.json"
sed 's/panely-olcum-coken\.service/panelyd.service/' <<< "$GERCEK" > "$CEKIRDEK_JSON"
: > "$SAHTE_KOK/bos.json"

tur_dene() {
    local ad="$1" kosul="$2"
    if eval "$kosul"; then echo "  ✓ $ad"; else echo "  ✗ $ad ($kosul)"; fail=1; fi
}
rm -f "$SAHTE_KOK/giden"
tur_kos "$CEKIRDEK_JSON" 0
tur_dene "ilk koşu geçmişi GÖNDERMEZ" '[[ ! -e $SAHTE_KOK/giden && -s $STATE_DIRECTORY/servis-imlec ]]'
tur_kos "$CEKIRDEK_JSON" 0
tur_dene "çöküş gönderildi" 'grep -q "ÇÖKTÜ — panelyd.service: 1 kez (signal)" $SAHTE_KOK/giden'
tur_dene "durum kaydedildi" 'grep -q "^panelyd.service coktu 0$" $STATE_DIRECTORY/servisler'
cp "$STATE_DIRECTORY/servisler" "$SAHTE_KOK/onceki"; rm -f "$SAHTE_KOK/giden"
tur_kos "$SAHTE_KOK/bos.json" 1 panely-caddy.service
tur_kos "$SAHTE_KOK/bos.json" 1 panely-caddy.service
tur_dene "gönderim başarısız → durum İLERLEMEDİ" 'cmp -s $SAHTE_KOK/onceki $STATE_DIRECTORY/servisler'
tur_kos "$SAHTE_KOK/bos.json" 0 panely-caddy.service
tur_dene "tekrar denemede panelyd toparlandı" 'grep -q "TOPARLANDI — panelyd.service" $SAHTE_KOK/giden'
tur_dene "caddy bir tur kapalı → henüz sessiz" '[[ -s $SAHTE_KOK/giden ]] && ! grep -q "caddy" $SAHTE_KOK/giden'
tur_kos "$SAHTE_KOK/bos.json" 0 panely-caddy.service
tur_dene "iki tur kapalı → ÇALIŞMIYOR" 'grep -q "ÇALIŞMIYOR — panely-caddy.service" $SAHTE_KOK/giden'
rm -f "$SAHTE_KOK/giden"
tur_kos "$SAHTE_KOK/bos.json" 0 panely-caddy.service
tur_dene "kapalı sürüyor → mesaj YOK" '[[ ! -e $SAHTE_KOK/giden ]]'
rm -rf "$SAHTE_KOK"

echo
if (( fail )); then
    echo "BAŞARISIZ: biçimlendirici beklenen çıktıyı üretmiyor."
    exit 1
fi
echo "Biçimlendirici doğru."
