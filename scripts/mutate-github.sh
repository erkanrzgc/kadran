#!/usr/bin/env bash
# GitHub cihaz akışı (K-167) testlerinin GERÇEKTEN bir şey koruduğunu sınar.
# Her mutasyon korunan bir özelliği KASTEN bozar; adı yazılı test kırmızıya
# dönmezse o özellik korunmuyor demektir.
#
# ── Neyin bozulması EN PAHALI ───────────────────────────────────────
#
#   - Jeton sızıntısı: hata metnine gövde girerse jeton/imzalı adres günlüğe
#     ve istemciye gider; yönlendirmede Authorization taşınırsa kalıcı jeton
#     codeload'a gider.
#   - Yenileme jetonu tek kullanımlık: yeni çift diske yazılmadan kullanılırsa
#     çökme bağlantıyı sonsuza dek öldürür; kilit kalkarsa iki yenileme yarışır.
#   - Karar yolu: `private:false` tar yoluna girerse herkese açık depo
#     jetonla çekilir; tar yolu hatası anonim yola düşerse özel depo sessizce
#     "bulunamadı" olur; karardan ÖNCEKİ hata derlemeyi düşürürse kadran-git
#     çökünce herkese açık derlemeler de durur.
#   - Soket politikaları ve birim çiti: admin soketi root'a, fetch soketi
#     kadran'a açılırsa ayrıcalık ayrımı çöker; kadran-git root'ta çalışırsa ya
#     da IPAddressDeny düşerse jeton tutan süreç metadata servisine görür.
#
# Soketli exec testleri (mutant 16) yalnız Linux'ta koşar. Linux'ta doğrudan
# `go test`, başka yerde test ikilisi GOOS=linux derlenip WSL'de koşturulur.
# Dağıtım KADRAN_WSL_DISTRO ile seçilir (varsayılan Ubuntu-24.04).
#
# kapsam: internal/github cmd/kadran-git internal/exec internal/api internal/gitclient cmd/kadran deploy/systemd
set -uo pipefail

cd "$(dirname "$0")/.."

FILES=(
    internal/github/archive.go
    internal/github/branch.go
    internal/github/client.go
    internal/github/devicecode.go
    internal/github/repocoverage.go
    internal/github/token.go
    cmd/kadran-git/main.go
    cmd/kadran-git/handlers.go
    internal/gitclient/client.go
    internal/exec/gitfetch.go
    internal/exec/container.go
    internal/exec/privategit.go
    internal/api/github.go
    internal/api/authz.go
    cmd/kadran/github.go
    deploy/systemd/kadran-git.service
    deploy/systemd/kadran-git-admin.socket
    deploy/systemd/kadran-git-fetch.socket
)
BAKDIR=$(mktemp -d)
bak() { printf '%s/%s' "$BAKDIR" "${1//\//__}"; }
for f in "${FILES[@]}"; do cp "$f" "$(bak "$f")"; done
restore() { for f in "${FILES[@]}"; do cp "$(bak "$f")" "$f"; done; }
trap 'restore; rm -rf "$BAKDIR"' EXIT
trap 'exit 130' INT TERM

fail=0

# Linux ikilisini Linux dışında koşturan WSL dağıtımı.
WSL_DISTRO="${KADRAN_WSL_DISTRO:-Ubuntu-24.04}"

# run_tests <paket> <desen> <mod> [go test bayrakları…]
#   mod=native: bu makinede; mod=linux: Linux ikilisi (Linux değilse WSL'de).
run_tests() {
    local pkg="$1" want="$2" mode="$3"
    shift 3
    if [ "$mode" = native ] || [ "$(uname -s)" = Linux ]; then
        go test "$pkg" -run "$want" -count=1 "$@"
        return
    fi
    local bin="$BAKDIR/linux.test" wbin wdir
    GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go test -c -o "$bin" "$pkg" || return 2
    wbin=$(cygpath -m "$bin" | sed -E 's#^([A-Za-z]):#/mnt/\L\1#')
    wdir=$(cygpath -m "$PWD/$pkg" | sed -E 's#^([A-Za-z]):#/mnt/\L\1#')
    # go test bayrakları -test. önekiyle ikiliye geçer.
    local args=() a
    for a in "$@"; do args+=("-test.${a#-}"); done
    MSYS_NO_PATHCONV=1 wsl.exe -d "$WSL_DISTRO" --cd "$wdir" -- "$wbin" -test.run "$want" -test.count=1 "${args[@]}"
}

