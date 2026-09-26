#!/usr/bin/env bash
#
# Panely alarm GÖNDERİCİSİ — alarmları Telegram'a iletir.
#
# ── Neden AYRI bir süreç (K-108) ─────────────────────────────────────
#
# panelyd `IPAddressDeny=any` taşıyor ve dışarı çıkamıyor (K-092'de
# kontrol gruplu ölçüldü). Bu kasıtlı: ele geçirilen bir kontrol
# düzlemi dışarı veri sızdıramasın. Uzak yedekte kurulan desen burada
# da geçerli: ağ yetkisi, işi tek şey olan ayrı bir birimde.
#
# ── Ne OKUYOR ────────────────────────────────────────────────────────
#
# panelyd'nin journal'ındaki `msg=ALARM` satırlarını. Kenar tetikleme
# (tekilleştirme, kalıcı durum) panelyd'de ve veritabanında yapılıyor;
# journal'a yalnızca GEÇİŞLER düşüyor. Bu betik o alarmlar için yeni bir
# karar vermiyor, yalnızca taşıyor.
#
# Bir de panelyd'nin kendisi, panely-exec ve panely-caddy çöktü mü,
# çalışıyor mu — panelyd kendi ölümünü bildiremez (K-110, `servisler`).
# Bunun kararı burada, kalıcı durumu bu birimin durum dizininde.
#
# ── Telegram anahtarı panelyd'den SAKLI ──────────────────────────────
#
# Birim `DynamicUser=yes` ve `LoadCredential=` ile koşuyor. Anahtar
# dosyası `/etc/panely/notify.conf` 0600 root:root olabilir; systemd onu
# root olarak okuyup YALNIZCA bu birime veriyor. `panely` kullanıcısı
# dosyayı okuyamıyor (ölçüldü). Ele geçirilen bir panelyd sahte alarm
# yazabilir (zaten yazabiliyordu) ama sizin adınıza mesaj ATAMAZ.
#
# Kipler:
#   izle          yeni ALARM satırlarını ve çekirdek servis durumunu
#                 gönder (zamanlayıcı bunu koşar)
#   hata <birim>  bir birimin başarısız olduğunu bildir (OnFailure=)
#   dene          deneme mesajı gönder
#   sohbet-bul    bota yazan sohbetlerin kimliklerini listele (root)
set -uo pipefail

# ── Günlük seviyeleri ────────────────────────────────────────────────
#
# Birim `LogLevelMax=notice` taşıyor (dakikalık koşu journal'ı
# doldurmasın diye). Bu sınır betiğin DÜZ çıktısını da susturuyor —
# systemd onu info sayıyor. Ölçüldü: düz satır kayboldu, `<5>` (notice)
# ve `<3>` (err) önekli satırlar kaldı. Önek olmasa bir gönderim
# başarısız olduğunda SEBEBİ görünmezdi; yalnızca systemd'nin
# "Failed with result" satırı kalırdı.
#
# Önek yalnızca journal'a yazarken ekleniyor ($JOURNAL_STREAM systemd
# tarafından konuyor); elle koşarken terminalde "<5>" görünmesin.
if [[ -n "${JOURNAL_STREAM:-}" ]]; then
    ONEK_NOTICE="<5>"; ONEK_HATA="<3>"
else
    ONEK_NOTICE=""; ONEK_HATA=""
fi
log() { echo "${ONEK_NOTICE}panely-notify: $*"; }
die() { echo "${ONEK_HATA}panely-notify: HATA: $*" >&2; exit 1; }

TOKEN=""
CHAT_ID=""
NABIZ_URL=""
NABIZ_ANAHTAR=""

