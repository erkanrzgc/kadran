#!/usr/bin/env bash
# Vekil izleyicisi ve onarımın testlerinin GERÇEKTEN bir şey koruduğunu
# sınar (K-112).
#
# Her mutasyon korunan bir özelliği KASTEN bozar; test kırmızıya
# dönmezse o özellik korunmuyor demektir.
#
# ── Bu dilimde neyin bozulması EN PAHALI ────────────────────────────
#
# Alarmın kapanma kuralı. Aynı gün ÜÇ kez değişti ve iki önceki hâli de
# ölçülerek yanlış çıktı:
#   - onarımdan sonra koşulsuz kapatmak "rotalanamayan uygulama var"
#     alarmını gizliyordu (güvenlik incelemesi),
#   - yalnızca kendi onardığını kapatmak reboot'tan sonra alarmı SONSUZA
#     DEK açık bıraktı (taze sunucuda ölçüldü).
# Akla ilk gelen "eksik yok, atlanan yok → kapat" kuralı da yanlış:
# canlıda kadrand'nin göndermediği bir rota varken de eksik yoktur
# (K-054). Üçü de aşağıda mutant olarak duruyor.
#
# ── K-071/K-080'in dersi ────────────────────────────────────────────
#
# Yeşil kalan bir mutasyon İKİ zıt sonuç doğurabilir: test zayıftır ya da
# MUTASYON zayıftır. Bu yüzden betik, mutasyonun dosyaya uygulanıp
# uygulanmadığını da ayrıca doğruluyor.
set -uo pipefail

cd "$(dirname "$0")/.."
REC=internal/deploy/reconcile.go
CLI=internal/proxydrv/client.go
WAT=cmd/kadrand/proxywatch.go
DEP=./internal/deploy/
DRV=./internal/proxydrv/
PAN=./cmd/kadrand/

BAK_REC=$(mktemp); BAK_CLI=$(mktemp); BAK_WAT=$(mktemp)
cp "$REC" "$BAK_REC"; cp "$CLI" "$BAK_CLI"; cp "$WAT" "$BAK_WAT"
restore() {
    cp "$BAK_REC" "$REC"; cp "$BAK_CLI" "$CLI"; cp "$BAK_WAT" "$WAT"
}
trap restore EXIT

fail=0

# ── Taban YEŞİL olmalı ───────────────────────────────────────────────
#
# Mutasyonsuz kodda düşen bir test her mutantı "yakalandı" gösterirdi:
# SESSİZ sahte geçiş. Her farklı test komutu, ilk mutantından önce bir kez
# mutasyonsuz kodda koşturuluyor.
declare -A TABAN=()
taban_yesil() {
    local key="$*"
    [[ -n "${TABAN[$key]:-}" ]] && return 0
    restore
    if ! go test "$@" >/dev/null 2>&1; then
        echo "!! TABAN KIRMIZI: mutasyonsuz kodda 'go test $*' düşüyor — ölçüm YAPILMADI"
        exit 1
    fi
    TABAN[$key]=1
}

# mutate <ad> <dosya> <"test-paketleri"> <python-ifadesi>
#
# Paketler BİLEREK bölünüyor: proxydrv'deki bir mutasyonu çoğunlukla
# deploy'un testleri yakalıyor.
mutate() {
    local name="$1" file="$2" pkgs="$3" expr="$4"
    restore
    taban_yesil $pkgs -count=1
    if ! python -c "
import io,sys
class _S(str):
    def replace(self,a,b,*r):
        # TEK eşleşme şart: aranan metin bir yorumda da geçiyorsa
        # replace(…,1) İLKİNİ, yani yorumu değiştirir ve kod hiç mutasyona
        # uğramadan ölçülür (K-127).
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

    # ── MUTANT DERLENMELİ ───────────────────────────────
    #
    # Derlenmeyen bir mutant `go test`'i düşürür ve betik bunu
    # "yakalandı" diye okur (K-096). K-112'de tam olarak oldu: bu
    # betiğin ilk elle hâlinde "canlı upstream doğrulanmaz" mutantı
    # `netip` importunu kullanılmaz bıraktığı için DERLENMİYORDU.
    local build_out
    # shellcheck disable=SC2086
    if ! build_out=$(go test $pkgs -run '^$' -count=1 2>&1); then
        echo "  !! MUTANT DERLENMİYOR: $name — ölçüm YAPILMADI,"
        echo "     mutasyon derlenebilir olacak şekilde yazılmalı. Derleyici:"
        echo "$build_out" | grep -vE '^(#|FAIL|ok)' | head -3 | sed 's/^/       /'
        fail=1
        return
    fi

    # shellcheck disable=SC2086
    if go test $pkgs -count=1 >/dev/null 2>&1; then
        echo "  KIRMIZI OLMADI: $name"
        fail=1
    else
        echo "  yakalandı: $name"
    fi
}

echo "== Alarmın kapanma kuralı =="

# Asıl kural: atlanan yok VE canlı İKİ YÖNLÜ birebir.
KOSUL='\tif sonuc.Exact && len(sonuc.Skipped) == 0 {'

# Akla ilk gelen kural. Yabancı bir rotanın alarmını on saniyede
# sessizce kapatırdı.
mutate "saf kural: yalnızca atlanan yok" "$WAT" "$PAN" \
    "s=s.replace('$KOSUL','\tif len(sonuc.Skipped) == 0 {',1)"

