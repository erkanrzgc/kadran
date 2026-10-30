#!/usr/bin/env bash
# İstemci kurulum betiğini (K-159, scripts/get-kadran.sh) koruyan denetimin
# GERÇEKTEN bir şey koruduğunu sınar.
#
# ── Neyin bozulması EN PAHALI ───────────────────────────────────────
#
# Özet denetimi: betik indirdiğini kullanıcının PATH'ine koyuyor. Değiştirme:
# yerinde yazmak, çalışan bir istemciyi kurarken düşürür. Sürüm: adrese ve
# dosya adına giriyor.
#
# Düzenek mutate-goc.sh'tan: tek eşleşme şartı (K-127), yeşil taban şartı,
# derleme kapısı (K-096; kabuk için `sh -n`), tam yoldan yedek.
set -uo pipefail

cd "$(dirname "$0")/.."
G=scripts/get-kadran.sh

BAKDIR=$(mktemp -d)
cp "$G" "$BAKDIR/get-kadran.sh"
restore() { cp "$BAKDIR/get-kadran.sh" "$G"; }
trap restore EXIT

fail=0

olc() {
    bash scripts/check-get-kadran.sh >/dev/null 2>&1
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
    # Sözdizimi bozuk bir mutant her denetimi düşürür, sahte "yakalandı" olur.
    if ! sh -n "$G"; then
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
    echo "!! TABAN KIRMIZI: mutasyonsuz betikte denetim düşüyor — ölçüm YAPILMADI"
    exit 1
fi

echo "== Özet denetimi =="

mutate "özet farkı yok sayılıyor" \
    "s=s.replace('if [ -z \"\$want\" ] || [ \"\$got\" != \"\$want\" ]; then','if [ -z \"\$want\" ]; then',1)"

mutate "SHA256SUMS'ta adı olmayan ikili geçiyor" \
    "s=s.replace('if [ -z \"\$want\" ] || [ \"\$got\" != \"\$want\" ]; then','if [ -n \"\$want\" ] && [ \"\$got\" != \"\$want\" ]; then',1)"

mutate "*ad biçimi okunmuyor" \
    "s=s.replace('sub(/^\\\\*/, \"\", n); ','',1)"

echo "== Kurulum =="

mutate "çalışan ikilinin üstüne yerinde yazıyor" \
    "s=s.replace('new=\"\$dir/.kadran.new.\$\$\"\n','new=\"\$target\"\n',1); s=s.replace('    mv -f \"\$new\" \"\$target\"\n','',1)"

mutate "kip 755 değil" \
    "s=s.replace('chmod 0755 \"\$new\"','chmod 0700 \"\$new\"',1)"

mutate "PATH uyarısı yok" \
    "s=s.replace('echo \"add \$dir to your PATH, for example:\"',':',1)"

mutate "PATH için hazır satır yok" \
    "s=s.replace('printf \\'  export PATH=\"%s:\$PATH\"\\\\n\\' \"\$dir\"',':',1)"

echo "== Sürüm ve araçlar =="

mutate "API etiketi doğrulanmıyor" \
    "s=s.replace('valid_version \"\$v\" || die','true || die',1)"

mutate "KADRAN_VERSION doğrulanmıyor" \
    "s=s.replace('elif ! valid_version \"\$ver\"; then','elif false; then',1)"

mutate "wget dalı yok" \
    "s=s.replace('elif command -v wget >/dev/null 2>&1; then','elif false; then',1)"

echo
if [[ "$fail" -ne 0 ]]; then
    echo "En az bir mutasyon yakalanmadı — denetim iddia ettiği şeyi korumuyor."
    exit 1
fi
echo "Bütün mutasyonlar yakalandı."
