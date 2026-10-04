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
INST=internal/bootstrap/install.sh
FILES=("$GOC" "$BOOT" "$INST")

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
    if ! build_out=$(bash -n "$GOC" 2>&1 && bash -n "$INST" 2>&1 && go test ./internal/bootstrap/ -run '^$' -count=1 2>&1); then
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
    "s=s.replace('    [ -e \"\$GOC_DIR/tamam\" ] && return 1\n','',1)"

echo "== Temizlik: eski konteynerler ne zaman silinir =="

mutate_in "$GOC" "IP eşleşmesi önekle yapılıyor (172.21.0.2 ~ 172.21.0.20)" \
    "s=s.replace('[[ \"\$yapi\" == *\"\\\\\"\$ip:\"* ]] && return 0','[[ \"\$yapi\" == *\"\$ip\"* ]] && return 0',1)"

mutate_in "$GOC" "okunamayan yapılandırma silmeye izin veriyor" \
    "s=s.replace('    [ -n \"\$yapi\" ] || return 1\n    ! goc_ip_var','    ! goc_ip_var',1)"

mutate_in "$GOC" "trafik alan eski konteyner siliniyor" \
    "s=s.replace('    ! goc_ip_var \"\$yapi\" \"\$@\"','    true',1)"

echo "== Erişim =="

mutate_in "$GOC" "goc_ak satır başına çapalanmıyor" \
    "s=s.replace('\"s#^\$ESKI_CONNECT#','\"s#\$ESKI_CONNECT#',1)"

mutate_in "$GOC" "goc_uzak_yedek offsite.conf'u çevirmiyor" \
    "s=s.replace('    goc_yerinde_sed \"\$oc\"','    : goc_yerinde_sed \"\$oc\"',1)"

mutate_in "$GOC" "goc_uzak_yedek iki hedef birden varken sürüyor" \
    "s=s.replace('        die \"göç: rclone.conf\\'ta hem','        : \"x',1)"

mutate_in "$GOC" "goc_uzak_yedek yarıda kalan koşuyu tamamlayamıyor" \
    "s=s.replace('    elif ! grep -qx','    elif true || grep -qx',1)"

echo "== Güvenlik: root dosya yazarken (güvenlik incelemesi) =="

mutate_in "$GOC" "goc_yerinde_sed sembolik bağı yeniden yazıyor" \
    "s=s.replace('    if [ -L \"\$f\" ]; then\n        die \"göç: \$f sembolik','    if false; then\n        die \"göç: \$f sembolik',1)"

mutate_in "$GOC" "goc_yerinde_sed tahmin edilebilir geçici ad kullanıyor" \
    "s=s.replace('gecici=\"\$(mktemp \"\$f.goc.XXXXXX\")\"','gecici=\"\$f.goc\"',1)"

mutate_in "$GOC" "geçici kopya umask'la açılıyor (anahtar bir an herkese okunur)" \
    "s=s.replace('gecici=\"\$(mktemp \"\$f.goc.XXXXXX\")\"','gecici=\"\$f.goc.r\"',1)"

mutate_in "$GOC" "goc_onek sembolik bağ dizinini izliyor" \
    "s=s.replace('    if [ -L \"\$dizin\" ]; then\n        die','    if false; then\n        die',1)"

echo "== Seçimli birimler (rc6 provası) =="

mutate_in "$GOC" "kurulu ama kapalı birim taşınmıyor" \
    "s=s.replace('    { cat \"\$GOC_DIR/kurulu\" 2>/dev/null || true; cat','    { cat',1)"

mutate_in "$GOC" "kurulu kaydı birim dosyasına bakmıyor" \
    "s=s.replace('panely-\$s.timer\" ]; then echo','panely-\$s.timer.YOK\" ]; then echo',1)"

echo "== Yükseltme: kurulu seçimli birimler yenileniyor (K-138) =="

mutate_in "$INST" "yükseltme seçimli birimleri yenilemiyor (v0.4.1'in canlı hatası)" \
    "s=s.replace('else\n    secimli_guncelle\nfi\n','fi\n',1)"

