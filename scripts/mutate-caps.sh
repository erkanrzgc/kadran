#!/usr/bin/env bash
# Konteyner yetenek/PID sınırı testlerinin GERÇEKTEN bir şey koruduğunu sınar.
#
# Her mutasyon korunan bir özelliği KASTEN bozar; test kırmızıya
# dönmezse o özellik korunmuyor demektir.
#
# ── Neden bu koruma pozitif olmak zorunda ───────────────────────────
#
# Sürücüdeki diğer sertleştirmeler ALANI HİÇ TANIMLAMAYARAK çalışıyor
# (`Privileged`, `CapAdd`, `Devices`…). Yetenek düşürme öyle çalışamaz:
# Docker'ın varsayılanı boş değil ~14 yetenektir, dolayısıyla "yazmamak"
# onları kaldırmaz. Bu yüzden `CapDrop: ALL` ve `PidsLimit` POZİTİF
# ifadelerdir ve pozitif ifadeler sessizce silinebilir — mutasyon tam da
# bunu yakalamak için var.
#
# ── K-071'in dersi ──────────────────────────────────────────────────
#
# Yeşil kalan bir mutasyon İKİ zıt sonuç doğurabilir: test zayıftır ya da
# MUTASYON zayıftır. Bu yüzden betik, mutasyonun dosyaya uygulanıp
# uygulanmadığını da ayrıca doğruluyor.
set -uo pipefail

cd "$(dirname "$0")/.."
SRC=internal/dockerdrv/container.go
BAK=$(mktemp)
cp "$SRC" "$BAK"
restore() { cp "$BAK" "$SRC"; }
trap restore EXIT

fail=0
WANT='TestCreateDropsAllCapabilities|TestPidsLimitIsBounded|TestCreatePinsSecurityOpt'

# mutate <ad> <python-ifadesi>
mutate() {
    local name="$1" expr="$2"
    restore
    if ! python -c "
import io,sys
p='$SRC'
s=io.open(p,encoding='utf-8').read()
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

    if go test ./internal/dockerdrv/ -run "$WANT" -count=1 >/dev/null 2>&1; then
        echo "  KIRMIZI OLMADI: $name"
        fail=1
    else
        echo "  yakalandı: $name"
    fi
}

echo "== Yetenek ve PID sınırı mutasyonları =="

mutate "CapDrop tamamen kaldırıldı" \
    "s=s.replace('\t\t\tCapDrop:     []string{\"ALL\"},\n','',1)"

mutate "CapDrop boş listeye çevrildi" \
    "s=s.replace('CapDrop:     []string{\"ALL\"},','CapDrop:     []string{},',1)"

mutate "CapDrop yalnızca NET_RAW düşürüyor" \
    "s=s.replace('CapDrop:     []string{\"ALL\"},','CapDrop:     []string{\"NET_RAW\"},',1)"

mutate "PidsLimit gönderilmiyor" \
    "s=s.replace('\t\t\tPidsLimit:   defaultPidsLimit,\n','',1)"

mutate "PidsLimit 0 (Docker'da SINIRSIZ)" \
    "s=s.replace('const defaultPidsLimit = 512','const defaultPidsLimit = 0',1)"

mutate "PidsLimit fork bombasını durduramayacak kadar yüksek" \
    "s=s.replace('const defaultPidsLimit = 512','const defaultPidsLimit = 1000000',1)"

mutate "CapAdd geri açıldı" \
    "s=s.replace('\tCapDrop []string \`json:\"CapDrop\"\`','\tCapDrop []string \`json:\"CapDrop\"\`\n\tCapAdd  []string \`json:\"CapAdd\"\`',1); s=s.replace('CapDrop:     []string{\"ALL\"},','CapDrop:     []string{\"ALL\"},\n\t\t\tCapAdd:      []string{\"NET_RAW\"},',1)"

restore
echo
if [ "$fail" -ne 0 ]; then
    echo "BAŞARISIZ: en az bir mutasyon yakalanmadı ya da uygulanamadı."
    exit 1
fi
echo "Bütün mutasyonlar yakalandı."
