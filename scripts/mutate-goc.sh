#!/usr/bin/env bash
# Göçü (K-136, internal/bootstrap/goc.sh) koruyan denetimlerin GERÇEKTEN
# bir şey koruduğunu sınar.
#
# ── Neyin bozulması EN PAHALI ───────────────────────────────────────
#
# Veri: iki tarafı da dolu bir dizinin üstüne yazmak, bir yedeğin adını
# başka bir yedeğin üstüne çevirmek. Erişim: authorized_keys'te yolun
# yarım çevrilmesi, rclone hedefinin bir dosyada değişip ötekinde
# değişmemesi. Paket: göçün okuduğu bir dosyanın pakette olmaması (hata
# ancak ilk canlı göçün ortasında çıkardı).
#
# Düzenek mutate-keys.sh'tan: tek eşleşme şartı (K-127), yeşil taban
# şartı, derleme kapısı (K-096; kabuk için `bash -n`), tam yoldan yedek.
set -uo pipefail

cd "$(dirname "$0")/.."
GOC=internal/bootstrap/goc.sh
BOOT=internal/bootstrap/bootstrap.go
FILES=("$GOC" "$BOOT")

BAKDIR=$(mktemp -d)
bak() { printf '%s/%s' "$BAKDIR" "${1//\//__}"; }
for f in "${FILES[@]}"; do cp "$f" "$(bak "$f")"; done
restore() { for f in "${FILES[@]}"; do cp "$(bak "$f")" "$f"; done; }
trap restore EXIT

fail=0

olc() {
    bash scripts/check-goc-sh.sh >/dev/null 2>&1 &&
        go test ./internal/bootstrap/ -run 'TestArchive' -count=1 -timeout 120s >/dev/null 2>&1
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
    # Sözdizimi bozuk bir kabuk mutantı her denetimi düşürür ve sahte
    # "yakalandı" üretirdi; Go mutantı için aynı şey derleme.
    local build_out
    if ! build_out=$(bash -n "$GOC" 2>&1 && go test ./internal/bootstrap/ -run '^$' -count=1 2>&1); then
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
    echo "!! TABAN KIRMIZI: mutasyonsuz kodda denetimler düşüyor — ölçüm YAPILMADI"
    exit 1
fi

echo "== Veri =="

mutate_in "$GOC" "goc_tasi dolu hedefin üstüne yazıyor" \
    "s=s.replace('            die \"göç: hem \$eski hem \$yeni var ve \$yeni boş değil.','            rm -rf \"\$yeni\"; die \"x',1)"

mutate_in "$GOC" "goc_onek var olan hedefin üstüne taşıyor" \
    "s=s.replace('        [ -e \"\$hedef\" ] && die','        false && die',1)"

mutate_in "$GOC" "goc_gerekli bitmiş göçü yeniden başlatıyor" \
    "s=s.replace(' && [ ! -e \"\$GOC_DIR/tamam\" ]','',1)"

echo "== Erişim =="

mutate_in "$GOC" "goc_ak satır başına çapalanmıyor" \
    "s=s.replace('\"s#^\$ESKI_CONNECT#','\"s#\$ESKI_CONNECT#',1)"

mutate_in "$GOC" "goc_uzak_yedek offsite.conf'u çevirmiyor" \
    "s=s.replace('    goc_yerinde_sed \"\$etc/offsite.conf\"','    : goc_yerinde_sed \"\$etc/offsite.conf\"',1)"

mutate_in "$GOC" "goc_uzak_yedek iki hedef birden varken sürüyor" \
    "s=s.replace('        die \"göç: rclone.conf\\'ta hem','        : \"x',1)"

echo "== Paket =="

mutate_in "$BOOT" "göçün okuduğu bir birim pakette yok" \
    "s=s.replace('\"kadran-offsite.timer\":','\"kadran-offsite.timer-YOK\":',1)"

mutate_in "$BOOT" "goc.sh pakete girmiyor" \
    "s=s.replace('range []string{\"install.sh\", \"goc.sh\"}','range []string{\"install.sh\"}',1)"

echo
if [[ "$fail" -ne 0 ]]; then
    echo "En az bir mutasyon yakalanmadı — denetimler iddia ettikleri şeyi korumuyor."
    exit 1
fi
echo "Bütün mutasyonlar yakalandı."