# kirp <metin> — baştaki ve sondaki boşlukları (CR dahil) atar.
kirp() {
    local s="$1"
    s="${s#"${s%%[![:space:]]*}"}"
    s="${s%"${s##*[![:space:]]}"}"
    printf '%s' "$s"
}

# ── Yapılandırma ─────────────────────────────────────────────────────
#
# systemd altında kimlik bilgisi $CREDENTIALS_DIRECTORY'den gelir. Elle
# (root olarak) koşarken doğrudan dosyadan okunur.
#
# Dosya SOURCE EDİLMİYOR, satır satır ayrıştırılıyor: yapılandırma
# dosyasını kabukta çalıştırmak, onu yazabilen birine komut çalıştırma
# yetkisi vermek olurdu.
yapilandirma_oku() {
    local conf
    if [[ -n "${CREDENTIALS_DIRECTORY:-}" ]]; then
        conf="$CREDENTIALS_DIRECTORY/notify"
    else
        conf="${PANELY_NOTIFY_CONF:-/etc/panely/notify.conf}"
    fi
    [[ -r "$conf" ]] || die "yapılandırma okunamadı: $conf (kurulum: deploy/notify/README.md)"

    local anahtar deger
    while IFS='=' read -r anahtar deger; do
        # Baştaki/sondaki boşluk ve CR atılıyor. Yapıştırılan anahtarın
        # sonunda boşluk kalması canlıda İKİ kez oldu (rclone.conf ve bu
        # dosya); biçim denetimi haklı olarak reddediyordu ama kullanıcı
        # sebebini göremiyordu.
        anahtar="$(kirp "$anahtar")"
        deger="$(kirp "$deger")"
        case "$anahtar" in
            TELEGRAM_TOKEN) TOKEN="$deger" ;;
            TELEGRAM_CHAT_ID) CHAT_ID="$deger" ;;
            HEARTBEAT_URL) NABIZ_URL="$deger" ;;
            HEARTBEAT_TOKEN) NABIZ_ANAHTAR="$deger" ;;
        esac
    done < "$conf"

    # Biçim denetimi. Anahtar ASLA yazdırılmıyor — hata iletisinde bile.
    [[ "$TOKEN" =~ ^[0-9]+:[A-Za-z0-9_-]+$ ]] ||
        die "TELEGRAM_TOKEN eksik ya da biçimi bozuk (BotFather'ın verdiği 123456:ABC… biçimi)"

    # Nabız isteğe bağlı, ama YARIM kurulmuşsa sessiz kalınmıyor: yalnızca
    # biri tanımlıysa dış kontrol hiç nabız almaz ve kullanıcı bunun
    # neden olduğunu göremezdi.
    if [[ -n "$NABIZ_URL" || -n "$NABIZ_ANAHTAR" ]]; then
        [[ "$NABIZ_URL" =~ ^https://[A-Za-z0-9.-]+/ping$ ]] ||
            die "HEARTBEAT_URL https://…/ping biçiminde olmalı"
        [[ "$NABIZ_ANAHTAR" =~ ^[A-Za-z0-9]{32,}$ ]] ||
            die "HEARTBEAT_TOKEN en az 32 harf/rakam olmalı (openssl rand -hex 32)"
    fi
}

# telegram <yöntem> [curl --data-urlencode argümanları…]
#
# Anahtar URL'nin içinde. URL argv'ye YAZILMIYOR: `curl -K -` onu
# standart girdiden okur. argv, aynı makinedeki diğer süreçlere
# /proc üzerinden görünebilir; standart girdi görünmez.
telegram() {
    local yontem="$1"; shift
    printf 'url = "https://api.telegram.org/bot%s/%s"\n' "$TOKEN" "$yontem" |
        curl -sS --max-time 20 -K - -o "$TMP/yanit" -w '%{http_code}' "$@" \
            2>"$TMP/curl.err"
}

# gonder <metin-dosyası>
gonder() {
    [[ "$CHAT_ID" =~ ^-?[0-9]+$ ]] ||
        die "TELEGRAM_CHAT_ID eksik ya da sayı değil (bulmak için: $0 sohbet-bul)"
    local kod
    kod="$(telegram sendMessage --data-urlencode "chat_id=$CHAT_ID" \
            --data-urlencode "text@$1")"
    if [[ "$kod" != 200 ]]; then
        # Telegram'ın hata gövdesi anahtarı İÇERMEZ; curl'ün hatası
        # da URL'yi basmaz (-sS). Yine de ilk satırla sınırlı.
        echo "${ONEK_HATA}panely-notify: gönderilemedi (http=${kod:-yok}): $(head -c 200 "$TMP/yanit" 2>/dev/null) $(head -1 "$TMP/curl.err")" >&2
        return 1
    fi
}

# alan <satır> <anahtar> — slog metin biçiminden bir alanı çıkarır.
# Değer tırnaklıysa tırnak içi, değilse ilk boşluğa kadar.
alan() {
    local satir="$1" k="$2" re_tirnak re_duz
    re_tirnak="(^| )$k=\"(([^\"\\\\]|\\\\.)*)\""
    re_duz="(^| )$k=([^ ]*)"
    if [[ "$satir" =~ $re_tirnak ]]; then
        printf '%s' "${BASH_REMATCH[2]//\\\"/\"}"
    elif [[ "$satir" =~ $re_duz ]]; then
        printf '%s' "${BASH_REMATCH[2]}"
    fi
}

# bicimle <ALARM satırı> — insan okuyacak tek satır üretir.
bicimle() {
    local s="$1" id durum ciddiyet hedef ayrinti simge etiket
    id="$(alan "$s" alarm)"
    durum="$(alan "$s" durum)"
    ciddiyet="$(alan "$s" ciddiyet)"
    hedef="$(alan "$s" hedef)"
    ayrinti="$(alan "$s" ayrinti)"
    case "$durum" in
        acildi)
            if [[ "$ciddiyet" == kritik ]]; then simge="🔴"; etiket="KRİTİK"
            else simge="🟡"; etiket="UYARI"; fi ;;
        tirmandi) simge="🔴"; etiket="KÖTÜLEŞTİ" ;;
        kapandi)  simge="✅"; etiket="DÜZELDİ" ;;
        *)        simge="❔"; etiket="${durum:-bilinmeyen}" ;;
    esac
    printf '%s %s — %s' "$simge" "$etiket" "${id:-?}"
    [[ -n "$hedef" && "$id" != *":$hedef" ]] && printf ' (%s)' "$hedef"
    [[ -n "$ayrinti" ]] && printf '\n   %s' "$ayrinti"
    printf '\n'
}

