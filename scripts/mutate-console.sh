#!/usr/bin/env bash
# Kadran Konsolu'nun güvenlik kapılarını sınayan testlerin GERÇEKTEN bir şey
# koruduğunu ölçer (K-169). Düzenek mutate-desktop.sh ile aynı: tek eşleşme
# şartı (K-127), yeşil taban şartı, derleme kapısı (K-096).
#
# ── Neyin bozulması EN PAHALI ───────────────────────────────────────
#
#   - Sınıf kapısı: bir yaprak sessizce "okuma" olur, onay kutusu atlanır.
#   - Arıtma: -env ya da -build-arg değeri geçmişe ve kutuya düz yazılır.
#   - Yazılı ad: büyük/küçük harf farkı onaylanır; yönetici ayarı yok sayılır.
#   - Gizli çıktı: TOTP sırrı kart yerine düz çıktıya gider.
#   - Sunucu bağlamı: başka sunucunun adıyla komut çalışır.
#   - Root yolu: ssh host anahtarını sormadan kabul eder.
#   - Akış: tek olay sınırsız büyür, arayüz donar.
#   - Onay kutusu: satır yine kesilir (R10) ya da duman bayrağı kutuyu atlar.
#
# Üç ölçüm: kök modülün CLI testleri (cli), masaüstünün Go paketleri (desk) ve
# duman testi (e2e). Duman testi yalnız Windows'ta koşar (WebView2); başka
# yerde son mutant ATLANIR ve bu açıkça yazılır.
#
# Tek başına koşar: iki ağır koşu aynı anda makineyi kilitler ve Windows kilidi
# geri yüklemeyi bozar.
#
# kapsam: desktop/internal/console desktop/internal/confirm internal/cli desktop/console_windows.go desktop/console_smoke.go
set -uo pipefail

cd "$(dirname "$0")/.."

FILES=(
    internal/cli/commands.go
    desktop/internal/console/redact.go
    desktop/internal/console/gate.go
    desktop/internal/console/launch.go
    desktop/internal/console/normalize.go
    desktop/internal/console/stream.go
    desktop/internal/confirm/box.go
    desktop/console_windows.go
)
BAKDIR=$(mktemp -d)
bak() { printf '%s/%s' "$BAKDIR" "${1//\//__}"; }
for f in "${FILES[@]}"; do cp "$f" "$(bak "$f")"; done
restore() { for f in "${FILES[@]}"; do cp "$(bak "$f")" "$f"; done; }
trap restore EXIT

fail=0

# Derleme kapıları: derlenmeyen mutant testleri düşürür ve "yakalandı" okunur.
cli_compiles() { go vet ./internal/cli/ >/dev/null 2>&1; }
desk_compiles() { (cd desktop && go vet ./internal/... >/dev/null 2>&1); }
e2e_compiles() { (cd desktop && go vet -tags desktop,production,smoke . >/dev/null 2>&1); }

cli_pass() { go test -count=1 -timeout 300s ./internal/cli/ >/dev/null 2>&1; }
desk_pass() { (cd desktop && go test -count=1 -timeout 180s ./internal/... >/dev/null 2>&1); }
# Duman ikilisini derler ve yolculuğu koşar (arayüz önceden derlenmiş olmalı).
e2e_pass() { (cd desktop && node scripts/smoke.ts >/dev/null 2>&1); }

# mutate <ölçüm: cli|desk|e2e> <dosya> <ad> <python-ifadesi>
mutate() {
    local harness="$1" file="$2" name="$3" expr="$4"
    restore
    if ! python -c "
import io,sys
class _S(str):
    def replace(self,a,b,*r):
        # TEK eşleşme şart: aranan metin bir yorumda da geçiyorsa
        # replace(…,1) İLKİNİ değiştirir ve kod mutasyonsuz ölçülür (K-127).
        n=self.count(a)
        if n!=1:
            sys.stderr.write('REPLACE '+str(n)+' KEZ ESLESTI (1 olmali): '+repr(a[:70])+chr(10))
            sys.exit(8)
        return _S(str.replace(self,a,b,*r))
p='$file'
s=_S(io.open(p,encoding='utf-8',newline='').read())
o=s
$expr
if s==o:
    sys.exit(9)
io.open(p,'w',encoding='utf-8',newline='').write(s)
"; then
        echo "  !! MUTASYON UYGULANAMADI: $name — betik bozuk, ölçüm YAPILMADI"
        fail=1
        return
    fi

    if ! "${harness}_compiles"; then
        echo "  !! MUTANT DERLENMİYOR: $name — ölçüm YAPILMADI"
        fail=1
        return
    fi

    if "${harness}_pass"; then
        echo "  KIRMIZI OLMADI: $name"
        fail=1
    else
        echo "  yakalandı: $name"
    fi
}

