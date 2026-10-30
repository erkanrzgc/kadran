#!/usr/bin/env bash
# Sürümden kurulumu (K-159, internal/bootstrap/release.go) koruyan testlerin
# GERÇEKTEN bir şey koruduğunu sınar.
#
# ── Neyin bozulması EN PAHALI ───────────────────────────────────────
#
# Özet denetimi: bootstrap indirdiği paketi root olarak kurar; denetim
# boşa düşerse yayını değiştirebilen herkes sunucuda root olur. Açma: özet
# tutsa bile paketin dışına yazan ya da bağ taşıyan girdi. Seçim: -binaries
# verildiği hâlde yayından kurmak, kullanıcının derlediği ikiliyi sessizce
# yok sayar.
#
# Düzenek mutate-goc.sh'tan: tek eşleşme şartı (K-127), yeşil taban şartı,
# derleme kapısı (K-096), tam yoldan yedek.
set -uo pipefail

cd "$(dirname "$0")/.."
REL=internal/bootstrap/release.go
MAIN=cmd/kadran/main.go
FILES=("$REL" "$MAIN")

BAKDIR=$(mktemp -d)
bak() { printf '%s/%s' "$BAKDIR" "${1//\//__}"; }
for f in "${FILES[@]}"; do cp "$f" "$(bak "$f")"; done
restore() { for f in "${FILES[@]}"; do cp "$(bak "$f")" "$f"; done; }
trap restore EXIT

fail=0

olc() {
    go test -count=1 -timeout 120s ./internal/bootstrap >/dev/null 2>&1 &&
        go test -count=1 -timeout 120s ./cmd/kadran -run TestUseRelease >/dev/null 2>&1
}

# mutate_in <dosya> <ad> <python-ifadesi>
mutate_in() {
    local file="$1" name="$2" expr="$3"
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
p='$file'
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
    # Derlenmeyen bir mutant her testi düşürür ve sahte "yakalandı" üretirdi.
    local build_out
    if ! build_out=$(go vet ./internal/bootstrap ./cmd/kadran 2>&1); then
        echo "  !! MUTANT DERLENMİYOR: $name — ölçüm YAPILMADI. Çıktı:"
        echo "$build_out" | grep -vE '^(#|FAIL|ok)' | head -3 | sed 's/^/       /'
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
    echo "!! TABAN KIRMIZI: mutasyonsuz kodda testler düşüyor — ölçüm YAPILMADI"
    exit 1
fi

echo "== Özet denetimi =="

mutate_in "$REL" "özet karşılaştırması hep geçiyor" \
    "s=s.replace('subtle.ConstantTimeCompare([]byte(got), []byte(want)) != 1 {','subtle.ConstantTimeCompare([]byte(got), []byte(want)) < -1 {',1)"

mutate_in "$REL" "özetin yalnız ilk 12 hanesi karşılaştırılıyor" \
    "s=s.replace('subtle.ConstantTimeCompare([]byte(got), []byte(want))','subtle.ConstantTimeCompare([]byte(got[:12]), []byte(want[:12]))',1)"

mutate_in "$REL" "paket özetten önce açılıyor" \
    "s=s.replace('\n\troot := filepath.Join(tmp, \"bundle\")\n\tif err = unpackBundle(tarball, root); err != nil {\n\t\treturn \"\", nil, err\n\t}\n','\n',1); s=s.replace('\tif subtle.ConstantTimeCompare','\troot := filepath.Join(tmp, \"bundle\")\n\tif err = unpackBundle(tarball, root); err != nil {\n\t\treturn \"\", nil, err\n\t}\n\tif subtle.ConstantTimeCompare',1)"

mutate_in "$REL" "özeti olmayan istemci yine de indiriyor" \
    "s=s.replace('if !isSHA256Hex(want) {','if false {',1)"

echo "== Paketi açmak =="

mutate_in "$REL" "sembolik bağ kabul ediliyor" \
    "s=s.replace('\t\tcase tar.TypeDir:\n','\t\tcase tar.TypeSymlink:\n\t\t\tcontinue\n\t\tcase tar.TypeDir:\n',1)"

mutate_in "$REL" "ad içindeki .. denetlenmiyor" \
    "s=s.replace('if seg == \"..\" {','if seg == \"..\" && false {',1)"

mutate_in "$REL" "mutlak ad ve ters bölü denetlenmiyor" \
    "s=s.replace('if name == \"\" || strings.HasPrefix(name, \"/\") || ','if false && name == \"\" && ',1)"

mutate_in "$REL" "izinli kök denetimi yok" \
    "s=s.replace('\treturn bad()\n}\n','\treturn clean, nil\n}\n',1)"

echo "== Kurulum kaynağının seçimi =="

mutate_in "$MAIN" "useRelease -binaries bayrağını yok sayıyor" \
    "s=s.replace('!explicit[\"binaries\"] && ','',1)"

echo
if [[ "$fail" -ne 0 ]]; then
    echo "En az bir mutasyon yakalanmadı — testler iddia ettikleri şeyi korumuyor."
    exit 1
fi
echo "Bütün mutasyonlar yakalandı."
