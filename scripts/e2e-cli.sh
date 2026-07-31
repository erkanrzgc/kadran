#!/usr/bin/env bash
# CLI'ı gerçek bir panelyd'ye karşı uçtan uca doğrular.
#
# # Bu betik neden var?
#
# Birim testleri istemciyi sahte bir sunucuya karşı sınıyor. Sahte sunucu,
# gerçeğinin yaptığı iki şeyi YAPMIYOR: SO_PEERCRED ile çağıranı doğrulamak
# ve kimlik önsözünü okumak. İkisi de yalnızca Linux'ta çalışır.
#
# Burada gerçek panelyd başlatılıp gerçek `panely` ile konuşuluyor. Yol
# üzerindeki her halka sınanıyor: unix soketi → SO_PEERCRED → kimlik önsözü
# → gRPC → SQLite denetim zinciri.
#
# Root GEREKTİRMEZ. Kullanıcının kendi birincil grubu istemci grubu olarak
# kullanılıyor; ayrıcalık izolasyonunun root gerektiren doğrulaması ayrı
# bir betikte (scripts/e2e-executor.sh).
#
# Kullanım:
#   scripts/e2e-cli.sh

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$REPO_ROOT"

WSL_DISTRO="${PANELY_WSL_DISTRO:-Ubuntu}"
OUT_DIR="bin/linux"

echo "==> linux/amd64 binary'leri derleniyor"
mkdir -p "$OUT_DIR"
GOOS=linux GOARCH=amd64 go build -o "$OUT_DIR/panelyd" ./cmd/panelyd
GOOS=linux GOARCH=amd64 go build -o "$OUT_DIR/panely" ./cmd/panely

# Windows yolunu WSL'in gördüğü yola çevir: C:\x\y -> /mnt/c/x/y
wsl_path() {
    local win="$1"
    local drive="${win:0:1}"
    printf '/mnt/%s%s' "$(echo "$drive" | tr '[:upper:]' '[:lower:]')" "$(echo "${win:2}" | tr '\\' '/')"
}
WSL_BIN="$(wsl_path "$(cd "$OUT_DIR" && pwd -W)")"

# Uzak betik ayrı bir dosyaya yazılıyor: tırnak içinde tırnak geçirmek
# WSL çağrılarında düzenli olarak bozuluyor.
RUNNER="$OUT_DIR/e2e-runner.sh"
cat > "$RUNNER" <<'REMOTE'
set -uo pipefail

BIN="$1"
WORK="$(mktemp -d /tmp/panely-e2e.XXXXXX)"
SOCK="$WORK/api.sock"
DB="$WORK/panely.db"
LOG="$WORK/panelyd.log"

fail=0
check() {
    local name="$1" want="$2" got="$3"
    if [[ "$want" == "$got" ]]; then
        printf '  ✓ %s\n' "$name"
    else
        printf '  ✗ %s — beklenen %q, alınan %q\n' "$name" "$want" "$got" >&2
        fail=1
    fi
}
contains() {
    local name="$1" needle="$2" haystack="$3"
    if [[ "$haystack" == *"$needle"* ]]; then
        printf '  ✓ %s\n' "$name"
    else
        printf '  ✗ %s — %q çıktıda yok\n' "$name" "$needle" >&2
        printf '    çıktı: %s\n' "$haystack" >&2
        fail=1
    fi
}

cleanup() {
    [[ -n "${DAEMON_PID:-}" ]] && kill "$DAEMON_PID" 2>/dev/null || true
    wait "${DAEMON_PID:-}" 2>/dev/null || true
    rm -rf "$WORK"
}
trap cleanup EXIT

chmod +x "$BIN/panelyd" "$BIN/panely"

echo "==> panelyd başlatılıyor (kullanıcı: $(id -un), grup: $(id -gn))"
# İstemci grubu olarak kullanıcının kendi birincil grubu veriliyor:
# SO_PEERCRED yalnızca birincil grubu bildirir, bu yüzden `panely` bu
# grupla bağlanabilecek. Üretimde bu grup `panely-client` olur.
#
# Executor soketi kasten yok: erişilemeyen executor'ın DOĞRULANAMADI
# olarak raporlandığını (GEÇERSİZ değil) burada sınıyoruz.
"$BIN/panelyd" \
    -socket "$SOCK" \
    -db "$DB" \
    -client-group "$(id -gn)" \
    -exec-socket "$WORK/olmayan-exec.sock" \
    > "$LOG" 2>&1 &
