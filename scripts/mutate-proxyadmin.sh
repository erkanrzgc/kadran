#!/usr/bin/env bash
# Ters vekil admin istemcisinin testlerinin GERÇEKTEN bir şey koruduğunu
# sınar (K-054, K-112).
#
# ── Neyin bozulması EN PAHALI ───────────────────────────────────────
#
# Geri okumanın atlanması. `POST /load`'ın 200 dönmesi canlının bizim
# yapılandırmamız olduğunu kanıtlamaz; admin soketine başkası da
# yazabilir. Geri okuma sessizce kaybolursa "yüklendi" diyen her satır
# gerçeğin kaynağının (SQLite) canlıyla aynı olduğunu İDDİA eder.
#
# ⚠ Testler Windows'ta ATLANIYOR (unix soketine bağlanma orada çalışmıyor,
# ölçüldü). Bu betik yalnızca Linux'ta anlamlı; CI'ın mutasyon işi
# Linux'ta koşuyor. Windows'ta her mutant "KIRMIZI OLMADI" çıkar — bu,
# betiğin değil platformun sonucu; bu yüzden orada çalışmayı reddediyor.
set -uo pipefail

if [[ "$(uname -s)" != Linux ]]; then
    echo "Bu betik yalnızca Linux'ta ölçer (testler başka platformda atlanıyor)." >&2
    exit 2
fi

cd "$(dirname "$0")/.."
CLI=internal/proxydrv/client.go
PKG=./internal/proxydrv/

BAK=$(mktemp)
cp "$CLI" "$BAK"
restore() { cp "$BAK" "$CLI"; }
trap restore EXIT

fail=0

# mutate <ad> <python-ifadesi>
mutate() {
    local name="$1" expr="$2"
    restore
    if ! python3 -c "
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

    if go test "$PKG" -count=1 >/dev/null 2>&1; then
        echo "  KIRMIZI OLMADI: $name"
        fail=1
    else
        echo "  yakalandı: $name"
    fi
}

echo "== Geri okuma (K-054) =="

# EN PAHALI. `_ = live` değişkeni kullanılır tutuyor; yoksa mutant derlenmez.
mutate "yüklemeden sonra doğrulama yapılmıyor" \
    "s=s.replace('\tif err := verifyApplied(cfg, live); err != nil {','\tif err := func() error { _ = live; return nil }(); err != nil {',1)"

mutate "gönderilmeyen rota görülmüyor (tek yönlü)" \
    "s=s.replace('\tif len(extra) > 0 {','\tif false && len(extra) > 0 {',1)"

mutate "upstream karşılaştırması hep eşit" \
    "s=s.replace('func sameStrings(a, b []string) bool {\n','func sameStrings(a, b []string) bool {\n\treturn true\n',1)"

echo "== Admin yanıtları =="

mutate "200 dışı yanıt başarı sayılıyor" \
    "s=s.replace('\tif resp.StatusCode != http.StatusOK {','\tif false {',1)"

mutate "Caddy'nin mesajı ayıklanmıyor (ham JSON)" \
    "s=s.replace('\tif json.Unmarshal(body, &e) == nil && e.Error != \"\" {','\tif false && json.Unmarshal(body, &e) == nil && e.Error != \"\" {',1)"

mutate "boş yanıt hata sayılıyor" \
    "s=s.replace('\tif len(bytes.TrimSpace(raw)) == 0 || bytes.Equal(','\tif bytes.Equal(',1)"

restore
if [[ $fail -ne 0 ]]; then
    echo
    echo "En az bir mutasyon yakalanmadı — testler iddia ettikleri şeyi korumuyor."
    exit 1
fi

echo
echo "Bütün mutasyonlar yakalandı."
