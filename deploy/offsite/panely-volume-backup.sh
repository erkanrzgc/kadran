#!/usr/bin/env bash
#
# Panely HACİM arşivleyicisi — uygulamaların kalıcı disk verisini
# şifreli arşivlere çevirir (K-111). Uzağa `panely-offsite.sh` taşır.
#
# ── Neden AYRI bir birim ─────────────────────────────────────────────
#
# panelyd uygulama verisini OKUYAMIYOR ve bu bir güvence (K-091'de
# ölçüldü: /var/lib/panely/volumes/<uyg> root:root 0750). İki kolay yol
# vardı, ikisi de bir şey götürüyordu:
#   - executor'a yeni bir RPC: ayrıcalıklı yüzey bütçesi (2498/2500)
#   - dizinleri `panely` grubuna açmak: güvencenin kendisi
#
# Bu birim üçüncü yol. HER ŞEYİ OKUYABİLİYOR (CAP_DAC_READ_SEARCH) ama:
#   - ağı YOK, soket açamıyor (Docker soketi dahil — ölçüldü)
#   - yalnızca kendi çıktı dizinine yazabiliyor
#   - DIŞARI YALNIZCA ŞİFRELİ METİN veriyor: `age` AÇIK anahtarla
#     şifreliyor, özel anahtar sunucuda YOK (uzak yedekle aynı anahtar)
# panelyd çıktıyı okuyabilir, ama içindeki veriyi okuyamaz.
#
# ── ⚠ ANLIK GÖRÜNTÜ DEĞİL ────────────────────────────────────────────
#
# tar dosyaları uygulama ÇALIŞIRKEN tek tek okur. Bir veritabanının
# dosyaları farklı anlardan gelebilir; geri yüklenen kopya bozuk
# olabilir. Uygulama durdurulmuyor: durdurmak Docker yetkisi ister, o da
# root'a eşdeğer bir birim demek. Veritabanı taşıyan uygulamalar önce
# dökümü hacme almalı (pg_dump, `sqlite3 .backup`); döküm dosyası
# tutarlıdır. README'de de yazıyor.
set -uo pipefail
umask 0027
export LC_ALL=C   # glob sırası = damga sırası; yerel ayar sıralamayı değiştirmesin

CONF="${PANELY_OFFSITE_CONF:-/etc/panely/offsite.conf}"
VOLUMES_DIR="${PANELY_VOLUMES_DIR:-/var/lib/panely/volumes}"
OUT_DIR="${PANELY_VOLUME_BACKUP_DIR:-/var/lib/panely-volume-backup}"

log() { echo "panely-volume-backup: $*"; }
die() { echo "panely-volume-backup: HATA: $*" >&2; exit 1; }

