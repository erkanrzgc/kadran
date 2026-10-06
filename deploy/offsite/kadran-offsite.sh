#!/usr/bin/env bash
#
# Kadran UZAK yedek yükleyicisi.
#
# Yerel anlık görüntüleri şifreler ve bir rclone hedefine kopyalar.
# Hacim arşivlerini (K-111, kadran-volume-backup.sh) zaten şifreli
# oldukları için OLDUĞU GİBİ kopyalar.
#
# ── Bu betik neden AYRI bir süreç ────────────────────────────────────
#
# kadrand `IPAddressDeny=any` taşıyor: hiçbir ağ hedefine ULAŞAMIYOR
# (K-092'de kontrol gruplu ölçüldü). Bu kasıtlı — ele geçirilen bir
# kontrol düzlemi dışarı veri sızdıramasın diye. Yükleme yeteneğini
# kadrand'ye vermek o özelliği çöpe atardı.
#
# Dolayısıyla yükleyici ayrı bir birim: kendi (dar) ağ politikası var,
# kadrand'ninki dokunulmadan kalıyor.
#
# ── Özel anahtar bu makinede YOK ─────────────────────────────────────
#
# Yedekler SIR TAŞIYOR. Ölçüldü, varsayılmadı:
#
#   sqlite3 yedek.db "SELECT env_json FROM apps"
#   → {"DATABASE_URL":"postgres://kadran:<parola>@db:5432/..."}
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
# süreç onu okuyabiliyor. `kadran` kullanıcısı ele geçirilirse saldırgan
# uzak yedekleri SİLEBİLİR. Şifreleme okumayı engelliyor, silmeyi
# engellemiyor. Gerçek koruma sağlayıcı tarafında: B2'de silme yetkisi
# olmayan bir uygulama anahtarı, S3'te Delete'i reddeden bir politika,
# R2'de ise kovadaki bucket lock (R2 token'larında silmesiz yazma izni
# yok). Bkz. deploy/offsite/README.md.
set -uo pipefail

CONF="${KADRAN_OFFSITE_CONF:-/etc/kadran/offsite.conf}"
BACKUP_DIR="${KADRAN_BACKUP_DIR:-/var/lib/kadran/backups}"
VOLUME_BACKUP_DIR="${KADRAN_VOLUME_BACKUP_DIR:-/var/lib/kadran-volume-backup}"

log()  { echo "kadran-offsite: $*"; }
die()  { echo "kadran-offsite: ERROR: $*" >&2; exit 1; }

# ── Yapılandırma YOKSA sessizce başarılı olma ────────────────────────
#
# Eksik yapılandırmayla "yapacak bir şey yok, çıkış 0" demek, yedeğin
# hiç alınmadığı bir sistemi SAĞLIKLI gösterirdi. Bu sınıf hata bu
# projede daha önce görüldü (K-073): hiçbir şey yapmayan bir adımın
# yeşil geçmesi.
[[ -r "$CONF" ]] || die "could not read the configuration: $CONF (setup: deploy/offsite/README.md)"

# shellcheck source=/dev/null
source "$CONF"

: "${OFFSITE_REMOTE:?OFFSITE_REMOTE is not set in offsite.conf}"
: "${OFFSITE_RECIPIENT:?OFFSITE_RECIPIENT (age public key) is not set in offsite.conf}"
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
    *) die "OFFSITE_PRUNE must be 'evet' (yes) or 'hayir' (no): $OFFSITE_PRUNE" ;;
esac

command -v age    >/dev/null || die "age is not installed"
command -v rclone >/dev/null || die "rclone is not installed"

# ── rclone yapılandırması daemon'un DEĞİŞTİREMEYECEĞİ yerde olmalı ─────
#
# rclone yapılandırması komut çalıştırabilir (webdav
# `bearer_token_command`). Bu betik AĞ GÖREN tek birimde koşuyor; ağı
# olmayan kadrand bu dosyayı değiştirebilseydi, ağa çıkan bir süreçte
# komut çalıştırmış olurdu (K-100).
#
# RCLONE_CONFIG tanımsızsa rclone `$HOME/.config/rclone` altına bakar —
# `kadran` için orası /var/lib/kadran, yani TAM OLARAK daemon'un dizini.
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
    || die "RCLONE_CONFIG is not set — rclone would look in the daemon's directory (see K-100)"