# Reboot hatası: gözetmen rotayı yükleyince izleyicinin onaracak bir
# şeyi kalmıyor ve alarm açık kalıyordu.
mutate "eski kural: yalnızca kendisi onarınca" "$WAT" "$PAN" \
    "s=s.replace('$KOSUL','\tif len(missing) > 0 && len(sonuc.Skipped) == 0 {',1)"

mutate "yalnızca birebir (atlanan yok sayılıyor)" "$WAT" "$PAN" \
    "s=s.replace('$KOSUL','\tif sonuc.Exact {',1)"

mutate "her turda kapatıyor" "$WAT" "$PAN" \
    "s=s.replace('$KOSUL','\tif true {',1)"

mutate "ilk başarısızlıkta alarm" "$WAT" "$PAN" \
    "s=s.replace('const proxyWatchAlarmAfter = 3','const proxyWatchAlarmAfter = 1',1)"

echo "== Birebir eşleşmenin hesabı =="

mutate "eksik yokken birebir hep true" "$REC" "$DEP" \
    "s=s.replace('\t\tout.Exact = proxydrv.Matches(cfg, live) == nil\n','\t\tout.Exact = true\n',1)"

mutate "birebir tek yönlü (deploy)" "$REC" "$DEP" \
    "s=s.replace('\t\tout.Exact = proxydrv.Matches(cfg, live) == nil\n','\t\tout.Exact = len(proxydrv.MissingHosts(cfg, live)) == 0\n',1)"

mutate "Matches tek yönlü (proxydrv)" "$CLI" "$DRV $DEP" \
    "s=s.replace('func Matches(want, live *Config) error {\n\treturn verifyApplied(want, live)\n}','func Matches(want, live *Config) error {\n\tif len(MissingHosts(want, live)) > 0 {\n\t\treturn fmt.Errorf(\"eksik\")\n\t}\n\treturn nil\n}',1)"

mutate "yüklemeden sonra birebir sayılmıyor" "$REC" "$DEP" \
    "s=s.replace('\tout.Exact = true\n\treturn out, nil\n}','\treturn out, nil\n}',1)"

echo "== Onarım =="

mutate "tetik iki yönlü (fazla rota da yükletiyor)" "$CLI" "$DRV $DEP" \
    "s=s.replace('\tsort.Strings(missing)\n\treturn missing\n}','\tfor host := range liveRoutes {\n\t\tif _, ok := routesByHost(want)[host]; !ok {\n\t\t\tmissing = append(missing, host)\n\t\t}\n\t}\n\tsort.Strings(missing)\n\treturn missing\n}',1)"

mutate "her turda yüklüyor" "$REC" "$DEP" \
    "s=s.replace('\t\tout.Exact = proxydrv.Matches(cfg, live) == nil\n\t\treturn out, nil\n','\t\tout.Exact = proxydrv.Matches(cfg, live) == nil\n',1)"

mutate "hiç yüklemiyor" "$REC" "$DEP" \
    "s=s.replace('\tif err := rc.proxy.Load(ctx, cfg); err != nil {\n\t\treturn out,','\tif err := error(nil); err != nil {\n\t\treturn out,',1)"

# Güvenlik incelemesi: onarım yüklemesi atlanan uygulamanın canlı
# rotasını siliyordu.
mutate "atlananın canlı rotası korunmuyor" "$REC" "$DEP" \
    "s=s.replace('\tfor appID, domain := range p.skippedDomains {','\tfor appID, domain := range map[string]string{} {',1)"

# Canlıya başkası yazmış olabilir (K-054): taşınan adres yeniden
# doğrulanmalı. `_ =` importu kullanılır tutuyor; yoksa mutant
# derlenmez (bu betiğin ilk hâlinde tam olarak bu oldu).
mutate "canlı upstream doğrulanmadan taşınıyor" "$CLI" "$DRV $DEP" \
    "s=s.replace('\t\tap, err := netip.ParseAddrPort(dial)\n\t\tif err != nil {\n\t\t\tcontinue\n\t\t}\n\t\tu, err := NewUpstream(ap.Addr().String(), uint32(ap.Port()))\n\t\tif err != nil {\n\t\t\tcontinue\n\t\t}\n\t\tout = append(out, u)','\t\t_ = netip.AddrPort{}\n\t\tout = append(out, Upstream{Dial: dial})',1)"

echo "== Eşzamanlılık =="

# Kilit olmadan izleyici bir dağıtımla yarışıp eski yapılandırmayı
# yükleyebilir.
mutate "Repair kilitsiz" "$REC" "$DEP" \
    "s=s.replace('func (rc *Reconciler) Repair(ctx context.Context) (RepairResult, error) {\n\trc.mu.Lock()\n\tdefer rc.mu.Unlock()\n','func (rc *Reconciler) Repair(ctx context.Context) (RepairResult, error) {\n',1)"

mutate "Reconcile kilitsiz" "$REC" "$DEP" \
    "s=s.replace('func (rc *Reconciler) Reconcile(ctx context.Context) (Result, error) {\n\trc.mu.Lock()\n\tdefer rc.mu.Unlock()\n','func (rc *Reconciler) Reconcile(ctx context.Context) (Result, error) {\n',1)"

restore
if [[ $fail -ne 0 ]]; then
    echo
    echo "En az bir mutasyon yakalanmadı — testler iddia ettikleri şeyi korumuyor."
    exit 1
fi

echo
echo "Bütün mutasyonlar yakalandı."
