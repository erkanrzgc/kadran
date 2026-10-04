#!/usr/bin/env bash
# Kasayı (K-123) koruyan testlerin GERÇEKTEN bir şey koruduğunu sınar.
#
# ── Neyin bozulması EN PAHALI ───────────────────────────────────────
#
# Sızıntı: executor'ın düz değeri kabul etmesi (kasa sessizce atlanır),
# bağın denetlenmemesi (ele geçirilmiş daemon bir değeri başka bir ada ya
# da uygulamaya taşıyıp açtırır), API'nin değeri döndürmesi, deponun düz
# yazması, göçten sonra düz metnin dosyada kalması. Kayıp: anahtarın
# üstüne yenisinin yazılması (bütün değerler çözülemez olur). Geri dönüş:
# root'un betiğinin veritabanındaki adlara güvenmesi.
#
# Düzenek mutate-env.sh'tan: tek eşleşme şartı (K-127), her paket/desen
# için yeşil taban, derleme kapısı (K-096; kabuk için `bash -n`). Python
# ifadeleri tırnaklı heredoc'tan geliyor: kabuk onlara dokunmuyor, Go
# kaynağındaki `\x00` gibi kaçışlar ham dizgeyle (r'…') birebir aranıyor.
set -uo pipefail

cd "$(dirname "$0")/.."

FILES=(
    internal/exec/kasa.go internal/exec/server.go internal/exec/validate.go
    internal/exec/container.go internal/store/vault.go internal/store/apps.go
    internal/store/appupdate.go internal/api/apps.go internal/api/appupdate.go
    internal/vault/vault.go internal/vault/vaultkey/vaultkey.go
    internal/bootstrap/kasa-coz.sh
)
BAKDIR=$(mktemp -d)
bak() { printf '%s/%s' "$BAKDIR" "${1//\//__}"; }
for f in "${FILES[@]}"; do cp "$f" "$(bak "$f")"; done
restore() { for f in "${FILES[@]}"; do cp "$(bak "$f")" "$f"; done; }
trap 'restore; rm -rf "$BAKDIR"' EXIT

fail=0

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

# mutate <ad> <dosya> <paket> <test-deseni>   (python ifadesi stdin'den)
mutate() {
    local name="$1" file="$2" pkg="$3" want="$4" expr
    expr="$(cat)"
    restore
    taban_yesil "$pkg" -run "$want" -count=1
    if ! MUT_FILE="$file" MUT_EXPR="$expr" python - <<'PY'
import io, os, sys
class _S(str):
    def replace(self, a, b, *r):
        # TEK eşleşme şart: aranan metin bir yorumda da geçiyorsa
        # replace(…,1) İLKİNİ, yani yorumu değiştirir (K-127).
        n = self.count(a)
        if n != 1:
            sys.stderr.write('REPLACE ' + str(n) + ' KEZ ESLESTI (1 olmali): ' + repr(a[:70]) + chr(10))
            sys.exit(8)
        return _S(str.replace(self, a, b, *r))
p = os.environ['MUT_FILE']
s = _S(io.open(p, encoding='utf-8').read())
o = s
exec(os.environ['MUT_EXPR'])
if s == o:
    sys.exit(9)
io.open(p, 'w', encoding='utf-8', newline='\n').write(s)
PY
    then
        echo "  !! MUTASYON UYGULANAMADI: $name — betik bozuk, ölçüm YAPILMADI"
        fail=1
        return
    fi

    # ── MUTANT DERLENMELİ (K-096) ───────────────────────
    # Sözdizimi bozuk bir mutant her testi düşürür ve sahte "yakalandı"
    # üretirdi. `-run '^$'` paketi ve test dosyalarını derler, test koşmaz.
    local build_out
    if ! build_out=$(bash -n internal/bootstrap/kasa-coz.sh 2>&1 && go test "$pkg" -run '^$' -count=1 2>&1); then
        echo "  !! MUTANT DERLENMİYOR: $name — ölçüm YAPILMADI. Çıktı:"
        echo "$build_out" | grep -vE '^(#|FAIL|ok)' | head -3 | sed 's/^/       /'
        fail=1
        return
    fi

    if go test "$pkg" -run "$want" -count=1 >/dev/null 2>&1; then
        echo "  KIRMIZI OLMADI: $name"
        fail=1
    else
        echo "  yakalandı: $name"
    fi
}

EX=./internal/exec/
ST=./internal/store/
AP=./internal/api/

echo "== Executor: kasa zorunlu, bağ, sınırlar =="

mutate "öneksiz (şifresiz) değer kabul ediliyor" internal/exec/kasa.go $EX \
    'TestOpenEnvRejectsPlaintext|TestContainerCreateRejectsPlaintextEnv' <<'EOF'
s = s.replace('\t\t\treturn nil, fmt.Errorf("env %q mühürlü değil (kasa zorunlu)", k)', '\t\t\tout[k] = v\n\t\t\tcontinue', 1)
EOF

