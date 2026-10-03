#!/usr/bin/env bash
# Kurulum betiğinin authorized_keys yazıcısını koruyan senaryoların
# (scripts/check-install-sh.sh) GERÇEKTEN bir şey koruduğunu sınar.
#
# ── Neyin bozulması EN PAHALI ───────────────────────────────────────
#
# Root'un kadran-client'ın dizininde bir bağı izlemesi: o kullanıcı root'a
# istediği dosyaya kendi satırlarını yazdırır ya da istediği dizini kendine
# devrettirir (K-137). Hemen arkasından authorized_keys'e kısıtsız bir
# satır düşmesi (K-131) ve okuma hatasında dağıtım satırlarının sessizce
# silinmesi.
#
# Düzenek mutate-goc.sh'tan: tek eşleşme şartı (K-127), yeşil taban
# şartı, derleme kapısı (K-096; kabuk için `bash -n`), tam yoldan yedek.
#
# Root'suz koşmalı (CI öyle): okuma hatası senaryosu root'ta kurulamaz,
# o mutant root'ta ÖLÇÜLMEDİ diye basılır.
set -uo pipefail

cd "$(dirname "$0")/.."
INST=internal/bootstrap/install.sh
FILES=("$INST")

BAKDIR=$(mktemp -d)
bak() { printf '%s/%s' "$BAKDIR" "${1//\//__}"; }
for f in "${FILES[@]}"; do cp "$f" "$(bak "$f")"; done
restore() { for f in "${FILES[@]}"; do cp "$(bak "$f")" "$f"; done; }
trap restore EXIT

fail=0

olc() {
    bash scripts/check-install-sh.sh >/dev/null 2>&1
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
    # "yakalandı" üretirdi.
    local build_out
    if ! build_out=$(bash -n "$INST" 2>&1); then
        echo "  !! MUTANT DERLENMİYOR: $name — ölçüm YAPILMADI. Çıktı:"
        echo "$build_out" | head -3 | sed 's/^/       /'
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

echo "== Güvenlik: root, kadran-client'ın dizinine yazıyor (K-137) =="

mutate_in "$INST" "yonetici_satiri_yaz bağlı authorized_keys'i yeniden yazıyor" \
    "s=s.replace('    if [ -L \"\$auth_file\" ] || { [ -e','    if { [ -e',1)"

mutate_in "$INST" "yonetici_satiri_yaz FIFO'da takılıyor (düzenli dosya denetimi yok)" \
    "s=s.replace(' || { [ -e \"\$auth_file\" ] && [ ! -f \"\$auth_file\" ]; }; then',' ; then',1)"

mutate_in "$INST" "yonetici_satiri_yaz tahmin edilebilir geçici ad kullanıyor" \
    "s=s.replace('gecici=\"\$(mktemp \"\$auth_file.XXXXXX\")\"','gecici=\"\$auth_file.yeni\"',1)"

mutate_in "$INST" "yonetici satırı yol üzerinden ekleniyor (kopyaya değil)" \
    "s=s.replace('restrict \$key\" >> \"\$gecici\"','restrict \$key\" >> \"\$auth_file\"',1)"

mutate_in "$INST" "ssh_dizini_hazirla bağlı .ssh'yi izliyor" \
    "s=s.replace('    if [ -L \"\$dizin\" ]; then','    if false; then',1)"

if [ "$(id -u)" -ne 0 ]; then
    mutate_in "$INST" "okuma hatası yutuluyor (dağıtım satırları düşer)" \
        "s=s.replace('        [ \"\$rc\" -le 1 ] || {','        true || {',1)"
else
    echo "  ölçülmedi (root): okuma hatası yutuluyor — root'suz koşturun (CI öyle)"
fi

echo "== Erişim: tek satır (K-131) =="

mutate_in "$INST" "iki satırlı anahtar dosyası kabul ediliyor" \
    "s=s.replace('-eq 1 ] || return 1','-ge 1 ] || return 1',1)"

mutate_in "$INST" "CR taşıyan anahtar kabul ediliyor" \
    "s=s.replace(') return 1 ;; esac',') : ;; esac',1)"

echo
if [[ "$fail" -ne 0 ]]; then
    echo "En az bir mutasyon yakalanmadı — denetimler iddia ettikleri şeyi korumuyor."
    exit 1
fi
echo "Bütün mutasyonlar yakalandı."
