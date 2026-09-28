#!/usr/bin/env bash
# İstemcinin (CLI) iki korumasını sınayan testlerin GERÇEKTEN bir şey
# koruduğunu ölçer (K-114).
#
# ── Neyin bozulması EN PAHALI ───────────────────────────────────────
#
# 1. Argüman enjeksiyonu: `-` ile başlayan kullanıcı ya da sunucu adını
#    ssh SEÇENEK sanar (`-oProxyCommand=…` iş istasyonunda komut
#    çalıştırır). Testler vardı, hiçbir mutasyon onları ölçmemişti.
# 2. Protokol uyumu: uyumsuz sözleşmeyle konuşmak sessizce yanlış
#    davranmak demek. Uçtan uca test yalnızca "sürümler aynı" yolunu
#    çalıştırıyordu.
#
# ── K-071/K-080'in dersi ────────────────────────────────────────────
#
# Yeşil kalan bir mutasyon İKİ zıt sonuç doğurabilir: test zayıftır ya da
# MUTASYON zayıftır. Bu yüzden betik, mutasyonun dosyaya uygulanıp
# uygulanmadığını da ayrıca doğruluyor.
set -uo pipefail

cd "$(dirname "$0")/.."
CLI=internal/client/client.go
PKG=./internal/client/

BAK=$(mktemp)
cp "$CLI" "$BAK"
restore() { cp "$BAK" "$CLI"; }
trap restore EXIT

fail=0

# mutate <ad> <python-ifadesi>
mutate() {
    local name="$1" expr="$2"
    restore
    if ! python -c "
import io,sys
class _S(str):
    def replace(self,a,b,*r):
        out=str.replace(self,a,b,*r)
        if out==self:
            sys.stderr.write('REPLACE ESLESMEDI: '+repr(a[:70])+chr(10))
            sys.exit(8)
        return _S(out)
p='$CLI'
s=_S(io.open(p,encoding='utf-8').read())
o=s
$expr
if s==o:
    sys.exit(9)
io.open(p,'w',encoding='utf-8',newline='\n').write(s)
"; then
        echo "  !! MUTASYON UYGULANAMADI: $name — betik bozuk, ölçüm YAPILMADI"
        fail=1
        return
    fi

    # ── MUTANT DERLENMELİ ───────────────────────────────
    #
    # Derlenmeyen bir mutant `go test`'i düşürür ve betik bunu
    # "yakalandı" diye okur (K-096).
    local build_out
    if ! build_out=$(go test "$PKG" -run '^$' -count=1 2>&1); then
        echo "  !! MUTANT DERLENMİYOR: $name — ölçüm YAPILMADI,"
        echo "     mutasyon derlenebilir olacak şekilde yazılmalı. Derleyici:"
        echo "$build_out" | grep -vE '^(#|FAIL|ok)' | head -3 | sed 's/^/       /'
        fail=1
        return
    fi

    if go test "$PKG" -run 'OptionLike|CheckProtocol|SinglePositional' -count=1 >/dev/null 2>&1; then
        echo "  KIRMIZI OLMADI: $name"
        fail=1
    else
        echo "  yakalandı: $name"
    fi
}

echo "== Argüman enjeksiyonu =="

mutate "- ile başlayan kullanıcı adı kabul ediliyor" \
    "s=s.replace('\tif strings.HasPrefix(user, \"-\") {','\tif false {',1)"

mutate "- ile başlayan sunucu adı kabul ediliyor" \
    "s=s.replace('\tif strings.HasPrefix(host, \"-\") {','\tif false {',1)"

echo "== Protokol uyumu =="

mutate "uyumsuz protokol kabul ediliyor" \
    "s=s.replace('\tif resp.GetProtocolVersion() != version.Protocol {','\tif false {',1)"

mutate "istemci sürümünü göndermiyor" \
    "s=s.replace('&panelyv1.PingRequest{ClientVersion: version.Version}','&panelyv1.PingRequest{}',1)"

restore
if [[ $fail -ne 0 ]]; then
    echo
    echo "En az bir mutasyon yakalanmadı — testler iddia ettikleri şeyi korumuyor."
    exit 1
fi

echo
echo "Bütün mutasyonlar yakalandı."