mutate "ContainerCreate kasa adımını atlıyor" internal/exec/container.go $EX \
    'TestContainerCreateRejectsPlaintextEnv' <<'EOF'
s = s.replace('env, err := openEnv(s.vault, ref.GetRelease().GetAppId(), req.GetEnv())', 'env, err := req.GetEnv(), error(nil)', 1)
EOF

mutate "bağ değişken adını denetlemiyor" internal/exec/kasa.go $EX \
    'TestOpenEnvRejectsValueMovedToAnotherAppOrKey' <<'EOF'
s = s.replace(r'val, ok := strings.CutPrefix(string(plain), appID+"\x00"+k+"\x00")', 'parts := strings.SplitN(string(plain), "\\x00", 3)\n\t\tval, ok := parts[len(parts)-1], len(parts) == 3 && parts[0] == appID', 1)
EOF

mutate "bağ uygulamayı denetlemiyor" internal/exec/kasa.go $EX \
    'TestOpenEnvRejectsValueMovedToAnotherAppOrKey' <<'EOF'
s = s.replace(r'val, ok := strings.CutPrefix(string(plain), appID+"\x00"+k+"\x00")', 'parts := strings.SplitN(string(plain), "\\x00", 3)\n\t\tval, ok := parts[len(parts)-1], len(parts) == 3 && parts[1] == k', 1)
EOF

mutate "mühürlü değerin boyutu çözmeden önce sınanmıyor" internal/exec/kasa.go $EX \
    'TestOpenEnvRejectsOversizedBeforeDecoding' <<'EOF'
s = s.replace('\t\tif len(b64) > maxSealedValue {', '\t\tif false {', 1)
EOF

mutate "açılan değerler doğrulanmıyor (NUL, toplam)" internal/exec/kasa.go $EX \
    'TestOpenEnvValidatesDecryptedValues' <<'EOF'
s = s.replace('\treturn out, validateEnv(out, maxEnvBytes)', '\treturn out, nil', 1)
EOF

mutate "ham istek düz metin sınırıyla doğrulanıyor" internal/exec/validate.go $EX \
    'TestLargestLegitimateEnvPassesSealedLimits' <<'EOF'
s = s.replace('validateEnv(req.GetEnv(), maxSealedEnvBytes)', 'validateEnv(req.GetEnv(), maxEnvBytes)', 1)
EOF

mutate "kasasız executor kuruluyor" internal/exec/server.go $EX \
    'TestNewServerRequiresVault' <<'EOF'
s = s.replace('\tif opts.Vault == nil {', '\tif false {', 1)
EOF

mutate "başkalarına açık anahtar dosyası kabul ediliyor" internal/exec/kasa.go $EX \
    'TestLoadVaultIdentity' <<'EOF'
s = s.replace('if runtime.GOOS != "windows" && fi.Mode().Perm()&0o077 != 0 {', 'if runtime.GOOS == "plan9" && fi.Mode().Perm()&0o077 != 0 {', 1)
EOF

echo "== Depo: düz metin diske girmiyor, göç temizliyor =="

mutate "oluşturma değeri düz yazıyor" internal/store/apps.go $ST \
    'TestCreateAndUpdatePersistOnlySealedValues' <<'EOF'
s = s.replace('sealed, err := s.sealEnv(app.ID, app.Env)', 'sealed, err := app.Env, error(nil)', 1)
EOF

mutate "güncelleme değeri düz yazıyor" internal/store/appupdate.go $ST \
    'TestCreateAndUpdatePersistOnlySealedValues' <<'EOF'
s = s.replace('sealed, err := s.sealEnv(id, upd.Env)', 'sealed, err := upd.Env, error(nil)', 1)
EOF

mutate "kasa açılmadan değer yazılabiliyor" internal/store/vault.go $ST \
    'TestEnvWritesRequireVault' <<'EOF'
s = s.replace('\tif s.sealer == nil {\n\t\treturn nil, errors.New("kasa açılmadı: ortam değişkeni yazılamaz")', '\tif s.sealer == nil {\n\t\treturn env, nil', 1)
EOF

mutate "göçten sonra VACUUM yok (düz metin dosyada kalır)" internal/store/vault.go $ST \
    'TestEnableVaultSealsPlaintextAndScrubsFiles' <<'EOF'
s = s.replace('[]string{"VACUUM", "PRAGMA wal_checkpoint(TRUNCATE)"}', '[]string{"PRAGMA wal_checkpoint(TRUNCATE)"}', 1)
EOF

mutate "göçten sonra WAL kesilmiyor" internal/store/vault.go $ST \
    'TestEnableVaultSealsPlaintextAndScrubsFiles' <<'EOF'
s = s.replace('[]string{"VACUUM", "PRAGMA wal_checkpoint(TRUNCATE)"}', '[]string{"VACUUM"}', 1)
EOF

mutate "değişen alıcı kabul ediliyor" internal/store/vault.go $ST \
    'TestEnableVaultRefusesChangedRecipient' <<'EOF'
