#!/usr/bin/env bash
# www eşi ve yönlendirme adlarını koruyan testlerin GERÇEKTEN bir şey
# koruduğunu sınar (proje B: appnames, store, api, deploy, proxydrv).
#
# ── Neyin bozulması EN PAHALI ───────────────────────────────────────
#
# Yönlendirme adı bir alan adı rotasının üzerine binerse trafik çalınır:
# başka bir uygulamanın domaini 308 ile yabancı bir hedefe gider. Hedefi
# kümede olmayan, durum kodu 302 olan ya da Location şablonu gevşek bir
# yönlendirme açık yönlendirme (open redirect) ya da yöntem kaybı demek.
# Ayrıca tek bir çakışan ad bütün rota kümesini düşürürse HER uygulama
# kesilir; başarısız bir dağıtım da www'yi düşürmemeli. Göç mevcut
# uygulamaları www ile başlatırsa kullanıcının haberi olmadan eşler
# yönlendirilmeye başlar.
#
# Düzenek mutate-domaincheck.sh'tan: tek eşleşme şartı (K-127), yeşil taban
# şartı, derleme kapısı (K-096), tam yoldan yedek. Ek olarak TESTS düzeninin
# yönlendirme test dosyalarındaki (OWNED) bütün testleri kapsadığı ve TESTS'teki
# her adın gerçekten koştuğu denetlenir: kapsanmayan ya da yeniden adlandırılıp
# koşmaz olan test sessizce mutantları ölçmez (önceki betiklerde yaşandı).
# Betik git'e dokunmaz; squash sonrası geçmişe bağlı değildir.
set -uo pipefail