# Her farklı test komutu, ilk mutantından önce bir kez mutasyonsuz kodda
# koşturulur: düşen taban her mutantı "yakalandı" gösterirdi.
declare -A TABAN=()
taban_yesil() {
    local key="$*"
    [[ -n "${TABAN[$key]:-}" ]] && return 0
    restore
    if [ "$3" = linux ] && [ "$(uname -s)" != Linux ] &&
        ! MSYS_NO_PATHCONV=1 wsl.exe -d "$WSL_DISTRO" -- true >/dev/null 2>&1; then
        echo "!! WSL dağıtımı '$WSL_DISTRO' açılamadı: Linux testleri için KADRAN_WSL_DISTRO ile var olan bir dağıtım seç — ölçüm YAPILMADI"
        exit 1
    fi
    if ! run_tests "$@" >/dev/null 2>&1; then
        echo "!! TABAN KIRMIZI: mutasyonsuz kodda '$key' düşüyor — ölçüm YAPILMADI"
        exit 1
    fi
    TABAN[$key]=1
}

# mutate <dosya> <paket> <test-deseni> <mod> <ad> <python-ifadesi> [go test bayrakları…]
mutate() {
    local file="$1" pkg="$2" want="$3" mode="$4" name="$5" expr="$6"
    shift 6
    restore
    taban_yesil "$pkg" "$want" "$mode" "$@"
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

    # ── MUTANT DERLENMELİ (K-096) ───────────────────────
    local build_out
    if ! build_out=$(go test "$pkg" -run '^$' -count=1 2>&1); then
        echo "  !! MUTANT DERLENMİYOR: $name — ölçüm YAPILMADI. Derleyici:"
        echo "$build_out" | grep -vE '^(#|FAIL|ok)' | head -3 | sed 's/^/       /'
        fail=1
        return
    fi

    if run_tests "$pkg" "$want" "$mode" "$@" >/dev/null 2>&1; then
        echo "  KIRMIZI OLMADI: $name"
        fail=1
    else
        echo "  yakalandı: $name"
    fi
}

GH=./internal/github/
EX=./internal/exec/
API=./internal/api/
GIT=./cmd/kadran-git/

echo "== Cihaz akışı ve jeton deposu =="
# 1. TestSlowDownAddsFiveSeconds
mutate internal/github/devicecode.go "$GH" 'TestSlowDownAddsFiveSeconds' native "slow_down aralığa 5 sn eklemiyor" \
    "s=s.replace('next := interval + slowDownStep','next := interval',1)"
# 2. TestRefreshPersistsBeforeUse
mutate internal/github/token.go "$GH" 'TestRefreshPersistsBeforeUse' native "yenilenen jeton diske yazılmadan kullanılıyor" \
    "s=s.replace('if err := saveStateFn(t.path, next); err != nil {','if err := error(nil); err != nil {',1)"
# 3. TestRefreshIsSingleFlight, TestForceRefreshIsSingleFlightAcross401s (yarış: -race)
mutate internal/github/token.go "$GH" 'TestRefreshIsSingleFlight|TestForceRefreshIsSingleFlightAcross401s' native "Token() yenileme kilidi kaldırıldı" \
    "import re; s=_S(re.sub(r'(func \(t \*tokens\) Token\(.*?\n\t//[^\n]*\n)\tt\.mu\.Lock\(\)\n\tdefer t\.mu\.Unlock\(\)\n', r'\1', str(s), count=1, flags=re.S))" -race

