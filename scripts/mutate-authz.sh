#!/usr/bin/env bash
# Yetki ayrımını koruyan testlerin GERÇEKTEN bir şey koruduğunu sınar
# (K-131: yalnızca dağıtım yapabilen anahtar).
#
# ── Neyin bozulması EN PAHALI ───────────────────────────────────────
#
# Gevşeyen her yön sessizdir: CI'a verilen bir anahtar yönetici olur ve
# hiçbir şey kırmızıya dönmez. En pahalısı akış önleyicisinin kaybolması:
# Deploy ve StreamLogs akış RPC'leri ve tekli önleyiciden geçmiyorlar.
#
# Düzenek mutate-domaincheck.sh'tan: tek eşleşme şartı (K-127), yeşil
# taban şartı, derleme kapısı (K-096), tam yoldan yedek.
set -uo pipefail

cd "$(dirname "$0")/.."
AUTHZ=internal/api/authz.go
CRED=internal/api/credentials.go
ROLE=internal/connproto/role.go
CONN=cmd/panely-connect/main.go
LOCAL=internal/client/client.go
DEPLOY=cmd/kadran/deploy.go
PKGS=(./internal/api/ ./internal/connproto/ ./cmd/panely-connect/ ./internal/client/ ./cmd/kadran/)
TESTS='TestDeployKey|TestAdminReaches|TestDeployAllowlist|TestInvalidRole|TestInterceptorRejects|TestUnaryScope|TestAppIDPatternMatches|TestParseDeployScope|TestCheckRole|TestCanDeploy|TestRoleRoundTrips|TestNoFlagMeansAdmin|TestDeployFlagSetsScope|TestBadArguments|TestIdentityCarriesRole|TestLocalIdentityIsAdmin'
FILES=("$AUTHZ" "$CRED" "$ROLE" "$CONN" "$LOCAL" "$DEPLOY")

BAKDIR=$(mktemp -d)
bak() { printf '%s/%s' "$BAKDIR" "${1//\//__}"; }
for f in "${FILES[@]}"; do cp "$f" "$(bak "$f")"; done
restore() { for f in "${FILES[@]}"; do cp "$(bak "$f")" "$f"; done; }
trap restore EXIT

fail=0

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

echo "== Kablolama =="

mutate_in "$AUTHZ" "akış önleyicisi kaydedilmiyor" \
    "s=s.replace('\t\tgrpc.ChainStreamInterceptor(AuthzStreamInterceptor()),\n','',1)"

mutate_in "$AUTHZ" "tekli yetki önleyicisi kaydedilmiyor" \
    "s=s.replace('grpc.ChainUnaryInterceptor(LoggingInterceptor(), AuthzUnaryInterceptor())','grpc.ChainUnaryInterceptor(LoggingInterceptor())',1)"

echo "== İzin listesi =="

# GetApp ortam değişkenlerinin DEĞERLERİNİ döndürüyor.
mutate_in "$AUTHZ" "GetApp dağıtım anahtarına açık" \
    "s=s.replace('\tpanelyv1.PanelyService_Ping_FullMethodName: nil,\n','\tpanelyv1.PanelyService_Ping_FullMethodName: nil,\n\tpanelyv1.PanelyService_GetApp_FullMethodName: nil,\n',1)"

mutate_in "$AUTHZ" "StreamLogs dağıtım anahtarına açık" \
    "s=s.replace('\tpanelyv1.PanelyService_Ping_FullMethodName: nil,\n','\tpanelyv1.PanelyService_Ping_FullMethodName: nil,\n\tpanelyv1.PanelyService_StreamLogs_FullMethodName: nil,\n',1)"

mutate_in "$AUTHZ" "listede olmayan yöntem dağıtım anahtarına açık" \
    "s=s.replace('return id, nil, deny(id, method, \"bu anahtar yalnızca dağıtım yapabilir\")','return id, nil, nil',1)"

mutate_in "$AUTHZ" "bilinmeyen rol önleyicide geçiyor" \
    "s=s.replace('return id, nil, deny(id, method, \"geçersiz rol\")','return id, nil, nil',1)"

echo "== Kapsam =="

mutate_in "$AUTHZ" "Deploy kapsamı denetlenmiyor" \
    "s=s.replace('return ok && id.CanDeploy(r.GetAppId())','return ok && (id.CanDeploy(r.GetAppId()) || true)',1)"