# imlec_koy <kaynak> <hedef> — imleci ATOMİK olarak değiştirir.
#
# Geçici dosya PrivateTmp'te (/tmp), imleç durum dizininde; ikisi ayrı
# dosya sistemleri olabilir ve aralarındaki `mv` kopyala+sil olur.
# Yarıda kesilen bir kopya bozuk bir imleç bırakırdı. Önce hedefin
# yanına kopyalanıyor, sonra aynı dizin içinde yeniden adlandırılıyor.
imlec_koy() {
    cp "$1" "$2.yeni" && mv "$2.yeni" "$2"
}

# ── izle ─────────────────────────────────────────────────────────────
#
# EN AZ BİR KEZ teslim. journalctl'nin --cursor-file'ı imleci satırları
# BASTIĞI ANDA ilerletiyor (ölçüldü). Doğrudan kalıcı imleçle okunsaydı,
# gönderim başarısız olduğunda o alarmlar sessizce kaybolurdu. Bu
# yüzden imlecin GEÇİCİ bir kopyası ilerletiliyor ve ancak gönderim
# başarılıysa kalıcı imlecin yerine konuyor. Başarısız bir gönderim bir
# sonraki koşuda tekrarlanır.
izle() {
    local durum_dizini="${STATE_DIRECTORY:-/var/lib/panely-notify}"
    local imlec="$durum_dizini/imlec"
    local gecici="$TMP/imlec"

    # İlk koşu: GEÇMİŞİ gönderme. Kurulumdan önceki alarmları
    # topluca yollamak, kurulumun ilk dakikasında anlamsız bir mesaj
    # yağmuru olurdu. İmleç "şimdi"ye konur.
    if [[ ! -s "$imlec" ]]; then
        journalctl -u panelyd.service --no-pager -o cat -n 0 \
            --cursor-file="$imlec" >/dev/null ||
            die "journal okunamadı (systemd-journal grubu?)"
        log "ilk koşu — imleç şimdiye kondu, geçmiş gönderilmedi"
        return 0
    fi

    cp "$imlec" "$gecici"
    journalctl -u panelyd.service --no-pager -o cat \
        --cursor-file="$gecici" > "$TMP/yeni" ||
        die "journal okunamadı (systemd-journal grubu?)"

    grep -F 'msg=ALARM ' "$TMP/yeni" > "$TMP/alarmlar" || true
    local n
    n="$(wc -l < "$TMP/alarmlar")"
    if (( n == 0 )); then
        imlec_koy "$gecici" "$imlec"
        return 0
    fi

    {
        printf '🖥 %s — %d alarm olayı\n\n' "$SUNUCU" "$n"
        local sayac=0
        while IFS= read -r satir; do
            sayac=$((sayac + 1))
            # Telegram mesajı 4096 karakterle sınırlı.
            if (( sayac > 20 )); then
                printf '… ve %d olay daha (journalctl -u panelyd)\n' $((n - 20))
                break
            fi
            bicimle "$satir"
        done < "$TMP/alarmlar"
    } > "$TMP/mesaj"

    if gonder "$TMP/mesaj"; then
        imlec_koy "$gecici" "$imlec"
        log "$n alarm olayı gönderildi"
    else
        # İmleç İLERLEMEDİ: bir sonraki koşu aynı olayları yeniden dener.
        die "$n alarm olayı gönderilemedi — bir sonraki koşuda tekrar denenecek"
    fi
}