cd "$(dirname "$0")/.."
ROUTES=internal/proxydrv/routes.go
CONFIG=internal/proxydrv/config.go
RECON=internal/deploy/reconcile.go
NAMES=internal/appnames/appnames.go
SNAMES=internal/store/appnames.go
MIG=internal/store/migrations/0010_app_redirects.sql
VALID=internal/api/appvalidate.go
PKGS=(./internal/proxydrv/ ./internal/proxyguard/ ./internal/deploy/ ./internal/appnames/ ./internal/store/ ./internal/api/ ./cmd/kadran/)
OWNED=(
    cmd/kadran/appredirects_test.go
    internal/api/appredirects_test.go
    internal/appnames/appnames_test.go
    internal/deploy/reconcile_redirects_test.go
    internal/proxydrv/redirects_test.go
    internal/store/appredirects_test.go
)
# Tam ad eşleşmesi: bir test adı değişir ya da eklenirse kapsama denetimi
# aşağıda yakalar. build/caddy ayrı modül; testleri orada koşar.
TESTS='^(TestAppCreateSendsWWWByDefault|TestAppCreateNoWWW|TestAppCreateRepeatsRedirect|TestAppUpdateWWWAndNoWWWConflict|TestAppUpdateRedirectRm|TestAppShowPrintsRedirects|TestAppCreateWarnsOnOldServer|TestAppUpdateNamesOnlyOnOldServerHintsUpgrade|TestAppUpdateNamesValidationErrorIsShownAsIs|TestAppCreateRedirectDNSWarnsButSucceeds|TestAppCreateSkipDNSCheckSkipsRedirects|TestAppCreateSubdomainHasNoWWW|TestRedirectChecksRunInParallel|TestValidateRedirects|TestCreateAppEchoesWWW|TestCreateAppNameConflictIsAlreadyExists|TestUpdateAppNamesOnlyReconciles|TestUpdateAppReportsSkippedRedirects|TestUpdateAppNamesReconcileFailureSaysSaved|TestUpdateAppAddAndRemoveSameRedirect|TestUpdateAppAuditCarriesRedirects|TestCreateAppAuditCarriesWWWAndRedirects|TestAppRedirectsRoundTripThroughProto|TestUpdateAppRejectsEmptyRedirectName|TestUpdateAppNamesOnlyOnUnroutedAppIsNotReportedAsApplied|TestUpdateAppWWWAloneReportsPartner|TestUpdateAppReportsSkippedWWWPartner|TestUpdateAppWWWOffWithNoNamesReportsRemoved|TestUpdateAppNamesOnUnroutedAppSplitsRemovedFromSaved|TestUpdateAppWWWOnSubdomainSaysNoPartner|TestUpdateAppRemovingDomainDoesNotPrintBlankDomain|TestUpdateAppDomainMoveReportsRedirects|TestPartner|TestNames|TestConflict|TestReconcileAddsRedirectRoutes|TestReconcileSkipsOnlyTheCollidingRedirect|TestReconcileSkipsPartnerOfAnotherApp|TestReconcileCarriesLiveRedirectsOfSkippedApp|TestReconcileDropsRemovedRedirectOfSkippedApp|TestReconcileCarriesOnlyStillDesiredRedirectsOfSkippedApp|TestReconcileDoesNotCarryRedirectToOldDomainOfSkippedApp|TestRepairReportsSkippedRedirects|TestWarnSkippedRedirectsLogsEachNameInOrder|TestRolloutLogsSkippedRedirects|TestBuildConfigEmitsRedirectRoute|TestBuildConfigPlacesRedirectsAfterDomainRoutes|TestBuildConfigRejectsRedirectNameClashes|TestFixedConfigRejectsRedirectToUnroutedDomain|TestFixedConfigRejectsRedirectFromRoutedDomain|TestFixedConfigRejectsDuplicateRedirect|TestFixedConfigRejectsTooManyRedirects|TestFixedConfigRejectsInvalidRedirectName|TestSafeRoutesRoundTripsRedirects|TestSafeRoutesRejectsTamperedRedirect|TestRedirectOfRequiresExactLocationTemplate|TestSafeRoutesRejectsRedirectWithExtraHandler|TestRouteSetWithoutRedirectsKeepsWireFormat|TestLiveRedirects|TestLiveRedirectsIgnoresMalformedRoutes|TestVerifyAppliedDetectsRedirectTargetDrift|TestCreateAppStoresWWWAndRedirects|TestMigrationLeavesExistingAppsWithoutWWW|TestCreateAppRejectsNameUsedAsPartner|TestCreateAppRejectsRedirectThatIsAnotherDomain|TestCreateAppKeepsDomainTaken|TestUpdateAppMovesPartnerWithDomain|TestUpdateAppRejectsPartnerConflict|TestUpdateAppRedirectAddRemove|TestUpdateAppRejectsDomainMoveWhosePartnerIsTaken|TestUpdateAppUnrelatedChangeIgnoresExistingNameConflict|TestActiveDeploymentsCarryNames|TestBrokerLoadsTypedRedirectRoute|TestBrokerRejectsUntypedRedirects|TestBrokerRejectsTamperedLiveRedirect|TestMigration0005CarriesExistingDeploymentsForward)$'
FILES=("$ROUTES" "$CONFIG" "$RECON" "$NAMES" "$SNAMES" "$MIG" "$VALID")

BAKDIR=$(mktemp -d)
bak() { printf '%s/%s' "$BAKDIR" "${1//\//__}"; }
for f in "${FILES[@]}"; do cp "$f" "$(bak "$f")"; done
restore() { for f in "${FILES[@]}"; do cp "$(bak "$f")" "$f"; done; }
trap restore EXIT

fail=0

# ── Kapsama denetimi ────────────────────────────────────────────────
#
# 1) OWNED dosyalardaki her Test* işlevi TESTS düzenine uymalı ve
#    `go test -list` çıktısında görünmeli.
# 2) TESTS'teki her ad `go test -list` çıktısında olmalı: değiştirilmiş
#    dosyalardaki (guard_test.go, migration0005_test.go) adları da kapsar ve
#    yeniden adlandırmayı yakalar. build/caddy ayrı modüldür.
owned_tests=$(for f in "${OWNED[@]}"; do sed -n 's/^func \(Test[A-Za-z0-9_]*\)(.*/\1/p' "$f"; done | sort -u)
if [[ -z "$owned_tests" ]]; then
    echo "!! KAPSAMA DENETİMİ: OWNED dosyalarda test bulunamadı — dosya adları değişmiş olabilir"
    exit 1
