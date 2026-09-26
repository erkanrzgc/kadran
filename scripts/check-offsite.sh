#!/usr/bin/env bash
# Hacim arşivleyicisini ve uzak yükleyiciyi sınar (K-111).
#
# `rclone` ve `age` SAHTE (PATH'in başında): uzak hedef yerel bir dizin,
# "şifreleme" başlık + düz metin — boyutu belirlenimci, tıpkı gerçeği
# gibi. tar, zstd ve betiklerin kendisi GERÇEK.
#
# ROOT GEREKİR: iki betik de yapılandırmanın ve köke kadar her üst
# dizinin root'a ait olmasını şart koşuyor (K-100, K-111). Bu denetimi
# test için gevşeten bir kaçış kapısı açmak yerine test root olarak
# koşuyor. CI: `sudo bash scripts/check-offsite.sh`.
set -uo pipefail
cd "$(dirname "$0")/.."
DEPO="$PWD"

if [[ "$(id -u)" != 0 ]]; then
    echo "root gerekli (sahiplik denetimleri gerçek root dosyaları istiyor)" >&2
    exit 2
fi
for arac in tar zstd stat; do
    command -v "$arac" >/dev/null || { echo "$arac yok" >&2; exit 2; }
done

KOK="$(mktemp -d /root/panely-offsite-check.XXXXXX)"   # /root 0700: üst dizinler root'un
trap 'rm -rf "$KOK"' EXIT
BIN="$KOK/bin"; ETC="$KOK/etc"; YEDEK="$KOK/backups"; HACIM="$KOK/volumes"
CIKTI="$KOK/hacim-yedek"; UZAK="$KOK/uzak"
mkdir -p "$BIN" "$ETC" "$YEDEK" "$CIKTI" "$UZAK"
chmod 755 "$ETC"

cat > "$BIN/age" <<'EOF'
#!/bin/bash
# sahte age: age -r ALICI [-o ÇIKTI] [GİRDİ] → başlık + girdi
out=""; in=""
while [ $# -gt 0 ]; do
    case "$1" in -r) shift ;; -o) out="$2"; shift ;; *) in="$1" ;; esac
    shift