# ── nabız ────────────────────────────────────────────────────────────
#
# Dış kontrol (deploy/nabiz, Cloudflare Worker, K-109) bu nabzı
# bekliyor; 15 dakika gelmezse Telegram'a yazıyor. Nabız `izle` ve
# `servisler` BAŞARIYLA bittikten sonra atılıyor: böylece yalnızca
# "sunucu açık" değil, "alarm göndericisi çalışıyor" da kanıtlanıyor.
#
# Seyreltme: gönderici dakikada bir koşuyor, ama Worker'ın KV'si günde
# 1000 yazmaya izin veriyor. 4 dakikadan taze bir nabız varsa
# atlanıyor — günde ~300 yazma.
#
# Nabız atılamazsa koşu BAŞARISIZ SAYILMIYOR: alarmlar gitti, imleç
# ilerledi. Hata journal'a yazılıyor; kalıcıysa dış kontrol zaten
# alarm verecek — o kontrol tam olarak bunun için var.
nabiz_at() {
    [[ -n "$NABIZ_URL" ]] || return 0
    local isaret="${STATE_DIRECTORY:-/var/lib/panely-notify}/son-nabiz"
    if [[ -n "$(find "$isaret" -mmin -4 2>/dev/null)" ]]; then
        return 0
    fi
    local kod
    # URL ve anahtar argv'ye yazılmıyor; ikisi de standart girdiden.
    kod="$(printf 'url = "%s"\nheader = "Authorization: Bearer %s"\n' \
                "$NABIZ_URL" "$NABIZ_ANAHTAR" |
           curl -sS --max-time 15 -X POST -K - -o /dev/null -w '%{http_code}' \
                2>"$TMP/nabiz.err")"
    if [[ "$kod" == 204 ]]; then
        : > "$isaret"
    else
        echo "${ONEK_HATA}panely-notify: nabız atılamadı (http=${kod:-yok}) $(head -1 "$TMP/nabiz.err")" >&2
    fi
}

