#!/usr/bin/env bash
#
# Panely UZAK yedek yükleyicisi.
#
# Yerel anlık görüntüleri şifreler ve bir rclone hedefine kopyalar.
#
# ── Bu betik neden AYRI bir süreç ────────────────────────────────────
#
# panelyd `IPAddressDeny=any` taşıyor: hiçbir ağ hedefine ULAŞAMIYOR
# (K-092'de kontrol gruplu ölçüldü). Bu kasıtlı — ele geçirilen bir
# kontrol düzlemi dışarı veri sızdıramasın diye. Yükleme yeteneğini
# panelyd'ye vermek o özelliği çöpe atardı.
#
# Dolayısıyla yükleyici ayrı bir birim: kendi (dar) ağ politikası var,
# panelyd'ninki dokunulmadan kalıyor.
#
# ── Özel anahtar bu makinede YOK ─────────────────────────────────────
#
# Yedekler SIR TAŞIYOR. Ölçüldü, varsayılmadı:
#
#   sqlite3 yedek.db "SELECT env_json FROM apps"
#   → {"DATABASE_URL":"postgres://panely:<parola>@db:5432/..."}
#
# Bu yüzden şifreleme zorunlu. `age` AÇIK ANAHTARLA şifreliyor:
# sunucuda yalnızca alıcı açık anahtarı duruyor, özel anahtar burada
# HİÇ bulunmuyor. Sunucu ele geçirilse bile saldırgan GEÇMİŞ yedekleri
# çözemez — yalnızca yenilerini yazabilir.
#
# ⚠ Bunun bedeli: özel anahtar kaybolursa yedekler KURTARILAMAZ.
# Anahtar sunucudan BAŞKA bir yerde, en az iki kopya durmalı.
#
# ── Uzak kimlik bilgisi hakkında dürüst uyarı ────────────────────────
#
# rclone yapılandırması uzak hedefin kimlik bilgisini taşıyor ve bu
# süreç onu okuyabiliyor. `panely` kullanıcısı ele geçirilirse saldırgan
# uzak yedekleri SİLEBİLİR. Şifreleme okumayı engelliyor, silmeyi
# engellemiyor. Gerçek koruma sağlayıcı tarafında: B2'de silme yetkisi
# olmayan bir uygulama anahtarı, S3'te Delete'i reddeden bir politika,
# R2'de ise kovadaki bucket lock (R2 token'larında silmesiz yazma izni
# yok). Bkz. deploy/offsite/README.md.
set -uo pipefail

CONF="${PANELY_OFFSITE_CONF:-/etc/panely/offsite.conf}"
BACKUP_DIR="${PANELY_BACKUP_DIR:-/var/lib/panely/backups}"

log()  { echo "panely-offsite: $*"; }
die()  { echo "panely-offsite: HATA: $*" >&2; exit 1; }

# ── Yapılandırma YOKSA sessizce başarılı olma ────────────────────────
#
# Eksik yapılandırmayla "yapacak bir şey yok, çıkış 0" demek, yedeğin
# hiç alınmadığı bir sistemi SAĞLIKLI gösterirdi. Bu sınıf hata bu
# projede daha önce görüldü (K-073): hiçbir şey yapmayan bir adımın
# yeşil geçmesi.
[[ -r "$CONF" ]] || die "yapılandırma okunamadı: $CONF (kurulum: deploy/offsite/README.md)"

# shellcheck source=/dev/null
source "$CONF"

: "${OFFSITE_REMOTE:?offsite.conf içinde OFFSITE_REMOTE tanımlı değil}"
: "${OFFSITE_RECIPIENT:?offsite.conf içinde OFFSITE_RECIPIENT (age açık anahtarı) tanımlı değil}"
OFFSITE_KEEP="${OFFSITE_KEEP:-30}"

# OFFSITE_PRUNE=hayir: uzak budama TAMAMEN kapalı; eskiyenleri
# sağlayıcının yaşam döngüsü kuralı siler.
#
# Neden gerekli: Cloudflare R2'nin token'larında "yaz ama silme" izni
# YOK (yalnızca Object Read & Write). Silmeyi engelleyen şey kovadaki
# bucket lock (saklama kilidi). Kilitli dosyaları silmeye çalışan bir
# budama her koşuda hata basardı. OFFSITE_KEEP=0 bu anlama GELMİYOR —
# o "yerelde olmayan her şeyi sil" demek; açık bir anahtar gerekiyordu.
OFFSITE_PRUNE="${OFFSITE_PRUNE:-evet}"
case "$OFFSITE_PRUNE" in
    evet|hayir) ;;
    *) die "OFFSITE_PRUNE yalnızca 'evet' ya da 'hayir' olabilir: $OFFSITE_PRUNE" ;;
esac

command -v age    >/dev/null || die "age kurulu değil"
command -v rclone >/dev/null || die "rclone kurulu değil"