mutate_in "$GOC" "kurulu olmayan seçimli birim de kuruluyor" \
    "s=s.replace('        [ -e \"\$z\" ] || continue\n','',1)"

mutate_in "$GOC" "maskelenmiş birimin üstüne yazılıyor" \
    "s=s.replace('        if [ \"\$(readlink \"\$z\")\" = /dev/null ]; then','        if false; then',1)"

mutate_in "$GOC" "güncellemeden sonra systemd yeniden yüklenmiyor" \
    "s=s.replace('    [ \"\$guncel\" -eq 0 ] || systemctl daemon-reload\n','    :\n',1)"

mutate_in "$GOC" "güncelleme kapalı zamanlayıcıyı açıyor" \
    "s=s.replace('        secimli_dosyalari_kur \"\$s\"\n        say \"seçimli birim güncellendi','        secimli_dosyalari_kur \"\$s\"\n        systemctl enable --now \"kadran-\$s.timer\"\n        say \"seçimli birim güncellendi',1)"

mutate_in "$GOC" "uzak yedek betiği yenilenmiyor" \
    "s=s.replace('            install -m 0755 -o root -g root \"\$STAGE/kadran-offsite.sh\"','            : \"\$STAGE/kadran-offsite.sh\"',1)"

echo "== Kurulumu takılı bırakmamak (güvenlik incelemesi) =="

mutate_in "$GOC" "silinemeyen eski ağ kurulumu düşürüyor" \
    "s=s.replace('        xargs -r docker network rm >/dev/null ||\n','        xargs -r docker network rm >/dev/null\n',1)"

mutate_in "$INST" "eski kalıntılar doğrulamadan ÖNCE temizleniyor" \
    "s=s.replace('# (son çalışan sürüm) yerinde kalsın.\nif goc_artik_var; then\n    goc_temizle\nfi\n','# (son çalışan sürüm) yerinde kalsın.\n',1); s=s.replace('\n# ── Kurulum sonrası doğrulama','\nif goc_artik_var; then\n    goc_temizle\nfi\n\n# ── Kurulum sonrası doğrulama',1)"

echo "== Güvenlik: depo beyaz listesi (K-056) =="

mutate_in "$GOC" "drop-in yeni birime taşınmıyor" \
    "s=s.replace('        goc_dropin_tasi \"\$b\"\n','',1)"

mutate_in "$GOC" "drop-in içindeki eski adlar çevrilmiyor" \
    "s=s.replace('        sed \\'s/panely/kadran/g\\' \"\$f\" > \"\$hedef\"','        cat \"\$f\" > \"\$hedef\"',1)"

mutate_in "$GOC" "okunamayan komut satırı 'beyaz liste yok' sayılıyor" \
    "s=s.replace('        die \"göç: executor\\'ın komut satırı okunamadı','        : \"x',1)"

mutate_in "$GOC" "beyaz liste değerinin yalnız ilki okunuyor" \
    "s=s.replace('{ grep -oE -- \\'--allow-repo[ =][^ ;]*\\' || true; }','{ grep -oE -- \\'--allow-repo[ =][^ ;]*\\' || true; } | head -1',1)"

mutate_in "$GOC" "goc_gerekli makinenin gerçek kullanıcılarına bakıyor" \
    "s=s.replace('    if [ -n \"\$KOK\" ]; then\n        grep -q','    if false; then\n        grep -q',1)"

echo "== Paket =="

mutate_in "$BOOT" "göçün okuduğu bir birim pakette yok" \
    "s=s.replace('\"kadran-offsite.timer\":','\"kadran-offsite.timer-YOK\":',1)"

mutate_in "$BOOT" "goc.sh pakete girmiyor" \
    "s=s.replace('range []string{\"install.sh\", \"goc.sh\", \"geri.sh\", \"kasa-coz.sh\"}','range []string{\"install.sh\", \"geri.sh\", \"kasa-coz.sh\"}',1)"

echo
if [[ "$fail" -ne 0 ]]; then
    echo "En az bir mutasyon yakalanmadı — denetimler iddia ettikleri şeyi korumuyor."
    exit 1
fi
echo "Bütün mutasyonlar yakalandı."