# ── Alıcı anahtarı panelyd'nin DEĞİŞTİREMEYECEĞİ yerde olmalı ────────
#
# Bu, tasarımın taşıyıcı duvarı. Alıcıyı değiştirebilen biri KENDİ
# anahtarını koyar ve bu birim, her şeyi okuyabilen bir süreç olarak,
# bütün uygulama verisini ona şifreler. panelyd çıktıyı okuyabildiği
# için bu, "panelyd uygulama verisini okuyamaz" güvencesinin sonu olurdu.
#
# Dosya ve köke kadar her üst dizin root'un olmalı ve grup/diğerleri
# YAZAMAMALI (K-100: yazılabilir dizindeki root dosyası silinip yerine
# başkası konabilir). Canlıda: /etc/panely root:root 0755, offsite.conf
# root:panely 0640; panely içine dosya koyamıyor (ölçüldü).
root_disinda_yazilamaz() {
    local yol="$1" sahip kip
    while :; do
        read -r sahip kip < <(stat -c '%u %a' "$yol") ||
            die "sahiplik okunamadı: $yol"
        if [[ "$sahip" != 0 ]] || (( 8#$kip & 8#022 )); then
            die "$yol root'a ait değil ya da başkası yazabiliyor (sahip=$sahip kip=$kip) — alıcı anahtarı değiştirilebilir (bkz. K-111)"
        fi
        [[ "$yol" == / ]] && return 0
        yol="$(dirname "$yol")"
    done
}

# ── Dosya SOURCE EDİLMİYOR, ayrıştırılıyor ───────────────────────────
#
# Bu süreç her şeyi okuyabiliyor. Yapılandırmayı `source` etmek, oraya
# yazılan her komutu bu yetkiyle çalıştırmak demekti. Yalnızca iki
# anahtar okunuyor, ikisi de sıkı biçimde doğrulanıyor.
ayar() {
    sed -n "s/^[[:space:]]*$1[[:space:]]*=[[:space:]]*//p" "$CONF" | tail -1 |
        tr -d "\"'[:space:]"
}

[[ -r "$CONF" ]] || die "yapılandırma okunamadı: $CONF (kurulum: deploy/offsite/README.md)"
root_disinda_yazilamaz "$CONF"

ALICI="$(ayar OFFSITE_RECIPIENT)"
[[ "$ALICI" =~ ^age1[a-z0-9]{58}$ ]] ||
    die "OFFSITE_RECIPIENT bir age açık anahtarı değil (age1 + 58 karakter): '$ALICI'"

KEEP="$(ayar OFFSITE_VOLUME_KEEP)"
KEEP="${KEEP:-3}"
[[ "$KEEP" =~ ^[1-9][0-9]*$ ]] || die "OFFSITE_VOLUME_KEEP pozitif bir sayı olmalı: '$KEEP'"

for arac in tar zstd age; do
    command -v "$arac" >/dev/null || die "$arac kurulu değil"
done
[[ -d "$OUT_DIR" ]] || die "çıktı dizini yok: $OUT_DIR"

# Hacim dizini hiç yoksa panely'de kalıcı disk kullanan uygulama yok:
# yapacak iş gerçekten yok. Bu sessiz bir başarı değil, açıkça yazılıyor.
if [[ ! -d "$VOLUMES_DIR" ]]; then
    log "hacim dizini yok ($VOLUMES_DIR) — arşivlenecek veri yok"
    exit 0
fi

TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

# Damga sabit genişlikte ve UTC: sözlük sırası = zaman sırası (K-091).
DAMGA="$(date -u +%Y%m%dT%H%M%SZ)"
DAMGA_GLOB='[0-9][0-9][0-9][0-9][0-9][0-9][0-9][0-9]T[0-9][0-9][0-9][0-9][0-9][0-9]Z'

# arsivle <uygulama> — tek uygulamanın bütün hacimleri, tek arşiv.
#
# Arşiv önce gizli bir adla yazılıyor, ancak her aşama başarılıysa
# yerine konuyor: yükleyici yarım bir dosyayı asla görmüyor.
arsivle() {
    local uyg="$1" gecici hedef kodlar
    gecici="$OUT_DIR/.yaziliyor-$uyg-$DAMGA"
    hedef="$OUT_DIR/panely-hacim-$uyg-$DAMGA.tar.zst.age"

    # --one-file-system: hacmin içine bağlanmış başka bir dosya sistemi
    #   arşive girmez.
    # -h YOK: konteyner hacmin içeriğini yönetiyor; /etc/shadow'u
    #   gösteren bir sembolik bağ, bağ olarak arşivlenir. -h ile hedefin
    #   İÇERİĞİ girerdi (ölçüldü: 0 bayt vs 933 bayt).
    # --numeric-owner: konteynerin kullanıcısı (ör. uid 101) host'ta
    #   başka birinin adına denk gelebilir; geri yüklemede sayı korunmalı.
    tar --one-file-system --numeric-owner -C "$VOLUMES_DIR" -cf - "$uyg" 2>"$TMP/tar.err" |
        zstd -q |
        age -r "$ALICI" > "$gecici" 2>"$TMP/age.err"
    kodlar=("${PIPESTATUS[@]}")

    # tar 1 = "dosya okunurken değişti": arşiv yazıldı ama o dosya
    # tutarsız olabilir. Uyarı; arşiv tutuluyor (hiç yoktan iyi).
    if (( kodlar[0] > 1 || kodlar[1] != 0 || kodlar[2] != 0 )); then
        rm -f "$gecici"
        echo "panely-volume-backup: arşivlenemedi $uyg (tar=${kodlar[0]} zstd=${kodlar[1]} age=${kodlar[2]}): $(head -1 "$TMP/tar.err") $(head -1 "$TMP/age.err")" >&2
        return 1
    fi
    mv "$gecici" "$hedef" || { rm -f "$gecici"; return 1; }
    if (( kodlar[0] == 1 )); then
        log "UYARI: $uyg okunurken değişti — arşiv tutarlı olmayabilir: $(head -1 "$TMP/tar.err")"
    fi
    log "arşivlendi $(basename "$hedef") ($(stat -c %s "$hedef") bayt)"
}

# budama <uygulama> — o uygulamanın en yeni $KEEP arşivi kalır.
#
# Desen damgayı da içeriyor: yalnızca önek kullanılsaydı "web"in
# budaması "web-2"nin arşivlerini de silerdi.
budama() {
    local uyg="$1" arsivler fazla i
    # shellcheck disable=SC2206
    arsivler=( "$OUT_DIR"/panely-hacim-"$uyg"-$DAMGA_GLOB.tar.zst.age )
    fazla=$(( ${#arsivler[@]} - KEEP ))
    for (( i = 0; i < fazla; i++ )); do
        rm -f "${arsivler[i]}" && log "budandı $(basename "${arsivler[i]}")"
    done
}

shopt -s nullglob
arsivlendi=0
basarisiz=0
for yol in "$VOLUMES_DIR"/*; do
    uyg="$(basename "$yol")"
    # Uygulama adı kuralı internal/api/appvalidate.go'daki ile aynı.
    # Beklenmeyen bir girdi sessizce atlanmıyor: koşu başarısız olur.
    if [[ -L "$yol" || ! -d "$yol" || ! "$uyg" =~ ^[a-z][a-z0-9-]{0,31}$ ]]; then
        echo "panely-volume-backup: beklenmeyen girdi, atlandı: $yol" >&2
        basarisiz=$((basarisiz + 1))
        continue
    fi
    if arsivle "$uyg"; then
        arsivlendi=$((arsivlendi + 1))
        budama "$uyg"
    else
        basarisiz=$((basarisiz + 1))
    fi
done

# Silinmiş bir uygulamanın hacim dizini duruyorsa o da arşivleniyor
# (`app delete` veriye dokunmuyor). Dizini SİLİNMİŞ bir uygulamanın
# eski arşivleri ise budanmıyor: o verinin son kopyası onlar.

log "özet: arşivlendi=$arsivlendi başarısız=$basarisiz"
(( basarisiz == 0 )) || exit 1
(( arsivlendi > 0 )) || log "hacim dizini boş — arşivlenecek uygulama yok"
