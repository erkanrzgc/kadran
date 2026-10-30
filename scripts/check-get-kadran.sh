#!/usr/bin/env bash
# scripts/get-kadran.sh'ı sahte bir yayına karşı GERÇEKTEN çalıştırır (K-159).
#
# Bu betik `curl | sh` ile, root'suz, kullanıcının makinesinde koşuyor ve
# indirdiğini PATH'e koyuyor. Bozulması en pahalı yeri: özet tutmadığı hâlde
# kurmak, ya da yarım kalıp çalışan eski ikiliyi bozmak.
set -uo pipefail
cd "$(dirname "$0")/.."

fail=0
dene() {
    local ad="$1" rc
    shift
    # `if ( set -e; … )` OLMAZ: koşul içinde set -e etkisizdir ve yalnız son
    # satır sayılır. Betik henüz yokken dört denetim böyle yeşil çıktı.
    ( set -e; "$@" ) >/dev/null 2>&1
    rc=$?
    if [ "$rc" -eq 0 ]; then echo "  ✓ $ad"; else echo "  ✗ $ad"; fail=1; fi
}

T="$(mktemp -d)"
trap 'rm -rf "$T"' EXIT

B=kadran-v9.9.9-linux-amd64
ozet() { printf '%s' "$1" | sha256sum | cut -d' ' -f1; }

# yayin <kök> <ikili içeriği> <SHA256SUMS satırı>
yayin() {
    mkdir -p "$1/v9.9.9"
    printf '%s' "$2" > "$1/v9.9.9/$B"
    printf '%s\n' "$3" > "$1/v9.9.9/SHA256SUMS"
}
yayin "$T/iyi" binary-v9 "$(ozet binary-v9)  $B"
yayin "$T/yildiz" binary-v9 "$(ozet binary-v9) *$B"
yayin "$T/sahte" evil "$(ozet binary-v9)  $B"
yayin "$T/eksik" binary-v9 "$(ozet binary-v9)  kadran-v9.9.9-darwin-arm64"

# kur <yayın kökü> <kurulum dizini> [ek ortam…]: env'de sonraki değer kazanır.
kur() {
    env KADRAN_RELEASE_BASE="file://$1" KADRAN_VERSION=v9.9.9 KADRAN_OS=linux KADRAN_ARCH=amd64 \
        KADRAN_INSTALL_DIR="$2" "${@:3}" sh scripts/get-kadran.sh
}

# reddedilir <beklenen ileti> <kurulum dizini> <kur argümanları…>: çıkış ≠ 0,
# ileti doğru, ikili yok.
reddedilir() {
    local ileti="$1" d="$2" out
    shift 2
    if out="$("$@" 2>&1)"; then return 1; fi
    [[ "$out" == *"$ileti"* ]] && [ ! -e "$d/kadran" ]
}

echo "== Kurulum =="

c_kurar() {
    kur "$T/iyi" "$T/bin1"
    [ "$(cat "$T/bin1/kadran")" = binary-v9 ]
    [ "$(ls -l "$T/bin1/kadran" | cut -c1-10)" = -rwxr-xr-x ]
}
dene "kurar: içerik aynı, kip 755" c_kurar

c_yildiz() {
    kur "$T/yildiz" "$T/bin1y"
    [ "$(cat "$T/bin1y/kadran")" = binary-v9 ]
}
# Windows'ta derlenen yayınlar böyle (v0.9.2'nin dosyası): sha256sum ikili kipte.
dene "SHA256SUMS'taki *ad biçimini okur" c_yildiz

c_ustune() {
    mkdir -p "$T/bin1e"
    printf old > "$T/bin1e/kadran"
    kur "$T/iyi" "$T/bin1e"
    [ "$(cat "$T/bin1e/kadran")" = binary-v9 ]
    [ -z "$(ls -A "$T/bin1e" | grep -F .kadran.new || true)" ]
}
dene "eski sürümün üstüne kurar, geçici dosya kalmaz" c_ustune

c_path() {
    local out
    out="$(kur "$T/iyi" "$T/bin1p" PATH="$PATH")"
    [[ "$out" == *"installed kadran v9.9.9 to $T/bin1p/kadran"* ]]
    [[ "$out" == *"add $T/bin1p to your PATH"* ]]
    [[ "$out" == *"export PATH=\"$T/bin1p:\$PATH\""* ]]
    out="$(kur "$T/iyi" "$T/bin1p" PATH="$T/bin1p:$PATH")"
    [[ "$out" != *"to your PATH"* ]]
}
dene "dizin PATH'te değilse söyler, varsa susar" c_path

c_calisan() {
    # Gerçek bir ELF şart: çalışan ikilinin üstüne yazmak ETXTBSY verir, ama
    # kabuk betiğinin üstüne yazılabilir ve denetim hiçbir şey ölçmezdi.
    # `sleep` değil bash: CI'da (Ubuntu 26.04) kopyalanan `sleep`le mutant sağ
    # çıktı; `bash -c 'sleep …'` de kendini sleep'e çevirir, o yüzden `read`.
    mkdir -p "$T/bin1c"
    cp "$BASH" "$T/bin1c/kadran"
    mkfifo "$T/bekle"
    "$T/bin1c/kadran" -c 'exec 3<>"$1"; read -r -t 30 _ <&3' _ "$T/bekle" &
    local p=$! rc=0 i=0 st=0
    # ETXTBSY alana kadar exec bitmemiştir, üstüne yazmak serbesttir. `>>`
    # içeriğe dokunmaz.
    while ( : >>"$T/bin1c/kadran" ) 2>/dev/null; do
        i=$((i + 1))
        if [ "$i" -ge 50 ]; then
            kill "$p" 2>/dev/null || true
            return 1
        fi
        sleep 0.1
    done
    kur "$T/iyi" "$T/bin1c" || rc=$?
    kill "$p" 2>/dev/null || true
    wait "$p" 2>/dev/null || st=$?
    # 143: kurulum boyunca yaşıyordu, onu biz öldürdük. Kapanan süreç de bir an
    # ETXTBSY veriyor, yukarıdaki döngü bunu tek başına kanıtlamaz.
    [ "$st" -eq 143 ]
    [ "$rc" -eq 0 ]
    [ "$(cat "$T/bin1c/kadran")" = binary-v9 ]
}
dene "çalışan eski ikilinin yerine kurar (geçici ad + yeniden adlandırma)" c_calisan