# ── rclone yapılandırması daemon'un DEĞİŞTİREMEYECEĞİ yerde olmalı ─────
#
# rclone yapılandırması komut çalıştırabilir (webdav
# `bearer_token_command`). Bu betik AĞ GÖREN tek birimde koşuyor; ağı
# olmayan panelyd bu dosyayı değiştirebilseydi, ağa çıkan bir süreçte
# komut çalıştırmış olurdu (K-100).
#
# RCLONE_CONFIG tanımsızsa rclone `$HOME/.config/rclone` altına bakar —
# `panely` için orası /var/lib/panely, yani TAM OLARAK daemon'un dizini.
#
# `-w` ile sınamak İŞE YARAMAZ: birim ProtectSystem=strict ile koşuyor,
# bu ad alanında her şey salt okunur görünür. Tehdit bu sürecin değil,
# DAEMON'un yazabilmesi. O yüzden sahiplik ve kip okunuyor: dosya ve
# köke kadar her üst dizin root'un olmalı ve grup/diğerleri
# yazamamalı. Dosyanın kendi izni yetmez — yazılabilir bir dizindeki
# root dosyası silinip yerine başkası konabilir (K-100'de ölçüldü).
# (`${VAR:?…}` burada KULLANILMIYOR: iletideki kesme işareti o sözdizimi
# içinde tırnak açar ve betiğin tamamını bozar — ilk sürümde oldu.)
[[ -n "${RCLONE_CONFIG:-}" ]] \
    || die "RCLONE_CONFIG tanımlı değil — rclone daemon'un dizinine bakardı (bkz. K-100)"
