#!/usr/bin/env bash
# İki aşamalı girişi (TOTP, K-155) koruyan testlerin GERÇEKTEN bir şey
# koruduğunu sınar.
#
# Her mutasyon korunan bir özelliği KASTEN bozar; test kırmızıya dönmezse o
# özellik korunmuyor demektir. Düzenek mutate-proxyguard.sh'tan: tek eşleşme
# şartı (K-127), yeşil taban şartı, derleme kapısı (K-096).
#
# Windows'ta da koşar: bu testler unix soketi istemez.
set -uo pipefail

cd "$(dirname "$0")/.."
VRF=internal/totp/totp.go
REC=internal/totp/recovery.go
STO=internal/store/totp.go
SES=internal/api/session.go
HND=internal/api/totp.go
CLI=internal/client/session.go
FILES="$VRF $REC $STO $SES $HND $CLI"
PKGS="./internal/totp/ ./internal/store/ ./internal/api/"

BAK=$(mktemp -d)
for f in $FILES; do mkdir -p "$BAK/$(dirname "$f")"; cp "$f" "$BAK/$f"; done
restore() { for f in $FILES; do cp "$BAK/$f" "$f"; done; }
trap 'restore; rm -rf "$BAK"' EXIT

fail=0

# Taban YEŞİL olmalı: kırmızı bir taban her mutantı "yakalandı" gösterirdi.
restore
# shellcheck disable=SC2086
if ! go test $PKGS ./internal/client/ -count=1 >/dev/null 2>&1; then
    echo "!! TABAN KIRMIZI: mutasyonsuz kodda testler düşüyor — ölçüm YAPILMADI"
    exit 1
fi