done
[ -n "${SAHTE_AGE_BOZ:-}" ] && { echo "sahte age: bozuldu" >&2; exit 1; }
{ printf 'age-encryption.org/v1\n'; if [ -n "$in" ]; then cat "$in"; else cat; fi; } > "${out:-/dev/stdout}"
EOF
cat > "$BIN/rclone" <<'EOF'
#!/bin/bash
# sahte rclone: "sahte:kova" = $UZAK dizini
yol() { echo "$UZAK/${1#sahte:kova/}"; }
case "$1" in
    lsf)
        if [ "$2" = --format ]; then
            for f in "$UZAK"/*; do [ -f "$f" ] && echo "$(basename "$f")|$(stat -c %s "$f")"; done
        else
            for f in "$UZAK"/*; do [ -f "$f" ] && basename "$f"; done
        fi
        exit 0 ;;
    copyto) cp "$2" "$(yol "$3")" ;;
    size) f="$(yol "$3")"; [ -f "$f" ] && echo "{\"count\":1,\"bytes\":$(stat -c %s "$f")}" ;;
    deletefile) rm -f "$(yol "$2")" ;;
    *) echo "sahte rclone: bilinmeyen $1" >&2; exit 1 ;;
esac
EOF
chmod 755 "$BIN/age" "$BIN/rclone"
export PATH="$BIN:$PATH" UZAK

ALICI="age1$(printf 'q%.0s' {1..58})"
conf_yaz() {  # conf_yaz [ek satırlar]
    printf 'OFFSITE_REMOTE=sahte:kova\nOFFSITE_RECIPIENT=%s\nOFFSITE_KEEP=2\nOFFSITE_PRUNE=evet\nOFFSITE_VOLUME_KEEP=2\n%s' \
        "$ALICI" "${1:-}" > "$ETC/offsite.conf"
    chmod 644 "$ETC/offsite.conf"
}
conf_yaz
: > "$ETC/rclone.conf"; chmod 600 "$ETC/rclone.conf"

export PANELY_OFFSITE_CONF="$ETC/offsite.conf" PANELY_VOLUMES_DIR="$HACIM" \
       PANELY_VOLUME_BACKUP_DIR="$CIKTI" PANELY_BACKUP_DIR="$YEDEK" RCLONE_CONFIG="$ETC/rclone.conf"

hacim_kur() {
    rm -rf "$HACIM"; mkdir -p "$HACIM/web/veri" "$HACIM/web-2/data"
    echo "web verisi" > "$HACIM/web/veri/a.txt"
    echo "web-2 verisi" > "$HACIM/web-2/data/b.txt"
    ln -s /etc/shadow "$HACIM/web/veri/kacis"
    chown -R 101:101 "$HACIM/web/veri"; chmod 750 "$HACIM/web/veri" "$HACIM/web"
}
arsivle() { bash "$DEPO/deploy/offsite/panely-volume-backup.sh" > "$KOK/cikti" 2>&1; }
yukle()   { bash "$DEPO/deploy/offsite/panely-offsite.sh" > "$KOK/cikti" 2>&1; }
ac()      { tail -n +2 "$1" | zstd -dq; }   # sahte age başlığını at, aç

fail=0
dene() {
    local ad="$1" kosul="$2"
    if eval "$kosul"; then echo "  ✓ $ad"
    else echo "  ✗ $ad"; echo "      koşul: $kosul"; sed 's/^/      | /' "$KOK/cikti" | tail -5; fail=1; fi
}
say() { local n=0 f; for f in "$@"; do [[ -e "$f" ]] && n=$((n + 1)); done; echo "$n"; }

echo "== Hacim arşivleyicisi =="
hacim_kur
# Budama tuzağı: web-2'nin arşivi EN ESKİ damgayı taşıyor. Önek eşleşmesi
# ("panely-hacim-web-*") kullanılsaydı web'in budaması onu silerdi.
echo eski > "$CIKTI/panely-hacim-web-2-20250101T000000Z.tar.zst.age"
for g in 01 02 03; do echo eski > "$CIKTI/panely-hacim-web-202601${g}T000000Z.tar.zst.age"; done

arsivle; kod=$?
shopt -s nullglob
yeni_web=( "$CIKTI"/panely-hacim-web-2026[0-9][0-9][0-9][0-9]T*Z.tar.zst.age )
dene "çıkış 0" '[[ $kod == 0 ]]'
dene "web için KEEP=2 arşiv kaldı" '[[ ${#yeni_web[@]} == 2 ]]'
dene "en eski web arşivleri budandı" '[[ ! -e $CIKTI/panely-hacim-web-20260101T000000Z.tar.zst.age && ! -e $CIKTI/panely-hacim-web-20260102T000000Z.tar.zst.age ]]'
dene "web-2'nin arşivine dokunulmadı (önek tuzağı)" '[[ -e $CIKTI/panely-hacim-web-2-20250101T000000Z.tar.zst.age ]]'
YENI="${yeni_web[1]}"
dene "arşiv kipi 0640" '[[ $(stat -c %a "$YENI") == 640 ]]'
dene "yarım dosya kalmadı" '[[ $(say "$CIKTI"/.yaziliyor-*) == 0 ]]'
# Liste --numeric-owner OLMADAN okunuyor: arşiv kullanıcı ADI taşısaydı
# (uid 101 bu makinede başka birinin adı olabilir) burada görünürdü.
ac "$YENI" | tar -tvf - > "$KOK/liste" 2>&1
dene "içerik: web/veri/a.txt, sahip SAYI olarak 101/101" 'grep -q "101/101 .* web/veri/a.txt" "$KOK/liste"'
dene "sembolik bağ BAĞ olarak (hedef okunmadı)" 'grep -q "^l.* web/veri/kacis -> /etc/shadow" "$KOK/liste"'
dene "bağın içeriği 0 bayt" '[[ $(ac "$YENI" | tar -xOf - web/veri/kacis | wc -c) == 0 ]]'
dene "başka uygulamanın verisi bu arşivde YOK" '! grep -q "web-2" "$KOK/liste"'

echo "== Arşivleyici: reddetmesi gerekenler =="
chmod 664 "$ETC/offsite.conf"; arsivle; kod=$?
dene "grup yazabiliyorsa REDDEDER" '[[ $kod != 0 ]] && grep -q "başkası yazabiliyor" "$KOK/cikti"'
conf_yaz; chmod 775 "$ETC"; arsivle; kod=$?
dene "ÜST DİZİN grup yazabiliyorsa REDDEDER" '[[ $kod != 0 ]] && grep -q "başkası yazabiliyor" "$KOK/cikti"'
chmod 755 "$ETC"
sed -i "s/^OFFSITE_RECIPIENT=.*/OFFSITE_RECIPIENT=age1kisa/" "$ETC/offsite.conf"; arsivle; kod=$?
dene "bozuk alıcı REDDEDİLİR" '[[ $kod != 0 ]] && grep -q "age açık anahtarı değil" "$KOK/cikti"'
conf_yaz "OFFSITE_VOLUME_KEEP=2; touch $KOK/calisti
\$(touch $KOK/calisti2)
"
arsivle
dene "yapılandırma ÇALIŞTIRILMAZ (source yok)" '[[ ! -e $KOK/calisti && ! -e $KOK/calisti2 ]]'
conf_yaz
once=$(say "$CIKTI"/panely-hacim-*)
SAHTE_AGE_BOZ=1 arsivle; kod=$?
dene "şifreleme düşerse: çıkış≠0, yeni arşiv yok" '[[ $kod != 0 && $(say "$CIKTI"/panely-hacim-*) == $once ]]'
dene "şifreleme düşerse: yarım dosya kalmaz" '[[ $(say "$CIKTI"/.yaziliyor-*) == 0 ]]'
mkdir "$HACIM/Kotu_Ad"; ln -s "$HACIM/web" "$HACIM/bagli"; arsivle; kod=$?
dene "beklenmeyen girdi: çıkış≠0" '[[ $kod != 0 ]] && grep -q "beklenmeyen girdi" "$KOK/cikti"'
dene "beklenmeyen girdi: diğerleri yine arşivlendi" 'grep -q "özet: arşivlendi=2" "$KOK/cikti"'
rmdir "$HACIM/Kotu_Ad"; rm -f "$HACIM/bagli"
mv "$HACIM" "$HACIM.x"; arsivle; kod=$?
dene "hacim dizini yoksa: çıkış 0 ve açıkça yazar" '[[ $kod == 0 ]] && grep -q "hacim dizini yok" "$KOK/cikti"'
mv "$HACIM.x" "$HACIM"