[[ -r "$RCLONE_CONFIG" ]] || die "rclone yapılandırması okunamadı: $RCLONE_CONFIG"
yol="$RCLONE_CONFIG"
while :; do
    read -r sahip kip < <(stat -c '%u %a' "$yol") \
        || die "sahiplik okunamadı: $yol"
    if [[ "$sahip" != 0 ]] || (( 8#$kip & 8#022 )); then
        die "$yol root'a ait değil ya da başkası yazabiliyor (sahip=$sahip kip=$kip) — rclone yapılandırması daemon'un değiştirebileceği bir yerde (bkz. K-100)"
    fi
    [[ "$yol" == / ]] && break
    yol="$(dirname "$yol")"
done

[[ -d "$BACKUP_DIR" ]] || die "yedek dizini yok: $BACKUP_DIR"

# ── Alıcı anahtarı BİÇİM olarak doğrula ──────────────────────────────
#
# Bozuk bir alıcı dizgisiyle `age` zaten hata verir, ama hata mesajı
# "yükleme başarısız" gibi okunur. Burada erken ve AÇIK ölüyoruz.
[[ "$OFFSITE_RECIPIENT" == age1* ]] ||
    die "OFFSITE_RECIPIENT bir age açık anahtarı değil (age1... olmalı): $OFFSITE_RECIPIENT"

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

uploaded=0
skipped=0
failed=0

# Uzakta ZATEN olanları bir kez listele: her dosya için ayrı ağ turu
# atmak, 24 yedekle 24 gereksiz istek demekti.
remote_list="$tmp/remote.txt"
if ! rclone lsf "$OFFSITE_REMOTE" > "$remote_list" 2>"$tmp/lsf.err"; then
    die "uzak hedef listelenemedi ($OFFSITE_REMOTE): $(head -2 "$tmp/lsf.err")"
fi
log "uzak hedefte $(wc -l < "$remote_list") dosya var"

shopt -s nullglob
for snap in "$BACKUP_DIR"/panely-*.db; do
    base="$(basename "$snap")"
    enc="${base}.age"

    if grep -qxF "$enc" "$remote_list"; then
        skipped=$((skipped + 1))
        continue
    fi

    # Şifrele. Çıktı PrivateTmp içinde; düz metin asla kalıcı diske
    # yazılmıyor.
    if ! age -r "$OFFSITE_RECIPIENT" -o "$tmp/$enc" "$snap" 2>"$tmp/age.err"; then
        echo "panely-offsite: şifrelenemedi $base: $(head -1 "$tmp/age.err")" >&2
        failed=$((failed + 1))
        continue
    fi

    if ! rclone copyto "$tmp/$enc" "$OFFSITE_REMOTE/$enc" 2>"$tmp/cp.err"; then
        echo "panely-offsite: yüklenemedi $enc: $(head -1 "$tmp/cp.err")" >&2
        failed=$((failed + 1))
        rm -f "$tmp/$enc"
        continue
    fi

    # ── YÜKLENDİ demek YERİNE ULAŞTI'yı ÖLÇ ──────────────────────────
    #
    # `rclone copyto`nun sıfır dönmesi dosyanın karşıda DOĞRU boyutta
    # durduğunu kanıtlamaz. Kesilmiş bir yedek, olmayan bir yedekten
    # daha kötüdür: geri yükleme gününe kadar sağlıklı görünür.
    want="$(stat -c %s "$tmp/$enc")"
    got="$(rclone size --json "$OFFSITE_REMOTE/$enc" 2>/dev/null |
           grep -oE '"bytes":[0-9]+' | cut -d: -f2)"
    if [[ "$got" != "$want" ]]; then
        echo "panely-offsite: BOYUT UYUŞMUYOR $enc (yerel=$want uzak=${got:-yok})" >&2
        failed=$((failed + 1))
        rm -f "$tmp/$enc"
        continue
    fi

    rm -f "$tmp/$enc"
    uploaded=$((uploaded + 1))
    log "yüklendi $enc ($want bayt, doğrulandı)"
done

# ── Uzak budama ──────────────────────────────────────────────────────
#
# Damga SABİT GENİŞLİKTE ve UTC (K-091): bu yüzden sözlük sırası =
# zaman sırası. Tarih ayrıştırmaya gerek yok.
#
# Yerel kopyalar SİLİNMİYOR. Yerel, hızlı geri yükleme yolu; uzak,
# diskin tamamen gitmesine karşı. İkisi farklı arızaya karşı duruyor.
#
# ── 🔴 HÂLÂ YERELDE OLAN BİR YEDEK UZAKTAN SİLİNMEZ ──────────────────
#
# Bu kural olmadan betik SONSUZ BİR DÖNGÜYE giriyordu. Ölçüldü:
#
#   OFFSITE_KEEP=5, yerelde 24 anlık görüntü
#   1. koşu : yüklendi=24            → budama 19'unu sildi
#   2. koşu : yüklendi=19 atlandı=5  → budama 19'unu sildi
#   3. koşu : yüklendi=19 atlandı=5  → budama 19'unu sildi
#
# Her koşuda aynı 19 dosya yeniden şifrelenip yükleniyor ve hemen
# siliniyordu. Ücretli bir sağlayıcıda bu, sonsuza kadar süren ve
# kimsenin fark etmediği bir masraf demekti: birim her seferinde
# BAŞARILI raporluyordu.
#
# Kök sebep: budama, bir sonraki koşunun yeniden yükleyeceği dosyaları
# siliyordu. Yerelde duran bir yedeği uzaktan silmek zaten anlamsız —
# uzak kopyanın işi, yerel kopya GİTTİKTEN sonra başlıyor.
uzak_buda() {
    mapfile -t remote_all < <(rclone lsf "$OFFSITE_REMOTE" 2>/dev/null |
                              grep -E '^panely-.*\.db\.age$' | sort)

    prunable=()
    for r in "${remote_all[@]}"; do
        # "panely-....db.age" → "panely-....db"
        [[ -e "$BACKUP_DIR/${r%.age}" ]] && continue
        prunable+=("$r")
    done

    keep_floor=$(( ${#remote_all[@]} - ${#prunable[@]} ))
    if (( OFFSITE_KEEP < keep_floor )); then
        log "UYARI: OFFSITE_KEEP=$OFFSITE_KEEP ama yerelde $keep_floor yedek duruyor;" \
            "onlar silinmiyor (silinseler bir sonraki koşu yeniden yüklerdi)."
    fi

    target_extra=$(( OFFSITE_KEEP - keep_floor ))
    (( target_extra < 0 )) && target_extra=0

    if (( ${#prunable[@]} > target_extra )); then
        drop=$(( ${#prunable[@]} - target_extra ))
        log "uzakta ${#remote_all[@]} yedek var — yerelde olmayan $drop tanesi siliniyor"
        for ((i = 0; i < drop; i++)); do
            rclone deletefile "$OFFSITE_REMOTE/${prunable[i]}" 2>/dev/null ||
                echo "panely-offsite: silinemedi ${prunable[i]}" >&2
        done
    fi
}

if [[ "$OFFSITE_PRUNE" == evet ]]; then
    uzak_buda
else
    log "uzak budama KAPALI (OFFSITE_PRUNE=hayir) — eskiyenleri sağlayıcının yaşam döngüsü kuralı siler"
fi

log "özet: yüklendi=$uploaded atlandı=$skipped başarısız=$failed"

# ── Kısmi başarı BAŞARI DEĞİLDİR ─────────────────────────────────────
#
# Tek bir dosya bile yüklenemediyse birim BAŞARISIZ olmalı. systemd
# birimin durumunu tutuyor; sıfır dönersek arıza hiçbir yerde
# görünmezdi.
(( failed == 0 )) || exit 1

# Hiç yedek yoksa bu da bir arızadır: yerel yedekleme çalışmıyor demektir.
if (( uploaded == 0 && skipped == 0 )); then
    die "yüklenecek yedek BULUNAMADI — yerel yedekleme çalışıyor mu?"
fi