mutate_in "$AUTHZ" "akış sarmalanmıyor" \
    "s=s.replace('\t\tif check != nil {\n\t\t\tss = &scopedStream','\t\tif false && check != nil {\n\t\t\tss = &scopedStream',1)"

mutate_in "$AUTHZ" "akış mesajı denetlenmiyor" \
    "s=s.replace('\tif !s.check(s.id, m) {','\tif false && !s.check(s.id, m) {',1)"

mutate_in "$AUTHZ" "tekli kapsam denetlenmiyor" \
    "s=s.replace('if check != nil && !check(id, req) {','if false && check != nil && !check(id, req) {',1)"

echo "== Rol doğrulaması =="

mutate_in "$CRED" "el sıkışma rolü denetlemiyor" \
    "s=s.replace('if err := identity.CheckRole(); err != nil {','if err := identity.CheckRole(); false && err != nil {',1)"

# Boş rol eski bir panely-connect ya da rolü yazmayı unutan bir kod yolu.
mutate_in "$ROLE" "boş rol yönetici sayılıyor" \
    "s=s.replace('\tcase RoleAdmin:\n\t\tif len(id.Apps) != 0 {','\tcase RoleAdmin, \"\":\n\t\tif len(id.Apps) != 0 {',1)"

mutate_in "$ROLE" "yönetici kapsam taşıyabiliyor" \
    "s=s.replace('\t\tif len(id.Apps) != 0 {','\t\tif false && len(id.Apps) != 0 {',1)"

mutate_in "$ROLE" "kapsamsız dağıtım anahtarı geçiyor" \
    "s=s.replace('\tif len(apps) == 0 {','\tif false && len(apps) == 0 {',1)"

mutate_in "$ROLE" "kapsam sınırı yok" \
    "s=s.replace('\tif len(apps) > MaxDeployApps {','\tif false && len(apps) > MaxDeployApps {',1)"

# Desen aynı zamanda kabuk enjeksiyonu sınırı.
mutate_in "$ROLE" "uygulama adı deseni denetlenmiyor" \
    "s=s.replace('\t\tif !appIDPattern.MatchString(app) {','\t\tif false && !appIDPattern.MatchString(app) {',1)"

mutate_in "$ROLE" "tekrarlı kapsam geçiyor" \
    "s=s.replace('\t\tif seen[app] {','\t\tif false && seen[app] {',1)"

mutate_in "$ROLE" "dağıtım anahtarı her uygulamayı dağıtıyor" \
    "s=s.replace('return slices.Contains(id.Apps, app)','return slices.Contains(id.Apps, app) || true',1)"

mutate_in "$ROLE" "geçersiz kimlik dağıtabiliyor" \
    "s=s.replace('\tif id.CheckRole() != nil {\n\t\treturn false\n\t}','\tif false && id.CheckRole() != nil {\n\t\treturn false\n\t}',1)"

echo "== panely-connect =="

mutate_in "$CONN" "argümansız anahtar yönetici değil" \
    "s=s.replace('opts := options{role: connproto.RoleAdmin}','opts := options{}',1)"

mutate_in "$CONN" "-deploy iki kez verilebiliyor" \
    "s=s.replace('\t\t\tif deploySet {','\t\t\tif false && deploySet {',1)"

mutate_in "$CONN" "konumsal argüman kabul ediliyor" \
    "s=s.replace('\tif fs.NArg() > 0 {','\tif false && fs.NArg() > 0 {',1)"

mutate_in "$CONN" "rol önsöze yazılmıyor" \
    "s=s.replace('Role: opts.role, Apps: opts.apps}','Apps: opts.apps}',1)"

echo "== İstemciler =="

mutate_in "$LOCAL" "yerel kimlik rolsüz" \
    "s=s.replace('return connproto.Identity{Origin: \"local\", Role: connproto.RoleAdmin}','return connproto.Identity{Origin: \"local\"}',1)"

mutate_in "$DEPLOY" "dağıtım anahtarına -commit ipucu verilmiyor" \
    "s=s.replace('if status.Code(err) == codes.PermissionDenied {','if false && status.Code(err) == codes.PermissionDenied {',1)"

echo
if [[ "$fail" -ne 0 ]]; then
    echo "En az bir mutasyon yakalanmadı — testler iddia ettikleri şeyi korumuyor."
    exit 1
fi
echo "Bütün mutasyonlar yakalandı."