echo "== Uzak yükleyici =="
rm -f "$CIKTI"/*; arsivle
echo db1 > "$YEDEK/panely-20260926T100000Z.db"; echo db22 > "$YEDEK/panely-20260926T110000Z.db"
: > "$CIKTI/.yaziliyor-web-20990101T000000Z"
yukle; kod=$?
dene "çıkış 0" '[[ $kod == 0 ]]'
dene "hacim arşivleri BAYT BAYT aynı (yeniden şifrelenmedi)" '( for f in "$CIKTI"/panely-hacim-*; do cmp -s "$f" "$UZAK/$(basename "$f")" || exit 1; done )'
dene "veritabanı yedekleri şifrelendi" '[[ $(head -1 "$UZAK/panely-20260926T100000Z.db.age") == age-encryption.org/v1 ]]'
dene "yarım arşiv (.yaziliyor-) yüklenmedi" '[[ ! -e $UZAK/.yaziliyor-web-20990101T000000Z ]]'
yukle
dene "ikinci koşu: yeniden yükleme yok" 'grep -q "yüklendi=0 atlandı=4 başarısız=0" "$KOK/cikti"'
bir="$(ls "$CIKTI"/panely-hacim-web-2026* | head -1)"; truncate -s 10 "$UZAK/$(basename "$bir")"
yukle
dene "kesik uzak arşiv yeniden yüklendi" 'grep -q "UZAK KOPYA BOZUK" "$KOK/cikti" && cmp -s "$bir" "$UZAK/$(basename "$bir")"'

echo "== Uzak budama =="
# KEEP=1 ama yerelde 2 hacim arşivi (web, web-2) ve 2 veritabanı yedeği
# var: yerelde OLAN hiçbir şey silinmemeli, yerelde olmayan eskilerin
# hepsi gitmeli. KEEP yerel sayıya eşit olsaydı "yereldekini silme" ile
# "en yeni N'i tut" aynı sonucu verirdi; test ikisini ayıramazdı (ilk
# hâli böyleydi, mutasyon kaçtı).
conf_yaz "OFFSITE_KEEP=1"
# web'in yerelde İKİ arşivi olsun (biri eski): tek arşiv hep en yeni
# olduğundan "en yeniyi tut" onu da korurdu ve kural sınanmazdı.
cp "$(ls "$CIKTI"/panely-hacim-web-2026*)" "$CIKTI/panely-hacim-web-20250601T000000Z.tar.zst.age"
for g in 01 02 03; do
    echo eski > "$UZAK/panely-hacim-web-201901${g}T000000Z.tar.zst.age"
    echo eski > "$UZAK/panely-201901${g}T000000Z.db.age"
done
yukle
dene "yerelde olmayan eski hacim arşivleri budandı" '[[ $(say "$UZAK"/panely-hacim-web-201901*) == 0 ]]'
dene "yerelde olan hacim arşivleri DURUYOR" '( for f in "$CIKTI"/panely-hacim-*; do [[ -e $UZAK/$(basename "$f") ]] || exit 1; done )'
dene "yerelde olmayan eski veritabanı yedekleri budandı" '[[ $(say "$UZAK"/panely-201901*.db.age) == 0 ]]'
dene "yerelde olan veritabanı yedekleri DURUYOR" '[[ -e $UZAK/panely-20260926T100000Z.db.age && -e $UZAK/panely-20260926T110000Z.db.age ]]'
# Uygulama başına ve DAMGAYA göre: aa'nın arşivleri YENİ, zz'ninkiler
# ESKİ, ikisi de yerelde yok. KEEP=1 → her uygulamanın en yenisi kalır.
# Tek grupta ADA göre budansaydı aa'nınkiler (alfabede önce) silinirdi;
# tek grupta ZAMANA göre budansaydı zz'nin hepsi silinirdi.
for a in aa-20260101 aa-20260102 zz-20190101 zz-20190102; do
    echo eski > "$UZAK/panely-hacim-${a}T000000Z.tar.zst.age"
done
yukle
dene "uygulama başına: aa'nın en yenisi kaldı" \
    '[[ -e $UZAK/panely-hacim-aa-20260102T000000Z.tar.zst.age && ! -e $UZAK/panely-hacim-aa-20260101T000000Z.tar.zst.age ]]'
dene "uygulama başına: zz'nin en yenisi kaldı" \
    '[[ -e $UZAK/panely-hacim-zz-20190102T000000Z.tar.zst.age && ! -e $UZAK/panely-hacim-zz-20190101T000000Z.tar.zst.age ]]'
conf_yaz

echo "== Uzak yükleyici: sınırlar =="
mv "$CIKTI" "$CIKTI.x"; yukle; kod=$?
dene "hacim dizini yoksa: veritabanı yedekleri yine gider" '[[ $kod == 0 ]] && grep -q "hacim yedeği kurulu değil" "$KOK/cikti"'
mv "$CIKTI.x" "$CIKTI"
mkdir "$KOK/bosyedek"; PANELY_BACKUP_DIR="$KOK/bosyedek" yukle; kod=$?
dene "veritabanı yedeği yoksa: yereldeki hacim arşivi bunu ÖRTMEZ" \
    '[[ $(say "$CIKTI"/panely-hacim-*) -gt 0 && $kod != 0 ]] && grep -q "BULUNAMADI" "$KOK/cikti"'

# Birim, arşivleyici ve yükleyici AYNI dizinde buluşmalı. Biri değişip
# diğeri kalırsa arşivler üretilir ama hiç yüklenmez — sessizce.
echo "== Üç dosya aynı dizini söylüyor =="
a="$(sed -n 's/^OUT_DIR="\${PANELY_VOLUME_BACKUP_DIR:-\(.*\)}"$/\1/p' deploy/offsite/panely-volume-backup.sh)"
y="$(sed -n 's/^VOLUME_BACKUP_DIR="\${PANELY_VOLUME_BACKUP_DIR:-\(.*\)}"$/\1/p' deploy/offsite/panely-offsite.sh)"
b="/var/lib/$(sed -n 's/^StateDirectory=//p' deploy/systemd/panely-volume-backup.service)"
: > "$KOK/cikti"
dene "arşivleyici=$a yükleyici=$y birim=$b" '[[ -n $a && $a == "$y" && $a == "$b" ]]'

echo
if (( fail )); then
    echo "BAŞARISIZ: hacim yedeği ya da uzak yükleyici beklenen davranışı göstermiyor."
    exit 1
fi
echo "Hacim yedeği ve uzak yükleyici doğru."