echo "== Karar yolu: tar yalnız private:true için =="
# 4. TestArchiveDecisionUsesPrivateFlag
mutate internal/github/repocoverage.go "$GH" 'TestArchiveDecisionUsesPrivateFlag' native "private:false depo tar yoluna gidiyor" \
    "s=s.replace('case private:','case private || true:',1)"
# 5. TestPrivateArchiveErrorFailsBuildWithItsOwnText (Windows'ta da koşar)
mutate internal/exec/container.go "$EX" 'TestPrivateArchiveErrorFailsBuildWithItsOwnText' native "tar yolu hatası anonim yola düşüyor" \
    "s=s.replace('FetchArchiveResponse_DECISION_NOT_COVERED, err','FetchArchiveResponse_DECISION_NOT_COVERED, nil',1)"

echo "== Arşiv indirme: yönlendirme ve jeton =="
# 6. TestTarballRejectsOtherHostRedirect, TestRedirectRules
mutate internal/github/archive.go "$GH" 'TestTarballRejectsOtherHostRedirect|TestRedirectRules' native "yönlendirme host denetimi kaldırıldı" \
    "s=s.replace(' || u.Hostname() != \"codeload.github.com\"','',1)"
# 7. TestTarballFollowsOnlyCodeloadRedirect
mutate internal/github/archive.go "$GH" 'TestTarballFollowsOnlyCodeloadRedirect' native "imzalı adres isteğinde Authorization taşınıyor" \
    "s=s.replace('req.Header.Set(\"Accept\", \"application/vnd.github+json\")'+chr(10)+chr(9)+'return c.send(req)','req.Header.Set(\"Accept\", \"application/vnd.github+json\")'+chr(10)+chr(9)+'req.Header.Set(\"Authorization\", \"Bearer leaked\")'+chr(10)+chr(9)+'return c.send(req)',1)"

echo "== Soket politikaları =="
# 8. TestAdminPolicyAdmitsOnlyTheAdminUser
mutate cmd/kadran-git/main.go "$GIT" 'TestAdminPolicyAdmitsOnlyTheAdminUser' native "admin politikası root'a açık" \
    "s=s.replace('AllowUIDs: []uint32{adminUID}}','AllowUIDs: []uint32{adminUID, 0}}',1)"
# 9. TestFetchPolicyAdmitsOnlyRoot
mutate cmd/kadran-git/main.go "$GIT" 'TestFetchPolicyAdmitsOnlyRoot' native "fetch politikası kadran kullanıcısına açık" \
    "s=s.replace('AllowUIDs: []uint32{0}}','AllowUIDs: []uint32{0, 1001}}',1)"

echo "== Girdi doğrulama ve sızıntı =="
# 10. TestResolveBranchRejectsUnsafeNames
mutate internal/github/branch.go "$GH" 'TestResolveBranchRejectsUnsafeNames' native "dal adında .. kabul ediliyor" \
    "s=s.replace('strings.Contains(name, \"..\") || ','',1)"
# 11. TestErrorTextNeverContainsToken
mutate internal/github/client.go "$GH" 'TestErrorTextNeverContainsToken' native "hata metnine yanıt gövdesi ekleniyor" \
    "s=s.replace('fmt.Errorf(\"github: request failed (HTTP %d)\", code)','fmt.Errorf(\"github: request failed (HTTP %d) %s\", code, func() string { b, _ := io.ReadAll(resp.Body); return string(b) }())',1)"

echo "== Yetki: yalnız yönetici =="
# 12. TestDeployAllowlistIsExact, TestDeployRoleCannotUseGitHubCalls
mutate internal/api/authz.go "$API" 'TestDeployAllowlistIsExact|TestDeployRoleCannotUseGitHubCalls' native "deployAllowed'a StartGitHubConnect eklendi" \
    "s=s.replace('kadranv1.KadranService_Ping_FullMethodName: nil,','kadranv1.KadranService_Ping_FullMethodName: nil,'+chr(10)+chr(9)+'kadranv1.KadranService_StartGitHubConnect_FullMethodName: nil,',1)"