echo "== Özet denetimi =="

dene "değiştirilmiş ikili kurulmaz" \
    reddedilir "checksum mismatch for $B; nothing installed" "$T/bin2" kur "$T/sahte" "$T/bin2"
dene "SHA256SUMS'ta adı olmayan ikili kurulmaz" \
    reddedilir "checksum mismatch for $B; nothing installed" "$T/bin3" kur "$T/eksik" "$T/bin3"

c_atomik() {
    mkdir -p "$T/bin4"
    printf old > "$T/bin4/kadran"
    if kur "$T/sahte" "$T/bin4"; then return 1; fi
    [ "$(cat "$T/bin4/kadran")" = old ]
    [ -z "$(ls -A "$T/bin4" | grep -F .kadran.new || true)" ]
}
dene "reddedilince eski ikili yerinde, geçici dosya kalmaz" c_atomik

c_kontrol() {
    cp "$T/sahte/v9.9.9/$B" "$T/kontrol"
    [ "$(cat "$T/kontrol")" = evil ]
    [ "$(sha256sum "$T/kontrol" | cut -d' ' -f1)" != "$(cut -d' ' -f1 "$T/sahte/v9.9.9/SHA256SUMS")" ]
}
# Kontrol grubu: denetimsiz bir kopya sahte ikiliyi kurardı ve özeti gerçekten
# tutmuyor. Yoksa yukarıdaki ölçüm bir şey kanıtlamıyor olurdu.
dene "KONTROL: denetimsiz kopya sahte ikiliyi kurardı" c_kontrol

echo "== Platform ve sürüm =="

dene "desteklenmeyen mimari" \
    reddedilir "unsupported platform: linux/mips" "$T/bin5" kur "$T/iyi" "$T/bin5" KADRAN_ARCH=mips
dene "desteklenmeyen işletim sistemi" \
    reddedilir "unsupported platform: freebsd/amd64" "$T/bin5" kur "$T/iyi" "$T/bin5" KADRAN_OS=freebsd

# son_surum <API yanıtı> <kurulum dizini>: KADRAN_VERSION boş.
son_surum() {
    printf '%s' "$1" > "$T/latest.json"
    kur "$T/iyi" "$2" KADRAN_VERSION= KADRAN_RELEASE_API="file://$T/latest.json"
}
c_son_duzgun() {
    son_surum "$(printf '{\n  "url": "x",\n  "tag_name": "v9.9.9",\n  "name": "Kadran v9.9.9"\n}\n')" "$T/bin6"
    [ "$(cat "$T/bin6/kadran")" = binary-v9 ]
}
dene "son sürümü API'den okur (girintili yanıt)" c_son_duzgun
c_son_sikisik() {
    son_surum '{"url":"x","tag_name":"v9.9.9","name":"Kadran v9.9.9"}' "$T/bin7"
    [ "$(cat "$T/bin7/kadran")" = binary-v9 ]
}
dene "son sürümü API'den okur (tek satır yanıt)" c_son_sikisik
dene "API'den tuhaf etiket gelirse kurmaz" \
    reddedilir "could not determine the latest version" "$T/bin8" son_surum '{"tag_name":"v1;rm -rf x"}' "$T/bin8"
dene "API'de etiket yoksa kurmaz" \
    reddedilir "could not determine the latest version" "$T/bin9" son_surum '{"message":"rate limited"}' "$T/bin9"
dene "elle verilen tuhaf sürümü kabul etmez" \
    reddedilir "invalid KADRAN_VERSION" "$T/bin10" kur "$T/iyi" "$T/bin10" KADRAN_VERSION='v1/../../x'

echo "== İndirme aracı =="

# PATH'i daraltmak: curl'ü gerçekten yok etmenin tek yolu. Yalnız betiğin
# kullandığı araçlar.
ARAC="$T/arac"
mkdir -p "$ARAC"
for a in sh cat cut awk sed tr head mkdir mktemp cp chmod mv rm uname sha256sum; do
    ln -s "$(command -v "$a")" "$ARAC/$a"
done

c_wget() {
    # wget file:// okumuyor; sahtesi adresi diske çevirip -qO sözleşmesini sınar.
    cat > "$ARAC/wget" <<'EOF'
#!/bin/sh
[ "$1" = -qO ] || exit 2
exec cp "${3#file://}" "$2"
EOF
    chmod +x "$ARAC/wget"
    kur "$T/iyi" "$T/bin11" PATH="$ARAC"
    [ "$(cat "$T/bin11/kadran")" = binary-v9 ]
    rm -f "$ARAC/wget"
}
dene "curl yoksa wget kullanır" c_wget
dene "ikisi de yoksa söyler" \
    reddedilir "need curl or wget" "$T/bin12" kur "$T/iyi" "$T/bin12" PATH="$ARAC"

echo
if [ "$fail" -ne 0 ]; then
    echo "get-kadran.sh denetimi KIRMIZI."
    exit 1
fi
echo "get-kadran.sh denetimi yeşil."
