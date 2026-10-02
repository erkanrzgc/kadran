#!/usr/bin/env bash
# `kadran key`i koruyan testlerin GERÇEKTEN bir şey koruduğunu sınar
# (K-131: dağıtım anahtarlarını authorized_keys'e ekleme/listeleme/silme).
#
# Uzak betiğin mutantlarını yalnızca Linux'taki gerçek-bash testleri
# (keys_linux_test.go) yakalıyor; bu betik CI'da Linux'ta koşuyor.
#
# ── Neyin bozulması EN PAHALI ───────────────────────────────────────
#
# authorized_keys'e kısıtsız bir satır düşmesi: panely-client'a kabuk.
# Hemen arkasından aynı anahtarın iki satırda olması (sshd İLK eşleşeni
# kullanır, rol satır sırasına kalır) ve son yönetici satırının silinmesi.
#
# Düzenek mutate-authz.sh'tan: tek eşleşme şartı (K-127), yeşil taban
# şartı, derleme kapısı (K-096), tam yoldan yedek.
set -uo pipefail

cd "$(dirname "$0")/.."
KEYS=internal/bootstrap/keys.go
CLI=cmd/kadran/key.go
PKGS=(./internal/bootstrap/ ./cmd/kadran/)
TESTS='TestKey|TestRemote|TestParsePublicKey|TestDeployLine|TestParseAuthorizedKeys|TestClientPaths'
FILES=("$KEYS" "$CLI")

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

    # ── MUTANT DERLENMELİ (K-096) ───────────────────────
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

echo "== Uzak betik (sunucu istemciye GÜVENMİYOR) =="

mutate_in "$KEYS" "satır sunucuda doğrulanmıyor" \
    "s=s.replace('[[ \"\$line\" =~ \$re ]] || {','[[ \"\$line\" =~ \$re ]] || true || {',1)"

mutate_in "$KEYS" "satırın gövdesi denetlenen gövde mi bakılmıyor" \
    "s=s.replace('*\",restrict \$body\"|*\",restrict \$body \"*) ;;','*) ;;',1)"

# sshd İLK eşleşen satırı kullanır: rol satır sırasına kalırdı.
mutate_in "$KEYS" "aynı anahtar ikinci kez ekleniyor" \
    "s=s.replace('if grep -qF -- \"\$body\" \"\$f\"; then echo \"bu anahtar zaten kayıtlı\"','if false; then echo \"bu anahtar zaten kayıtlı\"',1)"

mutate_in "$KEYS" "son yönetici satırı silinebiliyor" \
    "s=s.replace('grep -qE \"\$admin_re\" \"\$tmp\" || {','true || {',1)"

mutate_in "$KEYS" "silmede olmayan anahtar sessizce geçiyor" \
    "s=s.replace('grep -qF -- \"\$body\" \"\$f\" || { echo \"anahtar bulunamadı\"','true || { echo \"anahtar bulunamadı\"',1)"

echo "== Açık anahtar doğrulaması =="

mutate_in "$KEYS" "gövdenin türü denetlenmiyor" \
    "s=s.replace('if uint64(n) > uint64(len(blob)-4) || string(blob[4:4+n]) != typ {','if uint64(n) > uint64(len(blob)-4) {',1)"

mutate_in "$KEYS" "kesik gövde kabul ediliyor" \
    "s=s.replace('\t\tif uint64(m) > uint64(len(rest)-4) {\n\t\t\treturn \"\", errors.New(\"anahtar gövdesi kesik\")\n\t\t}','\t\tif uint64(m) > uint64(len(rest)-4) {\n\t\t\trest, parts = nil, parts+1\n\t\t\tcontinue\n\t\t}',1)"

echo "== Satır kurma =="

mutate_in "$KEYS" "kapsam denetlenmiyor" \
    "s=s.replace('\tif err := (connproto.Identity{Role: connproto.RoleDeploy, Apps: apps}).CheckRole(); err != nil {\n\t\treturn \"\", err\n\t}\n\tcomment','\tcomment',1)"

mutate_in "$KEYS" "-name denetlenmiyor" \
    "s=s.replace('\t\tif !commentPattern.MatchString(name) {','\t\tif false && !commentPattern.MatchString(name) {',1)"

mutate_in "$KEYS" "anahtarın geçersiz yorumu satıra giriyor" \
    "s=s.replace('\tif commentPattern.MatchString(comment) {\n\t\tline +=','\tif comment != \"\" {\n\t\tline +=',1)"

echo "== Listeleme =="

mutate_in "$KEYS" "restrict'siz satır kısıtlı sayılıyor" \
    "s=s.replace('\tif !hasCommand || !restricted {','\tif !hasCommand || (!restricted && false) {',1)"

mutate_in "$KEYS" "yönetici komutu önekle eşleşiyor" \
    "s=s.replace('\tcase command == connect:','\tcase strings.HasPrefix(command, connect) && !strings.Contains(command, \"-deploy=site\"):',1)"

mutate_in "$CLI" "kısıtsız satır sayılmıyor" \
    "s=s.replace('\t\t\tunrestricted++\n','',1)"

mutate_in "$CLI" "-deploy zorunlu değil" \
    "s=s.replace('\tif *deploy == \"\" {','\tif false && *deploy == \"\" {',1)"

echo
if [[ "$fail" -ne 0 ]]; then
    echo "En az bir mutasyon yakalanmadı — testler iddia ettikleri şeyi korumuyor."
    exit 1
fi
echo "Bütün mutasyonlar yakalandı."