echo "== systemd birimi =="
# 13. TestGitBrokerDeniesLocalAndMetadataAddresses
mutate deploy/systemd/kadran-git.service ./internal/bootstrap/ 'TestGitBrokerDeniesLocalAndMetadataAddresses' native "birimden IPAddressDeny satırı silindi" \
    "import re; s=_S(re.sub(r'(?m)^IPAddressDeny=.*\n','',str(s)))"
# 14. TestGitBrokerRunsAsUnprivilegedUser
mutate deploy/systemd/kadran-git.service ./internal/bootstrap/ 'TestGitBrokerRunsAsUnprivilegedUser' native "birimden User= satırı silindi (kadran-git root'ta çalışır)" \
    "s=s.replace('User=kadran-git'+chr(10),'',1)"

echo "== Karardan önce herkese açık yola düşme (gitfetch) =="
# 15. TestPreDecisionProtocolViolationFallsBackToPublic (Windows'ta da koşar)
mutate internal/exec/gitfetch.go "$EX" 'TestPreDecisionProtocolViolationFallsBackToPublic' native "karardan önceki başarısızlık derlemeyi düşürüyor" \
    "s=s.replace('FetchArchiveResponse_DECISION_NOT_CONNECTED, nil, nil'+chr(10)+'}','FetchArchiveResponse_DECISION_NOT_CONNECTED, nil, errors.New(\"pre-decision failure\")'+chr(10)+'}',1)"
# 16. TestPublicBuildSurvivesUnresponsiveGitService (soketli: Linux ikilisi)
mutate internal/exec/gitfetch.go "$EX" 'TestPublicBuildSurvivesUnresponsiveGitService' linux "karar süre sınırı kaldırıldı" \
    "s=s.replace('time.AfterFunc(g.decisionTimeout, cancel)','time.AfterFunc(time.Hour, cancel)',1)" -timeout=60s

echo "== Arşiv akışı: boşta süresi ve sınırlar (Görev 8) =="
# 17. TestArchiveSlowButSteadyStreamSucceeds
mutate internal/exec/gitfetch.go "$EX" 'TestArchiveSlowButSteadyStreamSucceeds' native "parça başına boşta zamanlayıcısı sıfırlanmıyor" \
    "s=s.replace(chr(9)+chr(9)+'timer.Reset(idle)'+chr(10),'',1)"
# 18. TestDecisionTimeoutDefaultIsFiveSeconds
mutate internal/exec/gitfetch.go "$EX" 'TestDecisionTimeoutDefaultIsFiveSeconds' native "NewGitFetcher üretim chunkIdle varsayılanını düşürüyor" \
    "s=s.replace(', chunkIdle: gitChunkIdleTimeout}','}',1)"
# 19. TestRewriteRejectsSparseTotalBelowWriterCap
mutate internal/exec/privategit.go "$EX" 'TestRewriteRejectsSparseTotalBelowWriterCap' native "toplam h.Size denetimi kalktı, yalnız capWriter kaldı" \
    "s=s.replace('total > limits.expanded','false',1)"

echo "== kadrand ve istemci (Görev 9, 10) =="
# 20. TestConcurrentStatusAuditsOnce (yarış olasılıksal: 20 tekrar)
mutate internal/api/github.go "$API" 'TestConcurrentStatusAuditsOnce' native "denetim-bir-kez muteksi kaldırıldı" \
    "s=s.replace('s.githubMu.Lock()'+chr(10)+chr(9)+'defer s.githubMu.Unlock()','',1)" -count=20
