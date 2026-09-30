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
PIPE=internal/client/pipeconn.go
PKG=./internal/client/

BAK=$(mktemp); BAK_PIPE=$(mktemp)
cp "$CLI" "$BAK"; cp "$PIPE" "$BAK_PIPE"
restore() { cp "$BAK" "$CLI"; cp "$BAK_PIPE" "$PIPE"; }
trap restore EXIT

fail=0

# mutate <ad> <python-ifadesi>; HEDEF=<dosya> ile client.go dışında bir
# dosya bozulur.
mutate() {
    local name="$1" expr="$2" hedef="${HEDEF:-$CLI}"
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
p='$hedef'
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

    if go test "$PKG" -run 'OptionLike|CheckProtocol|SinglePositional|SSHFailure|CleanSSHExit' -count=1 >/dev/null 2>&1; then
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

echo "== ssh'ın hata mesajı kullanıcıya ulaşıyor (K-120) =="

mutate "okuyucuya ssh'ın sebebi bağlanmıyor" \
    "s=s.replace('\tpc.onEOF = sebep\n','',1)"

HEDEF=$PIPE mutate "okuyucu sebebi yok sayıyor" \
    "s=s.replace('\tif errors.Is(err, io.EOF) && c.onEOF != nil {','\tif errors.Is(err, io.EOF) && c.onEOF != nil && n < 0 {',1)"

# stderr'i tutan bir alt süreç Wait'i 20 sn kilitliyordu (ölçüldü).
mutate "Wait'in gecikme sınırı yok" \
    "s=s.replace('\tcmd.WaitDelay = sshExitGrace\n','',1)"

# Kontrol grubunun koruduğu yön: temiz çıkış bir hata gibi görünmemeli.
mutate "temiz çıkış da hata sayılıyor" \
    "s=s.replace('\t\terr := wait()\n\t\tif err == nil {\n\t\t\treturn nil\n\t\t}\n','\t\terr := wait()\n\t\tif err == nil {\n\t\t\terr = errors.New(\"temiz\")\n\t\t}\n',1)"

restore
if [[ $fail -ne 0 ]]; then
    echo
    echo "En az bir mutasyon yakalanmadı — testler iddia ettikleri şeyi korumuyor."
    exit 1
fi

echo
echo "Bütün mutasyonlar yakalandı."