# mutate <ad> <dosya> <python-ifadesi> [paketler]
mutate() {
    local name="$1" file="$2" expr="$3" pkgs="${4:-$PKGS}"
    restore
    if ! python -c "
import io,sys
class _S(str):
    def replace(self,a,b,*r):
        # TEK eşleşme şart (K-127).
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

    # MUTANT DERLENMELİ (K-096): derlenmeyen mutant sahte "yakalandı" üretir.
    local build_out
    # shellcheck disable=SC2086
    if ! build_out=$(go test $pkgs -run '^$' -count=1 2>&1); then
        echo "  !! MUTANT DERLENMİYOR: $name — ölçüm YAPILMADI. Derleyici:"
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

echo "== Kod doğrulama =="

mutate "pencere ±1 yerine ±2 adım" "$VRF" \
    "s=s.replace('for step := s - 1; step <= s+1; step++ {','for step := s - 2; step <= s+2; step++ {',1)"

# Tekrar iki katmanla kapalı: Verify son kabul edilen adımı atlıyor ve
# AcceptTOTPStep koşullu güncelliyor. API testi tek katmanlı mutantta yeşil
# kalıyor (öbür katman tutuyor, ölçüldü); her katmanı kendi testi koruyor.
mutate "Verify son kabul edilen adımı yok sayıyor" "$VRF" \
    "s=s.replace('\t\tif step <= lastStep {\n\t\t\tcontinue\n\t\t}\n','',1)"

mutate "kabul edilen adım tekrar kabul ediliyor (AcceptTOTPStep hep doğru)" "$STO" \
    "s=s.replace('WHERE id = 1 AND secret IS NOT NULL AND last_step < ?\`, step, step)','WHERE id = 1 AND secret IS NOT NULL AND ? > -1\`, step, step)',1)"

echo "== Oturum kapısı (CheckSession) =="

mutate "jeton başka bir anahtarla da geçiyor (parmak izi karşılaştırılmıyor)" "$SES" \
    "s=s.replace('if err != nil || c.Fingerprint == \"\" || c.Fingerprint != id.Fingerprint {','if err != nil {',1)"

mutate "eski kuşağın jetonu geçiyor (kuşak denetimi yok)" "$SES" \
    "s=s.replace('if c.Generation != st.Generation || !s.now().Before(c.Expires) {','if !s.now().Before(c.Expires) {',1)"

mutate "süresi dolan jeton geçiyor (süre denetimi yok)" "$SES" \
    "s=s.replace('if c.Generation != st.Generation || !s.now().Before(c.Expires) {','if c.Generation != st.Generation {',1)"

mutate "yönetici de jetonsuz geçiyor (dağıtım muafiyeti herkese)" "$SES" \
    "s=s.replace('if id.Role != connproto.RoleAdmin || sessionExempt[method] {','if true {',1)"

mutate "dağıtım rolü (CI) jeton istiyor" "$SES" \
    "s=s.replace('if id.Role != connproto.RoleAdmin || sessionExempt[method] {','if sessionExempt[method] {',1)"

mutate "parmak izsiz yöneticiye login döngüsü (SSH yönlendirmesi yok)" "$SES" \
    "s=s.replace('\tif id.Fingerprint == \"\" {\n\t\treturn sessionDenied(id, method, \"no key identity\", msgSessionNoKey)\n\t}\n','',1)"

echo "== Kilit =="

mutate "kilitli anahtar yine deniyor (kilit denetimi yok)" "$STO" \
    "s=s.replace('\tif until > now.Unix() {','\tif false && until > now.Unix() {',1)"

mutate "Login denemeyi kod denenmeden önce saymıyor" "$HND" \
    "s=s.replace('attempt, err := s.reserveAttempt(ctx, \"auth.login\", fp, now)\n\tif err != nil {\n\t\treturn nil, err\n\t}','attempt := store.LoginFailure{}',1)"

mutate "onay kodu kilide sayılmıyor" "$HND" \
    "s=s.replace('\tattempt, err := s.reserveAttempt(ctx, \"totp.confirm\", fp, now)\n\tif err != nil {\n\t\treturn nil, err\n\t}','\tattempt := store.LoginFailure{}',1)"

mutate "başarılı giriş sayacı ve kilit alarmını silmiyor" "$HND" \
    "s=s.replace('\tif err := s.clearAttempts(ctx, fp); err != nil {\n\t\treturn nil, err\n\t}\n\tif err := s.recordAction(ctx, \"auth.login\"','\tif err := s.recordAction(ctx, \"auth.login\"',1)"

echo "== Kurtarma kodu =="

mutate "kurtarma kodu biçimi normalleştirilmiyor (küçük harf/tire)" "$REC" \
    "s=s.replace('sha256.Sum256([]byte(strings.ToUpper(recoveryCleaner.Replace(code))))','sha256.Sum256([]byte(code))',1)"

mutate "kurtarma kodu ikinci kez kullanılabiliyor" "$STO" \
    "s=s.replace('WHERE hash = ? AND used_at IS NULL\`','WHERE hash = ?\`',1)"

echo "== Adım yükseltme (kapatma, yeniden kurulum) =="

mutate "kapatma taze kod istemiyor" "$HND" \
    "s=s.replace('\tif err := s.stepUp(ctx, \"totp.disable\", msgDisableCode, fp, st, req.GetCode(), s.now()); err != nil {\n\t\treturn nil, err\n\t}\n','',1)"

mutate "yeniden kurulum güncel kod istemiyor" "$HND" \
    "s=s.replace('\tif st.Enabled() {\n\t\tif err := s.stepUp(ctx, \"totp.enroll\"','\tif false && st.Enabled() {\n\t\tif err := s.stepUp(ctx, \"totp.enroll\"',1)"

echo "== İstemci =="

mutate "sarılmış oturum hatası tanınmıyor (login ipucu kaybolur)" "$CLI" \
    "s=s.replace('\tvar gs interface{ GRPCStatus() *status.Status }\n\tif !errors.As(err, &gs) {\n\t\treturn false\n\t}\n\tst := gs.GRPCStatus()','\tst, ok := status.FromError(err)\n\tif !ok || err == nil {\n\t\treturn false\n\t}',1)" \
    "./internal/client/"

restore
if [[ $fail -ne 0 ]]; then
    echo
    echo "En az bir mutasyon yakalanmadı — testler iddia ettikleri şeyi korumuyor."
    exit 1
fi

echo
echo "Bütün mutasyonlar yakalandı."