# Taban YEŞİL olmalı: kırmızı bir taban her mutantı "yakalandı" gösterirdi.
if ! cli_compiles || ! cli_pass || ! desk_compiles || ! desk_pass; then
    echo "!! TABAN KIRMIZI: mutasyonsuz kodda derleme ya da testler düşüyor — ölçüm YAPILMADI"
    exit 1
fi

echo "== Sınıf kapısı =="
mutate cli internal/cli/commands.go "deploy yaprağı okuma sınıfı (kutusuz çalışır)" \
    "s=s.replace('Class: ClassChange, run: (*cli).runDeploy','Class: ClassRead, run: (*cli).runDeploy',1)"

echo "== Arıtma =="
mutate desk desktop/internal/console/redact.go "-env=KEY=değer biçimi arıtılmıyor" \
    "s=s.replace('case joined && isMaskedFlag(name):','case joined && isMaskedFlag(name) && false:',1)"
mutate desk desktop/internal/console/redact.go "Redact -build-arg değerini arıtmıyor" \
    "s=s.replace('var maskedFlags = []string{\"env\", \"build-arg\"}','var maskedFlags = []string{\"env\"}',1)"

mutate desk desktop/internal/console/redact.go "login/totp konumsal değeri arıtılmıyor" \
    "s=s.replace('\targs = maskSecretPositionals(args)\n','',1)"

echo "== Yazılı ad ve yönetici ayarı =="
mutate desk desktop/internal/console/gate.go "yazılı ad büyük/küçük harf farkını kabul ediyor" \
    "s=s.replace('if got != want {','if !strings.EqualFold(got, want) {',1)"
mutate desk desktop/internal/console/gate.go "yönetici ayarı kapalıyken root komutu geçiyor" \
    "s=s.replace('\t\tif !r.d.Admin.Load() {\n\t\t\treturn ErrAdminOff\n\t\t}\n','',1)"

echo "== Gizli çıktı =="
mutate desk desktop/internal/console/launch.go "Sensitive akışı Stdout'a bağlı (sır düz çıktıya gider)" \
    "s=s.replace('Sensitive: rio.secret,','Sensitive: rs.out.writer(StreamOut),',1)"

echo "== Sunucu bağlamı =="
mutate desk desktop/internal/console/normalize.go "farklı -s kabul ediliyor" \
    "s=s.replace('if r != server {','if r != server && false {',1)"

echo "== Root yolu =="
mutate desk desktop/internal/console/launch.go "StrictHostKeyChecking eklenmiyor" \
    "s=s.replace('args = append([]string{\"-o\", strictHostKey}, args...)','_ = strictHostKey',1)"

echo "== Akış =="
mutate desk desktop/internal/console/stream.go "birleştirici sınırı 32 KiB'tan büyüdü" \
    "s=s.replace('maxEventBytes = 32 << 10','maxEventBytes = 1 << 30',1)"
mutate desk desktop/internal/console/stream.go "büyük yazma olay başına kesilmiyor" \
    "s=s.replace('if n > maxEventBytes {','if false {',1)"

echo "== Onay kutusu (R10) =="
mutate desk desktop/internal/confirm/box.go "kutu komut satırını yine kesiyor (safe)" \
    "s=s.replace('escapeParts(req.Line))','safeParts(req.Line))',1)"
mutate desk desktop/internal/console/gate.go "1024 birim sınırı yok (uzun satır kutuda kesilir)" \
    "s=s.replace('confirm.ConsoleLineUnits(p.line) > confirm.MaxConsoleLineUnits','false',1)"

echo "== Duman bayrağı kutuyu atlatmaz =="
if [ "$(go env GOOS)" = windows ] && [ -d desktop/node_modules ] && [ -d desktop/out/renderer ]; then
    mutate e2e desktop/console_windows.go "fake@test'te konsol kutusu atlanıyor (Fixture)" \
        "s=s.replace('Confirm: consoleConfirm(h),','Confirm: func(confirm.Box) (bool, error) { return smokeBuild, nil },',1)"
else
    echo "  ATLANDI: duman testi Windows ve derlenmiş arayüz ister (cd desktop && npm run build)"
fi

restore
if [ "$fail" -ne 0 ]; then
    echo "SONUÇ: en az bir mutant yakalanmadı ya da ölçülemedi."
    exit 1
fi
echo "Bütün mutasyonlar yakalandı."