[[ -r "$RCLONE_CONFIG" ]] || die "could not read the rclone configuration: $RCLONE_CONFIG"
yol="$RCLONE_CONFIG"
while :; do
    read -r sahip kip < <(stat -c '%u %a' "$yol") \
        || die "could not read ownership: $yol"
    if [[ "$sahip" != 0 ]] || (( 8#$kip & 8#022 )); then
        die "$yol is not owned by root or someone else can write to it (owner=$sahip mode=$kip) — the rclone configuration is somewhere the daemon could change it (see K-100)"
    fi
    [[ "$yol" == / ]] && break
    yol="$(dirname "$yol")"
done

[[ -d "$BACKUP_DIR" ]] || die "backup directory missing: $BACKUP_DIR"

# ── Alıcı anahtarı BİÇİM olarak doğrula ──────────────────────────────
#
# Bozuk bir alıcı dizgisiyle `age` zaten hata verir, ama hata mesajı
# "yükleme başarısız" gibi okunur. Burada erken ve AÇIK ölüyoruz.
[[ "$OFFSITE_RECIPIENT" == age1* ]] ||
    die "OFFSITE_RECIPIENT is not an age public key (it must look like age1...): $OFFSITE_RECIPIENT"

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

uploaded=0
skipped=0
failed=0
snapshots=0

# Uzakta ZATEN olanları bir kez, BOYUTLARIYLA listele: her dosya için
# ayrı ağ turu atmak, 24 yedekle 24 gereksiz istek demekti.
remote_list="$tmp/remote.txt"
if ! rclone lsf --format ps --separator '|' "$OFFSITE_REMOTE" \
        > "$remote_list" 2>"$tmp/lsf.err"; then
    die "could not list the remote ($OFFSITE_REMOTE): $(head -2 "$tmp/lsf.err")"
fi
declare -A uzak_boyut=()
while IFS='|' read -r ad boyut; do
    [[ -n "$ad" ]] && uzak_boyut["$ad"]="$boyut"
done < "$remote_list"
log "the remote holds ${#uzak_boyut[@]} files"

# yukle_dogrula <yerel şifreli dosya> <uzak ad>
#
# ── Uzakta ADI olan dosya DOĞRU sanılmaz ─────────────────────────────
#
# Eski hâli yalnızca ada bakıyordu. Ölçüldü: uzaktaki 100 baytlık KESİK
# bir kopya "atlandı" sayıldı, koşu çıkış 0 ile bitti ve bozuk kopya
# kalıcı oldu. Yüklemenin kendi boyut denetimi o koşuyu doğru biçimde
# düşürürdü, ama BİR SONRAKİ koşu dosyayı "zaten var" diye geçiyordu.
# Artık boyut da tutmalı; tutmuyorsa yeniden yükleniyor.
#
# Sağlayıcı üzerine yazmayı reddederse (R2 bucket lock) yükleme düşer
# ve koşu BAŞARISIZ olur — bozuk kopya en azından sessiz kalmaz.
#
# ⚠ Sınır: AYNI boyutta bozulmuş bir kopya bu denetimden geçer.
# Şifreli içerik her seferinde farklı olduğu için karşılaştırılacak
# bir hash yok; bunu yakalayan tek yol geri yükleme tatbikatı.
yukle_dogrula() {
    local yerel="$1" enc="$2" want got
    want="$(stat -c %s "$yerel")"
    if [[ -n "${uzak_boyut[$enc]+var}" ]]; then
        if [[ "${uzak_boyut[$enc]}" == "$want" ]]; then
            skipped=$((skipped + 1))
            return 0
        fi
        echo "kadran-offsite: REMOTE COPY CORRUPT $enc (remote=${uzak_boyut[$enc]} expected=$want) — uploading again" >&2
    fi

    if ! rclone copyto "$yerel" "$OFFSITE_REMOTE/$enc" 2>"$tmp/cp.err"; then
        echo "kadran-offsite: upload failed $enc: $(head -1 "$tmp/cp.err")" >&2
        failed=$((failed + 1))
        return 1
    fi

    # ── YÜKLENDİ demek YERİNE ULAŞTI'yı ÖLÇ ──────────────────────────
    #
    # `rclone copyto`nun sıfır dönmesi dosyanın karşıda DOĞRU boyutta
    # durduğunu kanıtlamaz. Kesilmiş bir yedek, olmayan bir yedekten
    # daha kötüdür: geri yükleme gününe kadar sağlıklı görünür.
    got="$(rclone size --json "$OFFSITE_REMOTE/$enc" 2>/dev/null |
           grep -oE '"bytes":[0-9]+' | cut -d: -f2)"
    if [[ "$got" != "$want" ]]; then
        echo "kadran-offsite: SIZE MISMATCH $enc (local=$want remote=${got:-missing})" >&2
        failed=$((failed + 1))
        return 1
    fi

    uploaded=$((uploaded + 1))
    log "uploaded $enc ($want bytes, verified)"
}

shopt -s nullglob
for snap in "$BACKUP_DIR"/kadran-*.db; do
    snapshots=$((snapshots + 1))
    base="$(basename "$snap")"
    enc="${base}.age"

    # Şifrele. Çıktı PrivateTmp içinde; düz metin asla kalıcı diske
    # yazılmıyor.
    #
    # Uzakta zaten olsa bile ÖNCE şifreleniyor: beklenen boyutu bilmenin
    # tek güvenilir yolu bu. age çıktısının boyutu aynı girdi ve aynı
    # alıcı türü için SABİT (ölçüldü: aynı yedek üç kez → 3 × 143.592
    # bayt); içerik her seferinde farklı olduğu için yalnızca boyut
    # karşılaştırılabilir.
    if ! age -r "$OFFSITE_RECIPIENT" -o "$tmp/$enc" "$snap" 2>"$tmp/age.err"; then
        echo "kadran-offsite: encryption failed $base: $(head -1 "$tmp/age.err")" >&2
        failed=$((failed + 1))
        continue
    fi
    yukle_dogrula "$tmp/$enc" "$enc"
    rm -f "$tmp/$enc"
done

# ── Hacim arşivleri (K-111) ──────────────────────────────────────────
#
# kadran-volume-backup.sh onları AYNI alıcıyla zaten şifreledi; burada
# yeniden şifrelenmiyor, olduğu gibi yükleniyor. Boyut karşılaştırması
# da yerel ŞİFRELİ dosyanın boyutuyla. Arşivleyici isteğe bağlı: kurulu
# değilse dizin yok.
#
# Gizli adlar (.yaziliyor-…) desene uymuyor: arşivleyici yarım dosyayı
# o adla yazıyor, bitince yerine koyuyor.
# ── Zincir çapaları (K-126 C) ────────────────────────────────────────
#
# Daemon her yedeğin yanına denetim zincirinin o anki ucunu yazıyor
# (kadran-<damga>.capa: sıra no + hash). ŞİFRELENMEDEN yükleniyor: içinde
# sır yok ve `kadran audit verify -anchors` özel anahtar istemesin.
# `kadran-` öneki kova kilidinin kapsamında: daemon ele geçirilse bile
# yüklenmiş bir çapayı 30 gün değiştiremez. Budama deseni (`.db.age`)
# onları SEÇMEZ; eskiyenleri yaşam döngüsü kuralı siler.
#
# Veritabanı yedeklerinden SONRA: bir çapanın yüklenememesi yedeği
# engellemiyor, ama başarısız sayılıyor ve koşu sonunda birim düşüyor.
for capa in "$BACKUP_DIR"/kadran-*.capa; do
    yukle_dogrula "$capa" "$(basename "$capa")"
done

if [[ -d "$VOLUME_BACKUP_DIR" ]]; then
    for arsiv in "$VOLUME_BACKUP_DIR"/kadran-hacim-*.tar.zst.age; do
        yukle_dogrula "$arsiv" "$(basename "$arsiv")"
    done
else
    log "no volume archive directory ($VOLUME_BACKUP_DIR) — the volume backup is not set up"
fi

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
#
# Veritabanı yedekleri ve hacim arşivleri AYRI budanıyor; hacim arşivleri
# ayrıca UYGULAMA BAŞINA. OFFSITE_KEEP her gruba ayrı uygulanıyor.
#
# ── Ad sırası zaman sırası DEĞİLDİR ─────────────────────────────────
#
# Budama "en eskiden" siler ve en eskiyi ADA göre sıralayarak bulur. Bu
# yalnızca aynı önekli adlarda doğru: "kadran-hacim-aa-2026…" ile
# "kadran-hacim-zz-2019…" karşılaştırılırken önce UYGULAMA adı
# karşılaştırılır. Tüm hacim arşivleri tek grupta budansaydı, en eskiler
# yerine alfabede önce gelen uygulamanın arşivleri silinirdi. İlk hâli
# böyleydi; elle mutasyon testi buldu (K-111). Yerel ayar da sıralamayı
# değiştiriyordu (tire yok sayılıyor) — LC_ALL=C.
#
# uzak_buda <grup> <uzak ad deseni> <uzak adı yerel yola çeviren fonksiyon>
uzak_buda() {
    local sinif="$1" desen="$2" yerel_yol="$3" r drop i keep_floor target_extra
    local remote_all=() prunable=()
    mapfile -t remote_all < <(rclone lsf "$OFFSITE_REMOTE" 2>/dev/null |
                              grep -E "$desen" | LC_ALL=C sort)

    for r in "${remote_all[@]}"; do
        [[ -e "$("$yerel_yol" "$r")" ]] && continue
        prunable+=("$r")
    done

    keep_floor=$(( ${#remote_all[@]} - ${#prunable[@]} ))
    if (( OFFSITE_KEEP < keep_floor )); then
        log "WARNING: OFFSITE_KEEP=$OFFSITE_KEEP but $keep_floor $sinif are still kept locally;" \
            "they are not deleted (the next run would upload them again)."
    fi

    target_extra=$(( OFFSITE_KEEP - keep_floor ))
    (( target_extra < 0 )) && target_extra=0

    if (( ${#prunable[@]} > target_extra )); then
        drop=$(( ${#prunable[@]} - target_extra ))
        log "the remote holds ${#remote_all[@]} $sinif — deleting $drop that are no longer local"
        for ((i = 0; i < drop; i++)); do
            rclone deletefile "$OFFSITE_REMOTE/${prunable[i]}" 2>/dev/null ||
                echo "kadran-offsite: could not delete ${prunable[i]}" >&2
        done
    fi
}
db_yerel()    { printf '%s/%s' "$BACKUP_DIR" "${1%.age}"; }   # "kadran-….db.age" → yerel "kadran-….db"
hacim_yerel() { printf '%s/%s' "$VOLUME_BACKUP_DIR" "$1"; }   # arşiv yerelde de şifreli, aynı ad

# Uzakta arşivi olan uygulamalar. Adın kuralı internal/api/appvalidate.go
# ile aynı; desene girerken güvenli (yalnızca harf, rakam, tire).
hacim_uygulamalari() {
    rclone lsf "$OFFSITE_REMOTE" 2>/dev/null |
        sed -nE 's/^kadran-hacim-([a-z][a-z0-9-]{0,31})-[0-9]{8}T[0-9]{6}Z\.tar\.zst\.age$/\1/p' |
        LC_ALL=C sort -u
}

if [[ "$OFFSITE_PRUNE" == evet ]]; then
    uzak_buda "database backups" '^kadran-.*\.db\.age$' db_yerel
    while read -r uyg; do
        uzak_buda "volume archives ($uyg)" \
            "^kadran-hacim-$uyg-[0-9]{8}T[0-9]{6}Z\\.tar\\.zst\\.age\$" hacim_yerel
    done < <(hacim_uygulamalari)
else
    log "remote pruning OFF (OFFSITE_PRUNE=hayir) — the provider's lifecycle rule deletes old copies"
fi

log "summary: uploaded=$uploaded skipped=$skipped failed=$failed"

# ── Kısmi başarı BAŞARI DEĞİLDİR ─────────────────────────────────────
#
# Tek bir dosya bile yüklenemediyse birim BAŞARISIZ olmalı. systemd
# birimin durumunu tutuyor; sıfır dönersek arıza hiçbir yerde
# görünmezdi.
(( failed == 0 )) || exit 1

# Hiç veritabanı yedeği yoksa bu da bir arızadır: yerel yedekleme
# çalışmıyor demektir. Hacim arşivleri bunu örtmemeli.
if (( snapshots == 0 )); then
    die "NO BACKUP FOUND to upload — is the local backup running?"
fi