fi
listed=$(go test "${PKGS[@]}" -list '.*' 2>/dev/null | sed -n 's/^\(Test[A-Za-z0-9_]*\)$/\1/p' | sort -u)
if [[ -z "$listed" ]]; then
    echo "!! KAPSAMA DENETİMİ: go test -list çıktısı boş — ölçüm YAPILMADI"
    exit 1
fi
uncovered=0
while IFS= read -r t; do
    if ! grep -qx "$t" <<<"$listed"; then
        echo "!! KAPSAMA: $t go test -list çıktısında yok (koşmuyor)"
        uncovered=1
    fi
    if ! grep -Eq "$TESTS" <<<"$t"; then
        echo "!! KAPSAMA: $t TESTS düzenine girmiyor — mutantlar bu testle ölçülmüyor"
        uncovered=1
    fi
done <<<"$owned_tests"
pattern_names=$(sed -e 's/^\^(//' -e 's/)\$$//' <<<"$TESTS" | tr '|' '\n')
while IFS= read -r t; do
    if ! grep -qx "$t" <<<"$listed"; then
        echo "!! KAPSAMA: TESTS içindeki $t go test -list çıktısında yok (silinmiş ya da yeniden adlandırılmış)"
        uncovered=1
    fi
done <<<"$pattern_names"
if [[ $uncovered -ne 0 ]]; then
    exit 1
fi
echo "kapsama: $(wc -l <<<"$owned_tests") dosya testi + $(wc -l <<<"$pattern_names") TESTS adı, hepsi tutarlı"

# mutate_in <dosya> <ad> <python-ifadesi>
mutate_in() {
    local file="$1" name="$2" expr="$3"
    restore
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
    # "yakalandı" diye okur (K-096).
    local build_out
    if ! build_out=$(go test "${PKGS[@]}" -run '^$' -count=1 2>&1); then
        echo "  !! MUTANT DERLENMİYOR: $name — ölçüm YAPILMADI,"
        echo "     mutasyon derlenebilir olacak şekilde yazılmalı. Derleyici:"
        echo "$build_out" | grep -vE '^(#|FAIL|ok)' | head -3 | sed 's/^/       /'
        fail=1
        return
    fi

    if go test "${PKGS[@]}" -run "$TESTS" -count=1 -timeout 120s >/dev/null 2>&1; then
        echo "  KIRMIZI OLMADI: $name"
        fail=1
    else
        echo "  yakalandı: $name"
    fi
}

# Taban YEŞİL olmalı: kırmızı bir taban her mutantı "yakalandı" gösterirdi.
if ! go test "${PKGS[@]}" -run "$TESTS" -count=1 -timeout 120s >/dev/null 2>&1; then
    echo "!! TABAN KIRMIZI: mutasyonsuz kodda testler düşüyor — ölçüm YAPILMADI"
    exit 1
fi

echo "== Aracı: FixedConfig yönlendirme denetimi =="

mutate_in "$ROUTES" "yönlendirme hedefi kümede aranmıyor" \
    "s=s.replace('\t\tidx, ok := routeIndex[rd.To]\n\t\tif !ok {','\t\tidx, ok := routeIndex[rd.To]\n\t\tif !ok && false {',1)"

mutate_in "$ROUTES" "yönlendirme adı domainle çakışabiliyor" \
    "s=s.replace('; routed {','; routed && false {',1)"

mutate_in "$ROUTES" "yönlendirme kaynağı tekrar edebiliyor" \
    "s=s.replace('\t\tif seenFrom[rd.From] {','\t\tif seenFrom[rd.From] && false {',1)"

mutate_in "$ROUTES" "yönlendirme sınırı yok" \
    "s=s.replace('if len(set.Redirects) > MaxRedirects {','if len(set.Redirects) > MaxRedirects*1000 {',1)"

