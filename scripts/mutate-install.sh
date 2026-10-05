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
#
# K-139 mutantları (yazmanın kadran-client olarak yapılması) gerçek bir
# kullanıcı ve setpriv ister, yalnız root'ta ölçülür. CI bu betiği root'suz
# koşturuyor; KADRAN_TEST_REAL_SUDO=1 ile her ölçümde check-install-sh.sh'ı
# bir kez de `sudo -n` ile koşturur ve o mutantlar ZORUNLU olarak ölçülür.
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

# Root bölümü ölçülebiliyor mu: root'uz ya da CI'da parolasız sudo var.
KOK_OLCULUR=0
if [ "$(id -u)" -eq 0 ]; then
    KOK_OLCULUR=1
elif [ "${KADRAN_TEST_REAL_SUDO:-}" = 1 ]; then
    sudo -n true || { echo "!! KADRAN_TEST_REAL_SUDO=1 ama parolasız sudo yok — ölçüm YAPILMADI"; exit 1; }
    KOK_OLCULUR=2
fi

olc() {
    bash scripts/check-install-sh.sh >/dev/null 2>&1 || return 1
    if [ "$KOK_OLCULUR" = 2 ]; then
        sudo -n env KADRAN_TEST_REQUIRE_ROOT=1 bash scripts/check-install-sh.sh >/dev/null 2>&1 || return 1
    fi
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
    "s=s.replace('gecici=\"\$(mktemp \"\$f.XXXXXX\")\"','gecici=\"\$f.yeni\"',1)"

mutate_in "$INST" "yonetici satırı yol üzerinden ekleniyor (kopyaya değil)" \
    "s=s.replace('\"\$satir\" >> \"\$gecici\"','\"\$satir\" >> \"\$f\"',1)"

mutate_in "$INST" "ssh_dizini_hazirla bağlı .ssh'yi izliyor" \
    "s=s.replace('    if [ -L \"\$dizin\" ]; then','    if false; then',1)"

if [ "$(id -u)" -ne 0 ]; then
    mutate_in "$INST" "okuma hatası yutuluyor (dağıtım satırları düşer)" \
        "s=s.replace('        [ \"\$rc\" -le 1 ] || {','        true || {',1)"
else
    echo "  ölçülmedi (root): okuma hatası yutuluyor — root'suz koşturun (CI öyle)"
fi

echo "== Güvenlik: yazma kadran-client olarak (K-139) =="

# Sınama düzeneğinin kendi kapısı: root'suz koşuda başka bir kullanıcı
# istenirse reddetmeli. Root'ta o dal hiç koşmaz.
if [ "$(id -u)" -ne 0 ]; then
    mutate_in "$INST" "istemci_olarak root'suzken başka kullanıcıya geçiyor" \
        "s=s.replace(' && [ \"\$kullanici\" != \"\$(id -un 2>/dev/null)\" ]; then',' && false; then',1)"
else
    echo "  ölçülmedi (root): istemci_olarak'ın root'suz kapısı — root'suz koşturun (CI öyle)"
fi

if [ "$KOK_OLCULUR" != 0 ]; then
    mutate_in "$INST" "istemci_olarak setpriv'i atlıyor (root olarak yazıyor)" \
        "s=s.replace('onek=(setpriv --reuid \"\$kullanici\" --regid \"\$grup\" --clear-groups --)','onek=()',1)"

    mutate_in "$INST" "yonetici_satiri_yaz yazıcıyı root olarak çağırıyor" \
        "s=s.replace('istemci_olarak \"\$kullanici\" \"\$grup\" ak_yaz_istemci','ak_yaz_istemci',1)"

    mutate_in "$INST" "ssh_dizini_hazirla .ssh'yi root olarak kuruyor" \
        "s=s.replace('istemci_olarak \"\$kullanici\" \"\$grup\" install -d -m 0700 \"\$dizin\"','install -d -m 0700 -o \"\$kullanici\" -g \"\$grup\" \"\$dizin\"',1)"
else
    echo "  ölçülmedi (root yok): K-139'un üç root mutantı — root'la ya da KADRAN_TEST_REAL_SUDO=1 ile koşturun (CI öyle)"
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
