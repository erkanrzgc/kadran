#!/usr/bin/env bash
# Mutasyon parçalayıcısını (K-164, scripts/mutation-shard.sh) koruyan
# denetimin (scripts/check-mutation-shard.sh) GERÇEKTEN bir şey koruduğunu
# sınar.
#
# ── Neyin bozulması EN PAHALI ───────────────────────────────────────
#
# Hiçbir parçaya girmeyen betik koşmaz ve iş yeşil kalır. CI'daki birleşim
# denetimi bunu koşarken de yakalar; bu betik lint'teki denetimi ölçer.
# İkincisi denge: her betik koşar ama tam koşu yine en ağır parça kadar.
#
# Düzenek mutate-mutation-select.sh'tan: tek eşleşme şartı (K-127), yeşil
# taban şartı, derleme kapısı (K-096; kabuk için `bash -n`).
set -uo pipefail

cd "$(dirname "$0")/.."
G=scripts/mutation-shard.sh

BAKDIR=$(mktemp -d)
cp "$G" "$BAKDIR/mutation-shard.sh"
restore() { cp "$BAKDIR/mutation-shard.sh" "$G"; }
trap restore EXIT

fail=0

olc() {
    bash scripts/check-mutation-shard.sh >/dev/null 2>&1
}

# mutate <ad> <python-ifadesi>
mutate() {
    local name="$1" expr="$2"
    restore
    if ! python -c "
import io,sys
class _S(str):
    def replace(self,a,b,*r):
        # TEK eşleşme şart: aranan metin bir yorumda da geçiyorsa
        # replace(…,1) İLKİNİ, yani yorumu değiştirir (K-127).
        n=self.count(a)
        if n!=1:
            sys.stderr.write('REPLACE '+str(n)+' KEZ ESLESTI (1 olmali): '+repr(a[:70])+chr(10))
            sys.exit(8)
        return _S(str.replace(self,a,b,*r))
p='$G'
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

    # ── MUTANT DERLENMELİ (K-096) ───────────────────────
    # Sözdizimi bozuk bir mutant denetimi düşürür, sahte "yakalandı" olur.
    if ! bash -n "$G"; then
        echo "  !! MUTANT DERLENMİYOR: $name — ölçüm YAPILMADI"
        fail=1
        return
    fi

    if olc; then
        echo "  KIRMIZI OLMADI: $name"
        fail=1
    else
        echo "  yakalandı: $name"
    fi
}

# Taban YEŞİL olmalı: kırmızı bir taban her mutantı "yakalandı" gösterirdi.
if ! olc; then
    echo "!! TABAN KIRMIZI: mutasyonsuz parçalayıcıda denetim düşüyor — ölçüm YAPILMADI"
    exit 1
fi

echo "== Betik düşmesi =="

mutate "son parça basılmıyor" \
    "s=s.replace('p <= n; p++) print','p < n; p++) print',1)"
mutate "ölçülmemiş betik düşüyor" \
    "s=s.replace('{ print ((\$0 in w) ? w[\$0] : v), \$0 }','{ if (\$0 in w) print w[\$0], \$0 }',1)"
mutate "boşlukla ayrılmış seçim bölünmüyor" \
    "s=s.replace(\"tr -s ' \\\\t' '\\\\n'\",'cat',1)"

echo "== Boş ya da tekrarlı parça =="

mutate "parça sayısı betik sayısına inmiyor" \
    "s=s.replace('n=\"\$k\"','n=\"\$n\"',1)"
mutate "aynı betik iki kez koşuyor" \
    "s=s.replace('sort -u','sort',1)"
mutate "geçersiz parça sayısı kabul ediliyor" \
    "s=s.replace('if ! [[ \"\$n\" =~ ^[1-9][0-9]*\$ ]]; then','if false; then',1)"

echo "== Denge =="

mutate "en ağır parçaya ekleniyor" \
    "s=s.replace('yuk[p] < yuk[en]','yuk[p] > yuk[en]',1)"
mutate "hafif betik önce" \
    "s=s.replace('-k1,1nr','-k1,1n',1)"
mutate "ölçülmemiş betik süresiz" \
    "s=s.replace('VARSAYILAN=60','VARSAYILAN=0',1)"

echo
if [ "$fail" -ne 0 ]; then
    echo "En az bir mutasyon yakalanmadı — denetim iddia ettiği şeyi korumuyor."
    exit 1
fi
echo "Bütün mutasyonlar yakalandı."
