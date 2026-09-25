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

echo
if (( fail )); then
    echo "BAŞARISIZ: biçimlendirici beklenen çıktıyı üretmiyor."
    exit 1
fi
echo "Biçimlendirici doğru."