# ── servisler: çekirdek birimler çöktü mü, çalışıyor mu (K-110) ──────
#
# Neden OnFailure= DEĞİL (ölçüldü, 26 Eyl, systemd 255):
#   - Çöküş döngüsünde OnFailure her çöküşte tetikleniyor: 40 sn'de 18.
#     RestartSec=2s ile başlatma sınırı (10 sn'de 5) HİÇ dolmuyor; döngü
#     sonsuz, 2 sn'de bir mesaj giderdi. 4 Ağustos'ta panelyd gerçekten
#     6,5 dakikada 155 kez çöktü.
#   - `exit 0` ile ölen servis OnFailure'ı tetiklemiyor ve yeniden de
#     başlamıyor: sessiz ölüm.
# Neden `systemctl show` DEĞİL: DynamicUser ile sistem veri yoluna
# bağlanılamıyor ("Transport endpoint is not connected"); User=nobody
# ile bağlanılıyor (ölçüldü). Sebep kesinleşmedi.
#
# Bu yüzden iki kaynak, ikisi de yeni yetki istemiyor:
#   - ÇÖKÜŞ: systemd'nin kendi "Failed with result" olayı journal'da.
#     Ölçüldü: her çöküşte BİR kez; restart, stop ve exit 0'da HİÇ.
#   - ÇALIŞIYOR MU: birimin cgroup'unda süreç var mı. Sandbox'tan
#     okunuyor, root'un gördüğüyle aynı (ölçüldü).
#
# Kapsam: çöküş ve ölüm. ASKIDA KALMA değil — birimlerde WatchdogSec yok,
# kilitlenen bir panelyd'nin süreci durur ve cgroup'u dolu kalır.
CEKIRDEK_BIRIMLER=(panelyd.service panely-exec.service panely-caddy.service)
OLAY_BASARISIZ=d9b373ed55a64feb8242e02dbe79a49c
CGROUP_KOK=/sys/fs/cgroup/system.slice

# servis_karar <eski_durum> <eski_asagi> <cokus> <calisiyor 0|1>
#   → "<eylem> <yeni_durum> <yeni_asagi>"
#
# Kenar tetikleme: mesaj yalnızca durum DEĞİŞİNCE. Bir kez kapalı
# görülmek yetmiyor (yükseltmedeki kısa yeniden başlatma); iki tur şart.
# Çöküşte beklenmiyor: çöküş olayı bakım işleminde hiç üretilmiyor.
servis_karar() {
    local durum="$1" asagi="$2" cokus="$3" calisiyor="$4"
    [[ "$durum" =~ ^(saglam|coktu|dongu|calismiyor)$ ]] || durum=saglam
    [[ "$asagi" =~ ^[0-9]+$ ]] || asagi=0
    [[ "$cokus" =~ ^[0-9]+$ ]] || cokus=0

    if (( cokus > 0 )); then
        local yeni_asagi=$(( calisiyor ? 0 : 1 ))
        case "$durum" in
            coktu) echo "dongu dongu $yeni_asagi" ;;
            dongu) echo "yok dongu $yeni_asagi" ;;
            *)     echo "coktu coktu $yeni_asagi" ;;
        esac
    elif (( calisiyor )); then
        case "$durum" in
            coktu|dongu) echo "duzeldi saglam 0" ;;
            calismiyor)  echo "geri_geldi saglam 0" ;;
            *)           echo "yok saglam 0" ;;
        esac
    else
        asagi=$(( asagi >= 2 ? 2 : asagi + 1 ))
        if (( asagi >= 2 )) && [[ "$durum" != calismiyor ]]; then
            echo "calismiyor calismiyor 2"
        else
            echo "yok $durum $asagi"
        fi
    fi
}

# servis_olay_ayikla — journalctl -o json satırlarından "<birim> <sonuç>".
#
# Yalnızca _PID=1: sıradan bir süreç UNIT= ve MESSAGE_ID= alanlarını
# kendisi yazabiliyor (ölçüldü); _PID'i journald koyuyor. Aksi hâlde
# ele geçirilen panelyd, caddy adına sahte çöküş yollayabilirdi.
servis_olay_ayikla() {
    local satir birim sonuc
    while IFS= read -r satir; do
        [[ "$satir" == *"\"MESSAGE_ID\":\"$OLAY_BASARISIZ\""* ]] || continue
        [[ "$satir" == *'"_PID":"1"'* ]] || continue
        [[ "$satir" =~ \"UNIT\":\"([A-Za-z0-9@._:-]+)\" ]] || continue
        birim="${BASH_REMATCH[1]}"
        sonuc="?"
        [[ "$satir" =~ \"UNIT_RESULT\":\"([a-z-]+)\" ]] && sonuc="${BASH_REMATCH[1]}"
        printf '%s %s\n' "$birim" "$sonuc"
    done
}

# servis_satiri <eylem> <birim> <cokus> <sonuç> <calisiyor>
#
# Yalnızca systemd'nin alanları: servisin kendi çıktısı (bir panik
# mesajı, içinde veri olabilir) Telegram'a GİTMİYOR.
servis_satiri() {
    local eylem="$1" birim="$2" cokus="$3" sonuc="$4" calisiyor="$5" hal
    case "$eylem" in
        coktu)
            if (( calisiyor )); then hal="systemd yeniden başlattı"; else hal="şu an ÇALIŞMIYOR"; fi
            printf '🟠 ÇÖKTÜ — %s: %d kez (%s), %s\n' "$birim" "$cokus" "$sonuc" "$hal" ;;
        dongu)
            printf '🔴 ÇÖKME DÖNGÜSÜ — %s: son kontrolden beri %d kez daha (%s)\n' "$birim" "$cokus" "$sonuc" ;;
        calismiyor) printf '🔴 ÇALIŞMIYOR — %s\n' "$birim" ;;
        duzeldi)    printf '✅ TOPARLANDI — %s: son kontrolden beri çökmedi\n' "$birim" ;;
        geri_geldi) printf '✅ YENİDEN ÇALIŞIYOR — %s\n' "$birim" ;;
    esac
}