echo "== Aracı: SafeRoutes Location şablonu =="

mutate_in "$ROUTES" "Location şablonu gevşek (önek)" \
    "s=s.replace(' || !strings.HasPrefix(loc[0], prefix) ||',' ||',1)"

mutate_in "$ROUTES" "Location şablonu gevşek (sonek)" \
    "s=s.replace(' || !strings.HasSuffix(loc[0], suffix) {',' {',1)"

echo "== Yapılandırma =="

mutate_in "$CONFIG" "yönlendirme durum kodu 302" \
    "s=s.replace('const redirectStatus = 308','const redirectStatus = 302',1)"

echo "== Uzlaştırma =="

mutate_in "$RECON" "çakışan yönlendirme bütün kümeyi düşürüyor" \
    "s=s.replace('\tcase isDomain:\n','\tcase isDomain && false:\n',1)"

mutate_in "$RECON" "aynı ad iki hedefe yönlenebiliyor" \
    "s=s.replace('case p.redirectTo[name] != \"\" && p.redirectTo[name] != domain:','case false && p.redirectTo[name] != \"\" && p.redirectTo[name] != domain:',1)"

mutate_in "$RECON" "atlanan uygulamanın yönlendirmesi taşınmıyor" \
    "s=s.replace('Upstreams: ups, Redirects: redirects})','Upstreams: ups})',1)"

# -no-www atlanan uygulamada "kaldırıldı" der; süzgeç yoksa 308 canlıda kalır.
mutate_in "$RECON" "atlanan uygulamanın kaldırılan yönlendirmesi taşınıyor" \
    "s=s.replace('if slices.Contains(desired, name) && p.acceptRedirect(name, domain) {','if (slices.Contains(desired, name) || true) && p.acceptRedirect(name, domain) {',1)"

echo "== Ad türetme =="

mutate_in "$NAMES" "www eşi alt ada da türetiliyor" \
    "s=s.replace('\tdefault:\n\t\treturn \"\"\n','\tdefault:\n\t\tpartner = \"www.\" + domain\n',1)"

mutate_in "$NAMES" "kökün eşi türetilmiyor" \
    "s=s.replace('\tcase base:\n','\tcase \"-\":\n',1)"

mutate_in "$NAMES" "www'nin eşi (kök) türetilmiyor" \
    "s=s.replace('\tcase \"www.\" + base:\n','\tcase \"-\":\n',1)"

echo "== Depo =="

mutate_in "$SNAMES" "ad çakışması denetlenmiyor" \
    "s=s.replace('appnames.Conflict(c, others); found {','appnames.Conflict(c, others); found && false {',1)"

mutate_in "$MIG" "mevcut uygulamalar www ile başlıyor" \
    "s=s.replace('INTEGER NOT NULL DEFAULT 0 CHECK (www','INTEGER NOT NULL DEFAULT 1 CHECK (www',1)"

echo "== API =="

mutate_in "$VALID" "yönlendirme sınırı 99" \
    "s=s.replace('const maxRedirects = 8','const maxRedirects = 99',1)"

restore
# Geri yükleme kanıtlanıyor: her dosya kendi yedeğiyle bayt bayt aynı ve
# hiçbir yedek bir başkasının yerine geçmemiş.
for f in "${FILES[@]}"; do
    if ! cmp -s "$f" "$(bak "$f")"; then
        echo "!! GERİ YÜKLEME BOZUK: $f yedeğiyle aynı değil"
        exit 1
    fi
done
if [[ $(for f in "${FILES[@]}"; do sha256sum < "$f"; done | sort -u | wc -l) -ne ${#FILES[@]} ]]; then
    echo "!! İki dosya aynı içeriğe sahip: yedekler birbirine karışmış olabilir"
    exit 1
fi

if [[ $fail -ne 0 ]]; then
    echo
    echo "En az bir mutasyon yakalanmadı — testler iddia ettikleri şeyi korumuyor."
    exit 1
fi

echo
echo "Bütün mutasyonlar yakalandı."