DAEMON_PID=$!

for _ in $(seq 1 50); do
    [[ -S "$SOCK" ]] && break
    sleep 0.1
done
if [[ ! -S "$SOCK" ]]; then
    echo "panelyd soketi açmadı. Günlük:" >&2
    cat "$LOG" >&2
    exit 1
fi

echo
echo "==> panely status"
out="$("$BIN/panely" status "unix://$SOCK" 2>&1)"; code=$?
check "çıkış kodu" 0 "$code"
contains "daemon sürümü gösterildi" "Daemon" "$out"
contains "executor erişilemiyor olarak raporlandı" "ERİŞİLEMİYOR" "$out"
# panelyd root çalışmayı reddediyor; ekranda da root görünmemeli.
if [[ "$out" == *"KURULUM BOZUK"* ]]; then
    echo "  ✗ panelyd root olarak raporlandı" >&2
    fail=1
else
    echo "  ✓ panelyd yetkisiz kullanıcı olarak çalışıyor"
fi

echo
echo "==> panely status --json"
out="$("$BIN/panely" status --json "unix://$SOCK" 2>&1)"; code=$?
check "çıkış kodu" 0 "$code"
contains "JSON gövdesi" '"daemon_version"' "$out"
contains "proto alan adları korundu" '"executor_reachable"' "$out"

echo
echo "==> panely audit list"
out="$("$BIN/panely" audit list "unix://$SOCK" 2>&1)"; code=$?
check "çıkış kodu" 0 "$code"
# panelyd açılışta zincire bir daemon.start kaydı yazar.
contains "başlangıç kaydı zincirde" "daemon.start" "$out"
contains "sonuç sütunu" "BAŞARILI" "$out"

echo
echo "==> panely audit verify"
out="$("$BIN/panely" audit verify "unix://$SOCK" 2>&1)"; code=$?
# ASIL SORU: executor erişilemezken çıkış kodu 1 mi (doğrulanamadı),
# yoksa 3 mü (kurcalama)? 3 dönerse her yeniden başlatma sahte bir
# güvenlik alarmı üretirdi.
check "erişilemeyen executor çıkış kodu (1 = doğrulanamadı, 3 = kırık)" 1 "$code"
contains "daemon zinciri geçerli" "GEÇERLİ" "$out"
contains "executor zinciri doğrulanamadı" "DOĞRULANAMADI" "$out"
if [[ "$out" == *"GEÇERSİZ"* ]]; then
    echo "  ✗ erişilemeyen zincir GEÇERSİZ olarak raporlandı" >&2
    fail=1
else
    echo "  ✓ erişilemeyen zincir kurcalama olarak raporlanmadı"
fi

echo
echo "==> panely sidecar (stdio JSON-RPC)"
out="$(printf '%s\n%s\n' \
    '{"jsonrpc":"2.0","id":1,"method":"version"}' \
    "{\"jsonrpc\":\"2.0\",\"id\":2,\"method\":\"status\",\"params\":{\"target\":\"unix://$SOCK\"}}" \
    | "$BIN/panely" sidecar 2>&1)"; code=$?
check "çıkış kodu" 0 "$code"
contains "version yanıtı" '"protocol"' "$out"
contains "status yanıtı gerçek sunucudan geldi" '"daemon_version"' "$out"

echo
echo "==> yetkisiz çağıran reddediliyor mu?"
# Soket 0660 ve grup sahipliği ile korunuyor; ayrıca SO_PEERCRED her
# bağlantıda grubu yeniden denetliyor. İzinleri kasten gevşetip
# SO_PEERCRED'in tek başına yeterli olduğunu göstermek root gerektirdiği
# için burada yalnızca soket modunu doğruluyoruz.
mode="$(stat -c '%a' "$SOCK")"
check "soket izinleri" "660" "$mode"

echo
if [[ $fail -eq 0 ]]; then
    echo "Tüm CLI uçtan uca kontrolleri geçti."
else
    echo "Bazı kontroller BAŞARISIZ." >&2
fi
exit $fail
REMOTE

WSL_RUNNER="$(wsl_path "$(cd "$OUT_DIR" && pwd -W)")/e2e-runner.sh"

echo "==> WSL ($WSL_DISTRO) içinde çalıştırılıyor"
MSYS_NO_PATHCONV=1 wsl.exe -d "$WSL_DISTRO" -- bash "$WSL_RUNNER" "$WSL_BIN"
