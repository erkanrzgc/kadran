#!/usr/bin/env bash
# CLI'ı gerçek bir panelyd'ye karşı uçtan uca doğrular. LINUX'TA ÇALIŞIR.
#
# # Bu betik neden var?
#
# Birim testleri istemciyi sahte bir sunucuya karşı sınıyor. Sahte sunucu,
# gerçeğinin yaptığı iki şeyi YAPMIYOR: SO_PEERCRED ile çağıranı doğrulamak
# ve kimlik önsözünü okumak. İkisi de yalnızca Linux'ta çalışır — ve K-012'de
# düzeltilen hata tam olarak bu boşlukta yaşıyordu.
#
# Root GEREKTİRMEZ. İstemci grubu olarak kullanıcının kendi birincil grubu
# kullanılıyor; üretimde bu `panely-client` olur. Ayrıcalık izolasyonunun
# root gerektiren doğrulaması ayrı bir betikte (scripts/e2e-executor.sh).
#
# Kullanım:
#   scripts/e2e-cli-runner.sh <binary-dizini>
#
# Windows iş istasyonundan WSL üzerinden çalıştırmak için:
#   scripts/e2e-cli.sh

set -uo pipefail

BIN="${1:?kullanım: e2e-cli-runner.sh <binary-dizini>}"

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

lacks() {
    local name="$1" needle="$2" haystack="$3"
    if [[ "$haystack" == *"$needle"* ]]; then
        printf '  ✗ %s — %q çıktıda var, olmamalıydı\n' "$name" "$needle" >&2
        fail=1
    else
        printf '  ✓ %s\n' "$name"
    fi
}

cleanup() {
    if [[ -n "${DAEMON_PID:-}" ]]; then
        kill "$DAEMON_PID" 2>/dev/null || true
        wait "$DAEMON_PID" 2>/dev/null || true
    fi
    rm -rf "$WORK"
}
trap cleanup EXIT

chmod +x "$BIN/panelyd" "$BIN/panely"

echo "==> panelyd başlatılıyor (kullanıcı: $(id -un), grup: $(id -gn))"
# Executor soketi kasten YOK: erişilemeyen executor'ın DOĞRULANAMADI olarak
# raporlandığını (GEÇERSİZ değil) burada sınıyoruz — K-013.
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
# panelyd root ile başlamayı reddediyor; ekranda da root görünmemeli.
lacks "panelyd yetkisiz kullanıcı olarak çalışıyor" "KURULUM BOZUK" "$out"

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
# ASIL SORU: executor erişilemezken çıkış kodu 1 mi (doğrulanamadı), yoksa
# 3 mü (kurcalama)? 3 dönerse her yeniden başlatma sahte alarm üretirdi.
check "erişilemeyen executor çıkış kodu (1 = doğrulanamadı, 3 = kırık)" 1 "$code"
contains "daemon zinciri geçerli" "GEÇERLİ" "$out"
contains "executor zinciri doğrulanamadı" "DOĞRULANAMADI" "$out"
lacks "erişilemeyen zincir kurcalama olarak raporlanmadı" "GEÇERSİZ" "$out"

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
echo "==> soket izinleri"
# Soket 0660 ve grup sahipliğiyle korunuyor; ayrıca SO_PEERCRED her
# bağlantıda grubu yeniden denetliyor. İzinleri kasten gevşetip
# SO_PEERCRED'in tek başına yettiğini göstermek root gerektirdiği için
# scripts/e2e-executor.sh'a bırakıldı.
check "soket modu" "660" "$(stat -c '%a' "$SOCK")"

echo
if [[ $fail -eq 0 ]]; then
    echo "Tüm CLI uçtan uca kontrolleri geçti."
else
    echo "Bazı kontroller BAŞARISIZ." >&2
fi
exit $fail
