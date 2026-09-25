#!/usr/bin/env bash
# Alarm göndericisinin biçimlendiricisini sınar (K-108).
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

echo
if (( fail )); then
    echo "BAŞARISIZ: biçimlendirici beklenen çıktıyı üretmiyor."
    exit 1
fi
echo "Biçimlendirici doğru."