s = s.replace('\t\tif recipient != sealer.Recipient() {', '\t\tif false {', 1)
EOF

mutate "işaretsiz mühürlü değer ikinci kez mühürleniyor" internal/store/vault.go $ST \
    'TestEnableVaultRefusesSealedValuesWithoutMarker' <<'EOF'
s = s.replace('\t\t\tif strings.HasPrefix(v, sealedHead) {', '\t\t\tif false && strings.HasPrefix(v, sealedHead) {', 1)
EOF

mutate "kasa işareti yazılmıyor (her açılışta yeniden mühürler)" internal/store/vault.go $ST \
    'TestEnableVaultIsIdempotent' <<'EOF'
s = s.replace('`INSERT INTO env_seal (id, recipient, sealed_at) VALUES (1, ?, ?)`', '`SELECT ?, ?`', 1)
EOF

echo "== API: değer dönmüyor, güncelleme düz boyutla ölçüyor =="

mutate "API değerleri döndürüyor" internal/api/apps.go $AP \
    'TestAppEnvRoundTripsThroughProto|TestUpdateAppEnvActuallyPersists' <<'EOF'
s = s.replace('Env:            envNamesOnly(a.Env),', 'Env:            a.Env,', 1)
EOF

mutate "güncelleme mühürlü metnin boyutunu ölçüyor" internal/api/appupdate.go $AP \
    'TestUpdateAppMeasuresSealedEnvByPlainSize' <<'EOF'
s = s.replace('\t\tout[k] = strings.Repeat("x", n)', '\t\tout[k] = v + strings.Repeat("", n)', 1)
EOF

echo "== Mühür ve anahtar =="

mutate "mühür değişken adını bağlamıyor" internal/vault/vault.go ./internal/vault/ \
    'TestSealBindsAppAndKey' <<'EOF'
s = s.replace(r'io.WriteString(w, appID+"\x00"+key+"\x00"+value)', r'io.WriteString(w, appID+"\x00"+"\x00"+value)', 1)
EOF

mutate "PlainLen parça etiketini saymıyor" internal/vault/vault.go ./internal/vault/ \
    'TestPlainLenMatchesSealedValue' <<'EOF'
s = s.replace('\tsealTagLen    = 16', '\tsealTagLen    = 0', 1)
EOF

mutate "var olan anahtarın üstüne yenisi yazılıyor" internal/vault/vaultkey/vaultkey.go ./internal/vault/vaultkey/ \
    'TestInitCreatesKeyOnceAndPrintsRecipient' <<'EOF'
s = s.replace('\tif errors.Is(err, fs.ErrNotExist) {', '\tif err == nil || errors.Is(err, fs.ErrNotExist) {', 1)
s = s.replace('os.O_WRONLY|os.O_CREATE|os.O_EXCL', 'os.O_WRONLY|os.O_CREATE|os.O_TRUNC', 1)
EOF

mutate "bozuk anahtarın üstüne yenisi yazılıyor" internal/vault/vaultkey/vaultkey.go ./internal/vault/vaultkey/ \
    'TestInitDoesNotReplaceBrokenKey' <<'EOF'
s = s.replace('\tif errors.Is(err, fs.ErrNotExist) {', '\tif err != nil || errors.Is(err, fs.ErrNotExist) {', 1)
s = s.replace('os.O_WRONLY|os.O_CREATE|os.O_EXCL', 'os.O_WRONLY|os.O_CREATE|os.O_TRUNC', 1)
EOF

echo "== Geri dönüş betiği (root, veritabanına güvenmiyor) =="

mutate "veritabanındaki değişken adı SQL'e sınanmadan giriyor" internal/bootstrap/kasa-coz.sh ./internal/bootstrap/ \
    'TestKasaCozRejectsHostileNamesFromDatabase' <<'EOF'
s = s.replace('[[ "$key" =~ ^[A-Za-z_][A-Za-z0-9_]*$ ]] || die', 'true || die', 1)
EOF

mutate "geri dönüş bağı denetlemiyor" internal/bootstrap/kasa-coz.sh ./internal/bootstrap/ \
    'TestKasaCozRejectsMovedValue' <<'EOF'
s = s.replace('[[ "$acik" == "$bag"* ]] || die', 'true || die', 1)
EOF

mutate "od -v yok (tekrar eden satırlar '*' ile kısalır)" internal/bootstrap/kasa-coz.sh ./internal/bootstrap/ \
    'TestKasaCozRoundTrip' <<'EOF'
s = s.replace("hex() { od -An -v -tx1 | tr -d ' \\n'; }", "hex() { od -An -tx1 | tr -d ' \\n'; }", 1)
EOF

echo
if [[ "$fail" -ne 0 ]]; then
    echo "En az bir mutasyon yakalanmadı — denetimler iddia ettikleri şeyi korumuyor."
    exit 1
fi
echo "Bütün mutasyonlar yakalandı."