calisiyor_mu() {
    local pid
    { read -r pid < "$CGROUP_KOK/$1/cgroup.procs"; } 2>/dev/null && [[ -n "$pid" ]]
}

# servis_olaylari_oku <kalıcı imleç> <geçici imleç> — olayları "$TMP/servis-olaylar"a.
#
# İmleç birimlerin BÜTÜN akışında tutuluyor, olay sonra süzülüyor.
# Ölçüldü: MESSAGE_ID ile süzülen sorgu hiç eşleşme bulamazsa imleci
# YAZMIYOR. Çöküş geçmişi olmayan bir sunucuda her koşu "ilk koşu"
# olurdu ve ilk çöküş, imleç "şimdi"ye konurken yutulurdu.
servis_olaylari_oku() {
    local imlec="$1" gecici="$2" b birimler=()
    for b in "${CEKIRDEK_BIRIMLER[@]}"; do birimler+=(-u "$b"); done
    if [[ -s "$imlec" ]]; then
        cp "$imlec" "$gecici"
        journalctl "${birimler[@]}" --no-pager -o json \
            --output-fields=MESSAGE_ID,UNIT,UNIT_RESULT,_PID \
            --cursor-file="$gecici" > "$TMP/servis-json" ||
            die "journal okunamadı (servis olayları)"
    else
        # İlk koşu: GEÇMİŞİ gönderme. Test sunucusunun journal'ında
        # kurulum gününden kalma 155 çöküş var; hepsi bir mesajda giderdi.
        journalctl "${birimler[@]}" --no-pager -o json -n 0 \
            --cursor-file="$gecici" > /dev/null ||
            die "journal okunamadı (servis olayları)"
        : > "$TMP/servis-json"
    fi
    servis_olay_ayikla < "$TMP/servis-json" > "$TMP/servis-olaylar"
}

