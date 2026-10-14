#!/usr/bin/env bash
# Uygulama ağı alt ağ denetiminin (K-150) testlerinin GERÇEKTEN bir şey
# koruduğunu sınar.
#
# Docker'ın varsayılan havuzu 172.x tükenince 192.168'e geçer. O ağdaki
# uygulamaya kadrand yoklama yapamaz, vekil aracısı rotasını reddeder. Sürücü
# ağı oluşturduktan sonra alt ağını okuyup bu durumda dağıtımı durduruyor;
# kendi oluşturduğu ağı siliyor, önceden var olana dokunmuyor. Her mutasyon bu
# davranışlardan birini KASTEN bozar.
set -uo pipefail

cd "$(dirname "$0")/.."
SRC=internal/dockerdrv/network.go
BAK=$(mktemp)
cp "$SRC" "$BAK"
restore() { cp "$BAK" "$SRC"; }
trap restore EXIT

fail=0
WANT='TestNetworkEnsure'

# ── Taban YEŞİL olmalı ───────────────────────────────────────────────
#
# Mutasyonsuz kodda düşen bir test her mutantı "yakalandı" gösterirdi:
# SESSİZ sahte geçiş. Her farklı test komutu, ilk mutantından önce bir kez
# mutasyonsuz kodda koşturuluyor.
declare -A TABAN=()
taban_yesil() {
    local key="$*"
    [[ -n "${TABAN[$key]:-}" ]] && return 0
    restore
    if ! go test "$@" >/dev/null 2>&1; then
        echo "!! TABAN KIRMIZI: mutasyonsuz kodda 'go test $*' düşüyor — ölçüm YAPILMADI"
        exit 1
    fi
    TABAN[$key]=1
}

# mutate <ad> <python-ifadesi>
mutate() {
    local name="$1" expr="$2"
    restore
    taban_yesil ./internal/dockerdrv/ -run "$WANT" -count=1
    if ! python -c "
import io,sys
class _S(str):
    def replace(self,a,b,*r):
        # TEK eşleşme şart: aranan metin bir yorumda da geçiyorsa
        # replace(…,1) İLKİNİ, yani yorumu değiştirir ve kod hiç mutasyona
        # uğramadan ölçülür (K-127).
        n=self.count(a)
        if n!=1:
            sys.stderr.write('REPLACE '+str(n)+' KEZ ESLESTI (1 olmali): '+repr(a[:70])+chr(10))
            sys.exit(8)
        return _S(str.replace(self,a,b,*r))
p='$SRC'
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
    # "yakalandı" diye okur — yani testin iddiası hiç sınanmadan YEŞİL
    # rapor üretilir. K-092'de `mutate-alarm.sh`'ın EN ÖNEMLİ dört
    # mutasyonu tam olarak böyle sahte çıktı; K-096 kapıyı bütün
    # betiklere yaydı.
    #
    # `-run '^$'` seçildi çünkü paketi VE test dosyalarını derler ama
    # hiçbir test koşmaz. `go build` yalnızca üretim kodunu derlerdi;
    # test kodunun derlenmesini bozan bir mutasyon yine sahte
    # "yakalandı" verirdi.
    local build_out
    if ! build_out=$(go test ./internal/dockerdrv/ -run '^$' -count=1 2>&1); then
        echo "  !! MUTANT DERLENMİYOR: $name — ölçüm YAPILMADI,"
        echo "     mutasyon derlenebilir olacak şekilde yazılmalı. Derleyici:"
        echo "$build_out" | grep -vE '^(#|FAIL|ok)' | head -3 | sed 's/^/       /'
        fail=1
        return
    fi

    if go test ./internal/dockerdrv/ -run "$WANT" -count=1 >/dev/null 2>&1; then
        echo "  KIRMIZI OLMADI: $name"
        fail=1
    else
        echo "  yakalandı: $name"
    fi
}

echo "== Uygulama ağı alt ağ mutasyonları =="

mutate "alt ağ hiç denetlenmiyor" \
    "s=s.replace('\tif err := c.checkNetworkSubnet(ctx, name); err != nil {','\tif err := error(nil); err != nil {',1)"

mutate "yeni oluşturulan ağ silinmiyor" \
    "s=s.replace('\t\tif created {','\t\tif created && false {',1)"

mutate "önceden var olan ağ da siliniyor" \
    "s=s.replace('\t\tif created {','\t\tif created || true {',1)"

mutate "havuzdan geniş alt ağ kabul" \
    "s=s.replace('if err != nil || p.Bits() < appSubnetPool.Bits() || !appSubnetPool.Contains(p.Addr()) {','if err != nil || !appSubnetPool.Contains(p.Addr()) {',1)"

mutate "havuz /11'e genişledi" \
    "s=s.replace('netip.MustParsePrefix(\"172.16.0.0/12\")','netip.MustParsePrefix(\"172.0.0.0/11\")',1)"

mutate "IPv4 alt ağı olmayan ağ kabul" \
    "s=s.replace('\tif v4 == 0 {','\tif v4 < 0 {',1)"

mutate "ayrıştırılamayan alt ağ atlanıyor" \
    "s=s.replace('\t\tif err == nil && p.Addr().Is6() {','\t\tif err != nil || p.Addr().Is6() {',1)"

mutate "409 dışındaki oluşturma hatası yutuluyor" \
    "s=s.replace('if !errors.As(err, &apiErr) || apiErr.Status != http.StatusConflict {','if !errors.As(err, &apiErr) {',1)"

restore
echo
if [ "$fail" -ne 0 ]; then
    echo "BAŞARISIZ: en az bir mutasyon yakalanmadı ya da uygulanamadı."
    exit 1
fi
echo "Bütün mutasyonlar yakalandı."
