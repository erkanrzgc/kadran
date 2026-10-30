#!/usr/bin/env bash
# Mutasyon seçicisini (K-160, scripts/mutation-select.sh) koruyan denetimin
# GERÇEKTEN bir şey koruduğunu sınar.
#
# ── Neyin bozulması EN PAHALI ───────────────────────────────────────
#
# Seçici az seçerse PR'da mutasyon koşmaz ve iş yeşil kalır: bir hata ancak
# gecelik koşuda görülür. Fazla seçmek yalnız zaman kaybıdır.
#
# Düzenek mutate-get-kadran.sh'tan: tek eşleşme şartı (K-127), yeşil taban
# şartı, derleme kapısı (K-096; kabuk için `bash -n`).
set -uo pipefail

cd "$(dirname "$0")/.."
G=scripts/mutation-select.sh

BAKDIR=$(mktemp -d)
cp "$G" "$BAKDIR/mutation-select.sh"
restore() { cp "$BAKDIR/mutation-select.sh" "$G"; }
trap restore EXIT

fail=0

olc() {
    bash scripts/check-mutation-select.sh >/dev/null 2>&1
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
    echo "!! TABAN KIRMIZI: mutasyonsuz seçicide denetim düşüyor — ölçüm YAPILMADI"
    exit 1
fi

echo "== Az seçmek =="

mutate "boş liste hiçbir şey seçmiyor" \
    "s=s.replace('if [ -z \"\$degisen\" ] || grep -qE','if grep -qE',1)"
mutate "ci.yml değişince hepsi koşmuyor" \
    "s=s.replace('^(\\\\.github/|proto/|','^(proto/|',1)"
mutate "proto değişince hepsi koşmuyor" \
    "s=s.replace('^(\\\\.github/|proto/|','^(\\\\.github/|',1)"
mutate "seçici değişince hepsi koşmuyor" \
    "s=s.replace('scripts/mutation-select\\\\.sh\$|','',1)"
mutate "alt modülün go.mod'u ortak sayılmıyor" \
    "s=s.replace('(.*/)?go','go',1)"
mutate "betiğin andığı paket kapsama girmiyor" \
    "s=s.replace('(\$0 in dosya) || ((\$0 in dizin) && /\\\\//) { print }','(\$0 in dosya) { print }',1)"
mutate "betiğin andığı dosya kapsama girmiyor" \
    "s=s.replace('(\$0 in dosya) || ((\$0 in dizin) && /\\\\//) { print }','((\$0 in dizin) && /\\\\//) { print }',1)"
mutate "# kapsam satırı okunmuyor" \
    "s=s.replace(\"sed -n 's/^# kapsam: *//p' \\\"\$1\\\" | tr ' ' '\\\\n' | sed 's#/*\$##'\",'true',1)"
mutate "alt yol öneke uymuyor" \
    "s=s.replace('if (!sub(/\\\\/[^\\\\/]*\$/, \"\", p)) break','break',1)"

echo "== Fazla seçmek =="

mutate "her betik her zaman seçiliyor" \
    "s=s.replace('if grep -qxF \"\$b\" <<<\"\$degisen\" ||','if true ||',1)"

echo
if [ "$fail" -ne 0 ]; then
    echo "En az bir mutasyon yakalanmadı — denetim iddia ettiği şeyi korumuyor."
    exit 1
fi
echo "Bütün mutasyonlar yakalandı."