# 21. TestStartRejectsForeignVerificationURI
mutate internal/api/github.go "$API" 'TestStartRejectsForeignVerificationURI' native "verification_uri önek denetimi kaldırıldı" \
    "s=s.replace('validGitHubURL(p.GetVerificationUri(), githubVerifyPref) &&','true &&',1)"
# 22. TestConnectStopsWhenFirstPollIsNotConnected
mutate cmd/kadran/github.go ./cmd/kadran/ 'TestConnectStopsWhenFirstPollIsNotConnected' native "NOT_CONNECTED sonrası yoklama sürüyor" \
    "s=s.replace('default:'+chr(10)+chr(9)+chr(9)+'return c.githubStop(msgGitHubEnded), true','default:'+chr(10)+chr(9)+chr(9)+'return 0, false',1)"

echo "== Son inceleme düzeltmeleri (gerçek GitHub, 17 Kas) =="
# 23. TestResolveBranchMissingBranchIsNotFound
mutate internal/github/branch.go "$GH" 'TestResolveBranchMissingBranchIsNotFound' native "olmayan dalın 422'si bulunamadı sayılmıyor" \
    "s=s.replace('if errors.Is(err, errUnprocessable) {','if false && errors.Is(err, errUnprocessable) {',1)"
# 24. TestFetchArchiveErrorAfterDecisionIsStatus, TestAdminMapsErrorsWithoutLeaking
mutate cmd/kadran-git/handlers.go "$GIT" 'TestFetchArchiveErrorAfterDecisionIsStatus|TestAdminMapsErrorsWithoutLeaking' native "kadran-git genel hatası yine Unavailable (kadrand 'çalışmıyor' okur)" \
    "s=s.replace('return status.Error(codes.Unknown, \"GitHub request failed\")','return status.Error(codes.Unavailable, \"GitHub request failed\")',1)"
# 25. TestUnknownFromKadranGitIsNotNotRunning, TestMapErrorTable
mutate internal/gitclient/client.go ./internal/gitclient/ 'TestUnknownFromKadranGitIsNotNotRunning|TestMapErrorTable' native "kadrand Unknown'ı genel iç hataya eşliyor" \
    "s=s.replace(chr(9)+'case codes.Unknown:'+chr(10)+chr(9)+chr(9)+'return status.Error(codes.Unavailable, MsgGitHubFailed)'+chr(10),'',1)"
# 26. TestDecideRejoinsRefreshThatSupersededItsOwn
mutate internal/github/repocoverage.go "$GH" 'TestDecideRejoinsRefreshThatSupersededItsOwn' native "yeri alınan yenilemeyi bekleyen yenisine katılmıyor (verisiz NotCovered)" \
    "s=s.replace('if s.gen != gen && s.refresh != nil {','if false && s.gen != gen && s.refresh != nil {',1)"
# 27. TestStalePublicHitRechecksInBackground
mutate internal/github/repocoverage.go "$GH" 'TestStalePublicHitRechecksInBackground' native "eski herkese açık kayıt yeniden denetlenmiyor (özele çevrilen depo görülmez)" \
    "s=s.replace(chr(9)+chr(9)+'s.recheckPublicLocked(d)'+chr(10),'',1)"
# 28. TestGitHubStatusShowsReconnectAfterDrop
mutate cmd/kadran/github.go ./cmd/kadran/ 'TestGitHubStatusShowsReconnectAfterDrop' native "yeniden bağlan uyarısı yalnız CONNECTED'da (sunucu o şekli hiç üretmez)" \
    "s=s.replace('conn.GetState() != kadranv1.GitHubConnection_STATE_CONNECTED && conn.GetReconnectRequired()','conn.GetState() == kadranv1.GitHubConnection_STATE_CONNECTED && conn.GetReconnectRequired()',1)"

restore
echo
if [ "$fail" -ne 0 ]; then
    echo "BAŞARISIZ: en az bir mutasyon yakalanmadı ya da uygulanamadı."
    exit 1
fi
echo "Bütün mutasyonlar yakalandı."