# EN AZ BİR KEZ teslim, `izle` ile aynı desen: imleç de durum da ancak
# gönderim başarılıysa ilerliyor.
servisler() {
    local durum_dizini="${STATE_DIRECTORY:-/var/lib/panely-notify}"
    local imlec="$durum_dizini/servis-imlec" durum_dosyasi="$durum_dizini/servisler"
    [[ -d "$CGROUP_KOK" ]] || die "cgroup v2 bekleniyordu, $CGROUP_KOK yok"
    servis_olaylari_oku "$imlec" "$TMP/servis-imlec"

    local b eski cokus sonuc calisiyor eylem yeni_durum yeni_asagi
    : > "$TMP/servis-mesaj"; : > "$TMP/servis-durum"
    for b in "${CEKIRDEK_BIRIMLER[@]}"; do
        eski="$(awk -v b="$b" '$1 == b { print $2, $3 }' "$durum_dosyasi" 2>/dev/null)"
        cokus="$(awk -v b="$b" '$1 == b { n++ } END { print n + 0 }' "$TMP/servis-olaylar")"
        sonuc="$(awk -v b="$b" '$1 == b { s = $2 } END { print s }' "$TMP/servis-olaylar")"
        calisiyor=0; calisiyor_mu "$b" && calisiyor=1
        # shellcheck disable=SC2086
        read -r eylem yeni_durum yeni_asagi < <(servis_karar ${eski:-saglam 0} "$cokus" "$calisiyor")
        printf '%s %s %s\n' "$b" "$yeni_durum" "$yeni_asagi" >> "$TMP/servis-durum"
        [[ "$eylem" == yok ]] ||
            servis_satiri "$eylem" "$b" "$cokus" "$sonuc" "$calisiyor" >> "$TMP/servis-mesaj"
    done

    if [[ -s "$TMP/servis-mesaj" ]]; then
        { printf '🖥 %s\n' "$SUNUCU"; cat "$TMP/servis-mesaj"; } > "$TMP/servis-gonder"
        gonder "$TMP/servis-gonder" ||
            die "servis durumu gönderilemedi — bir sonraki koşuda tekrar denenecek"
        log "servis durumu gönderildi ($(wc -l < "$TMP/servis-mesaj") satır)"
    fi
    imlec_koy "$TMP/servis-imlec" "$imlec"
    imlec_koy "$TMP/servis-durum" "$durum_dosyasi"
}

# ── hata <birim> ─────────────────────────────────────────────────────
#
# Bazı arızalar panelyd'de ALARM üretmiyor. En önemlisi uzak yedeğin
# başarısız olması: o ayrı bir birim ve panelyd onu göremiyor. systemd
# `OnFailure=` ile bu kipi çağırıyor.
hata() {
    local birim="${1:-}"
    [[ "$birim" =~ ^[A-Za-z0-9@._:-]+$ ]] || die "geçersiz birim adı: $birim"
    {
        printf '🖥 %s\n🔴 BİRİM BAŞARISIZ — %s\n\n' "$SUNUCU" "$birim"
        journalctl -u "$birim" --no-pager -o cat -n 4 2>/dev/null |
            cut -c1-300 | sed 's/^/   /'
    } > "$TMP/mesaj"
    gonder "$TMP/mesaj" || die "birim arızası bildirilemedi: $birim"
    log "birim arızası bildirildi: $birim"
}

dene() {
    printf '🖥 %s\n✅ Panely deneme mesajı — alarm teslimatı çalışıyor.\n' "$SUNUCU" > "$TMP/mesaj"
    gonder "$TMP/mesaj" || die "deneme mesajı gönderilemedi"
    log "deneme mesajı gönderildi"
}

# ── sohbet-bul ───────────────────────────────────────────────────────
#
# Kullanıcı bota bir mesaj yazdıktan sonra, o sohbetin kimliğini
# bulmak için. Yalnızca kimlik ve ad basılır; anahtar basılmaz.
sohbet_bul() {
    local kod sohbetler
    kod="$(telegram getUpdates)"
    [[ "$kod" == 200 ]] || die "getUpdates başarısız (http=${kod:-yok}) — anahtar doğru mu?"
    sohbetler="$(grep -o '"chat":{"id":-\?[0-9]*[^}]*' "$TMP/yanit" | sort -u |
        sed -E 's/"chat":\{"id":(-?[0-9]+).*"(first_name|title)":"([^"]*)".*/sohbet kimliği: \1   (\3)/')"
    [[ -n "$sohbetler" ]] ||
        die "hiç mesaj yok — önce Telegram'da bota bir mesaj yaz, sonra tekrar dene"
    printf '%s\n' "$sohbetler"
}

# Kaynak olarak yüklenirse (biçimlendirici testi) yalnızca fonksiyonlar
# tanımlanır; hiçbir şey okunmaz ve gönderilmez.
[[ "${BASH_SOURCE[0]}" == "$0" ]] || return 0

yapilandirma_oku
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT
SUNUCU="$(hostname)"

case "${1:-izle}" in
    izle)       izle; servisler; nabiz_at ;;
    hata)       hata "${2:-}" ;;
    dene)       dene ;;
    sohbet-bul) sohbet_bul ;;
    *)          die "bilinmeyen kip: $1 (izle | hata <birim> | dene | sohbet-bul)" ;;
esac
